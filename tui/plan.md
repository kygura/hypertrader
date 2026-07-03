# Hyperagent — Autonomous Hyperliquid Scanner & Reasoning Engine

**Stack:** Go · Bubble Tea (TUI) · Lipgloss (all rendering) · raw `net/http` + WebSocket against the HL API · model-agnostic LLM layer
**Thesis:** The edge is not tick-level speed. It is a wide data surface, sampled per timeframe, synthesized by an LLM into ranked trade candidates across markets you would otherwise watch by hand.

---

## 0. Dependency posture

No exchange SDK. Hyperliquid's API is plain HTTPS `POST` + a public WebSocket carrying JSON. Info queries and every market subscription are unauthenticated. The only authenticated surface is the exchange (order) endpoint, which needs EIP-712 signing — implemented once against `go-ethereum/crypto` (keccak + ECDSA), a dep you'd carry for key handling regardless. Owning the signing module is preferable to inheriting an opinionated abstraction over the one dangerous layer.

No charting library. All visualization is built from Lipgloss primitives — styled panes, horizontal bars, block-rune columns, sign-colored deltas. At terminal resolution this conveys magnitude and direction better than a squinty braille line chart.

| Concern | Dependency |
|---|---|
| TUI framework | `charmbracelet/bubbletea` |
| Styling + all rendering | `charmbracelet/lipgloss` (+ `lipgloss/table`, `lipgloss/list`) |
| Input / viewport components | `charmbracelet/bubbles` (textinput, viewport) |
| EIP-712 signing (exec only) | `ethereum/go-ethereum/crypto` |
| WebSocket | `nhooyr.io/websocket` (or `gorilla/websocket`) — thin, for ping/pong + reconnect |
| REST / JSON / channels / config | stdlib (`net/http`, `encoding/json`, `context`); `BurntSushi/toml` optional |
| Telegram | stdlib `net/http` against the Bot API |

That is the whole tree. Everything structural is stdlib + Charm.

---

## 1. Core principle

The LLM is the bottleneck, and that is fine — this is not HFT. Everything mechanical (ingest, aggregation, feature computation, order placement, risk) is deterministic Go on goroutines. The LLM sits *adjacent* to the hot path: it reads batched digests, reasons over historical context, emits structured trade candidates. It never sits inside execution timing.

Three things define the build:

- **Wide surface, low frequency.** Track 10–30 markets. Sample each on its configured timeframe (hourly default). Per batch close, hand the LLM a digest rich enough to reason about perp mechanics, not a tick firehose.
- **History matters.** The LLM needs a large historical sample of price action + perp metrics per asset to build a regime picture. Context is a rolling historical window, not a single snapshot.
- **The TUI is the product.** Simple, navigable panes. Price and perp metrics rendered in Lipgloss, plus a chat pane to talk to the agent directly.

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  INGESTOR     HL websocket → typed events → channels           │
│  AGGREGATOR   ticks → OHLCV + perp metrics per asset/timeframe  │
│  STORE        ring buffers (live) + on-disk history (context)   │
│  BATCHER      on each {timeframe} close → digest per asset       │
│  GATE         deterministic: which assets are decision-worthy    │
│  REASONER     model-agnostic LLM → ranked trade candidates       │
│  EXECUTOR     candidate → risk gates → signed HL order (optional)│
│  JOURNAL      every candidate + fill persisted + Telegram        │
│  TUI          Bubble Tea panes: markets · detail · chat          │
└──────────────────────────────────────────────────────────────┘
        all components communicate over typed Go channels
        (an internal event bus; every consumer subscribes)
```

One goroutine per component, typed channels between them, `context.Context` for cancellation. This is the "well-designed API surface": attach a new consumer (backtester, second client, webhook) by subscribing to a channel; you never edit core logic.

---

## 3. Component detail

### 3.1 Ingestor (no SDK)
Open one WebSocket to `wss://api.hyperliquid.xyz/ws`. Send subscription frames as JSON: `{"method":"subscribe","subscription":{"type":"l2Book","coin":"ETH"}}` and likewise for `trades`, `activeAssetCtx` (funding / OI / mark), `allMids`, and `webData2` (account state). Read frames, stamp each with monotonic receive-time, decode to a typed internal event, push to a channel. No logic.

**Resilience:** a watchdog tracks last-message time per channel; silent past a threshold → reconnect + resubscribe. Library handles ping/pong frames; watchdog handles silent death. On reconnect, backfill the gap via REST `POST /info {"type":"candleSnapshot",...}` so the history window has no holes.

### 3.2 Aggregator
Folds the trade/book stream into OHLCV bars + derived perp metrics, per asset, at **multiple timeframes simultaneously** off one input. Metrics per bar:

- **Price/structure:** OHLC, return, realized vol, range position, swing H/L, distance to key levels.
- **Flow:** trade imbalance (aggressor side), CVD, large-print flag.
- **Perp regime:** funding rate + trajectory, OI delta, basis, liquidation proximity.
- **Cross-asset:** BTC correlation, relative strength vs watchlist basket.

Each metric is a pure function over a buffer, registered in a table — adding one is a single entry. This breadth is the moat.

