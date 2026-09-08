package engine_test

// The repository directory under the data root, end to end: every commit
// lands in the segment file, the boot check heals a file a crash left behind
// and writes a directory a row never had, a copied directory imports into a
// fresh database as the same repository, and a directory that disagrees with
// the table refuses the boot (decisions 0050 and 0051, docs/plans/filesystem-changelog.md).

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const taskKind = "samples.substrate.reamde.dev/tasks/task"

// repoDirOf is the dataset's directory under the service's data root.
func repoDirOf(t *testing.T, svc substrate.Service, ds substrate.Dataset) string {
	t.Helper()
	dir, err := changelogfile.RepoDir(engine.DataRootOf(svc), repositoryIDOf(t, ds))
	if err != nil {
		t.Fatalf("repository directory: %v", err)
	}
	return dir
}

// reopen opens a second service over the same database and data root, the
// shape of a restart.
func reopen(t *testing.T, dsn, root string) (substrate.Service, error) {
	t.Helper()
	svc, err := engine.Open(context.Background(), dsn,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		t.Cleanup(func() { _ = svc.Close() })
	}
	return svc, err
}

func mustReopen(t *testing.T, dsn, root string) substrate.Service {
	t.Helper()
	svc, err := reopen(t, dsn, root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	return svc
}

// tableChangelog reads every (seq, hash) of the one repository in the schema
// through the tamperer's seat.
func tableChangelog(t *testing.T, dsn string) map[int64][]byte {
	t.Helper()
	rows, err := rawDB(t, dsn).Query(`SELECT seq, hash FROM changelog ORDER BY seq`)
	if err != nil {
		t.Fatalf("read the changelog table: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64][]byte{}
	for rows.Next() {
		var seq int64
		var hash []byte
		if err := rows.Scan(&seq, &hash); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[seq] = hash
	}
	return out
}

// activeSegment is the path of the active (highest) segment of a repository
// directory.
func activeSegment(t *testing.T, dir string) string {
	t.Helper()
	segs, err := changelogfile.Segments(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	if len(segs) == 0 {
		t.Fatal("the repository has no changelog segment")
	}
	last := segs[len(segs)-1]
	if last.Finished {
		t.Fatalf("the highest segment %s is finished; the test needs an active one", last.Name)
	}
	return filepath.Join(changelogfile.ChangelogDir(dir), last.Name)
}

// rewriteChangelogEntries applies edit to every entry of the repository's
// changelog and writes back the ones it changed, in BOTH places the changelog
// lives: the segment file (the line re-encoded, so it stays self-consistent)
// and the table row (op, payload and a re-stamped hash), so the boot check and
// the rebuild see one history and the replay meets the edit. It returns how
// many entries changed. Every edited entry must sit in the active segment,
// which is rewritten in place.
func rewriteChangelogEntries(t *testing.T, svc substrate.Service, dsn string, ds substrate.Dataset, edit func(*changelogfile.Entry) bool) int {
	t.Helper()
	dir := repoDirOf(t, svc, ds)
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatalf("open the changelog files: %v", err)
	}
	entries, err := log.Read(0, 0)
	if err != nil {
		t.Fatalf("read the changelog files: %v", err)
	}
	segs, err := changelogfile.Segments(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	active := segs[len(segs)-1]
	db, err := engine.OpenScopedDB(dsn, repositoryIDOf(t, ds), engine.RoleMaint)
	if err != nil {
		t.Fatalf("open the maintenance pool: %v", err)
	}
	defer func() { _ = db.Close() }()
	var file bytes.Buffer
	changed := 0
	for i := range entries {
		e := &entries[i]
		if edit(e) {
			if e.Seq < active.First {
				t.Fatalf("seq %d is in a finished segment; the test can only rewrite the active one", e.Seq)
			}
			changed++
			_, sum, err := changelogfile.Encode(*e)
			if err != nil {
				t.Fatalf("encode seq %d: %v", e.Seq, err)
			}
			if _, err := db.Exec(`UPDATE changelog SET op = $2, payload = $3::jsonb, hash = $4 WHERE seq = $1`,
				e.Seq, e.Op, []byte(e.Payload), sum[:]); err != nil {
				t.Fatalf("rewrite seq %d in the table: %v", e.Seq, err)
			}
		}
		if e.Seq >= active.First {
			line, _, err := changelogfile.Encode(*e)
			if err != nil {
				t.Fatalf("encode seq %d: %v", e.Seq, err)
			}
			file.Write(line)
			file.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(changelogfile.ChangelogDir(dir), active.Name), file.Bytes(), 0o600); err != nil {
		t.Fatalf("rewrite the active segment: %v", err)
	}
	return changed
}

// copyDir copies a directory tree, the way a backup cron would.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
}

// copyRepositoryDir copies one repository's directory into a fresh data root
// and returns that root.
func copyRepositoryDir(t *testing.T, srcRoot, id string) string {
	t.Helper()
	dstRoot := t.TempDir()
	src, err := changelogfile.RepoDir(srcRoot, id)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := changelogfile.RepoDir(dstRoot, id)
	if err != nil {
		t.Fatal(err)
	}
	copyDir(t, src, dst)
	return dstRoot
}

// openSecret reads one secret-typed value's material the way the engine does:
// the sealed row by ref, the repository's DEK unwrapped from the control plane
// under the host key, the payload opened under the DEK with its row binding.
func openSecret(t *testing.T, dsn, ref string) string {
	t.Helper()
	return openSecretUnder(t, dsn, ref, engine.TestCredentialKeyBytes)
}

// openSecretUnder is openSecret with the host key named: the restoring host's
// key after a rewrap.
func openSecretUnder(t *testing.T, dsn, ref string, hostKey []byte) string {
	t.Helper()
	db := rawDB(t, dsn)
	var payload []byte
	var kind, rid, repoID string
	if err := db.QueryRow(
		`SELECT payload, record_kind, record_id, repository FROM sealed WHERE ref = $1`, ref).
		Scan(&payload, &kind, &rid, &repoID); err != nil {
		t.Fatalf("read sealed payload for %s: %v", ref, err)
	}
	var wrapped []byte
	if err := db.QueryRow(`SELECT dek FROM repositories WHERE id = $1`, repoID).Scan(&wrapped); err != nil {
		t.Fatalf("read wrapped dek: %v", err)
	}
	dek, err := engine.OpenPayloadWithKey(hostKey, wrapped, engine.DEKAAD(repoID))
	if err != nil {
		t.Fatalf("unwrap the DEK: %v", err)
	}
	plain, err := engine.OpenPayloadWithKey(dek, payload, engine.SealedAAD(ref, kind, rid))
	if err != nil {
		t.Fatalf("open payload under the DEK: %v", err)
	}
	return string(plain)
}

// secretRefOf reads the sealed-store ref a record's secret property holds,
// off the stored row: the read surface redacts a secret-typed property.
func secretRefOf(t *testing.T, dsn, kind, id, prop string) string {
	t.Helper()
	var ref string
	if err := rawDB(t, dsn).QueryRow(`SELECT props->>$3 FROM records WHERE kind = $1 AND id = $2`, kind, id, prop).
		Scan(&ref); err != nil {
		t.Fatalf("read %s.%s: %v", kind, prop, err)
	}
	if !strings.HasPrefix(ref, "secret:") {
		t.Fatalf("%s.%s holds %q, not a sealed-store ref", kind, prop, ref)
	}
	return ref
}

// putProvider writes an llmprovider row whose apiKey is a secret-typed
// property, and returns the ref the property stores.
func putProvider(t *testing.T, ds substrate.Dataset, dsn, id, key string) string {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeProvider, ID: id,
		Properties: map[string]any{
			"label": id, "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": key,
		},
	})
	return secretRefOf(t, dsn, typeProvider, id, "apiKey")
}

func putBlob(t *testing.T, ds substrate.Dataset, data []byte) string {
	t.Helper()
	info, err := ds.(substrate.BlobStore).PutBlob(context.Background(), owner,
		substrate.BlobUpload{Name: "note.txt", MediaType: "text/plain"}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	return info.Digest
}

func getBlob(t *testing.T, ds substrate.Dataset, digest string) []byte {
	t.Helper()
	_, data, err := ds.(substrate.BlobStore).GetBlob(context.Background(), digest)
	if err != nil {
		t.Fatalf("get blob %s: %v", digest, err)
	}
	return data
}

// Every commit reaches the segment file: the file's head is the table's, and
// every line's checksum is the row's stamped hash.
func TestChangelogFileFollowsEveryCommit(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
	}
	head := maxSeq(t, ds)
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(repoDirOf(t, svc, ds)))
	if err != nil {
		t.Fatalf("open the changelog files: %v", err)
	}
	if log.Head() != head {
		t.Fatalf("file head = %d, table head = %d", log.Head(), head)
	}
	entries, err := log.Read(0, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	stamped := tableChangelog(t, dsn)
	if int64(len(entries)) != head || len(stamped) != int(head) {
		t.Fatalf("file holds %d entries, table %d rows, head %d", len(entries), len(stamped), head)
	}
	for _, e := range entries {
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			t.Fatalf("seq %d: %v", e.Seq, err)
		}
		if !bytes.Equal(sum[:], stamped[e.Seq]) {
			t.Fatalf("seq %d: the line's checksum is not the row's", e.Seq)
		}
	}
}

// A crash between commit and append leaves the file behind the table; the
// next boot appends the missing entries, byte for byte what the writer would
// have written.
func TestBootCatchesUpTheFileFromTheTable(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	for _, name := range []string{"one", "two", "three", "four"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
	}
	head := maxSeq(t, ds)
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	path := activeSegment(t, dir)
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Drop the last three lines, at a newline boundary: a file that stopped
	// three transactions short of the table.
	cut := whole
	for range 3 {
		cut = cut[:bytes.LastIndexByte(cut[:len(cut)-1], '\n')+1]
	}
	if err := os.WriteFile(path, cut, 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir)); err != nil || l.Head() != head-3 {
		t.Fatalf("after the cut: head = %d, err = %v, want %d", l.Head(), err, head-3)
	}

	mustReopen(t, dsn, root)
	l, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatalf("open after the boot: %v", err)
	}
	if l.Head() != head {
		t.Fatalf("the boot left the file at head %d, the table is at %d", l.Head(), head)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, whole) {
		t.Fatal("the caught-up segment is not byte for byte the one the writer wrote")
	}
}

