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
	"maps"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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
	// MaxObservedMagnitude bounds a value's magnitude. The observed index
	// reduces in float64 and weighs a gauge's value by the milliseconds it
	// holds: a value at the bound held from year 1 to year 9999 (3.2e14 ms)
	// weighs 3.2e294, so a time-weighted sum stays finite across 1e13
	// streams, where a value near the largest float64 overflows in a minute.
	MaxObservedMagnitude = 1e280
	// MaxObservedCode bounds a code, a free-text state included, in
	// characters. The observed index keys its rollup rows on the code, and
	// Postgres refuses a key past one index page with an internal error, so
	// the write path refuses a longer code first. 255 is Home Assistant's
	// bound on an entity's state.
	MaxObservedCode = 255
)

// The property names the rules read, every one of them the trait's: the
// rules read no property a trait does not contract.
const (
	obsMetric    = "metric"
	obsForm      = "form"
	obsValue     = "value"
	obsCode      = "code"
	obsStatistic = "statistic"
	obsPeriod    = "period"
	obsHold      = "hold"
	obsResetRule = "resetRule"
	obsResetAt   = "resetAt"
	obsDelta     = "delta"
	obsCircular  = "circular"
	obsWrapAt    = "wrapAt"
	obsUTCOffset = "utcOffset"
	obsUnit      = "unit"
	obsEvery     = "every"
	obsOffsets   = "offsets"
	obsValues    = "values"
	obsCodes     = "codes"
	// ObservedStats is the packed variant's managed property: the engine
	// computes it from the values and drops whatever a writer sends.
	ObservedStats = "stats"
	// ObservedRevision rises with each rewrite of an observed record and is
	// never lowered (decision 0162).
	ObservedRevision = "revision"
	// ObservedMeasure is the property the engine stamped on an observed
	// record before decision 0186: the measure its unit resolved to. Nothing
	// stamps it now; a write drops a written one and a rewrite clears a
	// stored one.
	ObservedMeasure = "measure"
	// ObservedMetric is the reference to the metric a value is a value of.
	ObservedMetric = obsMetric
	// ObservedOrigin and ObservedOrigins are the provider record, or a
	// trace's records, an adapter built the row from (decision 0162).
	ObservedOrigin  = "origin"
	ObservedOrigins = "origins"

	// A single record's explanations when it holds neither a value nor a
	// code: the reading as received, and why there is none.
	obsSourceValue  = "sourceValue"
	obsAbsentReason = "absentReason"
)

// ObservedResultShapes are the result shapes core's `result` trait adds
// beside `observed(single)`'s `value` and `code`, a lab result's FHIR value
// types: a coded result in full, a ratio, free text. A kind binding both holds
// exactly one shape per record (CheckObserved); on any other kind the names
// are ordinary properties.
var ObservedResultShapes = []string{ResultCodedValue, ResultRatio, ResultText}

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
// `offsets`, `values` and `codes`, a trace's points, and `origins`, the rows
// a trace was built from. Every other one holds a single value.
func ObservedListed(name string) bool {
	return contractShapes[TraitObservedCore][name].list
}

// contractShape is the declared shape the loader holds one contract property
// of a binding kind to, beyond the datatype the trait states: a list, a keyed
// map, or one value, and an object's fields. A trait's property list carries
// datatypes alone, and the rules and readers read each of these one way: a
// list where one value is read would be admitted on every write and read as
// nothing, and a single value where a list is read refuses every write.
type contractShape struct {
	list   bool
	keyed  bool
	fields map[string]Datatype
	// pin is the kind a reference contract property must pin: a metric's
	// `unit` and `units` name unit records (decision 0186), which the write
	// rules read by id.
	pin string
}

