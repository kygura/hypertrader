// Package aggregator folds the live trade and perp-context streams into OHLCV
// bars plus derived perp-regime metrics, per asset, at multiple timeframes
// simultaneously off one input. Each timeframe close emits a finalized bar on
// the bus and persists it to history.
//
// The metric set is the moat: price/structure, flow (CVD, trade imbalance),
// perp regime (funding trajectory, OI delta, basis, liquidation proximity), and
// cross-asset (BTC correlation, relative strength).
package aggregator

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// Timeframe maps a name to its duration.
type Timeframe struct {
	Name string
	Dur  time.Duration
}

// ParseTimeframe converts HL-style notation to a Timeframe.
func ParseTimeframe(name string) (Timeframe, bool) {
	durs := map[string]time.Duration{
		"1m": time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute,
		"30m": 30 * time.Minute, "1h": time.Hour, "4h": 4 * time.Hour,
		"1d": 24 * time.Hour, "1w": 7 * 24 * time.Hour,
	}
	d, ok := durs[name]
	return Timeframe{Name: name, Dur: d}, ok
}

// bucketStart returns the aligned open time of the bucket containing t.
func bucketStart(t time.Time, d time.Duration) time.Time {
	return t.Truncate(d)
}

// liveBar accumulates trades within one timeframe bucket before finalizing.
type liveBar struct {
	bar      metrics.Bar
	rets     []float64 // intra-bar tick returns for realized-vol estimate
	lastTick float64
	prevBar  *metrics.Bar // previous finalized bar (for deltas / returns)
}

// assetState tracks, per asset, one liveBar per timeframe plus latest context
// and a running CVD.
type assetState struct {
	bars map[string]*liveBar // timeframe name -> live bar
	ctx  metrics.AssetCtx
	cvd  float64
}

// Aggregator consumes trades + contexts and produces bars across timeframes.
type Aggregator struct {
	bus        *bus.Bus
	store      *store.Store
	timeframes map[string][]Timeframe // coin -> timeframes to build
	defaultTFs []Timeframe            // used for coins added at runtime
	largePrint float64                // base-size threshold for large-print flag

	mu     sync.Mutex
	assets map[string]*assetState

	// recent close-to-close returns per coin, for cross-asset correlation.
	retHist map[string][]float64
}

// defaultTimeframeSet is folded for any coin lacking an explicit configuration —
// e.g. one added at runtime via the TUI's /watch command. It matches the full
// fold set main.go configures: LTF rungs for the deviation gate, HTF rungs for
// the thesis-review ladder.
func defaultTimeframeSet() []Timeframe {
	var tfs []Timeframe
	for _, name := range []string{"1m", "5m", "15m", "1h", "4h", "1d", "1w"} {
		if tf, ok := ParseTimeframe(name); ok {
			tfs = append(tfs, tf)
		}
	}
	return tfs
}

// tfsFor returns the timeframes to fold for a coin, defaulting when unconfigured.
func (a *Aggregator) tfsFor(coin string) []Timeframe {
	if tfs := a.timeframes[coin]; len(tfs) > 0 {
		return tfs
	}
	return a.defaultTFs
}

// New builds an aggregator. tfByCoin gives which timeframes to fold per asset.
func New(b *bus.Bus, s *store.Store, tfByCoin map[string][]Timeframe, largePrint float64) *Aggregator {
	if largePrint <= 0 {
		largePrint = 1e6 // notional fallback; treated as base*price below
	}
	return &Aggregator{
		bus:        b,
		store:      s,
		timeframes: tfByCoin,
		defaultTFs: defaultTimeframeSet(),
		largePrint: largePrint,
		assets:     make(map[string]*assetState),
		retHist:    make(map[string][]float64),
	}
}

func (a *Aggregator) state(coin string) *assetState {
	st, ok := a.assets[coin]
	if !ok {
		st = &assetState{bars: make(map[string]*liveBar)}
		a.assets[coin] = st
	}
	return st
}

// Run consumes the trade and asset-context feeds plus a ticker that finalizes
// elapsed buckets. It blocks until ctx is cancelled.
func (a *Aggregator) Run(ctx context.Context) {
	trades := a.bus.SubscribeTrades(4096)
	ctxs := a.bus.SubscribeAssetCtxs(1024)
	mids := a.bus.SubscribeMids(256)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-trades:
			a.onTrade(t)
		case c := <-ctxs:
			a.onCtx(c)
		case m := <-mids:
			a.store.PutMids(m)
		case now := <-tick.C:
			a.finalizeElapsed(now)
		}
	}
}

func (a *Aggregator) onCtx(c metrics.AssetCtx) {
	a.mu.Lock()
	a.state(c.Coin).ctx = c
	a.mu.Unlock()
	a.store.PutAssetCtx(c)
}

func (a *Aggregator) onTrade(t metrics.Trade) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.state(t.Coin)

	// Running CVD across the asset's lifetime.
	signed := t.Size
	if t.Side == metrics.SideSell {
		signed = -t.Size
	}
	st.cvd += signed

	for _, tf := range a.tfsFor(t.Coin) {
		lb := st.bars[tf.Name]
		open := bucketStart(t.Time, tf.Dur)
		if lb == nil || !lb.bar.OpenTime.Equal(open) {
			// Bucket rolled: finalize the old one, start a new bar.
			if lb != nil {
				a.finalize(t.Coin, tf, lb)
			}
			lb = a.newLiveBar(t.Coin, tf, open, st)
			st.bars[tf.Name] = lb
		}
		applyTrade(lb, t, signed, st.cvd, a.largePrint)
		// Push the in-progress bar so the TUI sees live updates.
		a.store.PutBar(lb.bar)
		a.bus.PublishBar(lb.bar)
	}
}

