package api

// Codex regression round 2 (server, API surface):
//   #1  GraphQL mutations enforce token scopes; bundle lifecycle is gated
//   #4  a successful uninstall acks with a tombstone, never an error
//   #9  GraphQL JSON inputs decode strictly (a miscased ifVersion errors)
//   #15 a fractional / out-of-range Long variable errors, never truncates
//   #16 a malformed trailing closer {}} is bad_request

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// ---- bundle-lifecycle fake (#1 gate, #4 ack) --------------------------------

type bundleDataset struct {
	*fakeDataset
	authority   string
	statusErr   error
	uninstalled bool
	// boundInput/boundRecord record the last bind call, for the handler test.
	boundInput  string
	boundRecord string
}

func (d *bundleDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return nil, nil
}

func (d *bundleDataset) BundleStatus(_ context.Context, id string) (substrate.BundleStatus, error) {
	if d.statusErr != nil {
		return substrate.BundleStatus{}, d.statusErr
	}
	return substrate.BundleStatus{ID: id, Authority: d.authority, Installed: true, Enabled: true}, nil
}

func (d *bundleDataset) BundleAuthority(context.Context, string) (string, error) {
	return d.authority, nil
}
func (d *bundleDataset) DisableBundle(context.Context, string) error { return nil }
func (d *bundleDataset) BindBundleInput(_ context.Context, _, input, record string) error {
	d.boundInput, d.boundRecord = input, record
	return nil
}
func (d *bundleDataset) EnableBundle(context.Context, string) error { return nil }
func (d *bundleDataset) UninstallBundle(context.Context, string) error {
	d.uninstalled = true
	return nil
}
func (d *bundleDataset) PurgeBundle(context.Context, string) (int, error) { return 3, nil }
func (d *bundleDataset) StartOAuth(context.Context, substrate.Actor, string) (string, error) {
	return "", nil
}

func (d *bundleDataset) TypesImplementing(context.Context, string) ([]substrate.KindInfo, error) {
	return nil, nil
}

var _ substrate.BundleOps = (*bundleDataset)(nil)

type bundleService struct {
	*fakeService
	ds *bundleDataset
}

func (s *bundleService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	_, info, err := s.fakeService.Authenticate(ctx, secret)
	if err != nil {
		return nil, info, err
	}
	return s.ds, info, nil
}

func newBundleEnv(t *testing.T) (*testEnv, *bundleDataset) {
	t.Helper()
	fs := newFakeService()
	bd := &bundleDataset{fakeDataset: fs.datasets["geoah"], authority: "widgets.bundles.substrate.reamde.dev"}
	svc := &bundleService{fakeService: fs, ds: bd}
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	return &testEnv{svc: fs, h: New(Config{Service: svc, Now: clock.now}), clock: clock}, bd
}

const uninstallPath = "/api/v1/core.substrate.reamde.dev/bundle/widgets.bundles.substrate.reamde.dev/-/uninstall"

const bindPath = "/api/v1/core.substrate.reamde.dev/bundle/widgets.bundles.substrate.reamde.dev/-/bind"

