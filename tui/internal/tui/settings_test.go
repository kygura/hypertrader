package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

// drive sends a key through the full Update path (stack routing included).
func drive(m *Model, keys ...string) {
	for _, k := range keys {
		mdl, _ := m.Update(keyPress(k))
		*m = *(mdl.(*Model))
	}
}

// TestSettingsOpensAndTabs: s opens the hub; tab cycles all four tabs and wraps.
func TestSettingsOpensAndTabs(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)

	drive(m, "s")
	so, ok := m.top().(*settingsOverlay)
	if !ok {
		t.Fatalf("s should push settings, got %T", m.top())
	}
	want := []settingsTab{tabKeys, tabTrading, tabMarkets, tabModels}
	for _, w := range want {
		drive(m, "tab")
		if so.tab != w {
			t.Fatalf("tab cycle: got %v want %v", so.tab, w)
		}
	}
	drive(m, "esc")
	if m.hasOverlay() {
		t.Fatal("esc should close settings")
	}
}

// TestSettingsAPIKeyFlow drives the API Keys tab end-to-end: enter starts the
// masked editor, typing + enter calls SetProviderKey with the provider and the
// key via the daemon's PUT /api/providers/{name}/key endpoint.
func TestSettingsAPIKeyFlow(t *testing.T) {
	m, rec := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)

	m.openAPIKeys()
	so := m.top().(*settingsOverlay)
	drive(m, "enter") // start editing the first provider row (anthropic, sorted)
	if !so.editing {
		t.Fatal("enter should open the key editor")
	}
	if so.input.EchoMode == 0 { // textinput.EchoNormal == 0
		t.Fatal("key editor must mask input")
	}
	drive(m, "s", "k", "-", "1") // type a key
	drive(m, "enter")            // submit
	if rec.keyProvider != "anthropic" || rec.keyValue != "sk-1" {
		t.Fatalf("SetProviderKey got (%q,%q), want (anthropic, sk-1)", rec.keyProvider, rec.keyValue)
	}
	if !strings.Contains(so.status, "✓") {
		t.Fatalf("status should confirm, got %q", so.status)
	}
}

// TestSettingsModelPickerPersists: choosing a model in the picker applies it
// live and persists it through the daemon's PUT /api/settings endpoint.
func TestSettingsModelPickerPersists(t *testing.T) {
	m, rec := newTestModel(t)
	m.settings.ProviderModels = map[string][]string{"anthropic": {"claude-opus-4-8", "claude-sonnet-4-6"}}
	m.settings.Chat = apiclient.RoleSettings{Provider: "anthropic", Model: "claude-opus-4-8"}
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)

	m.pushModelPicker(RoleChat)
	drive(m, "down", "enter") // pick the second model
	if len(rec.saves) == 0 {
		t.Fatal("model selection must persist via SaveSettings")
	}
	last := rec.saves[len(rec.saves)-1]
	if last.ChatModel != "claude-sonnet-4-6" {
		t.Fatalf("persisted chat model %q, want claude-sonnet-4-6", last.ChatModel)
	}
	if m.hasOverlay() {
		t.Fatal("picker should close after selection")
	}
}

// TestModePickerPersists: mode change applies live via the daemon's
// PUT /api/execution/mode endpoint.
func TestModePickerPersists(t *testing.T) {
	m, rec := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)

	m.pushModePicker()
	drive(m, "down", "enter") // propose -> autonomous
	if rec.mode != "autonomous" || m.mode != "autonomous" {
		t.Fatalf("mode not applied live: rec=%q m=%q", rec.mode, m.mode)
	}
}

// TestOverlayStackRouting: a picker opened from settings pops back to settings,
// not to the dashboard — the stack discipline the old enum design lacked.
func TestOverlayStackRouting(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)

	drive(m, "s")     // settings
	drive(m, "enter") // open chat-model picker from row 0
	if _, ok := m.top().(*listOverlay); !ok {
		t.Fatalf("enter on Models row should push a picker, got %T", m.top())
	}
	drive(m, "esc")
	if _, ok := m.top().(*settingsOverlay); !ok {
		t.Fatalf("esc should return to settings, got %T", m.top())
	}
	drive(m, "esc")
	if m.hasOverlay() {
		t.Fatal("second esc should close the stack")
	}
}

// TestHelpOverlayPages: ? opens the tutorial, pages flip and stay in bounds.
func TestHelpOverlayPages(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	m = mdl.(*Model)

	drive(m, "?")
	ho, ok := m.top().(*helpOverlay)
	if !ok {
		t.Fatalf("? should push help, got %T", m.top())
	}
	n := len(ho.helpPages(m))
	for i := 1; i <= n; i++ {
		drive(m, "l")
		if ho.page != i%n {
			t.Fatalf("page after %d flips = %d, want %d", i, ho.page, i%n)
		}
		v := m.View() // every page must render within the terminal
		if w := len(strings.Split(v.Content, "\n")); w > 35 {
			t.Fatalf("help page %d taller than terminal: %d", ho.page, w)
		}
	}
	drive(m, "esc")
	if m.hasOverlay() {
		t.Fatal("esc should close help")
	}
}
