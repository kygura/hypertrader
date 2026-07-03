// Package batcher freezes a per-asset digest on each timeframe boundary and
// hands it (via the gate) to the reasoner. Short-timeframe assets batch often;
// 4h-swing assets rarely — that cadence difference is the context-window
// economizer the plan calls for.
//
// The batcher listens for finalized bars on the bus. A finalized bar arriving
// for (coin, tf) means that timeframe just closed, so it is the natural trigger
// to assemble a digest from the store.
package batcher

import (
	"context"
	"sort"
	"sync"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// Batcher assembles digests on timeframe closes.
type Batcher struct {
	bus         *bus.Bus
	store       *store.Store
	journal     *journal.Journal
	historyBars int

	mu         sync.RWMutex
	strategies map[string]metrics.AssetStrategy // coin -> strategy (the tracked set)
}

// New builds a batcher. strategies maps tracked coins to their per-asset config;
// only coins present here are batched (the tracked subset). The set is mutable at
// runtime via Track/Untrack so the TUI's /track command takes effect live.
func New(b *bus.Bus, s *store.Store, j *journal.Journal, strategies map[string]metrics.AssetStrategy, historyBars int) *Batcher {
	if strategies == nil {
		strategies = map[string]metrics.AssetStrategy{}
	}
	return &Batcher{
		bus:         b,
		store:       s,
		journal:     j,
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

// Run watches for finalized bars and emits digests for tracked assets when their
// configured timeframe closes. It blocks until ctx is cancelled.
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
		return // not a tracked asset, or not its decision timeframe
	}
	bt.bus.PublishDigest(bt.buildDigest(strat, bar))
}

// Scan publishes a digest per named tracked coin — all tracked coins when none
// are named — from the store's current state. This is the on-demand synthesis
// path: where onBar waits for a timeframe close, Scan reads the tape now, so
// the reasoner can rank the whole watchlist the moment the operator asks
// instead of up to a full timeframe later. Untracked names and coins whose
// rings are still cold are skipped.
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
		bt.bus.PublishDigest(bt.buildDigest(strat, bar))
	}
}

// buildDigest assembles the full per-asset digest from the store.
func (bt *Batcher) buildDigest(strat metrics.AssetStrategy, current metrics.Bar) metrics.Digest {
	history := bt.store.History(strat.Coin, strat.Timeframe, bt.historyBars)
	var recent []string
	if bt.journal != nil {
		recent = bt.journal.RecentSummaries(strat.Coin, 5)
	}
	return metrics.Digest{
		Coin:          strat.Coin,
		Timeframe:     strat.Timeframe,
		At:            current.CloseTime,
		Current:       current,
		History:       history,
		Position:      bt.store.Position(strat.Coin),
		StrategyCfg:   strat,
		RecentJournal: recent,
	}
}
