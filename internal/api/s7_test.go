package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// listWithFilter builds a list URL carrying a raw filter document, so a test
// can send a deliberately malformed one.
func listWithFilter(filter string) string {
	return recordsPath + "?" + url.Values{"filter": {filter}}.Encode()
}

// A misspelled / miscased top-level write key must ERROR naming the key, never
// silently drop — the whole point of A7's strict decode: a dropped `ifVersion`
// would turn a guarded write into an unconditional one.
func TestStrictDecodeNamesUnknownBodyKey(t *testing.T) {
	// The stub resolves the kind and has no Patch or Put: the decode is
	// refused before either ever runs, or the answer would be a 500.
	env := newStubEnv(t, newStubKinds(personKind))
	tok := stubToken

	// `ifversion` (lowercase) is not the `ifVersion` field — strict decode
	// refuses it rather than dropping the CAS precondition.
	rec := env.do(t, http.MethodPatch, peoplePath+"/p1", tok, map[string]any{
		"properties": map[string]any{"name": "Ada"},
		"ifversion":  3,
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if body := rec.Body.String(); !strings.Contains(body, "ifversion") {
		t.Fatalf("error must name the offending key, got %s", body)
	}

	// A create body carries the same guarantee.
	rec = env.do(t, http.MethodPost, recordsPath, tok, map[string]any{
		"kind":       personKind,
		"properties": map[string]any{"name": "Ada"},
		"typo":       true,
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if body := rec.Body.String(); !strings.Contains(body, "typo") {
		t.Fatalf("error must name the offending key, got %s", body)
	}
}

// The patch body is strict the same way: a near-miss key is named, never
// dropped, because a dropped `properties` writes nothing and answers 200.
func TestStrictDecodeNamesUnknownPatchKey(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	tok := stubToken
	rec := env.do(t, http.MethodPatch, peoplePath+"/p1", tok, map[string]any{
		"props": map[string]any{"name": "Ada"}, // it is `properties`, not `props`
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if body := rec.Body.String(); !strings.Contains(body, "props") {
		t.Fatalf("error must name the offending key, got %s", body)
	}
}

// A misspelled filter key must ERROR — a dropped predicate could broaden a
// destructive query.
func TestStrictDecodeNamesUnknownFilterKey(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	tok := stubToken
	rec := env.do(t, http.MethodGet, listWithFilter(`{"nope":1}`), tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if body := rec.Body.String(); !strings.Contains(body, "nope") {
		t.Fatalf("error must name the offending filter key, got %s", body)
	}
}

// s7Recorder resolves the person kind and records what the handler decoded
// for a create and a list: the decoder's openness is the claim, and only the
// handed input shows it, since the engine refuses a property the kind does
// not declare.
type s7Recorder struct {
	stubKinds

	mu    sync.Mutex
	put   substrate.PutInput
	query substrate.Query
}

func (d *s7Recorder) Put(_ context.Context, _ substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.put = in
	return &substrate.Record{ID: "r1", Kind: in.Kind, Version: 1, Created: true, Properties: in.Properties}, nil
}

func (d *s7Recorder) List(_ context.Context, q substrate.Query) (*substrate.Page, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.query = q
	return &substrate.Page{Records: []*substrate.Record{}}, nil
}

func (d *s7Recorder) Head(context.Context) (substrate.ChangelogHead, error) {
	return substrate.ChangelogHead{Seq: 1, Generation: "stub-generation"}, nil
}

// Openness stays INSIDE the map-valued fields: arbitrary property names are
// authored content, never unknown struct fields, so a create with a property
// the decoder has never heard of reaches the write with it.
func TestStrictDecodeKeepsPropertiesOpen(t *testing.T) {
	ds := &s7Recorder{stubKinds: newStubKinds(personKind)}
	env := newStubEnv(t, ds)
	rec := env.do(t, http.MethodPost, recordsPath, stubToken, map[string]any{
		"kind":       personKind,
		"properties": map[string]any{"name": "Ada", "anythingGoesHere": 42},
	})
	wantStatus(t, rec, http.StatusCreated)
	ds.mu.Lock()
	got := ds.put.Properties["anythingGoesHere"]
	ds.mu.Unlock()
	if fmt.Sprint(got) != "42" {
		t.Fatalf("the write was handed anythingGoesHere = %#v, want 42", got)
	}
	// And a well-formed filter still parses.
	rec = env.do(t, http.MethodGet, listWithFilter(`{"properties":{"name":{"eq":"Ada"}}}`), stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	ds.mu.Lock()
	props := ds.query.Filter.Properties
	ds.mu.Unlock()
	if _, ok := props["name"]; !ok {
		t.Fatalf("the list was handed filter properties %+v, want the name predicate", props)
	}
}
