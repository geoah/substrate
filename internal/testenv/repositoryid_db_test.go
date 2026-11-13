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
	"repositories.authority": "the name itself, unique on the substrate",
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
	if len(held) > 0 {
		t.Errorf("key columns holding the name %s:\n  %s", name, strings.Join(held, "\n  "))
	}
	if scopedByID == 0 || len(keys) < 30 {
		t.Fatalf("read %d key columns and found %d rows keyed by the id: the scan read nothing", len(keys), scopedByID)
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
}
