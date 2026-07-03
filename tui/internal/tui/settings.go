// The settings modal: a tabbed configuration hub (Models · API Keys · Trading ·
// Markets) opened with s. Every change applies to the live daemon through
// Controls and persists to config.toml through Controls.SaveSettings /
// SetAPIKey, so a model switch or a pasted API key survives a restart. API keys
// are entered through a masked textinput and never echoed back in full.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// Settings is the persisted snapshot handed to Controls.SaveSettings: the
// per-role model bindings plus the execution mode. The daemon maps it onto
// config.toml; the TUI never touches the file directly.
type Settings struct {
	ChatProvider, ChatModel   string
	BatchProvider, BatchModel string
	Mode                      string
}

// RiskView is the read-only execution-risk display for the Trading tab.
type RiskView struct {
	MaxPositionUSD      float64
	MaxTotalExposureUSD float64
	MaxConcurrent       int
	DailyLossKillUSD    float64
}

type settingsTab int

const (
	tabModels settingsTab = iota
	tabKeys
	tabTrading
	tabMarkets
	tabCount
)

func (s settingsTab) String() string {
	switch s {
	case tabModels:
		return "Models"
	case tabKeys:
		return "API Keys"
	case tabTrading:
		return "Trading"
	default:
		return "Markets"
	}
}

// settingsRow is one actionable line in a tab: label/value for display, act
// fires on enter. A nil act renders as read-only.
type settingsRow struct {
	label string
	value string
	note  string // dim annotation under the value column
	act   func(m *Model, so *settingsOverlay) tea.Cmd
}

// settingsOverlay is the tabbed hub. Rows are rebuilt from live state on every
// render, so a provider switched in a nested picker shows immediately.
type settingsOverlay struct {
	tab    settingsTab
	cursor int

	// Inline editor (API key entry / add-market symbol).
	editing   bool
	editLabel string
	input     textinput.Model
	onSubmit  func(m *Model, so *settingsOverlay, value string) tea.Cmd

	status string // inline feedback: "✓ anthropic key saved", "✗ ..."
}

func newSettingsOverlay(tab settingsTab) *settingsOverlay {
	return &settingsOverlay{tab: tab}
}

// openSettings pushes the settings hub (s key, /settings).
func (m *Model) openSettings() { m.push(newSettingsOverlay(tabModels)) }

// openAPIKeys jumps straight to the API Keys tab (/keys).
func (m *Model) openAPIKeys() { m.push(newSettingsOverlay(tabKeys)) }

// --- persistence -----------------------------------------------------------

// saveSettings snapshots the live model/mode selections through SaveSettings so
// they land in config.toml. Errors surface on the status line, never crash.
func (m *Model) saveSettings() {
	if m.controls.SaveSettings == nil {
		return
	}
	chatProv, chatModel := m.activeModel(reasoner.RoleChat)
	batchProv, batchModel := m.activeModel(reasoner.RoleBatch)
	err := m.controls.SaveSettings(Settings{
		ChatProvider: chatProv, ChatModel: chatModel,
		BatchProvider: batchProv, BatchModel: batchModel,
		Mode: m.mode,
	})
	if err != nil {
		m.statusMsg = "save: " + err.Error()
	}
}

// --- rows per tab ----------------------------------------------------------

func (so *settingsOverlay) rows(m *Model) []settingsRow {
	switch so.tab {
	case tabModels:
		return so.modelRows(m)
	case tabKeys:
		return so.keyRows(m)
	case tabTrading:
		return so.tradingRows(m)
	default:
		return so.marketRows(m)
	}
}

