package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The app in these tests serves its document from memory: the real fetcher
// never dials a loopback address, which is every address a test has.
const (
	appClientID = "https://notes.example.org/client.json"
	appRedirect = "https://notes.example.org/callback"
	appVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-the-verifier"
	taskKind    = "samples.substrate.reamde.dev/tasks/task"
)

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type memDocs struct {
	mu    sync.Mutex
	docs  map[string]authorize.Document
	err   error
	calls int
}

func (m *memDocs) Fetch(_ context.Context, c authorize.Client) (authorize.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return authorize.Document{}, m.err
	}
	d, ok := m.docs[c.ID]
	if !ok {
		return authorize.Document{}, authorize.NewFetchError("the app's document could not be fetched: it answered 404, not 200")
	}
	return d, nil
}

// mintingDataset makes the fake's mint real enough for a flow: a minted token
// authenticates, and revoking it stops it authenticating.
type mintingDataset struct {
	*fakeDataset
	svc *fakeService
}

// failRevokes is how many token deletes fail before one lands, for the
// replay revocation's retry.
var failRevokes struct {
	sync.Mutex
	n int
}

func (d *mintingDataset) MintToken(_ context.Context, in substrate.MintTokenInput) (substrate.TokenInfo, string, error) {
	d.svc.mu.Lock()
	defer d.svc.mu.Unlock()
	n := len(d.svc.tokens) + 1
	secret := fmt.Sprintf("substrate_tok_app%d", n)
	info := substrate.TokenInfo{
		ID: fmt.Sprintf("app%d", n), Label: in.Label, Created: time.Unix(0, 0).UTC(),
		ExpiresAt: in.ExpiresAt, Scopes: in.Scopes, ClientID: in.ClientID, ClientName: in.ClientName,
	}
	d.svc.tokens[secret] = fakeToken{repository: d.repository.Authority, info: info}
	return info, secret, nil
}

func (d *mintingDataset) Delete(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.DeleteInput) (*substrate.Record, error) {
	if typ == tokenType {
		failRevokes.Lock()
		if failRevokes.n > 0 {
			failRevokes.n--
			failRevokes.Unlock()
			return nil, fmt.Errorf("%w: the database refused the write", substrate.ErrUnavailable)
		}
		failRevokes.Unlock()
		d.svc.mu.Lock()
		defer d.svc.mu.Unlock()
		for secret, t := range d.svc.tokens {
			if t.info.ID == id {
				delete(d.svc.tokens, secret)
				return &substrate.Record{ID: id, Kind: tokenType}, nil
			}
		}
		return nil, substrate.ErrNotFound
	}
	return d.fakeDataset.Delete(ctx, actor, typ, id, in)
}

type appEnv struct {
	t     *testing.T
	svc   *fakeService
	h     http.Handler
	docs  *memDocs
	owner string
}

func newAppEnv(t *testing.T) *appEnv {
	t.Helper()
	svc := newFakeService()
	svc.wrap = func(d *fakeDataset) substrate.Dataset { return &mintingDataset{fakeDataset: d, svc: svc} }
	docs := &memDocs{docs: map[string]authorize.Document{
		appClientID: {ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Notes", URI: "https://notes.example.org"},
	}}
	return &appEnv{
		t: t, svc: svc, docs: docs, owner: svc.token(fakeRepository),
		h: New(Config{Service: svc, ClientDocuments: docs, AuthInterval: time.Nanosecond}),
	}
}

func (e *appEnv) do(method, target, bearer string, body []byte, contentType string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Host = "substrate.example.com"
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func authorizeURL(params map[string]string) string {
	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	return "/oauth/authorize?" + v.Encode()
}

func appRequest(scopes string) map[string]string {
	return map[string]string{
		"response_type": "code", "client_id": appClientID, "redirect_uri": appRedirect,
		"code_challenge": challengeOf(appVerifier), "code_challenge_method": "S256",
		"scope": scopes, "state": "1.0",
	}
}

// park sends a request to the authorization endpoint and answers the parked
// request's id.
func (e *appEnv) park(params map[string]string) string {
	e.t.Helper()
	rec := e.do(http.MethodGet, authorizeURL(params), "", nil, "")
	if rec.Code != http.StatusSeeOther {
		e.t.Fatalf("authorize = %d %s, want 303", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	id, ok := strings.CutPrefix(loc, "/authorize/")
	if !ok || id == "" {
		e.t.Fatalf("authorize redirected to %q, want /authorize/{request}", loc)
	}
	return id
}

// show loads a parked request the way the approval page does, which is what
// an approval or a denial is later held to.
func (e *appEnv) show(id string) *httptest.ResponseRecorder {
	return e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
}

// approve answers a request the page has shown.
func (e *appEnv) approve(id string, scopes []string, days int) *httptest.ResponseRecorder {
	e.t.Helper()
	wantStatus(e.t, e.show(id), http.StatusOK)
	return e.approveShown(id, scopes, days)
}

func (e *appEnv) approveShown(id string, scopes []string, days int) *httptest.ResponseRecorder {
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: scopes, LifetimeDays: &days})
	return e.do(http.MethodPost, "/api/v1/authorizations/"+id, e.owner, body, "application/json")
}

func (e *appEnv) exchange(form url.Values) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/oauth/token", "", []byte(form.Encode()), "application/x-www-form-urlencoded")
}

func codeFrom(t *testing.T, redirect string) string {
	t.Helper()
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatalf("redirect %q carries no code", redirect)
	}
	return code
}

func tokenForm(code string) url.Values {
	return url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {appClientID},
		"redirect_uri": {appRedirect}, "code_verifier": {appVerifier},
	}
}

// grant runs the whole flow and answers the app's token.
func (e *appEnv) grant(scopes string, approved []string) string {
	e.t.Helper()
	id := e.park(appRequest(scopes))
	rec := e.approve(id, approved, 30)
	wantStatus(e.t, rec, http.StatusOK)
	code := codeFrom(e.t, decodeJSON[substrate.AuthorizationAnswer](e.t, rec).Redirect)
	tok := e.exchange(tokenForm(code))
	wantStatus(e.t, tok, http.StatusOK)
	return decodeJSON[tokenResponse](e.t, tok).AccessToken
}

