package vocabulary

// The `observed` trait's rules that a trait cannot state (decisions 0154 to
// 0158), held on every write of a record whose kind binds the trait, and the
// managed `stats` of a packed record. Both are pure functions of the record
// alone, so the write path, a test and the observed index (which expands a
// trace's points the same way) read one definition.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The four forms (decision 0156): what an observed number means, which
// decides how it may be aggregated.
const (
	FormGauge   = "gauge"
	FormAmount  = "amount"
	FormCounter = "counter"
	FormSummary = "summary"
)

// ObservedForms lists the forms in the order the docs give them.
var ObservedForms = []string{FormGauge, FormAmount, FormCounter, FormSummary}

// The three counter contracts (decision 0157).
const (
	ResetMonotonic = "monotonic"
	ResetSigned    = "signed"
	ResetWrap      = "wrap"
)

// ObservedResetRules lists the counter contracts.
var ObservedResetRules = []string{ResetMonotonic, ResetSigned, ResetWrap}

// The limits a packed record is held to. A trace above either is refused,
// naming the limit: the point count keeps one record a bounded unit of index
// work, and the byte size leaves room for a function's response frame and a
// later REST re-apply.
const (
	MaxTracePoints = 20000
	MaxTraceBytes  = 1 << 20
	// MaxUTCOffsetMinutes bounds a recorded UTC offset: 18 hours either way,
	// which every civil zone in use sits inside.
	MaxUTCOffsetMinutes = 1080
)

// The property names the rules read. The trait's own names are its contract;
// the others are declared beside it by the shipped kinds, and a rule that
// reads one applies only where the kind declares it.
const (
	obsMetric         = "metric"
	obsForm           = "form"
	obsValue          = "value"
	obsCode           = "code"
	obsStatistic      = "statistic"
	obsPeriod         = "period"
	obsHold           = "hold"
	obsResetRule      = "resetRule"
	obsResetAt        = "resetAt"
	obsDelta          = "delta"
	obsCircular       = "circular"
	obsWrapAt         = "wrapAt"
	obsUTCOffset      = "utcOffset"
	obsUTCOffsetStart = "utcOffsetStart"
	obsUTCOffsetEnd   = "utcOffsetEnd"
	obsUnit           = "unit"
	obsEvery          = "every"
	obsOffsets        = "offsets"
	obsValues         = "values"
	obsCodes          = "codes"
	// ObservedStats is the packed variant's managed property: the engine
	// computes it from the values and drops whatever a writer sends.
	ObservedStats = "stats"
	// ObservedRevision rises with each rewrite of an observed record and is
	// never lowered (decision 0162).
	ObservedRevision = "revision"

	// The kind's own explanations for a single record that holds neither a
	// value nor a code: the reading as received, and why there is none.
	obsSourceValue  = "sourceValue"
	obsAbsentReason = "absentReason"
)

// ObservedResultShapes are the result shapes a single kind may declare beside
// the trait's `value` and `code`, a lab result's FHIR value types: a coded
// result in full, a ratio, free text. A kind declaring any of them holds
// exactly one shape per record (CheckObserved).
var ObservedResultShapes = []string{"codedValue", "ratio", "text"}

// ObservedNullable reports whether a list property of the kind admits a null
// item: `values` and `codes` on a packed record, where a null means the value
// is unavailable from that point on (decision 0158). Every other list refuses
// one, as it always has.
func ObservedNullable(t *Kind, name string) bool {
	if name != obsValues && name != obsCodes {
		return false
	}
	b, ok := t.Binding(TraitObservedCore)
	return ok && b.Variant == ObservedPacked
}

// ObservedListed reports whether an `observed` contract property is a list:
// `offsets`, `values` and `codes`, a trace's points. Every other one holds a
// single value.
func ObservedListed(name string) bool {
	return name == obsOffsets || name == obsValues || name == obsCodes
}

