package engine_test

// The archive (decision 0262): a pass copies a repository to a store after
// its writes commit, and what the store's manifest names is there. These
// tests hold the archive to the table it is copied from, with the pass
// stopped at each of its durable steps, and to a store that cannot be
// reached.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/archive/archivetest"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// archiveFixture is a repository whose archive goes to a store the test
// reads, with passes only when the test runs them.
type archiveFixture struct {
	svc   substrate.Service
	ds    substrate.Dataset
	dsn   string
	root  string
	store archive.Store
	id    string
	raw   *sql.DB
	// failAt names the stage the next pass stops at, "" for none.
	failAt atomic.Value
}

var errStopped = errors.New("the pass stopped here, as a process killed here would")

func newArchiveFixture(t *testing.T, store archive.Store, opts ...engine.Option) *archiveFixture {
	t.Helper()
	f := &archiveFixture{store: store}
	f.failAt.Store("")
	all := append([]engine.Option{
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithArchive(store),
		engine.WithArchiveInterval(time.Hour),
		engine.WithTestArchiveFault(f.fault),
	}, opts...)
	svc, dsn := newService(t, all...)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	f.svc, f.ds, f.dsn, f.root, f.id, f.raw = svc, ds, dsn, engine.DataRootOf(svc), ds.Repository().ID, raw
	return f
}

func (f *archiveFixture) fault(stage string) error {
	if f.failAt.Load().(string) == stage {
		return errStopped
	}
	return nil
}

// reopen closes the service, as a killed process ends, and opens the same
// database, data root and store again.
func (f *archiveFixture) reopen(t *testing.T, opts ...engine.Option) {
	t.Helper()
	_ = f.svc.Close()
	all := append([]engine.Option{
		engine.WithArchive(f.store),
		engine.WithArchiveInterval(time.Hour),
		engine.WithTestArchiveFault(f.fault),
	}, opts...)
	f.svc = reopenWith(t, f.dsn, f.root, all...)
	ds, err := f.svc.Dataset(context.Background(), f.id)
	if err != nil {
		t.Fatalf("reopen the dataset: %v", err)
	}
	f.ds = ds
}

func (f *archiveFixture) pass(t *testing.T) {
	t.Helper()
	if err := engine.ArchivePass(context.Background(), f.ds); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
}

// tableHead is the changelog's head in the database.
func (f *archiveFixture) tableHead(t *testing.T) int64 {
	t.Helper()
	var head int64
	if err := f.raw.QueryRow(`SELECT coalesce(max(seq), 0) FROM changelog WHERE repository = $1`, f.id).Scan(&head); err != nil {
		t.Fatal(err)
	}
	return head
}

// sealedValue is one sealed value as a restore opens it: the record it is
// bound to, its ciphertext, when it expires and when it was written.
type sealedValue struct {
	kind, id string
	payload  []byte
	expires  string
	updated  time.Time
}

