package engine

import (
	"context"
	"encoding/json"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// gcBatch bounds one sweep so a huge cascade stays incremental.
const gcBatch = 200

// RunGC performs one owner-pointer mark-and-collect sweep: tombstoned
// records with no remaining finalizers are hard-deleted, and every record whose
// owner pointer named one of them — an `ownerRef` edge or an `ownerRef`
// reference — is tombstoned so the next pass collects it. Iterates to a
// fixpoint within the sweep.
func (ds *dataset) RunGC(ctx context.Context) (int, error) {
	collected := 0
	for {
		n, err := ds.gcPass(ctx)
		if err != nil {
			return collected, err
		}
		collected += n
		if n == 0 {
			break
		}
	}
	// A blob with no referencing record is collectable: the same
	// sweep collects the bytes and tombstones the manifest. Runs after the
	// record fixpoint, so a blob freed by a just-collected referrer goes too.
	blobs, err := ds.blobGCPass(ctx)
	if err != nil {
		return collected, err
	}
	return collected + blobs, nil
}

func (ds *dataset) gcPass(ctx context.Context) (int, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, kind FROM records
		WHERE deleted_at IS NOT NULL AND cardinality(finalizers) = 0
		ORDER BY deleted_at LIMIT $1`, gcBatch)
	if err != nil {
		return 0, err
	}
	type victim struct{ id, typ string }
	var victims []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.id, &v.typ); err != nil {
			_ = rows.Close()
			return 0, err
		}
		victims = append(victims, v)
	}
	_ = rows.Close()
	if len(victims) == 0 {
		return 0, nil
	}

	n := 0
	for _, v := range victims {
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			ref := eref{Kind: v.typ, ID: v.id}
			row, err := t.loadRow(ref, true)
			if err != nil || row == nil {
				return err
			}
			if row.DeletedAt == nil || len(row.Finalizers) > 0 {
				return nil
			}
			if err := t.cascadeOwned(ref); err != nil {
				return err
			}
			// The purge lands BEFORE the entry that reports it: the entry
			// carries the effects folded since the previous one, so an effect
			// applied after its own append would ride on the next entry instead
			// (fold.go settleFold catches the rest).
			if err := t.hardDelete(ref); err != nil {
				return err
			}
			return t.appendChange(substrate.ActorSystem, substrate.OpGC, v.id, v.typ,
				map[string]any{"reason": "collected"})
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ownedChild is one record the cascade collects: its kind and id, plus the
// declaration that named the owner, for the sake of the error nobody reads
// until it happens.
type ownedChild struct{ id, typ string }

// cascadeOwned tombstones every live record that points at the record about to
// be hard-deleted through an owner pointer. There are TWO of those and they are
// found differently: an `ownerRef` EDGE is a row in `edges` and joins, an
// `ownerRef` REFERENCE is a value in the owner-naming record's own `props` and
// is probed. Both end in the same tombstone, so a kind that moves `account`
// from one spelling to the other keeps its cascade.
//
// Neither half is limited. `gcBatch` bounds how many tombstoned records one
// pass collects; the cascade of ONE of them has to be complete, because
// `gcPass` hard-deletes the owner in the same transaction and a second pass
// would have nothing left to walk from.
func (t *txn) cascadeOwned(owner eref) error {
	children, err := t.edgeOwnedChildren(owner)
	if err != nil {
		return err
	}
	refChildren, err := t.referenceOwnedChildren(owner)
	if err != nil {
		return err
	}
	children = append(children, refChildren...)

	seen := map[eref]bool{}
	for _, c := range children {
		ref := eref{Kind: c.typ, ID: c.id}
		if seen[ref] {
			// A kind mid-migration can hold both spellings at once (0032), and
			// the second tombstone would be a no-op the fold discards anyway —
			// but the changelog entry beside it would not be.
			continue
		}
		seen[ref] = true
		if _, err := t.tombstone(ref, ""); err != nil {
			return err
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpGC, c.id, c.typ,
			map[string]any{"reason": "owner_collected", "owner": owner.ID}); err != nil {
			return err
		}
	}
	return nil
}

// edgeOwnedChildren lists the live records whose `ownerRef` EDGE points at the
// owner. The rel it came in on is checked against the declaration, so an
// ordinary edge that happens to point here is left alone.
func (t *txn) edgeOwnedChildren(owner eref) ([]ownedChild, error) {
	rows, err := t.query(`
		SELECT e.rel, e.src, x.kind FROM edges e JOIN records x ON x.kind = e.src_kind AND x.id = e.src
		WHERE e.dst_kind = $1 AND e.dst = $2 AND x.deleted_at IS NULL`, owner.Kind, owner.ID)
	if err != nil {
		return nil, err
	}
	type candidate struct{ rel, id, typ string }
	var found []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.rel, &c.id, &c.typ); err != nil {
			_ = rows.Close()
			return nil, err
		}
		found = append(found, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	reg := t.ds.registry()
	var out []ownedChild
	for _, c := range found {
		ty, ok := reg.ByIdentity(c.typ)
		if !ok {
			continue
		}
		ed, ok := ty.Edge(c.rel)
		if !ok || !ed.OwnerRef {
			continue
		}
		out = append(out, ownedChild{id: c.id, typ: c.typ})
	}
	return out, nil
}

// referenceOwnedChildren lists the live records whose `ownerRef` REFERENCE
// names the owner. A reference is a stored VALUE and not a row, so there is
// nothing to join from: the REGISTRY says which (kind, property) pairs can
// point at this kind — an owner pointer is pinned, which is what makes that
// list finite — and each pair is one containment probe against `props`, served
// by `records_props_idx` (a GIN jsonb_path_ops index, which is exactly `@>`).
//
// It probes every id the owner has ever had, not only the canonical one. A
// merge REPOINTS edge rows and leaves reference values alone (merge.go: "every
// old reference still resolves"), so a record synced before its account won a
// merge still names the loser id, and reading only the canonical id would walk
// past it — the same list `incomingArms` probes, for the same reason.
func (t *txn) referenceOwnedChildren(owner eref) ([]ownedChild, error) {
	paths, err := t.ownerPaths(owner)
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	var out []ownedChild
	for _, k := range t.ds.registry().Kinds() {
		for _, pname := range k.PropOrder {
			p := k.Props[pname]
			if p.Datatype != vocabulary.DatatypeReference || !p.OwnerRef || p.To != owner.Kind {
				continue
			}
			for _, path := range paths {
				probe, err := json.Marshal(map[string]any{pname: path})
				if err != nil {
					return nil, err
				}
				rows, err := t.query(`
					SELECT id FROM records
					WHERE kind = $1 AND deleted_at IS NULL AND props @> $2::jsonb`, k.Identity, string(probe))
				if err != nil {
					return nil, err
				}
				for rows.Next() {
					var id string
					if err := rows.Scan(&id); err != nil {
						_ = rows.Close()
						return nil, err
					}
					out = append(out, ownedChild{id: id, typ: k.Identity})
				}
				if err := rows.Err(); err != nil {
					_ = rows.Close()
					return nil, err
				}
				_ = rows.Close()
			}
		}
	}
	return out, nil
}

// ownerPaths is every record path a stored reference may spell the owner as:
// the canonical one, plus one per id the record used to live under.
func (t *txn) ownerPaths(owner eref) ([]string, error) {
	ids := []string{owner.ID}
	rows, err := t.query(`SELECT former_id FROM former_ids WHERE record_kind = $1 AND record_id = $2`,
		owner.Kind, owner.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var former string
		if err := rows.Scan(&former); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, former)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		paths = append(paths, vocabulary.RecordPath(owner.Kind, id))
	}
	return paths, nil
}
