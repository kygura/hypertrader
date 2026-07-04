package tui

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/apiclient"
	"github.com/hyperagent/tui/internal/signal"
)

// Detail-pane section constants, in render order. The cursor (m.detailSection)
// cycles through these with [ and ]; Enter triggers an agent action on the
// focused section.
const (
	detailSectionContext = 0 // the §4.1 metric stack       → no action
	detailSectionThesis  = 1 // thesis box                  → regenerate via g
	detailSectionSignals = 2 // cross-timeframe confluence  → ask agent to explain
	detailSectionCount   = 3
)

// sectionTitle renders a section header, adding a focus indicator when the
// detail pane is active and this section is the cursor position.
func (m *Model) sectionTitle(label string, section int) string {
	if m.focus == focusDetail && m.detailSection == section {
		return lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("▸ " + label)
	}
	return m.theme.PaneTitle.Render(label)
}

// renderDetail composes the per-asset detail panel per plan §4.1: the metric
// stack leads — price with a move bar, then OI Δ and funding as block-rune
// history columns, then the scalar bar rows (basis, CVD, liq prox, vol) — the
// wide perp surface read at a glance. The agent's thesis block sits beneath the
// stack it interprets, then the cross-timeframe confluence signals, then any
// open position.
func (m *Model) renderDetail(width int) string {
	coin := m.selectedCoin()
	if coin == "" {
		return m.theme.Label.Render("no asset selected")
	}
	tf := m.timeframes[coin]
	bar, hasBar := m.cache.LatestBar(coin, tf)
	ctx, _ := m.cache.AssetCtx(coin)
	mid := m.cache.Mid(coin)
	history := m.cache.History(coin, tf, 48)

	var s []string
	add := func(lines ...string) { s = append(s, lines...) }

	// --- Price row: `price  41.20  +3.1%  ███████▌······` — the move bar is
	// scaled against the window's biggest bar move so a quiet drift and a 5%
	// candle read differently at a glance. ---
	price := mid
	if price == 0 {
		price = bar.Close
	}
	if price == 0 {
		price = ctx.MarkPrice
	}
	maxMove := 0.0
	for _, b := range history {
		maxMove = math.Max(maxMove, math.Abs(b.Return))
	}
	moveFrac := 0.0
	if maxMove > 0 {
		moveFrac = math.Abs(bar.Return) / maxMove
	}
	add(fmt.Sprintf("%s  %s  %s  %s",
		m.theme.Label.Render(fmt.Sprintf("%-9s", "price")),
		lipgloss.NewStyle().Bold(true).Foreground(m.theme.SignColor(bar.Return)).Render(fmtPx(price)),
		lipgloss.NewStyle().Foreground(m.theme.SignColor(bar.Return)).Render(fmt.Sprintf("%+.2f%%", bar.Return*100)),
		m.theme.fillBar(moveFrac, max(6, min(14, width-30)), lipgloss.NewStyle().Foreground(m.theme.SignColor(bar.Return))),
	))
	if !hasBar && mid == 0 {
		add("", m.theme.Label.Render(fmt.Sprintf("warming up %s %s…", coin, tf)))
		return strings.Join(s, "\n")
	}

	// --- The §4.1 metric stack. History-shaped series (OI, funding) get block
	// columns — the shape over time is the signal; scalars get bar rows. ---
	add("", m.sectionTitle("metrics", detailSectionContext)+m.theme.Label.Render("  "+tf+" · last "+fmt.Sprint(len(history))+" bars"))
	oiSeries := seriesOf(history, func(b apiclient.Bar) float64 { return b.OpenInterest })
	fundSeries := seriesOf(history, func(b apiclient.Bar) float64 { return b.Funding })
	add(m.seriesRow("OI Δ", m.theme.blockColumn(oiSeries), bar.OIDelta*100, "%+.2f%%"))
	add(m.seriesRow("funding", m.theme.blockColumn(fundSeries), ctx.Funding*100, "%+.4f%%"))
	barW := max(6, width-26)
	add(m.theme.barRow("basis", bar.Basis*100, -absBound(history, func(b apiclient.Bar) float64 { return b.Basis * 100 }), absBound(history, func(b apiclient.Bar) float64 { return b.Basis * 100 }), barW, "%+.4f%%", true))
	add(m.theme.divergingBar("CVD", bar.CVD, cvdBound(history), max(4, barW/2), "%+.0f"))
	add(m.theme.barRow("liq prox", bar.LiqProx*100, 0, 100, barW, "%.1f%%", false))
	add(m.theme.barRow("vol", bar.RealizedVol*100, 0, absBound(history, func(b apiclient.Bar) float64 { return b.RealizedVol * 100 }), barW, "%.2f%%", false))
	if bar.BuyVolume > 0 || bar.SellVolume > 0 {
		add(m.theme.stackedBar("flow", bar.BuyVolume, bar.SellVolume, barW, "%+.2f"))
	}
	if ctx.OpenInterest > 0 || ctx.DayVolume > 0 {
		add(m.theme.Label.Render(fmt.Sprintf("%-9s", "OI/24h")) + " " +
			lipgloss.NewStyle().Foreground(m.theme.Text).Render(fmtBig(ctx.OpenInterest)) +
			m.theme.Label.Render(" oi · ") +
			lipgloss.NewStyle().Foreground(m.theme.Text).Render(fmtBig(ctx.DayVolume)) +
			m.theme.Label.Render(" vol"))
	}

	// --- Thesis block: the agent's interpretation of the stack above. The batch
	// reading (OI/funding regime, one line) renders above the full thesis. ---
	if reading := m.reading[coin]; reading != "" {
		add("", m.theme.PaneTitle.Render("agent read"))
		add(m.renderMarkdown(reading, width))
	}
	thesisBody := m.theme.Label.Render("press g for the agent's thesis · S scans all tracked markets")
	if thesis := m.thesis[coin]; thesis != "" {
		thesisBody = m.renderMarkdown(thesis, width)
	}
	add("", m.theme.ThesisBox.Width(width).Render(
		m.sectionTitle("thesis", detailSectionThesis) + "\n" + thesisBody))

	// --- Confluence: the cross-timeframe interpretation, ranked. ---
	conf := m.confluence(coin)
	add("", m.sectionTitle("signals", detailSectionSignals)+m.theme.Label.Render("  across "+strings.Join(m.tfCycle, "·")))
	if len(conf) == 0 {
		add(m.theme.Label.Render("quiet tape — nothing aligns across timeframes"))
	} else {
		n := min(4, len(conf))
		for _, c := range conf[:n] {
			add(m.signalConfluenceRow(c, width))
		}
	}

	// --- Open position, if any. ---
	if pos := m.cache.Position(coin); !pos.IsFlat() {
		dir := "LONG"
		if pos.IsShort() {
			dir = "SHORT"
		}
		add("", lipgloss.NewStyle().Foreground(m.theme.SignColor(pos.UnrealPnl)).Render(
			fmt.Sprintf("position %s %.4f @ %.4f  uPnL %+.2f", dir, pos.Size, pos.EntryPrice, pos.UnrealPnl)))
	}

	return strings.Join(s, "\n")
}

