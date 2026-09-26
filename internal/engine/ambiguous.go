package engine

import (
	"fmt"
	"reflect"

	"github.com/geoah/substrate/internal/vocabulary"
)

// THE AMBIGUITY MARK. A mapping whose `onAmbiguous` is `park` leaves a source
// unlinked when its probe finds several candidates (#577, record 0103), and
// an unset slot alone does not say why: a source that offers nothing is
// unlinked too. `records.ambiguous_at` is the engine saying which: DERIVED
// STORAGE on the SOURCE row, the same class as `orphaned_at` on a target,
// never carried by the changelog and derived again by a rebuild.
//
// A source is marked when, under at least one mapping from its kind, its
// subject slot names no live record AND the mapping's probes find several
// candidates. The definition does not read the policy: under `oldest` and
// `mint` the source's own write links it, and so does the apply that sets the
// policy (mappingbackfill.go), so an unlinked ambiguous source under those
// policies is one an earlier binary left, and it is waiting exactly as a
// parked one is.
//
// It is a reading taken when the SOURCE is written. Settling the ambiguity
// (a merge, a delete) writes the candidates, not the source, so the mark
// stands until the source's next write, or the next apply of its mapping,
// links it and clears it. A rebuild and
// a mapping change read it again for every source of the kinds involved.

// markAmbiguous sets or clears one source's mark from what its own write just
// decided. The UPDATE touches the derived column alone: no version, no
// updated_at, no changelog entry, for the reason `orphaned_at` gives.
func (t *txn) markAmbiguous(src eref, parked bool) error {
	if parked {
		_, err := t.exec(
			`UPDATE records SET ambiguous_at = $3 WHERE kind = $1 AND id = $2 AND deleted_at IS NULL AND ambiguous_at IS NULL`,
			src.Kind, src.ID, t.now)
		return err
	}
	_, err := t.exec(
		`UPDATE records SET ambiguous_at = NULL WHERE kind = $1 AND id = $2 AND ambiguous_at IS NOT NULL`,
		src.Kind, src.ID)
	return err
}

// isAmbiguousSource answers the definition above from the stored row alone,
// for the passes that did not just write it.
func (t *txn) isAmbiguousSource(src eref) (bool, error) {
	row, err := t.loadRow(src, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return false, err
	}
	ty, ok := t.declarations().ByIdentity(row.Kind)
	if !ok {
		return false, nil
	}
	for _, m := range t.declarations().MappingsFrom(ty.Identity) {
		linked, err := t.subjectTargetOf(src, m.Property)
		if err != nil {
			return false, err
		}
		if linked.ID != "" {
			continue
		}
		_, candidates, err := t.matchSubject(row, ty, m)
		if err != nil {
			return false, err
		}
		if len(candidates) > 1 {
			return true, nil
		}
	}
	return false, nil
}

// deriveAmbiguousOf clears and re-derives the mark on every live record of
// the given source kinds.
func (t *txn) deriveAmbiguousOf(kinds []string) error {
	for _, kind := range kinds {
		if _, err := t.exec(`UPDATE records SET ambiguous_at = NULL WHERE kind = $1 AND ambiguous_at IS NOT NULL`, kind); err != nil {
			return fmt.Errorf("substrate/engine: clear the ambiguity marks of %s: %w", kind, err)
		}
		if len(t.declarations().MappingsFrom(kind)) == 0 {
			continue
		}
		ids, err := t.liveIDsOf(kind)
		if err != nil {
			return err
		}
		for _, id := range ids {
			ref := eref{Kind: kind, ID: id}
			parked, err := t.isAmbiguousSource(ref)
			if err != nil {
				return err
			}
			if parked {
				if err := t.markAmbiguous(ref, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ambiguitySources names every kind some mapping reads, in identity order.
func ambiguitySources(reg *vocabulary.Registry) []string {
	sources := map[string]bool{}
	for _, m := range reg.Mappings() {
		sources[m.From] = true
	}
	return sortedKeys(sources)
}

// changedMappingSources lists the source kinds whose mapping set differs
// between two registries: a mapping added, removed or redefined, in identity
// order.
func changedMappingSources(old, cand *vocabulary.Registry) []string {
	sources := map[string]bool{}
	for _, m := range old.Mappings() {
		sources[m.From] = true
	}
	for _, m := range cand.Mappings() {
		sources[m.From] = true
	}
	var out []string
	for _, k := range sortedKeys(sources) {
		if !reflect.DeepEqual(old.MappingsFrom(k), cand.MappingsFrom(k)) {
			out = append(out, k)
		}
	}
	return out
}
