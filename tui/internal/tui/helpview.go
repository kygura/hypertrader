// The help overlay: a paged tutorial (toggled with ?) that explains the layout,
// navigation, the chat agent, and settings — not just a key list. Pages flip
// with ←/→; the page dots show where you are.
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type helpOverlay struct {
	page int
}

func (m *Model) openHelp() { m.push(&helpOverlay{}) }

// helpPages returns the tutorial content. Built per render so it can reflect
// live state (the current mode, the selected coin's timeframe, …).
func (ho *helpOverlay) helpPages(m *Model) []struct {
	title string
	body  string
} {
	t := m.theme
	h2 := func(s string) string { return lipgloss.NewStyle().Foreground(t.Gold).Bold(true).Render(s) }
	kv := func(key, desc string) string {
		return "  " + lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(padRight(key, 12)) + t.Label.Render(desc)
	}
	txt := func(s string) string { return lipgloss.NewStyle().Foreground(t.Text).Render(s) }
	dim := func(s string) string { return t.Label.Render(s) }

	layoutDiagram := strings.Join([]string{
		dim("  ╭─ ") + lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("MARKETS") + dim(" ──╮╭─ ") + lipgloss.NewStyle().Foreground(t.AccentAlt).Bold(true).Render("DETAIL") + dim(" ────────╮"),
		dim("  │ watchlist ││ price · signals  │"),
		dim("  │ Δ% · fund ││ funding · OI     │"),
		dim("  │ OIΔ · 7d  ││ thesis           │"),
		dim("  ╰───────────╯╰──────────────────╯"),
		dim("  ╭─ ") + lipgloss.NewStyle().Foreground(t.Violet).Bold(true).Render("AGENT") + dim(" ─────────────────────╮"),
		dim("  │ chat with the reasoner      │"),
		dim("  ╰─────────────────────────────╯"),
	}, "\n")

	return []struct {
		title string
		body  string
	}{
		{"WELCOME", strings.Join([]string{
			txt("hyperagent watches Hyperliquid perps across your watchlist,"),
			txt("computes per-bar perp metrics, and hands ranked digests to an"),
			txt("LLM that proposes journaled trade candidates."),
			"",
			layoutDiagram,
			"",
			h2("The three panes"),
			kv("MARKETS", "the watchlist — ↑↓ select an asset, the rest follows"),
			kv("DETAIL", "the selected asset: metric stack · thesis · signals"),
			kv("AGENT", "chat · IDEAS (ranked candidates) · LIVE feed tabs"),
			"",
			h2("The synthesis loop"),
			kv("S / /scan", "read the tape now — candidates land on IDEAS ranked"),
			dim("  batches also fire on every timeframe close, hands-free"),
			"",
			dim("Flip pages with ←/→ for navigation, chat, and settings help."),
		}, "\n")},

		{"NAVIGATION", strings.Join([]string{
			h2("Anywhere — even while typing"),
			kv("ctrl+s", "open / close settings"),
			kv("ctrl+q", "quit (q also quits outside chat)"),
			kv("ctrl+↑↓", "grow / shrink the chat pane"),
			kv("? / F1", "this tutorial"),
			"",
			h2("Focus"),
			kv("tab / ←→", "cycle pane focus (accent border = focused)"),
			kv("1 – 5", "jump to markets · detail · chat · ideas · live"),
			kv("esc", "clears the filter, then chat ⇄ markets; closes overlays"),
			"",
			h2("Markets"),
			kv("↑↓", "move the selection — works from any pane"),
			kv("jk", "move selection (markets) or scroll line-by-line (detail)"),
			kv("home/end", "jump to the first / last asset (G = last)"),
			kv("enter", "open the selected asset's detail"),
			kv("S", "scan now — LLM ranks the tracked markets on IDEAS"),
			kv("x", "toggle tracking for the selected asset"),
			kv("t", "cycle timeframe (15m → 1h → 4h → 1d)"),
			kv("o", "cycle sort column"),
			kv("/", "filter — enter keeps it, esc cancels it"),
			"",
			h2("Detail"),
			kv("jk / pgup/pgdn", "scroll the detail panel"),
			kv("[ / ]", "cycle section cursor (metrics · thesis · signals)"),
			kv("enter", "act on focused section: generate thesis · explain signal"),
			kv("g", "generate thesis for the selected asset"),
		}, "\n")},

		{"CHAT & COMMANDS", strings.Join([]string{
			txt("The AGENT pane is a direct line to the chat model, grounded in"),
			txt("the selected asset's live metrics and cross-timeframe signals."),
			"",
			h2("Talking"),
			kv("3 or esc", "focus the chat input"),
			kv("enter", "send — replies stream into the scrollback"),
			kv("pgup/pgdn", "scroll the conversation history"),
			kv("/ + tab", "autocomplete slash commands (↑↓ cycles, tab accepts)"),
			"",
			h2("The IDEAS board (tab 4)"),
			kv("↑↓ / enter", "pick a candidate · jump to its market"),
			dim("  one row per asset, ranked by the agent's confidence"),
			"",
			h2("Slash commands"),
			kv("/scan", "synthesize tracked markets now (or /scan ETH SOL)"),
			kv("/watch", "add/remove watchlist symbols"),
			kv("/track", "give/remove an asset from the agent's set"),
			kv("/tf", "set a timeframe, e.g. /tf 4h BTC"),
			kv("/model", "free-form model id, e.g. /model chat o3-mini"),
			kv("/mode", "propose | autonomous"),
			kv("/clear", "clear the scrollback"),
			kv("/help", "full command list"),
			"",
			dim("◆ headlines are the agent speaking up on its own after a batch."),
		}, "\n")},

		{"SETTINGS & KEYS", strings.Join([]string{
			h2("The settings modal (s · ctrl+s anywhere)"),
			kv("Models", "pick model per role (chat vs batch) — provider auto-selected"),
			kv("API Keys", "paste provider keys — masked input, applied live"),
			kv("Trading", "execution mode + the hard risk gates"),
			kv("Markets", "watchlist, tracked set, per-asset timeframes"),
			"",
			txt("Everything set here persists to config.toml, so models, keys"),
			txt("and mode survive a restart. Keys are stored with 0600 perms;"),
			txt("env vars (ANTHROPIC_API_KEY, …) still work and are never"),
			txt("overwritten unless you save a key here."),
			"",
			h2("Execution modes"),
			kv("propose", "candidates are journaled + need approval (default)"),
			kv("autonomous", "agent signs orders within the risk gates"),
			"",
			dim("Autonomy is earned: run propose until the journal proves it."),
		}, "\n")},
	}
}

