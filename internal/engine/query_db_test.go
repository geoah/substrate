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
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
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
		Kind: "samples.substrate.reamde.dev/messaging/conversation", ID: "slack-channel:x1",
		Properties: map[string]any{"category": "direct", "account": enginetest.AccountType + "/" + acc.ID},
	})
	other := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/messaging/conversation", ID: "slack-channel:x2",
		Properties: map[string]any{"category": "group", "name": "Family", "account": enginetest.AccountType + "/" + acc.ID},
	})
	alex := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Alex"},
	})
	var msgs []*substrate.Record
	for i, at := range []string{"2026-08-01T10:00:00Z", "2026-08-02T10:00:00Z", "2026-08-03T10:00:00Z"} {
		m := mustPut(t, ds, beeper, substrate.PutInput{
			Kind: "samples.substrate.reamde.dev/messaging/conversationmessage",
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
		Kind: "samples.substrate.reamde.dev/messaging/conversationmessage", ID: "slack-msg:z1",
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
			Kinds:      []string{"samples.substrate.reamde.dev/messaging/conversationmessage"},
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
		Implements: "substrate.reamde.dev/core/temporal",
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
		Kinds: []string{"samples.substrate.reamde.dev/messaging/conversationmessage"}, Labels: map[string]substrate.Cond{"owner/seen": {Eq: true}},
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
			Kinds:      []string{"samples.substrate.reamde.dev/messaging/conversationmessage"},
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
			Kinds:      []string{"samples.substrate.reamde.dev/messaging/conversationmessage"},
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
		Kinds:      []string{"samples.substrate.reamde.dev/messaging/conversation"},
		Properties: map[string]substrate.Cond{"category": {In: []any{"group", "channel"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 1 || got[0] != other.ID {
		t.Fatalf("enum in filter = %v", got)
	}
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"samples.substrate.reamde.dev/messaging/conversationmessage"}, Properties: map[string]substrate.Cond{"delivery": {Eq: "received"}},
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
			Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": fmt.Sprintf("p%02d", i)},
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
			Filter: substrate.Filter{Kinds: []string{"samples.substrate.reamde.dev/people/person"}},
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
				if _, err := ds.Delete(ctx, owner, "samples.substrate.reamde.dev/people/person", id, substrate.DeleteInput{}); err != nil {
					t.Fatalf("delete: %v", err)
				}
				delete(stable, id) // no longer exists for the whole walk
				del++
			}
			for i := range 4 {
				mustPut(t, ds, owner, substrate.PutInput{
					Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": fmt.Sprintf("n%02d", i)},
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
			Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": fmt.Sprintf("p%d", i)},
		})
	}

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"samples.substrate.reamde.dev/people/person"}}})
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
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "late"},
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
	mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", ID: "dup", Properties: map[string]any{"title": "same"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/organization", ID: "dup", Properties: map[string]any{"title": "same"}})

	q := substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{"samples.substrate.reamde.dev/people/person", "samples.substrate.reamde.dev/people/organization"}},
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
		mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "p"}})
	}
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{"samples.substrate.reamde.dev/people/person"}}, First: 2}
	page1, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if page1.Head == 0 || page1.Cursor == "" {
		t.Fatalf("page 1 head=%d cursor=%q — want a non-zero head and a continuation", page1.Head, page1.Cursor)
	}

	// A write lands BETWEEN the pages, bumping the changelog head.
	mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "mid-walk"}})

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

const kindlessPackage = "kindless.example.substrate.reamde.dev/kindless"

