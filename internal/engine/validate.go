package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Redacted is what a secret-typed property reads back as, everywhere.
const Redacted = "<redacted>"

var rePhone = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

var reDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// coerceProps validates a write's properties against the type and returns
// them in their stored (JSON-safe, normalised) form. A nil value is a
// delete marker and passes through.
func coerceProps(ty *vocabulary.Kind, in map[string]any) (map[string]any, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(in))
	var problems []string
	for _, name := range sortedKeys(in) {
		p, ok := ty.Prop(name)
		if !ok {
			// A null may address ANY stored property: deleting needs no
			// declaration, or a schema that stops declaring something leaves
			// its stored values unremovable (record 58's cleanup is the
			// witness). A VALUE still requires the declaration.
			if in[name] == nil {
				out[name] = nil
				continue
			}
			problems = append(problems, fmt.Sprintf("props.%s: not declared on %s", name, ty.Identity))
			continue
		}
		v := in[name]
		if p.IsState() {
			// Reached only by engine code building a props map directly: the
			// write path splits state properties out before validation, and a
			// state has no value form to coerce (MODEL §11.4).
			problems = append(problems, fmt.Sprintf("props.%s: a state property moves by transition, not by assignment", name))
			continue
		}
		if v == nil {
			out[name] = nil
			continue
		}
		// Reads redact sensitive values: writing the sentinel back is a round
		// trip, not an assignment, and must leave the stored value alone.
		if p.Sensitive() && v == Redacted {
			continue
		}
		cv, err := coerceValue(p, v)
		if err != nil {
			problems = append(problems, fmt.Sprintf("props.%s: %v", name, err))
			continue
		}
		out[name] = cv
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	return out, nil
}

// coerceValue validates one declared value in its declared CONTAINER: a keyed
// map, a list, or the value itself. The container is the declaration's, so the
// same function coerces a kind's own property and a field at any admitted
// depth — which is what keeps a nested list and a top-level list one rule.
func coerceValue(p *vocabulary.Property, v any) (any, error) {
	switch {
	case p.Keyed:
		return coerceKeyed(p, v)
	case p.Repeated:
		items, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected a list of %s", p.Datatype)
		}
		out := make([]any, 0, len(items))
		for i, item := range items {
			cv, err := coerceScalar(p, item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out = append(out, cv)
		}
		return out, nil
	}
	return coerceScalar(p, v)
}

// coerceKeyed validates a keyed map: the KEYS are data, so nothing refuses one
// for being undeclared — the declared key contract is the whole check — and
// every VALUE follows the rest of the declaration (the declared fields for an
// object, the declared scalar otherwise). A null value drops its key, exactly as
// a null field drops from an object. An empty map stores as {}.
func coerceKeyed(p *vocabulary.Property, v any) (any, error) {
	in, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected a keyed map of %s", p.Datatype)
	}
	out := make(map[string]any, len(in))
	for _, key := range sortedKeys(in) {
		if err := p.CheckKey(key); err != nil {
			return nil, err
		}
		kv := in[key]
		if kv == nil {
			continue
		}
		cv, err := coerceScalar(p, kv)
		if err != nil {
			return nil, fmt.Errorf(".%s: %w", key, err)
		}
		out[key] = cv
	}
	return out, nil
}

// coerceObject validates one object value against its declared fields
// : undeclared fields are rejected, a field explicitly null is
// dropped from the stored object, and each field coerces in its OWN declared
// container — a repeated field elementwise, a keyed field per key, a nested
// object recursively. An empty object stores as {}.
func coerceObject(p *vocabulary.Property, v any) (any, error) {
	in, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected an object with fields %s", strings.Join(p.FieldOrder, ", "))
	}
	out := make(map[string]any, len(in))
	for _, fname := range sortedKeys(in) {
		f, declared := p.Fields[fname]
		if !declared {
			return nil, fmt.Errorf(".%s: not a declared field", fname)
		}
		fv := in[fname]
		if fv == nil {
			continue
		}
		cv, err := coerceValue(f, fv)
		if err != nil {
			return nil, fmt.Errorf(".%s: %w", fname, err)
		}
		out[fname] = cv
	}
	return out, nil
}

