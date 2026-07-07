// Package gate is the deterministic deviation detector deciding when the tape
// has earned LLM tokens between scheduled thesis reviews. It watches finalized
// low-timeframe bars, runs threshold rules (return z-score, funding, OI delta,
// CVD z-score), and fires a trigger — rarely, per-(coin, rule) cooldown-capped
// — toward the batcher. It also watches each coin's thesis invalidation level
// and forces a review on crossing: the agent cannot sleep through its level
// being run. The default rules are non-permissive; a quiet tape produces zero
// trigger calls.
package gate

import (
	"context"
	"math"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// Rule names. The batcher routes invalidation and position-review fires to a
// forced review digest (full ladder); the threshold rules produce a compact
// trigger digest.
const (
	RuleZScoreReturn   = "zscore_return"
	RuleFundingAbs     = "funding_abs"
	RuleOIDeltaAbs     = "oi_delta_abs"
	RuleCVDZScore      = "cvd_zscore"
	RuleInvalidation   = "invalidation"
	RulePositionReview = "position_review"
)

// zHistoryBars is how many finalized bars back the z-score rules measure
// against, and zMinBars the minimum sample before they activate — too small a
// sample makes every bar look extreme, defeating the non-permissive intent.
const (
	zHistoryBars = 120
	zMinBars     = 20
)

// Rules configures the deterministic fire conditions. A zero threshold
// disables that rule; an empty timeframe set disables deviation detection
// entirely (invalidation/position watches included, since they ride the same
// LTF closes).
type Rules struct {
	LTFTimeframes []string // finalized bars the rules run on
	ZScoreReturn  float64  // |return z-score| over history exceeds this
	FundingAbs    float64  // |funding| exceeds this
	OIDeltaAbs    float64  // |OI delta| fraction exceeds this
	CVDZScore     float64  // |per-bar CVD delta z-score| over history exceeds this
	Cooldown      time.Duration
	// PositionAlways forces a review for a coin holding an open position with
	// no live thesis — an open position must never run uncovered by a thesis,
	// even between scheduled review closes.
	PositionAlways bool
}

// DefaultRules returns the non-permissive default, mirroring config.Default's
// [gate] section.
func DefaultRules() Rules {
	return Rules{
		LTFTimeframes:  []string{"1m", "5m", "15m"},
		ZScoreReturn:   3.0,
		FundingAbs:     0.0008,
		OIDeltaAbs:     0.04,
		CVDZScore:      3.0,
		Cooldown:       30 * time.Minute,
		PositionAlways: true,
	}
}

// ThesisView is the read side of the thesis store the gate needs: the live
// thesis (for its invalidation level) per coin. Kept as an interface so tests
// stub it without a disk-backed store.
type ThesisView interface {
	Get(coin string) (metrics.Thesis, bool)
}

// Gate consumes finalized bars and fires deviations toward the batcher.
type Gate struct {
	bus     *bus.Bus
	rules   Rules
	store   *store.Store
	theses  ThesisView
	trigger func(metrics.Bar, metrics.Deviation)

	ltf       map[string]bool      // timeframe -> watched
	lastFired map[string]time.Time // coin|rule -> last fire time
	now       func() time.Time     // injectable clock for cooldown tests
}

// New builds a gate. trigger is called (synchronously, from Run's goroutine)
// for every fire that survives the cooldown — main wires it to the batcher's
// Trigger method. theses may be nil (no invalidation watch until wired).
func New(b *bus.Bus, rules Rules, st *store.Store, theses ThesisView, trigger func(metrics.Bar, metrics.Deviation)) *Gate {
	ltf := make(map[string]bool, len(rules.LTFTimeframes))
	for _, tf := range rules.LTFTimeframes {
		ltf[tf] = true
	}
	return &Gate{
		bus:       b,
		rules:     rules,
		store:     st,
		theses:    theses,
		trigger:   trigger,
		ltf:       ltf,
		lastFired: make(map[string]time.Time),
		now:       time.Now,
	}
}

// Run consumes bars and evaluates the rules on finalized LTF closes. Blocks
// until ctx is cancelled.
func (g *Gate) Run(ctx context.Context) {
	bars := g.bus.SubscribeBars(1024)
	for {
		select {
		case <-ctx.Done():
			return
		case bar, ok := <-bars:
			if !ok {
				return
			}
			g.onBar(bar)
		}
	}
}

// onBar runs every rule against one finalized LTF bar and fires at most one
// deviation — the highest-priority rule that both exceeds its threshold and
// is off cooldown. One fire per bar keeps a violent bar from fanning out into
// several near-identical trigger digests for the same coin.
func (g *Gate) onBar(bar metrics.Bar) {
	if !bar.Final || !g.ltf[bar.Timeframe] {
		return
	}
	for _, dev := range g.evaluate(bar) {
		if g.onCooldown(bar.Coin, dev.Rule) {
			continue
		}
		g.markFired(bar.Coin, dev.Rule)
		if g.trigger != nil {
			g.trigger(bar, dev)
		}
		return
	}
}

// evaluate returns every rule that fires for the bar, in priority order:
// invalidation first (it forces a review — the thesis's own risk logic), then
// the deviation thresholds, then the uncovered-position watch.
func (g *Gate) evaluate(bar metrics.Bar) []metrics.Deviation {
	var out []metrics.Deviation
	add := func(rule string, magnitude float64) {
		out = append(out, metrics.Deviation{Rule: rule, Magnitude: magnitude, Timeframe: bar.Timeframe})
	}

	var thesis metrics.Thesis
	var hasThesis bool
	if g.theses != nil {
		thesis, hasThesis = g.theses.Get(bar.Coin)
	}

	// Invalidation watch: the bar's range swept the thesis level.
	if hasThesis && thesis.Invalidation > 0 &&
		bar.Low <= thesis.Invalidation && thesis.Invalidation <= bar.High {
		add(RuleInvalidation, thesis.Invalidation)
	}

	history := g.history(bar)
	if g.rules.ZScoreReturn > 0 {
		if z := zScore(returns(history), bar.Return); math.Abs(z) >= g.rules.ZScoreReturn {
			add(RuleZScoreReturn, z)
		}
	}
	if g.rules.CVDZScore > 0 && len(history) > 0 {
		delta := bar.CVD - history[len(history)-1].CVD
		if z := zScore(cvdDeltas(history), delta); math.Abs(z) >= g.rules.CVDZScore {
			add(RuleCVDZScore, z)
		}
	}
	if g.rules.OIDeltaAbs > 0 && math.Abs(bar.OIDelta) >= g.rules.OIDeltaAbs {
		add(RuleOIDeltaAbs, bar.OIDelta)
	}
	if g.rules.FundingAbs > 0 && math.Abs(bar.Funding) >= g.rules.FundingAbs {
		add(RuleFundingAbs, bar.Funding)
	}

	// Uncovered position: open exposure with no live thesis forces a review so
	// the position is never held without a maintained view behind it.
	if g.rules.PositionAlways && !hasThesis && g.store != nil && !g.store.Position(bar.Coin).IsFlat() {
		add(RulePositionReview, 0)
	}
	return out
}

// history returns the bar's timeframe history excluding the fired bar itself,
// so the z-score sample is "everything before this close".
func (g *Gate) history(bar metrics.Bar) []metrics.Bar {
	if g.store == nil {
		return nil
	}
	h := g.store.History(bar.Coin, bar.Timeframe, zHistoryBars+1)
	if n := len(h); n > 0 && h[n-1].OpenTime.Equal(bar.OpenTime) {
		h = h[:n-1]
	}
	return h
}

func (g *Gate) onCooldown(coin, rule string) bool {
	if g.rules.Cooldown <= 0 {
		return false
	}
	last, ok := g.lastFired[coin+"|"+rule]
	return ok && g.now().Sub(last) < g.rules.Cooldown
}

func (g *Gate) markFired(coin, rule string) {
	g.lastFired[coin+"|"+rule] = g.now()
}

func returns(bars []metrics.Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Return
	}
	return out
}

// cvdDeltas converts the running CVD series into per-bar deltas — CVD is
// cumulative, so the anomaly signal is the change per bar, not the level.
func cvdDeltas(bars []metrics.Bar) []float64 {
	if len(bars) < 2 {
		return nil
	}
	out := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		out = append(out, bars[i].CVD-bars[i-1].CVD)
	}
	return out
}

// zScore measures x against the mean/stddev of the sample. It returns 0 for
// samples below zMinBars — a thin sample makes everything look extreme, which
// would defeat the non-permissive default during warm-up.
func zScore(sample []float64, x float64) float64 {
	if len(sample) < zMinBars {
		return 0
	}
	var sum float64
	for _, v := range sample {
		sum += v
	}
	mean := sum / float64(len(sample))
	var v float64
	for _, s := range sample {
		v += (s - mean) * (s - mean)
	}
	sd := math.Sqrt(v / float64(len(sample)-1))
	if sd == 0 {
		return 0
	}
	return (x - mean) / sd
}
