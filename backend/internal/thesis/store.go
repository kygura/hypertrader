// The thesis store is the persisted directional memory of the agent: one live
// Thesis per coin, survives restarts, and is the executor's authorization
// source for trigger-path trades. State changes are written through to disk
// immediately (one JSON file per coin under data/theses/), journaled, and
// mirrored to the bus so every frontend sees thesis updates live.
package thesis

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// Thesis is re-exported from metrics (the dependency-free domain layer) the
// same way reasoner aliases Verdict — the bus and executor reference the type
// without importing this package, and this package owns its lifecycle.
type Thesis = metrics.Thesis

// Store holds the live thesis per coin behind an RWMutex: the reasoner writes
// on reviews, many readers (gate, batcher, executor, API) read on every bar.
type Store struct {
	bus     *bus.Bus         // nil-tolerant: tests may run without a bus
	journal *journal.Journal // nil-tolerant: tests may run without a journal
	dir     string           // data/theses root

	mu     sync.RWMutex
	theses map[string]Thesis // coin -> live thesis
}

// NewStore creates the store rooted at dataDir/theses and loads any theses
// persisted by a previous run, so the agent wakes up with its views intact.
func NewStore(b *bus.Bus, jr *journal.Journal, dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "theses")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("thesis: mkdir %s: %w", dir, err)
	}
	s := &Store{bus: b, journal: jr, dir: dir, theses: make(map[string]Thesis)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads every per-coin JSON file. A corrupt file is skipped rather than
// failing startup — one bad thesis shouldn't blind the agent to the rest.
func (s *Store) load() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("thesis: read dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var t Thesis
		if json.Unmarshal(raw, &t) != nil || t.Coin == "" || t.Version < 1 {
			continue
		}
		s.theses[t.Coin] = t
	}
	return nil
}

// Get returns the live thesis for a coin. ok is false when the coin has no
// live thesis — never reviewed, or invalidated (distinct from a "neutral"
// thesis, which Get returns like any other).
func (s *Store) Get(coin string) (Thesis, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.theses[coin]
	return t, ok
}

// All returns a snapshot of every live thesis, sorted by coin — the
// /api/theses cold-start payload.
func (s *Store) All() []Thesis {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Thesis, 0, len(s.theses))
	for _, t := range s.theses {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Coin < out[j].Coin })
	return out
}

// Upsert creates or updates the live thesis for t.Coin from the model-supplied
// fields (direction, summary, invalidation, targets, horizon, confidence).
// The store owns the lifecycle fields: a new thesis starts at Version 1 with
// CreatedAt=ReviewedAt=now; an existing one keeps CreatedAt, bumps Version,
// and stamps ReviewedAt. The result is written through to disk, journaled,
// and published before returning.
func (s *Store) Upsert(t Thesis) (Thesis, error) {
	if t.Coin == "" || t.Coin != filepath.Base(t.Coin) {
		return Thesis{}, fmt.Errorf("thesis: invalid coin %q", t.Coin)
	}
	now := time.Now().UTC()

	s.mu.Lock()
	prev, existed := s.theses[t.Coin]
	if existed {
		t.CreatedAt = prev.CreatedAt
		t.Version = prev.Version + 1
	} else {
		t.CreatedAt = now
		t.Version = 1
	}
	t.ReviewedAt = now
	s.theses[t.Coin] = t
	err := s.writeThrough(t)
	s.mu.Unlock()

	op := "created"
	if existed {
		op = "updated"
	}
	s.record(t.Coin, fmt.Sprintf("thesis %s: %s v%d inv %.4f conf %.2f horizon %s — %s",
		op, t.Direction, t.Version, t.Invalidation, t.Confidence, t.Horizon, t.Summary))
	if s.bus != nil {
		s.bus.PublishThesis(t)
	}
	return t, err
}

// Invalidate removes the live thesis for a coin, deletes its file, journals
// the invalidation, and publishes a Version-0 tombstone so live consumers see
// the coin drop to no-thesis. Returns false when there was nothing to remove.
func (s *Store) Invalidate(coin string) bool {
	s.mu.Lock()
	prev, ok := s.theses[coin]
	if ok {
		delete(s.theses, coin)
		_ = os.Remove(s.path(coin))
	}
	s.mu.Unlock()
	if !ok {
		return false
	}

	s.record(coin, fmt.Sprintf("thesis invalidated: was %s v%d — %s", prev.Direction, prev.Version, prev.Summary))
	if s.bus != nil {
		// Version 0 is the "no live thesis" sentinel (see metrics.Thesis).
		s.bus.PublishThesis(Thesis{Coin: coin, ReviewedAt: time.Now().UTC()})
	}
	return true
}

func (s *Store) path(coin string) string {
	return filepath.Join(s.dir, coin+".json")
}

// writeThrough persists one thesis atomically (temp file + rename), matching
// config.Save's pattern: a crash mid-write can never truncate a thesis file.
// Called with s.mu held so the file always reflects the map's latest state.
func (s *Store) writeThrough(t Thesis) error {
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("thesis: encode %s: %w", t.Coin, err)
	}
	tmp := s.path(t.Coin) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("thesis: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path(t.Coin)); err != nil {
		return fmt.Errorf("thesis: rename %s: %w", t.Coin, err)
	}
	return nil
}

// record journals a thesis lifecycle change, best-effort. The journal mirrors
// to the bus itself, so the entry reaches the TUI's journal panel too.
func (s *Store) record(coin, summary string) {
	if s.journal == nil {
		return
	}
	_ = s.journal.Record(journal.Entry{Coin: coin, Kind: "thesis", Summary: summary})
}
