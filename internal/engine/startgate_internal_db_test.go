package engine

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// THE START'S ONE GATE (setaside.go through): every pass of the start that
// touches a repository goes through it, so a repository one pass set aside is
// changed by no later pass, and a failure of one repository's own sets it
// aside rather than stopping the start for the others.

// gateWorld is two repositories on one database and data root, A the one
// damaged and B the healthy neighbor, each holding a contact.
type gateWorld struct {
	dsn, root string
	a, b      Repository
}

func newGateWorld(t *testing.T) gateWorld {
	t.Helper()
	ctx := context.Background()
	w := gateWorld{dsn: MigratedDSN(t), root: t.TempDir()}
	svc, err := OpenForTest(t, ctx, w.dsn, WithDataRoot(w.root))
	if err != nil {
		t.Fatal(err)
	}
	s := svc.(*service)
	for i, name := range []string{testdb.Repository(t), "bob.example.com"} {
		if _, err := s.CreateRepository(ctx, name); err != nil {
			t.Fatal(err)
		}
		d, err := s.Dataset(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		ds := d.(*dataset)
		mustPutInternal(t, ds, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", ID: fid("ada"), Properties: map[string]any{"name": "Ada"}})
		repo, err := s.repositoryByID(ctx, ds.info.ID)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			w.a = repo
		} else {
			w.b = repo
		}
	}
	_ = svc.Close()
	return w
}

// damageWrap flips the last byte of A's data key wrap in its row: a wrap
// that names this host's key and does not open, which the credential check
// sets aside.
func (w gateWorld) damageWrap(t *testing.T) {
	t.Helper()
	if _, err := rawDBOf(t, w.dsn).Exec(`UPDATE repositories SET dek = set_byte(dek, length(dek) - 1, (get_byte(dek, length(dek) - 1) + 1) % 256) WHERE id = $1`, w.a.ID); err != nil {
		t.Fatal(err)
	}
}

// adoptedLeftovers leaves under A's directory what a boot that adopted a 0.x
// directory's history and stopped before removing its files leaves: the
// manifest set aside and a segment file.
func (w gateWorld) adoptedLeftovers(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(w.root, changelogfile.RepositoriesDir, w.a.ID)
	if err := os.MkdirAll(changelogfile.ChangelogDir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(dir, changelogfile.AdoptedManifestName):                    "{}",
		filepath.Join(changelogfile.ChangelogDir(dir), "000000000000001.ndjson"): "a line\n",
		filepath.Join(changelogfile.ChangelogDir(dir), "000000000000002.ndjson"): "another\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(changelogfile.ChangelogDir(dir), "000000000000001.ndjson")
}

func (w gateWorld) start(t *testing.T, opts ...Option) *service {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), w.dsn, append([]Option{WithDataRoot(w.root)}, opts...)...)
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc.(*service)
}

// wantAsideAndNeighborServing holds the start to A set aside and B served.
func (w gateWorld) wantAsideAndNeighborServing(t *testing.T, s *service) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Dataset(ctx, w.a.Authority); !errors.Is(err, substrate.ErrSetAside) {
		t.Fatalf("the damaged repository opens: %v", err)
	}
	b, err := s.Dataset(ctx, w.b.Authority)
	if err != nil {
		t.Fatalf("the healthy neighbor: %v", err)
	}
	if _, err := b.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil {
		t.Fatalf("the neighbor's record: %v", err)
	}
}

// The cleanup of an adopted history fails for one repository (the seam
// stands in for a file it cannot remove): that repository is set aside at
// the boot check, and its healthy neighbor is served.
func TestACleanupThatFailsSetsItsRepositoryAsideAndTheNeighborServes(t *testing.T) {
	t.Parallel()
	w := newGateWorld(t)
	w.adoptedLeftovers(t)
	s := w.start(t, WithTestImportFault(0, func(stage string) error {
		if stage == changelogfile.RemoveHistoryPartly {
			return errors.New("permission denied")
		}
		return nil
	}))
	w.wantAsideAndNeighborServing(t, s)
	if r := s.setAsideOf(w.a.ID, ""); r == nil || r.stage != setAsideAtCheck {
		t.Fatalf("the repository is set aside as %+v, want at the boot check", r)
	}
}

