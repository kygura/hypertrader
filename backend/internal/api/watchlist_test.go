package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/batcher"
	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/ingestor"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// newWatchlistTestServer starts an httptest server for the given deps,
// filling in the Bus/Store/Cfg every Server needs to boot (runCaches
// subscribes to the bus unconditionally) when the caller didn't set them.
func newWatchlistTestServer(t *testing.T, deps Deps) *httptest.Server {
	t.Helper()
	if deps.Bus == nil {
		deps.Bus = bus.New()
	}
	if deps.Store == nil {
		st, err := store.New(t.TempDir(), 8)
		if err != nil {
			t.Fatal(err)
		}
		deps.Store = st
	}
	if deps.Cfg.Execution.Mode == "" {
		deps.Cfg = config.Default()
	}
	s := NewServer(deps)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func newTrackTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// collectDigests drains digests arriving within the wait window (mirrors
// batcher_test.go's collect helper, duplicated here since it's unexported
// there and this is a different package).
func collectDigests(ch <-chan metrics.Digest, n int, wait time.Duration) []metrics.Digest {
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

func TestWatchlistSubscribeNilIngestorReturns503(t *testing.T) {
	srv := newWatchlistTestServer(t, Deps{})
	resp := postJSON(t, srv, "/api/watchlist/subscribe", map[string]any{"coins": []string{"BTC"}}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestWatchlistSubscribeOpensLiveFeeds(t *testing.T) {
	in := ingestor.New("", nil, bus.New())
	srv := newWatchlistTestServer(t, Deps{Ingestor: in})
	resp := postJSON(t, srv, "/api/watchlist/subscribe", map[string]any{"coins": []string{"BTC"}}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

func TestWatchlistTrackNilBatcherReturns503(t *testing.T) {
	srv := newWatchlistTestServer(t, Deps{})
	resp := postJSON(t, srv, "/api/watchlist/track", map[string]any{"coin": "BTC", "timeframe": "1h"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestWatchlistTrackAddsCoin(t *testing.T) {
	bt := batcher.New(bus.New(), newTrackTestStore(t), nil, nil, nil, 10)
	srv := newWatchlistTestServer(t, Deps{Batcher: bt})
	resp := postJSON(t, srv, "/api/watchlist/track", map[string]any{"coin": "BTC", "timeframe": "1h"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	tracked := bt.Tracked()
	if len(tracked) != 1 || tracked[0] != "BTC" {
		t.Fatalf("Tracked() = %v, want [BTC]", tracked)
	}
}

// requiresConfirmationFor tracks nothing itself: it seeds a bar for coin/tf,
// subscribes to digests, forces a Scan, and reads the RequiresConfirmation
// carried on the resulting digest's StrategyCfg. Batcher exposes no direct
// strategy getter beyond Tracked() (coin names only), so this is the only
// black-box way to observe what Track actually stored.
func requiresConfirmationFor(t *testing.T, b *bus.Bus, st *store.Store, bt *batcher.Batcher, coin, tf string) bool {
	t.Helper()
	st.PutBar(metrics.Bar{Coin: coin, Timeframe: tf, Close: 1, Return: 0})
	ch := b.SubscribeDigests(4)
	bt.Scan(coin)
	got := collectDigests(ch, 1, time.Second)
	if len(got) != 1 {
		t.Fatalf("Scan(%s) published %d digests, want 1", coin, len(got))
	}
	return got[0].StrategyCfg.RequiresConfirmation
}

func TestWatchlistTrackAutonomousModeDoesNotRequireConfirmation(t *testing.T) {
	b := bus.New()
	st := newTrackTestStore(t)
	bt := batcher.New(b, st, nil, nil, nil, 10)
	exec, _ := newFakeExchangeExecutor(t, baseRisk()) // baseRisk().Mode == "autonomous"
	srv := newWatchlistTestServer(t, Deps{Batcher: bt, Bus: b, Store: st, Exec: exec})

	resp := postJSON(t, srv, "/api/watchlist/track", map[string]any{"coin": "BTC", "timeframe": "1h"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if requiresConfirmationFor(t, b, st, bt, "BTC", "1h") {
		t.Errorf("RequiresConfirmation = true, want false in autonomous mode")
	}
}

func TestWatchlistTrackNilExecRequiresConfirmation(t *testing.T) {
	b := bus.New()
	st := newTrackTestStore(t)
	bt := batcher.New(b, st, nil, nil, nil, 10)
	srv := newWatchlistTestServer(t, Deps{Batcher: bt, Bus: b, Store: st})

	resp := postJSON(t, srv, "/api/watchlist/track", map[string]any{"coin": "BTC", "timeframe": "1h"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if !requiresConfirmationFor(t, b, st, bt, "BTC", "1h") {
		t.Errorf("RequiresConfirmation = false, want true when Exec is nil")
	}
}

func TestWatchlistTrackMissingCoinReturns400(t *testing.T) {
	bt := batcher.New(bus.New(), newTrackTestStore(t), nil, nil, nil, 10)
	srv := newWatchlistTestServer(t, Deps{Batcher: bt})
	resp := postJSON(t, srv, "/api/watchlist/track", map[string]any{"timeframe": "1h"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestWatchlistUntrackRemovesCoin(t *testing.T) {
	b := bus.New()
	st := newTrackTestStore(t)
	bt := batcher.New(b, st, nil, nil, map[string]metrics.AssetStrategy{
		"BTC": {Coin: "BTC", Timeframe: "1h"},
	}, 10)
	srv := newWatchlistTestServer(t, Deps{Batcher: bt, Bus: b, Store: st})

	resp := postJSON(t, srv, "/api/watchlist/untrack", map[string]any{"coin": "BTC"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	for _, c := range bt.Tracked() {
		if c == "BTC" {
			t.Fatalf("Tracked() still contains BTC after untrack: %v", bt.Tracked())
		}
	}
}

func TestWatchlistUntrackMissingCoinReturns400(t *testing.T) {
	bt := batcher.New(bus.New(), newTrackTestStore(t), nil, nil, nil, 10)
	srv := newWatchlistTestServer(t, Deps{Batcher: bt})
	resp := postJSON(t, srv, "/api/watchlist/untrack", map[string]any{}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestWatchlistScanNilBatcherReturns503(t *testing.T) {
	srv := newWatchlistTestServer(t, Deps{})
	resp := postJSON(t, srv, "/api/watchlist/scan", map[string]any{}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestWatchlistScanPublishesDigestPerTrackedCoin(t *testing.T) {
	b := bus.New()
	st := newTrackTestStore(t)
	bt := batcher.New(b, st, nil, nil, map[string]metrics.AssetStrategy{
		"BTC": {Coin: "BTC", Timeframe: "1h"},
		"ETH": {Coin: "ETH", Timeframe: "1h"},
	}, 10)
	st.PutBar(metrics.Bar{Coin: "BTC", Timeframe: "1h", Close: 95000, Return: 0.01})
	st.PutBar(metrics.Bar{Coin: "ETH", Timeframe: "1h", Close: 3500, Return: -0.02})

	srv := newWatchlistTestServer(t, Deps{Batcher: bt, Bus: b, Store: st})
	ch := b.SubscribeDigests(16)

	// Empty body ({}) means "scan everything tracked."
	resp := postJSON(t, srv, "/api/watchlist/scan", map[string]any{}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	got := collectDigests(ch, 2, time.Second)
	if len(got) != 2 {
		t.Fatalf("scan published %d digests, want 2: %+v", len(got), got)
	}
	coins := map[string]bool{}
	for _, d := range got {
		coins[d.Coin] = true
	}
	if !coins["BTC"] || !coins["ETH"] {
		t.Fatalf("digests cover %v, want BTC and ETH", coins)
	}
}
