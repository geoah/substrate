package engine

// A VERIFY BESIDE A SERVER THAT WRITES AND ARCHIVES (#1073). `repository
// verify` opens the engine read-only beside a running server, reads the
// table in one snapshot and then the archive's store. A server that commits
// and publishes a manifest between that snapshot and a read of the store,
// dropping the manifest and the small last piece before it, is what an
// intact repository looks like to it under load. The seam puts one commit
// and one published manifest before a chosen read of the store, so the test
// does not hang on timing. The verify reads the archive again where a pass
// published under a reading, and reads only what the earlier readings did
// not prove; damage it finds in an object stands whatever the record did,
// and an archive that moved under every reading is a finding.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// besideServer is a server that wrote and archived a repository, every
// transaction its own piece and one blob among them, and a verify opened
// read-only beside it.
type besideServer struct {
	op     *service
	repo   string
	server *dataset
	store  string
	dsn    string

	mu sync.Mutex
	// before runs before each of the verify's reads of the store, the
	// reading counted from 1 (a reading starts with its listing).
	before  func(b *besideServer, reading int, stage, key string) error
	reading int
	// reads is each reading's object and blob reads, by key.
	reads  map[int][]string
	failed error
	writes int
}

func newBesideServer(t *testing.T) *besideServer {
	t.Helper()
	return newBesideServerWith(t, nil)
}

