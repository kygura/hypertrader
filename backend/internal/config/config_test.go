package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestSaveLoadRoundTrip verifies the settings-modal persistence path: a config
// written with Save loads back identical in the fields the TUI mutates (API
// keys, model selections, mode, durations).
func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")

	cfg := Default()
	cfg.Providers.Anthropic.APIKey = "sk-ant-test-123456789"
	cfg.Providers.Custom = map[string]ProviderCfg{
		"openrouter": {APIKey: "or-key", BaseURL: "https://openrouter.ai/api/v1", Model: "x"},
	}
	cfg.Reasoner.ChatProvider = "openai"
	cfg.Reasoner.ChatModel = "gpt-4o-mini"
	cfg.Execution.Mode = "autonomous"
	cfg.Execution.PostStopCooldown = Duration{45 * time.Minute}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Providers.Anthropic.APIKey != cfg.Providers.Anthropic.APIKey {
		t.Errorf("api key: got %q", got.Providers.Anthropic.APIKey)
	}
	if got.Providers.Custom["openrouter"].APIKey != "or-key" {
		t.Errorf("custom key lost: %+v", got.Providers.Custom)
	}
	if got.Reasoner.ChatProvider != "openai" || got.Reasoner.ChatModel != "gpt-4o-mini" {
		t.Errorf("model selection lost: %+v", got.Reasoner)
	}
	if got.Execution.Mode != "autonomous" {
		t.Errorf("mode lost: %q", got.Execution.Mode)
	}
	if got.Execution.PostStopCooldown.Duration != 45*time.Minute {
		t.Errorf("duration lost: %v", got.Execution.PostStopCooldown)
	}
}

// TestDefaultAPI verifies a bare config gets the loopback-only, no-auth API
// defaults so the daemon serves the HTTP API out of the box.
func TestDefaultAPI(t *testing.T) {
	cfg := Default()
	if !cfg.API.Enabled {
		t.Error("API.Enabled should default true")
	}
	if cfg.API.Addr != "127.0.0.1:8787" {
		t.Errorf("API.Addr = %q, want 127.0.0.1:8787", cfg.API.Addr)
	}
}

// TestLoadRejectsNonLoopbackWithoutToken: binding the execution surface off
// loopback with no bearer token is a startup error, not a silent open port.
func TestLoadRejectsNonLoopbackWithoutToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[api]\n  enabled = true\n  addr = \"0.0.0.0:8787\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for non-loopback addr without token")
	}
	if !strings.Contains(err.Error(), "without [api] token") {
		t.Errorf("error = %q, want it to mention 'without [api] token'", err.Error())
	}
}

// TestLoadAllowsNonLoopbackWithToken: a token authorizes an off-loopback bind.
func TestLoadAllowsNonLoopbackWithToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[api]\n  enabled = true\n  addr = \"0.0.0.0:8787\"\n  token = \"x\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("token should authorize non-loopback bind: %v", err)
	}
}

// TestSavePermissions: a config that can hold API keys must not be world-readable.
func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix permissions")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config perm = %o, want 600", perm)
	}
}

// TestGateDefaultsAndMigration verifies the [gate] section: a config written
// before the section existed (including the removed reasoner.read_every_batch
// key) still parses and receives the non-permissive gate defaults; an explicit
// [gate] section overrides them.
func TestGateDefaultsAndMigration(t *testing.T) {
	old := filepath.Join(t.TempDir(), "old.toml")
	if err := os.WriteFile(old, []byte(`
[markets]
  visualized = ["BTC"]
  tracked = ["BTC"]

[reasoner]
  batch_provider = "deepseek"
  read_every_batch = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(old)
	if err != nil {
		t.Fatalf("pre-gate config must still parse: %v", err)
	}
	g := cfg.Gate
	if len(g.LTFTimeframes) != 3 || g.LTFTimeframes[0] != "1m" {
		t.Fatalf("ltf_timeframes default = %v, want [1m 5m 15m]", g.LTFTimeframes)
	}
	if g.ZScoreReturn != 3.0 || g.FundingAbs != 0.0008 || g.OIDeltaAbs != 0.04 || g.CVDZScore != 3.0 {
		t.Fatalf("threshold defaults wrong: %+v", g)
	}
	if g.Cooldown.Duration != 30*time.Minute || !g.PositionAlways {
		t.Fatalf("cooldown/position_always defaults wrong: %+v", g)
	}

	custom := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(custom, []byte(`
[markets]
  visualized = ["BTC"]

[gate]
  ltf_timeframes = ["5m"]
  zscore_return = 4.5
  cooldown = "1h"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(custom)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg2.Gate.LTFTimeframes) != 1 || cfg2.Gate.ZScoreReturn != 4.5 || cfg2.Gate.Cooldown.Duration != time.Hour {
		t.Fatalf("[gate] overrides lost: %+v", cfg2.Gate)
	}
}

// TestGateSectionRoundTrips verifies Save/Load preserve the gate section the
// same way the settings modal relies on for every other section.
func TestGateSectionRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.Gate.ZScoreReturn = 2.5
	cfg.Gate.Cooldown = Duration{45 * time.Minute}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Gate.ZScoreReturn != 2.5 || got.Gate.Cooldown.Duration != 45*time.Minute {
		t.Fatalf("gate round-trip lost values: %+v", got.Gate)
	}
}
