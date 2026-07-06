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
