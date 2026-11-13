package engine

// The boot gives every repository a release before ids left keyed by its name
// an id (repositoryid.go). These tests make that shape by hand from a
// repository this binary created: every row, the control-plane row, the data
// key's binding, a spooled webhook request and the directory moved back from
// the id to the name, the manifest written in format 1. Then a boot, whole or
// cut short between its two halves, has to leave the repository keyed by a
// new id with nothing keyed by the name, and serving.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const keyingSpoolFire = "fire-keying"

// keyedByName is a repository put back into the shape a release before ids
// left it in.
type keyedByName struct {
	dsn, root, name string
	spool           string
}

func writeKeyedByName(t *testing.T) keyedByName {
	t.Helper()
	ctx := context.Background()
	k := keyedByName{dsn: MigratedDSN(t), root: t.TempDir(), name: testdb.Repository(t)}
	svcI, err := OpenForTest(t, ctx, k.dsn, WithDataRoot(k.root))
	if err != nil {
		t.Fatal(err)
	}
	s := svcI.(*service)
	info, err := s.CreateRepository(ctx, k.name)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Dataset(ctx, k.name)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/contact", ID: fid("ada"), Properties: map[string]any{"name": "Ada"},
	}); err != nil {
		t.Fatal(err)
	}
	// The repository's own record under the name, where a release before
	// ids wrote it.
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		row, err := tx.loadRow(eref{Kind: kindRepository, ID: info.ID}, true)
		if err != nil || row == nil {
			return fmt.Errorf("the repository's record: %v, %w", row, err)
		}
		named := &erow{ID: k.name, Kind: kindRepository, Title: row.Title, States: row.States, Props: row.Props, Labels: row.Labels, KindVersion: row.KindVersion}
		if _, err := tx.foldRow(nil, named, false, false); err != nil {
			return err
		}
		if err := tx.appendChange(substrate.ActorSystem, substrate.OpPut, k.name, kindRepository, map[string]any{"created": true}); err != nil {
			return err
		}
		return tx.migrateTombstone(eref{Kind: kindRepository, ID: info.ID})
	}); err != nil {
		t.Fatal(err)
	}
	// A spooled request the drain never reached, sealed under the name as a
	// release before ids sealed it.
	sealed, err := ds.sealPayload([]byte(`{"format":1}`), spoolAAD(k.name, keyingSpoolFire))
	if err != nil {
		t.Fatal(err)
	}
	k.spool = changelogfile.SpoolFileName(time.Now().UnixNano(), keyingSpoolFire)
	repo, err := s.repositoryByID(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	dek, err := s.unwrapDEK(repo.DEK, repo.ID, repo.DEKKeyID)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := s.wrapDEK(dek, k.name)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db := rawDBOf(t, k.dsn)
	// A row in every table the writes above leave empty, so the keying is
	// held to every table the schema keys by repository, the ones no
	// ordinary write here fills included (the allowance ledger, the embed
	// queue, a trigger's cursor).
	fillEveryRepositoryTable(t, db, info.ID)
	// Every table the schema keys by repository, discovered rather than
	// taken from the list the keying walks, so a table that list lacks is
	// keyed by the name here and found keyed by it after the boot.
	for _, table := range repositoryColumnTables(t, db) {
		if _, err := db.ExecContext(ctx, `UPDATE `+table+` SET repository = $1 WHERE repository = $2`, k.name, info.ID); err != nil {
			t.Fatalf("key %s by the name: %v", table, err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE repositories SET id = $1, dek = $2 WHERE id = $3`, k.name, wrapped, info.ID); err != nil {
		t.Fatal(err)
	}
	// A release before ids never ran the migration that moves the record.
	if _, err := db.ExecContext(ctx, `DELETE FROM repository_migrations WHERE repository = $1 AND name = 'key_the_repository_record_by_its_id'`, k.name); err != nil {
		t.Fatal(err)
	}
	from, err := changelogfile.RepoDir(k.root, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	to, err := changelogfile.RepoDir(k.root, k.name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(to, changelogfile.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var mf changelogfile.Manifest
	if err := mf.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	mf.Format, mf.ID, mf.DEK = changelogfile.ManifestFormatBeforeIDs, "", wrapped
	if err := changelogfile.WriteManifest(to, mf); err != nil {
		t.Fatal(err)
	}
	if err := changelogfile.WriteSpoolFile(to, k.spool, sealed); err != nil {
		t.Fatal(err)
	}
	return k
}

// holdKeyed holds the repository to the end of its keying: a minted id on the
// row, no row of any table keyed by the name, the directory under the id with
// a format-2 manifest, the spooled request opening under the id, and the
// repository serving its record, with its own record moved to the id.
func holdKeyed(t *testing.T, k keyedByName) {
	t.Helper()
	ctx := context.Background()
	svcI, err := OpenForTest(t, ctx, k.dsn, WithDataRoot(k.root))
	if err != nil {
		t.Fatalf("the boot that keys it: %v", err)
	}
	t.Cleanup(func() { _ = svcI.Close() })
	s := svcI.(*service)
	repo, err := s.repositoryByAuthority(ctx, k.name)
	if err != nil {
		t.Fatal(err)
	}
	if !vocabulary.ValidRepositoryID(repo.ID) {
		t.Fatalf("the repository is keyed %q after the boot", repo.ID)
	}
	db := rawDBOf(t, k.dsn)
	// Every table the schema gives a repository column, discovered rather
	// than taken from the list the keying walks, so a table that list lacks
	// is a failure here and not a table both pass over.
	for _, table := range repositoryColumnTables(t, db) {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE repository = $1`, k.name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			t.Errorf("%s holds %d rows keyed by the name", table, n)
		}
	}
	if _, err := os.Stat(filepath.Join(k.root, changelogfile.RepositoriesDir, k.name)); !os.IsNotExist(err) {
		t.Fatalf("the name's directory is still there: %v", err)
	}
	dir, err := changelogfile.RepoDir(k.root, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	m, err := changelogfile.ReadManifest(dir)
	if err != nil || m.Format != changelogfile.ManifestFormat || m.ID != repo.ID || m.Authority != k.name {
		t.Fatalf("manifest %+v, %v", m, err)
	}
	d, err := s.Dataset(ctx, k.name)
	if err != nil {
		t.Fatalf("open the keyed repository: %v", err)
	}
	ds := d.(*dataset)
	sealed, err := changelogfile.ReadSpoolFile(dir, k.spool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.openPayload(sealed, spoolAAD(repo.ID, keyingSpoolFire)); err != nil {
		t.Fatalf("the spooled request does not open under the id: %v", err)
	}
	if rec, err := ds.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil || rec.Properties["name"] != "Ada" {
		t.Fatalf("the contact reads %+v, %v", rec, err)
	}
	self, err := ds.Get(ctx, kindRepository, repo.ID)
	if err != nil || self.Properties["authority"] != k.name {
		t.Fatalf("the repository's record under its id: %+v, %v", self, err)
	}
	if old, err := ds.Get(ctx, kindRepository, k.name); err == nil && old.DeletedAt == nil {
		t.Fatalf("the repository's record under its name is still live")
	}
}

func TestARepositoryKeyedByItsNameTakesAnIDAtTheBoot(t *testing.T) {
	t.Parallel()
	holdKeyed(t, writeKeyedByName(t))
}

// A boot stopped after the rows took the id and before the directory did, or
// after the manifest was rewritten and before the rename, is finished by the
// next.
func TestAKeyingCutShortIsFinishedByTheNextBoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, stop := range []string{"after the rows", "after the manifest"} {
		t.Run(stop, func(t *testing.T) {
			t.Parallel()
			k := writeKeyedByName(t)
			svcI, err := OpenForTest(t, ctx, k.dsn, WithDataRoot(k.root), WithDirectoryReadOnly())
			if err != nil {
				t.Fatal(err)
			}
			s := svcI.(*service)
			s.lease = newWriterLease(s.maint, s.log)
			repo, err := s.repositoryByAuthority(ctx, k.name)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.keyRow(ctx, repo); err != nil {
				t.Fatal(err)
			}
			if stop == "after the manifest" {
				dir, err := changelogfile.RepoDir(k.root, k.name)
				if err != nil {
					t.Fatal(err)
				}
				m, err := changelogfile.ReadManifestToKey(dir)
				if err != nil {
					t.Fatal(err)
				}
				keyed, err := s.repositoryByAuthority(ctx, k.name)
				if err != nil {
					t.Fatal(err)
				}
				// The manifest rewritten and the spool not yet sealed again.
				m.Format, m.ID, m.DEK = changelogfile.ManifestFormat, keyed.ID, keyed.DEK
				if err := changelogfile.WriteManifestToKey(dir, m); err != nil {
					t.Fatal(err)
				}
			}
			s.lease.close()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			holdKeyed(t, k)
		})
	}
}

// A process that does not run the boot refuses to open a repository still
// keyed by its name rather than read it under the name.
func TestAReadOnlyOpenRefusesARepositoryKeyedByItsName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	k := writeKeyedByName(t)
	svcI, err := OpenForTest(t, ctx, k.dsn, WithDataRoot(k.root), WithDirectoryReadOnly())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svcI.Close() })
	if _, err := svcI.Dataset(ctx, k.name); !errors.Is(err, ErrRepositoryNotKeyed) {
		t.Fatalf("a read-only open of a repository keyed by its name: %v, want ErrRepositoryNotKeyed", err)
	}
	if _, err := svcI.(*service).VerifyRepository(ctx, k.name); !errors.Is(err, ErrRepositoryNotKeyed) {
		t.Fatalf("a read-only verify of a repository keyed by its name: %v, want ErrRepositoryNotKeyed", err)
	}
}

