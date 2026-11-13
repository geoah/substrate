package engine_test

// THE CRASH TABLE (decisions 0262 and 0279). Every durable step of a boot
// over a directory a release before the archive wrote, of a restore from the
// archive, and of a repository's deletion, with the process stopped right
// after it, and what the next boot does with what it left: with the archive
// reachable and with it unreachable. The archive pass's own steps, and a
// publication's recovery, are archivecontract_db_test.go's; the
// conversion's, conversion_internal_db_test.go's.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// unreachableStore is an archive whose every call fails, as a bucket the
// network cannot reach does.
type unreachableStore struct{}

var errUnreachable = errors.New("the archive's store cannot be reached")

func (unreachableStore) Put(context.Context, string, io.ReadSeeker, int64) error {
	return errUnreachable
}

func (unreachableStore) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errUnreachable
}

func (unreachableStore) List(context.Context, string) ([]archive.Object, error) {
	return nil, errUnreachable
}
func (unreachableStore) Delete(context.Context, string) error { return errUnreachable }
func (unreachableStore) String() string                       { return "unreachable" }

var errKilledHere = errors.New("the process died here")

// bootWith boots a service over dsn and root with opts.
func bootWith(t *testing.T, dsn, root string, opts ...engine.Option) (substrate.Service, error) {
	t.Helper()
	svc, err := engine.OpenForTest(t, context.Background(), dsn, append([]engine.Option{
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestSkipWriterLease(),
	}, opts...)...)
	if err == nil {
		t.Cleanup(func() { _ = svc.Close() })
	}
	return svc, err
}

// bootStoppedAt boots and dies at the first time the import reaches stage,
// in batches of seven lines, so a stop after the first batch leaves lines
// out.
func bootStoppedAt(t *testing.T, dsn, root, stage string) {
	t.Helper()
	fired := false
	_, err := bootWith(t, dsn, root, engine.WithTestImportFault(7, func(s string) error {
		if s == stage && !fired {
			fired = true
			return errKilledHere
		}
		return nil
	}))
	if !errors.Is(err, errKilledHere) {
		t.Fatalf("the boot did not die %s: %v", stage, err)
	}
}

// crashSource is a repository with history, a secret and a blob, and its
// archive, as a stopped server leaves them.
type crashSource struct {
	dsn, root, repo, id, ref, digest string
	fold                             []byte
}

func newCrashSource(t *testing.T) crashSource {
	t.Helper()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	src := crashSource{dsn: dsn, root: engine.DataRootOf(svc), repo: testdb.Repository(t), id: repositoryIDOf(t, ds)}
	src.ref = putProvider(t, ds, dsn, "openai", "sk-crash")
	src.digest = putBlob(t, ds, []byte("crash bytes"))
	src.fold = foldOf(t, ds)
	_ = svc.Close()
	return src
}

// holdsTheSource fails the test unless the service serves the source's
// repository whole: its fold, its secret and its blob.
func (src crashSource) holdsTheSource(t *testing.T, svc substrate.Service, dsn string) {
	t.Helper()
	ds, err := svc.Dataset(context.Background(), src.repo)
	if err != nil {
		t.Fatalf("open the repository: %v", err)
	}
	if after := foldOf(t, ds); string(after) != string(src.fold) {
		t.Fatalf("the fold is not the source's\n%s", firstDifference(src.fold, after))
	}
	if got := openSecret(t, dsn, src.ref); got != "sk-crash" {
		t.Fatalf("the secret = %q", got)
	}
	if got := getBlob(t, ds, src.digest); string(got) != "crash bytes" {
		t.Fatalf("the blob = %q", got)
	}
}

