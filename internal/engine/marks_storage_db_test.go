package engine

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The marks' and the offers' keys sort in byte order whatever the database's
// locale, as every identity column does: the catalog says so of each one.
func TestTheMarksAndOffersKeysSortInByteOrder(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	db, err := sql.Open("pgx", MigratedDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for table, columns := range map[string][]string{
		"property_marks":  {"repository", "record_kind", "record_id", "property"},
		"property_offers": {"repository", "record_kind", "record_id", "property", "source"},
	} {
		for _, column := range columns {
			var collation sql.NullString
			if err := db.QueryRowContext(context.Background(), `
				SELECT collation_name FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
				table, column).Scan(&collation); err != nil {
				t.Fatalf("%s.%s: %v", table, column, err)
			}
			if collation.String != "C" {
				t.Errorf("%s.%s collates %q, want C", table, column, collation.String)
			}
		}
	}
}

// THE MARK'S TIER IS GONE FROM STORAGE (decision 0284): neither table holds a
// tier, a manager, the actor's spelling or a token, and the fold knows no
// `manager` effect, so nothing can write one. The writer's tier, which says
// what a hand may write, is step 16's and is held there.
func TestTheMarksStoreNoTier(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	db, err := sql.Open("pgx", MigratedDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.QueryContext(context.Background(), `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name IN ('property_marks', 'property_offers')
		  AND column_name IN ('tier', 'manager', 'actor', 'principal', 'token')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s stores %s", table, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	ds := newTierDataset(t)
	err = ds.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
		_, err := tx.foldOne(foldOp{Kind: "manager", Ref: typeTierProfile, ID: "p1", Property: "name"})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "does not know the effect") {
		t.Fatalf("the fold took a manager effect: %v", err)
	}
}

// The manager table's view names the person by the actor id every repository
// derives for it (0026): migration 0005 reads it before migration 9 writes the
// actor records of an imported history.
func TestTheManagersViewNamesThePersonByItsDerivedID(t *testing.T) {
	raw, err := os.ReadFile("migrations/0026_property_marks.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if id := actorID(substrate.ActorPerson); !strings.Contains(string(raw), "'"+id+"'") {
		t.Fatalf("0022's view does not name the person's actor id %s", id)
	}
}
