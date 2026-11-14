package testenv_test

// A DAMAGED REPOSITORY IS SET ASIDE AND THE OTHERS SERVE (decision 0281).
// One substrate holds three repositories, and one of them is damaged while it
// is stopped, once for each step of the start that sets a repository aside.
// The restart serves the other two whole (they sign in, read, write and run a
// trigger), the damaged one answers what it can with the reason `set_aside`,
// and the operator's list names it. Its repair, then a restart, serves it
// again.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

const asidePassword = "correct-horse-battery-staple"

// asideNames are the three repositories; the last is the one damaged.
var asideNames = []string{"ada.example.com", "bob.example.com", "cat.example.com"}

// asideVocabulary is one repository's own package: an item, its copy, and the
// function a trigger runs to copy one into the other.
func asideVocabulary(authority string) string {
	ref := authority + "/aside"
	return `
kind: substrate.reamde.dev/core/package
metadata:
  id: ` + ref + `
data:
  authority: ` + authority + `
  package: aside
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + ref + `/item
data:
  authority: ` + authority + `
  package: aside
  names:
    singular: item
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: what the item is called
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + ref + `/copy
data:
  authority: ` + authority + `
  package: aside
  names:
    singular: copy
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: the name of the item it copies
---
kind: substrate.reamde.dev/core/function
metadata:
  id: ` + ref + `/mirror
data:
  authority: ` + authority + `
  package: aside
  description: copies an item, so a trigger has something to deliver
  runtime: python
  permissions:
    writes: [` + ref + `/copy]
  source: |
    def main(input, host):
        env = input["envelope"]
        return {"effects": [{"action": "put", "kind": "` + ref + `/copy",
                             "id": "c-" + env["change"]["id"],
                             "properties": {"name": env["record"]["properties"]["name"]}}]}
`
}

func itemPath(authority, name string) string {
	return "/api/v1/" + authority + "/aside/item/" + vocabulary.DerivedMintedID("aside", authority, name)
}

func copyPath(authority, name string) string {
	return "/api/v1/" + authority + "/aside/copy/c-" + vocabulary.DerivedMintedID("aside", authority, name)
}

// stepClock is the TOTP clock, moved a step before each sign-in so a code is
// never one the registration already spent.
type stepClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *stepClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *stepClock) step() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(31 * time.Second)
}

// asideWorld is one substrate: its database, data root, key and clock, and
// what each of the three users holds.
type asideWorld struct {
	dsn, root, key string
	clock          *stepClock
	users          map[string]*testenv.Session
	ids            map[string]string
	db             *sql.DB
}

// newAsideWorld registers the three repositories, gives each its package and
// a trigger, writes the item the damage reaches in each, and stops.
func newAsideWorld(t *testing.T) *asideWorld {
	t.Helper()
	w := &asideWorld{
		dsn: testdb.NewSchema(t), root: t.TempDir(), key: testenv.MintCredentialKey(),
		clock: &stepClock{at: time.Now()}, users: map[string]*testenv.Session{}, ids: map[string]string{},
	}
	e := testenv.Start(t, testenv.WithDSN(w.dsn), testenv.WithDataRoot(w.root), testenv.WithCredentialKey(w.key),
		testenv.WithClock(w.clock.now), testenv.WithUser(asideNames[0], asidePassword))
	for i, name := range asideNames {
		u := e
		if i > 0 {
			u = e.RegisterUser(name, asidePassword)
		}
		w.users[name] = u.Session
		u.ApplyVocabularyYAML(asideVocabulary(name))
		u.MustJSON(http.MethodPut, "/api/v1/substrate.reamde.dev/core/trigger/mirror-items", map[string]any{
			"properties": map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{name + "/aside/item"}}},
				"callable": "substrate.reamde.dev/core/function/" + name + "/aside/mirror",
			},
		}, nil)
		u.MustJSON(http.MethodPut, itemPath(name, "damage-me"), map[string]any{"properties": map[string]any{"name": "damage-me"}}, nil)
	}
	e.Stop()
	db, err := sql.Open("pgx", w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	w.db = db
	for _, name := range asideNames {
		var id string
		if err := db.QueryRow(`SELECT id FROM repositories WHERE authority = $1`, name).Scan(&id); err != nil {
			t.Fatalf("the id of %s: %v", name, err)
		}
		w.ids[name] = id
	}
	return w
}

// start starts the substrate again over the same database and data root.
func (w *asideWorld) start(t *testing.T, opts ...engine.Option) *testenv.Env {
	t.Helper()
	return testenv.Start(t, testenv.WithDSN(w.dsn), testenv.WithDataRoot(w.root), testenv.WithCredentialKey(w.key),
		testenv.WithClock(w.clock.now), testenv.WithoutRegistration(), testenv.WithRepository(asideNames[0]),
		testenv.WithEngineOptions(opts...))
}

// archiveDir is the repository's archive, the copy a restore reads
// (decision 0262), under the data root where the server keeps it.
func (w *asideWorld) archiveDir(name string) string {
	return filepath.Join(w.root, "archive", changelogfile.RepositoriesDir, w.ids[name])
}

