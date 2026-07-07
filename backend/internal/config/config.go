// Package config loads and validates the single config.toml that drives the
// daemon. It encodes the plan's sane defaults so a bare config still runs, and
// the explicit visualized-vs-tracked split (watch many, reason over a subset).
package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the full application configuration.
type Config struct {
	Markets    Markets    `toml:"markets"`
	Timeframe  Timeframe  `toml:"timeframe"`
	Reasoner   Reasoner   `toml:"reasoner"`
	Gate       Gate       `toml:"gate"`
	Execution  Execution  `toml:"execution"`
	Telegram   Telegram   `toml:"telegram"`
	Providers  Providers  `toml:"providers"`
	Storage    Storage    `toml:"storage"`
	MarketData MarketData `toml:"marketdata"`
	API        API        `toml:"api"`
}

// MarketData configures the historical backfill sources independent of
// Hyperliquid: a local CSV corpus and the CoinGecko free-tier OHLC API. Warm-up
// tries CSV first, then CoinGecko, before HL's own candleSnapshot — so a fresh
// install has real price history immediately and can run fully offline from CSVs.
type MarketData struct {
	CSVDir       string            `toml:"csv_dir"`
	UseCoinGecko bool              `toml:"use_coingecko"`
	IDs          map[string]string `toml:"ids"` // HL symbol -> CoinGecko id overrides
}

// Markets is the watchlist split: visualized in the TUI, tracked by the LLM.
type Markets struct {
	Visualized []string `toml:"visualized"`
	Tracked    []string `toml:"tracked"`
}

// Timeframe configures the default bar timeframe and per-asset overrides.
type Timeframe struct {
	Default  string            `toml:"default"`
	PerAsset map[string]string `toml:"per_asset"`
}

// For returns the effective timeframe for an asset.
func (t Timeframe) For(coin string) string {
	if tf, ok := t.PerAsset[coin]; ok && tf != "" {
		return tf
	}
	return t.Default
}

// Reasoner selects providers and models per role. The *_model fields are
// optional: when blank, a role uses its provider's default model. They are what
// make per-role model selection configurable up front; the TUI's /model command
// and picker override them live.
type Reasoner struct {
	BatchProvider string `toml:"batch_provider"`
	ChatProvider  string `toml:"chat_provider"`
	BatchModel    string `toml:"batch_model"` // optional; defaults to the provider's model
	ChatModel     string `toml:"chat_model"`  // optional; defaults to the provider's model
}

// Gate configures the deterministic deviation detector that decides when a
// low-timeframe anomaly earns an LLM call. The defaults are deliberately
// non-permissive — a quiet tape produces zero trigger calls; only the
// scheduled review cadence reaches the model. A zero threshold disables that
// rule. Old config files without a [gate] section get these defaults on load
// (unknown keys, including the removed reasoner.read_every_batch, are ignored).
type Gate struct {
	LTFTimeframes  []string `toml:"ltf_timeframes"`  // finalized bars the rules run on
	ZScoreReturn   float64  `toml:"zscore_return"`   // |return z-score| vs the timeframe's history
	FundingAbs     float64  `toml:"funding_abs"`     // |funding rate| per hour
	OIDeltaAbs     float64  `toml:"oi_delta_abs"`    // |OI delta| fraction per bar
	CVDZScore      float64  `toml:"cvd_zscore"`      // |per-bar CVD delta z-score| vs history
	Cooldown       Duration `toml:"cooldown"`        // per (coin, rule) re-fire floor
	PositionAlways bool     `toml:"position_always"` // open positions always get their HTF review
}

