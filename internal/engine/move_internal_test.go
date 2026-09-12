package engine

// The parts of a kind move that need no store (record 0078): what the plan
// counts as work, what the two doors refuse before any row is read, and the two
// value rewrites. The performed move is internal/testenv's drill; the apply
// door's refusal is move_db_test.go.

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func mvTestKind(identity string, props ...string) *vocabulary.Kind {
	k := &vocabulary.Kind{Identity: identity, Props: map[string]*vocabulary.Property{}}
	for _, p := range props {
		k.PropOrder = append(k.PropOrder, p)
		k.Props[p] = &vocabulary.Property{Name: p}
	}
	return k
}

// THE WORK IS THE UPPER BOUND ON THE ENTRIES (record 0067). Every entry the
// move appends is counted: a put and a delete per carried row, a patch per row
// whose references were deferred, a patch per repointed source, and a patch per
// grant rewritten.
func TestMoveWorkCountsEveryEntryItWillAppend(t *testing.T) {
	to := mvTestKind("ada.example.com/new/gadget", "name")
	move := kindMove{from: "ada.example.com/old/widget", to: to, ids: []string{"a", "b", "c"}}
	planned := movesPlanned{
		moves: []kindMove{move},
		order: []movedRow{
			{move: &move, id: "a"},
			{move: &move, id: "b", deferred: map[string]bool{"parent": true}},
			{move: &move, id: "c", deferred: map[string]bool{"parent": true}},
		},
		repoints: []repoint{{src: eref{Kind: "k", ID: "1"}}, {src: eref{Kind: "k", ID: "2"}}},
	}
	steps, work := countMoves(planned, 4)
	if len(steps) != 1 || steps[0].Step != substrate.StepMove || steps[0].Records != 3 {
		t.Fatalf("steps = %+v, want one move step over 3 records", steps)
	}
	if steps[0].Lossy {
		t.Error("a move loses nothing and must never ask for a confirmation")
	}
	// 3 puts + 3 deletes + 2 deferred patches + 2 repoints + 4 grants.
	if want := int64(3*2 + 2 + 2 + 4); work != want {
		t.Fatalf("work = %d, want %d", work, want)
	}
	// A move with no live row is not a step, and contributes no work.
	empty := kindMove{from: "ada.example.com/old/widget", to: to}
	if steps, work := countMoves(movesPlanned{moves: []kindMove{empty}}, 0); len(steps) != 0 || work != 0 {
		t.Fatalf("an empty move plans %+v at work %d, want nothing", steps, work)
	}
}

// The apply door's whole answer: one refusal per implied move, naming the key.
func TestUserDoorMoveGuardsNameTheKey(t *testing.T) {
	moves := []kindMove{{from: "a.example.com/old/x", to: mvTestKind("a.example.com/new/x")}}
	lines := userDoorMoveGuards(moves)
	if len(lines) != 1 {
		t.Fatalf("lines = %q, want one per implied move", lines)
	}
	if !strings.Contains(lines[0], "movedFrom is honored for the shipped vocabulary only") ||
		!strings.Contains(lines[0], "a.example.com/old/x") {
		t.Fatalf("the refusal does not name the key and the kind: %q", lines[0])
	}
	if got := userDoorMoveGuards(nil); got != nil {
		t.Fatalf("a batch with no move is refused nothing, got %q", got)
	}
}

