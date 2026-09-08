package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE LOCK ORDER ON THE DELETE AND SPLIT PATHS: registry-dep < subject-type <
// record, the order every put takes. A delete or a split that locked its row
// first and then queued for the shared registry-dependency lock would deadlock
// against a vocabulary apply holding the exclusive side while waiting on that
// row. Both verbs park behind an in-flight apply either way (the changelog
// ordering lock sees to that), so what these tests pin is WHAT THEY HOLD
// while parked: nothing.

// rowLockFree reports whether a record's row is free of a FOR UPDATE lock,
// probing with NOWAIT from a transaction of its own.
func rowLockFree(t *testing.T, ds *dataset, kind, id string) bool {
	t.Helper()
	ctx := context.Background()
	probe, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = probe.Rollback() }()
	var one int
	err = probe.QueryRowContext(ctx,
		`SELECT 1 FROM records WHERE kind = $1 AND id = $2 FOR UPDATE NOWAIT`, kind, id).Scan(&one)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

// A delete takes the shared registry-dependency lock before its record lock.
// Started inside the apply's transaction it parks, and while parked its
// record's advisory lock is free: it queued at the registry lock, not behind
// it with the row in hand.
func TestADeleteParksAtTheRegistryDepLockBeforeItsRecordLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	const kind = publishPackage + "/token"
	token := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "token", "plural": "tokens"},
				map[string]any{"properties": props}),
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, token(map[string]any{
		"name": map[string]any{"type": "string"},
	})); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: kind, ID: "doomed", Properties: map[string]any{"name": "doomed"},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	deleted := make(chan error, 1)
	docs, err := parseVocabularyDocs(token(map[string]any{
		"name": map[string]any{"type": "string"},
		"note": map[string]any{"type": "string"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.applyVocabularyBatch(ctx, substrate.ActorAPI, vocabularyBatch{docs: docs, extra: func(*txn) error {
		go func() {
			_, err := ds.Delete(ctx, substrate.ActorAPI, kind, "doomed")
			deleted <- err
		}()
		time.Sleep(400 * time.Millisecond)
		select {
		case err := <-deleted:
			return fmt.Errorf("the delete did not park behind the apply: %w", err)
		default:
		}
		if !tryLockFree(t, ds, "record|"+kind+"|doomed") {
			return errors.New("the parked delete holds its record lock: it queued for the registry lock with the row in hand")
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("the apply: %v", err)
	}
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatalf("the woken delete: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the parked delete never finished")
	}
	// A soft delete leaves a tombstone, which Get still returns.
	gone, err := ds.Get(ctx, kind, "doomed")
	if err != nil || gone.DeletedAt == nil {
		t.Fatalf("the delete did not land once the apply published: %v %+v", err, gone)
	}
}

// A split takes the shared registry-dependency lock before it locks the merge
// record's row. While parked behind the apply, that row is free.
func TestASplitParksAtTheRegistryDepLockBeforeItsRowLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	const kind = publishPackage + "/badge"
	badge := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "badge", "plural": "badges"},
				map[string]any{"properties": props}),
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, badge(map[string]any{
		"name": map[string]any{"type": "string"},
	})); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, id := range []string{"keep", "gone"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: kind, ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	merged, err := ds.Merge(ctx, substrate.ActorAPI, kind, "keep", "gone")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	split := make(chan error, 1)
	docs, err := parseVocabularyDocs(badge(map[string]any{
		"name": map[string]any{"type": "string"},
		"note": map[string]any{"type": "string"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.applyVocabularyBatch(ctx, substrate.ActorAPI, vocabularyBatch{docs: docs, extra: func(*txn) error {
		go func() {
			_, err := ds.Split(ctx, substrate.ActorAPI, merged.ID)
			split <- err
		}()
		time.Sleep(400 * time.Millisecond)
		select {
		case err := <-split:
			return fmt.Errorf("the split did not park behind the apply: %w", err)
		default:
		}
		if !rowLockFree(t, ds, kindRecordMerge, merged.ID) {
			return errors.New("the parked split holds the merge record's row: it queued for the registry lock with the row in hand")
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("the apply: %v", err)
	}
	select {
	case err := <-split:
		if err != nil {
			t.Fatalf("the woken split: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the parked split never finished")
	}
	if _, err := ds.Get(ctx, kind, "gone"); err != nil {
		t.Fatalf("the split did not resurrect the merged-away record once the apply published: %v", err)
	}
}
