// The Bubble Tea model: the Elm-style Model/Update/View that is the product. It
// composes three panes (markets · detail · chat) using lipgloss.JoinHorizontal /
// JoinVertical, sizes them to the detected terminal, and reads everything from
// the cache (live) plus bridge events (push updates). Floating panels (settings,
// pickers, help) live on a single overlay stack — see overlays.go.
package tui

import (
	"context"
	"maps"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// focus identifies which pane has keyboard focus.
type focus int

const (
	focusMarkets focus = iota
	focusDetail
	focusChat
)

// Role selects which reasoner role (chat vs batch) a model/provider setting
// applies to — mirrors backend/internal/reasoner.Role's two values locally,
// since that package is backend-internal and this module cannot import it.
type Role string

const (
	RoleBatch Role = "batch"
	RoleChat  Role = "chat"
)

// ChatFunc runs an interactive completion against the daemon's /api/chat
// endpoint. The model calls it in a tea.Cmd so the render loop never blocks on
// the LLM. Grounding context is built server-side (reasoner.BuildChatContext,
// run by the daemon), so unlike the in-process TUI this no longer takes a
// separate contextText argument — callers fold any extra grounding straight
// into userMsg when they have it (see generateThesis/thesisContextMsg).
type ChatFunc func(ctx context.Context, userMsg string, history []apiclient.ChatTurn) (reply string, err error)

// ThesisFn fetches live multi-TF Hyperliquid perp data for coin and returns a
// compact grounding block for the thesis LLM prompt.
type ThesisFn func(ctx context.Context, coin, displayTF string) (string, error)

// chatTab identifies which sub-tab of the bottom pane is active, in display
// order: the conversation, the ranked-candidates board, the execution feed.
const (
	chatTabAgent = 0
	chatTabIdeas = 1
	chatTabLive  = 2
	chatTabCount = 3
)

// liveEntry is one entry in the Live feed: a journal event with optional
// verdict details for fills / candidates.
type liveEntry struct {
	at      time.Time
	coin    string
	kind    string // "fill" | "candidate" | "alert" | "error"
	summary string
	verdict *apiclient.Verdict
}

// Model is the root TUI model.
type Model struct {
	theme    Theme
	cache    *apiclient.Cache
	chatFn   ChatFunc
	controls *apiclient.Client
	risk     RiskView

	// settings is the last-fetched full settings snapshot (provider/model
	// lists, key hints, risk, mode) — seeded from Config.Settings at startup
	// and refreshed whenever the settings modal opens (see openSettings).
	settings apiclient.SettingsResponse

	width, height int
	lay           layout // resolved responsive geometry (recomputed on resize)
	focus         focus

	// Floating panels: a stack — the top owns the keyboard, esc pops one level.
	overlays    []overlay
	detailModal bool          // floating detail view (enter on a market in narrow layouts)
	spinner     spinner.Model // chat "thinking" indicator

	// Watchlist.
	visualized []string
	tracked    map[string]bool
	timeframes map[string]string // coin -> current display timeframe
	tfCycle    []string          // timeframes selectable with 't'
	selected   int               // index into the ordered (filtered+sorted) watchlist
	filter     string
	filtering  bool
	sortKey    sortKey // markets table ordering

	// Detail / chat.
	thesis       map[string]string // coin -> user-triggered thesis (chat provider)
	reading      map[string]string // coin -> batch-model OI/funding reading
	postedThesis map[string]string // coin -> last thesis posted to the feed (dedup)
	chat         chatState
	chatTab      int // chatTabAgent, chatTabIdeas, or chatTabLive
	input        textinput.Model
	chatVP       viewport.Model
	liveVP       viewport.Model // viewport for the Live tab feed
	liveEntries  []liveEntry    // accumulated live feed entries
	ideasVP      viewport.Model // viewport for the Ideas board
	candidates   []candidate    // ranked trade candidates (one per asset)
	ideasSel     int            // board cursor (enter jumps to the asset)
	detailVP         viewport.Model // scrolls detail content when its pane shrinks
	detailSection    int            // active section cursor (0=signals, 1=thesis, 2=context)
	chatHeightOffset int            // rows added/removed from base chat height via ctrl+↑↓
	md               mdState        // glamour renderers + cache for model-output markdown
	thesisFn         ThesisFn       // fetches live HL multi-TF context for /g

	// Chat input history (shell-style up/down recall in the chat pane).
	inputHistory  []string
	historyCursor int    // -1 = not navigating; >= 0 = index into inputHistory
	historyDraft  string // saved draft before history navigation began

	// Status.
	connected bool
	provider  string
	mode      string
	statusMsg string
	statusSeq int // generation counter pairing each transient note with its expiry timer
}

type chatState struct {
	turns []apiclient.ChatTurn
	busy  bool
}

// Chat turn roles. user/assistant are the conversation proper and are sent to the
// LLM; system (command output) is rendered locally and never sent; thesis (the
// proactive batch feed) is rendered specially and sent to the LLM as an assistant
// turn so the agent remembers what it volunteered.
const (
	roleUser   = "user"
	roleAgent  = "assistant"
	roleSystem = "system"
	roleThesis = "thesis"
)

// Config carries everything the model needs at construction.
type Config struct {
	Theme    Theme
	Cache    *apiclient.Cache
	Controls *apiclient.Client
	Settings apiclient.SettingsResponse // seeds Visualized/Tracked/Timeframes/Mode/Chat.Provider/Risk
	ChatFn   ChatFunc
	ThesisFn ThesisFn
}

// New builds the root model.
func New(cfg Config) *Model {
	ti := textinput.New()
	ti.Placeholder = "ask the agent… (/help for commands)"
	ti.Prompt = "> "

	// Native ghost-text autocomplete for slash commands. Strip up/down from
	// NextSuggestion/PrevSuggestion so they don't fight global market navigation.
	ti.ShowSuggestions = true
	ti.SetSuggestions(chatCommandList)
	ti.KeyMap.NextSuggestion = key.NewBinding(key.WithKeys("ctrl+n"))
	ti.KeyMap.PrevSuggestion = key.NewBinding(key.WithKeys("ctrl+p"))
	// Style the ghost-text as dim so it reads clearly as a suggestion, not typed text.
	styles := textinput.DefaultStyles(cfg.Theme.HasDarkBG)
	styles.Focused.Suggestion = lipgloss.NewStyle().Faint(true)
	styles.Blurred.Suggestion = lipgloss.NewStyle().Faint(true)
	ti.SetStyles(styles)

	tracked := make(map[string]bool, len(cfg.Settings.Tracked))
	for _, c := range cfg.Settings.Tracked {
		tracked[c] = true
	}
	tf := make(map[string]string, len(cfg.Settings.Timeframes))
	maps.Copy(tf, cfg.Settings.Timeframes)
	for _, c := range cfg.Settings.Visualized {
		if _, ok := tf[c]; !ok {
			tf[c] = "1h"
		}
	}

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(cfg.Theme.Accent)),
	)

	m := &Model{
		theme:         cfg.Theme,
		cache:         cfg.Cache,
		chatFn:        cfg.ChatFn,
		thesisFn:      cfg.ThesisFn,
		controls:      cfg.Controls,
		settings:      cfg.Settings,
		risk:          riskViewFrom(cfg.Settings.Risk),
		visualized:    cfg.Settings.Visualized,
		tracked:       tracked,
		timeframes:    tf,
		tfCycle:       []string{"15m", "1h", "4h", "1d"},
		thesis:        make(map[string]string),
		reading:       make(map[string]string),
		postedThesis:  make(map[string]string),
		input:         ti,
		historyCursor: -1,
		chatVP:        viewport.New(),
		liveVP:        viewport.New(),
		ideasVP:       viewport.New(),
		detailVP:      viewport.New(),
		spinner:       sp,
		mode:          cfg.Settings.Mode,
		provider:      cfg.Settings.Chat.Provider,
		focus:         focusMarkets, // markets-first: selection drives the detail pane
	}
	m.seedWelcome()
	return m
}

