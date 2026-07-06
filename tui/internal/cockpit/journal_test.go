package cockpit

import (
	"testing"
	"time"
)

func TestTagFor(t *testing.T) {
	cases := map[string]string{
		"candidate": "REASON",
		"open":      "EXECUTE",
		"close":     "EXECUTE",
		"fill":      "FILL",
		"alert":     "RISK",
		"error":     "ERROR",
		"":          "OPERATOR",
		"whatever":  "OPERATOR",
	}
	for kind, want := range cases {
		if got := tagFor(kind); got != want {
			t.Errorf("tagFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestAppendJournalCap(t *testing.T) {
	var entries []journalEntry
	for i := 0; i < maxJournal+50; i++ {
		entries = appendJournal(entries, journalEntry{at: time.Now(), tag: "FILL", text: "x"})
	}
	if len(entries) != maxJournal {
		t.Errorf("journal len = %d, want %d", len(entries), maxJournal)
	}
}
