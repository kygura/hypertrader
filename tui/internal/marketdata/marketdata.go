package marketdata

import (
	"context"
	"math"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// Source orchestrates the independent historical providers behind one call. The
// order is deliberate: local CSV first (offline, deterministic, free), then the
// CoinGecko network fallback. A nil/zero Source is safe — Backfill returns
// (nil, nil), so callers can wire it unconditionally.
type Source struct {
	csvDir string
	cg     *CoinGecko
}

// Config selects which providers are active.
type Config struct {
	// CSVDir, if set, is searched first for "<COIN>_<tf>.csv" style files.
	CSVDir string
	// UseCoinGecko enables the network fallback.
	UseCoinGecko bool
	// IDOverrides merges over the built-in symbol→CoinGecko-id map.
	IDOverrides map[string]string
}

// New builds a Source from config. CoinGecko is only constructed when enabled so
// the offline/CSV-only path makes no network client at all.
func New(cfg Config) *Source {
	s := &Source{csvDir: cfg.CSVDir}
	if cfg.UseCoinGecko {
		s.cg = NewCoinGecko(cfg.IDOverrides)
	}
	return s
}

// Enabled reports whether any provider is configured.
func (s *Source) Enabled() bool {
	return s != nil && (s.csvDir != "" || s.cg != nil)
}

// Backfill returns up to `want` enriched bars for coin/tf, trying each configured
// provider in order and returning the first non-empty result. The string return
// names the provider that supplied the data, for logging.
func (s *Source) Backfill(ctx context.Context, coin, tf string, want int) ([]metrics.Bar, string, error) {
	if !s.Enabled() {
		return nil, "", nil
	}
	if s.csvDir != "" {
		if bars, err := LoadCSV(s.csvDir, coin, tf, want); err == nil && len(bars) > 0 {
			return bars, "csv", nil
		}
	}
	if s.cg != nil {
		bars, err := s.cg.OHLC(ctx, coin, tf, want)
		if err != nil {
			return nil, "", err
		}
		if len(bars) > 0 {
			return bars, "coingecko", nil
		}
	}
	return nil, "", nil
}

// enrich fills the pure price-derived fields (close-to-close return and range
// position) on an oldest-first bar slice. These are exactly the fields the TUI
// sparklines and bar rows read, so a warmed historical series renders with real
// shape immediately — not flat zeros until the live feed catches up.
func enrich(bars []metrics.Bar) {
	for i := range bars {
		b := &bars[i]
		if span := b.High - b.Low; span > 0 {
			b.RangePos = (b.Close - b.Low) / span
		} else {
			b.RangePos = 0.5
		}
		if i > 0 {
			if prev := bars[i-1].Close; prev != 0 {
				b.Return = (b.Close - prev) / prev
			}
		}
	}
}

// timeframeDuration parses Hyperliquid-style interval notation ("15m","1h","4h",
// "1d") into a Duration. Unknown input defaults to one hour.
func timeframeDuration(tf string) time.Duration {
	if len(tf) < 2 {
		return time.Hour
	}
	unit := tf[len(tf)-1]
	n, ok := atoiPrefix(tf[:len(tf)-1])
	if !ok || n <= 0 {
		return time.Hour
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute
	case 'h':
		return time.Duration(n) * time.Hour
	case 'd':
		return time.Duration(n) * 24 * time.Hour
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

// atoiPrefix parses an all-digit string, returning ok=false on any non-digit.
func atoiPrefix(s string) (n int, ok bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

func maxf(a, b float64) float64 { return math.Max(a, b) }
func minf(a, b float64) float64 { return math.Min(a, b) }
