// Package marketdata supplies historical price history from sources independent
// of Hyperliquid: the CoinGecko free-tier OHLC API and local CSV files. This is
// the plan's "warm the rings from disk + REST backfill so the agent has context
// immediately" — broadened so a fresh install has a real historical sample even
// for assets HL's candleSnapshot is thin on, and so the system runs fully offline
// from a CSV corpus with no API keys at all.
package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// CoinGeckoBaseURL is the free, unauthenticated public API. No key required; the
// only constraint is a loose rate limit, which warm-up's handful of calls respect.
const CoinGeckoBaseURL = "https://api.coingecko.com/api/v3"

// DefaultIDs maps Hyperliquid coin symbols to CoinGecko coin ids. CoinGecko keys
// markets by a slug, not a ticker, so the mapping is explicit. Extend via config.
var DefaultIDs = map[string]string{
	"BTC":    "bitcoin",
	"ETH":    "ethereum",
	"SOL":    "solana",
	"HYPE":   "hyperliquid",
	"ARB":    "arbitrum",
	"OP":     "optimism",
	"AVAX":   "avalanche-2",
	"MATIC":  "matic-network",
	"DOGE":   "dogecoin",
	"LINK":   "chainlink",
	"SUI":    "sui",
	"APT":    "aptos",
	"TIA":    "celestia",
	"INJ":    "injective-protocol",
	"SEI":    "sei-network",
	"WLD":    "worldcoin-wld",
	"PEPE":   "pepe",
	"WIF":    "dogwifcoin",
	"BNB":    "binancecoin",
	"XRP":    "ripple",
	"BONK":   "bonk",
	"PENDLE": "pendle",
	"ENA":    "ethena",
	"JUP":    "jupiter-exchange-solana",
	"NEAR":   "near",
	"PYTH":   "pyth-network",
	"W":      "wormhole",
	"ONDO":   "ondo-finance",
	"ETHFI":  "ether-fi",
	"EIGEN":  "eigenlayer",
}

// CoinGecko is a thin client over the public OHLC endpoint. Stdlib only, matching
// the plan's dependency posture: net/http + encoding/json, no SDK.
type CoinGecko struct {
	baseURL string
	http    *http.Client
	ids     map[string]string
}

// NewCoinGecko returns a client. idOverrides is merged over DefaultIDs so config
// can add or correct symbol→id mappings without touching code.
func NewCoinGecko(idOverrides map[string]string) *CoinGecko {
	ids := make(map[string]string, len(DefaultIDs)+len(idOverrides))
	for k, v := range DefaultIDs {
		ids[k] = v
	}
	for k, v := range idOverrides {
		ids[k] = v
	}
	return &CoinGecko{
		baseURL: CoinGeckoBaseURL,
		http:    &http.Client{Timeout: 20 * time.Second},
		ids:     ids,
	}
}

// ID resolves a Hyperliquid symbol to its CoinGecko id, falling back to the
// lower-cased symbol (which is correct for many slugs) if unmapped.
func (c *CoinGecko) ID(coin string) (string, bool) {
	if id, ok := c.ids[coin]; ok {
		return id, true
	}
	return "", false
}

// daysForBars picks the smallest CoinGecko `days` window that yields at least
// `want` bars at the timeframe's native candle width, clamped to the values the
// free tier accepts. CoinGecko fixes OHLC granularity by window size (≤2d→30m,
// ≤30d→4h, >30d→4d), so we choose the window, not the interval.
func daysForBars(tf string, want int) int {
	dur := timeframeDuration(tf)
	needed := dur * time.Duration(want)
	days := int(math.Ceil(needed.Hours() / 24))
	switch {
	case days <= 1:
		return 1
	case days <= 7:
		return 7
	case days <= 14:
		return 14
	case days <= 30:
		return 30
	case days <= 90:
		return 90
	case days <= 180:
		return 180
	default:
		return 365
	}
}

// OHLC fetches historical candles for a coin and returns enriched bars
// (close-to-close return and range position filled) tagged with the requested
// timeframe. CoinGecko's OHLC endpoint carries no volume, so Volume stays zero —
// the live websocket fills volume forward once the daemon is running.
func (c *CoinGecko) OHLC(ctx context.Context, coin, tf string, want int) ([]metrics.Bar, error) {
	id, ok := c.ID(coin)
	if !ok {
		return nil, fmt.Errorf("marketdata: no CoinGecko id for %q", coin)
	}
	days := daysForBars(tf, want)
	url := fmt.Sprintf("%s/coins/%s/ohlc?vs_currency=usd&days=%d", c.baseURL, id, days)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("marketdata: request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketdata: get %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketdata: coingecko status %d for %s", resp.StatusCode, id)
	}

	// Wire shape: [[t_ms, open, high, low, close], ...].
	var raw [][]float64
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("marketdata: decode %s: %w", id, err)
	}

	bars := make([]metrics.Bar, 0, len(raw))
	step := timeframeDuration(tf)
	for _, r := range raw {
		if len(r) < 5 {
			continue
		}
		open := time.UnixMilli(int64(r[0]))
		bars = append(bars, metrics.Bar{
			Coin:      coin,
			Timeframe: tf,
			OpenTime:  open,
			CloseTime: open.Add(step),
			Open:      r[1],
			High:      r[2],
			Low:       r[3],
			Close:     r[4],
		})
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].OpenTime.Before(bars[j].OpenTime) })
	enrich(bars)
	if want > 0 && len(bars) > want {
		bars = bars[len(bars)-want:]
	}
	return bars, nil
}
