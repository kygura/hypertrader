package tui

import (
	"image/color"
	"strings"

	"github.com/hyperagent/hyperagent/internal/signal"
)

// This file is the glue between the signal package's cross-timeframe confluence and
// the views that render it (markets table + detail panel). The store holds rings for
// the whole display timeframe set per visualized coin, so confluence reads real
// multi-timeframe data rather than the single noisy bar the panel used to show.

// confluenceInputs assembles per-timeframe interpretation inputs for a coin from the
// store across the standard timeframe set, weighted so higher timeframes count more.
func (m *Model) confluenceInputs(coin string) []signal.TimeframeInput {
	weights := signal.DefaultWeights()
	ctx, _ := m.store.AssetCtx(coin)
	tfs := make([]signal.TimeframeInput, 0, len(m.tfCycle))
	for _, tf := range m.tfCycle { // {"15m","1h","4h","1d"}
		bar, ok := m.store.LatestBar(coin, tf)
		if !ok {
			continue
		}
		tfs = append(tfs, signal.TimeframeInput{
			Timeframe: tf,
			Weight:    weights[tf],
			In:        signal.Inputs{Cur: bar, History: m.store.History(coin, tf, 48), Ctx: ctx},
		})
	}
	return tfs
}

// confluence returns the ranked cross-timeframe signals for a coin (strongest-first).
func (m *Model) confluence(coin string) []signal.Confluence {
	return signal.Aggregate(m.confluenceInputs(coin))
}

// confluenceArrows renders a confluence's alignment as plain (uncolored) glyphs: one
// arrow per agreeing timeframe (capped at 4) for a directional read, a diamond for a
// non-directional context read. The caller colors the whole cell by direction.
func confluenceArrows(c signal.Confluence) string {
	if !c.Directional {
		return "◆"
	}
	n := c.Agree
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	if c.Score < 0 {
		return strings.Repeat("▾", n)
	}
	return strings.Repeat("▴", n)
}

// confColor maps a confluence to its directional color: up/down for a lean, gold for
// a non-directional context read.
func (t Theme) confColor(c signal.Confluence) color.Color {
	switch {
	case !c.Directional:
		return t.Gold
	case c.Score < 0:
		return t.Down
	default:
		return t.Up
	}
}

// confluenceCell returns the plain text and single color for a coin's strongest
// confluence, sized for the markets-table SIG column. The cell stays uncolored text
// so the table's StyleFunc can color (and background-highlight) it cleanly.
func (m *Model) confluenceCell(coin string) (string, color.Color) {
	cs := m.confluence(coin)
	if len(cs) == 0 {
		return "·", m.theme.Dim
	}
	c := cs[0]
	return confluenceArrows(c) + " " + compactLabel(c.Label), m.theme.confColor(c)
}

// compactLabel shortens a confluence label to its most informative token for the
// width-constrained markets table. The detail panel shows the full label.
func compactLabel(label string) string {
	switch label {
	case "short covering":
		return "cover"
	case "long capitulation":
		return "capit"
	case "bearish divergence":
		return "bear div"
	case "bullish divergence":
		return "bull div"
	case "crowded longs", "longs paying":
		return "longs"
	case "crowded shorts", "shorts paying":
		return "shorts"
	case "new longs":
		return "longs+"
	case "new shorts":
		return "shorts+"
	case "leading basket":
		return "leads"
	case "lagging basket":
		return "lags"
	case "vol compression":
		return "coil"
	case "vol expansion":
		return "vol↑"
	case "OI at cap", "liq pressure":
		return "squeeze"
	}
	return label
}