// coerceReference validates a reference value's SHAPE and normalizes it toward
// the canonical RECORD PATH — "<kind>/<id>", ONE flat string, the whole stored
// value. Like a blob-ref, this is the PURE half: the existence gate — the
// referent KIND must be known, and the `kind:` pin must match — is taken inside
// the transaction (validateReferences). Here we only reach a path:
//
//   - a full path ("core.substrate.reamde.dev/llmprovider/claude"), left alone;
//   - the AUTHORED SHORT FORM, a bare record id, ONLY when `kind:` pins a
//     concrete kind, which then supplies what the value omits, mirroring a
//     single-target edge's shorthand.
//
// Which of the two a string is, is decided by the value's own SHAPE against the
// pin and never by the registry, so an authored value means the same thing on
// every path that reads it.
//
// AN AMBIGUOUS VALUE IS REFUSED, NEVER GUESSED. Under a pin, a value that
// parses as a full path whose kind is NOT the pin has two live readings — that
// pointer, or a bare id the pin would complete — and they name different
// records. "foo.bar/baz/qux" under a pin at `p/target` is the case: a pointer at
// `foo.bar/baz`, or an id `foo.bar/baz/qux`. Both are refused together, naming
// both readings, because picking one silently is how a pointer ends up at the
// wrong row.
//
// A value that does NOT parse as a path is unambiguous even carrying slashes,
// and that is deliberate: a kind or function IDENTITY is the ordinary short form
// for the properties that name one ("writes: [web.…/page]" under a pin at
// core's `kind`), and it cannot be read as a path because nothing is left for an
// id. Only empty-segment shapes are refused there, since "target/" is an id of
// nothing.
func coerceReference(p *vocabulary.Property, v any) (any, error) {
	pin := p.To
	if pin == vocabulary.ToAny {
		pin = ""
	}
	switch t := v.(type) {
	case string:
		return coerceReferencePath(pin, t)
	case map[string]any:
		// The retired dialect-1 shape, refused BY NAME rather than folded. The
		// boot rung canonicalizes the stored rows that hold one
		// (canonicalizeTriggerCallables), so this door never has to: a pair
		// arriving here is either an author writing the dead shape or a store
		// that skipped the rung, and quietly accepting it would keep the old
		// spelling alive in a dialect that says it is gone.
		kind, _ := t["kind"].(string)
		id, _ := t["id"].(string)
		return nil, fmt.Errorf(
			"a reference is a %q path string; the {kind: %q, id: %q} pair is the retired shape, and the boot rung is what migrates a stored one",
			"<kind>/<id>", kind, id)
	default:
		return nil, fmt.Errorf(`a reference is a "<kind>/<id>" path string`)
	}
}

// coerceReferencePath holds one authored string to the value model: the whole
// decision, in one place, so the write path and the rung cannot answer it
// differently.
func coerceReferencePath(pin, s string) (any, error) {
	if s == "" {
		return nil, fmt.Errorf("a reference needs an id")
	}
	kind, id, isPath := vocabulary.SplitRecordPath(s)
	if pin == "" {
		// Unpinned there is no kind to borrow, so only a full path says what
		// this names. A dotted first segment is an AUTHORITY, so "foo.bar/baz"
		// names the kind `foo.bar/baz` and leaves nothing for the id; "note/abc"
		// is the local kind `note` and the id `abc`.
		if !isPath {
			return nil, fmt.Errorf(
				`a reference to any kind needs a full "<kind>/<id>" path, and %q is not one`, s)
		}
		if hasEmptySegment(id) {
			return nil, fmt.Errorf("reference %q has an empty id segment", s)
		}
		return s, nil
	}
	if isPath {
		if kind == pin {
			if hasEmptySegment(id) {
				return nil, fmt.Errorf("reference %q has an empty id segment", s)
			}
			return s, nil
		}
		// Both readings, named, because either end may be the one to change.
		return nil, fmt.Errorf(
			"reference %q is ambiguous: it reads as a pointer at %s, or as a bare id the pin would complete to %q — and the declaration pins %s, so write that path in full",
			s, kind, vocabulary.RecordPath(pin, s), pin)
	}
	if hasEmptySegment(s) {
		return nil, fmt.Errorf("reference %q has an empty segment, so it names no record", s)
	}
	return vocabulary.RecordPath(pin, s), nil
}