// observedShapeProblems holds a binding kind's declaration of one `observed`
// contract property to the shape the rules and the index read: a trace's
// three point lists repeated, everything else a single value. The trait's
// property list carries datatypes alone, so the loader holds the shape here.
// A list where one value is read would be admitted on every write and index
// nothing (a reference path is read from one value), and a single value where
// a list is read refuses every write.
func observedShapeProblems(where, name, trait string, p *Property) []string {
	switch {
	case p.Keyed:
		return []string{fmt.Sprintf("%s: data.properties.%s: trait %s requires %s unkeyed, not a keyed map",
			where, name, trait, name)}
	case ObservedListed(name) && !p.Repeated:
		return []string{fmt.Sprintf("%s: data.properties.%s: trait %s requires %s to be a list: declare repeated: true",
			where, name, trait, name)}
	case !ObservedListed(name) && p.Repeated:
		return []string{fmt.Sprintf("%s: data.properties.%s: trait %s requires %s to hold one value, not a list: drop repeated",
			where, name, trait, name)}
	}
	return nil
}

// ObservedRecord is one record of a kind that binds `observed`, as the write
// path holds it after the merge: the variant its kind binds, the two time
// columns, and the stored property values.
type ObservedRecord struct {
	Variant string
	At      *time.Time
	EndsAt  *time.Time
	Props   map[string]any
	// Declares answers whether the kind declares a property, for the rules
	// that read a property the trait does not contract.
	Declares func(name string) bool
}

// CheckObserved holds one record to the trait's rules and answers every
// problem it finds, each naming the property and the rule, in the
// `props.<name>: ...` form a validation error carries. It reads stored
// values: a unit is already its table code and a decimal its canonical
// digits.
func CheckObserved(r ObservedRecord) []string {
	c := &obsCheck{r: r}
	form := c.form()
	switch r.Variant {
	case ObservedSingle:
		c.single(form)
	case ObservedPacked:
		c.packed(form)
	default:
		c.addf("props: %q is not a variant of %s", r.Variant, TraitObservedCore)
	}
	c.readingRules(form)
	return c.problems
}

type obsCheck struct {
	r        ObservedRecord
	problems []string
}

