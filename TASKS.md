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

## Done (increment 1)

Feature complete. `cd backend && go build ./... && go test ./...` and
`cd tui && go build ./... && go test ./...` both green. Known-accepted risk:
codex's read-only sandbox doesn't disable shell/read tools (mitigated by env
scrubbing, never defaulted). Minor un-actioned ponytail nit (~13 lines,
harness.go's redundant LookPath pre-check).

## Increment 2 — doctor + in-app auth (coordinator-approved follow-up)

### Destination

`hyperagent doctor` reports per-harness health (binary/auth/model) in plain
script-friendly text. `hyperagent auth <pi|claude|codex>` execs that CLI's
real interactive login with inherited TTY so OAuth/browser flows work,
using an env that keeps HOME/config paths intact (unlike the scrubbed
allow-list used for reasoning calls) but still never leaks exchange signing
keys.

### Real CLI facts (verified locally by orchestrator, trust these)

- `claude auth status` → prints a JSON object with a `loggedIn` bool (plus
  email/org — don't echo that raw into doctor output, extract just the bool).
  `claude auth login` is the interactive login subcommand. `claude doctor`
  exists too (installation health, not auth) — not what we want for the auth
  check.
- `codex login status` → plain text, confirmed real output today:
  `Logged in using ChatGPT`. `codex login` (bare) is the interactive login
  subcommand. `codex doctor --json` also exists (full health incl. auth) —
  either works, `login status` is the narrower/cheaper probe.
  Codex has NO usage quota this session (confirmed repeatedly) — don't spend
  a live completion call proving this feature; smoke-test the Go plumbing
  (subprocess construction, TTY wiring) rather than the actual interactive
  OAuth flow, and don't force a logout/re-login cycle in this shared dev
  environment (codex is already logged in).
- `pi` — **no explicit login/auth/status subcommand found** in `pi --help`
  (only install/remove/update/list/config). Auth appears to be via
  `--api-key` flag, provider env vars (e.g. `ANTHROPIC_OAUTH_TOKEN`), or
  `pi config`'s TUI. `pi --list-models` runs instantly with no visible
  network call and lists models regardless of live auth state, so it is
  NOT a reliable "auth valid" signal — worker must verify further and pick
  the most honest real check, or report plainly that pi has no auth-status
  primitive and doctor should say "unknown, see `pi config`" rather than
  fabricate a check. Same for `auth pi`: if no real interactive login
  subcommand exists, don't fake one — implement `auth pi` as a clear
  informative passthrough (e.g. exec `pi config` for credential management,
  or print how pi actually resolves credentials) instead of silently no-op'ing.
- `main.go` already has a subcommand dispatch pattern (`os.Args[1]` switch,
  one file per subcommand: `approve.go`, `mcp.go`) — follow it exactly, add
  `doctor.go` and `auth.go`.

### Decisions so far

- `auth <harness>` subprocess env: full `os.Environ()` minus a deny-list of
  just `HL_AGENT_KEY`/`HL_MASTER_ADDRESS`/`HL_AGENT_ADDRESS` — opposite
  direction from `harness.go`'s reasoning-call `allowlistEnv()` (which is a
  strict allow-list). Login flows need real HOME/config-home to persist
  credentials; only the exchange signing keys are named as forbidden by the
  coordinator, so nothing else is scrubbed here.
- Doctor's checks reuse the same probe helpers `auth`'s CLI-detection code
  builds — sequential: Task G (opus, auth + probe helpers) then Task H
  (sonnet, doctor + tests) on top.
- HTTP surfacing of doctor status: optional, only if it falls out cheaply
  from Task H's own structured result type — no new TUI screens this round
  regardless.

### Tasks

- [ ] **Task G** (opus, no deps) — `backend/src/auth.go`: `runAuth(args
      []string) error`, dispatches `pi|claude|codex`, execs the real login
      command with `cmd.Stdin/Stdout/Stderr = os.Stdin/Stdout/Stderr`
      (inherited TTY) and the deny-list env above. Wire `case "auth":` into
      `main.go`'s subcommand switch. Also build the shared per-harness probe
      helpers (binary-on-PATH via exec.LookPath, auth-status probe, cheap
      model-reachability signal) that Task H will reuse — verify each CLI's
      real subcommands via `--help` locally before implementing, don't guess
      past what's already confirmed above.
- [ ] **Task H** (sonnet, depends on G) — `backend/src/doctor.go`:
      `runDoctor(args []string) error` using Task G's probe helpers, plain
      text output per harness (binary found / auth ok-expired-unknown /
      model reachable). Wire `case "doctor":` into `main.go`. Tests using
      injected/fake probe results (no real subprocess spawned in `go test`,
      mirroring `harness_test.go`'s convention). Check cheaply whether
      surfacing this through the existing settings/status HTTP endpoint
      falls out naturally — add it if so, skip if it needs new machinery.
- [ ] **Task I** (haiku, depends on H) — docs: `backend/README.md` — document
      `hyperagent doctor` and `hyperagent auth <harness>`.
- [ ] Verification gate: go build/test both modules; review-risk (TTY/env/
      subprocess-exec is the security-relevant surface again) +
      review-reliability; ponytail-review. Fix → re-verify, max 3 rounds.
- [ ] Commit (conventional, granular) + final report.
- [ ] Final report to user.

## Frontier

Task A and Task B running now.
