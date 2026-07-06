package cockpit

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case barMsg, positionMsg:
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
	case "esc":
		m.chatOpen = false
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

// submit routes the chat bar's input; Task 4 replaces this stub with the
// command dispatcher + chat call.
func (m *Model) submit(text string) tea.Cmd {
	m.turns = append(m.turns, chatTurn("user", text))
	return nil
}
