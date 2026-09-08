package engine

// A PROPERTY RENAME IS ORDINARY RECORD WRITES (decision 0063). A declaration
// that names its previous name with `renamedFrom:` is admitted while live
// records still carry that name, and the apply moves each record's value to
// the new name inside the apply's transaction, against the candidate registry:
// one record effect (a Set of the new name and a Del of the old, for every
// property the record renames at once) and one changelog entry per record,
// through the same fold every write takes. The fold reads no declaration, so a
// fresh replay reproduces the renamed records without ever reading the
// declaration that renamed them.
//
// What travels with the value: the property's manager row moves with its
// actor, tier, principal and stamp, because the rename changes where a value sits and
// not who last wrote it; the offer rows and the embeddings rows are rekeyed,
// because both are keyed by property and describe the same value; the old
// name's queue row is dropped and the new name enqueued where it declares
// `embed`, so a worker mid-flight on the old name finds no row and writes
// nothing (commitEmbedding). The sealed store is keyed by record, so a secret's
// opaque ref moves as any other value does.
//
// The bound is the live count: a kind with N records carrying an old name
// costs N entries and N row rewrites in one transaction, under the vocabulary
// write mutex and the exclusive registry-dependency lock. Nothing caps it.

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// renameProperties performs every rename a batch declares and reports how
// many records it rewrote. It runs after the declaration rows projected and
// before the refs index re-derives, so the index reads the renamed properties.
func (t *txn) renameProperties(candidate *vocabulary.Registry, renames []propertyRename) (int64, error) {
	if len(renames) == 0 {
		return 0, nil
	}
	// The fold reads the transaction's declarations for the search bands and
	// the refs index (foldRecordOp), and a row rewritten to the new name has
	// to index and project under the declaration that names it. The apply
	// door sets the candidate for its whole transaction; the boot upgrade
	// sets none, so it is set here for the rewrite alone.
	prev := t.writeReg
	t.writeReg = candidate
	defer func() { t.writeReg = prev }()
	// Grouped by kind, so a record renaming several properties at once is
	// rewritten once and appends one entry.
	byKind := map[string][]propertyRename{}
	for _, r := range renames {
		byKind[r.kind.Identity] = append(byKind[r.kind.Identity], r)
	}
	var total int64
	for _, ident := range sortedKeys(byKind) {
		n, err := t.renameKindProperties(byKind[ident])
		if err != nil {
			return total, fmt.Errorf("substrate/engine: rename properties of %s: %w", ident, err)
		}
		total += n
	}
	return total, nil
}

// renameKindProperties rewrites every live record of one kind that carries any
// of the old names, in id order.
func (t *txn) renameKindProperties(rs []propertyRename) (int64, error) {
	kind := rs[0].kind
	args := []any{kind.Identity}
	holds := make([]string, 0, len(rs))
	for _, r := range rs {
		args = append(args, r.from)
		holds = append(holds, "props ? $"+strconv.Itoa(len(args)))
	}
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL AND (`+
		strings.Join(holds, " OR ")+`) ORDER BY id`, args...)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	var n int64
	for _, id := range ids {
		moved, err := t.renameRecordProperties(rs, eref{Kind: kind.Identity, ID: id})
		if err != nil {
			return n, fmt.Errorf("record %s: %w", id, err)
		}
		if moved {
			n++
		}
	}
	// Offer rows are recompute's projection of the live sources, keyed by
	// property (mapping.go syncOffers). A mapping onto the old name cannot
	// compile against the candidate, so every row under it describes the value
	// that just moved; rekeyed, the alternatives a reader sees follow it.
	for _, r := range rs {
		// A stale row under the new name (a mapping onto a property dropped in
		// an earlier life) would collide with the rekey; nothing live backs it.
		if _, err := t.exec(`DELETE FROM property_offers WHERE record_kind = $1 AND property = $2`,
			kind.Identity, r.to); err != nil {
			return n, err
		}
		if _, err := t.exec(`UPDATE property_offers SET property = $3 WHERE record_kind = $1 AND property = $2`,
			kind.Identity, r.from, r.to); err != nil {
			return n, err
		}
	}
	return n, nil
}

// renameRecordProperties rewrites one record: every old name it carries moves
// to its new one in a single record effect. It reports false when the record
// is gone or carries none of the old names, which the id query excludes and a
// concurrent write cannot produce under the locks the apply holds.
func (t *txn) renameRecordProperties(rs []propertyRename, ref eref) (bool, error) {
	row, err := t.loadRow(ref, true)
	if err != nil || row == nil || row.DeletedAt != nil {
		return false, err
	}
	before := row.clone()
	var moved []propertyRename
	for _, r := range rs {
		value, held := row.Props[r.from]
		if !held {
			continue
		}
		row.Props[r.to] = value
		delete(row.Props, r.from)
		moved = append(moved, r)
	}
	if len(moved) == 0 {
		return false, nil
	}
	kind := rs[0].kind
	// The title renders under the candidate: a displayTemplate naming the new
	// property finds its value only once the value sits under that name.
	title, err := t.deriveTitle(kind, row)
	if err != nil {
		return false, err
	}
	row.Title = title
	// The rewritten row was validated against the candidate declaration (the
	// narrowing guards counted it under the old name), so it carries that
	// declaration's version, as a write through apply would (decision 0060).
	row.KindVersion = kind.Version
	res, err := t.foldRow(before, row, false, false)
	if err != nil {
		return false, err
	}
	if !res.changed {
		return false, nil
	}

	properties := make([]string, 0, 2*len(moved))
	renamed := make(map[string]string, len(moved))
	for _, r := range moved {
		if err := t.moveManager(ref, r.from, r.to); err != nil {
			return false, err
		}
		if err := t.moveEmbeddings(ref, r.from, r.to, kind.Props[r.to].Embed); err != nil {
			return false, err
		}
		properties = append(properties, r.from, r.to)
		renamed[r.from] = r.to
	}
	// One entry per record, as a patch: the record's properties changed, some
	// set and some deleted, and `renamed` says the apply moved them rather
	// than a writer.
	sort.Strings(properties)
	return true, t.appendChange(t.actor, substrate.OpPatch, ref.ID, ref.Kind, map[string]any{
		"properties": properties,
		"renamed":    renamed,
	})
}

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