// hasEmptySegment reports whether a record id has a slash with nothing on one
// side of it. "target/" and "/x" and "a//b" all name no record, and completing
// one from a pin would store a path that can never be split back.
func hasEmptySegment(id string) bool {
	return id == "" || strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") ||
		strings.Contains(id, "//")
}

func coerceScalar(p *vocabulary.Property, v any) (any, error) {
	switch p.Datatype {
	case vocabulary.DatatypeObject:
		return coerceObject(p, v)
	case vocabulary.DatatypeReference:
		return coerceReference(p, v)
	case vocabulary.DatatypeJSON:
		return jsonRoundTrip(v)
	case vocabulary.DatatypeBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expected a bool")
		}
		return b, nil
	case vocabulary.DatatypeInt:
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		if err := checkRange(p, float64(n)); err != nil {
			return nil, err
		}
		return n, nil
	case vocabulary.DatatypeDecimal:
		return coerceDecimal(p, v)
	case vocabulary.DatatypeFloat:
		f, err := asFloat(v)
		if err != nil {
			return nil, err
		}
		if err := checkRange(p, f); err != nil {
			return nil, err
		}
		return f, nil
	}

	s, err := asString(v)
	if err != nil {
		return nil, err
	}
	switch p.Datatype {
	case vocabulary.DatatypeDatetime:
		ts, err := parseTime(s)
		if err != nil {
			return nil, err
		}
		return ts.UTC().Format(time.RFC3339Nano), nil
	case vocabulary.DatatypeDate:
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, fmt.Errorf("expected a civil date (2006-01-02)")
		}
	case vocabulary.DatatypeDuration:
		d, err := parseISODuration(s)
		if err != nil {
			return nil, err
		}
		s = formatISODuration(d)
	case vocabulary.DatatypeEmail:
		if _, err := mail.ParseAddress(s); err != nil {
			return nil, fmt.Errorf("expected an RFC 5322 mailbox")
		}
	case vocabulary.DatatypeURL:
		u, err := url.Parse(s)
		if err != nil || !u.IsAbs() {
			return nil, fmt.Errorf("expected an absolute URL")
		}
	case vocabulary.DatatypePhone:
		if !rePhone.MatchString(s) {
			return nil, fmt.Errorf("expected an E.164 phone number")
		}
	case vocabulary.DatatypeTimezone:
		if _, err := time.LoadLocation(s); err != nil {
			return nil, fmt.Errorf("expected an IANA time zone name")
		}
	case vocabulary.DatatypeRecurrence:
		body := strings.TrimPrefix(s, "RRULE:")
		if !strings.Contains(body, "FREQ=") {
			return nil, fmt.Errorf("expected an RFC 5545 RRULE string")
		}
	case vocabulary.DatatypeBlobRef:
		// Shape only here; the txn checks the blob record exists
		// (validateBlobRefs), because coercion is pure. A blob-ref names bytes
		// by their digest, which is the blob record's id.
		if !validBlobDigest(s) {
			return nil, fmt.Errorf("expected a blob digest (%s<64 hex>)", substrate.BlobDigestPrefix)
		}
		return s, nil
	case vocabulary.DatatypeDigest:
		// Server-minted comparator: exactly a lowercase hex SHA-256. Anything
		// else is material masquerading as a digest, and material is `secret`.
		if !reDigest.MatchString(s) {
			return nil, fmt.Errorf("expected a SHA-256 digest (64 lowercase hex)")
		}
	case vocabulary.DatatypeEnum:
		if vals := p.ValueStrings(); !containsString(vals, s) {
			return nil, fmt.Errorf("expected one of %s", strings.Join(vals, ", "))
		}
	}
	if vals := p.ValueStrings(); len(vals) > 0 && p.Datatype != vocabulary.DatatypeEnum && !containsString(vals, s) {
		return nil, fmt.Errorf("expected one of %s", strings.Join(vals, ", "))
	}
	if p.Pattern != nil && !p.Pattern.MatchString(s) {
		return nil, fmt.Errorf("does not match %s", p.Pattern.String())
	}
	return s, nil
}