// expiresText spells an expiry for comparison: empty for none.
func expiresText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// held is what the database holds now that the archive must hold at its
// head: each sealed value whole, by ref, and the stored blobs by digest.
func (f *archiveFixture) held(t *testing.T) (sealed map[string]sealedValue, blobs map[string]bool) {
	t.Helper()
	sealed, blobs = map[string]sealedValue{}, map[string]bool{}
	rows, err := f.raw.Query(`SELECT ref, record_kind, record_id, payload, expires_at, updated_at FROM sealed WHERE repository = $1`, f.id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ref string
		var v sealedValue
		var expires sql.NullTime
		if err := rows.Scan(&ref, &v.kind, &v.id, &v.payload, &expires, &v.updated); err != nil {
			t.Fatal(err)
		}
		if expires.Valid {
			v.expires = expiresText(&expires.Time)
		}
		v.updated = v.updated.UTC()
		sealed[ref] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	rows, err = f.raw.Query(`SELECT id FROM records WHERE repository = $1 AND kind = 'substrate.reamde.dev/core/blob' AND deleted_at IS NULL AND states->>'status' = 'stored'`, f.id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		blobs[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	return sealed, blobs
}

// archived is what a restore of the store opens: the manifest's head, the
// lines of the chain of pieces to it, checked against the table's stamped
// checksums, each sealed value the manifest selects, read and held to the
// version its name says, and the blobs, each held to the digest that names
// it. strays are sealed objects the manifest does not select, which only a
// pass that stopped before its deletes leaves. It fails the test when the
// manifest names what the store does not hold.
type archived struct {
	f       *archiveFixture
	head    int64
	pieces  []string
	sealed  map[string]sealedValue
	blobs   map[string]bool
	missing []string
	strays  []string
}

func (f *archiveFixture) archived(t *testing.T) archived {
	t.Helper()
	ctx := context.Background()
	out := archived{f: f, sealed: map[string]sealedValue{}, blobs: map[string]bool{}}
	m, aid, _, ok := newestManifest(t, f.store, f.id)
	if !ok {
		return out
	}
	prefix := changelogfile.ArchivePrefix(f.id, aid)
	out.head, out.missing = m.Head, append(slices.Clone(m.MissingBlobs), m.PendingBlobs...)
	selected := map[string]bool{}
	for ref, name := range m.Sealed {
		sum, _, ok := changelogfile.ParseSealedObjectName(ref, name)
		if !ok {
			t.Fatalf("the manifest selects %q for %s", name, ref)
		}
		selected[name] = true
		body, err := archive.ReadAll(ctx, f.store, prefix+name)
		if err != nil {
			t.Fatalf("the archive at head %d lacks the sealed value %s its manifest selects: %v", m.Head, ref, err)
		}
		rec, err := changelogfile.DecodeSealedVersion(ref, sum, body)
		if err != nil {
			t.Fatalf("the archive at head %d: %v", m.Head, err)
		}
		out.sealed[ref] = sealedValue{kind: rec.RecordKind, id: rec.RecordID, payload: rec.Payload, expires: expiresText(rec.ExpiresAt), updated: rec.UpdatedAt.UTC()}
	}
	listed, err := f.store.List(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range listed {
		name := strings.TrimPrefix(o.Key, prefix)
		switch dir, _, _ := strings.Cut(name, "/"); dir {
		case changelogfile.SealedSubdir:
			if !selected[name] {
				out.strays = append(out.strays, name)
			}
		case changelogfile.BlobsSubdir:
			digest, _, ok := changelogfile.ParseBlobObjectName(name)
			if !ok {
				t.Fatalf("the archive holds %s, which is not a blob's", o.Key)
			}
			body, err := archive.ReadAll(ctx, f.store, o.Key)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(body)
			if digest != substrate.BlobDigestPrefix+hex.EncodeToString(sum[:]) {
				t.Errorf("the archive's blob %s holds other bytes", o.Key)
			}
			out.blobs[digest] = true
		}
	}
	chain, err := m.Chain()
	if err != nil {
		t.Fatalf("the archive at head %d: %v", m.Head, err)
	}
	stamped := map[int64][]byte{}
	rows, err := f.raw.Query(`SELECT seq, hash FROM changelog WHERE repository = $1 AND seq <= $2`, f.id, m.Head)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var seq int64
		var hash []byte
		if err := rows.Scan(&seq, &hash); err != nil {
			t.Fatal(err)
		}
		stamped[seq] = hash
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	for _, p := range chain {
		out.pieces = append(out.pieces, p.Key[strings.LastIndex(p.Key, "/")+1:])
		body, err := archive.ReadAll(ctx, f.store, prefix+p.Key)
		if err != nil {
			t.Fatal(err)
		}
		if err := changelogfile.ReadPiece(bytes.NewReader(body), p.First, p.Last, func(e changelogfile.Entry, _ []byte, sum [32]byte) error {
			if !bytes.Equal(stamped[e.Seq], sum[:]) {
				t.Errorf("the archive's seq %d is not the table's", e.Seq)
			}
			return nil
		}); err != nil {
			t.Fatalf("read %s: %v", p.Key, err)
		}
	}
	if m.Head > 0 && changelogfile.SumText([32]byte(stamped[m.Head])) != m.HeadSum {
		t.Errorf("the manifest's headSum is not the table's line at seq %d", m.Head)
	}
	return out
}

// covers fails the test unless a restore of the archive opens every sealed
// value in want as want holds it, bound to the same record and opening under
// the repository's key with that binding, and finds every blob in want
// (or the manifest names it missing).
func (a archived) covers(t *testing.T, when string, sealed map[string]sealedValue, blobs map[string]bool) {
	t.Helper()
	for ref, want := range sealed {
		got, ok := a.sealed[ref]
		switch {
		case !ok:
			t.Errorf("%s: the archive at head %d does not hold the sealed value %s", when, a.head, ref)
		case got.kind != want.kind || got.id != want.id:
			t.Errorf("%s: the archive at head %d holds %s bound to %s %s, the records at that head bind it to %s %s", when, a.head, ref, got.kind, got.id, want.kind, want.id)
		case !bytes.Equal(got.payload, want.payload):
			t.Errorf("%s: the archive at head %d holds another version of %s than the records at that head", when, a.head, ref)
		case got.expires != want.expires || !got.updated.Equal(want.updated):
			t.Errorf("%s: the archive at head %d holds %s expiring %q written %s, the table %q written %s", when, a.head, ref, got.expires, got.updated, want.expires, want.updated)
		default:
			if err := engine.OpenSealedForTest(context.Background(), a.f.svc, a.f.id, ref, want.kind, want.id, got.payload); err != nil {
				t.Errorf("%s: the archive at head %d holds %s, which does not open bound to %s %s: %v", when, a.head, ref, want.kind, want.id, err)
			}
		}
	}
	for digest := range blobs {
		if !a.blobs[digest] && !slices.Contains(a.missing, digest) {
			t.Errorf("%s: the archive at head %d lacks the blob %s", when, a.head, digest)
		}
	}
}

// equals fails the test unless the archive is the database: its head,
// exactly its sealed values and blobs, and no version of a sealed value its
// manifest does not select.
func (a archived) equals(t *testing.T, when string, head int64, sealed map[string]sealedValue, blobs map[string]bool) {
	t.Helper()
	if a.head != head {
		t.Errorf("%s: the archive's head is %d, the table's %d", when, a.head, head)
	}
	a.covers(t, when, sealed, blobs)
	for ref := range a.sealed {
		if _, ok := sealed[ref]; !ok {
			t.Errorf("%s: the archive holds the sealed value %s the table no longer has", when, ref)
		}
	}
	for digest := range a.blobs {
		if !blobs[digest] {
			t.Errorf("%s: the archive holds the blob %s no record holds", when, digest)
		}
	}
	if len(a.strays) > 0 {
		t.Errorf("%s: the archive keeps sealed objects no manifest selects: %v", when, a.strays)
	}
}

// newestManifest is the manifest a restore of repository id from store
// reads: the newest archive's newest manifest, with the archive's id and the
// manifest's name, or false when the store holds none. Two of the highest
// epoch fail the test.
func newestManifest(t *testing.T, store archive.Store, id string) (changelogfile.Manifest, string, string, bool) {
	t.Helper()
	ctx := context.Background()
	listed, err := store.List(ctx, changelogfile.ManifestsPrefix(id))
	if err != nil {
		t.Fatal(err)
	}
	newest, names := "", map[string][]string{}
	for _, o := range listed {
		k, ok := changelogfile.ParseArchiveKey(o.Key)
		if !ok || !k.Manifests || k.Name == changelogfile.DeletedMark {
			continue
		}
		names[k.Archive] = append(names[k.Archive], k.Name)
		newest = max(newest, k.Archive)
	}
	if newest == "" {
		return changelogfile.Manifest{}, "", "", false
	}
	var top int64
	var tops []string
	for _, name := range names[newest] {
		epoch, _, _ := changelogfile.ParseArchiveManifestName(name)
		switch {
		case epoch > top:
			top, tops = epoch, []string{name}
		case epoch == top:
			tops = append(tops, name)
		}
	}
	if len(tops) != 1 {
		t.Fatalf("archive %s holds %d manifests of its highest epoch", newest, len(tops))
	}
	data, err := archive.ReadAll(ctx, store, changelogfile.ArchiveManifestsPrefix(id, newest)+tops[0])
	if err != nil {
		t.Fatalf("read the archive's manifest: %v", err)
	}
	m, err := changelogfile.DecodeArchiveManifest(data, id)
	if err != nil {
		t.Fatalf("decode the archive's manifest: %v", err)
	}
	return m, newest, tops[0], true
}

// putForeignManifest puts, beside the newest manifest of repository id, a
// manifest the database did not publish: the newest one changed by change,
// as another writer, or a database's forgotten future, would leave it. It
// returns the new manifest's key and bytes.
func putForeignManifest(t *testing.T, store archive.Store, id string, change func(*changelogfile.Manifest)) (string, []byte) {
	t.Helper()
	m, aid, _, ok := newestManifest(t, store, id)
	if !ok {
		t.Fatal("the store holds no manifest to change")
	}
	change(&m)
	data, err := changelogfile.EncodeArchiveManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	key := changelogfile.ArchiveManifestsPrefix(id, aid) + changelogfile.ArchiveManifestName(m.Epoch, data)
	if err := store.Put(context.Background(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	return key, data
}

// archivedManifestFile is the file of the newest manifest of an archive's
// repository directory on disk.
func archivedManifestFile(t *testing.T, repoDir string) string {
	t.Helper()
	m, name, err := changelogfile.ReadArchiveManifestDir(repoDir)
	if err != nil {
		t.Fatalf("the archive's manifest: %v", err)
	}
	return filepath.Join(repoDir, changelogfile.ManifestsSubdir, m.Archive, name)
}

// archiveObjectsDir is the directory, on disk, of the objects of the newest
// archive of a repository directory in a directory store.
func archiveObjectsDir(t *testing.T, repoDir string) string {
	t.Helper()
	m, _, err := changelogfile.ReadArchiveManifestDir(repoDir)
	if err != nil {
		t.Fatalf("the archive's manifest: %v", err)
	}
	return filepath.Join(repoDir, m.Archive)
}

func dirStore(t *testing.T) archive.Store {
	t.Helper()
	d, err := archive.NewDir(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A pass copies the repository: every line in pieces that are the table's,
// every sealed value and every blob, and a manifest naming the head.
func TestAnArchivePassCopiesTheRepository(t *testing.T) {
	t.Parallel()
	f := newArchiveFixture(t, dirStore(t))
	ctx := context.Background()
	sgPutProvider(t, f.ds, sgPlainKey)
	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": name}})
	}
	if _, err := f.ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("archived bytes"), ""); err != nil {
		t.Fatalf("put a blob: %v", err)
	}
	f.pass(t)
	sealed, blobs := f.held(t)
	if len(sealed) == 0 || len(blobs) == 0 {
		t.Fatalf("the repository holds %d sealed values and %d blobs; the test wants some of each", len(sealed), len(blobs))
	}
	got := f.archived(t)
	got.equals(t, "after a pass", f.tableHead(t), sealed, blobs)

	// The mark is on the row too, naming the store.
	var seq int64
	var where string
	if err := f.raw.QueryRow(`SELECT archived_seq, archive_store FROM repositories WHERE id = $1`, f.id).Scan(&seq, &where); err != nil {
		t.Fatal(err)
	}
	if seq != got.head || where != f.store.String() {
		t.Fatalf("the row's mark is %d in %q, want %d in %q", seq, where, got.head, f.store.String())
	}
}

// Small pieces: a pass cuts a piece at each transaction past the size, and a
// last piece under the tail size is written again with the next pass's lines
// and the one it replaced deleted.
func TestArchivePiecesAreCutAndTheSmallTailIsReplaced(t *testing.T) {
	t.Parallel()
	f := newArchiveFixture(t, dirStore(t), engine.WithArchivePieceBytes(1))
	for _, name := range []string{"one", "two"} {
		mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": name}})
	}
	f.pass(t)
	first := f.archived(t)
	if len(first.pieces) < 2 {
		t.Fatalf("pieces cut at every transaction: %v", first.pieces)
	}
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "three"}})
	f.pass(t)
	second := f.archived(t)
	second.equals(t, "after the second pass", f.tableHead(t), second.sealed, second.blobs)
	// Every piece of the first pass but its last is still in the chain, and
	// nothing outside the chain is left in the store.
	for _, p := range first.pieces[:len(first.pieces)-1] {
		if !strings.Contains(strings.Join(second.pieces, " "), p) {
			t.Errorf("the piece %s the second pass did not replace is gone", p)
		}
	}
	_, aid, _, _ := newestManifest(t, f.store, f.id)
	listed, err := f.store.List(context.Background(), changelogfile.ArchivePrefix(f.id, aid)+changelogfile.ChangelogSubdir+"/")
	if err != nil {
		t.Fatal(err)
	}
	// Under the dialect of the steps this binary carries, whichever it is.
	dialectDir := fmt.Sprintf("/%s/%d/", changelogfile.ChangelogSubdir, engine.MaxChangelogDialect())
	for _, o := range listed {
		if !strings.Contains(o.Key, dialectDir) {
			t.Fatalf("the piece %s is not under the changelog dialect %s", o.Key, dialectDir)
		}
	}
	if len(listed) != len(second.pieces) {
		t.Fatalf("the store holds %d pieces and the chain is %d: %v", len(listed), len(second.pieces), listed)
	}
}

// A sealed value deleted from the table leaves the archive once a pass has
// written a manifest that no longer needs it.
func TestArchiveDropsWhatTheRepositoryNoLongerHolds(t *testing.T) {
	t.Parallel()
	f := newArchiveFixture(t, dirStore(t))
	ctx := context.Background()
	sgPutProvider(t, f.ds, sgPlainKey)
	f.pass(t)
	if before := f.archived(t); len(before.sealed) == 0 {
		t.Fatal("the archive holds no sealed value to drop")
	}
	if _, err := f.ds.Delete(ctx, owner, sgProviderKind, "prov", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the provider: %v", err)
	}
	if _, err := f.ds.RunGC(ctx); err != nil {
		t.Fatalf("collect: %v", err)
	}
	f.pass(t)
	sealed, blobs := f.held(t)
	f.archived(t).equals(t, "after the delete", f.tableHead(t), sealed, blobs)
}

// THE KILL POINTS. A pass stopped at each durable step leaves an archive
// that agrees with the table: its manifest's head is a head the table had,
// its pieces to that head are the table's lines, and every sealed value and
// blob the records held at that head is there. The next process's pass
// brings it to the table's head.
func TestAnArchivePassStoppedAtEachStepLeavesAnArchiveThatAgrees(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		engine.ArchiveAfterBlobs, engine.ArchiveAfterSealed, engine.ArchiveAfterPiece,
		engine.ArchiveAfterManifest, engine.ArchiveAfterMark,
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := newArchiveFixture(t, dirStore(t), engine.WithArchivePieceBytes(1))
			sgPutProvider(t, f.ds, sgPlainKey)
			mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "before"}})
			if _, err := f.ds.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("the first blob"), ""); err != nil {
				t.Fatal(err)
			}
			f.pass(t)
			headA := f.tableHead(t)
			sealedA, blobsA := f.held(t)

			// What the stopped pass would archive: a second secret, the
			// first one changed, a blob, more lines, and the first
			// provider gone so a delete is owed.
			mustPut(t, f.ds, owner, substrate.PutInput{
				Kind: sgProviderKind, ID: "second",
				Properties: map[string]any{"label": "second", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-second-0001"},
			})
			if _, err := f.ds.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("the second blob"), ""); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"after one", "after two"} {
				mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": name}})
			}
			if _, err := f.ds.Delete(ctx, owner, sgProviderKind, "prov", substrate.DeleteInput{}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.ds.RunGC(ctx); err != nil {
				t.Fatal(err)
			}
			headB := f.tableHead(t)
			sealedB, blobsB := f.held(t)

			f.failAt.Store(stage)
			if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errStopped) {
				t.Fatalf("the pass did not stop at %q: %v", stage, err)
			}
			// The close runs one more pass, which a killed process does not:
			// it stops where the first did.
			f.reopen(t)
			f.failAt.Store("")

			// Between the stop and the next pass: the archive is at A or
			// at B, and holds what that head needs.
			got := f.archived(t)
			switch got.head {
			case headA:
				got.covers(t, "stopped "+stage, sealedA, blobsA)
			case headB:
				got.covers(t, "stopped "+stage, sealedB, blobsB)
			default:
				t.Fatalf("stopped %s: the archive's head is %d, neither %d nor %d", stage, got.head, headA, headB)
			}

			f.pass(t)
			f.archived(t).equals(t, "the next process's pass", f.tableHead(t), sealedB, blobsB)
		})
	}
}

