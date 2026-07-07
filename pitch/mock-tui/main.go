package main

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time
type journalMsg time.Time

type model struct {
	width, height int

	running   bool
	phase     string // last journal tag, shown in the header
	startedAt time.Time

	markets   []market
	positions []position
	man       mandate
	journal   []entry
	scriptIdx int

	spin spinner.Model
	prog progress.Model
}

func newModel() model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = phaseStyle

	pb := progress.New(
		progress.WithScaledGradient("#0B3B2E", "#2DE0A7"),
		progress.WithoutPercentage(),
	)

	m := model{
		running:   true,
		phase:     "INGEST",
		startedAt: time.Now().Add(-37*time.Hour - 12*time.Minute),
		markets:   initialMarkets(),
		positions: initialPositions(),
		man:       initialMandate(),
		spin:      sp,
		prog:      pb,
	}

	// Seed the journal so the panel is alive on first paint.
	now := time.Now()
	for i, e := range script[:4] {
		e.at = now.Add(time.Duration(i-4) * 41 * time.Second)
		m.journal = append(m.journal, e)
	}
	m.scriptIdx = 4
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, tick(), journalTick())
}

func tick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func journalTick() tea.Cmd {
	return tea.Tick(journalEvery, func(t time.Time) tea.Msg { return journalMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "h":
			m.running = !m.running
			if m.running {
				m.append(entry{tag: "OPERATOR", text: "resume requested — loop re-entered at ingest, gates re-armed"})
			} else {
				m.append(entry{tag: "OPERATOR", text: "halt requested — open orders cancelled, loop paused, positions held"})
			}
			return m, nil
		}

	case tickMsg:
		jitter(m.markets)
		if m.running {
			// Drift the mandate toward its target so progress is visible
			// over a short demo.
			if m.man.allocPct < m.man.allocTarget {
				m.man.allocPct += 0.02
			}
		}
		return m, tick()

	case journalMsg:
		if m.running {
			e := script[m.scriptIdx%len(script)]
			m.scriptIdx++
			m.phase = e.tag
			m.append(e)
		}
		return m, journalTick()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *model) append(e entry) {
	e.at = time.Now()
	m.journal = append(m.journal, e)
	if len(m.journal) > maxJournal {
		m.journal = m.journal[len(m.journal)-maxJournal:]
	}
}

func main() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
