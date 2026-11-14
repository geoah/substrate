package engine

// WHO MAY PERMANENTLY DELETE (decision 0305, under the roles of 0276): the
// caller needs what a delete of the record needs, the one check on the
// record's kind. What the delete takes with the record (the change requests
// that target it, the merge's own record, the loser) is the engine's doing,
// acting for the caller's person, and its lines say so.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestWhoMayPermanentlyDelete(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		narrow  bool
		as      func(rr *rolesRepository) string
		refused bool
	}{
		{"an editor", false, func(rr *rolesRepository) string { return rr.boID }, false},
		{"an editor whose rules do not reach the kind", true, func(rr *rolesRepository) string { return rr.boID }, true},
		{"a viewer", false, func(rr *rolesRepository) string { return rr.cyID }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			rr := newRolesRepository(t)
			ds := rr.ds
			const pkg = "erasewho.test.dev/erasewho"
			if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
				vocabulary.PackageManifest(pkg, 0),
				vocabulary.KindManifest(pkg, map[string]any{"singular": "box"}, map[string]any{
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				}),
			}); err != nil {
				t.Fatal(err)
			}
			admin := rr.as(rr.adaID)
			put := func(in substrate.PutInput) *substrate.Record {
				t.Helper()
				rec, err := ds.Put(admin, substrate.ActorPerson, in)
				if err != nil {
					t.Fatal(err)
				}
				return rec
			}
			winner := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "the winner"}})
			loser := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "the loser"}})
			merge, err := ds.Merge(admin, substrate.ActorPerson, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
			if err != nil {
				t.Fatalf("merge: %v", err)
			}
			request := put(substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
				"diff": map[string]any{"properties": map[string]any{"name": "renamed"}}, "target": vocabulary.RecordPath(winner.Kind, winner.ID),
			}})
			if c.narrow {
				if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
					_, err := tx.put(substrate.PutInput{Kind: kindRole, ID: scope.RoleEditor, Properties: map[string]any{
						"name": "Editor", "rules": []any{
							map[string]any{"verbs": []any{"read"}, "kinds": []any{"*"}},
							map[string]any{"verbs": []any{"write"}, "kinds": []any{"x.example.com/p/other"}},
						},
					}})
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			who := c.as(rr)
			caller := rr.as(who)
			head := historyHead(t, ds)
			before := mustGetInternal(t, ds, winner.Kind, winner.ID)
			_, err = ds.Delete(caller, substrate.ActorPerson, winner.Kind, winner.ID, substrate.DeleteInput{Purge: true})
			if c.refused {
				if !errors.Is(err, substrate.ErrNotPermitted) {
					t.Fatalf("the permanent delete answered %v, want refused by a rule", err)
				}
				if got := mustGetInternal(t, ds, winner.Kind, winner.ID); got.DeletedAt != nil || got.Version != before.Version {
					t.Fatalf("the refused delete changed the winner: %+v", got)
				}
				if again := historyHead(t, ds); again != head {
					t.Fatalf("the refused delete wrote %d lines, want none", again-head)
				}
				return
			}
			if err != nil {
				t.Fatalf("the editor's permanent delete: %v", err)
			}
			for _, r := range []eref{{Kind: winner.Kind, ID: winner.ID}, {Kind: loser.Kind, ID: loser.ID}, {Kind: merge.Kind, ID: merge.ID}, {Kind: request.Kind, ID: request.ID}} {
				if row, err := ds.loadRowDB(ctx, r); err != nil || row != nil {
					t.Fatalf("%s %s is %+v (%v) after the editor's permanent delete, want it gone", r.Kind, r.ID, row, err)
				}
			}
			person := actorID(ds.personActorFor(caller, substrate.ActorPerson))
			lines := func(id string) [][2]string {
				t.Helper()
				rows, err := ds.db.QueryContext(ctx, `SELECT actor, on_behalf_of FROM changelog WHERE record_id = $1 AND seq > $2`, id, head)
				if err != nil {
					t.Fatal(err)
				}
				var out [][2]string
				for rows.Next() {
					var a, b string
					if err := rows.Scan(&a, &b); err != nil {
						t.Fatal(err)
					}
					out = append(out, [2]string{a, b})
				}
				if err := errors.Join(rows.Err(), rows.Close()); err != nil {
					t.Fatal(err)
				}
				return out
			}
			for _, l := range lines(winner.ID) {
				if l[0] != person {
					t.Errorf("the winner's line is by %s, want the editor's %s", l[0], person)
				}
			}
			for _, id := range []string{loser.ID, merge.ID, request.ID} {
				got := lines(id)
				if len(got) == 0 {
					t.Fatalf("no line of the delete purged %s", id)
				}
				for _, l := range got {
					if l[0] != actorID(substrate.ActorSystem) || l[1] != person {
						t.Errorf("a line purging %s is by %s for %s, want the engine's for the editor's %s", id, l[0], l[1], person)
					}
				}
			}
		})
	}
}
