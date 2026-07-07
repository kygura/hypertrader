package thesis

import (
	"fmt"
	"sync"
	"testing"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(nil, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// TestDiskRoundTrip verifies write-through persistence: a thesis created in
// one store instance survives into a fresh instance over the same directory —
// the agent wakes up with its views intact.
func TestDiskRoundTrip(t *testing.T) {
	s, dir := newTestStore(t)
	in := metrics.Thesis{
		Coin: "BTC", Direction: "long", Summary: "higher lows into supply",
		Invalidation: 92000, Targets: []float64{105000, 112000},
		Horizon: "weeks", Confidence: 0.7,
	}
	if _, err := s.Upsert(in); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	reopened, err := NewStore(nil, nil, dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.Get("BTC")
	if !ok {
		t.Fatal("thesis lost across restart")
	}
	if got.Direction != "long" || got.Invalidation != 92000 || got.Version != 1 {
		t.Fatalf("round-trip mangled thesis: %+v", got)
	}
	if len(got.Targets) != 2 || got.Targets[1] != 112000 {
		t.Fatalf("targets lost: %v", got.Targets)
	}
	if got.CreatedAt.IsZero() || got.ReviewedAt.IsZero() {
		t.Fatal("lifecycle timestamps not stamped")
	}
}

// TestVersionBumpAndInvalidate verifies the lifecycle: create at v1, update
// bumps the version and keeps CreatedAt, invalidate removes the thesis (and
// its file) entirely — invalidated is no-thesis, not a state.
func TestVersionBumpAndInvalidate(t *testing.T) {
	s, dir := newTestStore(t)
	first, err := s.Upsert(metrics.Thesis{Coin: "ETH", Direction: "short", Confidence: 0.6})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.Version != 1 {
		t.Fatalf("create version = %d, want 1", first.Version)
	}

	second, err := s.Upsert(metrics.Thesis{Coin: "ETH", Direction: "neutral", Confidence: 0.4})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if second.Version != 2 || second.Direction != "neutral" {
		t.Fatalf("update = %+v, want version 2 neutral", second)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("update must preserve CreatedAt")
	}

	if !s.Invalidate("ETH") {
		t.Fatal("invalidate reported nothing removed")
	}
	if _, ok := s.Get("ETH"); ok {
		t.Fatal("thesis still live after invalidation")
	}
	if s.Invalidate("ETH") {
		t.Fatal("second invalidate should be a no-op")
	}

	// The file is gone too: a restart must not resurrect the dead thesis.
	reopened, err := NewStore(nil, nil, dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, ok := reopened.Get("ETH"); ok {
		t.Fatal("invalidated thesis resurrected from disk")
	}
}

// TestNeutralIsDistinctFromNoThesis pins the spec's semantic split: "neutral"
// is a live thesis Get returns; a never-reviewed coin has none.
func TestNeutralIsDistinctFromNoThesis(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Upsert(metrics.Thesis{Coin: "SOL", Direction: "neutral", Confidence: 0.5}); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.Get("SOL"); !ok || got.Direction != "neutral" {
		t.Fatalf("neutral thesis not live: %+v ok=%v", got, ok)
	}
	if _, ok := s.Get("DOGE"); ok {
		t.Fatal("never-reviewed coin reported a thesis")
	}
}

// TestUpsertRejectsUnsafeCoin verifies path-shaped coins can never escape the
// theses directory via the per-coin filename.
func TestUpsertRejectsUnsafeCoin(t *testing.T) {
	s, _ := newTestStore(t)
	for _, coin := range []string{"", "../evil", "a/b"} {
		if _, err := s.Upsert(metrics.Thesis{Coin: coin, Direction: "long"}); err == nil {
			t.Fatalf("coin %q accepted", coin)
		}
	}
}

// TestConcurrentAccess hammers the store from parallel writers and readers;
// run with -race this is the concurrency guarantee, and the final version
// count proves no update was lost under contention.
func TestConcurrentAccess(t *testing.T) {
	s, _ := newTestStore(t)
	const writers, updates = 4, 25

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(2)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < updates; i++ {
				if _, err := s.Upsert(metrics.Thesis{
					Coin: "BTC", Direction: "long",
					Summary: fmt.Sprintf("writer %d update %d", w, i),
				}); err != nil {
					t.Errorf("upsert: %v", err)
				}
			}
		}(w)
		go func() {
			defer wg.Done()
			for i := 0; i < updates; i++ {
				s.Get("BTC")
				s.All()
			}
		}()
	}
	wg.Wait()

	got, ok := s.Get("BTC")
	if !ok {
		t.Fatal("thesis missing after concurrent writes")
	}
	if want := writers * updates; got.Version != want {
		t.Fatalf("version = %d, want %d (lost updates)", got.Version, want)
	}
}
