package engine

// WHO WRITES `heldTo` (decision 0276): the engine alone. Every door a hand
// writes through tries to set it on a record that has none, change it, and
// drop it, on a thread, a message, an ask and an approval: each is refused,
// or leaves it as it was. A hand's write of a thread's lease, a merge and a
// split, and the replay an import or a restore makes are tried too.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// heldRecords are a thread, a message, an ask and an approval of ada's
// runs, by kind.
func (r *runsRepository) heldRecords(t *testing.T) map[string]string {
	t.Helper()
	ctx := context.Background()
	ada := r.adaCtx()
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "scribe", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
	out := map[string]string{typeThread: thread}
	for kind, query := range map[string]string{
		typeMessage:                   `SELECT id FROM records WHERE kind = $1 AND ` + referencePathSQL("props", msgRelThread) + ` = $2 LIMIT 1`,
		vocabulary.KindLLMInteraction: `SELECT id FROM records WHERE kind = $1 AND ` + referencePathSQL("props", msgRelThread) + ` = $2 LIMIT 1`,
	} {
		var id string
		if err := r.ds.db.QueryRowContext(ctx, query, kind, vocabulary.RecordPath(typeThread, thread)).Scan(&id); err != nil {
			t.Fatalf("the %s of the run: %v", kind, err)
		}
		out[kind] = id
	}
	out[vocabulary.KindLLMApproval] = r.approvalIn(t, approvalPending)
	return out
}

// heldRaw is the stored `heldTo` of kind/id as text, "" where it has none.
func (r *runsRepository) heldRaw(t *testing.T, kind, id string) string {
	t.Helper()
	var raw string
	if err := r.ds.db.QueryRowContext(context.Background(), `SELECT coalesce((props->'heldTo')::text, '') FROM records WHERE kind = $1 AND id = $2`,
		kind, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

// forgedHeld is a `heldTo` naming bo, which no hand may write.
func (r *runsRepository) forgedHeld() map[string]any {
	return map[string]any{"onBehalfOf": vocabulary.ActorRef(actorID(substrate.PersonActor(r.boID)))}
}

// heldWriteDoors try one write of heldTo, the value or nil to drop it.
var heldWriteDoors = map[string]func(t *testing.T, r *runsRepository, kind, id string, v any) error{
	"a full token's put": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		_, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: id, Properties: map[string]any{propHeldTo: v}})
		return err
	},
	"a full token's patch": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		_, err := r.ds.Patch(r.adaCtx(), substrate.ActorPerson, kind, id, substrate.PatchInput{Properties: map[string]any{propHeldTo: v}})
		return err
	},
	"an editor's patch, as a person's view passes it on": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		bo := substrate.PersonActor(r.boID)
		_, err := r.ds.Patch(r.person(r.boID, bo), bo, kind, id, substrate.PatchInput{Properties: map[string]any{propHeldTo: v}})
		return err
	},
	"an app's token": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		actor, ctx, _ := r.app(t, "writer.example.org", "write:*")
		_, err := r.ds.Patch(ctx, actor, kind, id, substrate.PatchInput{Properties: map[string]any{propHeldTo: v}})
		return err
	},
	"an MCP tool, an assistant's": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		_, err := r.ds.Patch(substrate.WithAssistant(r.adaCtx()), substrate.Actor("app:assistant.example.org"), kind, id,
			substrate.PatchInput{Properties: map[string]any{propHeldTo: v}})
		return err
	},
	"a function's effect": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		return r.ds.inTx(r.adaCtx(), substrate.FunctionActor(r.ds.Repository().Authority, "runs", "reader"), false, func(tx *txn) error {
			emit := []string{kind}
			tx.setEffectEmit(emit)
			return tx.applyEffects(emit, []effect{{Action: effectPatch, Type: kind, ID: id, Properties: map[string]any{propHeldTo: v}}})
		})
	},
	"an agent's write tool": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		_, rows := r.chat(t, r.adaCtx(), substrate.ActorPerson, "historian", "",
			fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "patch", kind, id, map[string]any{propHeldTo: v})}}}, fakeTurn{content: "done."})
		if len(rows) == 1 && rows[0]["ok"] == false {
			return fmt.Errorf("%v", rows[0]["content"])
		}
		return nil
	},
	"an agent's edit tool": func(t *testing.T, r *runsRepository, kind, id string, v any) error {
		_, rows := r.chat(t, r.adaCtx(), substrate.ActorPerson, "historian", "",
			fakeTurn{calls: []fakeCall{{"edit", toolArgs(t, map[string]any{
				"op": "replace", "kind": kind, "id": id, "property": propHeldTo, "edits": []any{map[string]any{"old": "ref", "new": "forged"}},
			})}}}, fakeTurn{content: "done."})
		if len(rows) == 1 && rows[0]["ok"] == false {
			return fmt.Errorf("%v", rows[0]["content"])
		}
		return nil
	},
}