// A row with no directory is a store that predates the data root: the boot
// writes the directory out from the tables, and it verifies.
func TestBootWritesTheDirectoryForARowWithoutOne(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "kept"}})
	head := maxSeq(t, ds)
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	id := repositoryIDOf(t, ds)
	_ = svc.Close()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	svc2 := mustReopen(t, dsn, root)
	m, err := changelogfile.ReadManifest(dir)
	if err != nil {
		t.Fatalf("the boot wrote no manifest: %v", err)
	}
	if m.Authority != id || m.Username != "ada" || m.Authority != "ada.example.com" || len(m.DEK) == 0 ||
		m.ChangelogDialect != engine.MaxChangelogDialect() || m.CreatedAt.IsZero() {
		t.Fatalf("manifest = %+v", m)
	}
	segs, err := changelogfile.Segments(changelogfile.ChangelogDir(dir))
	if err != nil || len(segs) == 0 {
		t.Fatalf("segments = %v, %v", segs, err)
	}
	sealed, err := changelogfile.ReadSealed(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Registration seals the password hash and the TOTP material.
	if len(sealed) != 2 {
		t.Fatalf("sealed files = %d, want the credential's two", len(sealed))
	}
	report := mustVerify(t, svc2, "ada")
	if !report.OK || report.Head != head || report.FileHead != head || report.SealedRows != 2 || report.SealedFiles != 2 {
		t.Fatalf("the written directory does not verify: %+v", report)
	}
}

// A repository directory copied into a fresh data root imports into a fresh
// database as the same repository: the row from the manifest, the fold from
// the files, the blob bytes and the secret intact.
func TestBootImportsARepositoryDirectory(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "carried"}})
	ref := putProvider(t, ds, dsn, "openai", "sk-carried-across")
	digest := putBlob(t, ds, []byte("bytes that travel with the directory"))
	before := foldOf(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dsn2 := testdb.NewSchema(t)
	svc2 := mustReopen(t, dsn2, root2)
	ctx := context.Background()
	repos, err := svc2.Repositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].ID != id || repos[0].Name != "geoah" || repos[0].Authority != "geoah.example.com" {
		t.Fatalf("repositories after the import = %+v", repos)
	}
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the imported fold is not the original\n%s", firstDifference(before, after))
	}
	if got := getBlob(t, ds2, digest); string(got) != "bytes that travel with the directory" {
		t.Fatalf("blob bytes = %q", got)
	}
	if got := secretRefOf(t, dsn2, typeProvider, "openai", "apiKey"); got != ref {
		t.Fatalf("apiKey ref = %q, want %q", got, ref)
	}
	if got := openSecret(t, dsn2, ref); got != "sk-carried-across" {
		t.Fatalf("secret = %q", got)
	}
	if got, want := openSecret(t, dsn2, ref), openSecret(t, dsn, ref); got != want {
		t.Fatalf("the imported secret %q is not the original %q", got, want)
	}
}

