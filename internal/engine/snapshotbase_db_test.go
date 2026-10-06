package engine_test

// A snapshot with a base links what an earlier snapshot holds and copies only
// what was written since, and the server serves a repository before it has
// digested the finished segments, refusing writes once one turns out damaged
// (decision 0146).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

type baseSnapshotter interface {
	SnapshotRepositoryWith(ctx context.Context, repository, destRoot string, opts engine.SnapshotOptions) (engine.SnapshotReport, error)
}

// segmentBytes keeps segments small enough that a short history spans many,
// and large enough that a finished one holds several lines.
const segmentBytes = 4096

// finishedOf lists the finished segments of a repository directory.
func finishedOf(t *testing.T, repoDir string) []changelogfile.Segment {
	t.Helper()
	segs, err := changelogfile.Segments(changelogfile.ChangelogDir(repoDir))
	if err != nil {
		t.Fatal(err)
	}
	var out []changelogfile.Segment
	for _, s := range segs {
		if s.Finished {
			out = append(out, s)
		}
	}
	return out
}

// putTasks writes n tasks named after prefix.
func putTasks(t *testing.T, ds substrate.Dataset, prefix string, n int) {
	t.Helper()
	for i := range n {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{
			"name": prefix + strings.Repeat("x", 200) + string(rune('a'+i%26)),
		}})
	}
}

