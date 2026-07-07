package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

const (
	minW = 96
	minH = 28

	leftColW  = 42
	topRowH   = 12
	chromeH   = 3 // header + footer + slack
)

var (
	cAccent = lipgloss.Color("#2DE0A7")
	cText   = lipgloss.Color("#C9D4DE")
	cBright = lipgloss.Color("#EDF3F9")
	cDim    = lipgloss.Color("#5C6B7A")
	cBorder = lipgloss.Color("#28323D")
	cGreen  = lipgloss.Color("#4ADE80")
	cRed    = lipgloss.Color("#FF6B6B")
	cAmber  = lipgloss.Color("#F0B35B")
	cPurple = lipgloss.Color("#B48EF7")
	cCyan   = lipgloss.Color("#4FC1E9")

	logoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#06130D")).Background(cAccent).Bold(true)
	textStyle   = lipgloss.NewStyle().Foreground(cText)
	brightStyle = lipgloss.NewStyle().Foreground(cBright).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(cDim)
	borderStyle = lipgloss.NewStyle().Foreground(cBorder)
	titleStyle  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	phaseStyle  = lipgloss.NewStyle().Foreground(cAccent)
	greenStyle  = lipgloss.NewStyle().Foreground(cGreen)
	redStyle    = lipgloss.NewStyle().Foreground(cRed)
	amberStyle  = lipgloss.NewStyle().Foreground(cAmber)
	quoteStyle  = lipgloss.NewStyle().Foreground(cBright).Italic(true)
	keyStyle    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)

	tagStyles = map[string]lipgloss.Style{
		"INGEST":   lipgloss.NewStyle().Foreground(cCyan).Bold(true),
		"REASON":   lipgloss.NewStyle().Foreground(cPurple).Bold(true),
		"EXECUTE":  lipgloss.NewStyle().Foreground(cAccent).Bold(true),
		"FILL":     lipgloss.NewStyle().Foreground(cGreen).Bold(true),
		"RISK":     lipgloss.NewStyle().Foreground(cAmber).Bold(true),
		"OPERATOR": lipgloss.NewStyle().Foreground(cRed).Bold(true),
	}
)

// ── layout ──────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minW || m.height < minH {
		msg := dimStyle.Render(fmt.Sprintf("hypertrader needs at least %d×%d — current %d×%d", minW, minH, m.width, m.height))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	bodyH := m.height - chromeH
	botRowH := bodyH - topRowH
	rightColW := m.width - leftColW

	top := lipgloss.JoinHorizontal(lipgloss.Top,
		m.mandateView(leftColW, topRowH),
		m.marketsView(rightColW, topRowH),
	)
	bot := lipgloss.JoinHorizontal(lipgloss.Top,
		m.positionsView(leftColW, botRowH),
		m.journalView(rightColW, botRowH),
	)

	return m.headerView() + "\n" + top + "\n" + bot + "\n" + m.footerView()
}

func (m model) headerView() string {
	left := logoStyle.Render(" HYPERTRADER ") +
		dimStyle.Render("  autonomous trading operator · Hyperliquid mainnet")

	up := time.Since(m.startedAt)
	uptime := dimStyle.Render(fmt.Sprintf("up %dh %02dm  ", int(up.Hours()), int(up.Minutes())%60))

	var loop, status string
	if m.running {
		loop = m.spin.View() + " " + phaseStyle.Render("LOOP · "+m.phase)
		status = greenStyle.Bold(true).Render("● RUNNING")
	} else {
		loop = amberStyle.Render("◼ LOOP PAUSED")
		status = amberStyle.Bold(true).Render("● HALTED (operator)")
	}

	return spread(left, uptime+loop+"   "+status+" ", m.width)
}

func (m model) footerView() string {
	keys := " " + keyStyle.Render("h") + dimStyle.Render(" halt / resume   ") +
		keyStyle.Render("q") + dimStyle.Render(" quit")
	note := dimStyle.Italic(true).Render("mock demo · simulated data — every decision in writing, halt at any time ")
	return spread(keys, note, m.width)
}

// ── panels ──────────────────────────────────────────────────────────────

func (m model) mandateView(w, h int) string {
	cw := w - 4
	var lines []string

	quote := quoteStyle.Width(cw).Render("“" + m.man.quote + "”")
	lines = append(lines, strings.Split(quote, "\n")...)
	lines = append(lines, "")

	alloc := dimStyle.Render(padR("ALLOCATION", 12)) +
		brightStyle.Render(fmt.Sprintf("ETH %.1f%%", m.man.allocPct)) +
		dimStyle.Render(fmt.Sprintf("  →  %.0f%% target", m.man.allocTarget))
	lines = append(lines, alloc)

	pb := m.prog
	pb.Width = cw
	lines = append(lines, pb.ViewAs(m.man.allocPct/m.man.allocTarget))
	lines = append(lines, "")

	lines = append(lines, envelopeLine("DRAWDOWN", fmt.Sprintf("%.1f%%", m.man.drawdownPct),
		fmt.Sprintf("/ %.1f%% max", m.man.drawdownMax), cw))
	lines = append(lines, envelopeLine("LEVERAGE", fmt.Sprintf("%.1f×", m.man.leverage),
		fmt.Sprintf("/ %.1f× cap", m.man.leverageCap), cw))

	horizon := dimStyle.Render(padR("HORIZON", 12)) +
		textStyle.Render(fmt.Sprintf("day %d of %d", m.man.day, m.man.horizonDays))
	remaining := dimStyle.Render(fmt.Sprintf("%dd left", m.man.horizonDays-m.man.day))
	lines = append(lines, spread(horizon, remaining, cw))

	return box("MANDATE", "goal · horizon · risk", lines, w, h)
}

