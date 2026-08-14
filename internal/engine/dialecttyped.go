package engine

// DIALECT 2: THE TYPED DECLARATIONS.
//
// Dialect 1 stored a declaration's authored content in one `definition` json
// blob, with a handful of projected mirror columns beside it (`name`, `plural`,
// an agent's `functions`/`subagents`). Dialect 2 stores the declaration's own
// data map as PROPERTIES — what the author wrote is what the row holds — and the
// mirrors are gone. This rung moves a repository's rows from the first shape to
// the second, and it is the one write path that can: the projection cannot,
// because it writes what the binary's registry holds and the registry is built
// FROM these rows.
//
// Three properties make it safe to run at every open:
//
//   - CONTENT-GATED AND IDEMPOTENT. Every row is folded, and a fold that moves
//     nothing records nothing: a repository whose rows are already typed writes
//     no row and appends no entry. A HALF-migrated store (some rows typed, some
//     not) migrates exactly the rest.
//   - IT WRITES THROUGH THE FOLD. The rows and one changelog entry per row land
//     together, under the actor `substrate`, so `rebuild-repository` replays the
//     rewrite instead of losing it. Nothing here touches `records` directly.
//   - THE STAMP RIDES THE SAME TRANSACTION. A downgrade is silent loss — an
//     older binary skips a declaration row without `definition`, opening a store
//     with every agent, function and bundle declaration quietly gone — so the
//     dialect stamp and the rewrite commit or roll back together, and the gate's
//     refusal (dialect.go) is what an older binary meets.
//
// The translation is the LOADER's: the stored blob is parsed by the current
// loader (which still admits every dialect-1 spelling and leaves the parsed data
// in the one canonical form, vocabulary/canonical.go), and the typed properties
// are then rendered by authorityDeclarations — the same derivation the projection
// writes. The rung and the projection cannot disagree about what a typed row is,
// because there is one function that says.

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// dialectTypedDeclarations is dialect 2's name in vocabulary_promotions.
const dialectTypedDeclarations = "typed-declarations"

// promoteTypedDeclarations rewrites every declaration row this repository holds
// into its typed form, in one transaction with the dialect stamp.
func (ds *dataset) promoteTypedDeclarations(ctx context.Context) error {
	// The translation, before any write: the stored rows parse through the
	// current loader, and what comes back is the typed property map of every
	// declaration each authority holds. A closure that no longer PARSES cannot be
	// translated — it is left exactly as it stands and quarantined a moment later
	// by loadStoredVocabulary, which is the same answer this repository already
	// got from the binary before it.
	built, unparsed, err := ds.storedAuthorities(ctx, nil)
	if err != nil {
		return err
	}
	for _, q := range unparsed {
		ds.svc.log.Error("substrate: leaving a stored closure at the old declaration shape — it no longer parses under this binary, so there is nothing to translate; re-install the bundle",
			"repository", ds.info.Name, "authority", q.name, "reason", q.reason)
	}
	if len(built) == 0 {
		return nil
	}
	// The rung's OWN candidate registry, built from the translated documents.
	// The live registry is empty at rung time (promote runs before
	// loadStoredVocabulary), so nothing here may resolve against it; this proves
	// the translated set still resolves whole — meta-kinds included, since core is
	// among the authorities rebuilt above and a repository whose meta-kinds do not
	// parse never reaches here (storedAuthorities returns core's failure as an
	// error).
	candidate := vocabulary.NewRegistry()
	if err := candidate.InstallAll(built); err != nil {
		ds.svc.log.Error("substrate: the translated declarations do not admit together — the rung writes the rows it can and the inadmissible closures quarantine at open",
			"repository", ds.info.Name, "reason", err.Error())
	}

	// What each row must BECOME, keyed by kind+id. The typed properties are the
	// projection's own derivation; the row's server-owned values (its version,
	// its origin, the quarantine marks, the bundle lifecycle bools) are kept as
	// stored, because the rung is a change of SHAPE and never of content.
	want := map[string][]declaration{}
	for _, g := range built {
		decls, err := authorityDeclarations(g)
		if err != nil {
			return fmt.Errorf("substrate/engine: translate %s: %w", g.Name, err)
		}
		want[g.Name] = decls
	}

	// Every typed row is held to the declaration it will live under: the
	// BINARY's, which is what the boot upgrade installs a moment later and what
	// this shape is defined by. A row the new declarations would refuse fails the
	// rung — and the open — before anything is written, rather than landing a
	// store no binary can read back.
	for _, aname := range sortedKeys(want) {
		for _, d := range want[aname] {
			ty, err := resolveKindIn(ds.svc.base, d.typ)
			if err != nil {
				return fmt.Errorf("substrate/engine: typed declaration %s %s: %w", d.short, d.id, err)
			}
			authored, _, _, err := splitProps(ty, d.props)
			if err != nil {
				return fmt.Errorf("substrate/engine: typed declaration %s %s: %w", d.short, d.id, err)
			}
			if _, err := coerceProps(ty, authored); err != nil {
				return fmt.Errorf("substrate/engine: typed declaration %s %s: %w", d.short, d.id, err)
			}
		}
	}

	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		moved := 0
		for _, aname := range sortedKeys(want) {
			for _, d := range want[aname] {
				changed, err := t.retypeDeclarationRow(d)
				if err != nil {
					return err
				}
				if changed {
					moved++
				}
			}
		}
		if moved > 0 {
			ds.svc.log.Info("substrate: migrated a repository's declaration rows to their typed shape",
				"repository", ds.info.Name, "rows", moved)
		}
		// LAST, and in this transaction: the stamp is what makes the rewrite
		// indivisible from the dialect it produced. The ladder stamps again after
		// the step returns, which is a no-op (the stamp only ever moves up).
		return t.stampDialect(2, dialectTypedDeclarations)
	})
}

