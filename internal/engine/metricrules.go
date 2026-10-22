package engine

// THE METRIC RULES (decision 0186), held at the end of a transaction, against
// the transaction's final state:
//
//   - an observed value the transaction wrote names a live record of a kind
//     binding `metric`, and that metric admits the value's unit;
//   - a metric the transaction created, edited or restored still admits the
//     unit of every stored value pointing at it, tombstones included because
//     a restore brings a value back;
//   - a metric the transaction deleted or purged has no stored value
//     pointing at it, tombstones included.
//
// A metric admits a unit when the unit is its `unit` or one of its listed
// `units`, and, where it lists none, when the unit converts to its `unit`
// through linear conversions. A metric naming no unit admits every unit.
// The fold notes what to check (noteMetricRules); checkMetricValues runs
// beside checkUnitGraph in inTx, so the order of writes inside one
// transaction never matters.

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// metricValueNote is what an observed row the transaction folded points at,
// as the fold left it: its metric's record path and its unit's spelling.
type metricValueNote struct {
	metric string
	unit   string
}

// noteMetricRules records, during a live write's fold, a
// folded observed row (live, or nil to forget it) and a folded metric row.
// A replay and a repository migration note nothing.
func (t *txn) noteMetricRules(ty *vocabulary.Kind, ref eref, row *erow) {
	if t.replaying || t.migrating || ty == nil {
		return
	}
	if bindsMetric(ty) {
		if t.metricRecords == nil {
			t.metricRecords = map[eref]bool{}
		}
		t.metricRecords[ref] = true
	}
	b, observed := observedBinding(ty)
	if !observed {
		return
	}
	if row == nil || row.DeletedAt != nil {
		delete(t.metricValues, ref)
		return
	}
	if t.metricValues == nil {
		t.metricValues = map[eref]metricValueNote{}
	}
	n := metricValueNote{metric: referencePath(row.Props[vocabulary.ObservedMetric])}
	if b.Variant == vocabulary.ObservedPacked {
		n.unit, _ = row.Props[vocabulary.QuantityUnit].(string)
	} else if v, ok := row.Props["value"].(map[string]any); ok {
		n.unit, _ = v[vocabulary.QuantityUnit].(string)
	}
	t.metricValues[ref] = n
}

// referencePath is a stored reference value's record path, or "".
func referencePath(v any) string {
	switch r := v.(type) {
	case map[string]any:
		s, _ := r[vocabulary.ReferenceValueKey].(string)
		return s
	case string:
		return r
	}
	return ""
}

// checkMetricValues holds the transaction's noted observed values and
// metrics to the metric rules. A value is refused as a validation error; a
// metric that would strand stored values is refused as a conflict.
func (t *txn) checkMetricValues() error {
	if len(t.metricValues) == 0 && len(t.metricRecords) == 0 {
		return nil
	}
	at, err := t.unitGraph()
	if err != nil {
		return err
	}
	g := at.graph
	reg := t.declarations()
	metrics := map[eref]*erow{}
	loadMetric := func(ref eref) (*erow, error) {
		if row, held := metrics[ref]; held {
			return row, nil
		}
		row, err := t.loadRow(ref, false)
		if err != nil {
			return nil, err
		}
		if row != nil && row.DeletedAt != nil {
			row = nil
		}
		if ty, ok := reg.ByIdentity(ref.Kind); !ok || !bindsMetric(ty) {
			row = nil
		}
		metrics[ref] = row
		return row, nil
	}
	var invalid []string
	for _, ref := range erefKeys(t.metricValues) {
		n := t.metricValues[ref]
		if n.metric == "" {
			continue
		}
		kind, id, ok := vocabulary.SplitRecordPath(n.metric)
		if !ok {
			continue
		}
		metric, err := loadMetric(eref{Kind: kind, ID: id})
		if err != nil {
			return err
		}
		where := vocabulary.RecordPath(ref.Kind, ref.ID)
		if metric == nil {
			invalid = append(invalid, fmt.Sprintf("%s: props.metric: %s is no live metric record; write the metric before or beside its values", where, n.metric))
			continue
		}
		if n.unit == "" {
			continue
		}
		if err := metricAdmits(g, metric.Props, n.unit); err != nil {
			member := "props.value.unit"
			if b, _ := observedBinding(mustKind(reg, ref.Kind)); b.Variant == vocabulary.ObservedPacked {
				member = "props.unit"
			}
			invalid = append(invalid, fmt.Sprintf("%s: %s: %v", where, member, err))
		}
	}
	if len(invalid) > 0 {
		return &substrate.ValidationError{Problems: invalid}
	}
	var stranded []string
	observedKinds := observedKindsOf(reg)
	for _, ref := range erefKeys(t.metricRecords) {
		where := vocabulary.RecordPath(ref.Kind, ref.ID)
		units, pointed, err := t.unitsPointingAt(ref, observedKinds)
		if err != nil {
			return err
		}
		if !pointed {
			continue
		}
		metric, err := loadMetric(ref)
		if err != nil {
			return err
		}
		if metric == nil {
			stranded = append(stranded, fmt.Sprintf("%s: stored values point at this metric (tombstones included), so it cannot be deleted; delete or purge those values first", where))
			continue
		}
		for _, u := range units {
			if err := metricAdmits(g, metric.Props, u); err != nil {
				stranded = append(stranded, fmt.Sprintf("%s: a stored value in %s points at this metric (tombstones included), and %v", where, u, err))
			}
		}
	}
	if len(stranded) > 0 {
		return fmt.Errorf("%w: %s", substrate.ErrConflict, strings.Join(stranded, "; "))
	}
	return nil
}

