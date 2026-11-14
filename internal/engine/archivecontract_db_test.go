package engine_test

// THE ARCHIVE'S CONTRACT (decision 0279, docs/operations.md "The archive's
// contract"), rule by rule: each case is a store that does what the contract
// allows (a request answered with an error and applied later, whole) or a
// database that forgets (restored from a backup), and what the archive must
// still hold.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// delayStore answers the requests hold names with an error, as a
// connection lost before the answer does, and applies them when release
// runs: requests a store applies late, after the client gave up on them.
// down fails every request, as a store the network cannot reach.
type delayStore struct {
	archive.Store
	// root is the directory the store keeps its objects in.
	root string
	mu   sync.Mutex
	hold func(op, key string) bool
	late []func() error
	down atomic.Bool
	// fail answers the gets and lists it names with an error, applied or
	// not, as a store that answers some requests and not others.
	fail func(op, key string) bool
	// puts counts the puts that reached the store, by key.
	puts map[string]int
}

var (
	errLost   = errors.New("the connection was lost before the store answered")
	errRefuse = errors.New("the store refused the request")
)

func newDelayStore(t *testing.T) *delayStore {
	root := filepath.Join(t.TempDir(), "archive")
	d, err := archive.NewDir(root)
	if err != nil {
		t.Fatal(err)
	}
	return &delayStore{Store: d, root: root, hold: holdNothing, fail: holdNothing, puts: map[string]int{}}
}

func (s *delayStore) failing(op, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fail(op, key)
}

func (s *delayStore) setFail(fail func(op, key string) bool) {
	s.mu.Lock()
	s.fail = fail
	s.mu.Unlock()
}

func (s *delayStore) holding(op, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hold(op, key)
}

func (s *delayStore) setHold(hold func(op, key string) bool) {
	s.mu.Lock()
	s.hold = hold
	s.mu.Unlock()
}

func (s *delayStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	if s.down.Load() {
		return errUnreachable
	}
	if !s.holding("put", key) {
		s.mu.Lock()
		s.puts[key]++
		s.mu.Unlock()
		return s.Store.Put(ctx, key, body, size)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.late = append(s.late, func() error {
		return s.Store.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)))
	})
	s.mu.Unlock()
	return errLost
}

func (s *delayStore) Delete(ctx context.Context, key string) error {
	if s.down.Load() {
		return errUnreachable
	}
	if !s.holding("delete", key) {
		return s.Store.Delete(ctx, key)
	}
	s.mu.Lock()
	s.late = append(s.late, func() error { return s.Store.Delete(context.Background(), key) })
	s.mu.Unlock()
	return errLost
}

func (s *delayStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.down.Load() {
		return nil, errUnreachable
	}
	if s.failing("get", key) {
		return nil, errRefuse
	}
	return s.Store.Get(ctx, key)
}

func (s *delayStore) List(ctx context.Context, prefix string) ([]archive.Object, error) {
	if s.down.Load() {
		return nil, errUnreachable
	}
	if s.failing("list", prefix) {
		return nil, errRefuse
	}
	return s.Store.List(ctx, prefix)
}

// putsOf is how many puts reached the store under key.
func (s *delayStore) putsOf(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts[key]
}

// release applies every request held back, in the order they were sent.
func (s *delayStore) release(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	late := s.late
	s.late = nil
	s.mu.Unlock()
	for _, apply := range late {
		if err := apply(); err != nil && !errors.Is(err, archive.ErrNotFound) {
			t.Fatalf("apply a late request: %v", err)
		}
	}
}

func holdNothing(string, string) bool { return false }

// isBlobKey, isManifestKey and isPieceKey name what a request is for.
func isBlobKey(key string) bool { return strings.Contains(key, "/"+changelogfile.BlobsSubdir+"/") }

func isManifestKey(key string) bool {
	k, ok := changelogfile.ParseArchiveKey(key)
	return ok && k.Manifests && k.Name != changelogfile.DeletedMark
}

// backup copies the fixture's database as it stands, the service closed and
// the store down for the close's last pass, so the copy is the database at
// this step: what a restore from a backup taken now brings back.
func (f *archiveFixture) backup(t *testing.T, store *delayStore) string {
	t.Helper()
	store.down.Store(true)
	_ = f.svc.Close()
	store.down.Store(false)
	_ = f.raw.Close()
	copied := testdb.Copy(t, f.dsn)
	f.reconnect(t)
	return copied
}

// restoreDatabase boots the fixture on dsn, a backup, with the same data
// root and store: the database restored with the server stopped, the
// process it replaces stopped with the store down, so its last pass adds
// nothing to what it left.
func (f *archiveFixture) restoreDatabase(t *testing.T, dsn string) {
	t.Helper()
	if store, ok := f.store.(*delayStore); ok {
		store.down.Store(true)
		defer store.down.Store(false)
	}
	_ = f.svc.Close()
	_ = f.raw.Close()
	f.dsn = dsn
	f.reconnect(t)
	if store, ok := f.store.(*delayStore); ok {
		store.down.Store(false)
	}
	f.reopen(t)
}

func (f *archiveFixture) reconnect(t *testing.T) {
	t.Helper()
	raw, err := sql.Open("pgx", f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	f.raw = raw
}

// verifyArchive runs the operator's verify and fails the test unless the
// archive's newest publication verifies whole.
func (f *archiveFixture) verifyArchive(t *testing.T, when string) engine.VerifyReport {
	t.Helper()
	report := mustVerify(t, f.svc, f.id)
	if !report.OK || report.ArchiveUnverified != "" {
		t.Fatalf("%s: the archive does not verify: unverified %q, findings %v", when, report.ArchiveUnverified, report.Findings)
	}
	return report
}

// newestIsWhole fails the test unless the store's newest snapshot restores
// whole, read on its own (rule 8), whatever any database recorded: a copy of
// the store restored into an empty database, every piece held to its
// checksum and every line to its own, every sealed value opened under the
// key, and every blob the restored records hold there with its bytes.
func newestIsWhole(t *testing.T, store *delayStore, id, when string) {
	t.Helper()
	if err := newestSnapshotProblem(t, store, id); err != nil {
		t.Fatalf("%s: %v", when, err)
	}
}

// newestSnapshotProblem is what keeps the store's newest snapshot from
// restoring whole, nil when nothing does.
func newestSnapshotProblem(t *testing.T, store *delayStore, id string) error {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	copyDir(t, store.root, filepath.Join(root, "archive"))
	svc, err := engine.OpenForTest(t, ctx, engine.MigratedDSN(t),
		engine.WithKindsFS(engine.SeedKinds), engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey), engine.WithArchiveInterval(time.Hour))
	if err != nil {
		return fmt.Errorf("restore the newest snapshot into an empty database: %w", err)
	}
	defer func() { _ = svc.Close() }()
	report, err := svc.(engine.Operator).VerifyRepository(ctx, id)
	if err != nil {
		return fmt.Errorf("the newest snapshot restored nothing: %w", err)
	}
	if !report.OK || report.Head == 0 {
		return fmt.Errorf("the restored newest snapshot: head %d, findings %v", report.Head, report.Findings)
	}
	return nil
}

