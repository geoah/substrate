package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// HOW LONG A KIND'S HISTORY IS KEPT, IN ONE REPOSITORY (decision record 0282).
// A kind's declaration states its retention (vocabulary/retention.go), its
// own key or its package header's, and a `core/retention` record overrides it
// for one kind in this repository. retentionOf is the one answer the engine
// gives, and what the compaction asks when it lands; until then nothing drops
// an entry by it.

// kindRetention is the kind of the record a repository overrides one kind's
// retention with.
const kindRetention = "substrate.reamde.dev/core/retention"

const (
	propRetentionAppliesTo = "appliesTo"
	propRetentionValue     = "retention"
)

// retentionOverride is one live `core/retention` record.
type retentionOverride struct {
	id, value string
}

// retentionOverrides reads every live override, by the kind it applies to.
func (ds *dataset) retentionOverrides(ctx context.Context) (map[string]retentionOverride, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, coalesce(props->>$2, ''), coalesce(props->$3->>$4, '')
		  FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`,
		kindRetention, propRetentionValue, propRetentionAppliesTo, vocabulary.ReferenceValueKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]retentionOverride{}
	for rows.Next() {
		var o retentionOverride
		var path string
		if err := rows.Scan(&o.id, &o.value, &path); err != nil {
			return nil, err
		}
		kind, ok := retentionTarget(path)
		if !ok {
			continue
		}
		// The write guard keeps one per kind; the first by id wins should two
		// ever stand, so the answer does not depend on the order rows come in.
		if _, seen := out[kind]; !seen {
			out[kind] = o
		}
	}
	return out, rows.Err()
}

// retentionTarget is the kind an override's `appliesTo` path names.
func retentionTarget(path string) (string, bool) {
	kind, id, ok := vocabulary.SplitRecordPath(path)
	if !ok || kind != kindKind {
		return "", false
	}
	return id, true
}

// retentionOf is how long kind's history is kept in this repository: the
// override's, else what the declaration states, else all. It is the one
// function a caller asks.
func (ds *dataset) retentionOf(ctx context.Context, kind string) (vocabulary.Retention, error) {
	overrides, err := ds.retentionOverrides(ctx)
	if err != nil {
		return vocabulary.Retention{}, err
	}
	return vocabulary.MustRetention(kindRetentionIn(ds.registry(), overrides, kind).Effective), nil
}

// kindRetentionIn is one kind's retention under reg and the overrides: the
// one rule retentionOf and the listing share. An override whose value no
// longer parses (the write guard admits none) applies nothing, and neither
// does one of a kind reg does not declare: it waits for the kind's
// declaration, and a kind removed while its history stays keeps all of it,
// which the listing, holding declared kinds only, could not show.
func kindRetentionIn(reg *vocabulary.Registry, overrides map[string]retentionOverride, kind string) substrate.KindRetention {
	kr := substrate.KindRetention{Kind: kind, Declared: reg.RetentionOf(kind)}
	kr.Effective = vocabulary.MustRetention(kr.Declared).Value
	if _, declared := reg.ByIdentity(kind); !declared {
		return kr
	}
	if o, ok := overrides[kind]; ok {
		kr.Override, kr.OverrideID = o.value, o.id
		if r, err := vocabulary.ParseRetention(o.value); err == nil {
			kr.Effective = r.Value
		}
	}
	return kr
}

// retentionShortenings is each kind both registries declare whose effective
// retention the candidate keeps for less time, compared by Keeps, so a
// respelling and a lengthening are not among them. The overrides apply to
// both sides: a kind the owner overrode keeps what the owner chose either way.
func retentionShortenings(current, candidate *vocabulary.Registry, overrides map[string]retentionOverride) []substrate.RetentionChange {
	var out []substrate.RetentionChange
	for _, k := range candidate.Kinds() {
		if _, ok := current.ByIdentity(k.Identity); !ok {
			continue
		}
		before := kindRetentionIn(current, overrides, k.Identity).Effective
		after := kindRetentionIn(candidate, overrides, k.Identity).Effective
		if vocabulary.MustRetention(after).Keeps() < vocabulary.MustRetention(before).Keeps() {
			out = append(out, substrate.RetentionChange{Kind: k.Identity, From: before, To: after})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// KindRetentions answers every declared kind's retention: what its
// declaration states, the override, and the one that applies.
func (ds *dataset) KindRetentions(ctx context.Context) ([]substrate.KindRetention, error) {
	overrides, err := ds.retentionOverrides(ctx)
	if err != nil {
		return nil, err
	}
	reg := ds.registry()
	kinds := reg.Kinds()
	out := make([]substrate.KindRetention, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, kindRetentionIn(reg, overrides, k.Identity))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

// carryRetentionOverride carries the owner's retention override of a kind
// whose rows a move carried onto the kind they moved to (decision 0282). The
// override names the kind's declaration record, which the reference repoint
// never sees, as a grant does, so the move does it here, as ordinary record
// writes under the move's hand. Where the destination holds an override of
// its own, the one that keeps longer stands on the destination and the moved
// one is deleted: a move never keeps either kind's rows for less time than
// the owner chose, and a lengthening is what an upgrade may do unasked.
func (t *txn) carryRetentionOverride(from, to string) error {
	moved, ok, err := t.retentionOverrideOf(from)
	if err != nil || !ok {
		return err
	}
	standing, has, err := t.retentionOverrideOf(to)
	if err != nil {
		return err
	}
	if !has {
		return t.rewriteRetentionOverride(moved.id, propRetentionAppliesTo,
			map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindKind, to)})
	}
	if vocabulary.MustRetention(moved.value).Keeps() > vocabulary.MustRetention(standing.value).Keeps() {
		if err := t.rewriteRetentionOverride(standing.id, propRetentionValue, moved.value); err != nil {
			return err
		}
	}
	ref := eref{Kind: kindRetention, ID: moved.id}
	row, err := t.loadRow(ref, true)
	if err != nil || row == nil {
		return err
	}
	if _, err := t.tombstone(ref, ""); err != nil {
		return err
	}
	if err := t.appendChange(t.actor, substrate.OpDelete, moved.id, kindRetention, map[string]any{
		"finalizers": row.Finalizers,
	}); err != nil {
		return err
	}
	return t.afterTombstone(ref)
}

// retentionOverrideOf is the live override of one kind, read inside the
// transaction; the first by id stands should two, as retentionOverrides reads.
func (t *txn) retentionOverrideOf(kind string) (retentionOverride, bool, error) {
	var o retentionOverride
	err := t.row(`SELECT id, coalesce(props->>$2, '') FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->$3->>$4 = $5
		ORDER BY id LIMIT 1`,
		kindRetention, propRetentionValue, propRetentionAppliesTo, vocabulary.ReferenceValueKey,
		vocabulary.RecordPath(kindKind, kind)).Scan(&o.id, &o.value)
	if errors.Is(err, sql.ErrNoRows) {
		return o, false, nil
	}
	return o, err == nil, err
}

// rewriteRetentionOverride sets one property of an override row, folded and
// appended as the move's other rewrites are.
func (t *txn) rewriteRetentionOverride(id, name string, value any) error {
	ref := eref{Kind: kindRetention, ID: id}
	row, err := t.loadRow(ref, true)
	if err != nil || row == nil {
		return err
	}
	before := row.clone()
	row.Props[name] = value
	if _, err := t.foldRow(before, row, false, false); err != nil {
		return err
	}
	return t.appendChange(t.actor, substrate.OpPatch, id, kindRetention, map[string]any{
		"properties": []string{name},
	})
}

// guardRetentionWrite holds a `core/retention` record to the declaration's
// grammar and to one live record per kind. It runs inside apply beside the
// other per-kind write guards, after the reference pass made `appliesTo`
// canonical, and reads the write's own properties over the stored ones, so a
// patch naming either one is held with the other.
func (t *txn) guardRetentionWrite(sp *applySpec) error {
	if sp.ty.Identity != kindRetention {
		return nil
	}
	if v, named := sp.props[propRetentionValue]; named && v != nil {
		s, _ := v.(string)
		if _, err := vocabulary.ParseRetention(s); err != nil {
			return &substrate.ValidationError{Problems: []string{"props." + propRetentionValue + ": " + err.Error()}}
		}
	}
	path := referencePathOf(sp.props[propRetentionAppliesTo])
	if _, named := sp.props[propRetentionAppliesTo]; !named && sp.existing != nil {
		path = referencePathOf(sp.existing.Props[propRetentionAppliesTo])
	}
	kind, ok := retentionTarget(path)
	if !ok {
		return nil
	}
	var other string
	err := t.tx.QueryRowContext(t.ctx, `
		SELECT id FROM records
		 WHERE kind = $1 AND deleted_at IS NULL AND id <> $2 AND props->$3->>$4 = $5
		 ORDER BY id LIMIT 1`,
		kindRetention, sp.id, propRetentionAppliesTo, vocabulary.ReferenceValueKey, path).Scan(&other)
	if err == nil {
		return fmt.Errorf("%w: the retention of %s is already set by %s: change that record, or delete it first",
			substrate.ErrConflict, kind, vocabulary.RecordPath(kindRetention, other))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}
