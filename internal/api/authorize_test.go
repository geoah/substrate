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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The authorization flow's own state, the document fetch and the token
// endpoint's form, on the stub. What an approval writes, the token it mints
// and what that token reaches, is the engine's, in authorize_db_test.go.

// The app in these tests serves its document from memory: the real fetcher
// never dials a loopback address, which is every address a test has.
const (
	appClientID = "https://notes.example.org/client.json"
	appRedirect = "https://notes.example.org/callback"
	appVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-the-verifier"
	taskKind    = "samples.substrate.reamde.dev/tasks/task"
	messageKind = "samples.substrate.reamde.dev/messaging/conversationmessage"
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

// notesDocs serves the notes app's document.
func notesDocs() *memDocs {
	return &memDocs{docs: map[string]authorize.Document{
		appClientID: {ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Notes", URI: "https://notes.example.org"},
	}}
}

// appEnv drives the flow as an app and its owner do: newAuthorizeStub on
// the stub, newAuthorizeEnv on the engine.
type appEnv struct {
	t     *testing.T
	h     http.Handler
	docs  *memDocs
	owner string
}

// authorizePageReads is a repository with an empty registry and no token: all
// the approval page reads of one for a request that sets nothing up. What
// a page lists of a real registry and a real token is the engine's.
type authorizePageReads struct{ stubDataset }

func (authorizePageReads) Kinds(context.Context) ([]substrate.KindInfo, error) { return nil, nil }

func (authorizePageReads) Tokens(context.Context) ([]substrate.TokenInfo, error) { return nil, nil }

// newAuthorizeStub serves the flow on the stub, the notes app's document
// served from memory and the park pace refilled by the wall clock. Its
// owner can park, read, approve and deny; nothing on it mints a token.
func newAuthorizeStub(t *testing.T, cfg ...func(*Config)) *appEnv {
	t.Helper()
	docs := notesDocs()
	base := func(c *Config) { c.ClientDocuments, c.AuthInterval, c.Now = docs, time.Nanosecond, time.Now }
	env := newStubEnv(t, authorizePageReads{}, append([]func(*Config){base}, cfg...)...)
	return &appEnv{t: t, h: env.h, docs: docs, owner: stubToken}
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

// A token whose stored grant does not parse is refused at the door, never
// read as no grant at all, which would be full access.
func TestAnAppsTokenWithADamagedGrantDoesNotAuthenticate(t *testing.T) {
	svc := &stubService{ds: stubDataset{}, info: substrate.TokenInfo{ID: "tok1", Label: "test", Scopes: []string{"everything"}}}
	e := &appEnv{t: t, h: newStubEnvOn(t, svc).h}
	rec := e.do(http.MethodGet, "/api/v1/records", stubToken, nil, "")
	wantStatus(t, rec, http.StatusUnauthorized)
	if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="invalid_token"`) ||
		!strings.Contains(wa, `resource_metadata="https://substrate.example.com/.well-known/oauth-protected-resource"`) {
		t.Fatalf("401 WWW-Authenticate = %q", wa)
	}
}

func TestARequestWithoutATokenLearnsWhereTheMetadataIs(t *testing.T) {
	e := newAuthorizeStub(t)
	rec := e.do(http.MethodGet, "/api/v1/records", "", nil, "")
	wantStatus(t, rec, http.StatusUnauthorized)
	if wa := rec.Header().Get("WWW-Authenticate"); wa != `Bearer resource_metadata="https://substrate.example.com/.well-known/oauth-protected-resource"` {
		t.Fatalf("WWW-Authenticate = %q", wa)
	}
}

func TestAConfiguredPublicURLIsTheIssuerWhateverTheHost(t *testing.T) {
	h := New(Config{Service: &stubService{}, ClientDocuments: &memDocs{}, PublicURL: "https://substrate.example.net/"})
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
	e := newAuthorizeStub(t)
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
	e := newAuthorizeStub(t)
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

	// A web app's access goes back to its own origin unless a verified
	// publisher's authority holds the redirect (decision 0209). Deciding that
	// reads the app's document and its publisher's DNS, which waits for the
	// owner's sign-in: the request parks, the page refuses it, and nothing
	// can be approved.
	for name, redirect := range map[string]string{
		"other host": "https://elsewhere.example.com/callback",
		"other port": "https://notes.example.org:8443/callback",
		"sub-domain": "https://app.notes.example.org/callback",
	} {
		t.Run(name, func(t *testing.T) {
			p := appRequest("read:" + personKind)
			p["redirect_uri"] = redirect
			id := e.park(p)
			page := e.show(id)
			wantStatus(t, page, http.StatusUnprocessableEntity)
			if !strings.Contains(page.Body.String(), "the redirect_uri") && !strings.Contains(page.Body.String(), "redirect_uri must be on the client_id's own origin") {
				t.Fatalf("the page reads %s", page.Body.String())
			}
			wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 30), http.StatusUnprocessableEntity)
		})
	}
}

func TestAnApprovalIsHeldToTheRequest(t *testing.T) {
	e := newAuthorizeStub(t)
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

// authorizeStranger is the bearer authorizeTwoRepositories opens another
// repository with.
const authorizeStranger = "substrate_tok_stranger"

// authorizeTwoRepositories is the stub's service with a second repository,
// whose token is authorizeStranger.
type authorizeTwoRepositories struct{ *stubService }

func (s authorizeTwoRepositories) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	if secret == authorizeStranger {
		return authorizeOtherRepository{}, substrate.TokenInfo{ID: "tok2", Label: "test", Created: time.Unix(0, 0).UTC()}, nil
	}
	return s.stubService.Authenticate(ctx, secret)
}

// authorizeOtherRepository is the second repository authorizeTwoRepositories
// opens.
type authorizeOtherRepository struct{ authorizePageReads }

func (authorizeOtherRepository) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: "other.example.com", Authority: "other.example.com", State: "active"}
}

func TestARequestIsAnsweredOnlyByTheRepositoryThatClaimedIt(t *testing.T) {
	docs := notesDocs()
	svc := authorizeTwoRepositories{&stubService{ds: authorizePageReads{}, info: substrate.TokenInfo{ID: "tok1", Label: "test", Created: time.Unix(0, 0).UTC()}}}
	e := &appEnv{t: t, docs: docs, owner: stubToken, h: newStubEnvOn(t, svc, func(c *Config) {
		c.ClientDocuments, c.AuthInterval, c.Now = docs, time.Nanosecond, time.Now
	}).h}
	id := e.park(appRequest("read:" + personKind))
	wantStatus(t, e.show(id), http.StatusOK)
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: []string{"read:" + personKind}, LifetimeDays: &days})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/authorizations/"+id, authorizeStranger, nil, ""), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/"+id, authorizeStranger, body, "application/json"), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/authorizations/"+id, authorizeStranger, nil, ""), http.StatusNotFound)
	wantStatus(t, e.approveShown(id, []string{"read:" + personKind}, 30), http.StatusOK)
}

func TestAnApprovalIsHeldToTheDocumentThePageShowed(t *testing.T) {
	e := newAuthorizeStub(t)
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

func TestAnUnlistedRedirectOrAnUnfetchableDocumentIsRefusedToTheOwner(t *testing.T) {
	e := newAuthorizeStub(t)
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
	e := newAuthorizeStub(t)
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

func TestACodeExchangedOnAnotherIssuerIsRefused(t *testing.T) {
	e := newAuthorizeStub(t)
	id := e.park(appRequest("read:" + personKind))
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm(code).Encode()))
	req.Host = "other.example.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusBadRequest)
}

func TestTheTokenEndpointReadsAFormAndNothingElse(t *testing.T) {
	e := newAuthorizeStub(t)
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

func TestALocalAppIsNeverFetched(t *testing.T) {
	e := newAuthorizeStub(t)
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

func TestTheAuthorizationEndpointAnswersNoCORS(t *testing.T) {
	e := newAuthorizeStub(t)
	req := httptest.NewRequest(http.MethodGet, authorizeURL(appRequest("read:"+personKind)), nil)
	req.Header.Set("Origin", "https://evil.example.org")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("authorize answered Access-Control-Allow-Origin %q", got)
	}
}

func TestTheApprovalCallsAreNeverStored(t *testing.T) {
	e := newAuthorizeStub(t)
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

func TestTheSameScopeAskedTwiceIsListedOnce(t *testing.T) {
	e := newAuthorizeStub(t)
	id := e.park(appRequest("read:~/tasks/* read:" + stubRepository + "/tasks/*"))
	got := decodeJSON[substrate.AuthorizationRequest](t, e.show(id))
	if len(got.Scopes) != 1 {
		t.Fatalf("the page lists %d scopes, want the one they resolve to", len(got.Scopes))
	}
}

func TestOneSourceCannotParkEveryRequest(t *testing.T) {
	h := New(Config{Service: &stubService{}, ClientDocuments: &memDocs{}, AuthInterval: time.Nanosecond})
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

// Until revoked is 0, so an approval that names no lifetime is refused, never
// read as 0: forgetting the field must not grant the longest access.
func TestAnApprovalNamesItsLifetime(t *testing.T) {
	e := newAuthorizeStub(t)
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
// A loopback one writes as app:local (decision 0209).
func TestAnIPv6AppsActorIsAValidLabelNamespace(t *testing.T) {
	if actor := appActorOf(substrate.TokenInfo{ClientID: "http://[::1]:8080/client"}); actor != substrate.AppActorLocal {
		t.Fatalf("a loopback app's actor = %q", actor)
	}
	actor := appActorOf(substrate.TokenInfo{ClientID: "https://[2001:db8::1]:8443/client"})
	if actor != "app:2001:db8::1:8443" {
		t.Fatalf("actor = %q", actor)
	}
	if problem := vocabulary.MetaKeyProblem(string(actor) + "/tag"); problem != "" {
		t.Fatalf("the app's label namespace is refused: %s", problem)
	}
}

// A web app may send the access to a program on the owner's own computer,
// which the page marks as unverified; anything else stays on its origin.
func TestAWebAppsLoopbackRedirectIsMarked(t *testing.T) {
	e := newAuthorizeStub(t)
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

func TestFullAccessIsAskedForAloneAndSetsNothingUp(t *testing.T) {
	e := newAuthorizeStub(t)
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

// authorizeAppGrant is a repository whose one token is the notes app's
// read of people, approved under a grant a later decision retired (decision
// 0214's `declare:`) when retired is set, as the engine lists one.
type authorizeAppGrant struct {
	authorizePageReads
	retired bool
}

func (d authorizeAppGrant) Tokens(context.Context) ([]substrate.TokenInfo, error) {
	scopes := []string{"read:" + personKind}
	if d.retired {
		scopes = append(scopes, "declare:notes.example.org/notes")
	}
	return []substrate.TokenInfo{{
		ID: "app1", Label: "notes.example.org", Created: time.Unix(0, 0).UTC(), ClientID: appClientID, ClientName: "Notes",
		Scopes: scopes, Retired: d.retired,
	}}, nil
}

// A token whose grant a later decision retired is no previous approval: the
// page reads the request as the app's first, where the same grant live
// reads as asked before.
func TestARetiredAppTokenIsNotPreviousApproval(t *testing.T) {
	for _, retired := range []bool{false, true} {
		docs := notesDocs()
		e := &appEnv{t: t, docs: docs, owner: stubToken, h: newStubEnv(t, authorizeAppGrant{retired: retired}, func(c *Config) {
			c.ClientDocuments, c.AuthInterval, c.Now = docs, time.Nanosecond, time.Now
		}).h}
		page := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(appRequest("read:"+personKind))))
		if retired && (page.Previous != nil || page.Scopes[0].Allowed) {
			t.Fatalf("retired grant shown as previous approval: %+v", page)
		}
		if !retired && (page.Previous == nil || !page.Scopes[0].Allowed) {
			t.Fatalf("a live grant is not shown as previous approval: %+v", page)
		}
	}
}

// authorizeSearches records the searches an app's token hands the
// repository, and answers one hit of a kind the token reads and one of a
// kind it does not, each with its scores.
type authorizeSearches struct {
	stubKinds
	got *[]substrate.SearchInput
}

func (d authorizeSearches) Kinds(context.Context) ([]substrate.KindInfo, error) {
	var out []substrate.KindInfo
	for _, r := range d.refs {
		k, _ := d.KindByRef(context.Background(), r)
		out = append(out, k)
	}
	return out, nil
}

func (d authorizeSearches) Search(_ context.Context, in substrate.SearchInput) (substrate.SearchResult, error) {
	*d.got = append(*d.got, in)
	return substrate.SearchResult{Hits: []substrate.Hit{
		{Record: &substrate.Record{Kind: personKind, ID: "p1", Properties: map[string]any{}}, Lexical: 0.7, Semantic: 0.4},
		{Record: &substrate.Record{Kind: taskKind, ID: "t1", Properties: map[string]any{}}, Lexical: 0.9, Semantic: 0.8},
	}}, nil
}

// An app's search reaches the repository in the mode it asked for, the
// semantic one included (decision 0219), over the kinds it reads alone, and
// what comes back is cut to those kinds, without scores. What the engine
// finds in each mode is authorize_db_test.go's.
func TestAnAppsSearchKeepsItsModeAndItsKinds(t *testing.T) {
	var got []substrate.SearchInput
	ds := authorizeSearches{stubKinds: newStubKinds(personKind, taskKind), got: &got}
	svc := &stubService{ds: ds, info: substrate.TokenInfo{ID: "app1", Label: "notes.example.org", ClientID: appClientID, Scopes: []string{"read:" + personKind}}}
	e := &appEnv{t: t, h: newStubEnvOn(t, svc).h}
	for _, mode := range []substrate.SearchMode{"", substrate.SearchHybrid, substrate.SearchLexical, substrate.SearchSemantic} {
		path := "/api/v1/records?q=notes"
		if mode != "" {
			path += "&mode=" + string(mode)
		}
		rec := e.do(http.MethodGet, path, stubToken, nil, "")
		wantStatus(t, rec, http.StatusOK)
		last := got[len(got)-1]
		if last.Mode != mode {
			t.Fatalf("an app's search in mode %q ran %q, want the mode it asked for", mode, last.Mode)
		}
		// A search naming no kind is cut to the kinds the token reads, never
		// widened.
		if len(last.Kinds) != 1 || last.Kinds[0] != personKind {
			t.Fatalf("an app's search ran over %v, want the readable kind alone", last.Kinds)
		}
		ranked := decodeJSON[substrate.RankedPage](t, rec)
		if len(ranked.Records) != 1 || ranked.Records[0].ID != "p1" {
			t.Fatalf("an app's search answered %d hits, want only the readable one", len(ranked.Records))
		}
		if s := ranked.Scores[personKind+"/p1"]; s.Lexical != 0 || s.Semantic != 0 {
			t.Fatalf("an app's search carried scores %+v", s)
		}
	}
	// A kind it cannot read is unknown, in the semantic mode as in every
	// other, and never reaches the repository.
	before := len(got)
	filter := url.QueryEscape(`{"kinds":["` + taskKind + `"]}`)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?q=notes&mode=semantic&filter="+filter, stubToken, nil, ""), http.StatusNotFound)
	if len(got) != before {
		t.Fatal("a search of a kind the token cannot read reached the repository")
	}
}
