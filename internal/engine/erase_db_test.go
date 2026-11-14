package engine_test

// THE ERASURE AND A MAPPING (decision 0305): a permanent delete of a mapped
// source erases the copy of its value a target holds, from the target's
// history too, and keeps the target's current state, whether or not a plain
// delete released the copy first.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAPermanentDeleteErasesAMappedTargetsCopyOfTheValue(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"purged at once", "deleted first, then purged"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			erasesAMappedCopy(t, route == "deleted first, then purged")
		})
	}
}

// erasesAMappedCopy maps a contact's name onto a person and purges the
// contact, after a plain delete where deletedFirst says so: the delete's
// tombstone has released the person's copy by the time of the purge.
func erasesAMappedCopy(t *testing.T, deletedFirst bool) {
	_, ds, dsn := newDatasetWithDSN(t)
	installPeopleSources(t, ds)
	ctx := context.Background()
	const surname = "Erasablemappedname"
	src, err := ds.Put(ctx, "google.connectors.substrate.reamde.dev/google/people", substrate.PutInput{
		Kind: typeGoogleContact,
		ID:   fid("erasable-c1"),
		Properties: map[string]any{
			"name":   map[string]any{"displayName": "Ada " + surname, "firstName": "Ada", "lastName": surname},
			"emails": []any{map[string]any{"value": "ada@example.org", "type": "home", "primary": true, "verified": true}},
			"etag":   "a1",
		},
	})
	if err != nil {
		t.Fatalf("contact: %v", err)
	}
	db := rawDB(t, dsn)
	holding := func() []string {
		t.Helper()
		rows, err := db.QueryContext(ctx, `
			SELECT table_name FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`)
		if err != nil {
			t.Fatal(err)
		}
		var tables []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, n)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, table := range tables {
			var n int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` x WHERE x::text LIKE '%' || $1 || '%'`, surname).Scan(&n); err != nil {
				t.Fatalf("scan %s: %v", table, err)
			}
			if n > 0 {
				out = append(out, table)
			}
		}
		return out
	}
	var copies int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE kind = $1 AND props::text LIKE '%' || $2 || '%'`, typePerson, surname).Scan(&copies); err != nil || copies == 0 {
		t.Fatalf("no person holds the contact's name (%v): the test proves nothing", err)
	}
	if deletedFirst {
		if _, err := ds.Delete(ctx, owner, typeGoogleContact, src.ID, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	deleted, err := ds.Delete(ctx, owner, typeGoogleContact, src.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if deleted.Erasure == nil || deleted.Erasure.Pending {
		t.Fatalf("the delete's erasure is %+v, want it done", deleted.Erasure)
	}
	if tables := holding(); len(tables) > 0 {
		t.Fatalf("after the permanent delete, the name is still in %v", tables)
	}
}