func (c *obsCheck) addf(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

// has reports whether a property holds a value, the way `required` reads one:
// absent, null, an empty string and an empty list hold nothing.
func (c *obsCheck) has(name string) bool {
	switch v := c.r.Props[name].(type) {
	case nil:
		return false
	case string:
		return v != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}

func (c *obsCheck) str(name string) string {
	s, _ := c.r.Props[name].(string)
	return s
}

func (c *obsCheck) declares(name string) bool {
	return c.r.Declares != nil && c.r.Declares(name)
}

func (c *obsCheck) form() string {
	form := c.str(obsForm)
	switch {
	case form == "":
		c.addf("props.form: an observed value needs a form: %s", orList(ObservedForms))
	case !slices.Contains(ObservedForms, form):
		c.addf("props.form: %q is not a form: %s", form, orList(ObservedForms))
		form = ""
	}
	if !c.has(obsMetric) {
		c.addf("props.metric: an observed value names the metric it is a value of")
	}
	if c.r.At == nil {
		c.addf("props.at: an observed value needs at, when it was observed")
	}
	if c.r.At != nil && c.r.EndsAt != nil && c.r.EndsAt.Before(*c.r.At) {
		c.addf("props.endsAt: %s is before at %s", fmtInstant(*c.r.EndsAt), fmtInstant(*c.r.At))
	}
	return form
}

// single holds a record with one value.
func (c *obsCheck) single(form string) {
	hasValue, hasCode := c.has(obsValue), c.has(obsCode)
	explained := c.has(obsSourceValue) || c.has(obsAbsentReason)
	var shapes []string
	for _, s := range ObservedResultShapes {
		if c.declares(s) {
			shapes = append(shapes, s)
		}
	}
	switch {
	case hasValue && hasCode && form != FormAmount:
		c.addf("props.value: an observed value is a number or a code, never both, except an amount with its category: drop value or code")
	case len(shapes) > 0:
		c.resultShape(shapes, hasValue, hasCode)
	case !hasValue && !hasCode && !explained:
		c.addf("props.value: an observed value needs value or code; without either, sourceValue keeps the reading as received or absentReason says why there is none")
	case (hasValue || hasCode) && c.has(obsAbsentReason):
		c.addf("props.absentReason: says why there is no value, and this record holds one: drop absentReason")
	}
	if hasCode && form == FormCounter {
		c.addf("props.code: a counter is a running total, a number, never a code")
	}
	if q, ok := c.r.Props[obsValue].(map[string]any); ok {
		if digits, _ := q[QuantityValue].(string); pastFloat(digits) {
			c.addf("props.value: %s", pastFloatWhy)
		}
	}
	if code, isString := c.r.Props[obsCode].(string); isString && code == "" {
		c.addf("props.code: a code is not empty: drop code")
	}
	if form == FormAmount || form == FormSummary {
		if c.r.At != nil && (c.r.EndsAt == nil || !c.r.EndsAt.After(*c.r.At)) {
			c.addf("props.endsAt: %s covers [at, endsAt), so it needs endsAt after at", article(form))
		}
	}
	switch {
	case form == FormSummary:
		if !c.has(obsStatistic) {
			c.addf("props.statistic: a summary names the statistic it is (sum, avg, min, max, last, vendor)")
		}
		if !c.has(obsPeriod) {
			c.addf("props.period: a summary names the period it covers (hour, day, night, cycle, session)")
		}
	case form != "":
		if c.has(obsStatistic) {
			c.addf("props.statistic: only a summary states a statistic; %s does not", article(form))
		}
		if c.has(obsPeriod) {
			c.addf("props.period: only a summary states a period; %s does not", article(form))
		}
	}
	c.offset(obsUTCOffset)
}

// resultShape holds a kind that declares result shapes beside the trait's
// value: exactly one of value, the coded result (`code`, `codedValue` or
// both), each declared shape and absentReason; or none of them where
// sourceValue keeps a reading the substrate could not normalize.
func (c *obsCheck) resultShape(shapes []string, hasValue, hasCode bool) {
	var held []string
	if hasValue {
		held = append(held, obsValue)
	}
	coded := hasCode
	for _, s := range shapes {
		switch {
		case !c.has(s):
		case s == "codedValue":
			coded = true
		default:
			held = append(held, s)
		}
	}
	if coded {
		held = append(held, "codedValue")
	}
	if c.has(obsAbsentReason) {
		held = append(held, obsAbsentReason)
	}
	names := append([]string{obsValue}, shapes...)
	names = append(names, obsAbsentReason)
	switch {
	case len(held) == 1:
	case len(held) == 0 && c.has(obsSourceValue):
	case len(held) == 0:
		c.addf("props.value: a result holds exactly one of %s (or sourceValue alone, as received); this record holds none",
			orList(names))
	default:
		c.addf("props.value: a result holds exactly one of %s; this record holds %s",
			orList(names), strings.Join(held, " and "))
	}
}

// packed holds a record with many values over [at, endsAt).
func (c *obsCheck) packed(form string) {
	if form == FormSummary {
		c.addf("props.form: a summary is one value per record: write it as a single observation, not a trace")
	}
	var period time.Duration
	switch {
	case c.r.At == nil:
	case c.r.EndsAt == nil || !c.r.EndsAt.After(*c.r.At):
		c.addf("props.endsAt: a trace covers the period [at, endsAt), so it needs endsAt after at")
	default:
		period = c.r.EndsAt.Sub(*c.r.At)
	}
	hasValues, hasCodes := c.r.Props[obsValues] != nil, c.r.Props[obsCodes] != nil
	var n int
	switch {
	case hasValues && hasCodes && form != FormAmount:
		c.addf("props.values: a trace holds values or codes, never both, except an amount trace with each value's category: drop one")
		return
	case hasValues:
		n = len(listOf(c.r.Props[obsValues]))
		if !c.has(obsUnit) {
			c.addf("props.unit: a trace of values names the unit every value is in")
		}
		if hasCodes && len(listOf(c.r.Props[obsCodes])) != n {
			c.addf("props.codes: %d codes for %d values; an amount trace with categories carries one code per value", len(listOf(c.r.Props[obsCodes])), n)
		}
	case hasCodes:
		n = len(listOf(c.r.Props[obsCodes]))
		if c.has(obsUnit) {
			c.addf("props.unit: a trace of codes carries no unit: drop unit")
		}
		if form == FormCounter {
			c.addf("props.codes: a counter is a running total, a number, never a code")
		}
	default:
		c.addf("props.values: a trace holds values or codes, one of the two")
		return
	}
	list := obsValues
	if hasCodes {
		list = obsCodes
		// An empty code would be a third spelling of "no value" beside null
		// and an absent list, read one way by one table and another by the
		// next; null is the one that says a point is unavailable.
		for i, code := range listOf(c.r.Props[obsCodes]) {
			if s, isString := code.(string); isString && s == "" {
				c.addf("props.codes[%d]: a code is not empty; null marks a point with nothing from it on", i)
			}
		}
	}
	switch {
	case n == 0:
		c.addf("props.%s: a trace holds at least one point", list)
	case n > MaxTracePoints:
		c.addf("props.%s: %d points; a trace holds at most %d", list, n, MaxTracePoints)
	}
	if unit := c.str(obsUnit); unit != "" {
		code, err := CanonicalUnit(unit)
		if err == nil {
			_, err = MeasureOf(code)
		}
		if err != nil {
			c.addf("props.unit: %v", err)
		}
	}
	if hasValues {
		for i, v := range listOf(c.r.Props[obsValues]) {
			if digits, _ := v.(string); pastFloat(digits) {
				c.addf("props.values[%d]: %s", i, pastFloatWhy)
				break
			}
		}
	}
	hasEvery, hasOffsets := c.r.Props[obsEvery] != nil, c.r.Props[obsOffsets] != nil
	switch {
	case hasEvery && hasOffsets:
		c.addf("props.every: a trace spaces its points by every or places them by offsets, never both: drop one")
	case hasEvery:
		every, err := ParseISODuration(c.str(obsEvery))
		switch {
		case err != nil:
			c.addf("props.every: %v", err)
		case every <= 0:
			c.addf("props.every: the spacing between points is longer than zero")
		case period > 0 && n > 0 && time.Duration(n) > period/every:
			c.addf("props.every: %d points every %s do not fit the period %s",
				n, FormatISODuration(every), FormatISODuration(period))
		}
	case hasOffsets:
		c.offsets(n, list, period)
	default:
		c.addf("props.offsets: a trace places its points by offsets (milliseconds from at) or spaces them by every, one of the two")
	}
	c.offset(obsUTCOffsetStart)
	c.offset(obsUTCOffsetEnd)
	if raw, err := json.Marshal(c.r.Props); err == nil && len(raw) > MaxTraceBytes {
		c.addf("props: the trace's properties are %d bytes; a trace holds at most %d (1 MiB): split the period", len(raw), MaxTraceBytes)
	}
}

// pastFloatWhy is the refusal of a value pastFloat finds.
const pastFloatWhy = "the magnitude is past 1.8e308, the largest number the observed index reduces"

// pastFloat reports whether a decimal's magnitude is past the largest float64,
// which the observed index reduces in: such a value would derive no point,
// and a trace's stats no mean. A magnitude below the smallest one reads as 0.
func pastFloat(digits string) bool {
	_, err := strconv.ParseFloat(digits, 64)
	return errors.Is(err, strconv.ErrRange)
}

// offsets holds the irregular placement: integer milliseconds from at, one per
// value, never decreasing, inside [0, period).
func (c *obsCheck) offsets(n int, list string, period time.Duration) {
	offsets := listOf(c.r.Props[obsOffsets])
	if len(offsets) != n {
		c.addf("props.offsets: %d offsets for %d %s; a trace pairs each point with one offset", len(offsets), n, list)
		return
	}
	periodMs := period.Milliseconds()
	prev := int64(-1)
	for i, raw := range offsets {
		ms, ok := obsInt(raw)
		switch {
		case !ok:
			c.addf("props.offsets[%d]: an offset is an integer count of milliseconds from at", i)
			return
		case ms < 0 || (period > 0 && ms >= periodMs):
			c.addf("props.offsets[%d]: %d ms is outside the period [0, %d)", i, ms, periodMs)
			return
		case ms < prev:
			c.addf("props.offsets[%d]: %d ms comes before the offset ahead of it (%d ms); offsets never decrease", i, ms, prev)
			return
		}
		prev = ms
	}
}

// readingRules are the rules about how a value is read, the same on both
// variants: which form holds, resets, wraps, and which measure a delta or a
// circular value belongs to.
func (c *obsCheck) readingRules(form string) {
	if c.has(obsHold) {
		if form != "" && form != FormGauge {
			c.addf("props.hold: only a gauge holds its value; %s does not", article(form))
		} else if d, err := ParseISODuration(c.str(obsHold)); err == nil && d < 0 {
			c.addf("props.hold: a hold is never negative")
		}
	}
	rule := c.str(obsResetRule)
	switch {
	case rule == "":
	case !slices.Contains(ObservedResetRules, rule):
		c.addf("props.resetRule: %q is not a counter contract: %s", rule, orList(ObservedResetRules))
	case form != "" && form != FormCounter:
		c.addf("props.resetRule: only a counter resets; %s does not", article(form))
	}
	if c.has(obsResetAt) && rule == "" {
		c.addf("props.resetAt: names when a counter's accumulation began, so it needs resetRule")
	}
	hasWrap := c.has(obsWrapAt)
	switch {
	case hasWrap && rule != ResetWrap:
		c.addf("props.wrapAt: is the value a wrapping counter returns to zero at, so it needs resetRule wrap")
	case !hasWrap && rule == ResetWrap:
		c.addf("props.wrapAt: a wrapping counter names the value it wraps at")
	case hasWrap:
		if w, ok := new(big.Rat).SetString(c.str(obsWrapAt)); !ok || w.Sign() <= 0 {
			c.addf("props.wrapAt: the wrap point is above zero")
		}
	}
	unit := c.str(obsUnit)
	if c.r.Variant == ObservedSingle {
		if v, ok := c.r.Props[obsValue].(map[string]any); ok {
			unit, _ = v[QuantityUnit].(string)
		} else {
			unit = ""
		}
	}
	c.measureFlag(obsDelta, unit, "temperature", "marks a temperature difference")
	c.measureFlag(obsCircular, unit, "angle", "averages an angle on the circle")
}

// measureFlag holds a true bool flag to the measure the value's unit belongs
// to.
func (c *obsCheck) measureFlag(name, unit, measure, does string) {
	if b, _ := c.r.Props[name].(bool); !b {
		return
	}
	if unit == "" {
		c.addf("props.%s: %s, and this record has no unit", name, does)
		return
	}
	m, err := MeasureOf(unit)
	if err != nil {
		return
	}
	if m.Name != measure {
		c.addf("props.%s: %s, and %s measures %s", name, does, unit, m.Name)
	}
}

// offset holds a recorded UTC offset in minutes.
func (c *obsCheck) offset(name string) {
	raw, held := c.r.Props[name]
	if !held || raw == nil {
		return
	}
	n, ok := obsInt(raw)
	if !ok || n < -MaxUTCOffsetMinutes || n > MaxUTCOffsetMinutes {
		c.addf("props.%s: minutes east of UTC, between -%d and %d", name, MaxUTCOffsetMinutes, MaxUTCOffsetMinutes)
	}
}

// RevisionProblems holds a rewrite of a live record whose kind binds
// `observed` or `revised` to its stored revision (decision 0162): the written
// revision is at least the stored one, and a stored one is never removed. An
// equal one is admitted, which is how an adapter fix rewrites output its
// upstream did not change.
func RevisionProblems(stored, written map[string]any) []string {
	was, had := ObservedRevisionOf(stored)
	if !had {
		return nil
	}
	now, has := ObservedRevisionOf(written)
	switch {
	case !has:
		return []string{fmt.Sprintf(
			"props.revision: the stored revision %d is never removed; a rewrite carries one at least as high", was)}
	case now < was:
		return []string{fmt.Sprintf(
			"props.revision: %d is below the stored revision %d; a rewrite never lowers it, so this is a stale delivery", now, was)}
	}
	return nil
}

// ObservedRevisionOf reads a stored revision, false when there is none.
func ObservedRevisionOf(props map[string]any) (int64, bool) {
	raw, held := props[ObservedRevision]
	if !held || raw == nil {
		return 0, false
	}
	return obsInt(raw)
}

// ObservedOffsetOf reads a stored UTC offset in minutes (utcOffset,
// utcOffsetStart or utcOffsetEnd), false when there is none. It reads the
// value the way the offset check does, so the index and the write rules
// agree on what an offset is.
func ObservedOffsetOf(props map[string]any, name string) (int64, bool) {
	raw, held := props[name]
	if !held || raw == nil {
		return 0, false
	}
	return obsInt(raw)
}

// TracePoint is one point of a packed record.
type TracePoint struct {
	// At is the point's instant: the record's at plus its offset.
	At time.Time
	// Value is the point's digits, or "" for a code or an unavailable point.
	Value string
	// Code is the point's code, or "" for a value or an unavailable point.
	Code string
	// Unavailable is a null in the list, or a stored empty code: no value
	// from this point on, until the next point (decision 0158,
	// ObservedPointAvailable).
	Unavailable bool
}

// ObservedPointAvailable is the one definition of an available point, which
// every reader of the observed index holds to (the points the fold inserts,
// the count a deletion takes back, a trace's stats, the rollups, the latest
// row and a rebuild): a point is available when it holds a value, in a
// numeric stream, or a code that is not empty, in a coded stream. A null in a
// trace holds neither, and so does an empty code a row stored before the
// write refused it.
func ObservedPointAvailable(hasValue bool, code string) bool {
	return hasValue || code != ""
}

// TracePoints expands a packed record's stored properties into its points, in
// list order: every value or code at `at` plus its offset, or plus its index
// times `every`. It reads a record CheckObserved admitted and refuses one it
// cannot place.
func TracePoints(at time.Time, props map[string]any) ([]TracePoint, error) {
	coded := props[obsCodes] != nil && props[obsValues] == nil
	items := listOf(props[obsValues])
	codes := listOf(props[obsCodes])
	if coded {
		items = codes
	}
	offsets := listOf(props[obsOffsets])
	var every time.Duration
	if s, _ := props[obsEvery].(string); s != "" {
		d, err := ParseISODuration(s)
		if err != nil {
			return nil, fmt.Errorf("every: %w", err)
		}
		every = d
	} else if len(offsets) != len(items) {
		return nil, fmt.Errorf("%d offsets for %d points", len(offsets), len(items))
	}
	out := make([]TracePoint, len(items))
	for i, item := range items {
		p := TracePoint{At: at.Add(time.Duration(i) * every)}
		if every == 0 {
			ms, ok := obsInt(offsets[i])
			if !ok {
				return nil, fmt.Errorf("offsets[%d] is not an integer", i)
			}
			p.At = at.Add(time.Duration(ms) * time.Millisecond)
		}
		switch s, isString := item.(string); {
		case item == nil:
			p.Unavailable = true
		case !isString:
			return nil, fmt.Errorf("point %d is not a string", i)
		case coded:
			p.Code = s
			p.Unavailable = !ObservedPointAvailable(false, s)
		default:
			p.Value = s
			// An amount trace may carry each value's category beside it.
			if i < len(codes) {
				if cs, ok := codes[i].(string); ok {
					p.Code = cs
				}
			}
		}
		out[i] = p
	}
	return out, nil
}

// TraceStats computes a packed record's managed `stats` from its points, in
// one deterministic routine: the same properties always produce the same
// value, so a rewrite that changes no point changes no stats. Unavailable
// points count in nothing. A trace of values carries `count`, `min` and `max`
// (exact digits as stored), `mean` and `m2` (the sum of squared deviations
// from the mean, for a stable variance; both the nearest float64 to the exact
// rational), `first` and `last` (`{at, value}`) and `coverage` (the ISO
// duration from the first point to the last). A trace of codes carries
// `count`, `first`, `last` and `coverage`, with the code as each `value`.
func TraceStats(at time.Time, props map[string]any) (map[string]any, error) {
	points, err := TracePoints(at, props)
	if err != nil {
		return nil, err
	}
	coded := props[obsCodes] != nil && props[obsValues] == nil
	var (
		count       int64
		first, last *TracePoint
		minV, maxV  string
		minR, maxR  *big.Rat
		sum, sumSq  = new(big.Rat), new(big.Rat)
	)
	for i := range points {
		p := &points[i]
		if p.Unavailable {
			continue
		}
		count++
		if first == nil {
			first = p
		}
		last = p
		if coded {
			continue
		}
		r, ok := new(big.Rat).SetString(p.Value)
		if !ok {
			return nil, fmt.Errorf("point %d: %q is not a decimal", i, p.Value)
		}
		if minR == nil || r.Cmp(minR) < 0 {
			minR, minV = r, p.Value
		}
		if maxR == nil || r.Cmp(maxR) > 0 {
			maxR, maxV = r, p.Value
		}
		sum.Add(sum, r)
		sumSq.Add(sumSq, new(big.Rat).Mul(r, r))
	}
	out := map[string]any{"count": count}
	if count == 0 {
		return out, nil
	}
	point := func(p *TracePoint) map[string]any {
		v := p.Value
		if coded {
			v = p.Code
		}
		return map[string]any{"at": fmtInstant(p.At), "value": v}
	}
	out["first"] = point(first)
	out["last"] = point(last)
	out["coverage"] = FormatISODuration(last.At.Sub(first.At))
	if coded {
		return out, nil
	}
	n := new(big.Rat).SetInt64(count)
	mean := new(big.Rat).Quo(sum, n)
	// m2 = sum(x^2) - n * mean^2, exact before the one rounding to float64.
	m2 := new(big.Rat).Sub(sumSq, new(big.Rat).Mul(n, new(big.Rat).Mul(mean, mean)))
	meanF, _ := mean.Float64()
	m2F, _ := m2.Float64()
	if math.IsInf(meanF, 0) || math.IsInf(m2F, 0) {
		return nil, fmt.Errorf("the values' mean or spread is past 1.8e308, the largest number the observed index reduces; values more than about 1e154 apart do not fit one trace")
	}
	out["min"] = minV
	out["max"] = maxV
	out["mean"] = meanF
	out["m2"] = m2F
	return out, nil
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

// obsInt reads an integer the way a stored value arrives: int64 from a write's
// coercion, float64 from a jsonb read-back, json.Number from a changelog
// decode. A changelog segment's canonical line spells every number with an
// exponent (118000 is `1.18E5`), so a replayed number is read exactly as a
// rational and admitted when it is whole.
func obsInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
			return i, true
		}
		r, ok := new(big.Rat).SetString(string(n))
		if !ok || !r.IsInt() || !r.Num().IsInt64() {
			return 0, false
		}
		return r.Num().Int64(), true
	}
	return 0, false
}

func fmtInstant(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// article is a form with its article, for a refusal's sentence.
func article(form string) string {
	if form == FormAmount {
		return "an amount"
	}
	return "a " + form
}
