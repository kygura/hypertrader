// The reasoning engine wires providers to the gate output and the journal. It
// runs LLM calls in their own goroutines with timeouts, so the rest of the
// system never blocks on the model — the LLM sits adjacent to the hot path.
package reasoner

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
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

// Engine consumes gated digests, reasons, and emits validated verdicts on the bus.
type Engine struct {
	bus       *bus.Bus
	registry  *Registry
	digestsIn <-chan metrics.Digest
	timeout   time.Duration

	// onVerdict, if set, is called for each emitted verdict (the executor /
	// journal hook). Verdicts are also published on the bus regardless.
	onVerdict func(Verdict)
}

// NewEngine builds the batch-reasoning engine reading from the gate's output.
func NewEngine(b *bus.Bus, reg *Registry, digestsIn <-chan metrics.Digest, onVerdict func(Verdict)) *Engine {
	return &Engine{
		bus:       b,
		registry:  reg,
		digestsIn: digestsIn,
		timeout:   90 * time.Second,
		onVerdict: onVerdict,
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
		batch := pending
		pending = nil
		timerC = nil
		go e.reason(ctx, batch)
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

// reason runs one batch completion with a timeout and publishes the verdicts.
func (e *Engine) reason(ctx context.Context, digests []metrics.Digest) {
	provider, model, ok := e.registry.For(RoleBatch)
	if !ok {
		e.bus.PublishStatus(bus.StatusEvent{Detail: "no reasoning provider configured"})
		return
	}
	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	resp, err := provider.Complete(cctx, Request{Role: RoleBatch, Digests: digests, Model: model})
	if err != nil {
		e.bus.PublishStatus(bus.StatusEvent{Provider: provider.Name(), Detail: "reason error: " + err.Error()})
		return
	}
	for _, v := range resp.Verdicts {
		e.bus.PublishVerdict(v)
		if e.onVerdict != nil {
			e.onVerdict(v)
		}
	}
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

var errNoProvider = &reasonerError{"no chat provider configured"}

type reasonerError struct{ msg string }

func (e *reasonerError) Error() string { return e.msg }
