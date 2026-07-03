package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// TestRenderMarkdown verifies glamour output: markdown syntax is consumed (no
// raw ** markers survive), the text content does, lines respect the wrap width,
// and a repeat render is served from the cache.
func TestRenderMarkdown(t *testing.T) {
	m, _ := newTestModel(t)
	const src = "## Thesis\n\nBTC is **coiled**:\n\n- funding flat\n- OI rising\n\n`long > 95k`"

	out := m.renderMarkdown(src, 40)
	// Compare on the ANSI-stripped text: glamour styles each word as its own
	// escape segment, so substrings can't be matched on the raw output.
	plain := ansi.Strip(out)
	if strings.Contains(plain, "**") {
		t.Errorf("raw emphasis markers survived rendering:\n%s", plain)
	}
	for _, want := range []string{"Thesis", "coiled", "funding flat", "long > 95k"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rendered markdown lost %q:\n%s", want, plain)
		}
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line %d is %d cells, want ≤ 40", i, w)
		}
	}

	if len(m.md.cache) == 0 {
		t.Fatal("render should populate the cache")
	}
	if again := m.renderMarkdown(src, 40); again != out {
		t.Error("cached render differs from the original")
	}
}

// TestRenderMarkdownFallback verifies degenerate inputs come back as-is rather
// than erroring: tiny widths and empty text.
func TestRenderMarkdownFallback(t *testing.T) {
	m, _ := newTestModel(t)
	if got := m.renderMarkdown("plain", 4); got != "plain" {
		t.Errorf("narrow width: got %q, want raw text", got)
	}
	if got := m.renderMarkdown("  \n ", 40); got != "" {
		t.Errorf("blank input: got %q, want empty", got)
	}
}

// TestChatRendersAssistantMarkdown pushes a markdown reply through the real
// update path and asserts the transcript shows styled content, not raw syntax.
func TestChatRendersAssistantMarkdown(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)

	mdl, _ = m.Update(chatReplyMsg{text: "**bold call** on `BTC`"})
	m = mdl.(*Model)

	content := m.chatVP.View()
	_ = content // viewport may window the welcome text; assert on the turns render instead
	m.refreshChat()
	full := strings.Join([]string{m.chatVP.View()}, "\n")
	if strings.Contains(full, "**bold call**") {
		t.Errorf("assistant turn shows raw markdown:\n%s", full)
	}

	// The model view must still render within bounds with markdown in the feed.
	v := m.View()
	if gw, gh := lipgloss.Width(v.Content), lipgloss.Height(v.Content); gw > 120 || gh > 40 {
		t.Errorf("frame %dx%d exceeds 120x40 with markdown content", gw, gh)
	}
}

// TestThesisFeedRendersMarkdownBody verifies the proactive thesis turn renders
// its body through glamour beneath the ◆ headline.
func TestThesisFeedRendersMarkdownBody(t *testing.T) {
	m, _ := newTestModel(t)
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mdl.(*Model)

	m.chat.turns = append(m.chat.turns, reasoner.ChatTurn{
		Role: roleThesis,
		Text: "12:00 · BTC 1h · LONG\nfunding is *negative* while OI climbs",
	})
	m.refreshChat()
	if got := m.chatVP.View(); strings.Contains(got, "*negative*") {
		t.Errorf("thesis body shows raw markdown:\n%s", got)
	}
}
