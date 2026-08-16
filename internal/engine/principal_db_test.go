package engine_test

// The PRINCIPAL (#102): the token id the door resolved from the bearer
// secret, stamped on every changelog entry a request appends and on every
// manager row it lands. The actor beside it is what the caller ASSERTED, so
// two tokens writing as `api` are one actor and two principals, and only the
// principal says which token wrote.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const principalTask = "tasks.substrate.reamde.dev/task"

// managerPrincipals reads the distinct principals the record's manager rows
// carry, so a test can say "every property this write landed names the token".
func managerPrincipals(t *testing.T, dsn, kind, id string) []string {
	t.Helper()
	rows, err := rawDB(t, dsn).Query(`
		SELECT DISTINCT principal FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 ORDER BY principal`, kind, id)
	if err != nil {
		t.Fatalf("read manager principals: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan manager principal: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read manager principals: %v", err)
	}
	return out
}

func lastPrincipal(t *testing.T, dsn string) string {
	t.Helper()
	var p string
	if err := rawDB(t, dsn).QueryRow(`SELECT principal FROM changelog ORDER BY seq DESC LIMIT 1`).Scan(&p); err != nil {
		t.Fatalf("read the head entry's principal: %v", err)
	}
	return p
}

func TestPrincipalStampsTheEntryAndItsManagerRows(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newChainDataset(t)
	first := substrate.WithPrincipal(context.Background(), "tok_first")

	task, err := ds.Put(first, owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "Ship it", "description": "with the token that wrote it"},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := lastPrincipal(t, dsn); got != "tok_first" {
		t.Fatalf("the entry's principal is %q, want the writing token's id", got)
	}
	if got := managerPrincipals(t, dsn, task.Kind, task.ID); len(got) != 1 || got[0] != "tok_first" {
		t.Fatalf("manager principals are %v, want every row on tok_first", got)
	}

	// A second token, the same asserted actor: the entry and the manager row
	// both move, which is the whole point — the actor cannot tell them apart.
	second := substrate.WithPrincipal(context.Background(), "tok_second")
	if _, err := ds.Patch(second, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Ship it now"},
	}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got := lastPrincipal(t, dsn); got != "tok_second" {
		t.Fatalf("the second entry's principal is %q, want tok_second", got)
	}
	var namePrincipal string
	if err := rawDB(t, dsn).QueryRow(`
		SELECT principal FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 AND property = 'name'`, task.Kind, task.ID).Scan(&namePrincipal); err != nil {
		t.Fatalf("read the name manager: %v", err)
	}
	if namePrincipal != "tok_second" {
		t.Fatalf("the name manager still names %q after tok_second wrote it", namePrincipal)
	}

	// The principal is hashed like every other column, so the chain has to
	// still verify over the entries that now carry one.
	if report := mustVerify(t, svc, "geoah"); !report.OK {
		t.Fatalf("the chain does not verify with principals stamped: %+v", report.Findings)
	}
}

// A write no token stands behind — the seed, the boot upgrade, a background
// worker — carries the empty principal, and NOTHING writes the 'invalid'
// placeholder any more: it is migration 0005's mark on history alone.
func TestPrincipalIsEmptyWhereNoTokenWrote(t *testing.T) {
	t.Parallel()
	_, ds, dsn := newChainDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "no token here"},
	})
	if got := lastPrincipal(t, dsn); got != "" {
		t.Fatalf("an unauthenticated write stamped principal %q, want empty", got)
	}
	if got := managerPrincipals(t, dsn, task.Kind, task.ID); len(got) != 1 || got[0] != "" {
		t.Fatalf("manager principals are %v, want empty", got)
	}
	var placeholders int64
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM changelog WHERE principal = 'invalid'`).Scan(&placeholders); err != nil {
		t.Fatalf("count placeholders: %v", err)
	}
	if placeholders != 0 {
		t.Fatalf("%d entries carry the 'invalid' placeholder; no write path may stamp it", placeholders)
	}
}

// A split puts back exactly what its merge moved. A manager row another token
// has written since names the same actor and tier and a DIFFERENT principal,
// and that row records a write the split must not erase.
func TestSplitKeepsAManagerRowAnotherTokenWroteSince(t *testing.T) {
	t.Parallel()
	_, ds, dsn := newChainDataset(t)
	first := substrate.WithPrincipal(context.Background(), "tok_first")

	winner, err := ds.Put(first, owner, substrate.PutInput{
		Kind: principalTask, Properties: map[string]any{"name": "Winner"},
	})
	if err != nil {
		t.Fatalf("put winner: %v", err)
	}
	loser, err := ds.Put(substrate.WithPrincipal(context.Background(), "tok_second"), owner, substrate.PutInput{
		Kind: principalTask, Properties: map[string]any{"name": "Loser", "url": "https://example.com/1"},
	})
	if err != nil {
		t.Fatalf("put loser: %v", err)
	}
	merge, err := ds.Merge(first, owner, principalTask, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// A third token rewrites the migrated property under the same actor: the
	// manager row now stands on tok_third.
	third := substrate.WithPrincipal(context.Background(), "tok_third")
	if _, err := ds.Patch(third, owner, principalTask, winner.ID, substrate.PatchInput{
		Properties: map[string]any{"url": "https://example.com/2"},
	}); err != nil {
		t.Fatalf("patch url: %v", err)
	}

	if _, err := ds.Split(first, owner, merge.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	var principal string
	err = rawDB(t, dsn).QueryRow(`
		SELECT principal FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 AND property = 'url'`, winner.Kind, winner.ID).Scan(&principal)
	if err != nil {
		t.Fatalf("the split erased a manager row tok_third wrote after the merge: %v", err)
	}
	if principal != "tok_third" {
		t.Fatalf("the url manager names %q, want tok_third", principal)
	}
}

// The manager ledger is a fold of the changelog, principal included: replaying
// the history has to put the same token back on every row, through the
// per-property effect and through the merge's resync snapshot alike.
func TestRebuildReplaysTheManagerPrincipal(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newChainDataset(t)
	ctx := substrate.WithPrincipal(context.Background(), "tok_first")

	winner, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "Winner", "description": "keeps its own"},
	})
	if err != nil {
		t.Fatalf("put winner: %v", err)
	}
	loser, err := ds.Put(substrate.WithPrincipal(context.Background(), "tok_second"), owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "Loser", "url": "https://example.com/1"},
	})
	if err != nil {
		t.Fatalf("put loser: %v", err)
	}
	// The merge migrates the loser's manager rows where the winner has none,
	// and its resync snapshot is what a replay writes back.
	if _, err := ds.Merge(ctx, owner, principalTask, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := managerPrincipals(t, dsn, winner.Kind, winner.ID); len(got) != 2 {
		t.Fatalf("the merged winner's manager principals are %v, want both tokens", got)
	}

	before := foldOf(t, ds)
	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(context.Background(), "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(after) != string(before) {
		t.Fatal("the rebuilt fold does not match the one the writes left; the manager principal did not replay")
	}
}
