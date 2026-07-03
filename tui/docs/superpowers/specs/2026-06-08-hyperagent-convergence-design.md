# Hyperagent Convergence — Design Spec

**Date:** 2026-06-08
**Status:** Approved in brainstorming — pending implementation plan
**Scope:** Make model selection switchable at runtime; build a data→signal interpretation
layer; rebuild the TUI as a chat-first agent surface. The backend pipeline, executor,
and signing module are untouched.

---

## 1. Problem

Two named failures, plus a root cause surfaced during investigation.

1. **Model switching is structurally impossible.** Each provider is constructed with its
   model frozen in (`NewAnthropic(key, model, …)`, `NewOpenAICompatible(name, key, model, …)`);
   the registry maps a *name* → that fixed provider; and `/model` is a literal alias for
   `/provider` (`internal/tui/commands.go:56`). There is no path to change the model *within*
   a provider at runtime. For a multi-model endpoint (e.g. OpenRouter) provider-switching is
   meaningless — the model id is the only real selector.

2. **The TUI buries its own value.** The layout engine is "chat-primary" and, in its default
   `layoutFull` mode, renders a floating chat hero on top of *dimmed* market panes
   (`paneBackground`). The detail pane is an undifferentiated wall of ~14 metrics across ~7
   visual encodings. Eight render primitives + five layout modes + an overlay compositor are
   accretion without editing.

3. **(Root cause) Raw data, not signal.** Every metric is an absolute number with no frame of
   reference — "funding +0.011%", "OIΔ +12%" — uninterpretable without normalizing against the
   asset's own history and without combining metrics. The data is real and fully populated
   (verified across stored bars); the failure is interpretation, not ingestion.

**Direction (decided):** the terminal is a **chat-first agent**. The agent's reasoning is the
product; market data is compact, *interpreted* context that feeds both the human's eye and the
agent's prompt.

---

## 2. Goals / Non-goals

**Goals**
- Runtime model selection independent of provider, with a picker and persistence.
- An interpretation layer that turns raw metrics into ranked, normalized, labeled **signals**,
  consumed by both the detail panel and the reasoner.
- A chat-first TUI: conversation as the hero, a persistent curated detail panel, a condensed
  watchlist strip, and a proactive thesis feed.
- A net reduction in TUI code.

**Non-goals (this pass)**
- Backend pipeline changes (ingestor / aggregator / store / batcher / gate) beyond the additive
  signal layer and the Tier-2 data pulls.
- Executor, signing, autonomous trading — untouched (stays in `propose`).
- `l2Book` / `bbo` order-book ingest — designed-for but deferred (follow-on).

---

## 3. Part A — Provider / model separation (the logic fix)

**Principle:** a *provider* is a transport (base-url + key + wire protocol); a *model* is the id
you select. Bind each role to a `(provider, model)` pair and let the model travel into the request.

- `reasoner.Request` gains `Model string`. `Anthropic.Complete` and `OpenAICompatible.Complete`
  use `req.Model` when non-empty, else fall back to their constructed default model. The adapter
  becomes a dumb transport.
- `Registry` role binding becomes `{provider string, model string}`:
  - `For(role) (Provider, model string, ok bool)` — the engine injects `model` into the Request.
  - `SetProvider(role, name)` — switch transport; reset model to that provider's default.
  - `SetModel(role, id)` — switch model on the role's current provider; **free-form id allowed**.
  - `Active(role) (provider, model string)` — for the status line.
