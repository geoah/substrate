package engine

import (
	"context"

	"github.com/geoah/substrate/internal/substrate"
)

// gcBatch bounds one sweep so a huge cascade stays incremental.
const gcBatch = 200

// RunGC performs one owner-reference mark-and-collect sweep: tombstoned
// records with no remaining finalizers are hard-deleted, and every record
// whose required owner_ref target went with them is tombstoned so the next
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

// cascadeOwned tombstones every live record whose required owner_ref edge
// pointed at the record about to be hard-deleted.
func (t *txn) cascadeOwned(owner eref) error {
	rows, err := t.query(`
		SELECT e.rel, e.src, x.kind FROM edges e JOIN records x ON x.kind = e.src_kind AND x.id = e.src
		WHERE e.dst_kind = $1 AND e.dst = $2 AND x.deleted_at IS NULL`, owner.Kind, owner.ID)
	if err != nil {
		return err
	}
	type child struct{ rel, id, typ string }
	var children []child
	for rows.Next() {
		var c child
		if err := rows.Scan(&c.rel, &c.id, &c.typ); err != nil {
			_ = rows.Close()
			return err
		}
		children = append(children, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	reg := t.ds.registry()
	for _, c := range children {
		ty, ok := reg.ByIdentity(c.typ)
		if !ok {
			continue
		}
		ed, ok := ty.Edge(c.rel)
		if !ok || !ed.OwnerRef {
			continue
		}
		if _, err := t.tombstone(eref{Kind: c.typ, ID: c.id}, ""); err != nil {
			return err
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpGC, c.id, c.typ,
			map[string]any{"reason": "owner_collected", "owner": owner.ID}); err != nil {
			return err
		}
	}
	return nil
}
