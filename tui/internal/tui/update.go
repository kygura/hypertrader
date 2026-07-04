package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// Update implements tea.Model. It handles window sizing, navigation keys, chat
// input, and the bus-derived messages from the bridge.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case spinner.TickMsg:
		// Keep the chat "thinking" indicator animating only while busy.
		if m.chat.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case barMsg:
		// Live data refresh: re-render pulls from the cache, nothing to store here.
		return m, nil

	case verdictMsg:
		v := apiclient.Verdict(msg)
		if v.Thesis != "" {
			m.thesis[v.Asset] = v.Thesis
		}
		if v.Reading != "" {
			m.reading[v.Asset] = v.Reading
		}
		m.upsertCandidate(v) // the ranked board is the synthesis surface
		m.postThesis(v)      // proactive feed: the agent speaks up in the conversation
		return m, nil

	case journalMsg:
		if e, ok := liveEntryFrom(msg.Coin, msg.Kind, msg.Summary, msg.Verdict); ok {
			m.liveEntries = append(m.liveEntries, e)
			m.refreshLive()
		}
		if msg.Kind == "alert" || msg.Kind == "error" {
			return m, m.note(msg.Summary)
		}
		return m, nil

	case thesisContextMsg:
		contextText := msg.context
		if msg.err != nil {
			// No local fallback grounding in the standalone TUI — there is no
			// in-process store to read from anymore, so a failed ThesisFn
			// call just proceeds without the extra HL data block rather than
			// blocking the thesis prompt entirely.
			contextText = ""
		}
		prompt := fmt.Sprintf(
			"Generate a concise trading thesis for %s on the %s timeframe. "+
				"State: directional bias, the two or three key price levels (support/resistance from swing highs/lows), "+
				"and the primary risk. Ground it in the perp mechanics shown in the data above — "+
				"funding direction, OI trend, basis. One tight paragraph.",
			msg.coin, msg.tf)
		return m, m.submitChatWithCtx(prompt, contextText)

	case statusMsg:
		// Only StatusConn events own m.connected and m.provider. Notice-only events
		// (batch reasoner errors, aggregator warnings) carry the erroring provider
		// in Detail ("deepseek: api error: …") so the attribution is visible in the
		// status line without clobbering the chat-provider display.
		if msg.Kind == statusConn {
			m.connected = msg.Connected
			if msg.Provider != "" {
				m.provider = msg.Provider
			}
		}
		if msg.Mode != "" {
			m.mode = msg.Mode
		}
		if msg.Detail != "" {
			return m, m.note(msg.Detail)
		}
		return m, nil

	case chatReplyMsg:
		m.chat.busy = false
		text := msg.text
		if msg.err != nil {
			text = "error: " + msg.err.Error()
		}
		m.chat.turns = append(m.chat.turns, apiclient.ChatTurn{Role: "assistant", Text: text})
		m.refreshChat()
		return m, nil

	case fetchSettingsMsg:
		// A failed refresh keeps the last-known-good snapshot rather than
		// blanking provider/model lists out from under an open settings modal.
		if msg.err == nil {
			m.settings = msg.settings
		}
		return m, nil

	case statusClearMsg:
		// Expire a transient status note, but only the one that armed this timer —
		// a newer note owns the bar and gets its own expiry.
		if msg.seq == m.statusSeq {
			m.statusMsg = ""
		}
		return m, nil
	}

	// Forward to the focused interactive component.
	if m.focus == focusChat && !m.hasOverlay() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// statusClearMsg expires the transient status note armed by note(). seq ties the
// timer to the note that started it, so a newer note isn't cleared early.
type statusClearMsg struct{ seq int }

// thesisContextMsg carries the result of the async HL data fetch kicked off by
// generateThesis. On success, context holds the multi-TF grounding block; on
// error, submitChatWithCtx proceeds with no extra grounding block.
type thesisContextMsg struct {
	coin    string
	tf      string
	context string
	err     error
}