// Execution holds the deterministic risk gates and the propose/autonomous mode.
// The absolute USD gates and the capital-relative pct gates compose: the
// effective cap is the stricter of the two, so a small account is protected by
// the pct gates while a large one is still bounded by the absolute ceilings.
type Execution struct {
	Mode                string   `toml:"mode"` // "propose" | "autonomous"
	MaxPositionUSD      float64  `toml:"max_position_usd"`
	MaxTotalExposureUSD float64  `toml:"max_total_exposure_usd"`
	MaxPositionPct      float64  `toml:"max_position_pct"`       // fraction of account equity per position; 0 disables
	MaxTotalExposurePct float64  `toml:"max_total_exposure_pct"` // fraction of account equity across all positions; 0 disables
	MaxConcurrent       int      `toml:"max_concurrent"`
	DailyLossKillUSD    float64  `toml:"daily_loss_kill_usd"`
	MaxPriceDeviation   float64  `toml:"max_price_deviation"` // sanity gate vs live mid (fraction)
	PostStopCooldown    Duration `toml:"post_stop_cooldown"`
}

// Telegram configures the bot-API log channel and inline approval.
type Telegram struct {
	Enabled  bool   `toml:"enabled"`
	BotToken string `toml:"bot_token"`
	ChatID   string `toml:"chat_id"`
}

// Providers holds per-backend credentials and endpoints. The three named fields
// are conveniences; Custom registers any number of additional OpenAI-compatible
// endpoints by name (OpenRouter, Together, a local vLLM, …). Every entry is
// selectable from the Reasoner role fields and the TUI's /provider command.
type Providers struct {
	Anthropic ProviderCfg            `toml:"anthropic"`
	OpenAI    ProviderCfg            `toml:"openai"`
	Deepseek  ProviderCfg            `toml:"deepseek"`
	Custom    map[string]ProviderCfg `toml:"custom"`
}

// ProviderCfg is one model backend. BaseURL lets the OpenAI-compatible adapter
// cover Deepseek and any base-URL-swappable endpoint. APIKeyEnv names an
// environment variable to read the key from when APIKey is blank, so secrets stay
// out of the config file.
type ProviderCfg struct {
	APIKey    string `toml:"api_key"`
	APIKeyEnv string `toml:"api_key_env"`
	Model     string `toml:"model"`
	BaseURL   string `toml:"base_url"`
	// Models lists known model ids for this provider, populating the TUI's /model
	// picker. It is a convenience only — any model id can be typed free-form at
	// runtime, so endpoints exposing hundreds of models (OpenRouter) still work.
	Models []string `toml:"models"`
	// Kind selects the wire protocol for Custom providers: "openai" (default) or
	// "anthropic". The three named providers set this implicitly.
	Kind string `toml:"kind"`
}

// Key resolves the effective API key: the literal config value, else the named
// environment variable, else the conventional <DEFAULT>_API_KEY fallback.
func (p ProviderCfg) Key(defaultEnv string) string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.APIKeyEnv != "" {
		if v := os.Getenv(p.APIKeyEnv); v != "" {
			return v
		}
	}
	if defaultEnv != "" {
		return os.Getenv(defaultEnv)
	}
	return ""
}

// Storage configures the on-disk history location and live ring sizes.
type Storage struct {
	Dir         string `toml:"dir"`
	RingSize    int    `toml:"ring_size"`
	HistoryBars int    `toml:"history_bars"`
}

// API configures the daemon's HTTP+WS surface (internal/api): the unified
// backend core any frontend attaches to. Default bind is loopback-only with no
// auth; a non-loopback Addr requires a Token, enforced in validate() — the
// execution surface must never come up open on a public interface.
type API struct {
	Enabled     bool     `toml:"enabled"`
	Addr        string   `toml:"addr"`
	Token       string   `toml:"token"`
	CORSOrigins []string `toml:"cors_origins"`
}

// Duration is a TOML-friendly time.Duration that parses "1h", "30s", etc.
type Duration struct{ time.Duration }

