package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/testdb"
)

// verifyFinding reports whether a report holds a finding containing want.
func verifyFinding(report VerifyReport, want string) bool {
	for _, f := range report.Findings {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}

// verify says what it did not prove of the archive: before any pass, that
// no copy was published (and that is no finding); once a pass published
// one, that the store lost its manifest, which is a finding, because a boot
// would restore nothing from it.
func TestVerifySaysWhatItDidNotProveOfTheArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	store := archiveDir(t, t.TempDir())
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(root), WithArchive(store), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	repo := testdb.Repository(t)
	info, err := svc.CreateRepository(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	s := svc.(*service)

	report, err := s.VerifyRepository(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || !strings.Contains(report.ArchiveUnverified, "no pass has published") {
		t.Fatalf("verify before any pass: ok %v, unverified %q, findings %v; want ok and the archive said unverified", report.OK, report.ArchiveUnverified, report.Findings)
	}

	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if report, err = s.VerifyRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.ArchiveUnverified != "" || report.ArchiveHead != report.Head {
		t.Fatalf("verify after a pass: ok %v, unverified %q, archive head %d of %d, findings %v", report.OK, report.ArchiveUnverified, report.ArchiveHead, report.Head, report.Findings)
	}

	listed, err := store.List(ctx, changelogfile.ManifestsPrefix(info.ID))
	if err != nil || len(listed) == 0 {
		t.Fatalf("the archive's manifests: %v (%v)", listed, err)
	}
	for _, o := range listed {
		if err := store.Delete(ctx, o.Key); err != nil {
			t.Fatal(err)
		}
	}
	if report, err = s.VerifyRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if report.OK || !verifyFinding(report, "the store holds no manifest") {
		t.Fatalf("verify of an archive that lost its manifest: ok %v, unverified %q, findings %v; want a finding", report.OK, report.ArchiveUnverified, report.Findings)
	}

	// A store that holds nothing of the repository at all (emptied, or
	// another store than the one the pass published to) says so.
	everything, err := store.List(ctx, changelogfile.RepositoryPrefix(info.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range everything {
		if err := store.Delete(ctx, o.Key); err != nil {
			t.Fatal(err)
		}
	}
	if report, err = s.VerifyRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if report.OK || !verifyFinding(report, "the store holds nothing of the repository") {
		t.Fatalf("verify of a store that holds nothing of the repository: ok %v, unverified %q, findings %v; want a finding", report.OK, report.ArchiveUnverified, report.Findings)
	}
}

// An archive in an earlier changelog dialect than the table's is not held
// to the table line by line, and verify says so as a finding rather than
// pass over it.
func TestVerifyReportsAnArchiveInAnEarlierDialectAsNotVerified(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeConvertSource(t)
	svc, err := openConverting(t, src.dsn, src.root, src.archive)
	if err != nil {
		t.Fatalf("open under the chain: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	report, err := svc.(*service).VerifyRepository(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	// The source is at dialect 1 (sourceChain) and the table at the chain's
	// dialect, whatever steps it carries.
	want := fmt.Sprintf("changelog dialect 1 and the table's in %d", svc.(*service).maxDialect)
	if report.OK || !verifyFinding(report, want) {
		t.Fatalf("verify of an archive behind a conversion: ok %v, unverified %q, findings %v; want a finding", report.OK, report.ArchiveUnverified, report.Findings)
	}
	d, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if report, err = svc.(*service).VerifyRepository(ctx, src.repo); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.ArchiveUnverified != "" {
		t.Fatalf("verify once a pass wrote the archive in the table's dialect: ok %v, unverified %q, findings %v", report.OK, report.ArchiveUnverified, report.Findings)
	}
}
