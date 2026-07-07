// The reasoning engine wires providers to the gate output and the journal. It
// runs LLM calls in their own goroutines with timeouts, so the rest of the
// system never blocks on the model — the LLM sits adjacent to the hot path.
package reasoner

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// binding is a role's current (provider, model) pair — the unit of selection. The
// model is part of the binding so it switches independently of the provider.
type binding struct {
	provider string
	model    string
}

// Registry resolves provider names to Provider instances and tracks, per role, the
// active (provider, model). Both are mutable at runtime (the TUI's /provider and
// /model commands) so the active model switches live without a restart — a
// "provider" is a transport, a "model" is what you actually select.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	models    map[string][]string // provider name -> known model ids (picker list)
	roles     map[Role]binding
}

// NewRegistry builds a registry. providers is keyed by name ("anthropic", etc);
// models lists the known model ids per provider for the picker; the role arguments
// seed the initial (provider, model) binding for the batch and chat roles.
func NewRegistry(providers map[string]Provider, models map[string][]string, batchProvider, batchModel, chatProvider, chatModel string) *Registry {
	return &Registry{
		providers: providers,
		models:    models,
		roles: map[Role]binding{
			RoleBatch: {provider: batchProvider, model: batchModel},
			RoleChat:  {provider: chatProvider, model: chatModel},
		},
	}
}

// For returns the provider and bound model for a role, falling back to any available
// provider (with an empty id, signalling the adapter's own default) when the bound
// provider is missing.
func (r *Registry) For(role Role) (Provider, string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b := r.roles[role]
	if p, ok := r.providers[b.provider]; ok {
		return p, b.model, true
	}
	for _, p := range r.providers {
		return p, "", true // any provider beats none; "" → adapter's own default
	}
	return nil, "", false
}

// SetProvider switches a role's transport to the named provider, resetting the model
// to that provider's default (its first known model, else the adapter default).
func (r *Registry) SetProvider(role Role, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[name]; !ok {
		return fmt.Errorf("unknown provider %q (have %v)", name, sortedKeys(r.providers))
	}
	b := r.roles[role]
	b.provider = name
	b.model = r.defaultModel(name)
	r.roles[role] = b
	return nil
}

// SetModel switches a role's model on its current provider. The id is free-form, so
// base-URL-swappable endpoints (OpenRouter, a local vLLM) that expose many models are
// fully addressable — the capability the old /provider-only design lacked.
func (r *Registry) SetModel(role Role, id string) error {
	if id == "" {
		return fmt.Errorf("empty model id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.roles[role]
	if b.provider == "" {
		return fmt.Errorf("no provider set for role %q", role)
	}
	b.model = id
	r.roles[role] = b
	return nil
}

// Replace swaps the Provider instance behind a name, keeping role bindings and
// known-model lists intact. This is how a new API key entered in the settings
// modal takes effect live: the caller rebuilds the adapter with the new key and
// replaces the transport in place — no restart, no rebinding.
func (r *Registry) Replace(name string, p Provider) error {
	if p == nil {
		return fmt.Errorf("nil provider for %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[name]; !ok {
		return fmt.Errorf("unknown provider %q (have %v)", name, sortedKeys(r.providers))
	}
	r.providers[name] = p
	return nil
}

// Names lists registered provider names, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sortedKeys(r.providers)
}

// Active returns the provider and model bound to a role, for status display.
func (r *Registry) Active(role Role) (provider, model string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b := r.roles[role]
	return b.provider, b.model
}

// ProviderModels returns the known model ids per provider for the picker, with each
// role's currently-bound model guaranteed present so the picker can mark it.
func (r *Registry) ProviderModels() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string][]string, len(r.providers))
	for name := range r.providers {
		out[name] = append([]string(nil), r.models[name]...)
	}
	for _, b := range r.roles {
		if b.provider == "" || b.model == "" {
			continue
		}
		if !slices.Contains(out[b.provider], b.model) {
			out[b.provider] = append(out[b.provider], b.model)
		}
	}
	return out
}

// defaultModel returns the first known model for a provider, or "" to let the
// adapter fall back to its constructed default.
func (r *Registry) defaultModel(name string) string {
	if ms := r.models[name]; len(ms) > 0 {
		return ms[0]
	}
	return ""
}

