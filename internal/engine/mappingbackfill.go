package engine

import (
	"fmt"
	"reflect"

	"github.com/geoah/substrate/internal/vocabulary"
)

// THE MAPPING BACKFILL (#584, decision record 0106). A mapping resolves a
// source's subject on the SOURCE's write (write.go ensureSubject), and
// admitting a mapping is not a write to its sources, so a mirror synced
// before the mapping existed kept an empty slot until its provider happened to
// write that row again. The vocabulary apply closes the gap: every mapping it
// admits, changes or names by document links each live source whose slot names
// no live record, the way that source's own write would.

// backfilledMappings lists the candidate's mappings an apply links the
// existing sources of: every one the live registry does not hold identically
// (new or redefined), and every one a document in the batch names. The second
// half is what makes re-applying an unchanged mapping the door that reprojects
// it, for sources an earlier binary or an ambiguous probe left unlinked.
func backfilledMappings(live, cand *vocabulary.Registry, docs []vocabulary.Document) []*vocabulary.Mapping {
	named := map[string]bool{}
	for _, d := range docs {
		if d.Kind == vocabulary.DocRecordMapping {
			named[d.ID] = true
		}
	}
	var out []*vocabulary.Mapping
	for _, m := range cand.Mappings() {
		old, ok := live.MappingFor(m.From, m.Property)
		if named[m.Identity()] || !ok || !reflect.DeepEqual(old, m) {
			out = append(out, m)
		}
	}
	return out
}

// linkUnpointedSources runs the backfill for each mapping, against the
// transaction's declarations (the apply's candidate). Each link is the engine's
// own write of the source's slot (writeSubject), so it lands in the changelog,
// a rebuild replays it, and the source's write path recomputes the subject it
// now names and clears its ambiguity mark.
func (t *txn) linkUnpointedSources(ms []*vocabulary.Mapping) error {
	for _, m := range ms {
		srcTy, ok := t.declarations().ByIdentity(m.From)
		if !ok {
			continue
		}
		ids, err := t.unpointedSourceIDs(m)
		if err != nil {
			return fmt.Errorf("substrate/engine: list the unlinked sources of %s: %w", m.Identity(), err)
		}
		for _, id := range ids {
			if err := t.linkSource(eref{Kind: m.From, ID: id}, srcTy, m); err != nil {
				return fmt.Errorf("substrate/engine: link %s through %s: %w",
					vocabulary.RecordPath(m.From, id), m.Identity(), err)
			}
		}
	}
	return nil
}

// unpointedSourceIDs lists the live sources of a mapping whose slot names no
// live record by its stored id. It is a superset of what linkSource links: a
// slot naming a merged-away subject resolves through the former-id trail, and
// linkSource asks that of each row before it probes.
func (t *txn) unpointedSourceIDs(m *vocabulary.Mapping) ([]string, error) {
	rows, err := t.query(`
		SELECT r.id FROM records r
		WHERE r.kind = $1 AND r.deleted_at IS NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM refs f
		    JOIN records d ON d.kind = f.dst_kind AND d.id = f.dst AND d.deleted_at IS NULL
		    WHERE f.src_kind = r.kind AND f.src = r.id AND f.property = $2 AND f.path = '')
		ORDER BY r.id`, m.From, m.Property)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// linkSource decides one source's subject exactly as ensureSubject does on the
// source's own write: a probe that finds one candidate links it, one that finds
// none mints a shell, and a source that offers nothing, or parks on an
// ambiguous probe, stays unlinked. A slot the source kind declares `required:`
// keeps its unconditional mint.
func (t *txn) linkSource(src eref, srcTy *vocabulary.Kind, m *vocabulary.Mapping) error {
	if err := t.lockRecord(src); err != nil {
		return err
	}
	linked, err := t.subjectTargetOf(src, m.Property)
	if err != nil || linked.ID != "" {
		return err
	}
	row, err := t.loadRow(src, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return err
	}
	slot, declared := srcTy.Prop(m.Property)
	target, _, err := t.matchOrMint(row, srcTy, m, declared && slot.Required)
	if err != nil || target == "" {
		return err
	}
	return t.writeSubject(src, m.Property, eref{Kind: m.To, ID: target})
}