// The check above finds what it must: a blob the newest snapshot's records
// hold, its object gone from the store, fails it, though every piece,
// sealed value and manifest is there.
func TestNewestIsWholeFindsALostBlob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	digest := putBlob(t, f.ds, []byte("bytes the store loses"))
	f.pass(t)
	newestIsWhole(t, store, f.id, "before the loss")
	for _, key := range blobObjects(t, f, digest) {
		if err := store.Store.Delete(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	if err := newestSnapshotProblem(t, store, f.id); err == nil || !strings.Contains(err.Error(), digest) {
		t.Fatalf("a snapshot whose selected blob is gone: %v, want a finding naming %s", err, digest)
	}
}

// collectBlob has the collector take a blob no record references: its
// manifest made older than the upload grace, then a GC run, which
// tombstones the manifest and deletes the server's bytes. The records no
// longer hold its digest, and the next publication drops its object.
func collectBlob(t *testing.T, f *archiveFixture, digest string) {
	t.Helper()
	if _, err := f.raw.Exec(`UPDATE records SET updated_at = now() - interval '1 day' WHERE repository = $1 AND kind = 'substrate.reamde.dev/core/blob' AND id = $2`, f.id, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ds.RunGC(context.Background()); err != nil {
		t.Fatalf("collect the blob: %v", err)
	}
	if rec, err := f.ds.Get(context.Background(), "substrate.reamde.dev/core/blob", digest); err == nil && rec.DeletedAt == nil {
		t.Fatal("the collector kept the blob")
	}
}

// blobObjects is every object of digest under the archive the database
// writes.
func blobObjects(t *testing.T, f *archiveFixture, digest string) []string {
	t.Helper()
	_, aid, _, ok := newestManifest(t, f.store, f.id)
	if !ok {
		return nil
	}
	prefix := changelogfile.ArchivePrefix(f.id, aid)
	listed, err := f.store.List(context.Background(), prefix+changelogfile.BlobsSubdir+"/")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, o := range listed {
		if d, _, ok := changelogfile.ParseBlobObjectName(strings.TrimPrefix(o.Key, prefix)); ok && d == digest {
			out = append(out, o.Key)
		}
	}
	return out
}

// RULE 1 (finding 4 of the fifth review). A delete applied late removes
// only the name it was sent for: the blob's object a publication dropped.
// The same bytes uploaded again, after a restart, go under a name of their
// own pass, so the late delete cannot reach what the manifest now selects.
func TestALateDeleteNeverRemovesWhatALaterManifestSelects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	bytesOf := []byte("bytes uploaded twice")
	digest := putBlob(t, f.ds, bytesOf)
	f.pass(t)
	first := blobObjects(t, f, digest)
	if len(first) != 1 {
		t.Fatalf("the blob's objects after the first pass: %v", first)
	}
	// The blob goes; the publication drops its object, and the delete is
	// lost on the way, to be applied later.
	collectBlob(t, f, digest)
	store.setHold(func(op, key string) bool { return op == "delete" && isBlobKey(key) })
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errLost) {
		t.Fatalf("the pass whose delete was lost: %v", err)
	}
	store.setHold(holdNothing)
	f.reopen(t)
	// The same bytes again: a new pass in a new process puts them under its
	// own name.
	if got := putBlob(t, f.ds, bytesOf); got != digest {
		t.Fatalf("the same bytes got digest %s", got)
	}
	f.pass(t)
	store.release(t)
	again := blobObjects(t, f, digest)
	if len(again) != 1 || again[0] == first[0] {
		t.Fatalf("the blob's objects after the late delete: %v, want one other than %s", again, first[0])
	}
	f.verifyArchive(t, "after the late delete landed")
}

// RULE 1. A put applied late, after the process that sent it is gone, lands
// under a name of a pass nothing selects: the archive verifies, and the
// object is counted as one the record does not hold.
func TestALatePutAfterARestartIsNeverSelected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	f.pass(t)
	putBlob(t, f.ds, []byte("bytes whose first put is lost"))
	store.setHold(func(op, key string) bool { return op == "put" && isBlobKey(key) })
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errLost) {
		t.Fatalf("the pass whose put was lost: %v", err)
	}
	store.setHold(holdNothing)
	f.reopen(t)
	f.pass(t)
	// Whole before the late put lands: nothing selects what it carries.
	f.verifyArchive(t, "before the late put landed")
	store.release(t)
	report := f.verifyArchive(t, "after the late put landed")
	if report.ArchiveUnaccounted == 0 {
		t.Fatalf("the late put is not counted as an object the record does not hold: %+v", report)
	}
}

// RULE 5. A manifest of a lower epoch that the record does not hold, one a
// forgotten future put and whose put landed late, is left where it is and
// counted, never deleted: no pass deletes what the record does not hold.
func TestAManifestTheRecordDoesNotHoldIsLeftAndCounted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	f.pass(t)
	backup := f.backup(t, store)
	f.reopen(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "forgotten"}})
	store.setHold(func(op, key string) bool { return op == "put" && isManifestKey(key) })
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errLost) {
		t.Fatalf("the forgotten pass: %v", err)
	}
	store.setHold(holdNothing)
	f.restoreDatabase(t, backup)
	for _, name := range []string{"one", "two"} {
		mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": name}})
		f.pass(t)
	}
	before, err := store.List(ctx, changelogfile.ManifestsPrefix(f.id))
	if err != nil {
		t.Fatal(err)
	}
	store.release(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "three"}})
	f.pass(t)
	after, err := store.List(ctx, changelogfile.ManifestsPrefix(f.id))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("%d manifests after the late one landed and a pass ran, were %d: the forgotten one was deleted", len(after), len(before))
	}
	if report := f.verifyArchive(t, "beside the forgotten manifest"); report.ArchiveUnaccounted == 0 {
		t.Fatal("the forgotten manifest is not counted as one the record does not hold")
	}
}

// objectRow is one row of the record's objects, as a test reads it.
type objectRow struct {
	name, holds, state string
}

