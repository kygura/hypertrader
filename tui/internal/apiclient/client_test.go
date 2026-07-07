package apiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestServer builds an httptest.Server that asserts the incoming request's
// method and path, decodes the JSON body (if wantBody is non-nil) into a
// fresh map for comparison, and writes back the given status/response.
func newTestServer(t *testing.T, wantMethod, wantPath string, checkBody func(t *testing.T, body []byte), status int, respBody any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wantMethod {
			t.Errorf("method = %q, want %q", r.Method, wantMethod)
		}
		if r.URL.RequestURI() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.RequestURI(), wantPath)
		}
		if checkBody != nil {
			buf, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			checkBody(t, buf)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if respBody != nil {
			if err := json.NewEncoder(w).Encode(respBody); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}
	}))
}

func decodeBody(t *testing.T, buf []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatalf("unmarshal body %q: %v", buf, err)
	}
	return m
}

func TestSubscribe(t *testing.T) {
	srv := newTestServer(t, http.MethodPost, "/api/watchlist/subscribe", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		coins, ok := m["coins"].([]any)
		if !ok || len(coins) != 2 || coins[0] != "BTC" || coins[1] != "ETH" {
			t.Errorf("coins = %v, want [BTC ETH]", m["coins"])
		}
	}, http.StatusOK, nil)
	defer srv.Close()

	c := New(srv.URL, "")
	if err := c.Subscribe(context.Background(), "BTC", "ETH"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
}

func TestTrack(t *testing.T) {
	srv := newTestServer(t, http.MethodPost, "/api/watchlist/track", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["coin"] != "BTC" || m["timeframe"] != "1h" {
			t.Errorf("body = %v, want coin=BTC timeframe=1h", m)
		}
	}, http.StatusOK, nil)
	defer srv.Close()

	c := New(srv.URL, "")
	if err := c.Track(context.Background(), "BTC", "1h"); err != nil {
		t.Fatalf("Track: %v", err)
	}
}

func TestUntrack(t *testing.T) {
	srv := newTestServer(t, http.MethodPost, "/api/watchlist/untrack", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["coin"] != "BTC" {
			t.Errorf("body = %v, want coin=BTC", m)
		}
	}, http.StatusOK, nil)
	defer srv.Close()

	c := New(srv.URL, "")
	if err := c.Untrack(context.Background(), "BTC"); err != nil {
		t.Fatalf("Untrack: %v", err)
	}
}

func TestScan(t *testing.T) {
	srv := newTestServer(t, http.MethodPost, "/api/watchlist/scan", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		coins, ok := m["coins"].([]any)
		if !ok || len(coins) != 1 || coins[0] != "SOL" {
			t.Errorf("coins = %v, want [SOL]", m["coins"])
		}
	}, http.StatusOK, nil)
	defer srv.Close()

	c := New(srv.URL, "")
	if err := c.Scan(context.Background(), "SOL"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
}

func TestSetMode(t *testing.T) {
	srv := newTestServer(t, http.MethodPut, "/api/execution/mode", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["mode"] != "live" {
			t.Errorf("body = %v, want mode=live", m)
		}
	}, http.StatusOK, nil)
	defer srv.Close()

	c := New(srv.URL, "")
	if err := c.SetMode(context.Background(), "live"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
}

func TestSettings(t *testing.T) {
	want := SettingsResponse{
		Mode:           "live",
		Batch:          RoleSettings{Provider: "anthropic", Model: "claude-sonnet"},
		Chat:           RoleSettings{Provider: "anthropic", Model: "claude-haiku"},
		ProviderNames:  []string{"anthropic", "openai"},
		ProviderModels: map[string][]string{"anthropic": {"claude-sonnet"}},
		KeyHints:       map[string]string{"anthropic": "sk-...abcd"},
		Visualized:     []string{"BTC", "ETH"},
		Tracked:        []string{"BTC"},
		Timeframes:     map[string]string{"BTC": "1h"},
		Risk: RiskSettings{
			MaxPositionUSD:      1000,
			MaxTotalExposureUSD: 5000,
			MaxConcurrent:       3,
			DailyLossKillUSD:    200,
		},
	}
	srv := newTestServer(t, http.MethodGet, "/api/settings", nil, http.StatusOK, want)
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Settings(context.Background())
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if got.Mode != want.Mode || got.Risk.MaxPositionUSD != want.Risk.MaxPositionUSD || len(got.Visualized) != 2 {
		t.Errorf("Settings() = %+v, want %+v", got, want)
	}
}

func TestSaveSettings(t *testing.T) {
	srv := newTestServer(t, http.MethodPut, "/api/settings", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["chat_provider"] != "anthropic" || m["chat_model"] != "claude-haiku" ||
			m["batch_provider"] != "openai" || m["batch_model"] != "gpt-5" {
			t.Errorf("body = %v", m)
		}
	}, http.StatusOK, nil)
	defer srv.Close()

	c := New(srv.URL, "")
	if err := c.SaveSettings(context.Background(), "anthropic", "claude-haiku", "openai", "gpt-5"); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
}

