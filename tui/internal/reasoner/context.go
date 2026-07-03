package reasoner

import (
	"fmt"
	"strings"

	"github.com/hyperagent/hyperagent/internal/signal"
	"github.com/hyperagent/hyperagent/internal/store"
)

// StandardTimeframes is the fixed cross-timeframe set used for confluence
// grounding in chat context — the same set the TUI cycles through with 't'
// ({"15m","1h","4h","1d"}). It does not depend on the caller's display
// timeframe: confluence is always read across the full set.
var StandardTimeframes = []string{"15m", "1h", "4h", "1d"}

// BuildChatContext assembles the grounding text handed to the chat LLM for a
// given coin: a snapshot of live metrics plus ranked cross-timeframe
// confluence. This mirrors what the TUI's chat pane builds (internal/tui's
// former chatContext, extracted here so the HTTP API's /api/chat endpoint
// and the TUI share one implementation) so the agent answers from the same
// normalized read the panel shows — not raw numbers alone. tf is the coin's
// display timeframe (drives the headline bar); confluence always spans
// StandardTimeframes regardless of tf.
func BuildChatContext(st *store.Store, coin, tf string) string {
	if coin == "" {
		return ""
	}
	bar, ok := st.LatestBar(coin, tf)
	if !ok {
		return fmt.Sprintf("Selected asset: %s (%s). No live bar yet.", coin, tf)
	}

	var b strings.Builder
	fmt.Fprintf(&b,
		"Selected asset %s (display tf %s): close %.4f  return %+.3f%%  funding %+.4f%%  OIΔ %+.2f%%  CVD %.0f.  Position size %.4f.",
		coin, tf, bar.Close, bar.Return*100, bar.Funding*100, bar.OIDelta*100, bar.CVD, st.Position(coin).Size)

	// Hand the model the cross-timeframe confluence — structure, not canned prose —
	// so it writes the interpretation grounded in what actually aligns across 15m-1d.
	if conf := chatConfluence(st, coin); len(conf) > 0 {
		parts := make([]string, 0, len(conf))
		for _, c := range conf[:min(5, len(conf))] {
			dir := "context"
			if c.Directional {
				if c.Score > 0 {
					dir = "bullish"
				} else {
					dir = "bearish"
				}
			}
			parts = append(parts, fmt.Sprintf("%s (%s; aligns on %s; strength %.0f%%)",
				c.Label, dir, strings.Join(c.Timeframes, "/"), c.Strength*100))
		}
		b.WriteString("\nCross-timeframe signals (ranked): " + strings.Join(parts, "; "))
	}
	return b.String()
}

// chatConfluence computes ranked cross-timeframe confluence for coin from the
// store across StandardTimeframes, weighted so higher timeframes count more —
// the same computation the TUI's markets/detail panels use to render the SIG
// column, reused here for chat grounding.
func chatConfluence(st *store.Store, coin string) []signal.Confluence {
	weights := signal.DefaultWeights()
	ctx, _ := st.AssetCtx(coin)
	tfs := make([]signal.TimeframeInput, 0, len(StandardTimeframes))
	for _, tf := range StandardTimeframes {
		bar, ok := st.LatestBar(coin, tf)
		if !ok {
			continue
		}
		tfs = append(tfs, signal.TimeframeInput{
			Timeframe: tf,
			Weight:    weights[tf],
			In:        signal.Inputs{Cur: bar, History: st.History(coin, tf, 48), Ctx: ctx},
		})
	}
	return signal.Aggregate(tfs)
}