// seedWelcome posts the first-run orientation into the chat pane — the inline
// half of the tutorial (? opens the full paged version).
func (m *Model) seedWelcome() {
	m.chat.turns = append(m.chat.turns, apiclient.ChatTurn{Role: roleSystem, Text: strings.Join([]string{
		"welcome to hyperagent — a Hyperliquid marketwatch with an LLM reasoner",
		"↑↓ pick a market · enter detail · tab move focus",
		"S scan now — the agent ranks the tracked markets on the IDEAS tab ([ ] to flip)",
		"ctrl+s settings (models · API keys · trading) · ? tutorial · ctrl+q quit",
		"type below to ask the agent, or /help for commands",
	}, "\n")})
	m.refreshChat()
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return textinput.Blink }

// filtered returns the watchlist after applying the active filter.
func (m *Model) filtered() []string {
	if m.filter == "" {
		return m.visualized
	}
	var out []string
	f := strings.ToUpper(m.filter)
	for _, c := range m.visualized {
		if strings.Contains(strings.ToUpper(c), f) {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) selectedCoin() string {
	fl := m.ordered()
	if len(fl) == 0 {
		return ""
	}
	if m.selected >= len(fl) {
		m.selected = len(fl) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	return fl[m.selected]
}

// activeModel returns the (provider, model) bound to a role for display, read
// from the last-fetched settings snapshot.
func (m *Model) activeModel(role Role) (provider, model string) {
	switch role {
	case RoleChat:
		return m.settings.Chat.Provider, m.settings.Chat.Model
	case RoleBatch:
		return m.settings.Batch.Provider, m.settings.Batch.Model
	}
	return "", ""
}

// chatModelDisplay returns the chat role's provider and model for the status line,
// keeping view.go free of a direct settings-role dependency.
func (m *Model) chatModelDisplay() (provider, model string) {
	return m.activeModel(RoleChat)
}