func TestSetProviderKey(t *testing.T) {
	srv := newTestServer(t, http.MethodPut, "/api/providers/anthropic/key", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["key"] != "sk-test" {
			t.Errorf("body = %v, want key=sk-test", m)
		}
	}, http.StatusOK, nil)
	defer srv.Close()

	c := New(srv.URL, "")
	if err := c.SetProviderKey(context.Background(), "anthropic", "sk-test"); err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
}

func TestSetProviderKey_ErrorEnvelope(t *testing.T) {
	srv := newTestServer(t, http.MethodPut, "/api/providers/bogus/key", nil, http.StatusUnprocessableEntity,
		map[string]string{"error": "unknown provider"})
	defer srv.Close()

	c := New(srv.URL, "")
	err := c.SetProviderKey(context.Background(), "bogus", "sk-test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "unknown provider" {
		t.Errorf("err = %q, want %q", err.Error(), "unknown provider")
	}
}

func TestThesis(t *testing.T) {
	srv := newTestServer(t, http.MethodGet, "/api/thesis/BTC?tf=1h", nil, http.StatusOK,
		map[string]string{"context": "bullish thesis text"})
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Thesis(context.Background(), "BTC", "1h")
	if err != nil {
		t.Fatalf("Thesis: %v", err)
	}
	if got != "bullish thesis text" {
		t.Errorf("Thesis() = %q, want %q", got, "bullish thesis text")
	}
}

func TestTheses(t *testing.T) {
	srv := newTestServer(t, http.MethodGet, "/api/theses", nil, http.StatusOK,
		map[string]any{"theses": []Thesis{
			{
				Coin: "BTC", Direction: "long", Summary: "higher-timeframe uptrend intact",
				Invalidation: 58200, Targets: []float64{62000, 66500}, Horizon: "weeks",
				Confidence: 0.7,
				CreatedAt:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				ReviewedAt: time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC),
				Version:    4,
			},
		}})
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Theses(context.Background())
	if err != nil {
		t.Fatalf("Theses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Theses() len = %d, want 1", len(got))
	}
	th := got[0]
	if th.Coin != "BTC" || th.Direction != "long" || th.Invalidation != 58200 ||
		len(th.Targets) != 2 || th.Horizon != "weeks" || th.Version != 4 {
		t.Errorf("Theses()[0] = %+v", th)
	}
	if !th.ReviewedAt.Equal(time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("ReviewedAt = %s", th.ReviewedAt)
	}
}

func TestChat(t *testing.T) {
	srv := newTestServer(t, http.MethodPost, "/api/chat", func(t *testing.T, buf []byte) {
		m := decodeBody(t, buf)
		if m["message"] != "hello" {
			t.Errorf("message = %v, want hello", m["message"])
		}
		hist, ok := m["history"].([]any)
		if !ok || len(hist) != 1 {
			t.Errorf("history = %v, want 1 entry", m["history"])
		}
	}, http.StatusOK, map[string]string{
		"reply":    "hi there",
		"provider": "anthropic",
		"model":    "claude-haiku",
	})
	defer srv.Close()

	c := New(srv.URL, "")
	reply, provider, model, err := c.Chat(context.Background(), "hello", []ChatTurn{{Role: "user", Text: "prior"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply != "hi there" || provider != "anthropic" || model != "claude-haiku" {
		t.Errorf("Chat() = (%q, %q, %q)", reply, provider, model)
	}
}

func TestMarkets(t *testing.T) {
	want := []MarketEntry{
		{
			Coin: "BTC",
			Bar: Bar{
				Coin:      "BTC",
				Timeframe: "1h",
				OpenTime:  time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC),
				CloseTime: time.Date(2026, 7, 4, 1, 0, 0, 0, time.UTC),
				Open:      60000, High: 61000, Low: 59500, Close: 60500,
			},
			Mid: 60500,
			AssetCtx: AssetCtx{
				Coin:      "BTC",
				MarkPrice: 60500,
			},
			Position: Position{Coin: "BTC", Size: 0.5, MarkPrice: 60500},
		},
	}
	srv := newTestServer(t, http.MethodGet, "/api/markets", nil, http.StatusOK, want)
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Markets(context.Background())
	if err != nil {
		t.Fatalf("Markets: %v", err)
	}
	if len(got) != 1 || got[0].Coin != "BTC" || got[0].Position.Size != 0.5 {
		t.Errorf("Markets() = %+v", got)
	}
	if got[0].Position.IsFlat() {
		t.Errorf("Position.IsFlat() = true, want false for size 0.5")
	}
}

func TestBars(t *testing.T) {
	want := []Bar{
		{Coin: "ETH", Timeframe: "5m", Open: 3000, Close: 3010, Final: true},
	}
	srv := newTestServer(t, http.MethodGet, "/api/bars/ETH?tf=5m&n=100", nil, http.StatusOK, want)
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Bars(context.Background(), "ETH", "5m", 100)
	if err != nil {
		t.Fatalf("Bars: %v", err)
	}
	if len(got) != 1 || got[0].Coin != "ETH" || !got[0].Final {
		t.Errorf("Bars() = %+v", got)
	}
}

func TestAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "s3cr3t")
	if err := c.SetMode(context.Background(), "paper"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if gotAuth != "Bearer s3cr3t" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer s3cr3t")
	}
}