func (so *settingsOverlay) modelRows(m *Model) []settingsRow {
	chatProv, chatModel := m.activeModel(reasoner.RoleChat)
	batchProv, batchModel := m.activeModel(reasoner.RoleBatch)
	pick := func(role reasoner.Role) func(*Model, *settingsOverlay) tea.Cmd {
		return func(m *Model, so *settingsOverlay) tea.Cmd {
			m.pushModelPicker(role)
			return nil
		}
	}
	// Provider rows are omitted: selecting a model automatically applies its
	// provider, so there is no need to choose providers separately.
	return []settingsRow{
		{label: "Chat model", value: orDash(joinPM(chatProv, chatModel)),
			note: "answers the chat pane and escalations", act: pick(reasoner.RoleChat)},
		{label: "Batch model", value: orDash(joinPM(batchProv, batchModel)),
			note: "reads every digest batch — cheap model recommended", act: pick(reasoner.RoleBatch)},
	}
}

func (so *settingsOverlay) keyRows(m *Model) []settingsRow {
	var names []string
	if m.controls.ProviderNames != nil {
		names = m.controls.ProviderNames()
	}
	sort.Strings(names)
	rows := make([]settingsRow, 0, len(names))
	for _, name := range names {
		hint := ""
		if m.controls.KeyHint != nil {
			hint = m.controls.KeyHint(name)
		}
		value, note := "○ not set", "enter to paste a key — stored in config.toml"
		if hint != "" {
			value, note = "● "+hint, "enter to replace"
		}
		prov := name
		rows = append(rows, settingsRow{label: prov, value: value, note: note,
			act: func(m *Model, so *settingsOverlay) tea.Cmd {
				so.startEdit("API key · "+prov, true, func(m *Model, so *settingsOverlay, v string) tea.Cmd {
					v = strings.TrimSpace(v)
					if v == "" {
						so.status = "✗ empty key — nothing saved"
						return nil
					}
					if m.controls.SetAPIKey == nil {
						so.status = "✗ key configuration unavailable"
						return nil
					}
					if err := m.controls.SetAPIKey(prov, v); err != nil {
						so.status = "✗ " + err.Error()
						return nil
					}
					so.status = "✓ " + prov + " key saved · applied live"
					return nil
				})
				return nil
			}})
	}
	return rows
}

func (so *settingsOverlay) tradingRows(m *Model) []settingsRow {
	r := m.risk
	return []settingsRow{
		{label: "Execution mode", value: m.mode,
			note: "propose = candidates only · autonomous = signs orders",
			act: func(m *Model, so *settingsOverlay) tea.Cmd {
				m.pushModePicker()
				return nil
			}},
		{label: "Max position", value: fmt.Sprintf("$%.0f", r.MaxPositionUSD), note: "edit in config.toml"},
		{label: "Max exposure", value: fmt.Sprintf("$%.0f", r.MaxTotalExposureUSD)},
		{label: "Max concurrent", value: fmt.Sprintf("%d positions", r.MaxConcurrent)},
		{label: "Daily kill-switch", value: fmt.Sprintf("$%.0f loss", r.DailyLossKillUSD)},
	}
}

func (so *settingsOverlay) marketRows(m *Model) []settingsRow {
	rows := make([]settingsRow, 0, len(m.visualized)+1)
	for _, c := range m.visualized {
		coin := c
		value := m.timeframes[coin]
		if m.tracked[coin] {
			value += "  ◆ tracked"
		}
		rows = append(rows, settingsRow{label: coin, value: value,
			act: func(m *Model, so *settingsOverlay) tea.Cmd {
				m.pushCoinActions(coin)
				return nil
			}})
	}
	rows = append(rows, settingsRow{label: "+ Add market", value: "",
		note: "subscribe a new Hyperliquid symbol",
		act: func(m *Model, so *settingsOverlay) tea.Cmd {
			so.startEdit("Add market (symbol)", false, func(m *Model, so *settingsOverlay, v string) tea.Cmd {
				v = strings.ToUpper(strings.TrimSpace(v))
				if v == "" {
					return nil
				}
				out := m.cmdWatch([]string{"add", v})
				so.status = "✓ " + out
				return nil
			})
			return nil
		}})
	return rows
}

