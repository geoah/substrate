package engine

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

// THE OLD COPY OUTLIVES THE DOUBT. A 0.x directory beside its row holds the
// one other copy of every line of the repository's history, so until the
// table has been held row by row to its own checksums and converted, nothing
// of the directory's history is removed or disowned, at any start. Each
// world here is the start gate's two repositories (gateWorld) with A's
// history also written out as a 0.x directory beside its row, opened under
// the test's chain, so the table has a conversion to run; B is the
// neighbor, which serves throughout.

// newAdoptWorld writes A's archive, then the 0.x directory a release before
// the archive kept, from it, beside A's row. write, where given, is A's last
// write before the archive.
func newAdoptWorld(t *testing.T, write ...func(*dataset)) (gateWorld, string) {
	t.Helper()
	ctx := context.Background()
	w := newGateWorld(t)
	svc, err := OpenForTest(t, ctx, w.dsn, WithDataRoot(w.root))
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.Dataset(ctx, w.a.Authority)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range write {
		fn(d.(*dataset))
	}
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	dir := legacyDirFromArchive(t, filepath.Join(w.root, "archive"), w.root, w.a.ID, w.a.ID)
	if !changelogfile.HasHistory(dir) {
		t.Fatal("the fixture wrote no 0.x history")
	}
	return w, dir
}

// startUnder is one start of the world under the test's chain, its writer
// closed before the next.
func (w gateWorld) startUnder(t *testing.T, opts ...Option) *service {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), w.dsn, append([]Option{WithDataRoot(w.root), testConversion()}, opts...)...)
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	return svc.(*service)
}

