package cockpit

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperagent/tui/internal/apiclient"
)

// fixNow pins the timeNow seam for a test and restores it on cleanup.
func fixNow(t *testing.T, at time.Time) {
	t.Helper()
	timeNow = func() time.Time { return at }
	t.Cleanup(func() { timeNow = time.Now })
}

func TestDeriveCardState(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		thesis     apiclient.Thesis
		ok         bool
		reviewTF   string
		wantState  cardState
		wantReason string
	}{
		{
			name:       "never reviewed",
			ok:         false,
			reviewTF:   "4h",
			wantState:  cardNone,
			wantReason: "never reviewed",
		},
		{
			name:      "live long within window",
			thesis:    apiclient.Thesis{Coin: "BTC", Direction: "long", ReviewedAt: now.Add(-3 * time.Hour)},
			ok:        true,
			reviewTF:  "4h",
			wantState: cardLive,
		},
		{
			name:      "neutral is a real thesis",
			thesis:    apiclient.Thesis{Coin: "BTC", Direction: "neutral", ReviewedAt: now.Add(-time.Hour)},
			ok:        true,
			reviewTF:  "4h",
			wantState: cardLive,
		},
		{
			name:      "stale past twice the review timeframe",
			thesis:    apiclient.Thesis{Coin: "BTC", Direction: "short", ReviewedAt: now.Add(-9 * time.Hour)},
			ok:        true,
			reviewTF:  "4h",
			wantState: cardStale,
		},
		{
			name:      "exactly twice the review timeframe is still live",
			thesis:    apiclient.Thesis{Coin: "BTC", Direction: "short", ReviewedAt: now.Add(-8 * time.Hour)},
			ok:        true,
			reviewTF:  "4h",
			wantState: cardLive,
		},
		{
			name:       "invalidated direction reads as no thesis with timestamp",
			thesis:     apiclient.Thesis{Coin: "BTC", Direction: "", ReviewedAt: now.Add(-time.Hour)},
			ok:         true,
			reviewTF:   "4h",
			wantState:  cardNone,
			wantReason: "invalidated at " + now.Add(-time.Hour).Local().Format("15:04:05"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, reason := deriveCardState(tc.thesis, tc.ok, tc.reviewTF, now)
			if state != tc.wantState {
				t.Errorf("state = %d, want %d", state, tc.wantState)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestTfDur(t *testing.T) {
	cases := []struct {
		tf   string
		want time.Duration
	}{
		{"1m", time.Minute},
		{"15m", 15 * time.Minute},
		{"1h", time.Hour},
		{"4h", 4 * time.Hour},
		{"1d", 24 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"", time.Hour},
		{"bogus", time.Hour},
	}
	for _, c := range cases {
		if got := tfDur(c.tf); got != c.want {
			t.Errorf("tfDur(%q) = %s, want %s", c.tf, got, c.want)
		}
	}
}

func TestParseTier(t *testing.T) {
	cases := []struct {
		detail    string
		wantTier  string
		wantCoin  string
		wantTF    string
		wantExtra string
		wantOK    bool
	}{
		{"IDLE", "IDLE", "", "", "", true},
		{"REVIEW BTC 4h", "REVIEW", "BTC", "4h", "", true},
		{"TRIGGER ETH 5m", "TRIGGER", "ETH", "5m", "", true},
		{"TRIGGER ETH 5m z=3.4", "TRIGGER", "ETH", "5m", "z=3.4", true},
		{"mode → autonomous", "", "", "", "", false},
		{"REVIEW BTC", "", "", "", "", false},
		{"", "", "", "", "", false},
	}
	for _, c := range cases {
		tier, coin, tf, extra, ok := parseTier(c.detail)
		if tier != c.wantTier || coin != c.wantCoin || tf != c.wantTF || extra != c.wantExtra || ok != c.wantOK {
			t.Errorf("parseTier(%q) = (%q, %q, %q, %q, %v), want (%q, %q, %q, %q, %v)",
				c.detail, tier, coin, tf, extra, ok, c.wantTier, c.wantCoin, c.wantTF, c.wantExtra, c.wantOK)
		}
	}
}

// TestThesisCardRendersLiveState covers the live card: direction,
// confidence, invalidation, targets, horizon, review age, reasoning
// sentence, and the live chip.
func TestThesisCardRendersLiveState(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	fixNow(t, now)

	m := testModel()
	m.cache.PutThesis(apiclient.Thesis{
		Coin: "ETH", Direction: "long", Summary: "funding reset while spot bid holds",
		Invalidation: 3250, Targets: []float64{3600, 3800}, Horizon: "days",
		Confidence: 0.72, ReviewedAt: now.Add(-30 * time.Minute), Version: 3,
	})

	out := ansi.Strip(m.thesesView(60, 12))
	for _, want := range []string{"THESES", "ETH", "LONG", "72%", "days", "reviewed 30m ago",
		"● live", "inval 3,250.0", "targets 3,600.0 3,800.0", "funding reset while spot bid holds"} {
		if !strings.Contains(out, want) {
			t.Errorf("live card missing %q:\n%s", want, out)
		}
	}
}

// TestThesisCardRendersStaleState pins ReviewedAt past twice the coin's
// review timeframe (ETH: 1h in testModel) and expects the stale chip.
func TestThesisCardRendersStaleState(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	fixNow(t, now)

	m := testModel()
	m.cache.PutThesis(apiclient.Thesis{
		Coin: "ETH", Direction: "short", Confidence: 0.6,
		ReviewedAt: now.Add(-3 * time.Hour),
	})

	out := ansi.Strip(m.thesesView(60, 12))
	if !strings.Contains(out, "● stale") {
		t.Errorf("stale card missing stale chip:\n%s", out)
	}
	if !strings.Contains(out, "SHORT") {
		t.Errorf("stale card missing direction:\n%s", out)
	}
}

// TestThesisCardRendersNoThesisStates covers both no-thesis reasons: a coin
// never reviewed, and a coin whose thesis was invalidated.
func TestThesisCardRendersNoThesisStates(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	fixNow(t, now)

	m := testModel() // visualized ETH+BTC, no theses cached
	out := ansi.Strip(m.thesesView(60, 12))
	if !strings.Contains(out, "● no thesis") || !strings.Contains(out, "never reviewed") {
		t.Errorf("never-reviewed card missing:\n%s", out)
	}

	m.cache.PutThesis(apiclient.Thesis{Coin: "ETH", Direction: "", ReviewedAt: now.Add(-2 * time.Hour)})
	out = ansi.Strip(m.thesesView(60, 12))
	want := "invalidated at " + now.Add(-2*time.Hour).Local().Format("15:04:05")
	if !strings.Contains(out, want) {
		t.Errorf("invalidated card missing %q:\n%s", want, out)
	}
}

// TestTriggerStatusFlashesOwningCard drives a TRIGGER tier status through
// Update and asserts the flash lands on the owning card (and only there),
// the header phase shows the tier, and nothing is journaled.
func TestTriggerStatusFlashesOwningCard(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	fixNow(t, now)

	m := testModel()
	m.Update(statusMsg{Kind: statusNotice, Detail: "TRIGGER ETH 5m z=3.4"})

	if m.phase != "TRIGGER ETH 5m z=3.4" {
		t.Errorf("phase = %q, want the tier string", m.phase)
	}
	if len(m.journal) != 0 {
		t.Errorf("tier status must not journal, got %+v", m.journal)
	}

	out := ansi.Strip(m.thesesView(60, 12))
	if !strings.Contains(out, "⚡ 5m z=3.4 — entry check…") {
		t.Errorf("owning card missing trigger flash:\n%s", out)
	}
	if got := strings.Count(out, "⚡"); got != 1 {
		t.Errorf("flash rendered on %d cards, want exactly 1", got)
	}
}

// TestTriggerFlashExpires advances the pinned clock past flashDuration and
// expects the flash to vanish without any explicit clearing.
func TestTriggerFlashExpires(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	fixNow(t, now)

	m := testModel()
	m.Update(statusMsg{Kind: statusNotice, Detail: "TRIGGER ETH 5m"})
	if out := ansi.Strip(m.thesesView(60, 12)); !strings.Contains(out, "⚡") {
		t.Fatalf("flash not rendered while fresh:\n%s", out)
	}

	fixNow(t, now.Add(flashDuration+time.Second))
	if out := ansi.Strip(m.thesesView(60, 12)); strings.Contains(out, "⚡") {
		t.Errorf("flash still rendered after expiry:\n%s", out)
	}
}

// TestReviewAndIdleStatusUpdatePhaseOnly asserts REVIEW/IDLE tiers move the
// header phase strip without flashing cards or journaling.
func TestReviewAndIdleStatusUpdatePhaseOnly(t *testing.T) {
	m := testModel()

	m.Update(statusMsg{Kind: statusNotice, Detail: "REVIEW BTC 4h"})
	if m.phase != "REVIEW BTC 4h" {
		t.Errorf("phase = %q, want REVIEW BTC 4h", m.phase)
	}
	m.Update(statusMsg{Kind: statusNotice, Detail: "IDLE"})
	if m.phase != "IDLE" {
		t.Errorf("phase = %q, want IDLE", m.phase)
	}
	if len(m.journal) != 0 {
		t.Errorf("tier statuses must not journal, got %+v", m.journal)
	}
	if len(m.flashes) != 0 {
		t.Errorf("non-trigger tiers must not flash, got %+v", m.flashes)
	}
}

// TestClearWipesDisplayOnly is /clear's contract: chat scrollback, journal
// ring, trigger flashes, and card reasoning text all reset — thesis facts
// (direction, levels) stay, and a thesis reviewed after the clear shows its
// summary again.
func TestClearWipesDisplayOnly(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	fixNow(t, now)

	m := testModel()
	m.cache.PutThesis(apiclient.Thesis{
		Coin: "ETH", Direction: "long", Summary: "old reasoning sentence",
		Invalidation: 3250, Confidence: 0.7, ReviewedAt: now.Add(-10 * time.Minute),
	})
	m.Update(journalMsg{Coin: "ETH", Kind: "fill", Summary: "0.85 ETH filled"})
	m.Update(statusMsg{Kind: statusNotice, Detail: "TRIGGER ETH 5m"})
	m.turns = []apiclient.ChatTurn{{Role: "user", Text: "hi"}}

	out, cmd := m.runCommand("/clear")
	if cmd != nil {
		t.Error("/clear must be local (nil cmd) — no daemon calls")
	}
	if out == "" {
		t.Error("/clear should confirm")
	}
	if len(m.turns) != 0 || len(m.journal) != 0 || len(m.flashes) != 0 {
		t.Errorf("display state not wiped: turns=%d journal=%d flashes=%d",
			len(m.turns), len(m.journal), len(m.flashes))
	}

	view := ansi.Strip(m.thesesView(60, 12))
	if strings.Contains(view, "old reasoning sentence") {
		t.Errorf("reasoning text survived /clear:\n%s", view)
	}
	for _, keep := range []string{"ETH", "LONG", "inval 3,250.0"} {
		if !strings.Contains(view, keep) {
			t.Errorf("thesis fact %q lost after /clear:\n%s", keep, view)
		}
	}

	// A fresh review after the clear shows its reasoning again.
	m.cache.PutThesis(apiclient.Thesis{
		Coin: "ETH", Direction: "long", Summary: "new reasoning sentence",
		Invalidation: 3250, Confidence: 0.7, ReviewedAt: now.Add(time.Minute),
	})
	if view := ansi.Strip(m.thesesView(60, 12)); !strings.Contains(view, "new reasoning sentence") {
		t.Errorf("post-clear review's reasoning hidden:\n%s", view)
	}
}

// TestCardCoinsTrackedPlusThesisCoins asserts card ordering: the tracked set
// (falling back to visualized) first, then thesis-only coins from the cache.
func TestCardCoinsTrackedPlusThesisCoins(t *testing.T) {
	m := testModel() // tracked empty -> falls back to visualized ETH, BTC
	m.cache.PutThesis(apiclient.Thesis{Coin: "SOL", Direction: "long"})

	got := m.cardCoins()
	want := []string{"ETH", "BTC", "SOL"}
	if len(got) != len(want) {
		t.Fatalf("cardCoins() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cardCoins()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	m.tracked = []string{"BTC"}
	if got := m.cardCoins(); got[0] != "BTC" {
		t.Errorf("tracked set should lead ordering, got %v", got)
	}
}
