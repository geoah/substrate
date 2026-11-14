package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// authorizeClock is the wall clock moved forward by what a test adds: the
// park pace refills with real time, and a lapse is a jump.
type authorizeClock struct{ skew atomic.Int64 }

func (c *authorizeClock) now() time.Time { return time.Now().Add(time.Duration(c.skew.Load())) }

func (c *authorizeClock) advance(d time.Duration) { c.skew.Add(int64(d)) }

// authorizeEnv is the authorization flow on the real engine: the app's
// documents come from memory, the owner answers with the repository's
// full-access token, and what the approval writes, the token above all, is
// the engine's.
type authorizeEnv struct {
	*appEnv
	db *dbEnv
	// clock is the handler's: the store's lapses and the check memo read it.
	clock *authorizeClock
}

// newAuthorizeEnv opens the engine for the flow, the notes app's document
// served, and the park pace refilled by the wall clock.
func newAuthorizeEnv(t *testing.T, opts ...dbOption) *authorizeEnv {
	t.Helper()
	docs := notesDocs()
	clock := &authorizeClock{}
	base := []dbOption{withConfig(func(c *Config) {
		c.ClientDocuments, c.AuthInterval, c.Now = docs, time.Nanosecond, clock.now
	})}
	db := newDBEnv(t, append(base, opts...)...)
	return &authorizeEnv{appEnv: &appEnv{t: t, h: db.h, docs: docs, owner: db.token}, db: db, clock: clock}
}

// token is the record behind a bearer the flow minted, as the engine
// authenticates it.
func (e *authorizeEnv) token(secret string) substrate.TokenInfo {
	e.t.Helper()
	_, info, err := e.db.svc.Authenticate(context.Background(), secret)
	if err != nil {
		e.t.Fatalf("authenticate the minted token: %v", err)
	}
	return info
}

// tokens is every live token record of the repository.
func (e *authorizeEnv) tokens() []substrate.TokenInfo {
	e.t.Helper()
	out, err := e.db.ds.Tokens(context.Background())
	if err != nil {
		e.t.Fatalf("list tokens: %v", err)
	}
	return out
}

// holdsToken reports whether the repository holds a live token record id.
func (e *authorizeEnv) holdsToken(id string) bool {
	e.t.Helper()
	return slices.ContainsFunc(e.tokens(), func(t substrate.TokenInfo) bool { return t.ID == id })
}

// lastActor is the actor the engine recorded on a record's newest
// changelog entry.
func (e *authorizeEnv) lastActor(kind, id string) substrate.Actor {
	e.t.Helper()
	changes, err := e.db.ds.Changes(context.Background(), 0, substrate.ChangeFilter{Kinds: []string{kind}, RecordID: id}, 1000)
	if err != nil {
		e.t.Fatalf("read the changelog: %v", err)
	}
	if len(changes) == 0 {
		e.t.Fatalf("no changelog entry for %s/%s", kind, id)
	}
	key, _ := vocabulary.ActorKeyOf(changes[len(changes)-1].Actor)
	return key
}

// listOf lists through the records route with a filter, as bearer.
func (e *authorizeEnv) listOf(bearer, filter string) *httptest.ResponseRecorder {
	return e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(filter), bearer, nil, "")
}

// putAs writes one record through the record path as bearer.
func (e *authorizeEnv) putAs(bearer, kind, id, body string) *httptest.ResponseRecorder {
	return e.do(http.MethodPut, "/api/v1/"+kind+"/"+id, bearer, []byte(body), "application/json")
}

// inboxKinds declares, under the repository's full name, a message that
// names its author (a person), and answers its reference as the repository
// holds it, in its own namespace, `~` (decision record 0259). With feed set
// the package also declares a feed entry, which a mapping carries onto the
// message whose key it shares; a message's id is then the server's.
func (e *authorizeEnv) inboxKinds(feed bool) (message, entry string) {
	e.t.Helper()
	repo := e.db.repository
	message = repo + "/inbox/message"
	docs := `kind: substrate.reamde.dev/core/package
metadata:
  id: ` + repo + `/inbox
data:
  authority: ` + repo + `
  package: inbox
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + message + `
data:
  authority: ` + repo + `
  package: inbox
  description: A message.
  names:
    singular: message
  properties:
    text:
      type: string
      description: the body
    key:
      type: string
      description: what a feed entry matches it on
    author:
      type: reference
      kind: ` + personKind + `
      description: who wrote it
`
	if feed {
		entry = repo + "/inbox/feed"
		docs += `---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + entry + `
data:
  authority: ` + repo + `
  package: inbox
  description: A feed entry.
  names:
    singular: feed
  properties:
    content:
      type: string
      description: what it says
    key:
      type: string
      description: the message it is about
---
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: ` + repo + `/inbox/feedmessage
data:
  authority: ` + repo + `
  package: inbox
  description: A feed entry onto the message it is about.
  from: ` + entry + `
  to: ` + message + `
  property: message
  match:
    - from: key
      to: key
  map:
    text:
      path: content
`
	}
	e.db.declare(e.t, docs)
	message = "~/inbox/message"
	if feed {
		entry = "~/inbox/feed"
	}
	return message, entry
}

