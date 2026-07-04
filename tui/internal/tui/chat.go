package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// chatCommandList is the suggestion set for native ghost-text autocomplete.
// It is fed to textinput.SetSuggestions in New() — the textinput filters it
// against what the user has typed, so only matching commands appear as ghosts.
var chatCommandList = []string{
	"/scan", "/watch", "/track", "/tf", "/model", "/mode",
	"/g", "/keys", "/settings", "/clear", "/help",
}

// renderChat draws the active sub-tab: the agent conversation, the ranked
// candidates board, or the live execution feed. The input line only appears on
// the agent tab; the board and feed use their full height.
func (m *Model) renderChat() string {
	switch m.chatTab {
	case chatTabLive:
		return m.renderLiveFeed()
	case chatTabIdeas:
		return m.renderIdeas()
	default:
		return m.renderAgentChat()
	}
}

// renderAgentChat draws the conversation viewport above the input line.
func (m *Model) renderAgentChat() string {
	input := m.input.View()
	if m.chat.busy {
		input = m.spinner.View() + m.theme.Label.Render(" agent is thinking…")
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.chatVP.View(), input)
}

// renderLiveFeed draws the live execution / thesis feed viewport. A dim hint
// at the bottom shows the tab-switch key so the pane isn't a dead end.
func (m *Model) renderLiveFeed() string {
	hint := m.theme.Label.Render("  [ ] switch tab")
	return lipgloss.JoinVertical(lipgloss.Left, m.liveVP.View(), hint)
}

// refreshChat re-renders the conversation into the viewport. A blank line
// separates turns for breathing room, and each role gets a distinct voice: you
// (accent), agent reply (speaker line + glamour-rendered markdown), proactive
// thesis (◆ accent headline + markdown body), command output (dim, indented).
func (m *Model) refreshChat() {
	width := max(8, m.chatVP.Width())
	var b strings.Builder
	for i, t := range m.chat.turns {
		if i > 0 {
			b.WriteString("\n")
		}
		switch t.Role {
		case roleUser:
			b.WriteString(lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("you  "))
			b.WriteString(t.Text)
		case roleSystem:
			b.WriteString(m.theme.Label.Render(indent(t.Text, "  ")))
		case roleThesis:
			headline, body, _ := strings.Cut(t.Text, "\n")
			b.WriteString(lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("◆ " + headline))
			if body != "" {
				b.WriteString("\n" + indent(m.renderMarkdown(body, max(8, width-2)), "  "))
			}
		default: // assistant
			b.WriteString(lipgloss.NewStyle().Foreground(m.theme.Up).Bold(true).Render("agent"))
			b.WriteString("\n")
			b.WriteString(m.renderMarkdown(t.Text, width))
		}
		b.WriteString("\n")
	}
	atBottom := m.chatVP.AtBottom()
	m.chatVP.SetContent(b.String())
	if atBottom {
		m.chatVP.GotoBottom()
	}
}

// refreshLive re-renders the live execution feed into its viewport.
func (m *Model) refreshLive() {
	width := max(8, m.liveVP.Width())
	var b strings.Builder
	for i, e := range m.liveEntries {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderLiveEntry(e, width))
	}
	atBottom := m.liveVP.AtBottom()
	m.liveVP.SetContent(b.String())
	if atBottom {
		m.liveVP.GotoBottom()
	}
}

// renderLiveEntry formats a single live feed entry.
func renderLiveEntry(e liveEntry, width int) string {
	var b strings.Builder

	// Icon and timestamp header.
	icon := kindIcon(e.kind)
	ts := e.at.Format("15:04:05")
	header := fmt.Sprintf("%s %s  %s", icon, ts, e.coin)
	if e.verdict != nil && e.verdict.Action != "" {
		header += "  " + string(e.verdict.Action)
		if e.verdict.Confidence > 0 {
			header += fmt.Sprintf("  %.0f%%", e.verdict.Confidence*100)
		}
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(header))
	b.WriteString("\n")

	// Summary line.
	b.WriteString("  " + e.summary + "\n")

	// Verdict details when present (fill or candidate).
	if v := e.verdict; v != nil && v.Action.IsTrade() {
		if v.Entry.Price > 0 {
			b.WriteString(fmt.Sprintf("  entry %s @ %.4f", v.Entry.Type, v.Entry.Price))
			if v.Stop > 0 {
				b.WriteString(fmt.Sprintf("  stop %.4f", v.Stop))
			}
			if v.TakeProfit > 0 {
				b.WriteString(fmt.Sprintf("  tp %.4f", v.TakeProfit))
			}
			b.WriteString("\n")
		}
		if v.SizeUSD > 0 {
			b.WriteString(fmt.Sprintf("  size $%.0f\n", v.SizeUSD))
		}
		if v.Thesis != "" {
			// Thesis is a single sentence — no full glamour render, just dim indent.
			wrapped := wordWrap(v.Thesis, width-4)
			b.WriteString("  " + strings.ReplaceAll(wrapped, "\n", "\n  ") + "\n")
		}
		if v.Reading != "" {
			b.WriteString("  " + lipgloss.NewStyle().Faint(true).Render(v.Reading) + "\n")
		}
	}

	return b.String()
}

// kindIcon returns a one-char icon for a journal entry kind.
func kindIcon(kind string) string {
	switch kind {
	case "fill":
		return "✓"
	case "candidate":
		return "◎"
	case "alert":
		return "!"
	case "error":
		return "✗"
	}
	return "·"
}

// wordWrap wraps s to at most width characters per line, breaking on spaces.
func wordWrap(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}
	var out strings.Builder
	for len(s) > width {
		cut := strings.LastIndex(s[:width], " ")
		if cut <= 0 {
			cut = width
		}
		out.WriteString(s[:cut])
		out.WriteByte('\n')
		s = strings.TrimLeft(s[cut:], " ")
	}
	out.WriteString(s)
	return out.String()
}

// indent prefixes every line of s with pad (for multi-line command output).
func indent(s, pad string) string {
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// liveEntryFrom converts a journalMsg into a liveEntry. Returns ok=false when
// the kind should not appear in the live feed (alerts and errors are included;
// plain status notes are not).
func liveEntryFrom(coin, kind, summary string, verdict *metrics.Verdict) (liveEntry, bool) {
	switch kind {
	case "fill", "candidate", "alert", "error":
		e := liveEntry{
			at:      time.Now(),
			coin:    coin,
			kind:    kind,
			summary: summary,
			verdict: verdict,
		}
		return e, true
	}
	return liveEntry{}, false
}
