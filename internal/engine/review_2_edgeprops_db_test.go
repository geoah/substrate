package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// Skeptic #2 claim A: on a (rel, src, dst) collision the loser's edge props
// are dropped, and split rebuilds the loser's edge from the WINNER's props.
func TestSk2MergeDropsLoserEdgeProps(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	team := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "organization", Properties: map[string]any{"name": "Platform"},
	})
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
		Edges: []substrate.EdgeInput{{
			Rel: "memberOf", To: substrate.EdgeRef{ID: team.ID},
			Properties: map[string]any{"role": "guest"},
		}},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
		Edges: []substrate.EdgeInput{{
			Rel: "memberOf", To: substrate.EdgeRef{ID: team.ID},
			Properties: map[string]any{"role": "admin", "since": "2019"},
		}},
	})

	logEdges(t, ds, "before merge winner", winner.Kind, winner.ID)
	logEdges(t, ds, "before merge loser ", loser.Kind, loser.ID)

	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	t.Logf("merge record props: %+v", rec.Properties)
	logEdges(t, ds, "after merge winner", winner.Kind, winner.ID)

	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	logEdges(t, ds, "after split winner", winner.Kind, winner.ID)
	le := edgeProps(t, ds, loser.Kind, loser.ID, "memberOf", team.ID)
	t.Logf("after split loser member_of props = %+v", le)
	if le == nil {
		t.Errorf("DATA LOSS: loser's member_of edge is gone entirely")
	} else if le["role"] != "admin" {
		t.Errorf("DATA LOSS: loser's edge props came back as %+v, expected role=admin since=2019", le)
	}
}

// Skeptic #2 claim B: an edge between winner and loser (same-type rel) is
// deleted by the blanket DELETE and never recorded, so split cannot restore
// it. Messages no longer merge at all (§6: from_alias types do idempotency,
// not fusion), so the claim now lands on the guard; the round trip itself is
// covered by TestMergeSplitRestoresPairInternalEdge.
func TestSk2MergeDestroysWinnerLoserEdge(t *testing.T) {
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
		Properties: map[string]any{"kind": "direct"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acct.ID}}},
	})
	contact := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "A"},
	})
	mkMsg := func(ext, text string, extra ...substrate.EdgeInput) *substrate.Record {
		edges := []substrate.EdgeInput{
			{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
			{Rel: "author", To: substrate.EdgeRef{ID: contact.ID}},
		}
		edges = append(edges, extra...)
		return mustPut(t, ds, owner, substrate.PutInput{
			Kind: "conversationmessage", Properties: map[string]any{"text": text},
			Edges: edges,
		})
	}
	winner := mkMsg("M1", "root")
	loser := mkMsg("M2", "dup", substrate.EdgeInput{
		Rel: "replyTo", To: substrate.EdgeRef{ID: winner.ID},
		Properties: map[string]any{"src": "beeper"},
	})

	logEdges(t, ds, "before merge loser", loser.Kind, loser.ID)
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge of two messages: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	p := edgeProps(t, ds, loser.Kind, loser.ID, "replyTo", winner.ID)
	if p == nil {
		t.Fatalf("DATA LOSS: the loser's replyTo -> winner edge is gone")
	}
	if p["src"] != "beeper" {
		t.Fatalf("loser replyTo props = %+v", p)
	}
}

func logEdges(t *testing.T, ds substrate.Dataset, what, typ, id string) {
	t.Helper()
	e, err := ds.Get(context.Background(), typ, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	t.Logf("%s (%s): edges=%+v", what, id, e.Edges)
}

func edgeProps(t *testing.T, ds substrate.Dataset, typ, id, rel, other string) map[string]any {
	t.Helper()
	e, err := ds.Get(context.Background(), typ, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	for _, tgt := range e.Edges[rel] {
		if tgt.ID == other {
			if tgt.Properties == nil {
				return map[string]any{}
			}
			return tgt.Properties
		}
	}
	return nil
}
