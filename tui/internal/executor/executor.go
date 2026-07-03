// Package executor is the optional, deterministic, dangerous layer. A reasoner
// verdict is a *request*, not a command. Hard-coded risk gates run before any
// order hits the wire; no LLM output bypasses them — they are code. In propose
// mode, candidates are journaled/alerted and never sent; in autonomous mode,
// passing candidates are signed and submitted.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/signing"
)

// RiskConfig holds the hard limits. These are code, not suggestions.
type RiskConfig struct {
	Mode                string // "propose" | "autonomous"
	MaxPositionUSD      float64
	MaxTotalExposureUSD float64
	MaxConcurrent       int
	DailyLossKillUSD    float64
	MaxPriceDeviation   float64 // allowed |verdict price - live mid| / mid
	PostStopCooldown    time.Duration
}

// AssetIndex resolves a coin to its HL perp asset id.
type AssetIndex map[string]int

// MarketState is what the risk gates need to see: open positions and live
// per-asset context. The daemon passes the live *store.Store; the MCP server
// passes a REST-backed snapshot view. One gate implementation, two feeds.
type MarketState interface {
	Positions() []metrics.Position
	AssetCtx(coin string) (metrics.AssetCtx, bool)
}

// Executor applies risk gates and (in autonomous mode) submits signed orders.
type Executor struct {
	cfg     RiskConfig
	bus     *bus.Bus
	store   MarketState
	journal *journal.Journal
	signer  *signing.Signer
	assets  AssetIndex
	apiURL  string
	mainnet bool
	http    *http.Client

	mu            sync.Mutex
	dailyRealized float64
	dailyResetDay string
	cooldownUntil map[string]time.Time
	killed        bool
}

// New builds an executor. signer may be nil in propose mode (no signing needed).
func New(cfg RiskConfig, b *bus.Bus, s MarketState, j *journal.Journal, signer *signing.Signer, assets AssetIndex, apiURL string, mainnet bool) *Executor {
	return &Executor{
		cfg:           cfg,
		bus:           b,
		store:         s,
		journal:       j,
		signer:        signer,
		assets:        assets,
		apiURL:        apiURL,
		mainnet:       mainnet,
		http:          &http.Client{Timeout: 15 * time.Second},
		cooldownUntil: make(map[string]time.Time),
	}
}

// Mode returns the current execution mode under lock.
func (e *Executor) Mode() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.Mode
}

// SetMode switches execution mode at runtime. Flipping to autonomous is rejected
// unless an agent wallet signer is present — autonomy must never be enabled by a
// stray command when the daemon cannot actually (and safely) sign.
func (e *Executor) SetMode(m string) error {
	if m != "propose" && m != "autonomous" {
		return fmt.Errorf("mode must be propose|autonomous, got %q", m)
	}
	if m == "autonomous" && e.signer == nil {
		return fmt.Errorf("no agent wallet configured; cannot enable autonomous execution")
	}
	e.mu.Lock()
	e.cfg.Mode = m
	e.mu.Unlock()
	return nil
}

// Handle is the verdict hook wired into the reasoning engine. It journals every
// candidate, then either proposes (default) or executes after risk gates.
func (e *Executor) Handle(v reasoner.Verdict) {
	_ = e.journal.Record(journal.Entry{
		Coin:    v.Asset,
		Kind:    "candidate",
		Summary: candidateSummary(v),
		Verdict: &v,
	})

	if !v.Action.IsTrade() {
		return // hold / alert_only: nothing to execute
	}

	// Propose mode, or per-asset confirmation required: never auto-send.
	if e.Mode() != "autonomous" || v.RequiresConfirmation {
		e.bus.PublishJournal(bus.JournalEvent{
			Coin: v.Asset, Kind: "alert",
			Summary: "PROPOSED (awaiting confirmation): " + candidateSummary(v),
			Verdict: &v,
		})
		return
	}

	if err := e.riskCheck(v); err != nil {
		_ = e.journal.Record(journal.Entry{
			Coin: v.Asset, Kind: "error",
			Summary: "risk gate rejected: " + err.Error(), Verdict: &v,
		})
		return
	}

	if err := e.submit(context.Background(), v); err != nil {
		_ = e.journal.Record(journal.Entry{
			Coin: v.Asset, Kind: "error",
			Summary: "submit failed: " + err.Error(), Verdict: &v,
		})
		return
	}
	_ = e.journal.Record(journal.Entry{
		Coin: v.Asset, Kind: "fill", Summary: "submitted: " + candidateSummary(v), Verdict: &v,
	})
}