// contractShapes are the shapes held per core trait, keyed by the trait's
// identity. A trait listed here holds every contract property it does not
// name to one unkeyed value; a trait not listed holds no shape.
var contractShapes = map[string]map[string]contractShape{
	TraitObservedCore: {
		obsOffsets: {list: true}, obsValues: {list: true}, obsCodes: {list: true}, ObservedOrigins: {list: true},
	},
	TraitRevisedCore: {},
	TraitMetricCore: {
		MetricUnit: {pin: KindUnit}, MetricUnits: {list: true, pin: KindUnit},
		MetricCodeSet: {list: true}, MetricRollups: {list: true},
	},
	TraitSourceCore: {
		SourcePriorities: {keyed: true},
	},
	TraitResultCore: {
		ResultCodedValue: {fields: map[string]Datatype{"system": DatatypeString, "code": DatatypeString, "display": DatatypeString}},
		ResultRatio:      {fields: map[string]Datatype{"numerator": DatatypeDecimal, "denominator": DatatypeDecimal}},
		ResultInterpretation: {list: true, fields: map[string]Datatype{
			"system": DatatypeString, "code": DatatypeString, "display": DatatypeString,
		}},
		ResultReferenceRanges: {list: true, fields: map[string]Datatype{
			"low": DatatypeQuantity, "high": DatatypeQuantity, "text": DatatypeString,
		}},
	},
}

// contractDatatypeMet reports whether a declared datatype meets the one a
// trait contracts. Equal datatypes meet; a declared object meets `json`, an
// object being a JSON value whose fields the kind declares, so a trait can
// contract a structured property whose fields the loader holds
// (contractShapes).
func contractDatatypeMet(want, got Datatype) bool {
	return want == got || (want == DatatypeJSON && got == DatatypeObject)
}

// contractShapeProblems holds a binding kind's declaration of one contract
// property to its shape under the trait (contractShapes).
func contractShapeProblems(where, name, trait string, p *Property) []string {
	shapes, held := contractShapes[trait]
	if !held {
		return nil
	}
	want := shapes[name]
	var out []string
	switch {
	case want.keyed && !p.Keyed:
		out = append(out, fmt.Sprintf("%s: data.properties.%s: trait %s requires %s to be a keyed map: declare keyed: true",
			where, name, trait, name))
	case !want.keyed && p.Keyed:
		out = append(out, fmt.Sprintf("%s: data.properties.%s: trait %s requires %s unkeyed, not a keyed map",
			where, name, trait, name))
	case want.list && !p.Repeated:
		out = append(out, fmt.Sprintf("%s: data.properties.%s: trait %s requires %s to be a list: declare repeated: true",
			where, name, trait, name))
	case !want.list && p.Repeated:
		out = append(out, fmt.Sprintf("%s: data.properties.%s: trait %s requires %s to hold one value, not a list: drop repeated",
			where, name, trait, name))
	}
	if want.pin != "" && p.Datatype == DatatypeReference && (p.ToTrait != "" || !strings.EqualFold(p.To, want.pin)) {
		out = append(out, fmt.Sprintf("%s: data.properties.%s: trait %s requires %s to pin kind %s",
			where, name, trait, name, want.pin))
	}
	if want.fields == nil {
		return out
	}
	if p.Datatype != DatatypeObject {
		return append(out, fmt.Sprintf("%s: data.properties.%s: trait %s requires %s to be an object with fields %s",
			where, name, trait, name, strings.Join(slices.Sorted(maps.Keys(want.fields)), ", ")))
	}
	for _, field := range slices.Sorted(maps.Keys(want.fields)) {
		f, ok := p.Fields[field]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s: data.properties.%s: trait %s requires field %s: %s",
				where, name, trait, field, want.fields[field]))
		case f.Datatype != want.fields[field] || f.Repeated || f.Keyed:
			out = append(out, fmt.Sprintf("%s: data.properties.%s.fields.%s: trait %s requires one %s",
				where, name, field, trait, want.fields[field]))
		}
	}
	return out
}

