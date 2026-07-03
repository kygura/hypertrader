package aggregator

import (
	"math"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

func TestBucketStartAligns(t *testing.T) {
	tf, ok := ParseTimeframe("1h")
	if !ok {
		t.Fatal("1h should parse")
	}
	ts := time.Date(2026, 6, 6, 13, 47, 30, 0, time.UTC)
	got := bucketStart(ts, tf.Dur)
	want := time.Date(2026, 6, 6, 13, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("bucketStart = %v, want %v", got, want)
	}
}

func TestApplyTradeOHLCV(t *testing.T) {
	lb := &liveBar{bar: metrics.Bar{}}
	prices := []float64{100, 105, 98, 102}
	for i, p := range prices {
		side := metrics.SideBuy
		if i%2 == 1 {
			side = metrics.SideSell
		}
		applyTrade(lb, metrics.Trade{Price: p, Size: 1, Side: side}, 0, 0, 1e9)
	}
	b := lb.bar
	if b.Open != 100 || b.High != 105 || b.Low != 98 || b.Close != 102 {
		t.Fatalf("OHLC wrong: %+v", b)
	}
	if b.Volume != 4 {
		t.Fatalf("volume = %v, want 4", b.Volume)
	}
	if b.TradeCount != 4 {
		t.Fatalf("count = %d, want 4", b.TradeCount)
	}
	// 2 buys, 2 sells → imbalance 0.
	if b.TradeImbal != 0 {
		t.Fatalf("imbalance = %v, want 0", b.TradeImbal)
	}
}

func TestStddev(t *testing.T) {
	if got := stddev([]float64{}); got != 0 {
		t.Fatalf("empty stddev should be 0, got %v", got)
	}
	got := stddev([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	if math.Abs(got-2.138) > 0.01 {
		t.Fatalf("stddev = %v, want ~2.138", got)
	}
}

func TestCorrelationPerfectPositive(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	b := []float64{2, 4, 6, 8, 10}
	if got := correlation(a, b); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("correlation = %v, want 1.0", got)
	}
}

func TestCorrelationPerfectNegative(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	b := []float64{5, 4, 3, 2, 1}
	if got := correlation(a, b); math.Abs(got+1.0) > 1e-9 {
		t.Fatalf("correlation = %v, want -1.0", got)
	}
}

func TestAppendCapBounds(t *testing.T) {
	var xs []float64
	for i := 0; i < 100; i++ {
		xs = appendCap(xs, float64(i), 10)
	}
	if len(xs) != 10 {
		t.Fatalf("len = %d, want 10", len(xs))
	}
	if xs[0] != 90 || xs[9] != 99 {
		t.Fatalf("window wrong: %v..%v", xs[0], xs[9])
	}
}
