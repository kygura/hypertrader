package cockpit

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	minW = 96
	minH = 28

	leftColW = 42
	topRowH  = 12
	chromeH  = 3 // header + chat bar + footer
)

// View implements tea.Model. Content is built as a plain string (mirroring
// the panel layout) and wrapped in a tea.View at the end — this bubbletea v2
// release requires View() tea.View, not string; see internal/tui/view.go for
// the same pattern.
func (m *Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}
	if m.width < minW || m.height < minH {
		msg := dimStyle.Render(fmt.Sprintf("hypertrader needs at least %d×%d — current %d×%d", minW, minH, m.width, m.height))
		v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg))
		v.AltScreen = true
		return v
	}

	bodyH := m.height - chromeH
	botRowH := bodyH - topRowH
	rightColW := m.width - leftColW

	top := lipgloss.JoinHorizontal(lipgloss.Top,
		m.mandateView(leftColW, topRowH),
		m.marketsView(rightColW, topRowH),
	)
	rightBot := m.journalView(rightColW, botRowH)
	if m.chatOpen {
		rightBot = m.chatView(rightColW, botRowH)
	}
	bot := lipgloss.JoinHorizontal(lipgloss.Top,
		m.positionsView(leftColW, botRowH),
		rightBot,
	)

	frame := m.headerView() + "\n" + top + "\n" + bot + "\n" + m.chatBarView() + "\n" + m.footerView()
	v := tea.NewView(frame)
	v.AltScreen = true
	return v
}

func (m *Model) headerView() string {
	left := logoStyle.Render(" HYPERTRADER ") +
		dimStyle.Render("  autonomous trading operator · Hyperliquid")

	up := time.Since(m.startedAt)
	uptime := dimStyle.Render(fmt.Sprintf("up %dh %02dm  ", int(up.Hours()), int(up.Minutes())%60))

	loop := m.spin.View() + " " + phaseStyle.Render("LOOP · "+m.phase)

	status := amberStyle.Bold(true).Render("● DISCONNECTED")
	if m.connected {
		status = greenStyle.Bold(true).Render("● CONNECTED")
	}

	modeChip := amberStyle.Bold(true).Render("PROPOSE")
	if m.mode == "autonomous" {
		modeChip = redStyle.Bold(true).Render("AUTONOMOUS")
	}

	return spread(left, uptime+loop+"   "+modeChip+" "+status+" ", m.width)
}

func (m *Model) footerView() string {
	keys := " " + keyStyle.Render("/") + dimStyle.Render(" chat   ") +
		keyStyle.Render("m") + dimStyle.Render(" mode   ") +
		keyStyle.Render("q") + dimStyle.Render(" quit")
	note := dimStyle.Italic(true).Render("every decision in writing · connected to live daemon ")
	return spread(keys, note, m.width)
}

func (m *Model) chatBarView() string {
	if m.input.Focused() {
		return " " + m.input.View()
	}
	if m.busy {
		return " " + m.spin.View() + dimStyle.Render(" agent is thinking…")
	}
	return " " + dimStyle.Render("› press / to talk to the agent")
}

func (m *Model) mandateView(w, h int) string {
	cw := w - 4
	env := m.envelope()
	gates := m.gates()
	var lines []string

	lines = append(lines, dimStyle.Render(padR("MODE", 12))+brightStyle.Render(strings.ToUpper(m.mode)))
	lines = append(lines, "")

	lines = append(lines, dimStyle.Render(padR("EXPOSURE", 12))+
		brightStyle.Render("$"+fnum(env.ExposureUSD, 0))+
		dimStyle.Render("  / $"+fnum(env.Risk.MaxTotalExposureUSD, 0)+" cap"))
	ratio := 0.0
	if env.Risk.MaxTotalExposureUSD > 0 {
		ratio = env.ExposureUSD / env.Risk.MaxTotalExposureUSD
	}
	lines = append(lines, bar(ratio, cw))
	lines = append(lines, "")

	lines = append(lines, envelopeLine("POSITIONS", fmt.Sprintf("%d", env.OpenCount),
		fmt.Sprintf("/ %d max", env.Risk.MaxConcurrent), gates.ConcurrencyOK, cw))
	lines = append(lines, envelopeLine("MAX POS", "$"+fnum(env.Risk.MaxPositionUSD, 0), "per position", gates.MaxPosOK, cw))
	lines = append(lines, envelopeLine("uPnL", fmt.Sprintf("%+.2f", env.UPnL), "unrealized", env.UPnL >= 0, cw))
	lines = append(lines, envelopeLine("KILL-SWITCH", "$"+fnum(env.Risk.DailyLossKillUSD, 0), "daily loss · armed", true, cw))

	return box("MANDATE", "risk envelope", lines, w, h)
}