// newBesideServerWith is newBesideServer, with beforePass run after the
// writes and before the server's first archive pass.
func newBesideServerWith(t *testing.T, beforePass func(b *besideServer)) *besideServer {
	t.Helper()
	ctx := context.Background()
	b := &besideServer{reads: map[int][]string{}}
	dsn, root := MigratedDSN(t), t.TempDir()
	b.dsn = dsn
	b.store = t.TempDir()
	dir, err := archive.NewDir(b.store)
	if err != nil {
		t.Fatal(err)
	}
	server, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchive(dir), WithArchiveInterval(time.Hour),
		WithCompactionInterval(-1), WithArchivePieceBytes(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	b.repo = testdb.Repository(t)
	if _, err := server.CreateRepository(ctx, b.repo); err != nil {
		t.Fatal(err)
	}
	sd, err := server.Dataset(ctx, b.repo)
	if err != nil {
		t.Fatal(err)
	}
	b.server = sd.(*dataset)
	for range 3 {
		if err := b.write(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.server.PutBlob(ctx, substrate.ActorPerson, substrate.BlobUpload{MediaType: "text/plain"}, []byte("bytes the archive holds"), ""); err != nil {
		t.Fatal(err)
	}
	if beforePass != nil {
		beforePass(b)
	}
	if err := b.server.ArchivePass(ctx); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	op, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchive(dir), WithDirectoryReadOnly(),
		WithTestVerifyStoreRead(func(stage, key string) {
			b.mu.Lock()
			defer b.mu.Unlock()
			if stage == "list" {
				b.reading++
			}
			if stage == "object" || stage == "blob" {
				b.reads[b.reading] = append(b.reads[b.reading], key)
			}
			if b.failed == nil && b.before != nil {
				b.failed = b.before(b, b.reading, stage, key)
			}
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = op.Close() })
	b.op = op.(*service)
	return b
}

// write commits one record through the server.
func (b *besideServer) write() error {
	b.writes++
	_, err := b.server.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{
		Kind: vocabulary.KindContact, Properties: map[string]any{"name": fmt.Sprintf("written beside the verify %d", b.writes)},
	})
	return err
}

// publish is one commit and one published manifest of the server's, which
// drops the manifest before it and the small last piece it replaced.
func (b *besideServer) publish() error {
	if err := b.write(); err != nil {
		return err
	}
	return b.server.ArchivePass(context.Background())
}

func (b *besideServer) verify(t *testing.T) VerifyReport {
	t.Helper()
	report, err := b.op.VerifyRepository(context.Background(), b.repo)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failed != nil {
		t.Fatalf("the server beside the verify: %v", b.failed)
	}
	return report
}

// readAgain is the keys a later reading read that reading 1 read too.
func (b *besideServer) readAgain() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var again []string
	for r := 2; r <= b.reading; r++ {
		for _, key := range b.reads[r] {
			if slices.Contains(b.reads[1], key) {
				again = append(again, fmt.Sprintf("reading %d: %s", r, key))
			}
		}
	}
	return again
}

// damage changes the bytes of the archive's first piece, which no pass
// writes again, and of its blob, under the names that hold them.
func (b *besideServer) damage(t *testing.T) {
	t.Helper()
	m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(b.store, changelogfile.RepositoriesDir, b.server.info.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Pieces) < 3 {
		t.Fatalf("the archive holds %d pieces: the test proves nothing", len(m.Pieces))
	}
	prefix := filepath.Join(b.store, filepath.FromSlash(changelogfile.ArchivePrefix(b.server.info.ID, m.Archive)))
	piece := filepath.Join(prefix, filepath.FromSlash(m.Pieces[0].Name))
	data, err := os.ReadFile(piece)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0x01
	if err := os.WriteFile(piece, data, 0o644); err != nil {
		t.Fatal(err)
	}
	blobs, err := os.ReadDir(filepath.Join(prefix, changelogfile.BlobsSubdir))
	if err != nil || len(blobs) == 0 {
		t.Fatalf("the archive holds no blob (%v): the test proves nothing", err)
	}
	// A new file under the name: the store links a blob's file to the
	// server's own copy, which stays whole.
	blob := filepath.Join(prefix, changelogfile.BlobsSubdir, blobs[0].Name())
	if err := os.Remove(blob); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("other bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A publish before one read of the store at a time, each read in turn: the
// verify reads the archive again and reports no finding, and holds the
// archive to the table. A publish before every read: the archive never
// stood still, which is a finding that says to run the verify again.
func TestAVerifyBesideAServerThatWritesAndArchivesFindsNothing(t *testing.T) {
	t.Parallel()
	// The reads of the store one reading of this repository makes.
	probe := newBesideServer(t)
	var stages []string
	probe.before = func(_ *besideServer, _ int, stage, _ string) error {
		stages = append(stages, stage)
		return nil
	}
	if report := probe.verify(t); !report.OK || report.ArchiveUnverified != "" {
		t.Fatalf("a verify with nothing beside it: %+v", report)
	}
	for _, want := range []string{"list", "manifest", "object", "blobs", "blob", "unaccounted"} {
		if !slices.Contains(stages, want) {
			t.Fatalf("the verify makes no %s read of the store (%v): the test proves nothing", want, stages)
		}
	}
	for k := range stages {
		t.Run(fmt.Sprintf("before read %d, %s", k+1, stages[k]), func(t *testing.T) {
			t.Parallel()
			b := newBesideServer(t)
			n := 0
			b.before = func(b *besideServer, _ int, _, _ string) error {
				if n++; n != k+1 {
					return nil
				}
				return b.publish()
			}
			report := b.verify(t)
			if !report.OK || len(report.Findings) != 0 {
				t.Fatalf("a verify beside a server that published before its read %d (%s) found:\n%s", k+1, stages[k], strings.Join(report.Findings, "\n"))
			}
			if report.ArchiveUnverified != "" || report.ArchiveHead == 0 {
				t.Fatalf("the archive was not held to the table: head %d, %q", report.ArchiveHead, report.ArchiveUnverified)
			}
		})
	}
	t.Run("before every read", func(t *testing.T) {
		t.Parallel()
		b := newBesideServer(t)
		b.before = func(b *besideServer, _ int, _, _ string) error { return b.publish() }
		report := b.verify(t)
		if report.OK || !verifyFinding(report, "run the verify again, or with the server stopped") {
			t.Fatalf("an archive that moved under every reading: %v (%q), want it a finding", report.Findings, report.ArchiveUnverified)
		}
		if len(report.Findings) != 1 {
			t.Fatalf("an intact archive that moved under every reading found:\n%s", strings.Join(report.Findings, "\n"))
		}
	})
}

// What a reading proved about an object stands for the readings after it,
// and damage found in one is a finding whatever the record did.
func TestAVerifyKeepsWhatAReadingProved(t *testing.T) {
	t.Parallel()
	// A publish at a reading's listing of the objects the database has no
	// record of: after its blobs, before its pieces.
	under := func(readings ...int) func(b *besideServer, reading int, stage, key string) error {
		return func(b *besideServer, reading int, stage, key string) error {
			if stage != "unaccounted" || strings.Contains(key, "/"+changelogfile.ManifestsSubdir+"/") || (len(readings) > 0 && !slices.Contains(readings, reading)) {
				return nil
			}
			return b.publish()
		}
	}

	t.Run("an intact archive, a publish under each of the first two readings", func(t *testing.T) {
		t.Parallel()
		b := newBesideServer(t)
		b.before = under(1, 2)
		report := b.verify(t)
		if !report.OK || len(report.Findings) != 0 || report.ArchiveUnverified != "" {
			t.Fatalf("verify: %v (%q), want it settled and verified", report.Findings, report.ArchiveUnverified)
		}
		if b.reading != 3 {
			t.Fatalf("the verify read the archive %d times, want 3", b.reading)
		}
		if again := b.readAgain(); len(again) != 0 {
			t.Fatalf("later readings read what the first proved:\n%s", strings.Join(again, "\n"))
		}
		if len(b.reads[1]) == 0 {
			t.Fatal("the first reading read no object: the test proves nothing")
		}
	})

	t.Run("damage, a publish during the first reading", func(t *testing.T) {
		t.Parallel()
		b := newBesideServer(t)
		b.damage(t)
		b.before = under(1)
		report := b.verify(t)
		if report.OK || !verifyFinding(report, "is not the piece its manifest selects") || !verifyFinding(report, "holds other bytes than its digest names") {
			t.Fatalf("verify of a damaged piece and blob: %v, want both found", report.Findings)
		}
		if verifyFinding(report, "the stored bytes hash to") {
			t.Fatalf("the server's own blob is damaged too: %v", report.Findings)
		}
		if again := b.readAgain(); len(again) != 0 {
			t.Fatalf("later readings read what the first proved or found damaged:\n%s", strings.Join(again, "\n"))
		}
	})

	t.Run("damage, a publish under every reading", func(t *testing.T) {
		t.Parallel()
		b := newBesideServer(t)
		b.damage(t)
		b.before = under()
		report := b.verify(t)
		for _, want := range []string{"is not the piece its manifest selects", "holds other bytes than its digest names", "run the verify again, or with the server stopped"} {
			if !verifyFinding(report, want) {
				t.Errorf("verify of a damaged archive that moved under every reading: %v, want a finding naming %q", report.Findings, want)
			}
		}
		if report.OK {
			t.Error("the verify reads OK")
		}
	})
}

// A real mismatch is still a finding where the archive did not move while
// the verify read it: a manifest no pass of the database published, and a
// piece the published manifest selects gone from the store. A manifest the
// record says is about to be published, as a pass that stopped between its
// put and its record leaves it, is the database's, as the pass's rule 4
// reads it, and no finding.
func TestAVerifyStillFindsWhatNoPassBesideItExplains(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct {
		name, want string
		edit       func(t *testing.T, b *besideServer)
	}{
		{"a manifest no pass published", "which the database did not publish", func(t *testing.T, b *besideServer) {
			t.Helper()
			m, data := nextManifestOf(t, b)
			putManifest(t, b, m, data)
		}},
		{"a piece the published manifest selects gone", "the store holds no object under this key", func(t *testing.T, b *besideServer) {
			t.Helper()
			m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(b.store, changelogfile.RepositoriesDir, b.server.info.ID))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(b.store, filepath.FromSlash(changelogfile.ArchivePrefix(b.server.info.ID, m.Archive)+m.Pieces[0].Name))); err != nil {
				t.Fatal(err)
			}
		}},
		{"the manifest about to be published", "", func(t *testing.T, b *besideServer) {
			t.Helper()
			m, data := nextManifestOf(t, b)
			putManifest(t, b, m, data)
			if _, err := b.server.db.ExecContext(ctx, `UPDATE archive_records SET pending = $1, epoch = $2`, data, m.Epoch); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := newBesideServer(t)
			c.edit(t, b)
			report := b.verify(t)
			if c.want == "" {
				if !report.OK || len(report.Findings) != 0 {
					t.Fatalf("verify found:\n%s", strings.Join(report.Findings, "\n"))
				}
				return
			}
			if report.OK || !verifyFinding(report, c.want) {
				t.Fatalf("verify found %v, want a finding naming %q", report.Findings, c.want)
			}
		})
	}
}

// nextManifestOf is the next epoch of the newest manifest of the archive,
// under a pass of its own, and its bytes.
func nextManifestOf(t *testing.T, b *besideServer) (changelogfile.Manifest, []byte) {
	t.Helper()
	m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(b.store, changelogfile.RepositoriesDir, b.server.info.ID))
	if err != nil {
		t.Fatal(err)
	}
	m.Epoch++
	if m.Pass, err = changelogfile.NewPassID(); err != nil {
		t.Fatal(err)
	}
	data, err := changelogfile.EncodeArchiveManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	return m, data
}

