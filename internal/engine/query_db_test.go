package engine_test

// The query grammar over the shipped sample vocabulary: which filters a List
// takes (kind, reference, label, enum, state, id), how it orders, how a trait
// filter reaches across authorities, and where a page ends. Then the keyset
// walk (each row once, the head it carries, a total order across kinds) and
// the orderings, which sort in the property's DECLARED type rather than as the
// text `props->>` hands back.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// One conversation as a client renders it: a kind filter with a reference
// predicate, ordered; then the temporal trait range, the label filter, the
// enum and state filters, the id filter, and two pages.
func TestQueryGrammarFiltersOrdersAndPages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newVocabularyDataset(t, "messaging")
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "beeper-account:a",
		Properties: map[string]any{"provider": "beeper", "label": "Personal"},
	})
	conv := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:x1",
		Properties: map[string]any{"category": "direct", "account": enginetest.AccountType + "/" + acc.ID},
	})
	other := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:x2",
		Properties: map[string]any{"category": "group", "name": "Family", "account": enginetest.AccountType + "/" + acc.ID},
	})
	alex := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex"},
	})
	var msgs []*substrate.Record
	for i, at := range []string{"2026-08-01T10:00:00Z", "2026-08-02T10:00:00Z", "2026-08-03T10:00:00Z"} {
		m := mustPut(t, ds, beeper, substrate.PutInput{
			Kind: "conversationmessage",
			ID:   extID("slack.msg", string(rune('a'+i))+"1"),
			Properties: map[string]any{
				"at": at, "text": "message " + string(rune('a'+i)),
				"conversation": conv.ID,
				"author":       alex.ID,
			},
		})
		msgs = append(msgs, m)
	}
	mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack-msg:z1",
		Properties: map[string]any{
			"at": "2026-08-04T10:00:00Z", "text": "elsewhere",
			"conversation": other.ID,
			"author":       alex.ID,
		},
	})
	mustPatch(t, ds, owner, msgs[0].Kind, msgs[0].ID, substrate.PatchInput{Labels: map[string]any{"owner/seen": true}})

	// Kind + reference predicate, newest first.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{"conversationmessage"},
			Properties: map[string]substrate.Cond{"conversation": {Eq: conv.ID}},
		},
		OrderBy: []substrate.Order{{Property: "at", Desc: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 3 || got[0] != msgs[2].ID || got[2] != msgs[0].ID {
		t.Fatalf("ordered reference query = %v", got)
	}
	if refPathValue(page.Records[0], "author") != vocabulary.RecordPath(alex.Kind, alex.ID) {
		t.Fatalf("the pointer is not on the listed record: %+v", page.Records[0].Properties)
	}

	// Temporal range, cross-authority via the capability interface.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Implements: "Temporal",
		Properties: map[string]substrate.Cond{
			"at": {Gte: "2026-08-02T00:00:00Z", Lt: "2026-08-04T00:00:00Z"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 2 {
		t.Fatalf("temporal range = %v", got)
	}

	// Labels are first-class filters; annotations are not.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"conversationmessage"}, Labels: map[string]substrate.Cond{"owner/seen": {Eq: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 1 || got[0] != msgs[0].ID {
		t.Fatalf("label filter = %v", got)
	}

	// Pagination.
	first, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{"conversationmessage"},
			Properties: map[string]substrate.Cond{"conversation": {Eq: conv.ID}},
		},
		OrderBy: []substrate.Order{{Property: "at", Desc: true}},
		First:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 2 || first.Cursor == "" {
		t.Fatalf("page 1 = %v cursor %q", ids(first.Records), first.Cursor)
	}
	second, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{"conversationmessage"},
			Properties: map[string]substrate.Cond{"conversation": {Eq: conv.ID}},
		},
		OrderBy: []substrate.Order{{Property: "at", Desc: true}},
		First:   2, After: first.Cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Cursor != "" {
		t.Fatalf("page 2 = %v cursor %q", ids(second.Records), second.Cursor)
	}

	// Props filters and states filters.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{"conversation"},
		Properties: map[string]substrate.Cond{"category": {In: []any{"group", "channel"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 1 || got[0] != other.ID {
		t.Fatalf("enum in filter = %v", got)
	}
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"conversationmessage"}, Properties: map[string]substrate.Cond{"delivery": {Eq: "received"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 4 {
		t.Fatalf("state filter = %v", ids(page.Records))
	}
	// A lookup by the writer's own id is a plain id filter now.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{IDs: []string{conv.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != conv.ID {
		t.Fatalf("id filter = %v", ids(page.Records))
	}
}

// The stability guarantee: a cursor walk sees every row that existed for the
// WHOLE walk exactly once, even when rows are inserted and deleted mid-walk.
// An OFFSET walk would skip a row when one behind the cursor is deleted, or
// repeat one when a newer row is inserted; keyset seeks a position in the
// order, so neither happens.
func TestListKeysetWalkSeesEachRowOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// 17 rows up front — the set that exists for the whole walk (until some
	// are deleted below, which removes them from that set).
	stable := map[string]bool{}
	for i := range 17 {
		p := mustPut(t, ds, owner, substrate.PutInput{
			Kind: "person", Properties: map[string]any{"name": fmt.Sprintf("p%02d", i)},
		})
		stable[p.ID] = true
	}

	seen := map[string]int{}
	after := ""
	churned := false
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("walk did not terminate")
		}
		page, err := ds.List(ctx, substrate.Query{
			Filter: substrate.Filter{Kinds: []string{"person"}},
			First:  4,
			After:  after,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, e := range page.Records {
			seen[e.ID]++
		}
		// After the first page, churn the collection: delete three of the
		// original rows (seen or not) and insert four new ones.
		if !churned {
			churned = true
			del := 0
			for id := range stable {
				if del >= 3 {
					break
				}
				if _, err := ds.Delete(ctx, owner, "person", id, substrate.DeleteInput{}); err != nil {
					t.Fatalf("delete: %v", err)
				}
				delete(stable, id) // no longer exists for the whole walk
				del++
			}
			for i := range 4 {
				mustPut(t, ds, owner, substrate.PutInput{
					Kind: "person", Properties: map[string]any{"name": fmt.Sprintf("n%02d", i)},
				})
			}
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}

	for id := range stable {
		if seen[id] != 1 {
			t.Fatalf("stable row %s seen %d times, want exactly 1", id, seen[id])
		}
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("row %s seen %d times (a duplicate)", id, n)
		}
	}
}

// The list→watch handoff: every listed row's change is at or before the
// page's head seq, and a write made AFTER the list resumes at seq > head, so
// `watch?from=head` replays exactly the writes the list did not see, with no
// gap and no double-see.
func TestListCarriesHeadForGaplessWatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	for i := range 3 {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: "person", Properties: map[string]any{"name": fmt.Sprintf("p%d", i)},
		})
	}

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"person"}}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Head <= 0 {
		t.Fatalf("head = %d, want > 0", page.Head)
	}
	listed := map[string]bool{}
	for _, e := range page.Records {
		listed[e.ID] = true
	}

	// Every listed row's creating change is at or before head (no listed row
	// hides beyond the resume point).
	all, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 1000)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	for _, c := range all {
		if listed[c.RecordID] && c.Seq > page.Head {
			t.Fatalf("listed row %s has change seq %d beyond head %d", c.RecordID, c.Seq, page.Head)
		}
	}

	// A write after the list resumes strictly past head.
	late := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "late"},
	})
	resumed, err := ds.Changes(ctx, page.Head, substrate.ChangeFilter{}, 1000)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	sawLate := false
	for _, c := range resumed {
		if c.Seq <= page.Head {
			t.Fatalf("resume from head saw seq %d <= head %d (a double-see)", c.Seq, page.Head)
		}
		if c.RecordID == late.ID {
			sawLate = true
		}
	}
	if !sawLate {
		t.Fatalf("resume from head missed the late write %s (a gap)", late.ID)
	}
}

