package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/thesis"
)

// newHLFixture stands up a fake Hyperliquid /info endpoint that answers
// candleSnapshot and metaAndAssetCtxs in exactly the wire shape hlclient.go
// decodes (rawCandle / assetCtxWire / metaUniverse) — see
// internal/hlclient/hlclient.go. candlesPerInterval controls how many
// synthetic candles come back per candleSnapshot call: 0 reproduces HL's
// "no data for this coin/tf" case, driving thesis.FetchContext's error path.
// onCandleRequest, if non-nil, is invoked (interval string) for every
// candleSnapshot call so tests can assert which timeframes were actually
// fetched (e.g. to verify the per-asset config default was honored).
func newHLFixture(t *testing.T, coin string, candlesPerInterval int, onCandleRequest func(interval string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Type string `json:"type"`
			Req  struct {
				Coin     string `json:"coin"`
				Interval string `json:"interval"`
			} `json:"req"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("fixture: decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch envelope.Type {
		case "candleSnapshot":
			if onCandleRequest != nil {
				onCandleRequest(envelope.Req.Interval)
			}
			type rawCandle struct {
				OpenMillis  int64  `json:"t"`
				CloseMillis int64  `json:"T"`
				Coin        string `json:"s"`
				Interval    string `json:"i"`
				Open        string `json:"o"`
				High        string `json:"h"`
				Low         string `json:"l"`
				Close       string `json:"c"`
				Volume      string `json:"v"`
				NumTrades   int    `json:"n"`
			}
			candles := make([]rawCandle, 0, candlesPerInterval)
			for i := 0; i < candlesPerInterval; i++ {
				candles = append(candles, rawCandle{
					OpenMillis:  int64(i * 1000),
					CloseMillis: int64(i*1000 + 999),
					Coin:        envelope.Req.Coin,
					Interval:    envelope.Req.Interval,
					Open:        "100",
					High:        "110",
					Low:         "90",
					Close:       "105",
					Volume:      "42",
					NumTrades:   10,
				})
			}
			if err := json.NewEncoder(w).Encode(candles); err != nil {
				t.Fatalf("fixture: encode candles: %v", err)
			}
		case "metaAndAssetCtxs":
			meta := map[string]any{
				"universe": []map[string]string{{"name": coin}},
			}
			ctxs := []map[string]string{
				{
					"funding":      "0.0001",
					"openInterest": "1000",
					"markPx":       "105.5",
					"oraclePx":     "105.4",
					"premium":      "0.0005",
					"dayNtlVlm":    "50000",
				},
			}
			metaBytes, err := json.Marshal(meta)
			if err != nil {
				t.Fatalf("fixture: marshal meta: %v", err)
			}
			ctxsBytes, err := json.Marshal(ctxs)
			if err != nil {
				t.Fatalf("fixture: marshal ctxs: %v", err)
			}
			raw := []json.RawMessage{metaBytes, ctxsBytes}
			if err := json.NewEncoder(w).Encode(raw); err != nil {
				t.Fatalf("fixture: encode metaAndAssetCtxs: %v", err)
			}
		default:
			t.Fatalf("fixture: unexpected info type %q", envelope.Type)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestThesisNilRestClientReturns503(t *testing.T) {
	srv := httptest.NewServer(NewServer(testDeps(t, nil)).Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/thesis/BTC?tf=1h")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestThesisHappyPath(t *testing.T) {
	hl := newHLFixture(t, "BTC", 1, nil)
	deps := testDeps(t, nil)
	deps.RestClient = hlclient.New(hl.URL)
	srv := httptest.NewServer(NewServer(deps).Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/thesis/BTC?tf=1h")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["context"], "BTC") {
		t.Fatalf("context = %q, want to contain BTC", body["context"])
	}
}

func TestThesisNoDataReturns502(t *testing.T) {
	hl := newHLFixture(t, "BTC", 0, nil)
	deps := testDeps(t, nil)
	deps.RestClient = hlclient.New(hl.URL)
	srv := httptest.NewServer(NewServer(deps).Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/thesis/BTC?tf=1h")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

// TestThesisDefaultTimeframeFromConfig exercises the tf-omitted branch:
// s.deps.Cfg.Timeframe.For(coin) must supply the display TF. config.Default()
// (used by testDeps) sets Timeframe.Default = "1h" but overrides BTC to "4h"
// via PerAsset — so a request with no ?tf must fetch the "4h"/"1d" ladder
// (ladderFrom("4h")), never "1h" or "15m".
func TestThesisDefaultTimeframeFromConfig(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	hl := newHLFixture(t, "BTC", 1, func(interval string) {
		mu.Lock()
		seen[interval] = true
		mu.Unlock()
	})
	deps := testDeps(t, nil)
	deps.RestClient = hlclient.New(hl.URL)
	srv := httptest.NewServer(NewServer(deps).Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/thesis/BTC")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	if !seen["4h"] || !seen["1d"] {
		t.Fatalf("intervals requested = %v, want 4h and 1d (BTC's config override ladder)", seen)
	}
	if seen["1h"] || seen["15m"] {
		t.Fatalf("intervals requested = %v, want NOT to include 1h/15m (global default shadowed by BTC override)", seen)
	}
}

// TestThesesSnapshot verifies GET /api/theses: the {"theses":[...]} envelope
// with the spec's field names, empty (not null) without a store, and the full
// live set — invalidated coins absent — when one is wired. Clients treat this
// snapshot as authoritative on (re)connect.
func TestThesesSnapshot(t *testing.T) {
	// No store wired: empty array, still the envelope.
	srv := httptest.NewServer(NewServer(testDeps(t, nil)).Handler())
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/api/theses")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(raw), `"theses":[]`) {
		t.Fatalf("empty snapshot = %s, want {\"theses\":[]}", raw)
	}

	// Live store: created theses appear, invalidated ones drop out.
	ts, err := thesis.NewStore(nil, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Upsert(metrics.Thesis{Coin: "BTC", Direction: "long", Summary: "hl", Invalidation: 92000, Confidence: 0.7, Horizon: "weeks"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Upsert(metrics.Thesis{Coin: "ETH", Direction: "short", Confidence: 0.5}); err != nil {
		t.Fatal(err)
	}
	ts.Invalidate("ETH")

	deps := testDeps(t, nil)
	deps.Theses = ts
	srv2 := httptest.NewServer(NewServer(deps).Handler())
	defer srv2.Close()
	resp2, err := srv2.Client().Get(srv2.URL + "/api/theses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var body struct {
		Theses []map[string]any `json:"theses"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Theses) != 1 {
		t.Fatalf("theses = %+v, want only live BTC", body.Theses)
	}
	got := body.Theses[0]
	for _, field := range []string{"coin", "direction", "summary", "invalidation", "targets", "horizon", "confidence", "created_at", "reviewed_at", "version"} {
		if _, ok := got[field]; !ok {
			t.Errorf("snapshot missing wire field %q: %v", field, got)
		}
	}
	if got["coin"] != "BTC" || got["direction"] != "long" || got["version"] != float64(1) {
		t.Fatalf("thesis content wrong: %v", got)
	}
}