// The second snapshot links every finished segment and the blob the first
// holds, copies the rest, and restores into an empty database as the
// repository it was taken of, with the first snapshot deleted.
func TestSnapshotWithABaseLinksWhatTheBaseHolds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithChangelogSegmentBytes(segmentBytes))
	putTasks(t, ds, "before", 20)
	first := putBlob(t, ds, []byte("bytes the base holds"))
	root, id := engine.DataRootOf(svc), repositoryIDOf(t, ds)
	_ = svc.Close()

	backups := t.TempDir()
	baseRoot, nextRoot := filepath.Join(backups, "base"), filepath.Join(backups, "next")
	operator := reopenWith(t, dsn, root, engine.WithChangelogSegmentBytes(segmentBytes))
	baseReport, err := operator.(baseSnapshotter).SnapshotRepositoryWith(ctx, id, baseRoot, engine.SnapshotOptions{})
	if err != nil {
		t.Fatalf("the base snapshot: %v", err)
	}
	_ = operator.Close()
	baseFinished := finishedOf(t, baseReport.Directory)
	if len(baseFinished) < 3 {
		t.Fatalf("the base holds %d finished segments; the test wants several", len(baseFinished))
	}

	svc = reopenWith(t, dsn, root, engine.WithChangelogSegmentBytes(segmentBytes))
	ds, err = svc.Dataset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	putTasks(t, ds, "after", 20)
	second := putBlob(t, ds, []byte("bytes written after the base"))
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	_ = svc.Close()

	operator = reopenWith(t, dsn, root, engine.WithChangelogSegmentBytes(segmentBytes))
	report, err := operator.(baseSnapshotter).SnapshotRepositoryWith(ctx, id, nextRoot, engine.SnapshotOptions{Base: baseRoot})
	if err != nil {
		t.Fatalf("the snapshot with a base: %v", err)
	}
	_ = operator.Close()
	// Every finished segment of the base but its last is linked: the last
	// may end past the point the base recorded, so it is copied.
	if report.Base != baseReport.Directory || report.Head != head || report.LinkedSegments < len(baseFinished)-1 || report.LinkedSegments == 0 {
		t.Fatalf("report = %+v, want head %d with at least %d segments linked from %s", report, head, len(baseFinished)-1, baseReport.Directory)
	}
	if report.Blobs != 2 || report.LinkedBlobs != 1 || report.BlobBytes != int64(len("bytes written after the base")) {
		t.Fatalf("report = %+v, want the base's blob linked and the new one copied", report)
	}
	a, err := os.Stat(filepath.Join(changelogfile.ChangelogDir(baseReport.Directory), baseFinished[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(changelogfile.ChangelogDir(report.Directory), baseFinished[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatal("the base's first segment was copied, not linked")
	}
	if a, b := blobFile(t, baseReport.Directory, first), blobFile(t, report.Directory, first); !os.SameFile(a, b) {
		t.Fatal("the base's blob was copied, not linked")
	}

	// The base can go: the new snapshot is a whole directory.
	if err := os.RemoveAll(baseRoot); err != nil {
		t.Fatal(err)
	}
	svc2 := mustReopen(t, engine.MigratedDSN(t), nextRoot)
	ds2, err := svc2.Dataset(ctx, id)
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	for digest, want := range map[string]string{first: "bytes the base holds", second: "bytes written after the base"} {
		if got := getBlob(t, ds2, digest); string(got) != want {
			t.Fatalf("blob %s = %q, want %q", digest, got, want)
		}
	}
	if verified := mustVerify(t, svc2, id); !verified.OK || verified.Head != head || verified.Snapshot == nil || verified.Snapshot.Head != head {
		t.Fatalf("the restored repository does not verify at the recorded point: %+v", verified)
	}
}

func blobFile(t *testing.T, repoDir, digest string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(filepath.Join(changelogfile.BlobsDir(repoDir), digest))
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// A base the source's finished segment no longer matches byte for byte,
// with its sidecar unchanged, is the base's bytes in the copy: the segment is
// linked, not read, and the snapshot restores as the history was written. A
// snapshot without the base reads the source and refuses it.
func TestSnapshotWithABaseCarriesTheBasesBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithChangelogSegmentBytes(segmentBytes))
	putTasks(t, ds, "task", 30)
	before := foldOf(t, ds)
	root, id := engine.DataRootOf(svc), repositoryIDOf(t, ds)
	srcDir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	backups := t.TempDir()
	baseRoot := filepath.Join(backups, "base")
	operator := reopenWith(t, dsn, root, engine.WithChangelogSegmentBytes(segmentBytes))
	if _, err := operator.(baseSnapshotter).SnapshotRepositoryWith(ctx, id, baseRoot, engine.SnapshotOptions{}); err != nil {
		t.Fatalf("the base snapshot: %v", err)
	}
	_ = operator.Close()

	damageMiddleLine(t, srcDir)

	operator = reopenWith(t, dsn, root, engine.WithChangelogSegmentBytes(segmentBytes), engine.WithTestOperator())
	if _, err := operator.(baseSnapshotter).SnapshotRepositoryWith(ctx, id, filepath.Join(backups, "full"), engine.SnapshotOptions{}); !errors.Is(err, engine.ErrSnapshotUnverified) {
		t.Fatalf("a full snapshot of the damaged source: err = %v, want ErrSnapshotUnverified", err)
	}
	report, err := operator.(baseSnapshotter).SnapshotRepositoryWith(ctx, id, filepath.Join(backups, "next"), engine.SnapshotOptions{Base: baseRoot})
	if err != nil {
		t.Fatalf("the snapshot with a base: %v", err)
	}
	_ = operator.Close()
	svc2 := mustReopen(t, engine.MigratedDSN(t), filepath.Join(backups, "next"))
	ds2, err := svc2.Dataset(ctx, id)
	if err != nil {
		t.Fatalf("open the restored repository: %v (%+v)", err, report)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
}

// A base is an earlier snapshot of this repository, and nothing else: a
// directory with no snapshot.json is refused, and so is the repository's own
// data root, whatever it carries.
func TestSnapshotRefusesABaseThatIsNotASnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, id, _, _, _ := snapshotFixture(t)
	operator := reopenWith(t, dsn, root)
	defer func() { _ = operator.Close() }()

	empty := t.TempDir()
	if _, err := operator.(baseSnapshotter).SnapshotRepositoryWith(ctx, id, t.TempDir(), engine.SnapshotOptions{Base: empty}); !errors.Is(err, engine.ErrSnapshotBase) {
		t.Fatalf("an empty base: err = %v, want ErrSnapshotBase", err)
	}

	// A restored root keeps the snapshot.json it came back with.
	repoDir, err := changelogfile.RepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := changelogfile.WriteSnapshot(repoDir, changelogfile.Snapshot{Format: changelogfile.SnapshotFormat, BlobStore: "fs"}); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if _, err := operator.(baseSnapshotter).SnapshotRepositoryWith(ctx, id, dest, engine.SnapshotOptions{Base: root}); !errors.Is(err, engine.ErrSnapshotBase) {
		t.Fatalf("the repository's own root as the base: err = %v, want ErrSnapshotBase", err)
	}
	nothingAt(t, dest, id)
}

// The boot serves a repository whose finished segment is damaged in a line
// between its first and last lines, and the digest behind the open finds it: writes are
// refused from then on with ErrChangelogDamaged, a corrupt-data error.
func TestADamagedSegmentFoundAfterTheOpenRefusesWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithChangelogSegmentBytes(segmentBytes))
	putTasks(t, ds, "task", 30)
	root, id := engine.DataRootOf(svc), repositoryIDOf(t, ds)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	seg := finishedOf(t, dir)[0]
	damageMiddleLine(t, dir)

	svc2 := reopenWith(t, dsn, root, engine.WithChangelogSegmentBytes(segmentBytes))
	ds2, err := svc2.Dataset(ctx, id)
	if err != nil {
		t.Fatalf("the open of a repository damaged between a segment's first and last lines: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err := ds2.Put(ctx, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "after the damage"}})
		if errors.Is(err, engine.ErrChangelogDamaged) {
			if !errors.Is(err, substrate.ErrCorrupt) || !strings.Contains(err.Error(), seg.Name) {
				t.Fatalf("the refusal must be corrupt data naming %s: %v", seg.Name, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("a write before the damage was found: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the digest behind the open never found the damaged segment")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An export hashes every finished segment it streams against its sidecar,
// since an extracted export can be a snapshot's base, which links its
// segments unread: a segment damaged between its first and last lines fails
// the export instead of landing in an archive that looks complete.
func TestExportRefusesAFinishedSegmentThatDoesNotMatchItsSidecar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithChangelogSegmentBytes(segmentBytes))
	putTasks(t, ds, "task", 30)
	root, id := engine.DataRootOf(svc), repositoryIDOf(t, ds)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()
	damageMiddleLine(t, dir)

	svc2 := reopenWith(t, dsn, root, engine.WithChangelogSegmentBytes(segmentBytes), engine.WithTestOperator())
	ds2, err := svc2.Dataset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := ds2.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := ex.WriteTo(&buf); !errors.Is(err, changelogfile.ErrSegmentDigest) {
		t.Fatalf("an export over a damaged segment: err = %v, want ErrSegmentDigest", err)
	}
}

// damageMiddleLine flips a byte in a middle line of the repository's first
// finished segment, keeping its size and its sidecar.
func damageMiddleLine(t *testing.T, repoDir string) {
	t.Helper()
	seg := finishedOf(t, repoDir)[0]
	path := filepath.Join(changelogfile.ChangelogDir(repoDir), seg.Name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitAfter(raw, []byte("\n"))
	if len(lines) < 4 {
		t.Fatalf("segment %s holds %d lines; the test wants a middle one", seg.Name, len(lines)-1)
	}
	lines[1] = bytes.Replace(lines[1], []byte(`"actor":"`), []byte(`"actoR":"`), 1)
	if err := os.WriteFile(path, bytes.Join(lines, nil), 0o600); err != nil {
		t.Fatal(err)
	}
}