// startEdit opens the inline editor under the rows. masked hides input glyphs
// (API keys); submit runs on enter.
func (so *settingsOverlay) startEdit(label string, masked bool, submit func(*Model, *settingsOverlay, string) tea.Cmd) {
	ti := textinput.New()
	ti.Prompt = "▸ "
	ti.Placeholder = "paste, then enter"
	if masked {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	ti.Focus()
	so.editing = true
	so.editLabel = label
	so.input = ti
	so.onSubmit = submit
	so.status = ""
}

// --- key handling -----------------------------------------------------------

func (so *settingsOverlay) handleKey(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if so.editing {
		switch msg.String() {
		case "esc":
			so.editing = false
			return nil
		case "enter":
			v := so.input.Value()
			so.editing = false
			if so.onSubmit != nil {
				return so.onSubmit(m, so, v)
			}
			return nil
		default:
			var cmd tea.Cmd
			so.input, cmd = so.input.Update(msg)
			return cmd
		}
	}

	rows := so.rows(m)
	switch msg.String() {
	case "esc", "q":
		m.pop()
	case "tab", "right", "l":
		so.switchTab(1)
	case "shift+tab", "left", "h":
		so.switchTab(-1)
	case "1", "2", "3", "4":
		so.tab = settingsTab(int(msg.String()[0] - '1'))
		so.cursor, so.status = 0, ""
	case "up", "k":
		so.moveCursor(rows, -1)
	case "down", "j":
		so.moveCursor(rows, 1)
	case "enter":
		if so.cursor < len(rows) && rows[so.cursor].act != nil {
			return rows[so.cursor].act(m, so)
		}
	}
	return nil
}

func (so *settingsOverlay) switchTab(d int) {
	so.tab = settingsTab((int(so.tab) + d + int(tabCount)) % int(tabCount))
	so.cursor, so.status = 0, ""
}

// moveCursor advances over rows, skipping read-only ones so the cursor always
// rests on something enter can act on (unless the whole tab is read-only).
func (so *settingsOverlay) moveCursor(rows []settingsRow, d int) {
	n := len(rows)
	if n == 0 {
		return
	}
	for range n {
		so.cursor = (so.cursor + d + n) % n
		if rows[so.cursor].act != nil {
			return
		}
	}
}

// --- view -------------------------------------------------------------------

func (so *settingsOverlay) view(m *Model, maxW, maxH int) string {
	t := m.theme
	w := clampInt(maxW-8, 40, 76)
	inner := w - 4 // border + padding

	var b []string
	b = append(b, t.Title("SETTINGS"))

	// Tab bar.
	var tabs []string
	for i := range int(tabCount) {
		tb := settingsTab(i)
		if tb == so.tab {
			tabs = append(tabs, t.TabActive.Render(tb.String()))
		} else {
			tabs = append(tabs, t.TabInactive.Render(tb.String()))
		}
	}
	b = append(b, strings.Join(tabs, " "), t.Divider(inner))

	// Rows: label column · value column, cursor row highlighted.
	rows := so.rows(m)
	labelW := 0
	for _, r := range rows {
		if l := lipgloss.Width(r.label); l > labelW {
			labelW = l
		}
	}
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFDF5")).Background(t.Accent).Bold(true)
	for i, r := range rows {
		marker, valStyle := "  ", lipgloss.NewStyle().Foreground(t.Violet)
		if r.act == nil {
			valStyle = lipgloss.NewStyle().Foreground(t.Dim)
		}
		if so.tab == tabKeys {
			valStyle = t.KeySet
			if strings.HasPrefix(r.value, "○") {
				valStyle = t.KeyUnset
			}
		}
		line := fmt.Sprintf("%s%s   %s", marker, padRight(r.label, labelW), valStyle.Render(r.value))
		if i == so.cursor && r.act != nil && !so.editing {
			line = sel.Render(padRight(fmt.Sprintf("▸ %s   %s", padRight(r.label, labelW), r.value), inner))
		} else if r.act == nil {
			line = t.Label.Render(fmt.Sprintf("  %s   ", padRight(r.label, labelW))) + valStyle.Render(r.value)
		} else {
			line = fmt.Sprintf("  %s   ", padRight(t.Label.Render(r.label), labelW)) + valStyle.Render(r.value)
		}
		b = append(b, truncate(line, inner))
		// The cursor row's note renders beneath it as a dim explainer.
		if i == so.cursor && r.note != "" && !so.editing {
			b = append(b, t.Label.Render(truncate("  "+strings.Repeat(" ", labelW)+"   "+r.note, inner)))
		}
	}

	// Inline editor.
	if so.editing {
		so.input.SetWidth(max(8, inner-4))
		b = append(b, "", lipgloss.NewStyle().Foreground(t.Gold).Bold(true).Render(so.editLabel),
			so.input.View(),
			t.KeyHints([][2]string{{"enter", "save"}, {"esc", "cancel"}}))
	}

	// Status feedback line.
	if so.status != "" {
		style := lipgloss.NewStyle().Foreground(t.Up)
		if strings.HasPrefix(so.status, "✗") {
			style = lipgloss.NewStyle().Foreground(t.Down)
		}
		b = append(b, "", style.Render(truncate(so.status, inner)))
	}

	if !so.editing {
		b = append(b, "", t.KeyHints([][2]string{
			{"←→/tab", "tab"}, {"↑↓", "row"}, {"enter", "edit"}, {"esc", "close"},
		}))
	}

	body := strings.Join(b, "\n")
	body = lipgloss.NewStyle().MaxHeight(max(5, maxH-2)).Render(body)
	return t.PaneFocused.Width(w).Padding(0, 1).Render(body)
}

// --- nested pickers ----------------------------------------------------------

// pushProviderPicker lists registered providers for a role; selection applies
// live and persists.
func (m *Model) pushProviderPicker(role reasoner.Role) {
	cur, _ := m.activeModel(role)
	var names []string
	if m.controls.ProviderNames != nil {
		names = m.controls.ProviderNames()
	}
	items := make([]listItem, 0, len(names))
	for _, n := range names {
		hint := ""
		if m.controls.KeyHint != nil && m.controls.KeyHint(n) == "" {
			hint = "no key"
		}
		items = append(items, listItem{key: n, label: n, hint: hint, on: n == cur})
	}
	m.push(&listOverlay{
		title: "PROVIDER · " + string(role),
		items: items,
		onSelect: func(m *Model, it listItem) tea.Cmd {
			if m.controls.SetProvider != nil {
				if err := m.controls.SetProvider(role, it.key); err != nil {
					return m.note(err.Error())
				}
			}
			if role == reasoner.RoleChat {
				m.provider = it.key
			}
			m.saveSettings()
			m.pop()
			return m.note(string(role) + " provider → " + it.key)
		},
	})
}

// pushModelPicker lists every known model grouped by provider; picking a model
// under a different provider re-points the transport too.
func (m *Model) pushModelPicker(role reasoner.Role) {
	_, cur := m.activeModel(role)
	var items []listItem
	if m.controls.ProviderModels != nil {
		pm := m.controls.ProviderModels()
		provs := make([]string, 0, len(pm))
		for p := range pm {
			provs = append(provs, p)
		}
		sort.Strings(provs)
		for _, p := range provs {
			for _, id := range pm[p] {
				items = append(items, listItem{key: p + "\x00" + id, label: id, hint: p, on: id == cur})
			}
		}
	}
	m.push(&listOverlay{
		title:      "MODEL · " + string(role),
		items:      items,
		filterable: true,
		footnote:   "free-form ids: /model " + string(role) + " <id>",
		onSelect: func(m *Model, it listItem) tea.Cmd {
			prov, id, _ := strings.Cut(it.key, "\x00")
			if m.controls.SetProvider != nil {
				_ = m.controls.SetProvider(role, prov)
			}
			if m.controls.SetModel != nil {
				if err := m.controls.SetModel(role, id); err != nil {
					return m.note(err.Error())
				}
			}
			if role == reasoner.RoleChat {
				m.provider = prov
			}
			m.saveSettings()
			m.pop()
			return m.note(string(role) + " model → " + prov + "·" + id)
		},
	})
}

// pushModePicker flips propose/autonomous; applies live and persists.
func (m *Model) pushModePicker() {
	m.push(&listOverlay{
		title: "EXECUTION MODE",
		items: []listItem{
			{key: "propose", label: "propose", hint: "candidates need approval", on: m.mode == "propose"},
			{key: "autonomous", label: "autonomous", hint: "agent signs orders", on: m.mode == "autonomous"},
		},
		onSelect: func(m *Model, it listItem) tea.Cmd {
			if m.controls.SetMode != nil {
				if err := m.controls.SetMode(it.key); err != nil {
					return m.note("mode: " + err.Error())
				}
			}
			m.mode = it.key
			m.saveSettings()
			m.pop()
			return m.note("execution mode → " + it.key)
		},
	})
}

// pushMarketsManager is the filterable coin palette (ctrl+p): enter on a coin
// opens its actions; typing an unknown symbol offers to add it.
func (m *Model) pushMarketsManager() {
	items := make([]listItem, 0, len(m.visualized))
	for _, c := range m.visualized {
		hint := m.timeframes[c]
		if m.tracked[c] {
			hint += " ◆"
		}
		items = append(items, listItem{key: c, label: c, hint: hint})
	}
	m.push(&listOverlay{
		title:      "MARKETS  ◆ tracked",
		items:      items,
		filterable: true,
		onSelect: func(m *Model, it listItem) tea.Cmd {
			m.pushCoinActions(it.key)
			return nil
		},
		onMiss: func(m *Model, typed string) tea.Cmd {
			cmd := m.note(m.cmdWatch([]string{"add", strings.ToUpper(typed)}))
			m.pop()
			m.pushMarketsManager()
			return cmd
		},
	})
}

// pushCoinActions is the per-coin submenu: detail, track/untrack, timeframe,
// remove from watchlist.
func (m *Model) pushCoinActions(coin string) {
	track := listItem{key: "track", label: "Track", hint: "agent reasons over it"}
	if m.tracked[coin] {
		track = listItem{key: "untrack", label: "Untrack", hint: "stop agent reasoning", on: true}
	}
	m.push(&listOverlay{
		title: coin,
		items: []listItem{
			{key: "detail", label: "Show detail"},
			track,
			{key: "tf", label: "Timeframe", hint: m.timeframes[coin]},
			{key: "unwatch", label: "Remove from watchlist"},
		},
		onSelect: func(m *Model, it listItem) tea.Cmd {
			switch it.key {
			case "detail":
				m.jumpToCoin(coin)
				m.overlays = nil
				m.showDetail()
			case "track":
				cmd := m.note(m.cmdTrack([]string{"add", coin}))
				m.pop()
				m.pushCoinActions(coin)
				return cmd
			case "untrack":
				cmd := m.note(m.cmdTrack([]string{"rm", coin}))
				m.pop()
				m.pushCoinActions(coin)
				return cmd
			case "tf":
				m.pushTimeframePicker(coin)
			case "unwatch":
				cmd := m.note(m.cmdWatch([]string{"rm", coin}))
				m.pop()
				return cmd
			}
			return nil
		},
	})
}

// pushTimeframePicker picks the display/decision timeframe for one coin.
func (m *Model) pushTimeframePicker(coin string) {
	items := make([]listItem, 0, len(m.tfCycle))
	for _, tf := range m.tfCycle {
		items = append(items, listItem{key: tf, label: tf, on: tf == m.timeframes[coin]})
	}
	m.push(&listOverlay{
		title: "TIMEFRAME · " + coin,
		items: items,
		onSelect: func(m *Model, it listItem) tea.Cmd {
			cmd := m.note(m.cmdTimeframe([]string{it.key, coin}))
			m.pop()
			return cmd
		},
	})
}
