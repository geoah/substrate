// Package window is the window read: a records list whose filter bounds `at`
// on both ends answers with the stored rows in the window and the occurrences
// computed from every series among the kinds in play. The records route and a
// function's or an agent's list read call the same Read, so every reader sees
// one timeline (decision records 0081 and 0107).
package window

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/occurrence"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The window read: a records list whose filter bounds `at` on BOTH ends over
// temporal kinds answers with the rows in the window and, beside them, the
// occurrences it computes from every series among the kinds in play (the core
// `recurring` trait), minus each series' exdates and minus every slot an
// `override` row claims. One read, one page, ordered by the slot.
//
// The rule is stored and never expanded into rows (decision 0039); this is
// where the expansion happens, above the engine's storage, which answers the
// three halves on one snapshot (Dataset.Window) and holds no expander. The
// records route and the engine's function and agent list reads call Read, so
// every reader sees one timeline (decision 0107). A computed occurrence is
// served in the record envelope so every consumer renders it unchanged: the
// series' kind, the id `<seriesId>_<slot>`, the series' properties with the
// slot in its temporal columns and `recurrenceOf`/`originalAt` filled,
// version 0, `computed: true`. A put at that id is how it becomes a stored
// override.

// slotLayout spells a slot inside a computed id: UTC, basic format, second
// grain. Google's own instance ids carry the same spelling, so a mirror's
// exception and the occurrence the read computed for it share an id string.
const slotLayout = "20060102T150405Z"

// windowCursor is the page cursor a window read mints: the last emitted item's
// key, the signature of the filter and direction it was minted under, and
// the head and history generation the walk began at, the same guarantees the
// list cursor carries (engine/query.go keyset).
type windowCursor struct {
	At   time.Time `json:"a"`
	Kind string    `json:"k"`
	ID   string    `json:"i"`
	S    string    `json:"s"`
	H    int64     `json:"h,omitempty"`
	G    string    `json:"g,omitempty"`
}

// Bounds reads the `at` bound off a filter. Both ends set is a window
// read; one end alone is not (the rows are filtered as today and nothing is
// computed, because an unbounded side would make the expansion unbounded).
// `gt` and `lte` are folded into the half-open [from, to) at the column's
// microsecond grain.
func Bounds(f substrate.Filter) (from, to time.Time, ok bool, err error) {
	c, has := f.Properties[substrate.PropAt]
	if !has {
		return from, to, false, nil
	}
	lo, hi := c.Gte, c.Lt
	loOpen, hiClosed := false, false
	if lo == nil && c.Gt != nil {
		lo, loOpen = c.Gt, true
	}
	if hi == nil && c.Lte != nil {
		hi, hiClosed = c.Lte, true
	}
	if lo == nil || hi == nil {
		return from, to, false, nil
	}
	if from, err = instantValue(lo); err != nil {
		return from, to, false, fmt.Errorf("filter.properties.at: %w", err)
	}
	if to, err = instantValue(hi); err != nil {
		return from, to, false, fmt.Errorf("filter.properties.at: %w", err)
	}
	if loOpen {
		from = from.Add(time.Microsecond)
	}
	if hiClosed {
		to = to.Add(time.Microsecond)
	}
	if !from.Before(to) {
		return from, to, false, errors.New("filter.properties.at: the window is empty")
	}
	return from, to, true, nil
}

func instantValue(v any) (time.Time, error) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, errors.New("expected an RFC 3339 instant")
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, errors.New("expected an RFC 3339 instant")
	}
	return t.UTC(), nil
}

// Reader is what a window read needs of a dataset: the engine's half of the
// read, and the kinds binding `override`.
type Reader interface {
	Window(ctx context.Context, q substrate.WindowQuery) (*substrate.WindowPage, error)
	TypesImplementing(ctx context.Context, trait string) ([]substrate.KindInfo, error)
}

// QueryError refuses a window read the caller shaped wrong: an order other
// than `at`, an offset, a cursor that does not decode. The records route
// answers it 400; a function body receives its message.
type QueryError string

func (e QueryError) Error() string { return string(e) }