// MarshalText lets the TOML encoder round-trip durations as "30m0s" strings,
// which Save depends on.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// Default returns a fully populated config with the plan's sane defaults. Used
// when no file exists and as the base that file values overlay.
func Default() Config {
	return Config{
		Markets: Markets{
			Visualized: []string{"BTC", "ETH", "SOL", "HYPE"},
			Tracked:    []string{"BTC", "ETH", "SOL", "HYPE"},
		},
		Timeframe: Timeframe{
			Default:  "1h",
			PerAsset: map[string]string{"BTC": "4h"},
		},
		Reasoner: Reasoner{
			BatchProvider: "deepseek",
			ChatProvider:  "anthropic",
		},
		Gate: Gate{
			LTFTimeframes:  []string{"1m", "5m", "15m"},
			ZScoreReturn:   3.0,
			FundingAbs:     0.0008,
			OIDeltaAbs:     0.04,
			CVDZScore:      3.0,
			Cooldown:       Duration{30 * time.Minute},
			PositionAlways: true,
		},
		Execution: Execution{
			Mode:                "autonomous",
			MaxPositionUSD:      5000,
			MaxTotalExposureUSD: 15000,
			MaxPositionPct:      0.10,
			MaxTotalExposurePct: 0.50,
			MaxConcurrent:       5,
			DailyLossKillUSD:    1000,
			MaxPriceDeviation:   0.02,
			PostStopCooldown:    Duration{30 * time.Minute},
		},
		Providers: Providers{
			Anthropic: ProviderCfg{
				Model:   "claude-opus-4-8",
				BaseURL: "https://api.anthropic.com",
				Models:  []string{"claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5-20251001"},
			},
			OpenAI: ProviderCfg{
				Model:   "gpt-4o",
				BaseURL: "https://api.openai.com/v1",
				Models:  []string{"gpt-4o", "gpt-4o-mini"},
			},
			Deepseek: ProviderCfg{
				Model:   "deepseek-chat",
				BaseURL: "https://api.deepseek.com/v1",
				Models:  []string{"deepseek-chat", "deepseek-reasoner"},
			},
		},
		Storage: Storage{
			Dir:         "./data",
			RingSize:    512,
			HistoryBars: 120,
		},
		MarketData: MarketData{
			CSVDir:       "./data/csv",
			UseCoinGecko: true,
		},
		API: API{
			Enabled:     true,
			Addr:        "127.0.0.1:8787",
			CORSOrigins: []string{"http://localhost:5173"},
		},
	}
}

// Load reads config.toml from path, overlaying it on the defaults. A missing
// file is not an error — defaults are returned so the daemon runs out of the box.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes the config to path as TOML, creating parent directories as
// needed. It is the persistence half of the TUI settings modal: API keys, model
// selections, and execution mode survive a restart because they land here.
// The write is atomic (temp file + rename) so a crash mid-write can never
// truncate a config that may hold API keys.
func Save(path string, c Config) error {
	if path == "" {
		return fmt.Errorf("config: no path to save to")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("config: mkdir %s: %w", dir, err)
		}
	}
	var buf bytes.Buffer
	buf.WriteString("# hyperagent configuration — edited by the in-app settings (s); hand edits are preserved on load.\n")
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	tmp := path + ".tmp"
	// 0600: the file can carry provider API keys.
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

func (c Config) validate() error {
	if len(c.Markets.Visualized) == 0 {
		return fmt.Errorf("config: markets.visualized is empty")
	}
	if c.Execution.Mode != "propose" && c.Execution.Mode != "autonomous" {
		return fmt.Errorf("config: execution.mode must be propose|autonomous, got %q", c.Execution.Mode)
	}
	if p := c.Execution.MaxPositionPct; p < 0 || p > 1 {
		return fmt.Errorf("config: execution.max_position_pct must be in [0,1], got %v", p)
	}
	if p := c.Execution.MaxTotalExposurePct; p < 0 || p > 1 {
		return fmt.Errorf("config: execution.max_total_exposure_pct must be in [0,1], got %v", p)
	}
	if c.API.Enabled && c.API.Token == "" && !isLoopbackAddr(c.API.Addr) {
		return fmt.Errorf("api: refusing to bind non-loopback %s without [api] token", c.API.Addr)
	}
	return nil
}

// isLoopbackAddr reports whether addr's host resolves to a loopback address.
// "localhost" is treated as loopback without a DNS lookup, matching the
// common local-dev case even when it's not in /etc/hosts.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
