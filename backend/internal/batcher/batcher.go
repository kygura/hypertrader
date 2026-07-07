// Package batcher freezes a per-asset digest and hands it (via the bus) to the
// reasoner. Two tiers: a review digest on each tracked asset's review-timeframe
// close (the scheduled thesis cadence), and a trigger digest when the gate
// fires a low-timeframe deviation. Review digests carry the full HTF ladder;
// trigger digests stay compact — that split is the context-window economizer
// the plan calls for.
//
// The batcher listens for finalized bars on the bus. A finalized bar arriving
// for (coin, tf) means that timeframe just closed, so it is the natural trigger
// to assemble a digest from the store.
package batcher

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/gate"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// rung is one ladder entry: a timeframe and how many bars of it to carry.
type rung struct {
	TF string
	N  int
}

// ladderRungs is the review-digest context ladder: timeframe -> bar count.
// 1h gives the recent regime, 4h/1d the swing structure, 1w the macro trend.
// Rungs still warming up are omitted; the prompt notes the gap.
var ladderRungs = []rung{
	{"1h", 120},
	{"4h", 90},
	{"1d", 90},
	{"1w", 52},
}

// triggerRungs is the compact HTF summary trigger digests carry: enough
// higher-timeframe closes to place the deviation in context without paying
// for the full review ladder.
var triggerRungs = []rung{
	{"4h", 20},
	{"1d", 20},
}

// ThesisView is the read side of the thesis store the batcher needs; nil is
// tolerated (digests simply carry no thesis).
type ThesisView interface {
	Get(coin string) (metrics.Thesis, bool)
}

// Batcher assembles digests on timeframe closes and gate fires.
type Batcher struct {
	bus         *bus.Bus
	store       *store.Store
	journal     *journal.Journal
	theses      ThesisView
	historyBars int

	mu         sync.RWMutex
	strategies map[string]metrics.AssetStrategy // coin -> strategy (the tracked set)
}

// New builds a batcher. strategies maps tracked coins to their per-asset config;
// only coins present here are batched (the tracked subset). The set is mutable at
// runtime via Track/Untrack so the TUI's /track command takes effect live.
func New(b *bus.Bus, s *store.Store, j *journal.Journal, theses ThesisView, strategies map[string]metrics.AssetStrategy, historyBars int) *Batcher {
	if strategies == nil {
		strategies = map[string]metrics.AssetStrategy{}
	}
	return &Batcher{
		bus:         b,
		store:       s,
		journal:     j,
		theses:      theses,
		strategies:  strategies,
		historyBars: historyBars,
	}
}

// Track adds (or replaces) a coin in the tracked set the agent reasons over.
func (bt *Batcher) Track(strat metrics.AssetStrategy) {
	bt.mu.Lock()
	bt.strategies[strat.Coin] = strat
	bt.mu.Unlock()
}

// Untrack removes a coin from the tracked set.
func (bt *Batcher) Untrack(coin string) {
	bt.mu.Lock()
	delete(bt.strategies, coin)
	bt.mu.Unlock()
}