// A filter naming no kind compares a typed property by value where every kind
// declaring it agrees, as a quantity's always has: {"gte": "10"} over decimals
// used to compare text, so "9.50" passed and "100.1" did not. Every name is
// one no seeded kind declares, so the kindless read sees these rows alone.
func TestKindlessFilterComparesEveryTypedPropertyByValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	kind := func(name string, props map[string]any) map[string]any {
		return vocabulary.KindManifest(kindlessPackage, map[string]any{"singular": name},
			map[string]any{"properties": props})
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kindlessPackage, 0),
		kind("tally", map[string]any{"tallyCount": map[string]any{"type": "int"}}),
		kind("ledger", map[string]any{"ledgerBalance": map[string]any{"type": "decimal"}}),
		kind("fee", map[string]any{"feeCharge": map[string]any{"type": "money"}}),
		kind("alarm", map[string]any{"alarmAt": map[string]any{"type": "datetime"}}),
		// One name, an int on one kind and a decimal on the other: both numbers.
		kind("gauge", map[string]any{"gaugeReading": map[string]any{"type": "int"}}),
		kind("meter", map[string]any{"gaugeReading": map[string]any{"type": "decimal"}}),
		// One name, an int on one kind and a string on the other: still text.
		kind("badge", map[string]any{"badgeCode": map[string]any{"type": "int"}}),
		kind("sticker", map[string]any{"badgeCode": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("install the kinds: %v", err)
	}
	put := func(kind, property string, values ...any) {
		for _, v := range values {
			mustPut(t, ds, owner, substrate.PutInput{
				Kind: kindlessPackage + "/" + kind, Properties: map[string]any{property: v},
			})
		}
	}
	put("tally", "tallyCount", 9, 100, 2, 10)
	put("ledger", "ledgerBalance", "9.50", "100.1", "2", "10.05")
	put("fee", "feeCharge", moneyValue(950, "EUR"), moneyValue(10010, "EUR"), moneyValue(200, "EUR"), moneyValue(1005, "EUR"), moneyValue(5000, "USD"))
	put("alarm", "alarmAt", firedAtChronological[3], firedAtChronological[0], firedAtChronological[2], firedAtChronological[1])
	put("gauge", "gaugeReading", 9, 100)
	put("meter", "gaugeReading", "2", "10.5")
	put("badge", "badgeCode", 9, 10)
	put("sticker", "badgeCode", "2")

	for _, tc := range []struct {
		name     string
		property string
		cond     substrate.Cond
		want     []string
	}{
		{"an int", "tallyCount", substrate.Cond{Gte: 10}, []string{"10", "100"}},
		{"a decimal", "ledgerBalance", substrate.Cond{Gte: "10"}, []string{"10.05", "100.1"}},
		{"a decimal range", "ledgerBalance", substrate.Cond{Gt: "2", Lt: "10"}, []string{"9.50"}},
		{"money within its currency", "feeCharge", substrate.Cond{Gte: moneyValue(1000, "EUR")}, []string{"10.05 EUR", "100.10 EUR"}},
		{"a datetime", "alarmAt", substrate.Cond{Gte: firedAtChronological[1]}, firedAtChronological[1:]},
		{"an int and a decimal", "gaugeReading", substrate.Cond{Gte: "10"}, []string{"10.5", "100"}},
		{"an int and a string compare as text", "badgeCode", substrate.Cond{Gte: "10"}, []string{"10", "2", "9"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page, err := ds.List(ctx, substrate.Query{
				Filter:  substrate.Filter{Properties: map[string]substrate.Cond{tc.property: tc.cond}},
				OrderBy: []substrate.Order{{Property: tc.property}},
				First:   50,
			})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			got := make([]string, 0, len(page.Records))
			for _, r := range page.Records {
				v := r.Properties[tc.property]
				if m, ok := v.(map[string]any); ok {
					amount, _ := m["amount"].(json.Number)
					if amount == "" {
						amount = json.Number(fmt.Sprint(m["amount"]))
					}
					n, _ := amount.Int64()
					v = fmt.Sprintf("%d.%02d %v", n/100, n%100, m["currency"])
				}
				got = append(got, fmt.Sprint(v))
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("filtered to %v, want %v", got, tc.want)
			}
		})
	}
}

