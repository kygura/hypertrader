package cockpit

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// ChatFunc runs an interactive completion against the daemon's /api/chat
// endpoint. Called inside a tea.Cmd so the render loop never blocks on the
// LLM.
type ChatFunc func(ctx context.Context, userMsg string, history []apiclient.ChatTurn) (reply string, err error)

// Config carries everything the cockpit needs at construction.
type Config struct {
	Cache    *apiclient.Cache
	Controls *apiclient.Client
	Settings apiclient.SettingsResponse
	ChatFn   ChatFunc
}

// Model is the cockpit root model: one screen, four panels, a chat bar.
type Model struct {
	width, height int

	cache    *apiclient.Cache
	controls *apiclient.Client
	chatFn   ChatFunc

	visualized []string
	timeframes map[string]string // coin -> display timeframe (default 1h)
	risk       apiclient.RiskSettings

	mode      string // "propose" | "autonomous"
	connected bool
	phase     string // last journal tag, shown in the header
	startedAt time.Time

	journal []journalEntry

	// Chat: bottom input bar; when open, the reply pane replaces the
	// DECISION JOURNAL panel.
	chatOpen bool
	busy     bool
	turns    []apiclient.ChatTurn
	input    textinput.Model

	spin spinner.Model
}

// New builds the cockpit model from the startup settings snapshot.
func New(cfg Config) *Model {
	ti := textinput.New()
	ti.Placeholder = "ask the agent… (/help for commands)"
	ti.Prompt = "> "

	sp := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(phaseStyle),
	)

	tf := make(map[string]string, len(cfg.Settings.Timeframes))
	for k, v := range cfg.Settings.Timeframes {
		tf[k] = v
	}

	return &Model{
		cache:      cfg.Cache,
		controls:   cfg.Controls,
		chatFn:     cfg.ChatFn,
		visualized: cfg.Settings.Visualized,
		timeframes: tf,
		risk:       cfg.Settings.Risk,
		mode:       cfg.Settings.Mode,
		phase:      "INGEST",
		startedAt:  time.Now(),
		input:      ti,
		spin:       sp,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, textinput.Blink)
}

// tf returns the display timeframe for coin (default "1h").
func (m *Model) tf(coin string) string {
	if t, ok := m.timeframes[coin]; ok && t != "" {
		return t
	}
	return "1h"
}

// envelope computes live risk utilization from the visualized watchlist's
// cached positions.
func (m *Model) envelope() envelope {
	positions := make([]apiclient.Position, 0, len(m.visualized))
	for _, coin := range m.visualized {
		positions = append(positions, m.cache.Position(coin))
	}
	return computeEnvelope(positions, m.risk)
}

// note appends an operator-side journal entry (not from the bus).
func (m *Model) note(tag, text string) {
	m.phase = tag
	m.journal = appendJournal(m.journal, journalEntry{at: time.Now(), tag: tag, text: text})
}

// timeNow is a seam for tests.
var timeNow = time.Now

func chatTurn(role, text string) apiclient.ChatTurn {
	return apiclient.ChatTurn{Role: role, Text: text}
}
