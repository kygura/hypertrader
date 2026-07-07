package apiclient

import (
	"sort"
	"sync"
)

// Cache is a client-side mirror of backend/internal/store.Store's read
// surface: same method names/signatures, fed over HTTP+WS instead of
// in-process. Model's render code calls these exactly as it called
// store.Store directly when the TUI shared a process with the daemon.
type Cache struct {
	mu     sync.RWMutex
	bars   map[string]map[string][]Bar // coin -> tf -> bars, oldest-first
	mids   map[string]float64
	ctxs   map[string]AssetCtx
	pos    map[string]Position
	theses map[string]Thesis
}

func NewCache() *Cache {
	return &Cache{
		bars:   make(map[string]map[string][]Bar),
		mids:   make(map[string]float64),
		ctxs:   make(map[string]AssetCtx),
		pos:    make(map[string]Position),
		theses: make(map[string]Thesis),
	}
}

func (c *Cache) LatestBar(coin, tf string) (Bar, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	series := c.bars[coin][tf]
	if len(series) == 0 {
		return Bar{}, false
	}
	return series[len(series)-1], true
}

func (c *Cache) History(coin, tf string, n int) []Bar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	series := c.bars[coin][tf]
	if n <= 0 || n >= len(series) {
		return append([]Bar(nil), series...)
	}
	return append([]Bar(nil), series[len(series)-n:]...)
}

func (c *Cache) Mid(coin string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mids[coin]
}

func (c *Cache) AssetCtx(coin string) (AssetCtx, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ctx, ok := c.ctxs[coin]
	return ctx, ok
}

func (c *Cache) Position(coin string) Position {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pos[coin]
}

// Thesis returns coin's latest cached thesis, if one has been seen.
func (c *Cache) Thesis(coin string) (Thesis, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.theses[coin]
	return t, ok
}

// Theses returns every cached thesis, sorted by coin — the read surface the
// cockpit's per-asset cards render from (mirrors Position()).
func (c *Cache) Theses() []Thesis {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Thesis, 0, len(c.theses))
	for _, t := range c.theses {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Coin < out[j].Coin })
	return out
}

// PutThesis stores/replaces coin's thesis — the WS "thesis" event path.
func (c *Cache) PutThesis(t Thesis) {
	c.mu.Lock()
	c.theses[t.Coin] = t
	c.mu.Unlock()
}

// DropThesis removes coin's thesis — the WS invalidation-tombstone path
// (a Version-0 event). Without this the card would linger as an empty-direction
// ghost until the next reconnect re-seeded the snapshot.
func (c *Cache) DropThesis(coin string) {
	c.mu.Lock()
	delete(c.theses, coin)
	c.mu.Unlock()
}

// ApplyTheses replaces the whole thesis set from a GET /api/theses snapshot —
// the on-connect cold-start path. The snapshot is authoritative: theses the
// daemon no longer holds are dropped.
func (c *Cache) ApplyTheses(ts []Thesis) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.theses = make(map[string]Thesis, len(ts))
	for _, t := range ts {
		c.theses[t.Coin] = t
	}
}

// PutBar appends/replaces a bar in its (coin, tf) series, keyed by CloseTime —
// a re-published in-progress bar for the same close replaces the prior entry
// rather than duplicating it, mirroring store.Store.PutBar's ring semantics.
func (c *Cache) PutBar(b Bar) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bars[b.Coin] == nil {
		c.bars[b.Coin] = make(map[string][]Bar)
	}
	series := c.bars[b.Coin][b.Timeframe]
	if n := len(series); n > 0 && series[n-1].CloseTime.Equal(b.CloseTime) {
		series[n-1] = b
	} else {
		series = append(series, b)
		if len(series) > 512 { // matches backend's default ring_size
			series = series[len(series)-512:]
		}
	}
	c.bars[b.Coin][b.Timeframe] = series
}

func (c *Cache) PutMid(coin string, px float64) {
	c.mu.Lock()
	c.mids[coin] = px
	c.mu.Unlock()
}

// ApplyMarkets overwrites AssetCtx/Position (and seeds Mid) from a full
// GET /api/markets snapshot — the periodic-poll refresh path.
func (c *Cache) ApplyMarkets(entries []MarketEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range entries {
		c.ctxs[e.Coin] = e.AssetCtx
		c.pos[e.Coin] = e.Position
		if e.Mid != 0 {
			c.mids[e.Coin] = e.Mid
		}
	}
}

// SeedHistory installs a full backfilled series for (coin, tf), oldest-first —
// called once after a successful GET /api/bars/{coin} on /watch.
func (c *Cache) SeedHistory(coin, tf string, bars []Bar) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bars[coin] == nil {
		c.bars[coin] = make(map[string][]Bar)
	}
	c.bars[coin][tf] = append([]Bar(nil), bars...)
}
