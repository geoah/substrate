package engine

// A schedule trigger's due occurrences are its RRULE's occurrences in
// (fire state, now]. rrule-go walks a rule only from its anchor (StartsAt, or
// the trigger's creation), so a fresh walk of a minutely rule anchored a year
// back steps over half a million occurrences to reach the fire state: about
// 150 ms and 12 MB, paid on every look of the schedule lane and on every
// status read (#874). scheduleCursor keeps each trigger's iterator where the
// last walk left it, so a walk resumes from the fire state instead.
//
// The rule is never re-anchored nearer to now by arithmetic: DST, the BYxxx
// parts and COUNT make each occurrence depend on the ones before it, so only
// a walk from the anchor knows where the rule stands.

import (
	"sync"
	"time"
)

// scheduleCursor is one schedule trigger's position in its rule. Every
// occurrence the iterator has handed out is either at or before skipped, and
// so at or before every `after` a resuming walk accepts, or held, oldest
// first: a walk resumed from here sees exactly the occurrences a fresh walk
// from the anchor would.
type scheduleCursor struct {
	mu sync.Mutex
	// key is what the iterator was built from; a walk under another key
	// starts again from the anchor.
	key scheduleCursorKey
	// next is the rule's iterator, nil until the first walk.
	next func() (time.Time, bool)
	// skipped is the newest occurrence a walk stepped over as not after its
	// `after`.
	skipped time.Time
	// held is the occurrences taken from next and not yet stepped over, at
	// most one walk's limit plus one.
	held []time.Time
	// done is set once next has no occurrence left (COUNT, UNTIL).
	done bool
}

// scheduleCursorKey is a rule's identity: the recurrence, the timezone it is
// read in and the anchor it starts at.
type scheduleCursorKey struct {
	recurrence string
	timezone   string
	anchor     time.Time
}

func (k scheduleCursorKey) equal(o scheduleCursorKey) bool {
	return k.recurrence == o.recurrence && k.timezone == o.timezone && k.anchor.Equal(o.anchor)
}

func scheduleKeyOf(s *scheduleSource, createdAt time.Time) scheduleCursorKey {
	anchor := createdAt
	if s.StartsAt != nil {
		anchor = *s.StartsAt
	}
	return scheduleCursorKey{recurrence: s.Recurrence, timezone: s.Timezone, anchor: anchor}
}

// dueFires lists the trigger's occurrences in (after, now], oldest first, at
// most limit of them. Nothing is coalesced: a trigger that missed
// occurrences (the server down, the trigger disabled, a repository restored
// to an older fire state) fires each one, and the bound is what keeps a pass
// from turning a long gap into one burst.
//
// The walk resumes from the trigger's cursor when `after` is not behind it
// and the rule is the one the cursor walked; a fire state moved back (a
// restore) or a rule edited walks again from the anchor. The cursor's lock
// is held for the walk, so the schedule lane's workers and a status read of
// one trigger take turns, and two triggers walk at once.
func (ds *dataset) dueFires(triggerID string, s *scheduleSource, createdAt, after, now time.Time, limit int) ([]time.Time, error) {
	c := ds.scheduleCursorOf(triggerID)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.due(s, createdAt, after, now, limit)
}

// owesAfter reports whether the trigger has an occurrence in (after, now],
// which is whether dueFires would list one. A cursor that has stepped past
// `after` answers from the occurrence it stepped over instead of walking
// again from the anchor: the dispatcher asks with the fire state its pass
// began with, which a fire earlier in the pass, or a wake, has moved on.
func (ds *dataset) owesAfter(triggerID string, s *scheduleSource, createdAt, after, now time.Time) (bool, error) {
	c := ds.scheduleCursorOf(triggerID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.next != nil && c.key.equal(scheduleKeyOf(s, createdAt)) && after.Before(c.skipped) && !c.skipped.After(now) {
		return true, nil
	}
	due, err := c.due(s, createdAt, after, now, 1)
	return len(due) > 0, err
}

// scheduleCursorOf is the trigger's cursor, made on first use.
func (ds *dataset) scheduleCursorOf(triggerID string) *scheduleCursor {
	ds.scheduleCursorsMu.Lock()
	defer ds.scheduleCursorsMu.Unlock()
	if ds.scheduleCursors == nil {
		ds.scheduleCursors = map[string]*scheduleCursor{}
	}
	c := ds.scheduleCursors[triggerID]
	if c == nil {
		c = &scheduleCursor{}
		ds.scheduleCursors[triggerID] = c
	}
	return c
}

// pruneScheduleCursors drops the cursors of triggers that are gone or no
// longer carry a schedule source, so a repository whose triggers come and go
// does not grow the map.
func (ds *dataset) pruneScheduleCursors(triggers []loadedTrigger) {
	live := make(map[string]bool, len(triggers))
	for _, lt := range triggers {
		if lt.Err == nil && lt.Schedule != nil {
			live[lt.ID] = true
		}
	}
	ds.scheduleCursorsMu.Lock()
	defer ds.scheduleCursorsMu.Unlock()
	for id := range ds.scheduleCursors {
		if !live[id] {
			delete(ds.scheduleCursors, id)
		}
	}
}

// due is dueFires over the cursor; the caller holds c.mu.
func (c *scheduleCursor) due(s *scheduleSource, createdAt, after, now time.Time, limit int) ([]time.Time, error) {
	key := scheduleKeyOf(s, createdAt)
	if c.next == nil || !c.key.equal(key) || after.Before(c.skipped) {
		r, err := s.rule(createdAt)
		if err != nil {
			c.next = nil
			return nil, err
		}
		c.key, c.next, c.skipped, c.held, c.done = key, r.Iterator(), time.Time{}, nil, false
	}
	// Step over every held occurrence at or before after; the ones left are
	// all after it.
	i := 0
	for i < len(c.held) && !c.held[i].After(after) {
		c.skipped = c.held[i]
		i++
	}
	c.held = c.held[i:]
	if len(c.held) == 0 {
		for !c.done {
			at, ok := c.next()
			if !ok {
				c.done = true
				break
			}
			if !at.After(after) {
				c.skipped = at
				continue
			}
			c.held = append(c.held, at)
			break
		}
	}
	var out []time.Time
	for _, at := range c.held {
		if len(out) >= limit || at.After(now) {
			return out, nil
		}
		out = append(out, at.UTC())
	}
	for len(out) < limit && !c.done {
		at, ok := c.next()
		if !ok {
			c.done = true
			break
		}
		// Held whether or not it is due: the iterator cannot step back, so
		// the first occurrence past now is the next walk's.
		c.held = append(c.held, at)
		if at.After(now) {
			break
		}
		out = append(out, at.UTC())
	}
	return out, nil
}
