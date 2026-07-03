// Package thesis fetches multi-timeframe Hyperliquid perp data and assembles a
// compact grounding block for the thesis LLM prompt. It always pulls fresh from
// the HL REST endpoint — no cached store data — so the thesis is anchored to
// real OHLCV structure, not just the live tick the store holds.
package thesis

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// tfLadder is the ordered set of perp timeframes, lowest to highest.
var tfLadder = []string{"15m", "1h", "4h", "1d"}

// candleLimit returns how many bars to fetch per timeframe.
func candleLimit(tf string) int {
	switch tf {
	case "15m":
		return 48
	case "1h":
		return 48
	case "4h":
		return 30
	case "1d":
		return 20
	}
	return 30
}

// ladderFrom returns the display TF and all higher TFs in the ladder.
// If displayTF is not in the ladder, returns the full ladder.
func ladderFrom(displayTF string) []string {
	for i, tf := range tfLadder {
		if tf == displayTF {
			return tfLadder[i:]
		}
	}
	return tfLadder
}

// tfContext holds the derived structure for one timeframe.
type tfContext struct {
	tf        string
	n         int
	close     float64
	open      float64
	swingHigh float64
	swingLow  float64
	rangePos  float64 // 0=at low, 1=at high
	returnPct float64 // return over the window
	vol       float64 // realized vol (annualized %)
	trend     string  // "up" | "down" | "sideways"
}

// deriveTF extracts structure from a slice of bars (oldest first).
func deriveTF(tf string, bars []metrics.Bar) tfContext {
	if len(bars) == 0 {
		return tfContext{tf: tf}
	}
	hi := bars[0].High
	lo := bars[0].Low
	var retSumSq float64
	for _, b := range bars {
		if b.High > hi {
			hi = b.High
		}
		if b.Low < lo {
			lo = b.Low
		}
		r := b.Return
		retSumSq += r * r
	}
	first := bars[0].Open
	last := bars[len(bars)-1].Close

	// Annualized realized vol (bars-per-year varies by tf).
	barsPerYear := barsPerYearFor(tf)
	vol := math.Sqrt(retSumSq/float64(len(bars))*barsPerYear) * 100

	// Range position of the current close within the window's high/low.
	rng := hi - lo
	rangePos := 0.5
	if rng > 0 {
		rangePos = (last - lo) / rng
	}

	// Trend: compare last close to simple midpoint of window open and range.
	totalRet := 0.0
	if first > 0 {
		totalRet = (last - first) / first * 100
	}
	trend := "sideways"
	if totalRet > 2 {
		trend = "up"
	} else if totalRet < -2 {
		trend = "down"
	}

	return tfContext{
		tf:        tf,
		n:         len(bars),
		close:     last,
		open:      first,
		swingHigh: hi,
		swingLow:  lo,
		rangePos:  rangePos,
		returnPct: totalRet,
		vol:       vol,
		trend:     trend,
	}
}

func barsPerYearFor(tf string) float64 {
	switch tf {
	case "15m":
		return 365 * 24 * 4
	case "1h":
		return 365 * 24
	case "4h":
		return 365 * 6
	case "1d":
		return 365
	}
	return 365 * 24
}

// FetchContext fetches multi-TF candle data and live perp context for coin from
// the HL REST endpoint. Returns a compact, markdown-friendly grounding block
// suitable for injection into a thesis LLM prompt.
func FetchContext(ctx context.Context, client *hlclient.Client, coin, displayTF string) (string, error) {
	tfs := ladderFrom(displayTF)
	now := time.Now().UTC()

	// Fetch each TF concurrently via goroutines, collect results in order.
	type result struct {
		ctx tfContext
		err error
	}
	results := make([]result, len(tfs))
	done := make(chan struct{}, len(tfs))

	for i, tf := range tfs {
		go func() {
			limit := candleLimit(tf)
			dur := tfDuration(tf) * time.Duration(limit)
			start := now.Add(-dur)
			bars, err := client.CandleSnapshot(ctx, coin, tf, start, now)
			if err != nil {
				results[i] = result{err: err}
			} else {
				results[i] = result{ctx: deriveTF(tf, bars)}
			}
			done <- struct{}{}
		}()
	}
	for range tfs {
		<-done
	}

	// Fetch live perp context (funding, OI, basis, mark).
	assetCtxs, ctxErr := client.MetaAndAssetCtxs(ctx)
	var perpCtx metrics.AssetCtx
	if ctxErr == nil {
		perpCtx = assetCtxs[coin]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## %s Hyperliquid Perp Data\n\n", coin)

	// Live perp regime.
	if ctxErr == nil && perpCtx.MarkPrice > 0 {
		fmt.Fprintf(&b, "**Live perp:** mark %.4f  oracle %.4f  funding %+.4f%%/h  OI %.0f  day volume %.0f  premium %+.4f%%\n\n",
			perpCtx.MarkPrice, perpCtx.OraclePrice,
			perpCtx.Funding*100, perpCtx.OpenInterest,
			perpCtx.DayVolume, perpCtx.Premium*100)
	}

	// Multi-TF structure.
	b.WriteString("**Multi-TF structure** (display TF first):\n\n")
	anyData := false
	for i, tf := range tfs {
		r := results[i]
		if r.err != nil {
			fmt.Fprintf(&b, "- **%s**: fetch error: %v\n", tf, r.err)
			continue
		}
		tc := r.ctx
		if tc.n == 0 {
			fmt.Fprintf(&b, "- **%s**: no data\n", tf)
			continue
		}
		anyData = true
		fmt.Fprintf(&b,
			"- **%s** (%d bars): close %.4f  trend %s (%+.1f%% over window)  swing H %.4f / L %.4f  range-pos %.0f%%  vol %.1f%% ann\n",
			tf, tc.n, tc.close, tc.trend, tc.returnPct,
			tc.swingHigh, tc.swingLow, tc.rangePos*100, tc.vol)
	}
	if !anyData {
		return "", fmt.Errorf("thesis: no candle data returned for %s", coin)
	}

	b.WriteString("\n**Use the structure above to identify directional bias, key levels (swing H/L as S/R), and the perp regime (funding direction + OI trend) as the primary mechanistic thesis.**\n")
	return b.String(), nil
}

// tfDuration returns the duration of one bar for a timeframe string.
func tfDuration(tf string) time.Duration {
	switch tf {
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	case "1d":
		return 24 * time.Hour
	}
	return time.Hour
}
