package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/internal/window"
)

// The fake's Window mirrors the engine's contract (engine/window.go) over the
// in-memory rows: the rows after the key in slot order, every candidate
// series, and the overrides claiming a slot in the window. The traits map says
// which kinds bind temporal, recurring and override, as the registry does.

func (d *fakeDataset) Window(_ context.Context, q substrate.WindowQuery) (*substrate.WindowPage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Window"); err != nil {
		return nil, err
	}
	if !q.From.Before(q.To) {
		return nil, fmt.Errorf("%w: the window is empty", substrate.ErrValidation)
	}
	first := q.First
	if first <= 0 {
		first = 50
	}
	inPlay := d.windowKinds(q.Filter)
	if len(inPlay) == 0 {
		return nil, fmt.Errorf("%w: a window read needs kinds that bind temporal; none of the kinds in play does", substrate.ErrValidation)
	}
	isSeries := func(e *substrate.Record) bool {
		_, r := e.Properties[vocabulary.PropRecurrence]
		list, _ := e.Properties[vocabulary.PropRDates].([]any)
		return r || len(list) > 0
	}
	recurring := map[string]bool{}
	for _, k := range d.traits[vocabulary.TraitRecurringCore] {
		recurring[k] = true
	}
	override := map[string]bool{}
	for _, k := range d.traits[vocabulary.TraitOverrideCore] {
		override[k] = true
	}
	passes := func(e *substrate.Record) bool {
		if e.DeletedAt != nil || !inPlay[e.Kind] {
			return false
		}
		for name, c := range q.Filter.Properties {
			if name == substrate.PropAt {
				continue
			}
			if c.Eq != nil && fmt.Sprint(e.Properties[name]) != fmt.Sprint(c.Eq) {
				return false
			}
		}
		for name, c := range q.Filter.Labels {
			if c.Eq != nil && fmt.Sprint(e.Labels[name]) != fmt.Sprint(c.Eq) {
				return false
			}
		}
		return true
	}
	less := func(a, b substrate.WindowKey) bool {
		if !a.At.Equal(b.At) {
			if q.Desc {
				return a.At.After(b.At)
			}
			return a.At.Before(b.At)
		}
		if a.Kind != b.Kind {
			if q.Desc {
				return a.Kind > b.Kind
			}
			return a.Kind < b.Kind
		}
		if q.Desc {
			return a.ID > b.ID
		}
		return a.ID < b.ID
	}

	page := &substrate.WindowPage{
		Rows: []*substrate.Record{}, Series: []*substrate.Record{}, Overrides: []*substrate.Record{},
		Head: int64(len(d.changes)), Generation: d.generation,
	}
	var rows []*substrate.Record
	var paths []string
	for _, id := range sortedRecordIDs(d.records) {
		e := d.records[id]
		if !passes(e) {
			continue
		}
		if recurring[e.Kind] && isSeries(e) {
			page.Series = append(page.Series, e)
			paths = append(paths, vocabulary.RecordPath(e.Kind, e.ID))
			continue
		}
		k := window.KeyOf(e)
		if k.At.IsZero() || k.At.Before(q.From) || !k.At.Before(q.To) {
			continue
		}
		if q.After != nil && !less(*q.After, k) {
			continue
		}
		rows = append(rows, e)
	}
	sort.SliceStable(rows, func(i, j int) bool { return less(window.KeyOf(rows[i]), window.KeyOf(rows[j])) })
	if len(rows) > first {
		page.More = true
		rows = rows[:first]
	}
	page.Rows = rows
	for _, id := range sortedRecordIDs(d.records) {
		e := d.records[id]
		if e.DeletedAt != nil || !override[e.Kind] {
			continue
		}
		slot, ok := window.PropInstant(e.Properties, vocabulary.PropOriginalAt)
		if !ok || slot.Before(q.From) || !slot.Before(q.To) {
			continue
		}
		for _, p := range window.ReferencePaths(e.Properties[vocabulary.PropRecurrenceOf]) {
			if containsString(paths, p) {
				page.Overrides = append(page.Overrides, e)
				break
			}
		}
	}
	return page, nil
}

// windowKinds narrows the filter to kinds binding temporal, the engine's
// rule: named kinds intersected with implementors, or every implementor.
func (d *fakeDataset) windowKinds(f substrate.Filter) map[string]bool {
	temporal := map[string]bool{}
	for _, k := range d.traits[vocabulary.TraitTemporalCore] {
		temporal[k] = true
	}
	out := map[string]bool{}
	switch {
	case len(f.Kinds) > 0:
		for _, k := range f.Kinds {
			if temporal[k] {
				out[k] = true
			}
		}
	case f.Implements != "":
		want := f.Implements
		if !strings.Contains(want, "/") {
			want = vocabulary.PackageCore + "/" + want
		}
		for _, k := range d.traits[want] {
			if temporal[k] {
				out[k] = true
			}
		}
	default:
		out = temporal
	}
	return out
}

// seedWindowKinds registers the timeline traits on the fake for the kinds
// named, so a window read over them behaves as the engine's would.
func (d *fakeDataset) seedWindowKinds(temporal, recurring, override []string) {
	d.traits[vocabulary.TraitTemporalCore] = append(d.traits[vocabulary.TraitTemporalCore], temporal...)
	d.traits[vocabulary.TraitRecurringCore] = append(d.traits[vocabulary.TraitRecurringCore], recurring...)
	d.traits[vocabulary.TraitOverrideCore] = append(d.traits[vocabulary.TraitOverrideCore], override...)
}

var _ = time.Second
