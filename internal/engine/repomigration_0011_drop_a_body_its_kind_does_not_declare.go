package engine

// Repository migration 11: a `body` its kind does not declare (decision 0273).
//
// Before decision 0041 every record carried a body in a column of its own;
// after it the column backed a body only where the kind declared one, and a
// value written earlier on a kind that declares none stayed in the column,
// never served and never indexed. Conversion step 4 moves every body in the
// history in with the record's other properties, where a value is served
// whether its kind declares it or not, so this removes the body of every live
// record whose kind's usable declaration declares none: what a read served
// before the conversion is what it serves after. A record whose kind has no
// usable declaration keeps its body, as a read served it then: one with no
// stored declaration, and one whose package does not admit, which a listing
// serves with no kind. When its kind becomes usable (an install, or a
// quarantine lifted at an open) the body is dropped then (dropHiddenBodies),
// as the column would have hidden it.
//
// USABLE IS WHAT THE OPEN ADMITS. The migration runs at the first open after
// the boot check converted the history, before the stored vocabulary loads,
// and the load is what decides admission and writes the quarantine marks: a
// package this binary refuses for the first time at this very open carries
// no mark yet. So the migration builds the stored packages as the load
// builds them (storedPackages) and admits them by the load's own admission
// into a registry of its own (admitStoredInto), which writes nothing, and a
// kind is usable when its package admits there. A mark a package carries
// decides nothing here: the load lifts it off a package that admits, and
// sets it on one that does not.
//
// The rows are rewritten through the fold, as ordinary writes by the system;
// a record that carries no body, or whose kind declares one, is left as it is,
// and a second run finds nothing to do.

import (
	"database/sql"
	"fmt"
	"maps"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// undeclaredBody is the one property name this migration reads: the name
// that had a column of its own until dialect 4.
const undeclaredBody = "body"

func migrateDropABodyItsKindDoesNotDeclare(t *txn) (string, error) {
	built, _, err := t.ds.storedPackages(t.ctx, t.tx, nil)
	if err != nil {
		return "", err
	}
	good, _, _ := t.ds.admitStoredInto(vocabulary.NewRegistry(), built)
	var hidden []string
	for _, g := range good {
		for _, ty := range g.Kinds {
			if ty.Props[undeclaredBody] == nil {
				hidden = append(hidden, ty.Identity)
			}
		}
	}
	rows, err := t.query(`
		SELECT kind, id FROM records
		 WHERE deleted_at IS NULL AND props ? $1 AND kind = ANY($2)
		 ORDER BY kind, id`, undeclaredBody, hidden)
	if err != nil {
		return "", err
	}
	refs, err := scanRefs(rows)
	if err != nil {
		return "", err
	}
	dropped, err := t.dropBodies(refs)
	if err != nil {
		return "", err
	}
	if dropped == 0 {
		return "no record holds a body its kind does not declare", nil
	}
	return fmt.Sprintf("dropped the body of %d records whose kind declares none", dropped), nil
}

// dropHiddenBodies drops the body of every live record of the given kinds
// that declare none: kinds that became usable just now, whose records may
// hold a body migration 11 kept while they were not. An ordinary write by the
// system for each, so the history says what went.
func (t *txn) dropHiddenBodies(kinds []*vocabulary.Kind) (int, error) {
	dropped := 0
	for _, ty := range kinds {
		if ty == nil || ty.Props[undeclaredBody] != nil {
			continue
		}
		rows, err := t.query(`SELECT kind, id FROM records WHERE kind = $1 AND deleted_at IS NULL AND props ? $2 ORDER BY id`,
			ty.Identity, undeclaredBody)
		if err != nil {
			return dropped, err
		}
		refs, err := scanRefs(rows)
		if err != nil {
			return dropped, err
		}
		n, err := t.dropBodies(refs)
		dropped += n
		if err != nil {
			return dropped, err
		}
	}
	return dropped, nil
}

// scanRefs reads (kind, id) rows whole and closes them.
func scanRefs(rows *sql.Rows) ([]eref, error) {
	var refs []eref
	for rows.Next() {
		var ref eref
		if err := rows.Scan(&ref.Kind, &ref.ID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	return refs, rows.Close()
}

// dropBodies removes the body from each record, as a write by the system.
func (t *txn) dropBodies(refs []eref) (int, error) {
	dropped := 0
	for _, ref := range refs {
		row, err := t.loadRow(ref, true)
		if err != nil {
			return dropped, err
		}
		if row == nil {
			continue
		}
		before := row.clone()
		// A copy, so the row loaded before the write still reads as it was
		// and the fold sees the difference.
		row.Props = maps.Clone(row.Props)
		delete(row.Props, undeclaredBody)
		res, err := t.foldRow(before, row, false, false)
		if err != nil {
			return dropped, err
		}
		if !res.changed {
			continue
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, ref.ID, ref.Kind,
			map[string]any{"properties": []string{undeclaredBody}}); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}
