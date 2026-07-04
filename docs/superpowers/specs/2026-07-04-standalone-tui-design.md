# Standalone TUI — decouple the Bubble Tea UI into its own Go module

**Date:** 2026-07-04
**Status:** Draft — awaiting user approval
**Context:** follow-on to the tui/→backend/ rename (see `docs/superpowers/specs/2026-07-03-unified-backend-core-design.md`), part of making hypertrader presentable as an alpha pre-product: "one backend core, consumed by both a web dashboard and a Go TUI" needs to be literally true, not just true for the dashboard.

## Goal

`backend/internal/tui` (the Bubble Tea renderer) is compiled into the same
binary as the daemon and holds direct in-process Go references to the bus,
store, engine, and executor via a `Controls` struct built in `backend/src/main.go`.
The dashboard, by contrast, only ever talks to the daemon over the HTTP+WS API
on `:8787`. This sub-project makes the TUI a second, fully independent
consumer of that same API — its own Go module, its own binary, zero import
path into `backend/internal/*` — so the backend really is a standalone
service with two thin clients, not one binary wearing two hats.

## Decisions made

- **Separate Go module/binary**, not a client-only code path inside the
  existing module. A new top-level `tui/` directory gets its own `go.mod`. It
  re-declares the JSON wire types it needs (`Bar`, `Verdict`, `Digest`, etc.)
  from scratch, the same way `dashboard/src/lib/core-client.ts` already does
  in TypeScript, rather than importing `backend/internal/metrics`. This is a
  stronger structural proof for the pitch than a shared module would be, at
  the cost of some duplicated struct definitions.
- **Full control-plane parity.** Every operation the embedded TUI's `Controls`
  struct exposes today (watchlist subscribe/track/untrack/scan, execution
  mode, provider/model switching, settings persistence, API key management)
  gets a real HTTP endpoint. The standalone TUI is a complete replacement for
  the embedded one, not a stripped-down version.
- **Backend always runs "headless."** Once the TUI is out of process, there is
  only one run mode left. The `-headless` flag, `runTUI`, and `runHeadless`'s
  distinction all collapse into one code path in `backend/src/main.go`.

## Approaches considered

1. **Separate module, full API parity (chosen).** Described above.
2. **Same module, client-only code path.** Keep one `go.mod`; `Model` only
   ever holds an `api.Client`, never direct component references; reuses
   `internal/metrics` types directly. Less duplication, faster to build, but
   `backend/` and the TUI stay coupled at the module level — a weaker
   "these are independent programs" story for the pitch. Rejected per user
   decision.
3. **Trim control-plane scope for alpha** (defer provider/model/API-key
   management to config-file-edit-and-restart, ship only watchlist
   track/untrack/scan + mode over HTTP). Smaller surface, but the standalone
   TUI would regress a real feature (the live settings modal) relative to
   what exists today. Rejected per user decision — full parity chosen.

## Architecture

```
                         backend/ (always headless now)
ingestor → aggregator → store ─┐
                    batcher → reasoner.Registry → executor
                         │        │                  │
                       (bus: bars, digests, verdicts, journal, status)
                         │
                    api.Server — HTTP + WS, 127.0.0.1:8787
                    (existing read/act routes + NEW control-plane routes)
                         │
              ┌──────────┴──────────┐
        dashboard/ (web)        tui/ (NEW, separate go.mod)
        core-client.ts          api client + Bubble Tea renderer
        (existing, unchanged)   (moved from backend/internal/tui,
                                 bridge.go rewritten to consume /api/ws,
                                 Controls rewritten as HTTP calls)
```

`api.Deps` gains three fields it doesn't have today (`*ingestor.Ingestor`,
`*batcher.Batcher`, and a settings-persist function) plus reuses
`Engine.Registry()` — which already exists specifically so callers outside
the reasoning loop can read/mutate provider bindings — instead of adding a
fourth field for the reasoner.