func TestAnAppGetsATokenHeldToTheScopesTheOwnerApproved(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("tasks", "messaging"))
	repo := e.db.repository
	id := e.park(appRequest("read:" + personKind + " write:" + taskKind + " read:~/notes/*"))

	view := e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
	wantStatus(t, view, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, view)
	if got.App.Host != "notes.example.org" || got.App.Name != "Notes" || got.App.Local ||
		got.App.AppearsAs != "notes.example.org" {
		t.Fatalf("app = %+v", got.App)
	}
	if got.Repository != repo || got.DefaultLifetimeDays != 30 {
		t.Fatalf("repository %q default %d", got.Repository, got.DefaultLifetimeDays)
	}
	if len(got.Scopes) != 3 || got.Scopes[2].Scope != "read:~/notes/*" ||
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
	if res.TokenType != "Bearer" || res.Repository != repo ||
		res.Scope != "read:"+personKind+" write:"+taskKind || res.ExpiresIn == nil || *res.ExpiresIn != 30*24*3600 {
		t.Fatalf("token response = %+v", res)
	}
	minted := e.token(res.AccessToken)
	if minted.ID != res.TokenID || minted.ClientID != appClientID || minted.ClientName != "Notes" || minted.Label != "notes.example.org" ||
		len(minted.Scopes) != 2 || minted.ExpiresAt == nil {
		t.Fatalf("minted token = %+v", minted)
	}

	// The scoped surface: what the scopes reach and nothing else.
	app := res.AccessToken
	wantStatus(t, e.listOf(app, `{"kinds":["`+personKind+`"]}`), http.StatusOK)
	wantStatus(t, e.putAs(app, taskKind, mintedID("t1"), `{"properties":{"name":"x"}}`), http.StatusCreated)
	put := e.putAs(app, personKind, mintedID("p1"), `{"properties":{"name":"x"}}`)
	wantStatus(t, put, http.StatusForbidden)
	if wa := put.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
		t.Fatalf("scope refusal WWW-Authenticate = %q", wa)
	}
	if e.db.get(t, personKind, mintedID("p1")) != nil {
		t.Fatal("a write the scopes refuse landed")
	}
	// A kind the grant cannot read is answered as if it did not exist.
	wantStatus(t, e.listOf(app, `{"kinds":["`+messageKind+`"]}`), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+tokenType+"/"+res.TokenID, app, nil, ""), http.StatusNotFound)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/tokens"},
		{http.MethodPost, "/tokens"},
		{http.MethodGet, "/api/v1/export"},
		{http.MethodPost, "/api/v1/vocabulary/apply"},
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
	// The records route's tail is the owner's: an app follows changes on
	// the change feed, which cuts each row to what it reads.
	watch := e.do(http.MethodGet, "/api/v1/records?watch=1", app, nil, "")
	wantStatus(t, watch, http.StatusForbidden)
}

// A list that names no kind lists every kind the grant reads and no other,
// and its page carries no handoff to the owner's tail.
func TestAListWithNoKindsNamesEveryReadableKindAndNoOther(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("tasks", "messaging"))
	e.db.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	e.db.put(t, taskKind, "t1", map[string]any{"name": "Write"})
	e.db.put(t, "samples.substrate.reamde.dev/messaging/conversation", "c1", map[string]any{})
	e.db.put(t, messageKind, "m1", map[string]any{
		"text":         "hi",
		"author":       map[string]any{"ref": personKind + "/p1"},
		"conversation": map[string]any{"ref": "samples.substrate.reamde.dev/messaging/conversation/c1"},
	})
	// The owner's own data, outside every pattern the grant names.
	e.db.put(t, "substrate.reamde.dev/core/contact", "k1", map[string]any{"name": "Grace"})
	app := e.grant("read:samples.substrate.reamde.dev/*", []string{"read:samples.substrate.reamde.dev/*"})
	rec := e.do(http.MethodGet, "/api/v1/records", app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	var got []string
	for _, r := range page.Records {
		got = append(got, r.Kind+"/"+r.ID)
	}
	slices.Sort(got)
	want := []string{
		"samples.substrate.reamde.dev/messaging/conversation/c1", messageKind + "/m1", personKind + "/p1", taskKind + "/t1",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("listed %v, want %v", got, want)
	}
	if page.Head != 0 || page.Generation != "" {
		t.Fatalf("an app's page carries head %d generation %q, want neither", page.Head, page.Generation)
	}
}

