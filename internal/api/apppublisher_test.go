package api

import (
	"io"
	"net/http"
	"net/url"
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
	verifier := authorize.NewVerifier(authorize.PublisherOptions{
		Resolver: domains.Static{"_substrate.example.org.": {"v=substrate1 doc=" + pubDocURL}},
		Client: &http.Client{Transport: servedFiles{
			pubDocURL: `{"authority": "example.org", "name": "Example", "apps": [` + strings.Join(apps, ",") + `]}`,
		}},
	})
	e.h = New(Config{Service: e.svc, ClientDocuments: e.docs, Publishers: verifier, AuthInterval: time.Nanosecond})
	return e
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
	// Its redirect is off the client id's origin and nothing verifies it, so
	// the request is never parked.
	rec := e.do(http.MethodGet, authorizeURL(workoutsRequest("full", pubRedirect)), "", nil, "")
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "does not list this app") {
		t.Fatalf("the refusal reads %s", rec.Body.String())
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
