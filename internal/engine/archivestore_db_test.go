package engine_test

// The archive against a store that misbehaves without failing, a change that
// moves no marker, and a second writer: the classes the archive's reviews
// found one after another, each held here by the case that showed it.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// lateStore holds back the puts of manifests while hold is set, answers each
// with an error, as a connection lost before the answer does, and applies
// them when release is called: a request the store takes and applies after
// the client gave up on it.
type lateStore struct {
	archive.Store
	mu   sync.Mutex
	hold bool
	late []func() error
}

func (s *lateStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	s.mu.Lock()
	holding := s.hold && (strings.Contains(key, "/"+changelogfile.ManifestsSubdir+"/") || strings.HasSuffix(key, "/"+changelogfile.ManifestName))
	s.mu.Unlock()
	if !holding {
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
	return errors.New("the connection was lost before the store answered")
}

func (s *lateStore) setHold(hold bool) {
	s.mu.Lock()
	s.hold = hold
	s.mu.Unlock()
}

func (s *lateStore) release(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	late := s.late
	s.late = nil
	s.mu.Unlock()
	for _, put := range late {
		if err := put(); err != nil {
			t.Fatal(err)
		}
	}
}

// A manifest the store applies after a later pass published its own lands
// under its own older epoch and is not read: the archive stays the later
// pass's, every object it selects in place, though that pass deleted what
// only the late manifest selected.
func TestAManifestTheStoreAppliesLateIsNotRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &lateStore{Store: dirStore(t)}
	f := newArchiveFixture(t, store)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "first"}})
	f.pass(t)

	store.setHold(true)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "second"}})
	if err := engine.ArchivePass(ctx, f.ds); err == nil {
		t.Fatal("the pass whose manifest put was lost reported success")
	}
	store.setHold(false)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "third"}})
	f.pass(t)
	store.release(t)

	sealed, blobs := f.held(t)
	f.archived(t).equals(t, "after the late manifest landed", f.tableHead(t), sealed, blobs)
}

// A sealed value that changes and moves nothing else (a credential parked,
// which sets only its expiry; a refresh that commits after a later one,
// with an earlier updated_at and a payload of the same length) is copied by
// the next pass: the pass compares the sealed table whole, not a count and
// the latest write.
func TestASealedChangeThatMovesNoMarkerIsArchived(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newArchiveFixture(t, dirStore(t))
	sgPutProvider(t, f.ds, sgPlainKey)
	f.pass(t)
	var ref, kind, id string
	var payload []byte
	if err := f.raw.QueryRow(`SELECT ref, record_kind, record_id, payload FROM sealed WHERE repository = $1 AND record_kind = $2`, f.id, sgProviderKind).Scan(&ref, &kind, &id, &payload); err != nil {
		t.Fatal(err)
	}

	// Parked: the expiry alone.
	if _, err := f.raw.Exec(`UPDATE sealed SET expires_at = now() + interval '1 day' WHERE repository = $1 AND ref = $2`, f.id, ref); err != nil {
		t.Fatal(err)
	}
	f.pass(t)
	sealed, blobs := f.held(t)
	f.archived(t).equals(t, "after the expiry alone changed", f.tableHead(t), sealed, blobs)

	// Out of order: a payload of the same length under an older updated_at.
	again, err := engine.ResealForTest(ctx, f.svc, f.id, ref, kind, id, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(payload) || bytes.Equal(again, payload) {
		t.Fatalf("the resealed payload is %d bytes, the old %d: the test needs another ciphertext of the same length", len(again), len(payload))
	}
	if _, err := f.raw.Exec(`UPDATE sealed SET payload = $3, updated_at = updated_at - interval '1 hour' WHERE repository = $1 AND ref = $2`, f.id, ref, again); err != nil {
		t.Fatal(err)
	}
	f.pass(t)
	sealed, blobs = f.held(t)
	f.archived(t).equals(t, "after an older write of the same length", f.tableHead(t), sealed, blobs)
}

// A field of the manifest that changes with nothing else (the key a
// repository's data key is wrapped under, rewrapped) is in the next pass's
// manifest.
func TestAManifestFieldThatMovesNoMarkerIsArchived(t *testing.T) {
	t.Parallel()
	f := newArchiveFixture(t, dirStore(t))
	f.pass(t)
	if _, err := f.raw.Exec(`UPDATE repositories SET dek_key_id = 'fedcba9876543210' WHERE id = $1`, f.id); err != nil {
		t.Fatal(err)
	}
	f.pass(t)
	if m, _, _, _ := newestManifest(t, f.store, f.id); m.DEKKeyID != "fedcba9876543210" {
		t.Fatalf("the archive's manifest names the key %q, want the rewrapped one", m.DEKKeyID)
	}
}

// A store that holds a newer manifest than the one this process published
// has another writer: the pass refuses and writes nothing.
func TestAPassRefusesAnArchiveAnotherServerPublishes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newArchiveFixture(t, dirStore(t))
	f.pass(t)
	before := f.archived(t)
	putForeignManifest(t, f.store, f.id, func(m *changelogfile.Manifest) { m.Epoch += 5 })
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "beside another writer"}})
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, engine.ErrArchiveDiverged) {
		t.Fatalf("a pass beside another writer's manifest: %v, want ErrArchiveDiverged", err)
	}
	if after := f.archived(t); after.head != before.head || len(after.pieces) != len(before.pieces) {
		t.Fatalf("the refused pass wrote the archive: head %d, %d pieces, was %d, %d", after.head, len(after.pieces), before.head, len(before.pieces))
	}
}