// Execute is the direct command path used by the MCP interface: the caller
// (a human, or an agent acting on an explicit tool call) IS the confirmation,
// so mode and requires_confirmation are not consulted. The risk gates still
// run — no caller bypasses them, they are code.
func (e *Executor) Execute(ctx context.Context, v reasoner.Verdict) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if !v.Action.IsTrade() {
		return fmt.Errorf("action %q is not executable", v.Action)
	}
	if err := e.riskCheck(v); err != nil {
		_ = e.journal.Record(journal.Entry{
			Coin: v.Asset, Kind: "error",
			Summary: "risk gate rejected (mcp): " + err.Error(), Verdict: &v,
		})
		return fmt.Errorf("risk gate: %w", err)
	}
	if err := e.submit(ctx, v); err != nil {
		_ = e.journal.Record(journal.Entry{
			Coin: v.Asset, Kind: "error",
			Summary: "submit failed (mcp): " + err.Error(), Verdict: &v,
		})
		return err
	}
	_ = e.journal.Record(journal.Entry{
		Coin: v.Asset, Kind: "fill", Summary: "submitted (mcp): " + candidateSummary(v), Verdict: &v,
	})
	return nil
}

// Cancel signs and submits a cancel for one resting order by oid.
func (e *Executor) Cancel(ctx context.Context, coin string, oid uint64) error {
	if e.signer == nil {
		return fmt.Errorf("no signer configured")
	}
	assetID, ok := e.assets[coin]
	if !ok {
		return fmt.Errorf("unknown asset id for %s", coin)
	}
	action := buildCancelAction(assetID, oid)
	nonce := uint64(time.Now().UnixMilli())
	sig, err := e.signer.SignL1Action(action, nonce, nil, e.mainnet)
	if err != nil {
		return err
	}
	if err := e.postExchange(ctx, action, nonce, sig); err != nil {
		return err
	}
	_ = e.journal.Record(journal.Entry{
		Coin: coin, Kind: "alert", Summary: fmt.Sprintf("cancelled oid %d (mcp)", oid),
	})
	return nil
}

// riskCheck enforces every hard limit. Any breach returns an error → reject.
func (e *Executor) riskCheck(v reasoner.Verdict) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.maybeResetDaily()

	if e.killed {
		return fmt.Errorf("daily-loss kill-switch active")
	}
	if until, ok := e.cooldownUntil[v.Asset]; ok && time.Now().Before(until) {
		return fmt.Errorf("post-stop cooldown until %s", until.Format(time.Kitchen))
	}
	if v.SizeUSD > e.cfg.MaxPositionUSD {
		return fmt.Errorf("size %.0f exceeds max position %.0f", v.SizeUSD, e.cfg.MaxPositionUSD)
	}

	// Total exposure across open positions + this candidate.
	var exposure float64
	open := 0
	for _, p := range e.store.Positions() {
		exposure += absf(p.Size) * p.MarkPrice
		open++
	}
	if v.Action == reasoner.ActionOpenLong || v.Action == reasoner.ActionOpenShort {
		exposure += v.SizeUSD
		if open+1 > e.cfg.MaxConcurrent {
			return fmt.Errorf("would exceed max concurrent positions %d", e.cfg.MaxConcurrent)
		}
	}
	if exposure > e.cfg.MaxTotalExposureUSD {
		return fmt.Errorf("total exposure %.0f exceeds max %.0f", exposure, e.cfg.MaxTotalExposureUSD)
	}

	// Sanity: verdict price within X% of live mid.
	if v.Entry.Type == "limit" && v.Entry.Price > 0 {
		if ctx, ok := e.store.AssetCtx(v.Asset); ok && ctx.MarkPrice > 0 {
			dev := absf(v.Entry.Price-ctx.MarkPrice) / ctx.MarkPrice
			if dev > e.cfg.MaxPriceDeviation {
				return fmt.Errorf("price %.4f deviates %.1f%% from mark %.4f (max %.1f%%)",
					v.Entry.Price, dev*100, ctx.MarkPrice, e.cfg.MaxPriceDeviation*100)
			}
		}
	}
	return nil
}