func checkRange(p *vocabulary.Property, f float64) error {
	if p.Min != nil && f < *p.Min {
		return fmt.Errorf("must be >= %v", *p.Min)
	}
	if p.Max != nil && f > *p.Max {
		return fmt.Errorf("must be <= %v", *p.Max)
	}
	return nil
}

// maxSafeInt is the largest integer every JSON door carries exactly. The REST
// body decode (strictjson without UseNumber) and the jsonb read-back (rows.go)
// both ride float64, whose mantissa holds 53 bits, so an int past this bound
// would corrupt silently on its next trip even where THIS parse saw it
// exactly. 2^53 itself is excluded: 2^53+1 arrives AS 2^53, so a value at the
// boundary cannot prove it was not corrupted on the way in.
const maxSafeInt = 1<<53 - 1

// asInt reads an integer exactly, the bound enforced on every input shape so
// the contract does not depend on which door the value came through. A
// json.Number is read by its own spelling, so an oversized one is REFUSED
// rather than rounded into range by the float conversion.
func asInt(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return boundedInt(int64(n))
	case int64:
		return boundedInt(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return boundedInt(i)
		}
		f, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("expected an integer")
		}
		return intFromFloat(f)
	case float64:
		return intFromFloat(n)
	case float32:
		return intFromFloat(float64(n))
	default:
		return 0, fmt.Errorf("expected a number")
	}
}

func boundedInt(n int64) (int64, error) {
	if n > maxSafeInt || n < -maxSafeInt {
		return 0, fmt.Errorf("an int is a safe integer (|value| <= %d): a bigger count only survives the float64 doors as a decimal string", int64(maxSafeInt))
	}
	return n, nil
}

func intFromFloat(f float64) (int64, error) {
	// Bound before the integrality check: past the bound int64(f) is not
	// exact, and the bound is the real refusal anyway.
	if f > maxSafeInt || f < -maxSafeInt {
		return boundedInt(maxSafeInt + 1)
	}
	if f != float64(int64(f)) {
		return 0, fmt.Errorf("expected an integer")
	}
	return int64(f), nil
}

var reDecimal = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)

// reDecimalExponent spots scientific notation, so its refusal can say what to
// write instead of the generic grammar line.
var reDecimalExponent = regexp.MustCompile(`^[+-]?[0-9]*\.?[0-9]+[eE][+-]?[0-9]+$`)

// coerceDecimal admits an exact decimal: a STRING of digits, refused as a bare
// JSON number because a number rides float64 through every door and may
// arrive already rounded, which is the whole reason the datatype exists. The
// stored form is canonical (no '+', no leading zeros, no negative zero) but
// the SCALE is data: "19.90" stays "19.90".
func coerceDecimal(p *vocabulary.Property, v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf(`a decimal is written as a string ("19.99"): a bare JSON number rides float64 and may already be rounded`)
	}
	c, err := canonicalDecimal(s)
	if err != nil {
		return nil, err
	}
	if p.Min != nil || p.Max != nil {
		r, _ := new(big.Rat).SetString(c)
		if p.Min != nil {
			if min := new(big.Rat).SetFloat64(*p.Min); min != nil && r.Cmp(min) < 0 {
				return nil, fmt.Errorf("must be >= %v", *p.Min)
			}
		}
		if p.Max != nil {
			if max := new(big.Rat).SetFloat64(*p.Max); max != nil && r.Cmp(max) > 0 {
				return nil, fmt.Errorf("must be <= %v", *p.Max)
			}
		}
	}
	return c, nil
}

// canonicalDecimal holds one authored decimal to the grammar (an optional
// sign, digits, an optional fraction, and nothing else) and answers the
// canonical spelling. No exponent: the datatype is the exact digits, and
// "1.999e1" admits a second spelling for every value.
func canonicalDecimal(s string) (string, error) {
	if !reDecimal.MatchString(s) {
		if reDecimalExponent.MatchString(s) {
			return "", fmt.Errorf("a decimal writes its digits out, without an exponent")
		}
		return "", fmt.Errorf(`expected a decimal ("19.99"): an optional sign, digits, an optional fraction`)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	intPart, frac, _ := strings.Cut(s, ".")
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	out := intPart
	if frac != "" {
		out += "." + frac
	}
	// "-0.00" is zero: the sign drops so equal values store one spelling.
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out, nil
}

func asFloat(v any) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	default:
		return 0, fmt.Errorf("expected a number")
	}
}