// TestBundleBindValidatesAndAnswersStatus drives the bind endpoint: a bind
// reaches the engine with its input and record and answers the refreshed
// status; an empty record is the unbind spelling; a missing input is a 400
// before the engine is touched.
func TestBundleBindValidatesAndAnswersStatus(t *testing.T) {
	env, bd := newBundleEnv(t)
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodPost, bindPath, tok, map[string]any{"input": "client", "record": "cfg-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("bind status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if bd.boundInput != "client" || bd.boundRecord != "cfg-1" {
		t.Fatalf("bind reached the engine as (%q, %q)", bd.boundInput, bd.boundRecord)
	}
	var st substrate.BundleStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || !st.Installed {
		t.Fatalf("bind answers the refreshed status: %v %s", err, rec.Body.String())
	}

	rec = env.do(t, http.MethodPost, bindPath, tok, map[string]any{"input": "client", "record": ""})
	if rec.Code != http.StatusOK || bd.boundRecord != "" {
		t.Fatalf("unbind: %d (record %q)", rec.Code, bd.boundRecord)
	}

	bd.boundInput = ""
	rec = env.do(t, http.MethodPost, bindPath, tok, map[string]any{"record": "cfg-1"})
	if rec.Code != http.StatusBadRequest || bd.boundInput != "" {
		t.Fatalf("empty input must 400 before the engine: %d (%q)", rec.Code, bd.boundInput)
	}
}

// TestBundleUninstallAcksTombstone pins codex regress #4: uninstall deletes the
// bundle row, so reloading its status afterward always fails — the handler must
// NOT reload it. Even with a status read that errors (the deleted row), a
// successful uninstall answers 200 {"uninstalled": true}.
func TestBundleUninstallAcksTombstone(t *testing.T) {
	env, bd := newBundleEnv(t)
	bd.statusErr = substrate.ErrNotFound // the row is gone once uninstall ran
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodPost, uninstallPath, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[map[string]any](t, rec)
	if out["uninstalled"] != true {
		t.Fatalf("uninstall body = %v, want {\"uninstalled\": true}", out)
	}
	if !bd.uninstalled {
		t.Fatal("uninstall verb never ran")
	}
}

// ---- GraphQL scope + strict decode + Long (#1 read/write, #9, #15) ----------

// gqlRaw posts a GraphQL request and returns the parsed response WITHOUT
// failing on GraphQL errors (the gql helper fails on them).
func (e *testEnv) gqlRaw(t *testing.T, token, query string, vars map[string]any) gqlResponse {
	t.Helper()
	rec := e.do(t, http.MethodPost, graphqlPath, token, map[string]any{"query": query, "variables": vars})
	wantStatus(t, rec, http.StatusOK)
	return decodeJSON[gqlResponse](t, rec)
}

// TestGraphQLInputStrictDecodeMiscasedIfVersion pins codex regress #9: GraphQL
// mutation inputs go through the strict decoder, so a miscased `ifversion` key
// errors rather than silently dropping the CAS precondition.
func TestGraphQLInputStrictDecodeMiscasedIfVersion(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	res := env.gqlRaw(t, tok,
		`mutation ($in: JSON!) { patch(kind: "people.substrate.reamde.dev/person", id: "x", input: $in) { id } }`,
		map[string]any{"in": map[string]any{"ifversion": 3}})
	if len(res.Errors) == 0 {
		t.Fatal("a miscased ifversion silently decoded — the strict decoder is not on the GraphQL path")
	}
	if !strings.Contains(res.Errors[0].Message, "ifversion") {
		t.Fatalf("error = %q, want it to name the unknown field ifversion", res.Errors[0].Message)
	}
}

// TestGraphQLLongVariableRejectsFractional pins codex regress #15: a fractional
// Long variable errors (UseNumber + coerceLong), never truncates.
func TestGraphQLLongVariableRejectsFractional(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	res := env.gqlRaw(t, tok, `query ($from: Long) { changelog(from: $from) { from } }`,
		map[string]any{"from": 1.5})
	if len(res.Errors) == 0 {
		t.Fatal("a fractional Long variable was accepted — it truncated instead of erroring")
	}
}

// TestGraphQLLongVariableRejectsOutOfRange pins the other half of #15: a value
// past 2^63 errors rather than wrapping.
func TestGraphQLLongVariableRejectsOutOfRange(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	// 2^63 as a JSON number literal — one past the int64 ceiling.
	res := env.gqlRaw(t, tok, `query ($from: Long) { changelog(from: $from) { from } }`,
		map[string]any{"from": float64(9223372036854775808.0)})
	if len(res.Errors) == 0 {
		t.Fatal("an out-of-range Long variable was accepted — it wrapped instead of erroring")
	}
}

// TestGraphQLIntVariableIsUsable is the other side of #15: UseNumber must not
// make ordinary `Int` variables unusable. graphql-go coerces by type switch and
// has no json.Number case, so `{"first": 5}` used to fail variable coercion
// outright ("Variable \"$first\" got invalid value 5") and every client was
// pushed into inlining its page sizes.
func TestGraphQLIntVariableIsUsable(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	res := env.gqlRaw(t, tok,
		`query ($first: Int) { records(first: $first) { nodes { id } } }`,
		map[string]any{"first": 5})
	if len(res.Errors) > 0 {
		t.Fatalf("an Int variable was refused: %v", res.Errors)
	}
	// The value ARRIVES: the fake records the query it was listed with.
	if got := env.svc.datasets["geoah"].lastQuery.First; got != 5 {
		t.Fatalf("first reached the dataset as %d, want 5", got)
	}
	// And a Long variable travels the same path in the same request shape.
	res = env.gqlRaw(t, tok, `query ($from: Long) { changelog(from: $from) { from } }`,
		map[string]any{"from": 9007199254740993})
	if len(res.Errors) > 0 {
		t.Fatalf("a Long variable past 2^53 was refused: %v", res.Errors)
	}
}

// ---- scoped changefeed refill (#13) -----------------------------------------

// ---- strict REST decode trailing closer (#16) -------------------------------

// TestStrictDecodeRejectsTrailingCloser pins codex regress #16: a body like
// {}} must be bad_request. Decoder.More() missed it; a required io.EOF second
// decode catches the stray closer.
func TestStrictDecodeRejectsTrailingCloser(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/people.substrate.reamde.dev/person", strings.NewReader("{}}"))
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}
