package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ensureIndices materializes the `indices:` hints a type declares as partial
// indexes on the records table — filterable ≡ indexed ≡ declared — and, for
// every scalar reference property a type declares, the one index its filter
// by pointer walks (referenceIndexStatements), which no declaration names.
//
// The statements run on the ADMIN pool, not the repository's: substrate_app
// owns nothing and may not create an index. The index itself is shared — one
// table, one index per declared hint, `repository` leading so it stays useful
// under the row level security predicate — so a second repository declaring
// the same type finds it already there.
//
// It runs ONCE PER PROCESS at Open, over the binary's shipped vocabulary, and
// on every vocabulary apply over the kinds of the packages the apply touches,
// before its transaction opens (vocabularywrite.go). It is NOT on the
// repository-open path: opening a repository declares nothing.
//
// AN INDEX IS NAMED FOR ITS ORDINAL, so the name alone cannot say whether the
// index behind it is the declaration's current definition: an apply creates
// its indexes before its transaction, a transaction that then fails leaves
// them, and a corrected retry that changes what the ordinal indexes would
// find the stale one "already there". Each index therefore carries its
// rendered statement as its COMMENT, and one whose comment differs (or is
// missing, as every index built before this rule is) is rebuilt. Comparing
// our own rendering to our own rendering is exact; comparing it to
// pg_indexes.indexdef would mean normalizing Postgres's spelling of every
// expression.
//
// EVERY BUILD IS CONCURRENT (rebuild), because every repository shares the
// table: a plain DROP INDEX holds ACCESS EXCLUSIVE on records and a plain
// CREATE INDEX holds SHARE, each for the whole build, and either one stalls
// every repository's writes while it runs. Boot takes the same path as an
// apply, so a newer binary booting beside a running server does not stall
// that server. A concurrent build waits instead: for every write in flight
// on records, and then for every session whose snapshot is older than the
// build's last scan, a session blocked inside a lock call included. So a
// caller must not hold a lock that another session blocks on while it calls
// this, which is why lockAdvisory polls.
//
// progress, when set, is told each index before it is built and when the
// batch waits for another session's builds (indexProgress).
func ensureIndices(ctx context.Context, admin *sql.DB, types []*vocabulary.Kind, progress indexProgress) error {
	stmts, err := indexStatements(types)
	if err != nil {
		return err
	}
	// The build lock, taken at the first index that needs work and held to
	// the end of the batch. Every catalog read after it runs on its session.
	var conn *sql.Conn
	defer func() {
		if conn != nil {
			unlockIndexBuilds(ctx, conn)
		}
	}()
	for _, s := range stmts {
		var q queryRower = admin
		if conn != nil {
			q = conn
		}
		st, err := s.inspect(ctx, q)
		if err != nil {
			return fmt.Errorf("substrate/engine: inspect index for %s: %w", s.kind, err)
		}
		if st.current(s.stmt) && !st.leftover {
			continue
		}
		if conn == nil {
			if conn, err = lockIndexBuilds(ctx, admin, progress.waiting); err != nil {
				return fmt.Errorf("substrate/engine: index build lock for %s: %w", s.kind, err)
			}
		}
		if err := s.rebuild(ctx, conn, progress.building); err != nil {
			return fmt.Errorf("substrate/engine: create index for %s: %w", s.kind, err)
		}
	}
	return nil
}

// indexProgress is what ensureIndices reports while it works. building is
// told each index before its build, which on a large records table runs for
// as long as two scans of the table take. waiting is told once, when another
// session holds the build lock. Either may be nil.
type indexProgress struct {
	building func(kind, index string)
	waiting  func()
}

type indexStmt struct {
	// stmt is the rendered `CREATE INDEX IF NOT EXISTS` statement, stamped as
	// the index's comment. on is its tail from ` ON records`, which the
	// concurrent build runs under the scratch name.
	kind, name, stmt, on string
}

// The scratch names a rebuild uses beside an index's own name: the new
// definition is built under buildName, and the index it replaces waits under
// staleName for its drop. Each is the name plus a suffix derivedID never
// produces, well inside Postgres's 63-byte identifier limit.
// scratchIndexPattern matches both, for every kind at once (an index name is
// `idx_` and a 12-character derivedID).
func (s indexStmt) buildName() string { return s.name + "_new" }
func (s indexStmt) staleName() string { return s.name + "_old" }

const scratchIndexPattern = `^idx_[a-z2-7]{12}_(new|old)$`