// A directory a release before the archive wrote, imported into a database
// that does not hold its repository and stopped after each durable step
// (rule 6: compare, insert, set aside, convert and fold, remove): the next
// boot finishes the import and the removal of the directory's history, with
// the archive unreachable, because nothing of it comes from the archive.
// Before the set-aside the next boot compares the directory again, in the
// dialect both are still in; after it, it never reads the directory again.
// From the row on, the row carries the import marker, so no boot serves the
// repository before its history is all in.
func TestA0xImportCutShortAtEachStepIsFinishedByTheNextBoot(t *testing.T) {
	t.Parallel()
	src := newCrashSource(t)
	for _, stage := range []string{
		engine.ImportAfterRow, engine.ImportAfterSealed, engine.ImportAfterBatch, engine.CleanupSetAside,
		engine.ImportAfterFirstFold, engine.ImportBeforeCleanup, engine.CleanupPartly,
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := legacyDirectory(t, filepath.Join(src.root, "archive"), root, src.id)
			dsn := engine.MigratedDSN(t)
			bootStoppedAt(t, dsn, root, stage)
			svc, err := bootWith(t, dsn, root, engine.WithArchive(unreachableStore{}))
			if err != nil {
				t.Fatalf("the next boot, the archive unreachable: %v", err)
			}
			if changelogfile.HasHistory(dir) || changelogfile.HistoryAdopted(dir) {
				t.Fatal("the next boot left the directory's history")
			}
			src.holdsTheSource(t, svc, dsn)
		})
	}
}

// The same directory beside the database that holds its repository (an
// upgrade in place), stopped after the import and inside the removal of the
// directory's history: the next boot finishes the removal, even with the
// segments it has not removed yet starting past seq 1.
func TestA0xAdoptionCutShortAtEachStepIsFinishedByTheNextBoot(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{engine.ImportBeforeCleanup, engine.CleanupSetAside, engine.CleanupPartly} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			src := newCrashSource(t)
			dir := legacyDirectory(t, filepath.Join(src.root, "archive"), src.root, src.id)
			segments, err := os.ReadDir(changelogfile.ChangelogDir(dir))
			if err != nil || len(segments) < 2 {
				t.Fatalf("the directory holds %d segment files (%v); the removal cut short needs two", len(segments), err)
			}
			bootStoppedAt(t, src.dsn, src.root, stage)
			svc, err := bootWith(t, src.dsn, src.root, engine.WithArchive(unreachableStore{}))
			if err != nil {
				t.Fatalf("the next boot, the archive unreachable: %v", err)
			}
			if changelogfile.HasHistory(dir) || changelogfile.HistoryAdopted(dir) {
				t.Fatal("the next boot left the directory's history")
			}
			src.holdsTheSource(t, svc, src.dsn)
		})
	}
}

// A restore from the archive into an empty database, stopped after each
// durable step. With the archive reachable the next boot finishes it. With
// it unreachable the next boot refuses while the table lacks lines the
// marker names, rather than serve a repository whose history is not all in,
// and finishes once the archive answers; one stopped after the last line
// went in needs nothing more of the archive and finishes either way.
func TestARestoreCutShortAtEachStepIsFinishedOrRefusedByTheNextBoot(t *testing.T) {
	t.Parallel()
	src := newCrashSource(t)
	for _, c := range []struct {
		stage          string
		needsTheSource bool
	}{
		{engine.ImportAfterRow, true},
		{engine.ImportAfterSealed, true},
		{engine.ImportAfterBatch, true},
		{engine.ImportAfterFirstFold, false},
	} {
		t.Run(c.stage, func(t *testing.T) {
			t.Parallel()
			root := copyRepositoryDir(t, src.root, src.id)
			dsn := engine.MigratedDSN(t)
			bootStoppedAt(t, dsn, root, c.stage)
			if c.needsTheSource {
				if _, err := bootWith(t, dsn, root, engine.WithArchive(unreachableStore{})); !errors.Is(err, engine.ErrImportIncomplete) {
					t.Fatalf("the next boot, the archive unreachable: %v, want ErrImportIncomplete", err)
				}
				if incomplete, err := engine.ImportIncomplete(context.Background(), rawDB(t, dsn)); err != nil || !incomplete {
					t.Fatalf("the refused boot cleared the marker (%v)", err)
				}
			} else {
				svc, err := bootWith(t, dsn, root, engine.WithArchive(unreachableStore{}))
				if err != nil {
					t.Fatalf("the next boot, the archive unreachable: %v", err)
				}
				src.holdsTheSource(t, svc, dsn)
				_ = svc.Close()
			}
			svc, err := bootWith(t, dsn, root)
			if err != nil {
				t.Fatalf("the boot with the archive reachable: %v", err)
			}
			src.holdsTheSource(t, svc, dsn)
		})
	}
}

