package engine

// The boot upgrade's PREVIEW, and the one computation both of its doors run.
//
// upgradeShippedVocabulary (seed.go) decides at a repository's first open under
// a new binary whether the shipped packages move here and whether a
// refuse-breakage guard refuses the move. Until this file that decision was a
// write or one log line: a refused core upgrade left the repository open on its
// old declarations with nothing a repository token could read about it, because
// core is not a catalog entry and the catalog's preview (upgradeplan.go) covers
// installed providers alone.
//
// stageShippedUpgrade is the boot decision minus the write and minus the count:
// the version diff per declaration, the narrowings whose live rows must be
// counted, and the guard lines settled without a count. The boot path stages,
// counts inside its transaction and projects; PlanShippedUpgrade stages, counts
// over the bare pool and answers `GET /api/v1/vocabulary/upgrade`. One diff and
// one guard set, so what the read reports blocked is exactly what the boot
// refused, and a guard added to one door is added to both.

import (
	"context"
	"errors"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// shippedUpgradeStage is the boot upgrade's decision before any row is counted.
type shippedUpgradeStage struct {
	// upgrade is every shipped package with a declaration to write here: one
	// this repository never had, or one shipped at a newer version than
	// stored. Empty means the boot has nothing to do.
	upgrade map[string]bool
	// keep is every declaration held at or above its shipped version, by key.
	// The projection skips these, so a repository ahead of the binary stays
	// ahead and nothing is ever downgraded.
	keep map[string]bool
	// narrowings are the guards that need a live-row count. guards runs them
	// over whichever reader the door holds: the boot's transaction, or the
	// bare pool for the read.
	narrowings []narrowing
	// refused are the guard lines decided without a count: a declared default
	// no write could store, a retired name declared again or dropped, a
	// retirement of a kind this repository still declares. guards puts them
	// first, then the counted narrowings; the boot and the preview both read
	// that one list.
	refused []string
	// renames are the property renames the shipped tree declares against the
	// stored declarations (rename.go): the boot performs them in the
	// transaction that projects the declarations, and a reader of the old name
	// the candidate cannot refuse lands in refused.
	renames []propertyRename
	// candidate is the registry the boot would publish (shippedCandidate): the
	// stored one with the upgraded packages replaced by the shipped ones. The
	// rename rewrites run against it, so a stored user kind a renamed record
	// references still resolves. Nil when the closure does not compile, and
	// refused then carries the problems.
	candidate *vocabulary.Registry
	// plans is the version motion per shipped PACKAGE this repository holds as
	// shipped vocabulary, sorted by identity. The authority row beside the
	// packages is diffed and projected with them but is not a package, so it
	// has no entry.
	plans []substrate.ShippedUpgrade
}

// stageShippedUpgrade diffs the binary's shipped tree against this repository's
// stored declarations, per declaration and keyed on `version`, exactly as the
// boot upgrade has always done (upgradeShippedVocabulary's contract): a
// declaration the repository lacks or holds older is written, one held at the
// same or a newer version is kept. A package whose stored rows are not shipped
// vocabulary (a user or a bundle took the name) is skipped whole.
func (ds *dataset) stageShippedUpgrade(ctx context.Context) (*shippedUpgradeStage, error) {
	reg := ds.svc.base
	stored, err := ds.storedDeclarations(ctx)
	if err != nil {
		return nil, err
	}
	current := ds.registry()
	st := &shippedUpgradeStage{upgrade: map[string]bool{}, keep: map[string]bool{}}
	// The kinds and package headers this upgrade will NOT rewrite, by
	// identity: a declaration held at its stored version keeps whatever shape
	// it has, so it is not the upgrade's business and must not be able to
	// refuse the boot.
	keptIdents := map[string]bool{}
	for _, aname := range sortedKeys(shippedPackages(reg)) {
		g, ok := reg.PackageByName(aname)
		if !ok {
			continue
		}
		if cur, ok := current.PackageByName(aname); ok && cur.Source != vocabulary.SourceBuiltin {
			continue // the name is somebody else's here; the tree does not take it
		}
		decls, err := packageDeclarations(g)
		if err != nil {
			return nil, err
		}
		plan := substrate.ShippedUpgrade{Package: aname}
		plan.Upgrade.To = g.Version
		if row, ok := stored[kindPackage+"\x00"+aname]; ok {
			plan.Upgrade.From = row.version
		}
		for _, d := range decls {
			have, exists := stored[d.key()]
			switch {
			case !exists: // a declaration this repository has never had
				plan.Upgrade.Changes = append(plan.Upgrade.Changes, substrate.BundleUpgradeChange{
					Kind: d.short, ID: d.id, To: d.version(),
				})
			case vocabulary.CompareVersions(d.version(), have.version) > 0: // the shipped declaration moved forward
				plan.Upgrade.Changes = append(plan.Upgrade.Changes, substrate.BundleUpgradeChange{
					Kind: d.short, ID: d.id, From: have.version, To: d.version(),
				})
			default:
				st.keep[d.key()] = true // same or older than stored: never a downgrade
				if d.typ == kindKind || d.typ == kindPackage {
					keptIdents[d.id] = true
				}
			}
		}
		if len(plan.Upgrade.Changes) > 0 {
			plan.Upgrade.Available = true
			st.upgrade[aname] = true
		}
		if !g.IsAuthority() {
			st.plans = append(st.plans, plan)
		}
	}
	if len(st.upgrade) == 0 {
		return st, nil
	}

	// The SAME refuse-breakage guards `/vocabulary/apply` takes
	// (vocabularywrite.go): a narrowing declaration diff (a property dropped
	// or kind-changed, an enum value or state removed, required added)
	// is refused while live rows still hold the old shape, with the count.
	//
	// The two doors used to disagree. An operator applying the same change by
	// hand was refused; the boot upgrade projected it silently, leaving rows
	// shaped one way under a declaration that said another, with nothing
	// anywhere reporting it. A guard only one door honors is not a guard.
	st.narrowings = classifyNarrowingsExcept(current, reg, st.upgrade, keptIdents)

	// The default check `/vocabulary/apply` takes, for the same reason the
	// narrowing guards are here: a declared default no write could store would
	// land at boot and break every create of that kind afterwards, and the door
	// that refuses it by hand would have caught it. It needs no live rows, so it
	// is decided before any row is counted.
	st.refused = checkDeclaredDefaults(reg, st.upgrade)
	// The retired-name check the same door takes (decision 0055): a shipped
	// declaration that reuses a name this repository's stored closure retired,
	// or a tree that dropped a stored retirement, is refused before any row
	// moves. This door has no document merge in front of it, so a dropped list
	// is refused here rather than carried forward.
	st.refused = append(st.refused, retirementGuards(current, reg, st.upgrade, keptIdents)...)
	// And the branch only this door needs: the tree retires a name this
	// repository still declares. Nothing here prunes the kind, so the header
	// would land beside it and the next open would refuse the stored closure.
	st.refused = append(st.refused, heldRetirementGuards(current, reg, st.upgrade)...)
	// The apply door refuses what its candidate cannot compile; this door had
	// no candidate, so a shipped change a stored package could not re-resolve
	// against (a mapping path naming a property the tree dropped or renamed)
	// landed, and admissibleSubset quarantined the user's package at the next
	// load for a change the tree made. The candidate is built on EVERY upgrade
	// (shippedCandidate: the stored registry with the upgraded packages
	// replaced by the shipped ones) and its problems refuse the boot. The cost
	// is one parse of the upgraded packages from their projected documents,
	// the same work every open does for every stored package.
	candidate, problems, err := ds.shippedCandidate(ctx, current, reg, st.upgrade, st.keep)
	if err != nil {
		return nil, err
	}
	st.refused = append(st.refused, problems...)
	st.candidate = candidate
	// A shipped rename is not a narrowing: the boot moves the live values in
	// the same transaction, against the candidate (rename.go, decision 0063).
	// What refuses it beyond the compile is a stored template reading the old
	// name through a reference (renameGuards), read through the candidate.
	st.renames = classifyRenames(current, reg, st.upgrade, keptIdents)
	if candidate != nil {
		st.refused = append(st.refused, renameGuards(current, candidate, st.renames)...)
	}
	return st, nil
}

// shippedCandidate is the registry the boot upgrade would publish: the stored
// one with each upgraded package replaced by what the boot projects, built
// from the same projected documents the transaction writes (packageDeclarations
// and rowDocument, the projection's write and read halves). A closure that does
// not compile answers its problems and no registry, exactly as the apply door
// refuses a batch whose candidate does not compile.
func (ds *dataset) shippedCandidate(ctx context.Context, current, reg *vocabulary.Registry, upgrade, keep map[string]bool) (*vocabulary.Registry, []string, error) {
	candidate := current.Clone()
	// EXACTLY THE PROJECTED SET. The projection writes a shipped declaration
	// only where it moves the stored one forward and keeps every declaration
	// in `keep` (a stored one at the same or a newer version) and every stored
	// declaration the tree does not ship (the boot never prunes). A candidate
	// built from the shipped packages whole compiled the embedded declaration
	// where the kept stored one would stand, so a mixed package refused a
	// valid boot, or passed staging and failed the reload. The stored
	// documents of the upgraded packages come first; a shipped declaration
	// replaces its stored twin unless the key is kept.
	merged, err := ds.vocabularyDocumentRows(ctx, upgrade)
	if err != nil {
		return nil, nil, err
	}
	for _, aname := range sortedKeys(upgrade) {
		g, ok := reg.PackageByName(aname)
		if !ok {
			continue
		}
		candidate.Remove(aname)
		decls, err := packageDeclarations(g)
		if err != nil {
			return nil, nil, err
		}
		for _, d := range decls {
			if keep[d.key()] {
				continue // the stored declaration stands, as the projection leaves it
			}
			// A projected null deletes the key from the row (the put merges), so
			// the row rowDocument reads never carries it; its blob check is by
			// presence, and would read the null as the blob.
			stored := make(map[string]any, len(d.props))
			for k, v := range d.props {
				if v != nil {
					stored[k] = v
				}
			}
			doc, ok, err := rowDocument(d.id, d.typ, stored)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				merged[docKey(doc)] = doc
			}
		}
	}
	docs := make([]vocabulary.Document, 0, len(merged))
	for _, k := range sortedKeys(merged) {
		docs = append(docs, merged[k])
	}
	pkgs, err := vocabulary.BuildPackages(docs, vocabulary.SourceBuiltin)
	if err == nil {
		err = candidate.InstallAll(pkgs)
	}
	if err != nil {
		var ve *substrate.ValidationError
		if errors.As(err, &ve) {
			return nil, ve.Problems, nil
		}
		return nil, nil, err
	}
	return candidate, nil, nil
}

