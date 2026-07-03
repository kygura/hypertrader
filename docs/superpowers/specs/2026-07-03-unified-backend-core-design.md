# Unified Backend Core — HTTP + WS API on the hyperagent daemon

**Date:** 2026-07-03
**Status:** Draft — awaiting user approval
**Sub-project 1 of 4** (then: web intelligence layer → wallet activation smoke → pitch/landing finalization)

## Goal

One backend core, callable by any frontend. Today the daemon's intelligence
(store, digests, verdicts, journal, chat, gated execution) is reachable only
from the in-process TUI and the stdio MCP server. The React dashboard talks
directly to Hyperliquid's public API and has no access to the brain. This
sub-project adds an HTTP + WebSocket API surface to the existing daemon so the
web dashboard (and any future client) consumes the same core the TUI does.

## Decisions made

- **Data path: hybrid.** The dashboard keeps its direct Hyperliquid websocket
  for raw public market data (prices, candles, books). The core API serves only
  what the daemon uniquely owns: derived metrics, digests, theses/verdicts,
  journal, agent chat, and propose/confirm execution. No proxying of public
  feeds. *(Chosen as the recommended default when the user was away; flagged
  for review.)*
- **Embed, don't split.** The API server is a new bus consumer inside the
  existing single binary — exactly the extension pattern `internal/bus`
  documents ("attach a new consumer by subscribing; you never edit core
  logic"). No separate gateway process, no gRPC.

## Approaches considered

1. **Embedded HTTP+WS server in the daemon (chosen).** New `internal/api`
   package, started from `run()` in both TUI and headless modes. One process,
   one store, one executor. Preserves the single-static-binary pitch claim.
2. **Separate gateway process** bridging to the daemon over a Unix socket.
   More moving parts, duplicate state, breaks the one-binary story. Rejected.
3. **Extend the MCP server with HTTP.** The MCP process is per-client stdio
   with REST-cached state and no live store or reasoner — wrong foundation.
   Rejected.

## Architecture

```
ingestor → aggregator → store ─┐
                    batcher → reasoner → executor → journal
                         │        │          │         │
                       (bus: bars, digests, verdicts, journal, status)
                         │
        ┌────────────────┼────────────────────┐
      TUI (in-proc)   MCP (stdio)      api.Server (NEW)
                                        HTTP + WS, 127.0.0.1:8787
                                              │
                                     web dashboard / any client
```

`api.Server` receives the same dependencies the TUI model does: `*bus.Bus`,
`*store.Store`, `*reasoner.Engine`, `*executor.Executor`, journal reader,
`config.Config`. It subscribes to bus topics for the WS push and reads the
store/journal for request/response endpoints.

## Components

### 1. `internal/api` — the server

| Method & path | Serves | Source |
|---|---|---|
| `GET /api/health` | connection state, active providers/models, execution mode, version | bus status cache + registry |
| `GET /api/markets` | latest derived metrics per tracked asset (CVD, basis, funding trend, OI delta, liq proximity, regime) | store rings |
| `GET /api/bars/{coin}?tf=1h&n=100` | OHLCV + metric series | store rings |
| `GET /api/digests/{coin}` | latest frozen digest | batcher output cache |
| `GET /api/verdicts` | latest verdict/thesis per asset | verdict cache (bus-fed) |
| `GET /api/journal?date=YYYY-MM-DD` | journal entries for a day | NDJSON reader |
| `POST /api/chat` | `{message, history[]}` → `{reply, provider, model}` | `engine.Chat` (non-streaming today; SSE later if the engine gains streaming) |
| `GET /api/proposals` | pending propose-mode candidates | proposal registry (new) |
| `POST /api/proposals/{id}/approve` / `.../reject` | confirm flow | registry → `executor.Execute` |
| `POST /api/orders` | direct order (explicit human command, same semantics as MCP `place_order`) | `executor.Execute` |
| `DELETE /api/orders/{coin}/{oid}` | cancel | `executor.Cancel` |
| `GET /api/ws` | push stream | bus subscriptions |

WS frames: `{"topic":"bar|verdict|journal|status|mids","data":{...}}`. Slow
clients inherit the bus's drop-oldest backpressure via per-client buffered
channels.

### 2. Proposal registry (`internal/executor` addition)

Today propose-mode candidates are only broadcast as journal alerts; Telegram
tracks its own approve ids. Add a small registry owned by the executor:
`id → pending verdict`, TTL-expired (default 15 min), `Approve(id)` runs the
existing risk gates + submit path. Telegram's inline buttons and the API's
approve endpoint both call it — one confirm flow, two surfaces. Telegram is
refactored to consume the registry instead of its private map.

### 3. Config

```toml
[api]
enabled = true
addr    = "127.0.0.1:8787"
# token = ""   # optional; when set, Bearer auth required (needed off-localhost)
cors_origins = ["http://localhost:5173"]
```

Default bind is loopback-only, no auth. Setting a non-loopback addr without a
token is a startup error — refuse to run the execution surface open.

### 4. Dashboard thin client (proof only)

`dashboard/src/lib/core-client.ts` (fetch + WS wrapper) and a status pill in
the top nav showing daemon connectivity. Full intelligence UI (chat pane,
theses, journal views) is sub-project 2 and out of scope here.

## Error handling

- JSON errors: `{"error": "..."}` with 4xx/5xx. Risk-gate rejections return
  422 with the specific gate name in the message (parity with MCP behavior).
- Empty store (daemon just started) → 404 with hint, not empty 200s.
- Chat provider failure → 502 with provider error string.
- WS: server pings; dead clients are dropped and unsubscribed.

## Testing

- `httptest` against `api.Server` wired to a real bus + in-memory store fed
  with fixture bars: every endpoint has a request/response test.
- Proposal flow test: publish verdict in propose mode → appears in
  `GET /api/proposals` → approve → executor submit path invoked (fake HTTP
  exchange endpoint) → journal records fill.
- WS test: publish bus events, assert framed delivery and drop-oldest under a
  stalled reader.
- Auth test: token set → unauthenticated 401; non-loopback without token →
  startup error.

## Out of scope

Web intelligence UI, hosted/multi-tenant service, wallet approval ops, streaming
chat, TUI changes.

## Open questions for review

1. Hybrid data path confirmed? (Chosen by default while you were away.)
2. Port 8787 fine, or preference?
3. Should `POST /api/orders` exist at all in v1, or web confirm-only
   (proposals) with direct orders reserved for MCP/TUI?