func (e *Executor) maybeResetDaily() {
	day := time.Now().UTC().Format("2006-01-02")
	if day != e.dailyResetDay {
		e.dailyResetDay = day
		e.dailyRealized = 0
		e.killed = false
	}
}

// RecordRealizedPnL updates the daily loss tally and trips the kill-switch when
// the daily loss limit is breached. Called by the position tracker on closes.
func (e *Executor) RecordRealizedPnL(coin string, pnl float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.maybeResetDaily()
	e.dailyRealized += pnl
	if pnl < 0 {
		e.cooldownUntil[coin] = time.Now().Add(e.cfg.PostStopCooldown)
	}
	if -e.dailyRealized >= e.cfg.DailyLossKillUSD {
		e.killed = true
		e.bus.PublishJournal(bus.JournalEvent{
			Coin: coin, Kind: "alert",
			Summary: fmt.Sprintf("DAILY-LOSS KILL-SWITCH TRIPPED: realized %.0f", e.dailyRealized),
		})
	}
}

// submit signs and posts an order to the HL exchange endpoint.
func (e *Executor) submit(ctx context.Context, v reasoner.Verdict) error {
	if e.signer == nil {
		return fmt.Errorf("no signer configured")
	}
	assetID, ok := e.assets[v.Asset]
	if !ok {
		return fmt.Errorf("unknown asset id for %s", v.Asset)
	}

	mark := 0.0
	if c, ok := e.store.AssetCtx(v.Asset); ok {
		mark = c.MarkPrice
	}
	price := v.Entry.Price
	otype := v.Entry.Type
	if otype == "market" || price == 0 {
		otype = "market"
		// Aggressive IOC price: cross the book by the max deviation.
		if v.Action == reasoner.ActionOpenLong {
			price = mark * (1 + e.cfg.MaxPriceDeviation)
		} else {
			price = mark * (1 - e.cfg.MaxPriceDeviation)
		}
	}
	size := v.SizeUSD / price

	order := OrderRequest{
		AssetID:    assetID,
		IsBuy:      v.Action == reasoner.ActionOpenLong || v.Action == reasoner.ActionScale,
		Price:      price,
		Size:       size,
		ReduceOnly: v.Action == reasoner.ActionClose,
		OrderType:  otype,
	}
	action := buildOrderAction([]OrderRequest{order})
	nonce := uint64(time.Now().UnixMilli())

	var vault *common.Address
	sig, err := e.signer.SignL1Action(action, nonce, vault, e.mainnet)
	if err != nil {
		return err
	}
	return e.postExchange(ctx, action, nonce, sig)
}

// postExchange sends a signed action envelope. HL reports rejections in-band as
// HTTP 200 {"status":"err",...}, so both the status code and the body are checked.
func (e *Executor) postExchange(ctx context.Context, action any, nonce uint64, sig signing.Signature) error {
	envelope := map[string]any{
		"action":       action, // *signing.OrderedMap marshals in field order
		"nonce":        nonce,
		"signature":    sig,
		"vaultAddress": nil,
	}
	buf, _ := json.Marshal(envelope)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.apiURL+"/exchange", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("exchange status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("exchange: unparseable response: %s", string(body))
	}
	if out.Status != "ok" {
		return fmt.Errorf("exchange rejected: %s", string(body))
	}
	return nil
}

func candidateSummary(v reasoner.Verdict) string {
	return fmt.Sprintf("%s %s $%.0f @ %s%.4f stop %.4f tp %.4f conf %.2f — %s",
		v.Action, v.Asset, v.SizeUSD, v.Entry.Type, v.Entry.Price, v.Stop, v.TakeProfit, v.Confidence, v.Thesis)
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

var _ = metrics.SideNone // keep metrics import for shared types
