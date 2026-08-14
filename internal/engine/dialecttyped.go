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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// dialectTypedDeclarations is dialect 2's name in vocabulary_promotions.
const dialectTypedDeclarations = "typed-declarations"

// ErrDeclarationUntranslated is the rung's refusal: a declaration row this
// binary cannot put into the typed shape. THE OPEN FAILS, and that is the point
// — stamping the dialect with one dialect-1 row left would leave the store in
// the two-encodings state the design forbids, where some declarations read
// through their properties and some through a blob, and no reader can tell which
// it is holding. A closure that no longer parses is repaired by re-installing
// the bundle (or by opening under the binary that wrote it) BEFORE this one
// migrates the repository.
var ErrDeclarationUntranslated = errors.New("substrate/engine: a stored declaration cannot be translated to the typed shape")

// promoteTypedDeclarations rewrites every declaration row this repository holds
// into its typed form, in one transaction with the dialect stamp.
func (ds *dataset) promoteTypedDeclarations(ctx context.Context) error {
	// The translation, before any write: the stored rows parse through the
	// current loader, and what comes back is the typed property map of every
	// declaration each authority holds.
	built, unparsed, err := ds.storedAuthorities(ctx, nil)
	if err != nil {
		return err
	}
	// A closure that no longer PARSES cannot be translated, and skipping it is
	// not an option: the stamp below says the whole store is typed, so one
	// untranslatable row would make the stamp a lie. It refuses, loudly, naming
	// the authority — a quarantine is a state a MIGRATED repository may reach, not
	// one it may be migrated in.
	if len(unparsed) > 0 {
		names := make([]string, 0, len(unparsed))
		for _, q := range unparsed {
			names = append(names, q.name)
			ds.svc.log.Error("substrate: REFUSING to migrate a repository's declarations — this closure no longer parses under this binary, so its rows cannot be translated; re-install the bundle (or open under the binary that wrote it) first",
				"repository", ds.info.Name, "authority", q.name, "reason", q.reason)
		}
		return fmt.Errorf("%w: repository %s: %s", ErrDeclarationUntranslated, ds.info.Name, strings.Join(names, ", "))
	}
	if len(built) == 0 {
		return nil
	}
	// THE RUNG'S OWN CANDIDATE REGISTRY, built from the translated documents. The
	// live registry is empty at rung time (promote runs before
	// loadStoredVocabulary), so nothing here resolves against it; this proves the
	// translated set still admits whole, meta-kinds included — core is among the
	// authorities rebuilt above, and a repository whose meta-kinds do not parse
	// never reaches here (storedAuthorities returns core's failure as an error).
	candidate := vocabulary.NewRegistry()
	if err := candidate.InstallAll(built); err != nil {
		return fmt.Errorf("%w: repository %s: the translated declarations do not admit together: %w",
			ErrDeclarationUntranslated, ds.info.Name, err)
	}

	// What each row must BECOME, keyed by authority. The typed properties are the
	// projection's own derivation; the row's engine-owned values are kept as
	// stored, because the rung is a change of SHAPE and never of content.
	want := map[string][]declaration{}
	for _, g := range built {
		decls, err := authorityDeclarations(g)
		if err != nil {
			return fmt.Errorf("substrate/engine: translate %s: %w", g.Name, err)
		}
		want[g.Name] = decls
	}

	// Every typed row is held to the declaration it will LIVE under, and which
	// one that is depends on the repository, not on the binary (rungValidators).
	validators, err := ds.rungValidators(candidate, want)
	if err != nil {
		return err
	}
	for _, aname := range sortedKeys(want) {
		for _, d := range want[aname] {
			ty := validators[d.typ]
			if ty == nil {
				return fmt.Errorf("%w: repository %s: nothing declares %s", ErrDeclarationUntranslated, ds.info.Name, d.typ)
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
		// The bands are an index over the row computed from the declaration the row
		// is validated against, so the rung's fold reads its own candidate: a row
		// indexed under the empty registry (title and body alone) would be
		// re-indexed differently by its own replay, and for an installed authority
		// nothing would ever restate it.
		t.writeReg = candidate
		defer func() { t.writeReg = nil }()
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
		// THE STRUCTURAL INVARIANT, before the stamp and inside the transaction: no
		// live declaration row still carries a `definition`. Everything above is a
		// derivation from what the loader parsed, and a row it never reached — an
		// orphan whose authority row is gone, a kind this binary does not know —
		// would otherwise survive the stamp unmigrated. The count rolls the whole
		// rewrite back with it.
		if left, err := t.definitionBearingRows(); err != nil {
			return err
		} else if len(left) > 0 {
			return fmt.Errorf("%w: repository %s: %d row(s) still carry a definition: %s",
				ErrDeclarationUntranslated, ds.info.Name, len(left), strings.Join(left, ", "))
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

// rungValidators is the declaration each translated row is held to, per
// meta-kind: THE ONE THAT WILL BE LIVE once this open finishes.
//
// The repository decides, not the binary. A stored meta-kind declaration at or
// AHEAD of the binary's version survives this open untouched — the boot upgrade
// never downgrades (seed.go) — so its own translated declaration is what its rows
// must satisfy, and holding them to the binary's instead would refuse a
// repository for being newer than the substrate reading it. A stored declaration
// BEHIND the binary's is the one the upgrade replaces minutes later, so the
// binary's is what decides; that is the ordinary migration, where the stored
// meta-kind still declares the `definition` blob and could not admit a typed row
// at all.
func (ds *dataset) rungValidators(candidate *vocabulary.Registry, want map[string][]declaration) (map[string]*vocabulary.Kind, error) {
	shipped, err := ds.shippedDeclarationVersions()
	if err != nil {
		return nil, err
	}
	stored := map[string]string{}
	for _, decls := range want {
		for _, d := range decls {
			if d.typ == kindKind {
				stored[d.id] = d.version()
			}
		}
	}
	out := map[string]*vocabulary.Kind{}
	for ident := range vocabularyRecordKinds {
		if vocabulary.CompareVersions(stored[ident], shipped[ident]) >= 0 {
			if ty, ok := candidate.ByIdentity(ident); ok {
				out[ident] = ty
				continue
			}
		}
		if ty, ok := ds.svc.base.ByIdentity(ident); ok {
			out[ident] = ty
		}
	}
	return out, nil
}

// shippedDeclarationVersions is the version the embedded tree declares for each
// meta-kind — what the boot upgrade compares a stored declaration against.
func (ds *dataset) shippedDeclarationVersions() (map[string]string, error) {
	core, ok := ds.svc.base.AuthorityByName(vocabulary.AuthorityCore)
	if !ok {
		return nil, fmt.Errorf("substrate/engine: the binary ships no %s authority", vocabulary.AuthorityCore)
	}
	decls, err := authorityDeclarations(core)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, d := range decls {
		if d.typ == kindKind {
			out[d.id] = d.version()
		}
	}
	return out, nil
}

// definitionBearingRows lists the live declaration rows that still carry a
// `definition` property — the dialect-1 shape — as the ids the refusal names.
func (t *txn) definitionBearingRows() ([]string, error) {
	args := make([]any, 0, len(vocabularyKindRefs))
	ph := make([]string, 0, len(vocabularyKindRefs))
	for i, ident := range vocabularyKindRefs {
		args = append(args, ident)
		ph = append(ph, "$"+strconv.Itoa(i+1))
	}
	rows, err := t.query(`
		SELECT kind, id FROM records
		WHERE kind IN (`+strings.Join(ph, ", ")+`) AND deleted_at IS NULL
		  AND props ? 'definition'
		ORDER BY kind, id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, err
		}
		out = append(out, kind+" "+id)
	}
	return out, rows.Err()
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
	for k, v := range d.props {
		if v == nil {
			continue // a retired key: absent is what it means
		}
		props[k] = v
	}
	// The ENGINE'S OWN values are the ROW's, not the derivation's: a declaration
	// held at a version ahead of its authority's keeps it, a quarantine mark
	// survives the rewrite, a disabled bundle stays disabled — and a property some
	// NEWER binary stamps rides through untouched instead of being dropped by a
	// binary that does not know it.
	for k, v := range row.Props {
		if engineOwned(d.short, k) {
			props[k] = v
		}
	}
	for k := range retiredDeclarationProps(d.short) {
		delete(props, k)
	}
	row.Props = props
	// The TITLE is deliberately not recomputed: every declaration's title is its
	// local name under both the old template ({name}, the id-derived mirror) and
	// the new one ({localName}), so there is nothing to move. The search bands DO
	// move, and they are computed from the candidate the rows are validated
	// against (the fold's writeReg above), so a replay of this entry reproduces
	// them.
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
