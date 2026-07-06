package cockpit

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

func TestViewSmoke(t *testing.T) {
	cache := apiclient.NewCache()
	cache.PutMid("ETH", 3412.4)
	cache.PutBar(apiclient.Bar{
		Coin: "ETH", Timeframe: "1h", CloseTime: time.Now(),
		Open: 3400, Close: 3412.4, Funding: 0.0000125, OIDelta: 0.009, CVD: 4.2e6,
	})
	cache.ApplyMarkets([]apiclient.MarketEntry{{
		Coin:     "ETH",
		Mid:      3412.4,
		Position: apiclient.Position{Coin: "ETH", Size: 2.44, EntryPrice: 3402.1, MarkPrice: 3412.4, UnrealPnl: 25.1},
	}})

	m := New(Config{
		Cache: cache,
		Settings: apiclient.SettingsResponse{
			Mode:       "propose",
			Visualized: []string{"ETH"},
			Timeframes: map[string]string{"ETH": "1h"},
			Risk:       apiclient.RiskSettings{MaxPositionUSD: 5000, MaxTotalExposureUSD: 10000, MaxConcurrent: 3, DailyLossKillUSD: 500},
		},
	})
	m.Update(journalMsg{Coin: "ETH", Kind: "fill", Summary: "0.85 ETH filled @ 3,391.50"})
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})

	out := m.View().Content
	for _, want := range []string{"MANDATE", "MARKET PICTURE", "EXECUTION", "DECISION JOURNAL", "HYPERTRADER", "ETH"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if rows := strings.Count(out, "\n") + 1; rows != 30 {
		t.Errorf("view rows = %d, want 30", rows)
	}
}

func TestViewTooSmall(t *testing.T) {
	m := New(Config{Cache: apiclient.NewCache()})
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 10})
	if out := m.View().Content; !strings.Contains(out, "needs at least") {
		t.Error("small-terminal guard missing")
	}
}
