package marketdata

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTimeframeDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"15m": 15 * time.Minute,
		"1h":  time.Hour,
		"4h":  4 * time.Hour,
		"1d":  24 * time.Hour,
		"1w":  7 * 24 * time.Hour,
		"":    time.Hour, // default
		"xyz": time.Hour, // default
	}
	for in, want := range cases {
		if got := timeframeDuration(in); got != want {
			t.Errorf("timeframeDuration(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDaysForBars(t *testing.T) {
	// 120 1h bars = 5 days → smallest accepted window ≥5 is 7.
	if d := daysForBars("1h", 120); d != 7 {
		t.Errorf("daysForBars(1h,120) = %d, want 7", d)
	}
	// 120 4h bars = 20 days → 30.
	if d := daysForBars("4h", 120); d != 30 {
		t.Errorf("daysForBars(4h,120) = %d, want 30", d)
	}
}

func TestLoadCSVHeadered(t *testing.T) {
	dir := t.TempDir()
	csv := "time,open,high,low,close,volume\n" +
		"1700000000,100,110,90,105,1000\n" +
		"1700003600,105,120,104,118,1500\n"
	if err := os.WriteFile(filepath.Join(dir, "ETH_1h.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	bars, err := LoadCSV(dir, "ETH", "1h", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 {
		t.Fatalf("want 2 bars, got %d", len(bars))
	}
	// enrich: range position of bar 0 = (105-90)/(110-90) = 0.75.
	if bars[0].RangePos < 0.74 || bars[0].RangePos > 0.76 {
		t.Errorf("range pos = %v, want ~0.75", bars[0].RangePos)
	}
	// enrich: return of bar 1 = (118-105)/105 ≈ 0.1238.
	if bars[1].Return < 0.12 || bars[1].Return > 0.13 {
		t.Errorf("return = %v, want ~0.124", bars[1].Return)
	}
	if bars[0].Coin != "ETH" || bars[0].Timeframe != "1h" {
		t.Errorf("tagging wrong: %+v", bars[0])
	}
}

func TestLoadCSVHeadlessPositional(t *testing.T) {
	dir := t.TempDir()
	csv := "1700000000,100,110,90,105,1000\n1700003600,105,120,104,118,1500\n"
	if err := os.WriteFile(filepath.Join(dir, "btc.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	bars, err := LoadCSV(dir, "BTC", "1h", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 || bars[0].Close != 105 {
		t.Fatalf("positional parse failed: %+v", bars)
	}
}

func TestLoadCSVMissingIsNotError(t *testing.T) {
	bars, err := LoadCSV(t.TempDir(), "NOPE", "1h", 0)
	if err != nil || bars != nil {
		t.Fatalf("missing file should be (nil,nil), got (%v,%v)", bars, err)
	}
}

func TestCoinGeckoIDResolution(t *testing.T) {
	cg := NewCoinGecko(map[string]string{"FOO": "foo-token"})
	if id, _ := cg.ID("BTC"); id != "bitcoin" {
		t.Errorf("BTC -> %q, want bitcoin", id)
	}
	if id, _ := cg.ID("FOO"); id != "foo-token" {
		t.Errorf("override FOO -> %q, want foo-token", id)
	}
	if _, ok := cg.ID("UNKNOWNXYZ"); ok {
		t.Errorf("unknown symbol should not resolve")
	}
}
