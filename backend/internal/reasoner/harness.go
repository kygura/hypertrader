// Harness subprocess plumbing shared by the CLI-backed adapters (pi.go,
// claude.go, codex.go). These providers run state-of-the-art models by spawning
// the user's already-authenticated CLI harness as a subprocess instead of
// calling a raw HTTP API with a stored key — so the only new logic vs the HTTP
// adapters is turning a Request into a subprocess invocation and its stdout back
// into plain text. Message building (buildMessages) and response parsing
// (finishResponse) are reused verbatim from openai.go.
package reasoner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// harnessTimeout is a defensive belt-and-suspenders cap on a single subprocess
// completion. It is NOT the effective cutoff: engine.reason() already wraps the
// context with the Engine's own per-call timeout (now 120s, the single source of
// truth for every provider) before calling Complete, and context.WithTimeout can
// only shrink a deadline, never extend it. This local cap just guarantees a bound
// even if a future caller passes a context with no deadline at all.
const harnessTimeout = 120 * time.Second

// maxHarnessOutput caps captured stdout so a runaway subprocess can't exhaust
// memory — same posture as openai.go's io.LimitReader(1<<20) on HTTP bodies,
// with more headroom for JSONL event streams.
const maxHarnessOutput = 4 << 20 // 4 MiB

// runner spawns a CLI subprocess, writes stdin to it, and returns captured
// stdout. Injectable so each adapter's parse logic is unit-testable against a
// fake runner — no real subprocess is spawned in `go test`.
type runner func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error)

// execRunner is the real os/exec-backed runner and the default for every
// adapter. It passes the prompt on the subprocess's stdin — never argv (ARG_MAX
// plus shell-injection risk: market-digest text lands in the prompt) — caps
// stdout, respects ctx timeout/cancellation, and on nonzero exit wraps the error
// with a trimmed stderr snippet (bodySnippet style from openai.go).
func execRunner(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s: binary not found on PATH: %w", bin, err)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	// Deny by default: the CLI inherits ONLY a minimal allow-listed env, never the
	// daemon's full process env. The daemon loads HL_AGENT_KEY (live exchange
	// signing key), HL_MASTER_ADDRESS and provider API keys into its own env
	// (main.go loadDotEnv); none of the reasoning CLIs need any of them — they use
	// their own on-disk login under HOME. Passing them would hand every pi/claude/
	// codex subprocess the exchange key for zero functional reason (and codex's
	// read-only sandbox still lets the model read env vars). AllowlistEnv keeps
	// PATH/HOME/locale/XDG/tool-config only; anything secret-shaped is dropped.
	cmd.Env = AllowlistEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, n: maxHarnessOutput}
	cmd.Stderr = &limitedWriter{w: &stderr, n: 64 << 10}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w", bin, ctx.Err())
		}
		// Only a short trimmed stderr snippet — never dump the full prompt text.
		return nil, fmt.Errorf("%s: %v: %s", bin, err, bodySnippet(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

// envAllowExact are env var names a normal CLI needs to run (shell, locale,
// tmp) plus the config-home vars pi/claude/codex read their own auth/config
// from. envAllowPrefix covers the LC_*/XDG_* families. Everything not matched —
// notably HL_AGENT_KEY, HL_MASTER_ADDRESS, *_API_KEY, ANTHROPIC_*/OPENAI_*/
// DEEPSEEK_* — is dropped. Allow-list, not deny-list: a var not on the list
// never reaches the subprocess even if we didn't anticipate it.
var (
	envAllowExact = map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
		"SHELL": true, "TERM": true, "TZ": true, "TMPDIR": true,
		"PWD": true, "LANG": true, "LANGUAGE": true, "COLORTERM": true,
		// tool config/install homes (normally under HOME; honor overrides)
		"CODEX_HOME": true, "CLAUDE_CONFIG_DIR": true, "BUN_INSTALL": true,
		"NVM_DIR": true, "NODE_PATH": true, "PNPM_HOME": true,
		"VOLTA_HOME": true, "FNM_DIR": true, "ASDF_DIR": true,
	}
	envAllowPrefix = []string{"LC_", "XDG_"}
)

// AllowlistEnv returns the daemon's env filtered to the allow-list above, so a
// subprocess can find its binaries and its own on-disk login without ever
// seeing an exchange key (HL_AGENT_KEY/HL_MASTER_KEY) or an API key. Exported so
// the `auth`/`doctor` login+probe paths in package main share this ONE deny-by-
// default filter instead of maintaining a second, drift-prone deny-list.
func AllowlistEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if envAllowExact[name] {
			out = append(out, kv)
			continue
		}
		for _, p := range envAllowPrefix {
			if strings.HasPrefix(name, p) {
				out = append(out, kv)
				break
			}
		}
	}
	return out
}

// limitedWriter writes at most n bytes to w and silently drops the rest, so a
// runaway subprocess can't fill memory. It always reports the full length as
// written so os/exec doesn't treat the cap as a short-write error.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	total := len(p)
	if l.n <= 0 {
		return total, nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.w.Write(p)
	l.n -= n
	if err != nil {
		return n, err
	}
	return total, nil
}

// splitSystemUser extracts the system prompt and folds the remaining messages
// into a single stdin text block. Harness CLIs take one system flag plus one
// stdin prompt; the non-chat roles only ever produce a single user message, and
// any chat history (rare on this transport) is flattened to labeled lines.
func splitSystemUser(msgs []oaiMessage) (system, user string) {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "system":
			if system == "" {
				system = m.Content
			}
		case "user":
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(m.Content)
		default: // assistant history turns
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(m.Role + ": " + m.Content)
		}
	}
	return system, b.String()
}
