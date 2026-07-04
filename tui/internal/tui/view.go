package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// applyLayout resolves the responsive geometry for the current terminal size and
// sizes every embedded component to match. View and resize both call it, so the
// rendered frame and the viewport/input/table dimensions can never disagree — which
// is also what makes a live terminal resize reflow every zone.
func (m *Model) applyLayout() {
	m.lay = computeLayout(m.width, m.height, m.chatHeightOffset)
	l := m.lay
	if l.mode == layoutTooSmall {
		return
	}
	// Chat interior: border (2) + padding (2) horizontally; vertically it reserves
	// border (2) + title (1) + input (1) + a spacer row for the viewport.
	m.chatVP.SetWidth(max(1, l.chatW-4))
	m.chatVP.SetHeight(max(1, l.chatH-5))
	// Live/Ideas VPs fill the same width; one row goes to their bottom hint line.
	m.liveVP.SetWidth(max(1, l.chatW-4))
	m.liveVP.SetHeight(max(1, l.chatH-4))
	m.ideasVP.SetWidth(max(1, l.chatW-4))
	m.ideasVP.SetHeight(max(1, l.chatH-5))
	m.input.SetWidth(max(1, l.chatW-6))
	switch {
	case m.detailModal: // floating detail: size the viewport to the modal box
		bw, bh := m.detailModalSize()
		m.detailVP.SetWidth(max(1, bw-4))
		m.detailVP.SetHeight(max(1, bh-3)) // border (2) + title (1)
	case l.detailW > 0: // the detail column only exists in wide mode
		m.detailVP.SetWidth(max(1, l.detailW-4))
		m.detailVP.SetHeight(max(1, l.detailH-3)) // border (2) + title (1)
	}
}

// detailModalSize is the floating detail box's outer size for the current terminal.
func (m *Model) detailModalSize() (w, h int) {
	return clampInt(m.width-6, minW-4, 96), clampInt(m.height-4, 5, 34)
}

// resize is called on WindowSizeMsg: re-resolve layout and re-render chat.
func (m *Model) resize() {
	m.applyLayout()
	m.refreshChat()
}

// View implements tea.Model. It picks a renderer by layout mode, clamps to the
// terminal, and composites the overlay stack on top, bottom-up, so a picker
// opened from settings floats above the settings box.
func (m *Model) View() tea.View {
	if m.width == 0 {
		v := tea.NewView("initializing…")
		v.AltScreen = true
		return v
	}
	m.applyLayout()

	var frame string
	switch m.lay.mode {
	case layoutTooSmall:
		frame = m.renderTooSmall()
	case layoutTiny:
		frame = m.renderTiny()
	case layoutNarrow:
		frame = m.renderNarrow()
	default: // layoutWide
		frame = m.renderWide()
	}

	frame = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(frame)
	if m.lay.mode != layoutTooSmall {
		if m.detailModal {
			frame = m.compositeBox(frame, m.renderDetailModal())
		}
		for _, o := range m.overlays {
			frame = m.compositeBox(frame, o.view(m, m.width-4, m.height-2))
		}
	}

	v := tea.NewView(frame)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderWide is the plan's default main view: markets grid │ selected-asset detail
// side by side, the conversation strip beneath, and the status line last.
func (m *Model) renderWide() string {
	l := m.lay
	markets := m.theme.pane(m.marketsTitle(), m.renderMarkets(l.marketsW-4, l.marketsH-3), l.marketsW, l.marketsH, m.focus == focusMarkets)
	detail := m.theme.pane(m.detailTitle(), m.detailBody(), l.detailW, l.detailH, m.focus == focusDetail)
	chat := m.theme.pane(m.chatTitle(), m.renderChat(), l.chatW, l.chatH, m.focus == focusChat)
	top := lipgloss.JoinHorizontal(lipgloss.Top, markets, detail)
	return lipgloss.JoinVertical(lipgloss.Left, top, chat, m.renderStatus())
}

// renderDetailModal is the floating detail view summoned with enter when the
// layout has no detail column.
func (m *Model) renderDetailModal() string {
	bw, bh := m.detailModalSize()
	return m.theme.pane(m.detailTitle(), m.detailBody(), bw, bh, true)
}

// renderNarrow stacks the markets grid over the conversation when there isn't width
// for side-by-side. Chat keeps the larger share.
func (m *Model) renderNarrow() string {
	l := m.lay
	markets := m.theme.pane(m.marketsTitle(), m.renderMarkets(l.marketsW-4, l.marketsH-3), l.marketsW, l.marketsH, m.focus != focusChat)
	chat := m.theme.pane(m.chatTitle(), m.renderChat(), l.chatW, l.chatH, m.focus == focusChat)
	return lipgloss.JoinVertical(lipgloss.Left, markets, chat, m.renderStatus())
}

// renderTiny draws chat fullscreen with a one-line asset ticker and the status bar —
// the smallest usable state.
func (m *Model) renderTiny() string {
	l := m.lay
	chat := m.theme.pane(m.chatTitle(), m.renderChat(), l.chatW, l.chatH, true)
	return lipgloss.JoinVertical(lipgloss.Left, m.renderTicker(), chat, m.renderStatus())
}

// renderTooSmall centers a notice when the terminal can't host even chat.
func (m *Model) renderTooSmall() string {
	msg := lipgloss.NewStyle().Foreground(m.theme.Text).Render("terminal too small") + "\n" +
		m.theme.Label.Render(fmt.Sprintf("need at least %d×%d", minW, minH))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
}

// marketsTitle labels the markets column with the asset count and active sort/filter.
func (m *Model) marketsTitle() string {
	t := fmt.Sprintf("MARKETS · %d", len(m.ordered()))
	if m.sortKey != sortWatchlist {
		t += " · ↓" + m.sortKey.String()
	}
	if m.filtering || m.filter != "" {
		t += " · /" + m.filter
	}
	return t
}

// detailBody refreshes the detail viewport with live data at the current width and
// returns its scrolled view. SetContent preserves the scroll offset, so re-feeding
// every frame keeps the panel live without yanking the user.
func (m *Model) detailBody() string {
	m.detailVP.SetContent(m.renderDetail(m.detailVP.Width()))
	return m.detailVP.View()
}

// chatTitle renders the tab bar (AGENT · IDEAS · LIVE), with the active tab bold
// and accented. The thinking indicator appears only on the agent tab; the ideas
// tab carries the candidate count once the board has content.
func (m *Model) chatTitle() string {
	ideasLabel := "IDEAS"
	if n := len(m.candidates); n > 0 {
		ideasLabel = fmt.Sprintf("IDEAS·%d", n)
	}
	tabs := []struct {
		idx   int
		label string
	}{
		{chatTabAgent, "AGENT"},
		{chatTabIdeas, ideasLabel},
		{chatTabLive, "LIVE"},
	}
	parts := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		if tab.idx == m.chatTab {
			parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(m.theme.Accent).Render(tab.label))
		} else {
			parts = append(parts, m.theme.Label.Render(tab.label))
		}
	}
	title := strings.Join(parts, "  ")
	if m.chatTab == chatTabAgent && m.chat.busy {
		title += "  ⠙ thinking"
	}
	return title
}