// A kindless read scans rows no loaded declaration speaks for: a package
// quarantined at open is out of the registry, so its kinds cannot vote on a
// name, and its rows still sit in the table. Its "unrated" at a name every
// loaded kind declares an int must not match, and must not fail the cast for
// the whole read, filtered or ordered.
func TestKindlessTypedReadSkipsRowsNoLoadedKindSpeaksFor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := pfOpener(t, dsn, t.TempDir())
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	const parked = "parked.example.substrate.reamde.dev/parked"
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kindlessPackage, 0),
		vocabulary.KindManifest(kindlessPackage, map[string]any{"singular": "review"}, map[string]any{"properties": map[string]any{
			"reviewScore": map[string]any{"type": "int"},
		}}),
		vocabulary.PackageManifest(parked, 0),
		vocabulary.KindManifest(parked, map[string]any{"singular": "critique"}, map[string]any{"properties": map[string]any{
			"reviewScore": map[string]any{"type": "string"},
		}}),
	}); err != nil {
		t.Fatalf("install the kinds: %v", err)
	}
	for _, v := range []any{9, 12} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: kindlessPackage + "/review", Properties: map[string]any{"reviewScore": v}})
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: parked + "/critique", Properties: map[string]any{"reviewScore": "unrated"}})
	// A key no dialect reserves quarantines the package at the next open.
	if _, err := rawDB(t, dsn).ExecContext(ctx,
		`UPDATE records SET props = jsonb_set(props, '{properties,reviewScore,unheardOf}', 'true') WHERE id = $1`, parked+"/critique"); err != nil {
		t.Fatalf("corrupt the stored declaration: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := ds2.Put(ctx, owner, substrate.PutInput{Kind: parked + "/critique", Properties: map[string]any{"reviewScore": "7"}}); err == nil {
		t.Fatal("the corrupted package should be quarantined, its kind out of the registry")
	}
	page, err := ds2.List(ctx, substrate.Query{
		Filter: substrate.Filter{Properties: map[string]substrate.Cond{"reviewScore": {Gte: 10}}},
		First:  50,
	})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(page.Records) != 1 || fmt.Sprint(page.Records[0].Properties["reviewScore"]) != "12" {
		t.Fatalf("filtered to %v, want the one review scoring 12", page.Records)
	}
	page, err = ds2.List(ctx, substrate.Query{
		Filter:  substrate.Filter{Properties: map[string]substrate.Cond{"reviewScore": {Exists: new(true)}}},
		OrderBy: []substrate.Order{{Property: "reviewScore"}},
		First:   50,
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	got := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		got = append(got, fmt.Sprint(r.Properties["reviewScore"]))
	}
	if strings.Join(got, ",") != "9,12,unrated" {
		t.Fatalf("ordered %v, want 9,12 then the row no loaded kind speaks for", got)
	}
}

// A stored date is midnight UTC, the instant ParseInstant reads the same
// spelling as, whatever the session's timezone: cast bare, Postgres read it in
// the session's zone, so under New York "2026-10-24" was 04:00Z and failed a
// bound of 03:00Z.
func TestADateComparesAsMidnightUTCInAnySessionTimezone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	if _, err := rawDB(t, dsn).ExecContext(ctx,
		`DO $$ BEGIN EXECUTE format('ALTER DATABASE %I SET timezone = %L', current_database(), 'America/New_York'); END $$`); err != nil {
		t.Fatalf("set the database's timezone: %v", err)
	}
	svc := pfOpener(t, dsn, t.TempDir())()
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kindlessPackage, 0),
		vocabulary.KindManifest(kindlessPackage, map[string]any{"singular": "deadline"}, map[string]any{"properties": map[string]any{
			"deadlineOn": map[string]any{"type": "date"},
		}}),
	}); err != nil {
		t.Fatalf("install the kind: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: kindlessPackage + "/deadline", Properties: map[string]any{"deadlineOn": "2026-10-24"}})
	for _, kinds := range [][]string{nil, {kindlessPackage + "/deadline"}} {
		for _, cond := range []substrate.Cond{{Lt: "2026-10-24T03:00:00Z"}, {Eq: "2026-10-24T00:00:00Z"}} {
			page, err := ds.List(ctx, substrate.Query{
				Filter: substrate.Filter{Kinds: kinds, Properties: map[string]substrate.Cond{"deadlineOn": cond}},
				First:  50,
			})
			if err != nil {
				t.Fatalf("kinds %v, %+v: %v", kinds, cond, err)
			}
			if len(page.Records) != 1 {
				t.Errorf("kinds %v, %+v matched %d rows, want the date", kinds, cond, len(page.Records))
			}
		}
	}
}

// A money operand on a name the kinds disagree about has nothing to compare
// against: as text it is Go's rendering of a map, and it matched whatever
// sorted after "map[". It is refused the way a quantity's is.
func TestKindlessMoneyOperandIsRefusedWhereTheKindsDisagree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kindlessPackage, 0),
		vocabulary.KindManifest(kindlessPackage, map[string]any{"singular": "fee"}, map[string]any{"properties": map[string]any{
			"feeCharge": map[string]any{"type": "money"},
		}}),
		vocabulary.KindManifest(kindlessPackage, map[string]any{"singular": "memo"}, map[string]any{"properties": map[string]any{
			"feeCharge": map[string]any{"type": "string"},
		}}),
	}); err != nil {
		t.Fatalf("install the kinds: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: kindlessPackage + "/memo", Properties: map[string]any{"feeCharge": "waived"}})
	_, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Properties: map[string]substrate.Cond{"feeCharge": {Gte: moneyValue(1000, "EUR")}}},
		First:  50,
	})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "filter.kinds") {
		t.Fatalf("a money operand over a name the kinds disagree about: %v, want a refusal naming filter.kinds", err)
	}
}

