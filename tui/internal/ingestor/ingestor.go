// Package ingestor owns the single Hyperliquid WebSocket. It subscribes to the
// public feeds (trades, activeAssetCtx, allMids), stamps each frame with a
// monotonic receive time, decodes to a typed internal event, and pushes it onto
// the bus. No logic — just transport.
//
// Resilience: a watchdog tracks last-message time; silent past a threshold →
// reconnect + resubscribe. The gorilla library handles ping/pong; the watchdog
// handles silent death. This is the "single static binary, native channel
// backpressure" reliability the plan wants over a Python asyncio stack.
package ingestor

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

const (
	MainnetWS = "wss://api.hyperliquid.xyz/ws"
	TestnetWS = "wss://api.hyperliquid-testnet.xyz/ws"

	silenceTimeout = 30 * time.Second
	reconnectDelay = 2 * time.Second
)

// Ingestor manages the websocket lifecycle for a set of coins.
type Ingestor struct {
	url     string
	bus     *bus.Bus
	lastMsg atomic.Int64 // unix-nano of last frame received

	mu    sync.Mutex // guards coins + conn (shared with Subscribe)
	coins []string
	conn  *websocket.Conn // current live connection, nil when disconnected
}

// New builds an ingestor for the given WS url and coin list.
func New(url string, coins []string, b *bus.Bus) *Ingestor {
	if url == "" {
		url = MainnetWS
	}
	return &Ingestor{url: url, coins: append([]string(nil), coins...), bus: b}
}

// perCoinFeeds are the per-coin subscriptions opened for every tracked coin.
var perCoinFeeds = []string{"trades", "activeAssetCtx"}

// Subscribe adds coins to the watchlist at runtime and, if currently connected,
// opens their feeds on the live socket immediately. New coins are remembered so a
// later reconnect resubscribes them. This is what the TUI's /watch command calls.
func (in *Ingestor) Subscribe(coins ...string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	have := make(map[string]bool, len(in.coins))
	for _, c := range in.coins {
		have[c] = true
	}
	for _, c := range coins {
		if c == "" || have[c] {
			continue
		}
		in.coins = append(in.coins, c)
		have[c] = true
		if in.conn != nil {
			for _, typ := range perCoinFeeds {
				_ = in.conn.WriteJSON(subscribeMsg{Method: "subscribe", Subscription: subscriptionDef{Type: typ, Coin: c}})
			}
		}
	}
}

// subscribeMsg is a HL websocket subscription frame.
type subscribeMsg struct {
	Method       string          `json:"method"`
	Subscription subscriptionDef `json:"subscription"`
}

type subscriptionDef struct {
	Type string `json:"type"`
	Coin string `json:"coin,omitempty"`
}

// Run connects, subscribes, and reads frames until ctx is cancelled, reconnecting
// on error or silence. It blocks.
func (in *Ingestor) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		in.connectAndRead(ctx)
		in.bus.PublishStatus(bus.StatusEvent{Kind: bus.StatusConn, Connected: false, Detail: "reconnecting"})
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (in *Ingestor) connectAndRead(ctx context.Context) {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, in.url, nil)
	if err != nil {
		in.bus.PublishStatus(bus.StatusEvent{Kind: bus.StatusConn, Connected: false, Detail: "dial failed: " + err.Error()})
		return
	}
	defer conn.Close()

	// Publish the live conn so runtime Subscribe() can write to it, and snapshot
	// the current coin list under the same lock.
	in.mu.Lock()
	in.conn = conn
	coins := append([]string(nil), in.coins...)
	in.mu.Unlock()
	defer func() {
		in.mu.Lock()
		in.conn = nil
		in.mu.Unlock()
	}()

	// Subscribe to the public feeds per coin.
	for _, coin := range coins {
		for _, typ := range perCoinFeeds {
			frame := subscribeMsg{Method: "subscribe", Subscription: subscriptionDef{Type: typ, Coin: coin}}
			if err := conn.WriteJSON(frame); err != nil {
				return
			}
		}
	}
	// allMids is a single global subscription (no coin).
	_ = conn.WriteJSON(subscribeMsg{Method: "subscribe", Subscription: subscriptionDef{Type: "allMids"}})

	in.lastMsg.Store(time.Now().UnixNano())
	in.bus.PublishStatus(bus.StatusEvent{Kind: bus.StatusConn, Connected: true, Detail: "subscribed"})

	// Watchdog goroutine: close the conn if it goes silent, forcing a reconnect.
	wdCtx, cancelWD := context.WithCancel(ctx)
	defer cancelWD()
	go in.watchdog(wdCtx, conn)

	for {
		if ctx.Err() != nil {
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		in.lastMsg.Store(time.Now().UnixNano())
		in.dispatch(data)
	}
}

