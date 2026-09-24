package engine

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A trigger's source is compared against the changelog row's kind IDENTITY,
// exactly: a bare word is refused at parse (decision record 0101), a full
// reference fires on its rows, and a reference the repository does not
// declare is kept as written and matches nothing.
func TestTriggerSourceMatchesTheIdentityExactly(t *testing.T) {
	// A bare word is refused at parse (decision record 0101): admitted, it
	// would compare against identities and never fire.
	_, err := parseTrigger("t1", map[string]any{
		"callable": vocabulary.RecordPath(kindFunction, "x.substrate.reamde.dev/x/f"),
		"source": map[string]any{
			"record": map[string]any{"kinds": []any{"task"}, "ops": []any{"create"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `source.record.kinds[0]: "task" is a bare name`) {
		t.Fatalf("a bare source kind: %v, want the refusal", err)
	}
	tr, err := parseTrigger("t1", map[string]any{
		"callable": vocabulary.RecordPath(kindFunction, "x.substrate.reamde.dev/x/f"),
		"source": map[string]any{
			"record": map[string]any{"kinds": []any{"samples.substrate.reamde.dev/tasks/task"}, "ops": []any{"create"}},
		},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !tr.Record.matches("samples.substrate.reamde.dev/tasks/task", "create") {
		t.Fatalf("a trigger on the full kind did not fire for samples.substrate.reamde.dev/tasks/task: %v", tr.Record.Kinds)
	}
	// A kind the repository does not declare is kept as written and matches
	// nothing: the pattern is compared against the identity, exactly.
	tr2, err := parseTrigger("t2", map[string]any{
		"callable": vocabulary.RecordPath(kindFunction, "x.substrate.reamde.dev/x/f"),
		"source": map[string]any{
			"record": map[string]any{"kinds": []any{"x.substrate.reamde.dev/x/nosuchkind"}, "ops": []any{"create"}},
		},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tr2.Record.matches("samples.substrate.reamde.dev/tasks/task", "create") {
		t.Fatal("an unknown kind matched something")
	}
}

// THE ONE RETIRED REFERENCE VALUE. `trigger.callable` was a `{kind, id}` pair
// before a reference became one flat path, and a trigger is a DATA row that
// nothing re-projects. Two things therefore have to hold at once — a stored
// pair still dispatches, and any rewrite of that row canonicalizes it — and
// this pins both, so neither can be dropped as "the old shape".
func TestTriggerCallableReadsTheReleasedPairAndTheFlatPath(t *testing.T) {
	const id = "x.substrate.reamde.dev/x/f"
	source := map[string]any{
		"record": map[string]any{"kinds": []any{"samples.substrate.reamde.dev/tasks/task"}, "ops": []any{"create"}},
	}
	for name, callable := range map[string]any{
		"the flat path":           vocabulary.RecordPath(kindFunction, id),
		"the released {kind, id}": map[string]any{"kind": kindFunction, "id": id},
	} {
		t.Run(name, func(t *testing.T) {
			tr, err := parseTrigger("t1", map[string]any{"callable": callable, "source": source})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if tr.CallableKind != callableKindFunction || tr.CallableID != id {
				t.Fatalf("callable = %s/%s, want function/%s", tr.CallableKind, tr.CallableID, id)
			}
		})
	}

	// The WRITE half: the pair is the retired shape, refused by name, because
	// nothing migrates a stored one and nothing may author a new one.
	pin := &vocabulary.Property{Datatype: vocabulary.DatatypeReference, To: kindFunction}
	_, err := coerceReference(pin, map[string]any{"kind": kindFunction, "id": id})
	if err == nil {
		t.Fatal("the retired {kind, id} pair must be refused at the write door")
	}
	for _, want := range []string{"retired", "migrates"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
}

// TestCoalesceChangesKeysByKindAndID: trigger coalescing keys pending changes
// by the FULL (kind, id) identity, not the bare id. Two matched kinds that
// happen to share an id are two distinct records — keying on id alone dropped
// one delivery while the cursor advanced past it.
func TestCoalesceChangesKeysByKindAndID(t *testing.T) {
	// Two changes, identical ids, different kinds: both must survive coalescing.
	both := coalesceChanges([]substrate.Change{
		{Seq: 1, Kind: "a.substrate.reamde.dev/a/widget", RecordID: "dup"},
		{Seq: 2, Kind: "b.substrate.reamde.dev/b/gadget", RecordID: "dup"},
	})
	if len(both) != 2 {
		t.Fatalf("coalesced two distinct (kind, id) records into %d — a delivery was dropped", len(both))
	}

	// Same (kind, id) repeated: coalescing keeps the LAST, as before.
	one := coalesceChanges([]substrate.Change{
		{Seq: 1, Kind: "a.substrate.reamde.dev/a/widget", RecordID: "dup"},
		{Seq: 2, Kind: "a.substrate.reamde.dev/a/widget", RecordID: "dup"},
	})
	if len(one) != 1 || one[0].Seq != 2 {
		t.Fatalf("same-identity coalescing = %+v, want the last (seq 2) only", one)
	}
}
