// The channel→Msg bridge: a real WebSocket client consuming the daemon's
// /api/ws push stream. Bar/mids updates apply directly to the cache;
// verdict/journal/status frames are forwarded into the Bubble Tea program as
// the same message types Update already switches on. The render loop never
// blocks on network or LLM — all of this runs off the UI goroutine, dispatched
// via tea.Program.Send (safe to call concurrently; see PumpWS's doc comment).
package tui

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"

	"github.com/hyperagent/tui/internal/apiclient"
)

// Sender is the minimal interface the bridge needs from a tea.Program. Kept
// here so it can be depended on without pulling in bubbletea in places that
// only need to send messages (e.g. tests).
type Sender interface {
	Send(tea.Msg)
}

// statusKind discriminates what a statusMsg is asserting, so consumers read
// only the fields that event owns — mirrors backend/internal/bus.StatusKind's
// two values locally (bus is backend-internal and this module cannot import
// it).
type statusKind int

const (
	// statusNotice is a transient message (reasoner error, history-write
	// failure). It carries Detail and optionally Provider; it must not touch
	// connection state.
	statusNotice statusKind = iota
	// statusConn asserts the websocket connection state via Connected.
	statusConn
)

// Tea messages the render loop reacts to, mirroring the shape of
// backend/internal/bus events. PumpWS produces these from real server push
// frames.
type (
	barMsg     apiclient.Bar
	verdictMsg apiclient.Verdict

	// journalMsg mirrors backend/internal/bus.JournalEvent.
	journalMsg struct {
		Coin    string
		Kind    string // "candidate" | "fill" | "open" | "close" | "alert" | "error"
		Summary string
		Verdict *apiclient.Verdict // non-nil for candidate events
	}

	// statusMsg mirrors backend/internal/bus.StatusEvent.
	statusMsg struct {
		Kind      statusKind
		Connected bool // authoritative only when Kind == statusConn
		Provider  string
		Mode      string // "propose" | "autonomous"
		Detail    string
	}

	positionMsg apiclient.Position

	chatReplyMsg struct {
		text string
		err  error
	}
)

// wsFrame mirrors the {"topic":...,"data":...} envelope backend/internal/api/ws.go writes.
type wsFrame struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

// healthyConnDuration is how long a connection has to stay up before we
// treat its eventual drop as unrelated to daemon/network health and reset
// backoff to base. 3s is comfortably longer than a TCP handshake plus a
// WS upgrade plus the first few push frames, so a connection that clears
// this bar has demonstrably done real work; anything shorter (including a
// connection that never dialed at all) reads as a flapping proxy or a
// daemon rejecting connections post-handshake, so backoff keeps escalating
// instead of resetting.
const healthyConnDuration = 3 * time.Second

// nextBackoff decides the backoff to use for the *next* reconnect attempt,
// given the backoff used for the attempt that just ended and how long the
// resulting connection stayed up (pass 0 if the dial itself failed — that
// is indistinguishable from a connection that dropped instantly). Kept as
// a pure function so the reset-vs-escalate decision can be unit tested
// without driving a real dial/read loop.
func nextBackoff(prevBackoff, upDuration, maxBackoff time.Duration) time.Duration {
	if upDuration >= healthyConnDuration {
		return time.Second
	}
	next := prevBackoff * 2
	if next > maxBackoff {
		next = maxBackoff
	}
	return next
}

// sleepCtx blocks for d or until ctx is done, whichever comes first. It
// returns false if ctx ended the wait early, so callers can bail out of a
// retry loop instead of sleeping past cancellation.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// PumpWS connects to the daemon's /api/ws, applies bar/mids updates directly
// to cache, and forwards verdict/journal/status frames into the Bubble Tea
// program as the same message types Update already switches on. Reconnects
// with capped exponential backoff on every failed attempt — whether the
// dial itself failed or the connection dialed fine but dropped again in
// under healthyConnDuration (see nextBackoff). A connection that stays up
// at least that long is treated as healthy and backoff resets to base
// before the next attempt. Blocks until ctx is cancelled.
//
// httpBaseURL is the daemon's HTTP base URL (e.g. "http://127.0.0.1:8787"),
// the same value passed to apiclient.New — PumpWS derives the ws(s)://.../api/ws
// URL from it via wsURLFrom, since that helper is unexported and callers in
// package main cannot invoke it directly.
func PumpWS(ctx context.Context, httpBaseURL string, cache *apiclient.Cache, p *tea.Program) {
	wsURL := wsURLFrom(httpBaseURL)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		dialStart := time.Now()
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			backoff = nextBackoff(backoff, 0, maxBackoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			continue
		}
		readLoop(ctx, conn, cache, p)
		conn.Close()
		up := time.Since(dialStart)

		if ctx.Err() != nil {
			return
		}

		backoff = nextBackoff(backoff, up, maxBackoff)
		if !sleepCtx(ctx, backoff) {
			return
		}
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, cache *apiclient.Cache, p *tea.Program) {
	for {
		if ctx.Err() != nil {
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var f wsFrame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.Topic {
		case "bar":
			var b apiclient.Bar
			if json.Unmarshal(f.Data, &b) == nil {
				cache.PutBar(b)
				p.Send(barMsg{})
			}
		case "mids":
			var m struct{ Mids map[string]float64 }
			if json.Unmarshal(f.Data, &m) == nil {
				for coin, px := range m.Mids {
					cache.PutMid(coin, px)
				}
			}
		case "verdict":
			var v apiclient.Verdict
			if json.Unmarshal(f.Data, &v) == nil {
				p.Send(verdictMsg(v))
			}
		case "journal":
			var e journalMsg
			if json.Unmarshal(f.Data, &e) == nil {
				p.Send(e)
			}
		case "status":
			var s statusMsg
			if json.Unmarshal(f.Data, &s) == nil {
				p.Send(s)
			}
		}
	}
}

func wsURLFrom(httpBaseURL string) string {
	u, err := url.Parse(httpBaseURL)
	if err != nil {
		log.Printf("tui: bad base url %q: %v", httpBaseURL, err)
		return httpBaseURL
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/api/ws"
	return u.String()
}

// PollMarkets refreshes AssetCtx/Position for the whole visualized watchlist
// every 5s — there is no WS topic for either today. Blocks until ctx is done.
func PollMarkets(ctx context.Context, client *apiclient.Client, cache *apiclient.Cache, p *tea.Program) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			entries, err := client.Markets(ctx)
			if err == nil {
				cache.ApplyMarkets(entries)
				p.Send(barMsg{})
			}
		}
	}
}
