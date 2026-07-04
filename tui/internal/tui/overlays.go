// The modal core: every floating panel (settings, pickers, help, the markets
// manager) is an overlay — a self-contained sub-model that owns the keyboard
// while it is on top. The root Model keeps a stack; pushing opens a panel on
// top of whatever is open, esc pops one level, and key routing is a single
// rule: the top of the stack sees every key first. This replaces the previous
// enum-plus-shared-menu design where each new panel grew three switches.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// overlay is one floating modal panel. handleKey may mutate the model (push
// further overlays, pop itself, drive Controls) and may return a command for
// async work. view renders the panel box, clamped to maxW×maxH.
type overlay interface {
	handleKey(m *Model, msg tea.KeyPressMsg) tea.Cmd
	view(m *Model, maxW, maxH int) string
}

// --- stack operations ------------------------------------------------------

func (m *Model) push(o overlay)   { m.overlays = append(m.overlays, o) }
func (m *Model) hasOverlay() bool { return len(m.overlays) > 0 }

func (m *Model) pop() {
	if n := len(m.overlays); n > 0 {
		m.overlays = m.overlays[:n-1]
	}
}

func (m *Model) top() overlay {
	if n := len(m.overlays); n > 0 {
		return m.overlays[n-1]
	}
	return nil
}

// --- generic selection list ------------------------------------------------

// listItem is one selectable row. key is the value handed to onSelect; label is
// what's shown; hint is a dim right-aligned annotation; on marks the active choice.
type listItem struct {
	key   string
	label string
	hint  string
	on    bool
}

// listOverlay is a reusable picker: a titled, optionally filterable list whose
// selection is handled by a closure. Every picker in the app (provider, model,
// mode, timeframe, coin actions) is an instance of this one type.
type listOverlay struct {
	title      string
	items      []listItem
	cursor     int
	filter     string
	filterable bool
	footnote   string // optional extra hint under the list

	// onSelect handles enter on an item. onMiss (optional) handles enter while a
	// filter matches nothing — the "add this symbol" affordance.
	onSelect func(m *Model, it listItem) tea.Cmd
	onMiss   func(m *Model, typed string) tea.Cmd
}

func (lo *listOverlay) visible() []listItem {
	if !lo.filterable || lo.filter == "" {
		return lo.items
	}
	f := strings.ToUpper(lo.filter)
	out := make([]listItem, 0, len(lo.items))
	for _, it := range lo.items {
		if strings.Contains(strings.ToUpper(it.label), f) {
			out = append(out, it)
		}
	}
	return out
}

func (lo *listOverlay) move(d int) {
	n := len(lo.visible())
	if n == 0 {
		lo.cursor = 0
		return
	}
	lo.cursor = (lo.cursor + d + n) % n
}

func (lo *listOverlay) handleKey(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.pop()
	case "up", "ctrl+k":
		lo.move(-1)
	case "down", "ctrl+j":
		lo.move(1)
	case "enter":
		vis := lo.visible()
		if len(vis) == 0 || lo.cursor >= len(vis) {
			if lo.onMiss != nil && lo.filter != "" {
				return lo.onMiss(m, lo.filter)
			}
			return nil
		}
		if lo.onSelect != nil {
			return lo.onSelect(m, vis[lo.cursor])
		}
	case "backspace":
		if lo.filterable && len(lo.filter) > 0 {
			lo.filter = lo.filter[:len(lo.filter)-1]
			lo.cursor = 0
		}
	default:
		k := msg.String()
		if !lo.filterable && (k == "j" || k == "k") { // vim motion only when not typing
			if k == "k" {
				lo.move(-1)
			} else {
				lo.move(1)
			}
			return nil
		}
		if lo.filterable && len(k) == 1 {
			lo.filter += k
			lo.cursor = 0
		}
	}
	return nil
}

