// Package cockpit is the four-panel operator cockpit: the pitch mock's
// layout (pitch/mock-tui) rendered from real daemon data over the
// apiclient cache and WS bridge. Design:
// docs/superpowers/specs/2026-07-06-cockpit-tui-design.md
package cockpit

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The mock cockpit's fixed dark palette (pitch/mock-tui/view.go).
var (
	cAccent = lipgloss.Color("#2DE0A7")
	cText   = lipgloss.Color("#C9D4DE")
	cBright = lipgloss.Color("#EDF3F9")
	cDim    = lipgloss.Color("#5C6B7A")
	cBorder = lipgloss.Color("#28323D")
	cGreen  = lipgloss.Color("#4ADE80")
	cRed    = lipgloss.Color("#FF6B6B")
	cAmber  = lipgloss.Color("#F0B35B")
	cPurple = lipgloss.Color("#B48EF7")
	cCyan   = lipgloss.Color("#4FC1E9")

	logoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#06130D")).Background(cAccent).Bold(true)
	textStyle   = lipgloss.NewStyle().Foreground(cText)
	brightStyle = lipgloss.NewStyle().Foreground(cBright).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(cDim)
	borderStyle = lipgloss.NewStyle().Foreground(cBorder)
	titleStyle  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	phaseStyle  = lipgloss.NewStyle().Foreground(cAccent)
	greenStyle  = lipgloss.NewStyle().Foreground(cGreen)
	redStyle    = lipgloss.NewStyle().Foreground(cRed)
	amberStyle  = lipgloss.NewStyle().Foreground(cAmber)
	keyStyle    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)

	tagStyles = map[string]lipgloss.Style{
		"INGEST":   lipgloss.NewStyle().Foreground(cCyan).Bold(true),
		"REASON":   lipgloss.NewStyle().Foreground(cPurple).Bold(true),
		"EXECUTE":  lipgloss.NewStyle().Foreground(cAccent).Bold(true),
		"FILL":     lipgloss.NewStyle().Foreground(cGreen).Bold(true),
		"RISK":     lipgloss.NewStyle().Foreground(cAmber).Bold(true),
		"ERROR":    lipgloss.NewStyle().Foreground(cRed).Bold(true),
		"OPERATOR": lipgloss.NewStyle().Foreground(cRed).Bold(true),
	}
)

// box draws a rounded border with an embedded title, exactly h rows and
// w columns.
func box(title, rightTitle string, lines []string, w, h int) string {
	iw := w - 2 // width between the corner glyphs
	cw := iw - 2
	ch := h - 2

	t := " " + title + " "
	r := ""
	if rightTitle != "" {
		r = " " + rightTitle + " "
	}
	fill := iw - 1 - lipgloss.Width(t) - lipgloss.Width(r) - 1
	if fill < 0 {
		fill = 0
	}
	var b strings.Builder
	b.WriteString(borderStyle.Render("╭─") + titleStyle.Render(t) +
		borderStyle.Render(strings.Repeat("─", fill)) + dimStyle.Render(r) + borderStyle.Render("─╮"))

	for i := 0; i < ch; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		pad := cw - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("\n" + borderStyle.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + borderStyle.Render("│"))
	}

	b.WriteString("\n" + borderStyle.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return b.String()
}

// spread left-aligns l and right-aligns r within width w.
func spread(l, r string, w int) string {
	gap := w - lipgloss.Width(l) - lipgloss.Width(r)
	if gap < 1 {
		gap = 1
	}
	return l + strings.Repeat(" ", gap) + r
}

func padR(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padL(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// signed pads a numeric string to w then colors it green/red by sign.
func signed(s string, v float64, w int) string {
	if w > 0 {
		s = padL(s, w)
	}
	if v >= 0 {
		return greenStyle.Render(s)
	}
	return redStyle.Render(s)
}

// cvdStr abbreviates a cumulative-volume-delta value with a K/M/B suffix
// scaled to its own magnitude, rather than a single fixed divisor — CVD is
// in base-asset units and ranges from single digits (BTC) to millions
// (DOGE) across the visualized watchlist, so no one fixed scale reads well
// for every coin.
func cvdStr(v float64) string {
	av := v
	if av < 0 {
		av = -av
	}
	switch {
	case av >= 1e9:
		return fmt.Sprintf("%+.1fB", v/1e9)
	case av >= 1e6:
		return fmt.Sprintf("%+.1fM", v/1e6)
	case av >= 1e3:
		return fmt.Sprintf("%+.1fK", v/1e3)
	default:
		return fmt.Sprintf("%+.0f", v)
	}
}

// fnum formats with thousands separators.
func fnum(v float64, dec int) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	ip, fp := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		ip, fp = s[:i], s[i:]
	}
	var b strings.Builder
	for j, c := range ip {
		if j > 0 && (len(ip)-j)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + fp
}

func priceDec(v float64) int {
	switch {
	case v < 1:
		return 4
	case v < 100:
		return 2
	case v < 10000:
		return 1
	default:
		return 0
	}
}

// bar renders a filled utilization bar of exactly w cells, ratio clamped
// to [0, 1].
func bar(ratio float64, w int) string {
	if w < 1 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	fill := int(ratio*float64(w) + 0.5)
	if fill > w {
		fill = w
	}
	return phaseStyle.Render(strings.Repeat("█", fill)) + dimStyle.Render(strings.Repeat("─", w-fill))
}

// truncTail truncates s to at most w display cells with a "…" tail.
func truncTail(s string, w int) string {
	if w < 1 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
