package engine

// The registry-dependency lock and the verbs that take it. A vocabulary
// apply commits its rows and then swaps the dataset's registry pointer. Three
// things sit at that boundary and all are pinned here: the swap
// happens under ds.mu held across the commit, so a data write the commit wakes
// at the registry-dependency lock resolves the published declaration; the swap
// runs BEFORE the head signal, so a watcher woken by the kind's changelog
// entry resolves the kind; and the declared indexes are built BEFORE the
// transaction, every definition checked before any is created, so an index the
// engine cannot build refuses the apply with nothing landed and nothing left
// behind. Every other verb that reads a declaration takes the SHARED side of
// the same lock before its own record lock, which is what the rest of this
// file pins: a bundle bind, a delete, a split, a data write and a patch each
// park at the registry-dependency lock and keep what they wrote.
//
// Each case installs its own vocabulary and several plant a hook on the
// dataset, so they do not share a fixture.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const publishPackage = "publish.example.substrate.reamde.dev/publish"

// The registry publishes before the head signal. The seam between the two is
// the only place the order is observable without a race: the broadcaster
// coalesces signals for 300ms, so a subscriber that checked the registry on
// wake would pass against a swap that ran a microsecond after the signal.
func TestTheRegistryPublishesBeforeTheHeadSignal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/beacon"

	// entrySignaled: the apply's transaction reached the signal. published:
	// the live registry resolved the kind at that moment.
	var entrySignaled, published atomic.Bool
	ds.mu.Lock()
	ds.beforeSignal = func(tx *txn) {
		for _, e := range tx.entries {
			if e.kind == kindKind && e.id == kind {
				entrySignaled.Store(true)
				_, ok := ds.registry().ByIdentity(kind)
				published.Store(ok)
			}
		}
	}
	ds.mu.Unlock()

	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "beacon"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !entrySignaled.Load() {
		t.Fatal("the apply signaled no transaction carrying the kind's entry")
	}
	if !published.Load() {
		t.Fatal("the head signaled before the registry published: a watcher woken by the kind's entry resolves the retired registry")
	}
}

// A data write parked at the registry-dependency lock resolves the PUBLISHED
// declaration. The advisory lock releases at the commit and the write's next
// act is ds.registry(); the pointer swap happens under ds.mu held from before
// the commit, so the write blocks there until the candidate is in place. The
// write here carries a property the old declaration admits and the new one
// drops: it must be refused, never landed against the retired shape.
func TestAParkedWriteResolvesThePublishedDeclaration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/gizmo"
	gizmo := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "gizmo"},
				map[string]any{"properties": props}),
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, gizmo(map[string]any{
		"name":   map[string]any{"type": "string"},
		"legacy": map[string]any{"type": "string"},
	})); err != nil {
		t.Fatalf("install: %v", err)
	}

	// The write parks INSIDE the apply's transaction, which holds the lock
	// exclusive from its first statement to its commit. slippedBeforePublish
	// records the write landing while the commit had happened and the swap
	// had not: the window the mutex closes.
	putDone := make(chan error, 1)
	var slippedBeforePublish atomic.Bool
	ds.mu.Lock()
	ds.beforePublish = func(*txn) {
		time.Sleep(400 * time.Millisecond)
		select {
		case err := <-putDone:
			slippedBeforePublish.Store(true)
			putDone <- err
		default:
		}
	}
	ds.mu.Unlock()
	docs, err := parseVocabularyDocs(gizmo(map[string]any{"name": map[string]any{"type": "string"}}))
	if err != nil {
		t.Fatal(err)
	}
	actor := substrate.ActorAPI
	_, err = ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{docs: docs, extra: func(*txn) error {
		go func() {
			_, err := ds.Put(ctx, actor, substrate.PutInput{
				Kind: kind, ID: "racer", Properties: map[string]any{"name": "a", "legacy": "held"},
			})
			putDone <- err
		}()
		// Long enough for the write to reach the lock and park behind this
		// transaction's exclusive side.
		time.Sleep(400 * time.Millisecond)
		select {
		case err := <-putDone:
			return fmt.Errorf("the write did not park at the registry-dependency lock: %w", err)
		default:
			return nil
		}
	}})
	if err != nil {
		t.Fatalf("the narrowing apply: %v", err)
	}
	if slippedBeforePublish.Load() {
		t.Fatal("the parked write landed between the commit and the publish, against the retired declaration")
	}
	select {
	case err := <-putDone:
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "legacy") {
			t.Fatalf("the woken write must be refused against the published declaration, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the parked write never finished")
	}
	if _, err := ds.Get(ctx, kind, "racer"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused write landed a row: %v", err)
	}
}