// A repository the credential check set aside is changed by no later pass:
// the cleanup of its adopted history leaves its files where they are.
func TestARepositorySetAsideIsNotCleanedUp(t *testing.T) {
	t.Parallel()
	w := newGateWorld(t)
	segment := w.adoptedLeftovers(t)
	w.damageWrap(t)
	s := w.start(t)
	w.wantAsideAndNeighborServing(t, s)
	if _, err := os.Stat(segment); err != nil {
		t.Fatalf("the set-aside repository's adopted history was removed: %v", err)
	}
}

// A take recorded for a repository that the credential check sets aside is
// not carried out: nothing of the repository is removed, the take stays
// recorded and waits, and the neighbor serves.
func TestATakeOfARepositorySetAsideWaits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newGateWorld(t)
	op := w.start(t)
	if res, err := op.ResolveArchive(ctx, w.a.Authority, false); err != nil || res.Take == "" {
		t.Fatalf("the take = %+v, %v, want recorded", res, err)
	}
	_ = op.Close()
	before := repositoryState(t, w.dsn, w.a.ID)
	w.damageWrap(t)

	s := w.start(t)
	w.wantAsideAndNeighborServing(t, s)
	if after := repositoryState(t, w.dsn, w.a.ID); after != before {
		t.Fatalf("the set-aside repository changed: %s before, %s after", before, after)
	}
}

// unlistedStore is an archive whose listing does not answer.
type unlistedStore struct{ archive.Store }

func (unlistedStore) List(context.Context, string) ([]archive.Object, error) {
	return nil, errors.New("the store did not answer")
}

func (unlistedStore) String() string { return "unlisted" }

// The operator's list keeps what the database says when the archive does
// not answer: every row with its state, and the error says the archive did
// not answer rather than the list being refused whole.
func TestTheOperatorsListShowsTheRowsWhenTheArchiveDoesNotAnswer(t *testing.T) {
	t.Parallel()
	w := newGateWorld(t)
	states, err := RepositoryStates(context.Background(), rawDBOf(t, w.dsn), w.root, unlistedStore{})
	if !errors.Is(err, ErrArchiveUnlisted) {
		t.Fatalf("the list's error = %v, want the archive's unanswered listing", err)
	}
	seen := map[string]bool{}
	for _, st := range states {
		seen[st.ID] = true
	}
	if !seen[w.a.ID] || !seen[w.b.ID] {
		t.Fatalf("the list holds %+v, want both rows", states)
	}
}

// downStore is an archive's store that answers nothing.
type downStore struct{}

var errStoreDown = errors.New("the archive's store refuses the connection")

func (downStore) Put(context.Context, string, io.ReadSeeker, int64) error { return errStoreDown }
func (downStore) Get(context.Context, string) (io.ReadCloser, error)      { return nil, errStoreDown }

func (downStore) List(context.Context, string) ([]archive.Object, error) { return nil, errStoreDown }
func (downStore) Delete(context.Context, string) error                   { return errStoreDown }
func (downStore) String() string                                         { return "down" }

// A store that does not answer is nobody's damage. A take removed its
// repository's rows and its staged copy went before the restore, which finds
// the store down when it stages the copy again: the repository is not set
// aside and nothing of it is written, its take stays recorded, the neighbor
// serves, and the next start, the store answering, restores it.
func TestAStoreThatDoesNotAnswerSetsNoRepositoryAside(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newGateWorld(t)
	op := w.start(t)
	if res, err := op.ResolveArchive(ctx, w.a.Authority, false); err != nil || res.Take == "" {
		t.Fatalf("the take = %+v, %v, want recorded", res, err)
	}
	_ = op.Close()
	staged := filepath.Join(w.root, takeRootDir, w.a.ID)
	s := w.start(t, WithArchive(downStore{}), WithTestImportFault(0, func(stage string) error {
		if stage == takeAfterStaged {
			return os.RemoveAll(staged)
		}
		return nil
	}))
	if r := s.setAsideOf(w.a.ID, ""); r != nil {
		t.Fatalf("the store's failure set the repository aside: %+v", r)
	}
	if _, err := s.Dataset(ctx, w.a.Authority); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the repository the take removed opens as %v, want not found until it is restored", err)
	}
	if _, err := s.Dataset(ctx, w.b.Authority); err != nil {
		t.Fatalf("the neighbor: %v", err)
	}
	var takes int
	if err := rawDBOf(t, w.dsn).QueryRow(`SELECT count(*) FROM archive_takes WHERE repository_id = $1`, w.a.ID).Scan(&takes); err != nil || takes != 1 {
		t.Fatalf("the take is recorded %d times (%v), want once", takes, err)
	}
	_ = s.Close()

	again := w.start(t)
	d, err := again.Dataset(ctx, w.a.Authority)
	if err != nil {
		t.Fatalf("the repository at the start after, the store answering: %v", err)
	}
	if _, err := d.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil {
		t.Fatalf("its record: %v", err)
	}
}

