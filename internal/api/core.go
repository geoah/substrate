package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
)

// Nothing in core has endpoint-shaped collection behavior any more: the
// connector collection and its POST install shim went at the v1 freeze (ticket
// 004, ruling A12) and the repositories collection went with the control plane
// (B1), so every core collection is an ordinary resource and the sole install
// path is the schema-apply batch.

// The token endpoints moved OUT of the versioned resource tree and out of
// this file: `/register`, `/login`, `/tokens` sit beside `/api/…`
//  and live in auth_endpoints.go. With them went the whole
// least-privilege apparatus — scopes, the actor delegation check, the
// narrowing rules — because a token now has full access to its repository
// and nothing else. The repository-management endpoints went with
// the control plane in B1.

type mergeInput struct {
	// Kind is the merged records' kind reference: identity is the (kind, id)
	// pair, so a merge names the kind beside the two ids.
	Kind   string `json:"kind"`
	Winner string `json:"winner"`
	Loser  string `json:"loser"`
}

func (h *handler) postMerges(w http.ResponseWriter, r *http.Request) {
	var req mergeInput
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusUnprocessableEntity, codeValidation,
			"kind is required — a merge addresses two records of one kind by (kind, id)")
		return
	}
	ctx := r.Context()
	ent, err := DatasetFrom(ctx).Merge(ctx, ActorFrom(ctx), req.Kind, req.Winner, req.Loser)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ent)
}

type splitRequest struct {
	Merge string `json:"merge"`
}

func (h *handler) postSplits(w http.ResponseWriter, r *http.Request) {
	var req splitRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	ent, err := DatasetFrom(ctx).Split(ctx, ActorFrom(ctx), req.Merge)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ent)
}

// maxRequestBody caps every JSON request body, authenticated or not.
const maxRequestBody = 1 << 20

func decodeBody(r *http.Request, v any) error {
	return decodeJSONStrict(http.MaxBytesReader(nil, r.Body, maxRequestBody), v)
}

// decodeJSONStrict decodes exactly one JSON value into v with unknown fields
// REFUSED, then requires the stream to end. A misspelled top-level
// key — a dropped `ifVersion` CAS precondition, a broadened `filter` — is a
// `bad_request` NAMING the offending key, never a silent drop. Openness stays
// only inside the map-valued fields (`properties`, `labels`, `annotations`,
// the filter's `properties`, an object property, a `json`-typed property),
// whose dynamic keys a struct decoder never inspects.
//
// The decode runs in two passes because encoding/json matches struct fields
// CASE-INSENSITIVELY, so `ifversion` would quietly bind to `ifVersion` — the
// freeze wants exact casing. The first pass walks the top-level object's keys
// against v's exact json tags (also refusing a duplicate key), the second is
// the ordinary strict struct decode.
func decodeJSONStrict(r io.Reader, v any) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err // MaxBytesReader surfaces "http: request body too large" here
	}
	return decodeStrictBytes(raw, v, false)
}

// decodeStrictBytes is the shared strict decode: exact-key check, unknown-field
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
func decodeStrictBytes(raw []byte, v any, useNumber bool) error {
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
// anonymous embedded structs (edgeBody embeds EdgeRef, whose authority/type/id
// flatten to the top level).
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
