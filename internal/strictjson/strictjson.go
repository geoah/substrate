// Package strictjson is the ONE strict JSON decode both wire surfaces share:
// exact-key check, unknown-field refusal, and an end-of-stream check. The API
// body decoder and the GraphQL JSON-scalar remarshal (REST's regress #9 twin)
// hold inputs to the same rules, so a typo'd `ifversion` can never quietly
// disable CAS on either path.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

// DecodeBytes is the shared strict decode: exact-key check, unknown-field
// refusal, and an END-OF-STREAM check. `useNumber` decodes JSON numbers as
// json.Number rather than float64 — the GraphQL variable path needs it so a
// fractional or out-of-range Long input is rejected, not truncated (codex
// regress #15). Every other caller decodes without it, unchanged.
//
// The end-of-stream check requires a SECOND decode to return io.EOF rather than
// trusting Decoder.More() (codex regress #16): More() reports whether another
// ARRAY/OBJECT element follows, so a malformed trailing closer like `{}}`
// slipped past it — a second Decode surfaces the stray `}` as a syntax error
// instead.
func DecodeBytes(raw []byte, v any, useNumber bool) error {
	if err := exactKeyCheck(raw, v); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if useNumber {
		dec.UseNumber()
	}
	if err := dec.Decode(v); err != nil {
		return cleanDecodeError(err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing data after the JSON value")
	}
	return nil
}

// Keys is the exact set of top-level json keys this decoder accepts for v,
// sorted: the same set exactKeyCheck holds an input to. A refusal that names
// them is the only grammar an opaque JSON argument has, because on the GraphQL side
// `filter` is a JSON scalar, so introspection shows a caller nothing and a
// bare `unknown field "at"` reads as "the server cannot do this" rather than
// "you spelled it wrong".
func Keys(v any) []string {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	out := make([]string, 0, t.NumField())
	for k := range allowedKeys(t) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// exactKeyCheck refuses a top-level key that is not an EXACT-case json tag of
// v, and refuses a duplicate key — the two silent drops DisallowUnknownFields
// alone cannot catch (a miscased key binds case-insensitively; a duplicate is
// last-wins). It walks only the top level: nested openness (a Cond operator,
// an authored property) is intentional, and the struct decoder still refuses a
// genuinely unknown nested key.
func exactKeyCheck(raw []byte, v any) error {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		return checkObjectKeys(raw, allowedKeys(t))
	case reflect.Slice, reflect.Array:
		elem := t.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			return nil
		}
		return checkArrayKeys(raw, allowedKeys(elem))
	default:
		return nil
	}
}

// allowedKeys is the exact set of json object keys a struct accepts, following
// anonymous embedded structs, whose fields flatten to the top level.
func allowedKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := range t.NumField() {
		f := t.Field(i)
		if f.PkgPath != "" && !f.Anonymous { // unexported
			continue
		}
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k := range allowedKeys(ft) {
					keys[k] = true
				}
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		keys[name] = true
	}
	return keys
}

// checkObjectKeys token-walks a JSON object's top-level keys, refusing an
// unknown or duplicate one. A non-object (or malformed) body is left for the
// struct decoder to report.
func checkObjectKeys(raw []byte, allowed map[string]bool) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil
	}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := kt.(string)
		if !ok {
			return nil
		}
		if !allowed[key] {
			return fmt.Errorf("unknown field %q", key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return nil
}

// cleanDecodeError trims the encoding/json prefix so the wire message reads
// `unknown field "ifversion"` — naming the key the caller must fix — rather
// than the raw `json: …`. Everything else passes through unchanged.
func cleanDecodeError(err error) error {
	if err == nil {
		return nil
	}
	if msg, ok := strings.CutPrefix(err.Error(), "json: "); ok {
		return errors.New(msg)
	}
	return err
}

// checkArrayKeys applies the object check to every element of a JSON array
// (the orderBy list of Order objects).
func checkArrayKeys(raw []byte, allowed map[string]bool) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(trimmed, &elems); err != nil {
		return nil
	}
	for _, e := range elems {
		if err := checkObjectKeys(e, allowed); err != nil {
			return err
		}
	}
	return nil
}
