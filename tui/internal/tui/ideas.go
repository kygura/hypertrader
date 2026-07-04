package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// The IDEAS board: the LLM synthesis surface. Every batch verdict lands here as
// a ranked trade candidate — one row per asset, latest verdict wins, ordered by
// confidence — so the marketwatch always answers "where does the agent see a
// trade right now?" without digging through the journal feed.

// candidate is one board entry: a verdict plus its arrival time.
type candidate struct {
	at time.Time
	v  apiclient.Verdict
}

// upsertCandidate merges a verdict into the board — replacing any existing
// candidate for the same asset — and re-ranks by confidence descending (ties:
// newest first). Invalid verdicts never reach the board (validated upstream).
func (m *Model) upsertCandidate(v apiclient.Verdict) {
	c := candidate{at: time.Now(), v: v}
	replaced := false
	for i := range m.candidates {
		if m.candidates[i].v.Asset == v.Asset {
			m.candidates[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		m.candidates = append(m.candidates, c)
	}
	sortCandidates(m.candidates)
	if m.ideasSel >= len(m.candidates) {
		m.ideasSel = max(0, len(m.candidates)-1)
	}
}

// sortCandidates orders by confidence descending, newest first on ties.
func sortCandidates(cs []candidate) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			a, b := cs[j-1], cs[j]
			if b.v.Confidence > a.v.Confidence ||
				(b.v.Confidence == a.v.Confidence && b.at.After(a.at)) {
				cs[j-1], cs[j] = b, a
			} else {
				break
			}
		}
	}
}

// jumpToCandidate re-anchors the markets selection onto the cursor candidate's
// asset — the board is a launchpad into the detail pane.
func (m *Model) jumpToCandidate() {
	if m.ideasSel < 0 || m.ideasSel >= len(m.candidates) {
		return
	}
	m.reanchor(m.candidates[m.ideasSel].v.Asset)
}

// moveIdeasSel moves the board cursor by d, clamped.
func (m *Model) moveIdeasSel(d int) {
	n := len(m.candidates)
	if n == 0 {
		return
	}
	m.ideasSel = clampInt(m.ideasSel+d, 0, n-1)
}

// actionColor maps a verdict action to its display color: shorts read down,
// longs up, exits and passivity dim, alerts gold.
func (t Theme) actionColor(a apiclient.Action) color.Color {
	switch a {
	case apiclient.ActionOpenShort:
		return t.Down
	case apiclient.ActionOpenLong:
		return t.Up
	case apiclient.ActionAlertOnly:
		return t.Gold
	default: // close, scale, hold
		return t.Dim
	}
}

// renderIdeas draws the board into its viewport and returns the scrolled view
// with the tab-switch hint, mirroring the live feed's shape.
func (m *Model) renderIdeas() string {
	m.ideasVP.SetContent(m.renderIdeasBody(max(8, m.ideasVP.Width())))
	hint := m.theme.Label.Render("  ↑↓ pick · enter view market · S scan now · [ ] switch tab")
	return lipgloss.JoinVertical(lipgloss.Left, m.ideasVP.View(), hint)
}

// renderIdeasBody renders the ranked candidate rows at the given width.
func (m *Model) renderIdeasBody(width int) string {
	if len(m.candidates) == 0 {
		return m.theme.Label.Render(strings.Join([]string{
			"no trade candidates yet",
			"",
			"the agent posts ranked candidates here after each batch close —",
			"press S (or /scan) to synthesize the tracked markets now",
		}, "\n"))
	}
	var b strings.Builder
	for i, c := range m.candidates {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderCandidate(i, c, width))
		b.WriteString("\n")
	}
	return b.String()
}

// renderCandidate renders one board row: rank + asset + action + confidence bar
// on the head line, levels beneath, then the thesis (the words the ranking is
// built on), dimmed for passive actions.
func (m *Model) renderCandidate(rank int, c candidate, width int) string {
	t := m.theme
	v := c.v
	actCol := t.actionColor(v.Action)
	passive := !v.Action.IsTrade()

	cursor := "  "
	if m.focus == focusChat && m.chatTab == chatTabIdeas && rank == m.ideasSel {
		cursor = lipgloss.NewStyle().Foreground(t.Accent).Render("▸ ")
	}

	head := cursor +
		t.Label.Render(fmt.Sprintf("#%d ", rank+1)) +
		lipgloss.NewStyle().Bold(true).Foreground(t.AssetColor(m.assetIndex(v.Asset))).Render(fmt.Sprintf("%-6s", v.Asset)) +
		lipgloss.NewStyle().Foreground(actCol).Render(fmt.Sprintf("%-11s", string(v.Action))) + " " +
		t.fillBar(v.Confidence, 8, lipgloss.NewStyle().Foreground(actCol)) +
		t.Label.Render(fmt.Sprintf(" %3.0f%%", v.Confidence*100)) +
		t.Label.Render("  "+v.Timeframe) +
		t.Label.Render("  "+c.at.Format("15:04"))

	lines := []string{head}
	if v.Action.IsTrade() && v.Entry.Price > 0 {
		lv := fmt.Sprintf("    entry %s %s", v.Entry.Type, fmtPx(v.Entry.Price))
		if v.Stop > 0 {
			lv += "  stop " + fmtPx(v.Stop)
		}
		if v.TakeProfit > 0 {
			lv += "  tp " + fmtPx(v.TakeProfit)
		}
		if v.SizeUSD > 0 {
			lv += fmt.Sprintf("  $%.0f", v.SizeUSD)
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(t.Text).Render(lv))
	}
	if v.Thesis != "" {
		th := indent(wordWrap(v.Thesis, max(8, width-4)), "    ")
		if passive {
			th = t.Label.Render(th)
		}
		lines = append(lines, th)
	}
	return strings.Join(lines, "\n")
}