func (a *Aggregator) newLiveBar(coin string, tf Timeframe, open time.Time, st *assetState) *liveBar {
	lb := &liveBar{
		bar: metrics.Bar{
			Coin:      coin,
			Timeframe: tf.Name,
			OpenTime:  open,
			CloseTime: open.Add(tf.Dur),
		},
	}
	if prev, ok := a.store.LatestBar(coin, tf.Name); ok {
		p := prev
		lb.prevBar = &p
	}
	return lb
}

// applyTrade folds a single trade into the live bar.
func applyTrade(lb *liveBar, t metrics.Trade, signed, cvd, largePrint float64) {
	b := &lb.bar
	if b.TradeCount == 0 {
		b.Open, b.High, b.Low = t.Price, t.Price, t.Price
		lb.lastTick = t.Price
	}
	if t.Price > b.High {
		b.High = t.Price
	}
	if t.Price < b.Low {
		b.Low = t.Price
	}
	b.Close = t.Price
	b.Volume += t.Size
	b.TradeCount++
	if t.Side == metrics.SideBuy {
		b.BuyVolume += t.Size
	} else if t.Side == metrics.SideSell {
		b.SellVolume += t.Size
	}
	if t.Size*t.Price >= largePrint {
		b.LargePrint = true
	}
	b.CVD = cvd
	if denom := b.BuyVolume + b.SellVolume; denom > 0 {
		b.TradeImbal = (b.BuyVolume - b.SellVolume) / denom
	}
	if lb.lastTick > 0 {
		lb.rets = append(lb.rets, (t.Price-lb.lastTick)/lb.lastTick)
	}
	lb.lastTick = t.Price
}

// finalizeElapsed closes any live bar whose bucket has ended.
func (a *Aggregator) finalizeElapsed(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for coin, st := range a.assets {
		for _, tf := range a.tfsFor(coin) {
			lb := st.bars[tf.Name]
			if lb != nil && !now.Before(lb.bar.CloseTime) && lb.bar.TradeCount > 0 {
				a.finalize(coin, tf, lb)
				delete(st.bars, tf.Name)
			}
		}
	}
}

// finalize computes derived metrics, persists, and publishes a finalized bar.
func (a *Aggregator) finalize(coin string, tf Timeframe, lb *liveBar) {
	b := lb.bar
	b.Final = true
	st := a.assets[coin]

	// Perp regime snapshot at close.
	b.Funding = st.ctx.Funding
	b.OpenInterest = st.ctx.OpenInterest
	b.Basis = st.ctx.Basis()
	b.MarkPrice = st.ctx.MarkPrice

	// Deltas vs previous bar.
	if lb.prevBar != nil {
		p := lb.prevBar
		if p.Close > 0 {
			b.Return = (b.Close - p.Close) / p.Close
		}
		b.FundingDelta = b.Funding - p.Funding
		if p.OpenInterest > 0 {
			b.OIDelta = (b.OpenInterest - p.OpenInterest) / p.OpenInterest
		}
	}

	// Structure.
	if rng := b.High - b.Low; rng > 0 {
		b.RangePos = (b.Close - b.Low) / rng
	} else {
		b.RangePos = 0.5
	}
	b.RealizedVol = stddev(lb.rets)
	b.LiqProx = liqProximity(b, st.ctx)

	// Cross-asset: append this bar's return to history; compute BTC corr + RS.
	a.retHist[coin] = appendCap(a.retHist[coin], b.Return, 64)
	b.BTCCorr = correlation(a.retHist[coin], a.retHist["BTC"])
	b.RelStrength = b.Return - basketAvgReturn(a.retHist)

	a.store.PutBar(b)
	if err := a.store.AppendHistory(b); err != nil {
		a.bus.PublishStatus(bus.StatusEvent{Detail: "history write failed: " + err.Error()})
	}
	a.bus.PublishBar(b)
}

// liqProximity is a coarse proxy: how close the mark sits to a notional
// liquidation band, inferred from realized range. Higher = closer (0..1).
func liqProximity(b metrics.Bar, c metrics.AssetCtx) float64 {
	if c.MarkPrice == 0 || b.High == b.Low {
		return 0
	}
	// Distance of close from the bar extreme in the direction of the move,
	// normalized by range — a stand-in until real liq-level data is wired.
	if b.IsBullish() {
		return clamp01((b.Close - b.Low) / (b.High - b.Low))
	}
	return clamp01((b.High - b.Close) / (b.High - b.Low))
}

// --- small numeric helpers ---

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	var v float64
	for _, x := range xs {
		v += (x - mean) * (x - mean)
	}
	return math.Sqrt(v / float64(len(xs)-1))
}

func correlation(a, b []float64) float64 {
	n := min(len(a), len(b))
	if n < 3 {
		return 0
	}
	a, b = a[len(a)-n:], b[len(b)-n:]
	var sa, sb float64
	for i := 0; i < n; i++ {
		sa += a[i]
		sb += b[i]
	}
	ma, mb := sa/float64(n), sb/float64(n)
	var cov, va, vb float64
	for i := 0; i < n; i++ {
		da, db := a[i]-ma, b[i]-mb
		cov += da * db
		va += da * da
		vb += db * db
	}
	if va == 0 || vb == 0 {
		return 0
	}
	return cov / math.Sqrt(va*vb)
}

func basketAvgReturn(hist map[string][]float64) float64 {
	var sum float64
	var n int
	for _, xs := range hist {
		if len(xs) > 0 {
			sum += xs[len(xs)-1]
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func appendCap(xs []float64, v float64, cap int) []float64 {
	xs = append(xs, v)
	if len(xs) > cap {
		xs = xs[len(xs)-cap:]
	}
	return xs
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