func mustKind(reg *vocabulary.Registry, kind string) *vocabulary.Kind {
	ty, _ := reg.ByIdentity(kind)
	return ty
}

// observedKindsOf is every kind of the registry that binds `observed`.
func observedKindsOf(reg *vocabulary.Registry) []string {
	var out []string
	for _, k := range reg.Kinds() {
		if _, ok := observedBinding(k); ok {
			out = append(out, k.Identity)
		}
	}
	return out
}

// unitsPointingAt is the distinct units of the observed rows whose `metric`
// names the metric, live and tombstoned, through the refs index, and whether
// any row points at it at all.
func (t *txn) unitsPointingAt(metric eref, kinds []string) ([]string, bool, error) {
	if len(kinds) == 0 {
		return nil, false, nil
	}
	rows, err := t.query(`
		SELECT DISTINCT CASE WHEN jsonb_typeof(r.props->'value') = 'object'
			THEN r.props->'value'->>'unit' ELSE r.props->>'unit' END
		FROM refs f JOIN records r ON r.kind = f.src_kind AND r.id = f.src
		WHERE f.dst_kind = $1 AND f.dst = $2 AND f.property = $3 AND f.src_kind = ANY($4::text[])`,
		metric.Kind, metric.ID, vocabulary.ObservedMetric, kinds)
	if err != nil {
		return nil, false, fmt.Errorf("substrate/engine: read the values of metric %s: %w", metric.ID, err)
	}
	var units []string
	pointed := false
	for rows.Next() {
		var u *string
		if err := rows.Scan(&u); err != nil {
			_ = rows.Close()
			return nil, false, err
		}
		pointed = true
		if u != nil && *u != "" {
			units = append(units, *u)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, false, err
	}
	return units, pointed, nil
}

// metricAdmits reports why a metric does not admit a unit spelling, or nil.
func metricAdmits(g *vocabulary.UnitGraph, props map[string]any, spelling string) error {
	id, ok := g.Resolve(spelling)
	if !ok {
		return fmt.Errorf("%q names no unit of this repository", spelling)
	}
	unit, held, err := vocabulary.MetricUnitOf(g, props[vocabulary.MetricUnit])
	if err != nil {
		return fmt.Errorf("the metric's unit: %w", err)
	}
	listed, err := vocabulary.MetricUnitsOf(g, props[vocabulary.MetricUnits])
	if err != nil {
		return fmt.Errorf("the metric's units: %w", err)
	}
	if len(listed) > 0 {
		if (held && id == unit) || containsString(listed, id) {
			return nil
		}
		all := listed
		if held {
			all = append([]string{unit}, listed...)
		}
		return fmt.Errorf("%s is not one of the metric's units %s", id, strings.Join(all, ", "))
	}
	if !held || id == unit {
		return nil
	}
	if _, _, err := g.Linear(id, unit, false); err != nil {
		return fmt.Errorf("%s does not convert to the metric's unit %s: %w", id, unit, err)
	}
	return nil
}

// erefKeys is a map's record keys in kind, then id, order.
func erefKeys[V any](m map[eref]V) []eref {
	out := make([]eref, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}
