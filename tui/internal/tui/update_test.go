package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// TestGlobalShortcuts verifies the ctrl-chords work regardless of focus — the
// whole point of having them is that plain letters die inside the chat input.
func TestGlobalShortcuts(t *testing.T) {
	newSized := func(t *testing.T) *Model {
		m, _ := newTestModel(t)
		mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		return mdl.(*Model)
	}

	t.Run("ctrl+s opens settings while typing in chat", func(t *testing.T) {
		m := newSized(t)
		m.setChatFocus()
		mdl, _ := m.handleKey(ctrlKey('s'))
		m = mdl.(*Model)
		if _, ok := m.top().(*settingsOverlay); !ok {
			t.Fatal("ctrl+s from chat focus should open the settings overlay")
		}
	})

	t.Run("ctrl+s closes an open settings hub", func(t *testing.T) {
		m := newSized(t)
		m.openSettings()
		mdl, _ := m.handleKey(ctrlKey('s'))
		m = mdl.(*Model)
		if m.hasOverlay() {
			t.Fatal("ctrl+s should toggle the settings overlay closed")
		}
	})

	t.Run("ctrl+s does not close settings mid-edit", func(t *testing.T) {
		m := newSized(t)
		m.openSettings()
		so := m.top().(*settingsOverlay)
		so.startEdit("API key · test", true, nil)
		mdl, _ := m.handleKey(ctrlKey('s'))
		m = mdl.(*Model)
		if _, ok := m.top().(*settingsOverlay); !ok {
			t.Fatal("ctrl+s must not discard an in-progress key entry")
		}
	})

	t.Run("ctrl+q quits from chat focus", func(t *testing.T) {
		m := newSized(t)
		m.setChatFocus()
		_, cmd := m.handleKey(ctrlKey('q'))
		if cmd == nil {
			t.Fatal("ctrl+q should produce a quit command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("ctrl+q produced %T, want tea.QuitMsg", cmd())
		}
	})

	t.Run("f1 toggles help", func(t *testing.T) {
		m := newSized(t)
		m.setChatFocus()
		f1 := tea.KeyPressMsg{Code: tea.KeyF1}
		mdl, _ := m.handleKey(f1)
		m = mdl.(*Model)
		if _, ok := m.top().(*helpOverlay); !ok {
			t.Fatal("f1 should open help from chat focus")
		}
		mdl, _ = m.handleKey(f1)
		m = mdl.(*Model)
		if m.hasOverlay() {
			t.Fatal("f1 should close an open help overlay")
		}
	})
}

// TestFilterEscape verifies the filter QoL: esc while typing cancels the filter
// outright, and esc afterwards clears a kept filter before moving focus.
func TestFilterEscape(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)
	m.focus = focusMarkets

	press := func(k tea.KeyPressMsg) {
		mdl, _ := m.handleKey(k)
		m = mdl.(*Model)
	}
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}

	// Typing-mode esc cancels: no stale filter survives.
	m.filtering, m.filter = true, "BT"
	press(esc)
	if m.filtering || m.filter != "" {
		t.Fatalf("esc while filtering: filtering=%v filter=%q, want cancelled+empty", m.filtering, m.filter)
	}

	// A kept filter (enter) is cleared by the next esc, before focus moves.
	m.filtering, m.filter = true, "BT"
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.filter != "BT" {
		t.Fatalf("enter should keep the filter, got %q", m.filter)
	}
	press(esc)
	if m.filter != "" || m.focus != focusMarkets {
		t.Fatalf("first esc should clear the filter and keep markets focus, filter=%q focus=%v", m.filter, m.focus)
	}
	press(esc)
	if m.focus != focusChat {
		t.Fatal("second esc should hand focus to chat")
	}
}

// TestSelectionJumps verifies home/end/G move the watchlist cursor to the edges.
func TestSelectionJumps(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)
	m.focus = focusMarkets
	n := len(m.ordered())
	if n < 2 {
		t.Skip("test model needs at least two assets")
	}

	mdl, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	m = mdl.(*Model)
	if m.selected != n-1 {
		t.Fatalf("end: selected=%d, want %d", m.selected, n-1)
	}
	mdl, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyHome})
	m = mdl.(*Model)
	if m.selected != 0 {
		t.Fatalf("home: selected=%d, want 0", m.selected)
	}
}

// TestStatusNoteExpiry verifies a transient note clears when its own timer fires
// but survives an expiry armed by an older note.
func TestStatusNoteExpiry(t *testing.T) {
	m, _ := newTestModel(t)

	_ = m.note("first")
	firstSeq := m.statusSeq
	_ = m.note("second")

	// The stale timer from "first" must not clear "second".
	mdl, _ := m.Update(statusClearMsg{seq: firstSeq})
	m = mdl.(*Model)
	if m.statusMsg != "second" {
		t.Fatalf("stale expiry cleared a newer note: %q", m.statusMsg)
	}

	// The matching timer clears it.
	mdl, _ = m.Update(statusClearMsg{seq: m.statusSeq})
	m = mdl.(*Model)
	if m.statusMsg != "" {
		t.Fatalf("matching expiry should clear the note, got %q", m.statusMsg)
	}
}
