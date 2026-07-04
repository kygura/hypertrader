package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/executor"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/store"
)

// putJSON mirrors postJSON (act_test.go) but issues a PUT — the verb every
// settings/mode/key mutation in this file uses.
func putJSON(t *testing.T, srv *httptest.Server, path string, body any, out any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+path, bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	if out != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp
}

// newSettingsTestServer boots a Server with the given Deps, filling in the
// Bus/Store every Server needs (runCaches subscribes unconditionally) when the
// caller didn't set them. Unlike newWatchlistTestServer, it never overwrites
// deps.Cfg — settings tests always set Cfg explicitly (Markets, Execution).
func newSettingsTestServer(t *testing.T, deps Deps) *httptest.Server {
	t.Helper()
	if deps.Bus == nil {
		deps.Bus = bus.New()
	}
	if deps.Store == nil {
		st, err := store.New(t.TempDir(), 8)
		if err != nil {
			t.Fatal(err)
		}
		deps.Store = st
	}
	s := NewServer(deps)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// newSettingsTestEngine builds a real reasoner.Engine over two fake providers
// so GET/PUT /api/settings exercise the actual Registry, not a mock.
func newSettingsTestEngine(t *testing.T) *reasoner.Engine {
	t.Helper()
	anthropic := &fakeChatProvider{name: "anthropic", reply: "ok"}
	openai := &fakeChatProvider{name: "openai", reply: "ok"}
	reg := reasoner.NewRegistry(
		map[string]reasoner.Provider{"anthropic": anthropic, "openai": openai},
		map[string][]string{
			"anthropic": {"claude-opus-4-8", "claude-sonnet-4-6"},
			"openai":    {"gpt-4o"},
		},
		"anthropic", "claude-opus-4-8", // batch
		"openai", "gpt-4o", // chat
	)
	return reasoner.NewEngine(bus.New(), reg, make(chan metrics.Digest), nil)
}

func TestSettingsGetNilEngineReturns503(t *testing.T) {
	srv := newSettingsTestServer(t, Deps{Cfg: config.Default()})
	resp, err := srv.Client().Get(srv.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestSettingsGetHappyPath(t *testing.T) {
	engine := newSettingsTestEngine(t)
	cfg := config.Default()
	cfg.Markets = config.Markets{Visualized: []string{"BTC", "ETH"}, Tracked: []string{"BTC"}}
	cfg.Execution.MaxPositionUSD = 5000
	srv := newSettingsTestServer(t, Deps{Engine: engine, Cfg: cfg, CfgSnapshot: func() config.Config { return cfg }})

	var body settingsResponse
	resp, err := srv.Client().Get(srv.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if !slicesContain(body.ProviderNames, "anthropic") || !slicesContain(body.ProviderNames, "openai") {
		t.Fatalf("provider_names = %v, want to contain anthropic and openai", body.ProviderNames)
	}
	if body.Batch.Provider != "anthropic" || body.Batch.Model != "claude-opus-4-8" {
		t.Fatalf("batch = %+v, want anthropic/claude-opus-4-8", body.Batch)
	}
	if body.Chat.Provider != "openai" || body.Chat.Model != "gpt-4o" {
		t.Fatalf("chat = %+v, want openai/gpt-4o", body.Chat)
	}
	if len(body.Visualized) != 2 || body.Visualized[0] != "BTC" || body.Visualized[1] != "ETH" {
		t.Fatalf("visualized = %v, want [BTC ETH]", body.Visualized)
	}
	if len(body.Tracked) != 1 || body.Tracked[0] != "BTC" {
		t.Fatalf("tracked = %v, want [BTC]", body.Tracked)
	}
	if _, ok := body.Timeframes["BTC"]; !ok {
		t.Errorf("timeframes missing BTC entry: %v", body.Timeframes)
	}
	if _, ok := body.Timeframes["ETH"]; !ok {
		t.Errorf("timeframes missing ETH entry: %v", body.Timeframes)
	}
	if body.Risk.MaxPositionUSD != 5000 {
		t.Errorf("risk.max_position_usd = %v, want 5000", body.Risk.MaxPositionUSD)
	}
}

func TestSettingsPutSwitchesChatModelOnly(t *testing.T) {
	engine := newSettingsTestEngine(t)
	cfg := config.Default()
	srv := newSettingsTestServer(t, Deps{Engine: engine, Cfg: cfg, CfgSnapshot: func() config.Config { return cfg }})

	resp := putJSON(t, srv, "/api/settings", map[string]any{"chat_model": "gpt-4o-mini"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	var body settingsResponse
	getResp, err := srv.Client().Get(srv.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if err := json.NewDecoder(getResp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Chat.Provider != "openai" || body.Chat.Model != "gpt-4o-mini" {
		t.Fatalf("chat = %+v, want openai/gpt-4o-mini", body.Chat)
	}
	if body.Batch.Provider != "anthropic" || body.Batch.Model != "claude-opus-4-8" {
		t.Fatalf("batch drifted: %+v", body.Batch)
	}
}

func TestSettingsPutUnknownChatProviderReturns422(t *testing.T) {
	engine := newSettingsTestEngine(t)
	srv := newSettingsTestServer(t, Deps{Engine: engine, Cfg: config.Default()})

	resp := putJSON(t, srv, "/api/settings", map[string]any{"chat_provider": "nope"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestSettingsPutModeNilExecReturns503(t *testing.T) {
	srv := newSettingsTestServer(t, Deps{Cfg: config.Default()})
	resp := putJSON(t, srv, "/api/execution/mode", map[string]any{"mode": "propose"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// newProposeModeExecutor builds a real Executor in propose mode with no
// signer, mirroring executor_test.go's newTestExec harness — the guard that
// rejects switching to autonomous without a signer is real production code,
// not a mock.
func newProposeModeExecutor(t *testing.T) *executor.Executor {
	t.Helper()
	st, err := store.New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	cfg := executor.RiskConfig{
		Mode:                "propose",
		MaxPositionUSD:      5000,
		MaxTotalExposureUSD: 15000,
		MaxConcurrent:       3,
		DailyLossKillUSD:    1000,
		MaxPriceDeviation:   0.02,
		PostStopCooldown:    30 * time.Minute,
	}
	return executor.New(cfg, bus.New(), st, nil, nil, executor.AssetIndex{}, "http://localhost", false)
}

func TestSettingsPutModeAutonomousNoSignerReturns422(t *testing.T) {
	exec := newProposeModeExecutor(t)
	srv := newSettingsTestServer(t, Deps{Exec: exec, Cfg: config.Default()})

	resp := putJSON(t, srv, "/api/execution/mode", map[string]any{"mode": "autonomous"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestSettingsPutModePropose(t *testing.T) {
	exec := newProposeModeExecutor(t)
	srv := newSettingsTestServer(t, Deps{Exec: exec, Cfg: config.Default()})

	resp := putJSON(t, srv, "/api/execution/mode", map[string]any{"mode": "propose"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if exec.Mode() != "propose" {
		t.Fatalf("exec.Mode() = %q, want propose", exec.Mode())
	}
}

func TestSettingsPutProviderKeyUnknownReturns404(t *testing.T) {
	engine := newSettingsTestEngine(t)
	cfg := config.Default()
	srv := newSettingsTestServer(t, Deps{Engine: engine, Cfg: cfg, CfgSnapshot: func() config.Config { return cfg }})

	resp := putJSON(t, srv, "/api/providers/nope/key", map[string]any{"key": "sk-test"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestSettingsPutProviderKeySetsAndMasksHint exercises a *custom* provider
// name ("local-llm", stored in Cfg.Providers.Custom, a map). Deps.CfgSnapshot
// here mirrors main.go's real wiring: a mutex-guarded closure returning the
// live cfg value SaveConfig mutates, so a key written through SaveConfig's
// closure is visible to a later GET.
func TestSettingsPutProviderKeySetsAndMasksHint(t *testing.T) {
	custom := &fakeChatProvider{name: "local-llm", reply: "ok"}
	reg := reasoner.NewRegistry(
		map[string]reasoner.Provider{"local-llm": custom},
		map[string][]string{"local-llm": {"model-a"}},
		"local-llm", "model-a",
		"local-llm", "model-a",
	)
	engine := reasoner.NewEngine(bus.New(), reg, make(chan metrics.Digest), nil)

	cfg := config.Default()
	cfg.Providers.Custom = map[string]config.ProviderCfg{
		"local-llm": {Model: "model-a", BaseURL: "http://localhost:11434"},
	}
	var mu sync.Mutex
	var savedKey string
	deps := Deps{
		Engine: engine,
		Cfg:    cfg,
		CfgSnapshot: func() config.Config {
			mu.Lock()
			defer mu.Unlock()
			return cfg
		},
		SaveConfig: func(apply func(*config.Config)) error {
			mu.Lock()
			defer mu.Unlock()
			apply(&cfg)
			savedKey = cfg.Providers.Custom["local-llm"].APIKey
			return nil
		},
	}
	srv := newSettingsTestServer(t, deps)

	resp := putJSON(t, srv, "/api/providers/local-llm/key", map[string]any{"key": "sk-test-raw-secret-value"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if savedKey != "sk-test-raw-secret-value" {
		t.Fatalf("SaveConfig did not receive the new key: got %q", savedKey)
	}

	var body settingsResponse
	getResp, err := srv.Client().Get(srv.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if err := json.NewDecoder(getResp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	hint := body.KeyHints["local-llm"]
	if hint == "" {
		t.Fatal("key_hints[local-llm] is empty, want non-empty masked hint")
	}
	if strings.Contains(hint, "sk-test-raw-secret-value") {
		t.Fatalf("key_hints[local-llm] = %q leaks the raw key", hint)
	}
}

// TestSettingsPutProviderKeyNamedProviderRoundTrips is the case Task 3's
// reviewer flagged as broken: "anthropic" is a plain config.ProviderCfg
// struct field, not a map entry like the custom-provider case above, so
// writing its key through SaveConfig's apply(&cfg) only mutated the closure's
// own cfg copy — a Deps.Cfg value frozen at NewServer construction would
// never observe that mutation in the same process, leaving key_hints stale.
// CfgSnapshot (this task's fix) closes that gap: both handleGetSettings's
// key_hints and handlePutProviderKey's lookup now read through
// CfgSnapshot(), a mutex-guarded closure over the same live cfg SaveConfig
// writes — so this now round-trips for a named provider too.
func TestSettingsPutProviderKeyNamedProviderRoundTrips(t *testing.T) {
	engine := newSettingsTestEngine(t) // registers anthropic + openai
	cfg := config.Default()
	var mu sync.Mutex
	deps := Deps{
		Engine: engine,
		Cfg:    cfg,
		CfgSnapshot: func() config.Config {
			mu.Lock()
			defer mu.Unlock()
			return cfg
		},
		SaveConfig: func(apply func(*config.Config)) error {
			mu.Lock()
			defer mu.Unlock()
			apply(&cfg)
			return nil
		},
	}
	srv := newSettingsTestServer(t, deps)

	resp := putJSON(t, srv, "/api/providers/anthropic/key", map[string]any{"key": "sk-ant-raw-secret-value"}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	var body settingsResponse
	getResp, err := srv.Client().Get(srv.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if err := json.NewDecoder(getResp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	hint := body.KeyHints["anthropic"]
	if hint == "" {
		t.Fatal("key_hints[anthropic] is empty, want non-empty masked hint reflecting the just-written key")
	}
	if strings.Contains(hint, "sk-ant-raw-secret-value") {
		t.Fatalf("key_hints[anthropic] = %q leaks the raw key", hint)
	}
}

func slicesContain(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