func envelopeLine(label, val, extra string, ok bool, cw int) string {
	left := dimStyle.Render(padR(label, 12)) + brightStyle.Render(val) + dimStyle.Render(" "+extra)
	state := greenStyle.Render("● ok")
	if !ok {
		state = redStyle.Render("● breach")
	}
	return spread(left, state, cw)
}

func (m *Model) marketsView(w, h int) string {
	var lines []string
	// Funding is the daemon's per-hour rate (backend/internal/metrics.AssetCtx.Funding
	// doc: "current funding rate (per hour, as a fraction)") — labelled FUND/1H,
	// not the 8h convention some other venues use. OIDelta is also a fraction
	// of the previous bar (backend/internal/metrics.Bar.OIDelta doc), so it
	// needs the same *100 treatment as Funding to read as a percent; the
	// reasoner's own logging (backend/internal/reasoner/context.go) does the
	// same bar.OIDelta*100. CVD is a raw cumulative-volume-delta in base-asset
	// units whose magnitude varies by orders of magnitude across coins (single
	// digits for BTC, millions for DOGE) — a fixed /1e6 divisor read as ~0.0M
	// for most coins, so it's abbreviated adaptively instead (see cvdStr).
	lines = append(lines, dimStyle.Render(padR("MKT", 5)+"  "+padL("LAST", 10)+"  "+
		padL("FUND/1H", 9)+"  "+padL("OIΔ", 7)+"  "+padL("CVD", 8)))

	for _, coin := range m.visualized {
		mid := m.cache.Mid(coin)
		var funding, oiDelta, cvd float64
		if ctx, ok := m.cache.AssetCtx(coin); ok {
			funding = ctx.Funding * 100
		}
		if b, ok := m.cache.LatestBar(coin, m.tf(coin)); ok {
			oiDelta = b.OIDelta * 100
			cvd = b.CVD
		}
		row := brightStyle.Render(padR(coin, 5)) + "  " +
			textStyle.Render(padL(fnum(mid, priceDec(mid)), 10)) + "  " +
			signed(fmt.Sprintf("%+.4f%%", funding), funding, 9) + "  " +
			signed(fmt.Sprintf("%+.2f%%", oiDelta), oiDelta, 7) + "  " +
			signed(cvdStr(cvd), cvd, 8)
		lines = append(lines, row)
	}

	return box("MARKET PICTURE", "live ingest", lines, w, h)
}

