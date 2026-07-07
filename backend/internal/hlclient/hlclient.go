// Package hlclient is the no-SDK Hyperliquid REST surface. Hyperliquid's info
// API is plain HTTPS POST carrying JSON; this package owns the request shapes we
// need for warm-up and gap backfill: candleSnapshot, meta, and metaAndAssetCtxs.
//
// No third-party exchange dependency — just net/http + encoding/json, exactly as
// the plan's dependency posture demands.
package hlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

const (
	MainnetAPI = "https://api.hyperliquid.xyz"
	TestnetAPI = "https://api.hyperliquid-testnet.xyz"
)

// Client talks to the HL info endpoint over HTTPS.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client against the given base URL (use MainnetAPI/TestnetAPI).
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = MainnetAPI
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// info posts an arbitrary info request body and decodes the JSON response into v.
func (c *Client) info(ctx context.Context, body any, v any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("hlclient: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("hlclient: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hlclient: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hlclient: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("hlclient: decode: %w", err)
	}
	return nil
}

// rawCandle is the wire shape of a single candle from candleSnapshot.
type rawCandle struct {
	OpenMillis  int64  `json:"t"`
	CloseMillis int64  `json:"T"`
	Coin        string `json:"s"`
	Interval    string `json:"i"`
	Open        string `json:"o"`
	High        string `json:"h"`
	Low         string `json:"l"`
	Close       string `json:"c"`
	Volume      string `json:"v"`
	NumTrades   int    `json:"n"`
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

// CandleSnapshot fetches historical candles for warm-up / gap backfill. The
// interval is HL's notation ("1h", "4h", "15m", ...). Returns bars oldest-first.
func (c *Client) CandleSnapshot(ctx context.Context, coin, interval string, start, end time.Time) ([]metrics.Bar, error) {
	body := map[string]any{
		"type": "candleSnapshot",
		"req": map[string]any{
			"coin":      coin,
			"interval":  interval,
			"startTime": start.UnixMilli(),
			"endTime":   end.UnixMilli(),
		},
	}
	var raw []rawCandle
	if err := c.info(ctx, body, &raw); err != nil {
		return nil, err
	}
	bars := make([]metrics.Bar, 0, len(raw))
	for _, r := range raw {
		bars = append(bars, metrics.Bar{
			Coin:       coin,
			Timeframe:  interval,
			OpenTime:   time.UnixMilli(r.OpenMillis),
			CloseTime:  time.UnixMilli(r.CloseMillis),
			Open:       atof(r.Open),
			High:       atof(r.High),
			Low:        atof(r.Low),
			Close:      atof(r.Close),
			Volume:     atof(r.Volume),
			TradeCount: r.NumTrades,
		})
	}
	return bars, nil
}

// AssetCtx is the wire shape of one asset's perp context from metaAndAssetCtxs.
type assetCtxWire struct {
	Funding      string `json:"funding"`
	OpenInterest string `json:"openInterest"`
	MarkPx       string `json:"markPx"`
	OraclePx     string `json:"oraclePx"`
	Premium      string `json:"premium"`
	DayNtlVlm    string `json:"dayNtlVlm"`
}

type metaUniverse struct {
	Universe []struct {
		Name       string `json:"name"`
		SzDecimals int    `json:"szDecimals"`
	} `json:"universe"`
}

// MetaAndAssetCtxs returns current perp context for all assets, keyed by coin.
// Used to seed the store at startup before the websocket warms up.
func (c *Client) MetaAndAssetCtxs(ctx context.Context) (map[string]metrics.AssetCtx, error) {
	body := map[string]any{"type": "metaAndAssetCtxs"}
	// The response is a 2-element array: [meta, []assetCtx].
	var raw []json.RawMessage
	if err := c.info(ctx, body, &raw); err != nil {
		return nil, err
	}
	if len(raw) != 2 {
		return nil, fmt.Errorf("hlclient: metaAndAssetCtxs unexpected shape")
	}
	var meta metaUniverse
	if err := json.Unmarshal(raw[0], &meta); err != nil {
		return nil, fmt.Errorf("hlclient: meta: %w", err)
	}
	var ctxs []assetCtxWire
	if err := json.Unmarshal(raw[1], &ctxs); err != nil {
		return nil, fmt.Errorf("hlclient: assetCtxs: %w", err)
	}
	out := make(map[string]metrics.AssetCtx, len(ctxs))
	for i, w := range ctxs {
		if i >= len(meta.Universe) {
			break
		}
		coin := meta.Universe[i].Name
		out[coin] = metrics.AssetCtx{
			Coin:         coin,
			MarkPrice:    atof(w.MarkPx),
			OraclePrice:  atof(w.OraclePx),
			Funding:      atof(w.Funding),
			OpenInterest: atof(w.OpenInterest),
			Premium:      atof(w.Premium),
			DayVolume:    atof(w.DayNtlVlm),
			Time:         time.Now(),
		}
	}
	return out, nil
}

// Universe returns the perp universe coin names in meta order. The index of a
// coin in this slice IS its HL asset id — order placement depends on it, so
// this is the only sanctioned way to build an asset index.
func (c *Client) Universe(ctx context.Context) ([]string, error) {
	body := map[string]any{"type": "meta"}
	var meta metaUniverse
	if err := c.info(ctx, body, &meta); err != nil {
		return nil, err
	}
	names := make([]string, len(meta.Universe))
	for i, u := range meta.Universe {
		names[i] = u.Name
	}
	return names, nil
}

// AssetMeta is one perp universe entry: the coin name plus the venue's size
// precision. The index of an entry in the returned slice IS its HL asset id.
type AssetMeta struct {
	Name       string
	SzDecimals int
}

// UniverseMeta returns the perp universe with per-asset size precision, in meta
// order. Order construction needs szDecimals: the exchange rejects size/price
// strings carrying more precision than the asset allows.
func (c *Client) UniverseMeta(ctx context.Context) ([]AssetMeta, error) {
	body := map[string]any{"type": "meta"}
	var meta metaUniverse
	if err := c.info(ctx, body, &meta); err != nil {
		return nil, err
	}
	out := make([]AssetMeta, len(meta.Universe))
	for i, u := range meta.Universe {
		out[i] = AssetMeta{Name: u.Name, SzDecimals: u.SzDecimals}
	}
	return out, nil
}

// AllMids returns the current mid price for every coin.
func (c *Client) AllMids(ctx context.Context) (map[string]float64, error) {
	body := map[string]any{"type": "allMids"}
	var raw map[string]string
	if err := c.info(ctx, body, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(raw))
	for k, v := range raw {
		out[k] = atof(v)
	}
	return out, nil
}

// AccountState is the subset of clearinghouseState the agent needs: open
// positions plus margin totals.
type AccountState struct {
	Positions        []metrics.Position
	AccountValue     float64
	TotalNtlPosition float64
	Withdrawable     float64
}

type clearinghouseWire struct {
	AssetPositions []struct {
		Position struct {
			Coin           string `json:"coin"`
			Szi            string `json:"szi"`
			EntryPx        string `json:"entryPx"`
			PositionValue  string `json:"positionValue"`
			UnrealizedPnl  string `json:"unrealizedPnl"`
			LiquidationPx  string `json:"liquidationPx"`
			MarginUsedWire string `json:"marginUsed"`
		} `json:"position"`
	} `json:"assetPositions"`
	MarginSummary struct {
		AccountValue string `json:"accountValue"`
		TotalNtlPos  string `json:"totalNtlPos"`
	} `json:"marginSummary"`
	Withdrawable string `json:"withdrawable"`
}

// ClearinghouseState fetches a user's live perp account state.
func (c *Client) ClearinghouseState(ctx context.Context, user string) (AccountState, error) {
	body := map[string]any{"type": "clearinghouseState", "user": user}
	var raw clearinghouseWire
	if err := c.info(ctx, body, &raw); err != nil {
		return AccountState{}, err
	}
	st := AccountState{
		AccountValue:     atof(raw.MarginSummary.AccountValue),
		TotalNtlPosition: atof(raw.MarginSummary.TotalNtlPos),
		Withdrawable:     atof(raw.Withdrawable),
	}
	for _, ap := range raw.AssetPositions {
		size := atof(ap.Position.Szi)
		if size == 0 {
			continue
		}
		entry := atof(ap.Position.EntryPx)
		mark := entry
		if size != 0 && ap.Position.PositionValue != "" {
			mark = atof(ap.Position.PositionValue) / absf(size)
		}
		st.Positions = append(st.Positions, metrics.Position{
			Coin:       ap.Position.Coin,
			Size:       size,
			EntryPrice: entry,
			MarkPrice:  mark,
			UnrealPnl:  atof(ap.Position.UnrealizedPnl),
		})
	}
	return st, nil
}

// OpenOrder is one resting order from the openOrders info query.
type OpenOrder struct {
	Coin      string  `json:"coin"`
	Side      string  `json:"side"` // "B" buy / "A" ask (sell)
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	OID       uint64  `json:"oid"`
	Timestamp int64   `json:"timestamp"`
}

// OpenOrders lists a user's resting orders.
func (c *Client) OpenOrders(ctx context.Context, user string) ([]OpenOrder, error) {
	body := map[string]any{"type": "openOrders", "user": user}
	var raw []struct {
		Coin      string `json:"coin"`
		Side      string `json:"side"`
		LimitPx   string `json:"limitPx"`
		Sz        string `json:"sz"`
		OID       uint64 `json:"oid"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := c.info(ctx, body, &raw); err != nil {
		return nil, err
	}
	out := make([]OpenOrder, 0, len(raw))
	for _, r := range raw {
		out = append(out, OpenOrder{
			Coin: r.Coin, Side: r.Side, Price: atof(r.LimitPx),
			Size: atof(r.Sz), OID: r.OID, Timestamp: r.Timestamp,
		})
	}
	return out, nil
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
