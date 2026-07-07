// mcp: a Model Context Protocol stdio server exposing Hyperliquid trading as
// tools, so any MCP client (Claude Code, Claude Desktop, other agents) can read
// markets and — through the SAME hard-coded risk gates as the daemon — place and
// cancel orders. No MCP SDK: the protocol is newline-delimited JSON-RPC 2.0 over
// stdio, which stdlib covers, matching the repo's dependency posture.
//
//	claude mcp add hypertrader -- ./hyperagent mcp -address 0xYOURMASTER
//
// Reads: no key needed. Trading: HL_AGENT_KEY (approve one with
// `hyperagent approve-agent`) and -address (master account, for exposure gates).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/executor"
	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/signing"
)

const mcpProtocolVersion = "2024-11-05"

// ---- REST-backed MarketState -------------------------------------------------

// restState feeds the executor's risk gates from REST snapshots instead of the
// live daemon store. Cached briefly so a burst of gate checks costs one fetch.
type restState struct {
	rest    *hlclient.Client
	address string // master account; empty → no position visibility

	mu       sync.Mutex
	posAt    time.Time
	pos      []metrics.Position
	acct     float64 // venue equity from the same snapshot as pos
	ctxAt    time.Time
	ctxs     map[string]metrics.AssetCtx
	cacheTTL time.Duration
}

func newRestState(rest *hlclient.Client, address string) *restState {
	return &restState{rest: rest, address: address, cacheTTL: 2 * time.Second}
}

func (r *restState) Positions() []metrics.Position {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.address == "" {
		return nil
	}
	if time.Since(r.posAt) < r.cacheTTL {
		return r.pos
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := r.rest.ClearinghouseState(ctx, r.address)
	if err != nil {
		return r.pos // stale beats blind
	}
	r.pos, r.acct, r.posAt = st.Positions, st.AccountValue, time.Now()
	return r.pos
}

// AccountValue reports venue equity through the same cached snapshot the
// position view uses; 0 without an address, so the capital-relative gates
// fail closed rather than sizing blind.
func (r *restState) AccountValue() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.address == "" {
		return 0
	}
	if time.Since(r.posAt) < r.cacheTTL {
		return r.acct
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := r.rest.ClearinghouseState(ctx, r.address)
	if err != nil {
		return r.acct // stale beats blind
	}
	r.pos, r.acct, r.posAt = st.Positions, st.AccountValue, time.Now()
	return r.acct
}

func (r *restState) AssetCtx(coin string) (metrics.AssetCtx, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.ctxAt) >= r.cacheTTL {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctxs, err := r.rest.MetaAndAssetCtxs(ctx)
		if err == nil {
			r.ctxs, r.ctxAt = ctxs, time.Now()
		}
	}
	c, ok := r.ctxs[coin]
	return c, ok
}

// ---- server ------------------------------------------------------------------

type mcpServer struct {
	rest    *hlclient.Client
	exec    *executor.Executor
	state   *restState
	address string
	mainnet bool
}

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	configPath := fs.String("config", "config.toml", "path to config.toml (risk limits)")
	testnet := fs.Bool("testnet", false, "use Hyperliquid testnet")
	address := fs.String("address", os.Getenv("HL_MASTER_ADDRESS"), "master account address (account state + exposure gates)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	apiURL := hlclient.MainnetAPI
	if *testnet {
		apiURL = hlclient.TestnetAPI
	}
	rest := hlclient.New(apiURL)
	state := newRestState(rest, *address)

	b := bus.New()
	jr, err := journal.New(b, cfg.Storage.Dir)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}

	var signer *signing.Signer
	if k := strings.TrimSpace(os.Getenv("HL_AGENT_KEY")); k != "" {
		s, err := signing.NewSigner(k)
		if err != nil {
			return fmt.Errorf("HL_AGENT_KEY invalid: %w", err)
		}
		signer = s
	}

	ctx := context.Background()
	risk := executor.RiskConfig{
		Mode:                "autonomous", // MCP tool calls are explicit commands; gates still apply
		MaxPositionUSD:      cfg.Execution.MaxPositionUSD,
		MaxTotalExposureUSD: cfg.Execution.MaxTotalExposureUSD,
		MaxPositionPct:      cfg.Execution.MaxPositionPct,
		MaxTotalExposurePct: cfg.Execution.MaxTotalExposurePct,
		MaxConcurrent:       cfg.Execution.MaxConcurrent,
		DailyLossKillUSD:    cfg.Execution.DailyLossKillUSD,
		MaxPriceDeviation:   cfg.Execution.MaxPriceDeviation,
		PostStopCooldown:    cfg.Execution.PostStopCooldown.Duration,
	}
	assetIdx := buildAssetIndex(ctx, rest, allCoins(cfg))
	exec := executor.New(risk, b, state, jr, signer, assetIdx, apiURL, !*testnet)

	srv := &mcpServer{rest: rest, exec: exec, state: state, address: *address, mainnet: !*testnet}
	return srv.serve(os.Stdin, os.Stdout)
}