func sortedKeys(m map[string]Provider) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ThesisStore is the write side of the thesis lifecycle the engine drives on
// review responses. Declared here (not imported from internal/thesis) because
// the thesis package journals through internal/journal, which imports reasoner
// for the Verdict type — a direct import would cycle. main.go wires the
// concrete *thesis.Store.
type ThesisStore interface {
	Get(coin string) (metrics.Thesis, bool)
	Upsert(t metrics.Thesis) (metrics.Thesis, error)
	Invalidate(coin string) bool
}

// Engine consumes digests, reasons per tier, and emits validated verdicts on
// the bus. Review digests drive thesis create/update/invalidate; trigger
// digests produce verdicts stamped as trigger-sourced for the executor's
// thesis gate.
type Engine struct {
	bus       *bus.Bus
	registry  *Registry
	digestsIn <-chan metrics.Digest
	timeout   time.Duration

	// onVerdict, if set, is called for each emitted verdict (the executor /
	// journal hook). Verdicts are also published on the bus regardless.
	onVerdict func(Verdict)

	// theses and onJournal are wired via AttachThesisStore. Both nil-tolerant:
	// without them review responses are parsed but not persisted (legacy mode).
	theses    ThesisStore
	onJournal func(coin, kind, summary string)

	// inflight counts reason() goroutines currently running. A flush can spawn
	// a review group and a trigger group concurrently; only the last one to
	// finish drops the status line back to IDLE, so a fast trigger can no longer
	// report IDLE while a review is still reasoning.
	inflight atomic.Int32
}

// NewEngine builds the reasoning engine reading digests from digestsIn.
func NewEngine(b *bus.Bus, reg *Registry, digestsIn <-chan metrics.Digest, onVerdict func(Verdict)) *Engine {
	return &Engine{
		bus:       b,
		registry:  reg,
		digestsIn: digestsIn,
		timeout:   90 * time.Second,
		onVerdict: onVerdict,
	}
}

// AttachThesisStore wires the thesis store the review tier mutates and the
// journal hook used for review misses and discarded thesis JSON. Called once
// at wiring time, before Run.
func (e *Engine) AttachThesisStore(ts ThesisStore, onJournal func(coin, kind, summary string)) {
	e.theses = ts
	e.onJournal = onJournal
}

// journal records via the wired hook, best-effort.
func (e *Engine) journal(coin, kind, summary string) {
	if e.onJournal != nil {
		e.onJournal(coin, kind, summary)
	}
}

// Run batches incoming digests over a short collection window, then reasons over
// them as a group. Grouping lets the model see the watchlist together (relative
// strength is cross-asset) and amortizes the call. Blocks until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	const window = 750 * time.Millisecond
	var pending []metrics.Digest
	var timer *time.Timer
	var timerC <-chan time.Time

	flush := func() {
		if len(pending) == 0 {
			return
		}
		// Partition by kind: reviews and triggers use different prompts, so
		// they can never share a completion — but same-kind digests from
		// several assets still amortize one call.
		groups := make(map[string][]metrics.Digest)
		for _, d := range pending {
			groups[d.Kind] = append(groups[d.Kind], d)
		}
		pending = nil
		timerC = nil
		for kind, batch := range groups {
			// Count the goroutine before it starts so a sibling can never see a
			// zero inflight and publish IDLE while this one is still pending.
			e.inflight.Add(1)
			go e.reason(ctx, kind, batch)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-e.digestsIn:
			if !ok {
				return
			}
			pending = append(pending, d)
			if timer == nil {
				timer = time.NewTimer(window)
				timerC = timer.C
			} else {
				timer.Reset(window)
				timerC = timer.C
			}
		case <-timerC:
			flush()
		}
	}
}

// roleFor maps a digest kind to its prompt role. Legacy kind-less digests keep
// the original batch framing.
func roleFor(kind string) Role {
	switch kind {
	case metrics.DigestReview:
		return RoleReview
	case metrics.DigestTrigger:
		return RoleTrigger
	default:
		return RoleBatch
	}
}