func TestAnAppsTokenWritesAsItsOwnActorWhateverActorItSends(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("tasks"))
	app := e.grant("write:"+taskKind, []string{"write:" + taskKind})
	id := mintedID("t1")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/"+taskKind+"/"+id, strings.NewReader(`{"properties":{}}`))
	req.Header.Set("Authorization", "Bearer "+app)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(retiredActorHeader, "console")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusCreated)
	if got := e.lastActor(taskKind, id); got != "app:notes.example.org" {
		t.Fatalf("an app's write was attributed to %q, want app:notes.example.org", got)
	}
}

func TestAnAppsTokenNeverApprovesAnAuthorization(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("tasks"))
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	id := e.park(appRequest("write:" + taskKind))
	wantStatus(t, e.show(id), http.StatusOK)
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: []string{"write:" + taskKind}, LifetimeDays: &days})
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/"+id, app, body, "application/json"), http.StatusForbidden)
	if got := e.show(id); got.Code != http.StatusOK || decodeJSON[substrate.AuthorizationRequest](t, got).State != substrate.AuthorizationPending {
		t.Fatalf("the refused approval answered the request: %d %s", got.Code, got.Body.String())
	}
}

func TestTheExchangeChecksEveryBinding(t *testing.T) {
	t.Parallel()
	cases := map[string]func(url.Values){
		"verifier":     func(f url.Values) { f.Set("code_verifier", strings.Repeat("a", 43)) },
		"client_id":    func(f url.Values) { f.Set("client_id", "https://evil.example.org/client.json") },
		"redirect_uri": func(f url.Values) { f.Set("redirect_uri", "https://notes.example.org/other") },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newAuthorizeEnv(t, withSamples("people"))
			id := e.park(appRequest("read:" + personKind))
			code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
			f := tokenForm(code)
			edit(f)
			rec := e.exchange(f)
			wantStatus(t, rec, http.StatusBadRequest)
			if got := decodeJSON[oauthError](t, rec).Error; got != "invalid_grant" {
				t.Fatalf("error = %q, want invalid_grant", got)
			}
			if n := len(e.tokens()); n != 1 {
				t.Fatalf("a refused exchange minted: %d tokens, want the owner's alone", n)
			}
			// A presentation that proves nothing spends nothing: the app
			// holding the verifier still gets its token.
			wantStatus(t, e.exchange(tokenForm(code)), http.StatusOK)
		})
	}
}

func TestAReplayedCodeRevokesTheTokenOnlyWithTheVerifier(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	id := e.park(appRequest("read:" + personKind))
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
	first := e.exchange(tokenForm(code))
	wantStatus(t, first, http.StatusOK)
	minted := decodeJSON[tokenResponse](t, first)
	app := minted.AccessToken

	// Without the verifier, a replay is refused and revokes nothing.
	f := tokenForm(code)
	f.Set("code_verifier", strings.Repeat("b", 43))
	wantStatus(t, e.exchange(f), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", app, nil, ""), http.StatusOK)

	// With every binding, it is the code working twice: the token goes.
	wantStatus(t, e.exchange(tokenForm(code)), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", app, nil, ""), http.StatusUnauthorized)
	if e.holdsToken(minted.TokenID) {
		t.Fatal("the replayed code's token record is still live")
	}
}

func TestAnAppRevokesItsOwnToken(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
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

// A local app's client id is a loopback address anybody on the owner's
// computer may name, so a token the owner approved for it never makes a
// later request read as asked before: every one of its pages is a first
// approval.
func TestALocalAppsPageIsAlwaysAFirstApproval(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("tasks"))
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

// authorizeFailingRevokes refuses the next deletes of a token, as a database
// that refuses the write does: nothing in a repository's state makes the
// engine refuse a delete it would otherwise take, and then take it.
type authorizeFailingRevokes struct {
	substrate.Dataset
	left *atomic.Int32
}

func (d authorizeFailingRevokes) Delete(ctx context.Context, actor substrate.Actor, kind, id string, in substrate.DeleteInput) (*substrate.Record, error) {
	for kind == tokenType {
		n := d.left.Load()
		if n <= 0 {
			break
		}
		if d.left.CompareAndSwap(n, n-1) {
			return nil, fmt.Errorf("%w: the database refused the write", substrate.ErrUnavailable)
		}
	}
	return d.Dataset.Delete(ctx, actor, kind, id, in)
}

// Not parallel: it shortens revokeBackoff, which every handler reads.
func TestAReplayRevocationThatFailsIsRetriedUntilItLands(t *testing.T) {
	old := revokeBackoff
	revokeBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { revokeBackoff = old })
	e := newAuthorizeEnv(t, withSamples("people"))
	var failing atomic.Int32
	e.db.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return authorizeFailingRevokes{Dataset: ds, left: &failing}
	}
	id := e.park(appRequest("read:" + personKind))
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, e.approve(id, []string{"read:" + personKind}, 30)).Redirect)
	app := decodeJSON[tokenResponse](t, e.exchange(tokenForm(code))).AccessToken
	failing.Store(2)
	wantStatus(t, e.exchange(tokenForm(code)), http.StatusBadRequest)
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := e.do(http.MethodGet, "/api/v1/records", app, nil, "")
		if rec.Code == http.StatusUnauthorized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the replayed code's token still authenticates after the revoke failed twice: %d", rec.Code)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := failing.Load(); n != 0 {
		t.Fatalf("%d of the two refused revokes never ran", n)
	}
}

