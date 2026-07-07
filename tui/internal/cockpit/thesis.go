// Thesis cards: the reasoning/verdict area rendered as one card per tracked
// asset, replaced in place as thesis events arrive — state, where the
// DECISION JOURNAL below stays chronological events. Design:
// docs/superpowers/specs/2026-07-07-patient-agent-design.md ("TUI (cockpit)").
package cockpit

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// flashDuration is how long a trigger flash stays on its owning card. Long
// enough to be seen between repaints, short enough that a stale "entry
// check…" does not outlive the verdict it announced.
const flashDuration = 15 * time.Second

// cardFlash is a transient trigger notice shown on the owning asset's card
// (e.g. "⚡ 5m z=3.4 — entry check…").
type cardFlash struct {
	text  string
	until time.Time
}

// cardState classifies a thesis card for rendering.
type cardState int

const (
	cardLive cardState = iota
	cardStale
	cardNone
)

// deriveCardState classifies coin's thesis for rendering: no thesis at all
// (never reviewed), an invalidated thesis (the reasoner cleared its
// direction), stale (ReviewedAt older than 2× the review timeframe), or
// live. reason is non-empty only for cardNone.
func deriveCardState(t apiclient.Thesis, ok bool, reviewTF string, now time.Time) (cardState, string) {
	if !ok {
		return cardNone, "never reviewed"
	}
	switch t.Direction {
	case "long", "short", "neutral":
	default: // "" or "invalidated" — the thesis no longer authorizes anything
		at := t.ReviewedAt
		if at.IsZero() {
			at = t.CreatedAt
		}
		return cardNone, "invalidated at " + at.Local().Format("15:04:05")
	}
	if now.Sub(t.ReviewedAt) > 2*tfDur(reviewTF) {
		return cardStale, ""
	}
	return cardLive, ""
}

// tfDur parses a timeframe token ("1m", "5m", "15m", "1h", "4h", "1d", "1w")
// into its duration. Unknown tokens read as 1h — the same default the rest
// of the cockpit assumes for a coin without a configured timeframe.
func tfDur(tf string) time.Duration {
	if len(tf) < 2 {
		return time.Hour
	}
	n, err := strconv.Atoi(tf[:len(tf)-1])
	if err != nil || n <= 0 {
		return time.Hour
	}
	switch tf[len(tf)-1] {
	case 'm':
		return time.Duration(n) * time.Minute
	case 'h':
		return time.Duration(n) * time.Hour
	case 'd':
		return time.Duration(n) * 24 * time.Hour
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour
	}
	return time.Hour
}

// ageStr compactly renders how long ago a review happened.
func ageStr(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// parseTier recognizes the reasoning-tier strings the daemon's status events
// carry: "IDLE", "REVIEW <coin> <tf>", "TRIGGER <coin> <tf> [detail…]".
// Anything else (mode notices, error details) is not a tier and returns
// ok=false so it keeps flowing down the notice path.
func parseTier(detail string) (tier, coin, tf, extra string, ok bool) {
	fields := strings.Fields(detail)
	if len(fields) == 0 {
		return "", "", "", "", false
	}
	switch fields[0] {
	case "IDLE":
		if len(fields) == 1 {
			return "IDLE", "", "", "", true
		}
	case "REVIEW", "TRIGGER":
		if len(fields) >= 3 {
			return fields[0], fields[1], fields[2], strings.Join(fields[3:], " "), true
		}
	}
	return "", "", "", "", false
}

// flashCard records a transient trigger flash on coin's card, built from the
// tier status's timeframe and any extra deviation detail it carried.
func (m *Model) flashCard(coin, tf, extra string) {
	text := "⚡ " + tf
	if extra != "" {
		text += " " + extra
	}
	text += " — entry check…"
	m.flashes[coin] = cardFlash{text: text, until: timeNow().Add(flashDuration)}
}

// cardCoins is the ordered asset list the THESES panel renders: the tracked
// set from settings (falling back to the visualized watchlist when the
// daemon reports none), plus any extra coins the cache holds theses for.
func (m *Model) cardCoins() []string {
	coins := m.tracked
	if len(coins) == 0 {
		coins = m.visualized
	}
	out := append([]string(nil), coins...)
	for _, t := range m.cache.Theses() { // sorted by coin
		if !containsStr(out, t.Coin) {
			out = append(out, t.Coin)
		}
	}
	return out
}

// thesesView renders the latest-per-asset thesis cards.
func (m *Model) thesesView(w, h int) string {
	cw := w - 4
	var lines []string
	for i, coin := range m.cardCoins() {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.thesisCard(coin, cw)...)
	}
	if len(lines) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("no tracked assets"))
	}
	return box("THESES", "latest per asset", lines, w, h)
}

// thesisCard renders one asset's card: a header line (direction, confidence,
// horizon, review age, state chip), a levels line (invalidation, targets),
// and either an active trigger flash or the latest reasoning sentence(s).
func (m *Model) thesisCard(coin string, cw int) []string {
	now := timeNow()
	t, ok := m.cache.Thesis(coin)
	state, reason := deriveCardState(t, ok, m.tf(coin), now)

	var lines []string
	if state == cardNone {
		chip := dimStyle.Render("● no thesis")
		head := brightStyle.Render(padR(coin, 6)) + dimStyle.Render("—")
		lines = append(lines, spread(truncTail(head, cw-lipgloss.Width(chip)-1), chip, cw))
		lines = append(lines, dimStyle.Italic(true).Render("  "+truncTail(reason, cw-2)))
	} else {
		dirStyle := amberStyle // neutral
		switch t.Direction {
		case "long":
			dirStyle = greenStyle
		case "short":
			dirStyle = redStyle
		}
		chip := greenStyle.Render("● live")
		if state == cardStale {
			chip = amberStyle.Render("● stale")
		}
		meta := fmt.Sprintf("  %.0f%%", t.Confidence*100)
		if t.Horizon != "" {
			meta += " · " + t.Horizon
		}
		meta += " · reviewed " + ageStr(now.Sub(t.ReviewedAt)) + " ago"
		head := brightStyle.Render(padR(coin, 6)) + dirStyle.Bold(true).Render(strings.ToUpper(t.Direction)) + dimStyle.Render(meta)
		lines = append(lines, spread(truncTail(head, cw-lipgloss.Width(chip)-1), chip, cw))

		levels := dimStyle.Render("  inval ") + textStyle.Render(fnum(t.Invalidation, priceDec(t.Invalidation)))
		if len(t.Targets) > 0 {
			targets := make([]string, len(t.Targets))
			for i, tgt := range t.Targets {
				targets[i] = fnum(tgt, priceDec(tgt))
			}
			levels += dimStyle.Render(" · targets ") + textStyle.Render(strings.Join(targets, " "))
		}
		lines = append(lines, truncTail(levels, cw))
	}

	// Trigger flash outranks the reasoning sentence; the sentence itself is
	// suppressed for theses last reviewed before the operator's /clear.
	if f, has := m.flashes[coin]; has && now.Before(f.until) {
		lines = append(lines, amberStyle.Bold(true).Render("  "+truncTail(f.text, cw-2)))
	} else if state != cardNone && t.Summary != "" && t.ReviewedAt.After(m.displayClearedAt) {
		lines = append(lines, textStyle.Render("  "+truncTail(t.Summary, cw-2)))
	}
	return lines
}