// reason runs one completion for a same-kind digest group with a timeout,
// applies thesis operations (review tier), and publishes the verdicts. Both
// tiers ride the batch provider binding — RoleReview/RoleTrigger select the
// prompt, not the transport.
func (e *Engine) reason(ctx context.Context, kind string, digests []metrics.Digest) {
	// Registered first so every exit — including the no-provider return below —
	// decrements the inflight count. Only the last group returns the line to IDLE.
	defer func() {
		if e.inflight.Add(-1) == 0 {
			e.bus.PublishStatus(bus.StatusEvent{Detail: "IDLE"})
		}
	}()

	provider, model, ok := e.registry.For(RoleBatch)
	if !ok {
		e.bus.PublishStatus(bus.StatusEvent{Detail: "no reasoning provider configured"})
		return
	}
	role := roleFor(kind)

	// Tier status: the TUI's status line shows which asset is being reasoned
	// about and on which tier, then drops back to IDLE.
	for _, d := range digests {
		if role == RoleReview {
			e.bus.PublishStatus(bus.StatusEvent{Detail: fmt.Sprintf("REVIEW %s %s", d.Coin, d.Timeframe)})
		} else if role == RoleTrigger {
			e.bus.PublishStatus(bus.StatusEvent{Detail: fmt.Sprintf("TRIGGER %s %s", d.Coin, d.Timeframe)})
		}
	}

	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	resp, err := provider.Complete(cctx, Request{Role: role, Digests: digests, Model: model})
	if err != nil {
		e.bus.PublishStatus(bus.StatusEvent{Provider: provider.Name(), Detail: "reason error: " + err.Error()})
		if role == RoleReview {
			// A missed review leaves the existing thesis untouched; the next
			// close or trigger retries. The miss itself is journaled.
			for _, d := range digests {
				e.journal(d.Coin, "error", "thesis review missed: "+err.Error())
			}
		}
		return
	}

	if role == RoleReview {
		for _, reason := range resp.Discarded {
			e.journal(coinOf(reason), "error", "thesis review discarded: "+reason)
		}
		e.applyReviews(resp.Reviews)
	}
	for _, v := range resp.Verdicts {
		v.Source = kind
		e.emitVerdict(v)
	}
}

// applyReviews drives the thesis lifecycle from validated review items and
// emits any attached entry verdicts (review-sourced, so the executor's thesis
// gate does not apply to them).
func (e *Engine) applyReviews(reviews []ThesisReview) {
	for _, rv := range reviews {
		if e.theses != nil {
			switch rv.Op {
			case "invalidate":
				e.theses.Invalidate(rv.Coin)
			case "create", "update":
				if _, err := e.theses.Upsert(rv.Thesis); err != nil {
					e.journal(rv.Coin, "error", "thesis persist failed: "+err.Error())
				}
			}
		}
		if rv.Verdict != nil {
			v := *rv.Verdict
			v.Source = metrics.DigestReview
			e.emitVerdict(v)
		}
	}
}

// emitVerdict publishes a verdict on the bus and forwards it to the hook.
func (e *Engine) emitVerdict(v Verdict) {
	e.bus.PublishVerdict(v)
	if e.onVerdict != nil {
		e.onVerdict(v)
	}
}

// coinOf extracts the leading "COIN:" prefix a discard reason carries, or ""
// when the reason isn't coin-specific.
func coinOf(reason string) string {
	if i := strings.IndexByte(reason, ':'); i > 0 && !strings.ContainsRune(reason[:i], ' ') {
		return reason[:i]
	}
	return ""
}

// Chat runs a single interactive completion using the chat-role provider.
func (e *Engine) Chat(ctx context.Context, userMsg string, history []ChatTurn, contextText string) (string, error) {
	provider, model, ok := e.registry.For(RoleChat)
	if !ok {
		return "", errNoProvider
	}
	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	resp, err := provider.Complete(cctx, Request{
		Role:        RoleChat,
		UserMessage: userMsg,
		ChatHistory: history,
		Context:     contextText,
		Model:       model,
	})
	if err != nil {
		return "", err
	}
	return resp.Reply, nil
}

// ChatProviderName returns the name of the chat-role provider for status display.
func (e *Engine) ChatProviderName() string {
	provider, _ := e.registry.Active(RoleChat)
	if provider == "" {
		return "none"
	}
	return provider
}

// ChatModel returns the model id bound to the chat role, for status display.
func (e *Engine) ChatModel() string {
	_, model := e.registry.Active(RoleChat)
	return model
}

// Registry exposes the provider/model registry so callers outside the
// reasoning loop (the health endpoint's provider display) can read active
// bindings without the engine needing to mirror every Registry accessor.
func (e *Engine) Registry() *Registry { return e.registry }

var errNoProvider = &reasonerError{"no chat provider configured"}

type reasonerError struct{ msg string }

func (e *reasonerError) Error() string { return e.msg }
