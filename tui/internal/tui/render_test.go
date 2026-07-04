package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// TestViewRendersAtEverySize drives the model through a sweep of terminal sizes
// (covering all layout modes) and asserts View never panics and never produces a
// frame taller or wider than the terminal. This guards the resize math, the
// negative-dimension clamps, and the help/overlay compositor.
func TestViewRendersAtEverySize(t *testing.T) {
	sizes := []struct{ w, h int }{
		{200, 60},         // wide
		{120, 40},         // wide
		{120, 16},         // wide but short
		{twoColW, 40},     // wide (at the breakpoint)
		{twoColW - 1, 40}, // narrow (just below)
		{50, 40},          // narrow
		{50, 9},           // tiny (narrow + short)
		{40, 30},          // tiny (very narrow)
		{20, 40},          // too small (width)
		{80, 4},           // too small (height)
		{minW, minH},
		{twoColW, tinyH},
	}

	for _, withHelp := range []bool{false, true} {
		for _, s := range sizes {
			m, _ := newTestModel(t)
			if withHelp {
				m.openHelp()
			}
			mdl, _ := m.Update(tea.WindowSizeMsg{Width: s.w, Height: s.h})
			m = mdl.(*Model)

			v := m.View() // must not panic
			gotW := lipgloss.Width(v.Content)
			gotH := lipgloss.Height(v.Content)
			if gotW > s.w {
				t.Errorf("size %dx%d help=%v: width %d exceeds %d", s.w, s.h, withHelp, gotW, s.w)
			}
			if gotH > s.h {
				t.Errorf("size %dx%d help=%v: height %d exceeds %d", s.w, s.h, withHelp, gotH, s.h)
			}
		}
	}
}

// TestOverlaysRenderWithinBounds opens each picker overlay and asserts the
// composited frame never exceeds the terminal.
func TestOverlaysRenderWithinBounds(t *testing.T) {
	const w, h = 120, 40
	open := map[string]func(*Model){
		"settings":      (*Model).openSettings,
		"settings-keys": (*Model).openAPIKeys,
		"help":          (*Model).openHelp,
		"provider":      func(m *Model) { m.pushProviderPicker(reasoner.RoleChat) },
		"model":         func(m *Model) { m.pushModelPicker(reasoner.RoleBatch) },
		"mode":          (*Model).pushModePicker,
		"coin":          (*Model).pushMarketsManager,
		"coin-action":   func(m *Model) { m.pushCoinActions("BTC") },
		"coin-tf":       func(m *Model) { m.pushTimeframePicker("BTC") },
		"stacked":       func(m *Model) { m.openSettings(); m.pushModelPicker(reasoner.RoleChat) },
	}
	for name, fn := range open {
		t.Run(name, func(t *testing.T) {
			m, _ := newTestModel(t)
			mdl, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			m = mdl.(*Model)
			fn(m)
			v := m.View()
			if gw, gh := lipgloss.Width(v.Content), lipgloss.Height(v.Content); gw > w || gh > h {
				t.Errorf("%s overlay: %dx%d exceeds %dx%d", name, gw, gh, w, h)
			}
		})
	}
}

// TestPanesFillTheirSlots verifies the pane contract under lipgloss v2 semantics
// (Width/Height include the border): every zone renders at exactly the size the
// layout allotted, so the composed frame tiles the terminal with no gaps or
// overflow. This is the regression guard for the wrapped-markets-table bug.
func TestPanesFillTheirSlots(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 140, 40
	m.applyLayout()
	l := m.lay

	boxes := []struct {
		name string
		s    string
		w, h int
	}{
		{"markets", m.theme.pane("MARKETS", m.renderMarkets(l.marketsW-4, l.marketsH-3), l.marketsW, l.marketsH, true), l.marketsW, l.marketsH},
		{"detail", m.theme.pane("DETAIL", m.detailBody(), l.detailW, l.detailH, false), l.detailW, l.detailH},
		{"chat", m.theme.pane("AGENT", m.renderChat(), l.chatW, l.chatH, false), l.chatW, l.chatH},
	}
	for _, b := range boxes {
		if gw, gh := lipgloss.Width(b.s), lipgloss.Height(b.s); gw != b.w || gh != b.h {
			t.Errorf("%s pane: rendered %dx%d, slot %dx%d", b.name, gw, gh, b.w, b.h)
		}
	}
}