// sweepScratchIndexes drops every scratch index on records, whichever kind
// it was built for. Boot runs it: a rebuild whose session died cannot drop
// what it left (dropFailedBuild), the rebuild of that kind finds the leftover
// only when the kind is applied again, and an INVALID index that writes of
// the kind maintain would otherwise outlive the process that made it. The
// list is read again under the build lock, because a build that another
// process runs holds that lock until its scratch name is gone.
func sweepScratchIndexes(ctx context.Context, admin *sql.DB) error {
	names, err := scratchIndexes(ctx, admin)
	if err != nil || len(names) == 0 {
		return err
	}
	conn, err := lockIndexBuilds(ctx, admin, nil)
	if err != nil {
		return err
	}
	defer unlockIndexBuilds(ctx, conn)
	if names, err = scratchIndexes(ctx, conn); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := conn.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+name); err != nil {
			return fmt.Errorf("substrate/engine: drop the scratch index %s: %w", name, err)
		}
	}
	return nil
}

func scratchIndexes(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
},
) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.relname FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE i.indrelid = to_regclass('records') AND c.relname ~ $1
		ORDER BY 1`, scratchIndexPattern)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: list scratch indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// indexState is what the catalog says about one declared index.
type indexState struct {
	exists, valid bool
	comment       sql.NullString
	// leftover is a scratch name still present: a build that failed, which
	// leaves an INVALID index that writes of the kind still maintain, or a
	// process that died between two steps of a rebuild.
	leftover bool
}

// current is an index that exists, is valid and carries stmt as its comment.
func (st indexState) current(stmt string) bool {
	return st.exists && st.valid && st.comment.Valid && st.comment.String == stmt
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s indexStmt) inspect(ctx context.Context, q queryRower) (indexState, error) {
	var st indexState
	err := q.QueryRowContext(ctx, `
		SELECT i.indexrelid IS NOT NULL, coalesce(i.indisvalid, false),
		       obj_description(c.oid, 'pg_class'),
		       to_regclass($2) IS NOT NULL OR to_regclass($3) IS NOT NULL
		FROM (SELECT to_regclass($1)::oid AS oid) c
		LEFT JOIN pg_index i ON i.indexrelid = c.oid`,
		s.name, s.buildName(), s.staleName(),
	).Scan(&st.exists, &st.valid, &st.comment, &st.leftover)
	return st, err
}

// rebuild brings one index to its declared definition on conn, which holds
// the build lock, and takes no lock on records that a read or a write waits
// for. The new definition is built CONCURRENTLY under the scratch name,
// outside any transaction, because Postgres refuses a concurrent build inside
// one. One short transaction then renames the old index away, renames the
// new one into place and stamps the comment. A rename takes SHARE UPDATE
// EXCLUSIVE on the index alone, which no read or write of records conflicts
// with, and another session reads either the old name and comment or the new
// ones. The old index is dropped CONCURRENTLY after the commit. A build that
// fails drops its scratch index before it returns (dropFailedBuild). What a
// session that died leaves behind, the next ensureIndices over the kind
// finds (indexState.leftover) and drops before it builds again.
func (s indexStmt) rebuild(ctx context.Context, conn *sql.Conn, building func(kind, index string)) error {
	// Read again under the lock: another session may have built this same
	// definition while this one waited.
	st, err := s.inspect(ctx, conn)
	if err != nil {
		return err
	}
	if st.leftover {
		for _, name := range []string{s.buildName(), s.staleName()} {
			if _, err := conn.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+name); err != nil {
				return err
			}
		}
	}
	if st.current(s.stmt) {
		return nil
	}
	if building != nil {
		building(s.kind, s.name)
	}
	if err := s.buildAndSwap(ctx, conn, st.exists); err != nil {
		s.dropFailedBuild(ctx, conn)
		return err
	}
	if st.exists {
		if _, err := conn.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+s.staleName()); err != nil {
			return err
		}
	}
	return nil
}

// buildAndSwap builds the declared definition under the scratch name and
// swaps it into place, the steps of rebuild a failure must not leave half
// done.
func (s indexStmt) buildAndSwap(ctx context.Context, conn *sql.Conn, exists bool) error {
	if _, err := conn.ExecContext(ctx, `CREATE INDEX CONCURRENTLY `+s.buildName()+s.on); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if exists {
		if _, err := tx.ExecContext(ctx, `ALTER INDEX `+s.name+` RENAME TO `+s.staleName()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `ALTER INDEX `+s.buildName()+` RENAME TO `+s.name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `COMMENT ON INDEX `+s.name+` IS `+sqlLiteral(s.stmt)); err != nil {
		return err
	}
	return tx.Commit()
}

// dropFailedBuild drops what a failed build left under the scratch name: an
// INVALID index, which writes of the kind still maintain once the build got
// past its first scan, or a built one the swap never renamed. It runs on the
// build's own session, which still holds the build lock, under the caller's
// context, so it waits for other transactions on records as long as the
// build itself could have. A session that died with its build, or a context
// that ended, cannot run it, and that error is not the caller's: the next
// ensureIndices over the kind and the next boot drop the leftover
// (indexState.leftover, sweepScratchIndexes).
func (s indexStmt) dropFailedBuild(ctx context.Context, conn *sql.Conn) {
	_, _ = conn.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+s.buildName())
}

// indexBuildLockKeySQL is the advisory-lock key every index build on this
// schema's records table takes, composed the way advisoryKeySQL (identity.go)
// is. ONE key for the table, not one per index: a concurrent build blocked on
// the table lock another concurrent build of the same table holds keeps its
// snapshot while it waits, the holder waits out that snapshot before it
// finishes, and Postgres ends the cycle by failing one of them, which leaves
// its index INVALID. Postgres runs two builds of one table one at a time in
// any case, so one key costs no parallelism, and it is also what makes two
// repositories building the same index end with one.
const indexBuildLockKeySQL = `hashtext(current_schema() || '|index|records')::bigint`

// lockIndexBuilds takes the build lock on an admin connection of its own and
// returns the connection holding it. The lock is session-level because the
// concurrent build runs outside any transaction, and the connection is pinned
// because a session lock released from another pooled connection is a silent
// no-op.
func lockIndexBuilds(ctx context.Context, admin *sql.DB, waiting func()) (*sql.Conn, error) {
	conn, err := admin.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if err := lockAdvisory(ctx, conn, indexBuildLockKeySQL, nil, waiting); err != nil {
		// A round trip that failed may have been granted the lock first, so
		// the connection is discarded rather than pooled.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// unlockIndexBuilds releases the build lock on a context of its own, since
// the batch it ends may have failed because the caller's context ended, and
// discards a connection whose unlock failed rather than pool it still holding
// the lock.
func unlockIndexBuilds(ctx context.Context, conn *sql.Conn) {
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var released bool
	if err := conn.QueryRowContext(unlockCtx, `SELECT pg_advisory_unlock(`+indexBuildLockKeySQL+`)`).Scan(&released); err != nil || !released {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = conn.Close()
}

// lockAdvisory takes the session-level advisory lock keySQL names on conn,
// polling pg_try_advisory_lock until it is granted or ctx ends. It never
// blocks in pg_advisory_lock: a session blocked there keeps a snapshot for
// as long as it waits, and a concurrent index build waits out every such
// snapshot. A holder whose own work reaches a build (ensureIndices) would
// then wait on its own waiter forever, in a cycle Postgres cannot see,
// because the holder's half of it is a Go call. waiting, when set, is told
// once, at the first refusal.
func lockAdvisory(ctx context.Context, conn *sql.Conn, keySQL string, args []any, waiting func()) error {
	delay := 10 * time.Millisecond
	for {
		var got bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+keySQL+`)`, args...).Scan(&got); err != nil {
			return err
		}
		if got {
			return nil
		}
		if waiting != nil {
			waiting()
			waiting = nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(2*delay, 500*time.Millisecond)
	}
}

// indexStatements renders every declared index as its CREATE INDEX statement
// without running any. It is pure, so the apply's staging step runs it
// too (stageVocabularyBatch): an index the engine cannot build is then an
// admission failure the upgrade preview reports and the install refuses
// alike, and the execution above starts only once every definition rendered,
// so a refused definition leaves no index behind. Half of a kind's indexes
// created under a refusal would otherwise stay, and since the name is the
// declaration's ordinal, a corrected retry that changed the surviving
// ordinal's definition would find its stale index "already there".
func indexStatements(types []*vocabulary.Kind) ([]indexStmt, error) {
	var stmts []indexStmt
	for _, t := range types {
		declared := map[string]bool{}
		for i, cols := range t.Indices {
			exprs := make([]string, 0, len(cols))
			for _, c := range cols {
				expr, err := indexExpr(t, c)
				if err != nil {
					return nil, fmt.Errorf("substrate/engine: %s indices: %w", t.Identity, err)
				}
				exprs = append(exprs, expr)
			}
			if len(exprs) == 0 {
				continue
			}
			declared[strings.Join(exprs, ", ")] = true
			stmts = append(stmts, indexStatement(t, "idx_"+derivedID(t.Identity, strconv.Itoa(i)), exprs))
		}
		stmts = append(stmts, referenceIndexStatements(t, declared)...)
	}
	return stmts, nil
}

// indexStatement renders one partial index on the records table for a kind:
// `repository` leading so it serves under the row level security predicate,
// the kind's expressions after it, restricted to the kind's own rows.
func indexStatement(t *vocabulary.Kind, name string, exprs []string) indexStmt {
	on := ` ON records (repository, ` + strings.Join(exprs, ", ") + `) WHERE kind = ` + sqlLiteral(t.Identity)
	return indexStmt{kind: t.Identity, name: name, stmt: `CREATE INDEX IF NOT EXISTS ` + name + on, on: on}
}

// referenceIndexStatements renders the index every SCALAR reference property
// of a kind gets WITHOUT declaring it: one per (kind, property), on the path
// expression the reference filter compares (query.go condReference,
// `referencePathSQL = ANY($n::text[])`) and the reference ordering sorts by,
// partial on the kind like a declared one. A filter by pointer is the read a
// reference exists for — "the items of this order", "the runs of this
// trigger" — and left to the generic jsonb containment index the planner
// could not estimate it: a two-dozen-value `in` on a 100k-row repository is
// priced at tens of thousands of rows for a handful, and runs as a parallel
// seq scan measured in tens of seconds. A btree on the expression carries exact
// statistics and answers the read in one probe per value where nothing stands
// between the clause and the index. Under row level security something does:
// `->` is not leakproof, so a list by pointer reads refs instead
// (query.go condReference), and this index serves the reference ordering and
// the reads that cannot use refs.
//
// Named by (kind, "ref", property) — a different part list from a declared
// index's (kind, ordinal), so the two cannot collide — and reconciled by the
// same comment rule as a declared one (ensureIndices). A declaration that
// already indexes exactly this expression on its own is not doubled: the
// declared index IS this index, and `declared` is how the caller says so.
// Repeated and keyed references hold lists and maps, which the expression
// does not read; their filter stays on the containment index.
func referenceIndexStatements(t *vocabulary.Kind, declared map[string]bool) []indexStmt {
	var stmts []indexStmt
	for _, name := range t.PropOrder {
		p := t.Props[name]
		if !scalarReference(p) {
			continue
		}
		expr := `(` + referencePathSQL("props", name) + `)`
		if declared[expr] {
			continue
		}
		stmts = append(stmts, indexStatement(t, "idx_"+derivedID(t.Identity, "ref", name), []string{expr}))
	}
	return stmts
}

func indexExpr(t *vocabulary.Kind, name string) (string, error) {
	if col, err := columnFor(name); err != nil || col != "" {
		return col, err
	}
	// A bare name that is a STATE property indexes the states column: the
	// declaration says `properties`, the storage says `states`,
	// and an index built against the wrong one silently indexes nothing.
	if _, ok := t.StateProp(name); ok {
		return `(states->>` + sqlLiteral(name) + `)`, nil
	}
	head, tail, dotted := strings.Cut(name, ".")
	if dotted {
		if !vocabulary.ValidCamel(tail) {
			return "", fmt.Errorf("%w: %q is not an indexable column", substrate.ErrValidation, name)
		}
		switch head {
		case "states":
			return `(states->>` + sqlLiteral(tail) + `)`, nil
		case "properties":
			return `(props->>` + sqlLiteral(tail) + `)`, nil
		case "labels":
			return `(labels->>` + sqlLiteral(tail) + `)`, nil
		}
		return "", fmt.Errorf("%w: %q is not an indexable column", substrate.ErrValidation, name)
	}
	if !vocabulary.ValidCamel(name) {
		return "", fmt.Errorf("%w: %q is not an indexable column", substrate.ErrValidation, name)
	}
	// A REFERENCE indexes the PATH it points at, not the value's JSON text. Its
	// stored value is the object `{ref: …}` (decision 0044), so `props->>` would
	// build an index on a serialized object that no query asks for: every read
	// of a reference goes through referencePathSQL, and an index keyed on a
	// different expression is one Postgres can never use. `kinds/` declares one
	// today, `run`'s `[trigger, status]`.
	if p, ok := t.Prop(name); ok && p.Datatype == vocabulary.DatatypeReference && !p.Repeated && !p.Keyed {
		return `(` + referencePathSQL("props", name) + `)`, nil
	}
	return `(props->>` + sqlLiteral(name) + `)`, nil
}

func sqlLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
