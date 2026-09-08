package engine

// A PROPERTY RENAME IS ORDINARY RECORD WRITES (decision 0063). A declaration
// that names its previous name with `renamedFrom:` is admitted while live
// records still carry that name, and the apply moves each record's value to
// the new name inside the apply's transaction, against the candidate registry:
// one record effect (a Set of the new name and a Del of the old, for every
// property the record renames at once) and one changelog entry per record,
// through the same fold every write takes. The rewrite itself is convert.go's,
// where a rename composes with a backfill and a remap on the same record; this
// file holds what travels with a renamed value.
//
// The property's manager row moves with its actor, tier, principal and stamp,
// because the rename changes where a value sits and not who last wrote it; the
// offer rows and the embeddings rows are rekeyed, because both are keyed by
// property and describe the same value; the old name's queue row is dropped
// and the new name enqueued where it declares `embed`, so a worker mid-flight
// on the old name finds no row and writes nothing (commitEmbedding). The sealed
// store is keyed by record, so a secret's opaque ref moves as any other value
// does.

import (
	"database/sql"
	"errors"
	"time"
)

// moveManager moves one property's manager row to the new name with its actor,
// tier, principal and stamp: who last had a change accepted on the value, and
// when, did not change; only the value's name did. A property nobody manages
// moves nothing.
func (t *txn) moveManager(ref eref, from, to string) error {
	var actor, tier, principal string
	var updatedAt time.Time
	err := t.row(`SELECT actor, tier, principal, updated_at FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
		ref.Kind, ref.ID, from).Scan(&actor, &tier, &principal, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := t.fold(foldOp{
		Kind: foldManager, Ref: ref.Kind, ID: ref.ID,
		Property: to, Actor: actor, Tier: tier, Principal: principal,
		UpdatedAt: &updatedAt,
	}); err != nil {
		return err
	}
	return t.deleteManager(ref, from)
}

// moveEmbeddings rekeys one property's vectors and queue. The vectors are of
// the text that just moved, so they move with it where the new name still
// embeds: the worker then finds every chunk's hash in place and buys nothing.
// Where the new name does not embed they go, as a drop plan would remove them.
// The old name's queue row goes either way, and the new name is enqueued
// where it embeds.
func (t *txn) moveEmbeddings(ref eref, from, to string, embed bool) error {
	// Nothing live owns a vector under the new name (the stored kind did not
	// declare it, renameGuards), but a property dropped in an earlier life
	// may have left rows there: they go first, or the rekey would collide on
	// the primary key and fail every boot that retries it.
	if _, err := t.exec(`DELETE FROM embeddings WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
		ref.Kind, ref.ID, to); err != nil {
		return err
	}
	if embed {
		if _, err := t.exec(`UPDATE embeddings SET property = $4 WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
			ref.Kind, ref.ID, from, to); err != nil {
			return err
		}
	} else if _, err := t.exec(`DELETE FROM embeddings WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
		ref.Kind, ref.ID, from); err != nil {
		return err
	}
	if _, err := t.exec(`DELETE FROM embed_queue WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
		ref.Kind, ref.ID, from); err != nil {
		return err
	}
	if !embed {
		return nil
	}
	return t.enqueueEmbed(ref, to)
}
