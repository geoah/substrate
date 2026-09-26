package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// A repository indexed under older rules is re-derived at its next open: the
// search finds what the new rules index, the version is recorded, and no
// record, version or changelog entry moves.
func TestAnOpenReindexesRowsIndexedUnderOlderRules(t *testing.T) {
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	root := t.TempDir()
	repo := testdb.Repository(t)

	svc := rmOpen(t, dsn, root)
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := enginetest.ImportVocabulary(ctx, ds, "people"); err != nil {
		t.Fatal(err)
	}
	const person = "samples.substrate.reamde.dev/people/person"
	before := mustPut(t, ds, owner, substrate.PutInput{
		Kind: person, ID: "ada",
		Properties: map[string]any{"name": "Ada", "emails": []any{"ada@inbox.example"}},
	})

	raw, err := engine.OpenScopedDB(dsn, repo, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	version := func() int {
		t.Helper()
		var v int
		if err := raw.QueryRowContext(ctx, `SELECT version FROM search_index`).Scan(&v); err != nil {
			t.Fatalf("read the search index version: %v", err)
		}
		return v
	}
	// A creation indexes under this binary's rules and says so, so its first
	// open has nothing to do.
	if v := version(); v != 2 {
		t.Fatalf("a fresh repository records search index version %d, want 2", v)
	}
	// Stand for a repository from before the variants: the row indexed as the
	// bands alone, and the version the rules had then.
	if _, err := raw.ExecContext(ctx, `
		UPDATE records SET fts = setweight(to_tsvector('english', title), 'A') ||
		                         setweight(to_tsvector('english', props->'emails'->>0), 'B')
		WHERE kind = $1 AND id = 'ada'`, person); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `UPDATE search_index SET version = 1`); err != nil {
		t.Fatal(err)
	}
	if hits := searchIDs(t, ds, "inbox"); len(hits) != 0 {
		t.Fatalf("the planted old index already finds the domain: %v", hits)
	}
	head := changelogHead(t, raw)
	_ = svc.Close()

	svc2 := rmOpen(t, dsn, root)
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if hits := searchIDs(t, ds2, "inbox"); len(hits) != 1 || hits[0] != "ada" {
		t.Fatalf("after the reopen, search inbox = %v, want [ada]", hits)
	}
	if v := version(); v != 2 {
		t.Fatalf("the reopen recorded search index version %d, want 2", v)
	}
	after, err := ds2.Get(ctx, person, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("the reindex moved the record: version %d -> %d, updatedAt %v -> %v",
			before.Version, after.Version, before.UpdatedAt, after.UpdatedAt)
	}
	if got := changelogHead(t, raw); got != head {
		t.Fatalf("the reindex appended to the changelog: head %d -> %d", head, got)
	}
}

func searchIDs(t *testing.T, ds substrate.Dataset, q string) []string {
	t.Helper()
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: q, Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	out := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.Record.ID)
	}
	return out
}