// hangingStore takes every call and answers none until its caller gives
// up, as a store that accepts the connection and stops answering does.
type hangingStore struct{}

func (hangingStore) Put(ctx context.Context, _ string, _ io.ReadSeeker, _ int64) error {
	<-ctx.Done()
	return ctx.Err()
}

func (hangingStore) Get(ctx context.Context, _ string) (io.ReadCloser, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (hangingStore) List(ctx context.Context, _ string) ([]archive.Object, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (hangingStore) Delete(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}
func (hangingStore) String() string { return "hanging" }

// bootWithin boots, failing the test if the boot does not return within d.
func bootWithin(t *testing.T, d time.Duration, dsn, root string, opts ...engine.Option) (substrate.Service, error) {
	t.Helper()
	type booted struct {
		svc substrate.Service
		err error
	}
	done := make(chan booted, 1)
	go func() {
		svc, err := bootWith(t, dsn, root, opts...)
		done <- booted{svc, err}
	}()
	select {
	case b := <-done:
		return b.svc, b.err
	case <-time.After(d):
		t.Fatalf("the boot did not return within %s", d)
		return nil, nil
	}
}

// A store that takes the connection and never answers does not hold the
// boot: every call to it is bounded, the boot goes on without the archive
// and serves the repositories the database holds. A restore that has begun
// still refuses rather than serve a repository whose history is not all
// in.
func TestABootWhoseArchiveHangsGoesOn(t *testing.T) {
	t.Parallel()
	src := newCrashSource(t)
	svc, err := bootWithin(t, 30*time.Second, src.dsn, src.root,
		engine.WithArchive(hangingStore{}), engine.WithArchiveTimeouts(300*time.Millisecond, 300*time.Millisecond))
	if err != nil {
		t.Fatalf("the boot beside a store that never answers: %v", err)
	}
	src.holdsTheSource(t, svc, src.dsn)
	_ = svc.Close()

	root := copyRepositoryDir(t, src.root, src.id)
	dsn := engine.MigratedDSN(t)
	bootStoppedAt(t, dsn, root, engine.ImportAfterRow)
	if _, err := bootWithin(t, 30*time.Second, dsn, root,
		engine.WithArchive(hangingStore{}), engine.WithArchiveTimeouts(300*time.Millisecond, 300*time.Millisecond)); !errors.Is(err, engine.ErrImportIncomplete) {
		t.Fatalf("a begun restore beside a store that never answers: %v, want ErrImportIncomplete", err)
	}
}

// verify reads every blob the archive holds back out of it: a blob the
// archive lost, or holds other bytes for, is a finding though the server's
// own copy is whole.
func TestVerifyFindsABlobTheArchiveLostOrDamaged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		damage func(path string) error
		want   string
	}{
		{"lost", os.Remove, "is not in it"},
		{"damaged", func(path string) error { return os.WriteFile(path, []byte("other bytes"), 0o600) }, "holds other bytes"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc, ds, _ := newDatasetWithDSN(t)
			digest := putBlob(t, ds, []byte("bytes the archive keeps"))
			if err := engine.ArchivePass(ctx, ds); err != nil {
				t.Fatal(err)
			}
			repository := ds.Repository()
			if report := mustVerify(t, svc, repository.Authority); !report.OK || report.ArchiveBlobs != 1 {
				t.Fatalf("the whole archive does not verify: %+v", report)
			}
			repoDir := filepath.Join(engine.DataRootOf(svc), "archive", changelogfile.RepositoriesDir, repository.ID)
			paths, err := filepath.Glob(filepath.Join(archiveObjectsDir(t, repoDir), changelogfile.BlobsSubdir, digest+".*"))
			if err != nil || len(paths) != 1 {
				t.Fatalf("the archive's objects of %s: %v (%v)", digest, paths, err)
			}
			path := paths[0]
			// A link shares the server's bytes; the archive's copy is made
			// its own first, so damaging it leaves the server's whole.
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := c.damage(path); err != nil {
				t.Fatal(err)
			}
			report := mustVerify(t, svc, repository.Authority)
			if report.OK || !findingContaining(report, c.want) {
				t.Fatalf("verify over a %s archived blob: %+v", c.name, report)
			}
			if got := getBlob(t, ds, digest); string(got) != "bytes the archive keeps" {
				t.Fatalf("the server's own copy changed: %q", got)
			}
		})
	}
}

// A manifest a store applies after the repository's deletion finished
// restores nothing (rule 7): the deletion's mark covers the archive it
// lands in, and the boot restores no archive a mark covers.
func TestALateManifestAfterADeletionRestoresNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
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
	repoDir := filepath.Join(root, "archive", changelogfile.RepositoriesDir, ds.Repository().ID)
	lateFile := archivedManifestFile(t, repoDir)
	late, err := os.ReadFile(lateFile)
	if err != nil {
		t.Fatal(err)
	}
	waitStep(t)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{
		Identity: repository, Repository: repository, Password: testPassword, TOTPCode: user.code(t),
	}); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()
	// The put the store applies late, after the deletion.
	if err := os.MkdirAll(filepath.Dir(lateFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lateFile, late, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := bootWith(t, dsn, root)
	if err != nil {
		t.Fatalf("the boot beside a late manifest of a deleted repository: %v", err)
	}
	if _, err := after.Dataset(ctx, repository); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the deleted repository after a late manifest = %v, want ErrNotFound", err)
	}
	if repos, err := after.Repositories(ctx); err != nil || len(repos) != 0 {
		t.Fatalf("the boot restored %+v (%v)", repos, err)
	}
}
