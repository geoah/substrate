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
	// conversions are the record rewrites the shipped tree declares against the
	// stored declarations (a rename, a backfill, a remap: convert.go): the boot
	// performs them in the transaction that projects the declarations, and
	// what refuses one without a count (a reader of a renamed name, a lossy
	// remap) lands in refused.
	conversions conversionPlan
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
	// A shipped rename, backfill, remap or null is not a narrowing: the boot
	// rewrites the live records in the same transaction, against the
	// candidate (convert.go, decisions 0063 and 0066). What refuses one beyond
	// the compile is a stored template reading a renamed name through a
	// reference (renameGuards), read through the candidate; a lossy step and a
	// plan above the work ceiling refuse once counted (guards), because this
	// door has nobody to confirm them (decision 0067).
	st.conversions = classifyConversions(current, reg, st.upgrade, keptIdents)
	if candidate != nil {
		st.refused = append(st.refused, renameGuards(current, candidate, st.conversions.renames)...)
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
// without a count, then each narrowing whose count strands live rows, then
// the conversion plan's own refusals, read through q. Empty means the upgrade
// would be admitted. The plan comes back with the lines, counted once: the
// boot runs it, the preview reports it.
//
// The boot upgrade runs unattended, so a lossy step (convert.go) has nobody to
// confirm it and refuses here: the repository opens on its stored
// declarations, the preview names the step, and rewriting the records it
// counts (or applying the change through a door that can confirm it) is what
// clears the line. A plan above the work ceiling refuses for the same reason
// the apply door refuses it.
func (st *shippedUpgradeStage) guards(q sqlReader, ceiling int64) ([]string, substrate.ConversionPlan, error) {
	counted, err := narrowingGuards(q, st.narrowings)
	if err != nil {
		return nil, substrate.ConversionPlan{}, err
	}
	lines := append(append([]string(nil), st.refused...), counted...)
	plan, err := st.conversions.wire(q)
	if err != nil {
		return nil, plan, err
	}
	for _, line := range lossyLines(plan) {
		lines = append(lines, line+"; the boot upgrade runs unattended and never runs a lossy step, so a lossy conversion is refused here: rewrite the records it counts first")
	}
	if line := ceilingGuard(plan, ceiling); line != "" {
		lines = append(lines, line)
	}
	return lines, plan, nil
}

// PlanShippedUpgrade reports what this binary's boot upgrade would do to each
// shipped package here, and the guard lines it refuses on: the same staging
// and the same counts the boot runs (st.guards), over the bare pool, writing
// nothing. The boot refuses the shipped set as a whole, so every package with
// something to write carries the whole blocker list, which is exactly the list
// the refusal logged; the conversion steps are each package's own
// (packagePlan), because they rewrite that package's kinds.
func (ds *dataset) PlanShippedUpgrade(ctx context.Context) ([]substrate.ShippedUpgrade, error) {
	st, err := ds.stageShippedUpgrade(ctx)
	if err != nil {
		return nil, err
	}
	if len(st.upgrade) == 0 {
		return st.plans, nil
	}
	blockers, plan, err := st.guards(dbReader{ctx: ctx, db: ds.db}, ds.svc.conversionCeiling)
	if err != nil {
		return nil, err
	}
	for i := range st.plans {
		if !st.plans[i].Upgrade.Available {
			continue
		}
		st.plans[i].Upgrade.Blockers = blockers
		st.plans[i].Upgrade.ConversionPlan = packagePlan(plan, st.plans[i].Package)
		st.plans[i].Upgrade.Renames = legacyRenames(st.plans[i].Upgrade.Steps) //nolint:staticcheck // the deprecated field is produced here for readers that still read it
	}
	return st.plans, nil
}
