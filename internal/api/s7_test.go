package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// listWithFilter builds a people-collection list URL carrying a raw filter
// document, so a test can send a deliberately malformed one.
func listWithFilter(filter string) string {
	return peoplePath + "?" + url.Values{"filter": {filter}}.Encode()
}

// A misspelled / miscased top-level write key must ERROR naming the key, never
// silently drop — the whole point of A7's strict decode: a dropped `ifVersion`
// would turn a guarded write into an unconditional one.
func TestStrictDecodeNamesUnknownBodyKey(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	// Seed p1 so a strict-decode failure is what returns 400, not a missing
	// resource: the decode is refused before the Patch ever runs.
	env.svc.datasets["geoah"].records["p1"] = &substrate.Record{
		ID: "p1", Kind: "people.substrate.reamde.dev/person", Version: 1,
		Properties: map[string]any{"name": "Ada"},
	}

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
	rec = env.do(t, http.MethodPost, peoplePath, tok, map[string]any{
		"properties": map[string]any{"name": "Ada"},
		"typo":       true,
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if body := rec.Body.String(); !strings.Contains(body, "typo") {
		t.Fatalf("error must name the offending key, got %s", body)
	}
}

// The edge-verb body S6 left non-strict is strict now too.
func TestStrictDecodeNamesUnknownEdgeKey(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, peoplePath+"/p1/-/edges/member_of", tok, map[string]any{
		"id":    "org1",
		"props": map[string]any{"role": "admin"}, // it is `properties`, not `props`
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if body := rec.Body.String(); !strings.Contains(body, "props") {
		t.Fatalf("error must name the offending key, got %s", body)
	}
}

// A misspelled filter key must ERROR — a dropped predicate could broaden a
// destructive query.
func TestStrictDecodeNamesUnknownFilterKey(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, listWithFilter(`{"nope":1}`), tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if body := rec.Body.String(); !strings.Contains(body, "nope") {
		t.Fatalf("error must name the offending filter key, got %s", body)
	}
}

// Openness stays INSIDE the map-valued fields: arbitrary property names are
// authored content, never unknown struct fields, so a create with a property
// the decoder has never heard of still lands.
func TestStrictDecodeKeepsPropertiesOpen(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, peoplePath, tok, map[string]any{
		"properties": map[string]any{"name": "Ada", "anythingGoesHere": 42},
	})
	wantStatus(t, rec, http.StatusCreated)
	// And a well-formed filter still parses.
	rec = env.do(t, http.MethodGet, listWithFilter(`{"properties":{"name":{"eq":"Ada"}}}`), tok, nil)
	wantStatus(t, rec, http.StatusOK)
}

// An expired token authenticates as a spent credential: `auth`, not
// `forbidden`. A future expiry is fine.
func TestExpiredTokenRejected(t *testing.T) {
	env := newTestEnv(t)
	// The test clock sits at 2023-11-14 (api_test.go).
	past := time.Unix(1_600_000_000, 0).UTC()   // 2020
	future := time.Unix(1_800_000_000, 0).UTC() // 2027

	expired := env.svc.tokenWith("geoah", func(i *substrate.TokenInfo) { i.ExpiresAt = &past })
	rec := env.do(t, http.MethodGet, peoplePath, expired, nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)

	live := env.svc.tokenWith("geoah", func(i *substrate.TokenInfo) { i.ExpiresAt = &future })
	rec = env.do(t, http.MethodGet, peoplePath, live, nil)
	wantStatus(t, rec, http.StatusOK)
}