// Read answers a window read: q is a records list whose filter Bounds reads
// as [from, to). A cursor minted against another filter or direction is
// substrate.ErrValidation, and one minted against another history is
// substrate.ErrStaleHistory, as on a plain list.
func Read(ctx context.Context, ds Reader, q substrate.Query, from, to time.Time) (*substrate.Page, error) {
	desc := false
	switch {
	case len(q.OrderBy) == 0:
	case len(q.OrderBy) == 1 && q.OrderBy[0].Property == substrate.PropAt:
		desc = q.OrderBy[0].Desc
	default:
		return nil, QueryError("a window read (filter.properties.at bounded on both ends) orders by at alone, ascending or descending: it merges computed occurrences into the page, and only their slot is known")
	}
	// A window page is rows MERGED with occurrences computed for the slot, so
	// there is no row count to skip: offset would address a page of the
	// stored rows alone and silently drop the occurrences between them.
	if q.Offset > 0 {
		return nil, QueryError("offset is not supported on a window read (filter.properties.at bounded on both ends): it merges computed occurrences into the page, so pages are addressed by cursor alone")
	}
	first := q.First
	if first <= 0 {
		first = 50
	}
	if first > 500 {
		first = 500
	}
	sig := windowSignature(q.Filter, desc)
	var after *substrate.WindowKey
	var carried *windowCursor
	if q.After != "" {
		cur, err := decodeWindowCursor(q.After)
		if err != nil {
			return nil, err
		}
		if cur.S != sig {
			return nil, fmt.Errorf("%w: cursor does not match this filter and orderBy", substrate.ErrValidation)
		}
		after = &substrate.WindowKey{At: cur.At, Kind: cur.Kind, ID: cur.ID}
		carried = cur
	}
	wp, err := ds.Window(ctx, substrate.WindowQuery{
		Filter: q.Filter, From: from, To: to, Desc: desc, First: first, After: after,
		WithAnnotations: q.WithAnnotations, Expand: q.Expand,
	})
	if err != nil {
		return nil, err
	}
	if carried != nil && carried.G != "" && carried.G != wp.Generation {
		return nil, fmt.Errorf("%w: cursor was minted against another history; list again", substrate.ErrStaleHistory)
	}
	overrides, err := overrideKinds(ctx, ds)
	if err != nil {
		return nil, err
	}
	page := mergeWindow(wp, from, to, desc, first, after, overrides)
	page.Generation = wp.Generation
	page.Head = wp.Head
	if carried != nil && carried.H != 0 {
		page.Head = carried.H
	}
	if page.Cursor != "" {
		var last *windowCursor
		_ = json.Unmarshal([]byte(page.Cursor), &last)
		last.S, last.H, last.G = sig, page.Head, page.Generation
		page.Cursor = encodeWindowCursor(*last)
	}
	page.Included = wp.Included
	return page, nil
}

// windowItem is one candidate for the page: a stored row or a computed
// occurrence, positioned by its key.
type windowItem struct {
	key substrate.WindowKey
	rec *substrate.Record
}

