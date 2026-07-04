package tui

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/hyperagent/tui/internal/apiclient"
)

// The marketwatch: every visualized asset in one scannable, sortable grid, rendered
// with lipgloss/v2/table. Each cell is plain text; all color (sign colors, the
// selected-row highlight, the confluence direction) is applied by the table's
// StyleFunc, so a styled cell never leaves stray resets inside the row highlight.

// sortKey selects the column the markets table orders by.
type sortKey int

const (
	sortWatchlist sortKey = iota // configured watchlist order (stable; default)
	sortChange                   // 24h / timeframe return
	sortFunding
	sortOI
	sortRel
	sortConfluence
)

func (s sortKey) String() string {
	switch s {
	case sortChange:
		return "Δ%"
	case sortFunding:
		return "funding"
	case sortOI:
		return "OIΔ"
	case sortRel:
		return "rel-str"
	case sortConfluence:
		return "signal"
	default:
		return "watchlist"
	}
}

// cycleSort advances the sort key, re-anchoring the cursor onto the same asset so the
// selection doesn't jump when the order changes.
func (m *Model) cycleSort() tea.Cmd {
	prev := m.selectedCoin()
	m.sortKey = (m.sortKey + 1) % 6
	m.reanchor(prev)
	return m.note("sort: " + m.sortKey.String())
}

// reanchor points m.selected back at coin in the current display order.
func (m *Model) reanchor(coin string) {
	for i, c := range m.ordered() {
		if c == coin {
			m.selected = i
			return
		}
	}
}

// ordered is the displayed asset order: the filtered watchlist, sorted by the active
// sort key. It is the single source of truth selection indexes into, so the markets
// table, the watch strip, the detail panel, and j/k all agree.
func (m *Model) ordered() []string {
	coins := m.filtered()
	if m.sortKey == sortWatchlist || len(coins) < 2 {
		return coins
	}
	out := append([]string(nil), coins...)
	sort.SliceStable(out, func(i, j int) bool {
		return m.sortMetric(out[i]) > m.sortMetric(out[j]) // metrics: high → low
	})
	return out
}

// sortMetric returns the value the active sort key ranks a coin by.
func (m *Model) sortMetric(coin string) float64 {
	bar, _ := m.cache.LatestBar(coin, m.timeframes[coin])
	switch m.sortKey {
	case sortChange:
		return bar.Return
	case sortFunding:
		if ctx, ok := m.cache.AssetCtx(coin); ok {
			return ctx.Funding
		}
		return bar.Funding
	case sortOI:
		return bar.OIDelta
	case sortRel:
		return bar.RelStrength
	case sortConfluence:
		if cs := m.confluence(coin); len(cs) > 0 {
			return cs[0].Rank
		}
		return 0
	}
	return 0
}

// marketRowData is one asset's precomputed display values, gathered once per render
// so each column reads from a struct rather than re-hitting the cache.
type marketRowData struct {
	coin            string
	idx             int
	price           float64
	ret             float64
	moveFrac        float64 // |ret| / max |ret| across the displayed watchlist
	funding         float64
	oiDelta         float64
	rel             float64
	spark           []float64
	sigText         string
	sigColor        color.Color
	tracked, hasPos bool
}

func (m *Model) marketRow(coin string) marketRowData {
	tf := m.timeframes[coin]
	bar, _ := m.cache.LatestBar(coin, tf)
	ctx, _ := m.cache.AssetCtx(coin)
	price := m.cache.Mid(coin)
	if price == 0 {
		price = bar.Close
	}
	if price == 0 {
		price = ctx.MarkPrice
	}
	funding := ctx.Funding
	if funding == 0 {
		funding = bar.Funding
	}
	sigText, sigColor := m.confluenceCell(coin)
	hist := m.cache.History(coin, tf, 7)
	return marketRowData{
		coin:     coin,
		idx:      m.assetIndex(coin),
		price:    price,
		ret:      bar.Return,
		funding:  funding,
		oiDelta:  bar.OIDelta,
		rel:      bar.RelStrength,
		spark:    seriesOf(hist, func(b apiclient.Bar) float64 { return b.Close }),
		sigText:  sigText,
		sigColor: sigColor,
		tracked:  m.tracked[coin],
		hasPos:   !m.cache.Position(coin).IsFlat(),
	}
}

// marketCol describes one table column: its header, alignment, the minimum interior
// width at which it earns a slot, and how to render a cell from a row's data.
type marketCol struct {
	header string
	align  lipgloss.Position
	min    int
	cell   func(t Theme, d marketRowData) (string, color.Color)
}

// moveBarWidth is the cell width of the inline move-magnitude bar.
const moveBarWidth = 8

