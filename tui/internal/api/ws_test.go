package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/store"
)

// wsURL converts an httptest server's http(s) URL into a ws(s) URL for the
// given path.
func wsURL(httpURL, path string) string {
	u := "ws" + strings.TrimPrefix(httpURL, "http")
	return u + path
}

// dialWS opens a client connection to the server's /api/ws endpoint.
func dialWS(t *testing.T, srv *httptest.Server, query string) *websocket.Conn {
	t.Helper()
	url := wsURL(srv.URL, "/api/ws")
	if query != "" {
		url += "?" + query
	}
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestWSDeliversBarAndJournalFrames(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir, 8)
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	s := NewServer(Deps{Bus: b, Store: st, Cfg: config.Default(), Version: "test"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	conn := dialWS(t, srv, "")

	// Give the server a moment to register the client before publishing —
	// registration happens on the Upgrade goroutine, which races the dial
	// return; poll instead of a fixed sleep for determinism.
	waitFor(t, time.Second, func() bool {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
		return len(s.state.wsClients) == 1
	})

	b.PublishBar(metrics.Bar{Coin: "BTC", Timeframe: "1h", Close: 123, Final: true})
	b.PublishJournal(bus.JournalEvent{Coin: "BTC", Kind: "alert", Summary: "hello"})

	seen := map[string]bool{}
	deadline := time.Now().Add(2 * time.Second)
	for len(seen) < 2 && time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		var frame struct {
			Topic string          `json:"topic"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("unmarshal frame: %v", err)
		}
		seen[frame.Topic] = true
		switch frame.Topic {
		case "bar":
			var bar metrics.Bar
			if err := json.Unmarshal(frame.Data, &bar); err != nil {
				t.Fatalf("unmarshal bar data: %v", err)
			}
			if bar.Coin != "BTC" {
				t.Errorf("bar.Coin = %q, want BTC", bar.Coin)
			}
		case "journal":
			var je bus.JournalEvent
			if err := json.Unmarshal(frame.Data, &je); err != nil {
				t.Fatalf("unmarshal journal data: %v", err)
			}
			if je.Summary != "hello" {
				t.Errorf("journal.Summary = %q, want hello", je.Summary)
			}
		}
	}
	if !seen["bar"] || !seen["journal"] {
		t.Fatalf("frames seen = %+v, want bar and journal both", seen)
	}
}

// TestWSStalledClientDoesNotBlockPublish fills one client's send buffer
// without ever reading, then verifies bus publishes still return promptly
// (the drop-oldest guarantee) and a second, actively-reading client still
// gets served.
func TestWSStalledClientDoesNotBlockPublish(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir, 8)
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	s := NewServer(Deps{Bus: b, Store: st, Cfg: config.Default(), Version: "test"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	stalled := dialWS(t, srv, "")
	active := dialWS(t, srv, "")

	waitFor(t, time.Second, func() bool {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
		return len(s.state.wsClients) == 2
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			b.PublishBar(metrics.Bar{Coin: "BTC", Timeframe: "1h", Close: float64(i), Final: true})
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("publishing 500 bars blocked — stalled client is applying backpressure")
	}

	// The actively-reading client should still get at least one frame despite
	// the other client never draining its buffer.
	active.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := active.ReadMessage(); err != nil {
		t.Fatalf("active client ReadMessage: %v", err)
	}

	_ = stalled // never read from; that's the point of the test
}

func TestWSAuthTokenViaQueryParam(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir, 8)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.API.Token = "s3cret"
	b := bus.New()
	s := NewServer(Deps{Bus: b, Store: st, Cfg: cfg, Version: "test"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	// No token: dial should fail (401 during handshake).
	badURL := wsURL(srv.URL, "/api/ws")
	if _, _, err := websocket.DefaultDialer.Dial(badURL, nil); err == nil {
		t.Fatal("expected dial without token to fail")
	}

	// Correct token via query param: dial should succeed.
	conn := dialWS(t, srv, "token=s3cret")
	conn.Close()
}
