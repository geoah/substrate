package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func readIncoming(t *testing.T, ds substrate.Dataset, typ, id string, first int, after string) *substrate.IncomingPage {
	t.Helper()
	page, err := ds.Incoming(context.Background(), typ, id,
		substrate.IncomingOptions{First: first, After: after})
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}
	return page
}

func TestIncomingIsSeparateAndPaged(t *testing.T) {
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	sam := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name": aname("Samuel Jones"), "emails": gemails("sam@acme.com"),
	})
	s := syncSource(t, ds, slack, typeSlackUser, "s-sam", map[string]any{
		"realName": "Sam J", "email": "sam@acme.com",
	})

	// The record manifest never carries incoming edges: the
	// Record.Incoming field was removed at the v1 freeze, so the
	// reverse fan-out is reachable only through the paged resource below.
	first := readIncoming(t, ds, sam.Kind, sam.ID, 1, "")
	if len(first.Incoming) != 1 || first.Total != 2 || first.Cursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	if row := first.Incoming[0]; row.Rel != "person" || row.From.ID != g.ID || row.From.Title != "Samuel Jones" {
		t.Fatalf("first incoming row = %+v", row)
	}
	second := readIncoming(t, ds, sam.Kind, sam.ID, 1, first.Cursor)
	if len(second.Incoming) != 1 || second.Incoming[0].From.ID != s.ID || second.Cursor != "" {
		t.Fatalf("second page = %+v", second)
	}
}

func TestIncomingExcludesTombstonedSources(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	g := syncSource(t, ds, people, typeGoogleContact, "g-ned", map[string]any{
		"name": aname("Nameless Ned"),
	})
	pid := personOf(t, ds, g)
	if page := readIncoming(t, ds, typePerson, pid, 50, ""); page.Total != 1 {
		t.Fatalf("incoming before delete = %+v", page)
	}
	if _, err := ds.Delete(ctx, people, g.Kind, g.ID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	if page := readIncoming(t, ds, typePerson, pid, 50, ""); page.Total != 0 || len(page.Incoming) != 0 {
		t.Fatalf("incoming after delete = %+v", page)
	}
}

func TestIncomingResolvesFormerIDToCanonicalRecord(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name": aname("Samuel Jones"), "emails": gemails("sam@acme.com"),
	})
	s := syncSource(t, ds, slack, typeSlackUser, "s-sam", map[string]any{
		"realName": "Sam J", "email": "sam@corp.example",
	})
	winner, loser := personOf(t, ds, g), personOf(t, ds, s)
	if _, err := ds.Merge(ctx, owner, typePerson, winner, loser); err != nil {
		t.Fatalf("merge: %v", err)
	}

	canonical := readIncoming(t, ds, typePerson, winner, 50, "")
	former := readIncoming(t, ds, typePerson, loser, 50, "")
	if canonical.Total != former.Total || len(canonical.Incoming) != len(former.Incoming) {
		t.Fatalf("former page = %+v, canonical page = %+v", former, canonical)
	}
}
