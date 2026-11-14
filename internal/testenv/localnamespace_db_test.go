package testenv_test

import (
	"archive/tar"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An export imports under another name (decision record 0259), through the
// archive (decision 0278). A repository with kinds of its own, references
// between them, a function, a trigger and an app token scoped to them is
// exported from one server and put into the archive of another that already
// holds a repository of the same name. That server starts, serves its own
// repository and sets the stranger aside (decision 0281), naming the import
// that brings it in; the import under another name does, and the next start
// sets nothing aside. Every record, reference, declaration and scope reads
// as it did, the function and the trigger run, and no record spells the old
// name.
func TestAnArchiveSetAsideForItsNameImportsUnderAnother(t *testing.T) {
	label := testdb.RepositoryLabel(t)
	from, to := label+".example.com", label+"-bo.example.com"
	key := testenv.MintCredentialKey()

	a := testenv.Start(t, testenv.WithRepository(from), testenv.WithDataRoot(t.TempDir()), testenv.WithCredentialKey(key))
	a.ApplyVocabularyYAML(notesVocabulary(from))
	a.MustJSON(http.MethodPut, "/api/v1/substrate.reamde.dev/core/trigger/echo-notes", map[string]any{
		"properties": map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{from + "/notes/note"}}},
			"callable": "substrate.reamde.dev/core/function/" + from + "/notes/echo",
		},
	}, nil)
	first := vocabulary.DerivedMintedID("roundtrip", "first")
	if status, body := a.Do(http.MethodPut, "/api/v1/"+from+"/notes/note/"+first, map[string]any{
		"properties": map[string]any{"text": "first"},
	}); status != http.StatusCreated {
		t.Fatalf("write a note: %d %s", status, body)
	}
	if status, body := a.Do(http.MethodPut, "/api/v1/~/notes/note/"+vocabulary.DerivedMintedID("roundtrip", "second"), map[string]any{
		"properties": map[string]any{"text": "second", "related": map[string]any{"ref": from + "/notes/note/" + first}, "source": from + "/notes/note/" + first},
	}); status != http.StatusCreated {
		t.Fatalf("write a note pointing at the first: %d %s", status, body)
	}
	a.MustCallFunction("~/notes/stamp", map[string]any{"n": "1"})
	ds, err := a.Service.Dataset(t.Context(), from)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(t.Context()); err != nil {
		t.Fatalf("the trigger pass: %v", err)
	}
	_, appToken, err := ds.MintToken(t.Context(), substrate.MintTokenInput{Label: "an app", Scopes: []string{"read:" + from + "/notes/*"}})
	if err != nil {
		t.Fatalf("mint an app token: %v", err)
	}
	notes, echoes := recordsOf(t, a, a.Token, "~/notes/note"), recordsOf(t, a, a.Token, "~/notes/echo")
	if len(notes) != 3 || len(echoes) != 3 {
		t.Fatalf("repository %s holds %d notes and %d echoes, want 3 of each", from, len(notes), len(echoes))
	}
	declared := declarationOf(t, a, "~/notes/note")
	hands := actorIDsOf(t, a.DSN, ds.Repository().ID)
	for _, hand := range []substrate.Actor{substrate.FunctionActor("~", "notes", "stamp"), substrate.FunctionActor("~", "notes", "echo")} {
		if !slices.Contains(hands, vocabulary.ActorID(hand)) {
			t.Fatalf("%s holds no actor record of %s: %v", from, hand, hands)
		}
	}
	status, export, _ := a.DoRaw(http.MethodGet, "/api/v1/export", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("export: %d %s", status, export)
	}
	a.Stop()

	// Another server, whose own repository has the old name.
	b := newHolderOf(t, from, key)
	id, archiveID := untarArchive(t, export, filepath.Join(b.root, "archive"))
	var logs lockedBuffer
	e := b.start(t, engine.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	b.serves(t, e)
	st := b.state(t, id)
	if st.State != engine.StateSetAside || st.Authority != from || !st.Directory {
		t.Fatalf("the stranger at the start: %+v, want set aside under %s with no row", st, from)
	}
	way := "`substratectl archive import " + id + " " + archiveID + " --name <name>`"
	if !strings.Contains(logs.String(), way) || !strings.Contains(logs.String(), "stage=restore") {
		t.Fatalf("the set-aside does not name the import %s at the restore:\n%s", way, logs.String())
	}
	op := e.Service.(engine.Operator)
	if _, err := op.ImportArchive(t.Context(), id, archiveID, ""); err == nil ||
		!strings.Contains(err.Error(), "another repository of that name") || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("an import under the name the server's own repository holds = %v, want refused, naming --name", err)
	}
	repo, err := op.ImportArchive(t.Context(), id, archiveID, to)
	if err != nil {
		t.Fatalf("import under %s: %v", to, err)
	}
	if repo.ID != id || repo.Authority != to {
		t.Fatalf("imported %s (%s), want %s (%s)", repo.Authority, repo.ID, to, id)
	}
	// The set-aside is the start's, and the repair is served after a
	// restart (decision 0281).
	if _, err := e.Service.Dataset(t.Context(), to); !errors.Is(err, substrate.ErrSetAside) {
		t.Fatalf("the import's own process opens it: %v, want set aside until a restart", err)
	}
	e.Stop()

	e = b.start(t)
	b.serves(t, e)
	if st := b.state(t, id); st.State != engine.StateServing || st.Authority != to || st.Directory {
		t.Fatalf("the imported repository at the next start: %+v, want serving under %s", st, to)
	}
	states, err := engine.RepositoryStates(t.Context(), b.db, b.root, b.store(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.State != engine.StateServing {
			t.Errorf("after the import a start leaves %+v, want every repository served", st)
		}
	}
	// The export carried its person's sign-in, whose name the holder's own
	// person holds here, so the restore left it out (decision 0274); the
	// operator gives it back under another name, the same identity, which
	// the tokens minted under the old name name.
	if _, err := e.Service.(engine.Operator).ResetUser(t.Context(), to, "a long enough password", to); err != nil {
		t.Fatalf("give the imported repository's person back as %s: %v", to, err)
	}
	s := e.WithToken(a.Token)
	if got := recordsOf(t, s, a.Token, "~/notes/note"); !reflect.DeepEqual(got, notes) {
		t.Fatalf("the notes under %s do not compare with the ones under %s:\n before %v\n after  %v", to, from, notes, got)
	}
	if got := recordsOf(t, s, a.Token, "~/notes/echo"); !reflect.DeepEqual(got, echoes) {
		t.Fatalf("the echoes under %s do not compare with the ones under %s:\n before %v\n after  %v", to, from, echoes, got)
	}
	if got := recordsOf(t, s, appToken, "~/notes/note"); !reflect.DeepEqual(got, notes) {
		t.Fatalf("the app token's scope, granted under %s, reads %v under %s, want every note", from, got, to)
	}
	if got := declarationOf(t, s, "~/notes/note"); !reflect.DeepEqual(got, declared) {
		t.Fatalf("the declaration under %s does not compare with the one under %s:\n before %v\n after  %v", to, from, declared, got)
	}
	s.MustCallFunction("~/notes/stamp", map[string]any{"n": "2"})
	moved, err := e.Service.Dataset(t.Context(), to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := moved.ProcessTriggers(t.Context()); err != nil {
		t.Fatalf("the trigger pass under %s: %v", to, err)
	}
	if n, m := len(recordsOf(t, s, a.Token, "~/notes/note")), len(recordsOf(t, s, a.Token, "~/notes/echo")); n != 4 || m != 4 {
		t.Fatalf("the function and the trigger under %s left %d notes and %d echoes, want 4 of each", to, n, m)
	}
	if status, body := s.Do(http.MethodGet, "/api/v1/"+to+"/notes/note/"+first, nil); status != http.StatusOK {
		t.Fatalf("the new name is not a spelling of ~: %d %s", status, body)
	}
	if status, _ := s.Do(http.MethodGet, "/api/v1/"+from+"/notes/note/"+first, nil); status != http.StatusNotFound {
		t.Fatalf("the old name still reaches a record: %d", status)
	}
	// The hands are records whose ids are derived from `~`: the import
	// leaves every one of them as it was.
	if got := actorIDsOf(t, b.dsn, id); !slices.Equal(got, hands) {
		t.Fatalf("the actor records under %s are %v, want %v as under %s", to, got, hands, from)
	}
	b.spellsNeither(t, id, from, to)
}

// actorIDsOf is the id of every live actor record repository id holds, in
// order.
func actorIDsOf(t *testing.T, dsn, id string) []string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(t.Context(), `SELECT id FROM records WHERE repository = $1 AND kind = 'substrate.reamde.dev/core/actor' AND deleted_at IS NULL ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// An import under another name that leaves its secrets behind reads the
// operator's listing (SUBSTRATE_IMPORT_WITHOUT_SECRETS) by the name it takes
// or by its id: the name its archive gave is another repository's here. A
// listing that names only the old name refuses it, and changes nothing.
func TestAnImportUnderAnotherNameIsListedByTheNewNameOrTheID(t *testing.T) {
	label := testdb.RepositoryLabel(t)
	from, to := label+".example.com", label+"-bo.example.com"
	a := testenv.Start(t, testenv.WithRepository(from), testenv.WithDataRoot(t.TempDir()), testenv.WithCredentialKey(testenv.MintCredentialKey()))
	status, export, _ := a.DoRaw(http.MethodGet, "/api/v1/export", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("export: %d %s", status, export)
	}
	a.Stop()

	byName := newHolderOf(t, from, testenv.MintCredentialKey())
	id, archiveID := untarArchive(t, export, filepath.Join(byName.root, "archive"))
	e := byName.start(t, engine.WithImportWithoutSecrets(from))
	if _, err := e.Service.(engine.Operator).ImportArchive(t.Context(), id, archiveID, to); err == nil {
		t.Fatalf("an import under %s listed only by %s was taken", to, from)
	}
	if st := byName.state(t, id); st.State != engine.StateSetAside || !st.Directory {
		t.Fatalf("the refused import left %+v, want it set aside with no row", st)
	}
	e.Stop()
	for _, listed := range []struct {
		name  string
		holds *holder
		entry string
	}{
		{"the new name", byName, to},
		{"the id", newHolderOf(t, from, testenv.MintCredentialKey()), id},
	} {
		if listed.holds != byName {
			untarArchive(t, export, filepath.Join(listed.holds.root, "archive"))
		}
		e := listed.holds.start(t, engine.WithImportWithoutSecrets(listed.entry))
		if _, err := e.Service.(engine.Operator).ImportArchive(t.Context(), id, archiveID, to); err != nil {
			t.Fatalf("an import listed by %s: %v", listed.name, err)
		}
		e.Stop()
		e = listed.holds.start(t)
		listed.holds.serves(t, e)
		if st := listed.holds.state(t, id); st.State != engine.StateServing || st.Authority != to {
			t.Fatalf("listed by %s, the import reads %+v at the next start", listed.name, st)
		}
		e.Stop()
	}
}

// holder is a server whose own repository has the name of the one an
// archive put beside it holds.
type holder struct {
	dsn, root, key, name, token, contact string
	db                                   *sql.DB
}

// newHolderOf registers name on a server of its own, writes a record in it,
// and stops.
func newHolderOf(t *testing.T, name, key string) *holder {
	t.Helper()
	h := &holder{dsn: testdb.NewSchema(t), root: t.TempDir(), key: key, name: name}
	e := testenv.Start(t, testenv.WithDSN(h.dsn), testenv.WithDataRoot(h.root), testenv.WithCredentialKey(key), testenv.WithRepository(name))
	h.token = e.Token
	h.contact = "/api/v1/substrate.reamde.dev/core/contact/" + vocabulary.DerivedMintedID("holder", name)
	e.MustJSON(http.MethodPut, h.contact, map[string]any{"properties": map[string]any{"name": "the holder's own"}}, nil)
	e.Stop()
	db, err := sql.Open("pgx", h.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h.db = db
	return h
}

func (h *holder) start(t *testing.T, opts ...engine.Option) *testenv.Env {
	t.Helper()
	e := testenv.Start(t, testenv.WithDSN(h.dsn), testenv.WithDataRoot(h.root), testenv.WithCredentialKey(h.key),
		testenv.WithoutRegistration(), testenv.WithRepository(h.name), testenv.WithEngineOptions(opts...))
	e.Token = h.token
	return e
}

// serves holds the server's own repository to what it wrote before the stop.
func (h *holder) serves(t *testing.T, e *testenv.Env) {
	t.Helper()
	if status, raw := e.Do(http.MethodGet, h.contact, nil); status != http.StatusOK {
		t.Fatalf("%s reads its own record: %d %s", h.name, status, raw)
	}
}

func (h *holder) store(t *testing.T) archive.Store {
	t.Helper()
	d, err := archive.NewDir(filepath.Join(h.root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// state is the operator's list's line for repository id.
func (h *holder) state(t *testing.T, id string) engine.RepositoryState {
	t.Helper()
	states, err := engine.RepositoryStates(t.Context(), h.db, h.root, h.store(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.ID == id {
			return st
		}
	}
	t.Fatalf("the operator's list does not name %s: %+v", id, states)
	return engine.RepositoryState{}
}

// spellsNeither holds repository id's records to its two names: none spells
// the old one, and only the two whose job is to record the name, the
// repository's own record and its first person's actor, spell the new one.
func (h *holder) spellsNeither(t *testing.T, id, old, current string) {
	t.Helper()
	rows, err := h.db.QueryContext(t.Context(), `SELECT kind, id, title || ' ' || props::text || ' ' || labels::text FROM records WHERE repository = $1 ORDER BY kind, id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var kind, rid, text string
		if err := rows.Scan(&kind, &rid, &text); err != nil {
			t.Fatal(err)
		}
		n++
		if strings.Contains(kind+" "+rid+" "+text, old) {
			t.Errorf("%s/%s spells the old name %s: %s", kind, rid, old, text)
		}
		if strings.Contains(kind+" "+rid+" "+text, current) && !recordsTheName[kind] && rid != vocabulary.ActorID(substrate.ActorPerson) {
			t.Errorf("%s/%s spells the name %s: %s", kind, rid, current, text)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatalf("repository %s holds no records", id)
	}
	var scopes string
	if err := h.db.QueryRowContext(t.Context(), `SELECT string_agg(props->>'scopes', ' ') FROM records WHERE repository = $1 AND kind = 'substrate.reamde.dev/core/token'`, id).Scan(&scopes); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scopes, "read:~/notes/*") {
		t.Errorf("the tokens' scopes are %s, want the app's read:~/notes/*", scopes)
	}
}

