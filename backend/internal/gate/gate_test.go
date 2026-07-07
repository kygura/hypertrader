package gate

import (
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// fakeTheses is a minimal ThesisView for tests.
type fakeTheses struct {
	theses map[string]metrics.Thesis
}

func (f *fakeTheses) Get(coin string) (metrics.Thesis, bool) {
	t, ok := f.theses[coin]
	return t, ok
}

// newTestGate wires a gate that records fires into the returned slice instead
// of a batcher. The clock is pinned so cooldown behavior is deterministic.
func newTestGate(t *testing.T, rules Rules, theses ThesisView) (*Gate, *store.Store, *[]metrics.Deviation, *time.Time) {
	t.Helper()
	st, err := store.New(t.TempDir(), 256)
	if err != nil {
		t.Fatal(err)
	}
	var fired []metrics.Deviation
	g := New(bus.New(), rules, st, theses, func(_ metrics.Bar, dev metrics.Deviation) {
		fired = append(fired, dev)
	})
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	return g, st, &fired, &now
}

// seedHistory fills the store with n quiet finalized bars: tiny alternating
// returns and near-steady CVD steps, so the z-score rules have a calm baseline
// with non-zero variance to measure against.
func seedHistory(st *store.Store, coin, tf string, n int) {
	open := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		st.PutBar(metrics.Bar{
			Coin: coin, Timeframe: tf, Final: true,
			OpenTime: open.Add(time.Duration(i) * time.Minute),
			Open:     100, High: 101, Low: 99, Close: 100,
			Return: 0.001 * float64(i%2),           // mean 0.0005, sd > 0
			CVD:    float64(i) + 0.05*float64(i%2), // per-bar deltas ~1.0, sd > 0
		})
	}
}

// quietBar returns a finalized LTF bar that should fire nothing under the
// default rules against seedHistory(60)'s baseline: its return sits on the
// sample mean and its CVD delta on the mean per-bar step.
func quietBar(coin string) metrics.Bar {
	return metrics.Bar{
		Coin: coin, Timeframe: "1m", Final: true,
		OpenTime: time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
		Open:     100, High: 101, Low: 99, Close: 100,
		Return: 0.0005, CVD: 60.05, Funding: 0.0001, OIDelta: 0.001,
	}
}

// TestDefaultNonPermissive verifies the core inversion: a quiet finalized LTF
// bar produces zero fires under DefaultRules — the LLM is never called on a
// quiet tape.
func TestDefaultNonPermissive(t *testing.T) {
	g, st, fired, _ := newTestGate(t, DefaultRules(), &fakeTheses{})
	seedHistory(st, "BTC", "1m", 60)
	g.onBar(quietBar("BTC"))
	if len(*fired) != 0 {
		t.Fatalf("quiet bar fired %v, want nothing", *fired)
	}
}

// TestIgnoresNonLTFAndLiveBars verifies the gate only evaluates finalized bars
// on its configured low timeframes.
func TestIgnoresNonLTFAndLiveBars(t *testing.T) {
	g, st, fired, _ := newTestGate(t, DefaultRules(), &fakeTheses{})
	seedHistory(st, "BTC", "1m", 60)

	extreme := quietBar("BTC")
	extreme.Funding = 0.01 // far past funding_abs

	htf := extreme
	htf.Timeframe = "1h" // not an LTF rung
	g.onBar(htf)

	live := extreme
	live.Final = false // in-progress bar
	g.onBar(live)

	if len(*fired) != 0 {
		t.Fatalf("non-LTF/live bars fired %v, want nothing", *fired)
	}
}

// TestThresholdRules exercises each deviation rule crossing its threshold in
// isolation.
func TestThresholdRules(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*metrics.Bar)
		wantRule string
	}{
		{"return z-score", func(b *metrics.Bar) { b.Return = 0.5 }, RuleZScoreReturn},
		{"cvd z-score", func(b *metrics.Bar) { b.CVD = 500 }, RuleCVDZScore},
		{"oi delta", func(b *metrics.Bar) { b.OIDelta = 0.05 }, RuleOIDeltaAbs},
		{"funding", func(b *metrics.Bar) { b.Funding = 0.001 }, RuleFundingAbs},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, st, fired, _ := newTestGate(t, DefaultRules(), &fakeTheses{})
			seedHistory(st, "BTC", "1m", 60)
			bar := quietBar("BTC")
			tc.mutate(&bar)
			g.onBar(bar)
			if len(*fired) != 1 || (*fired)[0].Rule != tc.wantRule {
				t.Fatalf("fired %v, want exactly one %s", *fired, tc.wantRule)
			}
			if (*fired)[0].Timeframe != "1m" {
				t.Fatalf("deviation timeframe %q, want 1m", (*fired)[0].Timeframe)
			}
		})
	}
}