// A pass stopped after a piece it never published leaves that piece in the
// store, and the next process's first listing finds it unused. The next pass
// writes the same piece again, under the same name, and must not delete it
// with the unused ones once its manifest names it.
func TestAPieceAStoppedPassWroteIsKeptWhenThePassAfterWritesItAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newArchiveFixture(t, dirStore(t))
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "first"}})
	f.pass(t)
	for _, name := range []string{"second", "third"} {
		mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": name}})
	}
	f.failAt.Store(engine.ArchiveAfterPiece)
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, errStopped) {
		t.Fatalf("the pass did not stop after its piece: %v", err)
	}
	f.reopen(t)
	f.failAt.Store("")
	f.pass(t)
	sealed, blobs := f.held(t)
	f.archived(t).equals(t, "the pass after the stopped one", f.tableHead(t), sealed, blobs)
}

// An archive whose manifest is past the table's head holds writes this
// database lost: a pass refuses to write over it and leaves it as it was.
func TestAnArchiveAheadOfTheTableIsNotWrittenOver(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newArchiveFixture(t, dirStore(t))
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "one"}})
	f.pass(t)
	key, ahead := putForeignManifest(t, f.store, f.id, func(m *changelogfile.Manifest) {
		m.Epoch++
		m.HeadSum = changelogfile.SumText(sha256.Sum256([]byte("a line this database never wrote")))
	})
	f.reopen(t)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "two"}})
	if err := engine.ArchivePass(ctx, f.ds); !errors.Is(err, engine.ErrArchiveDiverged) {
		t.Fatalf("a pass over an archive ahead of the table: %v, want ErrArchiveDiverged", err)
	}
	after, err := archive.ReadAll(ctx, f.store, key)
	if err != nil || !bytes.Equal(after, ahead) {
		t.Fatalf("the manifest was written over (%v)", err)
	}
}