// ObservedRecord is one record of a kind that binds `observed`, as the write
// path holds it after the merge: the variant its kind binds, the two time
// columns, and the stored property values.
type ObservedRecord struct {
	Variant string
	At      *time.Time
	EndsAt  *time.Time
	Props   map[string]any
	// Result is whether the kind binds core's `result` beside `observed`,
	// which holds a laboratory's result shapes to one per record.
	Result bool
	// Units is the repository's unit graph (decision 0186): the record's
	// units are already resolved to unit ids, `delta` reads on any unit, and
	// `circular` needs a unit with a turn. A nil graph holds no unit, so a
	// `circular` record is refused.
	Units *UnitGraph
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
	if c.r.Result {
		shapes = ObservedResultShapes
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
	} else if n := utf8.RuneCountInString(code); n > MaxObservedCode {
		c.addf("props.code: a code is at most %d characters, got %d; keep longer text in an ordinary record", MaxObservedCode, n)
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
		long := false
		for i, code := range listOf(c.r.Props[obsCodes]) {
			s, isString := code.(string)
			switch n := utf8.RuneCountInString(s); {
			case isString && s == "":
				c.addf("props.codes[%d]: a code is not empty; null marks a point with nothing from it on", i)
			case n > MaxObservedCode && !long:
				long = true
				c.addf("props.codes[%d]: a code is at most %d characters, got %d; keep longer text in an ordinary record", i, MaxObservedCode, n)
			}
		}
	}
	switch {
	case n == 0:
		c.addf("props.%s: a trace holds at least one point", list)
	case n > MaxTracePoints:
		c.addf("props.%s: %d points; a trace holds at most %d", list, n, MaxTracePoints)
	}
	if hasValues {
		for i, v := range listOf(c.r.Props[obsValues]) {
			digits, _ := v.(string)
			if pastDigits(digits) {
				c.addf("props.values[%d]: a trace value holds at most %d digits, the bound a quantity value has", i, MaxDecimalDigits)
				break
			}
			if pastFloat(digits) {
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
	c.offset(obsUTCOffset)
	c.traceSize()
}

// traceSize holds a trace to MaxTraceBytes as the write path stores it: with
// the `stats` the engine computes in place of any the
// properties carry. The merged row of a rewrite still holds the stored ones,
// and a create holds neither, so measuring the properties as given would
// admit a trace whose stamped properties pass the limit and then refuse its
// identical rewrite. The stats are computed only from a trace the rules above
// admitted: TraceStats places points by offsets and spacing this check may
// just have refused.
func (c *obsCheck) traceSize() {
	props := maps.Clone(c.r.Props)
	delete(props, ObservedStats)
	delete(props, ObservedMeasure)
	if len(c.problems) == 0 && c.r.At != nil {
		if stats, err := TraceStats(*c.r.At, c.r.Props); err == nil {
			props[ObservedStats] = stats
		}
	}
	if raw, err := json.Marshal(props); err == nil && len(raw) > MaxTraceBytes {
		c.addf("props: the trace's properties are %d bytes with its stats; a trace holds at most %d (1 MiB): split the period", len(raw), MaxTraceBytes)
	}
}

// pastDigits reports whether a trace value has more than MaxDecimalDigits
// digits. The values list is the `decimal` datatype, which has no length
// bound, and the observed index casts every value to Postgres numeric, which
// holds at most 16383 fractional digits.
func pastDigits(digits string) bool {
	if len(digits) <= MaxDecimalDigits+2 {
		return false
	}
	canonical, err := CanonicalDecimalUnbounded(digits)
	return err == nil && len(strings.TrimLeft(strings.Replace(canonical, ".", "", 1), "-")) > MaxDecimalDigits
}

// pastFloatWhy is the refusal of a value pastFloat finds.
const pastFloatWhy = "the magnitude is past 1e280, the largest the observed index reduces: it weighs a value by the milliseconds it holds, in float64"

// pastFloat reports whether a decimal's magnitude is past
// MaxObservedMagnitude: such a value would overflow the index's time-weighted
// sums, or past the largest float64 derive no point at all. A magnitude below
// the smallest float64 reads as 0.
func pastFloat(digits string) bool {
	f, err := strconv.ParseFloat(digits, 64)
	if errors.Is(err, strconv.ErrRange) {
		return true
	}
	return err == nil && math.Abs(f) > MaxObservedMagnitude
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
// variants: which form holds, resets, wraps, and which unit a delta or a
// circular value needs.
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
	c.graphFlags(unit)
}

// graphFlags holds the two reading flags under the unit graph: `delta`
// marks a difference in any unit, and `circular` needs a unit whose circle
// the graph knows, its own `turn` or its circular component's.
func (c *obsCheck) graphFlags(unit string) {
	if b, _ := c.r.Props[obsDelta].(bool); b && unit == "" {
		c.addf("props.%s: marks a difference, and this record has no unit", obsDelta)
	}
	if b, _ := c.r.Props[obsCircular].(bool); b {
		switch {
		case unit == "":
			c.addf("props.%s: averages an angle on the circle, and this record has no unit", obsCircular)
		case c.r.Units == nil || c.r.Units.Turn(unit) == nil:
			c.addf("props.%s: averages an angle on the circle, and %s declares no turn, one full circle in the unit", obsCircular, unit)
		}
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

// ObservedOffsetOf reads a record's stored `utcOffset` in minutes, false when
// there is none. Both variants carry one: a single value's offset, and the
// one offset every point of a packed record takes. It reads the value the way
// the offset check does, so the index and the write rules agree on what an
// offset is.
func ObservedOffsetOf(props map[string]any) (int64, bool) {
	raw, held := props[obsUTCOffset]
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
		if d <= 0 {
			return nil, fmt.Errorf("every: %s is not longer than zero", s)
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

// observedMetricPins holds every kind of the registry that binds `observed`
// to a `metric` reference whose target carries the `metric` contract: pinned
// to a kind that binds core's `metric`, or to the trait itself. The write
// rules and the aggregate read take a metric's units through that trait, so
// a value pointing at a record without it would convert into nothing. It runs after
// every package of an install is resolved, because the pinned kind's binding
// may arrive in the same batch, and it reads every package, not the installed
// ones alone: a package that drops the binding from its metric kind strands
// an observed kind of a package the install never touched.
func (r *Registry) observedMetricPins() []string {
	r.mu.RLock()
	packages := make([]*Package, 0, len(r.order))
	for _, n := range r.order {
		packages = append(packages, r.packages[n])
	}
	r.mu.RUnlock()
	var problems []string
	for _, g := range packages {
		problems = append(problems, r.observedMetricPinsOf(g)...)
	}
	return problems
}

func (r *Registry) observedMetricPinsOf(g *Package) []string {
	var problems []string
	for _, name := range g.KindOrder {
		t := g.Kinds[name]
		if t == nil {
			continue
		}
		if _, ok := t.Binding(TraitObservedCore); !ok {
			continue
		}
		p, ok := t.Props[obsMetric]
		if !ok || p.Datatype != DatatypeReference {
			continue // the contract check names a missing or mistyped metric
		}
		where := DocKind + " " + t.Identity + ": data.properties.metric"
		switch {
		case p.ToTrait != "":
			if !strings.EqualFold(p.ToTrait, TraitMetricCore) {
				problems = append(problems, fmt.Sprintf("%s: pins trait %s; an observed value's metric pins a kind that binds %s, or that trait",
					where, p.ToTrait, TraitMetricCore))
			}
		case p.To == "" || p.To == ToAny:
			problems = append(problems, fmt.Sprintf("%s: pins no kind; an observed value's metric pins a kind that binds %s, or `trait: %s`",
				where, TraitMetricCore, TraitMetricCore))
		default:
			// A missing target is named here too: only the packages an
			// install rebuilds resolve their pins, so a package that deletes
			// the kind another package's value pins would pass, and the other
			// package would be quarantined at the next open.
			switch target, ok := r.ByIdentity(p.To); {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s: pins %s, which no package declares; declare it, or pin `trait: %s`",
					where, p.To, TraitMetricCore))
			case !target.Implements(TraitMetricCore):
				problems = append(problems, fmt.Sprintf("%s: pins %s, which does not bind %s; bind it there, or pin `trait: %s`",
					where, p.To, TraitMetricCore, TraitMetricCore))
			}
		}
	}
	return problems
}
