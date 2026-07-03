package signal

import (
	"strings"
	"testing"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

func signOf(x float64) int {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	default:
		return 0
	}
}

func TestOIPriceQuadrants(t *testing.T) {
	cases := []struct {
		oi, ret   float64
		wantLabel string
		wantSign  int
	}{
		{+0.03, +0.02, "new longs", +1},
		{+0.03, -0.02, "new shorts", -1},
		{-0.03, +0.02, "short covering", +1},
		{-0.03, -0.02, "long capitulation", -1},
	}
	for _, c := range cases {
		s, ok := oiPrice(Inputs{Cur: metrics.Bar{OIDelta: c.oi, Return: c.ret}})
		if !ok {
			t.Fatalf("oiPrice abstained for oi=%v ret=%v", c.oi, c.ret)
		}
		if s.Label != c.wantLabel {
			t.Errorf("oi=%v ret=%v: label=%q want %q", c.oi, c.ret, s.Label, c.wantLabel)
		}
		if signOf(s.Score) != c.wantSign {
			t.Errorf("oi=%v ret=%v: score=%v sign=%d want %d", c.oi, c.ret, s.Score, signOf(s.Score), c.wantSign)
		}
	}
}

func TestFundingRegimeCrowdedLongs(t *testing.T) {
	// A low, slightly noisy funding history, then a large positive current funding:
	// that's a positive z-score → crowded longs, which leans bearish (squeeze-down).
	noise := []float64{1.0e-5, 1.2e-5, 0.8e-5, 1.1e-5, 0.9e-5, 1.0e-5, 1.3e-5, 0.7e-5, 1.1e-5, 0.9e-5}
	var hist []metrics.Bar
	for _, f := range noise {
		hist = append(hist, metrics.Bar{Funding: f})
	}
	s, ok := fundingRegime(Inputs{Cur: metrics.Bar{Funding: 3.0e-4}, History: hist})
	if !ok {
		t.Fatal("fundingRegime abstained on a clear outlier")
	}
	if !strings.Contains(s.Label, "long") {
		t.Errorf("label=%q, want a 'long' regime", s.Label)
	}
	if s.Score >= 0 {
		t.Errorf("crowded longs should lean bearish, score=%v", s.Score)
	}
	if s.Strength < 0.5 {
		t.Errorf("a large funding outlier should be strong, got %v", s.Strength)
	}
}

func TestCVDBearishDivergence(t *testing.T) {
	// Price climbs across the window while CVD falls → bearish divergence (absorption).
	var hist []metrics.Bar
	for i := 0; i < 6; i++ {
		hist = append(hist, metrics.Bar{
			Close: 100 + float64(i),
			CVD:   1000 - float64(i)*100,
		})
	}
	s, ok := cvdDivergence(Inputs{History: hist})
	if !ok {
		t.Fatal("cvdDivergence abstained")
	}
	if s.Label != "bearish divergence" {
		t.Errorf("label=%q, want bearish divergence", s.Label)
	}
	if s.Score >= 0 {
		t.Errorf("bearish divergence should be negative, got %v", s.Score)
	}
}

func TestMoveSignificanceNormalizesByVol(t *testing.T) {
	// Calm history (~0.1% bars); a +1% bar is a ~10σ event → high strength, bullish.
	var hist []metrics.Bar
	for i := 0; i < 10; i++ {
		r := 0.001
		if i%2 == 0 {
			r = -0.001
		}
		hist = append(hist, metrics.Bar{Return: r})
	}
	s, ok := moveSignificance(Inputs{Cur: metrics.Bar{Return: 0.01}, History: hist})
	if !ok {
		t.Fatal("moveSignificance abstained")
	}
	if s.Strength < 0.9 {
		t.Errorf("a ~10σ move should be near-max strength, got %v", s.Strength)
	}
	if s.Score <= 0 {
		t.Errorf("an up move should be positive, got %v", s.Score)
	}
}

func TestComputeRanksByStrength(t *testing.T) {
	// Inputs crafted to fire multiple detectors; Compute must return them
	// strongest-first.
	var hist []metrics.Bar
	for i := 0; i < 12; i++ {
		hist = append(hist, metrics.Bar{
			Close:       100 + float64(i),     // rising price
			CVD:         1000 - float64(i)*80, // falling CVD (bearish divergence)
			Funding:     1.0e-5 + float64(i)*1e-7,
			Return:      0.001 * float64((i%3)-1), // some variance
			RealizedVol: 0.002,
		})
	}
	in := Inputs{
		Cur: metrics.Bar{
			OIDelta: 0.04, Return: -0.02, // new shorts
			Funding: 3.0e-4, // crowded longs
			Close:   112, High: 113, Low: 109,
			RealizedVol: 0.006,
		},
		History: hist,
	}
	sigs := Compute(in)
	if len(sigs) < 2 {
		t.Fatalf("expected several signals, got %d: %+v", len(sigs), sigs)
	}
	for i := 1; i < len(sigs); i++ {
		if sigs[i-1].Strength < sigs[i].Strength {
			t.Fatalf("Compute not sorted by strength desc: %+v", sigs)
		}
	}
}
