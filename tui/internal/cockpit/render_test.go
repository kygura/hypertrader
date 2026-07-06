package cockpit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestFnum(t *testing.T) {
	cases := []struct {
		v    float64
		dec  int
		want string
	}{
		{3412.4, 1, "3,412.4"},
		{67231, 0, "67,231"},
		{0.8412, 4, "0.8412"},
		{1234567.89, 2, "1,234,567.89"},
	}
	for _, c := range cases {
		if got := fnum(c.v, c.dec); got != c.want {
			t.Errorf("fnum(%v, %d) = %q, want %q", c.v, c.dec, got, c.want)
		}
	}
}

func TestPriceDec(t *testing.T) {
	cases := []struct {
		v    float64
		want int
	}{{0.5, 4}, {42, 2}, {3412, 1}, {67231, 0}}
	for _, c := range cases {
		if got := priceDec(c.v); got != c.want {
			t.Errorf("priceDec(%v) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestSpreadWidth(t *testing.T) {
	s := spread("left", "right", 40)
	if w := lipgloss.Width(s); w != 40 {
		t.Errorf("spread width = %d, want 40", w)
	}
}

func TestPad(t *testing.T) {
	if got := padR("ab", 5); got != "ab   " {
		t.Errorf("padR = %q", got)
	}
	if got := padL("ab", 5); got != "   ab" {
		t.Errorf("padL = %q", got)
	}
}

func TestBar(t *testing.T) {
	b := bar(0.5, 10)
	if w := lipgloss.Width(b); w != 10 {
		t.Errorf("bar width = %d, want 10", w)
	}
	if b2 := bar(1.5, 10); lipgloss.Width(b2) != 10 { // ratio clamped
		t.Errorf("bar clamp failed")
	}
}

func TestBoxGeometry(t *testing.T) {
	out := box("TITLE", "right", []string{"one", "two"}, 40, 8)
	rows := strings.Split(out, "\n")
	if len(rows) != 8 {
		t.Fatalf("box height = %d rows, want 8", len(rows))
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w != 40 {
			t.Errorf("row %d width = %d, want 40", i, w)
		}
	}
	if !strings.Contains(out, "TITLE") {
		t.Error("box missing title")
	}
}

func TestTruncTail(t *testing.T) {
	if got := truncTail("hello world", 6); lipgloss.Width(got) > 6 {
		t.Errorf("truncTail too wide: %q", got)
	}
}