func TestAnAppGetsATokenHeldToTheScopesTheOwnerApproved(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind + " write:" + taskKind + " read:~/notes/*"))

	view := e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
	wantStatus(t, view, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, view)
	if got.App.Host != "notes.example.org" || got.App.Name != "Notes" || got.App.Local ||
		got.App.Actor != "app:notes.example.org" {
		t.Fatalf("app = %+v", got.App)
	}
	if got.Repository != fakeRepository || got.DefaultLifetimeDays != 30 {
		t.Fatalf("repository %q default %d", got.Repository, got.DefaultLifetimeDays)
	}
	if len(got.Scopes) != 3 || got.Scopes[2].Scope != "read:"+fakeRepository+"/notes/*" ||
		got.Scopes[2].Requested != "read:~/notes/*" || !got.Scopes[2].Glob {
		t.Fatalf("scopes = %+v", got.Scopes)
	}
	if len(got.Scopes[0].Kinds) != 1 || got.Scopes[0].Kinds[0].Identity != personKind {
		t.Fatalf("read scope reaches %+v, want the person kind", got.Scopes[0].Kinds)
	}

	// The owner approves two of the three.
	rec := e.approveShown(id, []string{"read:" + personKind, "write:" + taskKind}, 30)
	wantStatus(t, rec, http.StatusOK)
	redirect := decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect
	u, _ := url.Parse(redirect)
	if !strings.HasPrefix(redirect, appRedirect+"?") || u.Query().Get("state") != "1.0" ||
		u.Query().Get("iss") != "https://substrate.example.com" {
		t.Fatalf("redirect = %q: want the app's redirect_uri with code, state verbatim and iss", redirect)
	}

	tok := e.exchange(tokenForm(codeFrom(t, redirect)))
	wantStatus(t, tok, http.StatusOK)
	if cc := tok.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("token answer Cache-Control = %q, want no-store", cc)
	}
	res := decodeJSON[tokenResponse](t, tok)
	if res.TokenType != "Bearer" || res.Repository != fakeRepository ||
		res.Scope != "read:"+personKind+" write:"+taskKind || res.ExpiresIn == nil || *res.ExpiresIn != 30*24*3600 {
		t.Fatalf("token response = %+v", res)
	}
	minted := e.svc.tokens[res.AccessToken].info
	if minted.ClientID != appClientID || minted.ClientName != "Notes" || minted.Label != "notes.example.org" ||
		len(minted.Scopes) != 2 || minted.ExpiresAt == nil {
		t.Fatalf("minted token = %+v", minted)
	}

	// The scoped surface: what the scopes reach and nothing else.
	app := res.AccessToken
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"kinds":["`+personKind+`"]}`), app, nil, ""), http.StatusOK)
	wantStatus(t, e.do(http.MethodPut, "/api/v1/"+taskKind+"/t1", app, []byte(`{"properties":{"note":"x"}}`), "application/json"), http.StatusCreated)
	put := e.do(http.MethodPut, "/api/v1/"+personKind+"/p1", app, []byte(`{"properties":{"name":"x"}}`), "application/json")
	wantStatus(t, put, http.StatusForbidden)
	if wa := put.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
		t.Fatalf("scope refusal WWW-Authenticate = %q", wa)
	}
	// A kind the grant cannot read is answered as if it did not exist.
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"kinds":["samples.substrate.reamde.dev/messaging/conversationmessage"]}`), app, nil, ""), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/token/tok1", app, nil, ""), http.StatusNotFound)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/tokens"},
		{http.MethodPost, "/tokens"},
		{http.MethodGet, "/api/v1/export"},
		{http.MethodPost, "/api/v1/vocabulary/apply"},
		{http.MethodGet, "/api/v1/changes"},
		{http.MethodGet, "/api/v1/catalog"},
		{http.MethodPut, "/api/v1/blobs"},
		{http.MethodPost, "/api/v1/merge"},
		{http.MethodGet, "/api/v1/authorizations/" + id},
		{http.MethodPost, "/api/v1/substrate.reamde.dev/core/function/x/call"},
		{http.MethodGet, "/api/v1/sync/status"},
	} {
		rec := e.do(c.method, c.path, app, []byte(`{}`), "application/json")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s with an app's token = %d %s, want 403", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	// The tail is the owner's: a watch is refused before it streams.
	watch := e.do(http.MethodGet, "/api/v1/records?watch=1", app, nil, "")
	wantStatus(t, watch, http.StatusForbidden)
	// So are the window and aggregate reads.
	window := `{"kinds":["` + taskKind + `"],"properties":{"at":{"gte":"2026-01-01T00:00:00Z","lt":"2026-02-01T00:00:00Z"}}}`
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(window), app, nil, ""), http.StatusForbidden)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?aggregate="+url.QueryEscape(`{"op":"count"}`), app, nil, ""), http.StatusForbidden)
}

func TestAListWithNoKindsNamesEveryReadableKindAndNoOther(t *testing.T) {
	e := newAppEnv(t)
	app := e.grant("read:samples.substrate.reamde.dev/*", []string{"read:samples.substrate.reamde.dev/*"})
	rec := e.do(http.MethodGet, "/api/v1/records", app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	got := e.svc.datasets[fakeRepository].lastQuery.Filter.Kinds
	want := []string{"samples.substrate.reamde.dev/messaging/conversationmessage", personKind, taskKind}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("list narrowed to %v, want %v", got, want)
	}
	if page := decodeJSON[substrate.Page](t, rec); page.Head != 0 || page.Generation != "" {
		t.Fatalf("an app's page carries head %d generation %q, want neither", page.Head, page.Generation)
	}
}

func TestAnAppsTokenWritesAsItsOwnActorWhateverActorItNames(t *testing.T) {
	e := newAppEnv(t)
	app := e.grant("write:"+taskKind, []string{"write:" + taskKind})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/"+taskKind+"/t1", strings.NewReader(`{"properties":{}}`))
	req.Header.Set("Authorization", "Bearer "+app)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorHeader, "console")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusCreated)
	if got := e.svc.datasets[fakeRepository].lastActor; got != "app:notes.example.org" {
		t.Fatalf("an app's write was attributed to %q, want app:notes.example.org", got)
	}
}

