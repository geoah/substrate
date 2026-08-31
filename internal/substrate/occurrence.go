package substrate

import "time"

// Occurrence is one computed slot of a recurring record (decision 0043): the
// API derives it from the stored rule at read time. It is not a record — no
// id of its own, no edges, never in the changelog — so a consumer building an
// agenda merges these with the temporal window query's rows on the source
// record and the instant.
type Occurrence struct {
	// Kind/ID/Title name the recurring record whose rule names this instant.
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	Title string    `json:"title,omitempty"`
	At    time.Time `json:"at"`
	// Log is the occurrencelog row answering this slot, when one exists.
	// Absence still means missed or still ahead, never suppressed.
	Log *OccurrenceLog `json:"log,omitempty"`
}

// OccurrenceLog names the log record that marked an occurrence, with the
// state its flip machine sits in (done or skipped, per decision 0040).
type OccurrenceLog struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

// OccurrenceProblem reports one recurring record the expansion could not
// read: a rule too dense for the iteration budget, an unknown timezone, an
// anchorless rule. The rest of the answer stands; the problem names what it
// is missing.
type OccurrenceProblem struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Message string `json:"message"`
}

// OccurrenceList is the occurrences read's envelope. Truncated reports that
// the slot cap cut the answer short; there is no cursor, because a computed
// occurrence has no stable address to resume from — narrow the window
// instead.
type OccurrenceList struct {
	Occurrences []Occurrence        `json:"occurrences"`
	Truncated   bool                `json:"truncated"`
	Problems    []OccurrenceProblem `json:"problems,omitempty"`
}
