package aggregator

import (
	"math"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// TestMultiTimeframeFold drives trades across bucket boundaries and asserts each
// configured rung (1m/5m/1w) folds independently: a trade landing in a new 1m
// bucket finalizes the prior 1m bar while the coarser 5m/1w bars keep
// accumulating, and finalizeElapsed later closes the 5m bar over the same
// trades. The 1w bucket never elapses here, so it must stay open.
func TestMultiTimeframeFold(t *testing.T) {
	st, err := store.New(t.TempDir(), 512)
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	bars := b.SubscribeBars(256)

	tfs := make([]Timeframe, 0, 3)
	for _, name := range []string{"1m", "5m", "1w"} {
		tf, ok := ParseTimeframe(name)
		if !ok {
			t.Fatalf("%s should parse", name)
		}
		tfs = append(tfs, tf)
	}
	a := New(b, st, map[string][]Timeframe{"BTC": tfs}, 1e12)

	t0 := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC) // Monday, aligned to 1m/5m
	trades := []metrics.Trade{
		{Coin: "BTC", Price: 100, Size: 1, Side: metrics.SideBuy, Time: t0},
		{Coin: "BTC", Price: 102, Size: 1, Side: metrics.SideBuy, Time: t0.Add(30 * time.Second)},
		{Coin: "BTC", Price: 101, Size: 1, Side: metrics.SideBuy, Time: t0.Add(70 * time.Second)}, // rolls the 1m bucket
	}
	for _, tr := range trades {
		a.onTrade(tr)
	}
	// Close everything whose bucket has ended by t0+6m: the second 1m bar and
	// the 5m bar. The 1w bar is still open.
	a.finalizeElapsed(t0.Add(6 * time.Minute))

	finals := map[string][]metrics.Bar{}
	for drained := false; !drained; {
		select {
		case bar := <-bars:
			if bar.Final {
				finals[bar.Timeframe] = append(finals[bar.Timeframe], bar)
			}
		default:
			drained = true
		}
	}

	// 1m: bucket [00:00,00:01) finalized by the third trade's roll; bucket
	// [00:01,00:02) finalized by finalizeElapsed.
	if len(finals["1m"]) != 2 {
		t.Fatalf("1m finalized bars = %d, want 2", len(finals["1m"]))
	}
	first := finals["1m"][0]
	if first.Open != 100 || first.High != 102 || first.Low != 100 || first.Close != 102 || first.Volume != 2 {
		t.Fatalf("first 1m bar wrong OHLCV: %+v", first)
	}

	// 5m: one bar over all three trades.
	if len(finals["5m"]) != 1 {
		t.Fatalf("5m finalized bars = %d, want 1", len(finals["5m"]))
	}
	m5 := finals["5m"][0]
	if m5.Open != 100 || m5.High != 102 || m5.Low != 100 || m5.Close != 101 || m5.Volume != 3 {
		t.Fatalf("5m bar wrong OHLCV: %+v", m5)
	}

	// 1w: bucket has not elapsed and no trade rolled it, so nothing finalized.
	if len(finals["1w"]) != 0 {
		t.Fatalf("1w finalized bars = %d, want 0 (bucket still open)", len(finals["1w"]))
	}
}

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