func TestASecondReadShowsTheFirstSnapshot(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
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
	if name := e.token(tok.AccessToken).ClientName; name != "Notes" {
		t.Fatalf("the token names the app %q, want the name the page showed", name)
	}
}

func TestAReferenceFilterNamesOnlyAKindTheGrantReads(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("messaging"))
	app := e.grant("read:"+messageKind, []string{"read:" + messageKind})
	list := func(filter string) *httptest.ResponseRecorder { return e.listOf(app, filter) }
	// author is a reference to a person, a kind this grant does not read: a
	// match would say which people were merged.
	wantStatus(t, list(`{"kinds":["`+messageKind+`"],"properties":{"author":{"eq":"`+personKind+`/bob"}}}`), http.StatusForbidden)
	wantStatus(t, list(`{"kinds":["`+messageKind+`"],"properties":{"author":{"eq":"bob"}}}`), http.StatusForbidden)
	wantStatus(t, list(`{"kinds":["`+messageKind+`"],"properties":{"author":{"in":["`+personKind+`/bob"]}}}`), http.StatusForbidden)
	// A condition that resolves nothing, and a non-reference property, pass.
	wantStatus(t, list(`{"kinds":["`+messageKind+`"],"properties":{"author":{"exists":true}}}`), http.StatusOK)
	wantStatus(t, list(`{"kinds":["`+messageKind+`"],"properties":{"text":{"eq":"hi"}}}`), http.StatusOK)

	both := e.grant("read:"+messageKind+" read:"+personKind, []string{"read:" + messageKind, "read:" + personKind})
	wantStatus(t, e.listOf(both, `{"kinds":["`+messageKind+`"],"properties":{"author":{"eq":"`+personKind+`/bob"}}}`), http.StatusOK)
}

func TestAnImplementsFilterAnswersAlikeForEveryTraitTheGrantCannotSee(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	repo := e.db.repository
	trait, chart := repo+"/clinical/patient", repo+"/clinical/chart"
	e.db.declare(t, `kind: substrate.reamde.dev/core/package
metadata:
  id: `+repo+`/clinical
data:
  authority: `+repo+`
  package: clinical
  version: 1
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: `+trait+`
data:
  authority: `+repo+`
  package: clinical
  description: a patient's record
  properties:
    summary: string
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: `+chart+`
data:
  authority: `+repo+`
  package: clinical
  description: A chart.
  names:
    singular: chart
  traits:
    - `+trait+`
  properties:
    summary:
      type: string
      description: what the chart says
`)
	e.db.put(t, chart, "c1", map[string]any{"summary": "private"})
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	for _, trait := range []string{trait, "nobody.example.com/x/never"} {
		rec := e.listOf(app, `{"implements":"`+trait+`"}`)
		wantStatus(t, rec, http.StatusOK)
		if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 0 {
			t.Fatalf("implements %s listed %d records", trait, len(page.Records))
		}
	}
	// The owner's list of the same trait finds the chart: the app's empty
	// answer is the grant's, not an empty repository's.
	if page := decodeJSON[substrate.Page](t, e.listOf(e.owner, `{"implements":"`+trait+`"}`)); len(page.Records) != 1 {
		t.Fatalf("the owner's list of the trait = %d records, want the chart", len(page.Records))
	}
}