// repositoryColumnTables is every table of the schema with a repository
// column, but the control plane's own.
func repositoryColumnTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
		SELECT c.table_name FROM information_schema.columns c
		JOIN information_schema.tables tb ON tb.table_schema = c.table_schema AND tb.table_name = c.table_name
		WHERE c.table_schema = current_schema() AND c.column_name = 'repository' AND tb.table_type = 'BASE TABLE'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		out = append(out, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) < len(repositoryScopedTables) {
		t.Fatalf("the schema lists %d tables with a repository column, fewer than the %d the keying walks", len(out), len(repositoryScopedTables))
	}
	return out
}

// rawDBOf is a pool of the DSN's own role, which reads every repository's
// rows.
func rawDBOf(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestKeyingTime measures the rows half of the keying on a repository of
// SUBSTRATE_KEYING_ROWS synthetic records, each with a changelog line and a
// property manager row beside it, and logs the time; it runs only when the
// variable is set.
func TestKeyingTime(t *testing.T) {
	n := os.Getenv("SUBSTRATE_KEYING_ROWS")
	if n == "" {
		t.Skip("set SUBSTRATE_KEYING_ROWS to time the keying")
	}
	ctx := context.Background()
	k := writeKeyedByName(t)
	db := rawDBOf(t, k.dsn)
	started := time.Now()
	for _, q := range []string{
		`INSERT INTO records (repository, kind, id, title, props, fts)
		 SELECT $1, 'keying.example.com/notes/note', 'n' || g, 'Note ' || g,
		        jsonb_build_object('body', repeat('note text ', 30), 'n', g), to_tsvector('simple', 'note ' || g)
		 FROM generate_series(1, $2::int) g`,
		`INSERT INTO changelog (repository, seq, actor, op, record_id, kind, payload, principal, txn, hash)
		 SELECT $1, 1000000 + g, 'api', 'put', 'n' || g, 'keying.example.com/notes/note',
		        jsonb_build_object('fold', jsonb_build_array(jsonb_build_object('kind', 'record', 'delta', jsonb_build_object('set', jsonb_build_object('body', repeat('note text ', 30)))))),
		        '', 1000000 + g, sha256(g::text::bytea)
		 FROM generate_series(1, $2::int) g`,
		`INSERT INTO property_managers (repository, record_kind, record_id, property, actor, tier, updated_at, principal)
		 SELECT $1, 'keying.example.com/notes/note', 'n' || g, 'body', 'api', 'owner', now(), ''
		 FROM generate_series(1, $2::int) g`,
	} {
		if _, err := db.ExecContext(ctx, q, k.name, n); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	t.Logf("seeded %s records, changelog lines and manager rows in %s", n, time.Since(started).Round(time.Millisecond))
	svcI, err := OpenForTest(t, ctx, k.dsn, WithDataRoot(k.root), WithDirectoryReadOnly())
	if err != nil {
		t.Fatal(err)
	}
	s := svcI.(*service)
	s.lease = newWriterLease(s.maint, s.log)
	t.Cleanup(func() { s.lease.close(); _ = s.Close() })
	repo, err := s.repositoryByAuthority(ctx, k.name)
	if err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	if err := s.keyRow(ctx, repo); err != nil {
		t.Fatal(err)
	}
	t.Logf("keyed %s records' rows in %s", n, time.Since(started).Round(time.Millisecond))
}

// A directory a release before ids wrote, restored where another repository
// has since taken its name, is not that repository's: the boot refuses it
// and leaves it exactly as it was, its manifest and its data key included.
func TestADirectoryWhoseNameAnotherRepositoryTookIsRefusedUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	old := writeKeyedByName(t)
	dsn, root := MigratedDSN(t), t.TempDir()
	svcI, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcI.CreateRepository(ctx, old.name); err != nil {
		t.Fatal(err)
	}
	if err := svcI.Close(); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(old.root, changelogfile.RepositoriesDir, old.name), filepath.Join(root, changelogfile.RepositoriesDir, old.name)); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, changelogfile.RepositoriesDir, old.name, changelogfile.ManifestName)
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenForTest(t, ctx, dsn, WithDataRoot(root)); err == nil || !strings.Contains(err.Error(), "another repository took the name") {
		t.Fatalf("the boot over another repository's directory of the same name: %v", err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("the refused directory moved: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the refused directory's manifest changed:\nbefore %s\nafter  %s", before, after)
	}
}