// allMarketCols is the full column set in the plan §4.1 marketwatch order:
// asset · price · %Δ · move bar, then the perp-regime extras as width allows.
// renderMarkets keeps the columns that fit (COIN + LAST + Δ% always survive).
func allMarketCols() []marketCol {
	return []marketCol{
		{"COIN", lipgloss.Left, 0, func(t Theme, d marketRowData) (string, color.Color) {
			name := d.coin
			if d.hasPos {
				name = "•" + name
			}
			return name, t.AssetColor(d.idx)
		}},
		{"LAST", lipgloss.Right, 0, func(t Theme, d marketRowData) (string, color.Color) {
			return fmtPx(d.price), t.Text
		}},
		{"Δ%", lipgloss.Right, 0, func(t Theme, d marketRowData) (string, color.Color) {
			return fmt.Sprintf("%+.2f%%", d.ret*100), t.SignColor(d.ret)
		}},
		{"", lipgloss.Left, 34, func(t Theme, d marketRowData) (string, color.Color) {
			// The plan's inline bar: filled width scales to the move's magnitude
			// relative to the watchlist's biggest mover; color carries the sign.
			return moveBarRunes(d.moveFrac, moveBarWidth), t.SignColor(d.ret)
		}},
		{"FUND", lipgloss.Right, 50, func(t Theme, d marketRowData) (string, color.Color) {
			// Positive funding = longs pay (crowded-long pressure) → warn color.
			c := t.Up
			if d.funding > 0 {
				c = t.Down
			}
			return fmt.Sprintf("%+.3f%%", d.funding*100), c
		}},
		{"OIΔ", lipgloss.Right, 62, func(t Theme, d marketRowData) (string, color.Color) {
			return fmt.Sprintf("%+.1f%%", d.oiDelta*100), t.SignColor(d.oiDelta)
		}},
		{"SIG", lipgloss.Left, 74, func(t Theme, d marketRowData) (string, color.Color) {
			return d.sigText, d.sigColor
		}},
		{"7d", lipgloss.Left, 90, func(t Theme, d marketRowData) (string, color.Color) {
			trend := 0.0
			if len(d.spark) >= 2 {
				trend = d.spark[len(d.spark)-1] - d.spark[0]
			}
			return sparkRunes(d.spark), t.SignColor(trend)
		}},
	}
}

// columnsFor returns the columns that fit width, preserving the all-cols order but
// keeping the marketwatch minimum — COIN, LAST, Δ% (indexes 0-2) — regardless.
func columnsFor(width int) []marketCol {
	all := allMarketCols()
	cols := all[:3:3] // COIN + LAST + Δ% always
	for _, c := range all[3:] {
		if width >= c.min {
			cols = append(cols, c)
		}
	}
	return cols
}

// moveBarRunes renders the move-magnitude bar as bare runes (no color): filled
// eighth-blocks scaled to frac of width, dim track for the rest. The caller
// colors the whole cell by the move's sign, preserving the table's
// single-color-per-cell contract (see the StyleFunc note atop this file).
func moveBarRunes(frac float64, width int) string {
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
	b.WriteString(strings.Repeat(barTrack, width-used))
	return b.String()
}

// renderMarkets builds the markets table sized to the given interior width/height,
// windowed so the selected row stays visible. Returns a placeholder when empty.
func (m *Model) renderMarkets(width, height int) string {
	coins := m.ordered()
	if len(coins) == 0 {
		return m.theme.Label.Render("no assets\n/watch add COIN")
	}
	cols := columnsFor(width)

	// Gather every row's data first: the move bar scales each asset's |return|
	// against the watchlist's biggest mover, so the max must be known before any
	// cell renders.
	rows := make([]marketRowData, len(coins))
	maxAbsRet := 0.0
	for r, coin := range coins {
		rows[r] = m.marketRow(coin)
		maxAbsRet = math.Max(maxAbsRet, math.Abs(rows[r].ret))
	}
	if maxAbsRet > 0 {
		for r := range rows {
			rows[r].moveFrac = math.Abs(rows[r].ret) / maxAbsRet
		}
	}

	// Precompute per-cell (text,color) once.
	type cell struct {
		text string
		col  color.Color
	}
	grid := make([][]cell, len(coins))
	headers := make([]string, len(cols))
	for c, mc := range cols {
		headers[c] = mc.header
	}
	for r := range rows {
		row := make([]cell, len(cols))
		for c, mc := range cols {
			txt, col := mc.cell(m.theme, rows[r])
			row[c] = cell{txt, col}
		}
		grid[r] = row
	}

	tbl := table.New().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).BorderBottom(false).BorderLeft(false).
		BorderRight(false).BorderColumn(false).BorderRow(false).BorderHeader(true).
		BorderStyle(lipgloss.NewStyle().Foreground(m.theme.Subtle)).
		Headers(headers...).
		Width(width).
		Height(max(2, height)).
		Wrap(false).
		StyleFunc(func(r, c int) lipgloss.Style {
			if c < 0 || c >= len(cols) {
				return lipgloss.NewStyle()
			}
			align := cols[c].align
			if r == table.HeaderRow {
				return m.theme.TableHeader.Padding(0, 1).Align(align)
			}
			st := lipgloss.NewStyle().Padding(0, 1).Align(align)
			if r >= 0 && r < len(grid) {
				st = st.Foreground(grid[r][c].col)
				if r == m.selected {
					st = st.Background(m.theme.Surface).Bold(true)
				}
			}
			return st
		})

	for _, row := range grid {
		cells := make([]string, len(row))
		for c, cl := range row {
			cells[c] = cl.text
		}
		tbl.Row(cells...)
	}

	// Window vertically so the cursor is always on screen.
	visible := max(1, height-2) // header + rule
	if off := m.selected - visible + 1; off > 0 {
		tbl.YOffset(off)
	}
	return tbl.Render()
}

// sparkRunes renders a series as bare block-rune glyphs (no color); the caller colors
// the whole string by trend so it stays a single-color cell.
func sparkRunes(series []float64) string {
	if len(series) == 0 {
		return strings.Repeat("·", 5)
	}
	lo, hi := series[0], series[0]
	for _, v := range series {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	span := hi - lo
	var b strings.Builder
	for _, v := range series {
		level := 0
		if span > 0 {
			level = int((v - lo) / span * 7)
		}
		if level < 0 {
			level = 0
		}
		if level > 7 {
			level = 7
		}
		b.WriteRune(blockRunes[level])
	}
	return b.String()
}
