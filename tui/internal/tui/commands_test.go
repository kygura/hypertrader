package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperagent/tui/internal/apiclient"
)

// recorder captures the control-plane calls the model makes through
// *apiclient.Client against a local httptest server, so command tests can
// assert on them without a live daemon.
type recorder struct {
	subscribed  []string
	tracked     []string
	untracked   []string
	scans       [][]string
	providerSet string
	mode        string
	keyProvider string
	keyValue    string
	saves       []saveSettingsCall
}

// saveSettingsCall is one recorded PUT /api/settings body.
type saveSettingsCall struct {
	ChatProvider  string `json:"chat_provider"`
	ChatModel     string `json:"chat_model"`
	BatchProvider string `json:"batch_provider"`
	BatchModel    string `json:"batch_model"`
}

// newTestServer wires an httptest.Server implementing just enough of the
// daemon's control-plane API to drive command tests, recording every call
// into rec. The server is closed via t.Cleanup.
func newTestServer(t *testing.T, rec *recorder) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/watchlist/subscribe", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Coins []string `json:"coins"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.subscribed = append(rec.subscribed, body.Coins...)
	})
	mux.HandleFunc("POST /api/watchlist/track", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Coin, Timeframe string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.tracked = append(rec.tracked, body.Coin+":"+body.Timeframe)
	})
	mux.HandleFunc("POST /api/watchlist/untrack", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Coin string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.untracked = append(rec.untracked, body.Coin)
	})
	mux.HandleFunc("POST /api/watchlist/scan", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Coins []string `json:"coins"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.scans = append(rec.scans, body.Coins)
	})
	mux.HandleFunc("PUT /api/execution/mode", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Mode string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.mode = body.Mode
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var body saveSettingsCall
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.saves = append(rec.saves, body)
		switch {
		case body.ChatProvider != "":
			rec.providerSet = "chat=" + body.ChatProvider
		case body.BatchProvider != "":
			rec.providerSet = "batch=" + body.BatchProvider
		}
	})
	mux.HandleFunc("PUT /api/providers/{name}/key", func(w http.ResponseWriter, r *http.Request) {
		rec.keyProvider = r.PathValue("name")
		var body struct{ Key string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.keyValue = body.Key
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiclient.SettingsResponse{})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestModel(t *testing.T) (*Model, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := newTestServer(t, rec)
	m := New(Config{
		Theme:    NewTheme(true),
		Cache:    apiclient.NewCache(),
		Controls: apiclient.New(srv.URL, ""),
		Settings: apiclient.SettingsResponse{
			Mode:          "propose",
			Chat:          apiclient.RoleSettings{Provider: "anthropic"},
			Visualized:    []string{"BTC", "ETH"},
			Tracked:       []string{"ETH"},
			Timeframes:    map[string]string{"BTC": "4h", "ETH": "1h"},
			ProviderNames: []string{"anthropic", "openai", "deepseek"},
		},
	})
	return m, rec
}

// run is a test helper that calls runCommand and returns only the reply string.
func run(m *Model, cmd string) string {
	out, _ := m.runCommand(cmd)
	return out
}

// TestCommandScan verifies the on-demand synthesis path: /scan drives the
// daemon's Scan endpoint (with named coins uppercased) and lands the operator on
// the IDEAS board where the candidates will arrive.
func TestCommandScan(t *testing.T) {
	m, rec := newTestModel(t)
	run(m, "/scan")
	run(m, "/scan eth")
	if len(rec.scans) != 2 {
		t.Fatalf("Scan called %d times, want 2", len(rec.scans))
	}
	if len(rec.scans[0]) != 0 {
		t.Errorf("bare /scan should scan all tracked, got %v", rec.scans[0])
	}
	if len(rec.scans[1]) != 1 || rec.scans[1][0] != "ETH" {
		t.Errorf("/scan eth should scan [ETH], got %v", rec.scans[1])
	}
	if m.chatTab != chatTabIdeas {
		t.Errorf("scan should flip to the IDEAS tab, got tab %d", m.chatTab)
	}
}

func TestCommandWatchAdd(t *testing.T) {
	m, rec := newTestModel(t)
	out := run(m, "/watch add sol")
	if !containsStr(m.visualized, "SOL") {
		t.Fatalf("SOL not added to watchlist: %v", m.visualized)
	}
	if len(rec.subscribed) != 1 || rec.subscribed[0] != "SOL" {
		t.Fatalf("ingestor not asked to subscribe SOL: %v", rec.subscribed)
	}
	if !strings.Contains(out, "SOL") {
		t.Fatalf("reply should echo watchlist, got %q", out)
	}
}

func TestCommandWatchRemove(t *testing.T) {
	m, _ := newTestModel(t)
	run(m, "/watch rm BTC")
	if containsStr(m.visualized, "BTC") {
		t.Fatalf("BTC not removed: %v", m.visualized)
	}
}

func TestCommandTrackAddImpliesWatch(t *testing.T) {
	m, rec := newTestModel(t)
	run(m, "/track add SOL")
	if !m.tracked["SOL"] {
		t.Fatal("SOL not tracked")
	}
	if !containsStr(m.visualized, "SOL") {
		t.Fatal("tracking should imply watching")
	}
	if len(rec.tracked) == 0 || !strings.HasPrefix(rec.tracked[len(rec.tracked)-1], "SOL:") {
		t.Fatalf("Track not called for SOL: %v", rec.tracked)
	}
}

func TestCommandTimeframe(t *testing.T) {
	m, _ := newTestModel(t)
	m.selected = 0 // BTC
	out := run(m, "/tf 1d")
	if m.timeframes["BTC"] != "1d" {
		t.Fatalf("BTC timeframe not set: %v", m.timeframes)
	}
	if !strings.Contains(out, "1d") {
		t.Fatalf("reply should confirm tf, got %q", out)
	}
	if bad := run(m, "/tf 7y"); !strings.Contains(bad, "unknown timeframe") {
		t.Fatalf("invalid tf should be rejected, got %q", bad)
	}
}

func TestCommandProvider(t *testing.T) {
	m, rec := newTestModel(t)
	out := run(m, "/provider chat openai")
	if rec.providerSet != "chat=openai" {
		t.Fatalf("provider not set: %q", rec.providerSet)
	}
	if m.provider != "openai" {
		t.Fatalf("status provider not updated: %q", m.provider)
	}
	if !strings.Contains(out, "openai") {
		t.Fatalf("reply should confirm provider, got %q", out)
	}
}

func TestCommandMode(t *testing.T) {
	m, rec := newTestModel(t)
	run(m, "/mode autonomous")
	if rec.mode != "autonomous" || m.mode != "autonomous" {
		t.Fatalf("mode not switched: rec=%q model=%q", rec.mode, m.mode)
	}
}

func TestCommandUnknown(t *testing.T) {
	m, _ := newTestModel(t)
	if out := run(m, "/frobnicate"); !strings.Contains(out, "unknown command") {
		t.Fatalf("expected unknown-command reply, got %q", out)
	}
}

// TestCommandAliases verifies the new one-key slash aliases work from chat focus.
func TestCommandAliases(t *testing.T) {
	m, _ := newTestModel(t)
	// /s and /settings open the settings overlay and kick off an async
	// settings refresh (fetchSettings), so the reply is empty but the
	// returned cmd is non-nil now that Controls is wired.
	reply, cmd := m.runCommand("/s")
	if reply != "" || cmd == nil {
		t.Fatalf("/s should open overlay silently and refresh settings, got reply=%q cmd=%v", reply, cmd)
	}
	if _, ok := m.top().(*settingsOverlay); !ok {
		t.Fatalf("/s should push the settings overlay, got %T", m.top())
	}

	// /keys jumps straight to the API Keys tab
	m.overlays = nil
	_, _ = m.runCommand("/keys")
	so, ok := m.top().(*settingsOverlay)
	if !ok || so.tab != tabKeys {
		t.Fatalf("/keys should open settings on the API Keys tab, got %T", m.top())
	}
}

func TestFmtPx(t *testing.T) {
	cases := map[float64]string{
		0:        "—",
		95234.7:  "95,235",
		41.2:     "41.20",
		0.0421:   "0.0421",
		0.000123: "0.000123",
	}
	for in, want := range cases {
		if got := fmtPx(in); got != want {
			t.Errorf("fmtPx(%v) = %q, want %q", in, got, want)
		}
	}
}
