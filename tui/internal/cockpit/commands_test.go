package cockpit

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperagent/tui/internal/apiclient"
)

func TestIsCommand(t *testing.T) {
	if !isCommand("/scan") {
		t.Error("/scan should be a command")
	}
	if isCommand("what is funding?") {
		t.Error("free text is not a command")
	}
}

func TestRunCommandHelp(t *testing.T) {
	m := testModel()
	out, cmd := m.runCommand("/help")
	if cmd != nil {
		t.Error("/help should be local (nil cmd)")
	}
	for _, want := range []string{"/scan", "/watch", "/track", "/tf", "/mode", "/clear"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestRunCommandClear(t *testing.T) {
	m := testModel()
	m.turns = []apiclient.ChatTurn{{Role: "user", Text: "hi"}}
	out, _ := m.runCommand("/clear")
	if len(m.turns) != 0 {
		t.Error("/clear should empty the conversation")
	}
	if out == "" {
		t.Error("/clear should confirm")
	}
}

func TestRunCommandLoginUsage(t *testing.T) {
	m := testModel()
	out, cmd := m.runCommand("/login")
	if cmd != nil {
		t.Error("/login with no args should be local (nil cmd)")
	}
	if !strings.Contains(out, "usage") {
		t.Errorf("missing usage message: %q", out)
	}
}

func TestRunCommandLoginDispatchesExecProcess(t *testing.T) {
	m := testModel()
	out, cmd := m.runCommand("/login claude")
	if out != "" {
		t.Errorf("login should have no immediate text output, got %q", out)
	}
	if cmd == nil {
		t.Error("/login claude should return the ExecProcess tea.Cmd")
	}
}

func TestRunCommandUnknown(t *testing.T) {
	m := testModel()
	out, _ := m.runCommand("/bogus")
	if !strings.Contains(out, "unknown") {
		t.Errorf("unknown command not reported: %q", out)
	}
}

func TestSubmitFreeTextGoesToChat(t *testing.T) {
	m := testModel()
	m.chatFn = func(ctx context.Context, msg string, h []apiclient.ChatTurn) (string, error) {
		return "reply", nil
	}
	cmd := m.submit("what is funding?")
	if !m.busy {
		t.Error("free text should set busy")
	}
	if cmd == nil {
		t.Fatal("free text should produce a chat cmd")
	}
	if len(m.turns) == 0 || m.turns[len(m.turns)-1].Role != "user" {
		t.Error("user turn not recorded")
	}
}

func TestSubmitCommandRecordsSystemTurn(t *testing.T) {
	m := testModel()
	m.submit("/help")
	last := m.turns[len(m.turns)-1]
	if last.Role != "system" {
		t.Errorf("command output role = %q, want system", last.Role)
	}
}
