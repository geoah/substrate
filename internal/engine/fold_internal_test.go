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
				Finalizers:  []string{"substrate.merge"},
				KindVersion: 3,
			},
		},
		{
			name: "values move, one property goes, a time clears, the kind version moves",
			before: &erow{
				ID: "t1", Kind: "task", Title: "Ship it",
				States: map[string]string{"status": "open"},
				Props:  map[string]any{"description": "a delta", "url": "https://example.com"},
				Labels: map[string]any{"owner/pinned": true},
				DueAt:  &due, KindVersion: 3,
			},
			after: &erow{
				ID: "t1", Kind: "task", Title: "Shipped",
				States:      map[string]string{"status": "done"},
				Props:       map[string]any{"description": "moved"},
				Labels:      map[string]any{"owner/urgent": "yes"},
				KindVersion: 4,
			},
		},
		{
			// The clear of a record's last label is the case `omitempty` on a
			// bare map lost: the empty map encoded as nothing and the fold
			// read nothing as "unchanged", so a rebuild restored the label.
			name: "the last label and the last state clear",
			before: &erow{
				ID: "t1", Kind: "task", Title: "Ship it",
				States: map[string]string{"status": "open"},
				Props:  map[string]any{"description": "a delta"},
				Labels: map[string]any{"owner/pinned": true},
			},
			after: &erow{
				ID: "t1", Kind: "task", Title: "Ship it",
				States: map[string]string{},
				Props:  map[string]any{"description": "a delta"},
				Labels: map[string]any{},
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

// TestAClearedMapIsSpelledOnTheWire pins the two spellings the fold tells
// apart: `"labels":{}` is the clear, and no `labels` key is "unchanged". An
// older binary reads both the same way, which is why the change is not a
// dialect bump; history that already holds the bare `{}` a lost clear left
// behind keeps replaying as it did.
func TestAClearedMapIsSpelledOnTheWire(t *testing.T) {
	before := &erow{
		ID: "t1", Kind: "task",
		States: map[string]string{"status": "open"},
		Labels: map[string]any{"owner/pinned": true},
	}
	after := &erow{ID: "t1", Kind: "task", States: map[string]string{}, Labels: map[string]any{}}
	raw, err := json.Marshal(diffRow(before, after))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"states":{},"labels":{}}` {
		t.Fatalf("the clear of the last label and state encodes as %s", raw)
	}

	var unchanged rowDelta
	if err := json.Unmarshal([]byte(`{}`), &unchanged); err != nil {
		t.Fatal(err)
	}
	row := before.clone()
	unchanged.applyTo(row)
	if !reflect.DeepEqual(row.Labels, before.Labels) || !reflect.DeepEqual(row.States, before.States) {
		t.Fatalf("an absent key moved the row: labels %v, states %v", row.Labels, row.States)
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
		d.States != nil || d.Labels != nil || d.Finalizers != nil || d.KindVersion != 0 {
		raw, _ := json.Marshal(d)
		t.Fatalf("an unchanged row described a change: %s", raw)
	}
}

// TestKindVersionIsAValueTheDeltaCarries pins the stamp's three spellings
// (decision 0060). A write under a newer declaration carries `kindVersion` and
// the fold restores it; a delta without the key leaves the row's stamp alone,
// so every entry written before the stamp folds to the column's default of 0;
// and a writer that stamped nothing (0 on `after`) describes no move, so no
// delta ever carries a stamp of 0.
func TestKindVersionIsAValueTheDeltaCarries(t *testing.T) {
	before := &erow{
		ID: "t1", Kind: "task",
		Props: map[string]any{"description": "a delta"}, States: map[string]string{}, Labels: map[string]any{},
		KindVersion: 3,
	}
	after := before.clone()
	after.Props["description"] = "moved"
	after.KindVersion = 4
	raw, err := json.Marshal(diffRow(before, after))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"set":{"description":"moved"},"kindVersion":4}` {
		t.Fatalf("a write under a newer declaration encodes as %s", raw)
	}
	var wire rowDelta
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	row := before.clone()
	wire.applyTo(row)
	if row.KindVersion != 4 {
		t.Fatalf("the fold restored kind version %d, want 4", row.KindVersion)
	}

	// History older than the stamp: the delta names the properties and nothing
	// else, and the row it folds onto is one the column's default left at 0.
	var old rowDelta
	if err := json.Unmarshal([]byte(`{"created":true,"set":{"description":"a delta"}}`), &old); err != nil {
		t.Fatal(err)
	}
	fresh := &erow{ID: "t1", Kind: "task"}
	old.applyTo(fresh)
	if fresh.KindVersion != 0 {
		t.Fatalf("an entry without the key folded to kind version %d, want 0", fresh.KindVersion)
	}

	// The segment file spells the same number `1.8E1` (changelogfile
	// canonicalNumber), and the replay reads the file: the stamp decodes from
	// that spelling exactly, and a lexeme that is not a whole number is refused
	// rather than rounded.
	var filed rowDelta
	if err := json.Unmarshal([]byte(`{"kindVersion":1.8E1}`), &filed); err != nil {
		t.Fatalf("the file's spelling of 18 does not decode: %v", err)
	}
	if filed.KindVersion != 18 {
		t.Fatalf("1.8E1 decoded to kind version %d, want 18", filed.KindVersion)
	}
	if err := json.Unmarshal([]byte(`{"kindVersion":1.5E0}`), &filed); err == nil {
		t.Fatal("a fractional kind version decoded; it must be refused")
	}
	if err := json.Unmarshal([]byte(`{"kindVersion":0}`), &filed); err == nil {
		t.Fatal("a kind version of 0 decoded; a version is at least 1")
	}
	// `null` is absent, as it is for every other delta field: the row's stamp
	// stays and the replay of the entry goes on.
	nulled := before.clone()
	var withNull rowDelta
	if err := json.Unmarshal([]byte(`{"set":{"description":"x"},"kindVersion":null}`), &withNull); err != nil {
		t.Fatalf("a null kind version refused the delta: %v", err)
	}
	withNull.applyTo(nulled)
	if nulled.KindVersion != 3 {
		t.Fatalf("a null kind version moved the stamp to %d, want 3 kept", nulled.KindVersion)
	}

	// An unstamped writer moves a property and leaves the stamp where it was.
	unstamped := before.clone()
	unstamped.Props["description"] = "moved again"
	unstamped.KindVersion = 0
	raw, err = json.Marshal(diffRow(before, unstamped))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"set":{"description":"moved again"}}` {
		t.Fatalf("an unstamped write encodes as %s; a stamp of 0 must never be written", raw)
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