func allCoins(cfg config.Config) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range append(append([]string{}, cfg.Markets.Visualized...), cfg.Markets.Tracked...) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// ---- JSON-RPC plumbing ---------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *mcpServer) serve(in *os.File, out *os.File) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue // not JSON-RPC; ignore
		}
		if req.ID == nil {
			continue // notification (e.g. notifications/initialized): no reply
		}
		result, rpcErr := s.dispatch(req)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *mcpServer) dispatch(req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "hypertrader", "version": "0.1.0"},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "bad params"}
		}
		text, err := s.callTool(p.Name, p.Arguments)
		if err != nil {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}},
				"isError": true,
			}, nil
		}
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

// ---- tools ---------------------------------------------------------------------

func toolDefs() []map[string]any {
	obj := func(props map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	return []map[string]any{
		{
			"name":        "get_markets",
			"description": "Live perp market snapshot (mid, mark, funding, open interest, premium, 24h volume) for the given coins, or the configured watchlist when omitted.",
			"inputSchema": obj(map[string]any{
				"coins": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Coin symbols, e.g. [\"BTC\",\"HYPE\"]"},
			}),
		},
		{
			"name":        "get_candles",
			"description": "Historical OHLCV candles for one coin. Intervals: 1m 5m 15m 1h 4h 1d.",
			"inputSchema": obj(map[string]any{
				"coin":     map[string]any{"type": "string"},
				"interval": map[string]any{"type": "string", "default": "1h"},
				"bars":     map[string]any{"type": "integer", "default": 50, "maximum": 500},
			}, "coin"),
		},
		{
			"name":        "get_account",
			"description": "Master account state: open positions, account value, exposure, withdrawable.",
			"inputSchema": obj(map[string]any{}),
		},
		{
			"name":        "get_open_orders",
			"description": "Resting (unfilled) orders on the master account.",
			"inputSchema": obj(map[string]any{}),
		},
		{
			"name":        "place_order",
			"description": "Place a perp order through the hard-coded risk gates (max size, max exposure, max concurrent, daily-loss kill-switch, price sanity). Requires HL_AGENT_KEY. Rejections return the specific gate that failed.",
			"inputSchema": obj(map[string]any{
				"coin":       map[string]any{"type": "string"},
				"action":     map[string]any{"type": "string", "enum": []string{"open_long", "open_short", "close", "scale"}},
				"size_usd":   map[string]any{"type": "number", "description": "Notional size in USD"},
				"order_type": map[string]any{"type": "string", "enum": []string{"limit", "market"}, "default": "limit"},
				"price":      map[string]any{"type": "number", "description": "Limit price; omit for market"},
				"stop":       map[string]any{"type": "number", "description": "Stop level (journaled)"},
				"take_profit": map[string]any{"type": "number", "description": "Target (journaled)"},
				"thesis":     map[string]any{"type": "string", "description": "Why — journaled for the audit trail"},
			}, "coin", "action", "size_usd"),
		},
		{
			"name":        "cancel_order",
			"description": "Cancel one resting order by coin and oid (from get_open_orders). Requires HL_AGENT_KEY.",
			"inputSchema": obj(map[string]any{
				"coin": map[string]any{"type": "string"},
				"oid":  map[string]any{"type": "integer"},
			}, "coin", "oid"),
		},
	}
}

func (s *mcpServer) callTool(name string, args json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}

	switch name {
	case "get_markets":
		var a struct {
			Coins []string `json:"coins"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		return s.getMarkets(ctx, a.Coins)

	case "get_candles":
		var a struct {
			Coin     string `json:"coin"`
			Interval string `json:"interval"`
			Bars     int    `json:"bars"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		if a.Interval == "" {
			a.Interval = "1h"
		}
		if a.Bars <= 0 {
			a.Bars = 50
		}
		if a.Bars > 500 {
			a.Bars = 500
		}
		return s.getCandles(ctx, a.Coin, a.Interval, a.Bars)

	case "get_account":
		if s.address == "" {
			return "", fmt.Errorf("no master address configured (start mcp with -address or HL_MASTER_ADDRESS)")
		}
		st, err := s.rest.ClearinghouseState(ctx, s.address)
		if err != nil {
			return "", err
		}
		return jsonText(st)

	case "get_open_orders":
		if s.address == "" {
			return "", fmt.Errorf("no master address configured (start mcp with -address or HL_MASTER_ADDRESS)")
		}
		oo, err := s.rest.OpenOrders(ctx, s.address)
		if err != nil {
			return "", err
		}
		return jsonText(oo)

	case "place_order":
		var a struct {
			Coin       string  `json:"coin"`
			Action     string  `json:"action"`
			SizeUSD    float64 `json:"size_usd"`
			OrderType  string  `json:"order_type"`
			Price      float64 `json:"price"`
			Stop       float64 `json:"stop"`
			TakeProfit float64 `json:"take_profit"`
			Thesis     string  `json:"thesis"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		if a.OrderType == "" {
			if a.Price > 0 {
				a.OrderType = "limit"
			} else {
				a.OrderType = "market"
			}
		}
		v := reasoner.Verdict{
			Asset:      strings.ToUpper(a.Coin),
			Timeframe:  "mcp",
			Action:     metrics.Action(a.Action),
			SizeUSD:    a.SizeUSD,
			Entry:      metrics.Entry{Type: a.OrderType, Price: a.Price},
			Stop:       a.Stop,
			TakeProfit: a.TakeProfit,
			Thesis:     firstNonEmpty(a.Thesis, "mcp direct order"),
			Confidence: 1,
			At:         time.Now(),
			Provider:   "mcp",
		}
		if err := s.exec.Execute(ctx, v); err != nil {
			return "", err
		}
		return fmt.Sprintf("submitted: %s %s $%.0f (%s) — journaled", a.Action, v.Asset, a.SizeUSD, a.OrderType), nil

	case "cancel_order":
		var a struct {
			Coin string `json:"coin"`
			OID  uint64 `json:"oid"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		if err := s.exec.Cancel(ctx, strings.ToUpper(a.Coin), a.OID); err != nil {
			return "", err
		}
		return fmt.Sprintf("cancelled oid %d on %s", a.OID, a.Coin), nil

	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (s *mcpServer) getMarkets(ctx context.Context, coins []string) (string, error) {
	mids, err := s.rest.AllMids(ctx)
	if err != nil {
		return "", err
	}
	ctxs, err := s.rest.MetaAndAssetCtxs(ctx)
	if err != nil {
		return "", err
	}
	if len(coins) == 0 {
		for c := range ctxs {
			coins = append(coins, c)
		}
	}
	type row struct {
		Coin         string  `json:"coin"`
		Mid          float64 `json:"mid"`
		Mark         float64 `json:"mark"`
		Funding      float64 `json:"funding"`
		OpenInterest float64 `json:"open_interest"`
		Premium      float64 `json:"premium"`
		DayVolumeUSD float64 `json:"day_volume_usd"`
	}
	var rows []row
	for _, c := range coins {
		c = strings.ToUpper(c)
		ac, ok := ctxs[c]
		if !ok {
			continue
		}
		rows = append(rows, row{
			Coin: c, Mid: mids[c], Mark: ac.MarkPrice, Funding: ac.Funding,
			OpenInterest: ac.OpenInterest, Premium: ac.Premium, DayVolumeUSD: ac.DayVolume,
		})
	}
	return jsonText(rows)
}

func (s *mcpServer) getCandles(ctx context.Context, coin, interval string, n int) (string, error) {
	if coin == "" {
		return "", fmt.Errorf("coin required")
	}
	dur, err := intervalDuration(interval)
	if err != nil {
		return "", err
	}
	end := time.Now()
	start := end.Add(-time.Duration(n) * dur)
	bars, err := s.rest.CandleSnapshot(ctx, strings.ToUpper(coin), interval, start, end)
	if err != nil {
		return "", err
	}
	if len(bars) > n {
		bars = bars[len(bars)-n:]
	}
	return jsonText(bars)
}

func intervalDuration(iv string) (time.Duration, error) {
	switch iv {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("unsupported interval %q (use 1m 5m 15m 1h 4h 1d)", iv)
}

func jsonText(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
