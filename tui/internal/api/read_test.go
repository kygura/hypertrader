package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// newTestServer builds a Server over a fresh bus + store rooted at a temp
// storage dir, honoring the config defaults (Markets.Tracked, Timeframe)
// unless mutate overrides them.
func newTestServer(t *testing.T, mutate func(*config.Config)) (*Server, *store.Store, *bus.Bus, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir, 32)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	b := bus.New()
	cfg := config.Default()
	cfg.Storage.Dir = dir
	if mutate != nil {
		mutate(&cfg)
	}
	s := NewServer(Deps{Bus: b, Store: st, Cfg: cfg, Version: "test"})
	return s, st, b, dir
}

func getJSON(t *testing.T, srv *httptest.Server, path string, out any) *http.Response {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	if out != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp
}

func TestMarketsEndpoint(t *testing.T) {
	s, st, _, _ := newTestServer(t, func(c *config.Config) {
		c.Markets.Tracked = []string{"BTC", "ETH", "SOL"}
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	t.Run("empty store returns 404", func(t *testing.T) {
		resp := getJSON(t, srv, "/api/markets", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	// Seed BTC (per-asset tf "4h") and ETH (default tf "1h"); leave SOL empty.
	st.PutBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Close: 65000, Final: true, CloseTime: time.Now()})
	st.PutBar(metrics.Bar{Coin: "ETH", Timeframe: "1h", Close: 3200, Final: true, CloseTime: time.Now()})
	st.PutMids(metrics.MidSnapshot{Mids: map[string]float64{"BTC": 65010, "ETH": 3201}})
	st.PutAssetCtx(metrics.AssetCtx{Coin: "BTC", MarkPrice: 65005})

	var entries []struct {
		Coin string      `json:"coin"`
		Bar  metrics.Bar `json:"bar"`
		Mid  float64     `json:"mid"`
	}
	resp := getJSON(t, srv, "/api/markets", &entries)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2 (SOL has no bar)", entries)
	}
	byCoin := map[string]float64{}
	for _, e := range entries {
		byCoin[e.Coin] = e.Mid
	}
	if byCoin["BTC"] != 65010 {
		t.Errorf("BTC mid = %v, want 65010", byCoin["BTC"])
	}
	if _, ok := byCoin["SOL"]; ok {
		t.Errorf("SOL should be absent (no bar), got %+v", byCoin)
	}
}

func TestBarsEndpoint(t *testing.T) {
	s, st, _, _ := newTestServer(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	t.Run("missing coin returns 404", func(t *testing.T) {
		resp := getJSON(t, srv, "/api/bars/BTC", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	base := time.Now()
	for i := 0; i < 5; i++ {
		st.PutBar(metrics.Bar{
			Coin: "BTC", Timeframe: "4h", Close: float64(100 + i),
			OpenTime: base.Add(time.Duration(i) * time.Hour), Final: true,
		})
	}

	var bars []metrics.Bar
	resp := getJSON(t, srv, "/api/bars/BTC?tf=4h", &bars)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(bars) != 5 {
		t.Fatalf("len(bars) = %d, want 5", len(bars))
	}

	t.Run("n caps at 1000 without erroring on huge n", func(t *testing.T) {
		var capped []metrics.Bar
		resp := getJSON(t, srv, "/api/bars/BTC?tf=4h&n=5000", &capped)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if len(capped) != 5 { // only 5 bars exist; cap just proves it doesn't error
			t.Fatalf("len(capped) = %d, want 5", len(capped))
		}
	})

	t.Run("default tf comes from config", func(t *testing.T) {
		var defaultTf []metrics.Bar
		resp := getJSON(t, srv, "/api/bars/BTC", &defaultTf) // BTC's per-asset tf is 4h
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if len(defaultTf) != 5 {
			t.Fatalf("len(defaultTf) = %d, want 5 (default tf should resolve to 4h for BTC)", len(defaultTf))
		}
	})
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestDigestsEndpoint(t *testing.T) {
	s, _, b, _ := newTestServer(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := getJSON(t, srv, "/api/digests/BTC", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 before any digest published", resp.StatusCode)
	}

	b.PublishDigest(metrics.Digest{Coin: "BTC", Timeframe: "4h", At: time.Now()})

	waitFor(t, time.Second, func() bool {
		resp := getJSON(t, srv, "/api/digests/BTC", nil)
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	var d metrics.Digest
	resp = getJSON(t, srv, "/api/digests/BTC", &d)
	resp.Body.Close()
	if d.Coin != "BTC" {
		t.Errorf("digest.Coin = %q, want BTC", d.Coin)
	}
}

func TestVerdictsEndpoint(t *testing.T) {
	s, _, b, _ := newTestServer(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	var empty []metrics.Verdict
	resp := getJSON(t, srv, "/api/verdicts", &empty)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for empty verdicts", resp.StatusCode)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty verdicts = %#v, want []", empty)
	}

	b.PublishVerdict(metrics.Verdict{Asset: "BTC", Action: metrics.ActionHold, Confidence: 0.5})
	b.PublishVerdict(metrics.Verdict{Asset: "ETH", Action: metrics.ActionHold, Confidence: 0.5})
	// Second BTC verdict should replace the first (latest per asset) and move
	// to the front (newest-first).
	b.PublishVerdict(metrics.Verdict{Asset: "BTC", Action: metrics.ActionOpenLong, Confidence: 0.9})

	waitFor(t, time.Second, func() bool {
		var vs []metrics.Verdict
		resp := getJSON(t, srv, "/api/verdicts", &vs)
		resp.Body.Close()
		return len(vs) == 2
	})

	var vs []metrics.Verdict
	resp = getJSON(t, srv, "/api/verdicts", &vs)
	resp.Body.Close()
	if len(vs) != 2 {
		t.Fatalf("len(vs) = %d, want 2 (latest per asset)", len(vs))
	}
	if vs[0].Asset != "BTC" || vs[0].Action != metrics.ActionOpenLong {
		t.Errorf("vs[0] = %+v, want the newer BTC verdict first", vs[0])
	}
}

func TestJournalEndpoint(t *testing.T) {
	s, _, _, dir := newTestServer(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	jdir := filepath.Join(dir, "journal")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"time":"2026-07-03T10:00:00Z","coin":"BTC","kind":"candidate","summary":"hi"}` + "\n"
	if err := os.WriteFile(filepath.Join(jdir, "2026-07-03.ndjson"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("bad date returns 400", func(t *testing.T) {
		resp := getJSON(t, srv, "/api/journal?date=not-a-date", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("valid date returns entries", func(t *testing.T) {
		var entries []struct {
			Coin    string `json:"coin"`
			Summary string `json:"summary"`
		}
		resp := getJSON(t, srv, "/api/journal?date=2026-07-03", &entries)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if len(entries) != 1 || entries[0].Coin != "BTC" {
			t.Fatalf("entries = %+v", entries)
		}
	})

	t.Run("missing date defaults to today and returns empty array", func(t *testing.T) {
		var entries []any
		resp := getJSON(t, srv, "/api/journal", &entries)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if entries == nil {
			t.Fatal("entries decoded as null, want [] for a day with no journal file")
		}
	})
}