### 3.3 Store
Two tiers. **Live:** fixed-size ring buffers per (asset, timeframe, metric) — O(1) append, bounded memory. **History:** append-only on-disk bars (one file per asset per day; SQLite if query needs grow) so the reasoner gets a large historical sample, not just RAM contents. On startup, warm the rings from disk + REST `candleSnapshot` backfill so the agent has context immediately, not after hours of uptime.

### 3.4 Batcher
On each timeframe boundary (configurable; hourly default), freeze a **digest** per tracked asset: current metrics + a compact historical series (last N bars) + open-position state + the asset's strategy config. The digest is what the LLM reads. Short-timeframe assets batch often; 4h-swing assets rarely — that cadence difference is the context-window economizer and the reason longer timeframes (where this style performs) get full attention cheaply.

### 3.5 Gate
Deterministic filter on which assets in a batch earn LLM tokens. Default rules: z-score band breach, regime flip, level breach, funding crossing a threshold, position nearing a managed boundary. **Sane default: permissive** — passes all tracked assets every batch (your spec: the LLM reads every digest and decides autonomously). The gate exists so you can tighten per-asset if cost or noise demands. Off switch included.

### 3.6 Reasoner (model-agnostic)
One Go interface:

```go
type Provider interface {
    Complete(ctx context.Context, req Request) (Verdict, error)
}
```

Adapters: **Anthropic** (Messages + tool use), **OpenAI** (chat completions + function calling), and one **OpenAI-compatible** adapter covering **Deepseek** and any base-URL-swappable endpoint — they differ only in `base_url` + model. Provider selectable **per role**: cheap model (Deepseek) for routine batch reasoning, stronger (Anthropic) for chat and escalations.

Input per batch: gated digests + rolling history + recent journal entries. Output is **structured, schema-validated**, never free text:

```json
{
  "asset": "HYPE",
  "timeframe": "1h",
  "action": "open_short | open_long | close | scale | hold | alert_only",
  "size_usd": 2500,
  "entry": { "type": "limit", "price": 41.20 },
  "stop": 43.10,
  "take_profit": 37.50,
  "thesis": "lower-high post-IPO, HIP-3 volume drain, funding flipped positive, OI rising into resistance",
  "confidence": 0.0,
  "requires_confirmation": true
}
```

Malformed → discarded + logged, never executed. `thesis` drives the journal and Telegram message.

### 3.7 Executor (optional, deterministic — the dangerous layer)
A candidate is a *request*, not a command. Hard-coded risk gates run before any order hits the wire: max size per asset, max total exposure, max concurrent positions, daily-loss kill-switch, post-stop cooldown, sanity check that LLM price is within X% of live mid. Any breach → reject + log + Telegram. **No LLM output bypasses these — they are code.**

**Signing (owned, not imported):** build the action hash and EIP-712 typed-data signature against `go-ethereum/crypto`, sign with a Hyperliquid **agent (API) wallet** approved once by the master account via `approveAgent` with a `valid_until` expiry (default 7d, auto-renew). Agent wallet signs trades but **cannot withdraw**; master key never touches the daemon. Sealed module, written once, tested against testnet.

`confirmation_required` (per-asset / per-timeframe) flips between **autonomous** and **propose-then-confirm** (candidate → Telegram with inline approve/reject; executes on tap). **Sane default: propose-then-confirm on, autonomous off.** Autonomy is earned per-asset after the journal proves the candidates.

### 3.8 Journal
Append-only. One NDJSON file per day + a per-position lifecycle file (open thesis → scales → close → realized PnL). Audit trail, backtest corpus, and the memory the reasoner reads back. Every entry mirrored to a Telegram log channel — external record independent of the machine.

---

## 4. TUI — the render layer

Bubble Tea (Elm: `Model`/`Update`/`View`), everything drawn with **Lipgloss**, following the composition model of `charmbracelet/lipgloss/examples/layout/main.go`: panes are styled blocks assembled with `lipgloss.JoinHorizontal` / `JoinVertical`, sized with `Width`/`Height`, framed with `Border`, positioned with `lipgloss.Place`, and colored via `LightDarkFunc` so it adapts to terminal background. A single theme struct holds the palette and per-pane styles, defined once.

### 4.1 Default main view

```
┌─ MARKETS ──────────────┬─ HYPE · 1h ───────────────────────────┐
│ ▸ HYPE  41.2  +3.1% ███▌│ price   41.20   +3.1%  ███████▌······ │
│   BTC   —     -0.8% ▌   │ OI Δ    ▁▂▃▅▆▇█▇▆  +12%                │
│   ETH   —     +1.2% ██  │ funding ▃▄▄▅▇█▇▆▅  +0.011%             │
│   SOL   —     +0.4% ▌   │ basis           +0.04%  ██▌··········· │
│   ...  (watchlist)      │ CVD             -1.2M   ◀████·········· │
│                         │ liq prox        2.1%    ███▌·········· │
│                         │ ─ thesis ───────────────────────────  │
│                         │ lower-high into 43; funding flipped + │
├─────────────────────────┴───────────────────────────────────────┤
│ CHAT  > what's the setup on HYPE right now?                       │
│ agent: 1h printed a lower-high into 43 resistance; OI rising...   │
└───────────────────────────────────────────────────────────────────┘
```

