package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The suggestion flow's engine half (GO.md stage 1): recordmergerequest's
// decision machine, applyMerge in the transition's transaction, and the
// diffConflict rollback+annotation pattern on a stale request.

const requestType = "core.substrate.reamde.dev/recordmergerequest"

// mergeRequest writes one request under a deterministic id — the legality of
// caller-composed ids on this type is itself part of the contract (the pair
// id is the suggestion dedupe and the rejection memory).
func mergeRequest(t *testing.T, ds substrate.Dataset, id, winner, loser string) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: requestType,
		ID:   id,
		Properties: map[string]any{
			"rationale": "they look like one person",
			"evidence":  map[string]any{"signals": []any{map[string]any{"signal": "email"}}},
		},
		Edges: []substrate.EdgeInput{
			{Rel: "winner", To: substrate.EdgeRef{Kind: "people.substrate.geoah.me/person", ID: winner}},
			{Rel: "loser", To: substrate.EdgeRef{Kind: "people.substrate.geoah.me/person", ID: loser}},
		},
	})
}

func decide(ds substrate.Dataset, id, decision string) (*substrate.Record, error) {
	return ds.Patch(context.Background(), owner, requestType, id, substrate.PatchInput{
		Properties: map[string]any{"decision": decision},
	})
}

func TestMergeRequestAcceptMerges(t *testing.T) {
	_, ds := newDataset(t)
	ctx := context.Background()

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "Alexandra Chen"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "Alex Chen"},
	})
	req := mergeRequest(t, ds, "dupe-"+winner.ID+"-"+loser.ID, winner.ID, loser.ID)

	accepted, err := decide(ds, req.ID, "accepted")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.Properties["decision"] != "accepted" {
		t.Fatalf("decision: %v", accepted.Properties["decision"])
	}
	if _, ok := accepted.Properties["decidedAt"]; !ok {
		t.Fatal("decidedAt not stamped")
	}
	// The merge happened in the same transaction: the canonical-id contract
	// holds for the loser's id, and the loser is tombstoned under the merge
	// finalizer.
	got := mustGet(t, ds, loser.Kind, loser.ID)
	if got.CanonicalID != winner.ID || got.ID != winner.ID {
		t.Fatalf("loser id did not canonicalize: %+v", got)
	}
	// The command record exists: split stays possible.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/recordmerge"}}})
	if err != nil {
		t.Fatalf("list merges: %v", err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("merge records: %d", len(page.Records))
	}
}

func TestMergeRequestRejectStamps(t *testing.T) {
	_, ds := newDataset(t)

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "B"}})
	req := mergeRequest(t, ds, "dupe-"+a.ID+"-"+b.ID, a.ID, b.ID)

	rejected, err := decide(ds, req.ID, "rejected")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Properties["decision"] != "rejected" || rejected.Properties["decidedAt"] == nil {
		t.Fatalf("rejected: %+v", rejected.Properties)
	}
	// Nothing merged: both persons stand.
	if got := mustGet(t, ds, b.Kind, b.ID); got.CanonicalID != "" || got.DeletedAt != nil {
		t.Fatalf("reject touched the pair: %+v", got)
	}
}

func TestMergeRequestStaleFailsWholeAndAnnotates(t *testing.T) {
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "B"}})
	first := mergeRequest(t, ds, "req-first", winner.ID, loser.ID)
	second := mergeRequest(t, ds, "req-second", winner.ID, loser.ID)

	if _, err := decide(ds, first.ID, "accepted"); err != nil {
		t.Fatalf("accept first: %v", err)
	}
	// The pair is already merged: the second request's loser edge was
	// re-pointed at the winner by the merge, so applying it is a self-merge
	// — refused, and the refusal fails the TRANSITION whole.
	_, err := decide(ds, second.ID, "accepted")
	wantErr(t, err, substrate.ErrConflict, "accepting a stale merge request")

	got := mustGet(t, ds, second.Kind, second.ID)
	if got.Properties["decision"] != "proposed" {
		t.Fatalf("stale accept moved the machine: %v", got.Properties["decision"])
	}
	if _, ok := got.Properties["decidedAt"]; ok {
		t.Fatal("stale accept stamped decidedAt")
	}
	note, ok := got.Annotations["substrate/conflict"].(map[string]any)
	if !ok {
		t.Fatalf("conflict not annotated: %+v", got.Annotations)
	}
	// The annotation records the REFUSAL (the applyDiff pattern): here the
	// stale request's loser edge already canonicalized onto the winner, so
	// the re-run guard sees a self-merge.
	reason, _ := note["reason"].(string)
	if !strings.Contains(reason, "cannot merge") {
		t.Fatalf("annotation reason: %q", reason)
	}
}

func TestMergeRequestRefusesDifferentTypes(t *testing.T) {
	_, ds := newDataset(t)

	person := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "A"}})
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"title": "t"}})
	req := mustPut(t, ds, owner, substrate.PutInput{
		Kind: requestType,
		ID:   "req-cross",
		Edges: []substrate.EdgeInput{
			{Rel: "winner", To: substrate.EdgeRef{Kind: "people.substrate.geoah.me/person", ID: person.ID}},
			{Rel: "loser", To: substrate.EdgeRef{Kind: taskType, ID: task.ID}},
		},
	})

	_, err := decide(ds, req.ID, "accepted")
	if err == nil || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("cross-type accept: %v", err)
	}
	if got := mustGet(t, ds, req.Kind, req.ID); got.Properties["decision"] != "proposed" {
		t.Fatalf("cross-type accept moved the machine: %v", got.Properties["decision"])
	}
}
