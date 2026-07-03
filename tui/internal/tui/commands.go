package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// isCommand reports whether a chat input is a slash command.
func isCommand(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "/")
}

// runCommand parses and executes a slash command, mutating model state and
// driving the backend Controls. It returns the text to echo into the chat pane
// and, for commands that kick off async work (thesis generation), a tea.Cmd.
// All configuration flows the daemon exposes are reachable here, so the running
// session can be reconfigured from the chat input without leaving that pane.
// scanNow asks the daemon to synthesize the named markets (all tracked when
// empty) immediately and brings the IDEAS board into view so the ranked
// candidates land where the operator is looking.
func (m *Model) scanNow(coins []string) tea.Cmd {
	if m.controls.ScanNow == nil {
		return m.note("scan unavailable — daemon controls not wired")
	}
	m.controls.ScanNow(coins...)
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
		m.openSettings()
		return "", nil
	case "keys", "apikeys":
		m.openAPIKeys()
		return "", nil
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
		m.saveWatchlist()
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
		if m.controls.Subscribe != nil && len(added) > 0 {
			m.controls.Subscribe(added...)
		}
		m.saveWatchlist()
		return "watchlist: " + strings.Join(m.visualized, " ")
	}
}

// cmdTrack manages the agent's tracked set via the batcher controls.
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
			if m.controls.Untrack != nil {
				m.controls.Untrack(c)
			}
		}
	default: // add / set
		for _, c := range coins {
			if !containsStr(m.visualized, c) {
				m.visualized = append(m.visualized, c)
				if m.controls.Subscribe != nil {
					m.controls.Subscribe(c)
				}
			}
			tf := m.timeframes[c]
			if tf == "" {
				tf = "1h"
				m.timeframes[c] = tf
			}
			m.tracked[c] = true
			if m.controls.Track != nil {
				m.controls.Track(c, tf)
			}
		}
	}
	m.saveWatchlist()
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
	if m.tracked[coin] && m.controls.Track != nil {
		m.controls.Track(coin, tf)
	}
	m.saveWatchlist()
	return fmt.Sprintf("%s timeframe → %s", coin, tf)
}

// cmdProvider switches the model provider for a role (batch or chat, or both).
func (m *Model) cmdProvider(args []string) string {
	names := []string{}
	if m.controls.ProviderNames != nil {
		names = m.controls.ProviderNames()
	}
	if len(args) == 0 {
		return "providers: " + strings.Join(names, " ") + "  — usage: /provider [batch|chat] NAME"
	}
	if m.controls.SetProvider == nil {
		return "provider switching unavailable"
	}
	var name string
	roles := []reasoner.Role{reasoner.RoleBatch, reasoner.RoleChat}
	switch strings.ToLower(args[0]) {
	case "batch":
		roles = []reasoner.Role{reasoner.RoleBatch}
		name = argOr(args, 1)
	case "chat":
		roles = []reasoner.Role{reasoner.RoleChat}
		name = argOr(args, 1)
	default:
		name = args[0]
	}
	if name == "" {
		return "usage: /provider [batch|chat] NAME  (have: " + strings.Join(names, " ") + ")"
	}
	for _, r := range roles {
		if err := m.controls.SetProvider(r, name); err != nil {
			return "provider: " + err.Error()
		}
	}
	if containsRole(roles, reasoner.RoleChat) {
		m.provider = name
	}
	m.saveSettings()
	return fmt.Sprintf("provider for %s → %s", roleNames(roles), name)
}

// cmdModel switches the MODEL for a role on its current provider — distinct from
// /provider, which swaps the transport. Model ids are free-form: any string the
// provider's API accepts works, so a base-URL-swappable endpoint exposing many
// models is fully addressable. This is the capability the old /model=/provider
// alias lacked.
func (m *Model) cmdModel(args []string) string {
	if m.controls.SetModel == nil {
		return "model switching unavailable"
	}
	role, label := reasoner.RoleChat, "chat"
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "batch":
			role, label, args = reasoner.RoleBatch, "batch", args[1:]
		case "chat":
			role, label, args = reasoner.RoleChat, "chat", args[1:]
		}
	}
	if len(args) == 0 {
		prov, model := m.activeModel(role)
		avail := ""
		if m.controls.ProviderModels != nil {
			if ms := m.controls.ProviderModels()[prov]; len(ms) > 0 {
				avail = "\n  " + prov + ": " + strings.Join(ms, "  ")
			}
		}
		return fmt.Sprintf("%s model: %s · %s\nusage: /model [batch|chat] MODEL-ID  (free-form)%s", label, prov, model, avail)
	}
	if err := m.controls.SetModel(role, args[0]); err != nil {
		return "model: " + err.Error()
	}
	m.saveSettings()
	return fmt.Sprintf("%s model → %s", label, args[0])
}

// cmdMode flips execution mode via the executor controls.
func (m *Model) cmdMode(args []string) string {
	if len(args) == 0 {
		return "mode: " + m.mode + "  — usage: /mode propose|autonomous"
	}
	want := strings.ToLower(args[0])
	if m.controls.SetMode == nil {
		return "mode switching unavailable"
	}
	if err := m.controls.SetMode(want); err != nil {
		return "mode: " + err.Error()
	}
	m.mode = want
	m.saveSettings()
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

func containsRole(rs []reasoner.Role, r reasoner.Role) bool { return slices.Contains(rs, r) }

func roleNames(rs []reasoner.Role) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = string(r)
	}
	return strings.Join(parts, "+")
}

// watchlistSnapshot is the on-disk format for watchlist persistence. Model and
// mode selections are NOT here — they persist to config.toml via SaveSettings,
// so each file owns one concern.
type watchlistSnapshot struct {
	Visualized []string          `json:"visualized"`
	Tracked    []string          `json:"tracked"`
	Timeframes map[string]string `json:"timeframes"`
}

// saveWatchlist writes the current watchlist state to disk. Failures are
// silently swallowed — the watchlist is runtime state and a save failure should
// never crash or block the UI.
func (m *Model) saveWatchlist() {
	if m.watchlistPath == "" {
		return
	}
	snap := watchlistSnapshot{
		Visualized: append([]string(nil), m.visualized...),
		Tracked:    trackedList(m.tracked),
		Timeframes: make(map[string]string, len(m.timeframes)),
	}
	maps.Copy(snap.Timeframes, m.timeframes)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.watchlistPath, data, 0o644)
}

// loadWatchlist reads a previously saved watchlist from disk and merges it
// over the model's current state. Missing file is not an error.
func (m *Model) loadWatchlist() {
	if m.watchlistPath == "" {
		return
	}
	data, err := os.ReadFile(m.watchlistPath)
	if err != nil {
		return // file not found on first run is expected
	}
	var snap watchlistSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	if len(snap.Visualized) > 0 {
		m.visualized = snap.Visualized
	}
	for _, c := range snap.Tracked {
		m.tracked[c] = true
	}
	maps.Copy(m.timeframes, snap.Timeframes)
}
