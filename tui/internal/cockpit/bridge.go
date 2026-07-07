// The channel→Msg bridge: a real WebSocket client consuming the daemon's
// /api/ws push stream. Bar/mids updates apply directly to the cache;
// verdict/journal/status frames are forwarded into the Bubble Tea program as
// the same message types Update already switches on. The render loop never
// blocks on network or LLM — all of this runs off the UI goroutine, dispatched
// via tea.Program.Send (safe to call concurrently; see PumpWS's doc comment).
package cockpit

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
// PumpWS also synthesizes its own statusMsg{Kind: statusConn} on dial
// success and on every connection loss, independent of any statusConn frame
// forwarded from the daemon (see readLoop's "status" case) — the daemon's
// frames report its own link to the exchange, whereas these synthesized
// ones report the TUI's own link to the daemon. Both flow through the same
// m.connected field, so the header's connection chip means "TUI↔daemon push
// link is up", not "daemon↔exchange is up".
//
// httpBaseURL is the daemon's HTTP base URL (e.g. "http://127.0.0.1:8787"),
// the same value passed to apiclient.New — PumpWS derives the ws(s)://.../api/ws
// URL from it via wsURLFrom, since that helper is unexported and callers in
// package main cannot invoke it directly.
//
// client, when non-nil, is used to seed the thesis cache from GET /api/theses
// after every successful dial (see seedTheses) — thesis frames missed while
// disconnected are state, not a stream, so the snapshot fully repairs the
// cache on reconnect.
func PumpWS(ctx context.Context, httpBaseURL string, client *apiclient.Client, cache *apiclient.Cache, p *tea.Program) {
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
		p.Send(statusMsg{Kind: statusConn, Connected: true})
		// Seed the thesis snapshot before consuming push frames: a "thesis"
		// frame read first and then overwritten by a slower snapshot apply
		// would silently roll the card back. Frames buffer on the socket
		// while the (time-boxed) snapshot fetch runs.
		if client != nil {
			seedTheses(ctx, client, cache, p)
		}
		readLoop(ctx, conn, cache, p)
		conn.Close()
		p.Send(statusMsg{Kind: statusConn, Connected: false})
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
		case "thesis":
			var t apiclient.Thesis
			if json.Unmarshal(f.Data, &t) == nil && t.Coin != "" {
				// Version 0 = invalidation tombstone: drop the card rather than
				// caching an empty-direction ghost. Any other version is a live
				// upsert. (Snapshot re-seed on reconnect stays the authority.)
				if t.Version == 0 {
					cache.DropThesis(t.Coin)
				} else {
					cache.PutThesis(t)
				}
				p.Send(thesisMsg(t))
			}
		case "status":
			var s statusMsg
			if json.Unmarshal(f.Data, &s) == nil {
				p.Send(s)
			}
		}
	}
}

// seedTheses cold-starts the THESES cards from GET /api/theses. Run once per
// successful WS (re)connect; a failure is reported as a statusNotice and the
// next reconnect (or the live "thesis" frames themselves) retries.
func seedTheses(ctx context.Context, client *apiclient.Client, cache *apiclient.Cache, p Sender) {
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ts, err := client.Theses(sctx)
	if err != nil {
		p.Send(statusMsg{Kind: statusNotice, Detail: "theses snapshot failed: " + err.Error()})
		return
	}
	cache.ApplyTheses(ts)
	p.Send(thesisMsg{})
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
// every 5s — there is no WS topic for either today. Blocks until ctx is
// done. A poll failure is reported as a statusNotice, but rate-limited to
// the first failure after a success — otherwise a dead daemon floods the
// journal with one entry every 5s.
func PollMarkets(ctx context.Context, client *apiclient.Client, cache *apiclient.Cache, p *tea.Program) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	failing := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pollMarketsOnce(ctx, client, cache, p, &failing)
		}
	}
}

// pollMarketsOnce runs a single Markets fetch and applies or reports it,
// updating *failing so PollMarkets only sends one statusNotice per run of
// consecutive failures (reset to false on the next success). Split out from
// PollMarkets so the rate-limiting behavior can be unit tested without
// driving the real 5s ticker.
func pollMarketsOnce(ctx context.Context, client *apiclient.Client, cache *apiclient.Cache, p *tea.Program, failing *bool) {
	entries, err := client.Markets(ctx)
	if err != nil {
		if !*failing {
			p.Send(statusMsg{Kind: statusNotice, Detail: "markets poll failed: " + err.Error()})
			*failing = true
		}
		return
	}
	*failing = false
	cache.ApplyMarkets(entries)
	p.Send(barMsg{})
}
