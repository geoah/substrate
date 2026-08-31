// Package occurrence expands a stored recurrence rule into the instants it
// names inside a window. It is a READ: it writes nothing, and it lives outside
// internal/engine on purpose, so decision 0039 ("the substrate stores a
// recurrence rule and never expands it") stays literally true while decision
// 0043 gives every consumer the one expander it promised.
package occurrence

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// budget bounds the rule iterations one expansion may spend. rrule-go walks
// forward from the anchor, so a dense rule with an old anchor (FREQ=MINUTELY
// since 2010) would otherwise burn CPU on every read; blowing the budget is an
// error the caller reports per record, never a truncated answer that reads as
// complete.
const budget = 500_000

// ErrTooDense is wrapped by Expand when the rule blows the iteration budget.
var ErrTooDense = errors.New("the rule is too dense to expand")

// Rule is the recurring trait's read-side contract: the four trait properties,
// the anchor, and the span whose occurrences already exist as records.
type Rule struct {
	// Recurrence is the RFC 5545 rule, with or without its "RRULE:" prefix.
	// Empty is legal: a schedule may be pure RDates.
	Recurrence string
	// StartsAt anchors the rule (iCalendar's DTSTART); its wall clock in
	// Timezone is the time of day every occurrence keeps.
	StartsAt time.Time
	// Timezone is the IANA zone a time-of-day rule resolves in; empty is UTC.
	Timezone string
	// RDates add occurrences beside the rule; ExDates remove them after the
	// union, iCalendar's semantics.
	RDates  []time.Time
	ExDates []time.Time
	// [MaterializedFrom, MaterializedUntil) is the span whose occurrences
	// exist as records (decision 0043): the expander emits nothing inside it,
	// because there the rows are the truth. A zero MaterializedUntil means no
	// span; a zero MaterializedFrom leaves the span open at the past end.
	MaterializedFrom  time.Time
	MaterializedUntil time.Time
}

// Expansion is one rule's computed occurrences, ascending, in UTC.
type Expansion struct {
	Times []time.Time
	// Truncated reports that maxSlots cut the answer short.
	Truncated bool
}

// Expand computes the rule's occurrences in [from, to). maxSlots caps the
// answer (a non-positive cap means uncapped); the iteration budget caps the
// work and errors rather than under-reporting.
func Expand(r Rule, from, to time.Time, maxSlots int) (Expansion, error) {
	if !from.Before(to) {
		return Expansion{}, fmt.Errorf("the window is empty: from %s is not before to %s", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	loc := time.UTC
	if r.Timezone != "" {
		l, err := time.LoadLocation(r.Timezone)
		if err != nil {
			return Expansion{}, fmt.Errorf("unknown timezone %q", r.Timezone)
		}
		loc = l
	}

	excluded := make(map[int64]bool, len(r.ExDates))
	for _, t := range r.ExDates {
		excluded[t.UnixNano()] = true
	}

	seen := make(map[int64]bool)
	var times []time.Time
	keep := func(t time.Time) {
		t = t.UTC()
		if t.Before(from) || !t.Before(to) {
			return
		}
		if excluded[t.UnixNano()] || seen[t.UnixNano()] {
			return
		}
		if !r.MaterializedUntil.IsZero() && t.Before(r.MaterializedUntil) &&
			(r.MaterializedFrom.IsZero() || !t.Before(r.MaterializedFrom)) {
			return
		}
		seen[t.UnixNano()] = true
		times = append(times, t)
	}

	if body := strings.TrimPrefix(r.Recurrence, "RRULE:"); body != "" {
		if r.StartsAt.IsZero() {
			return Expansion{}, errors.New("the rule has no anchor: neither startsAt nor at is set")
		}
		opt, err := rrule.StrToROption(body)
		if err != nil {
			return Expansion{}, fmt.Errorf("expected an RFC 5545 RRULE string: %w", err)
		}
		// The anchor's wall clock in the rule's zone is what recurs, so a
		// daily 09:00 stays 09:00 across a DST boundary.
		opt.Dtstart = r.StartsAt.In(loc)
		rule, err := rrule.NewRRule(*opt)
		if err != nil {
			return Expansion{}, fmt.Errorf("expected an RFC 5545 RRULE string: %w", err)
		}
		next := rule.Iterator()
		for i := 0; ; i++ {
			if i >= budget {
				return Expansion{}, fmt.Errorf("%w: %d occurrences walked without reaching %s", ErrTooDense, budget, to.Format(time.RFC3339))
			}
			t, ok := next()
			if !ok || !t.Before(to) {
				break
			}
			keep(t)
		}
	}
	for _, t := range r.RDates {
		keep(t)
	}

	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	if maxSlots > 0 && len(times) > maxSlots {
		return Expansion{Times: times[:maxSlots], Truncated: true}, nil
	}
	return Expansion{Times: times}, nil
}