// TestEnterShowsDetail verifies the plan's selection → detail flow: enter on the
// markets pane focuses the detail column when the layout shows one, and floats the
// detail modal when it doesn't — and the modal renders within the terminal.
func TestEnterShowsDetail(t *testing.T) {
	press := func(m *Model, key rune) *Model {
		mdl, _ := m.handleKey(tea.KeyPressMsg{Code: key})
		return mdl.(*Model)
	}

	t.Run("wide focuses the detail pane", func(t *testing.T) {
		m, _ := newTestModel(t)
		mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		m = mdl.(*Model)
		m.focus = focusMarkets
		m = press(m, tea.KeyEnter)
		if m.focus != focusDetail || m.detailModal {
			t.Fatalf("wide enter: focus=%v modal=%v, want detail focus without modal", m.focus, m.detailModal)
		}
	})

	t.Run("narrow floats the detail modal", func(t *testing.T) {
		m, _ := newTestModel(t)
		mdl, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 30})
		m = mdl.(*Model)
		m.focus = focusMarkets
		m = press(m, tea.KeyEnter)
		if !m.detailModal {
			t.Fatal("narrow enter should open the detail modal")
		}
		v := m.View()
		if gw, gh := lipgloss.Width(v.Content), lipgloss.Height(v.Content); gw > 50 || gh > 30 {
			t.Errorf("detail modal: %dx%d exceeds 50x30", gw, gh)
		}
		m = press(m, tea.KeyEscape)
		if m.detailModal {
			t.Fatal("esc should close the detail modal")
		}
	})
}

// TestMarketsMoveBar verifies the plan §4.1 marketwatch grid: an inline
// horizontal bar whose filled width scales to move magnitude. The big mover's
// row must carry more filled bar cells than a flat asset's row, and the column
// only earns a slot at sufficient width.
func TestMarketsMoveBar(t *testing.T) {
	m, _ := newTestModel(t)
	m.store.PutBar(metrics.Bar{Coin: "BTC", Timeframe: "4h", Close: 95000, Return: 0.05})
	m.store.PutBar(metrics.Bar{Coin: "ETH", Timeframe: "1h", Close: 3500, Return: 0.0})

	fullBlocks := func(s string) int { return strings.Count(s, "█") }

	out := m.renderMarkets(90, 10)
	lines := strings.Split(ansi.Strip(out), "\n")
	var btcRow, ethRow string
	for _, ln := range lines {
		if strings.Contains(ln, "BTC") {
			btcRow = ln
		}
		if strings.Contains(ln, "ETH") {
			ethRow = ln
		}
	}
	if btcRow == "" || ethRow == "" {
		t.Fatalf("missing rows in markets grid:\n%s", out)
	}
	if fullBlocks(btcRow) <= fullBlocks(ethRow) {
		t.Errorf("BTC (+5%%) should render a wider move bar than flat ETH\nBTC: %q\nETH: %q", btcRow, ethRow)
	}
	if !strings.Contains(ethRow, "░") {
		t.Errorf("flat ETH row should show an empty bar track, got %q", ethRow)
	}
}

// TestDetailMetricStack verifies the plan §4.1 detail pane: the full metric
// stack renders in mockup order — price (with a bar) → OI Δ → funding → basis →
// CVD → liq prox → vol — with the thesis block beneath the stack.
func TestDetailMetricStack(t *testing.T) {
	m, _ := newTestModel(t)
	m.store.PutBar(metrics.Bar{
		Coin: "BTC", Timeframe: "4h", Close: 95000, Return: 0.031,
		OpenInterest: 1.2e6, OIDelta: 0.12, Funding: 0.00011, Basis: 0.0004,
		CVD: -1.2e6, LiqProx: 0.021, RealizedVol: 0.45,
		BuyVolume: 100, SellVolume: 80,
	})
	m.selected = 0 // BTC heads the default watchlist order
	m.thesis["BTC"] = "lower-high into 43; funding flipped positive"

	body := ansi.Strip(m.renderDetail(70))
	order := []string{"price", "OI Δ", "funding", "basis", "CVD", "liq prox", "vol", "thesis"}
	last := -1
	for _, label := range order {
		i := strings.Index(body, label)
		if i < 0 {
			t.Fatalf("detail body missing %q:\n%s", label, body)
		}
		if i < last {
			t.Errorf("label %q out of mockup order (index %d < previous %d)\n%s", label, i, last, body)
		}
		last = i
	}
	priceLine, _, _ := strings.Cut(body, "\n")
	if !strings.ContainsAny(priceLine, "█▏▎▍▌▋▊▉") {
		t.Errorf("price row should carry a magnitude bar, got %q", priceLine)
	}
	if !strings.Contains(body, "funding flipped positive") {
		t.Errorf("thesis text missing from detail body")
	}
}

// TestFocusDetailScrolls verifies a key forwarded to the detail viewport when detail
// holds focus is handled without panicking.
func TestFocusDetailScrolls(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)
	m.focus = focusDetail
	if _, cmd := m.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"}); cmd == nil {
		_ = cmd // nil is acceptable (nothing to scroll); must not panic
	}
}