// What the boot refuses before it reads a row: two moves out of one kind, a
// move whose source is another's destination, and a kind the mapping graph
// touches, whose recompute would mint records under the move's admission
// bypass.
func TestMoveGuardsRefuseTheUnmovable(t *testing.T) {
	reg := vocabulary.NewRegistry()
	old := mvTestKind("a.example.com/old/x", "name")
	if err := reg.Install(&vocabulary.Package{
		Identity: "a.example.com/old", Version: 1,
		Kinds:     map[string]*vocabulary.Kind{"x": old},
		KindOrder: []string{"x"},
	}); err != nil {
		t.Fatalf("install the stored package: %v", err)
	}

	t.Run("two moves out of one kind", func(t *testing.T) {
		lines := moveGuards(reg, []kindMove{
			{from: old.Identity, to: mvTestKind("a.example.com/new/one", "name")},
			{from: old.Identity, to: mvTestKind("a.example.com/new/two", "name")},
		})
		if len(lines) == 0 || !strings.Contains(strings.Join(lines, "; "), "one kind's rows go to one place") {
			t.Fatalf("lines = %q, want the duplicate source refused", lines)
		}
	})

	t.Run("a move onto a moving kind", func(t *testing.T) {
		lines := moveGuards(reg, []kindMove{
			{from: old.Identity, to: mvTestKind("a.example.com/new/one", "name")},
			{from: "a.example.com/new/one", to: mvTestKind("a.example.com/new/two", "name")},
		})
		if len(lines) == 0 || !strings.Contains(strings.Join(lines, "; "), "a move is not a swap") {
			t.Fatalf("lines = %q, want the swap refused", lines)
		}
	})

	t.Run("a subject reference", func(t *testing.T) {
		subjectKind := mvTestKind("a.example.com/old/mapped", "name")
		subjectKind.PropOrder = append(subjectKind.PropOrder, "subject")
		subjectKind.Props["subject"] = &vocabulary.Property{
			Name: "subject", Datatype: vocabulary.DatatypeReference, Subject: true,
		}
		mapped := vocabulary.NewRegistry()
		if err := mapped.Install(&vocabulary.Package{
			Identity: "a.example.com/old", Version: 1,
			Kinds:     map[string]*vocabulary.Kind{"mapped": subjectKind},
			KindOrder: []string{"mapped"},
		}); err != nil {
			t.Fatalf("install: %v", err)
		}
		to := mvTestKind("a.example.com/new/mapped", "name")
		to.PropOrder = append(to.PropOrder, "subject")
		to.Props["subject"] = &vocabulary.Property{
			Name: "subject", Datatype: vocabulary.DatatypeReference, Subject: true,
		}
		lines := moveGuards(mapped, []kindMove{{from: subjectKind.Identity, to: to}})
		if len(lines) == 0 || !strings.Contains(strings.Join(lines, "; "), "subject reference") {
			t.Fatalf("lines = %q, want a mapping source refused", lines)
		}
	})
}

// A reference value follows its kind at any depth and inside any container, and
// a value that was already dangling follows too: the narrowing guard counts
// every reference at the old kind, so one left behind would sit outside the pin
// the same upgrade installs.
func TestRepointValueRewritesEveryReference(t *testing.T) {
	const from, to = "a.example.com/old/x", "a.example.com/new/x"
	in := map[string]any{
		"one":  map[string]any{"ref": from + "/live"},
		"gone": map[string]any{"ref": from + "/dangling"},
		"many": []any{
			map[string]any{"ref": from + "/live"},
			map[string]any{"ref": "other.example.com/k/n"},
		},
		"nested": map[string]any{"deep": map[string]any{"ref": from + "/live"}},
		"plain":  from + "/live",
	}
	out, changed := repointValue(in, from, to, map[string]bool{"live": true})
	if !changed {
		t.Fatal("nothing was rewritten")
	}
	m := out.(map[string]any)
	for _, at := range []string{"one", "gone"} {
		got := m[at].(map[string]any)["ref"]
		if !strings.HasPrefix(got.(string), to+"/") {
			t.Errorf("%s = %v, want it repointed even where the target is gone", at, got)
		}
	}
	if got := m["many"].([]any)[1].(map[string]any)["ref"]; got != "other.example.com/k/n" {
		t.Errorf("a reference at another kind was rewritten: %v", got)
	}
	if got := m["nested"].(map[string]any)["deep"].(map[string]any)["ref"]; got != to+"/live" {
		t.Errorf("a nested reference was missed: %v", got)
	}
	// A bare string is not a reference: only the reserved key is (record 0044).
	if got := m["plain"]; got != from+"/live" {
		t.Errorf("a plain string was rewritten: %v", got)
	}
}

// A grant names a kind in both spellings a stored declaration takes.
func TestReplaceGrantRewritesBothSpellings(t *testing.T) {
	const from, to = "a.example.com/old/x", "a.example.com/new/x"
	in := map[string]any{
		"writes": []any{
			vocabulary.RecordPath(kindKind, from),
			vocabulary.RecordPath(kindKind, "other.example.com/k/n"),
		},
		"reads": map[string]any{"kinds": []any{from, "other.example.com/k/n"}},
	}
	out, changed := replaceGrant(in, from, to)
	if !changed {
		t.Fatal("nothing was rewritten")
	}
	m := out.(map[string]any)
	if got := m["writes"].([]any)[0]; got != vocabulary.RecordPath(kindKind, to) {
		t.Errorf("the stored reference spelling was missed: %v", got)
	}
	if got := m["writes"].([]any)[1]; got != vocabulary.RecordPath(kindKind, "other.example.com/k/n") {
		t.Errorf("another kind's grant was rewritten: %v", got)
	}
	if got := m["reads"].(map[string]any)["kinds"].([]any)[0]; got != to {
		t.Errorf("the bare identity spelling was missed: %v", got)
	}
}
