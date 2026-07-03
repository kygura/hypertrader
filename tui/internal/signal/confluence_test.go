package signal

import (
	"testing"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// oiBar builds inputs that fire the oi_price detector in a chosen direction.
// oi=+0.04, ret=+0.03 → "new longs" (bullish); ret=−0.03 → "new shorts" (bearish).
func oiBar(oi, ret float64) Inputs { return Inputs{Cur: metrics.Bar{OIDelta: oi, Return: ret}} }

func findC(cs []Confluence, key string) (Confluence, bool) {
	for _, c := range cs {
		if c.Key == key {
			return c, true
		}
	}
	return Confluence{}, false
}

func TestAggregateAgreementAcrossTimeframes(t *testing.T) {
	tfs := []TimeframeInput{
		{Timeframe: "15m", Weight: 0.6, In: oiBar(0.04, 0.03)},
		{Timeframe: "1h", Weight: 1.0, In: oiBar(0.04, 0.03)},
		{Timeframe: "4h", Weight: 1.4, In: oiBar(0.04, 0.03)},
		{Timeframe: "1d", Weight: 1.6, In: oiBar(0.04, 0.03)},
	}
	cs := Aggregate(tfs)
	c, ok := findC(cs, "oi_price")
	if !ok {
		t.Fatal("oi_price confluence missing")
	}
	if c.Agree != 4 {
		t.Errorf("expected 4 agreeing timeframes, got %d", c.Agree)
	}
	if c.Score <= 0 {
		t.Errorf("a bullish confluence should be positive, got %v", c.Score)
	}
	if c.Label != "new longs" {
		t.Errorf("label=%q, want %q", c.Label, "new longs")
	}
	if len(c.Timeframes) != 4 || c.Timeframes[0] != "15m" || c.Timeframes[3] != "1d" {
		t.Errorf("timeframes not ordered low→high: %v", c.Timeframes)
	}
	if cs[0].Key != "oi_price" {
		t.Errorf("the only strong confluence should lead, got %q", cs[0].Key)
	}
}

func TestAggregateDropsConflict(t *testing.T) {
	// Bullish on two timeframes, bearish on two, equal weight → net ≈ 0, no agreeing
	// majority in either direction → the read is dropped as noise rather than shown.
	tfs := []TimeframeInput{
		{Timeframe: "15m", Weight: 1, In: oiBar(0.04, 0.03)},
		{Timeframe: "1h", Weight: 1, In: oiBar(0.04, 0.03)},
		{Timeframe: "4h", Weight: 1, In: oiBar(0.04, -0.03)},
		{Timeframe: "1d", Weight: 1, In: oiBar(0.04, -0.03)},
	}
	if _, ok := findC(Aggregate(tfs), "oi_price"); ok {
		t.Errorf("evenly conflicting oi_price should be dropped, not surfaced")
	}
}

func TestAggregateSingleTimeframeDownranked(t *testing.T) {
	abstain := Inputs{} // oi=0, ret=0 → oi_price abstains
	one := Aggregate([]TimeframeInput{
		{Timeframe: "15m", Weight: 1, In: oiBar(0.04, 0.03)},
		{Timeframe: "1h", Weight: 1, In: abstain},
		{Timeframe: "4h", Weight: 1, In: abstain},
		{Timeframe: "1d", Weight: 1, In: abstain},
	})
	four := Aggregate([]TimeframeInput{
		{Timeframe: "15m", Weight: 1, In: oiBar(0.04, 0.03)},
		{Timeframe: "1h", Weight: 1, In: oiBar(0.04, 0.03)},
		{Timeframe: "4h", Weight: 1, In: oiBar(0.04, 0.03)},
		{Timeframe: "1d", Weight: 1, In: oiBar(0.04, 0.03)},
	})
	c1, ok1 := findC(one, "oi_price")
	c4, ok4 := findC(four, "oi_price")
	if !ok1 || !ok4 {
		t.Fatal("oi_price confluence missing in one or both runs")
	}
	if c1.Agree != 1 || c4.Agree != 4 {
		t.Fatalf("agree counts wrong: one=%d four=%d", c1.Agree, c4.Agree)
	}
	if c1.Rank >= c4.Rank {
		t.Errorf("single-timeframe rank %v should be below four-timeframe rank %v", c1.Rank, c4.Rank)
	}
}

func TestAggregateEmpty(t *testing.T) {
	if cs := Aggregate(nil); len(cs) != 0 {
		t.Errorf("no inputs should yield no confluence, got %v", cs)
	}
}
