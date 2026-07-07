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
	return New(b, st, nil, nil, strategies, 32), b, st
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

// fakeTheses is a minimal ThesisView returning a fixed thesis per coin.
type fakeTheses struct {
	theses map[string]metrics.Thesis
}

func (f *fakeTheses) Get(coin string) (metrics.Thesis, bool) {
	t, ok := f.theses[coin]
	return t, ok
}

// seedLadder fills the store's HTF rings so ladder assembly has data.
func seedLadder(st *store.Store, coin string, tfs map[string]int) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for tf, n := range tfs {
		for i := 0; i < n; i++ {
			st.PutBar(metrics.Bar{
				Coin: coin, Timeframe: tf, Final: true,
				OpenTime: base.Add(time.Duration(i) * time.Hour),
				Close:    100 + float64(i),
			})
		}
	}
}

// TestReviewDigestOnReviewTimeframeOnly verifies kind routing on the bar path:
// a finalized bar on the asset's review timeframe emits a review digest; other
// timeframes, in-progress bars, and untracked coins emit nothing.
func TestReviewDigestOnReviewTimeframeOnly(t *testing.T) {
	bt, b, _ := newTestBatcher(t)
	ch := b.SubscribeDigests(16)

	final := metrics.Bar{Coin: "BTC", Timeframe: "4h", Final: true, Close: 95000}
	bt.onBar(final)
	got := collect(ch, 1, time.Second)
	if len(got) != 1 || got[0].Kind != metrics.DigestReview {
		t.Fatalf("review-TF close published %+v, want one review digest", got)
	}

	// Wrong timeframe, live bar, untracked coin: all silent.
	bt.onBar(metrics.Bar{Coin: "BTC", Timeframe: "1h", Final: true, Close: 95000})
	bt.onBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Final: false, Close: 95000})
	bt.onBar(metrics.Bar{Coin: "DOGE", Timeframe: "4h", Final: true, Close: 1})
	if extra := collect(ch, 1, 300*time.Millisecond); len(extra) != 0 {
		t.Fatalf("non-review bars published %+v, want nothing", extra)
	}
}

// TestReviewDigestCarriesLadderAndThesis verifies review digest content: the
// HTF ladder rungs that have data (missing rungs absent, never fabricated),
// the live thesis, and the review kind.
func TestReviewDigestCarriesLadderAndThesis(t *testing.T) {
	b := bus.New()
	st, err := store.New(t.TempDir(), 256)
	if err != nil {
		t.Fatal(err)
	}
	theses := &fakeTheses{theses: map[string]metrics.Thesis{
		"BTC": {Coin: "BTC", Direction: "long", Invalidation: 92000, Version: 3},
	}}
	strategies := map[string]metrics.AssetStrategy{
		"BTC": {Coin: "BTC", Timeframe: "4h"},
	}
	bt := New(b, st, nil, theses, strategies, 32)
	seedLadder(st, "BTC", map[string]int{"1h": 30, "4h": 10, "1d": 5}) // no 1w data

	ch := b.SubscribeDigests(16)
	bt.onBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Final: true, Close: 95000})
	got := collect(ch, 1, time.Second)
	if len(got) != 1 {
		t.Fatalf("want one digest, got %d", len(got))
	}
	d := got[0]
	if d.Kind != metrics.DigestReview {
		t.Fatalf("kind = %q, want review", d.Kind)
	}
	if len(d.Ladder["1h"]) != 30 || len(d.Ladder["4h"]) == 0 || len(d.Ladder["1d"]) != 5 {
		t.Fatalf("ladder rungs wrong: 1h=%d 4h=%d 1d=%d", len(d.Ladder["1h"]), len(d.Ladder["4h"]), len(d.Ladder["1d"]))
	}
	if _, ok := d.Ladder["1w"]; ok {
		t.Fatal("empty 1w rung must be absent, not fabricated")
	}
	if d.Thesis == nil || d.Thesis.Version != 3 {
		t.Fatalf("thesis missing from review digest: %+v", d.Thesis)
	}
	if d.Deviation != nil {
		t.Fatal("scheduled review must not carry a deviation")
	}
}

