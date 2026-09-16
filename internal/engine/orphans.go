package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE ORPHAN MARK. A mapping's target is minted out of a source record and
// carries what that source says; when the last live source of one goes — the
// connector deleted the row, a re-seed purged the mirrors and minted them
// under new ids — recompute empties the mapped properties and the row stays,
// a husk nothing describes and nothing can match
// ([#578](https://github.com/geoah/substrate/issues/578)).
//
// `records.orphaned_at` is the engine saying so: DERIVED STORAGE, a function
// of the live records exactly as `property_offers` is, set and cleared inside
// the recompute that discovered it, never carried by the changelog, and
// derived again by a rebuild from the fold it replayed. A re-link clears it
// on the spot: it is a reading of the present, not a state anybody has to
// clear.
//
// Three conditions, all of them, and they are the whole definition:
//
//  1. SOMETHING MAPS ONTO THE KIND. A record of a kind no recordmapping
//     targets is nobody's projection, so it cannot be orphaned by one. This
//     is what keeps every ordinary record out of the set.
//  2. NO LIVE SOURCE. Not one live record links here through a mapping's
//     subject slot — counted over every id the target has ever had, because a
//     source synced before its subject won a merge still names the loser
//     (subjectSourceSites).
//  3. NOTHING ABOVE THE MACHINE TIER HOLDS A PROPERTY. Every
//     property_managers row on the record is `machine`, or there are none at
//     all. A row at the bundle or the owner tier is a hand — a human's or
//     installed code's — and a hand's write is the record's own content, not
//     a projection of a source that has gone.
//
// Condition 3 is the judgement call, and it is deliberately the strict
// reading. The other candidate was "nothing above machine that a LIVE bundle
// wrote", which would mark a husk carrying a property some uninstalled
// bundle's function once pinned. It is rejected twice over: a bundle's direct
// write "pins like an owner edit" (substrate.Tier), so making it collectable
// under some conditions makes the tier's meaning conditional; and it would
// turn an uninstall into a retroactive delete of records the bundle merely
// touched, which is the class of surprise decision 0085 refuses when it
// declines to clear subject links on an uninstall. The cost is stated and
// accepted: a husk one bundle-tier write landed on is not marked, and the way
// to give it back to the machine is the documented one — null-patch the
// pinned property, which releases it (docs/projection.md).

// syncOrphaned sets or clears a target's orphan mark. It runs at the end of
// every recompute and after every direct write onto a mapped kind, because
// both halves of the condition move: a source leaves the live set, or a hand
// takes a property above the machine tier.
//
// The UPDATE touches the derived column alone — no version, no updated_at, no
// changelog entry — for the same reason `fts` does (fold.go reprojectFTS):
// the mark is a projection of state the changelog already holds, and moving
// the row's version for it would publish a change nobody made.
func (t *txn) syncOrphaned(target eref) error {
	orphan, err := t.isOrphan(target)
	if err != nil {
		return err
	}
	if orphan {
		_, err = t.exec(
			`UPDATE records SET orphaned_at = $3 WHERE kind = $1 AND id = $2 AND deleted_at IS NULL AND orphaned_at IS NULL`,
			target.Kind, target.ID, t.now)
		return err
	}
	_, err = t.exec(
		`UPDATE records SET orphaned_at = NULL WHERE kind = $1 AND id = $2 AND orphaned_at IS NOT NULL`,
		target.Kind, target.ID)
	return err
}

// isOrphan answers the three conditions above, cheapest first: the registry
// decides whether the kind can orphan at all, one indexed read decides
// whether a hand holds anything, and only then is the source join run.
func (t *txn) isOrphan(target eref) (bool, error) {
	row, err := t.loadRow(target, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return false, err
	}
	mappings := t.declarations().MappingsTo(row.Kind)
	if len(mappings) == 0 {
		return false, nil
	}
	var held int
	if err := t.row(`
		SELECT count(*) FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 AND tier <> $3`,
		target.Kind, target.ID, string(substrate.TierMachine)).Scan(&held); err != nil {
		return false, err
	}
	if held > 0 {
		return false, nil
	}
	sites, err := t.subjectSourceSites(target, mappings)
	if err != nil {
		return false, err
	}
	return len(sites) == 0, nil
}

