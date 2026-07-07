# Patient Agent — thesis-driven two-tier reasoning

Date: 2026-07-07
Status: approved

## Problem

The agent reasons on every 1h bar close of every tracked asset. The gate
(`internal/gate`) is hardwired permissive (`DefaultRules()` in
`backend/src/main.go`), digests carry only the single decision timeframe
(120×1h bars), and no thesis state survives between calls. The result is a
reactive scalper: the model re-derives its view hourly from noisy context,
despite the prompt asking for swing/position trading. `reasoner.read_every_batch`
is dead config (parsed, never read).

## Decisions (locked)

1. **Scalp policy: thesis-gated only.** Low-timeframe deviations may only
   trigger entries/exits in the direction of an existing live thesis. No
   thesis, or direction mismatch → no trade; the deviation is journaled and
   may force a thesis review. Enforced deterministically in the executor.
2. **Review cadence: HTF closes + events.** A thesis review runs per asset on
   its review-timeframe bar close (4h/1d per config), plus forced reviews on
   thesis-invalidation crossings and strong deviations without a thesis.
3. **Timeframes.** Thesis context ladder: 1h(120) + 4h(90) + 1d(90) + 1w(52).
   Deviation detection: deterministic rules on 1m/5m/15m finalized bars.
   Aggregator gains 1m, 5m, 1w folds. No monthly bars.
4. **Reasoning pane: latest-per-asset cards + `/clear`.** The pane shows live
   thesis state per asset, replaced in place. `/clear` resets the display
   only; daemon journal files are untouched.

## Architecture

```
WS ticks ──► Aggregator ──► folds 1m 5m 15m 1h 4h 1d 1w per coin
                │
                ├─ review-TF close (4h/1d per asset) ──► Batcher: REVIEW digest
                │      1h/4h/1d/1w ladder + thesis + position + journal
                │
                └─ LTF final bars (1m/5m/15m) ──► Gate (deviation detector)
                       z-score / funding / OI / CVD rules + invalidation watch
                       fires rarely (cooldown) ──► Batcher: TRIGGER digest
                              deviation + thesis + compact HTF context

REVIEW digest ──► Reasoner(RoleReview) ──► thesis create/update/invalidate
                                            (+ optional entry verdict)
TRIGGER digest ─► Reasoner(RoleTrigger) ─► verdict, thesis direction only

verdicts ──► Executor: existing risk gates + thesis gate
```

Inversions from today:

- The LLM is never called on a quiet tape. Gate default flips to
  non-permissive; 1h closes stop being reasoning events.
- The thesis is persisted state, present in every digest, and the executor's
  authorization token for trigger-path trades.
- Invalidation levels are watched deterministically; a crossing forces a
  review — the agent cannot sleep through its level being run.

The on-demand `Batcher.Scan()` path and chat role are unchanged.
`reasoner.read_every_batch` is removed from the config struct.

## Components

### `internal/thesis` (new package)

```go
type Thesis struct {
    Coin         string
    Direction    string    // "long" | "short" | "neutral"
    Summary      string    // narrative the model maintains
    Invalidation float64   // price level: crossed → forced review
    Targets      []float64
    Horizon      string    // "days" | "weeks"
    Confidence   float64   // 0..1
    CreatedAt    time.Time
    ReviewedAt   time.Time
    Version      int       // bumped per update
}
```

Store: RWMutex-guarded map, persisted one JSON file per coin under
`data/theses/`, written through on every update. Every create/update/
invalidate is journaled. `neutral` is a real thesis ("stay out"), distinct
from *no thesis* (never reviewed, or invalidated).

### Aggregator

Fold set per coin becomes `{1m, 5m, 15m, 1h, 4h, 1d, 1w}` plus the asset's
review timeframe. Ring size 512 suffices for all rungs. Warm-up extends to
4h/1d/1w so review digests are rich from first boot.

### Gate → deviation detector

New config section, non-permissive default:

```toml
[gate]
  ltf_timeframes  = ["1m", "5m", "15m"]
  zscore_return   = 3.0
  funding_abs     = 0.0008
  oi_delta_abs    = 0.04
  cvd_zscore      = 3.0
  cooldown        = "30m"     # per (coin, rule)
  position_always = true      # open positions always get their HTF review
```