// THE DELAY. With the store unreachable the passes fail, the writes go on,
// and the archive's status says how far behind it is and since when; once
// the store answers again a pass catches up and the delay is gone.
func TestTheArchiveDelayGrowsWhileTheStoreIsUnreachableAndWritesGoOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := archivetest.NewFakeS3("archive-bucket")
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store, err := archive.NewS3(archive.S3Config{
		Bucket: "archive-bucket", Endpoint: srv.URL, PathStyle: true,
		AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	f := newArchiveFixture(t, store)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "archived"}})
	if st := engine.ArchiveTick(ctx, f.ds); st.Err != nil || st.Pending != 0 {
		t.Fatalf("the first pass: %+v", st)
	}

	repository := f.id
	if g := archiveGauges(t, repository); g["substrate_archive_pending_entries"] != 0 || g["substrate_archive_delay_seconds"] != 0 {
		t.Fatalf("the gauges once the archive holds every entry: %v", g)
	}

	fake.SetDown(true)
	var pending []int64
	var delays []float64
	for i, name := range []string{"one", "two", "three"} {
		mustPut(t, f.ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": name}})
		st := engine.ArchiveTick(ctx, f.ds)
		if st.Err == nil || st.Failures != i+1 || st.Oldest.IsZero() {
			t.Fatalf("a pass against an unreachable store: %+v", st)
		}
		g := archiveGauges(t, repository)
		if g["substrate_archive_pending_entries"] != float64(st.Pending) || g["substrate_archive_failed_passes"] != float64(i+1) {
			t.Fatalf("the gauges after failed pass %d: %v, the pass said %+v", i+1, g, st)
		}
		pending = append(pending, st.Pending)
		delays = append(delays, g["substrate_archive_delay_seconds"])
		time.Sleep(50 * time.Millisecond)
	}
	if !(pending[0] < pending[1] && pending[1] < pending[2]) {
		t.Fatalf("the entries behind did not grow with the writes: %v", pending)
	}
	if !(0 < delays[0] && delays[0] < delays[1] && delays[1] < delays[2]) {
		t.Fatalf("the delay did not grow while the store was unreachable: %v", delays)
	}
	t.Logf("while the store was unreachable: entries behind %v, substrate_archive_delay_seconds %.3f", pending, delays)

	fake.SetDown(false)
	st := engine.ArchiveTick(ctx, f.ds)
	if st.Err != nil || st.Pending != 0 || !st.Oldest.IsZero() || st.Failures != 0 {
		t.Fatalf("the pass after the store came back: %+v", st)
	}
	if g := archiveGauges(t, repository); g["substrate_archive_pending_entries"] != 0 || g["substrate_archive_delay_seconds"] != 0 || g["substrate_archive_failed_passes"] != 0 {
		t.Fatalf("the gauges after the store came back: %v", g)
	}
	sealed, blobs := f.held(t)
	f.archived(t).equals(t, "caught up", f.tableHead(t), sealed, blobs)
}

