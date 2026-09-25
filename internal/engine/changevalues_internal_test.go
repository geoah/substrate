package engine

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The walk's refusals to guess, without a database: each case feeds a
// record's earlier entries straight to step.

var cvRef = eref{Kind: "samples.substrate.reamde.dev/tasks/task", ID: "t1"}

func cvKind() *vocabulary.Kind {
	return &vocabulary.Kind{
		Identity:        cvRef.Kind,
		DisplayTemplate: "{name|title}",
		Props: map[string]*vocabulary.Property{
			"name":     {Name: "name", Datatype: "string"},
			"priority": {Name: "priority", Datatype: "enum"},
			"token":    {Name: "token", Datatype: vocabulary.DatatypeSecret},
		},
	}
}

func cvSet(version int64, created bool, set map[string]any, del ...string) []foldOp {
	return []foldOp{{
		Kind: foldRecord, Ref: cvRef.Kind, ID: cvRef.ID,
		Version: json.Number(strconv.FormatInt(version, 10)),
		Delta:   &rowDelta{Created: created, Set: set, Del: del},
	}}
}

// cvWalk owes the befores of one request at seq, for the named properties.
func cvWalk(seq int64, names ...string) (*recordWalk, []*substrate.PropertyChange) {
	w := &recordWalk{ref: cvRef, ty: cvKind(), pending: map[string][]*substrate.PropertyChange{}, page: map[int64][]foldOp{}, below: seq + 1}
	req := valueRequest{seq: seq}
	var out []*substrate.PropertyChange
	for _, n := range names {
		pc := &substrate.PropertyChange{Name: n}
		req.props = append(req.props, pc)
		out = append(out, pc)
	}
	w.requests = []valueRequest{req}
	return w, out
}

func TestTheWalkFindsEachBeforeAndStopsAtTheCreation(t *testing.T) {
	w, pcs := cvWalk(30, "priority", "name")
	w.step([]earlierEntry{
		{seq: 30, ops: cvSet(3, false, map[string]any{"priority": "urgent", "name": "B"})},
		{seq: 20, ops: cvSet(2, false, map[string]any{"priority": "high"})},
		{seq: 10, ops: cvSet(1, true, map[string]any{"priority": "low", "name": "A"})},
	})
	if !w.done || pcs[0].Before != "high" || pcs[1].Before != "A" || pcs[0].BeforeUnknown || pcs[1].BeforeUnknown {
		t.Fatalf("befores = %+v %+v, done %v", pcs[0], pcs[1], w.done)
	}
}

func TestAPropertyTheRecordNeverHeldHasNoBefore(t *testing.T) {
	w, pcs := cvWalk(20, "name")
	w.step([]earlierEntry{
		{seq: 20, ops: cvSet(2, false, map[string]any{"name": "B"})},
		{seq: 10, ops: cvSet(1, true, map[string]any{"priority": "low"})},
	})
	if pcs[0].Before != nil || pcs[0].BeforeUnknown {
		t.Fatalf("before = %+v, want absent and known", pcs[0])
	}
}

func TestAClearedPropertyReadsAbsentBefore(t *testing.T) {
	w, pcs := cvWalk(30, "name")
	w.step([]earlierEntry{
		{seq: 30, ops: cvSet(3, false, map[string]any{"name": "C"})},
		{seq: 20, ops: cvSet(2, false, nil, "name")},
		{seq: 10, ops: cvSet(1, true, map[string]any{"name": "A"})},
	})
	if pcs[0].Before != nil || pcs[0].BeforeUnknown {
		t.Fatalf("before = %+v, want the clear's absence", pcs[0])
	}
}

func TestAVersionGapMakesTheBeforeUnknownRatherThanStale(t *testing.T) {
	// Version 2 rode an entry the walk never read: the value at version 1 is
	// not what the record held before version 3.
	w, pcs := cvWalk(30, "name")
	w.step([]earlierEntry{
		{seq: 30, ops: cvSet(3, false, map[string]any{"name": "C"})},
		{seq: 10, ops: cvSet(1, true, map[string]any{"name": "A"})},
	})
	if !pcs[0].BeforeUnknown || pcs[0].Before != nil {
		t.Fatalf("before = %+v, want unknown", pcs[0])
	}
}

