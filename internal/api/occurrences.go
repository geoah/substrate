package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/occurrence"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The occurrences read (decision 0043): the one place a stored recurrence
// rule turns into instants. It is served here, in the API layer, and never by
// the engine — the fold and the generic records query stay expander-free —
// and it writes nothing: a computed occurrence has no id and no changelog
// entry.

const (
	traitRecurring     = "samples.substrate.reamde.dev/scheduling/recurring"
	traitOccurrencelog = "samples.substrate.reamde.dev/scheduling/occurrencelog"
)

var occurrenceParams = []string{"from", "to", "limit"}

const (
	occurrenceDefaultLimit = 1000
	occurrenceMaxLimit     = 10000
	occurrencePageSize     = 500
)

func (h *handler) getOccurrences(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if bad := unsupportedParam(r, occurrenceParams...); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	from, err := instantParam(r, "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	to, err := instantParam(r, "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, codeBadRequest, "from must be before to")
		return
	}
	limit := occurrenceDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > occurrenceMaxLimit {
			writeError(w, http.StatusBadRequest, codeBadRequest,
				fmt.Sprintf("limit must be an integer between 1 and %d", occurrenceMaxLimit))
			return
		}
		limit = n
	}

	ds := DatasetFrom(ctx)
	// The trait is the contract: a new recurring kind is covered the day it
	// binds, with no per-kind list here to forget to extend.
	rules, err := listWhole(ctx, ds, substrate.Query{
		Filter: substrate.Filter{Implements: traitRecurring},
		First:  occurrencePageSize,
	})
	if err != nil {
		// A repository that never imported the scheduling vocabulary has no
		// implementors and the engine says so as a validation error; here
		// that means nothing recurs, which is an empty answer, not a 422.
		if errors.Is(err, substrate.ErrValidation) {
			writeJSON(w, http.StatusOK, substrate.OccurrenceList{Occurrences: []substrate.Occurrence{}})
			return
		}
		writeSubstrateError(w, err)
		return
	}
	// The logs annotate, never suppress: a logged dose still appears, marked.
	// scheduledAt is the occurrencelog trait's slot property, so the window
	// filter is the same one the expansion answers.
	logs, err := listWhole(ctx, ds, substrate.Query{
		Filter: substrate.Filter{
			Implements: traitOccurrencelog,
			Properties: map[string]substrate.Cond{"scheduledAt": {
				Gte: from.UTC().Format(time.RFC3339Nano),
				Lt:  to.UTC().Format(time.RFC3339Nano),
			}},
		},
		First: occurrencePageSize,
	})
	if err != nil && !errors.Is(err, substrate.ErrValidation) {
		writeSubstrateError(w, err)
		return
	}
	marks := indexLogs(logs)

	out := substrate.OccurrenceList{Occurrences: []substrate.Occurrence{}}
	for _, rec := range rules {
		rule, empty := ruleOf(rec.Properties)
		if empty {
			continue // an as-needed schedule: no rule, no rdates, no slots
		}
		// limit+1 so a single over-full rule still reports the cut.
		exp, err := occurrence.Expand(rule, from, to, limit+1)
		if err != nil {
			out.Problems = append(out.Problems, substrate.OccurrenceProblem{
				Kind: rec.Kind, ID: rec.ID, Message: err.Error(),
			})
			continue
		}
		out.Truncated = out.Truncated || exp.Truncated
		for _, t := range exp.Times {
			out.Occurrences = append(out.Occurrences, substrate.Occurrence{
				Kind: rec.Kind, ID: rec.ID, Title: rec.Title, At: t,
				Log: marks[markKey(rec.Kind, rec.ID, t)],
			})
		}
	}
	sort.Slice(out.Occurrences, func(i, j int) bool {
		a, b := out.Occurrences[i], out.Occurrences[j]
		if !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	if len(out.Occurrences) > limit {
		out.Occurrences = out.Occurrences[:limit]
		out.Truncated = true
	}
	writeJSON(w, http.StatusOK, out)
}

// ruleOf reads the recurring trait's read-side contract off a record's
// properties. The anchor is startsAt, else at (a temporal(range) kind seeds
// its rule from its own start); [materializedFrom, materializedUntil) is the
// span whose occurrences already exist as records.
func ruleOf(props map[string]any) (occurrence.Rule, bool) {
	rule := occurrence.Rule{
		Recurrence: propString(props, "recurrence"),
		Timezone:   propString(props, "timezone"),
		RDates:     propInstants(props, "rdates"),
		ExDates:    propInstants(props, "exdates"),
	}
	rule.StartsAt, _ = propInstant(props, "startsAt")
	if rule.StartsAt.IsZero() {
		rule.StartsAt, _ = propInstant(props, "at")
	}
	rule.MaterializedFrom, _ = propInstant(props, "materializedFrom")
	rule.MaterializedUntil, _ = propInstant(props, "materializedUntil")
	return rule, rule.Recurrence == "" && len(rule.RDates) == 0
}

// indexLogs keys every occurrencelog row by the recurring record its reference
// names and the slot its scheduledAt answers. The property's name is each
// kind's own (schedule, routine, task), so every reference the log carries is
// keyed: identity is the (kind, id) pair, and the slot pins the rest.
func indexLogs(logs []*substrate.Record) map[string]*substrate.OccurrenceLog {
	marks := make(map[string]*substrate.OccurrenceLog)
	for _, l := range logs {
		at, ok := propInstant(l.Properties, "scheduledAt")
		if !ok {
			continue
		}
		mark := &substrate.OccurrenceLog{Kind: l.Kind, ID: l.ID, Status: propString(l.Properties, "status")}
		for _, v := range l.Properties {
			for _, path := range referencePaths(v) {
				if kind, id, ok := vocabulary.SplitRecordPath(path); ok {
					marks[markKey(kind, id, at)] = mark
				}
			}
		}
	}
	return marks
}

// referencePaths reads the record paths a property value holds. A reference is
// SERVED as an object carrying the path under the reserved `ref` key, and a
// repeated one as a list of those. The bare string arm stays because a reader
// never picks its parse from the declaration (engine/refs.go states the rule):
// it is what a row written before decision 0044 still holds, and what an
// unnormalized caller may hand over. A value that is neither is not a reference
// and yields nothing. The kind declaration is not on this side of the seam, so
// the VALUE's shape is what says whether it points anywhere.
func referencePaths(v any) []string {
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
			out = append(out, referencePaths(item)...)
		}
		return out
	}
	return nil
}

func markKey(kind, id string, at time.Time) string {
	return kind + "|" + id + "|" + strconv.FormatInt(at.UnixNano(), 10)
}

// listWhole pages a filter to exhaustion. The recurring records and one
// window's logs are both small sets; the page size only bounds each read.
func listWhole(ctx context.Context, ds substrate.Dataset, q substrate.Query) ([]*substrate.Record, error) {
	var out []*substrate.Record
	for {
		page, err := ds.List(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Records...)
		if page.Cursor == "" {
			return out, nil
		}
		q.After = page.Cursor
	}
}

func instantParam(r *http.Request, name string) (time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return time.Time{}, fmt.Errorf("%s is required (RFC 3339)", name)
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC 3339 instant", name)
	}
	return t, nil
}

func propString(props map[string]any, key string) string {
	s, _ := props[key].(string)
	return s
}

func propInstant(props map[string]any, key string) (time.Time, bool) {
	s, _ := props[key].(string)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func propInstants(props map[string]any, key string) []time.Time {
	raw, _ := props[key].([]any)
	var out []time.Time
	for _, v := range raw {
		s, _ := v.(string)
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			out = append(out, t)
		}
	}
	return out
}