// archiveGauges reads the substrate_archive_* gauges the registry publishes
// for one repository, as a scrape would.
func archiveGauges(t *testing.T, repository string) map[string]float64 {
	t.Helper()
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, fam := range families {
		if !strings.HasPrefix(fam.GetName(), "substrate_archive_") {
			continue
		}
		for _, m := range fam.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "repository" && l.GetValue() == repository {
					out[fam.GetName()] = m.GetGauge().GetValue()
				}
			}
		}
	}
	return out
}

// A blob whose bytes this server lost cannot be archived, by a bucket's copy
// or a directory's link alike. The pass goes on and publishes, and its
// manifest names the blob as missing, the mark and the gauge count it, so
// "caught up" never reads as complete; every pass tries it again, and once
// the bytes are back the archive holds them and names nothing missing.
func TestAnArchiveNamesTheBlobsItCouldNotCopyAndTriesThemAgain(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		store func(t *testing.T) archive.Store
	}{
		{"a directory", dirStore},
		{"a bucket", func(t *testing.T) archive.Store {
			fake := archivetest.NewFakeS3("archive-bucket")
			srv := httptest.NewServer(fake)
			t.Cleanup(srv.Close)
			store, err := archive.NewS3(archive.S3Config{
				Bucket: "archive-bucket", Endpoint: srv.URL, PathStyle: true,
				AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key",
			})
			if err != nil {
				t.Fatal(err)
			}
			return store
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := newArchiveFixture(t, c.store(t))
			info, err := f.ds.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("bytes this server loses"), "")
			if err != nil {
				t.Fatal(err)
			}
			local := filepath.Join(f.root, changelogfile.RepositoriesDir, f.id, changelogfile.BlobsSubdir, info.Digest)
			body, err := os.ReadFile(local)
			if err != nil {
				t.Fatalf("the blob's bytes on this server: %v", err)
			}
			if err := os.Remove(local); err != nil {
				t.Fatal(err)
			}
			st := engine.ArchiveTick(ctx, f.ds)
			if st.Err != nil {
				t.Fatalf("a pass with a blob's bytes lost: %v", st.Err)
			}
			got := f.archived(t)
			if got.head != f.tableHead(t) || !slices.Equal(got.missing, []string{info.Digest}) || got.blobs[info.Digest] {
				t.Fatalf("the archive at head %d names %v missing and holds the blob = %v, want head %d naming %s", got.head, got.missing, got.blobs[info.Digest], f.tableHead(t), info.Digest)
			}
			var marked int
			if err := f.raw.QueryRow(`SELECT archive_missing FROM repositories WHERE id = $1`, f.id).Scan(&marked); err != nil || marked != 1 || st.Missing != 1 {
				t.Fatalf("the mark says %d missing (%v) and the pass %d, want 1", marked, err, st.Missing)
			}
			if g := archiveGauges(t, f.id); g["substrate_archive_missing_blobs"] != 1 {
				t.Fatalf("the gauges: %v", g)
			}

			if err := os.WriteFile(local, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if st := engine.ArchiveTick(ctx, f.ds); st.Err != nil || st.Missing != 0 {
				t.Fatalf("the pass after the bytes came back: %+v", st)
			}
			sealed, blobs := f.held(t)
			got = f.archived(t)
			if len(got.missing) != 0 {
				t.Fatalf("the archive still names %v missing", got.missing)
			}
			got.equals(t, "the bytes back", f.tableHead(t), sealed, blobs)
		})
	}
}