// Every value the write path accepts reaches a typed read whole: an offset
// carries a written 9999 into a five-digit year the timestamptz cast reads,
// and `decimal` is unbounded. Both compare by value, in a read naming the kind
// and in one naming none. An offset also carried a written 0001 into year 0,
// which the cast refuses, before the write path held the UTC year to the
// range; a row stored then reads as no instant (out of every range, last in
// an ascending order) instead of failing the read.
func TestATypedReadComparesEveryValueTheWritePathAccepts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn := newDatasetWithDSN(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kindlessPackage, 0),
		vocabulary.KindManifest(kindlessPackage, map[string]any{"singular": "extreme"}, map[string]any{"properties": map[string]any{
			"extremeAt":   map[string]any{"type": "datetime"},
			"extremeSize": map[string]any{"type": "decimal"},
		}}),
	}); err != nil {
		t.Fatalf("install the kind: %v", err)
	}
	put := func(property string, v any) string {
		r := mustPut(t, ds, owner, substrate.PutInput{Kind: kindlessPackage + "/extreme", Properties: map[string]any{property: v}})
		return fmt.Sprint(r.Properties[property])
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: kindlessPackage + "/extreme", Properties: map[string]any{"extremeAt": "0001-01-01T00:30:00+01:00"},
	}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("an offset carrying 0001 into year 0: %v, want a validation refusal", err)
	}
	const yearZero = "0000-12-31T23:30:00Z"
	r := mustPut(t, ds, owner, substrate.PutInput{Kind: kindlessPackage + "/extreme", Properties: map[string]any{"extremeAt": "2000-01-01T00:00:00Z"}})
	if _, err := rawDB(t, dsn).ExecContext(ctx,
		`UPDATE records SET props = jsonb_set(props, '{extremeAt}', to_jsonb($1::text)) WHERE kind = $2 AND id = $3`,
		yearZero, kindlessPackage+"/extreme", r.ID); err != nil {
		t.Fatalf("store the year-0 instant the write path once accepted: %v", err)
	}
	yearTenThousand := put("extremeAt", "9999-12-31T23:30:00-01:00")
	if yearTenThousand != "10000-01-01T00:30:00Z" {
		t.Fatalf("stored %q; the cases below assume the write path's normalization", yearTenThousand)
	}
	put("extremeAt", "9999-12-31T22:00:00Z")
	put("extremeAt", "2026-10-25T00:00:00Z")
	long := func(lead string) string { return lead + strings.Repeat("0", 1199) }
	put("extremeSize", long("1"))
	put("extremeSize", long("2"))
	put("extremeSize", "5")

	for _, kinds := range [][]string{{kindlessPackage + "/extreme"}, nil} {
		for _, tc := range []struct {
			name     string
			property string
			cond     substrate.Cond
			want     []string
		}{
			{"a five-digit year is in range", "extremeAt", substrate.Cond{Gte: "9999-12-31T23:00:00Z"}, []string{yearTenThousand}},
			{"year 0 is in no range", "extremeAt", substrate.Cond{Lt: "2026-01-01T00:00:00Z"}, nil},
			{
				"an order puts year 0 last", "extremeAt",
				substrate.Cond{Exists: new(true)},
				[]string{"2026-10-25T00:00:00Z", "9999-12-31T22:00:00Z", yearTenThousand, yearZero},
			},
			{"a 1,200-digit decimal is in range", "extremeSize", substrate.Cond{Gt: "100"}, []string{long("1"), long("2")}},
			{"a 1,200-digit decimal bounds a range", "extremeSize", substrate.Cond{Gt: "6", Lt: long("2")}, []string{long("1")}},
		} {
			page, err := ds.List(ctx, substrate.Query{
				Filter:  substrate.Filter{Kinds: kinds, Properties: map[string]substrate.Cond{tc.property: tc.cond}},
				OrderBy: []substrate.Order{{Property: tc.property}},
				First:   50,
			})
			if err != nil {
				t.Errorf("kinds %v, %s: %v", kinds, tc.name, err)
				continue
			}
			var got []string
			for _, r := range page.Records {
				got = append(got, fmt.Sprint(r.Properties[tc.property]))
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("kinds %v, %s: read %d rows %.80v, want %d %.80v", kinds, tc.name, len(got), got, len(tc.want), tc.want)
			}
		}
	}
}