// A cross-kind list whose two rows share an id AND an equal sort value
// paginates through BOTH: the (kind, id) tiebreak makes the keyset order
// strictly total, so a page boundary neither skips nor duplicates. With an
// id-only tiebreak the second page's seek (id > "dup") admitted nothing and
// one row was lost.
func TestListKeysetOrderIsTotalAcrossKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// Two DIFFERENT kinds, the SAME id, the SAME title (the sort key).
	mustPut(t, ds, owner, substrate.PutInput{Kind: "person", ID: "dup", Properties: map[string]any{"title": "same"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", ID: "dup", Properties: map[string]any{"title": "same"}})

	q := substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{"person", "organization"}},
		OrderBy: []substrate.Order{{Property: "title"}},
		First:   1,
	}
	seen := map[string]bool{}
	for range 5 {
		page, err := ds.List(ctx, q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, e := range page.Records {
			key := e.Kind + "/" + e.ID
			if seen[key] {
				t.Fatalf("duplicate at a page boundary: %s", key)
			}
			seen[key] = true
		}
		if page.Cursor == "" {
			break
		}
		q.After = page.Cursor
	}
	if len(seen) != 2 {
		t.Fatalf("the paged cross-kind walk saw %d of 2 records (a page boundary skipped one): %v", len(seen), seen)
	}
}

// Every page of a cursor walk reports the FIRST page's changelog head, so the
// list→watch handoff pins the snapshot the walk began at. A write that lands
// mid-walk bumps the changelog head, but the later page still reports the
// original head: the watch replays that write, it is not lost.
func TestListReportsTheFirstPagesHeadOnEveryPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	for range 5 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "p"}})
	}
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{"person"}}, First: 2}
	page1, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if page1.Head == 0 || page1.Cursor == "" {
		t.Fatalf("page 1 head=%d cursor=%q — want a non-zero head and a continuation", page1.Head, page1.Cursor)
	}

	// A write lands BETWEEN the pages, bumping the changelog head.
	mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "mid-walk"}})

	q.After = page1.Cursor
	page2, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if page2.Head != page1.Head {
		t.Fatalf("page 2 head = %d, want the first page's head %d — a mid-walk insert would be lost from the list→watch handoff", page2.Head, page1.Head)
	}
}

