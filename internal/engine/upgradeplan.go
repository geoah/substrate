package engine

// The upgrade PREVIEW. Install is already the upgrade (a bundle document
// replaces its package whole, atomically, breakage refused,
// vocabularywrite.go), so the only thing a "check for updates" needs is the
// install verb's answer WITHOUT the install: would this shipped closure move
// anything here, and would the door refuse it. PlanBundleUpgrade computes
// both, writes nothing, and the catalog serves it (catalog.Upgrade) so the
// console can offer the upgrade, or show why it is blocked, before anyone
// clicks.
//
// The two halves reuse the two existing mechanisms rather than paralleling
// them:
//
//   - the VERSION DIFF is the boot upgrade's (seed.go): the closure's packages
//     built alone, enumerated by packageDeclarations (the one enumeration)
//     against the stored versions, same keys, same comparator;
//   - the BLOCKERS are the apply door's own staging and guard counts
//     (stageVocabularyBatch, schemadiff.go), run over the bare pool instead
//     of the batch transaction. What the preview reports blocked, the install
//     refuses; what it reports clean, the install admits, modulo writes that
//     land between the preview and the click, which the install re-checks.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// dbReader runs the refuse-breakage counts over the bare pool for the
// read-only preview; the apply door runs the same counts inside its
// transaction (txn implements sqlReader too).
type dbReader struct {
	ctx context.Context
	db  *sql.DB
}

func (r dbReader) row(sqlText string, args ...any) *sql.Row {
	return r.db.QueryRowContext(r.ctx, sqlText, args...)
}

func (r dbReader) query(sqlText string, args ...any) (*sql.Rows, error) {
	return r.db.QueryContext(r.ctx, sqlText, args...)
}

// PlanBundleUpgrade reports what installing the given closure over this
// repository's stored declarations would do: which declarations it moves
// (newer version, new here, or pruned by the whole-package replace) and the
// guard lines the install verb would refuse it on. It writes nothing. A
// repository that never installed the bundle's package answers not-available:
// there is nothing to upgrade, only to install.
func (ds *dataset) PlanBundleUpgrade(ctx context.Context, vocabularyDocs []map[string]any) (substrate.BundleUpgrade, error) {
	var plan substrate.BundleUpgrade
	if len(vocabularyDocs) == 0 {
		return plan, fmt.Errorf("%w: no schema documents", substrate.ErrValidation)
	}
	docs, err := parseVocabularyDocs(vocabularyDocs)
	if err != nil {
		return plan, err
	}
	var bundlePackage string
	for _, d := range docs {
		if d.Kind == vocabulary.DocBundle {
			bundlePackage = d.DeclaredPackage()
		}
	}
	if bundlePackage == "" {
		return plan, fmt.Errorf("%w: the closure carries no bundle document", substrate.ErrValidation)
	}

	stored, err := ds.storedDeclarations(ctx)
	if err != nil {
		return plan, err
	}
	packageRow, installed := stored[kindPackage+"\x00"+bundlePackage]
	if !installed {
		return plan, nil
	}
	plan.From = packageRow.version

	// The version diff, exactly as the boot upgrade computes it (seed.go).
	byPackage := map[string][]vocabulary.Document{}
	for _, d := range docs {
		g := d.DeclaredPackage()
		if g == "" {
			return plan, fmt.Errorf("%w: %s %s: data.authority and data.package are required", substrate.ErrValidation, d.Kind, d.ID)
		}
		byPackage[g] = append(byPackage[g], d)
	}
	shipped := map[string]bool{}
	for _, aname := range sortedKeys(byPackage) {
		gs, err := vocabulary.BuildPackages(byPackage[aname], vocabulary.SourceInstalled)
		if err != nil {
			// A shipped closure that cannot even build is a blocker, not a
			// failed READ: this preview is computed for every installed bundle
			// on the catalog listing, and one malformed closure must not take
			// the listing (and with it the console's whole Registry) down.
			// Same posture as catalog.Load, which drops a broken directory
			// with a warning rather than bricking the shipped set.
			var ve *substrate.ValidationError
			if errors.As(err, &ve) {
				plan.Blockers = ve.Problems
				return plan, nil
			}
			return plan, err
		}
		for _, g := range gs {
			if g.Identity == bundlePackage {
				plan.To = g.Version
			}
			decls, err := packageDeclarations(g)
			if err != nil {
				// Same posture as the build above: a declaration missing its
				// version is the shipped closure's bug, reported, never a
				// failed listing.
				var ve *substrate.ValidationError
				if errors.As(err, &ve) {
					plan.Blockers = ve.Problems
					return plan, nil
				}
				return plan, err
			}
			for _, d := range decls {
				shipped[d.key()] = true
				have, exists := stored[d.key()]
				switch {
				case !exists:
					plan.Changes = append(plan.Changes, substrate.BundleUpgradeChange{
						Kind: d.short, ID: d.id, To: d.version(),
					})
				case vocabulary.CompareVersions(d.version(), have.version) > 0:
					plan.Changes = append(plan.Changes, substrate.BundleUpgradeChange{
						Kind: d.short, ID: d.id, From: have.version, To: d.version(),
					})
				}
			}
		}
	}
	// Declarations the closure stopped shipping under the bundle's own
	// package: the whole-package replace prunes them, so they are part of the
	// move, and with live rows its likeliest blocker.
	for key, s := range stored {
		if s.pkg != bundlePackage || shipped[key] {
			continue
		}
		typ, id, _ := strings.Cut(key, "\x00")
		plan.Changes = append(plan.Changes, substrate.BundleUpgradeChange{
			Kind: vocabularyRecordKinds[typ], ID: id, From: s.version,
		})
	}
	if len(plan.Changes) == 0 {
		return plan, nil
	}
	sort.Slice(plan.Changes, func(i, j int) bool {
		if plan.Changes[i].Kind != plan.Changes[j].Kind {
			return plan.Changes[i].Kind < plan.Changes[j].Kind
		}
		return plan.Changes[i].ID < plan.Changes[j].ID
	})
	plan.Available = true

	// The blockers: the same staging and the same guard counts the install
	// door runs, minus the transaction. A closure this repository cannot even
	// compile (a missing `requires:` package, say) blocks the same install,
	// so its problems land where the guard lines would rather than failing
	// the read.
	st, err := ds.stageVocabularyBatch(ctx, ds.registry(), nil, vocabularyBatch{docs: docs})
	if err != nil {
		var ve *substrate.ValidationError
		if errors.As(err, &ve) {
			plan.Blockers = ve.Problems
			return plan, nil
		}
		return plan, err
	}
	q := dbReader{ctx: ctx, db: ds.db}
	blockers, err := droppedTypeGuards(q, st.droppedTypes)
	if err != nil {
		return plan, err
	}
	blockers = append(blockers, st.strandedMappings...)
	narrowed, err := narrowingGuards(q, st.narrowings)
	if err != nil {
		return plan, err
	}
	blockers = append(blockers, narrowed...)
	stranded, err := droppedCallableGuards(q, st.droppedCallables)
	if err != nil {
		return plan, err
	}
	plan.Blockers = append(blockers, stranded...)
	return plan, nil
}
