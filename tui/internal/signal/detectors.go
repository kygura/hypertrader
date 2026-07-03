package signal

import (
	"fmt"
	"math"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// oiPrice classifies the open-interest/price relationship — the single most telling
// perp read. The 2×2 of OI direction vs price direction distinguishes fresh
// positioning (new longs/shorts) from unwinds (short-covering, capitulation), which
// a raw "OIΔ +12%" cannot convey.
func oiPrice(in Inputs) (Signal, bool) {
	oi, ret := in.Cur.OIDelta, in.Cur.Return
	if math.Abs(oi) < 1e-9 && math.Abs(ret) < 1e-9 {
		return Signal{}, false
	}
	oiUp, priceUp := oi >= 0, ret >= 0
	var label, read string
	var lean float64
	switch {
	case oiUp && priceUp:
		label, lean = "new longs", +1
		read = "OI rising with price — fresh longs adding, trend with conviction"
	case oiUp && !priceUp:
		label, lean = "new shorts", -1
		read = "OI rising as price falls — fresh shorts pressing, conviction selling"
	case !oiUp && priceUp:
		label, lean = "short covering", +0.5
		read = "OI falling as price rises — shorts covering, a squeeze more than new demand"
	default:
		label, lean = "long capitulation", -0.5
		read = "OI falling with price — longs bailing, deleveraging"
	}
	// Strength blends the OI move (5%/bar is strong) with the price move (3%/bar),
	// so a big OI shift on a flat tape isn't overrated and vice-versa.
	strength := clamp(0.5*clamp(math.Abs(oi)/0.05, 0, 1)+0.5*clamp(math.Abs(ret)/0.03, 0, 1), 0, 1)
	if strength < 0.1 {
		return Signal{}, false
	}
	return Signal{Label: label, Score: lean, Strength: strength, Read: read}, true
}

// fundingRegime normalizes funding against the asset's own recent funding so "is
// this high?" has an answer. Extremes are read contrarian (crowded side → squeeze
// risk), which is the swing-trading interpretation. Falls back to an absolute
// threshold when history is too short to z-score.
func fundingRegime(in Inputs) (Signal, bool) {
	fund := in.Cur.Funding
	series := seriesOf(in.History, func(b metrics.Bar) float64 { return b.Funding })
	if len(series) < 4 {
		return absoluteFunding(in)
	}
	z := zscore(series, fund)
	mag := clamp(math.Abs(z)/2.5, 0, 1) // |z| ≥ 2.5 is extreme
	if mag < 0.15 {
		return Signal{}, false // unremarkable funding → abstain
	}
	var label string
	var lean float64
	if fund >= 0 {
		label, lean = "longs paying", -mag // crowded longs lean bearish (squeeze-down)
		if mag > 0.7 {
			label = "crowded longs"
		}
	} else {
		label, lean = "shorts paying", +mag
		if mag > 0.7 {
			label = "crowded shorts"
		}
	}
	read := fmt.Sprintf("funding %+.4f%% at %+.1fσ vs recent — %s; mean-reversion/squeeze risk if it unwinds",
		fund*100, z, label)
	if in.PredictedFund != 0 {
		read += fmt.Sprintf("; predicted next %+.4f%%", in.PredictedFund*100)
	}
	return Signal{Label: label, Score: lean, Strength: mag, Read: read}, true
}

func absoluteFunding(in Inputs) (Signal, bool) {
	const bound = 0.0003 // ~0.03%/period is already notable
	fund := in.Cur.Funding
	mag := clamp(math.Abs(fund)/bound, 0, 1)
	if mag < 0.3 {
		return Signal{}, false
	}
	if fund >= 0 {
		return Signal{Label: "longs paying", Score: -mag, Strength: mag * 0.6,
			Read: fmt.Sprintf("funding %+.4f%% — longs paying (limited history to normalize)", fund*100)}, true
	}
	return Signal{Label: "shorts paying", Score: mag, Strength: mag * 0.6,
		Read: fmt.Sprintf("funding %+.4f%% — shorts paying (limited history to normalize)", fund*100)}, true
}

// cvdDivergence compares cumulative-volume-delta trend to price trend. Agreement
// confirms a move; disagreement (price up, CVD flat/down) is absorption — the
// hidden distribution/accumulation that pure price misses.
func cvdDivergence(in Inputs) (Signal, bool) {
	bars := lastN(in.History, 6)
	if len(bars) < 4 {
		return Signal{}, false
	}
	pFirst, pLast := bars[0].Close, bars[len(bars)-1].Close
	if pFirst == 0 {
		return Signal{}, false
	}
	priceChg := (pLast - pFirst) / pFirst
	if math.Abs(priceChg) < 0.003 {
		return Signal{}, false // flat price → divergence not meaningful
	}
	cvdRange := seriesRange(seriesOf(bars, func(b metrics.Bar) float64 { return b.CVD }))
	if cvdRange == 0 {
		return Signal{}, false
	}
	cvdSlope := (bars[len(bars)-1].CVD - bars[0].CVD) / cvdRange // ~ −1..+1
	priceUp, cvdUp := priceChg > 0, cvdSlope > 0
	mag := clamp(math.Abs(priceChg)/0.02, 0, 1) * clamp(math.Abs(cvdSlope), 0, 1)

	switch {
	case priceUp && !cvdUp:
		s := clamp(mag+0.2, 0, 1)
		return Signal{Label: "bearish divergence", Score: -clamp(mag+0.3, 0, 1), Strength: s,
			Read: "price making highs but CVD is not — buyers being absorbed, distribution"}, true
	case !priceUp && cvdUp:
		s := clamp(mag+0.2, 0, 1)
		return Signal{Label: "bullish divergence", Score: clamp(mag+0.3, 0, 1), Strength: s,
			Read: "price making lows but CVD is not — sellers being absorbed, accumulation"}, true
	default:
		if mag < 0.4 {
			return Signal{}, false // confirmation is weak signal; only surface if strong
		}
		if priceUp {
			return Signal{Label: "flow confirms up", Score: mag * 0.5, Strength: mag * 0.6,
				Read: "CVD rising with price — buy flow confirming the move"}, true
		}
		return Signal{Label: "flow confirms down", Score: -mag * 0.5, Strength: mag * 0.6,
			Read: "CVD falling with price — sell flow confirming the move"}, true
	}
}

// moveSignificance expresses the bar's return in units of the asset's own typical
// bar (σ), so a +3% move reads as an event on a calm asset and as noise on a wild
// one — the normalization that stops false alarms.
func moveSignificance(in Inputs) (Signal, bool) {
	ret := in.Cur.Return
	sigma := stddev(seriesOf(in.History, func(b metrics.Bar) float64 { return b.Return }))
	if sigma <= 0 {
		sigma = in.Cur.RealizedVol
	}
	if sigma <= 0 {
		return Signal{}, false
	}
	z := ret / sigma
	mag := clamp(math.Abs(z)/3.0, 0, 1) // 3σ = extreme
	if mag < 0.25 {
		return Signal{}, false
	}
	dir := "up"
	if z < 0 {
		dir = "down"
	}
	return Signal{
		Label:    fmt.Sprintf("%.1fσ %s move", math.Abs(z), dir),
		Score:    clampSigned(z / 3.0),
		Strength: mag,
		Read:     fmt.Sprintf("%+.2f%% is %.1fσ vs this asset's typical bar — %s", ret*100, math.Abs(z), significanceWord(mag)),
	}, true
}

func significanceWord(mag float64) string {
	switch {
	case mag > 0.8:
		return "extended, exhaustion risk"
	case mag > 0.5:
		return "a notable move"
	default:
		return "mildly above normal"
	}
}

// volRegime flags realized-vol extremes by percentile. It is non-directional (Score
// 0) but informative: compression precedes expansion (coiling), expansion warns of
// late-stage moves.
func volRegime(in Inputs) (Signal, bool) {
	vols := seriesOf(in.History, func(b metrics.Bar) float64 { return b.RealizedVol })
	if len(vols) < 6 {
		return Signal{}, false
	}
	p := percentile(vols, in.Cur.RealizedVol)
	switch {
	case p <= 0.15:
		return Signal{Label: "vol compression", Score: 0, Strength: clamp(0.4+(0.15-p)/0.15*0.6, 0, 1),
			Read: "realized vol in the low end of its range — coiling; expansion often follows"}, true
	case p >= 0.85:
		return Signal{Label: "vol expansion", Score: 0, Strength: clamp(0.4+(p-0.85)/0.15*0.6, 0, 1),
			Read: "realized vol elevated vs its range — energetic move, late-stage risk"}, true
	}
	return Signal{}, false
}

// relStrength reads outperformance vs the watchlist basket, noting decoupling from
// BTC (a rotation/leadership cue).
func relStrength(in Inputs) (Signal, bool) {
	rs, corr := in.Cur.RelStrength, in.Cur.BTCCorr
	mag := clamp(math.Abs(rs)/0.02, 0, 1) // 2%/bar rel move is strong
	if mag < 0.25 {
		return Signal{}, false
	}
	var label, read string
	if rs >= 0 {
		label = "leading basket"
		read = fmt.Sprintf("outperforming the watchlist by %+.2f%%", rs*100)
	} else {
		label = "lagging basket"
		read = fmt.Sprintf("underperforming the watchlist by %+.2f%%", rs*100)
	}
	if math.Abs(corr) < 0.3 {
		read += "; decoupled from BTC"
	}
	return Signal{Label: label, Score: clampSigned(rs / 0.02), Strength: mag, Read: read}, true
}

// liqPressure estimates liquidation-cascade risk from the venue's max leverage (real
// data) rather than the old proxy that merely duplicated range position. With no
// leverage data it still surfaces the OI-at-cap squeeze flag. Non-directional.
func liqPressure(in Inputs) (Signal, bool) {
	if in.MaxLeverage <= 0 {
		if in.OICapped {
			return Signal{Label: "OI at cap", Score: 0, Strength: 0.6,
				Read: "open interest pinned at the venue cap — new positioning constrained, squeeze-prone"}, true
		}
		return Signal{}, false
	}
	band := 1.0 / in.MaxLeverage // ~distance to liquidation for a max-leverage position
	var rng float64
	if in.Cur.Close > 0 {
		rng = (in.Cur.High - in.Cur.Low) / in.Cur.Close
	}
	if band <= 0 || rng <= 0 {
		return Signal{}, false
	}
	prox := clamp(rng/band, 0, 1)
	if prox < 0.3 && !in.OICapped {
		return Signal{}, false
	}
	read := fmt.Sprintf("bar range is %.0f%% of the %d× liquidation band — cascade risk elevated", prox*100, int(in.MaxLeverage))
	if in.OICapped {
		read += "; OI at venue cap"
		prox = clamp(prox+0.2, 0, 1)
	}
	return Signal{Label: "liq pressure", Score: 0, Strength: prox, Read: read}, true
}