// orphanedAt reads one record's mark, nil when it carries none or is gone.
func (t *txn) orphanedAt(ref eref) (*time.Time, error) {
	var at *time.Time
	err := t.row(`SELECT orphaned_at FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		ref.Kind, ref.ID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return at, nil
}

// referencedByLive reports whether any LIVE record other than this one points
// at it, through any property at any site — the refs index over every id the
// record has ever had, so a pointer written before a merge still counts. It
// is the collection's second question: the mark says nothing describes this
// record any more, and this says nothing else has hold of it either.
func (t *txn) referencedByLive(ref eref) (bool, error) {
	ids, err := t.idsOf(ref)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return false, err
	}
	var one int
	err = t.row(`
		SELECT 1 FROM refs r
		JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE r.dst_kind = $1 AND r.dst IN (SELECT jsonb_array_elements_text($2::jsonb))
		  AND x.deleted_at IS NULL AND NOT (x.kind = $1 AND x.id = $3)
		LIMIT 1`, ref.Kind, raw, ref.ID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// --- collection ---------------------------------------------------------

// collectOrphans is the sweep's orphan pass: every marked record whose mark
// is older than the deployment's grace window and that nothing live points at
// is tombstoned, and the sweep's own fixpoint purges it in the same pass.
//
// IT IS OFF UNLESS THE DEPLOYMENT ASKS FOR IT (WithOrphanCollection,
// SUBSTRATE_ORPHAN_GRACE). Marking is a reading; collecting is a delete of
// somebody's records decided by the machine, and "the last source went" is
// exactly the shape a connector outage takes on the way in. The mark ships
// on so an owner can look, the collection ships off so nothing deletes a
// person because Google was down.
//
// The grace window is not a formality either. A re-seed that purges its
// mirrors and mints them under new ids leaves every target orphaned for as
// long as the re-import takes, and the window is what stands between that and
// a sweep collecting the whole set mid-seed.
func (ds *dataset) collectOrphans(ctx context.Context) (int, error) {
	grace := ds.svc.orphanGrace
	if grace <= 0 {
		return 0, nil
	}
	cutoff := nowUTC().Add(-grace)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, id FROM records
		WHERE deleted_at IS NULL AND orphaned_at IS NOT NULL AND orphaned_at < $1
		ORDER BY orphaned_at LIMIT $2`, cutoff, gcBatch)
	if err != nil {
		return 0, err
	}
	var victims []eref
	for rows.Next() {
		var v eref
		if err := rows.Scan(&v.Kind, &v.ID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		victims = append(victims, v)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	n := 0
	for _, ref := range victims {
		collected, err := ds.collectOrphan(ctx, ref, cutoff)
		if err != nil {
			return n, err
		}
		if collected {
			n++
		}
	}
	return n, nil
}

// collectOrphan tombstones one marked record, re-deciding everything under
// the record's own lock: the mark the pass read is a row it read outside a
// transaction, and a write since then may have re-linked the record, pinned a
// property on it or pointed something at it. The mark is RE-DERIVED rather
// than trusted, because the cost of a stale one here is a deleted record.
func (ds *dataset) collectOrphan(ctx context.Context, ref eref, cutoff time.Time) (bool, error) {
	collected := false
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		// The record's advisory lock before its row lock, the order every
		// addressed write keeps (gc.go gcPass takes the same one).
		if err := t.lockRecord(ref); err != nil {
			return err
		}
		row, err := t.loadRow(ref, true)
		if err != nil || row == nil || row.DeletedAt != nil {
			return err
		}
		at, err := t.orphanedAt(ref)
		if err != nil || at == nil || at.After(cutoff) {
			return err
		}
		orphan, err := t.isOrphan(ref)
		if err != nil || !orphan {
			return err
		}
		referenced, err := t.referencedByLive(ref)
		if err != nil || referenced {
			return err
		}
		if _, err := t.tombstone(ref, ""); err != nil {
			return err
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpGC, ref.ID, ref.Kind,
			map[string]any{"reason": "orphaned"}); err != nil {
			return err
		}
		// What every non-fold tombstone owes the mapping graph, after the
		// entry reporting it (mapping.go afterTombstone).
		if err := t.afterTombstone(ref); err != nil {
			return err
		}
		collected = true
		return nil
	})
	return collected, err
}

// deriveOrphansOf re-derives the mark for every live record of the given
// kinds. The rebuild and the import both run it (rebuild.go), because the
// column is derived and a replayed fold carries no marks; the vocabulary
// apply runs it for a kind whose mapping set changed, where a kind that lost
// its last mapping has to lose every mark with it.
func (t *txn) deriveOrphansOf(kinds []string) error {
	for _, kind := range kinds {
		ids, err := t.liveIDsOf(kind)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := t.syncOrphaned(eref{Kind: kind, ID: id}); err != nil {
				return err
			}
		}
	}
	return nil
}

// clearOrphanMarks drops every mark on a kind. The vocabulary apply calls it
// before re-deriving, so a kind whose last mapping was removed keeps none:
// with nothing mapping onto it, no record of it is ever visited again.
func (t *txn) clearOrphanMarks(kind string) error {
	_, err := t.exec(`UPDATE records SET orphaned_at = NULL WHERE kind = $1 AND orphaned_at IS NOT NULL`, kind)
	return err
}

// orphanTargets names every kind some mapping targets, in identity order.
func orphanTargets(reg *vocabulary.Registry) []string {
	targets := map[string]bool{}
	for _, m := range reg.Mappings() {
		targets[m.To] = true
	}
	return sortedKeys(targets)
}
