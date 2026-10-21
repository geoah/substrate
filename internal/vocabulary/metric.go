package vocabulary

// The `metric`, `source` and `result` traits' rules that a trait cannot
// state, held on every write of a record whose kind binds one, and the one
// reading of a metric's contract the engine takes (MetricContractOf). Pure
// functions of the record alone, like the `observed` rules.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The `metric` trait's property names.
const (
	MetricName             = "name"
	MetricUnit             = "unit"
	MetricUnits            = "units"
	MetricForm             = "form"
	MetricCodeSet          = "codeSet"
	MetricDisplayPrecision = "displayPrecision"
	MetricCategory         = "category"
	MetricBucketPlace      = "bucketPlace"
	MetricRollups          = "rollups"
)

// SourcePriorities is the `source` trait's one property: the source's rank
// per metric, keyed by the metric's full record path.
const SourcePriorities = "priorities"

// The `result` trait's property names. `value`, `code`, `absentReason` and
// `sourceValue` beside them are `observed(single)`'s.
const (
	ResultCodedValue      = "codedValue"
	ResultRatio           = "ratio"
	ResultText            = "text"
	ResultInterpretation  = "interpretation"
	ResultReferenceRanges = "referenceRanges"
)

// The two places a read by the day or longer puts a value's span
// (`bucketPlace`): by its start, or by its end, which lands a night on the
// day it ends.
const (
	BucketPlaceStart = "start"
	BucketPlaceEnd   = "end"
)

// RollupTier is one rollup tier width a metric's `rollups` may list, by its
// spelling.
type RollupTier struct {
	Name  string
	Width time.Duration
}

// RollupTiers are the tier widths a metric may keep, finest first. Each
// divides every coarser one, so any ascending selection divides in turn.
var RollupTiers = []RollupTier{
	{"5m", 5 * time.Minute},
	{"15m", 15 * time.Minute},
	{"1h", time.Hour},
	{"1d", 24 * time.Hour},
}

// DefaultRollups are the tiers a metric keeps when it names none.
var DefaultRollups = []string{"15m", "1h", "1d"}

// MetricContract is what the engine reads from a metric record: the unit a
// read answers in by default, where a read by the day places a span, and the
// rollup tiers the index keeps, finest first.
type MetricContract struct {
	Unit string
	// UnitValue is the stored `unit` as written, a spelling or a reference
	// to a unit record, which the unit graph reads (MetricUnitOf).
	UnitValue   any
	BucketPlace string
	// Rollups is the tier widths, finest first: the default tiers where the
	// metric names none, and empty where it names an empty list.
	Rollups []time.Duration
}

// MetricContractOf reads a metric record's contract from its stored
// properties. It is lenient: the write path admitted the record under
// CheckMetricIn, and a value it cannot read is left out (a rollup it cannot
// read keeps no tier) rather than failing the read that asked.
func MetricContractOf(props map[string]any) MetricContract {
	c := MetricContract{}
	c.Unit = metricUnitText(props[MetricUnit])
	c.UnitValue = props[MetricUnit]
	c.BucketPlace, _ = props[MetricBucketPlace].(string)
	names := DefaultRollups
	if raw, held := props[MetricRollups]; held && raw != nil {
		names = nil
		for _, v := range listOf(raw) {
			if s, ok := v.(string); ok {
				names = append(names, s)
			}
		}
	}
	for _, name := range names {
		if i := slices.IndexFunc(RollupTiers, func(t RollupTier) bool { return t.Name == name }); i >= 0 {
			c.Rollups = append(c.Rollups, RollupTiers[i].Width)
		}
	}
	slices.Sort(c.Rollups)
	c.Rollups = slices.Compact(c.Rollups)
	if c.Rollups == nil {
		c.Rollups = []time.Duration{}
	}
	return c
}

// metricUnitText is a metric's `unit` as text: a spelling, or the id of the
// unit record a reference names (decision 0186).
func metricUnitText(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case map[string]any:
		path, _ := v[ReferenceValueKey].(string)
		if kind, id, ok := SplitRecordPath(path); ok && kind == KindUnit {
			return id
		}
	}
	return ""
}

// CheckMetricIn holds one record of a kind that binds `metric` to the rules
// the trait cannot state, each problem in the `props.<name>: ...` form a
// validation error carries: the unit and every listed unit resolve through
// the repository's graph (decision 0186), each a unit id or a reference to a
// unit record, and each listed unit converts to the unit through linear
// conversions; the form and the bucket place are words the readers know, and
// the rollup tiers are known widths, finest first, each once.
func CheckMetricIn(g *UnitGraph, props map[string]any) []string {
	problems := checkMetricShape(props)
	unit, held, err := MetricUnitOf(g, props[MetricUnit])
	if err != nil {
		problems = append(problems, fmt.Sprintf("props.%s: %v", MetricUnit, err))
	}
	listed, err := MetricUnitsOf(g, props[MetricUnits])
	if err != nil {
		return append(problems, fmt.Sprintf("props.%s: %v", MetricUnits, err))
	}
	if !held || unit == "" {
		return problems
	}
	for i, id := range listed {
		if _, _, err := g.Linear(id, unit, false); err != nil {
			problems = append(problems, fmt.Sprintf("props.%s[%d]: %s does not convert to the metric's unit %s: %v", MetricUnits, i, id, unit, err))
		}
	}
	return problems
}

