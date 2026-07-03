// Prompt construction and response parsing shared by every provider adapter.
// Keeping this provider-agnostic means a new backend only implements the HTTP
// transport; the framing, the digest serialization, and the strict JSON
// extraction are written once here.
package reasoner

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/signal"
)

// SystemPrompt is the framing every batch request carries. It tells the model it
// is a perp trading analyst and pins the output schema.
const SystemPrompt = `You are a disciplined crypto perpetual-futures analyst for Hyperliquid.
You receive a digest per asset: current bar metrics, recent history, perp regime
(funding, open interest, basis, liquidation proximity), flow (CVD, trade
imbalance), cross-asset strength, plus any open position and your own recent
journal notes.

Each digest also carries a ranked "signals" array: pre-computed, normalized
interpretations of the raw metrics (OI-vs-price regime, funding z-scored against the
asset's own history, CVD divergence, vol regime, relative strength). Each has a label,
a signed score (− bearish, + bullish), a 0..1 strength, and a one-line read. Treat the
signals as your primary read and use the raw numbers to confirm or refine — they are
the interpretation layer, normalized so "is this extreme?" already has an answer.

Reason about perp mechanics on the asset's timeframe — this is swing/position
trading, not scalping. For EACH asset, emit exactly one JSON verdict object.

Return ONLY a JSON array of verdict objects, no prose. Each object:
{
  "asset": string,
  "timeframe": string,
  "action": "open_short"|"open_long"|"close"|"scale"|"hold"|"alert_only",
  "size_usd": number,
  "entry": {"type": "limit"|"market", "price": number},
  "stop": number,
  "take_profit": number,
  "thesis": string (one tight sentence on the mechanics behind the call),
  "reading": string (one sentence on the current OI and funding rate regime: direction, magnitude, and what it implies for positioning),
  "confidence": number (0..1),
  "requires_confirmation": boolean
}
Prefer "hold" when the setup is unclear. Never invent precision you don't have.`

// ChatSystemPrompt frames the interactive chat role.
const ChatSystemPrompt = `You are the trading agent's analyst, answering questions
about the live market state on Hyperliquid. You have access to current metrics,
history, and the journal. Be concise, specific, and grounded in the data you are
given. Speak in terms of perp mechanics. Plain prose, no JSON.`

// BuildBatchPrompt serializes the gated digests into a single user message.
func BuildBatchPrompt(digests []metrics.Digest, extraContext string) string {
	var b strings.Builder
	if extraContext != "" {
		b.WriteString(extraContext)
		b.WriteString("\n\n")
	}
	b.WriteString("Digests:\n")
	for _, d := range digests {
		b.WriteString(formatDigest(d))
		b.WriteString("\n")
	}
	b.WriteString("\nReturn a JSON array of verdicts, one per asset above.")
	return b.String()
}