// absBound returns the max |value| of a series accessor over history, with a
// floor of 1e-9 so bar scaling never divides by zero.
func absBound(bars []apiclient.Bar, f func(apiclient.Bar) float64) float64 {
	bound := 1e-9
	for _, b := range bars {
		bound = math.Max(bound, math.Abs(f(b)))
	}
	return bound
}

// signalConfluenceRow renders one confluence: alignment arrows + label (colored by
// lean), the agreeing timeframes as dim chips, and a strength bar — all structure,
// no prose. The arrow count is how many timeframes agree, so a regime that holds on
// 1h+4h+1d reads visibly stronger than a lone 15m blip.
func (m *Model) signalConfluenceRow(c signal.Confluence, width int) string {
	col := m.theme.confColor(c)
	head := lipgloss.NewStyle().Foreground(col).Render(confluenceArrows(c) + " " + c.Label)
	tfs := m.theme.Label.Render(strings.Join(c.Timeframes, "·"))
	bar := m.theme.fillBar(c.Strength, 6, lipgloss.NewStyle().Foreground(col))
	gap := max(1, width-lipgloss.Width(head)-lipgloss.Width(tfs)-lipgloss.Width(bar)-1)
	return head + strings.Repeat(" ", gap) + tfs + " " + bar
}

// seriesRow renders `label  <sparkline/heatstrip>  value` for a time series whose
// shape is the signal and whose latest level is shown numerically alongside.
func (m *Model) seriesRow(label, series string, value float64, fmtVal string) string {
	return fmt.Sprintf("%s %s  %s",
		m.theme.Label.Render(fmt.Sprintf("%-9s", label)),
		series,
		lipgloss.NewStyle().Foreground(m.theme.SignColor(value)).Render(fmt.Sprintf(fmtVal, value)))
}

// seriesOf extracts a float series from bars via an accessor.
func seriesOf(bars []apiclient.Bar, f func(apiclient.Bar) float64) []float64 {
	out := make([]float64, 0, len(bars))
	for _, b := range bars {
		out = append(out, f(b))
	}
	return out
}

// cvdBound returns a symmetric bound for the diverging CVD bar from history.
func cvdBound(bars []apiclient.Bar) float64 {
	max := 1.0
	for _, b := range bars {
		if absf(b.CVD) > max {
			max = absf(b.CVD)
		}
	}
	return max
}
