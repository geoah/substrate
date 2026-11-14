package api

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// unitsRepository holds the unit graph and the kinds it is made of.
type unitsRepository struct{ substrate.Dataset }

func (unitsRepository) Kinds(context.Context) ([]substrate.KindInfo, error) {
	return []substrate.KindInfo{
		{Identity: vocabulary.CoreKind("unit")}, {Identity: vocabulary.CoreKind("conversion")}, {Identity: "x.example.com/notes/note"},
	}, nil
}

func (unitsRepository) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{Authority: "x.example.com"}
}

func (unitsRepository) Units(context.Context) (substrate.UnitTable, error) {
	return substrate.UnitTable{}, nil
}

func (unitsRepository) ConvertUnit(context.Context, substrate.UnitConvert) (substrate.UnitConversion, error) {
	return substrate.UnitConversion{}, nil
}

// A person reads the unit graph only where their rules read its units and
// conversions: the graph is those records.
func TestAPersonReadsTheUnitGraphOnlyWhereTheirRulesReadIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct {
		name  string
		rules []substrate.RoleRule
		reads bool
	}{
		{"a viewer", []substrate.RoleRule{{Verbs: []string{"read"}, Kinds: []string{"*"}}}, true},
		{"a role over notes alone", []substrate.RoleRule{{Verbs: []string{"read", "write"}, Kinds: []string{"x.example.com/notes/*"}}}, false},
	} {
		view := newPersonDataset(unitsRepository{}, rolesOf([]substrate.RoleRules{{ID: "held", Rules: c.rules}}, "x.example.com"))
		_, err := view.Units(ctx)
		_, cerr := view.ConvertUnit(ctx, substrate.UnitConvert{})
		if (err == nil) != c.reads || (cerr == nil) != c.reads {
			t.Errorf("%s: units %v, a conversion %v; want read %v", c.name, err, cerr, c.reads)
		}
		if !c.reads && !errors.Is(err, substrate.ErrNotFound) {
			t.Errorf("%s: units refused with %v, want a kind it does not read", c.name, err)
		}
	}
}