**Markets pane (left)** — the watchlist as a `lipgloss/table` or styled rows: asset · price · %Δ (sign-colored) · an inline horizontal bar whose filled width scales to move magnitude. Selection (▸) drives the detail pane.

**Detail pane (right)** — for the selected asset, composed as a `JoinVertical` stack:

- **History as a bar column** for **OI** and **aggregate funding** — the two metrics where the *shape over time* is the signal. Last N bars rendered as a row of vertical block runes `▁▂▃▅▆▇█` (sparkline built by hand: normalize the series to 0–7, map to the eight block glyphs, color the row by sign/trend). One row each for OI Δ and funding.
- **Per-metric bar rows** for everything scalar — basis, CVD, liquidation proximity, realized vol, etc. Each is a labeled horizontal bar: `label  value  ███████▌········` where fill width encodes magnitude and color encodes sign (CVD gets a centered/diverging bar `◀████···` since it's signed around zero). Bar choice follows the **shape of the data**: time-series-shaped → block column; scalar-magnitude-shaped → horizontal bar.
- **Thesis block** — latest agent thesis for the asset, wrapped in a sub-border.

**Chat pane (bottom)** — `bubbles/viewport` for scrollback + `bubbles/textinput` for entry. Direct line to the agent; on-demand interpretation synthesized from live store + history + journal. Same `Provider` interface as the autonomous loop, so chat and batch reasoning share one code path.

### 4.2 Rendering primitives (build once, reuse)
- `barRow(label string, value float64, min, max float64, signed bool) string` — labeled horizontal bar via repeated block runes + Lipgloss color by sign.
- `blockColumn(series []float64) string` — normalize to the eight vertical-block glyphs, return a colored single-line sparkline.
- `pane(title, body string, focused bool) string` — bordered box; focused pane gets an accent border color.
- Compose the frame with `JoinHorizontal(Top, markets, detail)` then `JoinVertical(Left, topRow, chat)`; truncate to detected terminal width as the layout example does.

### 4.3 Navigation
`tab` cycles focus (markets → detail → chat); `1/2/3` jump directly. `t` cycles timeframe on the focused asset. `/` filters the watchlist. `enter` sends in chat. A status line shows connection health, active provider, and mode (autonomous vs propose). Focused pane indicated by accent border (Lipgloss).

### 4.4 Config (sane defaults)
Single `config.toml`, hot-reloadable:

```toml
[markets]
visualized = ["HYPE","BTC","ETH","SOL"]   # shown in TUI
tracked    = ["HYPE","ETH","SOL"]         # actively reasoned by LLM (subset)

[timeframe]
default = "1h"
per_asset = { BTC = "4h" }

[reasoner]
batch_provider = "deepseek"     # routine batch reasoning
chat_provider  = "anthropic"    # interactive + escalations
read_every_batch = true         # LLM decides autonomously per batch

[execution]
mode = "propose"                # propose | autonomous
max_position_usd = 5000
max_total_exposure_usd = 15000
max_concurrent = 5
daily_loss_kill_usd = 1000
```

`visualized` vs `tracked` is the explicit split: watch many, reason actively over a subset — expand the surface without paying tokens on everything.

---

## 5. Go-specific notes
- One goroutine per component; typed channels; `context.Context` for clean shutdown and backpressure.
- TUI is a bus consumer: store/journal updates arrive as `tea.Msg` via a channel→Msg bridge goroutine (`p.Send(...)`). The render loop never blocks on network or LLM. If the TUI dies, the daemon trades on; the two are separable (TUI attaches/detaches over a local Unix socket).
- LLM calls run in their own goroutines with timeouts; results return as messages.
- Reliability win over Python: single static binary, goroutines instead of asyncio, native channel backpressure, no async-runtime fragility. This is the reason for Go.

---

## 6. Build order (each stage independently useful)
1. **Ingestor + Aggregator + Store** — raw WS + REST, multi-timeframe bars + perp metrics, persist + backfill. Prove the surface. Verify via logs, no UI.
2. **TUI main view** — markets pane + detail pane (block columns for OI/funding, bar rows for scalars) + navigation + Lipgloss theme. A live multi-market terminal with zero LLM.
3. **Batcher + Gate + Reasoner + Journal + Telegram + Chat pane** — read-only autonomy: batches, reasons, ranks, journals, alerts, answers in chat — but **cannot trade**. Run live for weeks to calibrate the gate and trust the theses.
4. **Executor** — owned signing module + agent wallet + risk gates, `propose` mode first, flip to `autonomous` per-asset only once the stage-3 journal proves the candidates.

**Stage 3 is the gate. Do not skip to stage 4.** Its journal is the evidence base for granting autonomy; the stage-4 risk gates are the only thing between a hallucinated candidate and your balance.

---

*Openclaw for trading: a single Go binary that watches the markets you can't, reasons over perp mechanics on your timeframe, and hands you ranked, journaled trade candidates — executing only when you let it.*
