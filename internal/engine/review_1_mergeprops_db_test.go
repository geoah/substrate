package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Skeptic probe A: two records pointing at one organization with DIFFERENT link
// data — does each side's link data survive a merge and a split?
func TestSkepticMergeSplitLinkDataCollision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	team := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "organization", Properties: map[string]any{"name": "Acme"},
	})
	teamRef := vocabulary.RecordPath(team.Kind, team.ID)
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{
			"name": "W",
			"memberOf": []any{map[string]any{
				vocabulary.ReferenceValueKey: teamRef, "role": "guest",
			}},
		},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{
			"name": "L",
			"memberOf": []any{map[string]any{
				vocabulary.ReferenceValueKey: teamRef, "role": "admin",
			}},
		},
	})

	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := linkDataOf(t, ds, winner.Kind, winner.ID, "memberOf", team.ID); got["role"] != "guest" {
		t.Fatalf("the merge moved link data onto the winner: %+v", got)
	}

	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	back := linkDataOf(t, ds, loser.Kind, loser.ID, "memberOf", team.ID)
	if back == nil {
		t.Fatalf("the loser's pointer is not restored at all")
	}
	if back["role"] != "admin" {
		t.Fatalf("loser link data role = %v, want admin", back["role"])
	}
	if got := linkDataOf(t, ds, winner.Kind, winner.ID, "memberOf", team.ID); got["role"] != "guest" {
		t.Fatalf("winner link data role = %v, want guest", got["role"])
	}
}

// Skeptic probe B: a reference from the loser to the winner (one kind pointing
// at itself) — does it survive merge and split?
func TestSkepticMergeSplitWinnerLoserReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "slack-account:T1",
		Properties: map[string]any{"provider": "slack", "label": "Acme"},
	})
	conv := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:T1:C1",
		Properties: map[string]any{"category": "channel", "name": "general", "account": enginetest.AccountType + "/" + acc.ID},
	})
	author := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "A"},
	})
	m1 := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack-msg:T1:C1:1",
		Properties: map[string]any{
			"at": "2026-08-03T10:00:00Z", "text": "root",
			"conversation": conv.ID,
			"author":       author.ID,
		},
	})
	m2 := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack-msg:T1:C1:2",
		Properties: map[string]any{
			"at": "2026-08-03T10:01:00Z", "text": "reply",
			"conversation": conv.ID,
			"author":       author.ID,
			"replyTo":      m1.ID,
		},
	})
	wantReplyTo := vocabulary.RecordPath(m1.Kind, m1.ID)
	if pre := mustGet(t, ds, m2.Kind, m2.ID); pre.Properties["replyTo"] != wantReplyTo {
		t.Fatalf("setup: replyTo missing: %+v", pre.Properties)
	}

	rec, err := ds.Merge(ctx, owner, m1.Kind, m1.ID, m2.ID)
	if err != nil {
		t.Fatalf("merge of two messages: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	if back := mustGet(t, ds, m2.Kind, m2.ID); back.Properties["replyTo"] != wantReplyTo {
		t.Fatalf("the loser->winner pointer is lost across merge+split: %+v", back.Properties["replyTo"])
	}
}
