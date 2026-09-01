package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Skeptic #2 claimed a merge dropped the loser's link data on a collision and
// that split rebuilt the loser's link from the WINNER's values. Both were true
// of edge ROWS, which a merge repointed. A reference is a value on the record
// that names it, and merge repoints nothing: the loser keeps its own pointers
// and its own link data through the merge and through the split, because
// neither verb reaches into them.
func TestMergeLeavesTheLosersLinkDataAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	team := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "organization", Properties: map[string]any{"name": "Platform"},
	})
	member := func(role, since string) any {
		v := map[string]any{vocabulary.ReferenceValueKey: team.ID, "role": role}
		if since != "" {
			v["since"] = since
		}
		return v
	}
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person",
		Properties: map[string]any{
			"name": "Nina Ray", "memberOf": []any{member("guest", "")},
		},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person",
		Properties: map[string]any{
			"name": "N. Ray", "memberOf": []any{member("admin", "2019-04-01")},
		},
	})

	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// The winner's own membership is untouched: nothing moved onto it.
	if got := memberRole(t, ds, winner.Kind, winner.ID, team.ID); got != "guest" {
		t.Fatalf("the merge rewrote the winner's link data: role=%q, want guest", got)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	le := memberLink(t, ds, loser.Kind, loser.ID, team.ID)
	if le == nil {
		t.Fatal("the loser's membership is gone entirely")
	}
	if le["role"] != "admin" || le["since"] != "2019-04-01" {
		t.Fatalf("the loser's link data changed across merge and split: %+v", le)
	}
}

// The same claim from the other side: a pointer BETWEEN the merged pair. The
// blanket delete that carried one off is gone with the edge table, so the
// loser's `replyTo` at the winner is exactly where it was written, before and
// after the split.
func TestMergeKeepsAPointerInsideTheMergedPair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acct := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, Properties: map[string]any{"provider": "x", "label": "l"},
	})
	conv := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "conversation",
		Properties: map[string]any{"category": "direct", "account": enginetest.AccountType + "/" + acct.ID},
	})
	contact := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "A"},
	})
	mkMsg := func(text string, extra map[string]any) *substrate.Record {
		props := map[string]any{
			"text": text, "conversation": conv.ID, "author": contact.ID,
		}
		for k, v := range extra {
			props[k] = v
		}
		return mustPut(t, ds, owner, substrate.PutInput{
			Kind: "conversationmessage", Properties: props,
		})
	}
	winner := mkMsg("root", nil)
	loser := mkMsg("dup", map[string]any{"replyTo": winner.ID})

	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge of two messages: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	got := mustGet(t, ds, loser.Kind, loser.ID)
	if want := vocabulary.RecordPath(winner.Kind, winner.ID); refPathValue(got, "replyTo") != want {
		t.Fatalf("the loser's replyTo = %q, want %q", refPathValue(got, "replyTo"), want)
	}
}

// memberLink reads the link data stored beside one organization in a person's
// `memberOf`, nil when the person does not name it.
func memberLink(t *testing.T, ds substrate.Dataset, typ, id, org string) map[string]any {
	t.Helper()
	e, err := ds.Get(context.Background(), typ, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	list, _ := e.Properties["memberOf"].([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path, _ := m[vocabulary.ReferenceValueKey].(string)
		if _, orgID, ok := vocabulary.SplitRecordPath(path); !ok || orgID != org {
			continue
		}
		out := map[string]any{}
		for k, v := range m {
			if k != vocabulary.ReferenceValueKey {
				out[k] = v
			}
		}
		return out
	}
	return nil
}

func memberRole(t *testing.T, ds substrate.Dataset, typ, id, org string) string {
	t.Helper()
	role, _ := memberLink(t, ds, typ, id, org)["role"].(string)
	return role
}

// refPathValue reads a single-valued reference off a projected record, in
// either value shape.
func refPathValue(e *substrate.Record, name string) string {
	return storedRefPath(e.Properties[name])
}
