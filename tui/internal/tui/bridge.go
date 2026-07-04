// The channel→Msg bridge. The TUI is a bus consumer: store/journal updates
// arrive as tea.Msg via this goroutine calling p.Send(...). The render loop never
// blocks on network or LLM — if the TUI dies, the daemon trades on.
package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// Sender is the minimal interface the bridge needs from a tea.Program.
type Sender interface {
	Send(tea.Msg)
}

// Tea messages mirroring bus events.
type (
	barMsg       metrics.Bar
	verdictMsg   metrics.Verdict
	journalMsg   bus.JournalEvent
	statusMsg    bus.StatusEvent
	positionMsg  metrics.Position
	chatReplyMsg struct {
		text string
		err  error
	}
)

// PumpBus subscribes to the bus and forwards every event into the program as a
// typed tea.Msg. Runs until ctx is cancelled. Each subscription gets its own
// goroutine so a slow topic never starves another.
func PumpBus(ctx context.Context, b *bus.Bus, p Sender) {
	go pump(ctx, b.SubscribeBars(1024), p, func(v metrics.Bar) tea.Msg { return barMsg(v) })
	go pump(ctx, b.SubscribeVerdicts(256), p, func(v metrics.Verdict) tea.Msg { return verdictMsg(v) })
	go pump(ctx, b.SubscribeJournal(256), p, func(v bus.JournalEvent) tea.Msg { return journalMsg(v) })
	go pump(ctx, b.SubscribeStatus(64), p, func(v bus.StatusEvent) tea.Msg { return statusMsg(v) })
	go pump(ctx, b.SubscribePositions(64), p, func(v metrics.Position) tea.Msg { return positionMsg(v) })
}

func pump[T any](ctx context.Context, ch <-chan T, p Sender, wrap func(T) tea.Msg) {
	for {
		select {
		case <-ctx.Done():
			return
		case v, ok := <-ch:
			if !ok {
				return
			}
			p.Send(wrap(v))
		}
	}
}