// Tracked returns the sorted list of currently tracked coins.
func (bt *Batcher) Tracked() []string {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	out := make([]string, 0, len(bt.strategies))
	for c := range bt.strategies {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (bt *Batcher) strategy(coin string) (metrics.AssetStrategy, bool) {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	s, ok := bt.strategies[coin]
	return s, ok
}

// Run watches for finalized bars and emits review digests for tracked assets
// when their review timeframe closes. It blocks until ctx is cancelled.
func (bt *Batcher) Run(ctx context.Context) {
	bars := bt.bus.SubscribeBars(1024)
	for {
		select {
		case <-ctx.Done():
			return
		case bar := <-bars:
			bt.onBar(bar)
		}
	}
}

func (bt *Batcher) onBar(bar metrics.Bar) {
	// The aggregator publishes the in-progress bar on every trade so the TUI can
	// render live; only finalized bars mark a timeframe close and earn a digest.
	if !bar.Final {
		return
	}
	strat, tracked := bt.strategy(bar.Coin)
	if !tracked || strat.Timeframe != bar.Timeframe {
		return // not a tracked asset, or not its review timeframe
	}
	bt.bus.PublishDigest(bt.buildReview(strat, bar, nil))
}

// Trigger is the gate's fire hook: bar is the finalized LTF bar that tripped
// the rule, dev the rule/magnitude/timeframe. Invalidation crossings and
// uncovered-position fires escalate to a forced review (full ladder — the
// thesis itself is in question); threshold deviations get the compact trigger
// digest. Untracked coins are ignored — the gate watches everything on the
// bus, the tracked set decides who earns tokens.
func (bt *Batcher) Trigger(bar metrics.Bar, dev metrics.Deviation) {
	strat, tracked := bt.strategy(bar.Coin)
	if !tracked {
		return
	}
	if dev.Rule == gate.RuleInvalidation || dev.Rule == gate.RulePositionReview {
		// Forced review: anchor on the review timeframe's latest bar when one
		// exists; the LTF bar that fired still stands in during warm-up.
		current := bar
		if latest, ok := bt.store.LatestBar(strat.Coin, strat.Timeframe); ok {
			current = latest
		}
		bt.bus.PublishDigest(bt.buildReview(strat, current, &dev))
		return
	}
	bt.bus.PublishDigest(bt.buildTrigger(strat, bar, dev))
}

// Scan publishes a review digest per named tracked coin — all tracked coins
// when none are named — from the store's current state. This is the on-demand
// synthesis path: where onBar waits for a timeframe close, Scan reads the tape
// now, so the reasoner can rank the whole watchlist the moment the operator
// asks instead of up to a full timeframe later. Untracked names and coins
// whose rings are still cold are skipped.
func (bt *Batcher) Scan(coins ...string) {
	if len(coins) == 0 {
		coins = bt.Tracked()
	}
	for _, coin := range coins {
		strat, tracked := bt.strategy(coin)
		if !tracked {
			continue
		}
		bar, ok := bt.store.LatestBar(coin, strat.Timeframe)
		if !ok {
			continue
		}
		bt.bus.PublishDigest(bt.buildReview(strat, bar, nil))
	}
}

// buildReview assembles the full review digest: base state plus the HTF ladder.
func (bt *Batcher) buildReview(strat metrics.AssetStrategy, current metrics.Bar, dev *metrics.Deviation) metrics.Digest {
	d := bt.buildBase(strat, current)
	d.Kind = metrics.DigestReview
	d.Ladder = bt.ladder(strat.Coin, ladderRungs)
	d.Deviation = dev
	return d
}

// buildTrigger assembles the compact trigger digest: base state, the deviation
// that fired, and a short HTF tail for context.
func (bt *Batcher) buildTrigger(strat metrics.AssetStrategy, current metrics.Bar, dev metrics.Deviation) metrics.Digest {
	d := bt.buildBase(strat, current)
	d.Kind = metrics.DigestTrigger
	d.Timeframe = dev.Timeframe // the digest is about the LTF bar that fired
	d.Ladder = bt.ladder(strat.Coin, triggerRungs)
	d.Deviation = &dev
	return d
}

// ladder collects the configured rungs from the store, omitting empty ones.
func (bt *Batcher) ladder(coin string, rungs []rung) map[string][]metrics.Bar {
	out := make(map[string][]metrics.Bar, len(rungs))
	for _, r := range rungs {
		if bars := bt.store.History(coin, r.TF, r.N); len(bars) > 0 {
			out[r.TF] = bars
		}
	}
	return out
}

// buildBase assembles the per-asset state every digest kind shares.
func (bt *Batcher) buildBase(strat metrics.AssetStrategy, current metrics.Bar) metrics.Digest {
	// The reasoner must see the cap the executor will actually enforce: the
	// absolute ceiling tightened by the capital-relative cap at live equity.
	if strat.MaxPositionPct > 0 {
		if eq := bt.store.AccountValue(); eq > 0 {
			if capUSD := math.Floor(eq * strat.MaxPositionPct); capUSD < strat.MaxPositionUSD {
				// Floored: a model that proposes exactly the advertised cap
				// must still pass the executor's exact-valued gate.
				strat.MaxPositionUSD = capUSD
			}
		}
	}
	history := bt.store.History(strat.Coin, strat.Timeframe, bt.historyBars)
	var recent []string
	if bt.journal != nil {
		recent = bt.journal.RecentSummaries(strat.Coin, 5)
	}
	var th *metrics.Thesis
	if bt.theses != nil {
		if t, ok := bt.theses.Get(strat.Coin); ok {
			th = &t
		}
	}
	return metrics.Digest{
		Coin:          strat.Coin,
		Timeframe:     strat.Timeframe,
		At:            current.CloseTime,
		Current:       current,
		History:       history,
		Thesis:        th,
		Position:      bt.store.Position(strat.Coin),
		StrategyCfg:   strat,
		RecentJournal: recent,
	}
}
