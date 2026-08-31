package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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
			"winner":    vocabulary.RecordPath(typePerson, winner),
			"loser":     vocabulary.RecordPath(typePerson, loser),
		},
	})
}

func decide(ds substrate.Dataset, id, decision string) (*substrate.Record, error) {
	return ds.Patch(context.Background(), owner, requestType, id, substrate.PatchInput{
		Properties: map[string]any{"decision": decision},
	})
}

func TestMergeRequestAcceptMerges(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "Alexandra Chen"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "Alex Chen"},
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
	t.Parallel()
	_, ds := newDataset(t)

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "B"}})
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

// decidedAt is declared `managed`: the decision transition writes it, a
// client's own value is refused, and the round-trip echo of the stored value
// still applies — the same contract checkDeclarationWrite holds declaration
// rows to.
func TestManagedStampIsEngineWritten(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "B"}})

	// A creating write may not supply the stamp.
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: requestType, ID: "forged-create",
		Properties: map[string]any{
			"decidedAt": "2020-01-01T00:00:00Z",
			"winner":    vocabulary.RecordPath(typePerson, a.ID),
			"loser":     vocabulary.RecordPath(typePerson, b.ID),
		},
	})
	wantErr(t, err, substrate.ErrValidation, "creating with a forged decidedAt")

	req := mergeRequest(t, ds, "dupe-"+a.ID+"-"+b.ID, a.ID, b.ID)

	// Neither may a patch, before or after the decision.
	_, err = ds.Patch(ctx, owner, requestType, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decidedAt": "2020-01-01T00:00:00Z"},
	})
	wantErr(t, err, substrate.ErrValidation, "patching decidedAt onto an undecided request")

	rejected, err := decide(ds, req.ID, "rejected")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	stamped, _ := rejected.Properties["decidedAt"].(string)
	if stamped == "" {
		t.Fatal("decidedAt not stamped")
	}
	_, err = ds.Patch(ctx, owner, requestType, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decidedAt": "2020-01-01T00:00:00Z"},
	})
	wantErr(t, err, substrate.ErrValidation, "rewriting a stamped decidedAt")

	// The echo of the stored value is fine: get | apply round-trips.
	echoed, err := ds.Patch(ctx, owner, requestType, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decidedAt": stamped, "rationale": "still one person"},
	})
	if err != nil {
		t.Fatalf("echoing the stored stamp: %v", err)
	}
	if echoed.Properties["decidedAt"] != stamped {
		t.Fatalf("echo moved the stamp: %v", echoed.Properties["decidedAt"])
	}
}

func TestMergeRequestStaleFailsWholeAndAnnotates(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "B"}})
	other := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "C"}})
	first := mergeRequest(t, ds, "req-first", winner.ID, loser.ID)
	second := mergeRequest(t, ds, "req-second", other.ID, loser.ID)

	if _, err := decide(ds, first.ID, "accepted"); err != nil {
		t.Fatalf("accept first: %v", err)
	}
	// The second request still names the loser it was written against — nothing
	// repoints — and that record has been merged away, so applying it is
	// refused, and the refusal fails the TRANSITION whole.
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
	// re-run guard sees a loser another merge already tombstoned.
	reason, _ := note["reason"].(string)
	if !strings.Contains(reason, "merge needs two live records") {
		t.Fatalf("annotation reason: %q", reason)
	}
}

func TestMergeRequestRefusesDifferentTypes(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	person := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "A"}})
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "t"}})
	req := mustPut(t, ds, owner, substrate.PutInput{
		Kind: requestType,
		ID:   "req-cross",
		Properties: map[string]any{
			"winner": vocabulary.RecordPath(typePerson, person.ID),
			"loser":  vocabulary.RecordPath(taskType, task.ID),
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
