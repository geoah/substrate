package testenv_test

// NO TABLE OR PATH IS KEYED BY THE NAME (decision 0261). A repository's
// storage is keyed by its id: the `repository` column of every table, the
// primary key of `repositories`, the directory under the data root. Its name
// is what a person signs in with and the namespace of their own kinds, and it
// may appear in a key only where it is the name: the `authority` column of
// `repositories`, which makes it unique.
//
// The test reads every column of every unique index in the schema and every
// path under the data root, after a registration and a write, and fails on
// any that holds the name. It also holds the id to the places the name used
// to be, so a test that read nothing would fail rather than pass.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

// nameKeys are the key columns the name may fill: where it is the name.
var nameKeys = map[string]string{
	"repositories.authority":       "the name itself, unique on the substrate",
	"identities.name":              "the person's sign-in name, unique on the substrate, which a registration takes from the repository's (decision 0274)",
	"repository_former_names.name": "a name the repository had, held for 90 days after a rename (decision 0287)",
}

func TestNoTableOrPathIsKeyedByTheName(t *testing.T) {
	root := t.TempDir()
	e := testenv.Start(t, testenv.WithUser("keyed.example.com", "correct-horse-battery-staple"), testenv.WithDataRoot(root))
	name := e.Repository
	ctx := context.Background()
	// A write beyond the seed, so the changelog, the fold and the side tables
	// hold rows the registration did not.
	e.MustJSON(http.MethodPut, "/api/v1/substrate.reamde.dev/core/contact/"+vocabulary.DerivedMintedID("keyed", "ada"),
		map[string]any{"properties": map[string]any{"name": "Ada"}}, nil)
	// A secret, which fills the sealed store: a person's sign-in is the
	// server's (decision 0274), so the registration seals nothing there.
	e.MustJSON(http.MethodPut, "/api/v1/substrate.reamde.dev/llm/provider/keyed", map[string]any{"properties": map[string]any{
		"label": "keyed", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-keyed-value",
	}}, nil)
	// A blob, whose bytes are the one file the data root keeps per record.
	status, raw, _ := e.DoRaw(http.MethodPut, "/api/v1/blobs?name=keyed.txt", []byte("keyed bytes"), map[string]string{"Content-Type": "text/plain"})
	if status != http.StatusCreated {
		t.Fatalf("put blob: %d %s", status, raw)
	}
	var blob struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(raw, &blob); err != nil || blob.Digest == "" {
		t.Fatalf("blob upload answered %s: %v", raw, err)
	}

	db, err := sql.Open("pgx", e.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var id string
	if err := db.QueryRowContext(ctx, `SELECT id FROM repositories WHERE authority = $1`, name).Scan(&id); err != nil {
		t.Fatalf("the repository named %s has no row: %v", name, err)
	}
	if !vocabulary.ValidRepositoryID(id) || id == name {
		t.Errorf("the repository named %s is keyed %q, not by a minted id", name, id)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT c.relname, a.attname
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (i.indkey)
		WHERE i.indisunique AND n.nspname = current_schema() AND a.attnum > 0
		ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	var keys [][2]string
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, [2]string{table, column})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()

	var held []string
	scopedByID := 0
	for _, k := range keys {
		table, column := k[0], k[1]
		var n int
		q := fmt.Sprintf(`SELECT count(*) FROM %q WHERE %q::text = $1`, table, column)
		if err := db.QueryRowContext(ctx, q, name).Scan(&n); err != nil {
			t.Fatalf("read %s.%s: %v", table, column, err)
		}
		if _, ok := nameKeys[table+"."+column]; n > 0 && !ok {
			held = append(held, fmt.Sprintf("%s.%s (%d rows)", table, column, n))
		}
		if column == "repository" {
			if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %q WHERE repository = $1`, table), id).Scan(&n); err != nil {
				t.Fatalf("read %s.repository: %v", table, err)
			}
			scopedByID += n
		}
	}
	// Every table with a repository column, in a unique index or not: the
	// name in none of them, and the id in each table the writes above fill,
	// so no other table's rows stand in for one a writer skipped.
	scoped, err := db.QueryContext(ctx, `
		SELECT c.table_name FROM information_schema.columns c
		JOIN information_schema.tables tb ON tb.table_schema = c.table_schema AND tb.table_name = c.table_name
		WHERE c.table_schema = current_schema() AND c.column_name = 'repository' AND tb.table_type = 'BASE TABLE'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for scoped.Next() {
		var table string
		if err := scoped.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := scoped.Err(); err != nil {
		t.Fatal(err)
	}
	_ = scoped.Close()
	byID := map[string]int{}
	for _, table := range tables {
		var byName int
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %q WHERE repository = $1`, table), name).Scan(&byName); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		if byName > 0 {
			held = append(held, fmt.Sprintf("%s.repository (%d rows)", table, byName))
		}
		var n int
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %q WHERE repository = $1`, table), id).Scan(&n); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		byID[table] = n
	}
	if len(held) > 0 {
		t.Errorf("key columns holding the name %s:\n  %s", name, strings.Join(held, "\n  "))
	}
	if scopedByID == 0 || len(keys) < 30 || len(tables) < 20 {
		t.Fatalf("read %d key columns and %d scoped tables and found %d rows keyed by the id: the scan read nothing", len(keys), len(tables), scopedByID)
	}
	for _, table := range []string{
		"records", "changelog", "refs", "property_marks", "sealed", "search_index",
		"repository_migrations", "changelog_dialect", "vocabulary_dialect",
		"unit_graph", "unit_uses_index", "observed_index",
	} {
		if byID[table] == 0 {
			t.Errorf("%s holds no row keyed by the id %s: the writes this test makes fill it, so its writer was not exercised", table, id)
		}
	}

	var paths []string
	sawID := false
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, segment := range strings.Split(rel, string(os.PathSeparator)) {
			if segment == name || strings.HasPrefix(segment, name+".") {
				paths = append(paths, rel)
			}
			if segment == id {
				sawID = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) > 0 {
		t.Errorf("paths under the data root naming %s:\n  %s", name, strings.Join(paths, "\n  "))
	}
	if !sawID {
		t.Errorf("no path under the data root names the repository's id %s", id)
	}
	if _, err := os.Stat(filepath.Join(root, "repositories", id, "blobs", blob.Digest)); err != nil {
		t.Errorf("the blob's bytes are not under the id's directory: %v", err)
	}
}
