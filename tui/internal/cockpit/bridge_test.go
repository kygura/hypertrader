package cockpit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"

	"github.com/hyperagent/tui/internal/apiclient"
)

// recorderModel is a minimal tea.Model that records every Msg it receives
// (besides bubbletea's own housekeeping messages) into a channel, so tests
// can assert on what PumpWS/readLoop dispatched without a real terminal.
type recorderModel struct {
	msgs chan tea.Msg
}

func (m *recorderModel) Init() tea.Cmd { return nil }

func (m *recorderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.KeyMsg:
		// Ignore bubbletea housekeeping we don't care about in these tests.
	default:
		select {
		case m.msgs <- msg:
		default:
		}
	}
	return m, nil
}

func (m *recorderModel) View() tea.View { return tea.NewView("") }

// newTestProgram builds a headless tea.Program (no renderer, no input) wired
// to a recorderModel, running under ctx. Callers must call cancel to stop it.
func newTestProgram(ctx context.Context) (*tea.Program, chan tea.Msg) {
	msgs := make(chan tea.Msg, 16)
	m := &recorderModel{msgs: msgs}
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithoutRenderer(),
		tea.WithInput(nil),
		tea.WithOutput(&discardWriter{}),
		tea.WithoutSignalHandler(),
	)
	return p, msgs
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

// TestPumpWSForwardsBarAndStatusFrames stands up a real httptest.Server with
// a gorilla/websocket Upgrader that pushes one "bar" frame and one "status"
// frame, then asserts PumpWS both applies the bar to the cache and forwards
// both frames as the right tea.Msg types. It also proves the fix for the WS
// link status finding: PumpWS must synthesize its own statusConn{Connected:
// true} right after a successful dial (distinct from the daemon-forwarded
// status frame, which carries Mode) and a statusConn{Connected: false} once
// the server closes the connection.
func TestPumpWSForwardsBarAndStatusFrames(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		bar := apiclient.Bar{Coin: "BTC", Timeframe: "1h", Close: 123.5, Final: true}
		barData, _ := json.Marshal(bar)
		_ = conn.WriteMessage(websocket.TextMessage, mustMarshalFrame(t, "bar", barData))

		status := statusMsg{Kind: statusConn, Connected: true, Mode: "propose"}
		statusData, _ := json.Marshal(status)
		_ = conn.WriteMessage(websocket.TextMessage, mustMarshalFrame(t, "status", statusData))

		// Keep the connection open briefly so the client has time to read both
		// frames, then let the deferred conn.Close() drop it — driving the
		// connected=false assertion below.
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := apiclient.NewCache()
	p, msgs := newTestProgram(ctx)

	go func() {
		if _, err := p.Run(); err != nil && ctx.Err() == nil {
			t.Errorf("program run: %v", err)
		}
	}()
	defer p.Kill()

	go PumpWS(ctx, srv.URL, nil, cache, p)

	var gotBar, gotForwardedStatus, gotConnTrue, gotConnFalse bool
	deadline := time.After(4 * time.Second)
	for !gotBar || !gotForwardedStatus || !gotConnTrue || !gotConnFalse {
		select {
		case msg := <-msgs:
			switch m := msg.(type) {
			case barMsg:
				gotBar = true
			case statusMsg:
				if m.Kind != statusConn {
					continue
				}
				switch {
				case m.Connected && m.Mode == "propose":
					gotForwardedStatus = true
				case m.Connected:
					gotConnTrue = true
				default:
					gotConnFalse = true
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for frames: gotBar=%v gotForwardedStatus=%v gotConnTrue=%v gotConnFalse=%v",
				gotBar, gotForwardedStatus, gotConnTrue, gotConnFalse)
		}
	}

	waitForCondition(t, time.Second, func() bool {
		b, ok := cache.LatestBar("BTC", "1h")
		return ok && b.Close == 123.5
	})
}

// TestPumpWSForwardsThesisFramesAndSeedsSnapshot stands up a server that
// answers GET /api/theses with a one-thesis snapshot and pushes a "thesis"
// WS frame, then asserts PumpWS both seeds the cache from the snapshot on
// connect (client non-nil) and applies + forwards the pushed frame as a
// thesisMsg.
func TestPumpWSForwardsThesisFramesAndSeedsSnapshot(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/theses" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"theses":[{"coin":"BTC","direction":"long","version":2}]}`))
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		th := apiclient.Thesis{Coin: "ETH", Direction: "short", Summary: "distribution", Version: 1}
		data, _ := json.Marshal(th)
		_ = conn.WriteMessage(websocket.TextMessage, mustMarshalFrame(t, "thesis", data))
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := apiclient.New(srv.URL, "")
	cache := apiclient.NewCache()
	p, msgs := newTestProgram(ctx)
	go func() { _, _ = p.Run() }()
	defer p.Kill()

	go PumpWS(ctx, srv.URL, client, cache, p)

	var gotFrame bool
	deadline := time.After(4 * time.Second)
	for !gotFrame {
		select {
		case msg := <-msgs:
			if tm, ok := msg.(thesisMsg); ok && tm.Coin == "ETH" {
				gotFrame = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for the pushed thesis frame")
		}
	}

	waitForCondition(t, time.Second, func() bool {
		eth, okETH := cache.Thesis("ETH")
		btc, okBTC := cache.Thesis("BTC")
		return okETH && eth.Direction == "short" && okBTC && btc.Version == 2
	})
}

// TestPumpWSReconnectsOnImmediateClose points PumpWS at a server that accepts
// the WS upgrade and immediately closes the connection, then asserts a
// second connection attempt eventually happens (observed via a counter
// incremented on every upgrade). Backoff now escalates after a quick drop
// (see TestPumpWSBacksOffAfterQuickDrop for the timing assertion), so the
// wait window here is generous rather than tight.
func TestPumpWSReconnectsOnImmediateClose(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var mu sync.Mutex
	attempts := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		attempts++
		mu.Unlock()
		conn.Close() // immediately drop — forces PumpWS's readLoop to return and retry
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := apiclient.NewCache()
	p, _ := newTestProgram(ctx)
	go func() { _, _ = p.Run() }()
	defer p.Kill()

	go PumpWS(ctx, srv.URL, nil, cache, p)

	waitForCondition(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return attempts >= 2
	})
}

// TestPumpWSBacksOffAfterQuickDrop is the fix's proof: before the fix,
// backoff only applied on dial failure, so a server that upgrades and then
// immediately closes caused PumpWS to redial at native loop speed (the old
// version of this test completed in ~20ms for several full cycles). This
// test asserts the gap between the first and second connection attempts is
// now at least 1s — the escalated backoff after a drop well under
// healthyConnDuration — proving backoff applies to post-connect drops too,
// not just dial errors.
func TestPumpWSBacksOffAfterQuickDrop(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var mu sync.Mutex
	attempts := 0
	first := make(chan time.Time, 1)
	second := make(chan time.Time, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		switch n {
		case 1:
			select {
			case first <- time.Now():
			default:
			}
		case 2:
			select {
			case second <- time.Now():
			default:
			}
		}
		conn.Close() // immediately drop — forces PumpWS's readLoop to return and retry
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := apiclient.NewCache()
	p, _ := newTestProgram(ctx)
	go func() { _, _ = p.Run() }()
	defer p.Kill()

	go PumpWS(ctx, srv.URL, nil, cache, p)

	var t1, t2 time.Time
	select {
	case t1 = <-first:
	case <-time.After(3 * time.Second):
		t.Fatal("first connection attempt never happened")
	}
	select {
	case t2 = <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("second connection attempt never happened")
	}

	if gap := t2.Sub(t1); gap < time.Second {
		t.Fatalf("reconnect after a quick drop happened too fast (%s); expected escalated backoff (>=1s) to apply, not just on dial failure", gap)
	}
}

// TestNextBackoff unit-tests the reset-vs-escalate decision in isolation
// from real dial/read timing, so the policy (escalate on dial failure or a
// quick drop, reset to base once a connection proves itself healthy) is
// covered by a fast, deterministic test alongside the real-timing proof
// above.
func TestNextBackoff(t *testing.T) {
	const maxBackoff = 30 * time.Second

	cases := []struct {
		name        string
		prevBackoff time.Duration
		upDuration  time.Duration
		want        time.Duration
	}{
		{"dial failure escalates", 1 * time.Second, 0, 2 * time.Second},
		{"quick drop escalates", 2 * time.Second, 500 * time.Millisecond, 4 * time.Second},
		{"drop just under threshold still escalates", 4 * time.Second, healthyConnDuration - time.Millisecond, 8 * time.Second},
		{"healthy connection resets to base", 16 * time.Second, healthyConnDuration, time.Second},
		{"long-lived connection resets to base", 16 * time.Second, time.Minute, time.Second},
		{"escalation is capped at maxBackoff", maxBackoff, 0, maxBackoff},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextBackoff(tc.prevBackoff, tc.upDuration, maxBackoff)
			if got != tc.want {
				t.Fatalf("nextBackoff(%s, %s, %s) = %s, want %s", tc.prevBackoff, tc.upDuration, maxBackoff, got, tc.want)
			}
		})
	}
}