func (ho *helpOverlay) handleKey(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	n := len(ho.helpPages(m))
	switch msg.String() {
	case "esc", "q", "?":
		m.pop()
	case "right", "l", "tab", "space", "down", "j":
		ho.page = (ho.page + 1) % n
	case "left", "h", "shift+tab", "up", "k":
		ho.page = (ho.page - 1 + n) % n
	case "1", "2", "3", "4":
		ho.page = clampInt(int(msg.String()[0]-'1'), 0, n-1)
	}
	return nil
}

func (ho *helpOverlay) view(m *Model, maxW, maxH int) string {
	t := m.theme
	pages := ho.helpPages(m)
	ho.page = clampInt(ho.page, 0, len(pages)-1)
	pg := pages[ho.page]

	// Page dots: ● for current, ○ for the rest.
	dots := make([]string, len(pages))
	for i := range pages {
		if i == ho.page {
			dots[i] = lipgloss.NewStyle().Foreground(t.Accent).Render("●")
		} else {
			dots[i] = t.Label.Render("○")
		}
	}

	header := t.Title("HELP · "+pg.title) + "  " + strings.Join(dots, " ") +
		t.Label.Render(fmt.Sprintf("  %d/%d", ho.page+1, len(pages)))
	footer := t.KeyHints([][2]string{{"←→", "page"}, {"1-4", "jump"}, {"esc", "close"}})

	w := clampInt(maxW-8, 40, 72)
	body := lipgloss.NewStyle().MaxHeight(max(5, maxH-6)).Render(pg.body)
	content := strings.Join([]string{header, t.Divider(w - 4), body, "", footer}, "\n")
	return t.PaneFocused.Width(w).Padding(0, 1).Render(content)
}
