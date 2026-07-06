package cockpit

import (
	"github.com/hyperagent/tui/internal/apiclient"
)

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
// backend/internal/bus events. PumpWS produces these from real server push
// frames.
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
