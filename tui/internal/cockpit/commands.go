package cockpit

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// isCommand reports whether s is a slash command.
func isCommand(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "/") }

// runCommand parses and executes a slash command against the cockpit Model.
// The string return is the immediate system output recorded as a chat turn;
// the tea.Cmd (when non-nil) carries the network half, run off the render
// loop, which reports back through journalMsg (failure) or statusMsg
// (success/notice) — the same message types the daemon push stream uses.
func (m *Model) runCommand(input string) (string, tea.Cmd) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return "", nil
	}
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	args := fields[1:]

	switch cmd {
	case "help", "?", "h":
		return commandHelp(), nil
	case "scan":
		return m.cmdScan(upperAll(args))
	case "watch":
		return m.cmdWatch(args)
	case "track":
		return m.cmdTrack(args)
	case "tf", "timeframe":
		return m.cmdTimeframe(args), nil
	case "mode":
		return m.cmdMode(args)
	case "clear":
		// Display-only reset: chat scrollback, journal ring, trigger
		// flashes, and card reasoning text. No daemon call — the daemon's
		// journal files are untouched.
		m.turns = nil
		m.journal = nil
		m.flashes = make(map[string]cardFlash)
		m.displayClearedAt = timeNow()
		return "display cleared — reasoning pane and journal scrollback reset (TUI only)", nil
	default:
		return "unknown command — /help lists commands", nil
	}
}

func commandHelp() string {
	return strings.Join([]string{
		"slash commands:",
		"  /scan [COIN…]            synthesize tracked markets now",
		"  /watch [add|rm] COIN…    manage the visualized watchlist",
		"  /track [add|rm] COIN…    manage the assets the agent reasons over",
		"  /tf TIMEFRAME COIN       set timeframe (15m,1h,4h,1d) for the named asset",
		"  /mode propose|autonomous set execution mode",
		"  /clear                   reset reasoning pane and journal scrollback (display only)",
		"  /help                    this list",
	}, "\n")
}

// cmdScan asks the daemon to synthesize the named markets (all visualized
// when empty) immediately.
func (m *Model) cmdScan(coins []string) (string, tea.Cmd) {
	label := "tracked markets"
	if len(coins) > 0 {
		label = strings.Join(coins, " ")
	}
	if m.controls == nil {
		return "scan unavailable — daemon controls not wired", nil
	}
	c := m.controls
	return "scanning " + label, func() tea.Msg {
		if err := c.Scan(context.Background(), coins...); err != nil {
			return journalMsg{Kind: "error", Summary: "scan: " + err.Error()}
		}
		return nil
	}
}

// cmdWatch manages the visualized watchlist and subscribes new coins live.
func (m *Model) cmdWatch(args []string) (string, tea.Cmd) {
	if len(args) == 0 {
		return "watchlist: " + strings.Join(m.visualized, " "), nil
	}
	op, coins := splitOp(args)
	coins = upperAll(coins)
	if len(coins) == 0 {
		return "usage: /watch [add|rm] COIN…", nil
	}
	switch op {
	case "rm", "remove", "del":
		for _, c := range coins {
			m.visualized = removeStr(m.visualized, c)
		}
		return "watchlist: " + strings.Join(m.visualized, " "), nil
	default: // add / set
		var added []string
		for _, c := range coins {
			if !containsStr(m.visualized, c) {
				m.visualized = append(m.visualized, c)
				if _, ok := m.timeframes[c]; !ok {
					m.timeframes[c] = "1h"
				}
				added = append(added, c)
			}
		}
		result := "watchlist: " + strings.Join(m.visualized, " ")
		if m.controls == nil || len(added) == 0 {
			return result, nil
		}
		c := m.controls
		return result, func() tea.Msg {
			if err := c.Subscribe(context.Background(), added...); err != nil {
				return journalMsg{Kind: "error", Summary: "watch: " + err.Error()}
			}
			return nil
		}
	}
}