// objectRows is the record's rows of the archive the database writes.
func (f *archiveFixture) objectRows(t *testing.T) []objectRow {
	t.Helper()
	rows, err := f.raw.Query(`SELECT name, holds, state FROM archive_objects WHERE repository = $1 ORDER BY name`, f.id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []objectRow
	for rows.Next() {
		var r objectRow
		if err := rows.Scan(&r.name, &r.holds, &r.state); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// recordArchive is the archive the record names, its published manifest and
// whether a publication is about to be made.
func (f *archiveFixture) recordArchive(t *testing.T) (archiveID, published string, pending []byte) {
	t.Helper()
	var pub sql.NullString
	if err := f.raw.QueryRow(`SELECT archive, published, pending FROM archive_records WHERE repository = $1`, f.id).Scan(&archiveID, &pub, &pending); err != nil {
		t.Fatal(err)
	}
	return archiveID, pub.String, pending
}

// publishedBytes is the record's bytes of the manifest it published.
func (f *archiveFixture) publishedBytes(t *testing.T) []byte {
	t.Helper()
	var data []byte
	if err := f.raw.QueryRow(`SELECT published_bytes FROM archive_records WHERE repository = $1`, f.id).Scan(&data); err != nil {
		t.Fatal(err)
	}
	return data
}

// holdsObject reports whether the store holds the object a row names.
func (f *archiveFixture) holdsObject(t *testing.T, store *delayStore, r objectRow) bool {
	t.Helper()
	archiveID, _, _ := f.recordArchive(t)
	key := changelogfile.ArchivePrefix(f.id, archiveID) + r.name
	if r.holds == "manifest" {
		key = changelogfile.ArchiveManifestsPrefix(f.id, archiveID) + r.name
	}
	_, err := store.Store.Get(context.Background(), key)
	return err == nil
}

// RULES 1, 2, 3 AND 5, THE CRASH TABLE OF A PASS. A pass stopped at every
// boundary the table names (each a seam that stops exactly there), the
// process gone: the state the stop leaves is asserted before the restart,
// and the next process's first pass finishes it: the archive holds the table
// at its head, verifies, keeps nothing the record holds to delete, and holds
// no object the record does not.
func TestAPassStoppedBetweenAnyTwoStepsIsFinishedByTheNext(t *testing.T) {
	t.Parallel()
	someRow := func(t *testing.T, f *archiveFixture, store *delayStore, state string, inStore bool) {
		t.Helper()
		for _, r := range f.objectRows(t) {
			if r.state == state && f.holdsObject(t, store, r) == inStore {
				return
			}
		}
		t.Fatalf("no row says %s with its object in the store = %v: %+v", state, inStore, f.objectRows(t))
	}
	for _, c := range []struct {
		stage string
		// left holds the state the stop leaves.
		left func(t *testing.T, f *archiveFixture, store *delayStore, before string)
	}{
		{engine.ArchiveAfterListing, func(t *testing.T, f *archiveFixture, _ *delayStore, before string) {
			if _, published, pending := f.recordArchive(t); published != before || pending != nil {
				t.Fatalf("the listing alone moved the record: published %q, pending %v", published, pending != nil)
			}
		}},
		{engine.ArchiveAfterObjectRow, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "putting", false)
		}},
		{engine.ArchiveAfterObjectPut, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "putting", true)
		}},
		{engine.ArchiveAfterBlobs, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "put", true)
		}},
		{engine.ArchiveAfterSealed, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "put", true)
		}},
		{engine.ArchiveAfterPiece, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "put", true)
		}},
		{engine.ArchiveAfterPending, func(t *testing.T, f *archiveFixture, store *delayStore, before string) {
			_, published, pending := f.recordArchive(t)
			if pending == nil || published != before {
				t.Fatalf("the record after the pending record: published %q, pending %v", published, pending != nil)
			}
			someRow(t, f, store, "putting", false)
		}},
		{engine.ArchiveAfterManifest, func(t *testing.T, f *archiveFixture, store *delayStore, before string) {
			_, published, pending := f.recordArchive(t)
			if pending == nil || published != before {
				t.Fatalf("the record after the manifest's put: published %q, pending %v", published, pending != nil)
			}
			someRow(t, f, store, "putting", true)
		}},
		{engine.ArchiveAfterPublished, func(t *testing.T, f *archiveFixture, store *delayStore, before string) {
			if _, published, pending := f.recordArchive(t); pending != nil || published == before {
				t.Fatalf("the record after the publication: published %q (was %q), pending %v", published, before, pending != nil)
			}
			someRow(t, f, store, "dropped", true)
		}},
		{engine.ArchiveAfterMark, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "dropped", true)
		}},
		{engine.ArchiveAfterDeleteRow, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "deleting", true)
		}},
		{engine.ArchiveAfterDeleteHit, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			someRow(t, f, store, "deleting", false)
		}},
		{engine.ArchiveAfterDelete, func(t *testing.T, f *archiveFixture, store *delayStore, _ string) {
			for _, r := range f.objectRows(t) {
				if r.state == "deleting" {
					t.Fatalf("a delete's row outlived its delete: %+v", r)
				}
			}
			someRow(t, f, store, "dropped", true)
		}},
	} {
		t.Run(c.stage, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := newDelayStore(t)
			f := newArchiveFixture(t, store, engine.WithArchivePieceBytes(1))
			gone := putBlob(t, f.ds, []byte("bytes the next publication drops"))
			sgPutProvider(t, f.ds, sgPlainKey)
			f.pass(t)
			collectBlob(t, f, gone)
			putBlob(t, f.ds, []byte("bytes the next publication adds"))
			sgPutProvider(t, f.ds, "sk-changed")
			_, before, _ := f.recordArchive(t)
			f.failAt.Store(c.stage)
			if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errStopped) {
				t.Fatalf("the pass did not stop %s: %v", c.stage, err)
			}
			c.left(t, f, store, before)
			store.down.Store(true)
			f.failAt.Store("")
			f.reopen(t)
			store.down.Store(false)
			f.pass(t)
			sealed, blobs := f.held(t)
			f.archived(t).equals(t, "after the next process's pass", f.tableHead(t), sealed, blobs)
			report := f.verifyArchive(t, "after the next process's pass")
			if report.ArchiveUnaccounted != 0 {
				t.Fatalf("objects the record does not hold are left: %d", report.ArchiveUnaccounted)
			}
			for _, r := range f.objectRows(t) {
				if r.state != "put" {
					t.Fatalf("the record holds %s %s after the next pass", r.state, r.name)
				}
			}
		})
	}
}

// RULE 3, A PUBLICATION'S RECOVERY AT ITS OWN BOUNDARIES. A manifest
// recorded as about to be published, the process gone before its put: the
// next process's recovery stopped after it put the same bytes again and
// before it recorded them, and after it recorded them, is finished by the
// process after. A manifest whose put landed before the process went is
// recorded without being put again.
func TestAPublicationsRecoveryStoppedAtEachBoundaryIsFinished(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// first is where the first process stops; recovery where the second
		// does, "" for nowhere.
		first, recovery string
		putsAgain       bool
	}{
		{"recovery stopped after its put", engine.ArchiveAfterPending, engine.ArchiveAfterManifest, true},
		{"recovery stopped after its record", engine.ArchiveAfterPending, engine.ArchiveAfterPublished, true},
		{"the put landed before the process went", engine.ArchiveAfterManifest, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := newDelayStore(t)
			f := newArchiveFixture(t, store)
			f.pass(t)
			_, before, _ := f.recordArchive(t)
			beforeBytes := f.publishedBytes(t)
			mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "pending"}})
			f.failAt.Store(c.first)
			if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errStopped) {
				t.Fatalf("the first process did not stop %s: %v", c.first, err)
			}
			archiveID, _, pending := f.recordArchive(t)
			if pending == nil {
				t.Fatal("the record holds no manifest about to be published")
			}
			m, err := changelogfile.DecodeArchiveManifest(pending, f.id)
			if err != nil {
				t.Fatal(err)
			}
			name := changelogfile.ArchiveManifestName(m.Epoch, pending)
			key := changelogfile.ArchiveManifestsPrefix(f.id, archiveID) + name
			// What each stop leaves durable: the record's pending and
			// published bytes, and whether the store holds the manifest.
			durable := func(when string, wantPending []byte, wantPublished string, wantPublishedBytes []byte, wantPut bool) {
				t.Helper()
				_, published, nowPending := f.recordArchive(t)
				if !bytes.Equal(nowPending, wantPending) || published != wantPublished || !bytes.Equal(f.publishedBytes(t), wantPublishedBytes) {
					t.Fatalf("%s: the record holds pending %d byte(s), published %q, want %d byte(s) pending and %q published, its bytes the same",
						when, len(nowPending), published, len(wantPending), wantPublished)
				}
				data, err := archive.ReadAll(ctx, store, key)
				switch {
				case wantPut && (err != nil || !bytes.Equal(data, pending)):
					t.Fatalf("%s: the store's manifest %s: %v, want the pending bytes", when, name, err)
				case !wantPut && !errors.Is(err, archive.ErrNotFound):
					t.Fatalf("%s: the store holds manifest %s (%v), which no put reached yet", when, name, err)
				}
			}
			durable("the first process stopped "+c.first, pending, before, beforeBytes, c.first == engine.ArchiveAfterManifest)
			putsBefore := store.putsOf(key)
			store.down.Store(true)
			f.failAt.Store(c.recovery)
			f.reopen(t)
			store.down.Store(false)
			if c.recovery != "" {
				if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errStopped) {
					t.Fatalf("the recovery did not stop %s: %v", c.recovery, err)
				}
				if c.recovery == engine.ArchiveAfterPublished {
					durable("the recovery stopped "+c.recovery, nil, name, pending, true)
				} else {
					durable("the recovery stopped "+c.recovery, pending, before, beforeBytes, true)
				}
				store.down.Store(true)
				f.failAt.Store("")
				f.reopen(t)
				store.down.Store(false)
			}
			f.failAt.Store("")
			f.pass(t)
			if _, published, pending := f.recordArchive(t); pending != nil || published == "" {
				t.Fatalf("after the recovery: published %q, pending %v", published, pending != nil)
			}
			if again := store.putsOf(key) > putsBefore; again != c.putsAgain {
				t.Fatalf("the recovery put the same bytes again = %v, want %v", again, c.putsAgain)
			}
			f.verifyArchive(t, "after the recovery")
		})
	}
}

// RULE 3. A manifest recorded as about to be published and lost on the
// way is put again, the same bytes under the same name, by the next pass of
// the next process, and published; the late put of the first try lands on
// the same bytes.
func TestThePendingManifestIsPutAgainWithTheSameBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	f.pass(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "pending"}})
	store.setHold(func(op, key string) bool { return op == "put" && isManifestKey(key) })
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errLost) {
		t.Fatalf("the pass whose manifest was lost: %v", err)
	}
	var pending []byte
	if err := f.raw.QueryRow(`SELECT pending FROM archive_records WHERE repository = $1`, f.id).Scan(&pending); err != nil || pending == nil {
		t.Fatalf("the record holds no manifest about to be published (%v)", err)
	}
	m, err := changelogfile.DecodeArchiveManifest(pending, f.id)
	if err != nil {
		t.Fatal(err)
	}
	want := changelogfile.ArchiveManifestName(m.Epoch, pending)
	store.setHold(holdNothing)
	store.down.Store(true)
	f.reopen(t)
	store.down.Store(false)
	f.pass(t)
	var published string
	if err := f.raw.QueryRow(`SELECT published FROM archive_records WHERE repository = $1`, f.id).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != want {
		t.Fatalf("the record published %s, want the pending %s put again", published, want)
	}
	store.release(t)
	f.pass(t)
	f.verifyArchive(t, "after the first try landed")
}

