// Command hyperagent is the single static binary: an autonomous Hyperliquid
// scanner and reasoning engine, headless daemon plus HTTP+WS API. It wires
// every component over the typed event bus, one goroutine per component,
// context for shutdown.
//
// Build order maps to the plan's stages; this entrypoint runs stages 1–3 live
// (ingest → aggregate → store → batch → gate → reason → journal) and wires
// stage 4 (executor) in propose mode by default. Autonomous execution requires
// both config mode=autonomous and an agent wallet key. Every frontend —
// standalone TUI included — attaches to the API server, not this process.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hyperagent/hyperagent/internal/aggregator"
	"github.com/hyperagent/hyperagent/internal/api"
	"github.com/hyperagent/hyperagent/internal/batcher"
	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/executor"
	"github.com/hyperagent/hyperagent/internal/gate"
	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/ingestor"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/marketdata"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/signing"
	"github.com/hyperagent/hyperagent/internal/store"
	"github.com/hyperagent/hyperagent/internal/telegram"
)

// version is the daemon build identifier reported by /api/health and the MCP
// server's initialize response — bump alongside releases.
const version = "0.1.0"

func main() {
	loadDotEnv(".env") // before flag defaults: -agent-key reads HL_AGENT_KEY

	// Subcommands: one-shot ops that don't start the daemon.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "approve-agent":
			if err := runApproveAgent(os.Args[2:]); err != nil {
				log.Fatalf("approve-agent: %v", err)
			}
			return
		case "mcp":
			if err := runMCP(os.Args[2:]); err != nil {
				log.Fatalf("mcp: %v", err)
			}
			return
		}
	}

	var (
		configPath = flag.String("config", "config.toml", "path to config.toml")
		testnet    = flag.Bool("testnet", false, "use Hyperliquid testnet endpoints")
		agentKey   = flag.String("agent-key", os.Getenv("HL_AGENT_KEY"), "agent wallet private key (autonomous execution)")
	)
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := run(cfg, *configPath, *testnet, *agentKey); err != nil {
		log.Fatalf("hyperagent: %v", err)
	}
}