// A kindless money filter scans rows no loaded declaration speaks for, and a
// quarantined package's row is not held to the money grammar: an amount that
// is no number, or one with more digits than numeric holds, reads as no
// value instead of failing the cast for the whole read, filtered or ordered.
func TestKindlessMoneyFilterSkipsAnAmountNumericRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := pfOpener(t, dsn, t.TempDir())
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	const parked = "parked.example.substrate.reamde.dev/parked"
	fee := map[string]any{"properties": map[string]any{"feeCharge": map[string]any{"type": "money"}}}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kindlessPackage, 0),
		vocabulary.KindManifest(kindlessPackage, map[string]any{"singular": "fee"}, fee),
		vocabulary.PackageManifest(parked, 0),
		vocabulary.KindManifest(parked, map[string]any{"singular": "levy"}, fee),
	}); err != nil {
		t.Fatalf("install the kinds: %v", err)
	}
	for _, amount := range []int64{900, 1200} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: kindlessPackage + "/fee", Properties: map[string]any{"feeCharge": moneyValue(amount, "EUR")}})
	}
	raw := rawDB(t, dsn)
	for _, amount := range []string{"n/a", strings.Repeat("9", 140000)} {
		r := mustPut(t, ds, owner, substrate.PutInput{Kind: parked + "/levy", Properties: map[string]any{"feeCharge": moneyValue(1, "EUR")}})
		if _, err := raw.ExecContext(ctx,
			`UPDATE records SET props = jsonb_set(props, '{feeCharge,amount}', to_jsonb($1::text)) WHERE kind = $2 AND id = $3`,
			amount, parked+"/levy", r.ID); err != nil {
			t.Fatalf("write the amount numeric refuses: %v", err)
		}
	}
	// A key no dialect reserves quarantines the package at the next open.
	if _, err := raw.ExecContext(ctx,
		`UPDATE records SET props = jsonb_set(props, '{properties,feeCharge,unheardOf}', 'true') WHERE id = $1`, parked+"/levy"); err != nil {
		t.Fatalf("corrupt the stored declaration: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, order := range [][]substrate.Order{nil, {{Property: "feeCharge"}}} {
		page, err := ds2.List(ctx, substrate.Query{
			Filter:  substrate.Filter{Properties: map[string]substrate.Cond{"feeCharge": {Gte: moneyValue(1000, "EUR")}}},
			OrderBy: order,
			First:   50,
		})
		if err != nil {
			t.Fatalf("order %v: %v", order, err)
		}
		if len(page.Records) != 1 || page.Records[0].Kind != kindlessPackage+"/fee" {
			t.Fatalf("order %v: filtered to %d records, want the one fee of 12.00 EUR", order, len(page.Records))
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
			Kinds: []string{taskKind}, Implements: "substrate.reamde.dev/core/temporal",
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
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Implements: "substrate.reamde.dev/core/temporal"}})
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
			Kinds: []string{personKind}, Implements: "substrate.reamde.dev/core/temporal",
		}})
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("error = %v, want a validation error naming the mismatch", err)
		}
	})
}