// store is the archive the server writes, which the operator's list reads.
func (w *asideWorld) store(t *testing.T) archive.Store {
	t.Helper()
	d, err := archive.NewDir(filepath.Join(w.root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// eraseRows removes every row of the repository named, as a database
// restored without it holds none; its archive and its data root stay.
func (w *asideWorld) eraseRows(t *testing.T, name string) {
	t.Helper()
	rows, err := w.db.Query(`
		SELECT c.table_name FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name AND t.table_type = 'BASE TABLE'
		WHERE c.table_schema = current_schema() AND c.column_name = 'repository'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	for _, table := range tables {
		if _, err := w.db.Exec(`DELETE FROM `+table+` WHERE repository = $1`, w.ids[name]); err != nil {
			t.Fatalf("erase %s's rows of %s: %v", name, table, err)
		}
	}
	if _, err := w.db.Exec(`DELETE FROM repositories WHERE id = $1`, w.ids[name]); err != nil {
		t.Fatal(err)
	}
	// Its people's places in it, and a person it was the last of, go too,
	// as an erase takes them (decision 0274).
	if _, err := w.db.Exec(`DELETE FROM identity_repositories WHERE repository_id = $1`, w.ids[name]); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.Exec(`DELETE FROM identities i WHERE NOT EXISTS (SELECT 1 FROM identity_repositories l WHERE l.identity = i.id)`); err != nil {
		t.Fatal(err)
	}
}

// restoreCutShort leaves the repository named mid-restore: its rows gone, a
// start restores it from its archive and dies after the first batch of its
// lines (the others serve, and it is set aside), and its archive is moved to
// backup. The next start finds an import its record says is reading an
// archive that is not there, and sets it aside at the boot check; moving
// the archive back is the repair.
func (w *asideWorld) restoreCutShort(t *testing.T, name, backup string) {
	t.Helper()
	w.eraseRows(t, name)
	killed := false
	e := w.start(t, engine.WithTestImportFault(16, func(stage string) error {
		if stage == "after a batch of rows committed" && !killed {
			killed = true
			return errors.New("the process died after the first batch")
		}
		return nil
	}))
	e.Stop()
	if !killed {
		t.Fatalf("the restore of %s was not cut short", name)
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(w.archiveDir(name), backup); err != nil {
		t.Fatal(err)
	}
}

// unreadableDirectory puts a 0.x directory beside the repository named, under
// its id in the data root, whose manifest does not read: the start holds such
// a directory to its rows before anything else and cannot read this one, so
// it sets the repository aside at the boot check. Its rows and its fold are
// whole, and a process that opens it read-only never reads the directory.
func (w *asideWorld) unreadableDirectory(t *testing.T, name string) {
	t.Helper()
	dir := filepath.Join(w.root, changelogfile.RepositoriesDir, w.ids[name])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, changelogfile.ManifestName), []byte("{not a manifest"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// leaveImportUnfinished marks the repository named's import unfinished past
// its table's head, with no archive its record names to finish it from: a
// 0.x directory's import cut short, and the directory gone since. The boot
// check sets it aside.
func (w *asideWorld) leaveImportUnfinished(t *testing.T, name string) {
	t.Helper()
	if _, err := w.db.Exec(`
		INSERT INTO import_progress (repository, file_head)
		SELECT $1, coalesce(max(seq), 0) + 1 FROM changelog WHERE repository = $1
		ON CONFLICT (repository) DO UPDATE SET file_head = EXCLUDED.file_head`, w.ids[name]); err != nil {
		t.Fatal(err)
	}
}

// serves holds one repository to what it serves: its user signs in, reads
// what it wrote before the stop, writes, and a trigger pass, the one
// substrated's dispatcher runs, delivers the write.
func (w *asideWorld) serves(t *testing.T, e *testenv.Env, name string, signIn bool) {
	t.Helper()
	u := e.WithToken(w.users[name].Token)
	if signIn {
		w.clock.step()
		u.TOTPSecret = w.users[name].TOTPSecret
		if status, raw := u.Login(name, asidePassword, u.TOTPCode()); status != http.StatusCreated {
			t.Fatalf("%s signs in: %d %s", name, status, raw)
		}
	}
	if status, raw := u.Do(http.MethodGet, itemPath(name, "damage-me"), nil); status != http.StatusOK {
		t.Fatalf("%s reads its item: %d %s", name, status, raw)
	}
	written := "after-" + strings.ReplaceAll(t.Name(), "/", "-")
	if status, raw := u.Do(http.MethodPut, itemPath(name, written), map[string]any{"properties": map[string]any{"name": written}}); status/100 != 2 {
		t.Fatalf("%s writes: %d %s", name, status, raw)
	}
	ds, err := e.Service.Dataset(context.Background(), name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	if _, err := ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("%s's trigger pass: %v", name, err)
	}
	if status, raw := u.Do(http.MethodGet, copyPath(name, written), nil); status != http.StatusOK {
		t.Fatalf("%s's trigger did not deliver the write: %d %s", name, status, raw)
	}
}

// refusedSetAside holds one answer to the refusal of a set-aside repository.
func refusedSetAside(t *testing.T, what string, status int, raw []byte, header http.Header) {
	t.Helper()
	var env substrate.ErrorEnvelope
	_ = json.Unmarshal(raw, &env)
	if status != http.StatusServiceUnavailable || env.Error.Code != "unavailable" || env.Error.Reason != substrate.ReasonSetAside {
		t.Fatalf("%s = %d %s, want 503 unavailable with the reason %s", what, status, raw, substrate.ReasonSetAside)
	}
	if header.Get("Retry-After") != "300" {
		t.Errorf("%s: Retry-After %q, want 300", what, header.Get("Retry-After"))
	}
}

// listed is the state the operator's list gives the repository named.
func (w *asideWorld) listed(t *testing.T, name string) engine.RepositoryState {
	t.Helper()
	states, err := engine.RepositoryStates(context.Background(), w.db, w.root, w.store(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.Authority == name {
			return st
		}
	}
	t.Fatalf("the operator's list does not name %s: %+v", name, states)
	return engine.RepositoryState{}
}

// copyDir copies a repository directory whole, for the damage to be undone
// from: the copy taken before it.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	copyKept(t, src, dst)
}

// asideCase is one step of the start that sets a repository aside: the damage
// that reaches it, what the repository can still answer, and its repair.
type asideCase struct {
	name, stage string
	damage      func(t *testing.T, w *asideWorld, backup string)
	// refuses holds what the damaged repository answers on the restarted
	// substrate.
	refuses func(t *testing.T, w *asideWorld, e *testenv.Env)
	// repair undoes the damage the way docs/operations.md says, and names the
	// options the start after it takes.
	repair func(t *testing.T, w *asideWorld, backup string) []engine.Option
	// signIn is whether the repaired one is signed in to; one imported
	// without its secrets has no password until `user reset`.
	signIn bool
}

var asideCases = []asideCase{{
	name:  "a data key wrap under this host's key that does not open",
	stage: "credential key",
	damage: func(t *testing.T, w *asideWorld, _ string) {
		if _, err := w.db.Exec(`UPDATE repositories SET dek = set_byte(dek, length(dek) - 1, (get_byte(dek, length(dek) - 1) + 1) % 256) WHERE id = $1`,
			w.ids["cat.example.com"]); err != nil {
			t.Fatal(err)
		}
	},
	refuses: func(t *testing.T, w *asideWorld, e *testenv.Env) {
		u := e.WithToken(w.users["cat.example.com"].Token)
		status, raw, header := u.DoRaw(http.MethodGet, "/api/v1/records", nil, nil)
		refusedSetAside(t, "a token of its own", status, raw, header)
		// The person's sign-in is the server's, under a key of its own
		// (decision 0274), so it proves; the repository it opens is set aside.
		w.clock.step()
		u.TOTPSecret = w.users["cat.example.com"].TOTPSecret
		status, raw, header = u.DoRaw(http.MethodPost, "/login", mustMarshal(t, substrate.LoginRequest{
			Identity: "cat.example.com", Password: asidePassword, TOTPCode: u.TOTPCode(),
		}), map[string]string{"Content-Type": "application/json", "Authorization": ""})
		refusedSetAside(t, "its sign-in", status, raw, header)
	},
	repair: func(t *testing.T, w *asideWorld, _ string) []engine.Option {
		if _, err := w.db.Exec(`UPDATE repositories SET dek = set_byte(dek, length(dek) - 1, (get_byte(dek, length(dek) - 1) + 255) % 256) WHERE id = $1`,
			w.ids["cat.example.com"]); err != nil {
			t.Fatal(err)
		}
		return nil
	},
	signIn: true,
}, {
	name:  "a 0.x directory beside it whose manifest does not read",
	stage: "boot check",
	damage: func(t *testing.T, w *asideWorld, _ string) {
		w.unreadableDirectory(t, "cat.example.com")
	},
	refuses: func(t *testing.T, w *asideWorld, e *testenv.Env) {
		u := e.WithToken(w.users["cat.example.com"].Token)
		status, raw, header := u.DoRaw(http.MethodGet, "/api/v1/records", nil, nil)
		refusedSetAside(t, "a token of its own", status, raw, header)
		status, raw, header = u.DoRaw(http.MethodPost, "/api/v1/authorizations/check", []byte(`{"scope":""}`),
			map[string]string{"Content-Type": "application/json"})
		refusedSetAside(t, "an app's check", status, raw, header)
		w.clock.step()
		u.TOTPSecret = w.users["cat.example.com"].TOTPSecret
		status, raw, header = u.DoRaw(http.MethodPost, "/login", mustMarshal(t, substrate.LoginRequest{
			Identity: "cat.example.com", Password: asidePassword, TOTPCode: u.TOTPCode(),
		}), map[string]string{"Content-Type": "application/json", "Authorization": ""})
		refusedSetAside(t, "its sign-in", status, raw, header)
		status, raw, header = u.DoRaw(http.MethodPost, "/webhooks/"+w.ids["cat.example.com"]+"/mirror-items", []byte(`{}`),
			map[string]string{"Content-Type": "application/json", "Authorization": ""})
		refusedSetAside(t, "a webhook addressed by its id", status, raw, header)
		if status, raw := u.Do(http.MethodPost, "/webhooks/cat.example.com/mirror-items", map[string]any{}); status != http.StatusNotFound {
			t.Errorf("a webhook addressed by its name = %d %s, want the door's 404", status, raw)
		}
	},
	repair: func(t *testing.T, w *asideWorld, _ string) []engine.Option {
		// The directory is nobody's history: moved out of the data root.
		if err := os.Remove(filepath.Join(w.root, changelogfile.RepositoriesDir, w.ids["cat.example.com"], changelogfile.ManifestName)); err != nil {
			t.Fatal(err)
		}
		return nil
	},
	signIn: true,
}, {
	name:  "an import a start began, whose archive is gone",
	stage: "boot check",
	damage: func(t *testing.T, w *asideWorld, backup string) {
		w.restoreCutShort(t, "cat.example.com", backup)
	},
	refuses: func(t *testing.T, w *asideWorld, e *testenv.Env) {
		// Its history is part restored and its fold not rebuilt, so no token
		// or credential of it is there to read: what addresses it by its id
		// is refused as set aside, and its name stays taken.
		u := e.WithToken("")
		status, raw, header := u.DoRaw(http.MethodPost, "/webhooks/"+w.ids["cat.example.com"]+"/mirror-items", []byte(`{}`),
			map[string]string{"Content-Type": "application/json", "Authorization": ""})
		refusedSetAside(t, "a webhook addressed by its id", status, raw, header)
		if status, raw := u.Do(http.MethodPost, "/webhooks/cat.example.com/mirror-items", map[string]any{}); status != http.StatusNotFound {
			t.Errorf("a webhook addressed by its name = %d %s, want the door's 404", status, raw)
		}
		_, err := e.Service.Register(context.Background(), substrate.RegisterInput{
			Identity: "someone.example.com", Repository: "cat.example.com", Password: asidePassword, SkipTOTP: true,
		})
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "already owned") {
			t.Errorf("a registration of the set-aside repository's name = %v, want refused as taken", err)
		}
	},
	repair: func(t *testing.T, w *asideWorld, backup string) []engine.Option {
		if err := os.Rename(backup, w.archiveDir("cat.example.com")); err != nil {
			t.Fatal(err)
		}
		return nil
	},
	signIn: true,
}, {
	name:  "an archive with no row whose key this host does not open",
	stage: "restore",
	damage: func(t *testing.T, w *asideWorld, backup string) {
		dir := w.archiveDir("cat.example.com")
		copyDir(t, dir, backup)
		m, _, err := changelogfile.ReadArchiveManifestDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		m.DEK[len(m.DEK)-1] ^= 1
		m.Epoch++
		if _, err := changelogfile.WriteArchiveManifestDir(dir, m); err != nil {
			t.Fatal(err)
		}
		// A database restored without it: the archive is all that is left.
		w.eraseRows(t, "cat.example.com")
	},
	refuses: func(t *testing.T, w *asideWorld, e *testenv.Env) {
		u := e.WithToken("")
		status, raw, header := u.DoRaw(http.MethodPost, "/webhooks/"+w.ids["cat.example.com"]+"/mirror-items", []byte(`{}`),
			map[string]string{"Content-Type": "application/json", "Authorization": ""})
		refusedSetAside(t, "a webhook addressed by its id", status, raw, header)
		_, err := e.Service.Register(context.Background(), substrate.RegisterInput{
			Identity: "someone.example.com", Repository: "cat.example.com", Password: asidePassword, SkipTOTP: true,
		})
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "already owned") {
			t.Errorf("a registration of the set-aside directory's name = %v, want refused as taken", err)
		}
	},
	repair: func(t *testing.T, w *asideWorld, backup string) []engine.Option {
		copyDir(t, backup, w.archiveDir("cat.example.com"))
		return nil
	},
	signIn: true,
}}

func TestADamagedRepositoryIsSetAsideAndTheOthersServe(t *testing.T) {
	for _, tc := range asideCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newAsideWorld(t)
			backup := filepath.Join(t.TempDir(), "backup")
			tc.damage(t, w, backup)

			e := w.start(t)
			for _, name := range asideNames[:2] {
				w.serves(t, e, name, true)
				if st := w.listed(t, name); st.State != engine.StateServing {
					t.Errorf("the list shows %s %q, want %q", name, st.State, engine.StateServing)
				}
			}
			tc.refuses(t, w, e)
			if st := w.listed(t, "cat.example.com"); st.State != engine.StateSetAside {
				t.Errorf("the list shows the damaged repository %q, want %q", st.State, engine.StateSetAside)
			}
			repos, err := e.Service.Repositories(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range repos {
				if r.Authority == "cat.example.com" {
					t.Errorf("the loops' listing names the set-aside repository: %+v", r)
				}
			}
			e.Stop()

			opts := tc.repair(t, w, backup)
			e = w.start(t, opts...)
			for _, name := range asideNames {
				w.serves(t, e, name, tc.signIn || name != "cat.example.com")
			}
			if st := w.listed(t, "cat.example.com"); st.State != engine.StateServing {
				t.Errorf("the list shows the repaired repository %q, want %q", st.State, engine.StateServing)
			}
		})
	}
}

// A directory from before repository ids, restored on a host whose key did
// not write it and not listed, cannot take its id: the keying sets it aside
// (decision 0264's refusal), and listing it is the way back.
func TestADirectoryFromBeforeIDsTheKeyingRefusesIsSetAside(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	w := newAsideWorld(t)
	kept := filepath.Join(w.root, "repositories", fixture.Repository)
	copyKept(t, filepath.Join(dir, "repositories", fixture.Repository), kept)

	e := w.start(t)
	for _, name := range asideNames {
		w.serves(t, e, name, true)
	}
	if st := w.listed(t, fixture.Repository); st.State != engine.StateSetAside || !st.Directory {
		t.Errorf("the list shows the kept directory %+v, want a directory set aside", st)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the set-aside directory moved: %v", err)
	}
	e.Stop()

	e = w.start(t, engine.WithImportWithoutSecrets(fixture.Repository))
	e.Token = fixture.Token
	// Its sign-in record was sealed under the key it left behind, so it has
	// no person until the operator's reset gives it one (decision 0274).
	if _, err := e.Service.(engine.Operator).ResetUser(context.Background(), fixture.Repository, asidePassword, ""); err != nil {
		t.Fatalf("give the listed directory its person: %v", err)
	}
	if status, raw := e.Do(http.MethodGet, "/api/v1/records", nil); status != http.StatusOK {
		t.Fatalf("the listed directory, imported: %d %s", status, raw)
	}
	if st := w.listed(t, fixture.Repository); st.State != engine.StateServing {
		t.Errorf("the list shows the imported directory %q, want %q", st.State, engine.StateServing)
	}
}

// What no repository's absence cures still stops the start: a damaged
// repository that is the only one, a second server over the same database,
// and a host key the database was not written under.
func TestWhatStopsTheStartStillStopsIt(t *testing.T) {
	t.Run("nothing left to serve", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		key := testenv.MintCredentialKey()
		e := testenv.Start(t, testenv.WithDataRoot(root), testenv.WithCredentialKey(key))
		ds, err := e.Service.Dataset(context.Background(), e.Repository)
		if err != nil {
			t.Fatal(err)
		}
		id := ds.Repository().ID
		e.Stop()
		db, err := sql.Open("pgx", e.DSN)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		if _, err := db.Exec(`UPDATE repositories SET dek = set_byte(dek, length(dek) - 1, (get_byte(dek, length(dek) - 1) + 1) % 256) WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		svc, err := engine.Open(context.Background(), e.DSN, engine.WithKindsFS(kinds.Seed()),
			engine.WithDataRoot(root), engine.WithCredentialKey(key))
		if err == nil {
			_ = svc.Close()
			t.Fatal("a start that set aside its only repository started")
		}
		if !strings.Contains(err.Error(), "does not open the DEK of repository") {
			t.Fatalf("the refusal is not the repository's: %v", err)
		}
	})
	t.Run("another writer", func(t *testing.T) {
		t.Parallel()
		w := newAsideWorld(t)
		e := w.start(t)
		svc, err := engine.Open(context.Background(), w.dsn, engine.WithKindsFS(kinds.Seed()),
			engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(w.key))
		if err == nil {
			_ = svc.Close()
			t.Fatal("a second server over the same database started")
		}
		if !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
			t.Fatalf("the refusal is not the writer lease's: %v", err)
		}
		w.serves(t, e, asideNames[2], true)
	})
	t.Run("another host key", func(t *testing.T) {
		t.Parallel()
		w := newAsideWorld(t)
		svc, err := engine.Open(context.Background(), w.dsn, engine.WithKindsFS(kinds.Seed()),
			engine.WithDataRoot(w.root), engine.WithCredentialKey(testenv.MintCredentialKey()))
		if err == nil {
			_ = svc.Close()
			t.Fatal("a host with another key started over the database")
		}
		if !strings.Contains(err.Error(), "does not open the DEK of 3 of this database's 3 repositories") {
			t.Fatalf("the refusal is not the key's: %v", err)
		}
	})
}

// A set-aside repository whose owner registered no second factor passes the
// sign-in door on the password alone, and its deletion behind that door is
// refused. A second factor's enrollment is the person's, held by the server
// beside the repositories (decision 0274): it writes no repository, so it
// goes on.
func TestASetAsideRepositoryWithNoSecondFactorIsNotDeleted(t *testing.T) {
	ctx := context.Background()
	w := newAsideWorld(t)
	e := w.start(t)
	const dan = "dan.example.com"
	reg, err := e.Service.Register(ctx, substrate.RegisterInput{Identity: dan, Repository: dan, Password: asidePassword, SkipTOTP: true})
	if err != nil {
		t.Fatal(err)
	}
	e.WithToken(reg.Secret).MustJSON(http.MethodPut, "/api/v1/substrate.reamde.dev/core/contact/"+vocabulary.DerivedMintedID("aside", dan),
		map[string]any{"properties": map[string]any{"name": "damage-me"}}, nil)
	ds, err := e.Service.Dataset(ctx, dan)
	if err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	e.Stop()
	w.ids[dan] = id
	w.leaveImportUnfinished(t, dan)
	archived := filepath.Join(w.root, "archive", changelogfile.RepositoriesDir, id)

	e = w.start(t)
	w.clock.step()
	status, raw, header := e.WithToken("").DoRaw(http.MethodPost, "/repository/delete",
		mustMarshal(t, map[string]any{"identity": dan, "repository": dan, "password": asidePassword}),
		map[string]string{"Content-Type": "application/json", "Authorization": ""})
	refusedSetAside(t, "/repository/delete", status, raw, header)
	w.clock.step()
	if status, raw, _ := e.WithToken("").DoRaw(http.MethodPost, "/totp/enroll",
		mustMarshal(t, map[string]any{"identity": dan, "password": asidePassword}),
		map[string]string{"Content-Type": "application/json", "Authorization": ""}); status != http.StatusOK {
		t.Errorf("the person's second factor's enrollment = %d %s, want it issued: it writes no repository", status, raw)
	}
	var rows int
	if err := w.db.QueryRow(`SELECT count(*) FROM repositories WHERE id = $1`, id).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("the set-aside repository's row: %d (%v), want it kept", rows, err)
	}
	if _, err := os.Stat(archived); err != nil {
		t.Errorf("the set-aside repository's archive: %v, want it kept", err)
	}
	w.serves(t, e, asideNames[0], true)
}

