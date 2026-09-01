package engine

// THE REGISTRY-DEPENDENCY BARRIER, from the DATA WRITE's side (#321).
//
// A write derives two things from the declaration it resolved: the row's
// properties, and the refs index rows those properties project to. A vocabulary
// apply reprojects the index for the kinds whose reference declarations moved,
// reading the rows COMMITTED at that moment. Without a barrier a data write
// could resolve the old declaration, be missed by that reprojection because it
// had not committed yet, and then commit `records.props` carrying a reference
// with no row in `refs` — a pointer no reverse read can see, and nothing says
// so.
//
// The write therefore takes the SHARED registry-dependency lock before it
// resolves its kind and holds it to commit, and the apply takes the EXCLUSIVE
// side across its reprojection. These tests drive the interleaving from inside
// the package, holding the exclusive side as the apply does and proving the
// racing write parks rather than slipping past.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const refLockAuthority = "reflock.example.substrate.reamde.dev"

// refLockDocs is one authority: a target kind and a holder pointing at it.
func refLockDocs() []map[string]any {
	return []map[string]any{
		vocabulary.AuthorityManifest(refLockAuthority, 0),
		vocabulary.KindManifest(refLockAuthority,
			map[string]any{"singular": "target", "plural": "targets"},
			map[string]any{}),
		vocabulary.KindManifest(refLockAuthority,
			map[string]any{"singular": "holder", "plural": "holders"},
			map[string]any{"properties": map[string]any{
				"points": map[string]any{"type": "reference", "kind": refLockAuthority + "/target"},
			}}),
	}
}

// refRowsOf counts the rows the refs index holds for one record at one property.
func refRowsOf(t *testing.T, ds *dataset, kind, id, property string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM refs WHERE src_kind = $1 AND src = $2 AND property = $3`,
		kind, id, property).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A data write cannot commit ACROSS a vocabulary apply's reprojection: it parks
// at the shared registry-dependency lock, and when it lands its refs row lands
// with it. The record's properties and its index rows are never observable apart.
func TestDataWriteParksAtTheRegistryDepLockAndKeepsItsRefsRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, refLockDocs()); err != nil {
		t.Fatalf("install the reflock authority: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: refLockAuthority + "/target", ID: "a", Properties: map[string]any{},
	}); err != nil {
		t.Fatalf("put the target: %v", err)
	}

	// The barrier stands in for the vocabulary apply: it holds the
	// registry-dependency lock EXCLUSIVE, as vocabularywrite.go does from the
	// top of its transaction through reprojectRefs and commit.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, ds.scope.lockKey(registryDepKey(ds))); err != nil {
		t.Fatal(err)
	}

	const holderID = "h1"
	done := make(chan error, 1)
	go func() {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: refLockAuthority + "/holder", ID: holderID,
			Properties: map[string]any{"points": "a"},
		})
		done <- err
	}()

	time.Sleep(400 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("the data write committed across the apply's exclusive lock: %v", err)
	default:
	}
	// It parked at the DEP lock, which is FIRST in the order (registry-dep <
	// subject-type < record), so it has not reached its own record lock. A write
	// that took the dep lock only later would already be holding this one.
	if !tryLockFree(t, ds, "record|"+refLockAuthority+"/holder|"+holderID) {
		t.Fatal("the data write locked its record before the registry-dep lock; the write can still commit across a reprojection")
	}
	// Nothing of it is visible either: neither half of the pair landed.
	if n := refRowsOf(t, ds, refLockAuthority+"/holder", holderID, "points"); n != 0 {
		t.Fatalf("the parked write already wrote %d refs rows", n)
	}

	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("the data write did not land once the barrier lifted: %v", err)
	}

	// THE PAIR, together. The row carries the reference and the index carries
	// its row: the outcome #321 asks for is that the refs row is present or the
	// write refused, never silently absent.
	got, err := ds.Get(ctx, refLockAuthority+"/holder", holderID)
	if err != nil {
		t.Fatalf("get the holder: %v", err)
	}
	if storedReferencePath(got.Properties["points"]) != vocabulary.RecordPath(refLockAuthority+"/target", "a") {
		t.Fatalf("stored points = %#v", got.Properties["points"])
	}
	if n := refRowsOf(t, ds, refLockAuthority+"/holder", holderID, "points"); n != 1 {
		t.Fatalf("refs rows for the committed reference = %d, want 1 — the write committed props without its index row", n)
	}
}

// The same barrier on a PATCH: a patch resolves its kind and re-derives the
// whole index for the record it touches, so it needs the lock at the same door
// a put does.
func TestPatchParksAtTheRegistryDepLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, refLockDocs()); err != nil {
		t.Fatalf("install the reflock authority: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: refLockAuthority + "/target", ID: "a", Properties: map[string]any{},
	}); err != nil {
		t.Fatalf("put the target: %v", err)
	}
	holder, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: refLockAuthority + "/holder", ID: "h2", Properties: map[string]any{},
	})
	if err != nil {
		t.Fatalf("put the holder: %v", err)
	}

	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, ds.scope.lockKey(registryDepKey(ds))); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ds.Patch(ctx, substrate.ActorAPI, holder.Kind, holder.ID, substrate.PatchInput{
			Properties: map[string]any{"points": "a"},
		})
		done <- err
	}()

	time.Sleep(400 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("the patch committed across the apply's exclusive lock: %v", err)
	default:
	}
	if !tryLockFree(t, ds, "record|"+holder.Kind+"|"+holder.ID) {
		t.Fatal("the patch locked its record before the registry-dep lock")
	}

	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("the patch did not land once the barrier lifted: %v", err)
	}
	if n := refRowsOf(t, ds, holder.Kind, holder.ID, "points"); n != 1 {
		t.Fatalf("refs rows after the patch = %d, want 1", n)
	}
}