// recordsTheName are the kinds whose one record records the repository's
// name, and so spells it.
var recordsTheName = map[string]bool{
	"substrate.reamde.dev/core/repository": true,
}

// notesVocabulary is a package of the repository's own: notes that refer to
// each other, their echoes, a function that writes a note and one a trigger
// runs to echo it, all written under the repository's name.
func notesVocabulary(name string) string {
	return `
kind: substrate.reamde.dev/core/package
metadata: {id: ` + name + `/notes}
data: {authority: ` + name + `, package: notes}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: ` + name + `/notes/note}
data:
  authority: ` + name + `
  package: notes
  names: {singular: note}
  properties:
    text: {type: string}
    related: {type: reference, kind: ` + name + `/notes/note}
    # A property the author named source, a key the dialect also has: the
    # grammar says it is a property, so its pin moves to ~ like any other.
    source: {type: reference, kind: ` + name + `/notes/note}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: ` + name + `/notes/echo}
data:
  authority: ` + name + `
  package: notes
  names: {singular: echo}
  properties:
    text: {type: string}
    note: {type: reference, kind: ` + name + `/notes/note}
---
kind: substrate.reamde.dev/core/function
metadata: {id: ` + name + `/notes/stamp}
data:
  authority: ` + name + `
  package: notes
  description: writes one note
  runtime: python
  arguments:
    - {name: n, type: string, required: true}
  permissions:
    writes: [` + name + `/notes/note]
  # A body's source is the owner's text, which nothing rewrites, so it names
  # the repository's own kinds by ~ and works under any name.
  source: |
    def main(input, host):
        n = input["args"]["n"]
        host.effects.put("~/notes/note", host.ids.minted("stamp", n),
                         properties={"text": "stamped " + n})
        return {"output": {"ok": True}}
---
kind: substrate.reamde.dev/core/function
metadata: {id: ` + name + `/notes/echo}
data:
  authority: ` + name + `
  package: notes
  description: echoes a note, so a trigger has something to deliver
  runtime: python
  permissions:
    writes: [` + name + `/notes/echo]
  source: |
    def main(input, host):
        env = input["envelope"]
        return {"effects": [{"action": "put", "kind": "~/notes/echo",
                             "id": "e-" + env["change"]["id"],
                             "properties": {"text": env["record"]["properties"]["text"],
                                            "note": {"ref": "~/notes/note/" + env["change"]["id"]}}}]}
`
}

