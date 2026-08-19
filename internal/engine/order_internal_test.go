package engine

import (
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// TestOrderExpr pins the orderBy grammar: the property spelling is the only
// thing concatenated into the ORDER BY text (indices.go builds its jsonb paths
// the same way), so the ValidCamel barrier and sqlLiteral quoting are the whole
// defense. Nothing else in the suite exercises a REFUSED property, so widening
// or deleting the barrier would otherwise leave the suite green while opening
// the concatenation (CodeQL go/sql-injection alert 4). An empty registry needs
// no database: no kind declares the names below, so a plain camelCase name
// falls to `props->>` and everything else is refused.
func TestOrderExpr(t *testing.T) {
	t.Parallel()
	ds := &dataset{reg: vocabulary.NewRegistry()}

	refused := []string{
		`x'; DROP TABLE records; --`, // the injection the quoting must survive
		`props->>'a'`,                // a raw SQL fragment, not a property name
		`created_at DESC, id`,        // a whole ORDER BY clause, not a property
		`a b`,                        // a space is not camelCase
		`A`,                          // an uppercase lead is not camelCase
	}
	for _, name := range refused {
		if _, err := ds.orderExpr(name); !errors.Is(err, substrate.ErrValidation) {
			t.Errorf("orderExpr(%q) = %v, want substrate.ErrValidation", name, err)
		}
	}

	// A plain camelCase name is the accepted grammar and orders the jsonb
	// property, quoted by sqlLiteral.
	if got, err := ds.orderExpr("priority"); err != nil || got != `props->>'priority'` {
		t.Errorf("orderExpr(%q) = %q, %v; want %q, nil", "priority", got, err, `props->>'priority'`)
	}
}
