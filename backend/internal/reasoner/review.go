// Review and trigger framing for the two-tier thesis pipeline. RoleReview asks
// the model to maintain a persistent thesis per asset (create/update/
// invalidate, plus an optional entry verdict); RoleTrigger asks for a standard
// verdict constrained to the live thesis direction. Both share the batch
// provider transport — this file owns only the prompts and the strict JSON
// extraction, mirroring prompt.go's verdict path.
package reasoner

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// ReviewSystemPrompt frames the scheduled thesis review. It pins the output
// schema: one object per asset carrying a thesis operation and an optional
// entry verdict.
const ReviewSystemPrompt = `You are a disciplined crypto perpetual-futures analyst for Hyperliquid,
maintaining a persistent directional thesis per asset. This is a scheduled
review on the asset's review timeframe — swing/position trading, not scalping.

Each digest carries: a multi-timeframe ladder (1h/4h/1d/1w closes with perp
metrics; missing rungs are listed, never fabricated), the current live thesis
(or null if none), any open position, ranked signals, and your recent journal.

For EACH asset decide whether to create a thesis, update the existing one, or
invalidate it. "neutral" is a real thesis meaning "stay out" — use it when the
structure is genuinely directionless; invalidate only when the prior thesis is
broken. Set "invalidation" to the price level that would prove the thesis
wrong; it is watched deterministically and a crossing forces this review.

Return ONLY a JSON array, one object per asset, no prose. Each object:
{
  "coin": string,
  "thesis": {
    "op": "create"|"update"|"invalidate",
    "direction": "long"|"short"|"neutral",
    "summary": string (the narrative you are maintaining, a few tight sentences),
    "invalidation": number (price level),
    "targets": [number],
    "horizon": "days"|"weeks",
    "confidence": number (0..1)
  },
  "verdict": { optional — only when the thesis warrants immediate positioning;
               same schema as a standard verdict:
               "asset", "timeframe", "action", "size_usd", "entry", "stop",
               "take_profit", "thesis", "reading", "confidence",
               "requires_confirmation" }
}
For "op":"invalidate" the remaining thesis fields may be omitted.
Never invent precision you don't have.`

// TriggerSystemPrompt frames the gate-fired deviation check. The output is the
// standard verdict schema; the direction constraint is stated here and
// enforced deterministically by the executor's thesis gate regardless.
const TriggerSystemPrompt = `You are a disciplined crypto perpetual-futures analyst for Hyperliquid.
A deterministic deviation rule fired on a low-timeframe close — this call is an
entry/exit check against your existing thesis, not a re-derivation of it.

Each digest carries: the deviation (rule, magnitude, timeframe), the current
live thesis (or null), any open position, the current bar's metrics, and a
compact higher-timeframe summary.

You may only act in the direction of the live thesis, or hold. No thesis, or a
deviation against the thesis direction → "hold" (the deviation may force a
review separately; that is not your job here). Closing an open position is
always allowed.

Reason about perp mechanics. For EACH asset, emit exactly one JSON verdict
object. Return ONLY a JSON array of verdict objects, no prose. Each object:
{
  "asset": string,
  "timeframe": string,
  "action": "open_short"|"open_long"|"close"|"scale"|"hold"|"alert_only",
  "size_usd": number,
  "entry": {"type": "limit"|"market", "price": number},
  "stop": number,
  "take_profit": number,
  "thesis": string (one tight sentence on the mechanics behind the call),
  "reading": string (one sentence on the deviation and what it implies),
  "confidence": number (0..1),
  "requires_confirmation": boolean
}
Prefer "hold" when the setup is unclear. Never invent precision you don't have.`

// reviewLadderTFs / triggerLadderTFs are the rung sets each digest kind is
// expected to carry (mirroring the batcher's ladder config); absent rungs are
// listed as missing in the prompt rather than silently dropped.
var (
	reviewLadderTFs  = []string{"1h", "4h", "1d", "1w"}
	triggerLadderTFs = []string{"4h", "1d"}
)

// BuildReviewPrompt serializes review digests into a single user message.
func BuildReviewPrompt(digests []metrics.Digest, extraContext string) string {
	var b strings.Builder
	if extraContext != "" {
		b.WriteString(extraContext)
		b.WriteString("\n\n")
	}
	b.WriteString("Review digests:\n")
	for _, d := range digests {
		b.WriteString(formatReviewDigest(d))
		b.WriteString("\n")
	}
	b.WriteString("\nReturn a JSON array with one thesis object per asset above.")
	return b.String()
}

