package engine

import (
	"context"
	"encoding/json"

	"github.com/geoah/substrate/internal/substrate"
)

// gcBatch bounds one sweep so a huge cascade stays incremental.
const gcBatch = 200

// RunGC performs one owner-pointer mark-and-collect sweep: tombstoned records
// with no remaining finalizers are hard-deleted, and every record whose
// `onDelete: cascade` reference named one of them is tombstoned so the next
// pass collects it. Iterates to a fixpoint within the sweep.
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

// ownedChild is one record the cascade collects: its kind (typ) and id.
type ownedChild struct{ id, typ string }

// cascadeOwned tombstones every live record that points at the record about to
// be hard-deleted through a reference whose declaration says `onDelete:
// cascade`. It is ONE query over the refs index (refs.go) plus a declaration
// check: the index is keyed on the target, so the sources are found without
// enumerating the kinds that could point here, and a cascade works unpinned.
//
// It matches every id the owner has ever had, not only the canonical one. A
// merge leaves reference values alone (they resolve forward through the
// former-id trail on read), so a record synced before its owner won a merge
// still names the loser id, and reading only the canonical id would walk past
// it.
//
// It is not limited. `gcBatch` bounds how many tombstoned records one pass
// collects; the cascade of ONE of them has to be complete, because `gcPass`
// hard-deletes the owner in the same transaction and a second pass would have
// nothing left to walk from.
func (t *txn) cascadeOwned(owner eref) error {
	children, err := t.cascadeChildren(owner)
	if err != nil {
		return err
	}
	seen := map[eref]bool{}
	for _, c := range children {
		ref := eref{Kind: c.typ, ID: c.id}
		if seen[ref] {
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

// cascadeChildren lists the live records whose cascading reference names the
// owner. The refs index answers WHICH records point here and through which
// property; the declaration decides whether that property cascades, so an
// ordinary pointer at the same record is left alone.
//
// Only a kind's OWN top-level reference may declare `onDelete:` (the loader
// refuses the other shapes), which is why the path filter is exact rather than
// a prefix: a pointer nested inside an object names no single owner.
func (t *txn) cascadeChildren(owner eref) ([]ownedChild, error) {
	ids, err := t.idsOf(owner)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := t.query(`
		SELECT r.property, r.src, r.src_kind FROM refs r
		JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE r.dst_kind = $1 AND r.dst IN (SELECT jsonb_array_elements_text($2::jsonb))
		  AND r.path = '' AND x.deleted_at IS NULL
		ORDER BY r.src_kind, r.src, r.property`, owner.Kind, raw)
	if err != nil {
		return nil, err
	}
	type candidate struct{ property, id, typ string }
	var found []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.property, &c.id, &c.typ); err != nil {
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
		p, ok := ty.Prop(c.property)
		if !ok || !p.Cascades() {
			continue
		}
		out = append(out, ownedChild{id: c.id, typ: c.typ})
	}
	return out, nil
}