// An empty collection answers `"records": []`, never `null`: the wire promises
// an array, the console's Page type is written to it, and a nil slice would
// serialize as the one value that type cannot hold.
func TestListEmptyCollectionSerializesAnArray(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"samples.substrate.reamde.dev/tasks/task"},
	}})
	if err != nil {
		t.Fatalf("list an empty collection: %v", err)
	}
	if page.Records == nil {
		t.Fatalf("an empty page carries a nil Records slice; the wire needs []")
	}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"records":[]`) {
		t.Fatalf("an empty page serialized as %s, want \"records\":[]", body)
	}
}

const orderPackage = "order.example.substrate.reamde.dev/order"

// firedAtChronological is the chronological order of the `ping` rows, which
// differs from their text order (.25Z, .5Z, 00Z, 01Z) at the whole-second
// row: validate.go normalizes every write to one UTC RFC 3339 layout, but
// RFC3339Nano trims trailing zeros in the fraction, so "09:00:00.5Z" lands
// before "09:00:00Z" as text ('.' is 0x2E, 'Z' is 0x5A) while the
// half-second instant comes after.
var firedAtChronological = []string{
	"2026-08-15T09:00:00Z",
	"2026-08-15T09:00:00.25Z",
	"2026-08-15T09:00:00.5Z",
	"2026-08-15T09:00:01Z",
}

// The filter half (condJSON) has always cast a datetime to timestamptz; the
// order applies the same cast, and the order expression is also the projected
// cursor key, so the keyset walk is exercised across the boundary a text sort
// gets wrong rather than only on the first page.
//
// installOrderedKinds declares one kind per ordered property type and seeds
// each with a set whose TEXT order differs from its natural one, inserted
// scrambled so neither insertion order nor created_at can fake an assertion.
// Every ordering case only reads these rows, so they share the one fixture.
func installOrderedKinds(t *testing.T) substrate.Dataset {
	t.Helper()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(orderPackage, 0),
		vocabulary.KindManifest(orderPackage,
			map[string]any{"singular": "step"},
			map[string]any{"properties": map[string]any{
				"turn": map[string]any{"type": "int"},
			}}),
		vocabulary.KindManifest(orderPackage,
			map[string]any{"singular": "price"},
			map[string]any{"properties": map[string]any{
				"amount": map[string]any{"type": "decimal"},
			}}),
		vocabulary.KindManifest(orderPackage,
			map[string]any{"singular": "label"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			}}),
		vocabulary.KindManifest(orderPackage,
			map[string]any{"singular": "ping"},
			map[string]any{"properties": map[string]any{
				"firedAt": map[string]any{"type": "datetime"},
			}}),
	}); err != nil {
		t.Fatalf("install the ordered kinds: %v", err)
	}
	// Ints spanning the boundary a text sort gets wrong: 0, 1, 10, 11, 2.
	for _, n := range []int{100, 0, 10, 2, 20, 1, 11, 9} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: orderPackage + "/step", Properties: map[string]any{"turn": n},
		})
	}
	// Decimals are CARRIED as exact digits and compared as numbers.
	for _, amount := range []string{"10.05", "0.99", "100.1", "9.50", "2"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: orderPackage + "/price", Properties: map[string]any{"amount": amount},
		})
	}
	// A numeric-LOOKING string is still a string: casting it would be the
	// same silent reinterpretation, in the other direction.
	for _, name := range []string{"10", "9", "2"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: orderPackage + "/label", Properties: map[string]any{"name": name},
		})
	}
	for _, at := range []string{
		firedAtChronological[2], firedAtChronological[0],
		firedAtChronological[3], firedAtChronological[1],
	} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: orderPackage + "/ping", Properties: map[string]any{"firedAt": at},
		})
	}
	return ds
}

// listedValues lists one property off every row a query returned, rendered as
// text so one table can hold an int, a decimal, a string and an instant.
func listedValues(t *testing.T, ds substrate.Dataset, q substrate.Query, property string) []string {
	t.Helper()
	page, err := ds.List(context.Background(), q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		out = append(out, fmt.Sprintf("%v", r.Properties[property]))
	}
	return out
}

// An order by a declared property sorts in that property's own type, never as
// the text `props->>` hands back: an int-typed property used to sort 0, 1, 10,
// 11, 2, which is not an ordering anyone asked for and said nothing about it.
func TestOrderByAPropertySortsInItsDeclaredType(t *testing.T) {
	t.Parallel()
	ds := installOrderedKinds(t)
	for _, tc := range []struct {
		name     string
		kind     string
		property string
		want     []string
	}{
		{"an int sorts numerically", "step", "turn", []string{"0", "1", "2", "9", "10", "11", "20", "100"}},
		{"a decimal sorts numerically", "price", "amount", []string{"0.99", "2", "9.50", "10.05", "100.1"}},
		{"a string stays textual", "label", "name", []string{"10", "2", "9"}},
		{"a datetime sorts chronologically", "ping", "firedAt", firedAtChronological},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := listedValues(t, ds, substrate.Query{
				Filter:  substrate.Filter{Kinds: []string{orderPackage + "/" + tc.kind}},
				OrderBy: []substrate.Order{{Property: tc.property}},
				First:   50,
			}, tc.property)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d rows %v, want %d", len(got), got, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("ordered %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// A decimal range bound rides to SQL as its own digits rather than through a
// float64, so the filter compares the same numbers the order does.
func TestFilterByADecimalRangeComparesNumerically(t *testing.T) {
	t.Parallel()
	ds := installOrderedKinds(t)
	got := listedValues(t, ds, substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{orderPackage + "/price"},
			Properties: map[string]substrate.Cond{"amount": {Gt: "9.50", Lte: "100.10"}},
		},
		OrderBy: []substrate.Order{{Property: "amount"}},
		First:   50,
	}, "amount")
	want := []string{"10.05", "100.1"}
	if len(got) != len(want) {
		t.Fatalf("filtered to %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filtered to %v, want %v", got, want)
		}
	}
}

// The round trip the datetime cast has to survive: the order expression is
// projected as the cursor key (a timestamptz rendered to text), passed back as
// a bound parameter and compared against the native expression in
// seekPredicate. One-row pages put a cursor at every boundary, including the
// whole-second/fractional one the text sort got wrong.
func TestKeysetWalkOverADatetimeOrderPagesAcrossPrecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installOrderedKinds(t)

	var got []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("walk did not terminate")
		}
		page, err := ds.List(ctx, substrate.Query{
			Filter:  substrate.Filter{Kinds: []string{orderPackage + "/ping"}},
			OrderBy: []substrate.Order{{Property: "firedAt"}},
			First:   1,
			After:   after,
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, r := range page.Records {
			s, _ := r.Properties["firedAt"].(string)
			got = append(got, s)
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	if len(got) != len(firedAtChronological) {
		t.Fatalf("walk saw %d rows %v, want %d", len(got), got, len(firedAtChronological))
	}
	for i := range firedAtChronological {
		if got[i] != firedAtChronological[i] {
			t.Fatalf("walked %v, want %v", got, firedAtChronological)
		}
	}
}

// A filter NARROWS. `kinds` and `implements` intersect, never union: a
// COLLECTION read forces the kind from the path, so a union let
// `/tasks?filter={"implements":"temporal"}` answer with every temporal row in
// the repository — transcripts, calendar events, rows of kinds the caller
// never addressed. The three cases only read the seeded rows, so they share
// one fixture.
func TestListIntersectsKindsAndImplements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newVocabularyDataset(t, "tasks", "calendar")

	const taskKind = "samples.substrate.reamde.dev/tasks/task"
	const personKind = "samples.substrate.reamde.dev/people/person"
	task := mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: taskKind,
		Properties: map[string]any{
			"title": "send the rack layout", "dueAt": "2026-08-04T09:00:00Z",
		},
	})
	transcript := mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/calendar/transcript",
		Properties: map[string]any{
			"title": "the standup", "text": "…",
			"at": "2026-08-03T09:00:00Z", "endsAt": "2026-08-03T09:30:00Z",
		},
	})
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: personKind, Properties: map[string]any{"name": "Ada"},
	})

	t.Run("a collection read answers nothing outside itself", func(t *testing.T) {
		t.Parallel()
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds: []string{taskKind}, Implements: "temporal",
		}})
		if err != nil {
			t.Fatalf("collection read with implements: %v", err)
		}
		if len(page.Records) != 1 || page.Records[0].ID != task.ID {
			t.Fatalf("collection read returned %v, want only the task %s", ids(page.Records), task.ID)
		}
		for _, e := range page.Records {
			if e.Kind != taskKind {
				t.Fatalf("collection read returned a %s — a collection never answers outside itself", e.Kind)
			}
		}
	})

	t.Run("a repository-wide read answers every implementor", func(t *testing.T) {
		t.Parallel()
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Implements: "temporal"}})
		if err != nil {
			t.Fatalf("repository-wide implements read: %v", err)
		}
		got := map[string]bool{}
		for _, e := range page.Records {
			got[e.ID] = true
		}
		if !got[task.ID] || !got[transcript.ID] {
			t.Fatalf("repository-wide implements read = %v, want both implementors", ids(page.Records))
		}
	})

	// The intersection is named when it is empty: a collection that does not
	// implement the trait is a caller mistake, and an empty page would read as
	// "nothing matched" rather than "nothing could".
	t.Run("a kind that does not implement the trait is refused", func(t *testing.T) {
		t.Parallel()
		_, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds: []string{personKind}, Implements: "temporal",
		}})
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("error = %v, want a validation error naming the mismatch", err)
		}
	})
}
