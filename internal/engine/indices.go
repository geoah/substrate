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
// WHERE it runs from matters: a plain `CREATE INDEX` locks the shared
// table for every repository, so it is taken exactly twice — ONCE PER PROCESS
// at Open, over the binary's shipped vocabulary, and on the schema-write path
// that admits a kind the process has not seen (a bundle install). It is
// NOT on the repository-open path: opening a repository declares nothing.
func (ds *dataset) ensureIndices(ctx context.Context) error {
	return ensureIndices(ctx, ds.svc.admin, ds.registry().Kinds())
}

func ensureIndices(ctx context.Context, admin *sql.DB, types []*vocabulary.Kind) error {
	for _, t := range types {
		for i, cols := range t.Indices {
			exprs := make([]string, 0, len(cols))
			for _, c := range cols {
				expr, err := indexExpr(t, c)
				if err != nil {
					return fmt.Errorf("substrate/engine: %s indices: %w", t.Identity, err)
				}
				exprs = append(exprs, expr)
			}
			if len(exprs) == 0 {
				continue
			}
			name := "idx_" + derivedID(t.Identity, strconv.Itoa(i))
			stmt := `CREATE INDEX IF NOT EXISTS ` + name + ` ON records (repository, ` + strings.Join(exprs, ", ") +
				`) WHERE kind = ` + sqlLiteral(t.Identity)
			if _, err := admin.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("substrate/engine: create index for %s: %w", t.Identity, err)
			}
		}
	}
	return nil
}

func indexExpr(t *vocabulary.Kind, name string) (string, error) {
	if col, err := columnFor(name); err != nil || col != "" {
		return col, err
	}
	// A bare name that is a STATE property indexes the states column: the
	// declaration says `properties`, the storage says `states` (MODEL §11.4),
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
	return `(props->>` + sqlLiteral(name) + `)`, nil
}

func sqlLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