// A copy restored where another repository has since taken its name only
// claims the name: it is set aside, and the repository that holds the name
// serves. Once for an archive and once for a directory from before ids.
func TestARefusedDirectoryLeavesTheRepositoryHoldingItsNameServing(t *testing.T) {
	t.Run("an archive", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		w := newAsideWorld(t)
		const cat = "cat.example.com"
		old, oldID := filepath.Join(t.TempDir(), "old"), w.ids[cat]
		// The repository goes, its rows and its archive, and somebody
		// registers its name again.
		if err := os.Rename(w.archiveDir(cat), old); err != nil {
			t.Fatal(err)
		}
		w.eraseRows(t, cat)
		e := w.start(t)
		reg, err := e.Service.Register(ctx, substrate.RegisterInput{Identity: cat, Repository: cat, Password: asidePassword, SkipTOTP: true})
		if err != nil {
			t.Fatal(err)
		}
		e.Stop()
		copyDir(t, old, filepath.Join(w.root, "archive", changelogfile.RepositoriesDir, oldID))

		e = w.start(t)
		u := e.WithToken(reg.Secret)
		if status, raw := u.Do(http.MethodGet, "/api/v1/records", nil); status != http.StatusOK {
			t.Fatalf("the repository holding the name reads: %d %s", status, raw)
		}
		if status, raw := u.Do(http.MethodPut, "/api/v1/substrate.reamde.dev/core/contact/"+vocabulary.DerivedMintedID("aside", "new"),
			map[string]any{"properties": map[string]any{"name": "new"}}); status/100 != 2 {
			t.Fatalf("the repository holding the name writes: %d %s", status, raw)
		}
		states, err := engine.RepositoryStates(ctx, w.db, w.root, w.store(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range states {
			switch {
			case st.ID == oldID && (!st.Directory || st.State != engine.StateSetAside):
				t.Errorf("the restored archive reads %+v, want an archive with no row set aside", st)
			case st.ID != oldID && st.State != engine.StateServing:
				t.Errorf("%s reads %+v, want serving", st.Authority, st)
			}
		}
		for _, name := range asideNames[:2] {
			w.serves(t, e, name, true)
		}
	})
	t.Run("from before ids", func(t *testing.T) {
		t.Parallel()
		var fixture keptFixture
		readKept(t, filepath.Join(keptRoot, "v0.123.1", "fixture.json"), &fixture)
		w := newAsideWorld(t)
		e := w.start(t)
		reg, err := e.Service.Register(context.Background(), substrate.RegisterInput{Identity: fixture.Repository, Repository: fixture.Repository, Password: asidePassword, SkipTOTP: true})
		if err != nil {
			t.Fatal(err)
		}
		e.Stop()
		copyKept(t, filepath.Join(keptRoot, "v0.123.1", "repositories", fixture.Repository), filepath.Join(w.root, "repositories", fixture.Repository))

		e = w.start(t)
		if status, raw := e.WithToken(reg.Secret).Do(http.MethodGet, "/api/v1/records", nil); status != http.StatusOK {
			t.Fatalf("the repository holding the name reads: %d %s", status, raw)
		}
		var rowState, dirState string
		states, err := engine.RepositoryStates(context.Background(), w.db, w.root, w.store(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range states {
			if st.Authority == fixture.Repository && st.Directory {
				dirState = st.State
			} else if st.Authority == fixture.Repository {
				rowState = st.State
			}
		}
		if rowState != engine.StateServing || dirState != engine.StateSetAside {
			t.Errorf("the row reads %q and the directory %q, want %q and %q", rowState, dirState, engine.StateServing, engine.StateSetAside)
		}
	})
}

// An operator's command runs in a process of its own, and the server's
// set-aside list is the server's memory. What that process reads is the
// writer leases: a running server holds the lease of every repository it
// serves and none of one it set aside. The re-embed opens read-only beside
// the server, runs no start of its own, and is the one such command that
// writes, its queue rows. Beside a server that set a repository aside for a
// 0.x directory beside it that does not read, which the read-only open never
// reads, it refuses that repository and leaves its queue as it was, and
// still queues the healthy neighbor's. A command that
// writes as the writer (rebuild, reindex, rotate-generation, user reset, an
// import at its start) refuses beside the running server whatever
// it names: its own start meets the leases the server holds.
func TestAnOperatorCommandBesideTheServerWritesNothingToASetAsideRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newAsideWorld(t)
	e := w.start(t)
	for _, name := range []string{"ada.example.com", "cat.example.com"} {
		u := e.WithToken(w.users[name].Token)
		u.MustJSON(http.MethodPost, "/api/v1/records", map[string]any{
			"kind": "substrate.reamde.dev/core/contact",
			"properties": map[string]any{
				"name": "Grace", "details": "met at the market; asks after the bees",
			},
		}, nil)
		u.MustJSON(http.MethodPut, "/api/v1/substrate.reamde.dev/llm/provider/vectors", map[string]any{"properties": map[string]any{
			"label": "vectors", "wire": "openai", "baseURL": "https://llm.example.com/v1",
			"apiKey": "sk-vectors-value", "embedModel": "text-embedding-3-small",
		}}, nil)
	}
	e.Stop()
	w.unreadableDirectory(t, "cat.example.com")
	e = w.start(t)
	defer e.Stop()
	if st := w.listed(t, "cat.example.com"); st.State != engine.StateSetAside {
		t.Fatalf("the list shows the repository beside an unreadable directory %q, want %q", st.State, engine.StateSetAside)
	}
	queue := func(name string) string {
		t.Helper()
		rows, err := w.db.Query(`SELECT record_kind, record_id, property, generation FROM embed_queue
		                          WHERE repository = $1 ORDER BY 1, 2, 3`, w.ids[name])
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var kind, id, prop string
			var gen int64
			if err := rows.Scan(&kind, &id, &prop, &gen); err != nil {
				t.Fatal(err)
			}
			out = append(out, kind+"/"+id+"#"+prop+"@"+strconv.FormatInt(gen, 10))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return strings.Join(out, ",")
	}
	before := queue("cat.example.com")

	op, err := engine.OpenOperator(ctx, w.dsn, engine.WithKindsFS(kinds.Seed()), engine.WithDataRoot(w.root),
		engine.WithCredentialKey(w.key), engine.WithDirectoryReadOnly())
	if err != nil {
		t.Fatalf("open beside the server: %v", err)
	}
	defer func() { _ = op.Close() }()
	ds, err := op.Dataset(ctx, "cat.example.com")
	if err == nil {
		_, err = ds.Reembed(ctx, true)
	}
	if !errors.Is(err, substrate.ErrSetAside) {
		t.Fatalf("a re-embed of the set-aside repository beside the server = %v, want refused as set aside", err)
	}
	if after := queue("cat.example.com"); after != before {
		t.Fatalf("the refused re-embed moved the set-aside repository's queue:\n before %s\n after  %s", before, after)
	}
	ada, err := op.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if report, err := ada.Reembed(ctx, true); err != nil || report.Enqueued == 0 {
		t.Fatalf("a re-embed of the healthy neighbor = %+v, %v, want its contact queued", report, err)
	}

	_, err = engine.OpenOperator(ctx, w.dsn, engine.WithKindsFS(kinds.Seed()), engine.WithDataRoot(w.root),
		engine.WithCredentialKey(w.key))
	if !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		t.Fatalf("a writer's open beside the server = %v, want refused for the leases it holds", err)
	}
}

// A command that opens as the writer (rebuild, reindex, rotate-generation,
// user reset) beside a running server refuses before it writes to any
// repository, the one the server set aside included, which the server holds
// no lease of and which its start would otherwise reach first. Here it is the
// oldest, set aside for a restore cut short whose archive was gone, and the
// operator has put the archive back, so a check of it would finish its
// import. Its rows and its archive are as they were after the refusal.
func TestAWriterBesideTheServerWritesNothingBeforeItRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newAsideWorld(t)
	const name = "cat.example.com"
	backup := filepath.Join(t.TempDir(), "backup")
	w.restoreCutShort(t, name, backup)
	if _, err := w.db.Exec(`UPDATE repositories SET created_at = created_at - interval '10 years' WHERE id = $1`, w.ids[name]); err != nil {
		t.Fatal(err)
	}
	e := w.start(t)
	defer e.Stop()
	if st := w.listed(t, name); st.State != engine.StateSetAside {
		t.Fatalf("the list shows %s %q, want %q", name, st.State, engine.StateSetAside)
	}
	// The repair: the archive, back where the import reads it.
	if err := os.Rename(backup, w.archiveDir(name)); err != nil {
		t.Fatal(err)
	}
	files := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		err := filepath.WalkDir(w.archiveDir(name), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[strings.TrimPrefix(path, w.archiveDir(name))] = string(raw)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	rows := func() string {
		t.Helper()
		var n, head int64
		if err := w.db.QueryRow(`SELECT count(*), coalesce(max(seq), 0) FROM changelog WHERE repository = $1`, w.ids[name]).Scan(&n, &head); err != nil {
			t.Fatal(err)
		}
		var records int64
		if err := w.db.QueryRow(`SELECT count(*) FROM records WHERE repository = $1`, w.ids[name]).Scan(&records); err != nil {
			t.Fatal(err)
		}
		var marked int64
		if err := w.db.QueryRow(`SELECT count(*) FROM import_progress WHERE repository = $1`, w.ids[name]).Scan(&marked); err != nil {
			t.Fatal(err)
		}
		return strconv.FormatInt(n, 10) + "/" + strconv.FormatInt(head, 10) + "/" + strconv.FormatInt(records, 10) + "/" + strconv.FormatInt(marked, 10)
	}
	beforeFiles, beforeRows := files(), rows()

	op, err := engine.OpenOperator(ctx, w.dsn, engine.WithKindsFS(kinds.Seed()), engine.WithDataRoot(w.root), engine.WithCredentialKey(w.key))
	if err == nil {
		_ = op.Close()
		t.Fatal("a writer's open beside the server was not refused")
	}
	if !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		t.Fatalf("a writer's open beside the server = %v, want refused for the leases it holds", err)
	}
	after := files()
	for path, body := range after {
		if was, ok := beforeFiles[path]; !ok || was != body {
			t.Errorf("the refused open wrote %s in the set-aside repository's archive", path)
		}
	}
	for path := range beforeFiles {
		if _, ok := after[path]; !ok {
			t.Errorf("the refused open removed %s from the set-aside repository's archive", path)
		}
	}
	if afterRows := rows(); afterRows != beforeRows {
		t.Fatalf("the refused open changed the set-aside repository's rows: %s before, %s after", beforeRows, afterRows)
	}
	// It took the set-aside repository's lease on the way to the server's and
	// gave it back with the refusal.
	if st := w.listed(t, name); st.State != engine.StateSetAside {
		t.Fatalf("after the refused open the list shows %s %q, want %q", name, st.State, engine.StateSetAside)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The webhook door answers a repository the start set aside as main's door
// did before a name became an id at the door (decision record 0287): by its
// id, the address the server gave out, `503 set_aside`, so a provider retries
// rather than drops the delivery; by its name, the door's one refusal, `404`,
// so a prober learns nothing. Both hold for a row the start set aside and,
// by its id, for an archive with no row that only claims its name.
func TestTheWebhookDoorSaysSetAsideByIDAndNothingByName(t *testing.T) {
	w := newAsideWorld(t)
	// bob: a row the boot check sets aside, its directory unreadable.
	w.unreadableDirectory(t, "bob.example.com")
	// cat: an archive with no row whose key this host does not open.
	dir := w.archiveDir("cat.example.com")
	m, _, err := changelogfile.ReadArchiveManifestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.DEK[len(m.DEK)-1] ^= 1
	m.Epoch++
	if _, err := changelogfile.WriteArchiveManifestDir(dir, m); err != nil {
		t.Fatal(err)
	}
	w.eraseRows(t, "cat.example.com")

	e := w.start(t)
	w.serves(t, e, "ada.example.com", true)
	u := e.WithToken("")
	post := func(address string) (int, []byte, http.Header) {
		return u.DoRaw(http.MethodPost, "/webhooks/"+address+"/mirror-items", []byte(`{}`),
			map[string]string{"Content-Type": "application/json", "Authorization": ""})
	}
	for _, name := range []string{"bob.example.com", "cat.example.com"} {
		status, raw, header := post(w.ids[name])
		refusedSetAside(t, name+" addressed by its id", status, raw, header)
		if status, raw, _ := post(name); status != http.StatusNotFound {
			t.Errorf("%s addressed by its name = %d %s, want the door's 404", name, status, raw)
		}
	}
}
