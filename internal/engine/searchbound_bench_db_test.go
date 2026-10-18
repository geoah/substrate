package engine_test

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// TestLexicalSearchTimingOnALargeRepository seeds a repository with many
// prose-sized rows whose words are common and times the lexical arm over
// them: the shape of a personal repository full of mail and documents, where
// `flight | booking | confirmation` matches a large share of the rows. It
// seeds through SQL, beside the fold, because a hundred thousand writes
// through the changelog would cost minutes and the fixture is the fts column
// alone; nothing here reads the changelog.
//
// It runs only when SUBSTRATE_SEARCH_BENCH names the row count, and it is a
// measurement, not a CI check: the numbers go to the log.
func TestLexicalSearchTimingOnALargeRepository(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("SUBSTRATE_SEARCH_BENCH"))
	if n <= 0 {
		t.Skip("set SUBSTRATE_SEARCH_BENCH to a row count to time the lexical arm")
	}
	ctx := context.Background()
	_, ds, dsn := newDatasetWithDSN(t)
	const taskKind = "samples.substrate.reamde.dev/tasks/task"
	// One row through the write path, so the kind's row and stats exist.
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, ID: "ba286", Properties: map[string]any{
		"name": "Flight BA286 booking confirmation", "description": "Your flight BA286 to San Francisco is confirmed.",
	}})
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	start := time.Now()
	// About 300 words a row, every tenth row 4 300, from a skewed vocabulary of 20 000, plus the three
	// incident words at 4%, 8% and 12% of rows, created over a year.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO records (repository, kind, id, title, props, created_at, updated_at, fts)
		SELECT $1, $2, 'bench-' || g, d.name, jsonb_build_object('name', d.name, 'description', d.descr),
		       now() - (random() * interval '365 days'), now(),
		       setweight(to_tsvector('english', d.name), 'A') || setweight(to_tsvector('english', d.descr), 'C')
		FROM generate_series(1, $3) g
		CROSS JOIN LATERAL (
			SELECT 'note ' || g AS name,
			       (SELECT string_agg('w' || floor(power(random(), 3) * 20000)::int, ' ') FROM generate_series(1, 280 + g % 40 + CASE WHEN g % 10 = 0 THEN 4000 ELSE 0 END))
			       || CASE WHEN random() < 0.04 THEN ' flight' ELSE '' END
			       || CASE WHEN random() < 0.08 THEN ' booking' ELSE '' END
			       || CASE WHEN random() < 0.12 THEN ' confirmation' ELSE '' END AS descr
		) d`, testdb.Repository(t), taskKind, n); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE records`); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d rows in %s", n, time.Since(start).Round(time.Millisecond))
	for _, q := range []string{"flight booking confirmation", "flight", "BA286", "confirmation", "w1 w2 w3"} {
		for _, run := range []string{"cold", "warm"} {
			began := time.Now()
			res, err := ds.Search(ctx, substrate.SearchInput{Q: q, Mode: substrate.SearchLexical, K: 20})
			took := time.Since(began)
			if err != nil {
				t.Logf("%-30q %s: error after %s: %v", q, run, took.Round(time.Millisecond), err)
				continue
			}
			top := ""
			if len(res.Hits) > 0 {
				top = res.Hits[0].Record.ID
			}
			t.Logf("%-30q %s: %3d hits in %8s, top %s, capped %v",
				q, run, len(res.Hits), took.Round(time.Millisecond), top, res.Capped)
		}
	}
}
