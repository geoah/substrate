package main

// NO LOOP OPENS A SET-ASIDE REPOSITORY (decision 0281). The server's start
// sets a damaged repository aside, and nothing may write it: every loop the
// server runs (the gc and resolution sweeps, the OAuth maintenance, the
// trigger dispatcher with its schedules, the embedding drain, the publisher
// refresh) runs once here over a substrate holding one, and the test fails
// if any of them asks for it, or if a byte of its archive or a row of its
// history moved.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
)

// openRecorder is the service the loops are given: every repository they
// ask for is recorded on the way to the engine.
type openRecorder struct {
	substrate.Service
	mu     sync.Mutex
	opened []string
}

func (r *openRecorder) Dataset(ctx context.Context, repository string) (substrate.Dataset, error) {
	r.mu.Lock()
	r.opened = append(r.opened, repository)
	r.mu.Unlock()
	return r.Service.Dataset(ctx, repository)
}

func TestNoLoopOpensASetAsideRepository(t *testing.T) {
	const password = "correct-horse-battery-staple"
	ctx := context.Background()
	root := t.TempDir()
	key := testenv.MintCredentialKey()
	e := testenv.Start(t, testenv.WithDataRoot(root), testenv.WithCredentialKey(key),
		testenv.WithUser("ada.example.com", password))
	e.RegisterUser("bob.example.com", password)
	e.RegisterUser("cat.example.com", password)
	ids := map[string]string{}
	for _, name := range []string{"ada.example.com", "bob.example.com", "cat.example.com"} {
		ds, err := e.Service.Dataset(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = ds.Repository().ID
	}
	aside := ids["cat.example.com"]
	e.Stop()

	db, err := sql.Open("pgx", e.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// A data key wrap under this host's key that does not open: the start
	// sets the repository aside.
	if _, err := db.Exec(`UPDATE repositories SET dek = set_byte(dek, length(dek) - 1, (get_byte(dek, length(dek) - 1) + 1) % 256) WHERE id = $1`, aside); err != nil {
		t.Fatal(err)
	}
	// The set-aside repository's copy is its archive (decision 0262): no loop
	// may write it, nor its rows.
	archived := filepath.Join(root, "archive", "repositories", aside)
	files, rows := directoryDigest(t, archived), historyOf(t, db, aside)

	e = testenv.Start(t, testenv.WithDSN(e.DSN), testenv.WithDataRoot(root), testenv.WithCredentialKey(key),
		testenv.WithoutRegistration(), testenv.WithRepository("ada.example.com"))
	rec := &openRecorder{Service: e.Service}
	cat, err := catalog.Load(catalog.Shipped()...)
	if err != nil {
		t.Fatal(err)
	}
	sweepGC(ctx, rec)
	sweepResolutions(ctx, rec)
	maintainOAuth(ctx, rec)
	var passes sync.WaitGroup
	newTriggerDispatcher(rec, &passes).dispatch(ctx)
	passes.Wait()
	drainEmbeds(ctx, rec)
	refreshPublishers(ctx, rec, cat)

	if slices.Contains(rec.opened, aside) {
		t.Errorf("a loop opened the set-aside repository: %v", rec.opened)
	}
	for _, name := range []string{"ada.example.com", "bob.example.com"} {
		if !slices.Contains(rec.opened, ids[name]) {
			t.Errorf("no loop opened %s, so the test holds nothing: %v", name, rec.opened)
		}
	}
	if !maps.Equal(files, directoryDigest(t, archived)) {
		t.Error("a file of the set-aside repository's archive changed")
	}
	if got := historyOf(t, db, aside); got != rows {
		t.Errorf("the set-aside repository's history moved: %+v, then %+v", rows, got)
	}
}

// directoryDigest is every file under dir by its path, as its SHA-256.
func directoryDigest(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = sha256.Sum256(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("no file under %s", dir)
	}
	return out
}

// history is how far one repository's history and fold reach in the table.
type history struct {
	Entries, Head, Records int64
}

func historyOf(t *testing.T, db *sql.DB, id string) history {
	t.Helper()
	var h history
	if err := db.QueryRow(`SELECT count(*), coalesce(max(seq), 0) FROM changelog WHERE repository = $1`, id).Scan(&h.Entries, &h.Head); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM records WHERE repository = $1`, id).Scan(&h.Records); err != nil {
		t.Fatal(err)
	}
	return h
}