// An index the engine cannot build refuses the apply whole. `created_at` is a
// column spelled the way indexExpr refuses (columnFor names `createdAt`), and
// the loader does not check index names, so it reaches the CREATE INDEX path.
// That path ran after the commit once: the kind landed, the registry
// published, and the caller was told the apply failed.
func TestAnIndexTheEngineCannotBuildRefusesTheApplyWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/gauge"
	before := maxSeqOf(t, ds)

	_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "gauge"},
			map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"indices":    []any{map[string]any{"properties": []any{"created_at"}}},
			}),
	})
	if err == nil {
		t.Fatal("an apply declaring an index the engine cannot build must be refused")
	}
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("the refusal names the index column as a validation error: %v", err)
	}
	// Nothing landed: no registry entry, no declaration row, no changelog entry.
	if _, ok := ds.registry().ByIdentity(kind); ok {
		t.Fatal("the refused apply published the kind")
	}
	if _, err := ds.Get(ctx, kindKind, kind); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused apply left a declaration row: %v", err)
	}
	if after := maxSeqOf(t, ds); after != before {
		t.Fatalf("the refused apply appended to the changelog: head %d -> %d", before, after)
	}
}

// A refused index definition leaves no index behind. The name is the
// declaration's ordinal, so a first index created under a refusal of the
// second would survive, and a corrected retry that changed the first's
// definition would find the stale one "already there" and report success.
func TestARefusedIndexDefinitionLeavesNoIndexBehind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/dial"

	_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "dial"},
			map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"indices": []any{
					map[string]any{"properties": []any{"name"}},
					map[string]any{"properties": []any{"created_at"}},
				},
			}),
	})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("the second index must refuse the apply: %v", err)
	}
	var n int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname = $1`,
		"idx_"+derivedID(kind, "0")).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the refused apply created the first index before refusing the second")
	}
}

// A bind takes the shared registry-dependency lock before it locks the bundle
// row: the patch it ends in takes the shared side too, but only after the row
// is held, and an upgrade of the same bundle holds the exclusive side while
// locking that row. Started inside an apply's transaction the bind parks, and
// while parked the bundle row is free.
func TestABindParksAtTheRegistryDepLockBeforeItsBundleRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const (
		pkg    = "bindlock.example.substrate.reamde.dev/bindlock"
		config = pkg + "/config"
	)
	closure := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "a bundle with one input to bind",
			"inputs":      map[string]any{"client": map[string]any{"kind": config}},
			"installs":    []any{config},
		}),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "config"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.ActorAPI, closure, nil, substrate.BundleInstall{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: config, ID: "c1", Properties: map[string]any{"name": "one"},
	}); err != nil {
		t.Fatalf("put the config: %v", err)
	}

	// The apply touches an unrelated package, so the only thing between the
	// bind and its commit is the registry-dependency lock the apply holds.
	bound := make(chan error, 1)
	docs, err := parseVocabularyDocs([]map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "lamp"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.applyVocabularyBatch(ctx, substrate.ActorAPI, vocabularyBatch{docs: docs, extra: func(*txn) error {
		go func() { bound <- ds.BindBundleInput(ctx, pkg, "client", "c1") }()
		if err := waitParkedOn(t, ds, registryDepKey(ds), changelogLockKey); err != nil {
			return err
		}
		select {
		case err := <-bound:
			return fmt.Errorf("the bind did not park behind the apply: %w", err)
		default:
		}
		if !rowLockFree(t, ds, kindBundle, pkg) {
			return errors.New("the parked bind holds the bundle row: it queued for the registry lock with the row in hand")
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("the apply: %v", err)
	}
	select {
	case err := <-bound:
		if err != nil {
			t.Fatalf("the woken bind: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the parked bind never finished")
	}
	row, err := ds.Get(ctx, kindBundle, pkg)
	if err != nil {
		t.Fatalf("the bundle row: %v", err)
	}
	if bindings, _ := row.Properties["bindings"].(map[string]any); bindings["client"] == nil {
		t.Fatalf("the bind did not land once the apply published: %v", row.Properties["bindings"])
	}
}

// A stale ordinal index never survives a changed definition. Indexes are built
// before the apply's transaction, so a transaction that fails afterwards
// leaves them; a corrected retry that changes what ordinal zero indexes must
// then rebuild it rather than find the old one "already there".
func TestAChangedIndexDefinitionRebuildsTheStaleOrdinalIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/meter"
	meter := func(indexed string) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "meter"},
				map[string]any{
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"email": map[string]any{"type": "string"},
					},
					"indices": []any{map[string]any{"properties": []any{indexed}}},
				}),
		}
	}
	indexDef := func() string {
		var def string
		if err := ds.db.QueryRowContext(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname = $1`,
			"idx_"+derivedID(kind, "0")).Scan(&def); err != nil {
			t.Fatalf("the ordinal-zero index: %v", err)
		}
		return def
	}

	// The first apply builds the [name] index and then fails inside its
	// transaction, so the index stays and the kind does not land.
	docs, err := parseVocabularyDocs(meter("name"))
	if err != nil {
		t.Fatal(err)
	}
	failed := errors.New("the batch's data write failed")
	_, err = ds.applyVocabularyBatch(ctx, substrate.ActorAPI, vocabularyBatch{docs: docs, extra: func(*txn) error {
		return failed
	}})
	if !errors.Is(err, failed) {
		t.Fatalf("the failing apply: %v", err)
	}
	if def := indexDef(); !strings.Contains(def, "'name'") {
		t.Fatalf("the failed apply did not leave the [name] index this test needs: %s", def)
	}

	// The corrected retry indexes email at the same ordinal.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meter("email")); err != nil {
		t.Fatalf("the corrected apply: %v", err)
	}
	if def := indexDef(); !strings.Contains(def, "'email'") || strings.Contains(def, "'name'") {
		t.Fatalf("the committed kind declares email indexed, but the ordinal-zero index is: %s", def)
	}
}

