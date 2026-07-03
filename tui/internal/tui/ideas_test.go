package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

func verdict(asset string, action metrics.Action, conf float64, thesis string) metrics.Verdict {
	return metrics.Verdict{
		Asset: asset, Timeframe: "1h", Action: action, Confidence: conf,
		SizeUSD: 2500, Entry: metrics.Entry{Type: "limit", Price: 41.20},
		Stop: 43.10, TakeProfit: 37.50, Thesis: thesis,
	}
}

// TestUpsertCandidateRanksByConfidence verifies the board's contract: one
// candidate per asset (latest verdict wins) ranked by confidence descending.
func TestUpsertCandidateRanksByConfidence(t *testing.T) {
	m, _ := newTestModel(t)
	m.upsertCandidate(verdict("ETH", metrics.ActionHold, 0.30, "chop"))
	m.upsertCandidate(verdict("HYPE", metrics.ActionOpenShort, 0.72, "lower-high into 43"))
	m.upsertCandidate(verdict("BTC", metrics.ActionOpenLong, 0.55, "reclaim of range mid"))

	got := make([]string, 0, len(m.candidates))
	for _, c := range m.candidates {
		got = append(got, c.v.Asset)
	}
	want := []string{"HYPE", "BTC", "ETH"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rank order = %v, want %v", got, want)
		}
	}

	// A newer verdict for the same asset replaces, not appends — and re-ranks.
	m.upsertCandidate(verdict("ETH", metrics.ActionOpenLong, 0.90, "squeeze building"))
	if len(m.candidates) != 3 {
		t.Fatalf("dedupe failed: %d candidates, want 3", len(m.candidates))
	}
	if m.candidates[0].v.Asset != "ETH" {
		t.Fatalf("re-rank failed: top is %s, want ETH", m.candidates[0].v.Asset)
	}
}

// TestRenderIdeasShowsRankedRows verifies the rendered board: rank markers,
// asset, action, a confidence bar, levels, and the thesis text.
func TestRenderIdeasShowsRankedRows(t *testing.T) {
	m, _ := newTestModel(t)
	m.upsertCandidate(verdict("HYPE", metrics.ActionOpenShort, 0.72, "lower-high into 43; funding flipped"))
	m.upsertCandidate(verdict("ETH", metrics.ActionHold, 0.30, "chop, no edge"))

	body := ansi.Strip(m.renderIdeasBody(80))
	for _, want := range []string{
		"#1", "HYPE", "open_short", "72%",
		"41.20", "43.10", "37.50", "lower-high into 43",
		"#2", "ETH", "hold",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ideas board missing %q:\n%s", want, body)
		}
	}
	if !strings.ContainsAny(body, "█▏▎▍▌▋▊▉") {
		t.Errorf("ideas board should render confidence bars:\n%s", body)
	}
	if i1, i2 := strings.Index(body, "HYPE"), strings.Index(body, "ETH"); i1 > i2 {
		t.Errorf("HYPE (conf .72) should rank above ETH (conf .30)")
	}
}

// TestIdeasEnterJumpsToAsset verifies enter on a board row re-anchors the
// markets selection onto the candidate's asset.
func TestIdeasEnterJumpsToAsset(t *testing.T) {
	m, _ := newTestModel(t)
	m.upsertCandidate(verdict("ETH", metrics.ActionOpenLong, 0.80, "squeeze"))
	m.ideasSel = 0
	m.jumpToCandidate()
	if got := m.selectedCoin(); got != "ETH" {
		t.Fatalf("selection = %q, want ETH", got)
	}
}
