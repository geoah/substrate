package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ensureIndices materializes the `indices:` hints a type declares as partial
// indexes on the records table — filterable ≡ indexed ≡ declared.
//
// The statements run on the ADMIN pool, not the repository's: substrate_app
// owns nothing and may not create an index. The index itself is shared — one
// table, one index per declared hint, `repository` leading so it stays useful
// under the row level security predicate — so a second repository declaring
// the same type finds it already there.
//
// WHERE it runs from matters: each `CREATE INDEX` takes a SHARE lock on the
// records table, which every repository shares and every in-flight write
// holds ROW EXCLUSIVE on, so the statement waits for those writes and they
// wait for it. It runs ONCE PER PROCESS at Open, over the binary's shipped
// vocabulary, and on every vocabulary apply over the kinds of the packages
// the apply touches, before its transaction opens (vocabularywrite.go). It is
// NOT on the repository-open path: opening a repository declares nothing.
//
// AN INDEX IS NAMED FOR ITS ORDINAL, so the name alone cannot say whether the
// index behind it is the declaration's current definition: an apply creates
// its indexes before its transaction, a transaction that then fails leaves
// them, and a corrected retry that changes what the ordinal indexes would
// find the stale one "already there". Each index therefore carries its
// rendered statement as its COMMENT, and one whose comment differs (or is
// missing, as every index built before this rule is) is dropped and rebuilt
// in one statement group. Comparing our own rendering to our own rendering
// is exact; comparing it to pg_indexes.indexdef would mean normalizing
// Postgres's spelling of every expression.
func ensureIndices(ctx context.Context, admin *sql.DB, types []*vocabulary.Kind) error {
	stmts, err := indexStatements(types)
	if err != nil {
		return err
	}
	for _, s := range stmts {
		var exists bool
		var have sql.NullString
		if err := admin.QueryRowContext(ctx,
			`SELECT to_regclass($1) IS NOT NULL, obj_description(to_regclass($1), 'pg_class')`, s.name,
		).Scan(&exists, &have); err != nil {
			return fmt.Errorf("substrate/engine: inspect index for %s: %w", s.kind, err)
		}
		if exists && have.Valid && have.String == s.stmt {
			continue
		}
		if err := s.rebuild(ctx, admin, exists); err != nil {
			return fmt.Errorf("substrate/engine: create index for %s: %w", s.kind, err)
		}
	}
	return nil
}

type indexStmt struct {
	kind, name, stmt string
}

// rebuild drops the stale index when one exists, creates the declared one and
// stamps its statement as the comment, in one transaction on the admin pool.
func (s indexStmt) rebuild(ctx context.Context, admin *sql.DB, exists bool) error {
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if exists {
		if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS `+s.name); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.stmt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `COMMENT ON INDEX `+s.name+` IS `+sqlLiteral(s.stmt)); err != nil {
		return err
	}
	return tx.Commit()
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
			name := "idx_" + derivedID(t.Identity, strconv.Itoa(i))
			stmts = append(stmts, indexStmt{kind: t.Identity, name: name, stmt: `CREATE INDEX IF NOT EXISTS ` + name +
				` ON records (repository, ` + strings.Join(exprs, ", ") + `) WHERE kind = ` + sqlLiteral(t.Identity)})
		}
	}
	return stmts, nil
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
