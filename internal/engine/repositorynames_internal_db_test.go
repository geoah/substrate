package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Two renames whose sign-ins both read the repository under one name: the
// first moves it away, the second, after the first committed, moves it back.
// The second is a change too, read off the locked row, so it fences the
// repository's names, and the open dataset answers under the name it has,
// with the name it left as a former one (decision record 0287).
func TestARenameBackFromAStaleSignInRefreshesTheOpenDataset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	svc := ds.svc
	a, b := ds.name(), testdb.RepositoryLabel(t)+"-b.example.com"
	signedIn, err := svc.repositoryByID(ctx, ds.info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.rename(ctx, signedIn, b, renameBy{}); err != nil {
		t.Fatalf("the first rename: %v", err)
	}
	if _, err := svc.rename(ctx, signedIn, a, renameBy{}); err != nil {
		t.Fatalf("the second rename, back: %v", err)
	}
	open, err := svc.Dataset(ctx, ds.info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := open.Repository().Authority; got != a {
		t.Errorf("the open dataset answers under %s, want %s, the name the row has", got, a)
	}
	if got := open.(*dataset).names(); !slices.Equal(got, []string{a, b}) {
		t.Errorf("the open dataset answers to %v, want %s and its former %s", got, a, b)
	}
}

// An open that read the repository's row before a rename committed, and
// publishes after it, serves the new name: the dataset it publishes holds no
// name, and asks the names cache when it needs one. The rename's request,
// canceled once it returned, changes nothing: nothing after its commit waits
// on the open or on the request.
func TestARenameCancelledBesideAnOpenLeavesNoDatasetOfTheOldName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	svc, id := ds.svc, ds.info.ID
	b := testdb.RepositoryLabel(t) + "-b.example.com"
	release, err := svc.claimErase(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	release()

	read, hold := make(chan struct{}), make(chan struct{})
	var reading sync.Once
	svc.testOpenRead = func(r string) {
		if r == id {
			reading.Do(func() {
				close(read)
				<-hold
			})
		}
	}
	type opening struct {
		ds  substrate.Dataset
		err error
	}
	opened := make(chan opening, 1)
	go func() {
		d, err := svc.Dataset(ctx, id)
		opened <- opening{d, err}
	}()
	<-read
	signedIn, err := svc.repositoryByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithCancel(ctx)
	if _, err := svc.rename(rctx, signedIn, b, renameBy{}); err != nil {
		t.Fatalf("the rename beside a paused open: %v", err)
	}
	cancel()
	close(hold)
	got := <-opened
	if got.err != nil {
		t.Fatalf("the open beside the rename: %v", got.err)
	}
	if name := got.ds.Repository().Authority; name != b {
		t.Errorf("the open that read the row before the rename serves %s, want %s", name, b)
	}
	again, err := svc.Dataset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if name := again.Repository().Authority; name != b || again != got.ds {
		t.Errorf("the next request finds the repository under %s (the same dataset: %v), want %s, the one the open published", name, again == got.ds, b)
	}
}

// WHAT FOLLOWS A RENAME'S COMMIT NEVER WAITS. As many renames as the
// maintenance pool has free connections, each beside an open of its
// repository paused after reading the row, all finish while the opens are
// still paused; each open, released, needs the pool to go on, finds it free,
// and serves the new name. When the commit was followed by a close that
// waited for the open, each rename held its registration lock's connection
// while it waited, and the opens waited for a connection: the pool was spent.
func TestRenamesBesideOpensThatNeedThePoolAllFinish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newRaceDataset(t).svc
	// The writer lease pins one connection for the life of the process.
	free := svc.maint.Stats().MaxOpenConnections - 1
	label := testdb.RepositoryLabel(t)
	ids, names := make([]string, free), make([]string, free)
	read := map[string]chan struct{}{}
	once := map[string]*sync.Once{}
	for i := range free {
		info, err := svc.CreateRepository(ctx, fmt.Sprintf("%s-r%d.example.com", label, i))
		if err != nil {
			t.Fatal(err)
		}
		release, err := svc.claimErase(ctx, info.ID)
		if err != nil {
			t.Fatal(err)
		}
		release()
		ids[i], names[i] = info.ID, fmt.Sprintf("%s-n%d.example.com", label, i)
		read[info.ID], once[info.ID] = make(chan struct{}), &sync.Once{}
	}
	hold := make(chan struct{})
	svc.testOpenRead = func(r string) {
		if o, ok := once[r]; ok {
			o.Do(func() {
				close(read[r])
				<-hold
			})
		}
	}
	type opening struct {
		i   int
		ds  substrate.Dataset
		err error
	}
	opened := make(chan opening, free)
	for i, id := range ids {
		go func() {
			d, err := svc.Dataset(ctx, id)
			opened <- opening{i, d, err}
		}()
		<-read[id]
	}
	renamed := make(chan error, free)
	for i, id := range ids {
		go func() {
			signedIn, err := svc.repositoryByID(ctx, id)
			if err == nil {
				_, err = svc.rename(ctx, signedIn, names[i], renameBy{})
			}
			renamed <- err
		}()
	}
	deadline := time.After(60 * time.Second)
	for range free {
		select {
		case err := <-renamed:
			if err != nil {
				t.Fatalf("a rename beside a paused open: %v", err)
			}
		case <-deadline:
			close(hold)
			t.Fatal("a rename is still waiting after its commit while the opens beside it are paused")
		}
	}
	close(hold)
	for range free {
		select {
		case got := <-opened:
			if got.err != nil {
				t.Fatalf("open %s: %v", ids[got.i], got.err)
			}
			if name := got.ds.Repository().Authority; name != names[got.i] {
				t.Errorf("the open of %s serves %s, want %s", ids[got.i], name, names[got.i])
			}
		case <-time.After(60 * time.Second):
			t.Fatal("an open is still waiting for the pool after every rename returned")
		}
	}
}