// mergeWindow is the bounded k-way merge. Every source is complete below the
// emit bound, so the page is ordered and complete: the rows are complete up
// to the last row the engine returned (or the whole window when there were no
// more), each series is complete up to its cap, and the bound is the least of
// those. The cursor it leaves in Page.Cursor is the last emitted key as raw
// JSON, which the caller stamps and encodes.
func mergeWindow(wp *substrate.WindowPage, from, to time.Time, desc bool, first int, after *substrate.WindowKey, overrides map[string]bool) *substrate.Page {
	less := func(a, b substrate.WindowKey) bool {
		if !a.At.Equal(b.At) {
			if desc {
				return a.At.After(b.At)
			}
			return a.At.Before(b.At)
		}
		if a.Kind != b.Kind {
			if desc {
				return a.Kind > b.Kind
			}
			return a.Kind < b.Kind
		}
		if desc {
			return a.ID > b.ID
		}
		return a.ID < b.ID
	}

	var items []windowItem
	for _, rec := range wp.Rows {
		items = append(items, windowItem{key: KeyOf(rec), rec: rec})
	}
	var bound *substrate.WindowKey
	if wp.More && len(wp.Rows) > 0 {
		k := KeyOf(wp.Rows[len(wp.Rows)-1])
		bound = &k
	}

	// Which slots the overrides claim, per series path.
	claimed := map[string]map[int64]bool{}
	for _, o := range wp.Overrides {
		slot, ok := PropInstant(o.Properties, vocabulary.PropOriginalAt)
		if !ok {
			continue
		}
		for _, path := range ReferencePaths(o.Properties[vocabulary.PropRecurrenceOf]) {
			if claimed[path] == nil {
				claimed[path] = map[int64]bool{}
			}
			claimed[path][slot.UnixNano()] = true
		}
	}

	page := &substrate.Page{Records: []*substrate.Record{}}
	capped := false
	for _, s := range wp.Series {
		rule, slotName, empty := ruleOf(s)
		if empty {
			continue
		}
		// A rule whose UNTIL passed is skipped without a walk, but only when
		// nothing else could contribute: an RDATE adds an occurrence
		// independently of the rule's end.
		if len(rule.RDates) == 0 && occurrence.EndsBefore(rule.Recurrence, from) {
			continue
		}
		exp, err := occurrence.Expand(rule, from, to, 0)
		if err != nil {
			page.Problems = append(page.Problems, substrate.OccurrenceProblem{Kind: s.Kind, ID: s.ID, Message: err.Error()})
			continue
		}
		times := exp.Times
		if desc {
			for i, j := 0, len(times)-1; i < j; i, j = i+1, j-1 {
				times[i], times[j] = times[j], times[i]
			}
		}
		path := vocabulary.RecordPath(s.Kind, s.ID)
		count := 0
		var lastKey *substrate.WindowKey
		for _, t := range times {
			k := substrate.WindowKey{At: t, Kind: s.Kind, ID: computedID(s.ID, t)}
			if after != nil && !less(*after, k) {
				continue
			}
			if claimed[path][t.UnixNano()] {
				continue
			}
			if bound != nil && less(*bound, k) {
				break
			}
			if count == first {
				// This series alone fills a page: its cap becomes the emit
				// bound, and whatever any source produced past it waits.
				capped = true
				bound = lastKey
				break
			}
			count++
			kk := k
			lastKey = &kk
			items = append(items, windowItem{key: k, rec: computedRecord(s, rule, slotName, t, path, overrides[s.Kind])})
		}
	}
	if bound != nil {
		kept := items[:0]
		for _, it := range items {
			if !less(*bound, it.key) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	sort.SliceStable(items, func(i, j int) bool { return less(items[i].key, items[j].key) })
	more := wp.More || capped || len(items) > first
	if len(items) > first {
		items = items[:first]
	}
	for _, it := range items {
		page.Records = append(page.Records, it.rec)
	}
	if more && len(items) > 0 {
		last := items[len(items)-1].key
		raw, _ := json.Marshal(windowCursor{At: last.At, Kind: last.Kind, ID: last.ID})
		page.Cursor = string(raw)
	}
	return page
}

// KeyOf positions a stored row: its bound temporal slot (`at`, or `dueAt` on a
// point-renamed binding), then kind, then id.
func KeyOf(rec *substrate.Record) substrate.WindowKey {
	k := substrate.WindowKey{Kind: rec.Kind, ID: rec.ID}
	switch {
	case rec.At != nil:
		k.At = rec.At.UTC()
	case rec.DueAt != nil:
		k.At = rec.DueAt.UTC()
	default:
		if t, ok := PropInstant(rec.Properties, substrate.PropAt); ok {
			k.At = t
		} else if t, ok := PropInstant(rec.Properties, substrate.PropDueAt); ok {
			k.At = t
		}
	}
	return k
}

// ruleOf reads the recurring trait's contract off a series row. The anchor is
// the bound temporal slot, `at` or `dueAt`; slotName says which, so the
// computed occurrence writes its instant under the same name.
func ruleOf(s *substrate.Record) (rule occurrence.Rule, slotName string, empty bool) {
	rule = occurrence.Rule{
		Recurrence: propString(s.Properties, vocabulary.PropRecurrence),
		Timezone:   propString(s.Properties, vocabulary.PropTimezone),
		RDates:     propInstants(s.Properties, vocabulary.PropRDates),
		ExDates:    propInstants(s.Properties, vocabulary.PropExDates),
	}
	slotName = substrate.PropAt
	switch {
	case s.At != nil:
		rule.StartsAt = s.At.UTC()
	case s.DueAt != nil:
		rule.StartsAt, slotName = s.DueAt.UTC(), substrate.PropDueAt
	default:
		if t, ok := PropInstant(s.Properties, substrate.PropAt); ok {
			rule.StartsAt = t
		} else if t, ok := PropInstant(s.Properties, substrate.PropDueAt); ok {
			rule.StartsAt, slotName = t, substrate.PropDueAt
		}
	}
	return rule, slotName, rule.Recurrence == "" && len(rule.RDates) == 0
}

// computedID is `<seriesId>_<slot>`: the slot in UTC basic format at second
// grain. A stored record at this id (kind and all) is the occurrence's
// override, and wins over the computed one wherever the two could meet.
func computedID(seriesID string, at time.Time) string {
	return seriesID + "_" + at.UTC().Format(slotLayout)
}

// splitComputedID reads a computed id apart, or reports that it is not one.
func splitComputedID(id string) (seriesID string, at time.Time, ok bool) {
	i := strings.LastIndex(id, "_")
	if i <= 0 || i+1+len(slotLayout) != len(id) {
		return "", time.Time{}, false
	}
	t, err := time.Parse(slotLayout, id[i+1:])
	if err != nil {
		return "", time.Time{}, false
	}
	return id[:i], t, true
}

// computedRecord renders one occurrence in the record envelope: the series'
// properties with the rule's three removed (an override never carries a
// rule), the slot under the series' own temporal name, `endsAt` kept at the
// anchor's wall-clock duration, and, when the series' kind binds `override`,
// the override pair filled so writing the envelope back at its id IS an
// override. A kind that does not bind it (a provider's series mirror) has
// no declared home for the pair, so its envelope carries the slot alone and
// is read-only by construction.
func computedRecord(s *substrate.Record, rule occurrence.Rule, slotName string, at time.Time, path string, overridable bool) *substrate.Record {
	props := make(map[string]any, len(s.Properties)+2)
	for k, v := range s.Properties {
		switch k {
		case vocabulary.PropRecurrence, vocabulary.PropRDates, vocabulary.PropExDates:
			continue
		}
		props[k] = v
	}
	props[slotName] = at.UTC().Format(time.RFC3339Nano)
	if end, ok := PropInstant(s.Properties, substrate.PropEndsAt); ok {
		props[substrate.PropEndsAt] = wallClockEnd(rule.StartsAt, end, at, rule.Timezone).Format(time.RFC3339Nano)
	}
	if overridable {
		props[vocabulary.PropRecurrenceOf] = map[string]any{vocabulary.ReferenceValueKey: path}
		props[vocabulary.PropOriginalAt] = at.UTC().Format(time.RFC3339Nano)
	}
	return &substrate.Record{
		ID: computedID(s.ID, at), Kind: s.Kind, Title: s.Title,
		Properties: props, Labels: s.Labels,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Computed: true,
	}
}

// wallClockEnd shifts the anchor's span onto an occurrence in the rule's
// zone's WALL clock: the span is a number of civil days plus a time-of-day
// difference, applied to the occurrence's local start. A daily 09:00–10:00
// keeps an hour; an all-day series that runs local midnight to local
// midnight keeps doing so across a DST change instead of becoming 23 or 25
// hours; an RDATE at 15:00 on a 09:00 series ends at 16:00, not at 10:00.
func wallClockEnd(anchorStart, anchorEnd, slot time.Time, zone string) time.Time {
	loc := time.UTC
	if zone != "" {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	}
	ls, le, lslot := anchorStart.In(loc), anchorEnd.In(loc), slot.In(loc)
	d1 := time.Date(ls.Year(), ls.Month(), ls.Day(), 0, 0, 0, 0, time.UTC)
	d2 := time.Date(le.Year(), le.Month(), le.Day(), 0, 0, 0, 0, time.UTC)
	days := int(d2.Sub(d1).Hours() / 24)
	tod := func(t time.Time) time.Duration {
		return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute +
			time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())
	}
	sameDay := time.Date(lslot.Year(), lslot.Month(), lslot.Day()+days, 0, 0, 0, 0, loc)
	return sameDay.Add(tod(lslot) + (tod(le) - tod(ls))).UTC()
}

// overrideKinds is the set of kinds binding core's `override`: the kinds
// whose computed envelopes carry the pair a put needs to become one.
func overrideKinds(ctx context.Context, ds Reader) (map[string]bool, error) {
	infos, err := ds.TypesImplementing(ctx, vocabulary.TraitOverrideCore)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(infos))
	for _, ti := range infos {
		out[ti.Identity] = true
	}
	return out, nil
}

