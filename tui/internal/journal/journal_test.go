package journal

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadDayRoundTrip verifies the read path the API's journal endpoint
// depends on: entries written through Record (or by hand, as here) come back
// decoded, and a malformed line in the middle of the file is skipped rather
// than failing the whole read.
func TestReadDayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"time":"2026-07-03T10:00:00Z","coin":"BTC","kind":"candidate","summary":"one"}
not valid json at all
{"time":"2026-07-03T10:05:00Z","coin":"ETH","kind":"fill","summary":"two"}
`
	if err := os.WriteFile(filepath.Join(jdir, "2026-07-03.ndjson"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadDay(dir, "2026-07-03")
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (malformed line skipped): %+v", len(entries), entries)
	}
	if entries[0].Coin != "BTC" || entries[0].Summary != "one" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Coin != "ETH" || entries[1].Summary != "two" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
}

// TestReadDayMissingFile: a day that never journaled anything is not an
// error — read endpoints should render "no entries", not fail.
func TestReadDayMissingFile(t *testing.T) {
	dir := t.TempDir()
	entries, err := ReadDay(dir, "2026-01-01")
	if err != nil {
		t.Fatalf("ReadDay on missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want empty", entries)
	}
}

// TestReadDayInvalidDate rejects a date that doesn't parse as YYYY-MM-DD so
// the API layer can turn it into a 400 rather than silently reading nothing
// or panicking on a path traversal attempt.
func TestReadDayInvalidDate(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadDay(dir, "not-a-date"); err == nil {
		t.Fatal("expected error for invalid date")
	}
	if _, err := ReadDay(dir, "../../etc/passwd"); err == nil {
		t.Fatal("expected error for path-traversal-shaped date")
	}
}