// THE LOCK ORDER ON THE DELETE AND SPLIT PATHS: registry-dep < subject-type <
// record, the order every put takes. A delete or a split that locked its row
// first and then queued for the shared registry-dependency lock would deadlock
// against a vocabulary apply holding the exclusive side while waiting on that
// row. Both verbs park behind an in-flight apply either way (the changelog
// ordering lock sees to that), so what these tests pin is WHAT THEY HOLD
// while parked: nothing.

// waitParkedOn blocks until some session waits, ungranted, on one of the
// repository's advisory locks named here, or fails after a bound. The verbs
// under test park either at the registry-dependency lock (the order the fix
// establishes) or at the changelog ordering lock (with a row already in hand),
// so waiting for either is what makes the probe that follows meaningful on a
// slow runner: a probe that ran before the verb reached its first lock would
// find every lock free against the unfixed code too. A 64-bit advisory key
// shows in pg_locks as classid (high word) and objid (low word), objsubid 1.
func waitParkedOn(t *testing.T, ds *dataset, names ...string) error {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, name := range names {
			var waiting int
			if err := ds.db.QueryRowContext(ctx, `
				SELECT count(*) FROM pg_locks
				WHERE locktype = 'advisory' AND NOT granted AND objsubid = 1
				  AND classid::bigint = ((hashtext(current_schema() || '|' || $1)::bigint >> 32) & 4294967295)
				  AND objid::bigint = (hashtext(current_schema() || '|' || $1)::bigint & 4294967295)`,
				ds.scope.lockKey(name)).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting > 0 {
				return nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("no session parked on the registry-dependency or changelog lock within the bound")
}

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
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/token"
	token := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "token"},
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
			_, err := ds.Delete(ctx, substrate.ActorAPI, kind, "doomed", substrate.DeleteInput{})
			deleted <- err
		}()
		if err := waitParkedOn(t, ds, registryDepKey(ds), changelogLockKey); err != nil {
			return err
		}
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
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/badge"
	badge := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "badge"},
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
	merged, err := ds.Merge(ctx, substrate.ActorAPI, substrate.MergeInput{Kind: kind, Winner: "keep", Loser: "gone"})
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
			_, err := ds.Split(ctx, substrate.ActorAPI, substrate.SplitInput{Merge: merged.ID})
			split <- err
		}()
		if err := waitParkedOn(t, ds, registryDepKey(ds), changelogLockKey); err != nil {
			return err
		}
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