// formatDigest renders one digest as a compact, model-readable block. We hand
// the model a small JSON object rather than free text so it parses reliably.
func formatDigest(d metrics.Digest) string {
	type histPoint struct {
		T     string  `json:"t"`
		Close float64 `json:"c"`
		Ret   float64 `json:"ret"`
		Fund  float64 `json:"fund"`
		OIΔ   float64 `json:"oi_d"`
		CVD   float64 `json:"cvd"`
	}
	hist := make([]histPoint, 0, len(d.History))
	for _, b := range d.History {
		hist = append(hist, histPoint{
			T:     b.CloseTime.Format("01-02T15:04"),
			Close: round(b.Close, 6),
			Ret:   round(b.Return, 5),
			Fund:  round(b.Funding, 6),
			OIΔ:   round(b.OIDelta, 4),
			CVD:   round(b.CVD, 2),
		})
	}
	c := d.Current

	// Interpreted signals: the normalized, ranked read of the raw metrics. Handing
	// the model interpretation — not just numbers — is the whole point of the layer.
	sigs := signal.Compute(signal.Inputs{Cur: d.Current, History: d.History})
	type sigOut struct {
		Key      string  `json:"key"`
		Label    string  `json:"label"`
		Score    float64 `json:"score"`
		Strength float64 `json:"strength"`
		Read     string  `json:"read"`
	}
	sigList := make([]sigOut, 0, len(sigs))
	for _, s := range sigs {
		sigList = append(sigList, sigOut{s.Key, s.Label, round(s.Score, 2), round(s.Strength, 2), s.Read})
	}

	payload := map[string]any{
		"asset":     d.Coin,
		"timeframe": d.Timeframe,
		"at":        d.At.Format(time.RFC3339),
		"signals":   sigList,
		"current": map[string]any{
			"close": round(c.Close, 6), "return": round(c.Return, 5),
			"realized_vol": round(c.RealizedVol, 5), "range_pos": round(c.RangePos, 3),
			"funding": round(c.Funding, 6), "funding_delta": round(c.FundingDelta, 6),
			"oi": round(c.OpenInterest, 2), "oi_delta": round(c.OIDelta, 4),
			"basis": round(c.Basis, 5), "cvd": round(c.CVD, 2),
			"trade_imbalance": round(c.TradeImbal, 3), "liq_proximity": round(c.LiqProx, 3),
			"btc_corr": round(c.BTCCorr, 3), "rel_strength": round(c.RelStrength, 5),
			"mark": round(c.MarkPrice, 6),
		},
		"history": hist,
		"position": map[string]any{
			"size": d.Position.Size, "entry": d.Position.EntryPrice,
			"unreal_pnl": round(d.Position.UnrealPnl, 2),
		},
		"config": map[string]any{
			"requires_confirmation": d.StrategyCfg.RequiresConfirmation,
			"max_position_usd":      d.StrategyCfg.MaxPositionUSD,
		},
		"recent_journal": d.RecentJournal,
	}
	j, _ := json.Marshal(payload)
	return string(j)
}

func round(f float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int64(f*p+sign(f)*0.5)) / p
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// ParseVerdicts extracts the JSON verdict array from a model's raw text. Models
// sometimes wrap JSON in prose or code fences; we locate the outermost array and
// decode it, discarding any element that fails validation.
func ParseVerdicts(raw string, provider string) ([]Verdict, error) {
	var verdicts []Verdict
	arrIdx := strings.IndexByte(raw, '[')
	objIdx := strings.IndexByte(raw, '{')
	switch {
	// An array response: '[' exists and is not preceded by a stray '{' (which
	// would mean a single object whose fields merely contain an array).
	case arrIdx >= 0 && (objIdx < 0 || arrIdx < objIdx):
		if err := json.Unmarshal([]byte(extractJSONArray(raw)), &verdicts); err != nil {
			return nil, fmt.Errorf("reasoner: decode verdicts: %w", err)
		}
	// A single object response.
	case objIdx >= 0:
		var single Verdict
		if err := json.Unmarshal([]byte(extractJSONObject(raw)), &single); err != nil {
			return nil, fmt.Errorf("reasoner: decode verdict: %w", err)
		}
		verdicts = []Verdict{single}
	default:
		return nil, fmt.Errorf("reasoner: no JSON verdict found in response")
	}
	now := time.Now()
	valid := verdicts[:0]
	for _, v := range verdicts {
		v.At = now
		v.Provider = provider
		v.RawText = raw
		if err := v.Validate(); err != nil {
			continue // malformed → discarded (never executed)
		}
		valid = append(valid, v)
	}
	return valid, nil
}

// extractJSONArray returns the substring from the first '[' to its matching ']'.
func extractJSONArray(s string) string { return extractBracketed(s, '[', ']') }

// extractJSONObject returns the substring from the first '{' to its matching '}'.
func extractJSONObject(s string) string { return extractBracketed(s, '{', '}') }

func extractBracketed(s string, open, close byte) string {
	start := strings.IndexByte(s, open)
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		switch {
		case esc:
			esc = false
		case ch == '\\' && inStr:
			esc = true
		case ch == '"':
			inStr = !inStr
		case inStr:
			// skip
		case ch == open:
			depth++
		case ch == close:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