// A file the table does not agree with refuses the boot and names the seq:
// once as a line whose checksum no longer verifies, once as a self-consistent
// line that is not what the table stamped.
func TestBootRefusesADivergentChangelog(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "original"}})
	head := maxSeq(t, ds)
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	path := activeSegment(t, dir)
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(whole, []byte("\n")), []byte("\n"))
	last := lines[len(lines)-1]
	headEntry, _, err := changelogfile.Decode(last)
	if err != nil || headEntry.Seq != head {
		t.Fatalf("the last line is seq %d (%v), want %d", headEntry.Seq, err, head)
	}
	seqText := "seq " + strconv.FormatInt(head, 10)
	writeLast := func(line []byte) {
		out := append(bytes.Join(lines[:len(lines)-1], []byte("\n")), '\n')
		out = append(out, line...)
		out = append(out, '\n')
		if err := os.WriteFile(path, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// One byte inside the payload, nothing re-summed: the line no longer
	// verifies.
	damaged := bytes.Replace(last, []byte(`"original"`), []byte(`"originaL"`), 1)
	if bytes.Equal(damaged, last) {
		t.Fatal("the head line does not carry the payload the test edits")
	}
	writeLast(damaged)
	_, err = reopen(t, dsn, root)
	if err == nil {
		t.Fatal("a changelog file with a line that does not verify booted")
	}
	if !errors.Is(err, changelogfile.ErrBadSum) || !strings.Contains(err.Error(), seqText) {
		t.Fatalf("the refusal must be the checksum's and name %s: %v", seqText, err)
	}

	// The same edit with its sum recomputed: the file is self-consistent and
	// the table disagrees.
	edited := headEntry
	edited.Actor = "somebody-else"
	line, _, err := changelogfile.Encode(edited)
	if err != nil {
		t.Fatal(err)
	}
	writeLast(line)
	_, err = reopen(t, dsn, root)
	if err == nil {
		t.Fatal("a changelog file that disagrees with the table booted")
	}
	if !errors.Is(err, engine.ErrChangelogDiverged) || !strings.Contains(err.Error(), seqText) {
		t.Fatalf("the refusal must be the divergence's and name %s: %v", seqText, err)
	}

	// The original line back: the same database boots.
	writeLast(last)
	mustReopen(t, dsn, root)
}

// The story decision 0051 tells: register, write, upload, store a secret,
// copy the directory, boot a fresh database from it, and the repository is
// back, verifies, and rebuilds to the same fold.
func TestRoundTripDirectoryRestoresARepository(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	_, token, secret := registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	cleared := writeSomeHistory(t, ds)
	ref := putProvider(t, ds, dsn, "openai", "sk-round-trip")
	digest := putBlob(t, ds, []byte("round trip bytes"))
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	secretBefore := openSecret(t, dsn, ref)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dsn2 := testdb.NewSchema(t)
	svc2 := mustReopen(t, dsn2, root2)
	ds2, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	wantLabelsAndVersion(t, ds2, cleared)
	if got := getBlob(t, ds2, digest); string(got) != "round trip bytes" {
		t.Fatalf("blob bytes = %q", got)
	}
	if got := openSecret(t, dsn2, ref); got != secretBefore || got != "sk-round-trip" {
		t.Fatalf("secret = %q, want %q", got, secretBefore)
	}
	// The token registration minted is a record, so it came back too.
	if _, info, err := svc2.Authenticate(ctx, secret); err != nil || info.ID != token.ID {
		t.Fatalf("the registered token does not open the restored repository: %v (%+v)", err, info)
	}
	report := mustVerify(t, svc2, "ada")
	if !report.OK || report.Head != head || report.FileHead != head {
		t.Fatalf("the restored repository does not verify: %+v", report)
	}
	rebuilt, err := svc2.(rebuilder).RebuildRepository(ctx, "ada")
	if err != nil {
		t.Fatalf("rebuild the restored repository: %v", err)
	}
	if rebuilt.Head != head {
		t.Fatalf("the rebuild stopped at %d, the head is %d", rebuilt.Head, head)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the rebuilt fold is not the original\n%s", firstDifference(before, after))
	}
	wantLabelsAndVersion(t, ds2, cleared)
}

// Rotating a secret deletes the old sealed row; the mirror follows, so
// exactly the live ref has a file.
func TestSealedMirrorFollowsRotation(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	first := putProvider(t, ds, dsn, "openai", "sk-first")
	dir := repoDirOf(t, svc, ds)
	if files, err := changelogfile.ReadSealed(dir); err != nil || len(files) != 1 || files[0].Ref != first {
		t.Fatalf("after the first write: files = %+v, %v", files, err)
	}
	mustPatch(t, ds, owner, typeProvider, "openai", substrate.PatchInput{
		Properties: map[string]any{"apiKey": "sk-second"},
	})
	second := secretRefOf(t, dsn, typeProvider, "openai", "apiKey")
	if second == first {
		t.Fatal("the rotation kept the ref; the test proves nothing")
	}
	files, err := changelogfile.ReadSealed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Ref != second {
		t.Fatalf("after the rotation: files = %+v, want exactly %s", files, second)
	}
	var payload []byte
	if err := rawDB(t, dsn).QueryRow(`SELECT payload FROM sealed WHERE ref = $1`, second).Scan(&payload); err != nil {
		t.Fatalf("read the live row: %v", err)
	}
	if !bytes.Equal(files[0].Payload, payload) || files[0].RecordKind != typeProvider || files[0].RecordID != "openai" {
		t.Fatalf("the file is not the row: %+v", files[0])
	}
}

// A store from before the data root carries chain hashes in `changelog.hash`
// that no line can reproduce. With NO file yet, the boot writes the directory
// out and re-stamps every row to the line's checksum, so the migration is the
// one place a stamp is rewritten; afterwards the table and the file agree row
// for row.
func TestBootMigratesRowsWithChainHashes(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
	}
	head := maxSeq(t, ds)
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	db := rawDB(t, dsn)
	if _, err := db.Exec(`UPDATE changelog SET hash = sha256(convert_to(seq::text || random()::text, 'UTF8'))`); err != nil {
		t.Fatalf("garble every stamp: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	svc2 := mustReopen(t, dsn, root)
	report := mustVerify(t, svc2, "geoah")
	if !report.OK || report.Head != head || report.FileHead != head {
		t.Fatalf("the migrated store does not verify: %+v", report)
	}
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := log.Read(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	stamped := tableChangelog(t, dsn)
	if int64(len(entries)) != head {
		t.Fatalf("the file holds %d entries, want %d", len(entries), head)
	}
	for _, e := range entries {
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sum[:], stamped[e.Seq]) {
			t.Fatalf("seq %d: the row's stamp was not rewritten to the line's checksum", e.Seq)
		}
	}
}

// Above a non-empty file a row whose stamp is not its content's checksum is
// not a migration, it is damage: the boot refuses and names the seq rather
// than re-stamping it into history.
func TestBootRefusesAGarbledRowAboveTheFileHead(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	for _, name := range []string{"one", "two"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
	}
	head := maxSeq(t, ds)
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	// The file one transaction short, the way a crash leaves it.
	path := activeSegment(t, dir)
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cut := whole[:bytes.LastIndexByte(whole[:len(whole)-1], '\n')+1]
	if err := os.WriteFile(path, cut, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB(t, dsn).Exec(`UPDATE changelog SET hash = sha256(convert_to('garbled', 'UTF8')) WHERE seq = $1`, head); err != nil {
		t.Fatalf("garble the head's stamp: %v", err)
	}
	_, err = reopen(t, dsn, root)
	if err == nil {
		t.Fatal("a garbled stamp above the file head was written out as history")
	}
	if !errors.Is(err, engine.ErrChangelogDiverged) || !strings.Contains(err.Error(), "seq "+strconv.FormatInt(head, 10)) {
		t.Fatalf("the refusal must be the divergence's and name seq %d: %v", head, err)
	}
	// And the file was not extended by the refused boot.
	if after, _ := os.ReadFile(path); !bytes.Equal(after, cut) {
		t.Fatal("the refused boot changed the segment")
	}
}

// A read-only open (the operator's verify beside a running server) repairs
// nothing: a torn tail stays on disk, a table ahead of its file stays ahead,
// and VerifyRepository names both. A dataset opened this way refuses to write.
func TestReadOnlyOpenLeavesDamageAndVerifyNamesIt(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
	}
	head := maxSeq(t, ds)
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	// The table one ahead of the file, and half of that last line torn on
	// the end of the segment.
	path := activeSegment(t, dir)
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cut := whole[:bytes.LastIndexByte(whole[:len(whole)-1], '\n')+1]
	last := whole[len(cut):]
	damaged := append(append([]byte{}, cut...), last[:len(last)/2]...)
	if err := os.WriteFile(path, damaged, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ro, err := engine.Open(ctx, dsn,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithDirectoryReadOnly())
	if err != nil {
		t.Fatalf("read-only open: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	if after, _ := os.ReadFile(path); !bytes.Equal(after, damaged) {
		t.Fatal("a read-only open changed the segment")
	}
	report := mustVerify(t, ro, "geoah")
	if report.OK {
		t.Fatalf("a torn, behind file verified: %+v", report)
	}
	if report.TruncatedBytes != int64(len(last)/2) {
		t.Fatalf("truncatedBytes = %d, want %d", report.TruncatedBytes, len(last)/2)
	}
	if !findingContaining(report, "torn line") {
		t.Fatalf("no finding names the torn tail: %v", report.Findings)
	}
	if want := "the table's head is " + strconv.FormatInt(head, 10) + " and the file's is " + strconv.FormatInt(head-1, 10); !findingContaining(report, want) {
		t.Fatalf("no finding says %q: %v", want, report.Findings)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, damaged) {
		t.Fatal("verify changed the segment")
	}

	// The dataset opens beside the damage and refuses to write.
	ro2, err := ro.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the dataset read-only: %v", err)
	}
	if _, err := ro2.Put(ctx, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "refused"}}); !errors.Is(err, engine.ErrDirectoryReadOnly) {
		t.Fatalf("a write on a read-only service: err = %v, want ErrDirectoryReadOnly", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, damaged) {
		t.Fatal("opening the dataset read-only changed the segment")
	}
	if maxSeq(t, ro2) != head {
		t.Fatal("a read-only service appended to the table")
	}
	_ = ro.Close()

	// The server's own boot cuts the tail and catches the file up.
	svc3 := mustReopen(t, dsn, root)
	if report := mustVerify(t, svc3, "geoah"); !report.OK || report.FileHead != head {
		t.Fatalf("after the writing boot: %+v", report)
	}
}

// A second process on the same data root (an operator's rebuild while the
// server runs) boots, because a healthy repository needs no writer at boot,
// and then refuses at the repository it would write: the server's dataset
// holds the changelog writer lock. Once the server closes, the same rebuild
// goes through.
func TestASecondProcessRefusesRebuildWhileTheServerHoldsTheLock(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "held"}})
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	if _, err := os.Stat(filepath.Join(changelogfile.ChangelogDir(dir), changelogfile.LockFileName)); err != nil {
		t.Fatalf("the open dataset holds no lock file: %v", err)
	}
	ctx := context.Background()

	second, err := reopen(t, dsn, root)
	if err != nil {
		t.Fatalf("a second process could not boot beside the server: %v", err)
	}
	_, err = second.(rebuilder).RebuildRepository(ctx, "geoah")
	if err == nil {
		t.Fatal("a second process rebuilt a repository the server holds the writer lock on")
	}
	if !errors.Is(err, engine.ErrChangelogLocked) || !errors.Is(err, changelogfile.ErrLocked) {
		t.Fatalf("the refusal must be the lock's: %v", err)
	}
	// The server was not disturbed.
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "still writing"}})
	head := maxSeq(t, ds)
	_ = svc.Close()

	report, err := second.(rebuilder).RebuildRepository(ctx, "geoah")
	if err != nil {
		t.Fatalf("with the server stopped the rebuild must run: %v", err)
	}
	if report.Head != head {
		t.Fatalf("rebuilt to %d, the head is %d", report.Head, head)
	}
}