// interleaved is the maint pool, running after between the statements a
// lookup sends.
type interleaved struct {
	dbx
	statements int
	after      func()
}

func (q *interleaved) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	row := q.dbx.QueryRowContext(ctx, query, args...)
	q.statements++
	if q.statements == 1 && q.after != nil {
		// Scanned before the hook runs, so the statement's snapshot is
		// taken before the rename commits; the row comes back with it.
		q.after()
	}
	return row
}

// A name inside its hold resolves throughout a rename back: the current name
// and the former one are read in one statement, one snapshot, so a rename
// back that commits after it (the former row goes, the name is current again)
// cannot leave a lookup that read "no current holder" and then "no former
// row" answering a name valid at every moment as nobody's.
func TestALookupReadsTheNameAndTheFormerNameInOneSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	svc, id, a := ds.svc, ds.info.ID, ds.name()
	b := testdb.RepositoryLabel(t) + "-b.example.com"
	signedIn, err := svc.repositoryByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.rename(ctx, signedIn, b, renameBy{}); err != nil {
		t.Fatal(err)
	}
	q := &interleaved{dbx: svc.maint, after: func() {
		if _, err := svc.rename(ctx, signedIn, a, renameBy{}); err != nil {
			t.Errorf("the rename back: %v", err)
		}
	}}
	got, err := svc.repositoryByNameOn(ctx, q, a)
	if err != nil || got.ID != id {
		t.Fatalf("%s during a rename back resolves to %+v (%v), want %s", a, got, err, id)
	}
	if q.statements != 1 {
		t.Errorf("the lookup sent %d statements, want one", q.statements)
	}
}

