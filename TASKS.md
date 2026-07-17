# TASKS — Harness-backed SOTA reasoning (see SPEC.md)

## Destination

Trading loop's thesis-formation and trade-execution-policy roles each run on their
own harness-CLI-spawned SOTA model by default, with the old direct-API providers
kept registered as a manual, zero-new-code fallback. `go build`/`go test` green in
both `backend/` and `tui/`.

## Decisions so far

- 3 separate adapter files (`pi.go`/`claude.go`/`codex.go`) + shared `harness.go`
  spawn helper, not one generic switch-based provider. Reason: incompatible output
  protocols per CLI, only `claude` verified live this session (opus second opinion,
  logged in SPEC.md).
- Default: `RoleReview` → `claude` harness (verified). `RoleTrigger`/`RoleBatch` →
  `pi` harness + `gpt-5.6-luna` (matches user's cost-efficiency ask; unverifiable
  live this session — no quota — so parser is defensive, fails loud not silent).
  `RoleChat` untouched.
- No dry-run feature added (none requested; verification never touches a live
  exchange). No auto-failover code (existing settings endpoint covers manual
  fallback).
- `codex.go` wired structurally, not defaulted anywhere (no quota to verify).
- `dashboard/` untouched. No TUI screen changes → no DESIGN.md needed.

## Tasks

- [x] Exploration: reasoner/config/provider-selection map (2 subagents, done)
- [x] Exploration: agentic loop/thesis/execution/subprocess-precedent map (done)
- [x] Second opinion on adapter architecture + default assignment (opus, done)
- [x] Live-checked `pi`/`claude`/`codex` CLI invocation shapes (done, see SPEC.md)
- [x] SPEC.md written
- [x] **Task A** (opus, done) — `harness.go`+`pi.go`+`claude.go`+`codex.go` +
      tests, all green (`go build`, `go vet`, `go test ./internal/reasoner/...`,
      11 new tests). Proven empirically (not assumed): claude's `--tools ""`
      genuinely strips tool access (positive/negative control test done live).
      pi confirmed stdin-fed, exits 0 even on error (parser keys on
      `stopReason`, not exit code) — real error envelope captured & tested.
      Deviations: pi gets `--no-session` (ephemeral, defensible); codex has no
      `--system-prompt` flag so system text is prepended to the stdin prompt.
      Scope held to exactly these 4 files — did NOT touch main.go/config.go/
      engine.go (that's Task C).
- [x] **Task B** (sonnet, done) —
      `backend/internal/config/config.go`: extended `Reasoner` with
      ReviewProvider/ReviewModel/TriggerProvider/TriggerModel; `config.toml`
      updated; added `[providers.custom.claude-harness]`/`[providers.custom.pi-harness]`
      stanzas; tests added, `go test ./...` green. FLAG for Task C: `main.go:374`
      `if pc.Kind == "anthropic"` currently misroutes any `harness-*` Kind into the
      OpenAI-compatible HTTP branch — Task C must add the harness dispatch there.
- [x] **Task C** (opus, done) — `engine.go`: `NewRegistry` now seeds all 4 role
      bindings; `Engine.reason()` resolves `role := roleFor(kind)` then
      `registry.For(role)` (was hardcoded `RoleBatch`) — root-cause fix, one
      call site all digest kinds route through. `main.go`: `switch pc.Kind`
      dispatches `harness-pi`/`harness-claude`/`harness-codex` to the Task A
      constructors; harness providers register unconditionally, `harness.go`'s
      own `exec.LookPath` failure surfaces as a call-time error (no separate
      guard needed — avoided redundant code). `settings.go` was hardcoded to
      batch/chat only — extended (not a new endpoint) so review/trigger are
      GET-visible and PUT-switchable. Regression test
      `TestReasonRoutesEachRoleToItsOwnProvider` added — proven to fail on old
      code, pass on fix. `go build`/`go vet`/`go test ./...` all green.
      Judgment call: `ProviderCfg.BaseURL` (meaningless for a subprocess
      harness) repurposed to carry pi's `--provider` sub-backend value
      (defaults `"openai-codex"`), instead of adding a new schema field.
- [x] **Task D** (haiku, done) — `backend/README.md` extended with a
      "Reasoner: providers & role binding" section.
- [x] Independent `go build`/`go test ./...` in `backend/` re-run by
      orchestrator — green.
- [x] Verification gate round 1: review-risk, review-reliability, checker,
      ponytail-review, mp-standards-spec-review (standards+spec axes) all
      landed. `tui/` build/test independently re-run, green.
      Findings (real, required a fix round):
        - BLOCKER (review-risk): harness.go's execRunner never scrubs
          subprocess env — pi/claude/codex inherit HL_AGENT_KEY + all API
          keys for no reason. codex.go's comment falsely claims
          `--sandbox read-only` = no tool access (it only blocks writes).
        - CRITICAL (review-reliability): settings.go review/trigger GET/PUT
          fields have zero test coverage. Only RoleReview journals a
          provider-call failure; RoleTrigger/RoleBatch (now hitting harness
          subprocess failure modes) don't.
        - Standards: settings.go's buildProvider (PUT-key endpoint) has no
          harness-* Kind case — would silently downgrade a harness provider
          to a broken HTTP one.
        - Spec-axis: harnessTimeout=120s neutered by pre-existing 90s
          engine.go outer timeout (dead code vs its own comment).
          provider.go's Role doc comment now stale (checker flagged same).
          [gate] toml diff traced to pre-existing uncommitted change from
          before this session — not scope creep, left alone.
        - ponytail-review: -13 lines (redundant exec.LookPath pre-check;
          1-case table test inlineable).
      SPEC.md's "registers unconditionally" wording corrected by
      orchestrator to describe the actual Custom-map mechanism.
- [x] **Fix round 1**: Task E (opus) — subprocess env now allow-listed (no
      secrets reach pi/claude/codex), codex.go comment corrected to state the
      real (unsolved) tool-access risk instead of a false safety claim, all
      non-chat roles now journal a failed provider call, engine timeout
      90s->120s (the actual effective cutoff), Registry.For fails loud on a
      blank binding, provider.go doc fixed, pi/claude runner-failure test
      parity added. Task F (sonnet) — settings.go's key-set endpoint now
      rejects harness-kind providers instead of silently downgrading them;
      GET/PUT round-trip tests added for review/trigger.
- [x] Re-verified independently by orchestrator: `go build`, `go vet`,
      `go test ./...` green in both `backend/` and `tui/`; spot-checked the
      actual diff (allowlistEnv, journal call, 120s timeout, provider.go
      comment) rather than trusting the reports alone.
- [x] Committed: 4 conventional commits (reasoner adapters, config schema,
      engine/main/settings wiring + hardening fixes, docs).

## Done

Feature complete. `cd backend && go build ./... && go test ./...` and
`cd tui && go build ./... && go test ./...` both green. See final report for
the one remaining known-accepted risk (codex's read-only sandbox doesn't
disable shell/read tools — mitigated by env scrubbing, never defaulted) and
minor un-actioned ponytail-review nit (~13 lines, harness.go's redundant
LookPath pre-check).
- [ ] Final report to user.

## Frontier

Task A and Task B running now.