// A repository's deletion stopped after each durable step (rule 7: the row
// says it began, the mark, the objects, the directory, the rows): from the
// row's word on, no one serves the repository, the next boot finishes the
// deletion, and no boot restores it from the archive, whatever the deletion
// had removed. With the archive unreachable the next boot goes on without
// finishing it, and the one after finishes it.
func TestADeletionCutShortAtEachStepIsFinishedByTheNextBoot(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{engine.EraseAfterBegan, engine.EraseAfterMark, engine.EraseAfterArchiveObject, engine.EraseAfterArchive, engine.EraseAfterDirectory} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			svc, dsn := newService(t)
			repository := testdb.Repository(t)
			user, _, _ := registerUser(t, svc, repository)
			ds, err := svc.Dataset(ctx, repository)
			if err != nil {
				t.Fatal(err)
			}
			mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/llm/memory", Properties: map[string]any{"text": "to be deleted"}})
			if err := engine.ArchivePass(ctx, ds); err != nil {
				t.Fatal(err)
			}
			root := engine.DataRootOf(svc)
			id := ds.Repository().ID
			_ = svc.Close()

			stopped, err := bootWith(t, dsn, root, engine.WithTestEraseFault(func(s string) error {
				if s == stage {
					return errKilledHere
				}
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			waitStep(t)
			if err := stopped.DeleteRepository(ctx, substrate.LoginInput{
				Repository: repository, Password: testPassword, TOTPCode: user.code(t),
			}); !errors.Is(err, errKilledHere) {
				t.Fatalf("the deletion did not die %s: %v", stage, err)
			}
			_ = stopped.Close()

			unreachable, err := bootWith(t, dsn, root, engine.WithArchive(unreachableStore{}))
			if err != nil {
				t.Fatalf("the next boot, the archive unreachable: %v", err)
			}
			if _, err := unreachable.Dataset(ctx, repository); !errors.Is(err, substrate.ErrNotFound) {
				t.Fatalf("the repository whose deletion began, the archive unreachable: %v, want ErrNotFound", err)
			}
			_ = unreachable.Close()

			for range 2 {
				after, err := bootWith(t, dsn, root)
				if err != nil {
					t.Fatalf("the boot with the archive reachable: %v", err)
				}
				if _, err := after.Dataset(ctx, repository); !errors.Is(err, substrate.ErrNotFound) {
					t.Fatalf("the deleted repository after a boot = %v, want ErrNotFound", err)
				}
				if repos, err := after.Repositories(ctx); err != nil || len(repos) != 0 {
					t.Fatalf("the boot holds %+v (%v), want nothing", repos, err)
				}
				_ = after.Close()
			}
			if _, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(root, "archive", changelogfile.RepositoriesDir, id)); !errors.Is(err, changelogfile.ErrArchiveDeleted) {
				t.Fatalf("the archive of the deleted repository: %v, want its deletion mark", err)
			}
		})
	}
}

// A deletion whose archive does not answer has begun: the repository is no
// longer served, and the next start, with the archive answering, finishes it.
func TestADeletionTheArchiveDoesNotAnswerIsFinishedAtTheNextStart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	repository := testdb.Repository(t)
	user, _, _ := registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/llm/memory", Properties: map[string]any{"text": "kept"}})
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	unreachable, err := bootWith(t, dsn, root, engine.WithArchive(unreachableStore{}))
	if err != nil {
		t.Fatal(err)
	}
	waitStep(t)
	if err := unreachable.DeleteRepository(ctx, substrate.LoginInput{
		Repository: repository, Password: testPassword, TOTPCode: user.code(t),
	}); !errors.Is(err, errUnreachable) {
		t.Fatalf("a deletion the archive does not answer: %v, want the store's error", err)
	}
	if _, err := unreachable.Dataset(ctx, repository); !errors.Is(err, engine.ErrRepositoryBeingDeleted) {
		t.Fatalf("the repository whose deletion began: %v, want ErrRepositoryBeingDeleted", err)
	}
	_ = unreachable.Close()
	after, err := bootWith(t, dsn, root)
	if err != nil {
		t.Fatal(err)
	}
	if repos, err := after.Repositories(ctx); err != nil || len(repos) != 0 {
		t.Fatalf("the next start holds %+v (%v), want the deletion finished", repos, err)
	}
}
