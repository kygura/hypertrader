package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// TestSmokeFrame renders one full wide-layout frame with seeded data and logs
// it — a human-eyeball check of the marketwatch redesign (run with -v).
func TestSmokeFrame(t *testing.T) {
	m, _ := newTestModel(t)
	now := time.Now()
	for i := range 24 {
		f := float64(i)
		m.cache.PutBar(apiclient.Bar{
			Coin: "BTC", Timeframe: "4h", Final: true,
			OpenTime: now.Add(time.Duration(i-24) * 4 * time.Hour),
			Open:     94000 + 40*f, Close: 94050 + 42*f, High: 94100 + 45*f, Low: 93900 + 40*f,
			Return: 0.003 * (1 + f/24), OpenInterest: 1.1e6 + 2e4*f, OIDelta: 0.004 * f,
			Funding: 0.00001 * f, Basis: 0.0003, CVD: -3e5 + 4e4*f, LiqProx: 0.18,
			RealizedVol: 0.32, BuyVolume: 900 + 30*f, SellVolume: 700 + 10*f,
		})
		m.cache.PutBar(apiclient.Bar{
			Coin: "ETH", Timeframe: "1h", Final: true,
			OpenTime: now.Add(time.Duration(i-24) * time.Hour),
			Open:     3500, Close: 3480 - 2*f, High: 3510, Low: 3470,
			Return: -0.002, OpenInterest: 8e5 - 1e3*f, OIDelta: -0.01,
			Funding: -0.00002, Basis: -0.0001, CVD: -2e5, LiqProx: 0.4,
			RealizedVol: 0.5, BuyVolume: 400, SellVolume: 600,
		})
	}
	m.cache.ApplyMarkets([]apiclient.MarketEntry{{Coin: "BTC", AssetCtx: apiclient.AssetCtx{Coin: "BTC", MarkPrice: 95080, OraclePrice: 95050,
		Funding: 0.0001, OpenInterest: 1.6e6, DayVolume: 4.2e8, Time: now}}})
	m.upsertCandidate(verdict("BTC", apiclient.ActionOpenLong, 0.71, "range reclaim; OI building with positive basis — squeeze fuel above 96k"))
	m.upsertCandidate(verdict("ETH", apiclient.ActionHold, 0.35, "downtrend intact but funding neutral; no edge at mid-range"))
	m.thesis["BTC"] = "range reclaim; OI building with positive basis"

	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 42})
	m = mdl.(*Model)
	t.Log("\n" + m.View().Content)

	m.chatTab = chatTabIdeas
	t.Log("\nIDEAS BODY:\n" + m.renderIdeasBody(100))
}