// RULE 4 AND THE CONTRACT'S LAST SECTION. A database restored from a backup
// taken at any step of a pass forgets what it did after. Whatever the
// forgotten future put, deleted or published, and whenever its requests
// land, nothing the archive's newest manifest selects is removed: the
// restored database's pass either stops at what it does not recognize or
// goes on in an archive that verifies.
func TestADatabaseRestoredFromABackupAtEachStepOfAPassLosesNothingTheArchiveSelects(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		"", engine.ArchiveAfterBlobs, engine.ArchiveAfterPiece, engine.ArchiveAfterPending,
		engine.ArchiveAfterManifest, engine.ArchiveAfterPublished, engine.ArchiveAfterDelete,
	} {
		name := stage
		if name == "" {
			name = "between passes"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := newDelayStore(t)
			f := newArchiveFixture(t, store, engine.WithArchivePieceBytes(1))
			gone := putBlob(t, f.ds, []byte("bytes a forgotten publication drops"))
			sgPutProvider(t, f.ds, sgPlainKey)
			f.pass(t)
			collectBlob(t, f, gone)
			sgPutProvider(t, f.ds, "sk-forgotten")
			if stage != "" {
				f.failAt.Store(stage)
				if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errStopped) {
					t.Fatalf("the pass did not stop %s: %v", stage, err)
				}
				f.failAt.Store("")
			}
			backup := f.backup(t, store)

			// The forgotten future: it finishes that pass and makes more,
			// every delete and every manifest it sends lost on the way.
			f.reopen(t)
			mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "forgotten"}})
			store.setHold(func(op, key string) bool { return op == "delete" || (op == "put" && isManifestKey(key)) })
			_ = engine.ArchivePass(ctx, f.ds)
			store.setHold(func(op, key string) bool { return op == "delete" })
			_ = engine.ArchivePass(ctx, f.ds)
			store.setHold(holdNothing)

			// The database comes back from the backup, with the server
			// stopped, and its passes run while the forgotten requests land.
			f.restoreDatabase(t, backup)
			mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "after the restore"}})
			first := engine.ArchivePass(ctx, f.ds)
			store.release(t)
			second := engine.ArchivePass(ctx, f.ds)
			for _, err := range []error{first, second} {
				if err != nil && !errors.Is(err, engine.ErrArchiveDiverged) {
					t.Fatalf("a pass of the restored database: %v", err)
				}
			}
			if second == nil {
				f.verifyArchive(t, "the restored database's archive")
			}
			newestIsWhole(t, store, f.id, "after every forgotten request landed")
		})
	}
}

// RULE 4. Every listing that does not show the archive as the database left
// it stops the passes, which write and delete nothing: a manifest of the
// same epoch with other bytes, a manifest of a newer archive, and the
// newest manifest hidden while the archive's objects are there (finding 1 of
// the fifth review: a listing that shows nothing is no proof of an empty
// store). A store that holds nothing of the repository at all was emptied or
// replaced: a new archive starts in it.
func TestAListingThatIsNotTheRecordsStopsThePasses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(t *testing.T, f *archiveFixture, store *delayStore)
	}{
		{"two manifests of one epoch", func(t *testing.T, f *archiveFixture, store *delayStore) {
			putForeignManifest(t, store, f.id, func(m *changelogfile.Manifest) { m.Pass = "fedcba9876543210" })
		}},
		{"a newer archive", func(t *testing.T, f *archiveFixture, store *delayStore) {
			putForeignManifest(t, store, f.id, func(m *changelogfile.Manifest) {
				newer, err := changelogfile.NewArchiveID(time.Now().Add(time.Hour), m.Archive)
				if err != nil {
					t.Fatal(err)
				}
				m.Archive = newer
			})
		}},
		{"the newest manifest hidden", func(t *testing.T, f *archiveFixture, store *delayStore) {
			_, aid, name, _ := newestManifest(t, store, f.id)
			if err := store.Store.Delete(context.Background(), changelogfile.ArchiveManifestsPrefix(f.id, aid)+name); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := newDelayStore(t)
			f := newArchiveFixture(t, store)
			putBlob(t, f.ds, []byte("bytes the stopped pass must not touch"))
			f.pass(t)
			c.change(t, f, store)
			before, err := store.List(ctx, changelogfile.RepositoryPrefix(f.id))
			if err != nil {
				t.Fatal(err)
			}
			mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "beside it"}})
			f.reopen(t)
			if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, engine.ErrArchiveDiverged) {
				t.Fatalf("a pass over %s: %v, want ErrArchiveDiverged", c.name, err)
			}
			after, err := store.List(ctx, changelogfile.RepositoryPrefix(f.id))
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) {
				t.Fatalf("the stopped pass changed the store: %d objects, were %d", len(after), len(before))
			}
			if st, _ := engine.ArchiveStatusOf(f.ds); st.Stopped == "" {
				t.Fatalf("the status does not say the passes stopped: %+v", st)
			}
		})
	}

	t.Run("nothing at all", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		store := newDelayStore(t)
		f := newArchiveFixture(t, store)
		f.pass(t)
		_, before, _, _ := newestManifest(t, store, f.id)
		everything, err := store.List(ctx, changelogfile.RepositoryPrefix(f.id))
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range everything {
			if err := store.Store.Delete(ctx, o.Key); err != nil {
				t.Fatal(err)
			}
		}
		mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "after the store was emptied"}})
		f.reopen(t)
		f.pass(t)
		if _, after, _, ok := newestManifest(t, store, f.id); !ok || after <= before {
			t.Fatalf("the pass over an emptied store published in archive %q, want a new one past %s", after, before)
		}
		f.verifyArchive(t, "after the store was emptied")
	})
}

// FINDING 2 OF THE FIFTH REVIEW. A database restored from a backup draws an
// epoch its forgotten future drew too, for a manifest whose put was still
// in flight. The two manifests carry their bytes' hashes in their names, so
// the late put lands beside the restored database's and overwrites
// nothing, and the next pass stops at two manifests of one epoch.
func TestAnEpochDrawnTwiceAcrossARestoreOverwritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	f.pass(t)
	backup := f.backup(t, store)
	f.reopen(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "forgotten"}})
	store.setHold(func(op, key string) bool { return op == "put" && isManifestKey(key) })
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errLost) {
		t.Fatalf("the forgotten pass: %v", err)
	}
	store.setHold(holdNothing)
	f.restoreDatabase(t, backup)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "remembered"}})
	f.pass(t)
	m, aid, name, _ := newestManifest(t, store, f.id)
	key := changelogfile.ArchiveManifestsPrefix(f.id, aid) + name
	ours, err := archive.ReadAll(ctx, store, key)
	if err != nil {
		t.Fatal(err)
	}
	store.release(t)
	if after, err := archive.ReadAll(ctx, store, key); err != nil || !bytes.Equal(after, ours) {
		t.Fatalf("the late put of epoch %d overwrote the restored database's manifest (%v)", m.Epoch, err)
	}
	// No write after it: an idle pass lists as every pass does, and stops
	// (finding 2 of the archive's sixth review).
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, engine.ErrArchiveDiverged) {
		t.Fatalf("an idle pass beside two manifests of epoch %d: %v, want ErrArchiveDiverged", m.Epoch, err)
	}
	if st, _ := engine.ArchiveStatusOf(f.ds); st.Stopped == "" {
		t.Fatalf("the status does not say the passes stopped: %+v", st)
	}
}

