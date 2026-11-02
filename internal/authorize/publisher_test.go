package authorize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/domains"
)

const (
	pubAuthority = "example.org"
	pubClientID  = "https://cdn.example.net/workouts/client.json"
	pubRedirect  = "https://apps.example.org/workouts/callback"
)

func authorityJSON(authority string, clients ...string) string {
	apps := make([]string, 0, len(clients))
	for _, c := range clients {
		apps = append(apps, `{"client_id": "`+c+`", "name": "Example Workouts", "packages": ["training"], "apple": {"appIDs": ["ABCDE12345.org.example.workouts"]}}`)
	}
	return `{"authority": "` + authority + `", "name": "Example", "description": "Example's apps.",
		"packages": [{"package": "training", "versions": [{"version": 1, "closure": "https://cdn.example.net/training-1.yaml", "sha256": "00"}]}],
		"apps": [` + strings.Join(apps, ",") + `]}`
}

// publisherServer serves body as the authority document over TLS and
// counts its fetches.
func publisherServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func claiming(authority string) Document {
	return Document{ClientID: pubClientID, RedirectURIs: []string{pubRedirect}, Name: "Workouts", Authority: authority}
}

func TestAnAppIsVerifiedOnlyByItsAuthoritysRecordAndDocument(t *testing.T) {
	listed := authorityJSON(pubAuthority, pubClientID)
	srv, _ := publisherServer(t, listed)
	docURL := srv.URL + "/.well-known/substrate/authority.json"
	sum := sha256.Sum256([]byte(listed))
	web := Client{ID: pubClientID, Host: "cdn.example.net"}
	local := filepath.Join(t.TempDir(), "authority.json")
	if err := os.WriteFile(local, []byte(authorityJSON("dev.example.com", pubClientID)), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		txt      []string
		client   Client
		doc      Document
		redirect string
		local    map[string]string
		verified bool
		reason   string
		listed   bool
	}{
		{name: "listed", txt: []string{"v=substrate1 doc=" + docURL}, verified: true, listed: true},
		{name: "listed under a pinned digest", txt: []string{"v=substrate1 doc=" + docURL + " sha256=" + hex.EncodeToString(sum[:])}, verified: true, listed: true},
		{name: "redirect on the authority itself", txt: []string{"v=substrate1 doc=" + docURL}, redirect: "https://example.org/cb", verified: true, listed: true},
		{
			name: "unlisted", txt: []string{"v=substrate1 doc=" + docURL}, client: Client{ID: "https://cdn.example.net/other/client.json", Host: "cdn.example.net"},
			doc: Document{ClientID: "https://cdn.example.net/other/client.json", Authority: pubAuthority}, reason: "does not list this app's client id",
		},
		{name: "no record", reason: "publishes no _substrate record"},
		{name: "a record without doc=", txt: []string{"v=substrate1 server=https://substrate.example.net"}, reason: "names no authority document"},
		{name: "two records", txt: []string{"v=substrate1 doc=" + docURL, "v=substrate1 doc=" + docURL}, reason: "malformed"},
		{name: "malformed record", txt: []string{"v=substrate1 doc"}, reason: "malformed"},
		{name: "redirect off the authority", txt: []string{"v=substrate1 doc=" + docURL}, redirect: "https://evil.example.com/cb", reason: "not example.org or a name under it", listed: true},
		{name: "redirect on a name that only ends like it", txt: []string{"v=substrate1 doc=" + docURL}, redirect: "https://notexample.org/cb", reason: "not example.org", listed: true},
		{name: "loopback redirect", txt: []string{"v=substrate1 doc=" + docURL}, redirect: "http://127.0.0.1:43123/cb", reason: "a program on this device", listed: true},
		{name: "loopback client id", client: Client{ID: "http://127.0.0.1:43123/cli", Host: "127.0.0.1:43123", Local: true}, reason: "runs on this device"},
		{name: "sha256 mismatch", txt: []string{"v=substrate1 doc=" + docURL + " sha256=" + strings.Repeat("ab", 32)}, reason: "does not match the sha256"},
		{name: "the document names another authority", txt: []string{"v=substrate1 doc=" + docURL}, doc: claiming("example.com"), reason: "not example.com's"},
		{name: "no claim", doc: Document{ClientID: pubClientID}},
		{name: "a claim that is no authority name", doc: Document{ClientID: pubClientID, RefusedAuthority: "Not A Name"}, reason: "not an authority name"},
		{name: "the development override", doc: claiming("dev.example.com"), redirect: "https://dev.example.com/cb", local: map[string]string{"dev.example.com": local}, verified: true, listed: true},
		{name: "the development override, unreadable", doc: claiming("dev.example.com"), redirect: "https://dev.example.com/cb", local: map[string]string{"dev.example.com": local + ".missing"}, reason: "could not be read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := domains.Static{}
			if tc.txt != nil {
				name := pubAuthority
				if tc.doc.Authority != "" {
					name = tc.doc.Authority
				}
				res["_substrate."+name+"."] = tc.txt
			}
			v := NewVerifier(PublisherOptions{Resolver: res, Client: srv.Client(), LocalDocuments: tc.local})
			c, doc, redirect := tc.client, tc.doc, tc.redirect
			if c.ID == "" {
				c = web
			}
			if doc.ClientID == "" && !c.Local {
				doc = claiming(pubAuthority)
			}
			if redirect == "" {
				redirect = pubRedirect
			}
			p := v.Verify(context.Background(), c, doc, redirect)
			if p.Verified != tc.verified {
				t.Fatalf("Verified = %v, want %v (reason %q)", p.Verified, tc.verified, p.Reason)
			}
			if tc.reason == "" && p.Reason != "" {
				t.Fatalf("reason = %q, want none", p.Reason)
			}
			if !strings.Contains(p.Reason, tc.reason) {
				t.Fatalf("reason = %q, want it to say %q", p.Reason, tc.reason)
			}
			if p.Listed != tc.listed {
				t.Fatalf("Listed = %v, want %v", p.Listed, tc.listed)
			}
			if tc.verified && (p.Name != "Example" || p.AppName != "Example Workouts" || p.Authority != doc.Authority) {
				t.Fatalf("a verified publisher reads %+v", p)
			}
		})
	}
}