// putManifest puts data in the store as manifest m.
func putManifest(t *testing.T, b *besideServer, m changelogfile.Manifest, data []byte) {
	t.Helper()
	dir := filepath.Join(b.store, filepath.FromSlash(changelogfile.ArchiveManifestsPrefix(b.server.info.ID, m.Archive)))
	if err := os.WriteFile(filepath.Join(dir, changelogfile.ArchiveManifestName(m.Epoch, data)), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// takeLineOut takes the table's line at seq out, keeping it to put back:
// a table that held no such line when the archive was written, as a repair
// that writes a row back leaves it.
func (b *besideServer) takeLineOut(t *testing.T, seq int64) (putBack func() error) {
	t.Helper()
	db := rawDBOf(t, b.dsn)
	saved := fmt.Sprintf("verify_saved_%d_%d", seq, time.Now().UnixNano())
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s AS SELECT * FROM changelog WHERE repository = '%s' AND seq = %d`, saved, b.server.info.ID, seq)); err != nil {
		t.Fatal(err)
	}
	if res, err := db.Exec(`DELETE FROM changelog WHERE repository = $1 AND seq = $2`, b.server.info.ID, seq); err != nil {
		t.Fatal(err)
	} else if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("took %d lines out at seq %d", n, seq)
	}
	return func() error {
		_, err := db.Exec(fmt.Sprintf(`INSERT INTO changelog SELECT * FROM %s`, saved))
		return err
	}
}

// lineOf is the seq of the record's newest line.
func (b *besideServer) lineOf(t *testing.T, id string) int64 {
	t.Helper()
	var seq int64
	if err := b.server.db.QueryRowContext(context.Background(), `SELECT max(seq) FROM changelog WHERE record_id = $1`, id).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// putContact writes one contact through the server and answers its id.
func (b *besideServer) putContact(t *testing.T, name string) string {
	t.Helper()
	rec, err := b.server.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{
		Kind: vocabulary.KindContact, Properties: map[string]any{"name": name},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec.ID
}

// The chain between pieces is held to the table at every reading, a skipped
// piece's included. A line the table holds that no piece holds, below a
// piece a reading proved and a later one skips, is found by the later one:
// one in the middle of the chain, and one below its first piece. Each is a
// line put back into the table after the archive was written without it,
// between the two readings.
func TestAVerifyHoldsTheChainBetweenSkippedPieces(t *testing.T) {
	t.Parallel()
	for _, first := range []bool{false, true} {
		name := "a line below a piece in the middle"
		if first {
			name = "a line below the first piece"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var seq int64
			var putBack func() error
			b := newBesideServerWith(t, func(b *besideServer) {
				if first {
					seq = 1
				} else {
					// A line with lines after it, so its piece is not the
					// small last one a publish writes again.
					seq = b.lineOf(t, b.putContact(t, "taken out"))
					b.putContact(t, "after it")
					b.putContact(t, "the head")
				}
				putBack = b.takeLineOut(t, seq)
			})
			b.before = func(b *besideServer, reading int, stage, key string) error {
				if reading != 1 || stage != "unaccounted" || strings.Contains(key, "/"+changelogfile.ManifestsSubdir+"/") {
					return nil
				}
				if err := putBack(); err != nil {
					return err
				}
				return b.publish()
			}
			report := b.verify(t)
			if b.reading != 2 {
				t.Fatalf("the verify read the archive %d times, want 2", b.reading)
			}
			want := fmt.Sprintf("seq %d is in the table and not in the archive", seq)
			if report.OK || !verifyFinding(report, want) {
				t.Fatalf("verify: %v, want a finding naming %q", report.Findings, want)
			}
			if again := b.readAgain(); len(again) != 0 {
				t.Fatalf("the second reading read what the first proved, so it did not skip:\n%s", strings.Join(again, "\n"))
			}
		})
	}
}

// A compaction between two readings rewrites lines of pieces the first
// reading proved: the second holds them to the table nowhere, awaiting their
// rewrite, and reports nothing. Once the pass writes them again, a verify
// reads the new pieces and holds them to the table.
func TestAVerifyAfterACompactionBetweenReadings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var compacted string
	b := newBesideServerWith(t, func(b *besideServer) {
		compacted = b.putContact(t, "compacted")
		if _, err := b.server.Patch(ctx, substrate.ActorPerson, vocabulary.KindContact, compacted, substrate.PatchInput{Properties: map[string]any{"name": "compacted, again"}}); err != nil {
			t.Fatal(err)
		}
		b.putContact(t, "after it")
		b.putContact(t, "the head")
	})
	upTo := b.lineOf(t, compacted)
	b.before = func(b *besideServer, reading int, stage, key string) error {
		if reading != 1 || stage != "unaccounted" || strings.Contains(key, "/"+changelogfile.ManifestsSubdir+"/") {
			return nil
		}
		if err := b.publish(); err != nil {
			return err
		}
		report, err := b.server.svc.CompactRepository(ctx, b.repo, upTo)
		if err == nil && report.Removed == 0 {
			err = fmt.Errorf("the compaction removed nothing: %+v", report)
		}
		return err
	}
	report := b.verify(t)
	if !report.OK || len(report.Findings) != 0 {
		t.Fatalf("verify after a compaction between readings: %v", report.Findings)
	}
	if b.reading != 2 || !strings.Contains(report.ArchiveUnverified, "a compaction rewrote lines") {
		t.Fatalf("the verify read the archive %d times and says %q, want 2 and the pieces awaiting their rewrite", b.reading, report.ArchiveUnverified)
	}
	b.before = nil
	if err := b.server.ArchivePass(ctx); err != nil {
		t.Fatalf("the pass that writes the pieces again: %v", err)
	}
	before := b.reading
	report = b.verify(t)
	if !report.OK || len(report.Findings) != 0 || report.ArchiveUnverified != "" {
		t.Fatalf("verify after the pass wrote the pieces again: %v (%q)", report.Findings, report.ArchiveUnverified)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var earlier []string
	for r := 1; r <= before; r++ {
		earlier = append(earlier, b.reads[r]...)
	}
	fresh := slices.DeleteFunc(slices.Clone(b.reads[b.reading]), func(key string) bool { return slices.Contains(earlier, key) })
	if len(fresh) == 0 {
		t.Fatalf("the verify after the pass read no piece the earlier ones had not: %v", b.reads[b.reading])
	}
}

// Damage a reading found in a piece a pass has since replaced is no
// finding about the archive as it stands: the reading the verify settles
// on reads the manifest that no longer selects it.
func TestAVerifyLeavesDamageInAPieceAPassReplaced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var compacted string
	b := newBesideServerWith(t, func(b *besideServer) {
		compacted = b.putContact(t, "compacted")
		if _, err := b.server.Patch(ctx, substrate.ActorPerson, vocabulary.KindContact, compacted, substrate.PatchInput{Properties: map[string]any{"name": "compacted, again"}}); err != nil {
			t.Fatal(err)
		}
		b.putContact(t, "after it")
		b.putContact(t, "the head")
	})
	upTo := b.lineOf(t, compacted)
	m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(b.store, changelogfile.RepositoriesDir, b.server.info.ID))
	if err != nil {
		t.Fatal(err)
	}
	// The piece holding the compacted record's last line, damaged: the
	// compaction rewrites that line, and the pass writes the piece again.
	var damaged string
	for i, p := range m.Pieces {
		_, f, l, _, _ := changelogfile.ParsePieceObjectName(p.Name)
		if f <= upTo && upTo <= l && i+1 < len(m.Pieces) {
			damaged = p.Name
		}
	}
	if damaged == "" {
		t.Fatal("no piece holds the compacted record's last line before the last piece: the test proves nothing")
	}
	path := filepath.Join(b.store, filepath.FromSlash(changelogfile.ArchivePrefix(b.server.info.ID, m.Archive)+damaged))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0x01
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Once the first reading met the damage: the compaction, and the pass
	// that writes the piece again.
	b.before = func(b *besideServer, reading int, stage, _ string) error {
		if reading != 1 || stage != "settle" {
			return nil
		}
		if _, err := b.server.svc.CompactRepository(ctx, b.repo, upTo); err != nil {
			return err
		}
		return b.publish()
	}
	report := b.verify(t)
	b.mu.Lock()
	metIt := slices.ContainsFunc(b.reads[1], func(key string) bool { return strings.HasSuffix(key, damaged) })
	b.mu.Unlock()
	if !metIt {
		t.Fatal("the first reading did not read the damaged piece: the test proves nothing")
	}
	if verifyFinding(report, "is not the piece its manifest selects") || !report.OK {
		t.Fatalf("verify reports damage in a piece a pass replaced: %v", report.Findings)
	}
}

// A verify holds one connection at a time: on a handle of one connection, a
// verify a pass overtakes under its first reading of the archive reads it
// again and settles on the second, where reading the record on the pool
// with the snapshot open, or holding the first reading's transaction into
// the second, waits on itself. TestNoPathTakesASecondConnectionWhileHoldingOne
// holds the verify that settles at once.
func TestAVerifyOnOneConnectionReadsTheArchiveAgain(t *testing.T) {
	t.Parallel()
	b := newBesideServer(t)
	b.op.repoConnsPerHandle = 1
	b.before = func(b *besideServer, reading int, stage, key string) error {
		if reading != 1 || stage != "unaccounted" || strings.Contains(key, "/"+changelogfile.ManifestsSubdir+"/") {
			return nil
		}
		return b.publish()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := b.op.VerifyRepository(ctx, b.repo)
	if err != nil {
		t.Fatalf("verify on one connection: %v", err)
	}
	if !report.OK || report.ArchiveUnverified != "" {
		t.Fatalf("verify on one connection: %v (%q)", report.Findings, report.ArchiveUnverified)
	}
	if b.reading != 2 {
		t.Fatalf("the verify read the archive %d times, want a second reading after the publish", b.reading)
	}
}
