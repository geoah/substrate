package engine

import (
	"encoding/json"
	"log/slog"
	"reflect"
	"testing"
	"time"
)

// TestSettleFoldRefusesAFoldWithoutAnEntry: a transaction that changed the fold
// but appended no changelog entry (maxSeq == 0) is a rebuild-divergence crack — the
// records table would hold a change the changelog never reproduces. settleFold must
// REFUSE it (roll the transaction back) rather than warn and commit. The error
// path returns before touching the database, so this holds without one.
func TestSettleFoldRefusesAFoldWithoutAnEntry(t *testing.T) {
	tx := &txn{
		ds:     &dataset{svc: &service{log: slog.Default()}, scope: Scope{Repository: "repo1"}},
		folded: []foldOp{{Kind: foldBump, Ref: "task", ID: "t1"}},
		maxSeq: 0,
	}
	if err := tx.settleFold(); err == nil {
		t.Fatal("settleFold accepted a fold with no changelog entry; it must roll back")
	}
	// The empty case is still fine: nothing folded, nothing to reconcile.
	empty := &txn{ds: &dataset{svc: &service{log: slog.Default()}, scope: Scope{Repository: "repo1"}}}
	if err := empty.settleFold(); err != nil {
		t.Fatalf("settleFold errored on an empty transaction: %v", err)
	}
}

// The fold rests on one invariant: applying an entry's delta to the row the
// writer loaded reproduces the row the writer produced — after a round trip
// through the changelog's jsonb, which is where a delta actually lives. These tests
// hold it without a database, so a shape that cannot survive the wire fails
// here rather than as a mysterious difference in a rebuild.

func TestDeltaRoundTripsThroughTheLog(t *testing.T) {
	at := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name          string
		before, after *erow
	}{
		{
			name: "creation",
			after: &erow{
				ID: "t1", Kind: "task", Title: "Ship it", Body: "prose",
				States: map[string]string{"status": "open"},
				Props:  map[string]any{"description": "a delta", "count": 2.0, "flag": false, "empty": ""},
				Labels: map[string]any{"owner/pinned": true},
				At:     &at, DueAt: &due,
				Finalizers: []string{"substrate.merge"},
			},
		},
		{
			name: "values move, one property goes, a time clears",
			before: &erow{
				ID: "t1", Kind: "task", Title: "Ship it",
				States: map[string]string{"status": "open"},
				Props:  map[string]any{"description": "a delta", "url": "https://example.com"},
				Labels: map[string]any{"owner/pinned": true},
				DueAt:  &due,
			},
			after: &erow{
				ID: "t1", Kind: "task", Title: "Shipped",
				States: map[string]string{"status": "done"},
				Props:  map[string]any{"description": "moved"},
				Labels: map[string]any{"owner/urgent": "yes"},
			},
		},
		{
			name: "a write that changes nothing describes nothing",
			before: &erow{
				ID: "t1", Kind: "task", Title: "Ship it",
				States: map[string]string{"status": "open"},
				Props:  map[string]any{"description": "a delta"},
				Labels: map[string]any{},
			},
			after: &erow{
				ID: "t1", Kind: "task", Title: "Ship it",
				States: map[string]string{"status": "open"},
				Props:  map[string]any{"description": "a delta"},
				Labels: map[string]any{},
			},
		},
		{
			name: "falsey values survive: false, zero and the empty string",
			before: &erow{
				ID: "t1", Kind: "task",
				Props:  map[string]any{"flag": true, "count": 3.0, "note": "x"},
				States: map[string]string{}, Labels: map[string]any{},
			},
			after: &erow{
				ID: "t1", Kind: "task", Title: "",
				Props:  map[string]any{"flag": false, "count": 0.0, "note": ""},
				States: map[string]string{}, Labels: map[string]any{},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delta := diffRow(tc.before, tc.after)
			// Through the changelog and back: the payload is jsonb, so anything a
			// delta cannot encode is a value the rebuild would lose.
			raw, err := json.Marshal(delta)
			if err != nil {
				t.Fatalf("marshal the delta: %v", err)
			}
			var wire rowDelta
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatalf("unmarshal the delta: %v", err)
			}

			got := tc.before.clone()
			if got == nil {
				got = &erow{ID: tc.after.ID, Kind: tc.after.Kind}
			}
			wire.applyTo(got)

			want := tc.after.clone()
			// The fold decides these three, not the delta.
			got.Version, got.CreatedAt, got.UpdatedAt = 0, time.Time{}, time.Time{}
			want.Version, want.CreatedAt, want.UpdatedAt = 0, time.Time{}, time.Time{}
			normalizeRow(got)
			normalizeRow(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("the delta did not reproduce the row\ngot  %+v\nwant %+v\ndelta %s", got, want, raw)
			}
		})
	}
}

// TestUnchangedRowDescribesNothing: a no-op write must produce an EMPTY delta,
// or the changelog would carry a change nobody made and the `q` search box would
// match rows that never moved.
func TestUnchangedRowDescribesNothing(t *testing.T) {
	row := &erow{
		ID: "t1", Kind: "task", Title: "Ship it",
		Props: map[string]any{"description": "a delta"}, States: map[string]string{"status": "open"},
		Labels: map[string]any{},
	}
	d := diffRow(row.clone(), row)
	if d.Created || d.Set != nil || d.Del != nil || d.Title != nil || d.Body != nil ||
		d.At != nil || d.EndsAt != nil || d.DueAt != nil ||
		d.States != nil || d.Labels != nil || d.Finalizers != nil {
		raw, _ := json.Marshal(d)
		t.Fatalf("an unchanged row described a change: %s", raw)
	}
}

// TestAnnotationValueSurvivesFalse: an annotation may legitimately be `false`,
// and an absent value on the wire means the DELETION — so the two must not
// encode alike.
func TestAnnotationValueSurvivesFalse(t *testing.T) {
	var v any = false
	set := foldOp{Kind: foldAnnotation, Ref: "task", ID: "t1", Key: "owner/seen", Value: &v}
	cleared := foldOp{Kind: foldAnnotation, Ref: "task", ID: "t1", Key: "owner/seen"}

	var back [2]foldOp
	for i, op := range []foldOp{set, cleared} {
		raw, err := json.Marshal(op)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := json.Unmarshal(raw, &back[i]); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
	}
	if back[0].Value == nil || *back[0].Value != false {
		t.Fatalf("an annotation set to false came back as %v", back[0].Value)
	}
	if back[1].Value != nil {
		t.Fatalf("a cleared annotation came back carrying %v", *back[1].Value)
	}
}

// normalizeRow flattens the nil/empty distinctions the fold does not preserve:
// a row read back from storage always has the three maps, never nil.
func normalizeRow(r *erow) {
	if r.States == nil {
		r.States = map[string]string{}
	}
	if r.Props == nil {
		r.Props = map[string]any{}
	}
	if r.Labels == nil {
		r.Labels = map[string]any{}
	}
	if r.Finalizers == nil {
		r.Finalizers = []string{}
	}
}