// A second process over the same database holding the writer lease under a
// directory's name refuses this boot's conversion of it before anything is
// read or written: two boots never mint two ids for one directory.
func TestAKeyingWaitsForNoOtherWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeKeyedByName(t)
	// The same directory restored into an empty database: no row, so the
	// boot would mint an id for it.
	dsn, root := MigratedDSN(t), t.TempDir()
	if err := copyTree(filepath.Join(src.root, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir)); err != nil {
		t.Fatal(err)
	}
	maint, err := openMaint(dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = maint.Close() })
	other := newWriterLease(maint, slog.Default())
	t.Cleanup(other.close)
	if err := other.acquire(ctx, src.name); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, changelogfile.RepositoriesDir, src.name, changelogfile.ManifestName)
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenForTest(t, ctx, dsn, WithDataRoot(root)); !errors.Is(err, ErrRepositoryHasAnotherWriter) {
		t.Fatalf("a boot beside another writer of the directory: %v, want ErrRepositoryHasAnotherWriter", err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("the directory changed under another writer's lease: %v", err)
	}
	other.release(src.name)
	svcI, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatalf("the boot once the other writer let go: %v", err)
	}
	t.Cleanup(func() { _ = svcI.Close() })
	if _, err := svcI.Dataset(ctx, src.name); err != nil {
		t.Fatal(err)
	}
}

