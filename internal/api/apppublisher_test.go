package api

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
)

// Who publishes an app (decision 0209), the handler's half: what the page
// shows of an unverified one. What a verified approval mints and what its
// token writes as is the engine's, in apppublisher_db_test.go.

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

// workoutsDoc is the workouts app's own document, naming example.org as its
// publisher.
var workoutsDoc = authorize.Document{
	ClientID: pubClientID, RedirectURIs: []string{pubRedirect, pubLoopback},
	Name: "Workouts", Authority: pubAuthority,
}

// publishedVerifier verifies example.org over the fixed DNS answers it also
// answers, which a test may change after the page reads a request, and a
// document listing listed.
func publishedVerifier(listed ...string) (*authorize.Verifier, domains.Static) {
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
	return verifier, dns
}

func workoutsRequest(scopes, redirect string) map[string]string {
	p := appRequest(scopes)
	p["client_id"], p["redirect_uri"] = pubClientID, redirect
	return p
}

func TestAnUnlistedAppIsMarkedUnverifiedAndMayStillAskForFull(t *testing.T) {
	verifier, _ := publishedVerifier()
	e := newAuthorizeStub(t, func(c *Config) { c.Publishers = verifier })
	e.docs.docs[pubClientID] = workoutsDoc
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