// RULE 8. A verify that stops short of counting what the store holds and
// the record does not says the count is unknown, a finding, and never zero:
// a store that will not list the repository's manifests, one that will not
// give the published manifest, and one that answers the manifest and the
// objects it selects and will not list the archive whole.
func TestVerifyDoesNotCountWhatItCannotList(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		fail func(id, archiveID string) func(op, key string) bool
		says string
	}{
		{"the manifests' listing fails", func(id, _ string) func(op, key string) bool {
			return func(op, key string) bool { return op == "list" && key == changelogfile.ManifestsPrefix(id) }
		}, "could not be read"},
		{"the published manifest does not read", func(id, archiveID string) func(op, key string) bool {
			return func(op, key string) bool {
				return op == "get" && strings.HasPrefix(key, changelogfile.ArchiveManifestsPrefix(id, archiveID))
			}
		}, "does not read"},
		{"the archive's listing fails", func(id, archiveID string) func(op, key string) bool {
			return func(op, key string) bool { return op == "list" && key == changelogfile.ArchivePrefix(id, archiveID) }
		}, "could not be counted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			store := newDelayStore(t)
			f := newArchiveFixture(t, store)
			f.pass(t)
			archiveID, _, _ := f.recordArchive(t)
			store.setFail(c.fail(f.id, archiveID))
			report := mustVerify(t, f.svc, f.id)
			if report.OK || report.ArchiveID != archiveID || report.ArchiveUnaccounted != -1 || !strings.Contains(report.ArchiveUnverified, c.says) {
				t.Fatalf("verify: ok %v, archive %q, unaccounted %d, unverified %q; want not ok, archive %s, -1 and %q",
					report.OK, report.ArchiveID, report.ArchiveUnaccounted, report.ArchiveUnverified, archiveID, c.says)
			}
		})
	}
}

// RULE 4, TAKING THE ARCHIVE. Nothing of the database goes until the whole
// snapshot it will install has been read, checked and is on local disk: a
// store that fails one read when the choice is made changes nothing and
// records nothing; the choice made and recorded, a boot stopped between the
// staging and the replacement leaves the repository's rows and files, the
// recorded choice and the whole staged copy, and the next boot replaces the
// repository from that copy alone, with the store unreachable, ends the take
// and removes the copy.
func TestTakingTheArchiveRemovesNothingUntilItsSnapshotIsStaged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	sgPutProvider(t, f.ds, sgPlainKey)
	putBlob(t, f.ds, []byte("a blob the take holds on to until its copy is staged"))
	f.pass(t)
	backup := f.backup(t, store)
	f.reopen(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the archive's"}})
	want := foldOf(t, f.ds)
	f.pass(t)
	f.restoreDatabase(t, backup)
	kept := foldOf(t, f.ds)
	_ = f.svc.Close()

	// One sealed value the store will not give: the choice is refused and
	// nothing moves.
	store.setFail(func(op, key string) bool {
		return op == "get" && strings.Contains(key, "/"+changelogfile.SealedSubdir+"/")
	})
	op := f.operator(t)
	if _, err := op.ResolveArchive(ctx, f.id, false); err == nil {
		t.Fatal("a take whose snapshot could not be read was recorded")
	}
	_ = op.Close()
	if c := takeOf(t, f.raw, f.id); c.stage != "" {
		t.Fatalf("the refused take was recorded: %+v", c)
	}
	store.setFail(holdNothing)
	f.reopen(t)
	if got := foldOf(t, f.ds); string(got) != string(kept) {
		t.Fatal("the refused take changed the repository")
	}
	_ = f.svc.Close()

	// The choice made, the snapshot staged and the choice recorded; the
	// boot stopped right after the staging removed nothing.
	op = f.operator(t)
	res, err := op.ResolveArchive(ctx, f.id, false)
	if err != nil {
		t.Fatalf("take the archive: %v", err)
	}
	_ = op.Close()
	choice := takeOf(t, f.raw, f.id)
	if choice.stage == "" || choice.archive+"/"+choice.manifest != res.Take {
		t.Fatalf("the recorded take is %+v, want %s", choice, res.Take)
	}
	holdStagedWhole(t, f.root, f.id, choice)
	before := repositoryState(t, f.raw, f.root, f.id)
	store.down.Store(true)
	if _, err := engine.OpenForTest(t, ctx, f.dsn, engine.WithKindsFS(engine.SeedKinds), engine.WithDataRoot(f.root),
		engine.WithCredentialKey(engine.TestCredentialKey), engine.WithArchive(store), engine.WithArchiveInterval(time.Hour),
		engine.WithTestImportFault(0, func(stage string) error {
			if stage == engine.TakeAfterStaged {
				return errStopped
			}
			return nil
		})); !errors.Is(err, errStopped) {
		t.Fatalf("the boot did not stop after the staging: %v", err)
	}
	if after := repositoryState(t, f.raw, f.root, f.id); after != before {
		t.Fatalf("the boot stopped after the staging changed the repository's rows or files:\nbefore %s\nafter  %s", before, after)
	}
	if again := takeOf(t, f.raw, f.id); again != choice {
		t.Fatalf("the boot stopped after the staging left the take %+v, want %+v", again, choice)
	}
	holdStagedWhole(t, f.root, f.id, choice)

	f.reopen(t)
	if got := foldOf(t, f.ds); string(got) != string(want) {
		t.Fatalf("the take from the staged copy, the store unreachable, is not the archive's\n%s", firstDifference(want, got))
	}
	holdTakeEnded(t, f.raw, f.root, f.id)
	store.down.Store(false)
	f.pass(t)
	f.verifyArchive(t, "after the take")
}

// RULES 4 AND 7. A take ends in the transaction that finishes its restore,
// on a restore resumed by a later boot as on one that ran through, and a
// deletion ends a take not yet carried out; either way the copy goes, and
// a deleted repository never comes back from a copy staged before its
// deletion (finding 1 of the re-check of step 7's sixth amend).
func TestATakeEndsWithItsRestoreAndADeletionEndsATake(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name       string
		carriedOut bool
	}{
		{"the take's restore resumed, then the deletion", true},
		{"the deletion before the take was carried out", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			svc, dsn := newService(t)
			repository := testdb.Repository(t)
			user, _, _ := registerUser(t, svc, repository)
			ds, err := svc.Dataset(ctx, repository)
			if err != nil {
				t.Fatal(err)
			}
			mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/llm/memory", Properties: map[string]any{"text": "taken, then deleted"}})
			if err := engine.ArchivePass(ctx, ds); err != nil {
				t.Fatal(err)
			}
			root, id := engine.DataRootOf(svc), ds.Repository().ID
			_ = svc.Close()
			raw := rawDB(t, dsn)

			op, err := bootWith(t, dsn, root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := op.(engine.Operator).ResolveArchive(ctx, repository, false); err != nil {
				t.Fatalf("take the archive: %v", err)
			}
			deleting := op
			if c.carriedOut {
				_ = op.Close()
				// The boot that carries the take out stops right after the
				// restored repository's row committed: the old rows are
				// gone, and the record of the take is what the next boot
				// acts on.
				if _, err := bootWith(t, dsn, root, engine.WithTestImportFault(0, func(stage string) error {
					if stage == engine.ImportAfterRow {
						return errKilledHere
					}
					return nil
				})); !errors.Is(err, errKilledHere) {
					t.Fatalf("the take's restore did not stop after its row: %v", err)
				}
				if takeOf(t, raw, id).stage == "" {
					t.Fatal("the take's record went with the repository's rows")
				}
				if deleting, err = bootWith(t, dsn, root); err != nil {
					t.Fatalf("the boot that resumes the take's restore: %v", err)
				}
				holdTakeEnded(t, raw, root, id)
			}
			waitStep(t)
			if err := deleting.DeleteRepository(ctx, substrate.LoginInput{
				Identity: repository, Repository: repository, Password: testPassword, TOTPCode: user.code(t),
			}); err != nil {
				t.Fatalf("delete: %v", err)
			}
			_ = deleting.Close()
			holdTakeEnded(t, raw, root, id)
			for range 2 {
				after, err := bootWith(t, dsn, root)
				if err != nil {
					t.Fatalf("the boot after the deletion: %v", err)
				}
				if _, err := after.Dataset(ctx, repository); !errors.Is(err, substrate.ErrNotFound) {
					t.Fatalf("the deleted repository after a boot = %v, want ErrNotFound", err)
				}
				_ = after.Close()
			}
		})
	}
}