func envelopeLine(label, val, cap string, cw int) string {
	left := dimStyle.Render(padR(label, 12)) + brightStyle.Render(val) + dimStyle.Render(" "+cap)
	return spread(left, greenStyle.Render("● ok"), cw)
}

func (m model) marketsView(w, h int) string {
	cw := w - 4
	var lines []string

	head := dimStyle.Render(padR("MKT", 5) + "  " + padL("LAST", 10) + "  " + padL("24H", 7) +
		"  " + padL("FUND/8H", 9) + "  " + padL("OIΔ 1H", 7) + "  " + padL("CVD 1H", 8))
	lines = append(lines, head)

	for _, mk := range m.markets {
		row := brightStyle.Render(padR(mk.sym, 5)) + "  " +
			textStyle.Render(padL(fnum(mk.last, priceDec(mk.last)), 10)) + "  " +
			signed(fmt.Sprintf("%+.2f%%", mk.chg24h), mk.chg24h, 7) + "  " +
			signed(fmt.Sprintf("%+.4f%%", mk.funding), mk.funding, 9) + "  " +
			signed(fmt.Sprintf("%+.1f%%", mk.oiDelta), mk.oiDelta, 7) + "  " +
			signed(fmt.Sprintf("%+.1fM", mk.cvd), mk.cvd, 8)
		lines = append(lines, row)
	}

	lines = append(lines, "")
	lines = append(lines, dimStyle.Italic(true).Render(
		truncate.StringWithTail("multi-timeframe bars · CVD, basis, funding trajectory, OI delta, liquidation proximity", uint(cw), "…")))

	return box("MARKET PICTURE", "live ingest", lines, w, h)
}

func (m model) positionsView(w, h int) string {
	cw := w - 4
	var lines []string

	eth := m.markets[0]
	pos := m.positions[0]
	upnl := (eth.last - pos.entry) * pos.size

	lines = append(lines, dimStyle.Render("OPEN POSITIONS"))
	lines = append(lines,
		brightStyle.Render(padR(pos.sym, 10))+
			greenStyle.Bold(true).Render(pos.side)+
			textStyle.Render(fmt.Sprintf(" %.2f @ %s", pos.size, fnum(pos.entry, 1))))
	upnlStr := fmt.Sprintf("%+.2f", upnl)
	upnlStr = upnlStr[:1] + "$" + upnlStr[1:]
	lines = append(lines, spread(
		dimStyle.Render("  uPnL ")+signed(upnlStr, upnl, 0),
		dimStyle.Render(fmt.Sprintf("mark %s · %.1f×", fnum(eth.last, 1), pos.lev)), cw))
	lines = append(lines, "")

	gateState := titleStyle.Render(fmt.Sprintf("%d/%d PASS", len(gates), len(gates)))
	if !m.running {
		gateState = amberStyle.Render("PAUSED")
	}
	lines = append(lines, spread(dimStyle.Render("RISK GATES — compiled"), gateState, cw))

	for _, g := range gates {
		line := greenStyle.Render("✓ ") + textStyle.Render(g.name)
		if g.extra != "" {
			line += dimStyle.Render(" · " + g.extra)
		}
		lines = append(lines, line)
	}

	return box("EXECUTION", "compiled gates", lines, w, h)
}

func (m model) journalView(w, h int) string {
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
		body := textStyle.Render(truncate.StringWithTail(e.text, uint(bodyW), "…"))
		if e.tag == "OPERATOR" {
			body = amberStyle.Render(truncate.StringWithTail(e.text, uint(bodyW), "…"))
		}
		lines = append(lines,
			dimStyle.Render(e.at.Format("15:04:05"))+" "+tag.Render(padR(e.tag, 8))+" "+body)
	}

	right := fmt.Sprintf("append-only · #%d", 4818+len(m.journal))
	return box("DECISION JOURNAL", right, lines, w, h)
}

// ── box: rounded border with an embedded title ──────────────────────────

func box(title, rightTitle string, lines []string, w, h int) string {
	iw := w - 2 // width between the corner glyphs
	cw := iw - 2
	ch := h - 2

	t := " " + title + " "
	r := ""
	if rightTitle != "" {
		r = " " + rightTitle + " "
	}
	fill := iw - 1 - lipgloss.Width(t) - lipgloss.Width(r) - 1
	if fill < 0 {
		fill = 0
	}
	var b strings.Builder
	b.WriteString(borderStyle.Render("╭─") + titleStyle.Render(t) +
		borderStyle.Render(strings.Repeat("─", fill)) + dimStyle.Render(r) + borderStyle.Render("─╮"))

	for i := 0; i < ch; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		pad := cw - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("\n" + borderStyle.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + borderStyle.Render("│"))
	}

	b.WriteString("\n" + borderStyle.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return b.String()
}

// ── formatting helpers ──────────────────────────────────────────────────

// spread left-aligns l and right-aligns r within width w.
func spread(l, r string, w int) string {
	gap := w - lipgloss.Width(l) - lipgloss.Width(r)
	if gap < 1 {
		gap = 1
	}
	return l + strings.Repeat(" ", gap) + r
}

func padR(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padL(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// signed pads a numeric string to w then colors it green/red by sign.
func signed(s string, v float64, w int) string {
	if w > 0 {
		s = padL(s, w)
	}
	if v >= 0 {
		return greenStyle.Render(s)
	}
	return redStyle.Render(s)
}

// fnum formats with thousands separators.
func fnum(v float64, dec int) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	ip, fp := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		ip, fp = s[:i], s[i:]
	}
	var b strings.Builder
	for j, c := range ip {
		if j > 0 && (len(ip)-j)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + fp
}

func priceDec(v float64) int {
	switch {
	case v < 1:
		return 4
	case v < 100:
		return 2
	case v < 10000:
		return 1
	default:
		return 0
	}
}