// TestTriggerDigestContent verifies the gate-fire path: a threshold deviation
// produces a compact trigger digest carrying the deviation, the LTF timeframe,
// the thesis, and short 4h/1d tails — and untracked coins are ignored.
func TestTriggerDigestContent(t *testing.T) {
	b := bus.New()
	st, err := store.New(t.TempDir(), 256)
	if err != nil {
		t.Fatal(err)
	}
	theses := &fakeTheses{theses: map[string]metrics.Thesis{
		"BTC": {Coin: "BTC", Direction: "long", Version: 1},
	}}
	strategies := map[string]metrics.AssetStrategy{
		"BTC": {Coin: "BTC", Timeframe: "4h"},
	}
	bt := New(b, st, nil, theses, strategies, 32)
	seedLadder(st, "BTC", map[string]int{"4h": 30, "1d": 30})

	ch := b.SubscribeDigests(16)
	ltfBar := metrics.Bar{Coin: "BTC", Timeframe: "5m", Final: true, Close: 95000}
	dev := metrics.Deviation{Rule: "zscore_return", Magnitude: 3.4, Timeframe: "5m"}
	bt.Trigger(ltfBar, dev)

	got := collect(ch, 1, time.Second)
	if len(got) != 1 {
		t.Fatalf("want one digest, got %d", len(got))
	}
	d := got[0]
	if d.Kind != metrics.DigestTrigger {
		t.Fatalf("kind = %q, want trigger", d.Kind)
	}
	if d.Timeframe != "5m" || d.Deviation == nil || d.Deviation.Rule != "zscore_return" {
		t.Fatalf("deviation not carried: tf=%q dev=%+v", d.Timeframe, d.Deviation)
	}
	if len(d.Ladder["4h"]) != 20 || len(d.Ladder["1d"]) != 20 {
		t.Fatalf("HTF summary wrong: 4h=%d 1d=%d, want 20/20", len(d.Ladder["4h"]), len(d.Ladder["1d"]))
	}
	if d.Thesis == nil || d.Thesis.Direction != "long" {
		t.Fatalf("thesis missing from trigger digest: %+v", d.Thesis)
	}

	// Untracked coin: the gate may fire, the batcher stays silent.
	bt.Trigger(metrics.Bar{Coin: "DOGE", Timeframe: "5m", Final: true}, dev)
	if extra := collect(ch, 1, 300*time.Millisecond); len(extra) != 0 {
		t.Fatalf("untracked trigger published %+v, want nothing", extra)
	}
}

// TestInvalidationTriggerForcesReview verifies the escalation: an invalidation
// crossing routes to a forced review digest (full ladder, review kind) with
// the deviation attached, anchored on the review timeframe's latest bar.
func TestInvalidationTriggerForcesReview(t *testing.T) {
	bt, b, st := newTestBatcher(t)
	st.PutBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Final: true, Close: 95000})

	ch := b.SubscribeDigests(16)
	dev := metrics.Deviation{Rule: "invalidation", Magnitude: 92000, Timeframe: "1m"}
	bt.Trigger(metrics.Bar{Coin: "BTC", Timeframe: "1m", Final: true, Close: 91990}, dev)

	got := collect(ch, 1, time.Second)
	if len(got) != 1 {
		t.Fatalf("want one digest, got %d", len(got))
	}
	d := got[0]
	if d.Kind != metrics.DigestReview {
		t.Fatalf("kind = %q, want review (forced)", d.Kind)
	}
	if d.Deviation == nil || d.Deviation.Rule != "invalidation" {
		t.Fatalf("forced review lost its deviation: %+v", d.Deviation)
	}
	if d.Current.Timeframe != "4h" {
		t.Fatalf("forced review anchored on %q, want the review timeframe bar", d.Current.Timeframe)
	}
}

// TestScanEmitsReviewKind verifies the on-demand path still works and rides
// the review tier.
func TestScanEmitsReviewKind(t *testing.T) {
	bt, b, st := newTestBatcher(t)
	st.PutBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Close: 95000})
	ch := b.SubscribeDigests(16)
	bt.Scan("BTC")
	got := collect(ch, 1, time.Second)
	if len(got) != 1 || got[0].Kind != metrics.DigestReview {
		t.Fatalf("Scan published %+v, want one review digest", got)
	}
}