// A rename whose COMMIT's answer is lost keeps its fence until the database
// proves the transaction ended (namescache.go): the rename's caller hears an
// error, nothing caches a name meanwhile, and the open dataset reads the row,
// which says the new name. Once the names lock the transaction held is free,
// the fence lifts, the cache answers the new name, and the repository's own
// record follows it.
func TestARenameWhoseCommitAnswerIsLostKeepsItsFenceUntilProved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	svc, id := ds.svc, ds.info.ID
	b := testdb.RepositoryLabel(t) + "-b.example.com"
	holder, err := svc.maint.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	lost := errors.New("test: the COMMIT's answer was lost")
	svc.testNamesCommit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		// The transaction ended; the proof is kept from the waiter until the
		// test has looked at the fence.
		if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_lock(`+namesLockKeySQL+`)`, id); err != nil {
			t.Errorf("hold the names lock: %v", err)
		}
		return lost
	}
	signedIn, err := svc.repositoryByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.rename(ctx, signedIn, b, renameBy{}); !errors.Is(err, lost) {
		t.Fatalf("the rename whose answer was lost answered %v", err)
	}
	svc.testNamesCommit = nil
	if !svc.names.fenced(id) {
		t.Fatal("a COMMIT whose answer was lost lifted its fence")
	}
	for range 3 {
		if got := ds.names(); len(got) == 0 || got[0] != b {
			t.Fatalf("under the fence the open dataset answers %v, want %s, the row's", got, b)
		}
	}
	if _, ok, _ := svc.names.get(id); ok {
		t.Fatal("the cache answered under the fence")
	}
	if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_unlock(`+namesLockKeySQL+`)`, id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for svc.names.fenced(id) {
		if time.Now().After(deadline) {
			t.Fatal("the fence stands after the transaction it waited for ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := ds.Repository().Authority; got != b {
		t.Errorf("after the proof the repository answers %s, want %s", got, b)
	}
	for {
		rec, err := ds.Get(ctx, kindRepository, id)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Properties["authority"] == b {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the repository's record says %v after the proof, want %s", rec.Properties["authority"], b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A dataset open across a rename is the one served after it: nothing is
// closed, opened again or orphaned because a name changed. A request already
// running on it finishes on it, its next fold asks the cache and reads the
// new name, and the name it left still folds while it is held.
func TestADatasetOpenAcrossARenameKeepsServing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	svc, id, a := ds.svc, ds.info.ID, ds.name()
	b := testdb.RepositoryLabel(t) + "-b.example.com"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest("~/notes", 1),
		vocabulary.KindManifest("~/notes", map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatal(err)
	}
	// A request mid-way: its transaction began under the old name.
	inside, release := make(chan struct{}), make(chan struct{})
	wrote := make(chan error, 1)
	go func() {
		wrote <- ds.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
			close(inside)
			<-release
			_, err := tx.put(substrate.PutInput{Kind: b + "/notes/note", ID: vocabulary.DerivedMintedID("rename", "during"), Properties: map[string]any{"text": "across"}})
			return err
		})
	}()
	<-inside
	signedIn, err := svc.repositoryByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.rename(ctx, signedIn, b, renameBy{}); err != nil {
		t.Fatalf("the rename beside a running request: %v", err)
	}
	close(release)
	if err := <-wrote; err != nil {
		t.Fatalf("the request running across the rename: %v", err)
	}
	served, err := svc.Dataset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if served != ds {
		t.Fatal("the rename replaced the open dataset")
	}
	if err := ds.db.PingContext(ctx); err != nil {
		t.Fatalf("the dataset open across the rename was closed: %v", err)
	}
	for _, kind := range []string{"~/notes/note", b + "/notes/note", a + "/notes/note"} {
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: vocabulary.DerivedMintedID("rename", kind), Properties: map[string]any{"text": kind}}); err != nil {
			t.Errorf("a write spelled %s after the rename: %v", kind, err)
		}
	}
}

// Two reconciles of the repository's record, each started by a rename, apply
// the names they read in the order they read them: the name is read under
// the changelog lock that the record's write takes, so the reconcile that
// read the older name commits before the one that reads the newer name can
// read at all. The first is paused after its read; the second, started after
// a second rename, waits on the lock; the record ends under the newer name.
// Read before the lock, the second committed first and the first wrote the
// older name over it.
func TestReconcilesOfTheRecordApplyNamesInTheOrderTheyRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	svc, id := ds.svc, ds.info.ID
	label := testdb.RepositoryLabel(t)
	b, c := label+"-b.example.com", label+"-c.example.com"
	moveRow := func(name string) {
		t.Helper()
		if _, err := svc.maint.ExecContext(ctx, `UPDATE repositories SET authority = $2 WHERE id = $1`, id, name); err != nil {
			t.Fatal(err)
		}
	}
	read, release := make(chan struct{}), make(chan struct{})
	var first sync.Once
	svc.testReconcileRead = func(r, name string) {
		if r == id && name == b {
			first.Do(func() {
				close(read)
				<-release
			})
		}
	}
	moveRow(b)
	firstDone := make(chan error, 1)
	go func() { firstDone <- ds.reconcileRepositoryName(ctx) }()
	<-read
	moveRow(c)
	secondDone := make(chan error, 1)
	go func() { secondDone <- ds.reconcileRepositoryName(ctx) }()
	// The second is either waiting on the lock the first holds, or, read
	// before the lock, already done.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var waiting int
		if err := svc.maint.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted
			AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 || len(secondDone) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second reconcile neither waits nor ends")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	rec, err := ds.Get(ctx, kindRepository, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.Properties["authority"]; got != c {
		t.Errorf("after both reconciles the record says %v, want %s, the name the row has", got, c)
	}
}

// The archive's manifest names the repository by the name it has when the
// manifest is built, asked of the names cache (namescache.go), never by a
// row read before: a manifest built from a row read before a rename, and the
// archive's next pass after it, both name the new name.
func TestTheArchiveManifestNamesTheNameItHasWhenItIsBuilt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	svc, id := ds.svc, ds.info.ID
	b := testdb.RepositoryLabel(t) + "-b.example.com"
	before, err := svc.repositoryByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.rename(ctx, before, b, renameBy{}); err != nil {
		t.Fatal(err)
	}
	m, err := svc.manifestOf(ctx, before, ds.db)
	if err != nil {
		t.Fatal(err)
	}
	if m.Authority != b {
		t.Errorf("a manifest built from the row read before the rename names %s, want %s", m.Authority, b)
	}
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if got := archivedName(ctx, svc.archive, id); got != b {
		t.Errorf("the archive's manifest after the rename names %q, want %s", got, b)
	}
}