// RULE 4, A STAGED COPY HAS NO AUTHORITY OF ITS OWN. A take installs only
// the copy staged for the choice the database recorded. Two choices are made
// in one operator's process, with no boot between them to carry the first
// out: a later choice staged and never recorded is removed by the next boot
// and never installed; a later choice whose staging fails leaves the
// recorded one and its copy as they were; and a copy that is not the
// recorded choice's, under its name (a database restored beside a data root
// of another time), is never installed: the recorded manifest, gone from the
// store, cannot be staged again, and the repository is served as it was
// (finding 1 of the re-check of step 7's sixth amend).
func TestATakeInstallsOnlyTheCopyStagedForItsRecordedChoice(t *testing.T) {
	t.Parallel()
	for _, c := range []string{"a later choice never recorded", "a later choice whose staging fails", "a copy of another choice under the recorded one's name"} {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			tw := newTwoTakes(t)
			f, store := tw.f, tw.store
			switch c {
			case "a later choice never recorded":
				later := tw.stageLater(t)
				_ = tw.op.Close()
				f.reopen(t)
				if got := foldOf(t, f.ds); string(got) != string(tw.first) {
					t.Fatalf("the boot did not install the recorded choice\n%s", firstDifference(tw.first, got))
				}
				holdTakeEnded(t, f.raw, f.root, f.id)
				if _, err := os.Stat(filepath.Join(f.root, "archive-take", f.id, later)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("the copy staged for a choice never recorded outlived the boot: %v", err)
				}
			case "a later choice whose staging fails":
				store.setFail(func(op, key string) bool {
					return op == "get" && strings.Contains(key, "/"+changelogfile.ChangelogSubdir+"/")
				})
				if _, err := tw.op.ResolveArchive(ctx, f.id, false); err == nil {
					t.Fatal("a take whose snapshot could not be read was recorded")
				}
				store.setFail(holdNothing)
				_ = tw.op.Close()
				if got := takeOf(t, f.raw, f.id); got != tw.choice {
					t.Fatalf("the failed choice left the take %+v, want %+v", got, tw.choice)
				}
				holdStagedWhole(t, f.root, f.id, tw.choice)
				if entries, err := os.ReadDir(filepath.Join(f.root, "archive-take", f.id)); err != nil || len(entries) != 1 {
					t.Fatalf("the failed choice left %d staged copies (%v), want the recorded one alone", len(entries), err)
				}
				f.reopen(t)
				if got := foldOf(t, f.ds); string(got) != string(tw.first) {
					t.Fatalf("the boot did not install the recorded choice\n%s", firstDifference(tw.first, got))
				}
			case "a copy of another choice under the recorded one's name":
				later := tw.stageLater(t)
				_ = tw.op.Close()
				recorded := filepath.Join(f.root, "archive-take", f.id, tw.choice.stage)
				if err := os.RemoveAll(recorded); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(f.root, "archive-take", f.id, later), recorded); err != nil {
					t.Fatal(err)
				}
				// The recorded manifest is gone from the store, as the
				// archive's next publication deletes it.
				key := changelogfile.ArchiveManifestsPrefix(f.id, tw.choice.archive) + tw.choice.manifest
				if err := store.Store.Delete(ctx, key); err != nil && !errors.Is(err, archive.ErrNotFound) {
					t.Fatal(err)
				}
				f.reopen(t)
				if got := foldOf(t, f.ds); string(got) != string(tw.kept) {
					t.Fatalf("the boot installed a copy that is not the recorded choice's\n%s", firstDifference(tw.kept, got))
				}
				if got := takeOf(t, f.raw, f.id); got != tw.choice {
					t.Fatalf("the take is %+v after the boot, want the recorded %+v still", got, tw.choice)
				}
			}
		})
	}
}

// twoTakes is a database restored from a backup beside the archive that went
// on, its take of the archive recorded (choice, whose fold is first) by an
// operator's process still open (op), and the archive gone on once more from
// the database that wrote it, so the store's newest manifest is another than
// the recorded one; kept is the restored database's own fold.
type twoTakes struct {
	f           *archiveFixture
	store       *delayStore
	op          engine.Operator
	stop        atomic.Bool
	choice      takeRecord
	first, kept []byte
}

func newTwoTakes(t *testing.T) *twoTakes {
	t.Helper()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	putBlob(t, f.ds, []byte("a blob of the repository the take replaces"))
	f.pass(t)
	backup := f.backup(t, store)
	f.reopen(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the first choice's"}})
	first := foldOf(t, f.ds)
	f.pass(t)
	ahead := f.dsn
	f.restoreDatabase(t, backup)
	kept := foldOf(t, f.ds)
	_ = f.svc.Close()
	tw := &twoTakes{f: f, store: store, first: first, kept: kept}
	tw.op = f.operator(t, engine.WithTestImportFault(0, func(stage string) error {
		if stage == engine.TakeAfterStaging && tw.stop.Load() {
			return errStopped
		}
		return nil
	}))
	if _, err := tw.op.ResolveArchive(ctx, f.id, false); err != nil {
		t.Fatalf("take the archive: %v", err)
	}
	tw.choice = takeOf(t, f.raw, f.id)
	holdStagedWhole(t, f.root, f.id, tw.choice)

	// The database the archive is the copy of goes on, from a data root of
	// its own: the store's newest manifest is now another.
	aheadRoot := t.TempDir()
	copyDir(t, filepath.Join(f.root, changelogfile.RepositoriesDir), filepath.Join(aheadRoot, changelogfile.RepositoriesDir))
	other := reopenWith(t, ahead, aheadRoot, engine.WithArchive(store), engine.WithArchiveInterval(time.Hour))
	ds, err := other.Dataset(ctx, f.id)
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "after the first choice"}})
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	_ = other.Close()
	return tw
}

// stageLater has the operator's process stage the store's newest manifest
// as a later choice and stop before recording it, and returns its stage.
func (tw *twoTakes) stageLater(t *testing.T) string {
	t.Helper()
	tw.stop.Store(true)
	defer tw.stop.Store(false)
	if _, err := tw.op.ResolveArchive(context.Background(), tw.f.id, false); !errors.Is(err, errStopped) {
		t.Fatalf("the later choice did not stop before it was recorded: %v", err)
	}
	if got := takeOf(t, tw.f.raw, tw.f.id); got != tw.choice {
		t.Fatalf("the later choice, never recorded, left the take %+v, want %+v", got, tw.choice)
	}
	entries, err := os.ReadDir(filepath.Join(tw.f.root, "archive-take", tw.f.id))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != tw.choice.stage {
			return e.Name()
		}
	}
	t.Fatal("the later choice staged nothing beside the recorded one")
	return ""
}

// operator is a process of the fixture's database, data root and store, for
// an operator's command.
func (f *archiveFixture) operator(t *testing.T, opts ...engine.Option) engine.Operator {
	t.Helper()
	return reopenWith(t, f.dsn, f.root, append([]engine.Option{engine.WithArchive(f.store), engine.WithArchiveInterval(time.Hour)}, opts...)...).(engine.Operator)
}

// takeRecord is a row of archive_takes.
type takeRecord struct {
	stage, archive, manifest string
}