// statusNoteTTL is how long a transient note owns the status bar before the key
// hints return.
const statusNoteTTL = 6 * time.Second

// note shows a transient message on the status line and arms its expiry, so
// feedback ("sort: signal", "mode → autonomous") never permanently hides the
// key hints.
func (m *Model) note(s string) tea.Cmd {
	m.statusMsg = s
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(statusNoteTTL, func(time.Time) tea.Msg { return statusClearMsg{seq} })
}

// handleKey is the single routing rule for keys: quit and the global chords work
// from anywhere (even while typing in chat), then the top of the overlay stack
// owns the keyboard, then the detail modal, then the watchlist filter, then the
// dashboard keymap for the focused pane.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global chords: ctrl-modified keys reach here regardless of focus, so these
	// work even while the chat input is capturing plain letters.
	switch key {
	case "ctrl+c", "ctrl+q":
		return m, tea.Quit
	case "ctrl+s":
		// Toggle the settings hub: close it if it's on top (and not mid-edit),
		// open it when nothing floats. With another picker on top, esc backs out.
		if so, ok := m.top().(*settingsOverlay); ok && !so.editing {
			m.pop()
			return m, nil
		}
		if !m.hasOverlay() {
			m.openSettings()
			return m, nil
		}
	case "f1":
		// Toggle help from anywhere — the function-key twin of '?'.
		if _, ok := m.top().(*helpOverlay); ok {
			m.pop()
		} else if !m.hasOverlay() {
			m.openHelp()
		}
		return m, nil
	case "ctrl+up":
		// Grow the chat pane so more conversation history is visible.
		m.chatHeightOffset = min(m.chatHeightOffset+2, 20)
		m.resize()
		return m, nil
	case "ctrl+down":
		// Shrink the chat pane back toward the default height.
		m.chatHeightOffset = max(m.chatHeightOffset-2, -10)
		m.resize()
		return m, nil
	}

	// Floating panels (settings / pickers / help) are modal, newest on top.
	if top := m.top(); top != nil {
		return m, top.handleKey(m, msg)
	}

	// The floating detail view (enter on a market in narrow layouts) is modal:
	// it scrolls itself, t cycles its timeframe, and esc/enter/q dismiss it.
	if m.detailModal {
		switch key {
		case "esc", "enter", "q":
			m.detailModal = false
		case "t":
			m.cycleTimeframe()
		case "up", "down", "j", "k", "pgup", "pgdown", "ctrl+u", "ctrl+d":
			var cmd tea.Cmd
			m.detailVP, cmd = m.detailVP.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Filtering mode captures typing for the watchlist filter. Enter keeps the
	// filter applied; esc cancels it entirely (no stale filter left behind).
	if m.filtering {
		switch key {
		case "enter":
			m.filtering = false
		case "esc":
			m.filtering, m.filter = false, ""
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
			}
		default:
			if len(key) == 1 {
				m.filter += key
			}
		}
		return m, nil
	}

	// Navigation: ↑/↓ in the chat pane recall previous input (shell-style) — or
	// move the board cursor on the ideas tab; in any other focus they move the
	// market selection. The detail panel and chat grounding follow the market
	// selection from any focus.
	switch key {
	case "up":
		switch {
		case m.focus == focusChat && m.chatTab == chatTabIdeas:
			m.moveIdeasSel(-1)
		case m.focus == focusChat:
			m.navigateHistory(-1)
		default:
			m.moveSelection(-1)
		}
		return m, nil
	case "down":
		switch {
		case m.focus == focusChat && m.chatTab == chatTabIdeas:
			m.moveIdeasSel(1)
		case m.focus == focusChat:
			m.navigateHistory(1)
		default:
			m.moveSelection(1)
		}
		return m, nil
	}

	// Chat focus: route text to the input, except focus / send / scroll keys.
	if m.focus == focusChat {
		switch key {
		case "esc":
			m.focus = focusMarkets
			m.input.Blur()
			return m, nil
		case "tab":
			// Accept a ghost-text suggestion when one is active; otherwise cycle focus.
			if m.input.CurrentSuggestion() != "" {
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
			m.cycleFocus()
			return m, nil
		case "shift+tab":
			m.cycleFocusBack()
			return m, nil
		case "]":
			// Cycle sub-tab forward (agent → ideas → live → agent).
			m.chatTab = (m.chatTab + 1) % chatTabCount
			return m, nil
		case "[":
			// Cycle sub-tab backward.
			m.chatTab = (m.chatTab - 1 + chatTabCount) % chatTabCount
			return m, nil
		case "enter":
			switch m.chatTab {
			case chatTabLive:
				// Enter on the live tab switches back to the agent tab.
				m.chatTab = chatTabAgent
				return m, nil
			case chatTabIdeas:
				// Enter on the board jumps the marketwatch to the candidate.
				m.jumpToCandidate()
				m.focus = focusMarkets
				m.input.Blur()
				return m, nil
			}
			return m, m.sendChat()
		case "pgup", "pgdown":
			var cmd tea.Cmd
			switch m.chatTab {
			case chatTabLive:
				m.liveVP, cmd = m.liveVP.Update(msg)
			case chatTabIdeas:
				m.ideasVP, cmd = m.ideasVP.Update(msg)
			default:
				m.chatVP, cmd = m.chatVP.Update(msg)
			}
			return m, cmd
		default:
			if m.chatTab != chatTabAgent {
				// No typing outside the conversation tab — fall through.
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	// Markets / detail focus: the full keymap. j/k mirror ↑/↓; h/l and ←/→ move
	// focus across columns; PgUp/PgDn scroll the focused detail panel.
	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		// A live filter is the most recent "mode" entered — esc unwinds it first;
		// a second esc hands focus to chat.
		if m.filter != "" {
			m.filter = ""
			return m, nil
		}
		m.setChatFocus()
	case "?":
		m.openHelp()
	case "tab":
		m.cycleFocus()
	case "shift+tab":
		m.cycleFocusBack()
	case "left", "h":
		m.focusLeft()
	case "right", "l":
		m.focusRight()
	case "k":
		if m.focus == focusDetail {
			var cmd tea.Cmd
			m.detailVP, cmd = m.detailVP.Update(msg)
			return m, cmd
		}
		m.moveSelection(-1)
	case "j":
		if m.focus == focusDetail {
			var cmd tea.Cmd
			m.detailVP, cmd = m.detailVP.Update(msg)
			return m, cmd
		}
		m.moveSelection(1)
	case "home":
		m.selected = 0
	case "end", "G", "shift+g":
		m.selected = max(0, len(m.ordered())-1)
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		if m.focus == focusDetail {
			var cmd tea.Cmd
			m.detailVP, cmd = m.detailVP.Update(msg)
			return m, cmd
		}
	case "[":
		if m.focus == focusDetail {
			m.detailSection = (m.detailSection - 1 + detailSectionCount) % detailSectionCount
		}
	case "]":
		if m.focus == focusDetail {
			m.detailSection = (m.detailSection + 1) % detailSectionCount
		}
	case "enter":
		if m.focus == focusDetail {
			return m, m.detailSectionAction()
		}
		m.showDetail()
	case "x":
		// Toggle tracking for the selected coin without leaving the current pane.
		if coin := m.selectedCoin(); coin != "" {
			if m.tracked[coin] {
				return m, m.note(m.cmdTrack([]string{"rm", coin}))
			}
			return m, m.note(m.cmdTrack([]string{"add", coin}))
		}
	case "1":
		m.focus = focusMarkets
		m.input.Blur()
	case "2":
		m.showDetail()
	case "3":
		m.chatTab = chatTabAgent
		m.setChatFocus()
	case "4":
		m.chatTab = chatTabIdeas
		m.setChatFocus()
	case "5":
		m.chatTab = chatTabLive
		m.setChatFocus()
	case "o":
		return m, m.cycleSort()
	case "S", "shift+s":
		return m, m.scanNow(nil)
	case "s":
		m.openSettings()
	case "m":
		m.pushModelPicker(RoleChat)
	case "g":
		return m, m.generateThesis()
	case "ctrl+p":
		m.pushMarketsManager()
	case "t":
		m.cycleTimeframe()
	case "/":
		m.filtering = true
		m.filter = ""
	}
	return m, nil
}

// showDetail brings the selected market's detail into view: focuses the pane when
// the current layout shows it, otherwise floats the detail as a modal on top.
func (m *Model) showDetail() {
	if m.lay.detailW > 0 {
		m.focus = focusDetail
		m.input.Blur()
		return
	}
	m.detailModal = true
	m.detailVP.GotoTop()
}

// moveSelection moves the watchlist cursor by d, clamped to the ordered list.
func (m *Model) moveSelection(d int) {
	n := len(m.ordered())
	if n == 0 {
		return
	}
	m.selected += d
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= n {
		m.selected = n - 1
	}
}

// focusLeft / focusRight move focus across the visible columns in screen order
// (markets ← chat ← detail), skipping the detail column when it isn't shown.
func (m *Model) focusLeft() {
	switch m.focus {
	case focusChat:
		m.focus = focusMarkets
	case focusDetail:
		m.focus = focusChat
	}
	m.syncInputFocus()
}

func (m *Model) focusRight() {
	switch m.focus {
	case focusMarkets:
		m.focus = focusChat
	case focusChat:
		if m.lay.detailW > 0 {
			m.focus = focusDetail
		}
	}
	m.syncInputFocus()
}

// syncInputFocus keeps the text input focused iff the chat column has focus.
func (m *Model) syncInputFocus() {
	if m.focus == focusChat {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
}

// cycleFocus advances focus in screen order markets → chat → detail → markets,
// skipping the detail column when the current layout doesn't show it.
func (m *Model) cycleFocus() {
	switch m.focus {
	case focusMarkets:
		m.focus = focusChat
	case focusChat:
		if m.lay.detailW > 0 {
			m.focus = focusDetail
		} else {
			m.focus = focusMarkets
		}
	case focusDetail:
		m.focus = focusMarkets
	}
	m.syncInputFocus()
}

// cycleFocusBack is cycleFocus reversed (shift+tab).
func (m *Model) cycleFocusBack() {
	switch m.focus {
	case focusMarkets:
		if m.lay.detailW > 0 {
			m.focus = focusDetail
		} else {
			m.focus = focusChat
		}
	case focusChat:
		m.focus = focusMarkets
	case focusDetail:
		m.focus = focusChat
	}
	m.syncInputFocus()
}

func (m *Model) setChatFocus() {
	m.focus = focusChat
	m.input.Focus()
}

func (m *Model) cycleTimeframe() {
	coin := m.selectedCoin()
	if coin == "" {
		return
	}
	cur := m.timeframes[coin]
	idx := 0
	for i, tf := range m.tfCycle {
		if tf == cur {
			idx = i
			break
		}
	}
	m.timeframes[coin] = m.tfCycle[(idx+1)%len(m.tfCycle)]
}

// detailSectionAction fires the keyboard action bound to the currently focused
// detail section. Signals → ask agent; thesis → regenerate; others → no-op.
func (m *Model) detailSectionAction() tea.Cmd {
	coin := m.selectedCoin()
	switch m.detailSection {
	case detailSectionSignals:
		conf := m.confluence(coin)
		if len(conf) == 0 {
			return m.note("no signals yet for " + coin)
		}
		c := conf[0]
		prompt := fmt.Sprintf(
			"Explain the %s signal for %s across %s — what does it mean for direction and what is the key risk?",
			c.Label, coin, strings.Join(c.Timeframes, "/"))
		m.setChatFocus()
		return m.submitChat(prompt)
	case detailSectionThesis:
		return m.generateThesis()
	}
	return nil
}

// sendChat dispatches the input. A leading "/" is a configuration command handled
// locally and echoed into the pane; anything else is a chat turn run on the LLM.
func (m *Model) sendChat() tea.Cmd {
	text := m.input.Value()
	if text == "" || m.chat.busy {
		return nil
	}
	m.input.SetValue("")
	// Record in history (deduplicate consecutive identical entries).
	if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
		m.inputHistory = append(m.inputHistory, text)
	}
	m.historyCursor = -1

	if isCommand(text) {
		m.chat.turns = append(m.chat.turns, apiclient.ChatTurn{Role: "user", Text: text})
		reply, cmd := m.runCommand(text)
		if reply != "" {
			m.chat.turns = append(m.chat.turns, apiclient.ChatTurn{Role: "system", Text: reply})
		}
		m.refreshChat()
		m.chatVP.GotoBottom()
		return cmd
	}

	return m.submitChat(text)
}

// submitChat appends a user turn, marks the chat busy, and kicks off the LLM
// call alongside the thinking spinner. Shared by free-text chat and the
// "generate thesis" action (with no extra grounding — see submitChatWithCtx).
func (m *Model) submitChat(text string) tea.Cmd {
	return m.submitChatWithCtx(text, "")
}

// submitChatWithCtx is like submitChat but folds contextText (extra grounding
// the caller already fetched — e.g. generateThesis's HL multi-TF data) into
// the message sent to the LLM. contextText never appears in the visible
// transcript: the user turn shown in the pane is always the clean prompt.
// Regular chat grounding is otherwise built server-side by the daemon's
// /api/chat handler, which is why ChatFunc itself takes no context argument.
func (m *Model) submitChatWithCtx(text, contextText string) tea.Cmd {
	if text == "" || m.chat.busy {
		return nil
	}
	m.chat.turns = append(m.chat.turns, apiclient.ChatTurn{Role: "user", Text: text})
	m.chat.busy = true
	m.refreshChat()
	m.chatVP.GotoBottom()

	history := chatHistoryForLLM(m.chat.turns)
	userMsg := text
	if contextText != "" {
		userMsg = contextText + "\n\n" + text
	}
	fn := m.chatFn
	ask := func() tea.Msg {
		if fn == nil {
			return chatReplyMsg{err: fmt.Errorf("chat not configured")}
		}
		reply, err := fn(context.Background(), userMsg, history)
		return chatReplyMsg{text: reply, err: err}
	}
	// Run the LLM call and start the thinking spinner together.
	return tea.Batch(ask, m.spinner.Tick)
}

// postThesis adds an actionable batch verdict to the conversation as a proactive
// agent turn — the "agent speaks up" feed. Holds stay quiet (they're reflected in the
// panel), and a thesis is only posted once per asset until it changes, so a steady
// regime doesn't spam the transcript every batch cycle.
func (m *Model) postThesis(v apiclient.Verdict) {
	if v.Action == apiclient.ActionHold || v.Thesis == "" {
		return
	}
	if m.postedThesis[v.Asset] == v.Thesis {
		return
	}
	m.postedThesis[v.Asset] = v.Thesis

	headline := fmt.Sprintf("%s · %s %s · %s", time.Now().Format("15:04"), v.Asset, v.Timeframe, v.Action)
	if v.Confidence > 0 {
		headline += fmt.Sprintf(" · %.0f%%", v.Confidence*100)
	}
	m.chat.turns = append(m.chat.turns, apiclient.ChatTurn{Role: roleThesis, Text: headline + "\n" + v.Thesis})
	m.refreshChat()
}

// navigateHistory moves the input history cursor by d steps.
// d = -1 (Up key) goes to an older entry; d = 1 (Down key) goes to a newer one.
// When at the bottom (not navigating) pressing Up saves the draft and jumps to the
// most-recent entry. Pressing Down past the newest restores the saved draft.
func (m *Model) navigateHistory(d int) {
	if len(m.inputHistory) == 0 {
		return
	}
	if m.historyCursor == -1 {
		if d > 0 {
			return // already at the bottom (current draft); can't go newer
		}
		m.historyDraft = m.input.Value()
		m.historyCursor = len(m.inputHistory) - 1
		m.input.SetValue(m.inputHistory[m.historyCursor])
		m.input.CursorEnd()
		return
	}
	m.historyCursor += d
	if m.historyCursor < 0 {
		m.historyCursor = 0
		return // already at oldest, stay
	}
	if m.historyCursor >= len(m.inputHistory) {
		// Moved past the newest: restore saved draft.
		m.historyCursor = -1
		m.input.SetValue(m.historyDraft)
		m.input.CursorEnd()
		return
	}
	m.input.SetValue(m.inputHistory[m.historyCursor])
	m.input.CursorEnd()
}

// handleMouseWheel scrolls the viewport under the mouse cursor. Routing uses
// the resolved layout geometry so scroll-wheel works regardless of pane focus.
func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y
	l := m.lay
	var cmd tea.Cmd
	switch l.mode {
	case layoutWide:
		if y >= l.marketsH && y < l.marketsH+l.chatH {
			if m.chatTab == chatTabLive {
				m.liveVP, cmd = m.liveVP.Update(msg)
			} else {
				m.chatVP, cmd = m.chatVP.Update(msg)
			}
		} else if y < l.detailH && x >= l.marketsW {
			m.detailVP, cmd = m.detailVP.Update(msg)
		}
	case layoutNarrow:
		if y >= l.marketsH && y < l.marketsH+l.chatH {
			if m.chatTab == chatTabLive {
				m.liveVP, cmd = m.liveVP.Update(msg)
			} else {
				m.chatVP, cmd = m.chatVP.Update(msg)
			}
		}
	case layoutTiny:
		if y >= 1 && y < 1+l.chatH {
			if m.chatTab == chatTabLive {
				m.liveVP, cmd = m.liveVP.Update(msg)
			} else {
				m.chatVP, cmd = m.chatVP.Update(msg)
			}
		}
	}
	return m, cmd
}

// handleMouseClick handles a left-click: focuses the clicked pane. Ignored when
// an overlay or the detail modal is open (keyboard has already been captured).
func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.hasOverlay() || m.detailModal {
		return m, nil
	}
	x, y := msg.X, msg.Y
	l := m.lay
	switch l.mode {
	case layoutWide:
		switch {
		case y >= l.marketsH && y < l.marketsH+l.chatH:
			m.setChatFocus()
		case y < l.detailH && x >= l.marketsW && l.detailW > 0:
			m.focus = focusDetail
			m.input.Blur()
		case y < l.marketsH:
			m.focus = focusMarkets
			m.input.Blur()
		}
	case layoutNarrow:
		switch {
		case y >= l.marketsH && y < l.marketsH+l.chatH:
			m.setChatFocus()
		case y < l.marketsH:
			m.focus = focusMarkets
			m.input.Blur()
		}
	case layoutTiny:
		if y >= 1 && y < 1+l.chatH {
			m.setChatFocus()
		}
	}
	return m, nil
}

// chatHistoryForLLM projects the on-screen transcript to the turns the LLM should
// see: user and assistant turns only. Command echoes (system) are dropped, and
// proactive theses are relayed as assistant turns so the agent recalls them — this
// also prevents non-standard roles from reaching the provider APIs.
func chatHistoryForLLM(turns []apiclient.ChatTurn) []apiclient.ChatTurn {
	out := make([]apiclient.ChatTurn, 0, len(turns))
	for _, t := range turns {
		switch t.Role {
		case roleUser:
			out = append(out, t)
		case roleAgent, roleThesis:
			out = append(out, apiclient.ChatTurn{Role: roleAgent, Text: t.Text})
		}
	}
	return out
}
