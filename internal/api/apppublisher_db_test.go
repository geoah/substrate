package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
)

// newPublishedEnv is the flow on the engine with the workouts app beside
// notes, and a verifier over example.org's record and a document listing
// listed.
func newPublishedEnv(t *testing.T, listed ...string) *authorizeEnv {
	e, _ := newPublishedEnvWithDNS(t, listed...)
	return e
}

// newPublishedEnvWithDNS is newPublishedEnv, answering the fixed DNS answers
// too, which a test may change after the page reads a request.
func newPublishedEnvWithDNS(t *testing.T, listed ...string) (*authorizeEnv, domains.Static) {
	t.Helper()
	verifier, dns := publishedVerifier(listed...)
	e := newAuthorizeEnv(t, withSamples("tasks"), withConfig(func(c *Config) { c.Publishers = verifier }))
	e.docs.docs[pubClientID] = workoutsDoc
	return e, dns
}

// grantWorkouts runs the flow for the workouts app and answers its minted
// token's record and secret.
func (e *authorizeEnv) grantWorkouts(scopes, redirect string, approved []string) (substrate.AuthorizationRequest, substrate.TokenInfo, string) {
	e.t.Helper()
	id := e.park(workoutsRequest(scopes, redirect))
	view := e.show(id)
	wantStatus(e.t, view, http.StatusOK)
	page := decodeJSON[substrate.AuthorizationRequest](e.t, view)
	rec := e.approveShown(id, approved, 30)
	wantStatus(e.t, rec, http.StatusOK)
	code := codeFrom(e.t, decodeJSON[substrate.AuthorizationAnswer](e.t, rec).Redirect)
	tok := e.exchange(workoutsForm(code, redirect))
	wantStatus(e.t, tok, http.StatusOK)
	secret := decodeJSON[tokenResponse](e.t, tok).AccessToken
	return page, e.token(secret), secret
}

// writeActor writes a task with the token and answers the actor the engine
// recorded on it.
func (e *authorizeEnv) writeActor(secret string) substrate.Actor {
	e.t.Helper()
	id := mintedID("t1")
	rec := e.putAs(secret, taskKind, id, `{"properties":{}}`)
	wantStatus(e.t, rec, http.StatusCreated)
	return e.lastActor(taskKind, id)
}

func TestAVerifiedAppIsShownByItsAuthorityAndWritesAsIt(t *testing.T) {
	t.Parallel()
	e := newPublishedEnv(t, pubClientID)
	page, minted, secret := e.grantWorkouts("write:"+taskKind, pubRedirect, []string{"write:" + taskKind})
	app := page.App
	if !app.Verified || app.AppearsAs != "example.org" || app.Publisher.Authority != pubAuthority ||
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

func TestAnAppWhoseAccessGoesToThisDeviceWritesAsLocal(t *testing.T) {
	t.Parallel()
	e := newPublishedEnv(t, pubClientID)
	page, minted, secret := e.grantWorkouts("write:"+taskKind, "http://127.0.0.1:43123/callback", []string{"write:" + taskKind})
	if page.App.Verified || page.App.AppearsAs != "local" || !page.App.Publisher.Listed ||
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

// A record withdrawn after the page showed the app as verified refuses the
// approval and ends the request: the page keeps what it first read, so only
// a fresh request from the app can show the publisher as it is now, and
// no code exists.
func TestAPublisherWithdrawnAfterThePageViewRefusesTheApproval(t *testing.T) {
	t.Parallel()
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
	if n := len(e.tokens()); n != 1 {
		t.Fatalf("a refused approval minted a token: %d tokens", n)
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
	t.Parallel()
	e, dns := newPublishedEnvWithDNS(t, pubClientID)
	id := e.park(workoutsRequest("full", pubRedirect))
	wantStatus(t, e.show(id), http.StatusOK)
	rec := e.approveShown(id, []string{"full"}, 30)
	wantStatus(t, rec, http.StatusOK)
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)
	before := len(e.tokens())
	delete(dns, "_substrate.example.org.")
	tok := e.exchange(workoutsForm(code, pubRedirect))
	wantStatus(t, tok, http.StatusBadRequest)
	if body := tok.Body.String(); !strings.Contains(body, "invalid_grant") || !strings.Contains(body, "no longer verifies") {
		t.Fatalf("the exchange answered %s", body)
	}
	if len(e.tokens()) != before {
		t.Fatal("a refused exchange minted a token")
	}
	// Restoring the record does not bring the spent code back.
	dns["_substrate.example.org."] = []string{"v=substrate1 doc=" + pubDocURL}
	wantStatus(t, e.exchange(workoutsForm(code, pubRedirect)), http.StatusBadRequest)
	if len(e.tokens()) != before {
		t.Fatal("a spent code minted a token")
	}
}

// An authority a local file vouches for is shown and minted as such.
func TestAPublisherALocalFileVouchesForSaysSo(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "authority.json")
	if err := os.WriteFile(path, []byte(`{"authority": "example.org", "name": "Example", "apps": [{"client_id": "`+pubClientID+`", "name": "Example Workouts"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier := authorize.NewVerifier(authorize.PublisherOptions{Resolver: domains.Static{}, LocalDocuments: map[string]string{pubAuthority: path}})
	e := newAuthorizeEnv(t, withSamples("tasks"), withConfig(func(c *Config) { c.Publishers = verifier }))
	e.docs.docs[pubClientID] = authorize.Document{
		ClientID: pubClientID, RedirectURIs: []string{pubRedirect}, Name: "Workouts", Authority: pubAuthority,
	}
	page, minted, _ := e.grantWorkouts("write:"+taskKind, pubRedirect, []string{"write:" + taskKind})
	if !page.App.Verified || !page.App.Publisher.FromFile || page.App.AppearsAs != "example.org" {
		t.Fatalf("a file-vouched app reads %+v", page.App)
	}
	if minted.AppAuthority != pubAuthority || !minted.AppAuthorityFromFile {
		t.Fatalf("the minted token reads %+v", minted)
	}
}