// takeOf is repository id's recorded take, the zero value when none is.
func takeOf(t *testing.T, raw *sql.DB, id string) takeRecord {
	t.Helper()
	var c takeRecord
	err := raw.QueryRow(`SELECT stage, archive, manifest FROM archive_takes WHERE repository_id = $1`, id).Scan(&c.stage, &c.archive, &c.manifest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return c
}

// holdStagedWhole fails the test unless the copy staged for c is whole and
// is c's: staged.json names it, its manifest is the bytes the name hashes,
// and every piece, sealed value and spooled request the manifest selects is
// there.
func holdStagedWhole(t *testing.T, root, id string, c takeRecord) {
	t.Helper()
	dir := filepath.Join(root, "archive-take", id, c.stage)
	raw, err := os.ReadFile(filepath.Join(dir, "staged.json"))
	if err != nil {
		t.Fatalf("the copy staged for the take: %v", err)
	}
	var staged map[string]string
	if err := json.Unmarshal(raw, &staged); err != nil || staged["archive"] != c.archive || staged["manifest"] != c.manifest || staged["next"] != c.stage {
		t.Fatalf("the staged copy names %s (%v), want the take %+v", raw, err, c)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(changelogfile.ArchiveManifestsPrefix(id, c.archive)+c.manifest)))
	if err != nil || !changelogfile.ManifestNamesBytes(c.manifest, data) {
		t.Fatalf("the staged manifest is not the bytes its name hashes (%v)", err)
	}
	m, err := changelogfile.DecodeArchiveManifest(data, id)
	if err != nil {
		t.Fatal(err)
	}
	selected := slices.Clone(m.Spool)
	for _, p := range m.Pieces {
		selected = append(selected, p.Name)
	}
	for _, name := range m.Sealed {
		selected = append(selected, name)
	}
	if len(m.Pieces) == 0 {
		t.Fatal("the staged manifest selects no piece: the check would hold nothing")
	}
	for _, name := range selected {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(changelogfile.ArchivePrefix(id, c.archive)+name))); err != nil {
			t.Fatalf("the staged copy lacks %s: %v", name, err)
		}
	}
}

// holdTakeEnded fails the test unless repository id has no take recorded and
// no staged copy.
func holdTakeEnded(t *testing.T, raw *sql.DB, root, id string) {
	t.Helper()
	if c := takeOf(t, raw, id); c.stage != "" {
		t.Fatalf("the take %+v is still recorded", c)
	}
	if _, err := os.Stat(filepath.Join(root, "archive-take", id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a staged copy outlived the take: %v", err)
	}
}

// repositoryState is what the database and the data root hold of
// repository id, compared whole: its row, every record's kind, id and
// version, the changelog's head, and every file under its directory with its
// bytes' hash. It fails the test on a repository with no records, where a
// comparison would hold nothing.
func repositoryState(t *testing.T, raw *sql.DB, root, id string) string {
	t.Helper()
	var b strings.Builder
	var rows, head int64
	var versions string
	if err := raw.QueryRow(`SELECT count(*), coalesce(string_agg(kind || '/' || id || '@' || version, ',' ORDER BY kind, id), '')
		FROM records WHERE repository = $1`, id).Scan(&rows, &versions); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow(`SELECT coalesce(max(seq), 0) FROM changelog WHERE repository = $1`, id).Scan(&head); err != nil {
		t.Fatal(err)
	}
	var named int
	if err := raw.QueryRow(`SELECT count(*) FROM repositories WHERE id = $1`, id).Scan(&named); err != nil {
		t.Fatal(err)
	}
	if rows == 0 || named != 1 {
		t.Fatalf("repository %s holds %d records and %d rows of its own: the state would hold nothing", id, rows, named)
	}
	fmt.Fprintf(&b, "row %d, %d records, head %d, %x", named, rows, head, sha256.Sum256([]byte(versions)))
	files := 0
	dir := filepath.Join(root, changelogfile.RepositoriesDir, id)
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files++
		fmt.Fprintf(&b, "; %s %x", rel, sha256.Sum256(data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatalf("repository %s holds no file under its directory: the state would hold none", id)
	}
	return b.String()
}

// FINDINGS 3 AND 8 OF THE FIFTH REVIEW. A database restored from a backup,
// beside an archive that went on (more lines, or only a sealed value that
// changed with no line), is never written over and never written from: the
// pass stops, and the archive's newer sealed version is still there. The
// operator's report says what each side holds.
func TestADatabaseRestoredBehindItsArchiveNeverWritesOverIt(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		ahead func(t *testing.T, f *archiveFixture)
	}{
		{"lines that differ at the same head", func(t *testing.T, f *archiveFixture) {
			mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the archive's"}})
		}},
		{"a sealed value alone", func(t *testing.T, f *archiveFixture) {
			if _, err := f.raw.Exec(`UPDATE sealed SET expires_at = now() + interval '1 day' WHERE repository = $1`, f.id); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := newDelayStore(t)
			f := newArchiveFixture(t, store)
			sgPutProvider(t, f.ds, sgPlainKey)
			f.pass(t)
			backup := f.backup(t, store)
			f.reopen(t)
			c.ahead(t, f)
			f.pass(t)
			m, aid, _, _ := newestManifest(t, store, f.id)
			f.restoreDatabase(t, backup)
			if c.name != "a sealed value alone" {
				mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the database's"}})
			}
			if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, engine.ErrArchiveDiverged) {
				t.Fatalf("a pass of the restored database: %v, want ErrArchiveDiverged", err)
			}
			for ref, name := range m.Sealed {
				if _, err := archive.ReadAll(ctx, store, changelogfile.ArchivePrefix(f.id, aid)+name); err != nil {
					t.Fatalf("the archive's version of %s is gone: %v", ref, err)
				}
			}
			report, err := f.svc.(engine.Operator).ArchiveReport(ctx, f.id)
			if err != nil {
				t.Fatal(err)
			}
			if c.name == "a sealed value alone" && len(report.SealedDiffering) == 0 {
				t.Fatalf("the report does not name the sealed value that differs: %+v", report)
			}
			if c.name != "a sealed value alone" && report.Differing == 0 {
				t.Fatalf("the report does not count the lines that differ: %+v", report)
			}
		})
	}
}

// RULE 4, THE OPERATOR KEEPING THE DATABASE. After a restore from a backup
// the passes stop; keeping the database starts a new archive the next pass
// publishes whole, the old archive is left, and removing it leaves the new
// one verifying.
func TestKeepingTheDatabaseStartsANewArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	f := newArchiveFixture(t, store)
	f.pass(t)
	backup := f.backup(t, store)
	f.reopen(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "forgotten"}})
	f.pass(t)
	f.restoreDatabase(t, backup)
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, engine.ErrArchiveDiverged) {
		t.Fatalf("a pass of the restored database: %v, want ErrArchiveDiverged", err)
	}
	_ = f.svc.Close()
	op := reopenWith(t, f.dsn, f.root, engine.WithArchive(store))
	resolved, err := op.(engine.Operator).ResolveArchive(ctx, f.id, true)
	if err != nil {
		t.Fatalf("keep the database: %v", err)
	}
	if resolved.Archive <= resolved.Left || resolved.Left == "" {
		t.Fatalf("the resolution: %+v, want a new archive past the old", resolved)
	}
	_ = op.Close()
	f.reopen(t)
	f.pass(t)
	if _, aid, _, _ := newestManifest(t, store, f.id); aid != resolved.Archive {
		t.Fatalf("the pass published in archive %s, want the new %s", aid, resolved.Archive)
	}
	_ = f.svc.Close()
	op = reopenWith(t, f.dsn, f.root, engine.WithArchive(store))
	if _, err := op.(engine.Operator).RemoveArchive(ctx, f.id, resolved.Archive); err == nil {
		t.Fatal("the archive the database writes was removed")
	}
	if n, err := op.(engine.Operator).RemoveArchive(ctx, f.id, resolved.Left); err != nil || n == 0 {
		t.Fatalf("remove the old archive: %d (%v)", n, err)
	}
	_ = op.Close()
	f.reopen(t)
	f.verifyArchive(t, "after the old archive was removed")
}

