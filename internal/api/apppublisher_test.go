package api

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
)

// A publisher in these tests is example.org: its record names a document
// served from memory, which lists the app whose client document lives on a
// CDN and whose callback is on the authority.
const (
	pubClientID  = "https://cdn.example.net/workouts/client.json"
	pubRedirect  = "https://apps.example.org/workouts/callback"
	pubLoopback  = "http://127.0.0.1/callback"
	pubDocURL    = "https://example.org/.well-known/substrate/authority.json"
	pubAuthority = "example.org"
)

type servedFiles map[string]string

func (s servedFiles) RoundTrip(r *http.Request) (*http.Response, error) {
	body, ok := s[r.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

// newPublishedEnv is newAppEnv with the workouts app beside notes, and a
// verifier over example.org's record and a document listing listed.
func newPublishedEnv(t *testing.T, listed ...string) *appEnv {
	e, _ := newPublishedEnvWithDNS(t, listed...)
	return e
}

// newPublishedEnvWithDNS is newPublishedEnv, answering the fixed DNS answers
// too, which a test may change after the page reads a request.
func newPublishedEnvWithDNS(t *testing.T, listed ...string) (*appEnv, domains.Static) {
	t.Helper()
	e := newAppEnv(t)
	e.docs.docs[pubClientID] = authorize.Document{
		ClientID: pubClientID, RedirectURIs: []string{pubRedirect, pubLoopback},
		Name: "Workouts", Authority: pubAuthority,
	}
	apps := make([]string, 0, len(listed))
	for _, c := range listed {
		apps = append(apps, `{"client_id": "`+c+`", "name": "Example Workouts"}`)
	}
	dns := domains.Static{"_substrate.example.org.": {"v=substrate1 doc=" + pubDocURL}}
	verifier := authorize.NewVerifier(authorize.PublisherOptions{
		Resolver: dns,
		Client: &http.Client{Transport: servedFiles{
			pubDocURL: `{"authority": "example.org", "name": "Example", "apps": [` + strings.Join(apps, ",") + `]}`,
		}},
	})
	e.h = New(Config{Service: e.svc, ClientDocuments: e.docs, Publishers: verifier, AuthInterval: time.Nanosecond})
	return e, dns
}

func workoutsRequest(scopes, redirect string) map[string]string {
	p := appRequest(scopes)
	p["client_id"], p["redirect_uri"] = pubClientID, redirect
	return p
}

// grantWorkouts runs the flow for the workouts app and answers its minted
// token's record and secret.
func (e *appEnv) grantWorkouts(scopes, redirect string, approved []string) (substrate.AuthorizationRequest, substrate.TokenInfo, string) {
	e.t.Helper()
	id := e.park(workoutsRequest(scopes, redirect))
	view := e.show(id)
	wantStatus(e.t, view, http.StatusOK)
	page := decodeJSON[substrate.AuthorizationRequest](e.t, view)
	rec := e.approveShown(id, approved, 30)
	wantStatus(e.t, rec, http.StatusOK)
	code := codeFrom(e.t, decodeJSON[substrate.AuthorizationAnswer](e.t, rec).Redirect)
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {pubClientID},
		"redirect_uri": {redirect}, "code_verifier": {appVerifier},
	}
	tok := e.exchange(form)
	wantStatus(e.t, tok, http.StatusOK)
	secret := decodeJSON[tokenResponse](e.t, tok).AccessToken
	return page, e.svc.tokens[secret].info, secret
}

func (e *appEnv) writeActor(secret string) substrate.Actor {
	e.t.Helper()
	rec := e.do(http.MethodPut, "/api/v1/"+taskKind+"/t1", secret, []byte(`{"properties":{}}`), "application/json")
	wantStatus(e.t, rec, http.StatusCreated)
	return e.svc.datasets[fakeRepository].lastActor
}

func TestAVerifiedAppIsShownByItsAuthorityAndWritesAsIt(t *testing.T) {
	e := newPublishedEnv(t, pubClientID)
	page, minted, secret := e.grantWorkouts("write:"+taskKind, pubRedirect, []string{"write:" + taskKind})
	app := page.App
	if !app.Verified || app.Actor != "app:example.org" || app.Publisher.Authority != pubAuthority ||
		app.Publisher.Name != "Example" || app.Publisher.AppName != "Example Workouts" || app.Publisher.Unconfirmed != "" {
		t.Fatalf("a verified app reads %+v", app)
	}
	if app.Publisher.Record != "v=substrate1 doc="+pubDocURL || app.Publisher.DocumentURL != pubDocURL || !app.Publisher.Listed {
		t.Fatalf("the technical details read %+v", app.Publisher)
	}
	if minted.AppAuthority != pubAuthority || minted.AppLocal || minted.ClientName != "Example Workouts" {
		t.Fatalf("the minted token reads %+v", minted)
	}
	if got := e.writeActor(secret); got != "app:example.org" {
		t.Fatalf("a verified app wrote as %q, want app:example.org", got)
	}
}

func TestAnUnlistedAppIsMarkedUnverifiedAndMayStillAskForFull(t *testing.T) {
	e := newPublishedEnv(t)
	// Its redirect is off the client id's origin and nothing verifies it.
	// Nothing is fetched before a sign-in, so the request parks, and the
	// signed-in page refuses it.
	refused := e.show(e.park(workoutsRequest("full", pubRedirect)))
	wantStatus(t, refused, http.StatusUnprocessableEntity)
	if !strings.Contains(refused.Body.String(), "does not list this app") {
		t.Fatalf("the refusal reads %s", refused.Body.String())
	}

	// Its own document on its own origin, claiming the publisher.
	e.docs.docs[appClientID] = authorize.Document{
		ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Notes", Authority: pubAuthority,
	}
	id := e.park(appRequest("full"))
	view := e.show(id)
	wantStatus(t, view, http.StatusOK)
	app := decodeJSON[substrate.AuthorizationRequest](t, view).App
	if app.Verified || app.Actor != "app:notes.example.org" || app.Publisher.Authority != pubAuthority ||
		!strings.Contains(app.Publisher.Unconfirmed, "does not list this app's client id") || app.Publisher.Listed {
		t.Fatalf("an unlisted app reads %+v", app)
	}
	wantStatus(t, e.approveShown(id, []string{"full"}, 30), http.StatusOK)
}

func TestAnAppWhoseAccessGoesToThisDeviceWritesAsLocal(t *testing.T) {
	e := newPublishedEnv(t, pubClientID)
	page, minted, secret := e.grantWorkouts("write:"+taskKind, "http://127.0.0.1:43123/callback", []string{"write:" + taskKind})
	if page.App.Verified || page.App.Actor != "app:local" || !page.App.Publisher.Listed ||
		!strings.Contains(page.App.Publisher.Unconfirmed, "a program on this device") {
		t.Fatalf("a listed app with a loopback redirect reads %+v", page.App)
	}
	if minted.AppAuthority != "" || !minted.AppLocal {
		t.Fatalf("the minted token reads %+v", minted)
	}
	if got := e.writeActor(secret); got != substrate.AppActorLocal {
		t.Fatalf("a loopback app wrote as %q, want app:local", got)
	}
}

func TestTheActorFollowsWhatTheApprovalProved(t *testing.T) {
	for _, c := range []struct {
		clientID, authority string
		local               bool
		want                substrate.Actor
	}{
		{pubClientID, pubAuthority, false, "app:example.org"},
		{pubClientID, "", false, "app:cdn.example.net"},
		{pubClientID, "", true, substrate.AppActorLocal},
		{"http://127.0.0.1:43123/cli", "", false, substrate.AppActorLocal},
		{"", "", false, substrate.ActorAPI},
	} {
		if got := appActorFor(c.clientID, c.authority, c.local); got != c.want {
			t.Errorf("appActorFor(%q, %q, %v) = %q, want %q", c.clientID, c.authority, c.local, got, c.want)
		}
	}
	if !substrate.ReservedActor(substrate.AppActorLocal) {
		t.Fatal("app:local is claimable by a request")
	}
}

func workoutsForm(code, redirect string) url.Values {
	return url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {pubClientID},
		"redirect_uri": {redirect}, "code_verifier": {appVerifier},
	}
}

