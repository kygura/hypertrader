package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/executor"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/signing"
	"github.com/hyperagent/hyperagent/internal/store"
)

// fakeChatProvider is a minimal reasoner.Provider for exercising the chat
// endpoint end to end through a real reasoner.Engine/Registry — the same
// fake-provider pattern used in internal/reasoner/engine_test.go.
type fakeChatProvider struct {
	name    string
	reply   string
	err     error
	lastReq reasoner.Request
}

func (p *fakeChatProvider) Name() string { return p.name }
func (p *fakeChatProvider) Complete(_ context.Context, req reasoner.Request) (reasoner.Response, error) {
	p.lastReq = req
	if p.err != nil {
		return reasoner.Response{}, p.err
	}
	return reasoner.Response{Reply: p.reply, Model: req.Model}, nil
}

func newTestEngine(provider *fakeChatProvider) *reasoner.Engine {
	reg := reasoner.NewRegistry(
		map[string]reasoner.Provider{provider.name: provider},
		map[string][]string{provider.name: {"model-x"}},
		provider.name, "model-x",
		provider.name, "model-x",
	)
	return reasoner.NewEngine(bus.New(), reg, make(chan metrics.Digest), nil)
}

// baseRisk is a permissive risk config for act_test's execution-endpoint tests.
func baseRisk() executor.RiskConfig {
	return executor.RiskConfig{
		Mode:                "autonomous",
		MaxPositionUSD:      5000,
		MaxTotalExposureUSD: 15000,
		MaxConcurrent:       3,
		DailyLossKillUSD:    1000,
		MaxPriceDeviation:   0.02,
		PostStopCooldown:    30 * time.Minute,
	}
}

