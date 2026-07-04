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

// PumpWS connects to the daemon's /api/ws, applies bar/mids updates directly
// to cache, and forwards verdict/journal/status frames into the Bubble Tea
// program as the same message types Update already switches on. Reconnects
// with capped exponential backoff on any read/dial error; blocks until ctx
// is cancelled.
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
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		readLoop(ctx, conn, cache, p)
		conn.Close()
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