// COUNT is the size of the set the list's own filter admits, read in the
// page's snapshot: every arm narrows it exactly as it narrows the rows, a
// tombstone and a merged-away loser are out of it as they are out of the
// list, and paging does not move it.
func TestListCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newVocabularyDataset(t, "tasks", "calendar")

	const (
		personKind = "samples.substrate.reamde.dev/people/person"
		orgKind    = "samples.substrate.reamde.dev/people/organization"
		taskKind   = "samples.substrate.reamde.dev/tasks/task"
		temporal   = "substrate.reamde.dev/core/temporal"
	)
	org := mustPut(t, ds, owner, substrate.PutInput{Kind: orgKind, Properties: map[string]any{"name": "Analytical"}})
	var people []*substrate.Record
	for _, name := range []string{"Ada", "Grace", "Grace B.", "Hedy", "Joan", "Katherine"} {
		props := map[string]any{"name": name}
		if name != "Joan" {
			props["memberOf"] = []any{org.ID}
		}
		people = append(people, mustPut(t, ds, owner, substrate.PutInput{Kind: personKind, Properties: props}))
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{
		"title": "send the rack layout", "dueAt": "2026-08-04T09:00:00Z",
	}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/calendar/transcript", Properties: map[string]any{
		"title": "the standup", "text": "…", "at": "2026-08-03T09:00:00Z", "endsAt": "2026-08-03T09:30:00Z",
	}})
	// One deleted, one merged away: seven people written, four live.
	if _, err := ds.Delete(ctx, owner, personKind, people[3].ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: personKind, Winner: people[1].ID, Loser: people[2].ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	count := func(q substrate.Query) (int64, *substrate.Page) {
		t.Helper()
		q.Count = true
		page, err := ds.List(ctx, q)
		if err != nil {
			t.Fatalf("count %+v: %v", q.Filter, err)
		}
		if page.Count == nil {
			t.Fatalf("count %+v: the page carried no count", q.Filter)
		}
		return *page.Count, page
	}
	people1 := substrate.Filter{Kinds: []string{personKind}}
	yes, no := true, false

	for _, tc := range []struct {
		name   string
		filter substrate.Filter
		want   int64
	}{
		{"one kind, live rows only", people1, 4},
		{"the tombstones", substrate.Filter{Kinds: []string{personKind}, Deleted: &yes}, 2},
		{"live, spelled out", substrate.Filter{Kinds: []string{personKind}, Deleted: &no}, 4},
		{"a property", substrate.Filter{Kinds: []string{personKind}, Properties: map[string]substrate.Cond{"name": {Prefix: "K"}}}, 1},
		{"the search index", substrate.Filter{Kinds: []string{personKind}, Search: "grace"}, 1},
		{"the reverse read", substrate.Filter{Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(orgKind, org.ID), Property: "memberOf"}}, 3},
		{"a trait across packages", substrate.Filter{Implements: temporal}, 2},
		{"a trait inside one kind", substrate.Filter{Kinds: []string{taskKind}, Implements: temporal}, 1},
		{"ids", substrate.Filter{Kinds: []string{personKind}, IDs: []string{people[0].ID, people[3].ID}}, 1},
	} {
		got, page := count(substrate.Query{Filter: tc.filter, First: 500})
		if got != tc.want {
			t.Errorf("%s: count = %d, want %d", tc.name, got, tc.want)
		}
		// The count and the rows answer one question.
		if int64(len(page.Records)) != got {
			t.Errorf("%s: count %d beside %d rows on an exhausted page", tc.name, got, len(page.Records))
		}
	}

	// Paging does not move it: a cursor page and an offset page count the
	// filtered set, not what is left past them.
	first, page := count(substrate.Query{Filter: people1, First: 1, OrderBy: []substrate.Order{{Property: "name"}}})
	if page.Cursor == "" {
		t.Fatal("first=1 over four rows minted no cursor")
	}
	if n, _ := count(substrate.Query{Filter: people1, First: 1, OrderBy: []substrate.Order{{Property: "name"}}, After: page.Cursor}); n != first {
		t.Fatalf("the second page counted %d, the first %d", n, first)
	}
	if n, _ := count(substrate.Query{Filter: people1, First: 1, Offset: 3}); n != first {
		t.Fatalf("an offset page counted %d, want %d", n, first)
	}

	// Not asked, not answered.
	plain, err := ds.List(ctx, substrate.Query{Filter: people1})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Count != nil {
		t.Fatalf("a list that did not ask carried count %d", *plain.Count)
	}
}

