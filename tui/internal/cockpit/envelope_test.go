package cockpit

import (
	"testing"

	"github.com/hyperagent/tui/internal/apiclient"
)

func TestComputeEnvelope(t *testing.T) {
	risk := apiclient.RiskSettings{
		MaxPositionUSD: 5000, MaxTotalExposureUSD: 10000,
		MaxConcurrent: 3, DailyLossKillUSD: 500,
	}
	positions := []apiclient.Position{
		{Coin: "ETH", Size: 2, MarkPrice: 3400, UnrealPnl: 25.5},   // +6800 notional
		{Coin: "SOL", Size: -10, MarkPrice: 150, UnrealPnl: -4.5}, // +1500 notional (abs)
		{Coin: "BTC", Size: 0, MarkPrice: 67000, UnrealPnl: 0},    // flat — ignored
	}
	env := computeEnvelope(positions, risk)
	if env.ExposureUSD != 8300 {
		t.Errorf("ExposureUSD = %v, want 8300", env.ExposureUSD)
	}
	if env.OpenCount != 2 {
		t.Errorf("OpenCount = %d, want 2", env.OpenCount)
	}
	if env.UPnL != 21.0 {
		t.Errorf("UPnL = %v, want 21.0", env.UPnL)
	}
	if env.Risk != risk {
		t.Errorf("Risk not carried through")
	}
}

func TestComputeEnvelopeEmpty(t *testing.T) {
	env := computeEnvelope(nil, apiclient.RiskSettings{})
	if env.ExposureUSD != 0 || env.OpenCount != 0 || env.UPnL != 0 {
		t.Errorf("empty envelope not zero: %+v", env)
	}
}