// A record withdrawn after the page showed the app as verified refuses the
// approval and ends the request: the page keeps what it first read, so only
// a fresh request from the app can show the publisher as it is now, and
// no code exists.
func TestAPublisherWithdrawnAfterThePageViewRefusesTheApproval(t *testing.T) {
	e, dns := newPublishedEnvWithDNS(t, pubClientID)
	id := e.park(workoutsRequest("full", pubRedirect))
	view := e.show(id)
	wantStatus(t, view, http.StatusOK)
	if !decodeJSON[substrate.AuthorizationRequest](t, view).App.Verified {
		t.Fatal("the page did not show the app as verified")
	}
	delete(dns, "_substrate.example.org.")
	rec := e.approveShown(id, []string{"full"}, 30)
	wantStatus(t, rec, http.StatusConflict)
	if body := rec.Body.String(); !strings.Contains(body, "no longer verifies") || !strings.Contains(body, "start again from the app") {
		t.Fatalf("the refusal reads %s", body)
	}
	if len(e.svc.tokens) != 1 {
		t.Fatalf("a refused approval minted a token: %d tokens", len(e.svc.tokens))
	}
	// The ended request is gone; the app's next request reads the
	// publisher as it is now.
	wantStatus(t, e.show(id), http.StatusNotFound)
	again := e.show(e.park(workoutsRequest("full", pubRedirect)))
	wantStatus(t, again, http.StatusUnprocessableEntity)
	if !strings.Contains(again.Body.String(), "publishes no _substrate record") {
		t.Fatalf("the fresh request reads %s", again.Body.String())
	}
}