func TestAnAppsPatchAndDeleteOutsideItsWriteScopesAreRefused(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	e.db.put(t, personKind, "p1", map[string]any{"name": "Ada"})
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
	if got.Properties["name"] != "Ada" || got.DeletedAt != nil {
		t.Fatalf("the refused writes changed the record: %v", got.Properties)
	}
	page := decodeJSON[substrate.Page](t, e.listOf(e.owner, `{"kinds":["`+personKind+`"]}`))
	if len(page.Records) != 1 {
		t.Fatalf("the refused create landed: %d people", len(page.Records))
	}
}

func TestARecordPathReadOfAKindTheGrantCannotReadIsUnknown(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("tasks"))
	e.db.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	e.db.put(t, taskKind, "t1", map[string]any{"name": "Write"})
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+personKind+"/p1", app, nil, ""), http.StatusOK)
	rec := e.do(http.MethodGet, "/api/v1/"+taskKind+"/t1", app, nil, "")
	wantStatus(t, rec, http.StatusNotFound)
	if !strings.Contains(rec.Body.String(), "unknown kind") {
		t.Fatalf("an unreadable kind answered %s, want it unknown", rec.Body.String())
	}
}

// Every record an app's token receives is cut to its grant: inbound links,
// the source records a mark names and expanded referents of kinds it cannot read are
// left out, on a read, a list and a write's answer alike.
func TestAnAppsRecordsAreCutToItsGrant(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	message, feed := e.inboxKinds(true)
	e.db.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	held, err := e.db.ds.Put(context.Background(), substrate.ActorSystem, substrate.PutInput{
		Kind: message, Properties: map[string]any{"key": "k1", "author": map[string]any{"ref": personKind + "/p1"}},
	})
	if err != nil {
		t.Fatalf("put the message: %v", err)
	}
	m1 := held.ID
	// The feed entry carries its body onto the message: the message's text
	// comes from a record of a kind the app does not read, and links back
	// to it.
	e.db.put(t, feed, "f1", map[string]any{"key": "k1", "content": "hi"})
	owner := decodeJSON[substrate.Record](t, e.do(http.MethodGet, "/api/v1/"+message+"/"+m1, e.owner, nil, ""))
	if owner.Properties["text"] != "hi" || len(owner.LinkedFrom) == 0 || !derivedFrom(owner, "text", feed+"/f1") {
		t.Fatalf("the owner's read = text %v linkedFrom %v marks %v: the test proves nothing", owner.Properties["text"], owner.LinkedFrom, owner.Marks)
	}
	app := e.grant("write:"+message, []string{"write:" + message})

	got := decodeJSON[substrate.Record](t, e.do(http.MethodGet, "/api/v1/"+message+"/"+m1, app, nil, ""))
	if len(got.LinkedFrom) != 0 || len(got.Marks["text"].DerivedFrom) != 0 {
		t.Fatalf("a read carried linkedFrom %v and marks %v of an unreadable kind", got.LinkedFrom, got.Marks)
	}
	filter := url.QueryEscape(`{"kinds":["` + message + `"]}`)
	page := decodeJSON[substrate.Page](t, e.do(http.MethodGet, "/api/v1/records?expand=author&filter="+filter, app, nil, ""))
	if len(page.Records) != 1 || len(page.Included) != 0 {
		t.Fatalf("a list with expand answered %d records and included %v", len(page.Records), page.Included)
	}
	if r := page.Records[0]; len(r.LinkedFrom) != 0 || len(r.Marks["text"].DerivedFrom) != 0 {
		t.Fatalf("a listed record carried linkedFrom %v and marks %v of an unreadable kind", r.LinkedFrom, r.Marks)
	}
	ownerPage := decodeJSON[substrate.Page](t, e.do(http.MethodGet, "/api/v1/records?expand=author&filter="+filter, e.owner, nil, ""))
	if len(ownerPage.Included) == 0 {
		t.Fatal("the owner's list includes no referent: the test proves nothing")
	}
	// A write's answer is cut the same way.
	wrote := e.do(http.MethodPatch, "/api/v1/"+message+"/"+m1, app, []byte(`{"properties":{"key":"k1"}}`), "application/json")
	wantStatus(t, wrote, http.StatusOK)
	if r := decodeJSON[substrate.Record](t, wrote); len(r.LinkedFrom) != 0 || len(r.Marks["text"].DerivedFrom) != 0 {
		t.Fatalf("a write's answer carried linkedFrom %v and marks %v of an unreadable kind", r.LinkedFrom, r.Marks)
	}
}

