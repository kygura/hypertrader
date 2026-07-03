package executor

import (
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

func newTestExec(t *testing.T, cfg RiskConfig) (*Executor, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	e := New(cfg, b, st, nil, nil, AssetIndex{}, "http://localhost", false)
	return e, st
}

func baseRisk() RiskConfig {
	return RiskConfig{
		Mode:                "autonomous",
		MaxPositionUSD:      5000,
		MaxTotalExposureUSD: 15000,
		MaxConcurrent:       3,
		DailyLossKillUSD:    1000,
		MaxPriceDeviation:   0.02,
		PostStopCooldown:    30 * time.Minute,
	}
}

func TestRiskRejectsOversize(t *testing.T) {
	e, _ := newTestExec(t, baseRisk())
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 6000,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	if err := e.riskCheck(v); err == nil {
		t.Fatal("oversize position should be rejected")
	}
}

func TestRiskRejectsPriceDeviation(t *testing.T) {
	e, st := newTestExec(t, baseRisk())
	st.PutAssetCtx(metrics.AssetCtx{Coin: "ETH", MarkPrice: 100})
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 1000,
		Entry: metrics.Entry{Type: "limit", Price: 110}, Confidence: 0.7} // 10% away
	if err := e.riskCheck(v); err == nil {
		t.Fatal("price 10% from mark should be rejected (max 2%)")
	}
}

func TestRiskAcceptsWithinBounds(t *testing.T) {
	e, st := newTestExec(t, baseRisk())
	st.PutAssetCtx(metrics.AssetCtx{Coin: "ETH", MarkPrice: 100})
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 1000,
		Entry: metrics.Entry{Type: "limit", Price: 100.5}, Confidence: 0.7}
	if err := e.riskCheck(v); err != nil {
		t.Fatalf("in-bounds verdict rejected: %v", err)
	}
}

func TestKillSwitchTrips(t *testing.T) {
	e, _ := newTestExec(t, baseRisk())
	e.RecordRealizedPnL("ETH", -1200) // exceeds daily kill of 1000
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 1000,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	if err := e.riskCheck(v); err == nil {
		t.Fatal("kill-switch should block trades after daily loss limit")
	}
}

func TestCooldownAfterLoss(t *testing.T) {
	e, st := newTestExec(t, baseRisk())
	st.PutAssetCtx(metrics.AssetCtx{Coin: "ETH", MarkPrice: 100})
	e.RecordRealizedPnL("ETH", -100) // a loss → cooldown for ETH
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 1000,
		Entry: metrics.Entry{Type: "limit", Price: 100}, Confidence: 0.7}
	if err := e.riskCheck(v); err == nil {
		t.Fatal("post-stop cooldown should block ETH")
	}
}

func TestMaxConcurrent(t *testing.T) {
	cfg := baseRisk()
	cfg.MaxConcurrent = 2
	e, st := newTestExec(t, cfg)
	st.PutAssetCtx(metrics.AssetCtx{Coin: "NEW", MarkPrice: 100})
	st.PutPosition(metrics.Position{Coin: "A", Size: 1, MarkPrice: 100})
	st.PutPosition(metrics.Position{Coin: "B", Size: 1, MarkPrice: 100})
	v := metrics.Verdict{Asset: "NEW", Action: metrics.ActionOpenLong, SizeUSD: 100,
		Entry: metrics.Entry{Type: "limit", Price: 100}, Confidence: 0.7}
	if err := e.riskCheck(v); err == nil {
		t.Fatal("third concurrent position should be rejected")
	}
}
