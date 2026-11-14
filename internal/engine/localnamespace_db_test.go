package engine_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The repository's name never enters what it stores of its own (decision
// record 0259): after declaring a package under the full name, adding a
// sample, writing references in both spellings and minting an app token
// scoped by the full name, no row of an own kind, no declaration of an own
// package, no token's scope and no history line of any of them spells the
// name, an actor text included: a hand of the repository's own carries `~`
// (`bundle:~:tasks`).
func TestTheRepositorysNameNeverEntersWhatItStoresOfItsOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	name := testdb.Repository(t)
	if _, err := svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}

	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(name+"/notes", 0),
		vocabulary.KindManifest(name+"/notes", map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{
				"text":    map[string]any{"type": "string"},
				"related": map[string]any{"type": "reference", "kind": name + "/notes/note"},
			},
		}),
	}); err != nil {
		t.Fatalf("declare under the full name: %v", err)
	}
	cat, err := catalog.Load(catalog.Shipped()...)
	if err != nil {
		t.Fatalf("load the catalog: %v", err)
	}
	for _, id := range []string{"samples.substrate.reamde.dev/people", "samples.substrate.reamde.dev/scheduling", "samples.substrate.reamde.dev/tasks"} {
		if _, _, err := cat.Import(ctx, substrate.ActorPerson, id, ds); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	first, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: name + "/notes/note", Properties: map[string]any{"text": "one"}})
	if err != nil {
		t.Fatalf("write under the full name: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "~/notes/note", Properties: map[string]any{
		"text": "two", "related": map[string]any{"ref": name + "/notes/note/" + first.ID},
	}}); err != nil {
		t.Fatalf("write a reference under the full name: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: name + "/tasks/task", Properties: map[string]any{"name": "a task"}}); err != nil {
		t.Fatalf("write a task under the full name: %v", err)
	}
	if _, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "an app", Scopes: []string{"read:" + name + "/notes/*", "write:" + name + "/tasks/task"}}); err != nil {
		t.Fatalf("mint a token scoped by the full name: %v", err)
	}

	db := rawDB(t, dsn)
	id := ds.Repository().ID
	var found []string
	own := func(kind, recordID string) bool {
		return vocabulary.IsLocal(kind) || vocabulary.IsLocal(recordID) || kind == vocabulary.CoreKind("token")
	}
	rows := query(t, db, `SELECT kind, id, props::text FROM records WHERE repository = $1`, id)
	for _, r := range rows {
		if own(r[0], r[1]) && strings.Contains(r[0]+" "+r[1]+" "+r[2], name) {
			found = append(found, fmt.Sprintf("the row %s/%s: %s", r[0], r[1], r[2]))
		}
	}
	lines := query(t, db, `SELECT kind, record_id, payload::text FROM changelog WHERE repository = $1`, id)
	var ownLines int
	for _, l := range lines {
		if !own(l[0], l[1]) {
			continue
		}
		ownLines++
		if strings.Contains(l[0]+" "+l[1]+" "+l[2], name) {
			found = append(found, fmt.Sprintf("the line of %s/%s: %s", l[0], l[1], l[2]))
		}
	}
	repo, err := ds.Get(ctx, vocabulary.CoreKind("repository"), id)
	if err != nil || repo.Properties["authority"] != name {
		t.Errorf("the repository record says its name is %v (%v), want %s: outside a declaration the name is the name", repo.Properties["authority"], err, name)
	}
	if ownLines == 0 || len(rows) == 0 {
		t.Fatalf("read %d rows and %d lines of the repository's own, which proves nothing", len(rows), ownLines)
	}
	for _, f := range found {
		t.Errorf("the repository's name %s is stored in %s", name, f)
	}
}

func query(t *testing.T, db *sql.DB, q string, args ...any) [][]string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		vals := make([]string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A machinery record named with the repository's full name is the `~` one on
// a delete and a purge as on a put (decision record 0259): a setting written
// under either spelling deletes under either.
func TestAMachineryRecordDeletesUnderEitherSpelling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	name := testdb.Repository(t)
	if _, err := svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	setting := vocabulary.CoreKind("setting")
	for i, c := range []struct {
		written, deleted string
		purge            bool
	}{{name, name, false}, {vocabulary.Local, name, false}, {name, name, true}} {
		id := fmt.Sprintf("/notes/flag%d", i)
		rec, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: setting, ID: c.written + id, Properties: map[string]any{"type": "string", "value": "on"}})
		if err != nil || rec.ID != vocabulary.Local+id {
			t.Fatalf("write %s: %v, %v", c.written+id, rec, err)
		}
		if _, err := ds.Delete(ctx, substrate.ActorPerson, setting, c.deleted+id, substrate.DeleteInput{Purge: c.purge}); err != nil {
			t.Errorf("delete %s (purge %v): %v", c.deleted+id, c.purge, err)
			continue
		}
		got, err := ds.Get(ctx, setting, vocabulary.Local+id)
		switch {
		case c.purge && !errors.Is(err, substrate.ErrNotFound):
			t.Errorf("after purging %s, %s reads %v, %v", c.deleted+id, vocabulary.Local+id, got, err)
		case !c.purge && (err != nil || got.DeletedAt == nil):
			t.Errorf("after deleting %s, %s reads %v, %v, want its tombstone", c.deleted+id, vocabulary.Local+id, got, err)
		}
	}
}

// A mapping onto a kind of the repository's own is admitted whatever form
// its `to` takes, a name or a reference value by record path, in either
// spelling, and stored with no trace of the name (decision record 0259).
func TestAMappingOntoAnOwnKindIsAdmittedInEveryForm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	name := testdb.Repository(t)
	if _, err := svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	pkg := name + "/tasks"
	kindPath := func(authority string) string {
		return vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocKind), authority+"/tasks/task")
	}
	for i, to := range []any{
		vocabulary.Local + "/tasks/task",
		name + "/tasks/task",
		map[string]any{"ref": kindPath(name)},
		map[string]any{"ref": kindPath(vocabulary.Local)},
	} {
		mapping := fmt.Sprintf("entrytask%d", i)
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "task"}, map[string]any{
				"displayTemplate": "{name}",
				"properties":      map[string]any{"name": map[string]any{"type": "string"}},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": fmt.Sprintf("entry%d", i)}, map[string]any{
				"displayTemplate": "{name}",
				"properties":      map[string]any{"name": map[string]any{"type": "string"}},
			}),
			vocabulary.MappingManifest(pkg, mapping, map[string]any{
				"from": fmt.Sprintf("%s/entry%d", pkg, i), "to": to, "property": "task",
				"map": map[string]any{"name": map[string]any{"path": "name"}},
			}),
		}); err != nil {
			t.Errorf("a mapping whose to is %v: %v", to, err)
			continue
		}
		rec, err := ds.Get(ctx, vocabulary.CoreKind(vocabulary.DocRecordMapping), vocabulary.Local+"/tasks/"+mapping)
		if err != nil {
			t.Errorf("read the mapping whose to is %v: %v", to, err)
			continue
		}
		if raw, _ := json.Marshal(rec.Properties); strings.Contains(string(raw), name) {
			t.Errorf("the mapping whose to is %v is stored with the name: %s", to, raw)
		}
	}
}
