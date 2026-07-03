// Package gate is the deterministic filter deciding which assets in a batch earn
// LLM tokens. Sane default is permissive — it passes every tracked asset every
// batch (the LLM reads every digest and decides autonomously). The gate exists
// so cost or noise can be tightened per-asset later, with an off switch.
package gate

import (
	"context"
	"math"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// Rules configures the deterministic pass conditions. With Permissive=true the
// gate forwards everything (the documented default).
type Rules struct {
	Permissive bool

	// When not permissive, a digest passes if ANY enabled rule fires.
	ZScoreReturn   float64 // |return z-score| over history exceeds this
	FundingAbs     float64 // |funding| exceeds this
	OIDeltaAbs     float64 // |OI delta| fraction exceeds this
	PositionAlways bool    // always pass assets with an open position
}

// DefaultRules returns the permissive default.
func DefaultRules() Rules { return Rules{Permissive: true, PositionAlways: true} }

// Gate forwards passing digests from the digest topic onward to the reasoner.
type Gate struct {
	bus   *bus.Bus
	rules Rules
	out   chan metrics.Digest
}

// New builds a gate. The reasoner subscribes via Out().
func New(b *bus.Bus, rules Rules) *Gate {
	return &Gate{bus: b, rules: rules, out: make(chan metrics.Digest, 256)}
}

// Out is the channel of digests that passed the gate.
func (g *Gate) Out() <-chan metrics.Digest { return g.out }

// Run consumes raw digests, applies rules, and forwards passers. Blocks.
func (g *Gate) Run(ctx context.Context) {
	digests := g.bus.SubscribeDigests(256)
	defer close(g.out)
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-digests:
			if g.pass(d) {
				select {
				case g.out <- d:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func (g *Gate) pass(d metrics.Digest) bool {
	if g.rules.Permissive {
		return true
	}
	if g.rules.PositionAlways && !d.Position.IsFlat() {
		return true
	}
	if g.rules.FundingAbs > 0 && math.Abs(d.Current.Funding) >= g.rules.FundingAbs {
		return true
	}
	if g.rules.OIDeltaAbs > 0 && math.Abs(d.Current.OIDelta) >= g.rules.OIDeltaAbs {
		return true
	}
	if g.rules.ZScoreReturn > 0 && math.Abs(returnZScore(d)) >= g.rules.ZScoreReturn {
		return true
	}
	return false
}

// returnZScore is the current bar's return measured against the mean/stddev of
// the historical bar returns.
func returnZScore(d metrics.Digest) float64 {
	if len(d.History) < 3 {
		return 0
	}
	var sum float64
	for _, b := range d.History {
		sum += b.Return
	}
	mean := sum / float64(len(d.History))
	var v float64
	for _, b := range d.History {
		v += (b.Return - mean) * (b.Return - mean)
	}
	sd := math.Sqrt(v / float64(len(d.History)-1))
	if sd == 0 {
		return 0
	}
	return (d.Current.Return - mean) / sd
}