func TestAnAppsTokenWithADamagedGrantDoesNotAuthenticate(t *testing.T) {
	e := newAppEnv(t)
	bad := e.svc.tokenWith(fakeRepository, func(i *substrate.TokenInfo) { i.Scopes = []string{"everything"} })
	rec := e.do(http.MethodGet, "/api/v1/records", bad, nil, "")
	wantStatus(t, rec, http.StatusUnauthorized)
	if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="invalid_token"`) ||
		!strings.Contains(wa, `resource_metadata="https://substrate.example.com/.well-known/oauth-protected-resource"`) {
		t.Fatalf("401 WWW-Authenticate = %q", wa)
	}
}

func TestARequestWithoutATokenLearnsWhereTheMetadataIs(t *testing.T) {
	e := newAppEnv(t)
	rec := e.do(http.MethodGet, "/api/v1/records", "", nil, "")
	wantStatus(t, rec, http.StatusUnauthorized)
	if wa := rec.Header().Get("WWW-Authenticate"); wa != `Bearer resource_metadata="https://substrate.example.com/.well-known/oauth-protected-resource"` {
		t.Fatalf("WWW-Authenticate = %q", wa)
	}
}

func TestAConfiguredPublicURLIsTheIssuerWhateverTheHost(t *testing.T) {
	svc := newFakeService()
	h := New(Config{Service: svc, ClientDocuments: &memDocs{}, PublicURL: "https://substrate.example.net/"})
	req := httptest.NewRequest(http.MethodGet, asMetadataPath, nil)
	req.Host = "10.0.0.7:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if md := decodeJSON[authorizationServerMetadata](t, rec); md.Issuer != "https://substrate.example.net" {
		t.Fatalf("issuer = %q, want the configured public URL", md.Issuer)
	}
}

func TestTheIssuerIsHTTPOnlyOnALoopbackHost(t *testing.T) {
	h := &handler{}
	for host, want := range map[string]string{
		"127.0.0.1:8080":        "http://127.0.0.1:8080",
		"[::1]:8080":            "http://[::1]:8080",
		"localhost:5173":        "http://localhost:5173",
		"10.0.0.7:8080":         "https://10.0.0.7:8080",
		"203.0.113.9":           "https://203.0.113.9",
		"substrate.example.com": "https://substrate.example.com",
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		if got := h.issuerOf(req); got != want {
			t.Fatalf("issuer for Host %q = %q, want %q", host, got, want)
		}
	}
}

func TestTheMetadataDocumentsDescribeTheProfile(t *testing.T) {
	e := newAppEnv(t)
	as := e.do(http.MethodGet, asMetadataPath, "", nil, "")
	wantStatus(t, as, http.StatusOK)
	md := decodeJSON[authorizationServerMetadata](t, as)
	if md.Issuer != "https://substrate.example.com" || md.TokenEndpoint != md.Issuer+"/oauth/token" ||
		md.CodeChallengeMethodsSupported[0] != "S256" || md.TokenEndpointAuthMethodsSupported[0] != "none" ||
		!md.ClientIDMetadataDocumentSupported || !md.AuthorizationResponseISSSupported {
		t.Fatalf("authorization server metadata = %+v", md)
	}
	pr := decodeJSON[protectedResourceMetadata](t, e.do(http.MethodGet, prMetadataPath, "", nil, ""))
	if pr.Resource != md.Issuer || pr.AuthorizationServers[0] != md.Issuer {
		t.Fatalf("protected resource metadata = %+v", pr)
	}
}

func TestTheAuthorizationEndpointRefusesWithAPageNeverARedirect(t *testing.T) {
	e := newAppEnv(t)
	cases := map[string]func(map[string]string){
		"no PKCE":           func(p map[string]string) { delete(p, "code_challenge") },
		"plain PKCE":        func(p map[string]string) { p["code_challenge_method"] = "plain" },
		"implicit":          func(p map[string]string) { p["response_type"] = "token" },
		"http web app":      func(p map[string]string) { p["client_id"] = "http://notes.example.org/client.json" },
		"no path":           func(p map[string]string) { p["client_id"] = "https://notes.example.org" },
		"javascript":        func(p map[string]string) { p["redirect_uri"] = "javascript:alert(1)" },
		"http redirect":     func(p map[string]string) { p["redirect_uri"] = "http://notes.example.org/cb" },
		"fragment":          func(p map[string]string) { p["redirect_uri"] = appRedirect + "#x" },
		"no scope":          func(p map[string]string) { delete(p, "scope") },
		"bad scope":         func(p map[string]string) { p["scope"] = "everything" },
		"auth kind":         func(p map[string]string) { p["scope"] = "read:substrate.reamde.dev/core/token" },
		"write core":        func(p map[string]string) { p["scope"] = "write:substrate.reamde.dev/core/trigger" },
		"bare kind":         func(p map[string]string) { p["scope"] = "read:task" },
		"other resource":    func(p map[string]string) { p["resource"] = "https://elsewhere.example.com" },
		"local, web return": func(p map[string]string) { p["client_id"] = "http://127.0.0.1/app" },
		// A web app's access goes back to its own origin: a document on a
		// shared host could otherwise send it anywhere it listed.
		"other host": func(p map[string]string) { p["redirect_uri"] = "https://elsewhere.example.com/callback" },
		"other port": func(p map[string]string) { p["redirect_uri"] = "https://notes.example.org:8443/callback" },
		"sub-domain": func(p map[string]string) { p["redirect_uri"] = "https://app.notes.example.org/callback" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			p := appRequest("read:" + personKind)
			edit(p)
			rec := e.do(http.MethodGet, authorizeURL(p), "", nil, "")
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("= %d Location %q, want 400 and no redirect", rec.Code, rec.Header().Get("Location"))
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("refusal is %q, want a page", rec.Header().Get("Content-Type"))
			}
		})
	}
	// A repeated parameter is refused before anything reads it.
	rec := e.do(http.MethodGet, authorizeURL(appRequest("read:"+personKind))+"&client_id=https://evil.example.org/c", "", nil, "")
	wantStatus(t, rec, http.StatusBadRequest)
}

func TestAnApprovalIsHeldToTheRequest(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	// A request the page never showed cannot be answered.
	wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 30), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/authorizations/"+id, e.owner, nil, ""), http.StatusUnprocessableEntity)
	wantStatus(t, e.show(id), http.StatusOK)
	// Nothing approved is a denial, never an empty grant.
	wantStatus(t, e.approveShown(id, nil, 30), http.StatusUnprocessableEntity)
	wantStatus(t, e.approveShown(id, []string{}, 30), http.StatusUnprocessableEntity)
	// A scope the app did not ask for.
	wantStatus(t, e.approveShown(id, []string{"read:" + taskKind}, 30), http.StatusUnprocessableEntity)
	// A lifetime the page does not offer.
	wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 365), http.StatusUnprocessableEntity)
	// The request is still pending after every refusal, and answers once.
	wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 7), http.StatusOK)
	wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 7), http.StatusNotFound)
}

func TestARequestIsAnsweredOnlyByTheRepositoryThatClaimedIt(t *testing.T) {
	e := newAppEnv(t)
	other := "other.example.com"
	e.svc.addRepository(other)
	stranger := e.svc.token(other)
	id := e.park(appRequest("read:" + personKind))
	wantStatus(t, e.show(id), http.StatusOK)
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: []string{"read:" + personKind}, LifetimeDays: &days})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/authorizations/"+id, stranger, nil, ""), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/"+id, stranger, body, "application/json"), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/authorizations/"+id, stranger, nil, ""), http.StatusNotFound)
	wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 30), http.StatusOK)
}

func TestAnApprovalIsHeldToTheDocumentThePageShowed(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	wantStatus(t, e.show(id), http.StatusOK)
	// The app drops the redirect from its document after the page loaded:
	// the approval still answers what the owner read, and the snapshot is
	// what decides, not a refetch.
	e.docs.mu.Lock()
	e.docs.docs[appClientID] = authorize.Document{ClientID: appClientID, RedirectURIs: []string{"https://notes.example.org/elsewhere"}}
	calls := e.docs.calls
	e.docs.mu.Unlock()
	rec := e.approveShown(id, []string{"read:" + personKind}, 30)
	wantStatus(t, rec, http.StatusOK)
	if e.docs.calls != calls {
		t.Fatalf("the approval fetched the document again (%d fetches, want %d)", e.docs.calls, calls)
	}
}

func TestAnAppsTokenNeverApprovesAnAuthorization(t *testing.T) {
	e := newAppEnv(t)
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	id := e.park(appRequest("write:" + taskKind))
	wantStatus(t, e.show(id), http.StatusOK)
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: []string{"write:" + taskKind}, LifetimeDays: &days})
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/"+id, app, body, "application/json"), http.StatusForbidden)
}

func TestAnUnlistedRedirectOrAnUnfetchableDocumentIsRefusedToTheOwner(t *testing.T) {
	e := newAppEnv(t)
	p := appRequest("read:" + personKind)
	p["redirect_uri"] = "https://notes.example.org/other"
	id := e.park(p)
	rec := e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if !strings.Contains(rec.Body.String(), "does not list the redirect_uri") {
		t.Fatalf("refusal = %s", rec.Body.String())
	}
	wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 30), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/authorizations/"+id, e.owner, nil, ""), http.StatusUnprocessableEntity)

	p = appRequest("read:" + personKind)
	p["client_id"] = "https://unknown.example.org/client.json"
	p["redirect_uri"] = "https://unknown.example.org/callback"
	id = e.park(p)
	rec = e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if !strings.Contains(rec.Body.String(), "answered 404") {
		t.Fatalf("refusal = %s", rec.Body.String())
	}
}

func TestADenialRedirectsWithAccessDeniedOnce(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	wantStatus(t, e.show(id), http.StatusOK)
	rec := e.do(http.MethodDelete, "/api/v1/authorizations/"+id, e.owner, nil, "")
	wantStatus(t, rec, http.StatusOK)
	u, _ := url.Parse(decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)
	if q := u.Query(); q.Get("error") != "access_denied" || q.Get("state") != "1.0" || q.Get("iss") == "" || q.Get("code") != "" {
		t.Fatalf("denial redirect query = %v", q)
	}
	wantStatus(t, e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, ""), http.StatusNotFound)
}

func TestTheExchangeChecksEveryBinding(t *testing.T) {
	cases := map[string]func(url.Values){
		"verifier":     func(f url.Values) { f.Set("code_verifier", strings.Repeat("a", 43)) },
		"client_id":    func(f url.Values) { f.Set("client_id", "https://evil.example.org/client.json") },
		"redirect_uri": func(f url.Values) { f.Set("redirect_uri", "https://notes.example.org/other") },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAppEnv(t)
			id := e.park(appRequest("read:" + personKind))
			code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
			f := tokenForm(code)
			edit(f)
			rec := e.exchange(f)
			wantStatus(t, rec, http.StatusBadRequest)
			if got := decodeJSON[oauthError](t, rec).Error; got != "invalid_grant" {
				t.Fatalf("error = %q, want invalid_grant", got)
			}
			// A presentation that proves nothing spends nothing: the app
			// holding the verifier still gets its token.
			wantStatus(t, e.exchange(tokenForm(code)), http.StatusOK)
		})
	}
}

func TestACodeExchangedOnAnotherIssuerIsRefused(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm(code).Encode()))
	req.Host = "other.example.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusBadRequest)
}

func TestAReplayedCodeRevokesTheTokenOnlyWithTheVerifier(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
	first := e.exchange(tokenForm(code))
	wantStatus(t, first, http.StatusOK)
	app := decodeJSON[tokenResponse](t, first).AccessToken

	// Without the verifier, a replay is refused and revokes nothing.
	f := tokenForm(code)
	f.Set("code_verifier", strings.Repeat("b", 43))
	wantStatus(t, e.exchange(f), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", app, nil, ""), http.StatusOK)

	// With every binding, it is the code working twice: the token goes.
	wantStatus(t, e.exchange(tokenForm(code)), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", app, nil, ""), http.StatusUnauthorized)
}

func TestTheTokenEndpointReadsAFormAndNothingElse(t *testing.T) {
	e := newAppEnv(t)
	rec := e.do(http.MethodPost, "/oauth/token", "", []byte(`{"grant_type":"authorization_code"}`), "application/json")
	wantStatus(t, rec, http.StatusBadRequest)
	if got := decodeJSON[oauthError](t, rec).Error; got != "invalid_request" {
		t.Fatalf("error = %q", got)
	}
	rec = e.exchange(url.Values{"grant_type": {"password"}})
	if got := decodeJSON[oauthError](t, rec).Error; got != "unsupported_grant_type" {
		t.Fatalf("error = %q", got)
	}
	rec = e.do(http.MethodPost, "/oauth/token", "", []byte("grant_type=authorization_code&code=a&code=b"), "application/x-www-form-urlencoded")
	if got := decodeJSON[oauthError](t, rec).Error; got != "invalid_request" {
		t.Fatalf("repeated code: error = %q", got)
	}
}

func TestAnAppRevokesItsOwnToken(t *testing.T) {
	e := newAppEnv(t)
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	form := url.Values{"token": {app}, "client_id": {"https://evil.example.org/client.json"}}
	wantStatus(t, e.do(http.MethodPost, "/oauth/revoke", "", []byte(form.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
	// Another app's client_id revokes nothing.
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", app, nil, ""), http.StatusOK)

	form = url.Values{"token": {app}}
	wantStatus(t, e.do(http.MethodPost, "/oauth/revoke", "", []byte(form.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", app, nil, ""), http.StatusUnauthorized)
	// An unknown token is the same 200.
	wantStatus(t, e.do(http.MethodPost, "/oauth/revoke", "", []byte(form.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
}

func TestALocalAppIsNeverFetched(t *testing.T) {
	e := newAppEnv(t)
	p := appRequest("read:" + personKind)
	p["client_id"] = "http://127.0.0.1:43123/cli"
	p["redirect_uri"] = "http://127.0.0.1:43123/callback"
	id := e.park(p)
	rec := e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, rec)
	if !got.App.Local || got.App.Name != "" || e.docs.calls != 0 {
		t.Fatalf("local app = %+v after %d fetches", got.App, e.docs.calls)
	}
}

// A local app's client id is a loopback address anybody on the owner's
// computer may name, so a token the owner approved for it never makes a
// later request read as asked before: every one of its pages is a first
// approval.
func TestALocalAppsPageIsAlwaysAFirstApproval(t *testing.T) {
	e := newSetupEnv(t)
	local := func() map[string]string {
		p := appRequest("read:" + personKind)
		p["client_id"] = "http://127.0.0.1/cli"
		p["redirect_uri"] = "http://127.0.0.1:43123/callback"
		return p
	}
	id := e.park(local())
	rec := e.approve(id, []string{"read:" + personKind}, 30)
	wantStatus(t, rec, http.StatusOK)
	form := tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect))
	form.Set("client_id", "http://127.0.0.1/cli")
	form.Set("redirect_uri", "http://127.0.0.1:43123/callback")
	wantStatus(t, e.exchange(form), http.StatusOK)
	again := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(local())))
	if again.Previous != nil || again.Scopes[0].Allowed {
		t.Fatalf("a local app's second request = previous %+v, allowed %v", again.Previous, again.Scopes[0].Allowed)
	}
	// A web app's does: the client id is its proven origin.
	e.grant("read:"+personKind, []string{"read:" + personKind})
	web := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(appRequest("read:"+personKind+" write:"+taskKind))))
	if web.Previous == nil || !web.Scopes[0].Allowed || web.Scopes[1].Allowed {
		t.Fatalf("a web app's second request = previous %+v, scopes %+v", web.Previous, web.Scopes)
	}
}

func TestTheAuthorizationEndpointAnswersNoCORS(t *testing.T) {
	e := newAppEnv(t)
	req := httptest.NewRequest(http.MethodGet, authorizeURL(appRequest("read:"+personKind)), nil)
	req.Header.Set("Origin", "https://evil.example.org")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("authorize answered Access-Control-Allow-Origin %q", got)
	}
}

func TestAReplayRevocationThatFailsIsRetriedUntilItLands(t *testing.T) {
	old := revokeBackoff
	revokeBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { revokeBackoff = old })
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
	app := decodeJSON[tokenResponse](t, e.exchange(tokenForm(code))).AccessToken
	failRevokes.Lock()
	failRevokes.n = 2
	failRevokes.Unlock()
	wantStatus(t, e.exchange(tokenForm(code)), http.StatusBadRequest)
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := e.do(http.MethodGet, "/api/v1/records", app, nil, "")
		if rec.Code == http.StatusUnauthorized {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the replayed code's token still authenticates after the revoke failed twice: %d", rec.Code)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheApprovalCallsAreNeverStored(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	for _, rec := range []*httptest.ResponseRecorder{
		e.show(id),
		e.approveShown(id, []string{"read:" + personKind}, 30),
		e.do(http.MethodDelete, "/api/v1/authorizations/"+id, e.owner, nil, ""),
	} {
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("an approval call answered %d with Cache-Control %q, want no-store", rec.Code, cc)
		}
	}
}

func TestASecondReadShowsTheFirstSnapshot(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	wantStatus(t, e.show(id), http.StatusOK)
	e.docs.mu.Lock()
	e.docs.docs[appClientID] = authorize.Document{ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Totally Notes"}
	e.docs.mu.Unlock()
	got := decodeJSON[substrate.AuthorizationRequest](t, e.show(id))
	if got.App.Name != "Notes" {
		t.Fatalf("a second read showed %q, want the first snapshot's Notes", got.App.Name)
	}
	rec := e.approveShown(id, []string{"read:" + personKind}, 30)
	wantStatus(t, rec, http.StatusOK)
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)
	tok := decodeJSON[tokenResponse](t, e.exchange(tokenForm(code)))
	if name := e.svc.tokens[tok.AccessToken].info.ClientName; name != "Notes" {
		t.Fatalf("the token names the app %q, want the name the page showed", name)
	}
}

func TestAReferenceFilterNamesOnlyAKindTheGrantReads(t *testing.T) {
	e := newAppEnv(t)
	const message = "samples.substrate.reamde.dev/messaging/conversationmessage"
	app := e.grant("read:"+message, []string{"read:" + message})
	list := func(filter string) *httptest.ResponseRecorder {
		return e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(filter), app, nil, "")
	}
	// author is a reference to a person, a kind this grant does not read: a
	// match would say which people were merged.
	wantStatus(t, list(`{"kinds":["`+message+`"],"properties":{"author":{"eq":"`+personKind+`/bob"}}}`), http.StatusForbidden)
	wantStatus(t, list(`{"kinds":["`+message+`"],"properties":{"author":{"eq":"bob"}}}`), http.StatusForbidden)
	wantStatus(t, list(`{"kinds":["`+message+`"],"properties":{"author":{"in":["`+personKind+`/bob"]}}}`), http.StatusForbidden)
	// A condition that resolves nothing, and a non-reference property, pass.
	wantStatus(t, list(`{"kinds":["`+message+`"],"properties":{"author":{"exists":true}}}`), http.StatusOK)
	wantStatus(t, list(`{"kinds":["`+message+`"],"properties":{"text":{"eq":"hi"}}}`), http.StatusOK)

	both := e.grant("read:"+message+" read:"+personKind, []string{"read:" + message, "read:" + personKind})
	rec := e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"kinds":["`+message+`"],"properties":{"author":{"eq":"`+personKind+`/bob"}}}`), both, nil, "")
	wantStatus(t, rec, http.StatusOK)
}

