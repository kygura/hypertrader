package signal

// Confluence is the cross-timeframe layer. A single bar's read flips every candle
// on a low timeframe — "new longs" one bar, "long capitulation" the next — which is
// the noise the panel used to surface. Aggregate runs the same detectors across a
// set of timeframes and keeps only what *agrees* across them, weighting higher
// timeframes more. A read that holds on 1h+4h+1d is signal; a lone 15m blip is not.
//
// Measurement (aggregator) and per-bar interpretation (detectors.go) are unchanged;
// this is a third, purely-additive layer over the existing detector registry.

import (
	"cmp"
	"slices"
)

// tfRank is the canonical low→high ordering used to present agreeing timeframes.
var tfRank = map[string]int{
	"1m": 0, "5m": 1, "15m": 2, "30m": 3, "1h": 4, "2h": 5,
	"4h": 6, "8h": 7, "12h": 8, "1d": 9, "3d": 10, "1w": 11,
}

// DefaultWeights weights higher timeframes more: a regime that holds on the daily
// is worth more than a 15-minute blip. Timeframes absent here default to 1.0.
func DefaultWeights() map[string]float64 {
	return map[string]float64{"15m": 0.6, "1h": 1.0, "4h": 1.4, "1d": 1.6}
}

// TimeframeInput is one timeframe's interpretation inputs plus its confluence weight.
type TimeframeInput struct {
	Timeframe string
	Weight    float64
	In        Inputs
}

// Confluence is one detector's signal aggregated across timeframes. Score/Strength
// are weighted across the agreeing timeframes; Agree/Total drive the alignment
// indicator (e.g. ▴▴▴ = bullish on 3 of 4). Rank scales strength by how broadly the
// read agrees, so cross-timeframe reads sort above single-timeframe ones.
type Confluence struct {
	Key         string
	Label       string   // label from the dominant (strongest-weighted) agreeing timeframe
	Score       float64  // weighted net directional lean, −1..+1
	Strength    float64  // weighted mean strength of the agreeing timeframes, 0..1
	Agree       int      // timeframes agreeing with the net direction
	Total       int      // timeframes considered
	Rank        float64  // display ranking: strength scaled by cross-timeframe agreement
	Timeframes  []string // agreeing timeframes, low→high, for display
	Directional bool     // false for context signals (vol regime, liq pressure)
}

// Aggregate runs every detector across all supplied timeframes and returns one
// Confluence per detector that fired with net cross-timeframe agreement, ranked so
// the reads that hold across the most (and highest) timeframes lead. Signals that
// conflict across timeframes (no net agreement) are dropped — that is the denoising.
func Aggregate(tfs []TimeframeInput) []Confluence {
	type hit struct {
		s  Signal
		w  float64
		tf string
	}
	hits := map[string][]hit{}
	for _, d := range detectors {
		for _, t := range tfs {
			w := t.Weight
			if w <= 0 {
				w = 1
			}
			if s, ok := d.fn(t.In); ok {
				s.Key = d.key
				hits[d.key] = append(hits[d.key], hit{s, w, t.Timeframe})
			}
		}
	}

	total := len(tfs)
	if total == 0 {
		total = 1
	}

	out := make([]Confluence, 0, len(detectors))
	for _, d := range detectors { // detector order → deterministic iteration
		hs := hits[d.key]
		if len(hs) == 0 {
			continue
		}

		// Weighted net directional lean across every timeframe that fired.
		var sumW, sumWScore float64
		for _, h := range hs {
			sumW += h.w
			sumWScore += h.w * h.s.Score
		}
		net := 0.0
		if sumW > 0 {
			net = sumWScore / sumW
		}
		dir := 0
		switch {
		case net > 0.05:
			dir = 1
		case net < -0.05:
			dir = -1
		}

		// Keep only the timeframes that agree with the net direction (or, for a
		// non-directional detector, the ones that fired). The dominant agreeing
		// timeframe (highest weight×strength) supplies the display label.
		var agreeW, agreeWStrength, dominantW float64
		var label string
		var tfList []string
		for _, h := range hs {
			agree := (dir > 0 && h.s.Score > 0.05) ||
				(dir < 0 && h.s.Score < -0.05) ||
				(dir == 0 && !h.s.Directional())
			if !agree {
				continue
			}
			agreeW += h.w
			agreeWStrength += h.w * h.s.Strength
			tfList = append(tfList, h.tf)
			if w := h.w * h.s.Strength; w >= dominantW {
				dominantW, label = w, h.s.Label
			}
		}
		if len(tfList) == 0 {
			continue // conflicting across timeframes → drop as noise
		}

		strength := 0.0
		if agreeW > 0 {
			strength = agreeWStrength / agreeW
		}
		agreement := float64(len(tfList)) / float64(total)
		slices.SortFunc(tfList, func(a, b string) int { return cmp.Compare(tfRank[a], tfRank[b]) })

		out = append(out, Confluence{
			Key:         d.key,
			Label:       label,
			Score:       net,
			Strength:    strength,
			Agree:       len(tfList),
			Total:       total,
			Rank:        strength * (0.4 + 0.6*agreement),
			Timeframes:  tfList,
			Directional: dir != 0,
		})
	}

	slices.SortStableFunc(out, func(a, b Confluence) int { return cmp.Compare(b.Rank, a.Rank) })
	return out
}

// TopConfluence returns at most n confluences, strongest-first (all of them for n < 0).
func TopConfluence(tfs []TimeframeInput, n int) []Confluence {
	c := Aggregate(tfs)
	if n >= 0 && len(c) > n {
		c = c[:n]
	}
	return c
}