func (m *Model) positionsView(w, h int) string {
	cw := w - 4
	env := m.envelope()
	var lines []string

	lines = append(lines, dimStyle.Render("OPEN POSITIONS"))
	open := 0
	for _, coin := range m.visualized {
		p := m.cache.Position(coin)
		if p.IsFlat() {
			continue
		}
		open++
		side, sideStyle := "LONG", greenStyle
		if p.IsShort() {
			side, sideStyle = "SHORT", redStyle
		}
		size := p.Size
		if size < 0 {
			size = -size
		}
		lines = append(lines,
			brightStyle.Render(padR(p.Coin, 10))+
				sideStyle.Bold(true).Render(side)+
				textStyle.Render(fmt.Sprintf(" %.2f @ %s", size, fnum(p.EntryPrice, priceDec(p.EntryPrice)))))
		lines = append(lines, spread(
			dimStyle.Render("  uPnL ")+signed(fmt.Sprintf("%+.2f", p.UnrealPnl), p.UnrealPnl, 0),
			dimStyle.Render("mark "+fnum(p.MarkPrice, priceDec(p.MarkPrice))), cw))
	}
	if open == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("flat — no open positions"))
	}
	lines = append(lines, "")

	// Compiled risk gates — same gateStates the MANDATE panel renders, so
	// the two panels can never disagree on pass/fail.
	gateStates := m.gates()
	pass := 0
	type gateRow struct {
		name string
		ok   bool
	}
	gateRows := []gateRow{
		{fmt.Sprintf("max position $%s", fnum(env.Risk.MaxPositionUSD, 0)), gateStates.MaxPosOK},
		{fmt.Sprintf("max exposure $%s", fnum(env.Risk.MaxTotalExposureUSD, 0)), gateStates.ExposureOK},
		{fmt.Sprintf("max concurrency %d/%d", env.OpenCount, env.Risk.MaxConcurrent), gateStates.ConcurrencyOK},
		{fmt.Sprintf("daily-loss kill-switch $%s · armed", fnum(env.Risk.DailyLossKillUSD, 0)), true},
	}
	for _, g := range gateRows {
		if g.ok {
			pass++
		}
	}
	lines = append(lines, spread(dimStyle.Render("RISK GATES — compiled"),
		titleStyle.Render(fmt.Sprintf("%d/%d PASS", pass, len(gateRows))), cw))
	for _, g := range gateRows {
		mark := greenStyle.Render("✓ ")
		if !g.ok {
			mark = redStyle.Render("✗ ")
		}
		lines = append(lines, mark+textStyle.Render(g.name))
	}

	return box("EXECUTION", "compiled gates", lines, w, h)
}

func (m *Model) journalView(w, h int) string {
	cw := w - 4
	ch := h - 2
	bodyW := cw - 18 // timestamp (8) + gap + tag (8) + gap

	start := len(m.journal) - ch
	if start < 0 {
		start = 0
	}

	var lines []string
	for _, e := range m.journal[start:] {
		tag, ok := tagStyles[e.tag]
		if !ok {
			tag = dimStyle
		}
		body := textStyle.Render(truncTail(e.text, bodyW))
		if e.tag == "OPERATOR" {
			body = amberStyle.Render(truncTail(e.text, bodyW))
		}
		lines = append(lines,
			dimStyle.Render(e.at.Format("15:04:05"))+" "+tag.Render(padR(e.tag, 8))+" "+body)
	}

	right := fmt.Sprintf("append-only · %d", len(m.journal))
	return box("DECISION JOURNAL", right, lines, w, h)
}

// chatView renders the agent conversation in place of the journal panel.
func (m *Model) chatView(w, h int) string {
	cw := w - 4
	ch := h - 2

	var lines []string
	for _, t := range m.turns {
		switch t.Role {
		case "user":
			lines = append(lines, keyStyle.Render("you  ")+textStyle.Render(truncTail(t.Text, cw-5)))
		case "system":
			for _, l := range strings.Split(t.Text, "\n") {
				lines = append(lines, dimStyle.Render("  "+truncTail(l, cw-2)))
			}
		default: // assistant
			lines = append(lines, greenStyle.Bold(true).Render("agent"))
			for _, l := range strings.Split(wordWrap(t.Text, cw-2), "\n") {
				lines = append(lines, textStyle.Render("  "+l))
			}
		}
	}
	if m.busy {
		lines = append(lines, m.spin.View()+dimStyle.Render(" thinking…"))
	}
	if len(lines) > ch {
		lines = lines[len(lines)-ch:]
	}

	return box("AGENT", "esc to close", lines, w, h)
}

// wordWrap wraps s to at most width characters per line, breaking on spaces.
func wordWrap(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}
	var out strings.Builder
	for len(s) > width {
		cut := strings.LastIndex(s[:width], " ")
		if cut <= 0 {
			cut = width
		}
		out.WriteString(s[:cut])
		out.WriteByte('\n')
		s = strings.TrimLeft(s[cut:], " ")
	}
	out.WriteString(s)
	return out.String()
}
