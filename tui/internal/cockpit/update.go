package cockpit

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case barMsg, positionMsg, thesisMsg:
		// Data already applied to the cache by the bridge; repaint happens
		// because a message arrived.
		return m, nil

	case journalMsg:
		text := msg.Summary
		if msg.Coin != "" {
			text = msg.Coin + " — " + msg.Summary
		}
		m.phase = tagFor(msg.Kind)
		m.journal = appendJournal(m.journal, journalEntry{at: timeNow(), tag: tagFor(msg.Kind), text: text})
		return m, nil

	case verdictMsg:
		text := fmt.Sprintf("%s %s", msg.Asset, msg.Action)
		if msg.Confidence > 0 {
			text += fmt.Sprintf(" %.0f%%", msg.Confidence*100)
		}
		if msg.Thesis != "" {
			text += " — " + msg.Thesis
		}
		m.phase = "REASON"
		m.journal = appendJournal(m.journal, journalEntry{at: timeNow(), tag: "REASON", text: text})
		return m, nil

	case statusMsg:
		switch msg.Kind {
		case statusConn:
			m.connected = msg.Connected
		case statusNotice:
			if msg.Mode != "" {
				m.mode = msg.Mode
			}
			// Reasoning-tier statuses (IDLE / REVIEW <coin> <tf> /
			// TRIGGER <coin> <tf>) drive the header phase strip — and a
			// trigger flashes its owning card — without journaling: the
			// journal stays events, the tier is state.
			if tier, coin, tf, extra, ok := parseTier(msg.Detail); ok {
				m.phase = msg.Detail
				if tier == "TRIGGER" {
					m.flashCard(coin, tf, extra)
				}
				return m, nil
			}
			if msg.Detail != "" {
				m.note("OPERATOR", msg.Detail)
			}
		}
		return m, nil

	case chatReplyMsg:
		m.busy = false
		if msg.err != nil {
			m.turns = append(m.turns, chatTurn("system", "error: "+msg.err.Error()))
		} else {
			m.turns = append(m.turns, chatTurn("assistant", msg.text))
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.input.Focused() {
		switch msg.String() {
		case "esc":
			m.input.Blur()
			m.chatOpen = false
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			if text == "" {
				return m, nil
			}
			return m, m.submit(text)
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "m":
		return m, m.toggleMode()
	case "/":
		m.chatOpen = true
		return m, m.input.Focus()
	}
	return m, nil
}

// toggleMode flips propose <-> autonomous via the daemon's control API. The
// authoritative mode value comes back as a statusMsg (either the push
// stream's or the one this cmd fabricates on success).
func (m *Model) toggleMode() tea.Cmd {
	if m.controls == nil {
		return nil
	}
	next := "autonomous"
	if m.mode == "autonomous" {
		next = "propose"
	}
	c := m.controls
	return func() tea.Msg {
		if err := c.SetMode(context.Background(), next); err != nil {
			return journalMsg{Kind: "error", Summary: "mode switch failed: " + err.Error()}
		}
		return statusMsg{Kind: statusNotice, Mode: next, Detail: "mode → " + next}
	}
}

// submit routes the chat bar's input: slash commands run through the
// dispatcher and record their output as a system turn; anything else is a
// user turn sent to the agent.
func (m *Model) submit(text string) tea.Cmd {
	if isCommand(text) {
		m.turns = append(m.turns, chatTurn("user", text))
		out, cmd := m.runCommand(text)
		if out != "" {
			m.turns = append(m.turns, chatTurn("system", out))
		}
		return cmd
	}
	if m.busy {
		// A chat call is already in flight; ignore free-text input entirely
		// rather than queuing a second concurrent request (which would
		// interleave replies and desync m.busy). Slash commands above are
		// unaffected — they may still run while the agent is thinking.
		return nil
	}
	// Snapshot history before appending this turn: the daemon appends the
	// current message to history itself, so including it here would
	// duplicate the question.
	history := chatHistory(m.turns)
	m.turns = append(m.turns, chatTurn("user", text))
	if m.chatFn == nil {
		m.turns = append(m.turns, chatTurn("system", "chat unavailable — no daemon connection"))
		return nil
	}
	m.busy = true
	return tea.Batch(m.sendChat(text, history), m.spin.Tick)
}

// chatHistory filters turns down to the roles the daemon's chat API
// accepts: "user" and "assistant". Slash-command output and chat errors are
// recorded locally as "system" turns for the journal/chat pane, but the
// backend forwards roles verbatim to the Anthropic API, which 400s on any
// role other than user/assistant — so "system" turns must never leak into
// the history sent upstream.
func chatHistory(turns []apiclient.ChatTurn) []apiclient.ChatTurn {
	out := make([]apiclient.ChatTurn, 0, len(turns))
	for _, t := range turns {
		if t.Role == "user" || t.Role == "assistant" {
			out = append(out, t)
		}
	}
	return out
}

// sendChat calls the daemon chat endpoint off the render loop. history must
// already be filtered (see chatHistory) and must not include the current
// text turn — the daemon appends the current message to history itself.
func (m *Model) sendChat(text string, history []apiclient.ChatTurn) tea.Cmd {
	fn := m.chatFn
	return func() tea.Msg {
		reply, err := fn(context.Background(), text, history)
		return chatReplyMsg{text: reply, err: err}
	}
}