func (lo *listOverlay) view(m *Model, maxW, maxH int) string {
	t := m.theme
	vis := lo.visible()

	labelOf := func(it listItem) string {
		s := it.label
		if it.on {
			s += " ✓"
		}
		return s
	}

	// Measure the widest row so the selection bar spans the full panel.
	textW := lipgloss.Width(lo.title)
	for _, it := range vis {
		w := lipgloss.Width("▸ "+labelOf(it)) + 2
		if it.hint != "" {
			w += lipgloss.Width(it.hint) + 2
		}
		if w > textW {
			textW = w
		}
	}
	if lo.footnote != "" {
		if w := lipgloss.Width(lo.footnote); w > textW {
			textW = w
		}
	}
	textW = clampInt(textW, 18, maxW-4)

	pad := func(s string) string { return padRight(truncate(s, textW), textW) }
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFDF5")).Background(t.Accent).Bold(true)
	row := lipgloss.NewStyle().Foreground(t.Text)

	rows := []string{pad(t.Title(lo.title))}
	if lo.filterable {
		rows = append(rows, pad(t.Label.Render("/"+lo.filter+"▏")))
	}
	rows = append(rows, t.Divider(textW))
	if len(vis) == 0 {
		miss := "(no matches)"
		if lo.onMiss != nil && lo.filter != "" {
			miss = "enter: add " + strings.ToUpper(lo.filter)
		}
		rows = append(rows, pad(t.Label.Render(miss)))
	}

	// Window the rows so a long list (model picker) fits the terminal.
	maxRows := max(3, maxH-7)
	start := 0
	if lo.cursor >= maxRows {
		start = lo.cursor - maxRows + 1
	}
	for i := start; i < len(vis) && i < start+maxRows; i++ {
		it := vis[i]
		line := "  " + labelOf(it)
		if i == lo.cursor {
			line = "▸ " + labelOf(it)
		}
		if it.hint != "" {
			gap := max(1, textW-lipgloss.Width(line)-lipgloss.Width(it.hint))
			line += strings.Repeat(" ", gap) + it.hint
		}
		if i == lo.cursor {
			rows = append(rows, sel.Render(pad(line)))
		} else {
			rows = append(rows, row.Render(pad(line)))
		}
	}
	if start > 0 || start+maxRows < len(vis) {
		rows = append(rows, pad(t.Label.Render(fmt.Sprintf("… %d/%d", lo.cursor+1, len(vis)))))
	}

	if lo.footnote != "" {
		rows = append(rows, pad(t.Label.Render(lo.footnote)))
	}
	rows = append(rows, pad(t.KeyHints([][2]string{{"↑↓", "move"}, {"enter", "select"}, {"esc", "back"}})))
	return t.PaneFocused.Padding(0, 1).Render(strings.Join(rows, "\n"))
}

// --- compositing -----------------------------------------------------------

// compositeBox floats box centered on top of base with a drop shadow, using the
// lipgloss layer compositor, clamped to the terminal.
func (m *Model) compositeBox(base, box string) string {
	base = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(base)
	box = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(box)
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x := max(0, (m.width-bw)/2)
	y := max(0, (m.height-bh)/2)
	sx := min(x+1, max(0, m.width-bw))
	sy := min(y+1, max(0, m.height-bh))

	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(base).Z(0),
		lipgloss.NewLayer(shadowBlock(m.theme, bw, bh)).X(sx).Y(sy).Z(1),
		lipgloss.NewLayer(box).X(x).Y(y).Z(2),
	}
	out := lipgloss.NewCompositor(layers...).Render()
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(out)
}

// shadowBlock returns a w×h block of dim shadow glyphs for the drop shadow.
func shadowBlock(t Theme, w, h int) string {
	if w < 1 || h < 1 {
		return ""
	}
	line := lipgloss.NewStyle().Foreground(t.BarTrack).Render(strings.Repeat("░", w))
	rows := make([]string, h)
	for i := range rows {
		rows[i] = line
	}
	return strings.Join(rows, "\n")
}

// --- small helpers ----------------------------------------------------------

func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// joinPM renders "provider · model" for settings labels, tolerating blanks.
func joinPM(prov, model string) string {
	switch {
	case prov == "" && model == "":
		return ""
	case model == "":
		return prov
	case prov == "":
		return model
	}
	return prov + " · " + model
}

// jumpToCoin clears the watchlist filter and selects the chosen coin.
func (m *Model) jumpToCoin(coin string) {
	m.filter = ""
	for i, c := range m.ordered() {
		if c == coin {
			m.selected = i
			return
		}
	}
}

// generateThesis generates a HL-data-grounded thesis for the selected asset.
// When ThesisFn is wired it fetches fresh multi-TF perp data first (async),
// then submits to the chat LLM with that as context. Without ThesisFn it
// submits a generic (ungrounded) thesis prompt via submitChat — never a
// silent failure.
func (m *Model) generateThesis() tea.Cmd {
	coin := m.selectedCoin()
	if coin == "" {
		return m.note("no asset selected")
	}
	m.setChatFocus()
	m.chatTab = chatTabAgent
	tf := m.timeframes[coin]

	if m.thesisFn == nil {
		prompt := fmt.Sprintf(
			"Give a concise trading thesis for %s on the %s timeframe: directional bias, key levels, and the primary risk.",
			coin, tf)
		return m.submitChat(prompt)
	}

	fn := m.thesisFn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		data, err := fn(ctx, coin, tf)
		return thesisContextMsg{coin: coin, tf: tf, context: data, err: err}
	}
}
