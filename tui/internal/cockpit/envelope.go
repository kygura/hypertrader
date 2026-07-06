package cockpit

import "github.com/hyperagent/tui/internal/apiclient"

// envelope is the live risk-envelope utilization shown in the MANDATE and
// EXECUTION panels, computed client-side from open positions against the
// daemon's risk settings. Exposure is Σ|size × mark| over non-flat
// positions.
type envelope struct {
	ExposureUSD float64
	OpenCount   int
	UPnL        float64
	Risk        apiclient.RiskSettings
}

func computeEnvelope(positions []apiclient.Position, risk apiclient.RiskSettings) envelope {
	env := envelope{Risk: risk}
	for _, p := range positions {
		if p.IsFlat() {
			continue
		}
		notional := p.Size * p.MarkPrice
		if notional < 0 {
			notional = -notional
		}
		env.ExposureUSD += notional
		env.OpenCount++
		env.UPnL += p.UnrealPnl
	}
	return env
}

// gateStates is the compiled pass/fail state of the risk gates, computed
// once so every panel (MANDATE, EXECUTION) renders the same truth. A zero
// cap on MaxPositionUSD or MaxConcurrent means "uncapped" — always pass —
// never "breach on any position/open count".
type gateStates struct {
	MaxPosOK      bool
	ExposureOK    bool
	ConcurrencyOK bool
}

// computeGates derives gate pass/fail state from live positions and the
// already-computed envelope.
func computeGates(positions []apiclient.Position, env envelope) gateStates {
	g := gateStates{MaxPosOK: true, ExposureOK: true, ConcurrencyOK: true}

	if env.Risk.MaxPositionUSD > 0 {
		for _, p := range positions {
			if p.IsFlat() {
				continue
			}
			notional := p.Size * p.MarkPrice
			if notional < 0 {
				notional = -notional
			}
			if notional > env.Risk.MaxPositionUSD {
				g.MaxPosOK = false
				break
			}
		}
	}

	if env.Risk.MaxTotalExposureUSD > 0 && env.ExposureUSD > env.Risk.MaxTotalExposureUSD {
		g.ExposureOK = false
	}

	if env.Risk.MaxConcurrent > 0 && env.OpenCount > env.Risk.MaxConcurrent {
		g.ConcurrencyOK = false
	}

	return g
}