// BuildTriggerPrompt serializes trigger digests into a single user message.
func BuildTriggerPrompt(digests []metrics.Digest, extraContext string) string {
	var b strings.Builder
	if extraContext != "" {
		b.WriteString(extraContext)
		b.WriteString("\n\n")
	}
	b.WriteString("Trigger digests:\n")
	for _, d := range digests {
		b.WriteString(formatTriggerDigest(d))
		b.WriteString("\n")
	}
	b.WriteString("\nReturn a JSON array of verdicts, one per asset above.")
	return b.String()
}

// ladderPoint is one compact ladder bar for the review prompt: close time,
// close, return, and the perp-regime deltas that matter for structure.
type ladderPoint struct {
	T    string  `json:"t"`
	C    float64 `json:"c"`
	Ret  float64 `json:"ret"`
	Fund float64 `json:"fund"`
	OIΔ  float64 `json:"oi_d"`
	CVD  float64 `json:"cvd"`
}

func ladderPoints(bars []metrics.Bar) []ladderPoint {
	out := make([]ladderPoint, 0, len(bars))
	for _, b := range bars {
		out = append(out, ladderPoint{
			T:    b.CloseTime.Format("01-02T15:04"),
			C:    round(b.Close, 6),
			Ret:  round(b.Return, 5),
			Fund: round(b.Funding, 6),
			OIΔ:  round(b.OIDelta, 4),
			CVD:  round(b.CVD, 2),
		})
	}
	return out
}

// missingRungs lists the expected rungs the digest's ladder lacks, so the
// prompt states the warm-up gap instead of the model hallucinating history.
func missingRungs(d metrics.Digest, expected []string) []string {
	missing := []string{}
	for _, tf := range expected {
		if len(d.Ladder[tf]) == 0 {
			missing = append(missing, tf)
		}
	}
	return missing
}

// thesisPayload renders the live thesis for a prompt, nil when none exists.
func thesisPayload(t *metrics.Thesis) any {
	if t == nil {
		return nil
	}
	return map[string]any{
		"direction":    t.Direction,
		"summary":      t.Summary,
		"invalidation": round(t.Invalidation, 6),
		"targets":      t.Targets,
		"horizon":      t.Horizon,
		"confidence":   round(t.Confidence, 2),
		"reviewed_at":  t.ReviewedAt.Format(time.RFC3339),
		"version":      t.Version,
	}
}

