package substrate

import "time"

// The window read: a records list whose filter bounds `at` on both ends over
// temporal kinds. It answers with the rows in the window AND the occurrences
// it computes from every series among them (decision 0039 stores the rule and
// never expands it into rows; its successor computes the expansion inside
// this one read instead of a second one). The API layer does the expanding;
// the engine answers the three halves below inside one snapshot and holds no
// expander.

// WindowKey is the position of one item in the window's total order: its
// slot, then its kind, then its id. A page's cursor names the last item
// emitted, computed or stored alike, and the next page seeks strictly past it.
type WindowKey struct {
	At   time.Time
	Kind string
	ID   string
}

// WindowQuery is the engine's half of a window read.
type WindowQuery struct {
	// Filter is the caller's, `at` bound included: the rows honor every arm,
	// the series candidates every arm but the `at` bound.
	Filter Filter
	// From and To are the `at` bound, half-open [From, To).
	From, To time.Time
	// Desc walks the window newest-first.
	Desc bool
	// First is how many rows to answer past After; the API merges computed
	// occurrences into them and cuts at the same count.
	First int
	// After is the last item the previous page emitted, or nil.
	After *WindowKey
	// WithAnnotations and Expand are the list's, applied to the rows.
	WithAnnotations bool
	Expand          []string
}

// WindowPage is what the engine answers, all three halves read on one
// snapshot so a master and its overrides are never seen in different states.
type WindowPage struct {
	// Rows are the plain events and overrides after the key, in order; no
	// series is among them (a series is on the timeline only through its
	// occurrences). Len is at most First.
	Rows []*Record
	// More reports that a row beyond Rows exists in the window, so the
	// caller's expansion bound is the last row's slot and not To.
	More bool
	// Series is every candidate series the filter admits, whole: more than
	// the budget is an error, never a short list.
	Series []*Record
	// Overrides are the rows whose `recurrenceOf` names one of Series and
	// whose `originalAt` falls in the window, whatever their kind and wherever
	// their own `at` went: each claims one slot the expansion must skip.
	Overrides []*Record
	// Head and Generation are the snapshot's, as on any page.
	Head       int64
	Generation string
	// Included carries the expanded referents of Rows, as on a list page.
	Included map[string]*Record
}

// WindowSeriesBudget bounds how many series one window read enumerates. The
// set must be complete for the page to be correct, so past the budget the
// read refuses rather than silently drops the series with the earliest slot.
const WindowSeriesBudget = 10000

// OccurrenceProblem reports one series the expansion could not read: a rule
// too dense for the iteration budget, an unknown timezone, an anchorless rule.
// The rest of the page stands; the problem names what it is missing.
type OccurrenceProblem struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Message string `json:"message"`
}