// dirHistory is every file of the directory's history, each by its path
// under the directory and the digest of its bytes: the manifest (set aside
// or not), the segments and the sealed files.
func dirHistory(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if !strings.HasPrefix(rel, changelogfile.ChangelogSubdir) && !strings.HasPrefix(rel, changelogfile.SealedSubdir) &&
			!strings.HasPrefix(rel, changelogfile.ManifestName) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %x\n", rel, sha256.Sum256(raw))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// recordLineSeq is the seq of A's line that wrote Ada.
func (w gateWorld) recordLineSeq(t *testing.T) int64 {
	t.Helper()
	var seq int64
	if err := rawDBOf(t, w.dsn).QueryRow(`SELECT min(seq) FROM changelog WHERE repository = $1 AND op = 'put' AND record_id = $2`, w.a.ID, fid("ada")).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// wantAsideAt holds A to the start's decision: set aside at the boot check,
// naming seq.
func (w gateWorld) wantAsideAt(t *testing.T, s *service, seq int64) {
	t.Helper()
	r := s.setAsideOf(w.a.ID, "")
	if r == nil || r.stage != setAsideAtCheck || !strings.Contains(r.cause.Error(), fmt.Sprintf("seq %d:", seq)) {
		t.Fatalf("A is set aside as %+v, want at the boot check naming seq %d", r, seq)
	}
	w.wantAsideAndNeighborServing(t, s)
}

// The reviewer's case: a column of the table's row edited under its intact
// checksum, the directory healthy. A comparison of checksums alone passes it,
// the directory would be set aside, and the conversion then refuses the row
// with its one good copy disowned. The comparison holds the row to its own
// checksum and refuses it, naming the seq, before the directory is set
// aside: at every start A is set aside with its directory as it was.
func TestARowEditedUnderItsChecksumLeavesTheDirectoryWholeAtEveryStart(t *testing.T) {
	t.Parallel()
	w, dir := newAdoptWorld(t)
	seq := w.recordLineSeq(t)
	if _, err := rawDBOf(t, w.dsn).Exec(`UPDATE changelog SET actor = actor || '.edited' WHERE repository = $1 AND seq = $2`, w.a.ID, seq); err != nil {
		t.Fatal(err)
	}
	was := dirHistory(t, dir)
	for start := 1; start <= 3; start++ {
		s := w.startUnder(t)
		w.wantAsideAt(t, s, seq)
		_ = s.Close()
		if changelogfile.HistoryAdopted(dir) || !changelogfile.HasHistory(dir) {
			t.Fatalf("start %d adopted the directory beside a row it could not verify", start)
		}
		if now := dirHistory(t, dir); now != was {
			t.Fatalf("start %d changed the directory's history:\nbefore\n%s\nafter\n%s", start, was, now)
		}
	}
}

// A row damaged after the directory was adopted, before the conversion
// commits (the seam stands in for it): the conversion refuses the row, and
// the adopted directory, which holds the row's one good copy, is kept at
// every start after. Once the row is put right, the next start converts the
// history, and only then removes the directory's.
func TestAnAdoptedDirectoryIsKeptUntilItsHistoryConverts(t *testing.T) {
	t.Parallel()
	w, dir := newAdoptWorld(t)
	seq := w.recordLineSeq(t)
	raw := rawDBOf(t, w.dsn)
	var actor string
	if err := raw.QueryRow(`SELECT actor FROM changelog WHERE repository = $1 AND seq = $2`, w.a.ID, seq).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	s := w.startUnder(t, WithTestImportFault(0, func(stage string) error {
		if stage == changelogfile.RemoveHistorySetAside {
			var err error
			once.Do(func() {
				_, err = raw.Exec(`UPDATE changelog SET actor = actor || '.edited' WHERE repository = $1 AND seq = $2`, w.a.ID, seq)
			})
			return err
		}
		return nil
	}))
	w.wantAsideAt(t, s, seq)
	_ = s.Close()
	if !changelogfile.HistoryAdopted(dir) {
		t.Fatal("the seam did not run after the directory was adopted")
	}
	adopted := dirHistory(t, dir)
	for start := 2; start <= 3; start++ {
		s := w.startUnder(t)
		w.wantAsideAt(t, s, seq)
		_ = s.Close()
		if now := dirHistory(t, dir); now != adopted {
			t.Fatalf("start %d removed history of an adopted directory whose repository's conversion it refused:\nbefore\n%s\nafter\n%s", start, adopted, now)
		}
	}

	if _, err := raw.Exec(`UPDATE changelog SET actor = $3 WHERE repository = $1 AND seq = $2`, w.a.ID, seq, actor); err != nil {
		t.Fatal(err)
	}
	s = w.startUnder(t, WithTestImportFault(0, convertedBeforeRemoval(t, w)))
	defer func() { _ = s.Close() }()
	if _, err := s.Dataset(context.Background(), w.a.Authority); err != nil {
		t.Fatalf("A once its row is put right: %v", err)
	}
	if changelogfile.HasHistory(dir) || changelogfile.HistoryAdopted(dir) {
		t.Fatalf("the start that converted A left its directory's history:\n%s", dirHistory(t, dir))
	}
}

// The crash after the adoption and before the conversion, every row whole:
// the next start converts the history, then removes the directory's, as it
// did before the rule.
func TestACrashAfterTheAdoptionConvertsThenRemoves(t *testing.T) {
	t.Parallel()
	w, dir := newAdoptWorld(t)
	crash := errors.New("the process died after the directory was set aside")
	var once sync.Once
	s := w.startUnder(t, WithTestImportFault(0, func(stage string) error {
		var err error
		if stage == changelogfile.RemoveHistorySetAside {
			once.Do(func() { err = crash })
		}
		return err
	}))
	if r := s.setAsideOf(w.a.ID, ""); r == nil || !errors.Is(r.cause, crash) {
		t.Fatalf("A after the crash: %+v", r)
	}
	_ = s.Close()
	if !changelogfile.HistoryAdopted(dir) {
		t.Fatal("the crash did not come after the adoption")
	}
	s = w.startUnder(t, WithTestImportFault(0, convertedBeforeRemoval(t, w)))
	defer func() { _ = s.Close() }()
	if _, err := s.Dataset(context.Background(), w.a.Authority); err != nil {
		t.Fatalf("A at the next start: %v", err)
	}
	if changelogfile.HasHistory(dir) || changelogfile.HistoryAdopted(dir) {
		t.Fatalf("the next start left the directory's history:\n%s", dirHistory(t, dir))
	}
}

// convertedBeforeRemoval is a seam that fails the start if A's directory
// begins to lose its history while A's table is not yet at the test's
// dialect.
func convertedBeforeRemoval(t *testing.T, w gateWorld) func(string) error {
	return func(stage string) error {
		if stage != changelogfile.RemoveHistoryPartly {
			return nil
		}
		var stamp int
		if err := rawDBOf(t, w.dsn).QueryRow(`SELECT dialect FROM changelog_dialect WHERE repository = $1`, w.a.ID).Scan(&stamp); err != nil {
			return err
		}
		if stamp != testDialect {
			return fmt.Errorf("the directory's history began to go with the table at dialect %d, before its conversion to %d", stamp, testDialect)
		}
		return nil
	}
}

// A transaction a release committed to Postgres and died appending to its
// files: the directory holds its first line whole and checksummed, and the
// rest torn, at the end of its highest segment. The table confirmed it, so
// the directory's copy of that line is the one other copy of the row, and
// the upgrade, which opens the directory read-only, leaves those bytes as
// they are. With the row edited under its checksum, every start refuses the
// table before the directory is given up, and the tail is there byte for
// byte; with the row put right, the next start converts, then removes.
func TestATornTransactionsWholeLineOutlivesARefusedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, dir := newAdoptWorld(t, func(ds *dataset) {
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
			{
				"kind": "substrate.reamde.dev/core/package", "metadata": map[string]any{"id": ds.Repository().Authority + "/notes"},
				"data": map[string]any{"authority": ds.Repository().Authority, "package": "notes"},
			},
			{
				"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": ds.Repository().Authority + "/notes/note"},
				"data": map[string]any{
					"authority": ds.Repository().Authority, "package": "notes", "names": map[string]any{"singular": "note"},
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	})
	raw := rawDBOf(t, w.dsn)
	var last int64
	if err := raw.QueryRow(`SELECT max(seq) FROM changelog WHERE repository = $1`, w.a.ID).Scan(&last); err != nil {
		t.Fatal(err)
	}
	var first int64
	if err := raw.QueryRow(`SELECT min(seq) FROM changelog WHERE repository = $1 AND txn = $2`, w.a.ID, last).Scan(&first); err != nil || first >= last {
		t.Fatalf("A's last transaction is %d..%d (%v): the test needs one of two lines or more", first, last, err)
	}

	// Tear the transaction's last line at the end of the highest segment,
	// the active one, as a crash in the middle of an append leaves it: the
	// fixture's writer finished it at its close, so its sidecar goes first.
	logDir := changelogfile.ChangelogDir(dir)
	segments, err := changelogfile.Segments(logDir)
	if err != nil || len(segments) == 0 {
		t.Fatalf("the directory's segments: %+v (%v)", segments, err)
	}
	if highest := segments[len(segments)-1]; highest.Finished {
		if err := os.Remove(filepath.Join(logDir, changelogfile.SidecarName(highest.Name))); err != nil {
			t.Fatal(err)
		}
		if segments, err = changelogfile.Segments(logDir); err != nil || segments[len(segments)-1].Finished {
			t.Fatalf("the highest segment is still finished: %+v (%v)", segments, err)
		}
	}
	active := filepath.Join(logDir, segments[len(segments)-1].Name)
	body, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.LastIndexByte(strings.TrimSuffix(string(body), "\n"), '\n') + 1
	if err := os.WriteFile(active, body[:start+(len(body)-start)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE changelog SET actor = actor || '.edited' WHERE repository = $1 AND seq = $2`, w.a.ID, first); err != nil {
		t.Fatal(err)
	}
	was := dirHistory(t, dir)
	for n := 1; n <= 3; n++ {
		s := w.startUnder(t)
		w.wantAsideAt(t, s, first)
		_ = s.Close()
		if now := dirHistory(t, dir); now != was {
			t.Fatalf("start %d changed the directory, the torn transaction's whole line with it:\nbefore\n%s\nafter\n%s", n, was, now)
		}
	}

	if _, err := raw.Exec(`UPDATE changelog SET actor = left(actor, length(actor) - length('.edited')) WHERE repository = $1 AND seq = $2`, w.a.ID, first); err != nil {
		t.Fatal(err)
	}
	s := w.startUnder(t, WithTestImportFault(0, convertedBeforeRemoval(t, w)))
	defer func() { _ = s.Close() }()
	if _, err := s.Dataset(ctx, w.a.Authority); err != nil {
		t.Fatalf("A once its row is put right: %v", err)
	}
	if changelogfile.HasHistory(dir) || changelogfile.HistoryAdopted(dir) {
		t.Fatalf("the start that converted A left its directory's history:\n%s", dirHistory(t, dir))
	}
}