// A directory whose manifest wraps its DEK under another host's credential
// key is refused at import, naming the repository and the variable: a row
// created from it would be a repository no login could open.
func TestImportRefusesADirectoryTheKeyCannotOpen(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "sealed elsewhere"}})
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dsn2 := testdb.NewSchema(t)
	other := make([]byte, 32)
	if _, err := rand.Read(other); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err := engine.Open(ctx, dsn2,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(root2),
		engine.WithCredentialKey(base64.StdEncoding.EncodeToString(other)))
	if err == nil {
		t.Fatal("a directory the credential key cannot open imported")
	}
	if !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") {
		t.Fatalf("the refusal must name the repository and the key: %v", err)
	}
	var rows int
	if err := rawDB(t, dsn2).QueryRow(`SELECT count(*) FROM repositories`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the refused import left %d row(s)", rows)
	}
	// Under the key the directory was written with, the same import runs.
	svc2 := mustReopen(t, dsn2, root2)
	repos, err := svc2.Repositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].ID != id {
		t.Fatalf("repositories under the right key = %+v, %v", repos, err)
	}
}

// An append that fails AFTER commit latches the dataset: the tables hold the
// write, the file does not, every later write is refused with
// ErrChangelogFileBehind, and the next boot catches the file up. The sealed
// file of the same transaction is on disk before the append is tried, which
// is mirrorAfterCommit's order: sealed/ is always a superset of what the log
// references.
func TestAFailedAppendLatchesTheDatasetUntilABootCatchesUp(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before"}})
	head := maxSeq(t, ds)
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)

	engine.BreakChangelogWriter(ds)
	// The write commits: a provider row with a secret, so a sealed upsert
	// rides the same transaction as the entry the writer cannot take.
	ref := putProvider(t, ds, dsn, "openai", "sk-latched")
	tableHead := maxSeq(t, ds)
	if tableHead <= head {
		t.Fatal("the write did not commit")
	}
	files, err := changelogfile.ReadSealed(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		found = found || f.Ref == ref
	}
	if !found {
		t.Fatalf("the sealed file for %s was not written before the failed append: %+v", ref, files)
	}
	l, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if l.Head() != head {
		t.Fatalf("file head = %d after the failed append, want %d", l.Head(), head)
	}
	// Latched: refused with the named error, and the table does not move.
	if _, err := ds.Put(context.Background(), owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "after"}}); !errors.Is(err, engine.ErrChangelogFileBehind) {
		t.Fatalf("a write after the latch: err = %v, want ErrChangelogFileBehind", err)
	}
	if maxSeq(t, ds) != tableHead {
		t.Fatal("a refused write reached the table")
	}
	_ = svc.Close()

	svc2 := mustReopen(t, dsn, root)
	report := mustVerify(t, svc2, "geoah")
	if !report.OK || report.Head != tableHead || report.FileHead != tableHead {
		t.Fatalf("the boot did not catch the file up: %+v", report)
	}
}