// derivedFrom reports whether a record's mark on property names the source
// record at path.
func derivedFrom(r substrate.Record, property, path string) bool {
	for _, l := range r.Marks[property].DerivedFrom {
		if l.Ref == path {
			return true
		}
	}
	return false
}

// authorizeEmbedder answers the OpenAI embeddings wire with a bag-of-words
// vector per input, so a word a record and a query share scores them close.
func authorizeEmbedder(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type datum struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var out struct {
			Data []datum `json:"data"`
		}
		for i, s := range req.Input {
			vec := make([]float32, 1536)
			for _, word := range strings.Fields(strings.ToLower(s)) {
				h := 0
				for _, r := range word {
					h = (h*31 + int(r)) % len(vec)
				}
				vec[h]++
			}
			var sum float64
			for _, v := range vec {
				sum += float64(v) * float64(v)
			}
			if sum == 0 {
				vec[0], sum = 1, 1
			}
			for j := range vec {
				vec[j] /= float32(math.Sqrt(sum))
			}
			out.Data = append(out.Data, datum{Index: i, Embedding: vec})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// An app searches in every mode, the semantic one included (decision 0219),
// over the kinds it reads alone, and sees no scores.
//
// Not parallel: the engine dials the test's embedder on loopback, which
// SUBSTRATE_EGRESS_ALLOW admits for the whole process.
func TestAnAppSearchesInEveryModeOverWhatItReadsAndSeesNoScores(t *testing.T) {
	t.Setenv(egress.AllowVar, "127.0.0.1")
	e := newAuthorizeEnv(t, withSamples("tasks"))
	embedder := authorizeEmbedder(t)
	e.db.put(t, llmPackage+"/provider", "vectors", map[string]any{
		"label": "vectors", "wire": "openai", "baseURL": embedder.URL, "apiKey": "row-key-vectors", "embedModel": "text-embedding-3-small",
	})
	e.db.put(t, personKind, "p1", map[string]any{"name": "Ada notes", "details": "notes about Ada"})
	e.db.put(t, taskKind, "t1", map[string]any{"name": "notes from Bob", "description": "notes from Bob"})
	if _, err := e.db.ds.ProcessEmbedQueue(context.Background(), 50); err != nil {
		t.Fatalf("embed the records: %v", err)
	}
	// The owner's search finds both, so the app's one hit is its grant's.
	owner := decodeJSON[substrate.RankedPage](t, e.do(http.MethodGet, "/api/v1/records?q=notes&mode=semantic", e.owner, nil, ""))
	if len(owner.Records) != 2 {
		t.Fatalf("the owner's semantic search answered %d hits, want both: the test proves nothing", len(owner.Records))
	}
	app := e.grant("read:"+personKind, []string{"read:" + personKind})
	for _, mode := range []substrate.SearchMode{"", substrate.SearchHybrid, substrate.SearchLexical, substrate.SearchSemantic} {
		path := "/api/v1/records?q=notes"
		if mode != "" {
			path += "&mode=" + string(mode)
		}
		rec := e.do(http.MethodGet, path, app, nil, "")
		wantStatus(t, rec, http.StatusOK)
		ranked := decodeJSON[substrate.RankedPage](t, rec)
		if len(ranked.Records) != 1 || ranked.Records[0].ID != "p1" {
			t.Fatalf("an app's search in mode %q answered %d hits, want only the readable one", mode, len(ranked.Records))
		}
		if s := ranked.Scores[personKind+"/p1"]; s.Lexical != 0 || s.Semantic != 0 {
			t.Fatalf("an app's search in mode %q carried scores %+v", mode, s)
		}
	}
	// A kind it cannot read is unknown, in the semantic mode as in every other.
	filter := url.QueryEscape(`{"kinds":["` + taskKind + `"]}`)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?q=notes&mode=semantic&filter="+filter, app, nil, ""), http.StatusNotFound)
}

func TestAGlobNeverReachesAnInternalKindAndAnExactScopeDoes(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	const preference = corePackage + "/consolepreference"
	e.db.put(t, preference, "c1", map[string]any{})
	e.db.put(t, personKind, "p1", map[string]any{"name": "Ada"})

	all := e.grant("read:*", []string{"read:*"})
	rec := e.do(http.MethodGet, "/api/v1/records?first=500", all, nil, "")
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if !slices.ContainsFunc(page.Records, func(r *substrate.Record) bool { return r.Kind == personKind }) {
		t.Fatalf("read:* listed no person: %d records", len(page.Records))
	}
	for _, r := range page.Records {
		k, err := e.db.ds.KindByRef(context.Background(), r.Kind)
		if err != nil {
			t.Fatalf("kind %s: %v", r.Kind, err)
		}
		if kindPurpose(k) == vocabulary.PurposeInternal {
			t.Fatalf("read:* listed %s/%s, of an internal kind", r.Kind, r.ID)
		}
	}
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+preference+"/c1", all, nil, ""), http.StatusNotFound)

	exact := e.grant("read:"+preference, []string{"read:" + preference})
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+preference+"/c1", exact, nil, ""), http.StatusOK)
}

