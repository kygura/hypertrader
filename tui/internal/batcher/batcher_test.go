package batcher

import (
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

func newTestBatcher(t *testing.T) (*Batcher, *bus.Bus, *store.Store) {
	t.Helper()
	b := bus.New()
	st, err := store.New(t.TempDir(), 64)
	if err != nil {
		t.Fatal(err)
	}
	strategies := map[string]metrics.AssetStrategy{
		"BTC": {Coin: "BTC", Timeframe: "4h", RequiresConfirmation: true},
		"ETH": {Coin: "ETH", Timeframe: "1h", RequiresConfirmation: true},
	}
	return New(b, st, nil, strategies, 32), b, st
}

// collect drains digests arriving within the wait window.
func collect(ch <-chan metrics.Digest, n int, wait time.Duration) []metrics.Digest {
	var out []metrics.Digest
	deadline := time.After(wait)
	for len(out) < n {
		select {
		case d := <-ch:
			out = append(out, d)
		case <-deadline:
			return out
		}
	}
	return out
}

// TestScanPublishesDigestsForTracked verifies the scan-now path: Scan() builds
// and publishes a digest per tracked coin from the store's current state — no
// timeframe close required; Scan(names...) restricts to the named coins.
func TestScanPublishesDigestsForTracked(t *testing.T) {
	bt, b, st := newTestBatcher(t)
	st.PutBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Close: 95000, Return: 0.01})
	st.PutBar(metrics.Bar{Coin: "ETH", Timeframe: "1h", Close: 3500, Return: -0.02})

	ch := b.SubscribeDigests(16)
	bt.Scan()
	got := collect(ch, 2, time.Second)
	if len(got) != 2 {
		t.Fatalf("Scan() published %d digests, want 2", len(got))
	}
	coins := map[string]bool{}
	for _, d := range got {
		coins[d.Coin] = true
		if d.Current.Close == 0 {
			t.Errorf("digest for %s has empty current bar", d.Coin)
		}
	}
	if !coins["BTC"] || !coins["ETH"] {
		t.Fatalf("digests cover %v, want BTC and ETH", coins)
	}

	bt.Scan("BTC")
	got = collect(ch, 2, 300*time.Millisecond)
	if len(got) != 1 || got[0].Coin != "BTC" {
		t.Fatalf("Scan(BTC) published %v, want exactly one BTC digest", got)
	}

	// Untracked coins are never scanned, named or not.
	bt.Scan("DOGE")
	if got = collect(ch, 1, 300*time.Millisecond); len(got) != 0 {
		t.Fatalf("Scan(DOGE) should publish nothing, got %v", got)
	}
}

// TestScanSkipsCoinsWithoutData verifies a tracked coin with an empty ring is
// skipped rather than published as a zero digest.
func TestScanSkipsCoinsWithoutData(t *testing.T) {
	bt, b, st := newTestBatcher(t)
	st.PutBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Close: 95000})

	ch := b.SubscribeDigests(16)
	bt.Scan()
	got := collect(ch, 2, 300*time.Millisecond)
	if len(got) != 1 || got[0].Coin != "BTC" {
		t.Fatalf("scan with one warm coin published %v, want just BTC", got)
	}
}