func asString(v any) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	case time.Time:
		return s.UTC().Format(time.RFC3339Nano), nil
	default:
		return "", fmt.Errorf("expected a string")
	}
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected an RFC 3339 instant")
}

// reISODuration is ISO 8601's duration, MINUS years and months: weeks, days,
// and a time part, each component optional, fractions only on the time part.
var reISODuration = regexp.MustCompile(
	`^-?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// reISOCalendar spots a year or a month in an ISO duration's DATE part (a
// time-part M is minutes and stays legal), so the refusal can say why.
var reISOCalendar = regexp.MustCompile(`^-?P[^T]*\d+[YM]`)

var errDurationGrammar = fmt.Errorf("expected an ISO 8601 duration (PT47M12S, P2DT3H, P1W)")

// parseISODuration reads a duration. ISO 8601 is the ONE grammar, in and out:
// Go's own syntax ("47m12s") is refused, because two grammars for one word is
// how "duration" stops meaning anything. The parse translates into Go's
// grammar internally and lets time.ParseDuration do the arithmetic (its
// parser sums repeated units, so weeks and days become hour terms). Years and
// months are refused BY NAME: neither has a fixed length, and a duration here
// is exact time: a day is exactly 24h and a week exactly 168h.
func parseISODuration(s string) (time.Duration, error) {
	m := reISODuration.FindStringSubmatch(s)
	if m == nil {
		if reISOCalendar.MatchString(s) {
			return 0, fmt.Errorf("years and months have no fixed length: spell the duration in weeks, days and a time part (P2DT3H)")
		}
		return 0, errDurationGrammar
	}
	if m[1] == "" && m[2] == "" && m[3] == "" && m[4] == "" && m[5] == "" {
		return 0, errDurationGrammar
	}
	var b strings.Builder
	if strings.HasPrefix(s, "-") {
		b.WriteString("-")
	}
	for _, part := range []struct {
		digits string
		hours  int64
	}{{m[1], 7 * 24}, {m[2], 24}} {
		if part.digits == "" {
			continue
		}
		n, err := strconv.ParseInt(part.digits, 10, 64)
		if err != nil || n > math.MaxInt64/part.hours {
			return 0, fmt.Errorf("the duration overflows what one can hold (about 292 years)")
		}
		fmt.Fprintf(&b, "%dh", n*part.hours)
	}
	for _, part := range []struct{ digits, unit string }{
		{m[3], "h"}, {m[4], "m"}, {m[5], "s"},
	} {
		if part.digits != "" {
			b.WriteString(part.digits + part.unit)
		}
	}
	d, err := time.ParseDuration(b.String())
	if err != nil {
		return 0, fmt.Errorf("the duration overflows what one can hold (about 292 years)")
	}
	return d, nil
}

// formatISODuration renders the ONE stored spelling of a duration: days (each
// exactly 24h), then a time part, zero components omitted, "PT0S" for zero.
// Deterministic decomposition is what makes it canonical: every value has
// exactly one stored form, however it was authored ("PT36H" stores as
// "P1DT12H", "P2W" as "P14D").
func formatISODuration(d time.Duration) string {
	var b strings.Builder
	if d < 0 {
		b.WriteString("-")
		d = -d
	}
	b.WriteString("P")
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	if days > 0 {
		fmt.Fprintf(&b, "%dD", days)
	}
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	ns := d % time.Second
	if h > 0 || m > 0 || s > 0 || ns > 0 || days == 0 {
		b.WriteString("T")
		if h > 0 {
			fmt.Fprintf(&b, "%dH", h)
		}
		if m > 0 {
			fmt.Fprintf(&b, "%dM", m)
		}
		if ns > 0 {
			frac := strings.TrimRight(fmt.Sprintf("%09d", ns), "0")
			fmt.Fprintf(&b, "%d.%sS", s, frac)
		} else if s > 0 || (h == 0 && m == 0 && days == 0) {
			fmt.Fprintf(&b, "%dS", s)
		}
	}
	return b.String()
}

func jsonRoundTrip(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("not JSON-encodable: %w", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// --- labels ---

// coerceLabels enforces namespaced keys, the writer's namespace, and the
// scalar-only shape.
func coerceLabels(actor substrate.Actor, in map[string]any) (map[string]any, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(in))
	for _, k := range sortedKeys(in) {
		if err := metaKeyAllowed(actor, k); err != nil {
			return nil, err
		}
		v := in[k]
		switch v.(type) {
		case nil, bool, string, float64, int, int64, json.Number:
		default:
			return nil, fmt.Errorf("%w: label %q must be a scalar (blobs are annotations)", substrate.ErrValidation, k)
		}
		cv, err := jsonRoundTrip(v)
		if err != nil {
			return nil, err
		}
		out[k] = cv
	}
	return out, nil
}

// --- derived title, snippet, FTS bands ---

// titleResolver renders a type's display_template against a stored row,
// following declared edges to their first target.
type titleResolver struct {
	t     *txn
	ty    *vocabulary.Kind
	row   *erow
	edges map[string][]eref
}

func (r *titleResolver) Prop(name string) string {
	// The loader refuses a sensitive property in a template, but a legacy
	// declaration may predate that rule: render empty rather than copy a
	// value every read surface redacts into the unsealed, FTS-indexed title.
	if p, ok := r.ty.Prop(name); ok && p.Sensitive() {
		return ""
	}
	if v, ok := r.row.Props[name]; ok {
		// A reference is a record PATH, which scalarString would render
		// verbatim — a title reading "core.substrate.reamde.dev/agent/x"
		// names the pointer, not the thing. It follows the pointer instead,
		// exactly as a bare edge token does, and
		// falls back to the id when the referent is not there to read: a
		// reference may name a row that does not exist (references.go).
		if p, ok := r.ty.Prop(name); ok && p.Datatype == vocabulary.DatatypeReference {
			return r.reference(name, "")
		}
		return scalarString(v)
	}
	if name == "title" {
		return r.row.Title
	}
	return ""
}

// Declares reports whether the kind declares a property OR an edge of that name,
// which is what a derived token yields to. An edge counts because a bare token
// means either one and the loader refuses a kind that declares both under one
// name: without it, `{localName}` on a kind whose EDGE is `localName` would
// render the id's last segment where the model says the target's title.
//
// A sensitive property counts as declared too: Prop renders it empty on purpose,
// and answering with the id-derived value instead would put something in a title
// the declaration meant to keep out of one.
func (r *titleResolver) Declares(name string) bool {
	if _, ok := r.ty.Prop(name); ok {
		return true
	}
	_, ok := r.ty.Edge(name)
	return ok
}

// reference renders a reference property: the referent's title, or the named
// property of it. Repeated references render each, comma-joined, the way a
// many-edge does.
func (r *titleResolver) reference(name, prop string) string {
	refs := referenceTargets(r.row.Props[name])
	if len(refs) == 0 {
		return ""
	}
	if len(refs) == 1 {
		return r.referenceProp(refs[0], prop)
	}
	var parts []string
	for _, ref := range refs {
		if s := r.referenceProp(ref, prop); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// referenceProp reads the referent, and answers with the bare id when there is
// no referent to read — a dangling pointer still NAMES something, and
// rendering "" would make a template lose the only identifier it had.
//
// The fallback turns on the row's ABSENCE, not on an empty answer: a referent
// that exists and has no title legitimately renders as nothing, and printing
// its id instead would claim the id was its title.
func (r *titleResolver) referenceProp(ref eref, prop string) string {
	row, err := r.t.loadRow(ref, false)
	if err != nil || row == nil {
		if prop == "" {
			return ref.ID
		}
		return ""
	}
	if prop == "" {
		return row.Title
	}
	if v, ok := row.Props[prop]; ok {
		return scalarString(v)
	}
	return ""
}

// referenceID reads the id a stored reference names, "" when the value is not
// one.
func referenceID(v any) string {
	refs := referenceTargets(v)
	if len(refs) == 0 {
		return ""
	}
	return refs[0].ID
}

// referenceTargets reads the stored shape of a reference property — one
// canonical record PATH, or a list of them when repeated. A stored value is
// always a full path (normalizeReference wrote it), so a string that does not
// split is not a reference and yields nothing rather than a kindless target.
func referenceTargets(v any) []eref {
	one := func(s string) (eref, bool) {
		kind, id, ok := vocabulary.SplitRecordPath(s)
		if !ok {
			return eref{}, false
		}
		return eref{Kind: kind, ID: id}, true
	}
	switch t := v.(type) {
	case string:
		if ref, ok := one(t); ok {
			return []eref{ref}
		}
	case []any:
		var out []eref
		for _, item := range t {
			if s, ok := item.(string); ok {
				if ref, ok := one(s); ok {
					out = append(out, ref)
				}
			}
		}
		return out
	}
	return nil
}

// Derived renders a derived token from the row itself. {localName} is the id's
// last segment and {id} the whole id: a DECLARATION's id is a kind reference
// ("people.substrate.reamde.dev/person"), so the local name is what a reader
// calls the thing, and for an ordinary slashless id the two answer the same.
func (r *titleResolver) Derived(token string) string {
	switch token {
	case vocabulary.DerivedSnippet:
		return snippetOf(r.ty, r.row)
	case vocabulary.DerivedLocalName:
		return localNameOf(r.row.ID)
	case vocabulary.DerivedID:
		return r.row.ID
	}
	return ""
}

// localNameOf is an id's last non-empty segment, and the whole id when it has
// none. The LAST slash, not the kind reference's one: an id may legally carry
// several (the alphabet admits "/" so a declaration's id can BE a kind
// reference), and the last segment is the one a reader would call the thing.
//
// A TRAILING slash is legal in the id alphabet, and splitting on it plainly
// would render an empty title for a row whose id is anything but empty — so it
// is trimmed first. The alphabet's first character is alphanumeric, so trimming
// can never empty the whole id.
func localNameOf(id string) string {
	trimmed := strings.TrimRight(id, "/")
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

func (r *titleResolver) Edge(rel, prop string) string {
	// A dotted token whose head is an OBJECT PROPERTY reads its field
	//: `{name.displayName}` renders one level into the object,
	// empty when the property or field is absent. The loader has already
	// checked the token names a declared edge or field, so the two forms
	// cannot collide.
	if prop != "" {
		if p, ok := r.ty.Props[rel]; ok && p.Datatype == vocabulary.DatatypeObject {
			if p.Repeated {
				return ""
			}
			if m, ok := r.row.Props[rel].(map[string]any); ok {
				return scalarString(m[prop])
			}
			return ""
		}
		// A dotted token whose head is a REFERENCE reads that property off the
		// referent, the same hop a dotted edge token takes. No shipped
		// declaration spells one: llmthread titled itself `{agent.name}` until
		// core/agent stopped declaring `name`, and the bare `{agent}` that
		// replaced it renders the referent's own TITLE instead (Prop, above).
		// The loader has checked the head names a declared edge, object or
		// reference, so the three forms cannot collide.
		if p, ok := r.ty.Prop(rel); ok && p.Datatype == vocabulary.DatatypeReference {
			return r.reference(rel, prop)
		}
	}
	targets := r.edges[rel]
	if len(targets) == 0 {
		return ""
	}
	if prop == "" {
		var titles []string
		for _, ref := range targets {
			if tt := r.targetProp(ref, ""); tt != "" {
				titles = append(titles, tt)
			}
		}
		return strings.Join(titles, ", ")
	}
	return r.targetProp(targets[0], prop)
}

func (r *titleResolver) targetProp(ref eref, prop string) string {
	row, err := r.t.loadRow(ref, false)
	if err != nil || row == nil {
		return ""
	}
	if prop == "" {
		return row.Title
	}
	// The loader cannot check an edge target's property (the target is
	// another type's business, and `to: any` has no target at all), so the
	// sensitive skip is enforced here.
	if ty, terr := r.t.ds.resolveType(row.Kind); terr == nil && ty != nil {
		if p, ok := ty.Prop(prop); ok && p.Sensitive() {
			return ""
		}
	}
	if v, ok := row.Props[prop]; ok {
		return scalarString(v)
	}
	return ""
}

func scalarString(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case []any:
		var parts []string
		for _, item := range s {
			if x := scalarString(item); x != "" {
				parts = append(parts, x)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		return ""
	case float64:
		if s == float64(int64(s)) {
			return fmt.Sprintf("%d", int64(s))
		}
		return fmt.Sprintf("%g", s)
	case json.Number:
		// A replayed delta carries its numbers as json.Number (scanChange
		// preserves the stored spelling), but the fts a rebuild writes must be
		// the fts the live fold wrote from int64/float64 values, so a Number
		// normalizes through the same formatting rather than printing verbatim.
		if i, err := s.Int64(); err == nil {
			return fmt.Sprintf("%d", i)
		}
		if f, err := s.Float64(); err == nil {
			return scalarString(f)
		}
		return s.String()
	default:
		return fmt.Sprint(v)
	}
}

// snippetOf is the first 80 characters of the longest text-family property.
func snippetOf(ty *vocabulary.Kind, row *erow) string {
	best := ""
	for _, name := range ty.PropOrder {
		p := ty.Props[name]
		if !vocabulary.IsLongText(p.Datatype) || p.Sensitive() {
			continue
		}
		s := scalarString(row.Props[name])
		if len(s) > len(best) {
			best = s
		}
	}
	best = strings.Join(strings.Fields(best), " ")
	if len(best) > 80 {
		return strings.TrimSpace(best[:80])
	}
	return best
}

// deriveTitle applies the type's display_template; types without one keep
// the writer's title.
func (t *txn) deriveTitle(ty *vocabulary.Kind, row *erow) (string, error) {
	if ty.Template == nil {
		return row.Title, nil
	}
	edges, err := t.edgesOf(row.ref())
	if err != nil {
		return "", err
	}
	return ty.Template.Render(&titleResolver{t: t, ty: ty, row: row, edges: edges}), nil
}

// ftsBands splits a row into the three weighted search bands: title (A),
// declared short string properties (B), body and prose (C). Properties opt
// out with fts:false; secrets never index.
func ftsBands(ty *vocabulary.Kind, row *erow) [3]string {
	var b, c []string
	for _, name := range ty.PropOrder {
		p := ty.Props[name]
		if !p.FTS || p.Sensitive() {
			continue
		}
		s := scalarString(row.Props[name])
		if s == "" {
			continue
		}
		if vocabulary.IsLongText(p.Datatype) {
			c = append(c, s)
		} else {
			b = append(b, s)
		}
	}
	if row.Body != "" {
		c = append(c, row.Body)
	}
	return [3]string{row.Title, strings.Join(b, " "), strings.Join(c, " ")}
}

// --- projection ---

// recordOf projects a stored row onto the wire type, redacting every
// sensitive property. EVERYTHING AUTHORED IS A PROPERTY:
// storage keeps title, body, the temporal instants and the machine states in
// their own columns, and the wire shows ONE properties map holding all of
// them.
func recordOf(ty *vocabulary.Kind, row *erow) *substrate.Record {
	e := &substrate.Record{
		ID: row.ID, Kind: row.Kind, Title: row.Title, Body: row.Body,
		At: row.At, EndsAt: row.EndsAt, DueAt: row.DueAt,
		Properties: redactProps(ty, row.Props), Labels: row.Labels,
		Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt, Finalizers: row.Finalizers,
	}
	if e.Properties == nil {
		e.Properties = map[string]any{}
	}
	for name, state := range row.States {
		e.Properties[name] = state
	}
	if row.Title != "" {
		e.Properties[substrate.PropTitle] = row.Title
	}
	if row.Body != "" {
		e.Properties[substrate.PropBody] = row.Body
	}
	for _, hc := range []struct {
		name string
		v    *time.Time
	}{
		{substrate.PropAt, row.At},
		{substrate.PropEndsAt, row.EndsAt},
		{substrate.PropDueAt, row.DueAt},
	} {
		if hc.v != nil {
			e.Properties[hc.name] = hc.v.UTC().Format(time.RFC3339Nano)
		}
	}
	if e.Labels == nil {
		e.Labels = map[string]any{}
	}
	return e
}

func redactProps(ty *vocabulary.Kind, props map[string]any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		if ty != nil {
			if p, ok := ty.Prop(k); ok && p.Sensitive() {
				out[k] = Redacted
				continue
			}
		}
		out[k] = v
	}
	return out
}
