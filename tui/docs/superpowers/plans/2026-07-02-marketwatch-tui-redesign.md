# Marketwatch TUI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the TUI as a marketwatch operation — a wide, scannable market surface — with the LLM synthesis (ranked trade candidates) and per-asset thesis generation surfaced as first-class UI, per plan.md §4.

**Architecture:** The backend pipeline (ingestor → aggregator → store → batcher → gate → reasoner) stays. The rebuild is in the render layer (`internal/tui`) plus one small batcher/main.go addition (on-demand scan). Three thrusts: (1) markets pane per plan §4.1 with the inline move-magnitude bar; (2) detail pane per §4.1 with the full metric stack (price bar, OI/funding block columns, basis/CVD/liq-prox/vol rows) and thesis block; (3) a new IDEAS tab — the ranked candidates board — plus a scan-now path so the LLM synthesizes on demand rather than only on timeframe closes.

**Tech Stack:** Go 1.25, bubbletea v2, lipgloss v2, existing internal packages.

## Global Constraints

- No new dependencies; all rendering from Lipgloss primitives (plan.md §0).
- All existing tests must keep passing (`go test ./...`).
- The LLM never blocks the render loop; all completions run via tea.Cmd / goroutines (plan.md §5).
- Malformed verdicts are never rendered as candidates (schema-validated upstream; the board renders only bus verdicts).
- Watch many, reason over a subset: `visualized` ⊇ `tracked` (plan.md §4.4).

---

### Task 1: Markets pane — the marketwatch grid per plan §4.1

**Files:**
- Modify: `internal/tui/markets.go`
- Test: `internal/tui/render_test.go`

**Interfaces:**
- Produces: column order `COIN · LAST · Δ% · <move bar> · FUND · OIΔ · SIG · 7d`; new col renderer using `Theme.fillBar` scaled to |return| against watchlist max move.

**Steps:**
- [x] Test: `TestMarketsMoveBar` — a row for a coin with the largest |return| renders a wider filled bar than a flat coin; bar column appears when width ≥ its threshold.
- [x] Reorder `allMarketCols()` to COIN, LAST, Δ%, BAR, FUND, OIΔ, SIG, 7d; keep COIN + LAST + Δ% always (marketwatch minimum), responsive extras by min width.
- [x] Implement the BAR column: `fillBar(|ret| / maxAbsRet(watchlist), width≈8)` colored by sign — the plan's "inline horizontal bar whose filled width scales to move magnitude".
- [x] Run `go test ./internal/tui/` — pass.

### Task 2: Detail pane — plan §4.1 metric stack + thesis block

**Files:**
- Modify: `internal/tui/detail.go`
- Test: `internal/tui/render_test.go`

**Interfaces:**
- Consumes: `metrics.Bar{OIDelta, Funding, Basis, CVD, LiqProx, RealizedVol}`, `metrics.AssetCtx`, `Theme.{fillBar, blockColumn, divergingBar}`.
- Produces: detail body ordered exactly as the mockup: price row (+magnitude bar) → `OI Δ` block column → `funding` block column → `basis` bar row → `CVD` diverging bar → `liq prox` bar row → `vol` bar row → flow stacked bar → OI/vol line → **thesis block** → signals (confluence) → position.

**Steps:**
- [x] Test: `TestDetailMetricStack` — detail body contains rows labeled `OI Δ`, `funding`, `basis`, `CVD`, `liq prox`, `vol` in mockup order, price row includes a bar, and the thesis block renders beneath the metric stack.
- [x] Add `barRow(label, value, min, max, signed)` composition on Theme (label · value · fillBar) — the plan §4.2 primitive; use for basis, liq prox, vol.
- [x] Rebuild `renderDetail`: metric stack first (marketwatch read), funding rendered as `blockColumn` (per mockup; keep heat strip out), thesis block after the stack, confluence signals after thesis, position last.
- [x] Run `go test ./internal/tui/` — pass.