// TestZScoreNeedsWarmSample verifies the z-rules stay silent below the minimum
// sample — a thin history must not make every bar look extreme.
func TestZScoreNeedsWarmSample(t *testing.T) {
	g, st, fired, _ := newTestGate(t, DefaultRules(), &fakeTheses{})
	seedHistory(st, "BTC", "1m", zMinBars-5)
	bar := quietBar("BTC")
	bar.Return = 0.5
	g.onBar(bar)
	if len(*fired) != 0 {
		t.Fatalf("thin sample fired %v, want nothing", *fired)
	}
}

// TestCooldownSuppression verifies a rule fires once, is suppressed inside its
// cooldown, and re-fires after it expires — per (coin, rule), so a different
// coin is unaffected.
func TestCooldownSuppression(t *testing.T) {
	g, st, fired, now := newTestGate(t, DefaultRules(), &fakeTheses{})
	seedHistory(st, "BTC", "1m", 60)
	seedHistory(st, "ETH", "1m", 60)

	hot := quietBar("BTC")
	hot.Funding = 0.01
	g.onBar(hot)
	g.onBar(hot) // immediately again: inside cooldown
	if len(*fired) != 1 {
		t.Fatalf("fired %d times inside cooldown, want 1", len(*fired))
	}

	// A different coin has its own cooldown key.
	hotETH := quietBar("ETH")
	hotETH.Funding = 0.01
	g.onBar(hotETH)
	if len(*fired) != 2 {
		t.Fatalf("other coin suppressed: fired %d, want 2", len(*fired))
	}

	// Advance past the cooldown: the original coin re-fires.
	*now = now.Add(31 * time.Minute)
	g.onBar(hot)
	if len(*fired) != 3 {
		t.Fatalf("post-cooldown re-fire missing: fired %d, want 3", len(*fired))
	}
}

// TestInvalidationCross verifies the thesis-invalidation watch: a bar whose
// range sweeps the level fires the invalidation rule (which outranks any
// threshold rule on the same bar), and a bar that stays clear does not.
func TestInvalidationCross(t *testing.T) {
	theses := &fakeTheses{theses: map[string]metrics.Thesis{
		"BTC": {Coin: "BTC", Direction: "long", Invalidation: 99.5, Version: 1},
	}}
	g, st, fired, _ := newTestGate(t, DefaultRules(), theses)
	seedHistory(st, "BTC", "1m", 60)

	crossing := quietBar("BTC")
	crossing.Funding = 0.01 // a threshold rule also fires; invalidation must win
	g.onBar(crossing)       // low 99 <= 99.5 <= high 101
	if len(*fired) != 1 || (*fired)[0].Rule != RuleInvalidation {
		t.Fatalf("fired %v, want exactly one %s", *fired, RuleInvalidation)
	}
	if (*fired)[0].Magnitude != 99.5 {
		t.Fatalf("invalidation magnitude %v, want the level 99.5", (*fired)[0].Magnitude)
	}

	clear := quietBar("BTC")
	clear.Low, clear.High = 101, 102 // level not swept
	clear.OpenTime = clear.OpenTime.Add(time.Minute)
	g2, st2, fired2, _ := newTestGate(t, DefaultRules(), theses)
	seedHistory(st2, "BTC", "1m", 60)
	g2.onBar(clear)
	if len(*fired2) != 0 {
		t.Fatalf("non-crossing bar fired %v, want nothing", *fired2)
	}
}

// TestPositionWithoutThesisForcesReview verifies position_always: an open
// position with no live thesis earns a forced review, and one covered by a
// thesis does not.
func TestPositionWithoutThesisForcesReview(t *testing.T) {
	g, st, fired, _ := newTestGate(t, DefaultRules(), &fakeTheses{})
	seedHistory(st, "BTC", "1m", 60)
	st.PutPosition(metrics.Position{Coin: "BTC", Size: 1, MarkPrice: 100})

	g.onBar(quietBar("BTC"))
	if len(*fired) != 1 || (*fired)[0].Rule != RulePositionReview {
		t.Fatalf("fired %v, want exactly one %s", *fired, RulePositionReview)
	}

	covered := &fakeTheses{theses: map[string]metrics.Thesis{
		"BTC": {Coin: "BTC", Direction: "long", Version: 1},
	}}
	g2, st2, fired2, _ := newTestGate(t, DefaultRules(), covered)
	seedHistory(st2, "BTC", "1m", 60)
	st2.PutPosition(metrics.Position{Coin: "BTC", Size: 1, MarkPrice: 100})
	g2.onBar(quietBar("BTC"))
	if len(*fired2) != 0 {
		t.Fatalf("covered position fired %v, want nothing", *fired2)
	}
}
