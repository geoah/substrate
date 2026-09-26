package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A MAPPING'S WHERE (#581, record 0106). `where:` narrows which records of the
// source kind a mapping covers, in the filter grammar's condition objects. A
// record outside it is treated the way a tombstoned source is: its own write
// resolves and mints no subject, recompute reads nothing from it, and it
// counts as no source for the orphan mark. Its subject pointer is left as it
// stands, because only merge and split move a subject slot; a record that
// comes back inside the where projects onto the same subject again.

// covers reports whether a mapping covers one source row. The conditions are
// compiled by the records route's own filter compiler (query.go condProp) and
// asked of the row as a one-row relation, so a where means exactly what the
// same `filter.properties` would mean on a list. The row may be one this
// transaction has not stored yet: the source's own write asks before the fold.
func (t *txn) covers(m *vocabulary.Mapping, srcTy *vocabulary.Kind, row *erow) (bool, error) {
	if len(m.Where) == 0 {
		return true, nil
	}
	props, err := json.Marshal(nonNilProps(row.Props))
	if err != nil {
		return false, err
	}
	states := row.States
	if states == nil {
		states = map[string]string{}
	}
	rawStates, err := json.Marshal(states)
	if err != nil {
		return false, err
	}
	b := &builder{}
	relation := `SELECT ` + b.arg(string(props)) + `::jsonb AS props, ` +
		b.arg(string(rawStates)) + `::jsonb AS states, ` +
		b.arg(row.Title) + `::text AS title, ` +
		b.arg(row.Body) + `::text AS body, ` +
		b.arg(row.At) + `::timestamptz AS at, ` +
		b.arg(row.EndsAt) + `::timestamptz AS ends_at, ` +
		b.arg(row.DueAt) + `::timestamptz AS due_at`
	for _, name := range m.WhereOrder {
		if err := t.ds.condProp(t.ctx, t.tx, b, []*vocabulary.Kind{srcTy}, name, m.Where[name]); err != nil {
			// Every refusal here is the declaration's, whichever helper
			// raised it, so every one answers as a validation error.
			if !errors.Is(err, substrate.ErrValidation) {
				err = fmt.Errorf("%w: %w", substrate.ErrValidation, err)
			}
			return false, fmt.Errorf("%s %s: data.where.%s: %w", vocabulary.DocRecordMapping, m.Identity(), name, err)
		}
	}
	var ok bool
	err = t.row(`SELECT EXISTS (SELECT 1 FROM (`+relation+`) AS r WHERE `+strings.Join(b.where, " AND ")+`)`,
		b.args...).Scan(&ok)
	return ok, err
}

func nonNilProps(p map[string]any) map[string]any {
	if p == nil {
		return map[string]any{}
	}
	return p
}

// checkMappingWhere compiles every where the candidate registry adds or
// changes, so a condition the filter grammar refuses (an ordering on a
// reference, `match` on a number, a value that is not the property's type)
// fails the apply that declares it and not every later write of its source
// kind.
func (t *txn) checkMappingWhere(live, cand *vocabulary.Registry) error {
	before := map[string]*vocabulary.Mapping{}
	for _, m := range live.Mappings() {
		before[m.Identity()] = m
	}
	for _, m := range cand.Mappings() {
		if len(m.Where) == 0 {
			continue
		}
		if prev, ok := before[m.Identity()]; ok && reflect.DeepEqual(prev.Where, m.Where) {
			continue
		}
		fromTy, ok := cand.ByIdentity(m.From)
		if !ok {
			continue // the loader already refused the mapping naming it
		}
		if _, err := t.covers(m, fromTy, &erow{Kind: m.From}); err != nil {
			return err
		}
	}
	return nil
}

// coveredSites drops the sites whose source record a mapping's where no
// longer covers. A mapping with no where keeps every site without reading a
// row.
func (t *txn) coveredSites(sites []sourceSite, bySlot map[sourceSlot]*vocabulary.Mapping) ([]sourceSite, error) {
	out := sites[:0]
	for _, c := range sites {
		m := bySlot[sourceSlot{c.typ, c.rel}]
		if m == nil || len(m.Where) == 0 {
			out = append(out, c)
			continue
		}
		row, err := t.loadRow(eref{Kind: c.typ, ID: c.id}, false)
		if err != nil {
			return nil, err
		}
		if row == nil {
			continue
		}
		srcTy, err := t.resolveType(c.typ)
		if err != nil {
			return nil, err
		}
		ok, err := t.covers(m, srcTy, row)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// uncoveredSource is the subject hop's refusal for a source record outside
// its mapping's where that has no subject stored: the hop never mints for a
// record the mapping does not cover.
func uncoveredSource(src *erow, m *vocabulary.Mapping) error {
	return fmt.Errorf("%w: reference names %s, which mapping %s does not cover (its where), so it describes no %s",
		substrate.ErrValidation, vocabulary.RecordPath(src.Kind, src.ID), m.Identity(), m.To)
}