### Task 3: IDEAS tab — the ranked candidates board (the synthesis surface)

**Files:**
- Create: `internal/tui/ideas.go`
- Modify: `internal/tui/model.go` (tab const, candidates slice, viewport), `internal/tui/view.go` (tab bar), `internal/tui/update.go` (verdictMsg → board; tab keys; enter-to-jump)
- Test: `internal/tui/ideas_test.go`

**Interfaces:**
- Consumes: `verdictMsg` (already bridged from bus).
- Produces: `chatTabIdeas = 2`; `type candidate struct{ at time.Time; v metrics.Verdict }`; `(*Model).upsertCandidate(v metrics.Verdict)` dedupes per asset (latest wins) and sorts by confidence desc; `(*Model).renderIdeas() string`.

**Steps:**
- [x] Test: `TestUpsertCandidateRanksByConfidence` (dedupe per asset, order by confidence desc) and `TestRenderIdeasShowsRankedRows` (rank · asset · action · confidence bar · levels · thesis; `hold`/`alert_only` rendered dimmer).
- [x] Implement `ideas.go`: board rows — `#1 HYPE  open_short  conf ███▌░░  entry 41.20 stop 43.10 tp 37.50` + wrapped thesis line; action colored (short=down, long=up, close/hold=dim, alert=gold).
- [x] Wire: verdictMsg upserts board + sets `m.thesis[asset]`; tab bar becomes `AGENT · IDEAS · LIVE`; up/down moves board cursor when IDEAS focused; enter re-anchors markets selection to the candidate's asset.
- [x] Run `go test ./internal/tui/` — pass.

### Task 4: Scan-now — on-demand LLM synthesis over the tracked set

**Files:**
- Modify: `internal/batcher/batcher.go` (`Scan(coins ...string)`) 
- Modify: `internal/tui/model.go` (`Controls.ScanNow`), `internal/tui/update.go` (key `S`), `internal/tui/commands.go` (`/scan`), `src/main.go` (wire `bt.Scan`)
- Test: `internal/batcher/batcher_test.go`

**Interfaces:**
- Produces: `(*Batcher).Scan(coins ...string)` — builds a digest per named tracked coin (all tracked when empty) from the store's latest finalized-or-live bar and publishes on the bus (flows through gate → reasoner → verdicts → IDEAS board).
- `tui.Controls.ScanNow func(coins ...string)`.

**Steps:**
- [x] Test: `TestScanPublishesDigestsForTracked` — with two tracked coins and warm store rings, `Scan()` publishes two digests on the bus; `Scan("BTC")` publishes one.
- [x] Implement `Scan` using the existing `buildDigest`; take the latest bar from the store (live bar acceptable — a scan is a "read the tape now" request).
- [x] Wire key `S` (markets/detail focus) + `/scan [COIN…]` → `Controls.ScanNow`, status note "scanning N markets…"; add hint to status line keys.
- [x] Run `go test ./internal/batcher/ ./internal/tui/` — pass.

### Task 5: Widen the watch surface + verify whole

**Files:**
- Modify: `config.toml` (visualized ≈ 12 markets, tracked subset — plan §1 "track 10–30 markets")
- Modify: `internal/tui/helpview.go` (document S / /scan / IDEAS)

**Steps:**
- [x] Expand `visualized` to 12 liquid HL perps; keep `tracked` = 6 subset.
- [x] Update help + welcome copy for the new tabs/keys.
- [x] `go build ./... && go test ./...` — all pass (includes vet's high-confidence analyzers).
- [x] Editor diagnostics (gopls + staticcheck) clean on all changed files; standalone `go vet` run blocked by a transient sandbox-classifier outage during execution — re-run when convenient.
- [x] Visual smoke: `go test ./internal/tui/ -run TestSmokeFrame -v` renders the full wide frame + IDEAS board with seeded data (kept as a permanent render-path guard).
