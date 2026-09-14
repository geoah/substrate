package engine

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The int contract: a safe integer, refused past 2^53-1 in magnitude on EVERY
// input shape, because both the REST decode and the jsonb read-back ride
// float64 and a bigger value corrupts silently on one of those trips. A
// json.Number is read by its own spelling, so 2^53+1 is refused rather than
// rounded into range.
func TestCoerceIntIsASafeInteger(t *testing.T) {
	p := &vocabulary.Property{Name: "n", Datatype: vocabulary.DatatypeInt}
	for _, tc := range []struct {
		name string
		in   any
		want int64
	}{
		{"a small float64", float64(12), 12},
		{"the largest safe integer", float64(maxSafeInt), maxSafeInt},
		{"the most negative safe integer", float64(-maxSafeInt), -maxSafeInt},
		{"a json.Number in range", json.Number("42"), 42},
	} {
		got, err := coerceScalar(p, tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %v, want %d", tc.name, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name string
		in   any
		want string
	}{
		{"2^53 as a float64", float64(maxSafeInt + 1), "safe integer"},
		{"-2^53 as a float64", float64(-maxSafeInt - 1), "safe integer"},
		{"2^53 as an int64", int64(maxSafeInt + 1), "safe integer"},
		// The silent-corruption case the bound exists for: 2^53+1 decodes to
		// the float64 2^53, inside any naive bound, so the spelling is what
		// refuses it.
		{"2^53+1 as a json.Number", json.Number("9007199254740993"), "safe integer"},
		{"far past the bound", float64(1e300), "safe integer"},
		{"a fraction", float64(1.5), "expected an integer"},
		{"a string", "12", "expected a number"},
	} {
		if _, err := coerceScalar(p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want it to name %q", tc.name, err, tc.want)
		}
	}
}

// The decimal contract: a string of exact digits, canonicalized but never
// rescaled, and a bare JSON number refused because it may already be rounded.
func TestCoerceDecimalIsExact(t *testing.T) {
	p := &vocabulary.Property{Name: "amount", Datatype: vocabulary.DatatypeDecimal}
	for in, want := range map[string]string{
		"19.99":                  "19.99",
		"19.90":                  "19.90", // the scale is data
		"+007.50":                "7.50",
		"0":                      "0",
		"-0.00":                  "0.00",
		"-12.05":                 "-12.05",
		"9007199254740993.00001": "9007199254740993.00001", // past any float64
	} {
		got, err := coerceScalar(p, in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %v, want %q", in, got, want)
		}
	}
	for _, tc := range []struct {
		name string
		in   any
		want string
	}{
		{"a bare JSON number", float64(19.99), "written as a string"},
		{"a json.Number", json.Number("19.99"), "written as a string"},
		{"an exponent", "1.9e2", "without an exponent"},
		{"a bare dot", ".5", "expected a decimal"},
		{"a trailing dot", "19.", "expected a decimal"},
		{"prose", "about twenty", "expected a decimal"},
		{"an empty string", "", "expected a decimal"},
	} {
		if _, err := coerceScalar(p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want it to name %q", tc.name, err, tc.want)
		}
	}
	zero := 0.0
	bounded := &vocabulary.Property{Name: "amount", Datatype: vocabulary.DatatypeDecimal, Min: &zero}
	if _, err := coerceScalar(bounded, "-0.01"); err == nil || !strings.Contains(err.Error(), ">= 0") {
		t.Fatalf("min: got %v, want the bound named", err)
	}
	if got, err := coerceScalar(bounded, "0.00"); err != nil || got != "0.00" {
		t.Fatalf("min boundary: got %v, %v", got, err)
	}
}

// The duration contract: ISO 8601 is the ONE grammar, in and out. Years and
// months are refused (no fixed length), Go's own syntax is refused (a second
// grammar for the same word), and the stored form is a deterministic ISO
// decomposition, so every value has exactly one spelling.
func TestCoerceDurationIsISO8601Only(t *testing.T) {
	p := &vocabulary.Property{Name: "for", Datatype: vocabulary.DatatypeDuration}
	for in, want := range map[string]string{
		"PT47M12S": "PT47M12S",
		"PT1M":     "PT1M", // a time-part M is minutes
		"PT90M":    "PT1H30M",
		"PT36H":    "P1DT12H",
		"P1DT12H":  "P1DT12H",
		"P1D":      "P1D",
		"P2W":      "P14D",
		"P1W2DT3H": "P9DT3H",
		"PT0.5H":   "PT30M",
		"PT1.5S":   "PT1.5S",
		"-PT30M":   "-PT30M",
		"PT0S":     "PT0S",
	} {
		got, err := coerceScalar(p, in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %v, want %q", in, got, want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"P1Y", "no fixed length"},
		{"P3M", "no fixed length"},
		{"P", "expected an ISO 8601 duration"},
		{"PT", "expected an ISO 8601 duration"},
		// Go's grammar is the retired spelling, refused so "duration" keeps
		// meaning one thing.
		{"47m12s", "expected an ISO 8601 duration"},
		{"3d", "expected an ISO 8601 duration"},
		{"soon", "expected an ISO 8601 duration"},
		{"P999999999999999999W", "overflows"},
	} {
		if _, err := coerceScalar(p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: got %v, want it to name %q", tc.in, err, tc.want)
		}
	}
}

// The instant contract: what Postgres's timestamptz holds and nothing wider.
// A stored year-0000 instant fails every read that casts (a range filter, an
// ordering) and takes the whole collection listing with it, so the refusal
// happens at the write, naming the property.
func TestCoerceDatetimeStaysInPostgresRange(t *testing.T) {
	dt := &vocabulary.Property{Name: "at", Datatype: vocabulary.DatatypeDatetime}
	date := &vocabulary.Property{Name: "on", Datatype: vocabulary.DatatypeDate}
	for _, tc := range []struct {
		p  *vocabulary.Property
		in string
	}{
		{dt, "2026-08-17T12:00:00Z"},
		{dt, "0001-01-01T00:00:00Z"},
		{date, "2026-08-17"},
		{date, "0001-01-01"},
	} {
		if _, err := coerceScalar(tc.p, tc.in); err != nil {
			t.Fatalf("%s %q: %v", tc.p.Datatype, tc.in, err)
		}
	}
	for _, tc := range []struct {
		p    *vocabulary.Property
		in   string
		want string
	}{
		{dt, "0000-01-01T00:00:00Z", "no year zero"},
		{dt, "0000-12-31T23:59:59Z", "no year zero"},
		{date, "0000-01-01", "no year zero"},
	} {
		if _, err := coerceScalar(tc.p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s %q: got %v, want it to name %q", tc.p.Datatype, tc.in, err, tc.want)
		}
	}
	// The whole properties map answers with a ValidationError naming the
	// property, which is what the API turns into a 422 with problems.
	_, err := coerceProps(&vocabulary.Kind{
		Identity: "example.com/example/thing",
		Props:    map[string]*vocabulary.Property{"at": dt},
	}, map[string]any{"at": "0000-01-01T00:00:00Z"})
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 1 || !strings.Contains(ve.Problems[0], "props.at") {
		t.Fatalf("got %v, want a ValidationError naming props.at", err)
	}
}

// The blob-ref contract: a read hands back the manifest ({digest, name,
// mediaType, size, status}) and the store holds the digest string, so a write
// carrying the read shape takes its `digest` and nothing else. Resolved
// metadata is not writable authority: a `name` or `size` in the object changes
// nothing, and an object with no digest names no blob.
func TestCoerceBlobRefTakesTheDigestFromTheReadShape(t *testing.T) {
	digest := substrate.BlobDigestPrefix + strings.Repeat("a", 64)
	other := substrate.BlobDigestPrefix + strings.Repeat("b", 64)
	manifest := map[string]any{
		"digest": digest, "name": "layout.png", "mediaType": "image/png",
		"size": json.Number("2048"), "status": "stored",
	}
	single := &vocabulary.Property{Name: "attachment", Datatype: vocabulary.DatatypeBlobRef}
	repeated := &vocabulary.Property{Name: "attachments", Datatype: vocabulary.DatatypeBlobRef, Repeated: true}

	for _, tc := range []struct {
		name string
		p    *vocabulary.Property
		in   any
		want any
	}{
		{"the digest string", single, digest, digest},
		{"the read shape", single, manifest, digest},
		{"the bare manifest of a missing blob", single, map[string]any{"digest": digest}, digest},
		{"a list of digests", repeated, []any{digest, other}, []any{digest, other}},
		{"a list mixing the two shapes", repeated, []any{manifest, other}, []any{digest, other}},
	} {
		got, err := coerceValue(tc.p, tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %#v, want %#v", tc.name, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name string
		p    *vocabulary.Property
		in   any
		want string
	}{
		{"an object with no digest", single, map[string]any{"name": "layout.png"}, `under "digest"`},
		{"an object whose digest is not a string", single, map[string]any{"digest": 7}, `under "digest"`},
		{"an object whose digest is malformed", single, map[string]any{"digest": "sha256:abc"}, "expected a blob digest"},
		{"a number", single, 7, "expected a string"},
		{"a list item with no digest", repeated, []any{map[string]any{"size": json.Number("1")}}, "[0]"},
	} {
		if _, err := coerceValue(tc.p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want it to name %q", tc.name, err, tc.want)
		}
	}
}

// A snippet is 80 CHARACTERS, not bytes: a byte cut that lands inside a
// multibyte sequence left a partial rune in the derived title, which Postgres
// refused as invalid UTF-8 (#540). The message that found it has an ellipsis
// across byte 80.
func TestSnippetCutsOnARuneBoundary(t *testing.T) {
	ty := &vocabulary.Kind{
		PropOrder: []string{"text"},
		Props:     map[string]*vocabulary.Property{"text": {Name: "text", Datatype: vocabulary.DatatypeText}},
	}
	for name, text := range map[string]string{
		"an ellipsis across byte 80": "RFC doc (still WIP) for fast -det mode. docs.google.com/document/d/1wnCnDf_rPnX…/edit?tab=t.0#heading=…",
		"multibyte throughout":       strings.Repeat("ü", 79) + "…" + strings.Repeat("ü", 40),
		"short":                      "under the limit …",
	} {
		t.Run(name, func(t *testing.T) {
			got := snippetOf(ty, &erow{Props: map[string]any{"text": text}})
			if !utf8.ValidString(got) {
				t.Fatalf("the snippet is not valid UTF-8: %q", got)
			}
			want := text
			if r := []rune(text); len(r) > snippetRunes {
				want = string(r[:snippetRunes])
			}
			if got != strings.TrimSpace(want) {
				t.Fatalf("snippet = %q, want %q", got, want)
			}
		})
	}
}

// Text Postgres would refuse at the INSERT is refused as a validation problem
// naming the property, wherever in the value it sits: a NUL in a text
// property, in a list item, in an object field. Before, the row failed at the
// INSERT and the caller read "internal error" for its own input (#540).
func TestCoercePropsRefusesTextNoRowStores(t *testing.T) {
	ty := &vocabulary.Kind{
		Identity:  "a.example.com/p/k",
		PropOrder: []string{"text", "tags", "meta"},
		Props: map[string]*vocabulary.Property{
			"text": {Name: "text", Datatype: vocabulary.DatatypeText},
			"tags": {Name: "tags", Datatype: vocabulary.DatatypeString, Repeated: true},
			"meta": {Name: "meta", Datatype: vocabulary.DatatypeJSON},
		},
	}
	for name, tc := range map[string]struct {
		in   map[string]any
		want string
	}{
		"a NUL in a text":         {map[string]any{"text": "a\x00b"}, "props.text: carries a NUL (U+0000)"},
		"a NUL in a list item":    {map[string]any{"tags": []any{"ok", "b\x00"}}, "props.tags: [1] carries a NUL"},
		"a NUL in an object leaf": {map[string]any{"meta": map[string]any{"note": "x\x00"}}, "props.meta: note carries a NUL"},
		"invalid UTF-8":           {map[string]any{"text": "a\xe2\x80"}, "props.text: is not valid UTF-8"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := coerceProps(ty, tc.in)
			var ve *substrate.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want a validation error", err)
			}
			if len(ve.Problems) != 1 || !strings.Contains(ve.Problems[0], tc.want) {
				t.Fatalf("problems = %q, want one containing %q", ve.Problems, tc.want)
			}
		})
	}
	if _, err := coerceProps(ty, map[string]any{"text": "…fine…", "tags": []any{"ü"}, "meta": map[string]any{"k": "v"}}); err != nil {
		t.Fatalf("ordinary multibyte text is refused: %v", err)
	}
}
