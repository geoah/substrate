package engine_test

// The query grammar over the shipped sample vocabulary: which filters a List
// takes (kind, reference, label, enum, state, id), how it orders, how a trait
// filter reaches across authorities, and where a page ends.

import (
	"context"
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
	_, ds := newDataset(t)
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