func TestNoHandWritesWhoseRulesHoldARun(t *testing.T) {
	t.Parallel()
	for door, write := range heldWriteDoors {
		t.Run(door, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := newRunsRepository(t)
			for kind, id := range r.heldRecords(t) {
				stored := r.heldRaw(t, kind, id)
				if stored == "" {
					t.Fatalf("the %s of the run holds no heldTo: the test proves nothing", kind)
				}
				for op, v := range map[string]any{"change": r.forgedHeld(), "drop": nil} {
					err := write(t, r, kind, id, v)
					t.Logf("%s, %s on a %s: %v", door, op, kind, err)
					if after := r.heldRaw(t, kind, id); err == nil && after != stored {
						t.Errorf("%s %s the heldTo of a %s: %s, then %s", door, op, kind, stored, after)
					}
				}
				// Set: on a record from before the release, which holds none.
				if _, err := r.ds.db.ExecContext(ctx, `UPDATE records SET props = props - 'heldTo' WHERE kind = $1 AND id = $2`, kind, id); err != nil {
					t.Fatal(err)
				}
				err := write(t, r, kind, id, r.forgedHeld())
				t.Logf("%s, set on a %s: %v", door, kind, err)
				if after := r.heldRaw(t, kind, id); err == nil && after != "" {
					t.Errorf("%s set the heldTo of a %s that held none: %s", door, kind, after)
				}
			}
		})
	}
}

// The doors that take a record whole, tried on each of the four kinds: the
// declaration door, a merge (and its split, where a merge lands), and a
// change request accepted through the policy door. A mapping's recompute is
// tried into a thread. A hand's write of a thread's lease does not make the
// thread its turn's: only the loop's own claim does. A replay of the
// history (a rebuild; an import and a restore replay the same fold,
// fold.go) leaves `heldTo` as it was.
func TestNoWriteOfALeaseAMergeOrAReplayChangesWhoseRulesHoldARun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	held, other := r.heldRecords(t), r.heldRecords(t)
	stored := func() map[string]string {
		out := map[string]string{}
		for kind, id := range held {
			out[kind] = r.heldRaw(t, kind, id)
		}
		return out
	}
	unchanged := func(what string, before map[string]string) {
		t.Helper()
		for kind, id := range held {
			if after := r.heldRaw(t, kind, id); after != before[kind] {
				t.Errorf("%s changed the heldTo of a %s: %s, then %s", what, kind, before[kind], after)
			}
		}
	}
	before := stored()

	for kind, id := range held {
		_, err := r.ds.ApplyVocabularyDocuments(r.adaCtx(), substrate.ActorPerson, []map[string]any{{
			"kind": kind, "metadata": map[string]any{"id": id},
			"data": map[string]any{"properties": map[string]any{propHeldTo: r.forgedHeld()}},
		}})
		t.Logf("the declaration door, on a %s: %v", kind, err)
		if err == nil {
			t.Errorf("the declaration door took a %s", kind)
		}

		merged, err := r.ds.Merge(r.adaCtx(), substrate.ActorPerson, substrate.MergeInput{Kind: kind, Winner: id, Loser: other[kind]})
		t.Logf("a merge of two of a %s: %v", kind, err)
		if err == nil {
			if merge, ok := merged.Properties["merge"].(string); ok && merge != "" {
				_, err := r.ds.Split(r.adaCtx(), substrate.ActorPerson, substrate.SplitInput{Merge: merge})
				t.Logf("its split: %v", err)
			}
		}

		reqID := fid("forge-" + kind)
		_, err = r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.KindRecordPatchRequest, ID: reqID, Properties: map[string]any{
			"op": "patch", "rationale": "forge", "target": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kind, id)},
			"diff": map[string]any{"properties": map[string]any{propHeldTo: r.forgedHeld()}},
		}})
		t.Logf("a change request to forge a %s's: %v", kind, err)
		if err == nil {
			req, err := r.ds.Get(ctx, vocabulary.KindRecordPatchRequest, reqID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.ds.Patch(r.adaCtx(), substrate.ActorPerson, vocabulary.KindRecordPatchRequest, reqID, substrate.PatchInput{
				Properties: map[string]any{"decision": decisionAccepted}, IfVersion: &req.Version,
			})
			t.Logf("its acceptance: %v", err)
		}
	}
	unchanged("the declaration door, a merge and a change request", before)

	pkg := r.ds.Repository().Authority + "/feeds"
	_, err := r.ds.ApplyVocabularyDocuments(r.adaCtx(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "feed"}, map[string]any{"properties": map[string]any{
			"held": map[string]any{"type": "string"},
			"run":  map[string]any{"type": "reference", "kind": typeThread, "subject": true},
		}}),
		vocabulary.MappingManifest(pkg, "feedrun", map[string]any{
			"from": pkg + "/feed", "to": typeThread, "property": "run",
			"map": map[string]any{propHeldTo: map[string]any{"path": "held"}},
		}),
	})
	t.Logf("a mapping into a thread's heldTo: %v", err)
	if err == nil {
		_, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: pkg + "/feed", ID: fid("feed"), Properties: map[string]any{
			"held": "forged", "run": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(typeThread, held[typeThread])},
		}})
		t.Logf("its source's write: %v", err)
	}
	unchanged("a mapping", before)

	thread := held[typeThread]
	bo := substrate.PersonActor(r.boID)
	_, err = r.ds.Patch(r.person(r.boID, bo), bo, typeThread, thread, substrate.PatchInput{Properties: map[string]any{
		"leaseUntil": nowUTC().Add(time.Hour).Format(time.RFC3339Nano),
	}})
	t.Logf("an editor's write of the lease: %v", err)
	unchanged("an editor's write of a thread's lease", before)

	before = stored()
	if _, err := r.r.s.RebuildRepository(ctx, r.repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	d, err := r.r.s.Dataset(ctx, r.ada)
	if err != nil {
		t.Fatal(err)
	}
	r.ds = d.(*dataset)
	unchanged("the replay", before)
}