// An authority-named directory under the root with no row and no manifest is
// nobody's: the boot logs it and leaves it, importing nothing and deleting
// nothing.
func TestBootSkipsADirectoryWithNoManifest(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	stray, err := changelogfile.EnsureRepoDir(root, "stray.example.com")
	if err != nil {
		t.Fatal(err)
	}
	w, err := changelogfile.OpenWriter(changelogfile.ChangelogDir(stray), changelogfile.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append([]changelogfile.Entry{{
		Seq: 1, TS: time.Now().UTC(), Actor: "api", Op: "put", RecordID: "r1", Kind: "x.example.com/p/k",
		Payload: json.RawMessage(`{}`),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	svc2 := mustReopen(t, dsn, root)
	repos, err := svc2.Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("a manifest-less directory imported: %+v", repos)
	}
	if _, err := os.Stat(filepath.Join(changelogfile.ChangelogDir(stray), changelogfile.SegmentName(1))); err != nil {
		t.Fatalf("the boot removed or moved the stray directory's segment: %v", err)
	}
}

// Anything else under repositories/ refuses the boot by name: a directory
// that is neither an authority nor a pre-authority id, and a pre-authority id
// with no manifest, may each be a repository nothing can import, so neither is
// skipped. The boot goes through once the entry is moved out.
func TestBootRefusesAStrayDirectoryThatIsNotAnAuthority(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	for _, name := range []string{"tmp", "Backup-2026", "not an id", "k3j9x2m41pfq"} {
		dir := filepath.Join(root, changelogfile.RepositoriesDir, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := reopen(t, dsn, root)
		if err == nil {
			t.Fatalf("%q under repositories/ booted", name)
		}
		if !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "move it out of the data root") {
			t.Fatalf("the refusal of %q must name it and say what to do: %v", name, err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	mustReopen(t, dsn, root)
}

// longAuthority is a valid authority of exactly n bytes, in 63-byte labels.
func longAuthority(n int) string {
	var b strings.Builder
	for b.Len() < n {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strings.Repeat("a", min(63, n-b.Len())))
	}
	return b.String()
}

// The import door holds an authority to the length registration does: the
// authority is the repository id and the id of its self-description record,
// and a record id is at most vocabulary.MaxIDLen bytes, where DNS admits 253.
func TestImportRefusesAnAuthorityLongerThanARecordID(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	authority := longAuthority(200)
	dir, err := changelogfile.EnsureRepoDir(root, authority)
	if err != nil {
		t.Fatalf("the directory layer must admit a DNS-length authority: %v", err)
	}
	if err := changelogfile.WriteManifest(dir, changelogfile.Manifest{
		Format: changelogfile.ManifestFormat, Username: "long", Authority: authority, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = reopen(t, dsn, root)
	if err == nil {
		t.Fatal("a 200-byte authority imported")
	}
	if !strings.Contains(err.Error(), "longer than 128 bytes") || !strings.Contains(err.Error(), authority) {
		t.Fatalf("the refusal must name the authority and the limit: %v", err)
	}
	if repos, err := svc.Repositories(context.Background()); err == nil && len(repos) != 0 {
		t.Fatalf("the refused import created %+v", repos)
	}
}

// The repository's id is its authority at every layer: the control-plane
// row's primary key and its authority column, RepositoryInfo.ID, the scope a
// dataset runs under, the self-description record's id, the directory under
// the data root, the fs blob path, and the binding of the DEK wrap.
func TestRegistrationUsesTheAuthorityAsTheId(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada")
	const authority = "ada.example.com"

	repos, err := svc.Repositories(ctx)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repositories = %+v, %v", repos, err)
	}
	if repos[0].ID != authority || repos[0].Authority != authority || repos[0].Name != "ada" {
		t.Fatalf("RepositoryInfo = %+v, want id and authority %q", repos[0], authority)
	}
	var id, column string
	var wrapped []byte
	if err := rawDB(t, dsn).QueryRow(`SELECT id, authority, dek FROM repositories`).Scan(&id, &column, &wrapped); err != nil {
		t.Fatal(err)
	}
	if id != authority || column != authority {
		t.Fatalf("row id = %q, authority = %q, want both %q", id, column, authority)
	}
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if got := repositoryIDOf(t, ds); got != authority {
		t.Fatalf("the dataset's repository id = %q", got)
	}
	// Every scoped row carries the authority as its repository.
	var scoped string
	if err := rawDB(t, dsn).QueryRow(`SELECT DISTINCT repository FROM changelog`).Scan(&scoped); err != nil || scoped != authority {
		t.Fatalf("changelog rows are scoped to %q (%v), want %q", scoped, err, authority)
	}
	var selfID string
	if err := rawDB(t, dsn).QueryRow(`SELECT id FROM records WHERE kind = 'substrate.reamde.dev/core/repository'`).Scan(&selfID); err != nil || selfID != authority {
		t.Fatalf("the self-description record's id = %q (%v), want %q", selfID, err, authority)
	}

	root := engine.DataRootOf(svc)
	dir := filepath.Join(root, changelogfile.RepositoriesDir, authority)
	m, err := changelogfile.ReadManifest(dir)
	if err != nil {
		t.Fatalf("no manifest at %s: %v", dir, err)
	}
	if m.Authority != authority || m.Username != "ada" || !bytes.Equal(m.DEK, wrapped) {
		t.Fatalf("manifest = %+v", m)
	}
	raw, err := os.ReadFile(filepath.Join(dir, changelogfile.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"id"`)) {
		t.Fatalf("the manifest carries an id key:\n%s", raw)
	}
	digest := putBlob(t, ds, []byte("bytes under the authority"))
	if got, err := os.ReadFile(filepath.Join(dir, "blobs", digest)); err != nil || string(got) != "bytes under the authority" {
		t.Fatalf("blob at <root>/repositories/%s/blobs/%s: %q, %v", authority, digest, got, err)
	}
	// The DEK wrap is bound to the authority and to nothing else.
	if _, err := engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, wrapped, engine.DEKAAD(authority)); err != nil {
		t.Fatalf("the DEK does not open under dek\\x00%s: %v", authority, err)
	}
	if _, err := engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, wrapped, engine.DEKAAD("ada")); err == nil {
		t.Fatal("the DEK opened under a binding that is not the authority")
	}
}

// A `repositories` row whose id is not its authority is a database written
// before the authority became the id. The boot refuses it and says what to do,
// and migration 0015's constraint is re-added without validating the old row.
func TestBootRefusesARowWhoseIdIsNotItsAuthority(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	registerUser(t, svc, "ada")
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	db := rawDB(t, dsn)
	// The constraint holds every row written from now on; the old shape is
	// reached the way an old database reaches it, with no constraint at all.
	if _, err := db.Exec(`ALTER TABLE repositories DROP CONSTRAINT repositories_id_is_authority`); err != nil {
		t.Fatalf("drop the constraint: %v", err)
	}
	if _, err := db.Exec(`UPDATE repositories SET id = 'k3j9x2m41pfq'`); err != nil {
		t.Fatalf("give the row a random id: %v", err)
	}
	_, err := reopen(t, dsn, root)
	if err == nil {
		t.Fatal("a row whose id is not its authority booted")
	}
	if !errors.Is(err, engine.ErrRepositoryIDNotAuthority) {
		t.Fatalf("the refusal must be ErrRepositoryIDNotAuthority: %v", err)
	}
	for _, want := range []string{"Wipe the database and boot again", "k3j9x2m41pfq", "ada.example.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must say %q: %v", want, err)
		}
	}
	var validated bool
	if err := db.QueryRow(`SELECT convalidated FROM pg_constraint WHERE conname = 'repositories_id_is_authority'`).Scan(&validated); err != nil {
		t.Fatalf("the migration did not re-add the constraint: %v", err)
	}
	if validated {
		t.Fatal("the re-added constraint validated the old row, which it cannot have")
	}
	// The wipe the message asks for, then the boot goes through.
	if _, err := db.Exec(`UPDATE repositories SET id = authority`); err != nil {
		t.Fatal(err)
	}
	mustReopen(t, dsn, root)
}

// legacyFixture is a repository directory in the shape a pre-authority binary
// wrote: named by the random id it minted, its manifest carrying `id`, its
// DEK wrapped under `dek\x00<random id>`, and its changelog holding the
// self-description record under that id. It is built from a real
// repository's directory, so the changelog, the sealed files and the blob are
// what such a binary wrote; only the two things that binary spelled with the
// old id are rewritten.
type legacyFixture struct {
	root, oldDir, oldID, authority string
	manifest                       changelogfile.Manifest
	dek, oldWrap                   []byte
	token                          substrate.TokenInfo
	secret, digest, ref            string
	before                         []byte
	head                           int64
}

const repositoryKind = "substrate.reamde.dev/core/repository"

func buildLegacyFixture(t *testing.T) legacyFixture {
	t.Helper()
	svc, dsn := newService(t)
	ctx := context.Background()
	_, token, secret := registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "from before"}})
	ref := putProvider(t, ds, dsn, "openai", "sk-old-binding")
	digest := putBlob(t, ds, []byte("old directory bytes"))
	fx := legacyFixture{
		oldID: "k3j9x2m41pfq", authority: "ada.example.com", token: token, secret: secret,
		digest: digest, ref: ref, before: foldOf(t, ds), head: maxSeq(t, ds),
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The old shape: <root>/repositories/<random id>, the manifest with `id`,
	// the DEK wrapped under `dek\x00<random id>`, the self-description under
	// the random id.
	src := filepath.Join(root, changelogfile.RepositoriesDir, fx.authority)
	fx.root = t.TempDir()
	fx.oldDir = filepath.Join(fx.root, changelogfile.RepositoriesDir, fx.oldID)
	copyDir(t, src, fx.oldDir)
	rewriteRecordID(t, changelogfile.ChangelogDir(fx.oldDir), repositoryKind, fx.authority, fx.oldID)
	m, err := changelogfile.ReadManifest(src)
	if err != nil {
		t.Fatal(err)
	}
	fx.manifest = m
	if fx.dek, err = engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, m.DEK, engine.DEKAAD(fx.authority)); err != nil {
		t.Fatalf("unwrap the DEK: %v", err)
	}
	if fx.oldWrap, err = engine.SealWithKey(engine.TestCredentialKeyBytes, fx.dek, engine.DEKAAD(fx.oldID)); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.MarshalIndent(map[string]any{
		"format": 1, "id": fx.oldID, "username": "ada", "authority": fx.authority,
		"createdAt": m.CreatedAt.Format("2006-01-02T15:04:05.000000Z"), "changelogDialect": m.ChangelogDialect,
		"dek": base64.StdEncoding.EncodeToString(fx.oldWrap),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.oldDir, changelogfile.ManifestName), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	// The fixture is what it claims: the new reader refuses it, the old one
	// reads it.
	if _, err := changelogfile.ReadManifest(fx.oldDir); err == nil {
		t.Fatal("the fixture's manifest reads as the current shape")
	}
	if lm, err := changelogfile.ReadLegacyManifest(fx.oldDir); err != nil || lm.ID != fx.oldID {
		t.Fatalf("the fixture's manifest is not the pre-authority shape: %+v, %v", lm, err)
	}
	return fx
}

// rewriteRecordID re-encodes a changelog directory with the entries of kind
// under id moved to newID, in the entry's record id and in the fold ops its
// payload carries. Every line's checksum is recomputed, so the result
// verifies and reads as a changelog a binary that minted newID wrote.
func rewriteRecordID(t *testing.T, dir, kind, id, newID string) {
	t.Helper()
	log, err := changelogfile.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`"id":\s*"` + regexp.QuoteMeta(id) + `"`)
	var entries []changelogfile.Entry
	moved := 0
	if err := log.Walk(func(e changelogfile.Entry) error {
		e.Payload = append(json.RawMessage(nil), e.Payload...)
		if e.Kind == kind && e.RecordID == id {
			e.RecordID = newID
			e.Payload = re.ReplaceAll(e.Payload, []byte(`"id":"`+newID+`"`))
			moved++
		}
		entries = append(entries, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if moved == 0 {
		t.Fatalf("no entry of %s under %s to move", kind, id)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := changelogfile.OpenWriter(dir, changelogfile.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(entries); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// withoutKind drops every row of one kind from a fold snapshot (the record,
// its managers, annotations, refs and former ids), for a comparison that must
// ignore a record the boot moved.
func withoutKind(t *testing.T, snap []byte, kind string) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(snap))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	for table, list := range doc {
		rows, _ := list.([]any)
		kept := make([]any, 0, len(rows))
		for _, r := range rows {
			if row, ok := r.(map[string]any); ok && (row["kind"] == kind || row["record_kind"] == kind || row["src_kind"] == kind) {
				continue
			}
			kept = append(kept, r)
		}
		doc[table] = kept
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A directory a pre-authority binary wrote, named by the random id it minted
// and carrying a manifest with `id`, imports into a fresh database: the boot
// moves it under its authority, re-wraps the DEK from the old binding to the
// new, writes the format-1 manifest, imports it as any directory with no row,
// and then moves the self-description record from the old id to the authority
// with two changelog entries, so the correction is in the changelog and a
// rebuild reproduces it.
func TestBootImportsAnOldIdNamedDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := buildLegacyFixture(t)
	m := fx.manifest

	dsn2 := testdb.NewSchema(t)
	svc2 := mustReopen(t, dsn2, fx.root)
	if _, err := os.Stat(fx.oldDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the old directory is still there: %v", err)
	}
	newDir := filepath.Join(fx.root, changelogfile.RepositoriesDir, fx.authority)
	m2, err := changelogfile.ReadManifest(newDir)
	if err != nil {
		t.Fatalf("no format-1 manifest under the authority: %v", err)
	}
	if m2.Authority != fx.authority || m2.Username != "ada" || m2.ChangelogDialect != m.ChangelogDialect || !m2.CreatedAt.Equal(m.CreatedAt) {
		t.Fatalf("manifest after the move = %+v", m2)
	}
	if bytes.Equal(m2.DEK, fx.oldWrap) {
		t.Fatal("the DEK was not re-wrapped")
	}
	if got, err := engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, m2.DEK, engine.DEKAAD(fx.authority)); err != nil || !bytes.Equal(got, fx.dek) {
		t.Fatalf("the re-wrapped DEK does not open under the authority to the same key: %v", err)
	}
	repos, err := svc2.Repositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].ID != fx.authority || repos[0].Name != "ada" {
		t.Fatalf("repositories after the import = %+v, %v", repos, err)
	}
	var wrapped []byte
	if err := rawDB(t, dsn2).QueryRow(`SELECT dek FROM repositories WHERE id = $1`, fx.authority).Scan(&wrapped); err != nil || !bytes.Equal(wrapped, m2.DEK) {
		t.Fatalf("the row's DEK is not the manifest's: %v", err)
	}
	ds2, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	// The fold is the original's, except the self-description the boot moved.
	if after := foldOf(t, ds2); string(withoutKind(t, after, repositoryKind)) != string(withoutKind(t, fx.before, repositoryKind)) {
		t.Fatalf("the imported fold is not the original\n%s", firstDifference(withoutKind(t, fx.before, repositoryKind), withoutKind(t, after, repositoryKind)))
	}
	if got := getBlob(t, ds2, fx.digest); string(got) != "old directory bytes" {
		t.Fatalf("blob bytes = %q", got)
	}
	if got := openSecret(t, dsn2, fx.ref); got != "sk-old-binding" {
		t.Fatalf("secret = %q", got)
	}
	if _, info, err := svc2.Authenticate(ctx, fx.secret); err != nil || info.ID != fx.token.ID {
		t.Fatalf("the registered token does not open the imported repository: %v (%+v)", err, info)
	}
	// The self-description: readable under the repository's id, a tombstone
	// under the old one, and both moves in the changelog by the system actor.
	self := mustGet(t, ds2, repositoryKind, ds2.Repository().ID)
	if self.ID != fx.authority || self.Properties["name"] != "ada" || self.Properties["authority"] != fx.authority || self.DeletedAt != nil {
		t.Fatalf("the self-description under the authority = %+v", self)
	}
	if old := mustGet(t, ds2, repositoryKind, fx.oldID); old.DeletedAt == nil {
		t.Fatalf("the self-description under the old id is not a tombstone: %+v", old)
	}
	moves := changesSince(t, ds2, fx.head)
	if len(moves) != 2 ||
		moves[0].Op != substrate.OpPut || moves[0].Kind != repositoryKind || moves[0].RecordID != fx.authority || moves[0].Actor != substrate.ActorSystem ||
		moves[1].Op != substrate.OpDelete || moves[1].Kind != repositoryKind || moves[1].RecordID != fx.oldID || moves[1].Actor != substrate.ActorSystem {
		t.Fatalf("the correction is not two system entries, a put under the authority and a delete of the old id: %+v", moves)
	}
	report := mustVerify(t, svc2, "ada")
	if !report.OK || report.Head != fx.head+2 || report.FileHead != fx.head+2 {
		t.Fatalf("the imported repository does not verify with the correction in both stores: %+v", report)
	}
	// The correction is in the changelog: a rebuild from the files reproduces
	// the fold that holds it.
	folded := foldOf(t, ds2)
	if _, err := svc2.(rebuilder).RebuildRepository(ctx, "ada"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if again := foldOf(t, ds2); string(again) != string(folded) {
		t.Fatalf("the rebuilt fold is not the imported one\n%s", firstDifference(folded, again))
	}
	// A second boot finds nothing to move and nothing to correct.
	_ = svc2.Close()
	svc3 := mustReopen(t, dsn2, fx.root)
	ds3, err := svc3.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if got := maxSeq(t, ds3); got != fx.head+2 {
		t.Fatalf("a second boot appended: head %d, want %d", got, fx.head+2)
	}
}

// s3LikeBackend is a blob backend named s3 whose old-id prefix holds what the
// test says: the shape of a bucket after a rename on disk, with no bucket.
// Everything but the name and the legacy listing is the fs backend's.
type s3LikeBackend struct {
	blobbytes.Backend
	legacy map[string][]blobbytes.Object
}

func (s3LikeBackend) Name() string { return blobbytes.BackendS3 }

func (b s3LikeBackend) ListLegacyRepository(_ context.Context, id string, _ int) ([]blobbytes.Object, error) {
	return b.legacy[id], nil
}

// Under a blob store that keys objects by the repository id, renaming the
// directory moves no blob: the boot refuses to move a pre-authority directory
// while the old id's prefix holds objects, names both prefixes, and leaves
// the directory where it is; once the prefix is empty the move proceeds.
func TestLegacyMoveRefusesWhileTheBucketHoldsTheOldPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := buildLegacyFixture(t)
	dsn2 := testdb.NewSchema(t)
	fsBackend, err := blobbytes.NewFS(fx.root)
	if err != nil {
		t.Fatal(err)
	}
	backend := &s3LikeBackend{Backend: fsBackend, legacy: map[string][]blobbytes.Object{
		fx.oldID: {{Digest: fx.digest, Size: 19}},
	}}
	open := func() (substrate.Service, error) {
		return engine.Open(ctx, dsn2,
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
			engine.WithDataRoot(fx.root),
			engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithBlobStore(backend))
	}
	_, err = open()
	if err == nil {
		t.Fatal("the boot moved a directory whose blobs are still under the old id")
	}
	for _, want := range []string{"<SUBSTRATE_BLOB_S3_PREFIX>" + fx.oldID + "/", "<SUBSTRATE_BLOB_S3_PREFIX>" + fx.authority + "/", "boot again"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must say %q: %v", want, err)
		}
	}
	if _, err := os.Stat(fx.oldDir); err != nil {
		t.Fatalf("the refusal moved the directory: %v", err)
	}
	var rows int
	if err := rawDB(t, dsn2).QueryRow(`SELECT count(*) FROM repositories`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("the refused boot created %d rows (%v)", rows, err)
	}

	// The operator moved the objects: the old prefix is empty.
	backend.legacy = nil
	svc, err := open()
	if err != nil {
		t.Fatalf("the boot after the move: %v", err)
	}
	defer func() { _ = svc.Close() }()
	repos, err := svc.Repositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].ID != fx.authority {
		t.Fatalf("repositories after the move = %+v, %v", repos, err)
	}
}

// A move that crashed between the rename and the manifest write (the
// directory under its authority, the manifest still the old shape) is held
// to the same check as the move: an authority a `repositories` row already
// holds refuses the boot.
func TestInterruptedLegacyMoveRefusesATakenAuthority(t *testing.T) {
	t.Parallel()
	fx := buildLegacyFixture(t)
	newDir := filepath.Join(fx.root, changelogfile.RepositoriesDir, fx.authority)
	if err := os.Rename(fx.oldDir, newDir); err != nil {
		t.Fatal(err)
	}
	// A row that holds the authority, registered on the same database from
	// another data root.
	dsn2 := testdb.NewSchema(t)
	other := mustReopen(t, dsn2, t.TempDir())
	registerUser(t, other, "ada")
	_ = other.Close()

	_, err := reopen(t, dsn2, fx.root)
	if err == nil {
		t.Fatal("an interrupted move onto a taken authority booted")
	}
	if !strings.Contains(err.Error(), "already holds") || !strings.Contains(err.Error(), fx.authority) {
		t.Fatalf("the refusal must name the taken authority: %v", err)
	}
}

// An import lands its rows in batches and folds after the last one, so a boot
// that died after that batch used to leave equal heads and an empty fold that
// the next boot served as a healthy repository; one that died between the two
// fold passes left records with no refs and unweighted fts. Each case here
// kills the import at one durable step (the no-row restore path first, then
// the row path a resumed boot takes), holds that no dataset opens over what
// the crash left, reboots, and compares the result to an uninterrupted import:
// the same fold (records, refs, fts), the same changelog rows, nothing
// appended, the marker gone.
func TestAnInterruptedImportResumesAtTheNextBoot(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	ref := putProvider(t, ds, dsn, "openai", "sk-survives-the-crash")
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	rows := tableChangelog(t, dsn)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// A batch far below the history, so the import spans several and "after
	// the last batch" is not "after the first".
	const batch = 7
	batches := int((head + batch - 1) / batch)
	if batches < 3 {
		t.Fatalf("head %d spans %d batches of %d; the schedule below needs at least 3", head, batches, batch)
	}

	// A crash is the stage that kills one boot, and which occurrence of it;
	// a schedule kills one boot per crash, in order, and the boot after the
	// last one runs undisturbed.
	type crash struct {
		stage string
		nth   int
	}
	cases := []struct {
		name    string
		crashes []crash
	}{
		{"after the first batch", []crash{{engine.ImportAfterBatch, 1}}},
		{"after the last batch", []crash{{engine.ImportAfterBatch, batches}}},
		{"between the fold passes", []crash{{engine.ImportAfterFirstFold, 1}}},
		// The resumed boot has batches-1 batches left, so its last batch is
		// the (batches-1)th: the same window as above, on the row path.
		{"at every step in turn", []crash{
			{engine.ImportAfterBatch, 1},
			{engine.ImportAfterBatch, batches - 1},
			{engine.ImportAfterFirstFold, 1},
		}},
	}
	errKilled := errors.New("the process died here")
	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root2 := copyRepositoryDir(t, root, id)
			dsn2 := testdb.NewSchema(t)
			for i, c := range tc.crashes {
				seen := 0
				_, err := engine.Open(ctx, dsn2,
					engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
					engine.WithDataRoot(root2),
					engine.WithCredentialKey(engine.TestCredentialKey),
					engine.WithTestImportFault(batch, func(stage string) error {
						if stage != c.stage {
							return nil
						}
						seen++
						if seen == c.nth {
							return errKilled
						}
						return nil
					}))
				if !errors.Is(err, errKilled) {
					t.Fatalf("boot %d did not die %s (occurrence %d): %v", i+1, c.stage, c.nth, err)
				}
				incomplete, err := engine.ImportIncomplete(ctx, rawDB(t, dsn2))
				if err != nil {
					t.Fatal(err)
				}
				if !incomplete {
					t.Fatalf("boot %d died %s and left no import-progress marker", i+1, c.stage)
				}
				if n := len(tableChangelog(t, dsn2)); n > int(head) {
					t.Fatalf("boot %d left %d changelog rows, the history has %d", i+1, n, head)
				}
				// What the crash left is not served, not even read-only: a
				// second process beside the (dead) server meets the marker at
				// the open and is told to boot the server.
				ro, err := engine.Open(ctx, dsn2,
					engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
					engine.WithDataRoot(root2),
					engine.WithCredentialKey(engine.TestCredentialKey),
					engine.WithDirectoryReadOnly())
				if err != nil {
					t.Fatalf("open read-only after boot %d: %v", i+1, err)
				}
				if _, err := ro.Dataset(ctx, "geoah"); !errors.Is(err, engine.ErrImportIncomplete) {
					t.Fatalf("a read-only open after boot %d = %v, want ErrImportIncomplete", i+1, err)
				}
				// The files and the rows agree, so verify would say OK; the
				// report names the unfinished import instead.
				report := mustVerify(t, ro, "geoah")
				if report.OK || !findingContaining(report, "import of the repository directory has not completed") {
					t.Fatalf("verify after boot %d does not name the unfinished import: %+v", i+1, report)
				}
				_ = ro.Close()
			}

			svc2 := mustReopen(t, dsn2, root2)
			ds2, err := svc2.Dataset(ctx, "geoah")
			if err != nil {
				t.Fatalf("open the repository after the import resumed: %v", err)
			}
			if after := foldOf(t, ds2); string(after) != string(before) {
				t.Fatalf("the resumed import's fold is not the original\n%s", firstDifference(before, after))
			}
			if got := maxSeq(t, ds2); got != head {
				t.Fatalf("head after the resumed import = %d, want %d: the boot appended", got, head)
			}
			after := tableChangelog(t, dsn2)
			if len(after) != len(rows) {
				t.Fatalf("%d changelog rows after the resumed import, want %d", len(after), len(rows))
			}
			for seq, hash := range rows {
				if !bytes.Equal(after[seq], hash) {
					t.Fatalf("seq %d: checksum after the resumed import is not the original's", seq)
				}
			}
			incomplete, err := engine.ImportIncomplete(ctx, rawDB(t, dsn2))
			if err != nil {
				t.Fatal(err)
			}
			if incomplete {
				t.Fatal("the import-progress marker outlived the import")
			}
			if got := openSecret(t, dsn2, ref); got != "sk-survives-the-crash" {
				t.Fatalf("secret after the resumed import = %q", got)
			}
			if report := mustVerify(t, svc2, "geoah"); !report.OK || report.Head != head {
				t.Fatalf("the resumed repository does not verify: %+v", report)
			}
		})
	}
}

// A binary from before the marker serves a marked repository and dies between
// a commit and its append, so the next boot of this binary finds the table
// ahead of the file AND the marker set. The catch-up appends the row, and the
// resumed fold must include it: a refold over the changelog as it was opened
// before the catch-up would fold one entry short and still clear the marker.
func TestAResumedImportFoldsWhatTheCatchUpAppended(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	putProvider(t, ds, dsn, "openai", "sk-appended-last")
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The import dies after its last batch: every row in the table, the
	// marker set, the fold empty.
	root2 := copyRepositoryDir(t, root, id)
	dsn2 := testdb.NewSchema(t)
	errKilled := errors.New("the process died here")
	ctx := context.Background()
	_, err := engine.Open(ctx, dsn2,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(root2),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestImportFault(int(head), func(stage string) error {
			if stage == engine.ImportAfterBatch {
				return errKilled
			}
			return nil
		}))
	if !errors.Is(err, errKilled) {
		t.Fatalf("the import did not die after its last batch: %v", err)
	}
	// The file loses its last line, which is what a commit with no append
	// leaves: the table one ahead of the file.
	dir, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	path := activeSegment(t, dir)
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cut := whole[:bytes.LastIndexByte(whole[:len(whole)-1], '\n')+1]
	if err := os.WriteFile(path, cut, 0o600); err != nil {
		t.Fatal(err)
	}

	svc2 := mustReopen(t, dsn2, root2)
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open after the catch-up and the resumed import: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the resumed fold lacks what the catch-up appended\n%s", firstDifference(before, after))
	}
	if report := mustVerify(t, svc2, "geoah"); !report.OK || report.Head != head || report.FileHead != head {
		t.Fatalf("the repository does not verify after the catch-up: %+v", report)
	}
}