// A boot holds the name of a directory it gave an id until the boot check
// has published the repository under it. Between the rename and the import a
// second server over the same database, with a copy of the same directory in
// a data root of its own, is refused before it reads or writes its copy:
// it would otherwise find no row yet and mint a second id.
func TestAKeyingHoldsTheNameUntilTheRepositoryIsPublished(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeKeyedByName(t)
	dsn, rootA, rootB := MigratedDSN(t), t.TempDir(), t.TempDir()
	for _, root := range []string{rootA, rootB} {
		if err := copyTree(filepath.Join(src.root, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir)); err != nil {
			t.Fatal(err)
		}
	}
	reached, resume := make(chan struct{}), make(chan struct{})
	type opened struct {
		svc substrate.Service
		err error
	}
	done := make(chan opened, 1)
	go func() {
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(rootA), WithTestImportFault(0, func(stage string) error {
			if stage == keyingAfterRename {
				close(reached)
				<-resume
			}
			return nil
		}))
		done <- opened{svc, err}
	}()
	select {
	case <-reached:
	case o := <-done:
		t.Fatalf("the first boot ended before its directory took its id: %v", o.err)
	case <-time.After(time.Minute):
		t.Fatal("the first boot never renamed its directory")
	}
	manifest := filepath.Join(rootB, changelogfile.RepositoriesDir, src.name, changelogfile.ManifestName)
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = OpenForTest(t, ctx, dsn, WithDataRoot(rootB))
	after, rerr := os.ReadFile(manifest)
	close(resume)
	if !errors.Is(err, ErrRepositoryHasAnotherWriter) {
		t.Errorf("a second boot while the first had not published the repository: %v, want ErrRepositoryHasAnotherWriter", err)
	}
	if rerr != nil || !bytes.Equal(before, after) {
		t.Errorf("the second boot changed its copy of the directory: %v", rerr)
	}
	o := <-done
	if o.err != nil {
		t.Fatalf("the first boot: %v", o.err)
	}
	t.Cleanup(func() { _ = o.svc.Close() })
	if _, err := o.svc.Dataset(ctx, src.name); err != nil {
		t.Fatal(err)
	}
}

