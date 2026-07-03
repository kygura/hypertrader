// Package journal is the append-only audit trail: one NDJSON file per day plus a
// per-position lifecycle record. It is simultaneously the audit log, the
// backtest corpus, and the memory the reasoner reads back (RecentSummaries).
// Every entry is mirrored to the bus so the TUI and Telegram both observe it.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// Entry is one journaled record.
type Entry struct {
	Time    time.Time         `json:"time"`
	Coin    string            `json:"coin"`
	Kind    string            `json:"kind"` // candidate|fill|open|close|alert|error
	Summary string            `json:"summary"`
	Verdict *reasoner.Verdict `json:"verdict,omitempty"`
}

// Journal writes entries to disk and mirrors them to the bus.
type Journal struct {
	bus *bus.Bus
	dir string

	mu     sync.Mutex
	recent map[string][]Entry // coin -> recent entries (in-memory tail)
}

// New creates a journal rooted at dir/journal.
func New(b *bus.Bus, dir string) (*Journal, error) {
	jdir := filepath.Join(dir, "journal")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		return nil, fmt.Errorf("journal: mkdir: %w", err)
	}
	return &Journal{bus: b, dir: jdir, recent: make(map[string][]Entry)}, nil
}

// Record persists an entry and mirrors it to the bus. Errors writing to disk are
// returned but the bus mirror still fires (external record independence).
func (j *Journal) Record(e Entry) error {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	j.mu.Lock()
	tail := append(j.recent[e.Coin], e)
	if len(tail) > 20 {
		tail = tail[len(tail)-20:]
	}
	j.recent[e.Coin] = tail
	j.mu.Unlock()

	j.bus.PublishJournal(bus.JournalEvent{
		Coin:    e.Coin,
		Kind:    e.Kind,
		Summary: e.Summary,
		Verdict: e.Verdict,
	})

	return j.appendFile(e)
}

func (j *Journal) appendFile(e Entry) error {
	day := e.Time.UTC().Format("2006-01-02")
	path := filepath.Join(j.dir, day+".ndjson")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(e)
}

// ReadDay reads the NDJSON journal file for one UTC calendar day (dir is the
// storage root, i.e. the same dir passed to New — this joins "journal" and
// the date itself, matching where Record writes). date must parse as
// YYYY-MM-DD; this also rules out path-traversal-shaped input reaching
// filepath.Join. Malformed lines are skipped rather than failing the whole
// read — one corrupt entry shouldn't hide a day's history. A missing file
// (nothing was ever journaled that day) returns an empty slice, not an error.
func ReadDay(dir, date string) ([]Entry, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, fmt.Errorf("journal: invalid date %q: %w", date, err)
	}
	path := filepath.Join(dir, "journal", date+".ndjson")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// RecentSummaries returns up to n recent entry summaries for a coin, oldest-first.
// This is the memory the reasoner reads back into each digest.
func (j *Journal) RecentSummaries(coin string, n int) []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	tail := j.recent[coin]
	if n > 0 && len(tail) > n {
		tail = tail[len(tail)-n:]
	}
	out := make([]string, 0, len(tail))
	for _, e := range tail {
		out = append(out, fmt.Sprintf("[%s %s] %s", e.Time.Format("01-02 15:04"), e.Kind, e.Summary))
	}
	return out
}
