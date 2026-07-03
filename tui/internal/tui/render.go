// Rendering primitives, built once and reused — the plan's "all visualization
// from Lipgloss primitives" approach. No charting library: magnitude and
// direction are conveyed by styled block runes and sign-colored bars, which read
// better at terminal resolution than a squinty braille line.
package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// blockRunes maps a 0..7 normalized level to the eight vertical block glyphs.
var blockRunes = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// hFrac maps an eighths value 0..8 to a left-aligned partial horizontal block,
// giving bars sub-character precision so a magnitude reads smoothly rather than
// snapping a whole cell at a time. Index 0 is empty; 8 is a full block.
var hFrac = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

const barTrack = "░" // unfilled portion of every horizontal bar

// fillBar renders a left-to-right magnitude bar of the given total width for a
// fraction in [0,1], with a fractional final cell. The filled portion is colored
// by fg, the remainder by the dim track color. This is the one intuitive bar
// every other primitive is built from: length encodes magnitude, nothing else.
func (t Theme) fillBar(frac float64, width int, fg lipgloss.Style) string {
	frac = clamp01(frac)
	eighths := int(math.Round(frac * float64(width) * 8))
	full := eighths / 8
	rem := eighths % 8
	if full > width {
		full, rem = width, 0
	}
	var b strings.Builder
	b.WriteString(strings.Repeat("█", full))
	used := full
	if rem > 0 && full < width {
		b.WriteString(hFrac[rem])
		used++
	}
	bar := fg.Render(b.String())
	if used < width {
		bar += t.track().Render(strings.Repeat(barTrack, width-used))
	}
	return bar
}

func (t Theme) track() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.BarTrack) }

// blockColumn renders a time series as a colored single-line sparkline. The
// series is normalized to the eight vertical-block glyphs; the row is colored by
// the sign of the net trend (last minus first). This is the right primitive for
// time-series-shaped data (OI history, funding history) where the shape is the
// signal.
func (t Theme) blockColumn(series []float64) string {
	if len(series) == 0 {
		return t.Label.Render("—")
	}
	lo, hi := series[0], series[0]
	for _, v := range series {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	span := hi - lo
	var b strings.Builder
	for _, v := range series {
		var level int
		if span > 0 {
			level = int(math.Round((v - lo) / span * 7))
		} else {
			level = 0
		}
		if level < 0 {
			level = 0
		}
		if level > 7 {
			level = 7
		}
		b.WriteRune(blockRunes[level])
	}
	trend := series[len(series)-1] - series[0]
	return lipgloss.NewStyle().Foreground(t.SignColor(trend)).Render(b.String())
}

// divergingBar renders a value signed around zero (CVD, basis) as fill growing
// outward from a center divider: left in the down color for negative, right in
// the up color for positive. No arrowheads — direction is read from which side
// of the center the fill sits on.
func (t Theme) divergingBar(label string, value, bound float64, halfWidth int, fmtVal string) string {
	frac := 0.0
	if bound > 0 {
		frac = value / bound
	}
	frac = math.Max(-1, math.Min(1, frac))
	n := int(math.Round(math.Abs(frac) * float64(halfWidth)))

	col := lipgloss.NewStyle().Foreground(t.SignColor(value))
	tr := t.track()

	var left, right string
	if value < 0 {
		left = tr.Render(strings.Repeat(barTrack, halfWidth-n)) + col.Render(strings.Repeat("█", n))
		right = tr.Render(strings.Repeat(barTrack, halfWidth))
	} else {
		left = tr.Render(strings.Repeat(barTrack, halfWidth))
		right = col.Render(strings.Repeat("█", n)) + tr.Render(strings.Repeat(barTrack, halfWidth-n))
	}
	center := tr.Render("│")
	return t.labelValue(label, value, fmtVal, t.SignColor(value)) + left + center + right
}

// barRow renders the plan §4.2 primitive: `label  value  ███▌░░░` — a labeled
// horizontal bar whose fill width encodes value's position in [min,max]. When
// signed, the value and fill take the sign color; otherwise the gold attention
// color (magnitude-only reads like liquidation proximity or realized vol).
func (t Theme) barRow(label string, value, lo, hi float64, width int, fmtVal string, signed bool) string {
	frac := 0.0
	if hi > lo {
		frac = (value - lo) / (hi - lo)
	}
	col := t.Gold
	if signed {
		col = t.SignColor(value)
	}
	return t.labelValue(label, value, fmtVal, col) +
		t.fillBar(clamp01(frac), max(1, width), lipgloss.NewStyle().Foreground(col))
}

// labelValue renders the shared `label  value  ` prefix used by every bar row.
func (t Theme) labelValue(label string, value float64, fmtVal string, valColor color.Color) string {
	lbl := t.Label.Render(fmt.Sprintf("%-9s", label))
	val := lipgloss.NewStyle().Foreground(valColor).Render(fmt.Sprintf("%10s", fmt.Sprintf(fmtVal, value)))
	return lbl + " " + val + "  "
}

// stackedBar renders flow composition in a single bar: the aggressor-buy share in
// the up color and the aggressor-sell share in the down color, sized by their
// proportion of total volume. This visualizes the HL trades feed's buy/sell split
// directly — a bar whose two segments encode *who is lifting whom*.
func (t Theme) stackedBar(label string, buy, sell float64, width int, fmtVal string) string {
	total := buy + sell
	lbl := t.Label.Render(fmt.Sprintf("%-9s", label))
	if total <= 0 {
		empty := lipgloss.NewStyle().Foreground(t.BarTrack).Render(strings.Repeat("·", width))
		return lbl + " " + t.Label.Render(fmt.Sprintf("%10s", "—")) + "  " + empty
	}
	buyW := min(int(math.Round(buy/total*float64(width))), width)
	sellW := width - buyW
	buySeg := lipgloss.NewStyle().Foreground(t.Up).Render(strings.Repeat("█", buyW))
	sellSeg := lipgloss.NewStyle().Foreground(t.Down).Render(strings.Repeat("█", sellW))

	imbal := (buy - sell) / total
	val := lipgloss.NewStyle().Foreground(t.SignColor(imbal)).Render(fmt.Sprintf("%10s", fmt.Sprintf(fmtVal, imbal)))
	return lbl + " " + val + "  " + buySeg + sellSeg
}

// fmtPx formats a price with decimal precision scaled to its magnitude, so both
// a $0.0042 micro-cap and a $95,000 BTC read cleanly without a fixed format.
func fmtPx(v float64) string {
	switch a := math.Abs(v); {
	case v == 0:
		return "—"
	case a >= 1000:
		return withCommas(v)
	case a >= 1:
		return fmt.Sprintf("%.2f", v)
	case a >= 0.01:
		return fmt.Sprintf("%.4f", v)
	default:
		return fmt.Sprintf("%.6f", v)
	}
}

// withCommas formats a value's integer part with thousands separators (Go's fmt
// has no grouping verb), e.g. 95234.7 → "95,235".
func withCommas(v float64) string {
	n := int64(math.Round(v))
	neg := n < 0
	if neg {
		n = -n
	}
	digits := fmt.Sprintf("%d", n)
	var out strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(d)
	}
	if neg {
		return "-" + out.String()
	}
	return out.String()
}