const refLockPackage = "reflock.example.substrate.reamde.dev/reflock"

// refLockDocs is one authority: a target kind and a holder pointing at it.
// The registry-dependency barrier from the DATA WRITE's side.
//
// A write derives two things from the declaration it resolved: the row's
// properties, and the refs index rows those properties project to. A
// vocabulary apply reprojects the index for the kinds whose reference
// declarations moved, reading the rows COMMITTED at that moment. Without a
// barrier a data write could resolve the old declaration, be missed by that
// reprojection because it had not committed yet, and then commit
// `records.props` carrying a reference with no row in `refs` — a pointer no
// reverse read can see, and nothing says so.
//
// The write therefore takes the SHARED registry-dependency lock before it
// resolves its kind and holds it to commit, and the apply takes the EXCLUSIVE
// side across its reprojection. The cases below drive the interleaving from
// inside the package, holding the exclusive side as the apply does and
// proving the racing write parks rather than slipping past.
func refLockDocs() []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(refLockPackage, 0),
		vocabulary.KindManifest(refLockPackage,
			map[string]any{"singular": "target"},
			map[string]any{}),
		vocabulary.KindManifest(refLockPackage,
			map[string]any{"singular": "holder"},
			map[string]any{"properties": map[string]any{
				"points": map[string]any{"type": "reference", "kind": refLockPackage + "/target"},
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
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, refLockDocs()); err != nil {
		t.Fatalf("install the reflock authority: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: refLockPackage + "/target", ID: "a", Properties: map[string]any{},
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
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(registryDepKey(ds))); err != nil {
		t.Fatal(err)
	}

	const holderID = "h1"
	done := make(chan error, 1)
	go func() {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: refLockPackage + "/holder", ID: holderID,
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
	if !tryLockFree(t, ds, "record|"+refLockPackage+"/holder|"+holderID) {
		t.Fatal("the data write locked its record before the registry-dep lock; the write can still commit across a reprojection")
	}
	// Nothing of it is visible either: neither half of the pair landed.
	if n := refRowsOf(t, ds, refLockPackage+"/holder", holderID, "points"); n != 0 {
		t.Fatalf("the parked write already wrote %d refs rows", n)
	}

	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("the data write did not land once the barrier lifted: %v", err)
	}

	// THE PAIR, together. The row carries the reference and the index carries
	// its row: the outcome #321 asks for is that the refs row is present or the
	// write refused, never silently absent.
	got, err := ds.Get(ctx, refLockPackage+"/holder", holderID)
	if err != nil {
		t.Fatalf("get the holder: %v", err)
	}
	if storedReferencePath(got.Properties["points"]) != vocabulary.RecordPath(refLockPackage+"/target", "a") {
		t.Fatalf("stored points = %#v", got.Properties["points"])
	}
	if n := refRowsOf(t, ds, refLockPackage+"/holder", holderID, "points"); n != 1 {
		t.Fatalf("refs rows for the committed reference = %d, want 1 — the write committed props without its index row", n)
	}
}

// The same barrier on a PATCH: a patch resolves its kind and re-derives the
// whole index for the record it touches, so it needs the lock at the same door
// a put does.
func TestPatchParksAtTheRegistryDepLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, refLockDocs()); err != nil {
		t.Fatalf("install the reflock authority: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: refLockPackage + "/target", ID: "a", Properties: map[string]any{},
	}); err != nil {
		t.Fatalf("put the target: %v", err)
	}
	holder, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: refLockPackage + "/holder", ID: "h2", Properties: map[string]any{},
	})
	if err != nil {
		t.Fatalf("put the holder: %v", err)
	}

	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(registryDepKey(ds))); err != nil {
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
