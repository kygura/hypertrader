package executor

import (
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/journal"
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

func TestRiskCapitalRelativeCapsPosition(t *testing.T) {
	cfg := baseRisk()
	cfg.MaxPositionPct = 0.10 // 10% of equity
	e, st := newTestExec(t, cfg)
	st.SetAccount(1000, nil) // equity $1000 → max position $100
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 150,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	if err := e.riskCheck(v); err == nil {
		t.Fatal("150 USD on 1000 equity with 10% cap should be rejected")
	}
	v.SizeUSD = 90
	if err := e.riskCheck(v); err != nil {
		t.Fatalf("90 USD within 10%% of 1000 equity rejected: %v", err)
	}
}

func TestRiskCapitalRelativeCapsExposure(t *testing.T) {
	cfg := baseRisk()
	cfg.MaxTotalExposurePct = 0.50 // 50% of equity deployed max
	e, st := newTestExec(t, cfg)
	st.SetAccount(1000, []metrics.Position{{Coin: "BTC", Size: 4, MarkPrice: 100}}) // $400 deployed
	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 200,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7} // → $600 > $500 cap
	if err := e.riskCheck(v); err == nil {
		t.Fatal("exposure 600 over 50% of 1000 equity should be rejected")
	}
	v.SizeUSD = 80 // → $480 ≤ $500
	if err := e.riskCheck(v); err != nil {
		t.Fatalf("exposure 480 within cap rejected: %v", err)
	}
}

func TestRiskUnknownEquityFailsClosed(t *testing.T) {
	cfg := baseRisk()
	cfg.MaxPositionPct = 0.10
	e, st := newTestExec(t, cfg) // no SetAccount: equity unknown
	open := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 50,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	if err := e.riskCheck(open); err == nil {
		t.Fatal("open with unknown equity and pct gate configured should be rejected")
	}
	st.PutPosition(metrics.Position{Coin: "ETH", Size: 1, MarkPrice: 100})
	closeV := metrics.Verdict{Asset: "ETH", Action: metrics.ActionClose, SizeUSD: 50,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	if err := e.riskCheck(closeV); err != nil {
		t.Fatalf("close must pass capital gates even with unknown equity: %v", err)
	}
}

// fakeTheses is a minimal ThesisState for the thesis-gate tests.
type fakeTheses struct {
	theses map[string]metrics.Thesis
}

func (f *fakeTheses) Get(coin string) (metrics.Thesis, bool) {
	t, ok := f.theses[coin]
	return t, ok
}

// TestThesisGateRefusalMatrix pins the deterministic scalp policy: trigger-path
// verdicts need a live, direction-matched thesis; close always passes; review
// and legacy paths bypass the gate entirely. The refusal reasons are exact —
// they are the journaled contract.
func TestThesisGateRefusalMatrix(t *testing.T) {
	long := &fakeTheses{theses: map[string]metrics.Thesis{
		"ETH": {Coin: "ETH", Direction: "long", Version: 1},
	}}
	neutral := &fakeTheses{theses: map[string]metrics.Thesis{
		"ETH": {Coin: "ETH", Direction: "neutral", Version: 1},
	}}
	none := &fakeTheses{}

	cases := []struct {
		name    string
		theses  ThesisState
		source  string
		action  metrics.Action
		posSize float64 // open position size for scale cases
		wantErr string  // "" → allowed
	}{
		{"trigger open matching long thesis", long, metrics.DigestTrigger, metrics.ActionOpenLong, 0, ""},
		{"trigger open against long thesis", long, metrics.DigestTrigger, metrics.ActionOpenShort, 0, "thesis-gate: direction mismatch"},
		{"trigger open with no thesis", none, metrics.DigestTrigger, metrics.ActionOpenLong, 0, "thesis-gate: no live thesis"},
		{"trigger open with nil store", nil, metrics.DigestTrigger, metrics.ActionOpenLong, 0, "thesis-gate: no live thesis"},
		{"trigger open against neutral thesis", neutral, metrics.DigestTrigger, metrics.ActionOpenLong, 0, "thesis-gate: direction mismatch"},
		{"trigger close always allowed", none, metrics.DigestTrigger, metrics.ActionClose, 0, ""},
		{"trigger scale matching position+thesis", long, metrics.DigestTrigger, metrics.ActionScale, 2, ""},
		{"trigger scale against thesis", long, metrics.DigestTrigger, metrics.ActionScale, -2, "thesis-gate: direction mismatch"},
		{"trigger scale on flat book", long, metrics.DigestTrigger, metrics.ActionScale, 0, "thesis-gate: direction mismatch"},
		{"review path bypasses gate", none, metrics.DigestReview, metrics.ActionOpenLong, 0, ""},
		{"legacy path bypasses gate", none, "", metrics.ActionOpenShort, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, st := newTestExec(t, baseRisk())
			if tc.theses != nil {
				e.SetTheses(tc.theses)
			}
			if tc.posSize != 0 {
				st.PutPosition(metrics.Position{Coin: "ETH", Size: tc.posSize, MarkPrice: 100})
			}
			v := metrics.Verdict{Asset: "ETH", Action: tc.action, SizeUSD: 1000,
				Entry: metrics.Entry{Type: "market"}, Confidence: 0.7, Source: tc.source}
			err := e.thesisGate(v)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("err = %v, want exactly %q", err, tc.wantErr)
			}
		})
	}
}

// TestHandleRefusesTriggerVerdictBeforeProposal verifies the gate sits ahead
// of the proposal registry: a refused trigger verdict never becomes a pending
// proposal, even in propose mode.
func TestHandleRefusesTriggerVerdictBeforeProposal(t *testing.T) {
	cfg := baseRisk()
	cfg.Mode = "propose"
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	jr, err := journal.New(b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(cfg, b, st, jr, nil, AssetIndex{}, "http://localhost", false)

	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 1000,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7, Source: metrics.DigestTrigger}
	e.Handle(v) // no thesis store wired → refused

	if got := e.Proposals().List(); len(got) != 0 {
		t.Fatalf("refused trigger verdict became a proposal: %+v", got)
	}
}
