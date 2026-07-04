package tui

import (
	"strings"
	"testing"

	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/store"
)

func newTestModel(t *testing.T) (*Model, *recorder) {
	t.Helper()
	st, err := store.New(t.TempDir(), 64)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	m := New(Config{
		Theme:      NewTheme(true),
		Store:      st,
		Visualized: []string{"BTC", "ETH"},
		Tracked:    []string{"ETH"},
		Timeframes: map[string]string{"BTC": "4h", "ETH": "1h"},
		Mode:       "propose",
		Provider:   "anthropic",
		Controls: Controls{
			Subscribe:     func(c ...string) { rec.subscribed = append(rec.subscribed, c...) },
			Track:         func(c, tf string) { rec.tracked = append(rec.tracked, c+":"+tf) },
			Untrack:       func(c string) { rec.untracked = append(rec.untracked, c) },
			ScanNow:       func(c ...string) { rec.scans = append(rec.scans, c) },
			SetProvider:   func(r reasoner.Role, n string) error { rec.providerSet = string(r) + "=" + n; return nil },
			ProviderNames: func() []string { return []string{"anthropic", "openai", "deepseek"} },
			SetMode:       func(mode string) error { rec.mode = mode; return nil },
		},
	})
	return m, rec
}

type recorder struct {
	subscribed  []string
	tracked     []string
	untracked   []string
	scans       [][]string
	providerSet string
	mode        string
}

// run is a test helper that calls runCommand and returns only the reply string.
func run(m *Model, cmd string) string {
	out, _ := m.runCommand(cmd)
	return out
}

// TestCommandScan verifies the on-demand synthesis path: /scan drives the
// daemon's ScanNow hook (with named coins uppercased) and lands the operator on
// the IDEAS board where the candidates will arrive.
func TestCommandScan(t *testing.T) {
	m, rec := newTestModel(t)
	run(m, "/scan")
	run(m, "/scan eth")
	if len(rec.scans) != 2 {
		t.Fatalf("ScanNow called %d times, want 2", len(rec.scans))
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
		t.Fatalf("batcher.Track not called for SOL: %v", rec.tracked)
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
	// /s and /settings open the settings overlay (return empty reply + nil cmd)
	reply, cmd := m.runCommand("/s")
	if reply != "" || cmd != nil {
		t.Fatalf("/s should open overlay silently, got reply=%q", reply)
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