// A failure of the store a pass meets once it has written part of a
// repository is recorded as the store's: the repository is set aside at the
// archive store's stage, never at a stage that tells the operator to repair
// it. A store's own timeout is no failure of the server's either.
func TestAStoreFailureIsRecordedAsTheStores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newGateWorld(t)
	s := w.start(t)
	if err := s.through(ctx, rowPass(w.a, setAsideAtCheck), func() error {
		return markStoreError(fmt.Errorf("piece: %w", context.Canceled))
	}); err != nil {
		t.Fatalf("the store's timeout stopped the start: %v", err)
	}
	if r := s.setAsideOf(w.a.ID, ""); r == nil || r.stage != setAsideAtStore {
		t.Fatalf("the repository is set aside as %+v, want at the archive store's stage", r)
	}
}

// A directory the boot check cannot read is its repository's failure, never
// a directory with no history whose row then passes as current: the read
// fails (the seam stands in for a directory the server may not read), the
// repository is set aside at the boot check, and its neighbor serves.
func TestADirectoryTheBootCannotReadSetsItsRepositoryAside(t *testing.T) {
	t.Parallel()
	w := newGateWorld(t)
	if err := os.RemoveAll(filepath.Join(w.root, changelogfile.RepositoriesDir, w.b.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(changelogfile.BlobsDir(filepath.Join(w.root, changelogfile.RepositoriesDir, w.a.ID)), 0o700); err != nil {
		t.Fatal(err)
	}
	s := w.start(t, WithTestImportFault(0, func(stage string) error {
		if stage == discoverStage {
			return errors.New("permission denied")
		}
		return nil
	}))
	w.wantAsideAndNeighborServing(t, s)
	if r := s.setAsideOf(w.a.ID, ""); r == nil || r.stage != setAsideAtCheck {
		t.Fatalf("the repository is set aside as %+v, want at the boot check", r)
	}
}

// A deletion recorded for a repository the credential check sets aside
// waits: nothing of it is erased and its deletion_began_at stays, and the
// start after its repair finishes the deletion.
func TestADeletionOfARepositorySetAsideWaits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newGateWorld(t)
	db := rawDBOf(t, w.dsn)
	var dek []byte
	if err := db.QueryRow(`SELECT dek FROM repositories WHERE id = $1`, w.a.ID).Scan(&dek); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE repositories SET deletion_began_at = now() WHERE id = $1`, w.a.ID); err != nil {
		t.Fatal(err)
	}
	before := repositoryState(t, w.dsn, w.a.ID)
	w.damageWrap(t)
	s := w.start(t)
	if r := s.setAsideOf(w.a.ID, ""); r == nil || r.stage != setAsideAtKey {
		t.Fatalf("the repository is set aside as %+v, want at the credential check", r)
	}
	if after := repositoryState(t, w.dsn, w.a.ID); after != before {
		t.Fatalf("the set-aside repository's deletion went on: %s before, %s after", before, after)
	}
	var began sql.NullTime
	if err := db.QueryRow(`SELECT deletion_began_at FROM repositories WHERE id = $1`, w.a.ID).Scan(&began); err != nil || !began.Valid {
		t.Fatalf("the deletion's mark: %v, %v", began, err)
	}
	_ = s.Close()

	if _, err := db.Exec(`UPDATE repositories SET dek = $1 WHERE id = $2`, dek, w.a.ID); err != nil {
		t.Fatal(err)
	}
	again := w.start(t)
	if _, err := again.repositoryByID(ctx, w.a.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the repository after its repair: %v, want deleted", err)
	}
	if _, err := again.Dataset(ctx, w.b.Authority); err != nil {
		t.Fatalf("the neighbor: %v", err)
	}
}

// eraseRowsOf removes every row of repository id, as a database restored
// without it holds none.
func eraseRowsOf(t *testing.T, dsn, id string) {
	t.Helper()
	db := rawDBOf(t, dsn)
	for _, table := range repositoryScopedTables {
		if _, err := db.Exec(`DELETE FROM `+table+` WHERE repository = $1`, id); err != nil {
			t.Fatalf("erase %s's rows of %s: %v", id, table, err)
		}
	}
	if _, err := db.Exec(`DELETE FROM repositories WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}

// A 0.X DIRECTORY WITH NO ROW WHOSE HISTORY DOES NOT VERIFY. A's repository
// comes back as the directory a release before the archive kept, into a
// database that holds none of it, and one of its lines is not the line its
// checksum names (a flipped byte on the disk). The import refuses it, and the
// start sets it aside at the import and serves B; its name stays taken. The
// line put back is the repair: the next start imports A.
func TestA0xDirectoryWhoseLineDoesNotVerifyIsSetAsideAtTheImport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newGateWorld(t)
	op := w.start(t)
	d, err := op.Dataset(ctx, w.a.Authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	_ = op.Close()
	archiveRoot := filepath.Join(t.TempDir(), "archive")
	if err := os.Rename(filepath.Join(w.root, "archive"), archiveRoot); err != nil {
		t.Fatal(err)
	}
	eraseRowsOf(t, w.dsn, w.a.ID)
	if err := os.RemoveAll(filepath.Join(w.root, changelogfile.RepositoriesDir, w.a.ID)); err != nil {
		t.Fatal(err)
	}
	dir := legacyDirFromArchive(t, archiveRoot, w.root, w.a.ID, w.a.ID)
	segments, err := filepath.Glob(filepath.Join(changelogfile.ChangelogDir(dir), "*.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	var damaged string
	var whole []byte
	for _, path := range segments {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if i := bytes.Index(raw, []byte(`"Ada"`)); i >= 0 {
			damaged, whole = path, raw
			flipped := bytes.Clone(raw)
			flipped[i+3] = 'b'
			if err := os.WriteFile(path, flipped, 0o600); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if damaged == "" {
		t.Fatal("no line of the directory holds the contact's name")
	}

	s := w.start(t)
	r := s.setAsideOf(w.a.ID, "")
	if r == nil || r.stage != setAsideAtImport {
		t.Fatalf("the directory is set aside as %+v, want at the import", r)
	}
	b, err := s.Dataset(ctx, w.b.Authority)
	if err != nil {
		t.Fatalf("the healthy neighbor: %v", err)
	}
	if _, err := b.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil {
		t.Fatalf("the neighbor's record: %v", err)
	}
	if _, err := s.Register(ctx, substrate.RegisterInput{Repository: w.a.Authority, Password: "correct-horse-battery-staple", SkipTOTP: true}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a registration of the set-aside directory's name = %v, want refused as taken", err)
	}
	_ = s.Close()

	if err := os.WriteFile(damaged, whole, 0o600); err != nil {
		t.Fatal(err)
	}
	s = w.start(t)
	if r := s.setAsideOf(w.a.ID, ""); r != nil {
		t.Fatalf("the repaired directory is still set aside: %+v", r)
	}
	a, err := s.Dataset(ctx, w.a.Authority)
	if err != nil {
		t.Fatalf("the repaired directory, imported: %v", err)
	}
	if rec, err := a.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil || rec.Properties["name"] != "Ada" {
		t.Fatalf("the imported contact = %+v, %v", rec, err)
	}
}

// A TAKE WHOSE ERASE THE DATABASE REFUSES. A's operator chose to take its
// archive, and the start that carries the take out removes A's directory and
// then cannot remove its rows: the database refuses the erase. The trigger
// on `records` stands for any refusal of the database's own (a lock, a
// constraint, a grant an operator changed). The start sets A aside at the
// take and serves B; with the refusal gone, the next start finishes the take
// and restores A from the archive it chose.
func TestATakeWhoseEraseTheDatabaseRefusesIsSetAsideAtTheTake(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newGateWorld(t)
	op := w.start(t)
	if res, err := op.ResolveArchive(ctx, w.a.Authority, false); err != nil || res.Take == "" {
		t.Fatalf("the take = %+v, %v, want recorded", res, err)
	}
	_ = op.Close()
	db := rawDBOf(t, w.dsn)
	if _, err := db.Exec(`
		CREATE FUNCTION refuse_the_take() RETURNS trigger LANGUAGE plpgsql AS $fn$
		BEGIN
			IF OLD.repository = '` + w.a.ID + `' THEN
				RAISE EXCEPTION 'the database refuses to erase this repository';
			END IF;
			RETURN OLD;
		END $fn$;
		CREATE TRIGGER refuse_the_take BEFORE DELETE ON records FOR EACH ROW EXECUTE FUNCTION refuse_the_take();`); err != nil {
		t.Fatal(err)
	}

	s := w.start(t)
	r := s.setAsideOf(w.a.ID, "")
	if r == nil || r.stage != setAsideAtTake {
		t.Fatalf("the repository is set aside as %+v, want at the take", r)
	}
	w.wantAsideAndNeighborServing(t, s)
	_ = s.Close()

	if _, err := db.Exec(`DROP TRIGGER refuse_the_take ON records; DROP FUNCTION refuse_the_take()`); err != nil {
		t.Fatal(err)
	}
	s = w.start(t)
	if r := s.setAsideOf(w.a.ID, ""); r != nil {
		t.Fatalf("the repository is still set aside after the refusal went: %+v", r)
	}
	a, err := s.Dataset(ctx, w.a.Authority)
	if err != nil {
		t.Fatalf("the repository, restored from the archive it took: %v", err)
	}
	if _, err := a.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil {
		t.Fatalf("the restored contact: %v", err)
	}
}

// stopsAfterPieces is the data root's archive store, which stops answering
// once it has handed out after pieces of history: a store gone down in the
// middle of a restore. It keeps the store's name, since it is the same
// archive.
type stopsAfterPieces struct {
	archive.Store
	mu            sync.Mutex
	pieces, after int
}

func (s *stopsAfterPieces) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.Contains(key, "/"+changelogfile.ChangelogSubdir+"/") {
		s.mu.Lock()
		s.pieces++
		n := s.pieces
		s.mu.Unlock()
		if n > s.after {
			return nil, errStoreDown
		}
	}
	return s.Store.Get(ctx, key)
}

// A RESTORE WHOSE STORE STOPS ANSWERING PART-WAY. A's rows are gone (a
// database restored without it) and its archive holds its history in several
// pieces; the store answers the first and then not, once the restore has
// written some of A. The start sets A aside at the archive store's stage,
// which names the store and tells the operator nothing of A is damaged, and
// serves B; with the store answering, the next start restores A.
func TestARestoreWhoseStoreStopsAnsweringIsSetAsideAtTheArchiveStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newGateWorld(t)
	op := w.start(t, WithArchivePieceBytes(1))
	d, err := op.Dataset(ctx, w.a.Authority)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	for _, name := range []string{"Grace", "Edsger", "Barbara"} {
		mustPutInternal(t, ds, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", ID: fid(name), Properties: map[string]any{"name": name}})
	}
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	_ = op.Close()
	eraseRowsOf(t, w.dsn, w.a.ID)
	dir, err := archive.NewDir(filepath.Join(w.root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	pieces, err := dir.List(ctx, changelogfile.RepositoryPrefix(w.a.ID))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, o := range pieces {
		if strings.Contains(o.Key, "/"+changelogfile.ChangelogSubdir+"/") {
			n++
		}
	}
	if n < 3 {
		t.Fatalf("the archive holds %d pieces of A's history, want several", n)
	}

	store := &stopsAfterPieces{Store: dir, after: 1}
	s := w.start(t, WithArchive(store))
	r := s.setAsideOf(w.a.ID, "")
	if r == nil || r.stage != setAsideAtStore {
		t.Fatalf("the repository is set aside as %+v after %d pieces, want at the archive store", r, store.pieces)
	}
	if _, err := s.Dataset(ctx, w.b.Authority); err != nil {
		t.Fatalf("the healthy neighbor: %v", err)
	}
	_ = s.Close()

	s = w.start(t)
	a, err := s.Dataset(ctx, w.a.Authority)
	if err != nil {
		t.Fatalf("the repository, restored with the store answering: %v", err)
	}
	if _, err := a.Get(ctx, "substrate.reamde.dev/core/contact", fid("Barbara")); err != nil {
		t.Fatalf("the restored contact: %v", err)
	}
}