func TestAnAppWritesReferencesOnlyToKindsItReads(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	message, _ := e.inboxKinds(false)
	writer := e.grant("write:"+message, []string{"write:" + message})
	// author is pinned to the person kind, which this grant does not read: a
	// write naming one would answer whether that person exists.
	for _, body := range []string{
		`{"properties":{"author":"` + personKind + `/p1"}}`,
		`{"properties":{"author":"p1"}}`,
		`{"properties":{"author":{"ref":"` + personKind + `/p1"}}}`,
	} {
		wantStatus(t, e.putAs(writer, message, mintedID("x1"), body), http.StatusForbidden)
	}
	if e.db.get(t, message, mintedID("x1")) != nil {
		t.Fatal("a refused write landed")
	}
	wantStatus(t, e.putAs(writer, message, mintedID("x1"), `{"properties":{"text":"no reference"}}`), http.StatusCreated)

	both := e.grant("write:"+message+" read:"+personKind, []string{"write:" + message, "read:" + personKind})
	e.db.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	rec := e.putAs(both, message, mintedID("x1"), `{"properties":{"author":"`+personKind+`/p1"}}`)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("a reference to a readable kind = %d %s", rec.Code, rec.Body.String())
	}
	if got := e.db.get(t, message, mintedID("x1")); got == nil || vocabulary.ReferencePath(got.Properties["author"]) != personKind+"/p1" {
		t.Fatalf("the reference did not land: %+v", got)
	}
}

func TestTheTokenEndpointHoldsTheResourceToThisServer(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
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

func TestAnOwnerMayApproveAnAppUntilTheyRevokeIt(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
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
	if exp := e.token(decodeJSON[tokenResponse](t, tok).AccessToken).ExpiresAt; exp != nil {
		t.Fatalf("a token approved until revoked expires at %v", exp)
	}
}

// holderKind declares a kind whose references sit in an object's field, in
// a keyed map, under a pin whose ids carry a slash (the seeded metric), and
// under no pin at all, and answers its reference.
func (e *authorizeEnv) holderKind() string {
	e.t.Helper()
	repo := e.db.repository
	holder := repo + "/nest/holder"
	e.db.declare(e.t, `kind: substrate.reamde.dev/core/package
metadata:
  id: `+repo+`/nest
data:
  authority: `+repo+`
  package: nest
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: `+holder+`
data:
  authority: `+repo+`
  package: nest
  description: A holder.
  names:
    singular: holder
  properties:
    box:
      type: object
      description: a box
      fields:
        who:
          type: reference
          kind: `+personKind+`
          description: who is in it
        inner:
          type: object
          repeated: true
          description: what is in it
          fields:
            who:
              type: reference
              kind: `+personKind+`
              description: who is in that
    byRole:
      type: reference
      kind: `+personKind+`
      keyed: true
      description: people by role
    metric:
      type: reference
      kind: `+metricKind+`
      description: what it measures
    about:
      type: reference
      kind: any
      description: what it is about
`)
	return holder
}

const metricKind = "substrate.reamde.dev/observations/metric"

func TestAnAppsNestedReferencesAreHeldToItsGrant(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	holder := e.holderKind()
	put := func(app, body string) int { return e.putAs(app, holder, mintedID("h1"), body).Code }
	writer := e.grant("write:"+holder, []string{"write:" + holder})
	reader := e.grant("write:"+holder+" read:"+personKind, []string{"write:" + holder, "read:" + personKind})
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
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	holder := e.holderKind()
	app := e.grant("write:"+holder+" read:"+metricKind, []string{"write:" + holder, "read:" + metricKind})
	rec := e.putAs(app, holder, mintedID("h1"), `{"properties":{"metric":"health/heart_rate"}}`)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("a bare slash-bearing id under a readable pin answered %d %s", rec.Code, rec.Body.String())
	}
	if got := e.db.get(t, holder, mintedID("h1")); got == nil || vocabulary.ReferencePath(got.Properties["metric"]) != metricKind+"/health/heart_rate" {
		t.Fatalf("the bare id did not complete under its pin: %+v", got)
	}
	filter := `{"kinds":["` + holder + `"],"properties":{"metric":{"eq":"health/heart_rate"}}}`
	listed := e.listOf(app, filter)
	wantStatus(t, listed, http.StatusOK)
	if page := decodeJSON[substrate.Page](t, listed); len(page.Records) != 1 {
		t.Fatalf("the filter on the bare id matched %d records, want the holder", len(page.Records))
	}

	blind := e.grant("write:"+holder, []string{"write:" + holder})
	wantStatus(t, e.listOf(blind, filter), http.StatusForbidden)
}

