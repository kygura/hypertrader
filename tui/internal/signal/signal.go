// Package signal is the interpretation layer: it turns raw, noisy perp metrics into
// normalized, labeled, directional signals. A raw number ("funding +0.011%") is not
// actionable; "funding at +2.1σ vs its own history — crowded longs" is. Every signal
// here is a metric (or a combination of metrics) normalized against the asset's own
// recent history and classified into a regime with a directional lean.
//
// The same signals feed two consumers: the TUI detail panel (which ranks them by
// strength and shows the strongest few) and the reasoner's prompt (so the model
// reasons over interpretation, not raw arrays). Measurement lives in the aggregator;
// interpretation lives here — a clean separation.
package signal

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// Signal is one interpreted reading. Score is the directional lean (negative =
// bearish, positive = bullish, zero = non-directional/context); Strength is the
// distance-from-neutral used to rank signals, independent of direction.
type Signal struct {
	Key      string  // stable id: "oi_price", "funding_regime", …
	Label    string  // human label: "new shorts", "crowded longs"
	Score    float64 // signed −1..+1 directional lean
	Strength float64 // 0..1 magnitude/confidence — drives ranking
	Read     string  // one-line rationale (panel detail + agent prompt)
}

// Directional reports whether the signal carries a bullish/bearish lean (vs a
// non-directional context signal like vol compression).
func (s Signal) Directional() bool { return s.Score > 0.05 || s.Score < -0.05 }

// Inputs is assembled by the caller so metrics.Bar stays lean. History is the
// trailing finalized bars (oldest-first) used to normalize Cur; Cur is the bar being
// interpreted. Tier-2 fields are zero-valued until their data is fetched, and every
// detector degrades gracefully when its inputs are absent.
type Inputs struct {
	Cur     metrics.Bar
	History []metrics.Bar
	Ctx     metrics.AssetCtx

	// Tier-2 enrichments (optional).
	MaxLeverage     float64
	PrevDayPx       float64
	PredictedFund   float64
	NextFundingTime time.Time
	OICapped        bool
}

// detector is a pure function producing at most one signal from the inputs. It
// returns ok=false to abstain — insufficient history, or nothing notable right now.
type detector struct {
	key string
	fn  func(Inputs) (Signal, bool)
}

// detectors is the registry. Adding a signal is a single entry here plus its
// function — the same "registered table" shape the aggregator uses for metrics.
var detectors = []detector{
	{"oi_price", oiPrice},
	{"funding_regime", fundingRegime},
	{"cvd_div", cvdDivergence},
	{"move_sig", moveSignificance},
	{"vol_regime", volRegime},
	{"rel_strength", relStrength},
	{"liq_pressure", liqPressure},
}

// Compute runs every detector and returns the signals that fired, strongest-first so
// a caller can take the top N for a compact display or prompt.
func Compute(in Inputs) []Signal {
	out := make([]Signal, 0, len(detectors))
	for _, d := range detectors {
		if s, ok := d.fn(in); ok {
			s.Key = d.key
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b Signal) int { return cmp.Compare(b.Strength, a.Strength) })
	return out
}

// Top returns at most n signals, strongest-first.
func Top(in Inputs, n int) []Signal {
	s := Compute(in)
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// --- numeric helpers (pure, shared by detectors) ---

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func clampSigned(x float64) float64 { return clamp(x, -1, 1) }

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	var v float64
	for _, x := range xs {
		v += (x - m) * (x - m)
	}
	return math.Sqrt(v / float64(len(xs)-1))
}

// zscore returns how many standard deviations v sits from the series mean.
func zscore(series []float64, v float64) float64 {
	sd := stddev(series)
	if sd == 0 {
		return 0
	}
	return (v - mean(series)) / sd
}

// percentile returns the fraction of the series ≤ v, in [0,1].
func percentile(series []float64, v float64) float64 {
	if len(series) == 0 {
		return 0.5
	}
	c := 0
	for _, x := range series {
		if x <= v {
			c++
		}
	}
	return float64(c) / float64(len(series))
}

// seriesRange returns max−min of the series (0 for empty/flat).
func seriesRange(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	lo, hi := xs[0], xs[0]
	for _, x := range xs {
		lo = min(lo, x)
		hi = max(hi, x)
	}
	return hi - lo
}

func lastN(bars []metrics.Bar, n int) []metrics.Bar {
	if len(bars) <= n {
		return bars
	}
	return bars[len(bars)-n:]
}

func seriesOf(bars []metrics.Bar, f func(metrics.Bar) float64) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = f(b)
	}
	return out
}
