package store

import (
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

func TestRingPushAndSlice(t *testing.T) {
	r := newRing(3)
	base := time.Now()
	for i := 0; i < 5; i++ {
		r.push(metrics.Bar{OpenTime: base.Add(time.Duration(i) * time.Hour), Close: float64(i)})
	}
	// Capacity 3 keeps the last three (closes 2,3,4).
	got := r.slice(0)
	if len(got) != 3 {
		t.Fatalf("want 3 bars, got %d", len(got))
	}
	if got[0].Close != 2 || got[2].Close != 4 {
		t.Fatalf("ring order wrong: %v %v", got[0].Close, got[2].Close)
	}
	last, ok := r.last()
	if !ok || last.Close != 4 {
		t.Fatalf("last wrong: %v %v", ok, last.Close)
	}
}

func TestRingOverwritesSameOpenTime(t *testing.T) {
	r := newRing(4)
	ot := time.Now().Truncate(time.Hour)
	r.push(metrics.Bar{OpenTime: ot, Close: 1})
	r.push(metrics.Bar{OpenTime: ot, Close: 2}) // same bucket → update in place
	if r.size != 1 {
		t.Fatalf("want size 1 after in-place update, got %d", r.size)
	}
	last, _ := r.last()
	if last.Close != 2 {
		t.Fatalf("want updated close 2, got %v", last.Close)
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 16)
	if err != nil {
		t.Fatal(err)
	}
	ct := time.Now().Truncate(time.Hour)
	for i := 0; i < 3; i++ {
		b := metrics.Bar{Coin: "ETH", Timeframe: "1h", CloseTime: ct.Add(time.Duration(i) * time.Hour), Close: float64(i)}
		if err := s.AppendHistory(b); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := s.LoadHistory("ETH", "1h", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("want 3 loaded, got %d", len(loaded))
	}
	if loaded[2].Close != 2 {
		t.Fatalf("want last close 2, got %v", loaded[2].Close)
	}
}

func TestPositionLifecycle(t *testing.T) {
	s, _ := New(t.TempDir(), 8)
	s.PutPosition(metrics.Position{Coin: "SOL", Size: 10, MarkPrice: 150})
	if got := s.Position("SOL"); got.Size != 10 {
		t.Fatalf("want size 10, got %v", got.Size)
	}
	s.PutPosition(metrics.Position{Coin: "SOL", Size: 0}) // flat clears it
	if got := s.Position("SOL"); !got.IsFlat() {
		t.Fatalf("want flat after clear")
	}
	if len(s.Positions()) != 0 {
		t.Fatalf("want no open positions")
	}
}