// cmdTrack manages the agent's tracked set via the daemon's control-plane
// API. The cockpit Model has no local tracked-set field (dropped with the
// ideas board), so this only drives the daemon and confirms the coins acted
// on; the visualized watchlist still gains any newly tracked coin.
func (m *Model) cmdTrack(args []string) (string, tea.Cmd) {
	if len(args) == 0 {
		return "usage: /track [add|rm] COIN…", nil
	}
	op, coins := splitOp(args)
	coins = upperAll(coins)
	if len(coins) == 0 {
		return "usage: /track [add|rm] COIN…", nil
	}
	if m.controls == nil {
		return "track unavailable — daemon controls not wired", nil
	}
	c := m.controls
	switch op {
	case "rm", "remove", "del":
		return "untracking: " + strings.Join(coins, " "), func() tea.Msg {
			for _, coin := range coins {
				if err := c.Untrack(context.Background(), coin); err != nil {
					return journalMsg{Kind: "error", Summary: "untrack: " + err.Error()}
				}
			}
			return nil
		}
	default: // add / set
		toSubscribe := make([]string, 0, len(coins))
		tfs := make(map[string]string, len(coins))
		for _, coin := range coins {
			if !containsStr(m.visualized, coin) {
				m.visualized = append(m.visualized, coin)
				toSubscribe = append(toSubscribe, coin)
			}
			tf := m.timeframes[coin]
			if tf == "" {
				tf = "1h"
				m.timeframes[coin] = tf
			}
			tfs[coin] = tf
		}
		return "tracking: " + strings.Join(coins, " "), func() tea.Msg {
			if len(toSubscribe) > 0 {
				if err := c.Subscribe(context.Background(), toSubscribe...); err != nil {
					return journalMsg{Kind: "error", Summary: "track: " + err.Error()}
				}
			}
			for _, coin := range coins {
				if err := c.Track(context.Background(), coin, tfs[coin]); err != nil {
					return journalMsg{Kind: "error", Summary: "track: " + err.Error()}
				}
			}
			return nil
		}
	}
}

// cmdTimeframe sets the display timeframe for a named asset. The cockpit
// Model has no asset-selection cursor (unlike the old package), so the coin
// argument is required. This is a local display-state change only — the old
// code's conditional re-Track call depended on the dropped tracked-set map
// and is not ported.
func (m *Model) cmdTimeframe(args []string) string {
	if len(args) == 0 {
		return "usage: /tf TIMEFRAME COIN  (15m, 1h, 4h, 1d)"
	}
	tf := strings.ToLower(args[0])
	if !containsStr(tfChoices, tf) {
		return fmt.Sprintf("unknown timeframe %q — choose from %s", tf, strings.Join(tfChoices, " "))
	}
	if len(args) < 2 {
		return "usage: /tf TIMEFRAME COIN"
	}
	coin := strings.ToUpper(args[1])
	m.timeframes[coin] = tf
	return fmt.Sprintf("%s timeframe → %s", coin, tf)
}

// tfChoices are the valid timeframe values /tf accepts.
var tfChoices = []string{"15m", "1h", "4h", "1d"}

// cmdMode flips execution mode via the daemon's execution-mode endpoint.
func (m *Model) cmdMode(args []string) (string, tea.Cmd) {
	if len(args) == 0 {
		return "mode: " + m.mode + "  — usage: /mode propose|autonomous", nil
	}
	if m.controls == nil {
		return "mode switching unavailable", nil
	}
	want := strings.ToLower(args[0])
	c := m.controls
	return "", func() tea.Msg {
		if err := c.SetMode(context.Background(), want); err != nil {
			return journalMsg{Kind: "error", Summary: "mode switch failed: " + err.Error()}
		}
		return statusMsg{Kind: statusNotice, Mode: want, Detail: "mode → " + want}
	}
}

// --- small helpers (ported verbatim from tui/internal/tui/commands.go) ---

func splitOp(args []string) (op string, rest []string) {
	switch strings.ToLower(args[0]) {
	case "add", "rm", "remove", "del", "set":
		return strings.ToLower(args[0]), args[1:]
	default:
		return "add", args
	}
}

func upperAll(xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if u := strings.ToUpper(strings.TrimSpace(x)); u != "" {
			out = append(out, u)
		}
	}
	return out
}

func containsStr(xs []string, s string) bool { return slices.Contains(xs, s) }

func removeStr(xs []string, s string) []string {
	out := xs[:0:0]
	for _, x := range xs {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func argOr(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