// guards is every guard line the staged upgrade refuses on: the ones decided
// without a count, then each narrowing whose count strands live rows, read
// through q. Empty means the upgrade would be admitted.
func (st *shippedUpgradeStage) guards(q sqlReader) ([]string, error) {
	counted, err := narrowingGuards(q, st.narrowings)
	if err != nil {
		return nil, err
	}
	return append(append([]string(nil), st.refused...), counted...), nil
}

// PlanShippedUpgrade reports what this binary's boot upgrade would do to each
// shipped package here, and the guard lines it refuses on: the same staging
// and the same counts the boot runs (st.guards), over the bare pool, writing
// nothing. The boot refuses the shipped set as a whole, so every package with
// something to write carries the whole list, which is exactly the list the
// refusal logged.
func (ds *dataset) PlanShippedUpgrade(ctx context.Context) ([]substrate.ShippedUpgrade, error) {
	st, err := ds.stageShippedUpgrade(ctx)
	if err != nil {
		return nil, err
	}
	if len(st.upgrade) == 0 {
		return st.plans, nil
	}
	q := dbReader{ctx: ctx, db: ds.db}
	blockers, err := st.guards(q)
	if err != nil {
		return nil, err
	}
	renames, err := renamePlans(q, st.renames)
	if err != nil {
		return nil, err
	}
	for i := range st.plans {
		if !st.plans[i].Upgrade.Available {
			continue
		}
		st.plans[i].Upgrade.Blockers = blockers
		// A rename belongs to the package whose kind declares it; the blockers
		// are the whole set, because the boot refuses the shipped set whole.
		for _, r := range renames {
			if vocabulary.KindPackage(r.Kind) == st.plans[i].Package {
				st.plans[i].Upgrade.Renames = append(st.plans[i].Upgrade.Renames, r)
			}
		}
	}
	return st.plans, nil
}