// A record withdrawn after the approval refuses the exchange: the code is
// spent, and no token is minted as anything the owner did not approve.
func TestAPublisherWithdrawnAfterTheApprovalRefusesTheExchange(t *testing.T) {
	e, dns := newPublishedEnvWithDNS(t, pubClientID)
	id := e.park(workoutsRequest("full", pubRedirect))
	wantStatus(t, e.show(id), http.StatusOK)
	rec := e.approveShown(id, []string{"full"}, 30)
	wantStatus(t, rec, http.StatusOK)
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)
	before := len(e.svc.tokens)
	delete(dns, "_substrate.example.org.")
	tok := e.exchange(workoutsForm(code, pubRedirect))
	wantStatus(t, tok, http.StatusBadRequest)
	if body := tok.Body.String(); !strings.Contains(body, "invalid_grant") || !strings.Contains(body, "no longer verifies") {
		t.Fatalf("the exchange answered %s", body)
	}
	if len(e.svc.tokens) != before {
		t.Fatal("a refused exchange minted a token")
	}
	// Restoring the record does not bring the spent code back.
	dns["_substrate.example.org."] = []string{"v=substrate1 doc=" + pubDocURL}
	wantStatus(t, e.exchange(workoutsForm(code, pubRedirect)), http.StatusBadRequest)
}

// An authority a local file vouches for is shown and minted as such.
func TestAPublisherALocalFileVouchesForSaysSo(t *testing.T) {
	e := newAppEnv(t)
	e.docs.docs[pubClientID] = authorize.Document{
		ClientID: pubClientID, RedirectURIs: []string{pubRedirect}, Name: "Workouts", Authority: pubAuthority,
	}
	path := filepath.Join(t.TempDir(), "authority.json")
	if err := os.WriteFile(path, []byte(`{"authority": "example.org", "name": "Example", "apps": [{"client_id": "`+pubClientID+`", "name": "Example Workouts"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier := authorize.NewVerifier(authorize.PublisherOptions{Resolver: domains.Static{}, LocalDocuments: map[string]string{pubAuthority: path}})
	e.h = New(Config{Service: e.svc, ClientDocuments: e.docs, Publishers: verifier, AuthInterval: time.Nanosecond})
	page, minted, _ := e.grantWorkouts("write:"+taskKind, pubRedirect, []string{"write:" + taskKind})
	if !page.App.Verified || !page.App.Publisher.FromFile || page.App.Actor != "app:example.org" {
		t.Fatalf("a file-vouched app reads %+v", page.App)
	}
	if minted.AppAuthority != pubAuthority || !minted.AppAuthorityFromFile {
		t.Fatalf("the minted token reads %+v", minted)
	}
}