- Config (`config.ProviderCfg`): add `Models []string` (known ids for the picker) and optional
  per-role models `batch_model` / `chat_model` (default to the provider's `model`). **Existing
  `config.toml` keeps working** — every addition is optional.
- TUI:
  - `/model [batch|chat] <id>` — a real command, distinct from `/provider`; free-form id.
  - Model-picker overlay (key `m`): models grouped by provider (from `Models` + the active model)
    plus a "type a model id…" free-form row. Applies to the chat role by default; a toggle picks
    the batch role.
  - Status line shows `chat <provider>·<model>`.
  - Persist role → (provider, model) in the watchlist snapshot file.
- `tui.Controls`: add `SetModel(role, id) error`, `ActiveModel(role) (provider, model string)`,
  and `ProviderModels() map[string][]string`; wire them to the registry in `buildControls`.

---

## 4. Part B — Interpretation layer (`internal/signal`)

A new package (imports only `metrics`). Measurement (the aggregator) stays separate from
interpretation (this layer).

```go
type Signal struct {
    Key      string  // stable id: "oi_price", "funding_regime", …
    Label    string  // human: "new shorts", "crowded longs"
    Score    float64 // signed −1..+1  (− bearish-lean, + bullish-lean)
    Strength float64 // 0..1, distance from neutral — drives ranking
    Read     string  // one-line rationale (panel detail + agent prompt)
}

// Inputs is assembled by the caller so Bar stays lean. Tier-2 fields are
// zero-valued when their data hasn't been fetched, and detectors degrade gracefully.
type Inputs struct {
    Cur      metrics.Bar
    History  []metrics.Bar
    Ctx      metrics.AssetCtx
    // Tier-2 enrichments:
    MaxLeverage     float64
    PrevDayPx       float64
    PredictedFund   float64   // next predicted funding rate
    NextFundingTime time.Time
    OICapped        bool
}

// Compute runs every registered detector and returns their signals.
// Adding a signal is a single registry entry — mirrors the aggregator's metric table.
func Compute(in Inputs) []Signal
```

**Core detectors (Tier 1 — existing data only):**

1. `oi_price` — `sign(OIΔ) × sign(Return)` 2×2 → *new longs / new shorts / short-covering /
   long capitulation*. The core perp regime classifier.
2. `funding_regime` — funding **z-scored against the asset's own recent funding history** +
   `FundingDelta` trajectory → *crowded longs paying / neutral / shorts paying*; extremes carry a
   contrarian squeeze implication.
3. `cvd_div` — CVD trend vs price trend over the last N bars → *confirming / absorbing /
   diverging* + direction (catches hidden distribution: price up, CVD down).
4. `move_sig` — `Return ÷ realized σ` → vol-normalized extension (e.g. "+3.1σ"), so a +3% move
   reads as an event on a calm asset and noise on a wild one.
5. `vol_regime` — `RealizedVol` percentile vs history → *compression (coiling) / normal /
   expansion*; compression precedes breaks.
6. `rel_strength` — `RelStrength` + `BTCCorr` → *leading / lagging the basket; decoupling from BTC*.

**Tier-2 enrichments (cheap REST polls):**

- `funding_regime` consumes `predictedFundings` (forward tilt + next-funding countdown). HL
  `fundingHistory` optionally provides a cleaner series for the z-score; the stored per-bar
  `Funding` series suffices otherwise.
- `liq_pressure` (new — supersedes the broken `LiqProx`, which is currently a copy of `RangePos`):
  uses `maxLeverage` + mark + recent range to estimate proximity to clustered liquidation bands;
  `perpsAtOpenInterestCap` raises a squeeze flag.
- 24h change from `prevDayPx` (panel header); `midPx` as a mid fallback.

**Consumers:**

- **Detail panel:** rank signals by `Strength`; render the strongest 2–3 as
  `arrow  label  strength-bar  read`; a raw "context" strip (funding + OI sparklines, CVD) beneath.
- **Reasoner digest** (`BuildBatchPrompt` / `formatDigest`): include a `signals` array of
  `{key, label, score, read}` per asset alongside the existing raw numbers, so the model reasons
  over interpretation.
- **Chat grounding** (`chatContext`): include the selected asset's top signals.

**Data wiring (Tier 2):**

- `hlclient`: add `FundingHistory(coin, start, end)`, `PredictedFundings()`, `PerpsAtOICap()`;
  extend meta parsing to capture `maxLeverage` per asset; map `prevDayPx` / `midPx` in assetCtx.
- A slow poller (minutes cadence — not hot-path) refreshes predicted funding, OI-cap set, and
  (optionally) funding history, storing them per asset for the signal layer.
- `store`: add light per-asset slots — `MaxLeverage`, `PredictedFunding` + `NextFundingTime`,
  `OICapped` — with `Put*`/getter pairs. `signal.Inputs` is assembled at the call site from the
  store; `Bar` is **not** widened.

---

## 5. Part C — Chat-first TUI

**Layout (one compositor, ≤3 modes, no overlay):**

- **Wide (default):** chat column (~62%) │ detail panel (~38%); watchlist is a selectable strip
  in the panel header.
- **Narrow:** stack — watchlist strip, detail card, chat fills the remainder.
- **Tiny:** chat + a one-line ticker (kept).

**Conversation (the hero):**

- Four turn styles: `you` / agent-reply / agent-thesis (proactive) / `system` (command output).
- **Proactive feed:** route `verdictMsg` into the transcript as a timestamped, asset-tagged,
  confidence-colored agent line; replyable inline. Verdicts still journal as today, and
  `update.go` keeps stashing `thesis` / `reading` for the panel.
- Keep the thinking spinner; improve wrapping and typography.

**Detail panel:** signal-first (Part B) — header (price, 24h Δ, %Δ), ranked signals, the raw
context strip, the batch `reading`, the `thesis`, and a position line when one is open.

**Navigation:** chat input is home; `tab` toggles chat ⇄ watchlist selection; `↑/↓` (or `j/k`
when the watchlist has focus) pick the asset; `m` model picker, `p` provider, `t` timeframe,
`/` command/filter, `?` help. Remove the 3-pane focus cycle, the `1/2/3` jumps, and hero-resize keys.

**Deletions:**
- `internal/tui/floating.go` (the hero card geometry).
- The overlay-hero path in `view.go`; `layoutFull` / `layoutShort` and most of `layout.go`
  (collapse to wide / narrow / tiny).
- `render.go` primitives: `heatBar`, `heatStrip`, `stackedBar`; fold `fundCell` / `oiCell` into a
  single regime glyph used by the watchlist strip.
- `chatGrowW/H`, `resizeChat`.
- Theme: `PaneDim`, `PaneTitleDim`, `TabBar`, `TabActive`, `TabInactive`.
- **Keep:** `blockColumn` (sparkline), `fillBar` / `barRow` (signal strength bars),
  `divergingBar` (CVD and signed values), `pane`, the palette and `AssetColor`.

---

## 6. Testing

- `internal/signal`: table tests per detector (synthetic bar sequences → expected label and score
  sign); a golden test over a recorded HYPE history.
- `reasoner`: registry binding + model-fallback tests; verify the Request carries the bound model.
- `tui`: layout tests for the three modes; a render test for the signal-ranked panel; a
  proactive-feed routing test (verdict → transcript turn).
- `hlclient`: decode tests against captured JSON for the new endpoints.

---

## 7. Build sequence (each step compiles and is independently testable)

1. **Provider/model split (Part A)** — self-contained; unblocks the chat-first model picker.
2. **`internal/signal` Tier-1 detectors + tests** — pure, no new data.
3. **Tier-2 data wiring** — hlclient methods + slow poller + field mapping, feeding the layer.
4. **Reasoner digest + chat grounding consume signals.**
5. **TUI chat-first rebuild** — layout collapse + deletions, conversation + proactive feed,
   signal-first panel, model picker, navigation.

---

## 8. Out of scope / follow-ons

- `l2Book` / `bbo` order-book imbalance — `signal.Inputs` and the detector registry are designed
  to accept a `book_imbalance` detector; wiring the feed is a separate change.
- Repairing the aggregator's `LiqProx` metric itself (dropped from display, superseded by
  `liq_pressure`).
- Executor / signing / autonomous mode.
