package vocabulary

import (
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// A PACKAGE STATES ITS STABILITY (docs/stability.md, decision record 0256).
// The header's `stability:` is the package's promise about change for the
// kinds it declares, one of substrate.Stabilities. There is no label on a
// kind: a reader has one place to look, the kind's package.
//
// The loader's half is the grammar: one of the three words or nothing. An
// absent key stores nothing, so a package that never said is told apart from
// one that said `experimental`; a reader treats both as the weakest promise.
// No write is refused over the value.
//
// The key is reserved by name, not tolerated by prefix (record 0020): it is in
// packageDataKeys alone, so an authority row and a bundle document refuse it,
// and a binary that did not know it refuses a document carrying it.

// parsePackageStability reads the header's `stability:`. The raw value is
// asserted rather than read through mstr, which would turn a number or a list
// into "" and so into the absent key, admitting a header its author did not
// write.
func (l *loader) parsePackageStability(where string, d map[string]any, g *Package) {
	raw, present := d["stability"]
	if !present {
		return
	}
	v, ok := raw.(string)
	if !ok {
		l.errf("%s: data.stability: must be one of %s — or drop the key and the package reads as %q",
			where, stabilityWords(), substrate.StabilityExperimental)
		return
	}
	if !slices.Contains(substrate.Stabilities, v) {
		l.errf("%s: data.stability: %q is not a stability — %s, or drop the key and the package reads as %q",
			where, v, stabilityWords(), substrate.StabilityExperimental)
		return
	}
	g.Stability = v
}

// stabilityWords spells the closed set as a refusal names it: `"stable",
// "beta" or "experimental"`.
func stabilityWords() string {
	quoted := make([]string, len(substrate.Stabilities))
	for i, s := range substrate.Stabilities {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	last := len(quoted) - 1
	return strings.Join(quoted[:last], ", ") + " or " + quoted[last]
}
