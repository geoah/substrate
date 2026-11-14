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
// The property's mark moves whole, because the rename changes where a value
// sits and not who wrote it, whether it is pinned or what it came from; the
// offer rows and the embeddings rows are rekeyed, because both are keyed by
// property and describe the same value; the old name's queue row is dropped
// and the new name enqueued where it declares `embed`, so a worker mid-flight
// on the old name finds no row and writes nothing (commitEmbedding). The sealed
// store is keyed by record, so a secret's opaque ref moves as any other value
// does.

// moveMark moves one property's mark to the new name, whole: who wrote the
// value, whether it is pinned, what it came from and when it was set did not
// change; only the value's name did. A property that carries no mark moves
// nothing.
func (t *txn) moveMark(ref eref, from, to string) error {
	marks, err := t.marksOf(ref)
	if err != nil {
		return err
	}
	m, ok := marks[from]
	if !ok {
		return nil
	}
	if err := t.setMark(ref, to, m); err != nil {
		return err
	}
	return t.unmark(ref, from)
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
