package tui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// isCommand reports whether a chat input is a slash command.
func isCommand(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "/")
}

// runCommand parses and executes a slash command, mutating model state and
// driving the daemon through Controls. It returns the text to echo into the
// chat pane and, for commands that kick off async work (thesis generation,
// opening settings), a tea.Cmd. All configuration flows the daemon exposes are
// reachable here, so the running session can be reconfigured from the chat
// input without leaving that pane.
// scanNow asks the daemon to synthesize the named markets (all tracked when
// empty) immediately and brings the IDEAS board into view so the ranked
// candidates land where the operator is looking.
func (m *Model) scanNow(coins []string) tea.Cmd {
	if m.controls == nil {
		return m.note("scan unavailable — daemon controls not wired")
	}
	if err := m.controls.Scan(context.Background(), coins...); err != nil {
		return m.note("scan: " + err.Error())
	}
	m.chatTab = chatTabIdeas
	label := "tracked markets"
	if len(coins) > 0 {
		label = strings.Join(coins, " ")
	}
	return m.note("scanning " + label + " — candidates land on IDEAS")
}

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

	// One-key shortcut aliases — these let every global key work from chat focus.
	case "s", "settings":
		return "", m.openSettings()
	case "keys", "apikeys":
		return "", m.openAPIKeys()
	case "g", "analyze", "thesis":
		coin := m.selectedCoin()
		if coin == "" {
			return "no asset selected", nil
		}
		return fmt.Sprintf("generating thesis for %s…", coin), m.generateThesis()

	case "scan":
		return "", m.scanNow(upperAll(args))

	case "watch":
		return m.cmdWatch(args), nil
	case "track":
		return m.cmdTrack(args), nil
	case "tf", "timeframe":
		return m.cmdTimeframe(args), nil
	case "provider":
		return m.cmdProvider(args), nil
	case "model":
		return m.cmdModel(args), nil
	case "mode":
		return m.cmdMode(args), nil
	case "clear":
		m.chat.turns = nil
		return "", nil
	default:
		return fmt.Sprintf("unknown command /%s — try /help", cmd), nil
	}
}

func commandHelp() string {
	return strings.Join([]string{
		"slash commands (also usable from chat focus):",
		"  /scan [COIN…]            synthesize tracked markets now → IDEAS board",
		"  /watch [add|rm] COIN…    manage the visualized watchlist",
		"  /track [add|rm] COIN…    manage the assets the agent reasons over",
		"  /tf TIMEFRAME [COIN]     set timeframe (15m,1h,4h,1d) for selected/named asset",
		"  /model [batch|chat] ID   switch the model id for a role (free-form)",
		"  /mode propose|autonomous set execution mode",
		"  /keys                    open the API-key settings tab",
		"  /clear                   clear the chat scrollback",
		"  /help                    this list",
		"",
		"one-key aliases (type these as /s, /g):",
		"  /s  settings (models · API keys · trading · markets)",
		"  /g  generate thesis for selected asset",
	}, "\n")
}

// cmdWatch manages the visualized watchlist and subscribes new coins live.
func (m *Model) cmdWatch(args []string) string {
	if len(args) == 0 {
		return "watchlist: " + strings.Join(m.visualized, " ")
	}
	op, coins := splitOp(args)
	coins = upperAll(coins)
	if len(coins) == 0 {
		return "usage: /watch [add|rm] COIN…"
	}
	switch op {
	case "rm", "remove", "del":
		for _, c := range coins {
			m.visualized = removeStr(m.visualized, c)
		}
		if m.selected >= len(m.visualized) {
			m.selected = max(0, len(m.visualized)-1)
		}
		return "watchlist: " + strings.Join(m.visualized, " ")
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
		if m.controls != nil && len(added) > 0 {
			_ = m.controls.Subscribe(context.Background(), added...)
		}
		return "watchlist: " + strings.Join(m.visualized, " ")
	}
}

// cmdTrack manages the agent's tracked set via the daemon's control-plane API.
func (m *Model) cmdTrack(args []string) string {
	if len(args) == 0 {
		return "tracked: " + strings.Join(trackedList(m.tracked), " ")
	}
	op, coins := splitOp(args)
	coins = upperAll(coins)
	if len(coins) == 0 {
		return "usage: /track [add|rm] COIN…"
	}
	switch op {
	case "rm", "remove", "del":
		for _, c := range coins {
			delete(m.tracked, c)
			if m.controls != nil {
				_ = m.controls.Untrack(context.Background(), c)
			}
		}
	default: // add / set
		for _, c := range coins {
			if !containsStr(m.visualized, c) {
				m.visualized = append(m.visualized, c)
				if m.controls != nil {
					_ = m.controls.Subscribe(context.Background(), c)
				}
			}
			tf := m.timeframes[c]
			if tf == "" {
				tf = "1h"
				m.timeframes[c] = tf
			}
			m.tracked[c] = true
			if m.controls != nil {
				_ = m.controls.Track(context.Background(), c, tf)
			}
		}
	}
	return "tracked: " + strings.Join(trackedList(m.tracked), " ")
}