// windowSignature pins a cursor to the filter and direction it was minted
// under, as the list cursor does: replayed against another filter it would
// seek past rows the new predicate admits.
func windowSignature(f substrate.Filter, desc bool) string {
	raw, _ := json.Marshal(f)
	sum := sha256.Sum256(append(raw, byte(map[bool]int{false: 0, true: 1}[desc])))
	return hex.EncodeToString(sum[:8])
}

func encodeWindowCursor(c windowCursor) string {
	raw, _ := json.Marshal(c)
	return "w." + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeWindowCursor(s string) (*windowCursor, error) {
	body, ok := strings.CutPrefix(s, "w.")
	if !ok {
		return nil, QueryError("bad cursor: not a window cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, QueryError("bad cursor")
	}
	var c windowCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, QueryError("bad cursor")
	}
	return &c, nil
}

// OccurrenceReader is what Occurrence needs: a window reader that also reads
// one record and lists the overrides at a slot.
type OccurrenceReader interface {
	Reader
	Get(ctx context.Context, kind, id string) (*substrate.Record, error)
	List(ctx context.Context, q substrate.Query) (*substrate.Page, error)
}

// Occurrence answers a GET at a computed id: when the kind binds
// `recurring`, the prefix names a live series whose rule produces the slot,
// and no override claims it, the same envelope the window read would have
// served. Otherwise the caller's not-found stands.
func Occurrence(ctx context.Context, ds OccurrenceReader, kind, id string) (*substrate.Record, bool) {
	seriesID, at, ok := splitComputedID(id)
	if !ok {
		return nil, false
	}
	series, err := ds.Get(ctx, kind, seriesID)
	if err != nil || series.DeletedAt != nil {
		return nil, false
	}
	recurring, err := ds.TypesImplementing(ctx, vocabulary.TraitRecurringCore)
	if err != nil {
		return nil, false
	}
	binds := false
	for _, ti := range recurring {
		if ti.Identity == kind {
			binds = true
		}
	}
	if !binds {
		return nil, false
	}
	rule, slotName, empty := ruleOf(series)
	if empty {
		return nil, false
	}
	exp, err := occurrence.Expand(rule, at, at.Add(time.Microsecond), 1)
	if err != nil || len(exp.Times) == 0 {
		return nil, false
	}
	path := vocabulary.RecordPath(series.Kind, series.ID)
	// An override at this slot, of any kind, supersedes the computed one.
	claimed, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Implements: vocabulary.TraitOverrideCore,
		Properties: map[string]substrate.Cond{
			vocabulary.PropRecurrenceOf: {Eq: path},
			vocabulary.PropOriginalAt:   {Eq: at.UTC().Format(time.RFC3339Nano)},
		},
	}, First: 50})
	if err != nil && !errors.Is(err, substrate.ErrValidation) {
		return nil, false
	}
	if claimed != nil {
		// Judged on the rows themselves, not on the filter having matched:
		// the engine's filter is exact, and a reader that is not (a fake, a
		// row written before 0044 holding a bare path) still answers right.
		for _, o := range claimed.Records {
			slot, ok := PropInstant(o.Properties, vocabulary.PropOriginalAt)
			if !ok || !slot.Equal(at) {
				continue
			}
			for _, p := range ReferencePaths(o.Properties[vocabulary.PropRecurrenceOf]) {
				if p == path {
					return nil, false
				}
			}
		}
	}
	overrides, err := overrideKinds(ctx, ds)
	if err != nil {
		return nil, false
	}
	return computedRecord(series, rule, slotName, at, path, overrides[kind]), true
}

// --- property readers ------------------------------------------------------

func propString(props map[string]any, key string) string {
	s, _ := props[key].(string)
	return s
}

// PropInstant reads an RFC 3339 instant property, in UTC.
func PropInstant(props map[string]any, key string) (time.Time, bool) {
	s, _ := props[key].(string)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func propInstants(props map[string]any, key string) []time.Time {
	list, _ := props[key].([]any)
	var out []time.Time
	for _, v := range list {
		s, _ := v.(string)
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			out = append(out, t.UTC())
		}
	}
	return out
}

// ReferencePaths reads the record paths a property value holds. A reference is
// SERVED as an object carrying the path under the reserved `ref` key, and a
// repeated one as a list of those; the bare string arm is what a row written
// before decision 0044 still holds.
func ReferencePaths(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case map[string]any:
		if s, ok := v[vocabulary.ReferenceValueKey].(string); ok {
			return []string{s}
		}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, ReferencePaths(item)...)
		}
		return out
	}
	return nil
}
