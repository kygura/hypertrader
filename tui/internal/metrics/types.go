// Package metrics defines the core domain types shared across the system: the
// OHLCV bar, the per-bar perp-regime metrics, and the digest the reasoner reads.
//
// These types are deliberately dependency-free so every other package (ingestor,
// aggregator, store, batcher, reasoner, tui) can import them without creating
// import cycles. They are the lingua franca that flows over the event bus.
package metrics

import "time"

// Side is the aggressor side of a trade.
type Side int

const (
	SideNone Side = iota
	SideBuy
	SideSell
)

func (s Side) String() string {
	switch s {
	case SideBuy:
		return "buy"
	case SideSell:
		return "sell"
	default:
		return "none"
	}
}

// Trade is a single executed print from the trades feed.
type Trade struct {
	Coin     string
	Price    float64
	Size     float64 // base-asset size
	Side     Side    // aggressor side
	Time     time.Time
	RecvTime time.Time // monotonic receive time stamped by the ingestor
}

// BookLevel is one price level of the L2 order book.
type BookLevel struct {
	Price float64
	Size  float64
}

// Book is an L2 order book snapshot.
type Book struct {
	Coin     string
	Bids     []BookLevel
	Asks     []BookLevel
	Time     time.Time
	RecvTime time.Time
}

// Mid returns the mid price; zero if either side is empty.
func (b Book) Mid() float64 {
	if len(b.Bids) == 0 || len(b.Asks) == 0 {
		return 0
	}
	return (b.Bids[0].Price + b.Asks[0].Price) / 2
}

// AssetCtx carries the perp-context fields from the activeAssetCtx / webData2
// feeds: funding, open interest, mark/oracle price, and premium.
type AssetCtx struct {
	Coin         string
	MarkPrice    float64
	OraclePrice  float64
	Funding      float64 // current funding rate (per hour, as a fraction)
	OpenInterest float64 // in base-asset units
	Premium      float64 // (mark - oracle) / oracle
	DayVolume    float64
	Time         time.Time
}

// Basis returns the percentage basis (mark vs oracle).
func (c AssetCtx) Basis() float64 {
	if c.OraclePrice == 0 {
		return 0
	}
	return (c.MarkPrice - c.OraclePrice) / c.OraclePrice
}

// MidSnapshot carries the allMids feed: every coin's current mid price in one
// frame. It is the cheapest live price for assets that have not printed a trade
// yet, so the TUI can show a price for the whole watchlist immediately.
type MidSnapshot struct {
	Mids map[string]float64
	Time time.Time
}

// Bar is a finalized OHLCV candle plus the derived perp-regime metrics for one
// asset at one timeframe. Pure data — the aggregator fills it, everyone reads it.
type Bar struct {
	Coin      string
	Timeframe string
	OpenTime  time.Time
	CloseTime time.Time
	Final     bool // true once the bucket closed; false for in-progress live bars

	// Price / structure.
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64 // base-asset volume in the bar

	// Flow.
	BuyVolume  float64 // aggressor-buy base volume
	SellVolume float64 // aggressor-sell base volume
	TradeCount int
	LargePrint bool    // a print exceeded the large-print threshold
	CVD        float64 // cumulative volume delta at bar close (running)
	TradeImbal float64 // (buy - sell) / (buy + sell) within the bar

	// Perp regime (snapshot at bar close).
	Funding      float64
	FundingDelta float64 // change vs previous bar
	OpenInterest float64
	OIDelta      float64 // change vs previous bar (fraction)
	Basis        float64
	MarkPrice    float64

	// Derived structure.
	Return      float64 // close-to-close return vs previous bar
	RealizedVol float64 // stddev of intra-bar returns (rough)
	RangePos    float64 // where close sits in [low,high]: 0..1
	LiqProx     float64 // proxy for liquidation proximity (0..1, higher = closer)

	// Cross-asset.
	BTCCorr     float64 // rolling correlation of returns vs BTC
	RelStrength float64 // return minus basket-average return
}

// IsBullish reports whether the bar closed up.
func (b Bar) IsBullish() bool { return b.Close >= b.Open }

// Position is the live state of an open position for an asset.
type Position struct {
	Coin       string
	Size       float64 // signed: positive long, negative short
	EntryPrice float64
	MarkPrice  float64
	UnrealPnl  float64
	OpenedAt   time.Time
}

// IsLong reports the position direction.
func (p Position) IsLong() bool  { return p.Size > 0 }
func (p Position) IsShort() bool { return p.Size < 0 }
func (p Position) IsFlat() bool  { return p.Size == 0 }

// Digest is the per-asset snapshot the batcher freezes on each timeframe close
// and hands to the reasoner. It is rich enough to reason about perp mechanics:
// current metrics, a compact historical series, open-position state, and config.
type Digest struct {
	Coin      string
	Timeframe string
	At        time.Time

	Current Bar   // most recent finalized bar
	History []Bar // last N bars, oldest-first, for regime context

	Position    Position
	StrategyCfg AssetStrategy

	// RecentJournal holds compact summaries of the latest journal entries for
	// this asset so the reasoner has memory of its own prior theses.
	RecentJournal []string
}

// AssetStrategy is the per-asset configuration the reasoner sees: the timeframe,
// whether confirmation is required before execution, and any risk overrides.
type AssetStrategy struct {
	Coin                 string
	Timeframe            string
	RequiresConfirmation bool
	MaxPositionUSD       float64
}
