package ingestor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hyperagent/hyperagent/internal/bus"
)

// TestConnectAndRead_SendsPeriodicPing pins down the Hyperliquid WS keepalive
// contract (docs: "the server will close any connection if it hasn't [received
// a message from the client] in the last 60 seconds"; clients must send
// {"method":"ping"} to stay alive on quiet subscriptions). Without an active
// ping, a low-traffic subscription (e.g. a testnet demo coin with few trades)
// can sit silent long enough for Hyperliquid to close the socket server-side,
// which reads to the TUI as connect/disconnect flapping on the live indicator.
func TestConnectAndRead_SendsPeriodicPing(t *testing.T) {
	upgrader := websocket.Upgrader{}
	pingReceived := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame struct {
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &frame) == nil && frame.Method == "ping" {
				select {
				case pingReceived <- struct{}{}:
				default:
				}
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	in := New(wsURL, nil, bus.New())
	in.pingInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go in.connectAndRead(ctx)

	select {
	case <-pingReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("expected client to send a {\"method\":\"ping\"} frame, none received within timeout")
	}
}

// TestConnectAndRead_SkipsCoinsOutsideUniverse pins down the fix for a
// confirmed-live bug: Hyperliquid testnet hard-closes the whole connection —
// no error frame, just a dropped socket — the instant it receives a
// per-coin subscribe for a coin outside its perp universe (e.g. XRP/LINK,
// which this repo's warmup already falls back to CoinGecko for). Before
// SetUniverse existed, one such coin in the watchlist poisoned every other
// coin's live feed in an endless connect/drop loop. This test asserts the
// subscribe loop never writes a frame for a coin SetUniverse didn't list.
func TestConnectAndRead_SkipsCoinsOutsideUniverse(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var mu sync.Mutex
	var subscribedCoins []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame subscribeMsg
			if json.Unmarshal(data, &frame) == nil && frame.Subscription.Coin != "" {
				mu.Lock()
				subscribedCoins = append(subscribedCoins, frame.Subscription.Coin)
				mu.Unlock()
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	in := New(wsURL, []string{"BTC", "XRP", "ETH", "LINK"}, bus.New())
	in.SetUniverse([]string{"BTC", "ETH"}) // venue universe excludes XRP/LINK

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go in.connectAndRead(ctx)

	// Give the subscribe loop time to run; there is no ack to wait on for
	// per-coin subscriptions, so a short sleep is the simplest deterministic
	// point to sample state after — the loop runs synchronously before the
	// read loop blocks, so this window is generous.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	for _, coin := range subscribedCoins {
		if coin == "XRP" || coin == "LINK" {
			t.Fatalf("subscribed to %s, which SetUniverse excluded; subscribedCoins=%v", coin, subscribedCoins)
		}
	}
	if len(subscribedCoins) == 0 {
		t.Fatal("expected subscriptions for BTC/ETH, got none")
	}
}
