package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is a thin HTTP client for the backend daemon's control-plane API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a client against baseURL (e.g. "http://127.0.0.1:8787").
// token is sent as "Authorization: Bearer <token>" when non-empty.
func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{}}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var errBody struct {
			Error string `json:"error"`
		}
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		_ = json.Unmarshal(buf, &errBody)
		if errBody.Error != "" {
			return fmt.Errorf("%s", errBody.Error)
		}
		return fmt.Errorf("request failed: status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Subscribe(ctx context.Context, coins ...string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/subscribe", map[string]any{"coins": coins}, nil)
}

func (c *Client) Track(ctx context.Context, coin, timeframe string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/track", map[string]any{"coin": coin, "timeframe": timeframe}, nil)
}

func (c *Client) Untrack(ctx context.Context, coin string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/untrack", map[string]any{"coin": coin}, nil)
}

func (c *Client) Scan(ctx context.Context, coins ...string) error {
	return c.do(ctx, http.MethodPost, "/api/watchlist/scan", map[string]any{"coins": coins}, nil)
}

func (c *Client) SetMode(ctx context.Context, mode string) error {
	return c.do(ctx, http.MethodPut, "/api/execution/mode", map[string]any{"mode": mode}, nil)
}

func (c *Client) Settings(ctx context.Context) (SettingsResponse, error) {
	var out SettingsResponse
	err := c.do(ctx, http.MethodGet, "/api/settings", nil, &out)
	return out, err
}

func (c *Client) SaveSettings(ctx context.Context, chatProvider, chatModel, batchProvider, batchModel string) error {
	return c.do(ctx, http.MethodPut, "/api/settings", map[string]any{
		"chat_provider": chatProvider, "chat_model": chatModel,
		"batch_provider": batchProvider, "batch_model": batchModel,
	}, nil)
}

func (c *Client) SetProviderKey(ctx context.Context, name, key string) error {
	return c.do(ctx, http.MethodPut, "/api/providers/"+name+"/key", map[string]any{"key": key}, nil)
}

func (c *Client) Thesis(ctx context.Context, coin, tf string) (string, error) {
	var out struct {
		Context string `json:"context"`
	}
	err := c.do(ctx, http.MethodGet, "/api/thesis/"+coin+"?tf="+tf, nil, &out)
	return out.Context, err
}

// Theses fetches the full thesis snapshot from GET /api/theses — the
// cockpit's cold-start for the per-asset thesis cards.
func (c *Client) Theses(ctx context.Context) ([]Thesis, error) {
	var out struct {
		Theses []Thesis `json:"theses"`
	}
	err := c.do(ctx, http.MethodGet, "/api/theses", nil, &out)
	return out.Theses, err
}

func (c *Client) Chat(ctx context.Context, message string, history []ChatTurn) (reply, provider, model string, err error) {
	var out struct {
		Reply    string `json:"reply"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	err = c.do(ctx, http.MethodPost, "/api/chat", map[string]any{"message": message, "history": history}, &out)
	return out.Reply, out.Provider, out.Model, err
}

func (c *Client) Markets(ctx context.Context) ([]MarketEntry, error) {
	var out []MarketEntry
	err := c.do(ctx, http.MethodGet, "/api/markets", nil, &out)
	return out, err
}

func (c *Client) Bars(ctx context.Context, coin, tf string, n int) ([]Bar, error) {
	var out []Bar
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/bars/%s?tf=%s&n=%d", coin, tf, n), nil, &out)
	return out, err
}