func TestAnEntryWithoutValuesMakesTheBeforeUnknown(t *testing.T) {
	w, pcs := cvWalk(20, "name")
	w.step([]earlierEntry{
		{seq: 20, ops: cvSet(2, false, map[string]any{"name": "B"})},
		{seq: 10, opaque: true},
	})
	if !pcs[0].BeforeUnknown {
		t.Fatalf("before = %+v, want unknown", pcs[0])
	}
}

func TestAHistoryWithoutItsCreationMakesTheBeforeUnknown(t *testing.T) {
	w, pcs := cvWalk(20, "name")
	w.step([]earlierEntry{{seq: 20, ops: cvSet(2, false, map[string]any{"name": "B"})}})
	if !pcs[0].BeforeUnknown || !w.done {
		t.Fatalf("before = %+v done %v, want unknown", pcs[0], w.done)
	}
}

func TestTheWalkStopsAtItsBudget(t *testing.T) {
	w, pcs := cvWalk(10_000, "name")
	for round := 0; !w.done; round++ {
		if round > valuesBudget {
			t.Fatal("the walk never stopped")
		}
		batch := make([]earlierEntry, valuesBatch)
		for i := range batch {
			seq := w.below - 1 - int64(i)
			// Every earlier entry touches another property: the name's
			// previous value lies beyond the budget.
			batch[i] = earlierEntry{seq: seq, ops: cvSet(seq, false, map[string]any{"priority": "p" + strconv.FormatInt(seq, 10)})}
		}
		w.step(batch)
	}
	if !pcs[0].BeforeUnknown || w.read > valuesBudget {
		t.Fatalf("before = %+v after reading %d, want unknown within %d", pcs[0], w.read, valuesBudget)
	}
}

func TestAPageRowAddressedElsewhereJoinsTheWalk(t *testing.T) {
	// Version 2 rode an entry addressed to another record; the page holds it,
	// so the walk reads it in order instead of seeing a gap.
	w, pcs := cvWalk(30, "name")
	w.page[20] = cvSet(2, false, map[string]any{"name": "B"})
	w.step([]earlierEntry{
		{seq: 30, ops: cvSet(3, false, map[string]any{"name": "C"})},
		{seq: 10, ops: cvSet(1, true, map[string]any{"name": "A"})},
	})
	if pcs[0].Before != "B" || pcs[0].BeforeUnknown {
		t.Fatalf("before = %+v, want the page row's value", pcs[0])
	}
}

func TestASecretReadsRedactedOnBothSides(t *testing.T) {
	ty := cvKind()
	set := func(v any) valueAt { return valueAt{value: v, present: true} }
	if got := set("secret:abc").render(ty, "token"); got != Redacted {
		t.Fatalf("secret = %v", got)
	}
	if got := set("").render(ty, "token"); got != "" {
		t.Fatalf("an unset secret = %v, want empty", got)
	}
	if got := set("Ada").render(ty, "name"); got != "Ada" {
		t.Fatalf("plain = %v", got)
	}
}

func TestAValueTheDeclarationNoLongerVouchesForReadsRedacted(t *testing.T) {
	ty := cvKind()
	ty.Props["label"] = &vocabulary.Property{Name: "label", Datatype: "string", RenamedFrom: "caption"}
	set := func(v any) valueAt { return valueAt{value: v, present: true} }
	for _, c := range []struct {
		name string
		v    valueAt
		want any
	}{
		{"dropped", set("anything"), Redacted},
		{"dropped", set([]any{"a"}), Redacted},
		{"caption", set("renamed plain"), "renamed plain"},
		{"name", set("secret:0123"), Redacted},
		{"name", set("9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"), Redacted},
		{substrate.PropAt, valueAt{value: "2026-01-02T03:04:05Z", present: true, column: true}, "2026-01-02T03:04:05Z"},
	} {
		if got := c.v.render(ty, c.name); jsonOfValue(t, got) != jsonOfValue(t, c.want) {
			t.Errorf("%s = %v, want %v", c.name, got, c.want)
		}
	}
}

func jsonOfValue(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestATemplatedTitleIsNotAChangeOfItsOwn(t *testing.T) {
	ty := cvKind()
	rc := composeRecordChange(ty, []foldOp{{
		Kind: foldRecord, Ref: cvRef.Kind, ID: cvRef.ID,
		Delta: &rowDelta{Set: map[string]any{"name": "B"}, Title: ptrTo("B")},
	}}, cvRef)
	if _, derived := rc.moved[substrate.PropTitle]; derived {
		t.Fatalf("a templated title is its own change: %+v", rc.moved)
	}
}