// fmtBig abbreviates large magnitudes with k/M/B suffixes for compact display of
// open interest and day volume.
func fmtBig(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case a >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	case a >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	default:
		return fmt.Sprintf("%.1f", v)
	}
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// gradientText colors each rune of s along a Blend1D gradient between the stops —
// the v2 primitive for a title that fades teal→cyan rather than sitting in one flat
// hue. With a single stop it degrades to a plain foreground.
func gradientText(s string, bold bool, stops ...color.Color) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	style := func(c color.Color) lipgloss.Style {
		st := lipgloss.NewStyle().Foreground(c)
		if bold {
			st = st.Bold(true)
		}
		return st
	}
	if len(stops) < 2 || len(runes) < 2 {
		return style(stops[0]).Render(s)
	}
	pal := lipgloss.Blend1D(len(runes), stops...)
	var b strings.Builder
	for i, ch := range runes {
		b.WriteString(style(pal[i]).Render(string(ch)))
	}
	return b.String()
}

// Title renders a pane/section title as a bold teal→cyan gradient.
func (t Theme) Title(s string) string { return gradientText(s, true, t.titleStops()...) }

// pane renders a titled, bordered box. The focused pane gets a gradient border and
// gradient title; the rest get a quiet single-color border.
func (t Theme) pane(title, body string, width, height int, focused bool) string {
	style := t.Pane
	header := t.PaneTitle.Render(title)
	if focused {
		style = t.PaneFocused
		header = t.Title(ansi.Strip(title))
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, body)
	// Clip content to the interior height (box height minus the 2 border rows).
	// lipgloss Height only pads to a minimum — it never truncates — so without
	// this a content-heavy pane (e.g. a long watchlist) grows past its slot and
	// pushes the rest of the layout (chat + its bottom border) off-screen.
	if maxC := height - 2; maxC >= 0 {
		content = clampLines(content, maxC)
	}
	// In lipgloss v2, Width/Height include the border, so the box fills the slot
	// exactly; the interior is width-4 (border+padding) by height-2.
	return style.Width(width).Height(height).Render(content)
}

// clampLines returns at most the first n lines of s.
func clampLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