func TestAnAuthorityDocumentIsCachedOnlyWhenItWasRead(t *testing.T) {
	srv, hits := publisherServer(t, authorityJSON(pubAuthority, pubClientID))
	now := time.Now()
	res := domains.Static{"_substrate.example.org.": {"v=substrate1 doc=" + srv.URL + "/a.json"}}
	v := NewVerifier(PublisherOptions{Resolver: res, Client: srv.Client(), Now: func() time.Time { return now }})
	c := Client{ID: pubClientID, Host: "cdn.example.net"}
	for range 3 {
		if p := v.Verify(context.Background(), c, claiming(pubAuthority), pubRedirect); !p.Verified {
			t.Fatalf("not verified: %s", p.Reason)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("fetched %d times inside the cache window, want 1", hits.Load())
	}
	now = now.Add(cacheFor + time.Second)
	v.Verify(context.Background(), c, claiming(pubAuthority), pubRedirect)
	if hits.Load() != 2 {
		t.Fatalf("fetched %d times after the window, want 2", hits.Load())
	}

	bad, badHits := publisherServer(t, `{"authority": "example.org", "apps": [], "extra": true}`)
	res["_substrate.example.org."] = []string{"v=substrate1 doc=" + bad.URL + "/a.json"}
	for range 2 {
		if p := v.Verify(context.Background(), c, claiming(pubAuthority), pubRedirect); p.Verified || !strings.Contains(p.Reason, "not one this server reads") {
			t.Fatalf("a document with an unknown key reads %+v", p)
		}
	}
	if badHits.Load() != 2 {
		t.Fatalf("a refused document was fetched %d times, want 2: a failure is never cached", badHits.Load())
	}
}

func TestAClientDocumentsPublisherIsAClaimAndABadOneRefusesOnlyTheClaim(t *testing.T) {
	base := `{"client_id": "` + pubClientID + `", "redirect_uris": ["` + pubRedirect + `"]`
	doc, err := ParseDocument([]byte(base+`, "substrate_authority": "example.org"}`), pubClientID)
	if err != nil || doc.Authority != "example.org" || doc.RefusedAuthority != "" {
		t.Fatalf("a valid claim reads %+v, %v", doc, err)
	}
	for _, bad := range []string{`"Example.Org"`, `"https://example.org"`, `"org"`, `42`} {
		doc, err := ParseDocument([]byte(base+`, "substrate_authority": `+bad+`}`), pubClientID)
		if err != nil {
			t.Fatalf("claim %s failed the document: %v", bad, err)
		}
		if doc.Authority != "" || doc.RefusedAuthority == "" {
			t.Fatalf("claim %s reads %+v", bad, doc)
		}
	}
}

func TestADevelopmentDocumentThatCannotBeReadRefusesTheBoot(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(authorityJSON("dev.example.com")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckLocalDocuments(map[string]string{"dev.example.com": good}); err != nil {
		t.Fatal(err)
	}
	if err := CheckLocalDocuments(map[string]string{"dev.example.com": filepath.Join(dir, "missing.json")}); err == nil {
		t.Fatal("a missing file was admitted")
	}
	if err := CheckLocalDocuments(map[string]string{"other.example.com": good}); err == nil {
		t.Fatal("a document naming another authority was admitted")
	}
}