// A boot that gave a directory its id and died before publishing the
// repository leaves the directory under its id. The boot that resumes it
// holds the name again, from before it reads whether the name is free until
// the repository is published, so a second server over the same database
// with a copy still keyed by the name is refused before it reads or writes
// its copy, rather than finding no row and minting a second id.
func TestAResumedImportHoldsTheNameUntilTheRepositoryIsPublished(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeKeyedByName(t)
	dsn, rootA, rootB := MigratedDSN(t), t.TempDir(), t.TempDir()
	for _, root := range []string{rootA, rootB} {
		if err := copyTree(filepath.Join(src.root, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir)); err != nil {
			t.Fatal(err)
		}
	}
	died := errors.New("the process died after the rename")
	if _, err := OpenForTest(t, ctx, dsn, WithDataRoot(rootA), WithTestImportFault(0, func(stage string) error {
		if stage == keyingAfterRename {
			return died
		}
		return nil
	})); !errors.Is(err, died) {
		t.Fatalf("the first boot did not die after its rename: %v", err)
	}
	reached, resume := make(chan struct{}), make(chan struct{})
	type opened struct {
		svc substrate.Service
		err error
	}
	done := make(chan opened, 1)
	go func() {
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(rootA), WithTestImportFault(0, func(stage string) error {
			if stage == importBeforeRow {
				close(reached)
				<-resume
			}
			return nil
		}))
		done <- opened{svc, err}
	}()
	select {
	case <-reached:
	case o := <-done:
		t.Fatalf("the restarted boot ended before it imported: %v", o.err)
	case <-time.After(time.Minute):
		t.Fatal("the restarted boot never reached its import")
	}
	manifest := filepath.Join(rootB, changelogfile.RepositoriesDir, src.name, changelogfile.ManifestName)
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = OpenForTest(t, ctx, dsn, WithDataRoot(rootB))
	after, rerr := os.ReadFile(manifest)
	close(resume)
	if !errors.Is(err, ErrRepositoryHasAnotherWriter) {
		t.Errorf("a second boot while the restarted one had not published the repository: %v, want ErrRepositoryHasAnotherWriter", err)
	}
	if rerr != nil || !bytes.Equal(before, after) {
		t.Errorf("the second boot changed its copy of the directory: %v", rerr)
	}
	o := <-done
	if o.err != nil {
		t.Fatalf("the restarted boot: %v", o.err)
	}
	t.Cleanup(func() { _ = o.svc.Close() })
	if _, err := o.svc.Dataset(ctx, src.name); err != nil {
		t.Fatal(err)
	}
}

