package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/signing"
	"github.com/hyperagent/hyperagent/internal/store"
)

func TestProposalRegistryAddList(t *testing.T) {
	r := NewProposalRegistry(0)
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 100,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	p := r.Add(v)
	if p.ID == "" {
		t.Fatal("expected non-empty id")
	}
	list := r.List()
	if len(list) != 1 || list[0].ID != p.ID {
		t.Fatalf("List() = %+v, want [%+v]", list, p)
	}
}

func TestProposalRegistryTakeRemoves(t *testing.T) {
	r := NewProposalRegistry(0)
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 100,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	p := r.Add(v)

	got, ok := r.Take(p.ID)
	if !ok || got.ID != p.ID {
		t.Fatalf("Take() = %+v, %v; want %+v, true", got, ok, p)
	}
	if _, ok := r.Take(p.ID); ok {
		t.Fatal("second Take should fail; proposal was removed")
	}
	if len(r.List()) != 0 {
		t.Fatal("List should be empty after Take")
	}
}

func TestProposalRegistryExpiry(t *testing.T) {
	r := NewProposalRegistry(time.Minute)
	fakeNow := time.Now()
	r.now = func() time.Time { return fakeNow }

	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 100,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	p := r.Add(v)

	// Advance the injected clock past the TTL.
	fakeNow = fakeNow.Add(2 * time.Minute)

	if list := r.List(); len(list) != 0 {
		t.Fatalf("List() after expiry = %+v, want empty", list)
	}
	if _, ok := r.Take(p.ID); ok {
		t.Fatal("Take should fail for an expired proposal")
	}
}

func TestProposalRegistryDefaultTTL(t *testing.T) {
	r := NewProposalRegistry(0)
	if r.ttl != 15*time.Minute {
		t.Errorf("default ttl = %v, want 15m", r.ttl)
	}
	r2 := NewProposalRegistry(-time.Second)
	if r2.ttl != 15*time.Minute {
		t.Errorf("negative ttl = %v, want 15m default", r2.ttl)
	}
}

// newFakeExchangeExecutor builds an Executor wired to a real signer and an
// httptest server standing in for the HL exchange endpoint, so Approve's
// Take+Execute path can be exercised end to end.
func newFakeExchangeExecutor(t *testing.T) (*Executor, *httptest.Server, *int) {
	t.Helper()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	t.Cleanup(srv.Close)

	signer, err := signing.NewSigner("3e44cdea317f6553d630e370a143ccbde9ee6fb7aef6d9009443be3606609718")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	jr, err := journal.New(b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := baseRisk()
	e := New(cfg, b, st, jr, signer, AssetIndex{"ETH": {ID: 1, SzDecimals: 4}}, srv.URL, false)
	return e, srv, &calls
}

func TestApproveRegisteredProposalSubmits(t *testing.T) {
	e, _, calls := newFakeExchangeExecutor(t)
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 100,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	p := e.Proposals().Add(v)

	if err := e.Approve(context.Background(), p.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("exchange call count = %d, want 1", *calls)
	}
	if _, ok := e.Proposals().Take(p.ID); ok {
		t.Fatal("approved proposal should have been consumed")
	}
}

func TestApproveUnknownID(t *testing.T) {
	e, _, _ := newFakeExchangeExecutor(t)
	err := e.Approve(context.Background(), "does-not-exist")
	if err == nil || err.Error() != "no such proposal" {
		t.Fatalf("Approve(unknown) = %v, want \"no such proposal\"", err)
	}
}

func TestRejectRegisteredProposal(t *testing.T) {
	e, _, calls := newFakeExchangeExecutor(t)
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 100,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	p := e.Proposals().Add(v)

	if err := e.Reject(p.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("exchange should not be called on reject, got %d calls", *calls)
	}
	if _, ok := e.Proposals().Take(p.ID); ok {
		t.Fatal("rejected proposal should have been consumed")
	}
}

func TestRejectUnknownID(t *testing.T) {
	e, _, _ := newFakeExchangeExecutor(t)
	if err := e.Reject("does-not-exist"); err == nil {
		t.Fatal("expected error rejecting unknown id")
	}
}
