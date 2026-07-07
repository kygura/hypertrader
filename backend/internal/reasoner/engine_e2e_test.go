package reasoner_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/executor"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/store"
	"github.com/hyperagent/hyperagent/internal/thesis"
)

// scriptProvider is a fake reasoner.Provider that counts calls, records the
// role and digest batch of each, and replays a canned response keyed on role.
// It stands in for a real LLM so the pipeline can be driven deterministically.
type scriptProvider struct {
	mu       sync.Mutex
	calls    int
	byRole   map[reasoner.Role]int
	batches  [][]metrics.Digest
	reviews  []reasoner.ThesisReview
	verdicts []reasoner.Verdict
}

func (p *scriptProvider) Name() string { return "script" }

func (p *scriptProvider) Complete(_ context.Context, req reasoner.Request) (reasoner.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.byRole == nil {
		p.byRole = map[reasoner.Role]int{}
	}
	p.byRole[req.Role]++
	p.batches = append(p.batches, req.Digests)
	if req.Role == reasoner.RoleReview {
		return reasoner.Response{Reviews: p.reviews}, nil
	}
	return reasoner.Response{Verdicts: p.verdicts}, nil
}

func (p *scriptProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func newScriptEngine(t *testing.T, prov *scriptProvider) (*reasoner.Engine, chan metrics.Digest) {
	t.Helper()
	providers := map[string]reasoner.Provider{"script": prov}
	models := map[string][]string{"script": {"m"}}
	reg := reasoner.NewRegistry(providers, models, "script", "m", "script", "m")
	ch := make(chan metrics.Digest, 16)
	eng := reasoner.NewEngine(bus.New(), reg, ch, nil)
	return eng, ch
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestEngineQuietTapeZeroLLMCalls is the spec's "quiet tape ⇒ zero LLM calls":
// with no digests on the wire, the engine must never touch the provider.
func TestEngineQuietTapeZeroLLMCalls(t *testing.T) {
	prov := &scriptProvider{}
	eng, _ := newScriptEngine(t, prov)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	time.Sleep(900 * time.Millisecond) // longer than the 750ms collection window
	if n := prov.callCount(); n != 0 {
		t.Fatalf("quiet tape produced %d LLM calls, want 0", n)
	}
}

// TestEngineRunPartitionsByKind pins the collection-window + kind-partition
// contract: same-kind digests arriving inside one window amortize a single
// call, while different kinds never share a completion.
func TestEngineRunPartitionsByKind(t *testing.T) {
	prov := &scriptProvider{}
	eng, ch := newScriptEngine(t, prov)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	// Two reviews and one trigger, all inside a single window.
	ch <- metrics.Digest{Coin: "BTC", Timeframe: "4h", Kind: metrics.DigestReview}
	ch <- metrics.Digest{Coin: "ETH", Timeframe: "4h", Kind: metrics.DigestReview}
	ch <- metrics.Digest{Coin: "SOL", Timeframe: "1m", Kind: metrics.DigestTrigger}

	if !waitFor(t, 3*time.Second, func() bool { return prov.callCount() >= 2 }) {
		t.Fatalf("expected 2 grouped calls, got %d", prov.callCount())
	}
	// Give any erroneous extra call a moment to show up.
	time.Sleep(200 * time.Millisecond)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if prov.calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (one review group, one trigger group)", prov.calls)
	}
	if prov.byRole[reasoner.RoleReview] != 1 || prov.byRole[reasoner.RoleTrigger] != 1 {
		t.Fatalf("role split = %v, want one review + one trigger", prov.byRole)
	}
	for _, b := range prov.batches {
		if len(b) == 0 {
			continue
		}
		switch b[0].Kind {
		case metrics.DigestReview:
			if len(b) != 2 {
				t.Fatalf("review batch size = %d, want 2 (BTC+ETH amortized)", len(b))
			}
		case metrics.DigestTrigger:
			if len(b) != 1 {
				t.Fatalf("trigger batch size = %d, want 1", len(b))
			}
		}
	}
}

// TestEngineReviewThenGatedTrigger walks the spec's two remaining e2e legs:
// an HTF close ⇒ review that creates a thesis, then a deviation ⇒ trigger whose
// verdict is thesis-gated — allowed for the coin with a matching live thesis,
// refused for the coin without one.
func TestEngineReviewThenGatedTrigger(t *testing.T) {
	prov := &scriptProvider{}
	providers := map[string]reasoner.Provider{"script": prov}
	models := map[string][]string{"script": {"m"}}
	reg := reasoner.NewRegistry(providers, models, "script", "m", "script", "m")
	ch := make(chan metrics.Digest, 16)

	b := bus.New()
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	jr, err := journal.New(b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ths, err := thesis.NewStore(b, jr, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := executor.RiskConfig{
		Mode: "propose", MaxPositionUSD: 5000, MaxTotalExposureUSD: 15000,
		MaxConcurrent: 5, DailyLossKillUSD: 1000, MaxPriceDeviation: 0.02,
	}
	exec := executor.New(cfg, b, st, jr, nil, executor.AssetIndex{}, "http://localhost", false)
	exec.SetTheses(ths)

	eng := reasoner.NewEngine(b, reg, ch, exec.Handle)
	eng.AttachThesisStore(ths, func(coin, kind, summary string) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	// Leg 1 — HTF close ⇒ review creates a long thesis for BTC.
	prov.mu.Lock()
	prov.reviews = []reasoner.ThesisReview{{
		Coin: "BTC", Op: "create",
		Thesis: metrics.Thesis{Coin: "BTC", Direction: "long", Confidence: 0.7},
	}}
	prov.mu.Unlock()
	ch <- metrics.Digest{Coin: "BTC", Timeframe: "4h", Kind: metrics.DigestReview}

	if !waitFor(t, 3*time.Second, func() bool { _, ok := ths.Get("BTC"); return ok }) {
		t.Fatal("review did not create the BTC thesis")
	}

	// Leg 2 — deviation ⇒ trigger verdict for BTC. The thesis is long and the
	// verdict is long, so the gate lets it through to a pending proposal.
	prov.mu.Lock()
	prov.reviews = nil
	prov.verdicts = []reasoner.Verdict{{
		Asset: "BTC", Action: metrics.ActionOpenLong, SizeUSD: 1000,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7,
	}}
	prov.mu.Unlock()
	ch <- metrics.Digest{Coin: "BTC", Timeframe: "1m", Kind: metrics.DigestTrigger}

	if !waitFor(t, 3*time.Second, func() bool { return len(exec.Proposals().List()) == 1 }) {
		t.Fatalf("gated trigger for BTC did not produce a proposal (have %d)", len(exec.Proposals().List()))
	}

	// Leg 3 — a trigger for ETH, which has no thesis, must be refused: it never
	// becomes a proposal, so the count stays at 1.
	prov.mu.Lock()
	prov.verdicts = []reasoner.Verdict{{
		Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 1000,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7,
	}}
	prov.mu.Unlock()
	ch <- metrics.Digest{Coin: "ETH", Timeframe: "1m", Kind: metrics.DigestTrigger}

	// Give the trigger time to be reasoned and (correctly) refused.
	time.Sleep(1200 * time.Millisecond)
	if n := len(exec.Proposals().List()); n != 1 {
		t.Fatalf("ungated ETH trigger leaked a proposal: count = %d, want 1", n)
	}
}