// formatReviewDigest renders one review digest: the full ladder plus thesis,
// position, signals, and journal memory — everything a scheduled review needs
// to maintain (or break) the narrative.
func formatReviewDigest(d metrics.Digest) string {
	ladder := make(map[string]any, len(d.Ladder))
	for tf, bars := range d.Ladder {
		ladder[tf] = ladderPoints(bars)
	}
	sigs := signalPayload(d)
	payload := map[string]any{
		"asset":            d.Coin,
		"review_timeframe": d.Timeframe,
		"at":               d.At.Format(time.RFC3339),
		"thesis":           thesisPayload(d.Thesis),
		"ladder":           ladder,
		"ladder_missing":   missingRungs(d, reviewLadderTFs),
		"signals":          sigs,
		"current":          currentPayload(d.Current),
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
	if d.Deviation != nil {
		// A forced review carries the deviation that demanded it.
		payload["forced_by"] = d.Deviation
	}
	j, _ := json.Marshal(payload)
	return string(j)
}

// formatTriggerDigest renders one trigger digest: the deviation front and
// center, the thesis it must trade with, and a compact HTF closes summary.
func formatTriggerDigest(d metrics.Digest) string {
	htf := make(map[string]any, len(d.Ladder))
	for tf, bars := range d.Ladder {
		closes := make([]float64, 0, len(bars))
		for _, b := range bars {
			closes = append(closes, round(b.Close, 6))
		}
		htf[tf] = closes
	}
	payload := map[string]any{
		"asset":       d.Coin,
		"timeframe":   d.Timeframe,
		"at":          d.At.Format(time.RFC3339),
		"deviation":   d.Deviation,
		"thesis":      thesisPayload(d.Thesis),
		"htf_closes":  htf,
		"htf_missing": missingRungs(d, triggerLadderTFs),
		"current":     currentPayload(d.Current),
		"position": map[string]any{
			"size": d.Position.Size, "entry": d.Position.EntryPrice,
			"unreal_pnl": round(d.Position.UnrealPnl, 2),
		},
		"config": map[string]any{
			"requires_confirmation": d.StrategyCfg.RequiresConfirmation,
			"max_position_usd":      d.StrategyCfg.MaxPositionUSD,
		},
	}
	j, _ := json.Marshal(payload)
	return string(j)
}

// ThesisReview is one parsed, validated review item: the thesis operation the
// model chose for a coin, plus an optional entry verdict.
type ThesisReview struct {
	Coin    string
	Op      string // "create" | "update" | "invalidate"
	Thesis  metrics.Thesis
	Verdict *Verdict
}

// reviewItem is the raw wire shape of one review response element.
type reviewItem struct {
	Coin   string `json:"coin"`
	Thesis *struct {
		Op           string    `json:"op"`
		Direction    string    `json:"direction"`
		Summary      string    `json:"summary"`
		Invalidation float64   `json:"invalidation"`
		Targets      []float64 `json:"targets"`
		Horizon      string    `json:"horizon"`
		Confidence   float64   `json:"confidence"`
	} `json:"thesis"`
	Verdict *Verdict `json:"verdict"`
}

// ParseThesisReviews extracts and validates the review array from a model's
// raw text, mirroring ParseVerdicts: locate the outermost JSON, decode, and
// discard any element that fails validation — a malformed item never mutates
// the thesis store (the prior version stays). discarded carries one line per
// dropped element so the caller can journal them.
func ParseThesisReviews(raw, provider string) (reviews []ThesisReview, discarded []string, err error) {
	var items []reviewItem
	arrIdx := strings.IndexByte(raw, '[')
	objIdx := strings.IndexByte(raw, '{')
	switch {
	case arrIdx >= 0 && (objIdx < 0 || arrIdx < objIdx):
		if err := json.Unmarshal([]byte(extractJSONArray(raw)), &items); err != nil {
			return nil, nil, fmt.Errorf("reasoner: decode thesis reviews: %w", err)
		}
	case objIdx >= 0:
		var single reviewItem
		if err := json.Unmarshal([]byte(extractJSONObject(raw)), &single); err != nil {
			return nil, nil, fmt.Errorf("reasoner: decode thesis review: %w", err)
		}
		items = []reviewItem{single}
	default:
		return nil, nil, fmt.Errorf("reasoner: no JSON thesis review found in response")
	}

	now := time.Now()
	for _, it := range items {
		if reason := validateReviewItem(it); reason != "" {
			discarded = append(discarded, reason)
			continue
		}
		rv := ThesisReview{Coin: it.Coin, Op: it.Thesis.Op}
		if rv.Op != "invalidate" {
			rv.Thesis = metrics.Thesis{
				Coin:         it.Coin,
				Direction:    it.Thesis.Direction,
				Summary:      it.Thesis.Summary,
				Invalidation: it.Thesis.Invalidation,
				Targets:      it.Thesis.Targets,
				Horizon:      it.Thesis.Horizon,
				Confidence:   it.Thesis.Confidence,
			}
		}
		if it.Verdict != nil {
			v := *it.Verdict
			v.At = now
			v.Provider = provider
			v.RawText = raw
			if v.Validate() == nil {
				rv.Verdict = &v
			} else {
				// A bad attached verdict never sinks the thesis operation.
				discarded = append(discarded, fmt.Sprintf("%s: attached verdict invalid", it.Coin))
			}
		}
		reviews = append(reviews, rv)
	}
	return reviews, discarded, nil
}

// validateReviewItem returns a human-readable reason when the item must be
// discarded, or "" when it is acceptable.
func validateReviewItem(it reviewItem) string {
	if it.Coin == "" {
		return "review item: empty coin"
	}
	if it.Thesis == nil {
		return it.Coin + ": missing thesis object"
	}
	switch it.Thesis.Op {
	case "invalidate":
		return "" // the remaining fields are irrelevant
	case "create", "update":
	default:
		return fmt.Sprintf("%s: invalid thesis op %q", it.Coin, it.Thesis.Op)
	}
	switch it.Thesis.Direction {
	case "long", "short", "neutral":
	default:
		return fmt.Sprintf("%s: invalid thesis direction %q", it.Coin, it.Thesis.Direction)
	}
	if c := it.Thesis.Confidence; c < 0 || c > 1 {
		return fmt.Sprintf("%s: thesis confidence out of range", it.Coin)
	}
	return ""
}