// OFFSET addresses a page by its number: the same ordered rows a keyset walk
// would reach, without the walk. The two continuations are alternatives, and
// a page reached by offset still mints a cursor so the reader can hand off to
// a stable walk from there (decision 0085).
func TestListOffset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// Six people, named so the title order is the insertion order.
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": name}})
	}
	base := substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{"samples.substrate.reamde.dev/people/person"}},
		OrderBy: []substrate.Order{{Property: "name"}},
		First:   2,
	}

	names := func(page *substrate.Page) []string {
		out := make([]string, 0, len(page.Records))
		for _, e := range page.Records {
			out = append(out, fmt.Sprint(e.Properties["name"]))
		}
		return out
	}

	// Page three by offset is page three by walking.
	q := base
	q.Offset = 4
	jumped, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list at offset 4: %v", err)
	}
	if got := names(jumped); !slices.Equal(got, []string{"e", "f"}) {
		t.Fatalf("offset 4 answered %v, want [e f]", got)
	}

	// The last page is exhausted: no cursor, because there is no next page.
	if jumped.Cursor != "" {
		t.Fatalf("the last page carried cursor %q, want none", jumped.Cursor)
	}

	// A middle offset page mints a cursor, and that cursor continues the
	// KEYSET walk from where the jump landed.
	q.Offset = 2
	middle, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list at offset 2: %v", err)
	}
	if got := names(middle); !slices.Equal(got, []string{"c", "d"}) {
		t.Fatalf("offset 2 answered %v, want [c d]", got)
	}
	if middle.Cursor == "" {
		t.Fatal("an offset page with rows behind it carried no cursor: a numbered reader cannot hand off to a stable walk")
	}
	q.Offset = 0
	q.After = middle.Cursor
	after, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("walk on from an offset page: %v", err)
	}
	if got := names(after); !slices.Equal(got, []string{"e", "f"}) {
		t.Fatalf("the cursor an offset page minted answered %v, want [e f]", got)
	}

	// An offset past the end is an empty page, not an error.
	q = base
	q.Offset = 99
	past, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list past the end: %v", err)
	}
	if len(past.Records) != 0 || past.Cursor != "" {
		t.Fatalf("offset past the end answered %d rows and cursor %q, want an empty exhausted page", len(past.Records), past.Cursor)
	}

	// The two continuations are alternatives: together they would seek and
	// then skip, and either precedence answers a page nobody asked for.
	q = base
	q.Offset = 2
	q.After = middle.Cursor
	if _, err := ds.List(ctx, q); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("offset with after: %v, want a validation error", err)
	}

	q = base
	q.Offset = -1
	if _, err := ds.List(ctx, q); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a negative offset: %v, want a validation error", err)
	}
}