The gate also compares each LTF close against the coin's thesis invalidation
level and emits a forced-review trigger on crossing. Cooldown caps trigger
rate per (coin, rule).

### Batcher

`metrics.Digest` gains `Kind` (`review` | `trigger`).

- Review digest (on review-TF final bar): full ladder 1h(120)/4h(90)/1d(90)/
  1w(52), thesis, position, recent journal, strategy config.
- Trigger digest (on gate fire): the deviation (rule, magnitude, timeframe),
  thesis, position, compact HTF summary (last ~20 closes of 4h and 1d).

### Reasoner

Two batch-provider roles:

- `RoleReview`: returns a thesis JSON (create/update/invalidate) and may
  attach an entry verdict when the thesis warrants immediate positioning.
- `RoleTrigger`: returns a standard verdict constrained to thesis direction
  or hold.

The 750ms collection window stays (multiple assets' HTF closes share a call).
Thesis JSON validation mirrors `ParseVerdicts`: malformed → discarded and
journaled, prior version stays.

### Executor

New deterministic check alongside existing risk gates: a trigger-path verdict
is refused unless a live thesis exists for the coin and the verdict direction
matches (`close` and `hold` always allowed). Review-path verdicts pass under
today's rules. Refusals journal an explicit reason
(`thesis-gate: no live thesis` / `direction mismatch`).

## API contract (backend ⇄ TUI)

- `GET /api/theses` → `{ "theses": [Thesis…] }` snapshot for pane cold-start.
  Thesis JSON field names: `coin`, `direction`, `summary`, `invalidation`,
  `targets`, `horizon`, `confidence`, `created_at`, `reviewed_at`, `version`.
- WS bus stream gains a `thesis` event carrying the updated Thesis (same
  envelope pattern as verdict/status events).
- Status events distinguish tiers: `IDLE`, `REVIEW <coin> <tf>`,
  `TRIGGER <coin> <tf>`.

## TUI (cockpit)

- Reasoning/verdict area becomes one card per tracked asset, replaced in
  place: direction, confidence, invalidation, targets, horizon, review age,
  latest reasoning sentence(s). Trigger events flash on the owning card
  (`⚡ 5m z=3.4 — entry check…`); the decision journal panel below stays
  chronological (state vs events).
- Card states: live, stale (`ReviewedAt` older than 2× review TF), no-thesis
  (with reason: never reviewed / invalidated at <time>).
- `/clear` chat command wipes pane text and journal scrollback in the TUI
  only.
- `apiclient.Cache` grows `Theses()` mirroring `Position()`.

## Error handling

- Review call failure/timeout (90s): existing thesis stays untouched; miss is
  journaled; next close or trigger retries. Stale rendering per above.
- Malformed thesis JSON: discarded + journaled, prior version stays.
- Trigger verdict without thesis authority: executor refuses, journaled
  reason, visible in journal panel.
- Invalidation crossed while reasoner down: forced-review trigger re-fires on
  cooldown expiry until a review lands; stop/risk machinery is independent of
  the LLM.
- Warm-up gaps: ladder includes whatever rungs have data; prompt notes
  missing rungs rather than fabricating.
- Config migration: files without `[gate]` get the new non-permissive
  defaults; `read_every_batch` removed from the struct (unknown keys ignored
  on load, old files still parse).

## Testing

Table-driven, matching existing package conventions:

- `gate`: thresholds, cooldown suppression, invalidation-cross, default
  non-permissive.
- `thesis`: disk round-trip, version bump, concurrent access.
- `batcher`: review digest only on review-TF final bar; trigger digest
  content; kind routing.
- `reasoner`: prompt construction for both roles; thesis parse/validate with
  malformed inputs.
- `executor`: thesis-gate refusal matrix (no thesis, mismatch, close-allowed,
  review-path bypass).
- `tui/cockpit`: card states, trigger flash, `/clear` (teatest harness).
- End-to-end: scripted bus replay with fake Provider — quiet tape ⇒ zero LLM
  calls; HTF close ⇒ review; deviation ⇒ thesis-gated trigger.
