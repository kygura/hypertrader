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
	// statusConn asserts a websocket connection state via Connected. Two
	// sources feed it: the daemon's own forwarded frames (its link to the
	// exchange) and PumpWS's synthesized events (the TUI's own link to the
	// daemon) — see PumpWS's doc comment. The header's connection chip
	// reflects whichever arrived most recently.
	statusConn
)

// Tea messages the render loop reacts to, mirroring the shape of
// backend/internal/bus events. PumpWS produces these from real server push
// frames.
type (
	barMsg     apiclient.Bar
	verdictMsg apiclient.Verdict

	// thesisMsg signals a thesis change already applied to the cache by the
	// bridge (WS "thesis" frame, or a zero value after an on-connect
	// snapshot) — the render loop repaints the THESES cards from the cache.
	thesisMsg apiclient.Thesis

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
