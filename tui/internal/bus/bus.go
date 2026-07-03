// Package bus is the internal event bus: a typed, fan-out publish/subscribe hub
// that every component communicates over. The plan's "well-designed API surface"
// is exactly this — attach a new consumer (backtester, TUI, webhook) by
// subscribing; you never edit core logic.
//
// Design notes:
//   - Each event kind has its own typed channel topic. Generics give us one
//     implementation reused per type with no interface{} casting at call sites.
//   - Publishing is non-blocking per subscriber: a slow consumer drops the
//     oldest message rather than stalling the producer. This is the "native
//     channel backpressure" the plan wants — the render loop or LLM never blocks
//     the ingest hot path.
package bus

import (
	"sync"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// topic is a generic fan-out channel hub for one event type.
type topic[T any] struct {
	mu   sync.RWMutex
	subs []chan T
}

func (t *topic[T]) subscribe(buffer int) <-chan T {
	ch := make(chan T, buffer)
	t.mu.Lock()
	t.subs = append(t.subs, ch)
	t.mu.Unlock()
	return ch
}

// publish delivers to every subscriber without blocking. If a subscriber's
// buffer is full, the oldest queued message is discarded to make room — newer
// market data is always more valuable than stale.
func (t *topic[T]) publish(v T) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, ch := range t.subs {
		select {
		case ch <- v:
		default:
			// Buffer full: drop oldest, enqueue newest.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- v:
			default:
			}
		}
	}
}

func (t *topic[T]) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, ch := range t.subs {
		close(ch)
	}
	t.subs = nil
}

// Bus holds one topic per event kind in the system.
type Bus struct {
	trades    topic[metrics.Trade]
	books     topic[metrics.Book]
	assetCtxs topic[metrics.AssetCtx]
	mids      topic[metrics.MidSnapshot]
	bars      topic[metrics.Bar]
	digests   topic[metrics.Digest]
	verdicts  topic[metrics.Verdict]
	positions topic[metrics.Position]
	journal   topic[JournalEvent]
	status    topic[StatusEvent]
}

// New constructs an empty bus.
func New() *Bus { return &Bus{} }

// JournalEvent is a human-readable record emitted whenever a candidate, fill, or
// lifecycle change is journaled. The TUI and Telegram both subscribe.
type JournalEvent struct {
	Coin    string
	Kind    string // "candidate" | "fill" | "open" | "close" | "alert" | "error"
	Summary string
	Verdict *metrics.Verdict // non-nil for candidate events
}

// StatusKind discriminates what a StatusEvent is asserting, so consumers read
// only the fields that event owns. Without it, a notice-only event (Detail set,
// Connected left at its zero value) would falsely report the feed offline.
type StatusKind int

const (
	// StatusNotice is a transient message (reasoner error, history-write failure).
	// It carries Detail and optionally Provider; it must not touch connection state.
	StatusNotice StatusKind = iota
	// StatusConn asserts the websocket connection state via Connected.
	StatusConn
)

// StatusEvent reports connection / provider / mode health for the status line.
type StatusEvent struct {
	Kind      StatusKind
	Connected bool // authoritative only when Kind == StatusConn
	Provider  string
	Mode      string // "propose" | "autonomous"
	Detail    string
}

// --- Publish helpers (producers call these) ---

func (b *Bus) PublishTrade(t metrics.Trade)       { b.trades.publish(t) }
func (b *Bus) PublishBook(bk metrics.Book)        { b.books.publish(bk) }
func (b *Bus) PublishAssetCtx(c metrics.AssetCtx) { b.assetCtxs.publish(c) }
func (b *Bus) PublishMids(m metrics.MidSnapshot)  { b.mids.publish(m) }
func (b *Bus) PublishBar(bar metrics.Bar)         { b.bars.publish(bar) }
func (b *Bus) PublishDigest(d metrics.Digest)     { b.digests.publish(d) }
func (b *Bus) PublishVerdict(v metrics.Verdict)   { b.verdicts.publish(v) }
func (b *Bus) PublishPosition(p metrics.Position) { b.positions.publish(p) }
func (b *Bus) PublishJournal(j JournalEvent)      { b.journal.publish(j) }
func (b *Bus) PublishStatus(s StatusEvent)        { b.status.publish(s) }

// --- Subscribe helpers (consumers call these) ---

func (b *Bus) SubscribeTrades(buf int) <-chan metrics.Trade       { return b.trades.subscribe(buf) }
func (b *Bus) SubscribeBooks(buf int) <-chan metrics.Book         { return b.books.subscribe(buf) }
func (b *Bus) SubscribeAssetCtxs(buf int) <-chan metrics.AssetCtx { return b.assetCtxs.subscribe(buf) }
func (b *Bus) SubscribeMids(buf int) <-chan metrics.MidSnapshot   { return b.mids.subscribe(buf) }
func (b *Bus) SubscribeBars(buf int) <-chan metrics.Bar           { return b.bars.subscribe(buf) }
func (b *Bus) SubscribeDigests(buf int) <-chan metrics.Digest     { return b.digests.subscribe(buf) }
func (b *Bus) SubscribeVerdicts(buf int) <-chan metrics.Verdict   { return b.verdicts.subscribe(buf) }
func (b *Bus) SubscribePositions(buf int) <-chan metrics.Position { return b.positions.subscribe(buf) }
func (b *Bus) SubscribeJournal(buf int) <-chan JournalEvent       { return b.journal.subscribe(buf) }
func (b *Bus) SubscribeStatus(buf int) <-chan StatusEvent         { return b.status.subscribe(buf) }

// Close shuts every topic. Call once on daemon shutdown after producers stop.
func (b *Bus) Close() {
	b.trades.close()
	b.books.close()
	b.assetCtxs.close()
	b.mids.close()
	b.bars.close()
	b.digests.close()
	b.verdicts.close()
	b.positions.close()
	b.journal.close()
	b.status.close()
}