**A dependency the first draft of this spec missed:** `Model` doesn't only
issue commands through `Controls` and receive bus pushes through `bridge.go`
— `markets.go`, `detail.go`, and others call `m.store.LatestBar/History/Mid/
AssetCtx/Position` directly and synchronously on nearly every render frame.
That's a third axis (live reads), not covered by the write-side `Controls` or
the push-side bridge. Component 3 below (`apiclient.Cache`) is the fix: a
client-side mirror of `store.Store`'s read surface, so `markets.go`'s call
sites change from `m.store.X(...)` to `m.cache.X(...)` with no logic changes.
Separately, `Model.thesisFn` calls `thesis.FetchContext(ctx, hlclient, coin,
tf)` directly against the backend's Hyperliquid REST client for the `/g`
thesis-generation command — also missed, fixed by a new passthrough endpoint
(Component 1).

## Components

### 1. New control-plane endpoints on `backend/internal/api`

| Method & path | Body / query | Calls | Notes |
|---|---|---|---|
| `POST /api/watchlist/subscribe` | `{"coins":["BTC","ETH"]}` | `Ingestor.Subscribe(coins...)` | opens live feeds for new visualized coins |
| `POST /api/watchlist/track` | `{"coin":"BTC","timeframe":"1h"}` | builds `metrics.AssetStrategy` (`RequiresConfirmation` from `Exec.Mode()!="autonomous"`, `true` if `Exec` is nil; `MaxPositionUSD` from `Cfg.Execution`) → `Batcher.Track` | intentional fix, not a behavior regression: today's embedded TUI computes this confirm flag once at startup (`buildControls`) and never re-reads it, so a coin tracked after a live `/mode` switch can carry a stale flag; reading `Exec.Mode()` per-request closes that gap |
| `POST /api/watchlist/untrack` | `{"coin":"BTC"}` | `Batcher.Untrack(coin)` | |
| `POST /api/watchlist/scan` | `{"coins":[...]}` (omit/empty → all tracked) | `Batcher.Scan(coins...)` | on-demand digest synthesis |
| `PUT /api/execution/mode` | `{"mode":"propose"\|"autonomous"}` | `Exec.SetMode(mode)` | `Exec` nil → 503; invalid mode or no signer → the underlying error message, 422 |
| `GET /api/settings` | — | `Engine.Registry()`: `Names()`, `ProviderModels()`, `Active(RoleBatch)`, `Active(RoleChat)`; masked key hints from `Cfg.Providers.*` | `{"mode","batch":{"provider","model"},"chat":{"provider","model"},"provider_names":[...],"provider_models":{...},"key_hints":{...}}` |
| `PUT /api/settings` | `{"chat_provider","chat_model","batch_provider","batch_model"}` | `Registry.SetProvider`/`SetModel` per role that changed, then persist via `config.Save` under the existing config mutex | any empty field leaves that role's current binding untouched |
| `PUT /api/providers/{name}/key` | `{"key":"sk-..."}` | rebuild the adapter (`buildProvider`, moved from `main.go`) → `Registry.Replace(name, adapter)`, then persist | unknown provider name → 404 |
| `GET /api/thesis/{coin}` | `?tf=1h` (default `Cfg.Timeframe.For(coin)`) | `thesis.FetchContext(ctx, restClient, coin, tf)` (the backend's existing `hlclient.Client`, already constructed in `main.go` for warm-up) | `{"context":"...markdown..."}`; upstream/no-data error → 502. Kept as a backend passthrough rather than having the TUI call Hyperliquid directly, unlike the dashboard's "direct HL websocket for public data" precedent — `thesis.FetchContext` already exists server-side, reusing it verbatim is less code than giving the TUI its own HL REST client, and it keeps the TUI's network surface to exactly one host |

All new handlers follow the existing conventions: `{"error":"..."}` envelope
on failure (`writeErr`, already in `server.go`), registered in `routes()`
alongside the existing table, tested with the same `httptest`-against-`Handler()`
pattern as `act_test.go`/`read_test.go`.

**Existing-bug fix required for the above to work:** `handleMarkets`
(`backend/internal/api/read.go:26`) currently iterates
`Cfg.Markets.Tracked`, but its own doc comment says it serves "the same
three sources the TUI's market panel reads" — the TUI's market panel shows
`Cfg.Markets.Visualized` (watched coins), a set that can be larger than
`Tracked` (reasoned-over coins) — that's the entire point of `/watch` vs
`/track` being separate commands. Today this is invisible because
`config.Default()` sets both lists identically, but the moment a user
`/watch`es a coin without `/track`ing it, that coin gets live bus/WS bar
pushes yet never appears in `GET /api/markets`'s initial snapshot — a
pre-existing bug, not something this sub-project introduces, but one the new
TUI's read cache (Component 3) would otherwise inherit silently. Fix: iterate
`Cfg.Markets.Visualized` instead of `Cfg.Markets.Tracked`, and add a
`Position metrics.Position` field to `marketEntry` (from `Store.Position(coin)`)
since the read cache needs it and there is no WS topic for position updates.

### 2. `backend/internal/api` — `Deps` and settings persistence

`Deps` grows:

```go
type Deps struct {
    Bus       *bus.Bus
    Store     *store.Store
    Engine    *reasoner.Engine
    Exec      *executor.Executor
    Ingestor  *ingestor.Ingestor  // NEW — nil-safe: watchlist/subscribe → 503 if nil
    Batcher   *batcher.Batcher    // NEW — nil-safe: watchlist/track|untrack|scan → 503 if nil
    Cfg       config.Config
    Version   string
    SaveConfig func(apply func(*config.Config)) error // NEW — the persist closure, moved from main.go's buildControls
}
```

The mutex-guarded `persist` closure, `buildProvider`, `providerCfgFor`,
`setProviderKey`, and `maskKey` helpers currently living in `backend/src/main.go`
move into `backend/internal/api` (or a small new `backend/internal/config`
extension) since they become the API server's job once the TUI no longer owns
that logic in-process. `main.go` shrinks to just wiring `Deps` and starting
the server — `buildControls`, `runTUI`, `-headless` flag, and `runHeadless`
are deleted.

### 3. New `tui/` module (moved from `backend/internal/tui`)

- `go.mod`: `module github.com/hyperagent/tui`, own `go build` target,
  depends only on `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, and
  stdlib `net/http` / `net/url` for the API client — no dependency on the
  `backend` module.
- Pure-layout files (`layout.go`, `view.go`, `theme.go`, `markdown.go`,
  `overlays.go`, `helpview.go`, `ideas.go`) move over unchanged — they operate
  on `Model`'s own fields, not on backend internals. `markets.go`,
  `detail.go`, and `signalview.go` move over with a mechanical rename: every
  `m.store.LatestBar/History/Mid/AssetCtx/Position(...)` call becomes
  `m.cache.LatestBar/History/Mid/AssetCtx/Position(...)` (Component 3) —
  same method names and signatures, so the rendering logic itself doesn't
  change, only the receiver.
- `bridge.go` is rewritten: today it subscribes to the in-process `*bus.Bus`
  and forwards events as `tea.Msg`; it becomes a `/api/ws` client that
  decodes `{"topic":"...","data":...}` frames into the same `tea.Msg` types,
  with reconnect-with-backoff (mirroring the pattern already proven in
  `dashboard/src/hooks/useCoreStream.ts`).
- `Controls` (today: Go closures set in `main.go`) becomes an `apiclient.Client`
  with one method per control-plane endpoint in the table above, each a thin
  `net/http` call returning `error`. `Model`'s `controls Controls` field
  becomes `controls *apiclient.Client`; call sites in `commands.go` and
  `settings.go` are unchanged in shape (still `m.controls.Track(coin, tf)`
  etc.) since the client exposes the same method names.
- A new `apiclient` package inside the `tui` module owns the re-declared wire
  types (`Bar`, `Digest`, `Verdict`, `AssetCtx`, journal entry/event,
  proposal) — hand-derived from the Go JSON producers, exactly as
  `dashboard/src/lib/core-client.ts`'s own header comment already documents
  doing in TypeScript. Each type's doc comment names the backend source type
  it mirrors, matching that existing convention.
- Reads config only for the API base URL and an optional bearer token
  (flag/env, e.g. `-core-url` defaulting to `http://127.0.0.1:8787`); it no
  longer reads `config.toml` for markets/execution/providers — that's the
  daemon's job now, surfaced through `/api/settings` and `/api/health`.

### 4. `apiclient.Cache` — the client-side read mirror

Replaces `Model`'s direct `*store.Store` dependency with a same-shaped local
cache inside the new `apiclient` package:

```go
type Cache struct {
    // internal: mu sync.RWMutex + maps keyed by (coin, tf) and by coin
}
func (c *Cache) LatestBar(coin, tf string) (Bar, bool)
func (c *Cache) History(coin, tf string, n int) []Bar
func (c *Cache) Mid(coin string) float64
func (c *Cache) AssetCtx(coin string) (AssetCtx, bool)
func (c *Cache) Position(coin string) Position
```

Populated three ways:
- **On watch** (`/watch COIN` → `Controls.Subscribe`): `GET /api/bars/{coin}?tf=&n=` backfills history for the newly-visualized coin.
- **On WS `bar`/`mids`/`status` frames** (via the rewritten `bridge.go`): updates the matching cache entry in place — the same role `Store.PutBar`/`PutMids` play in-process today.
- **Periodic poll of `GET /api/markets`** (every 5s — frequent enough that funding/OI/position display doesn't feel stale, infrequent enough not to add meaningful load): refreshes `AssetCtx` and `Position` for the full visualized watchlist, since there is no WS topic for either today (see the `handleMarkets` fix above).

### 5. `backend/src/main.go` simplification

`run()` keeps building the pipeline (ingestor → aggregator → store → batcher
→ gate → engine → executor → journal) and the API server exactly as it does
today, minus the TUI branch. `main()` loses the `-headless` flag (there is
only one mode); `approve-agent` and `mcp` subcommands are unaffected.

## Error handling

- Every new endpoint nil-checks its dependency (`Ingestor`, `Batcher`, `Exec`)
  and returns 503 `{"error":"<component> not configured"}`, matching the
  existing pattern for `Engine`/`Exec` in `act.go`.
- Domain errors from `SetMode`, `SetProvider`, `SetModel`, `Registry.Replace`
  (e.g. "unknown provider", "no agent wallet configured") surface as 422 with
  the underlying error string — same convention as risk-gate rejections in
  `POST /api/orders`.
- The `tui/` API client surfaces transport/HTTP errors as Go `error` values up
  to the Bubble Tea command layer, which renders them as the existing
  transient status-line note (`m.note(...)`) — no new error UI concept needed.

## Testing

- New `backend/internal/api` endpoint tests follow the `httptest`-against-
  `Handler()` pattern already used in `act_test.go`/`read_test.go`: table-
  driven per endpoint, covering the happy path, the nil-dependency 503, and
  one domain-error 422 case each.
- `tui/` module: existing `backend/internal/tui/*_test.go` files that test
  pure rendering/state (layout, markdown, commands, ideas, render, settings,
  smoke-visual) move over and keep passing unchanged. The new `apiclient`
  package gets its own tests for both the `Client` (control-plane calls) and
  the `Cache` (backfill + WS-frame updates + poll refresh) against an
  `httptest.Server` standing in for the backend — no live backend needed to
  test the TUI.
- Manual verification: run `backend/` headless, run the new `tui/` binary
  pointed at it, confirm `/watch`, `/track`, `/untrack`, `/scan`, the settings
  modal (provider/model/API key), and mode toggling all work identically to
  the embedded TUI's current behavior.

## Non-goals

- No change to the dashboard or its existing API usage — `handleMarkets` gains
  a `Position` field and switches `Tracked`→`Visualized`, which only adds
  coins/data to its response, never removes any the dashboard currently reads.
- No other change to the wire-level behavior of any existing endpoint.
- No new transport (still HTTP+WS, no gRPC/Unix sockets).
- Not rewriting the TUI's rendering/UX — this is a wiring change, not a
  redesign of what the TUI looks like.
