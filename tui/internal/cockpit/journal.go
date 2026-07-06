package cockpit

import "time"

// maxJournal caps the decision-journal ring (mock's value).
const maxJournal = 200

// journalEntry is one line of the append-only decision journal.
type journalEntry struct {
	at   time.Time
	tag  string // REASON | EXECUTE | FILL | RISK | ERROR | OPERATOR
	text string
}

// tagFor maps a bus journal kind (bridge journalMsg.Kind) to the cockpit
// tag vocabulary. Unknown kinds — including operator-side notices that
// never came from the bus — read as OPERATOR.
func tagFor(kind string) string {
	switch kind {
	case "candidate":
		return "REASON"
	case "open", "close":
		return "EXECUTE"
	case "fill":
		return "FILL"
	case "alert":
		return "RISK"
	case "error":
		return "ERROR"
	}
	return "OPERATOR"
}

// appendJournal appends e and trims the ring to maxJournal.
func appendJournal(entries []journalEntry, e journalEntry) []journalEntry {
	entries = append(entries, e)
	if len(entries) > maxJournal {
		entries = entries[len(entries)-maxJournal:]
	}
	return entries
}