// THE WRITE'S KILL POINTS. Postgres alone confirms a write, so a write killed
// at any step of its commit has committed or has not, and the archive copies
// what committed and nothing else: after the restart's pass it is the table,
// line for line, the killed write's line in it exactly when the table holds
// it. The steps are the commit's own (dataset.go commit): before the COMMIT,
// after it with its answer lost, and after it answered.
func TestAWriteKilledAtEachStepOfItsCommitLeavesTheArchiveInAgreement(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		stage  string
		landed bool
	}{
		{engine.CommitBeforeCommit, false},
		{engine.CommitInDoubt, true},
		{engine.CommitAfterCommit, true},
	} {
		t.Run(c.stage, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			fault := &armedFault{}
			f := newArchiveFixture(t, dirStore(t), engine.WithTestCommitFault(fault.hook))
			sgPutProvider(t, f.ds, sgPlainKey)
			f.pass(t)
			before := f.tableHead(t)

			fault.arm(c.stage)
			_, err := f.ds.Put(ctx, owner, substrate.PutInput{
				Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "killed"},
			})
			if !errors.Is(err, errCrash) {
				t.Fatalf("the write did not die at %s: %v", c.stage, err)
			}
			f.reopen(t)
			after := f.tableHead(t)
			if landed := after > before; landed != c.landed {
				t.Fatalf("killed at %s the write landed = %v, want %v", c.stage, landed, c.landed)
			}
			f.pass(t)
			sealed, blobs := f.held(t)
			f.archived(t).equals(t, "after the restart's pass", after, sealed, blobs)
		})
	}
}