// MetricUnitOf reads a metric's `unit` through the graph: a unit spelling or
// a reference to a unit record, answered as the unit's id; held is false
// where the metric names none.
func MetricUnitOf(g *UnitGraph, raw any) (id string, held bool, err error) {
	switch v := raw.(type) {
	case nil:
		return "", false, nil
	case string:
		if v == "" {
			return "", false, nil
		}
		u, ok := g.Resolve(v)
		if !ok {
			return "", true, fmt.Errorf("%q names no unit of this repository", v)
		}
		return u, true, nil
	case map[string]any:
		path, _ := v[ReferenceValueKey].(string)
		kind, id, ok := SplitRecordPath(path)
		if !ok || kind != KindUnit {
			return "", true, fmt.Errorf("%q is no reference to a %s record", path, KindUnit)
		}
		if _, ok := g.Unit(id); !ok {
			return "", true, fmt.Errorf("%s names no unit of this repository", id)
		}
		return id, true, nil
	}
	return "", true, fmt.Errorf("a unit is a unit id or a reference to a %s record", KindUnit)
}

// MetricUnitsOf reads a metric's `units` through the graph, each a unit
// spelling or a reference to a unit record, answered as unit ids.
func MetricUnitsOf(g *UnitGraph, raw any) ([]string, error) {
	var out []string
	for i, v := range listOf(raw) {
		id, held, err := MetricUnitOf(g, v)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		if held {
			out = append(out, id)
		}
	}
	return out, nil
}

// checkMetricShape holds the metric rules that read no unit: the form, the
// bucket place and the rollup tiers.
func checkMetricShape(props map[string]any) []string {
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if form, _ := props[MetricForm].(string); form != "" && !slices.Contains(ObservedForms, form) {
		addf("props.form: %q is not a form: %s", form, orList(ObservedForms))
	}
	if place, _ := props[MetricBucketPlace].(string); place != "" && place != BucketPlaceStart && place != BucketPlaceEnd {
		addf("props.bucketPlace: %q is not a place: %s or %s", place, BucketPlaceStart, BucketPlaceEnd)
	}
	problems = append(problems, rollupProblems(props[MetricRollups])...)
	return problems
}

func rollupProblems(raw any) []string {
	if raw == nil {
		return nil
	}
	names := make([]string, len(RollupTiers))
	for i, t := range RollupTiers {
		names[i] = t.Name
	}
	prev := -1
	for i, v := range listOf(raw) {
		s, _ := v.(string)
		at := slices.IndexFunc(RollupTiers, func(t RollupTier) bool { return t.Name == s })
		switch {
		case at < 0:
			return []string{fmt.Sprintf("props.rollups[%d]: %q is not a rollup tier: %s", i, s, orList(names))}
		case at == prev:
			return []string{fmt.Sprintf("props.rollups[%d]: %s is listed twice", i, s)}
		case at < prev:
			return []string{fmt.Sprintf("props.rollups[%d]: %s comes after the coarser %s; list the tiers finest first, each dividing the next",
				i, s, RollupTiers[prev].Name)}
		}
		prev = at
	}
	return nil
}

// CheckSourcePriorities holds a `source` record's `priorities` to the trait's
// rules: each key is the full record path of a metric (`<kind>/<id>`), the
// kind one that isMetric says binds `metric`, and each rank is 1 or more. A
// bare metric id is refused, because the key names a record of a kind a
// repository rehomes onto its own authority, and only the full path says which.
func CheckSourcePriorities(props map[string]any, isMetric func(kind string) bool) []string {
	m, ok := props[SourcePriorities].(map[string]any)
	if !ok {
		return nil
	}
	var problems []string
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		kind, _, ok := SplitRecordPath(key)
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf(
				"props.priorities.%s: a priority is keyed by the metric's full record path, <kind>/<id>, such as %s",
				key, examplePriorityKey(key)))
			continue
		case isMetric != nil && !isMetric(kind):
			problems = append(problems, fmt.Sprintf(
				"props.priorities.%s: %s does not bind %s, so the record is not a metric", key, kind, TraitMetricCore))
		}
		if n, ok := obsInt(m[key]); ok && n < 1 {
			problems = append(problems, fmt.Sprintf("props.priorities.%s: a rank is 1 or more, 1 first; got %d", key, n))
		}
	}
	return problems
}

func examplePriorityKey(key string) string {
	if key == "" || strings.Contains(key, ".") {
		return "<authority>/observations/metric/health/heart_rate"
	}
	return "<authority>/observations/metric/" + key
}

// SourcePriorityOf reads a source record's rank for one metric's record path,
// false where it ranks none.
func SourcePriorityOf(props map[string]any, metric string) (int64, bool) {
	m, ok := props[SourcePriorities].(map[string]any)
	if !ok {
		return 0, false
	}
	switch v := m[metric].(type) {
	case nil:
		return 0, false
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	default:
		return obsInt(v)
	}
}
