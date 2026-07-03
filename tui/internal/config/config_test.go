package config

import (
	"os"
	"path/filepath"
	"runtime"
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
