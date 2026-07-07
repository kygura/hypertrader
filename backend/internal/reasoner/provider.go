// Package reasoner is the model-agnostic LLM layer. One Go interface (Provider)
// abstracts every model; adapters live in subfiles. The reasoner reads gated
// digests and emits structured, schema-validated Verdicts — never free text.
//
// The same Provider interface backs both the autonomous batch loop and the
// interactive chat pane, so they share one code path (the plan's requirement).
package reasoner

import (
	"context"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// The structured-output types live in the dependency-free metrics package so the
// event bus can reference Verdict without importing reasoner (which would create
// a cycle). We re-export them here as aliases so reasoner code reads naturally.
type (
	Action  = metrics.Action
	Entry   = metrics.Entry
	Verdict = metrics.Verdict
)

const (
	ActionOpenShort = metrics.ActionOpenShort
	ActionOpenLong  = metrics.ActionOpenLong
	ActionClose     = metrics.ActionClose
	ActionScale     = metrics.ActionScale
	ActionHold      = metrics.ActionHold
	ActionAlertOnly = metrics.ActionAlertOnly
)

// Role identifies which configured provider to use and which prompt frames the
// request: routine batch reasoning vs interactive chat / escalations. The plan
// splits these so a cheap model can do batches while a stronger one handles
// chat. RoleReview and RoleTrigger are the two thesis-pipeline tiers; both run
// on the batch provider binding — they are prompts, not separate transports.
type Role string

const (
	RoleBatch   Role = "batch"
	RoleChat    Role = "chat"
	RoleReview  Role = "review"  // thesis review on a review-timeframe close
	RoleTrigger Role = "trigger" // gate-fired deviation check
)

// Request is the input to a provider completion. For batch reasoning it carries
// gated digests; for chat it carries the user message plus context.
type Request struct {
	Role Role

	// Model selects the model id for this completion. When empty, the adapter falls
	// back to its constructed default. The registry binds (provider, model) per role
	// and injects the bound model here — this is what makes the model, not just the
	// provider, switchable at runtime.
	Model string

	// Batch inputs.
	Digests []metrics.Digest

	// Chat inputs.
	UserMessage string
	ChatHistory []ChatTurn

	// Shared context: a system framing and any extra grounding text (e.g. recent
	// journal excerpts) the caller wants to inject.
	System  string
	Context string
}

// ChatTurn is one message in the interactive conversation.
type ChatTurn struct {
	Role string // "user" | "assistant"
	Text string
}

// Response is what a provider returns. For batch/trigger requests Verdicts is
// populated; for review requests Reviews carries the thesis operations (with
// any attached entry verdicts); for chat requests Reply holds the assistant's
// text (and the other fields may be empty).
type Response struct {
	Verdicts []Verdict
	Reviews  []ThesisReview
	// Discarded lists review elements dropped during validation, one line
	// each, for the engine to journal — a malformed thesis is discarded and
	// journaled; the prior version stays.
	Discarded []string
	Reply     string
	Model     string
}

// Provider is the single abstraction over every LLM backend.
type Provider interface {
	// Name returns a short identifier for status display and journaling.
	Name() string
	// Complete runs one completion. Implementations must respect ctx for
	// timeout/cancellation — LLM calls run in their own goroutines with timeouts.
	Complete(ctx context.Context, req Request) (Response, error)
}