// declarationOf is the declaration of kind as the server serves it.
func declarationOf(t *testing.T, e *testenv.Env, kind string) map[string]any {
	t.Helper()
	var rec substrate.Record
	e.MustJSON(http.MethodGet, "/api/v1/substrate.reamde.dev/core/kind/"+url.PathEscape(kind), nil, &rec)
	return rec.Properties
}

// recordsOf is every record of kind the token lists, by id: its kind and
// its properties.
func recordsOf(t *testing.T, e *testenv.Env, token, kind string) map[string]string {
	t.Helper()
	status, body := e.WithToken(token).Do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"kinds":["`+kind+`"]}`), nil)
	if status != http.StatusOK {
		t.Fatalf("list %s: %d %s", kind, status, body)
	}
	var page substrate.Page
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range page.Records {
		props, _ := json.Marshal(r.Properties)
		out[r.ID] = r.Kind + " " + string(props)
	}
	return out
}

// untarArchive unpacks an export into the archive at root, as an operator
// puts it there, and answers the repository and the archive it holds.
func untarArchive(t *testing.T, export []byte, root string) (string, string) {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(export))
	var id, archiveID string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read the export: %v", err)
		}
		path := filepath.Join(root, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(path, filepath.Clean(root)+string(os.PathSeparator)) {
			t.Fatalf("the export names %q, outside the archive", h.Name)
		}
		if parts := strings.Split(h.Name, "/"); len(parts) > 3 && parts[0] == changelogfile.RepositoriesDir && parts[2] != changelogfile.ManifestsSubdir {
			id, archiveID = parts[1], parts[2]
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if id == "" || archiveID == "" {
		t.Fatal("the export holds no archive")
	}
	return id, archiveID
}

// lockedBuffer is a log the server writes from its goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