// newFakeExchangeExecutor builds a real Executor wired to an httptest server
// standing in for the HL exchange endpoint, so the approve/orders HTTP paths
// can be exercised end to end (mirrors executor_test.go's harness).
func newFakeExchangeExecutor(t *testing.T, cfg executor.RiskConfig) (*executor.Executor, *int) {
	t.Helper()
	var calls int
	xsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	t.Cleanup(xsrv.Close)

	signer, err := signing.NewSigner("3e44cdea317f6553d630e370a143ccbde9ee6fb7aef6d9009443be3606609718")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	jr, err := journal.New(b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := executor.New(cfg, b, st, jr, signer, executor.AssetIndex{"ETH": 1}, xsrv.URL, false)
	return e, &calls
}

func newActTestServer(t *testing.T, engine *reasoner.Engine, exec *executor.Executor) (*httptest.Server, *Server) {
	t.Helper()
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Deps{
		Bus:     bus.New(),
		Store:   st,
		Engine:  engine,
		Exec:    exec,
		Cfg:     config.Default(),
		Version: "test",
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, s
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any, out any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Post(srv.URL+path, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	if out != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp
}

func TestChatHappyPath(t *testing.T) {
	provider := &fakeChatProvider{name: "fake", reply: "hello there"}
	engine := newTestEngine(provider)
	srv, _ := newActTestServer(t, engine, nil)

	var body struct {
		Reply    string `json:"reply"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	resp := postJSON(t, srv, "/api/chat", map[string]any{
		"message": "what's up with BTC",
		"history": []map[string]string{{"role": "user", "text": "hi"}},
	}, &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body.Reply != "hello there" {
		t.Errorf("reply = %q, want %q", body.Reply, "hello there")
	}
	if body.Provider != "fake" {
		t.Errorf("provider = %q, want %q", body.Provider, "fake")
	}
	if body.Model != "model-x" {
		t.Errorf("model = %q, want %q", body.Model, "model-x")
	}
	if len(provider.lastReq.ChatHistory) != 1 || provider.lastReq.ChatHistory[0].Role != "user" {
		t.Errorf("provider did not receive mapped history: %+v", provider.lastReq.ChatHistory)
	}
}

func TestChatNilEngineReturns503(t *testing.T) {
	srv, _ := newActTestServer(t, nil, nil)
	resp := postJSON(t, srv, "/api/chat", map[string]any{"message": "hi"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestChatProviderErrorReturns502(t *testing.T) {
	provider := &fakeChatProvider{name: "fake", err: context.DeadlineExceeded}
	engine := newTestEngine(provider)
	srv, _ := newActTestServer(t, engine, nil)
	resp := postJSON(t, srv, "/api/chat", map[string]any{"message": "hi"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

func TestProposalsListApproveReject(t *testing.T) {
	exec, calls := newFakeExchangeExecutor(t, baseRisk())
	srv, _ := newActTestServer(t, nil, exec)

	v := metrics.Verdict{Asset: "ETH", Action: metrics.ActionOpenLong, SizeUSD: 100,
		Entry: metrics.Entry{Type: "market"}, Confidence: 0.7}
	p := exec.Proposals().Add(v)

	var list []executor.Proposal
	getResp, err := srv.Client().Get(srv.URL + "/api/proposals")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/proposals status = %d, want 200", getResp.StatusCode)
	}
	if err := json.NewDecoder(getResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != p.ID {
		t.Fatalf("proposals list = %+v, want [%s]", list, p.ID)
	}

	approveResp := postJSON(t, srv, "/api/proposals/"+p.ID+"/approve", nil, nil)
	defer approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d, want 200", approveResp.StatusCode)
	}
	if *calls != 1 {
		t.Fatalf("exchange calls = %d, want 1", *calls)
	}

	unknownResp := postJSON(t, srv, "/api/proposals/does-not-exist/approve", nil, nil)
	defer unknownResp.Body.Close()
	if unknownResp.StatusCode != http.StatusNotFound {
		t.Fatalf("approve unknown status = %d, want 404", unknownResp.StatusCode)
	}

	p2 := exec.Proposals().Add(v)
	rejectResp := postJSON(t, srv, "/api/proposals/"+p2.ID+"/reject", nil, nil)
	defer rejectResp.Body.Close()
	if rejectResp.StatusCode != http.StatusOK {
		t.Fatalf("reject status = %d, want 200", rejectResp.StatusCode)
	}
	if *calls != 1 {
		t.Fatalf("exchange calls after reject = %d, want still 1", *calls)
	}

	rejectUnknown := postJSON(t, srv, "/api/proposals/does-not-exist/reject", nil, nil)
	defer rejectUnknown.Body.Close()
	if rejectUnknown.StatusCode != http.StatusNotFound {
		t.Fatalf("reject unknown status = %d, want 404", rejectUnknown.StatusCode)
	}
}

func TestOrdersHappyPath(t *testing.T) {
	exec, calls := newFakeExchangeExecutor(t, baseRisk())
	srv, _ := newActTestServer(t, nil, exec)

	var body map[string]string
	resp := postJSON(t, srv, "/api/orders", map[string]any{
		"coin":     "ETH",
		"action":   "open_long",
		"size_usd": 100,
	}, &body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%v", resp.StatusCode, body)
	}
	if *calls != 1 {
		t.Fatalf("exchange calls = %d, want 1", *calls)
	}
}

func TestOrdersGateRejectionReturns422(t *testing.T) {
	exec, _ := newFakeExchangeExecutor(t, baseRisk())
	srv, _ := newActTestServer(t, nil, exec)

	resp := postJSON(t, srv, "/api/orders", map[string]any{
		"coin":     "ETH",
		"action":   "open_long",
		"size_usd": 999999, // exceeds MaxPositionUSD
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected non-empty gate error message")
	}
}

func TestOrdersNilExecReturns503(t *testing.T) {
	srv, _ := newActTestServer(t, nil, nil)
	resp := postJSON(t, srv, "/api/orders", map[string]any{
		"coin": "ETH", "action": "open_long", "size_usd": 100,
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestProposalsNilExecReturns503(t *testing.T) {
	srv, _ := newActTestServer(t, nil, nil)
	resp, err := srv.Client().Get(srv.URL + "/api/proposals")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
