// Package store is the two-tier persistence layer from the plan: fixed-size ring
// buffers for live bars (O(1) append, bounded memory) plus append-only on-disk
// history so the reasoner gets a large historical sample, not just RAM contents.
//
// The store is the single source of truth the TUI renders from and the batcher
// freezes digests from. It is concurrency-safe: the aggregator writes, many
// readers (TUI, batcher, chat) read.
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// ring is a fixed-capacity circular buffer of bars for one (asset, timeframe).
type ring struct {
	buf  []metrics.Bar
	head int // index of next write
	size int // number of valid entries
	cap  int
}

func newRing(capacity int) *ring {
	if capacity < 1 {
		capacity = 1
	}
	return &ring{buf: make([]metrics.Bar, capacity), cap: capacity}
}

// push appends a bar. If a bar with the same OpenTime already sits at the head's
// previous slot, it is overwritten (bar updates in place until it finalizes).
func (r *ring) push(b metrics.Bar) {
	if r.size > 0 {
		last := (r.head - 1 + r.cap) % r.cap
		if r.buf[last].OpenTime.Equal(b.OpenTime) {
			r.buf[last] = b
			return
		}
	}
	r.buf[r.head] = b
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// last returns the most recent bar and whether one exists.
func (r *ring) last() (metrics.Bar, bool) {
	if r.size == 0 {
		return metrics.Bar{}, false
	}
	return r.buf[(r.head-1+r.cap)%r.cap], true
}

// slice returns up to n most-recent bars, oldest-first.
func (r *ring) slice(n int) []metrics.Bar {
	if n <= 0 || n > r.size {
		n = r.size
	}
	out := make([]metrics.Bar, n)
	start := (r.head - n + r.cap) % r.cap
	for i := 0; i < n; i++ {
		out[i] = r.buf[(start+i)%r.cap]
	}
	return out
}

func key(coin, tf string) string { return coin + "|" + tf }

// Store holds all rings plus the on-disk history root.
type Store struct {
	mu        sync.RWMutex
	rings     map[string]*ring
	latestCtx map[string]metrics.AssetCtx // coin -> latest perp context
	mids      map[string]float64          // coin -> latest mid (allMids feed)
	positions map[string]metrics.Position // coin -> open position
	// accountValue is the venue-reported account equity (USD), fed by the
	// account poller; 0 means "no snapshot yet", which the capital-relative
	// risk gates treat as unknown and refuse to size against.
	accountValue float64
	ringCap      int
	dir          string
}

// New creates a store with the given ring capacity and on-disk history dir.
func New(dir string, ringCap int) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("store: mkdir %s: %w", dir, err)
	}
	return &Store{
		rings:     make(map[string]*ring),
		latestCtx: make(map[string]metrics.AssetCtx),
		mids:      make(map[string]float64),
		positions: make(map[string]metrics.Position),
		ringCap:   ringCap,
		dir:       dir,
	}, nil
}

// PutBar stores a (possibly in-progress) bar into the live ring and, when the
// bar is finalized, appends it to the on-disk history for the day.
func (s *Store) PutBar(b metrics.Bar) {
	s.mu.Lock()
	k := key(b.Coin, b.Timeframe)
	r, ok := s.rings[k]
	if !ok {
		r = newRing(s.ringCap)
		s.rings[k] = r
	}
	r.push(b)
	s.mu.Unlock()
}

// AppendHistory writes a finalized bar to the append-only NDJSON history file:
// one file per asset per timeframe per day. Best-effort; errors are returned for
// the caller to log.
func (s *Store) AppendHistory(b metrics.Bar) error {
	day := b.CloseTime.UTC().Format("2006-01-02")
	dir := filepath.Join(s.dir, "bars", b.Coin)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.ndjson", b.Timeframe, day))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(b)
}

// LoadHistory reads up to n most-recent finalized bars for (coin, tf) from disk,
// scanning day files backwards. Used to warm the ring at startup.
func (s *Store) LoadHistory(coin, tf string, n int) ([]metrics.Bar, error) {
	dir := filepath.Join(s.dir, "bars", coin)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	prefix := tf + "_"
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > len(prefix) && e.Name()[:len(prefix)] == prefix {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files) // chronological by filename date
	var bars []metrics.Bar
	for _, name := range files {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var b metrics.Bar
			if json.Unmarshal(sc.Bytes(), &b) == nil {
				bars = append(bars, b)
			}
		}
		f.Close()
	}
	if n > 0 && len(bars) > n {
		bars = bars[len(bars)-n:]
	}
	return bars, nil
}

// WarmRing seeds the live ring for (coin, tf) from a slice of historical bars.
func (s *Store) WarmRing(coin, tf string, bars []metrics.Bar) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(coin, tf)
	r, ok := s.rings[k]
	if !ok {
		r = newRing(s.ringCap)
		s.rings[k] = r
	}
	for _, b := range bars {
		b.Coin, b.Timeframe = coin, tf
		r.push(b)
	}
}

// LatestBar returns the most recent bar for (coin, tf).
func (s *Store) LatestBar(coin, tf string) (metrics.Bar, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rings[key(coin, tf)]
	if !ok {
		return metrics.Bar{}, false
	}
	return r.last()
}

// History returns up to n recent bars for (coin, tf), oldest-first.
func (s *Store) History(coin, tf string, n int) []metrics.Bar {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rings[key(coin, tf)]
	if !ok {
		return nil
	}
	return r.slice(n)
}

// PutMids merges a mid-price snapshot from the allMids feed.
func (s *Store) PutMids(m metrics.MidSnapshot) {
	s.mu.Lock()
	for coin, px := range m.Mids {
		s.mids[coin] = px
	}
	s.mu.Unlock()
}

// Mid returns the latest mid price for a coin (0 if unknown).
func (s *Store) Mid(coin string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mids[coin]
}

// PutAssetCtx records the latest perp context for an asset.
func (s *Store) PutAssetCtx(c metrics.AssetCtx) {
	s.mu.Lock()
	s.latestCtx[c.Coin] = c
	s.mu.Unlock()
}

// AssetCtx returns the latest perp context for an asset.
func (s *Store) AssetCtx(coin string) (metrics.AssetCtx, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.latestCtx[coin]
	return c, ok
}

// SetAccount replaces the account snapshot wholesale: equity plus the full open
// position set, as reported by the venue. The account poller is the single
// writer; wholesale replacement is what clears positions closed off-daemon.
func (s *Store) SetAccount(equity float64, positions []metrics.Position) {
	s.mu.Lock()
	s.accountValue = equity
	s.positions = make(map[string]metrics.Position, len(positions))
	for _, p := range positions {
		if !p.IsFlat() {
			s.positions[p.Coin] = p
		}
	}
	s.mu.Unlock()
}

// AccountValue returns the last account equity reported by the venue; 0 until
// the first poll lands (the capital-relative gates treat 0 as "unknown").
func (s *Store) AccountValue() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accountValue
}

// PutPosition records (or clears) the open position for an asset.
func (s *Store) PutPosition(p metrics.Position) {
	s.mu.Lock()
	if p.IsFlat() {
		delete(s.positions, p.Coin)
	} else {
		s.positions[p.Coin] = p
	}
	s.mu.Unlock()
}

// Position returns the open position for an asset (zero value if flat).
func (s *Store) Position(coin string) metrics.Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.positions[coin]
}

// Positions returns a snapshot of all open positions.
func (s *Store) Positions() []metrics.Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]metrics.Position, 0, len(s.positions))
	for _, p := range s.positions {
		out = append(out, p)
	}
	return out
}
