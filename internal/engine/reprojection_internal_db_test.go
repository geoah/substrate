package engine

// Every request on a repository waits for its open, so the boot upgrade of a
// reshaped data kind may do nothing per stored row: it re-derives the
// declaration rows alone and leaves the data kinds to a pass behind the
// open, which reads only the rows that carry a property whose declaration
// moved and resumes where it stopped (reprojection.go).

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

const (
	reprojProvider = "substrate.reamde.dev/llm/provider"
	// reprojPeer and reprojNote are the two properties the tests move on
	// llm/provider: a reference site and an `fts` flag, one per index.
	reprojPeer  = "    peer:\n      type: reference\n      kind: " + reprojProvider + "\n"
	reprojNote  = "    note:\n      type: string\n"
	reprojNoFTS = "      fts: false\n"
	// reprojLinkProps gives `peer` link data, which moves its reference shape
	// (appendReferenceShape lists the link properties) without narrowing it.
	reprojLinkProps = "      properties:\n        role:\n          type: string\n"
)

// reprojTree copies the shipped tree into a directory the test owns, so it
// can play binary N and binary N+1 against one database.
func reprojTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, kinds.Seed()); err != nil {
		t.Fatalf("copy the shipped tree: %v", err)
	}
	return dir
}

// patchProvider rewrites llm/provider's declaration in a copied tree: the
// text after `properties:` is replaced by props, and the kind's own version
// pinned, so the next open sees it as newer.
func patchProvider(t *testing.T, tree, props, version string) {
	t.Helper()
	path := filepath.Join(tree, "substrate.reamde.dev/llm/provider.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	const anchor = "  properties:\n"
	if !strings.Contains(doc, anchor) {
		t.Fatal("llm/provider no longer declares `properties:`")
	}
	doc = strings.Replace(doc, anchor, anchor+props, 1)
	doc = regexp.MustCompile(`\n  version: \S+\n`).ReplaceAllString(doc, "\n  version: "+version+"\n")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

// openReprojTree opens a service over a copied tree, with the test's log
// sink and statement tracer when given.
func openReprojTree(t *testing.T, dsn, root, tree string, opts ...Option) *service {
	t.Helper()
	all := append([]Option{WithDataRoot(root), WithKindsDir(tree)}, opts...)
	svc, err := OpenForTest(t, context.Background(), dsn, all...)
	if err != nil {
		t.Fatalf("open the substrate: %v", err)
	}
	return svc.(*service)
}

// plantProviders writes n llm/provider rows in transactions of 500. Every
// third row carries `note`, every fifth points `peer` at the hub, so a kind
// of n rows holds a known number carrying each moved property.
func plantProviders(t *testing.T, ds *dataset, n int, withNote, withPeer bool) (noted, peered []string) {
	t.Helper()
	ctx := context.Background()
	mustPutInternal(t, ds, substrate.PutInput{
		Kind: reprojProvider, ID: "hub", Properties: map[string]any{"label": "the hub", "wire": "openai"},
	})
	const batch = 500
	for from := 0; from < n; from += batch {
		if err := ds.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
			for i := from; i < min(from+batch, n); i++ {
				id := fmt.Sprintf("p%05d", i)
				props := map[string]any{"label": "provider " + id, "wire": "openai"}
				if withNote && i%3 == 0 {
					props["note"] = "zanzibar"
					noted = append(noted, id)
				}
				if withPeer && i%5 == 0 {
					props["peer"] = "hub"
					peered = append(peered, id)
				}
				if _, err := tx.put(substrate.PutInput{Kind: reprojProvider, ID: id, Properties: props}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("plant providers from %d: %v", from, err)
		}
	}
	return noted, peered
}

// tracedStatements is a pgx tracer that keeps every statement the pool
// sends with its arguments until take, so a test can count the rows a
// batched statement names and not only the statements.
type tracedStatements struct {
	mu      sync.Mutex
	entries []tracedStatement
}

type tracedStatement struct {
	sql  string
	args []any
}

func (l *tracedStatements) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, tracedStatement{sql: d.SQL, args: d.Args})
	return ctx
}

func (l *tracedStatements) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// take returns the statements sent since the last take and forgets them.
func (l *tracedStatements) take() []tracedStatement {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.entries
	l.entries = nil
	return out
}

// refsStatements counts the statements that re-derive refs rows, one row's
// or a page's.
func refsStatements(entries []tracedStatement) int {
	n := 0
	for _, e := range entries {
		if strings.Contains(e.sql, "DELETE FROM refs WHERE src_kind") || strings.Contains(e.sql, "INSERT INTO refs") {
			n++
		}
	}
	return n
}

// refsRowsRederived counts the rows whose refs rows the statements
// re-derived: the ids each page's DELETE names (syncRefsOfRows), plus one per
// single-row DELETE (syncRefs).
func refsRowsRederived(t *testing.T, entries []tracedStatement) int {
	t.Helper()
	n := 0
	for _, e := range entries {
		switch {
		case strings.Contains(e.sql, "DELETE FROM refs WHERE src_kind = $1 AND src = ANY"):
			if len(e.args) < 2 {
				t.Fatalf("a page DELETE of refs rows carries %d arguments", len(e.args))
			}
			switch ids := e.args[1].(type) {
			case []string:
				n += len(ids)
			case []any:
				n += len(ids)
			default:
				t.Fatalf("a page DELETE of refs rows names its ids as a %T", e.args[1])
			}
		case strings.Contains(e.sql, "DELETE FROM refs WHERE src_kind = $1 AND src = $2"):
			n++
		}
	}
	return n
}

// pendingReprojectionsOf reads the kinds a repository still owes a pass.
func pendingReprojectionsOf(t *testing.T, ds *dataset) []string {
	t.Helper()
	pending, err := ds.pendingReprojections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range pending {
		out = append(out, p.kind)
	}
	return out
}

func waitReprojection(t *testing.T, ds *dataset) {
	t.Helper()
	select {
	case <-IndexReprojectionDone(ds):
	case <-time.After(time.Minute):
		t.Fatal("the index re-derivation did not return within a minute")
	}
}

// foldSnapshotOf is the dataset's fold snapshot once the background pass
// owes nothing: a rebuild leaves the observed index to it.
func foldSnapshotOf(t *testing.T, ds *dataset) string {
	t.Helper()
	DrainIndexReprojection(t, ds)
	raw, err := ds.FoldSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A shipped kind gains a reference property and an `fts` flag, as
// llm/message did on 2026-10-06. No stored row carries either property, so
// the open does nothing per row: it sends no refs statement, its statement
// count does not grow with the rows, the pass behind it finds nothing to
// re-derive and clears its request, and the fold still matches a rebuild.
func TestABootUpgradeThatReshapesAKindDoesNoPerRecordWorkInTheOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	const rows = 3000

	treeN := reprojTree(t)
	svc1 := openReprojTree(t, dsn, root, treeN)
	if _, err := svc1.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d1, err := svc1.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	plantProviders(t, d1.(*dataset), rows, false, false)
	if err := svc1.Close(); err != nil {
		t.Fatal(err)
	}

	treeN1 := reprojTree(t)
	patchProvider(t, treeN1, reprojNote+reprojPeer, "99")
	statements := &tracedStatements{}
	var logs reprojLog
	svc2 := openReprojTree(t, dsn, root, treeN1,
		WithTestQueryTracer(statements), WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	defer func() { _ = svc2.Close() }()
	statements.take()
	opened := time.Now()
	d2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	took := time.Since(opened)
	inOpen := statements.take()
	ds2 := d2.(*dataset)
	if ty, ok := ds2.registry().ByIdentity(reprojProvider); !ok || ty.Props["peer"] == nil {
		t.Fatal("the upgrade did not land: llm/provider does not declare `peer`")
	}
	if n := refsStatements(inOpen); n != 0 {
		t.Errorf("the open sent %d refs statements for rows that carry no reference; want none", n)
	}
	// The upgrade projects a few declaration rows, each a handful of
	// statements; a walk of the kind would be two or more per row.
	if len(inOpen) > rows/4 {
		t.Errorf("the open sent %d statements over %d rows; it must not scale with the rows", len(inOpen), rows)
	}
	t.Logf("the open of %d rows took %s and sent %d statements", rows, took.Round(time.Millisecond), len(inOpen))

	waitReprojection(t, ds2)
	behind := statements.take()
	if n := refsStatements(behind); n != 0 {
		t.Errorf("the pass behind the open sent %d refs statements; no row carries `peer`", n)
	}
	if pending := pendingReprojectionsOf(t, ds2); len(pending) != 0 {
		t.Errorf("the pass left %v owed", pending)
	}
	out := logs.String()
	for _, want := range []string{
		`msg="substrate: upgrading a repository's shipped vocabulary from the embedded tree"`,
		"reshapedKinds=[" + reprojProvider + "]",
		`msg="substrate: upgraded a repository's shipped vocabulary from the embedded tree"`,
		"reprojectingBehindTheOpen=[" + reprojProvider + "]",
		`msg="substrate: re-deriving the indexes of the kinds a declaration change reshaped"`,
		`msg="substrate: re-derived the indexes of one kind"`,
		`msg="substrate: re-derived the indexes of the kinds a declaration change reshaped"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %s\n%s", want, out)
		}
	}
	before := foldSnapshotOf(t, ds2)
	if _, err := svc2.RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldSnapshotOf(t, ds2); before != after {
		t.Fatalf("the rebuilt fold is not the upgraded fold\n%s", firstDifferenceOf([]byte(before), []byte(after)))
	}
}

// A moved property that stored rows do carry is re-derived behind the open
// for those rows alone: the refs statements the pass sends are one row's
// worth per row carrying `peer`, live and tombstoned, and the search finds
// the newly indexed `note` once the pass has finished. The upgrade's own
// transaction still sends none.
func TestTheReprojectionBehindTheOpenRederivesTheRowsThatCarryAMovedProperty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	const rows = 600

	treeN := reprojTree(t)
	patchProvider(t, treeN, reprojNote+reprojNoFTS+reprojPeer, "98")
	svc1 := openReprojTree(t, dsn, root, treeN)
	if _, err := svc1.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d1, err := svc1.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds1 := d1.(*dataset)
	noted, peered := plantProviders(t, ds1, rows, true, true)
	// A tombstone holding `peer`: a rebuild derives its refs rows too.
	mustPutInternal(t, ds1, substrate.PutInput{
		Kind: reprojProvider, ID: "gone", Properties: map[string]any{"label": "gone", "wire": "openai", "peer": "hub"},
	})
	if _, err := ds1.Delete(ctx, substrate.ActorPerson, reprojProvider, "gone", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if got := indexedRows(t, ds1, "zanzibar"); got != 0 {
		t.Fatalf("binary N indexed %d notes it declares `fts: false`", got)
	}
	if err := svc1.Close(); err != nil {
		t.Fatal(err)
	}

	treeN1 := reprojTree(t)
	patchProvider(t, treeN1, reprojNote+reprojPeer+reprojLinkProps, "99")
	statements := &tracedStatements{}
	svc2 := openReprojTree(t, dsn, root, treeN1, WithTestQueryTracer(statements))
	defer func() { _ = svc2.Close() }()
	statements.take()
	d2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	ds2 := d2.(*dataset)
	inOpen := statements.take()
	if n := refsStatements(inOpen); n != 0 {
		t.Errorf("the open sent %d refs statements; the rows carrying `peer` re-derive behind it", n)
	}
	waitReprojection(t, ds2)
	behind := statements.take()
	// The refs rows of the peered rows and the tombstone re-derive, in one
	// DELETE and one INSERT per page, and not those of the rows that carry
	// `note` alone or neither property.
	carrying := len(peered) + 1
	if n := refsRowsRederived(t, behind); n != carrying {
		t.Errorf("the pass re-derived the refs rows of %d rows, want the %d carrying `peer`", n, carrying)
	}
	if n := refsStatements(behind); n > 4 {
		t.Errorf("the pass sent %d refs statements for %d rows; a page is one DELETE and one INSERT", n, carrying)
	}
	if pending := pendingReprojectionsOf(t, ds2); len(pending) != 0 {
		t.Errorf("the pass left %v owed", pending)
	}
	if got := indexedRows(t, ds2, "zanzibar"); got != len(noted) {
		t.Errorf("after the pass the index holds %d noted rows, want %d", got, len(noted))
	}
	var tombstoneRefs int
	if err := ds2.db.QueryRowContext(ctx, `SELECT count(*) FROM refs WHERE src_kind = $1 AND src = 'gone'`, reprojProvider).Scan(&tombstoneRefs); err != nil {
		t.Fatal(err)
	}
	if tombstoneRefs != 1 {
		t.Errorf("the tombstone holds %d refs rows after the pass, want its one", tombstoneRefs)
	}
	before := foldSnapshotOf(t, ds2)
	if _, err := svc2.RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldSnapshotOf(t, ds2); before != after {
		t.Fatalf("the rebuilt fold is not the upgraded fold\n%s", firstDifferenceOf([]byte(before), []byte(after)))
	}
}

// A request the pass did not finish before the process stopped is found by
// the next open, which resumes the pass after the id its last page recorded:
// the rows past it are re-derived, the rows before it are the pages that
// committed and are left alone, and the request is cleared.
func TestAnInterruptedReprojectionResumesAtTheNextOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)

	tree := reprojTree(t)
	patchProvider(t, tree, reprojPeer, "99")
	svc1 := openReprojTree(t, dsn, root, tree)
	if _, err := svc1.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d1, err := svc1.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds1 := d1.(*dataset)
	_, peered := plantProviders(t, ds1, 50, false, true)
	// Stand for a pass stopped halfway: the request is on file with the id
	// its last page ended at, and no row holds refs rows. The rows up to
	// that id are the pages that committed; the pass owes the rest.
	const endedAt = "p00025"
	if _, err := ds1.db.ExecContext(ctx, `DELETE FROM refs WHERE src_kind = $1`, reprojProvider); err != nil {
		t.Fatal(err)
	}
	if _, err := ds1.db.ExecContext(ctx, `
		INSERT INTO index_reprojections (kind, refs, refs_properties, fts, after_id)
		VALUES ($1, true, $2::text[], false, $3)`,
		reprojProvider, []string{"peer"}, endedAt); err != nil {
		t.Fatal(err)
	}
	var owed int
	for _, id := range peered {
		if id > endedAt {
			owed++
		}
	}
	if err := svc1.Close(); err != nil {
		t.Fatal(err)
	}

	svc2 := openReprojTree(t, dsn, root, tree)
	defer func() { _ = svc2.Close() }()
	d2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds2 := d2.(*dataset)
	waitReprojection(t, ds2)
	var restored int
	if err := ds2.db.QueryRowContext(ctx, `SELECT count(*) FROM refs WHERE src_kind = $1 AND property = 'peer'`, reprojProvider).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != owed {
		t.Errorf("the next open restored %d refs rows, want the %d after %s where the pass stopped", restored, owed, endedAt)
	}
	if pending := pendingReprojectionsOf(t, ds2); len(pending) != 0 {
		t.Errorf("the pass left %v owed", pending)
	}
}

// Migration 0021 put record ids in byte order, so a pass the locale's order
// stopped after `b` still owes `Zed`, which now sorts before `b`: resumed from
// its checkpoint it would skip `Zed` for good and delete the request. The
// migration starts every pending pass from the first row, so the next open
// re-derives every row the pass owed. The upgrade is the migration's own SQL,
// run over the staged checkpoint: its columns are byte order already, so the
// reset is what acts.
func TestTheByteOrderMigrationRestartsAPassTheOldOrderStopped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)

	tree := reprojTree(t)
	patchProvider(t, tree, reprojPeer, "99")
	svc1 := openReprojTree(t, dsn, root, tree)
	if _, err := svc1.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d1, err := svc1.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds1 := d1.(*dataset)
	mustPutInternal(t, ds1, substrate.PutInput{
		Kind: reprojProvider, ID: "hub", Properties: map[string]any{"label": "the hub", "wire": "openai"},
	})
	// The locale orders them a, b, Zed, zz; bytes order them Zed, a, b, zz.
	owed := []string{"a", "b", "Zed", "zz"}
	for _, id := range owed {
		mustPutInternal(t, ds1, substrate.PutInput{
			Kind: reprojProvider, ID: id, Properties: map[string]any{"label": "provider " + id, "wire": "openai", "peer": "hub"},
		})
	}
	if _, err := ds1.db.ExecContext(ctx, `DELETE FROM refs WHERE src_kind = $1`, reprojProvider); err != nil {
		t.Fatal(err)
	}
	// A pass under the locale's order committed the pages through `b`, but
	// stands here for one whose rows were never derived, so every row it
	// owes, and every row it had done, shows whether it ran.
	if _, err := ds1.db.ExecContext(ctx, `
		INSERT INTO index_reprojections (kind, refs, refs_properties, fts, after_id)
		VALUES ($1, true, $2::text[], false, 'b')`, reprojProvider, []string{"peer"}); err != nil {
		t.Fatal(err)
	}
	if err := svc1.Close(); err != nil {
		t.Fatal(err)
	}

	upgrade, err := migrationsFS.ReadFile("migrations/0021_identity_columns_byte_order.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDBOf(t, dsn).ExecContext(ctx, string(upgrade)); err != nil {
		t.Fatalf("run 0021 again: %v", err)
	}

	svc2 := openReprojTree(t, dsn, root, tree)
	defer func() { _ = svc2.Close() }()
	d2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds2 := d2.(*dataset)
	waitReprojection(t, ds2)
	rows, err := ds2.db.QueryContext(ctx, `SELECT src FROM refs WHERE src_kind = $1 AND property = 'peer' ORDER BY src`, reprojProvider)
	if err != nil {
		t.Fatal(err)
	}
	var rederived []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		rederived = append(rederived, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if strings.Join(rederived, ",") != "Zed,a,b,zz" {
		t.Errorf("after the upgrade the pass re-derived %v, want every row it owed: Zed, a, b, zz", rederived)
	}
	if pending := pendingReprojectionsOf(t, ds2); len(pending) != 0 {
		t.Errorf("the pass left %v owed", pending)
	}
}

// The migration does not run beside a server that holds a repository
// (TestTheByteOrderMigrationRefusesWhileAServerHoldsARepository). Behind that
// rule the request's fence still holds a pass whose process took no writer
// lease, which no server does and a test may: paused after `b` across the
// migration, it would resume after `b` in byte order and skip `Zed`. The
// migration moves the fence with the checkpoint, so that pass's advance and
// its delete match nothing, the restarted request survives it, and the next
// pass re-derives every row.
func TestTheByteOrderMigrationFencesAPassPausedAcrossIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)

	tree := reprojTree(t)
	patchProvider(t, tree, reprojPeer, "99")
	svc1 := openReprojTree(t, dsn, root, tree)
	if _, err := svc1.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d1, err := svc1.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds1 := d1.(*dataset)
	mustPutInternal(t, ds1, substrate.PutInput{
		Kind: reprojProvider, ID: "hub", Properties: map[string]any{"label": "the hub", "wire": "openai"},
	})
	for _, id := range []string{"a", "b", "Zed", "zz"} {
		mustPutInternal(t, ds1, substrate.PutInput{
			Kind: reprojProvider, ID: id, Properties: map[string]any{"label": "provider " + id, "wire": "openai", "peer": "hub"},
		})
	}
	if _, err := ds1.db.ExecContext(ctx, `DELETE FROM refs WHERE src_kind = $1`, reprojProvider); err != nil {
		t.Fatal(err)
	}
	if _, err := ds1.db.ExecContext(ctx, `
		INSERT INTO index_reprojections (kind, refs, refs_properties, fts, after_id)
		VALUES ($1, true, $2::text[], false, 'b')`, reprojProvider, []string{"peer"}); err != nil {
		t.Fatal(err)
	}
	if err := svc1.Close(); err != nil {
		t.Fatal(err)
	}

	// The next open's pass reads the request (after `b`) and pauses before
	// its first page, until the migration has committed.
	paused, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hook := func(ctx context.Context, kind string) error {
		if kind != reprojProvider {
			return nil
		}
		once.Do(func() {
			close(paused)
			<-release
		})
		return nil
	}
	svc2 := openReprojTree(t, dsn, root, tree, WithTestReprojectionHook(hook), WithTestSkipWriterLease())
	defer func() { _ = svc2.Close() }()
	d2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds2 := d2.(*dataset)
	<-paused
	upgrade, err := migrationsFS.ReadFile("migrations/0021_identity_columns_byte_order.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDBOf(t, dsn).ExecContext(ctx, string(upgrade)); err != nil {
		t.Fatalf("run 0021 beside the paused pass: %v", err)
	}
	close(release)
	waitReprojection(t, ds2)

	rows, err := ds2.db.QueryContext(ctx, `SELECT src FROM refs WHERE src_kind = $1 AND property = 'peer' ORDER BY src`, reprojProvider)
	if err != nil {
		t.Fatal(err)
	}
	var rederived []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		rederived = append(rederived, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if strings.Join(rederived, ",") != "Zed,a,b,zz" {
		t.Errorf("the rows re-derived around a pass paused across the migration: %v, want Zed, a, b, zz", rederived)
	}
	if pending := pendingReprojectionsOf(t, ds2); len(pending) != 0 {
		t.Errorf("the passes left %v owed", pending)
	}
}

// No server runs while migration 0021 does. A server before it, holding a
// repository, holds positions in memory that compare the columns it changes:
// its search reindex paused after `a, b` in the locale's order owes `Zed`, and
// its reprojection paused before its first page owes every row after `b`.
// Each would resume in byte order past `Zed` and record its pass as done. So
// the migration refuses while that server holds the repository's writer
// lease, names the repository, and changes nothing. Once the server is gone
// it runs, and the next server's passes start over and finish: every row
// re-derived, the search index recorded as done, nothing owed.
func TestTheByteOrderMigrationRefusesWhileAServerHoldsARepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)

	tree := reprojTree(t)
	patchProvider(t, tree, reprojPeer, "99")
	svc1 := openReprojTree(t, dsn, root, tree)
	if _, err := svc1.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d1, err := svc1.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds1 := d1.(*dataset)
	waitReprojection(t, ds1)
	mustPutInternal(t, ds1, substrate.PutInput{
		Kind: reprojProvider, ID: "hub", Properties: map[string]any{"label": "the hub", "wire": "openai"},
	})
	for _, id := range []string{"a", "b", "Zed", "zz"} {
		mustPutInternal(t, ds1, substrate.PutInput{
			Kind: reprojProvider, ID: id, Properties: map[string]any{"label": "provider " + id, "wire": "openai", "peer": "hub"},
		})
	}
	// Both passes owe every row: the refs are gone with a request after `b`,
	// and every row's `fts` is stale under a search index of an older version.
	for _, stmt := range []string{
		`DELETE FROM refs WHERE src_kind = '` + reprojProvider + `'`,
		`INSERT INTO index_reprojections (kind, refs, refs_properties, fts, after_id)
			VALUES ('` + reprojProvider + `', true, ARRAY['peer'], false, 'b')`,
		`UPDATE records SET fts = to_tsvector('english', 'plantedstale')`,
		`UPDATE search_index SET version = 1`,
	} {
		if _, err := ds1.db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc1.Close(); err != nil {
		t.Fatal(err)
	}
	raw := rawDBOf(t, dsn)
	restoreViews := stageTheLocaleOrder(t, raw)

	// The server before the migration: its reindex pauses before its second
	// page of providers, its reprojection before its first.
	reindexed, reprojected := make(chan struct{}), make(chan struct{})
	var pages atomic.Int32
	var once sync.Once
	svc2 := openReprojTree(t, dsn, root, tree,
		WithTestSearchReindex(2, func(ctx context.Context, kind string) error {
			if kind != reprojProvider || pages.Add(1) != 2 {
				return nil
			}
			close(reindexed)
			<-ctx.Done()
			return ctx.Err()
		}),
		WithTestReprojectionHook(func(ctx context.Context, kind string) error {
			if kind != reprojProvider {
				return nil
			}
			once.Do(func() { close(reprojected) })
			<-ctx.Done()
			return ctx.Err()
		}))
	defer func() { _ = svc2.Close() }()
	d2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	<-reindexed
	<-reprojected

	// The migration as the boot applies it: the runner's own step, its
	// transaction and its row in the ledger, with 0021 pending again.
	var byteOrder migration
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.Version == 21 {
			byteOrder = m
		}
	}
	apply := func() error {
		conn, err := raw.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		return applyMigration(ctx, conn, byteOrder)
	}
	if _, err := raw.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 21`); err != nil {
		t.Fatal(err)
	}
	checkpoint := func() string {
		var after string
		var requested time.Time
		if err := d2.(*dataset).db.QueryRowContext(ctx, `SELECT after_id, requested_at FROM index_reprojections WHERE kind = $1`,
			reprojProvider).Scan(&after, &requested); err != nil {
			t.Fatal(err)
		}
		return after + " " + requested.UTC().Format(time.RFC3339Nano)
	}
	ledger := func() string {
		rows, err := raw.QueryContext(ctx, `SELECT version, sha256 FROM schema_migrations ORDER BY version`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var v int
			var sum string
			if err := rows.Scan(&v, &sum); err != nil {
				t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("%d:%s", v, sum))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return strings.Join(out, ",")
	}
	checkpointBefore, ledgerBefore := checkpoint(), ledger()
	if err := apply(); err == nil || !strings.Contains(err.Error(), "stop every running server") {
		t.Fatalf("0021 beside a server that holds the repository: %v, want it refused", err)
	}
	var collation string
	if err := raw.QueryRowContext(ctx, `
		SELECT c.collname FROM pg_attribute a JOIN pg_collation c ON c.oid = a.attcollation
		WHERE a.attrelid = 'records'::regclass AND a.attname = 'id'`).Scan(&collation); err != nil || collation != "default" {
		t.Errorf("after the refusal records.id sorts by %q (%v), want the database's own collation still", collation, err)
	}
	if got := checkpoint(); got != checkpointBefore || !strings.HasPrefix(got, "b ") {
		t.Errorf("after the refusal the reprojection's checkpoint is %q, want it as it was, %q", got, checkpointBefore)
	}
	if got := ledger(); got != ledgerBefore {
		t.Errorf("after the refusal the ledger of applied migrations is\n %s\nwant\n %s", got, ledgerBefore)
	}

	// The server is gone, its passes unfinished. A release before ids held
	// the lease under the repository's name, and that refuses too.
	if err := svc2.Close(); err != nil {
		t.Fatal(err)
	}
	named, err := raw.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var held bool
	if err := named.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+writerLeaseKeySQL+`)`, repo).Scan(&held); err != nil || !held {
		t.Fatalf("hold the lease under the name %s: %v, %v", repo, held, err)
	}
	if err := apply(); err == nil || !strings.Contains(err.Error(), "holds repository "+repo) {
		t.Errorf("0021 beside a lease held under the name: %v, want it refused", err)
	}
	if _, err := named.ExecContext(ctx, `SELECT pg_advisory_unlock(`+writerLeaseKeySQL+`)`, repo); err != nil {
		t.Fatal(err)
	}
	if err := named.Close(); err != nil {
		t.Fatal(err)
	}

	// With no server left the migration runs, and the next server's passes
	// finish what both owed.
	if err := apply(); err != nil {
		t.Fatalf("0021 once the server is gone: %v", err)
	}
	restoreViews()
	if got := ledger(); !strings.Contains(got, fmt.Sprintf("21:%s", byteOrder.SHA256)) {
		t.Errorf("after the migration the ledger is %s, want 0021 recorded", got)
	}
	svc3 := openReprojTree(t, dsn, root, tree)
	defer func() { _ = svc3.Close() }()
	d3, err := svc3.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds3 := d3.(*dataset)
	waitReindex(t, ds3)
	waitReprojection(t, ds3)
	if left := underivedRows(t, ds3); len(left) != 0 {
		t.Errorf("the search index left %v underived", left)
	}
	if v := searchIndexVersionOf(t, ds3); v != searchIndexVersion {
		t.Errorf("the search index is recorded at version %d, want %d", v, searchIndexVersion)
	}
	rows, err := ds3.db.QueryContext(ctx, `SELECT src FROM refs WHERE src_kind = $1 AND property = 'peer' ORDER BY src`, reprojProvider)
	if err != nil {
		t.Fatal(err)
	}
	var rederived []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		rederived = append(rederived, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if strings.Join(rederived, ",") != "Zed,a,b,zz" {
		t.Errorf("the rows re-derived after the migration: %v, want Zed, a, b, zz", rederived)
	}
	if pending := pendingReprojectionsOf(t, ds3); len(pending) != 0 {
		t.Errorf("the passes left %v owed", pending)
	}
}

// A row a write holds when its page comes is skipped and kept in memory for a
// retry after the kind's pages. A shutdown before that retry loses the list,
// so the page's recorded id must not pass the held row: the next open reads
// it again and re-derives its `fts` (issue #868).
func TestARowSkippedForALockIsRederivedAfterARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)

	tree := reprojTree(t)
	patchProvider(t, tree, reprojNote, "99")
	svc1 := openReprojTree(t, dsn, root, tree)
	if _, err := svc1.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d1, err := svc1.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds1 := d1.(*dataset)
	noted, _ := plantProviders(t, ds1, 7, true, false)
	const heldID = "p00003"
	if strings.Join(noted, ",") != "p00000,p00003,p00006" {
		t.Fatalf("planted notes on %v, want p00000, p00003 and p00006", noted)
	}
	// Stand for an `fts` derived under a declaration that did not index
	// `note`, and the request the upgrade to one that does writes.
	if _, err := ds1.db.ExecContext(ctx, `UPDATE records SET fts = ''::tsvector WHERE kind = $1`, reprojProvider); err != nil {
		t.Fatal(err)
	}
	if err := ds1.inRawTx(ctx, func(tx *txn) error {
		return tx.requestReprojections([]indexReprojection{{
			kind: reprojProvider, fts: indexFilter{moved: true, properties: []string{"note"}},
		}})
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := ds1.pendingReprojections(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending reprojections = %v, %v; want the one requested", pending, err)
	}

	lock, err := ds1.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err := lock.ExecContext(ctx, `SELECT 1 FROM records WHERE kind = $1 AND id = $2 FOR UPDATE`, reprojProvider, heldID); err != nil {
		t.Fatal(err)
	}
	// The first page commits, then the process stops before the held row's
	// retry.
	pass, cancel := context.WithCancel(ctx)
	defer cancel()
	redone, err := ds1.reprojectKind(pass, pending[0], func(int) { cancel() })
	if err == nil {
		t.Fatal("the interrupted pass finished the kind; want it stopped before the held row's retry")
	}
	stopped, err := ds1.pendingReprojections(ctx)
	if err != nil || len(stopped) != 1 {
		t.Fatalf("pending reprojections after the stop = %v, %v; want the one requested", stopped, err)
	}
	t.Logf("interrupted pass: redone=%d, persisted after_id=%q", redone, stopped[0].after)
	if stopped[0].after >= heldID {
		t.Errorf("the page recorded after_id %q, past the held row %s", stopped[0].after, heldID)
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := svc1.Close(); err != nil {
		t.Fatal(err)
	}

	svc2 := openReprojTree(t, dsn, root, tree)
	defer func() { _ = svc2.Close() }()
	d2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds2 := d2.(*dataset)
	waitReprojection(t, ds2)
	if pending := pendingReprojectionsOf(t, ds2); len(pending) != 0 {
		t.Errorf("the pass left %v owed", pending)
	}
	if got := indexedRows(t, ds2, "zanzibar"); got != len(noted) {
		t.Errorf("after the restart the index holds %d noted rows, want %d", got, len(noted))
	}
}

// reprojLog is a log sink the test reads while the service's own goroutines
// may still write to it.
type reprojLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *reprojLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *reprojLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// indexedRows counts the rows whose `fts` holds the word.
func indexedRows(t *testing.T, ds *dataset, word string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM records WHERE fts @@ to_tsquery('english', $1)`, word).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// kindMap is a kindLookup over hand-built kinds.
type kindMap map[string]*vocabulary.Kind

func (m kindMap) ByIdentity(ident string) (*vocabulary.Kind, bool) {
	ty, ok := m[ident]
	return ty, ok
}

// The loader sorts a kind's properties, so a document that only reorders
// them moves no shape and is no request at all; a property that becomes
// searchable alone asks for the rows carrying it. Were the order to differ
// between the two declarations (kindMap builds that by hand), a refs
// request would still be none, because deriveRefs sorts its rows, while the
// fts request would be every row, because ftsBands joins the bands in that
// order for every row.
func TestAReorderIsNoRefsRequestAndAnEveryRowFTSRequest(t *testing.T) {
	t.Parallel()
	const (
		kind   = "reorder.test.dev/pkg/thing"
		peer   = "    peer:\n      type: reference\n      kind: " + kind + "\n"
		first  = "    first:\n      type: string\n"
		last   = "    last:\n      type: string\n"
		note   = "    note:\n      type: string\n"
		header = "kind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + kind + "\ndata:\n  authority: reorder.test.dev\n  package: pkg\n  names:\n    singular: thing\n  displayTemplate: \"{title}\"\n  properties:\n"
	)
	registry := func(props string) *vocabulary.Registry {
		t.Helper()
		reg, err := vocabulary.LoadFS(fstest.MapFS{"pkg.yaml": {Data: []byte(
			"kind: substrate.reamde.dev/core/package\nmetadata:\n  id: reorder.test.dev/pkg\ndata:\n  authority: reorder.test.dev\n  package: pkg\n  version: 1\n---\n" +
				header + props)}})
		if err != nil {
			t.Fatal(err)
		}
		return reg
	}
	before := registry(first + last + peer + note + "      fts: false\n")
	kinds := map[string]bool{kind: true}

	set := reprojectionSet{}
	set.addMoved(before, registry(last+first+peer+note+"      fts: false\n"), kinds)
	if len(set) != 0 {
		t.Fatalf("a document reorder alone = %+v, want no request", set[kind])
	}
	set = reprojectionSet{}
	set.addMoved(before, registry(first+last+peer+note), kinds)
	if r := set[kind]; r == nil || r.refs.moved || !r.fts.moved || r.fts.every || strings.Join(r.fts.properties, ",") != "note" {
		t.Fatalf("a property becoming searchable = %+v, want the fts of the rows carrying note", r)
	}

	// The order itself, by hand: the same two searchable strings and one
	// reference, declared in the other order.
	str := func() *vocabulary.Property {
		return &vocabulary.Property{Datatype: vocabulary.DatatypeString, FTS: true}
	}
	ref := func() *vocabulary.Property { return &vocabulary.Property{Datatype: vocabulary.DatatypeReference} }
	a := kindMap{kind: {
		Identity: kind, PropOrder: []string{"first", "last", "peer"},
		Props: map[string]*vocabulary.Property{"first": str(), "last": str(), "peer": ref()},
	}}
	b := kindMap{kind: {
		Identity: kind, PropOrder: []string{"last", "first", "peer"},
		Props: map[string]*vocabulary.Property{"first": str(), "last": str(), "peer": ref()},
	}}
	set = reprojectionSet{}
	set.addMoved(a, b, kinds)
	if r := set[kind]; r == nil || r.refs.moved || !r.fts.moved || !r.fts.every {
		t.Fatalf("an order that differs = %+v, want no refs move and every row's fts", r)
	}
	// And an order that differs beside a property becoming searchable: still
	// every row, since the rows without it join their bands in the new
	// order too.
	b[kind].Props["note"] = str()
	b[kind].PropOrder = []string{"last", "first", "note", "peer"}
	a[kind].Props["note"] = &vocabulary.Property{Datatype: vocabulary.DatatypeString}
	a[kind].PropOrder = []string{"first", "last", "note", "peer"}
	set = reprojectionSet{}
	set.addMoved(a, b, kinds)
	if r := set[kind]; r == nil || r.refs.moved || !r.fts.moved || !r.fts.every {
		t.Fatalf("an order that differs beside a changed property = %+v, want every row's fts", r)
	}
}