// RULE 7 (the review of the contract's first hole). A repository deleted
// while its deletes were lost on the way, its archive still listed under the
// deletion's mark; the database restored from a backup taken before the
// deletion finds the mark over the archive it records and stops, so a
// publication of it never selects what a forgotten delete removes when it
// lands. Keeping the database starts an archive past the mark, which a
// boot with no rows restores.
func TestADeletionMarkStopsADatabaseRestoredFromBeforeTheDeletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	with := []engine.Option{engine.WithArchive(store), engine.WithArchiveInterval(time.Hour)}
	svc, dsn := newService(t, with...)
	repository := testdb.Repository(t)
	user, _, _ := registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, ds, []byte("bytes a forgotten delete removes"))
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	backup := testdb.Copy(t, dsn)
	svc = reopenWith(t, dsn, root, with...)
	// The deletion's deletes, and the manifest its dataset's last pass puts,
	// are lost on the way: the restored database meets the archive as it
	// recorded it, under the mark alone.
	store.setHold(func(op, key string) bool { return op == "delete" || (op == "put" && isManifestKey(key)) })
	waitStep(t)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{Identity: repository, Repository: repository, Password: testPassword, TOTPCode: user.code(t)}); !errors.Is(err, errLost) {
		t.Fatalf("the deletion whose deletes were lost: %v", err)
	}
	store.setHold(holdNothing)
	store.down.Store(true)
	_ = svc.Close()
	store.down.Store(false)

	restored := reopenWith(t, backup, root, with...)
	rds, err := restored.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, rds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/llm/memory", Properties: map[string]any{"text": "after the restore"}})
	if err := engine.ArchivePass(ctx, rds); !errors.Is(err, engine.ErrArchiveDiverged) {
		t.Fatalf("a pass under the deletion's mark: %v, want ErrArchiveDiverged", err)
	}
	store.release(t)
	if _, err := restored.(engine.Operator).ResolveArchive(ctx, repository, true); err != nil {
		t.Fatal(err)
	}
	_ = restored.Close()
	restored = reopenWith(t, backup, root, with...)
	if rds, err = restored.Dataset(ctx, repository); err != nil {
		t.Fatal(err)
	}
	if err := engine.ArchivePass(ctx, rds); err != nil {
		t.Fatalf("the pass in the archive past the mark: %v", err)
	}
	if report := mustVerify(t, restored, repository); !report.OK || report.ArchiveUnverified != "" {
		t.Fatalf("the archive past the mark: %+v", report)
	}
	_ = restored.Close()
	fresh := reopenWith(t, engine.MigratedDSN(t), t.TempDir(), with...)
	if _, err := fresh.Dataset(ctx, repository); err != nil {
		t.Fatalf("the archive past the mark is not restored by a boot with no rows: %v", err)
	}
}

// RULE 7 (the review of the contract's second hole). A deletion's mark
// covers every archive made before it, not only the one the database
// records: a database restored from a backup records an older archive than
// the one a later life started, whose first manifest is still on its way
// when the restored database deletes the repository. When it lands, the
// mark covers it, and a boot with no rows restores nothing.
func TestADeletionMarkCoversEveryArchiveBeforeIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newDelayStore(t)
	with := []engine.Option{engine.WithArchive(store), engine.WithArchiveInterval(time.Hour)}
	svc, dsn := newService(t, with...)
	repository := testdb.Repository(t)
	user, _, _ := registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	backup := testdb.Copy(t, dsn)
	// A later life of the database starts a newer archive, whose first
	// manifest is lost on the way and lands late.
	op := reopenWith(t, dsn, root, with...)
	if _, err := op.(engine.Operator).ResolveArchive(ctx, repository, true); err != nil {
		t.Fatal(err)
	}
	_ = op.Close()
	svc = reopenWith(t, dsn, root, with...)
	if ds, err = svc.Dataset(ctx, repository); err != nil {
		t.Fatal(err)
	}
	store.setHold(func(op, key string) bool { return op == "put" && isManifestKey(key) })
	if err := engine.ArchivePass(ctx, ds); !errors.Is(err, errLost) {
		t.Fatalf("the newer archive's first pass: %v", err)
	}
	store.setHold(holdNothing)
	store.down.Store(true)
	_ = svc.Close()
	store.down.Store(false)
	// The backup, which records the older archive, deletes the repository.
	restored := reopenWith(t, backup, root, with...)
	waitStep(t)
	if err := restored.DeleteRepository(ctx, substrate.LoginInput{Identity: repository, Repository: repository, Password: testPassword, TOTPCode: user.code(t)}); err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	_ = restored.Close()
	store.release(t)
	// What every reader decides by: the newest archive, its manifest landed,
	// is covered by the mark.
	if listed, err := engine.ListArchiveIDs(ctx, reopenWith(t, engine.MigratedDSN(t), t.TempDir(), with...), id); err != nil || len(listed) == 0 {
		t.Fatalf("the newer archive's late manifest did not land: %v (%v)", listed, err)
	}
	if covered, err := engine.ArchiveCovered(ctx, store, id); err != nil || !covered {
		t.Fatalf("the newest archive is not covered by the deletion's mark (%v)", err)
	}
	fresh := reopenWith(t, engine.MigratedDSN(t), t.TempDir(), with...)
	if repos, err := fresh.Repositories(ctx); err != nil || len(repos) != 0 {
		t.Fatalf("a boot with no rows restored %+v (%v): the mark did not cover the newer archive", repos, err)
	}
}

// RULE 7, THE WAY BACK. A deleted repository comes back only by an
// explicit import, which starts an archive past the mark.
func TestADeletedRepositoryComesBackOnlyByAnExplicitImport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	repository := testdb.Repository(t)
	user, _, _ := registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/llm/memory", Properties: map[string]any{"text": "brought back"}})
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	root := engine.DataRootOf(svc)
	before := foldOf(t, ds)
	// A copy of the archive taken before the deletion, put back after it.
	saved := t.TempDir()
	copyDir(t, root+"/archive", saved)
	waitStep(t)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{Identity: repository, Repository: repository, Password: testPassword, TOTPCode: user.code(t)}); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()
	copyDir(t, saved, root+"/archive")
	fresh := mustReopen(t, engine.MigratedDSN(t), root)
	if repos, err := fresh.Repositories(ctx); err != nil || len(repos) != 0 {
		t.Fatalf("a boot restored the deleted repository on its own: %+v (%v)", repos, err)
	}
	m, _, err := changelogfile.ReadArchiveManifestDir(root + "/archive/" + changelogfile.RepositoriesDir + "/" + id)
	if !errors.Is(err, changelogfile.ErrArchiveDeleted) {
		t.Fatalf("the archive put back reads as %+v (%v), want it covered by the mark", m, err)
	}
	listed, err := engine.ListArchiveIDs(ctx, fresh, id)
	if err != nil || len(listed) == 0 {
		t.Fatalf("the archives of the deleted repository: %v (%v)", listed, err)
	}
	if _, err := fresh.(engine.Operator).ImportArchive(ctx, id, listed[0], ""); err != nil {
		t.Fatalf("import the archive explicitly: %v", err)
	}
	ds, err = fresh.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if after := foldOf(t, ds); string(after) != string(before) {
		t.Fatalf("the imported fold is not the deleted repository's\n%s", firstDifference(before, after))
	}
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatalf("the first pass after the import: %v", err)
	}
}

// An archive whose history is from before the local namespace spells its
// repository's name in its lines, which the conversion moves to `~` by the
// row's name, so it is not imported under another: the server where its name
// is taken sets it aside, and the import under another name refuses, naming
// the dialect and changing nothing.
func TestAnArchiveFromBeforeTheLocalNamespaceIsNotImportedUnderAnotherName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithoutConversionSteps())
	repository := testdb.Repository(t)
	registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	from := engine.DataRootOf(svc) + "/archive"
	_ = svc.Close()

	holder, dsn := newService(t)
	registerUser(t, holder, repository)
	root := engine.DataRootOf(holder)
	_ = holder.Close()
	copyDir(t, from, root+"/archive")
	fresh := mustReopen(t, dsn, root)
	listed, err := engine.ListArchiveIDs(ctx, fresh, id)
	if err != nil || len(listed) == 0 {
		t.Fatalf("the archives of the stranger: %v (%v)", listed, err)
	}
	_, err = fresh.(engine.Operator).ImportArchive(ctx, id, listed[0], "other."+repository)
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), "changelog dialect 1") {
		t.Fatalf("an import under another name of a dialect-1 archive = %v, want refused naming the dialect", err)
	}
	if repos, err := fresh.Repositories(ctx); err != nil || len(repos) != 1 || repos[0].ID == id {
		t.Fatalf("the refused import left %+v (%v), want the holder alone", repos, err)
	}
}