// A reverse read names its target: one of a kind the grant cannot read is
// answered as the unknown kind it is to this token.
func TestAReverseReadNamesOnlyAReadableTarget(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("messaging"))
	app := e.grant("read:"+messageKind, []string{"read:" + messageKind})
	hidden := `{"kinds":["` + messageKind + `"],"referencing":{"ref":"` + personKind + `/p1"}}`
	wantStatus(t, e.listOf(app, hidden), http.StatusNotFound)
	both := e.grant("read:"+messageKind+" read:"+personKind, []string{"read:" + messageKind, "read:" + personKind})
	wantStatus(t, e.listOf(both, hidden), http.StatusOK)
}

// Full access (decision 0204) is asked for alone, approved whole through a
// human door, and mints a token without scopes that still names its app.
func TestAnAppAskingForFullAccessGetsAFullAccessTokenNamingIt(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t)
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
	rec := e.approveShown(id, []string{"full"}, 30)
	wantStatus(t, rec, http.StatusOK)
	tok := e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
	wantStatus(t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](t, tok)
	if res.Scope != "full" || res.Repository != e.db.repository || res.ExpiresIn == nil || res.TokenID == "" {
		t.Fatalf("token response = %+v", res)
	}
	minted := e.token(res.AccessToken)
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

// The page compares a full request with the newest token the app holds: a
// scoped one reads as not allowed, a full one as allowed before.
func TestASecondFullRequestReadsAsAllowedBefore(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	e.grant("read:"+personKind, []string{"read:" + personKind})
	narrower := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(appRequest("full"))))
	if narrower.Previous == nil || narrower.Scopes[0].Allowed {
		t.Fatalf("full after a scoped approval = previous %+v allowed %v, want not allowed", narrower.Previous, narrower.Scopes[0].Allowed)
	}
	// The full approval is now the newest token the app holds.
	e.grant("full", []string{"full"})
	again := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(appRequest("full"))))
	if again.Previous == nil || len(again.Previous.Scopes) != 0 || !again.Scopes[0].Allowed {
		t.Fatalf("a second full request = previous %+v allowed %v", again.Previous, again.Scopes[0].Allowed)
	}
}

// An app revokes its own token by its client id alone: another client id
// presenting the secret revokes nothing. A grant a later decision retired
// (decision 0214's `declare:`) is revoked the same way; no door of the engine
// stores one, so internal/engine's TestARetiredScopeTokenListsButNeverAuthenticates
// plants it, and TestARetiredAppTokenIsNotPreviousApproval holds the page to
// it.
func TestAnAppRevokesItsTokenByItsOwnClientIDAlone(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("people"))
	id := e.park(appRequest("read:" + personKind))
	rec := e.approve(id, []string{"read:" + personKind}, 30)
	wantStatus(t, rec, http.StatusOK)
	tok := e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
	wantStatus(t, tok, http.StatusOK)
	minted := decodeJSON[tokenResponse](t, tok)
	form := url.Values{"token": {minted.AccessToken}, "client_id": {"https://other.example/client.json"}}
	wantStatus(t, e.do(http.MethodPost, "/oauth/revoke", "", []byte(form.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
	if !e.holdsToken(minted.TokenID) {
		t.Fatal("a mismatched client revoked the token")
	}
	form.Set("client_id", appClientID)
	wantStatus(t, e.do(http.MethodPost, "/oauth/revoke", "", []byte(form.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
	if e.holdsToken(minted.TokenID) {
		t.Fatal("self-revocation left the token in storage")
	}
}
