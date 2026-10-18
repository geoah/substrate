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
	MetricMeasures         = "measures"
	MetricUnit             = "unit"
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

// MetricContract is what the engine reads from a metric record: the measure a
// read converts into, the unit it answers in by default, where a read by the
// day places a span, and the rollup tiers the index keeps, finest first.
type MetricContract struct {
	Measures    string
	Unit        string
	BucketPlace string
	// Rollups is the tier widths, finest first: the default tiers where the
	// metric names none, and empty where it names an empty list.
	Rollups []time.Duration
}

// MetricContractOf reads a metric record's contract from its stored
// properties. It is lenient: the write path admitted the record under
// CheckMetric, and a value it cannot read is left out (a rollup it cannot
// read keeps no tier) rather than failing the read that asked.
func MetricContractOf(props map[string]any) MetricContract {
	c := MetricContract{}
	c.Measures, _ = props[MetricMeasures].(string)
	c.Unit, _ = props[MetricUnit].(string)
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

// CheckMetric holds one record of a kind that binds `metric` to the rules the
// trait cannot state, each problem in the `props.<name>: ...` form a
// validation error carries: the measure is one the unit table names, the unit
// one of its units (a unit alone names its own measure), the form and the
// bucket place are words the readers know, and the rollup tiers are known
// widths, finest first, each once.
func CheckMetric(props map[string]any) []string {
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	measures, _ := props[MetricMeasures].(string)
	unit, _ := props[MetricUnit].(string)
	if measures != "" {
		if _, ok := LookupMeasure(measures); !ok {
			addf("props.measures: %q is not a measure of the unit table (docs/units.md)", measures)
			measures = ""
		}
	}
	if unit != "" {
		code, err := CanonicalUnit(unit)
		if err == nil {
			_, err = ResolveMeasure(code, measures)
		}
		switch {
		case err == nil:
		case measures != "":
			addf("props.unit: %s is not a unit of %s: %v", unit, measures, err)
		default:
			addf("props.unit: %v", err)
		}
	}
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