// loadDotEnv reads KEY=VALUE lines from path into the process environment so
// provider API keys live in .env instead of the shell profile. Real environment
// variables win over file entries; a missing file is not an error.
func loadDotEnv(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if k != "" && os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func run(cfg config.Config, configPath string, testnet bool, agentKey string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	apiURL, wsURL := hlclient.MainnetAPI, ingestor.MainnetWS
	if testnet {
		apiURL, wsURL = hlclient.TestnetAPI, ingestor.TestnetWS
	}

	// --- Core infra ---
	b := bus.New()
	st, err := store.New(cfg.Storage.Dir, cfg.Storage.RingSize)
	if err != nil {
		return err
	}
	jr, err := journal.New(b, cfg.Storage.Dir)
	if err != nil {
		return err
	}
	rest := hlclient.New(apiURL)

	// Build the per-coin timeframe sets the aggregator folds.
	tfByCoin := buildTimeframes(cfg)
	agg := aggregator.New(b, st, tfByCoin, 1e6)

	// Warm rings from disk + REST candleSnapshot + CSV/CoinGecko so context is
	// immediate, even offline or for assets HL's candleSnapshot is thin on.
	md := marketdata.New(marketdata.Config{
		CSVDir:       cfg.MarketData.CSVDir,
		UseCoinGecko: cfg.MarketData.UseCoinGecko,
		IDOverrides:  cfg.MarketData.IDs,
	})
	warmUp(ctx, cfg, rest, md, st)

	// --- Reasoner ---
	reg := buildReasoner(cfg)

	// --- Executor (stage 4, propose by default) ---
	exec := buildExecutor(ctx, cfg, b, st, jr, rest, agentKey, testnet)

	onVerdict := func(v metrics.Verdict) {
		if exec != nil {
			exec.Handle(v)
		} else {
			// No executor: still journal the candidate.
			_ = jr.Record(journal.Entry{Coin: v.Asset, Kind: "candidate",
				Summary: v.Thesis, Verdict: &v})
		}
	}

	// --- Gate + batcher + reasoning engine ---
	g := gate.New(b, gate.DefaultRules())
	strategies := buildStrategies(cfg)
	bt := batcher.New(b, st, jr, strategies, cfg.Storage.HistoryBars)
	engine := reasoner.NewEngine(b, reg, g.Out(), onVerdict)

	// --- Ingestor ---
	in := ingestor.New(wsURL, cfg.Markets.Visualized, b)

	// --- Telegram (optional) ---
	// Approve/reject route through the executor's shared proposal registry —
	// the same confirm flow the HTTP API's /api/proposals endpoints use.
	if cfg.Telegram.Enabled && cfg.Telegram.BotToken != "" {
		tg := telegram.New(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
		tg.OnApprove(func(id string) {
			if err := exec.Approve(ctx, id); err != nil {
				log.Printf("telegram approve %s: %v", id, err)
			}
		})
		tg.OnReject(func(id string) {
			if err := exec.Reject(id); err != nil {
				log.Printf("telegram reject %s: %v", id, err)
			}
		})
		go tg.Mirror(ctx, b)
		go tg.PollCallbacks(ctx)
	}

	// --- API server (unified backend core): the surface every frontend
	// (standalone TUI included) attaches to. Constructed (and its status/bus
	// subscriptions live) before the pipeline goroutines below start
	// publishing, so the initial status event isn't missed. Runs until ctx is
	// cancelled (SIGINT/SIGTERM).
	if cfg.API.Enabled {
		var cfgMu sync.Mutex
		srv := api.NewServer(api.Deps{
			Bus:        b,
			Store:      st,
			Engine:     engine,
			Exec:       exec,
			Ingestor:   in,
			Batcher:    bt,
			RestClient: rest,
			Cfg:        cfg,
			Version:    version,
			CfgSnapshot: func() config.Config {
				cfgMu.Lock()
				defer cfgMu.Unlock()
				return cfg
			},
			SaveConfig: func(apply func(*config.Config)) error {
				cfgMu.Lock()
				defer cfgMu.Unlock()
				apply(&cfg)
				return config.Save(configPath, cfg)
			},
		})
		go func() {
			if err := srv.ListenAndServe(ctx); err != nil {
				log.Printf("api server: %v", err)
			}
		}()
	}

	// --- Launch the pipeline goroutines (one per component) ---
	go in.Run(ctx)
	go agg.Run(ctx)
	go g.Run(ctx)
	go bt.Run(ctx)
	go engine.Run(ctx)

	b.PublishStatus(bus.StatusEvent{Kind: bus.StatusConn, Connected: false, Provider: cfg.Reasoner.ChatProvider, Mode: cfg.Execution.Mode, Detail: "starting"})

	<-ctx.Done()
	return nil
}

// buildTimeframes returns, per visualized coin, the timeframes the aggregator
// should fold: always the display set plus the tracked decision timeframe.
func buildTimeframes(cfg config.Config) map[string][]aggregator.Timeframe {
	displaySet := []string{"15m", "1h", "4h", "1d"}
	out := make(map[string][]aggregator.Timeframe)
	for _, coin := range cfg.Markets.Visualized {
		seen := map[string]bool{}
		var tfs []aggregator.Timeframe
		add := func(name string) {
			if seen[name] {
				return
			}
			if tf, ok := aggregator.ParseTimeframe(name); ok {
				tfs = append(tfs, tf)
				seen[name] = true
			}
		}
		for _, d := range displaySet {
			add(d)
		}
		add(cfg.Timeframe.For(coin))
		out[coin] = tfs
	}
	return out
}

// buildStrategies maps each tracked coin to its per-asset reasoning strategy.
func buildStrategies(cfg config.Config) map[string]metrics.AssetStrategy {
	out := make(map[string]metrics.AssetStrategy)
	confirm := cfg.Execution.Mode != "autonomous"
	for _, coin := range cfg.Markets.Tracked {
		out[coin] = metrics.AssetStrategy{
			Coin:                 coin,
			Timeframe:            cfg.Timeframe.For(coin),
			RequiresConfirmation: confirm,
			MaxPositionUSD:       cfg.Execution.MaxPositionUSD,
		}
	}
	return out
}

// buildReasoner constructs the provider registry from config. It registers the
// three named providers plus any number of custom OpenAI-compatible endpoints,
// resolving keys from the config or the environment. Providers with no key are
// still registered (calls error and are logged) so the system runs dry without
// secrets — and the active provider can be switched live via /provider.
func buildReasoner(cfg config.Config) *reasoner.Registry {
	providers := map[string]reasoner.Provider{}
	models := map[string][]string{} // provider name -> known model ids for the picker
	p := cfg.Providers

	// register wires a provider and records its known model ids (configured default
	// first, so it heads the picker list).
	register := func(name string, prov reasoner.Provider, pc config.ProviderCfg) {
		providers[name] = prov
		models[name] = mergeModels(pc.Model, pc.Models)
	}

	// Named providers: Anthropic speaks its own protocol; OpenAI + Deepseek are
	// the OpenAI-compatible adapter. Always registered so role selection resolves.
	register("anthropic", reasoner.NewAnthropic(p.Anthropic.Key("ANTHROPIC_API_KEY"), p.Anthropic.Model, p.Anthropic.BaseURL), p.Anthropic)
	register("openai", reasoner.NewOpenAICompatible("openai", p.OpenAI.Key("OPENAI_API_KEY"), p.OpenAI.Model, p.OpenAI.BaseURL), p.OpenAI)
	register("deepseek", reasoner.NewOpenAICompatible("deepseek", p.Deepseek.Key("DEEPSEEK_API_KEY"), p.Deepseek.Model, p.Deepseek.BaseURL), p.Deepseek)

	// Custom providers: any OpenAI-compatible (or Anthropic-protocol) endpoint,
	// registered by its config key. This is the provider-agnostic surface.
	for name, pc := range p.Custom {
		envDefault := strings.ToUpper(name) + "_API_KEY"
		key := pc.Key(envDefault)
		if pc.Kind == "anthropic" {
			register(name, reasoner.NewAnthropic(key, pc.Model, pc.BaseURL), pc)
		} else {
			register(name, reasoner.NewOpenAICompatible(name, key, pc.Model, pc.BaseURL), pc)
		}
	}

	// Resolve each role's model: explicit config override, else the provider's
	// default (first known model). The registry then binds (provider, model) per role.
	batchModel := firstNonEmpty(cfg.Reasoner.BatchModel, first(models[cfg.Reasoner.BatchProvider]))
	chatModel := firstNonEmpty(cfg.Reasoner.ChatModel, first(models[cfg.Reasoner.ChatProvider]))

	return reasoner.NewRegistry(providers, models, cfg.Reasoner.BatchProvider, batchModel, cfg.Reasoner.ChatProvider, chatModel)
}

// mergeModels returns a provider's known model ids with its default model first and
// duplicates removed, so the picker leads with the configured default.
func mergeModels(def string, list []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	add(def)
	for _, m := range list {
		add(m)
	}
	return out
}

func first(xs []string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return ""
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// buildExecutor wires the executor. In propose mode no signer is required; in
// autonomous mode an agent key must be supplied or the daemon stays in propose.
// The signer itself is built whenever a key is present regardless of mode:
// propose mode still needs it to sign an order once a human approves a
// proposal (via Telegram or the API) — only the auto-submit-without-confirmation
// behavior is gated on mode, not the ability to sign at all.
func buildExecutor(ctx context.Context, cfg config.Config, b *bus.Bus, st *store.Store, jr *journal.Journal, rest *hlclient.Client, agentKey string, testnet bool) *executor.Executor {
	risk := executor.RiskConfig{
		Mode:                cfg.Execution.Mode,
		MaxPositionUSD:      cfg.Execution.MaxPositionUSD,
		MaxTotalExposureUSD: cfg.Execution.MaxTotalExposureUSD,
		MaxConcurrent:       cfg.Execution.MaxConcurrent,
		DailyLossKillUSD:    cfg.Execution.DailyLossKillUSD,
		MaxPriceDeviation:   cfg.Execution.MaxPriceDeviation,
		PostStopCooldown:    cfg.Execution.PostStopCooldown.Duration,
	}

	var signer *signing.Signer
	if agentKey != "" {
		s, err := signing.NewSigner(agentKey)
		if err != nil {
			log.Printf("agent key invalid (%v); execution stays unsigned", err)
		} else {
			signer = s
		}
	}
	if cfg.Execution.Mode == "autonomous" && signer == nil {
		log.Printf("execution.mode=autonomous but no valid agent key supplied; forcing propose mode")
		risk.Mode = "propose"
	}

	apiURL := hlclient.MainnetAPI
	if testnet {
		apiURL = hlclient.TestnetAPI
	}
	assetIdx := buildAssetIndex(ctx, rest, cfg.Markets.Tracked)
	return executor.New(risk, b, st, jr, signer, assetIdx, apiURL, !testnet)
}

// buildAssetIndex resolves tracked coins to HL perp asset ids via meta. The
// asset id is the coin's POSITION in the meta universe array — never derived
// from map iteration. Best effort; coins not found simply can't be auto-executed.
func buildAssetIndex(ctx context.Context, rest *hlclient.Client, tracked []string) executor.AssetIndex {
	idx := executor.AssetIndex{}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	universe, err := rest.Universe(cctx)
	if err != nil {
		return idx
	}
	want := make(map[string]bool, len(tracked))
	for _, t := range tracked {
		want[t] = true
	}
	for i, coin := range universe {
		if want[coin] {
			idx[coin] = i
		}
	}
	return idx
}

// warmUp seeds rings so the agent has context immediately, not after hours of
// uptime. Per (coin, timeframe) it layers, oldest source first: on-disk history,
// then the best available backfill — HL's native candleSnapshot when it returns
// a full series, otherwise the CSV/CoinGecko market-data source. The store's ring
// dedupes by open-time, so overlapping sources merge cleanly.
func warmUp(ctx context.Context, cfg config.Config, rest *hlclient.Client, md *marketdata.Source, st *store.Store) {
	end := time.Now()
	want := cfg.Storage.HistoryBars
	for _, coin := range cfg.Markets.Visualized {
		seen := map[string]bool{}
		for _, tf := range []string{cfg.Timeframe.For(coin), "1h"} {
			if seen[tf] {
				continue
			}
			seen[tf] = true

			// Disk first (fastest, already-derived metrics).
			if disk, _ := st.LoadHistory(coin, tf, want); len(disk) > 0 {
				st.WarmRing(coin, tf, disk)
			}

			// Native HL candleSnapshot (real perp OHLCV with volume).
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			start := end.Add(-time.Duration(want) * tfDur(tf))
			hlBars, err := rest.CandleSnapshot(cctx, coin, tf, start, end)
			cancel()
			if err == nil && len(hlBars) >= want/2 {
				st.WarmRing(coin, tf, hlBars)
				log.Printf("warmup %s %s: %d bars (hyperliquid)", coin, tf, len(hlBars))
				continue
			}

			// HL thin or unavailable → CSV/CoinGecko fallback.
			if md.Enabled() {
				cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				mdBars, src, err := md.Backfill(cctx, coin, tf, want)
				cancel()
				if err != nil {
					log.Printf("warmup %s %s: marketdata: %v", coin, tf, err)
				} else if len(mdBars) > 0 {
					st.WarmRing(coin, tf, mdBars)
					log.Printf("warmup %s %s: %d bars (%s)", coin, tf, len(mdBars), src)
					continue
				}
			}
			if len(hlBars) > 0 { // partial HL data is better than none
				st.WarmRing(coin, tf, hlBars)
				log.Printf("warmup %s %s: %d bars (hyperliquid, partial)", coin, tf, len(hlBars))
			}
		}
	}
	// Seed current perp context.
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if ctxs, err := rest.MetaAndAssetCtxs(cctx); err == nil {
		for _, c := range ctxs {
			st.PutAssetCtx(c)
		}
	}
}

func tfDur(tf string) time.Duration {
	if t, ok := aggregator.ParseTimeframe(tf); ok {
		return t.Dur
	}
	return time.Hour
}
