package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// groupCore names the substrate's own machinery: none of it merges (§6).
const groupCore = "core.substrate.reamde.dev"

// Merge and split, the two manual verbs. Nothing fuses by
// value: two people holding one email address are two records until an owner
// merges them.

func (ds *dataset) Merge(ctx context.Context, actor substrate.Actor, typ, winner, loser string) (*substrate.Record, error) {
	var out *substrate.Record
	err := ds.inTx(ctx, actor, false, func(t *txn) error {
		ty, err := t.ds.resolveType(typ)
		if err != nil {
			return err
		}
		e, err := t.mergeRecord(eref{Kind: ty.Identity, ID: winner}, eref{Kind: ty.Identity, ID: loser})
		out = e
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// mergeRecord performs the merge and returns its command record — the moved
// sets it carries are what makes split possible. Merge joins two records of
// ONE type; the refs must agree.
func (t *txn) mergeRecord(winnerRef, loserRef eref) (*substrate.Record, error) {
	if winnerRef.Kind != loserRef.Kind {
		return nil, fmt.Errorf("%w: cannot merge %s into %s", substrate.ErrValidation, loserRef.Kind, winnerRef.Kind)
	}
	if winnerRef == loserRef {
		return nil, fmt.Errorf("%w: cannot merge a record into itself", substrate.ErrValidation)
	}
	winnerID, loserID := winnerRef.ID, loserRef.ID
	first, second := winnerRef, loserRef
	if second.less(first) {
		first, second = second, first
	}
	// Locks in ascending (type, id) order: concurrent merges decide one after
	// the other instead of deadlocking.
	if err := t.lockRecord(first); err != nil {
		return nil, err
	}
	if err := t.lockRecord(second); err != nil {
		return nil, err
	}
	if _, err := t.loadRow(first, true); err != nil {
		return nil, err
	}
	if _, err := t.loadRow(second, true); err != nil {
		return nil, err
	}
	winner, err := t.loadRow(winnerRef, false)
	if err != nil {
		return nil, err
	}
	// What the winner was before the merge folds the loser's labels and title
	// into it: the entry carries that delta (fold.go).
	winnerBefore := winner.clone()
	loser, err := t.loadRow(loserRef, false)
	if err != nil {
		return nil, err
	}
	if winner == nil || loser == nil {
		return nil, fmt.Errorf("%w: merge needs two records", substrate.ErrNotFound)
	}
	ty, err := t.ds.resolveType(winner.Kind)
	if err != nil {
		return nil, err
	}
	if err := guardMergeType(ty); err != nil {
		return nil, err
	}
	// Replay idempotence: this exact merge already happened — the loser is
	// tombstoned and its id resolves onto the winner's canonical record with
	// the merge record still open. Re-applying is a VERIFIED no-op returning
	// that record, so a replayed merge effect (or a retried Merge) never
	// parks and never double-merges. Anything else falls through to the
	// ordinary refusals below.
	if loser.DeletedAt != nil {
		canonLoser, err := t.canonicalOf(loserRef)
		if err != nil {
			return nil, err
		}
		canonWinner, err := t.canonicalOf(winnerRef)
		if err != nil {
			return nil, err
		}
		if canonLoser == canonWinner {
			rec, err := t.openMergeOf(loserRef)
			if err != nil {
				return nil, err
			}
			if rec != "" {
				return t.recordOf(eref{Kind: kindRecordMerge, ID: rec})
			}
		}
	}
	for _, r := range []*erow{winner, loser} {
		if r.DeletedAt != nil {
			return nil, fmt.Errorf("%w: %s is deleted; merge needs two live records", substrate.ErrConflict, r.ID)
		}
		open, err := t.openMergeOf(r.ref())
		if err != nil {
			return nil, err
		}
		if open != "" {
			return nil, fmt.Errorf("%w: %s was already merged away by %s; split that merge first",
				substrate.ErrConflict, r.ID, open)
		}
	}

	// Bundle lifecycle admission, same rules as put/patch/delete (wave-3
	// review #8): merge mutates rows directly, so without this a disabled
	// bundle's frozen accounts — or an uninstalled bundle's read-only rows —
	// could be merged through the back door. Both participants share the one
	// checked type. After the replay branch above: a verified no-op replays
	// clean whatever the lifecycle says, because it writes nothing.
	if err := t.checkBundleDelete(ty); err != nil {
		return nil, err
	}

	moved := map[string]any{}

	// The loser's own trail re-points at the winner, so trails stay FLAT:
	// A→B then B→C leaves A and B naming C directly. The
	// loser's own former-id row is written after the merge record, below.
	movedFormer, err := t.moveFormerIDs(ty.Identity, loserID, winnerID)
	if err != nil {
		return nil, err
	}
	moved["formerIds"] = movedFormer

	// NOTHING REPOINTS. Every pointer at the loser — a source record's subject,
	// an ordinary reference, an owner pointer — is a value in some other
	// record's own properties, and it goes on resolving to the winner through
	// the former-id trail on read (query.go Incoming, gc.go cascadeChildren,
	// mapping.go subjectSourcesOf all match the trail). Rewriting those values
	// would be this verb reaching into records it was not asked about, and split
	// would then have to put every one of them back.

	// Properties do NOT migrate: the winner now has more
	// sources pointing at it, so §7.1 recomputes them — through the same
	// yield rules, so a hand edit on the winner survives its own merge.
	// Copying values across would freeze a stale answer into the winner.
	// The loser's MANAGER rows migrate where the winner has none — tier and
	// all, recorded so split can put them back — and its offer rows go:
	// they are recompute's projection, and the winner's recompute rebuilds
	// them from the sources that moved.
	movedManagers, err := t.moveManagers(loserRef, winnerRef)
	if err != nil {
		return nil, err
	}
	moved["managers"] = movedManagers
	if _, err := t.exec(`DELETE FROM property_offers WHERE record_kind = $1 AND record_id = $2`,
		loserRef.Kind, loserID); err != nil {
		return nil, err
	}

	// Labels: the winner's stand, the loser's fill the gaps. The value goes
	// into the record too, so split can tell a still-moved label from one
	// the owner has since rewritten.
	var movedLabels []map[string]any
	for _, k := range sortedKeys(loser.Labels) {
		if _, ok := winner.Labels[k]; ok {
			continue
		}
		winner.Labels[k] = loser.Labels[k]
		movedLabels = append(movedLabels, map[string]any{"key": k, "value": loser.Labels[k]})
	}
	moved["labels"] = movedLabels

	allAnn, overwritten, applied, err := t.moveAnnotations(loserRef, winnerRef)
	if err != nil {
		return nil, err
	}
	moved["annotations"] = allAnn
	moved["applied"] = applied
	moved["overwritten"] = overwritten

	// The loser is tombstoned, not erased: a finalizer holds GC off so the
	// merge stays reversible until someone splits it.
	if _, err := t.tombstone(loserRef, finalizerMerge); err != nil {
		return nil, err
	}
	title, err := t.deriveTitle(ty, winner)
	if err != nil {
		return nil, err
	}
	winner.Title = title
	if _, err := t.foldRow(winnerBefore, winner, true, false); err != nil {
		return nil, err
	}
	// Everything above rewrote the graph around the pair with set-shaped
	// statements, which no delta describes. The resync effect carries the
	// after-state of every side-store row keyed on the affected records, so the
	// entry the next line appends is one a rebuild can replay (fold.go).
	if err := t.recordResync([]eref{winnerRef, loserRef}); err != nil {
		return nil, err
	}
	if err := t.appendChange(t.actor, substrate.OpMerge, winnerID, winner.Kind, map[string]any{
		"winner": winnerID, "loser": loserID, "moved": moved,
	}); err != nil {
		return nil, err
	}
	// The trail goes down BEFORE the recompute: nothing repoints, so every
	// source that named the loser still names it, and the recompute finds those
	// sources by walking the winner's former ids (mapping.go subjectSourcesOf).
	// Written after the entry above, so it rides that entry's effects.
	if err := t.recordFormerID(ty.Identity, loserID, winnerID); err != nil {
		return nil, err
	}
	// The winner's source set just grew: recompute it.
	if err := t.recompute(winnerRef); err != nil {
		return nil, err
	}
	// The record names both sides with unpinned references. Nothing here
	// canonicalizes — a reference stores the id it was written with — so the
	// `loser` value keeps naming the loser even though the trail above now
	// resolves that id forward on read. Unpinned, so each value carries its
	// kind.
	rec, err := t.ds.putIn(t, substrate.PutInput{
		Kind: kindRecordMerge,
		Properties: map[string]any{
			"moved":  moved,
			"winner": vocabulary.RecordPath(winner.Kind, winnerID),
			"loser":  vocabulary.RecordPath(loser.Kind, loserID),
		},
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// guardMergeType keeps the substrate's own state out of the generic merge
// surface: a repository is not merged into another repository.
func guardMergeType(ty *vocabulary.Kind) error {
	if systemKinds[ty.Identity] || ty.Authority == groupCore {
		return fmt.Errorf("%w: %s records are managed by the substrate, not the generic merge surface",
			substrate.ErrForbidden, ty.Identity)
	}
	return nil
}

// recordOf loads one row and projects it, whatever its lifecycle state —
// the shape a verified replay no-op returns.
func (t *txn) recordOf(ref eref) (*substrate.Record, error) {
	row, err := t.loadRow(ref, false)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("%w: record %s", substrate.ErrNotFound, ref.ID)
	}
	ty, err := t.ds.resolveType(row.Kind)
	if err != nil {
		return nil, err
	}
	return t.record(row, ty)
}

// splitRecordOf returns the newest split record pointing at one merge
// record, "" when it was never split.
func (t *txn) splitRecordOf(mergeID string) (string, error) {
	var rec string
	err := t.row(`SELECT r.src FROM refs r JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE r.property = 'merge' AND r.path = '' AND r.dst_kind = $3 AND r.dst = $1 AND x.kind = $2
		ORDER BY x.created_at DESC, x.id LIMIT 1`, mergeID, kindRecordSplit, kindRecordMerge).Scan(&rec)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return rec, err
}

// openMergeOf returns the merge record that merged this record away and has not
// been split, if any. The merge record names its loser with a reference, and the
// refs index is what makes that reverse read one statement.
func (t *txn) openMergeOf(ref eref) (string, error) {
	var rec string
	err := t.row(`SELECT r.src FROM refs r JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE r.property = 'loser' AND r.path = '' AND r.dst_kind = $1 AND r.dst = $2
		  AND x.kind = $3 AND x.deleted_at IS NULL
		ORDER BY x.created_at, x.id LIMIT 1`, ref.Kind, ref.ID, kindRecordMerge).Scan(&rec)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return rec, err
}

// moveManagers copies the loser's property-manager rows onto the winner
// where the winner has none — the winner's stand (§6.4) — tier included: an
// bundle pin on the loser is a bundle pin on the winner, and the
// recorded set says so, so split can take exactly it back. The loser keeps
// its own rows: its property values stay on the tombstone, and their
// attribution with them.
func (t *txn) moveManagers(loserRef, winnerRef eref) ([]map[string]any, error) {
	rows, err := t.query(`
		SELECT l.property, l.actor, l.tier, l.principal, l.updated_at FROM property_managers l
		WHERE l.record_kind = $1 AND l.record_id = $2 AND NOT EXISTS (
			SELECT 1 FROM property_managers w
			WHERE w.record_kind = $1 AND w.record_id = $3 AND w.property = l.property)
		ORDER BY l.property`, loserRef.Kind, loserRef.ID, winnerRef.ID)
	if err != nil {
		return nil, err
	}
	type mrow struct {
		property, actor, tier, principal string
		updatedAt                        time.Time
	}
	var migrate []mrow
	for rows.Next() {
		var m mrow
		if err := rows.Scan(&m.property, &m.actor, &m.tier, &m.principal, &m.updatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		migrate = append(migrate, m)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	var out []map[string]any
	for _, m := range migrate {
		if _, err := t.exec(`
			INSERT INTO property_managers (record_kind, record_id, property, actor, tier, principal, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (repository, record_kind, record_id, property) DO NOTHING`,
			winnerRef.Kind, winnerRef.ID, m.property, m.actor, m.tier, m.principal, m.updatedAt); err != nil {
			return nil, err
		}
		// The principal travels with the actor and the tier: a later write by
		// another token moves the row without moving either of those, and the
		// split must be able to tell that row from the one it migrated.
		out = append(out, map[string]any{
			"property": m.property, "actor": m.actor, "tier": m.tier, "principal": m.principal,
		})
	}
	return out, nil
}

// moveAnnotations resolves colliding keys newest-wins. all records every one
// of the loser's annotations (split re-materializes them), applied names the
// keys actually written onto the winner, and overwritten preserves the
// winner's losing values.
func (t *txn) moveAnnotations(loserRef, winnerRef eref) (all, overwritten []map[string]any, applied []string, err error) {
	rows, err := t.query(`SELECT key, value, updated_at FROM annotations WHERE record_kind = $1 AND record_id = $2 ORDER BY key`,
		loserRef.Kind, loserRef.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	type ann struct {
		key       string
		value     []byte
		updatedAt time.Time
	}
	var loserAnns []ann
	for rows.Next() {
		var a ann
		if err := rows.Scan(&a.key, &a.value, &a.updatedAt); err != nil {
			_ = rows.Close()
			return nil, nil, nil, err
		}
		loserAnns = append(loserAnns, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, nil, err
	}
	_ = rows.Close()

	for _, a := range loserAnns {
		all = append(all, map[string]any{"key": a.key, "value": rawJSON(a.value)})
		var winnerVal []byte
		var winnerAt time.Time
		err := t.row(`SELECT value, updated_at FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3`,
			winnerRef.Kind, winnerRef.ID, a.key).Scan(&winnerVal, &winnerAt)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := t.exec(`
				INSERT INTO annotations (record_kind, record_id, key, value, updated_at) VALUES ($1, $2, $3, $4::jsonb, $5)`,
				winnerRef.Kind, winnerRef.ID, a.key, a.value, a.updatedAt); err != nil {
				return nil, nil, nil, err
			}
			applied = append(applied, a.key)
		case err != nil:
			return nil, nil, nil, err
		default:
			if a.updatedAt.After(winnerAt) {
				if _, err := t.exec(`
					UPDATE annotations SET value = $4::jsonb, updated_at = $5 WHERE record_kind = $1 AND record_id = $2 AND key = $3`,
					winnerRef.Kind, winnerRef.ID, a.key, a.value, a.updatedAt); err != nil {
					return nil, nil, nil, err
				}
				applied = append(applied, a.key)
				overwritten = append(overwritten, map[string]any{"key": a.key, "value": rawJSON(winnerVal)})
			}
		}
	}
	if _, err := t.exec(`DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2`,
		loserRef.Kind, loserRef.ID); err != nil {
		return nil, nil, nil, err
	}
	return all, overwritten, applied, nil
}

func (ds *dataset) Split(ctx context.Context, actor substrate.Actor, mergeID string) (*substrate.Record, error) {
	var out *substrate.Record
	err := ds.inTx(ctx, actor, false, func(t *txn) error {
		e, err := t.split(mergeID)
		out = e
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (t *txn) split(mergeID string) (*substrate.Record, error) {
	rec, err := t.loadRow(eref{Kind: kindRecordMerge, ID: mergeID}, true)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("%w: merge record %s", substrate.ErrNotFound, mergeID)
	}
	if rec.DeletedAt != nil {
		// Replay idempotence: a merge record tombstones exactly when its
		// split commits, so a tombstoned record was ALREADY split — the
		// replay is a verified no-op returning the split record instead of
		// repeating the mutation and minting another one. (The FOR UPDATE
		// row lock above serializes concurrent splits into this branch.)
		splitID, err := t.splitRecordOf(mergeID)
		if err != nil {
			return nil, err
		}
		if splitID != "" {
			return t.recordOf(eref{Kind: kindRecordSplit, ID: splitID})
		}
		return nil, fmt.Errorf("%w: merge record %s is deleted", substrate.ErrConflict, mergeID)
	}
	winnerRef := referenceTargetOf(rec, "winner")
	loserRef := referenceTargetOf(rec, "loser")
	moved, _ := rec.Props["moved"].(map[string]any)
	if winnerRef.ID == "" || loserRef.ID == "" {
		return nil, fmt.Errorf("%w: merge record %s is incomplete", substrate.ErrValidation, mergeID)
	}
	winnerID, loserID := winnerRef.ID, loserRef.ID
	loser, err := t.loadRow(loserRef, true)
	if err != nil {
		return nil, err
	}
	// The pre-split images both rows are diffed against (fold.go).
	loserBefore := loser.clone()
	if loser == nil {
		return nil, fmt.Errorf("%w: merged-away record %s", substrate.ErrNotFound, loserID)
	}
	loserTy, err := t.ds.resolveType(loser.Kind)
	if err != nil {
		return nil, err
	}
	// Split RESURRECTS the loser, so it passes the same admission a create
	// does: the bundle lifecycle rules (a disabled bundle's inputs and
	// accounts are frozen). Checked BEFORE any graph surgery below, so a
	// refusal leaves nothing half-moved.
	if err := t.checkBundleWrite(loserTy, loserID, true); err != nil {
		return nil, err
	}
	// The same admission for the embeddings claim, and for the same reason.
	// A merge does not migrate properties, so a tombstoned llmprovider row
	// keeps its embedModel while another row is free to take the job; without
	// this, a split would land a second live claimant that no write path ever
	// admitted, and the refusal would surface later, to whoever searched.
	if loserTy.Identity == typeProvider {
		if err := t.admitProviderRow(loserID, loser.Props); err != nil {
			return nil, err
		}
	}

	// The loser answers to its own id again, and the trail the merge moved
	// onto the winner goes back with it. Trails are per-type; the pair share
	// the loser's type.
	if _, err := t.exec(`DELETE FROM former_ids WHERE record_kind = $1 AND former_id = $2`,
		loserRef.Kind, loserID); err != nil {
		return nil, err
	}
	for _, former := range stringsOf(moved["formerIds"]) {
		if _, err := t.exec(`
			UPDATE former_ids SET record_id = $3 WHERE record_kind = $1 AND former_id = $2 AND record_id = $4`,
			loserRef.Kind, former, loserID, winnerID); err != nil {
			return nil, err
		}
	}
	// The manager rows the merge migrated go back too — only where they
	// still name the migrated actor, tier AND principal, because a row
	// somebody has since claimed records a write the split must not erase.
	// A merge record written before the principal was recorded carries no
	// such key, and the empty string is the right match for it: migration
	// 0006 stamped exactly that on every row it found, so a row that names
	// anything else was written by a token after the upgrade.
	for _, m := range mapsOf(moved["managers"]) {
		property, _ := m["property"].(string)
		actor, _ := m["actor"].(string)
		tier, _ := m["tier"].(string)
		principal, _ := m["principal"].(string)
		if property == "" || actor == "" || tier == "" {
			continue
		}
		if _, err := t.exec(`
			DELETE FROM property_managers
			WHERE record_kind = $1 AND record_id = $2 AND property = $3
			  AND actor = $4 AND tier = $5 AND principal = $6`,
			winnerRef.Kind, winnerID, property, actor, tier, principal); err != nil {
			return nil, err
		}
	}
	winner, err := t.loadRow(winnerRef, true)
	if err != nil {
		return nil, err
	}
	winnerBefore := winner.clone()
	// A split reverts the merge, not everything that happened after it: a
	// key whose current value is no longer the one the merge moved was
	// rewritten by its owner since, and that write stands.
	var skippedLabels, skippedAnnotations []string
	if winner != nil {
		for _, m := range movedLabels(moved["labels"], loser) {
			cur, ok := winner.Labels[m.key]
			if !ok {
				continue
			}
			if !jsonEqual(cur, m.value) {
				skippedLabels = append(skippedLabels, m.key)
				continue
			}
			delete(winner.Labels, m.key)
		}
	}
	applied := map[string]bool{}
	for _, k := range stringsOf(moved["applied"]) {
		applied[k] = true
	}
	skipped := map[string]bool{}
	for _, a := range mapsOf(moved["annotations"]) {
		key, _ := a["key"].(string)
		raw, err := jsonb(a["value"])
		if err != nil {
			return nil, err
		}
		if applied[key] {
			same, err := t.annotationIs(winnerRef, key, raw)
			if err != nil {
				return nil, err
			}
			if !same {
				skipped[key] = true
				skippedAnnotations = append(skippedAnnotations, key)
			} else if _, err := t.exec(`DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3`,
				winnerRef.Kind, winnerID, key); err != nil {
				return nil, err
			}
		}
		if _, err := t.exec(`
			INSERT INTO annotations (record_kind, record_id, key, value, updated_at) VALUES ($1, $2, $3, $4::jsonb, $5)
			ON CONFLICT (repository, record_kind, record_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
			loserRef.Kind, loserID, key, raw, t.now); err != nil {
			return nil, err
		}
	}
	for _, a := range mapsOf(moved["overwritten"]) {
		key, _ := a["key"].(string)
		if skipped[key] {
			continue
		}
		raw, err := jsonb(a["value"])
		if err != nil {
			return nil, err
		}
		if _, err := t.exec(`
			INSERT INTO annotations (record_kind, record_id, key, value, updated_at) VALUES ($1, $2, $3, $4::jsonb, $5)
			ON CONFLICT (repository, record_kind, record_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
			winnerRef.Kind, winnerID, key, raw, t.now); err != nil {
			return nil, err
		}
	}

	loser.DeletedAt = nil
	loser.Finalizers = mergeFinalizers(loser.Finalizers, nil, []string{finalizerMerge})
	title, err := t.deriveTitle(loserTy, loser)
	if err != nil {
		return nil, err
	}
	loser.Title = title
	if _, err := t.foldRow(loserBefore, loser, true, true); err != nil {
		return nil, err
	}
	if winner != nil {
		winnerTy, err := t.ds.resolveType(winner.Kind)
		if err != nil {
			return nil, err
		}
		wtitle, err := t.deriveTitle(winnerTy, winner)
		if err != nil {
			return nil, err
		}
		winner.Title = wtitle
		if _, err := t.foldRow(winnerBefore, winner, true, false); err != nil {
			return nil, err
		}
	}
	result := map[string]any{"winner": winnerID, "loser": loserID, "moved": moved}
	if len(skippedLabels) > 0 || len(skippedAnnotations) > 0 {
		result["skipped"] = map[string]any{
			"labels": skippedLabels, "annotations": skippedAnnotations,
		}
	}
	// The undo is a rewrite too, over the same scope the merge named: the
	// entry carries the graph it put back (fold.go).
	if err := t.recordResync([]eref{winnerRef, loserRef}); err != nil {
		return nil, err
	}
	if err := t.appendChange(t.actor, substrate.OpSplit, loserID, loser.Kind, result); err != nil {
		return nil, err
	}
	// The loser answers to its own id again, so the sources that name it are its
	// own once more: both sides recompute from the sets they now have.
	for _, ref := range []eref{winnerRef, loserRef} {
		if err := t.recompute(ref); err != nil {
			return nil, err
		}
	}
	if _, err := t.tombstone(eref{Kind: kindRecordMerge, ID: mergeID}, ""); err != nil {
		return nil, err
	}
	return t.ds.putIn(t, substrate.PutInput{
		Kind: kindRecordSplit,
		Properties: map[string]any{
			"result": result,
			"merge":  vocabulary.RecordPath(kindRecordMerge, mergeID),
		},
	})
}

// annotationIs reports whether the stored annotation is still byte-for-byte
// the value a merge recorded.
func (t *txn) annotationIs(ref eref, key string, raw []byte) (bool, error) {
	var same bool
	err := t.row(`SELECT value IS NOT DISTINCT FROM $4::jsonb FROM annotations
		WHERE record_kind = $1 AND record_id = $2 AND key = $3`, ref.Kind, ref.ID, key, raw).Scan(&same)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return same, err
}

// movedLabel is one label a merge moved onto the winner, with the value it
// moved.
type movedLabel struct {
	key   string
	value any
}

// movedLabels reads a merge record's label set.
func movedLabels(v any, loser *erow) []movedLabel {
	var out []movedLabel
	for _, m := range mapsOf(v) {
		if k, _ := m["key"].(string); k != "" {
			out = append(out, movedLabel{key: k, value: m["value"]})
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, k := range stringsOf(v) {
		out = append(out, movedLabel{key: k, value: loser.Labels[k]})
	}
	return out
}

// putIn writes a system record inside an existing transaction.
func (ds *dataset) putIn(t *txn, in substrate.PutInput) (*substrate.Record, error) {
	was := t.internal
	t.internal = true
	defer func() { t.internal = was }()
	return t.put(in)
}

func mapsOf(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringsOf(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func rawJSON(raw []byte) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
