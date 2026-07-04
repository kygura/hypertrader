// The channel→Msg bridge. THIS FILE IS A COMPILE-TIME STUB for Task 8: it
// defines the tea.Msg envelope types the rest of the package (chiefly
// update.go) switches on, backed by apiclient types instead of the backend's
// internal bus/metrics packages. Nothing populates these yet — Task 9
// rewrites this file into a real WebSocket client that dispatches these (or
// equivalent) messages from the daemon's /ws push stream. The render loop
// still never blocks on network or LLM once that lands.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// Sender is the minimal interface the bridge needs from a tea.Program. Kept
// here so Task 9's WS client can depend on it without touching call sites.
type Sender interface {
	Send(tea.Msg)
}

// statusKind discriminates what a statusMsg is asserting, so consumers read
// only the fields that event owns — mirrors backend/internal/bus.StatusKind's
// two values locally (bus is backend-internal and this module cannot import
// it).
type statusKind int

const (
	// statusNotice is a transient message (reasoner error, history-write
	// failure). It carries Detail and optionally Provider; it must not touch
	// connection state.
	statusNotice statusKind = iota
	// statusConn asserts the websocket connection state via Connected.
	statusConn
)

// Tea messages the render loop reacts to, mirroring the shape of
// backend/internal/bus events. Task 9's WS client produces these from real
// server push frames.
type (
	barMsg     apiclient.Bar
	verdictMsg apiclient.Verdict

	// journalMsg mirrors backend/internal/bus.JournalEvent.
	journalMsg struct {
		Coin    string
		Kind    string // "candidate" | "fill" | "open" | "close" | "alert" | "error"
		Summary string
		Verdict *apiclient.Verdict // non-nil for candidate events
	}

	// statusMsg mirrors backend/internal/bus.StatusEvent.
	statusMsg struct {
		Kind      statusKind
		Connected bool // authoritative only when Kind == statusConn
		Provider  string
		Mode      string // "propose" | "autonomous"
		Detail    string
	}

	positionMsg apiclient.Position

	chatReplyMsg struct {
		text string
		err  error
	}
)
