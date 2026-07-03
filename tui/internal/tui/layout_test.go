package tui

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want layoutMode
	}{
		{"roomy default", 120, 40, layoutWide},
		{"wide but short still wide", 120, 16, layoutWide},
		{"at two-column breakpoint", twoColW, 40, layoutWide},
		{"just below two-column", twoColW - 1, 40, layoutNarrow},
		{"narrow and tall", 50, 40, layoutNarrow},
		{"narrow but short collapses to tiny", 50, tinyH - 1, layoutTiny},
		{"very narrow always tiny", tinyW - 1, 40, layoutTiny},
		{"too small by width", 20, 40, layoutTooSmall},
		{"too small by height", 80, 4, layoutTooSmall},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify(c.w, c.h); got != c.want {
				t.Errorf("classify(%d,%d) = %v, want %v", c.w, c.h, got, c.want)
			}
		})
	}
}

// TestWideColumnsFitWidth verifies the plan's top row: markets and detail tile the
// full terminal width, detail never falls below its floor, and the chat strip spans
// the terminal.
func TestWideColumnsFitWidth(t *testing.T) {
	for w := twoColW; w <= 220; w++ {
		l := computeLayout(w, 40, 0)
		if l.mode != layoutWide {
			t.Fatalf("w=%d: expected wide, got %v", w, l.mode)
		}
		if l.marketsW+l.detailW != w {
			t.Errorf("w=%d: marketsW=%d + detailW=%d != %d", w, l.marketsW, l.detailW, w)
		}
		if l.detailW < detailColMin {
			t.Errorf("w=%d: detailW=%d below floor %d", w, l.detailW, detailColMin)
		}
		if l.marketsW < marketsColMin {
			t.Errorf("w=%d: marketsW=%d below floor %d", w, l.marketsW, marketsColMin)
		}
		if l.chatW != w {
			t.Errorf("w=%d: chat strip width %d should span the terminal", w, l.chatW)
		}
	}
}

// TestChatNeverBelowFloor verifies the chat pane keeps at least its floor height in
// both the stacked (narrow) and wide layouts, even as the terminal shrinks.
func TestChatNeverBelowFloor(t *testing.T) {
	for h := tinyH; h <= 40; h++ {
		for _, w := range []int{50, 100} { // narrow, wide
			l := computeLayout(w, h, 0)
			if l.mode != layoutNarrow && l.mode != layoutWide {
				continue
			}
			if l.chatH < chatFloorH {
				t.Errorf("%v %dx%d: chatH=%d below floor %d", l.mode, w, h, l.chatH, chatFloorH)
			}
		}
	}
}

// TestZonesFitHeight verifies the resolved zones exactly fill the terminal height
// across a sweep of sizes — never overflowing (which would clip a border).
func TestZonesFitHeight(t *testing.T) {
	for w := minW; w <= 200; w += 3 {
		for h := minH; h <= 60; h++ {
			l := computeLayout(w, h, 0)
			body := h - statusHeight
			switch l.mode {
			case layoutWide:
				if l.marketsH != l.detailH {
					t.Errorf("wide %dx%d: marketsH=%d != detailH=%d", w, h, l.marketsH, l.detailH)
				}
				if got := l.marketsH + l.chatH; got != body {
					t.Errorf("wide %dx%d: top+chat=%d, want body=%d", w, h, got, body)
				}
			case layoutNarrow:
				if got := l.marketsH + l.chatH; got != body {
					t.Errorf("narrow %dx%d: marketsH+chatH=%d, want body=%d", w, h, got, body)
				}
			case layoutTiny:
				if want := max(3, h-1-statusHeight); l.chatH != want {
					t.Errorf("tiny %dx%d: chatH=%d, want %d", w, h, l.chatH, want)
				}
			}
		}
	}
}
