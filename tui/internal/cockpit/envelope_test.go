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

func TestComputeGates(t *testing.T) {
	breachingPos := []apiclient.Position{
		{Coin: "ETH", Size: 2, MarkPrice: 3400}, // 6800 notional
	}
	okPos := []apiclient.Position{
		{Coin: "ETH", Size: 1, MarkPrice: 100}, // 100 notional
	}

	tests := []struct {
		name      string
		positions []apiclient.Position
		risk      apiclient.RiskSettings
		want      gateStates
	}{
		{
			name:      "zero caps mean uncapped — all pass",
			positions: breachingPos,
			risk:      apiclient.RiskSettings{}, // all caps zero
			want:      gateStates{MaxPosOK: true, ExposureOK: true, ConcurrencyOK: true},
		},
		{
			name:      "max position breach",
			positions: breachingPos,
			risk:      apiclient.RiskSettings{MaxPositionUSD: 5000},
			want:      gateStates{MaxPosOK: false, ExposureOK: true, ConcurrencyOK: true},
		},
		{
			name:      "max exposure breach",
			positions: breachingPos,
			risk:      apiclient.RiskSettings{MaxTotalExposureUSD: 1000},
			want:      gateStates{MaxPosOK: true, ExposureOK: false, ConcurrencyOK: true},
		},
		{
			name:      "max concurrency breach",
			positions: breachingPos,
			risk:      apiclient.RiskSettings{MaxConcurrent: 0}, // zero is uncapped, needs a positive cap to breach
			want:      gateStates{MaxPosOK: true, ExposureOK: true, ConcurrencyOK: true},
		},
		{
			name: "max concurrency breach with positive cap",
			positions: []apiclient.Position{
				{Coin: "ETH", Size: 1, MarkPrice: 100},
				{Coin: "SOL", Size: 1, MarkPrice: 100},
			},
			risk: apiclient.RiskSettings{MaxConcurrent: 1},
			want: gateStates{MaxPosOK: true, ExposureOK: true, ConcurrencyOK: false},
		},
		{
			name:      "all pass under generous caps",
			positions: okPos,
			risk:      apiclient.RiskSettings{MaxPositionUSD: 5000, MaxTotalExposureUSD: 10000, MaxConcurrent: 3},
			want:      gateStates{MaxPosOK: true, ExposureOK: true, ConcurrencyOK: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := computeEnvelope(tt.positions, tt.risk)
			got := computeGates(tt.positions, env)
			if got != tt.want {
				t.Errorf("computeGates() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