func TestAnImplementsFilterAnswersAlikeForEveryTraitTheGrantCannotSee(t *testing.T) {
	e := newAppEnv(t)
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	ds.traits["private.example.com/clinical/patient"] = []string{taskKind}
	ds.mu.Unlock()
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	for _, trait := range []string{"private.example.com/clinical/patient", "nobody.example.com/x/never"} {
		rec := e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"implements":"`+trait+`"}`), app, nil, "")
		wantStatus(t, rec, http.StatusOK)
		if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 0 {
			t.Fatalf("implements %s listed %d records", trait, len(page.Records))
		}
	}
}

const messageKind = "samples.substrate.reamde.dev/messaging/conversationmessage"

// seed puts a record straight into the fake, with what the engine would
// attach to it.
func (e *appEnv) seed(rec substrate.Record, meta map[string]substrate.PropertyMeta) {
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if rec.Properties == nil {
		rec.Properties = map[string]any{}
	}
	ds.records[rec.ID] = &rec
	if meta != nil {
		ds.meta[rec.ID] = meta
	}
}

func TestAnAppsPatchAndDeleteOutsideItsWriteScopesAreRefused(t *testing.T) {
	e := newAppEnv(t)
	e.seed(substrate.Record{ID: "p1", Kind: personKind, Properties: map[string]any{"name": "Ada"}}, nil)
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	for _, rec := range []*httptest.ResponseRecorder{
		e.do(http.MethodPatch, "/api/v1/"+personKind+"/p1", app, []byte(`{"properties":{"name":"x"}}`), "application/json"),
		e.do(http.MethodDelete, "/api/v1/"+personKind+"/p1", app, nil, ""),
		e.do(http.MethodPost, "/api/v1/records", app, []byte(`{"kind":"`+personKind+`","properties":{"name":"y"}}`), "application/json"),
	} {
		wantStatus(t, rec, http.StatusForbidden)
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, "insufficient_scope") {
			t.Fatalf("refusal WWW-Authenticate = %q", wa)
		}
	}
	got := decodeJSON[substrate.Record](t, e.do(http.MethodGet, "/api/v1/"+personKind+"/p1", e.owner, nil, ""))
	if got.Properties["name"] != "Ada" {
		t.Fatalf("the refused writes changed the record: %v", got.Properties)
	}
}

func TestARecordPathReadOfAKindTheGrantCannotReadIsUnknown(t *testing.T) {
	e := newAppEnv(t)
	e.seed(substrate.Record{ID: "p1", Kind: personKind}, nil)
	e.seed(substrate.Record{ID: "m1", Kind: messageKind}, nil)
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+personKind+"/p1", app, nil, ""), http.StatusOK)
	rec := e.do(http.MethodGet, "/api/v1/"+messageKind+"/m1", app, nil, "")
	wantStatus(t, rec, http.StatusNotFound)
	if !strings.Contains(rec.Body.String(), "unknown kind") {
		t.Fatalf("an unreadable kind answered %s, want it unknown", rec.Body.String())
	}
}

// Every record an app's token receives is cut to its grant: inbound links,
// property provenance and expanded referents of kinds it cannot read are
// left out, on a read, a list and a write's answer alike.
func TestAnAppsRecordsAreCutToItsGrant(t *testing.T) {
	e := newAppEnv(t)
	personPath := personKind + "/p1"
	e.seed(substrate.Record{ID: "p1", Kind: personKind, Properties: map[string]any{"name": "Ada"}}, nil)
	e.seed(substrate.Record{
		ID: "m1", Kind: messageKind,
		Properties: map[string]any{"text": "hi", "author": map[string]any{"ref": personPath}},
		LinkedFrom: []substrate.LinkedRecord{{Ref: personPath, Kind: personKind, Property: "author"}},
	}, map[string]substrate.PropertyMeta{"text": {Source: personPath}})
	app := e.grant("write:"+messageKind, []string{"write:" + messageKind})

	got := decodeJSON[substrate.Record](t, e.do(http.MethodGet, "/api/v1/"+messageKind+"/m1", app, nil, ""))
	if len(got.LinkedFrom) != 0 || got.PropertyMeta["text"].Source != "" {
		t.Fatalf("a read carried linkedFrom %v and provenance %v of an unreadable kind", got.LinkedFrom, got.PropertyMeta)
	}
	page := decodeJSON[substrate.Page](t, e.do(http.MethodGet, "/api/v1/records?expand=author&filter="+url.QueryEscape(`{"kinds":["`+messageKind+`"]}`), app, nil, ""))
	if len(page.Records) != 1 || len(page.Included) != 0 {
		t.Fatalf("a list with expand answered %d records and included %v", len(page.Records), page.Included)
	}
	owner := decodeJSON[substrate.Page](t, e.do(http.MethodGet, "/api/v1/records?expand=author&filter="+url.QueryEscape(`{"kinds":["`+messageKind+`"]}`), e.owner, nil, ""))
	if len(owner.Included) == 0 {
		t.Fatal("the owner's list includes no referent: the test proves nothing")
	}
}

func TestAnAppSearchesByWordsAloneAndSeesNoScores(t *testing.T) {
	e := newAppEnv(t)
	e.seed(substrate.Record{ID: "p1", Kind: personKind, Title: "Ada notes"}, nil)
	e.seed(substrate.Record{ID: "m1", Kind: messageKind, Title: "notes from Bob"}, nil)
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	ds := e.svc.datasets[fakeRepository]
	for _, mode := range []string{"", "&mode=hybrid", "&mode=lexical"} {
		rec := e.do(http.MethodGet, "/api/v1/records?q=notes"+mode, app, nil, "")
		wantStatus(t, rec, http.StatusOK)
		if ds.lastSearch.Mode != substrate.SearchLexical {
			t.Fatalf("an app's search%s ran %q, want lexical", mode, ds.lastSearch.Mode)
		}
		ranked := decodeJSON[substrate.RankedPage](t, rec)
		if len(ranked.Records) != 1 || ranked.Records[0].ID != "p1" {
			t.Fatalf("an app's search answered %d hits, want only the readable one", len(ranked.Records))
		}
		if s := ranked.Scores[personKind+"/p1"]; s.Lexical != 0 || s.Semantic != 0 {
			t.Fatalf("an app's search carried scores %+v", s)
		}
	}
	rec := e.do(http.MethodGet, "/api/v1/records?q=notes&mode=semantic", app, nil, "")
	wantStatus(t, rec, http.StatusForbidden)
}

func TestAGlobNeverReachesAnInternalKindAndAnExactScopeDoes(t *testing.T) {
	e := newAppEnv(t)
	const connector = "substrate.reamde.dev/core/connector"
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	for i, k := range ds.types {
		if k.Package == "core" {
			def := map[string]any{}
			for key, v := range k.Definition {
				def[key] = v
			}
			def["purpose"] = "internal"
			ds.types[i].Definition = def
		}
	}
	ds.mu.Unlock()
	e.seed(substrate.Record{ID: "c1", Kind: connector}, nil)

	all := e.grant("read:*", []string{"read:*"})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", all, nil, ""), http.StatusOK)
	for _, k := range ds.lastQuery.Filter.Kinds {
		if strings.HasPrefix(k, "substrate.reamde.dev/core/") {
			t.Fatalf("read:* listed the internal kind %s", k)
		}
	}
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+connector+"/c1", all, nil, ""), http.StatusNotFound)

	exact := e.grant("read:"+connector, []string{"read:" + connector})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+connector+"/c1", exact, nil, ""), http.StatusOK)
}

func TestAnAppWritesReferencesOnlyToKindsItReads(t *testing.T) {
	e := newAppEnv(t)
	put := func(app, kind, body string) *httptest.ResponseRecorder {
		return e.do(http.MethodPut, "/api/v1/"+kind+"/x1", app, []byte(body), "application/json")
	}
	writer := e.grant("write:"+messageKind, []string{"write:" + messageKind})
	// author is pinned to the person kind, which this grant does not read: a
	// write naming one would answer whether that person exists.
	for _, body := range []string{
		`{"properties":{"author":"` + personKind + `/p1"}}`,
		`{"properties":{"author":"p1"}}`,
		`{"properties":{"author":{"ref":"` + personKind + `/p1","since":1}}}`,
	} {
		wantStatus(t, put(writer, messageKind, body), http.StatusForbidden)
	}
	wantStatus(t, put(writer, messageKind, `{"properties":{"text":"no reference"}}`), http.StatusCreated)

	both := e.grant("write:"+messageKind+" read:"+personKind, []string{"write:" + messageKind, "read:" + personKind})
	e.seed(substrate.Record{ID: "p1", Kind: personKind}, nil)
	rec := put(both, messageKind, `{"properties":{"author":"`+personKind+`/p1"}}`)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("a reference to a readable kind = %d %s", rec.Code, rec.Body.String())
	}
}

func TestTheTokenEndpointHoldsTheResourceToThisServer(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
	f := tokenForm(code)
	f.Set("resource", "https://elsewhere.example.com")
	rec := e.exchange(f)
	wantStatus(t, rec, http.StatusBadRequest)
	if got := decodeJSON[oauthError](t, rec).Error; got != "invalid_target" {
		t.Fatalf("error = %q, want invalid_target", got)
	}
	f.Set("resource", "https://substrate.example.com")
	wantStatus(t, e.exchange(f), http.StatusOK)
}

func TestTheSameScopeAskedTwiceIsListedOnce(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:~/tasks/* read:" + fakeRepository + "/tasks/*"))
	got := decodeJSON[substrate.AuthorizationRequest](t, e.show(id))
	if len(got.Scopes) != 1 {
		t.Fatalf("the page lists %d scopes, want the one they resolve to", len(got.Scopes))
	}
}

func TestOneSourceCannotParkEveryRequest(t *testing.T) {
	svc := newFakeService()
	h := New(Config{Service: svc, ClientDocuments: &memDocs{}, AuthInterval: time.Nanosecond})
	park := func(addr string) int {
		req := httptest.NewRequest(http.MethodGet, authorizeURL(appRequest("read:"+personKind)), nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	// One /64 is one source, however many addresses it spends.
	refused := 0
	for i := range 40 {
		if park(fmt.Sprintf("[2001:db8:1:2::%x]:443", i+1)) != http.StatusSeeOther {
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("forty requests from one /64 all parked")
	}
	if code := park("[2001:db8:9:9::1]:443"); code != http.StatusSeeOther {
		t.Fatalf("another source was refused %d", code)
	}
}

func TestAnOwnerMayApproveAnAppUntilTheyRevokeIt(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	view := decodeJSON[substrate.AuthorizationRequest](t, e.show(id))
	if !slices.Contains(view.LifetimeDays, 0) {
		t.Fatalf("lifetimes offered %v, want until revoked (0) among them", view.LifetimeDays)
	}
	rec := e.approveShown(id, []string{"read:" + personKind}, 0)
	wantStatus(t, rec, http.StatusOK)
	tok := e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
	wantStatus(t, tok, http.StatusOK)
	if strings.Contains(tok.Body.String(), "expires_in") {
		t.Fatalf("a token approved until revoked answered %s, want no expires_in", tok.Body.String())
	}
	if exp := e.svc.tokens[decodeJSON[tokenResponse](t, tok).AccessToken].info.ExpiresAt; exp != nil {
		t.Fatalf("a token approved until revoked expires at %v", exp)
	}
}

func TestNoRequestClaimsAnAppsActor(t *testing.T) {
	e := newAppEnv(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/"+taskKind+"/t1", strings.NewReader(`{"properties":{}}`))
	req.Header.Set("Authorization", "Bearer "+e.owner)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorHeader, "app:notes.example.org")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusForbidden)
}

// Until revoked is 0, so an approval that names no lifetime is refused, never
// read as 0: forgetting the field must not grant the longest access.
func TestAnApprovalNamesItsLifetime(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:" + personKind))
	wantStatus(t, e.show(id), http.StatusOK)
	post := func(body string) *httptest.ResponseRecorder {
		return e.do(http.MethodPost, "/api/v1/authorizations/"+id, e.owner, []byte(body), "application/json")
	}
	scopes := `"scopes":["read:` + personKind + `"]`
	wantStatus(t, post(`{`+scopes+`}`), http.StatusUnprocessableEntity)
	wantStatus(t, post(`{`+scopes+`,"lifetimeDays":null}`), http.StatusUnprocessableEntity)
	wantStatus(t, post(`{`+scopes+`,"lifetimeDays":7}`), http.StatusOK)
}

// An IPv6 app's actor is also its label namespace, so it carries no bracket.
func TestAnIPv6AppsActorIsAValidLabelNamespace(t *testing.T) {
	actor := appActorOf(substrate.TokenInfo{ClientID: "http://[::1]:8080/client"})
	if actor != "app:::1:8080" {
		t.Fatalf("actor = %q", actor)
	}
	if problem := vocabulary.MetaKeyProblem(string(actor) + "/tag"); problem != "" {
		t.Fatalf("the app's label namespace is refused: %s", problem)
	}
}

// A web app may send the access to a program on the owner's own computer,
// which the page marks as unverified; anything else stays on its origin.
func TestAWebAppsLoopbackRedirectIsMarked(t *testing.T) {
	e := newAppEnv(t)
	e.docs.mu.Lock()
	e.docs.docs[appClientID] = authorize.Document{
		ClientID: appClientID, RedirectURIs: []string{appRedirect, "http://127.0.0.1/callback"}, Name: "Notes",
	}
	e.docs.mu.Unlock()
	p := appRequest("read:" + personKind)
	p["redirect_uri"] = "http://127.0.0.1:51234/callback"
	got := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(p)))
	if !got.Redirect.Loopback {
		t.Fatalf("redirect = %+v, want it marked loopback", got.Redirect)
	}
	same := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(appRequest("read:"+personKind))))
	if same.Redirect.Loopback {
		t.Fatalf("a same-origin redirect = %+v, want it unmarked", same.Redirect)
	}
}

const (
	holderKind = "test.example.com/nest/holder"
	metricKind = "substrate.reamde.dev/observations/metric"
)

// withHolder adds a kind whose references sit in an object's field, in a
// keyed map, and under a pin whose ids carry a slash, and the metric kind it
// pins.
func (e *appEnv) withHolder() {
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.types = append(ds.types,
		substrate.KindInfo{
			Identity: holderKind, Name: "holder", Authority: "test.example.com", Package: "nest", Version: 1, Source: "installed",
			Definition: map[string]any{"properties": map[string]any{
				"box": map[string]any{"type": "object", "fields": map[string]any{
					"who": map[string]any{"type": "reference", "kind": personKind},
					"inner": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
						"who": map[string]any{"type": "reference", "kind": personKind},
					}},
				}},
				"byRole": map[string]any{"type": "reference", "kind": personKind, "keyed": true},
				"metric": map[string]any{"type": "reference", "kind": metricKind},
				"about":  map[string]any{"type": "reference", "kind": "any"},
			}},
		},
		substrate.KindInfo{
			Identity: metricKind, Name: "metric", Authority: "substrate.reamde.dev", Package: "observations", Version: 1, Source: "builtin",
			Definition: map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}},
		},
	)
}

func TestAnAppsNestedReferencesAreHeldToItsGrant(t *testing.T) {
	e := newAppEnv(t)
	e.withHolder()
	put := func(app, body string) int {
		return e.do(http.MethodPut, "/api/v1/"+holderKind+"/h1", app, []byte(body), "application/json").Code
	}
	writer := e.grant("write:"+holderKind, []string{"write:" + holderKind})
	reader := e.grant("write:"+holderKind+" read:"+personKind, []string{"write:" + holderKind, "read:" + personKind})
	for name, body := range map[string]string{
		"object field": `{"properties":{"box":{"who":"p1"}}}`,
		"nested list":  `{"properties":{"box":{"inner":[{"who":"` + personKind + `/p1"}]}}}`,
		"keyed map":    `{"properties":{"byRole":{"lead":"p1"}}}`,
		"keyed ref":    `{"properties":{"byRole":{"lead":{"ref":"` + personKind + `/p1"}}}}`,
		"unpinned":     `{"properties":{"about":"` + personKind + `/p1"}}`,
	} {
		if got := put(writer, body); got != http.StatusForbidden {
			t.Errorf("%s: a reference to a kind the grant does not read answered %d, want 403", name, got)
		}
		if got := put(reader, body); got != http.StatusOK && got != http.StatusCreated {
			t.Errorf("%s: the same write by a grant that reads the kind answered %d", name, got)
		}
	}
	// An unpinned reference names its kind in full, or it names none the
	// check could hold.
	if got := put(reader, `{"properties":{"about":"p1"}}`); got != http.StatusForbidden {
		t.Errorf("a bare id under an unpinned reference answered %d, want 403", got)
	}
}

// An id may carry a slash (`health/heart_rate`): a bare one completes under
// the pin, as the engine completes it, in a write and in a filter alike.
func TestASlashBearingIDCompletesUnderItsPin(t *testing.T) {
	e := newAppEnv(t)
	e.withHolder()
	app := e.grant("write:"+holderKind+" read:"+metricKind, []string{"write:" + holderKind, "read:" + metricKind})
	rec := e.do(http.MethodPut, "/api/v1/"+holderKind+"/h1", app, []byte(`{"properties":{"metric":"health/heart_rate"}}`), "application/json")
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("a bare slash-bearing id under a readable pin answered %d %s", rec.Code, rec.Body.String())
	}
	filter := `{"kinds":["` + holderKind + `"],"properties":{"metric":{"eq":"health/heart_rate"}}}`
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(filter), app, nil, ""), http.StatusOK)

	blind := e.grant("write:"+holderKind, []string{"write:" + holderKind})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(filter), blind, nil, ""), http.StatusForbidden)
}

// A reverse read names its target: one of a kind the grant cannot read is
// answered as the unknown kind it is to this token.
func TestAReverseReadNamesOnlyAReadableTarget(t *testing.T) {
	e := newAppEnv(t)
	app := e.grant("read:"+messageKind, []string{"read:" + messageKind})
	hidden := `{"kinds":["` + messageKind + `"],"referencing":{"ref":"` + personKind + `/p1"}}`
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(hidden), app, nil, ""), http.StatusNotFound)
	both := e.grant("read:"+messageKind+" read:"+personKind, []string{"read:" + messageKind, "read:" + personKind})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(hidden), both, nil, ""), http.StatusOK)
}

// Full access (decision 0204) is asked for alone, approved whole through a
// human door, and mints a token without scopes that still names its app.
func TestAnAppAskingForFullAccessGetsAFullAccessTokenNamingIt(t *testing.T) {
	e := newAppEnv(t)
	meta := e.do(http.MethodGet, "/.well-known/oauth-authorization-server", "", nil, "")
	wantStatus(t, meta, http.StatusOK)
	if !strings.Contains(meta.Body.String(), `"full"`) {
		t.Fatalf("the metadata does not list full: %s", meta.Body.String())
	}

	id := e.park(appRequest("full"))
	view := decodeJSON[substrate.AuthorizationRequest](t, e.show(id))
	if len(view.Scopes) != 1 || view.Scopes[0].Scope != "full" || view.Scopes[0].Verb != "full" ||
		view.Scopes[0].Pattern != "" || view.Scopes[0].Allowed || view.Scopes[0].Kinds == nil {
		t.Fatalf("scopes = %+v, want the one full row", view.Scopes)
	}

	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: []string{"full"}, LifetimeDays: &days})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/authorizations/"+id, bytes.NewReader(body))
	req.Host = "substrate.example.com"
	req.Header.Set("Authorization", "Bearer "+e.owner)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorHeader, "bob")
	bob := httptest.NewRecorder()
	e.h.ServeHTTP(bob, req)
	wantErrorCode(t, bob, http.StatusForbidden, codeForbidden)
	if !strings.Contains(bob.Body.String(), "granting full access is the owner's act") {
		t.Fatalf("the refusal says %s", bob.Body.String())
	}

	rec := e.approveShown(id, []string{"full"}, 30)
	wantStatus(t, rec, http.StatusOK)
	tok := e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
	wantStatus(t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](t, tok)
	if res.Scope != "full" || res.Repository != fakeRepository || res.ExpiresIn == nil || res.TokenID == "" {
		t.Fatalf("token response = %+v", res)
	}
	minted := e.svc.tokens[res.AccessToken].info
	if minted.Scopes != nil || minted.Scoped() || minted.ClientID != appClientID || minted.ClientName != "Notes" || minted.ExpiresAt == nil {
		t.Fatalf("minted token = %+v, want no scopes and the app's client id", minted)
	}
	// The owner's routes answer it as they answer a sign-in.
	for _, path := range []string{"/tokens", "/api/v1/catalog", "/api/v1/sync/status"} {
		wantStatus(t, e.do(http.MethodGet, path, res.AccessToken, nil, ""), http.StatusOK)
	}
	// And the app can still revoke what it was given.
	revoke := url.Values{"token": {res.AccessToken}, "client_id": {appClientID}}
	wantStatus(t, e.do(http.MethodPost, "/oauth/revoke", "", []byte(revoke.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, "/tokens", res.AccessToken, nil, ""), http.StatusUnauthorized)
}

func TestFullAccessIsAskedForAloneAndSetsNothingUp(t *testing.T) {
	e := newAppEnv(t)
	for _, params := range []map[string]string{
		appRequest("full read:" + personKind),
		func() map[string]string {
			p := appRequest("full")
			p["authorization_details"] = `[{"type":"package","package":"providers.substrate.reamde.dev/apple","version":{"atLeast":1}}]`
			return p
		}(),
	} {
		rec := e.do(http.MethodGet, authorizeURL(params), "", nil, "")
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
			t.Fatalf("authorize(%v) = %d %s, want a 400 page", params["scope"], rec.Code, rec.Header().Get("Location"))
		}
	}
	// A narrower app is never handed full by naming it in its approval.
	id := e.park(appRequest("read:" + personKind))
	wantErrorCode(t, e.approve(id, []string{"full"}, 30), http.StatusUnprocessableEntity, codeValidation)
}

func TestASecondFullRequestReadsAsAllowedBefore(t *testing.T) {
	e := newSetupEnv(t)
	e.grant("read:"+personKind, []string{"read:" + personKind})
	narrower := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(appRequest("full"))))
	if narrower.Previous == nil || narrower.Scopes[0].Allowed {
		t.Fatalf("full after a scoped approval = previous %+v allowed %v, want not allowed", narrower.Previous, narrower.Scopes[0].Allowed)
	}
	// The fake mints every token at one instant, so newest is decided only in
	// a repository holding one.
	e = newSetupEnv(t)
	e.grant("full", []string{"full"})
	again := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(appRequest("full"))))
	if again.Previous == nil || len(again.Previous.Scopes) != 0 || !again.Scopes[0].Allowed {
		t.Fatalf("a second full request = previous %+v allowed %v", again.Previous, again.Scopes[0].Allowed)
	}
}