// cmdTimeframe sets the display timeframe for the selected (or named) asset.
func (m *Model) cmdTimeframe(args []string) string {
	if len(args) == 0 {
		return "usage: /tf TIMEFRAME [COIN]  (15m, 1h, 4h, 1d)"
	}
	tf := strings.ToLower(args[0])
	if !containsStr(m.tfCycle, tf) {
		return fmt.Sprintf("unknown timeframe %q — choose from %s", tf, strings.Join(m.tfCycle, " "))
	}
	coin := m.selectedCoin()
	if len(args) > 1 {
		coin = strings.ToUpper(args[1])
	}
	if coin == "" {
		return "no asset selected"
	}
	m.timeframes[coin] = tf
	if m.tracked[coin] && m.controls != nil {
		_ = m.controls.Track(context.Background(), coin, tf)
	}
	return fmt.Sprintf("%s timeframe → %s", coin, tf)
}

// cmdProvider switches the model provider for a role (batch or chat, or both).
func (m *Model) cmdProvider(args []string) string {
	names := m.settings.ProviderNames
	if len(args) == 0 {
		return "providers: " + strings.Join(names, " ") + "  — usage: /provider [batch|chat] NAME"
	}
	if m.controls == nil {
		return "provider switching unavailable"
	}
	var name string
	roles := []Role{RoleBatch, RoleChat}
	switch strings.ToLower(args[0]) {
	case "batch":
		roles = []Role{RoleBatch}
		name = argOr(args, 1)
	case "chat":
		roles = []Role{RoleChat}
		name = argOr(args, 1)
	default:
		name = args[0]
	}
	if name == "" {
		return "usage: /provider [batch|chat] NAME  (have: " + strings.Join(names, " ") + ")"
	}
	var chatProv, batchProv string
	for _, r := range roles {
		switch r {
		case RoleChat:
			chatProv = name
		case RoleBatch:
			batchProv = name
		}
	}
	if err := m.controls.SaveSettings(context.Background(), chatProv, "", batchProv, ""); err != nil {
		return "provider: " + err.Error()
	}
	if chatProv != "" {
		m.settings.Chat.Provider = chatProv
		m.provider = chatProv
	}
	if batchProv != "" {
		m.settings.Batch.Provider = batchProv
	}
	return fmt.Sprintf("provider for %s → %s", roleNames(roles), name)
}

// cmdModel switches the MODEL for a role on its current provider — distinct from
// /provider, which swaps the transport. Model ids are free-form: any string the
// provider's API accepts works, so a base-URL-swappable endpoint exposing many
// models is fully addressable. This is the capability the old /model=/provider
// alias lacked.
func (m *Model) cmdModel(args []string) string {
	if m.controls == nil {
		return "model switching unavailable"
	}
	role, label := RoleChat, "chat"
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "batch":
			role, label, args = RoleBatch, "batch", args[1:]
		case "chat":
			role, label, args = RoleChat, "chat", args[1:]
		}
	}
	if len(args) == 0 {
		prov, model := m.activeModel(role)
		avail := ""
		if ms := m.settings.ProviderModels[prov]; len(ms) > 0 {
			avail = "\n  " + prov + ": " + strings.Join(ms, "  ")
		}
		return fmt.Sprintf("%s model: %s · %s\nusage: /model [batch|chat] MODEL-ID  (free-form)%s", label, prov, model, avail)
	}
	var err error
	switch role {
	case RoleChat:
		err = m.controls.SaveSettings(context.Background(), "", args[0], "", "")
	case RoleBatch:
		err = m.controls.SaveSettings(context.Background(), "", "", "", args[0])
	}
	if err != nil {
		return "model: " + err.Error()
	}
	switch role {
	case RoleChat:
		m.settings.Chat.Model = args[0]
	case RoleBatch:
		m.settings.Batch.Model = args[0]
	}
	return fmt.Sprintf("%s model → %s", label, args[0])
}

// cmdMode flips execution mode via the daemon's execution-mode endpoint.
func (m *Model) cmdMode(args []string) string {
	if len(args) == 0 {
		return "mode: " + m.mode + "  — usage: /mode propose|autonomous"
	}
	want := strings.ToLower(args[0])
	if m.controls == nil {
		return "mode switching unavailable"
	}
	if err := m.controls.SetMode(context.Background(), want); err != nil {
		return "mode: " + err.Error()
	}
	m.mode = want
	return "execution mode → " + want
}

// --- small helpers ---

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

func trackedList(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for c := range m {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func argOr(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func roleNames(rs []Role) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = string(r)
	}
	return strings.Join(parts, "+")
}