// A directory a crash left with its manifest rewritten and its spool not yet
// sealed again, beside another directory already under its id, is refused
// before either is touched: its manifest and its spooled request stay as they
// were.
func TestAResumedKeyingBesideItsDuplicateIsRefusedUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	k := writeKeyedByName(t)
	svcI, err := OpenForTest(t, ctx, k.dsn, WithDataRoot(k.root), WithDirectoryReadOnly())
	if err != nil {
		t.Fatal(err)
	}
	s := svcI.(*service)
	s.lease = newWriterLease(s.maint, s.log)
	repo, err := s.repositoryByAuthority(ctx, k.name)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.keyRow(ctx, repo); err != nil {
		t.Fatal(err)
	}
	keyed, err := s.repositoryByAuthority(ctx, k.name)
	if err != nil {
		t.Fatal(err)
	}
	s.lease.close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(k.root, changelogfile.RepositoriesDir, k.name)
	m, err := changelogfile.ReadManifestToKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Format, m.ID, m.DEK = changelogfile.ManifestFormat, keyed.ID, keyed.DEK
	if err := changelogfile.WriteManifestToKey(dir, m); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(dir, filepath.Join(k.root, changelogfile.RepositoriesDir, keyed.ID)); err != nil {
		t.Fatal(err)
	}
	read := func() ([]byte, []byte) {
		t.Helper()
		manifest, err := os.ReadFile(filepath.Join(dir, changelogfile.ManifestName))
		if err != nil {
			t.Fatal(err)
		}
		spool, err := changelogfile.ReadSpoolFile(dir, k.spool)
		if err != nil {
			t.Fatal(err)
		}
		return manifest, spool
	}
	manifest, spool := read()
	if _, err := OpenForTest(t, ctx, k.dsn, WithDataRoot(k.root)); err == nil || !strings.Contains(err.Error(), "are this repository's directory") {
		t.Fatalf("a resumed keying beside its duplicate: %v", err)
	}
	manifestAfter, spoolAfter := read()
	if !bytes.Equal(manifest, manifestAfter) || !bytes.Equal(spool, spoolAfter) {
		t.Fatal("the refused directory's manifest or spooled request changed")
	}
}

// keyingFixtureValues are the values a table's checks hold a column to,
// where a value by type alone would not pass them.
var keyingFixtureValues = map[string]string{
	"observed_rollups.tier":       "3600",
	"observed_segments.available": "0",
}

// fillEveryRepositoryTable writes one row for repository into every table
// the schema gives a repository column that holds none of its rows: a value
// for every column that needs one, by its type. A table it cannot fill so
// fails the test, naming it, so a table added to the schema is a failure
// here until the fixture covers it, never a table the keying test passes
// over.
func fillEveryRepositoryTable(t *testing.T, db *sql.DB, repository string) {
	t.Helper()
	ctx := context.Background()
	for _, table := range repositoryColumnTables(t, db) {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE repository = $1`, repository).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			continue
		}
		rows, err := db.QueryContext(ctx, `
			SELECT column_name, data_type, udt_name
			FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1
			  AND is_nullable = 'NO' AND column_default IS NULL AND is_identity = 'NO' AND is_generated = 'NEVER'
			ORDER BY ordinal_position`, table)
		if err != nil {
			t.Fatal(err)
		}
		cols, vals := []string{"repository"}, []string{"$1"}
		for rows.Next() {
			var name, dataType, udt string
			if err := rows.Scan(&name, &dataType, &udt); err != nil {
				t.Fatal(err)
			}
			if name == "repository" {
				continue
			}
			v, ok := keyingFixtureValues[table+"."+name]
			if ok {
				cols, vals = append(cols, name), append(vals, v)
				continue
			}
			v, ok = map[string]string{
				"text": "'keyed-" + name + "'", "character varying": "'keyed-" + name + "'",
				"integer": "1", "bigint": "1", "smallint": "1", "numeric": "1", "double precision": "1", "real": "1",
				"boolean": "false", "timestamp with time zone": "now()", "timestamp without time zone": "now()", "date": "current_date",
				"jsonb": "'{}'", "json": "'{}'", "bytea": "'\\x00'", "uuid": "gen_random_uuid()", "ARRAY": "'{}'",
			}[dataType]
			if !ok && udt == "vector" {
				v, ok = "'[0]'", true
			}
			if !ok {
				t.Fatalf("the keying fixture leaves %s empty and cannot fill its column %s (%s %s): add a row for %s to fillEveryRepositoryTable", table, name, dataType, udt, table)
			}
			cols, vals = append(cols, name), append(vals, v)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		if _, err := db.ExecContext(ctx, `INSERT INTO `+table+` (`+strings.Join(cols, ", ")+`) VALUES (`+strings.Join(vals, ", ")+`)`, repository); err != nil {
			t.Fatalf("the keying fixture leaves %s empty and cannot write a row of it (%v): add one to fillEveryRepositoryTable", table, err)
		}
	}
}