// TestPollMarketsOnceRateLimitsFailureNotice is the fix's proof for the
// silent-error finding: pollMarketsOnce must report a poll failure as a
// statusNotice, but only the first time after a success — a dead daemon
// polled every 5s must not flood the journal with one entry per tick. This
// drives a fake server that succeeds once then fails twice and asserts
// exactly one notice reaches the program.
func TestPollMarketsOnceRateLimitsFailureNotice(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := apiclient.New(srv.URL, "")
	cache := apiclient.NewCache()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, msgs := newTestProgram(ctx)
	go func() { _, _ = p.Run() }()
	defer p.Kill()

	failing := false
	pollMarketsOnce(ctx, client, cache, p, &failing) // succeeds
	pollMarketsOnce(ctx, client, cache, p, &failing) // fails -> one notice
	pollMarketsOnce(ctx, client, cache, p, &failing) // fails again -> rate-limited, no notice

	notices := 0
	deadline := time.After(500 * time.Millisecond)
drain:
	for {
		select {
		case msg := <-msgs:
			if sm, ok := msg.(statusMsg); ok && sm.Kind == statusNotice {
				notices++
			}
		case <-deadline:
			break drain
		}
	}
	if notices != 1 {
		t.Errorf("notices = %d, want 1", notices)
	}
	if calls != 3 {
		t.Errorf("server calls = %d, want 3", calls)
	}
}

func mustMarshalFrame(t *testing.T, topic string, data json.RawMessage) []byte {
	t.Helper()
	b, err := json.Marshal(wsFrame{Topic: topic, Data: data})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return b
}