// renderTicker is the single-line asset summary shown in tiny mode.
func (m *Model) renderTicker() string {
	coin := m.selectedCoin()
	conn := lipgloss.NewStyle().Foreground(m.theme.Up).Render("● live")
	if !m.connected {
		conn = lipgloss.NewStyle().Foreground(m.theme.Down).Render("○ offline")
	}
	if coin == "" {
		return ansi.Truncate(" "+conn, m.width, "…")
	}
	dot := lipgloss.NewStyle().Foreground(m.theme.AssetColor(m.assetIndex(coin))).Render("●")
	bar, _ := m.cache.LatestBar(coin, m.timeframes[coin])
	px := m.cache.Mid(coin)
	if px == 0 {
		px = bar.Close
	}
	name := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Text).Render(coin)
	move := lipgloss.NewStyle().Foreground(m.theme.SignColor(bar.Return)).Render(fmt.Sprintf("%+.2f%%", bar.Return*100))
	left := fmt.Sprintf("%s %s %s %s", dot, name, fmtPx(px), move)
	line := lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		lipgloss.PlaceHorizontal(max(0, m.width-lipgloss.Width(left)), lipgloss.Right, conn),
	)
	return ansi.Truncate(line, m.width, "…")
}

func (m *Model) detailTitle() string {
	coin := m.selectedCoin()
	if coin == "" {
		return "DETAIL"
	}
	dot := lipgloss.NewStyle().Foreground(m.theme.AssetColor(m.assetIndex(coin))).Render("●")
	title := fmt.Sprintf("%s %s · %s", dot, coin, m.timeframes[coin])
	// When the detail content scrolls, the title carries the position — both a
	// "where am I" and a "there is more below" cue.
	if m.detailVP.TotalLineCount() > m.detailVP.VisibleLineCount() {
		title += fmt.Sprintf(" · %d%%", int(m.detailVP.ScrollPercent()*100))
	}
	return title
}

// assetIndex returns a coin's position in the full watchlist, for stable coloring.
func (m *Model) assetIndex(coin string) int {
	for i, c := range m.visualized {
		if c == coin {
			return i
		}
	}
	return 0
}

// renderStatus draws the bottom status line as colored segments: the app badge,
// connection, the chat provider·model (violet — the model identity color), the
// execution-mode badge, then either the latest status note or the navigation
// hints — the always-visible half of the tutorial.
func (m *Model) renderStatus() string {
	t := m.theme

	key := t.StatusKey.Render("HYPERAGENT")

	conn := lipgloss.NewStyle().Foreground(t.Up).Render("● live")
	if !m.connected {
		conn = lipgloss.NewStyle().Foreground(t.Down).Render("○ offline")
	}

	prov, model := m.chatModelDisplay()
	chatID := prov
	if model != "" {
		chatID = prov + "·" + model
	}
	provCell := lipgloss.NewStyle().Foreground(t.Violet).Render(chatID)

	left := key + " " + conn + t.Label.Render(" · ") + provCell + " " + t.ModeBadge(m.mode)

	// Right side: a transient status note wins; otherwise the key hints.
	note := m.statusMsg
	if note == "" {
		note = t.KeyHints([][2]string{
			{"↑↓", "market"}, {"tab", "focus"}, {"enter", "detail"}, {"S", "scan"},
			{"ctrl+s", "settings"}, {"?", "help"}, {"ctrl+q", "quit"},
		})
	} else {
		note = t.Label.Render(note)
	}

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(note) - 1
	if pad < 1 {
		// Not enough room for the note: drop it rather than wrap the bar.
		note = ""
		pad = max(1, m.width-lipgloss.Width(left)-1)
	}
	line := left + strings.Repeat(" ", pad) + note
	return t.StatusBar.Width(m.width).Render(truncate(line, m.width))
}

// truncate clips s to n cells with an ellipsis, respecting wide runes / ANSI.
func truncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}
