package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// Skeptic probe A: collision on (rel, src, dst) during merge — does the
// loser's edge props survive the round trip?
func TestSkepticMergeSplitEdgePropsCollision(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	team := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "organization", Properties: map[string]any{"name": "Acme"},
	})
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "W"},
		Edges: []substrate.EdgeInput{{
			Rel: "memberOf", To: substrate.EdgeRef{ID: team.ID},
			Properties: map[string]any{"role": "guest"},
		}},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "L"},
		Edges: []substrate.EdgeInput{{
			Rel: "memberOf", To: substrate.EdgeRef{ID: team.ID},
			Properties: map[string]any{"role": "admin"},
		}},
	})

	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	t.Logf("merge record props: %+v", rec.Properties)

	afterMerge := mustGet(t, ds, winner.Kind, winner.ID)
	t.Logf("winner edges after merge: %+v", afterMerge.Edges["memberOf"])

	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	back := mustGet(t, ds, loser.Kind, loser.ID)
	t.Logf("loser edges after split: %+v", back.Edges["memberOf"])
	if len(back.Edges["memberOf"]) != 1 {
		t.Fatalf("loser edge not restored at all: %+v", back.Edges)
	}
	if got := back.Edges["memberOf"][0].Properties["role"]; got != "admin" {
		t.Fatalf("DEFECT CONFIRMED: loser edge props role = %v, want admin", got)
	}
	w := mustGet(t, ds, winner.Kind, winner.ID)
	if got := w.Edges["memberOf"][0].Properties["role"]; got != "guest" {
		t.Fatalf("winner edge props role = %v, want guest", got)
	}
}

// Skeptic probe B: an edge between the winner and the loser (self-referential
// rel) — recorded? restored?
func TestSkepticMergeSplitWinnerLoserEdge(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, slack, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "slack-account:T1",
		Properties: map[string]any{"provider": "slack", "label": "Acme"},
	})
	conv := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:T1:C1",
		Properties: map[string]any{"kind": "channel", "name": "general"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	author := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "A"},
	})
	m1 := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack-msg:T1:C1:1",
		Properties: map[string]any{"at": "2026-08-03T10:00:00Z", "text": "root"},
		Edges: []substrate.EdgeInput{
			{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
			{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
		},
	})
	m2 := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack-msg:T1:C1:2",
		Properties: map[string]any{"at": "2026-08-03T10:01:00Z", "text": "reply"},
		Edges: []substrate.EdgeInput{
			{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
			{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
			{Rel: "replyTo", To: substrate.EdgeRef{ID: m1.ID}},
		},
	})
	pre := mustGet(t, ds, m2.Kind, m2.ID)
	if len(pre.Edges["replyTo"]) != 1 {
		t.Fatalf("setup: replyTo edge missing: %+v", pre.Edges)
	}

	rec, err := ds.Merge(ctx, owner, m1.Kind, m1.ID, m2.ID)
	if err != nil {
		t.Fatalf("merge of two messages: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	back := mustGet(t, ds, m2.Kind, m2.ID)
	t.Logf("loser edges: %+v", back.Edges)
	if len(back.Edges["replyTo"]) != 1 || back.Edges["replyTo"][0].ID != m1.ID {
		t.Fatalf("winner<->loser edge lost across merge+split: %+v", back.Edges["replyTo"])
	}
}