// retypeDeclarationRow rewrites one declaration row's properties to the typed
// shape, through the fold, with one changelog entry describing it. It reports
// whether anything moved: a row already typed folds to nothing, which is what
// makes the rung free to re-run.
func (t *txn) retypeDeclarationRow(d declaration) (bool, error) {
	ref := eref{Kind: d.typ, ID: d.id}
	row, err := t.loadRow(ref, true)
	if err != nil {
		return false, err
	}
	if row == nil {
		// The declaration has no row: the authority-named actor is the one shape
		// that reaches here (its authority row IS it, record 60), and nothing else
		// can, since these declarations were rebuilt FROM rows.
		return false, nil
	}
	before := row.clone()
	props := make(map[string]any, len(d.props))
	server := serverDeclarationProps(d.short)
	for k, v := range d.props {
		if v == nil {
			continue // a retired key: absent is what it means
		}
		props[k] = v
	}
	// The server-owned values are the ROW's, not the derivation's: a declaration
	// held at a version ahead of its authority's keeps it, a quarantine mark
	// survives the rewrite, and a disabled bundle stays disabled.
	for k := range server {
		if v, held := row.Props[k]; held {
			props[k] = v
		}
	}
	for k := range retiredDeclarationProps(d.short) {
		delete(props, k)
	}
	row.Props = props
	// The TITLE is deliberately not recomputed: deriving it needs the registry,
	// which is empty at rung time, and every declaration's title is its local
	// name under both the old template ({name}, the id-derived mirror) and the new
	// one ({localName}) — so there is nothing to move. The same holds for the
	// search bands: the fold falls back to title and body while the registry is
	// empty, and the next write of the row (the boot upgrade's re-projection)
	// restates them.
	res, err := t.foldRow(before, row, false, false)
	if err != nil || !res.changed {
		return false, err
	}
	return true, t.appendChange(substrate.ActorSystem, substrate.OpPatch, d.id, d.typ,
		map[string]any{"properties": sortedKeys(props), "dialect": dialectTypedDeclarations})
}

// stampDialect records one completed promotion and stamps the dialect from
// INSIDE a step's own transaction — the atomicity a row rewrite needs, so a
// store can never hold the new rows under the old stamp (an older binary would
// read them as declarations without a declaration). recordDialectStep is the
// same two statements for a step that does not need them to ride its own work.
func (t *txn) stampDialect(dialect int, name string) error {
	if _, err := t.exec(`
		INSERT INTO vocabulary_promotions (dialect, name) VALUES ($1, $2)
		ON CONFLICT (repository, dialect) DO NOTHING`, dialect, name); err != nil {
		return fmt.Errorf("substrate/engine: record dialect step %d: %w", dialect, err)
	}
	if _, err := t.exec(`
		INSERT INTO vocabulary_dialect (dialect) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE
		SET dialect = GREATEST(vocabulary_dialect.dialect, EXCLUDED.dialect), updated_at = now()`,
		dialect); err != nil {
		return fmt.Errorf("substrate/engine: stamp dialect %d: %w", dialect, err)
	}
	return nil
}
