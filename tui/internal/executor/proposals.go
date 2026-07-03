package executor

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// Proposal is a pending propose-mode candidate awaiting human confirmation.
// It is the shared unit that both Telegram's inline buttons and the API's
// approve/reject endpoints resolve against — one confirm flow, two surfaces.
type Proposal struct {
	ID      string           `json:"id"`
	Verdict reasoner.Verdict `json:"verdict"`
	Created time.Time        `json:"created"`
	Expires time.Time        `json:"expires"`
}

// ProposalRegistry holds pending proposals with a TTL. Entries past their
// Expires time are treated as absent by List and Take — expiry is checked
// lazily on read rather than by a background sweeper, which is enough at this
// scale (a handful of pending candidates at a time).
type ProposalRegistry struct {
	ttl time.Duration
	now func() time.Time // injected for tests; defaults to time.Now

	mu    sync.Mutex
	items map[string]Proposal
}

// NewProposalRegistry builds a registry. ttl<=0 defaults to 15 minutes.
func NewProposalRegistry(ttl time.Duration) *ProposalRegistry {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &ProposalRegistry{
		ttl:   ttl,
		now:   time.Now,
		items: make(map[string]Proposal),
	}
}

// Add registers a verdict under a fresh id and returns the resulting Proposal.
func (r *ProposalRegistry) Add(v reasoner.Verdict) Proposal {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	p := Proposal{
		ID:      newProposalID(),
		Verdict: v,
		Created: now,
		Expires: now.Add(r.ttl),
	}
	r.items[p.ID] = p
	return p
}

// List returns unexpired proposals, newest first. Expired entries are dropped
// as a side effect.
func (r *ProposalRegistry) List() []Proposal {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	out := make([]Proposal, 0, len(r.items))
	for id, p := range r.items {
		if now.After(p.Expires) {
			delete(r.items, id)
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

// Take removes and returns the proposal for id. ok is false if id is unknown
// or the proposal has expired.
func (r *ProposalRegistry) Take(id string) (Proposal, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.items[id]
	if !ok {
		return Proposal{}, false
	}
	delete(r.items, id)
	if r.now().After(p.Expires) {
		return Proposal{}, false
	}
	return p, true
}

// newProposalID returns 8 random hex bytes (16 hex chars) as a proposal id.
func newProposalID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