func (in *Ingestor) watchdog(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			last := time.Unix(0, in.lastMsg.Load())
			if time.Since(last) > silenceTimeout {
				in.bus.PublishStatus(bus.StatusEvent{Kind: bus.StatusConn, Connected: false, Detail: "watchdog: silent, forcing reconnect"})
				conn.Close() // unblocks ReadMessage with an error
				return
			}
		}
	}
}

// wsEnvelope is the generic HL frame: {"channel": "...", "data": {...}}.
type wsEnvelope struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

func (in *Ingestor) dispatch(raw []byte) {
	var env wsEnvelope
	if json.Unmarshal(raw, &env) != nil {
		return
	}
	recv := time.Now()
	switch env.Channel {
	case "trades":
		in.handleTrades(env.Data, recv)
	case "activeAssetCtx":
		in.handleAssetCtx(env.Data, recv)
	case "allMids":
		in.handleAllMids(env.Data, recv)
	}
}

// allMidsWire is the allMids frame: {"mids": {"BTC": "95000.0", ...}}.
type allMidsWire struct {
	Mids map[string]string `json:"mids"`
}

func (in *Ingestor) handleAllMids(data json.RawMessage, recv time.Time) {
	var w allMidsWire
	if json.Unmarshal(data, &w) != nil || len(w.Mids) == 0 {
		return
	}
	mids := make(map[string]float64, len(w.Mids))
	for coin, px := range w.Mids {
		mids[coin] = atof(px)
	}
	in.bus.PublishMids(metrics.MidSnapshot{Mids: mids, Time: recv})
}

// wire shapes ---------------------------------------------------------------

type tradeWire struct {
	Coin string `json:"coin"`
	Side string `json:"side"` // "B" (buy/bid aggressor) or "A" (ask/sell)
	Px   string `json:"px"`
	Sz   string `json:"sz"`
	Time int64  `json:"time"`
}

func (in *Ingestor) handleTrades(data json.RawMessage, recv time.Time) {
	var trades []tradeWire
	if json.Unmarshal(data, &trades) != nil {
		return
	}
	for _, t := range trades {
		side := metrics.SideNone
		switch t.Side {
		case "B":
			side = metrics.SideBuy
		case "A":
			side = metrics.SideSell
		}
		in.bus.PublishTrade(metrics.Trade{
			Coin:     t.Coin,
			Price:    atof(t.Px),
			Size:     atof(t.Sz),
			Side:     side,
			Time:     time.UnixMilli(t.Time),
			RecvTime: recv,
		})
	}
}

type assetCtxWire struct {
	Coin string `json:"coin"`
	Ctx  struct {
		Funding      string `json:"funding"`
		OpenInterest string `json:"openInterest"`
		MarkPx       string `json:"markPx"`
		OraclePx     string `json:"oraclePx"`
		Premium      string `json:"premium"`
		DayNtlVlm    string `json:"dayNtlVlm"`
	} `json:"ctx"`
}

func (in *Ingestor) handleAssetCtx(data json.RawMessage, recv time.Time) {
	var w assetCtxWire
	if json.Unmarshal(data, &w) != nil {
		return
	}
	c := metrics.AssetCtx{
		Coin:         w.Coin,
		MarkPrice:    atof(w.Ctx.MarkPx),
		OraclePrice:  atof(w.Ctx.OraclePx),
		Funding:      atof(w.Ctx.Funding),
		OpenInterest: atof(w.Ctx.OpenInterest),
		Premium:      atof(w.Ctx.Premium),
		DayVolume:    atof(w.Ctx.DayNtlVlm),
		Time:         recv,
	}
	in.bus.PublishAssetCtx(c)
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
