package tui

// Responsive layout for the plan's main view: MARKETS │ DETAIL side by side on
// top, the CHAT strip beneath, and the status line last. A single classifier maps
// the terminal size to one of four shapes, and computeLayout turns (width, height)
// into concrete per-zone dimensions. View (rendering) and applyLayout (sizing the
// embedded components) consume the same computed layout, so the frame and the
// components never disagree — and resizing the terminal reflows every zone,
// because both run on each WindowSizeMsg.
//
// As the terminal shrinks the zones collapse in priority order: the detail column
// folds first (it reopens on demand as a floating modal via enter), then markets
// and chat stack, then everything reduces to chat plus a one-line ticker.

// layoutMode is the structural shape chosen for the current terminal size.
type layoutMode int

const (
	// layoutTooSmall: below this even chat can't render — show a centered notice.
	layoutTooSmall layoutMode = iota
	// layoutTiny: chat fullscreen + a one-line ticker; markets + detail dropped.
	layoutTiny
	// layoutNarrow: stack — markets grid over chat; detail opens as a modal.
	layoutNarrow
	// layoutWide: the plan default — markets │ detail on top, chat strip below.
	layoutWide
)

// Breakpoints and zone floors. These are the only magic numbers; everything else is
// derived from them, so the responsive behavior is tunable here.
const (
	minW = 24 // below this width nothing renders usefully
	minH = 6  // below this height nothing renders usefully

	tinyW = 46 // below this width go chat-only
	tinyH = 10 // below this height go chat-only

	twoColW = 64 // at/above this (and tall enough) markets sits beside detail

	marketsColMin = 28 // markets column floor (wide)
	marketsColMax = 52 // markets column cap
	detailColMin  = 32 // detail column floor (wide)

	statusHeight   = 1  // bottom status bar
	chatFloorH     = 5  // chat pane floor: border + title + input + a few history rows
	chatMaxH       = 14 // chat strip cap in the wide layout
	marketsRowsMin = 4  // markets grid floor in the stacked layout
	marketsRowsMax = 14 // markets grid cap in the stacked layout
)

// classify maps a terminal size to a layout mode. Order matters: the smallest modes
// are tested first so a cramped terminal collapses all the way to chat-only.
func classify(w, h int) layoutMode {
	switch {
	case w < minW || h < minH:
		return layoutTooSmall
	case w < tinyW || h < tinyH:
		return layoutTiny
	case w < twoColW:
		return layoutNarrow
	default:
		return layoutWide
	}
}

// layout holds the resolved geometry for every zone in the current frame.
type layout struct {
	mode          layoutMode
	width, height int

	marketsW, marketsH int
	chatW, chatH       int
	detailW, detailH   int

	showTicker bool // one-line asset ticker (tiny mode)
}

// computeLayout resolves the geometry for a given terminal size. chatOff adds or
// removes rows from the base chat height (positive = taller chat), clamped so neither
// the chat nor the top section shrinks below its usable floor. It is pure so it can
// be unit-tested directly.
func computeLayout(w, h, chatOff int) layout {
	l := layout{mode: classify(w, h), width: w, height: h}

	switch l.mode {
	case layoutTooSmall:
		return l

	case layoutTiny:
		// Ticker (1) + chat (fills) + status (1).
		l.showTicker = true
		l.chatW = w
		l.chatH = max(3, h-1-statusHeight)
		return l

	case layoutNarrow:
		// Stack: markets grid over chat. Chat keeps at least its floor.
		body := h - statusHeight
		ch := clampInt(body*60/100+chatOff, chatFloorH, body-marketsRowsMin)
		mh := max(1, body-ch)
		l.marketsW, l.marketsH = w, mh
		l.chatW, l.chatH = w, ch
		return l

	default: // layoutWide — markets │ detail on top, chat strip below
		body := h - statusHeight
		ch := clampInt(body*35/100+chatOff, chatFloorH, body-8)
		top := body - ch

		// Markets keeps its scannable share; detail gets the remainder, never below
		// its floor (the breakpoint guarantees marketsColMin + detailColMin fit).
		mw := clampInt(w*42/100, marketsColMin, marketsColMax)
		if mw > w-detailColMin {
			mw = max(marketsColMin, w-detailColMin)
		}
		l.marketsW, l.marketsH = mw, top
		l.detailW, l.detailH = w-mw, top
		l.chatW, l.chatH = w, ch
		return l
	}
}
