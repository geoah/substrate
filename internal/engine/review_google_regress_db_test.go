package engine_test

// Regression gate for the Google-connector adversarial review
//, findings 1-4:
//
//   #1 — provider endpoints and scopes are TRUSTED manifest metadata, not
//        mutable config-record properties: a config patch cannot redirect a
//        token exchange, and the requested scopes derive from the account's
//        enabled toggles (an unwired feature requests nothing).
//   #2 — external create/delete of config/account records and oauth/start are
//        owner-only, and per-property `writer:` ownership is enforced
//        server-side after the merged row is known.
//   #3 — the OAuth clientSecret is sealed at rest: a raw database read never
//        shows the plaintext.
//   #4 — a function's injected config omits the clientSecret and tokenRef.

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// #1: a config patch that sets a would-be endpoint property does NOT redirect
// the flow — the consent URL and the token exchange both use the manifest's
// (fake-provider) endpoints. Scopes derive from the account's toggles.
func TestReviewGoogleEndpointsFromManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, ops, p, account := installOAuthBundle(t)

	// An authenticated caller patches a would-be token endpoint on the config.
	// The property still exists on the mail config type, but the engine ignores
	// it — endpoints come only from the manifest.
	mustPatch(t, ds, owner, mbConfigType, mbConfigOf(t, ds), substrate.PatchInput{
		Properties: map[string]any{
			"authorizationEndpoint": "https://evil.example/authorize",
			"tokenEndpoint":         "https://evil.example/token",
		},
	})

	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasPrefix(consent, p.ts.URL+"/authorize") {
		t.Fatalf("consent url was redirected off the manifest endpoint: %s", consent)
	}
	// Scopes derive from the account's enabled toggles (enabledMail=true).
	if scope := scopeOf(t, consent); !strings.Contains(scope, "mail.read") {
		t.Fatalf("consent scope not derived from toggles: %q", scope)
	}

	// The code exchange lands on the MANIFEST token endpoint (the fake
	// provider), never the patched evil one — the exchange succeeds.
	if _, err := svc.(substrate.OAuthCompleter).CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback (exchange must hit the manifest endpoint): %v", err)
	}
	if p.exchanges != 1 {
		t.Fatalf("the exchange did not reach the manifest (fake) provider: %d", p.exchanges)
	}
}

// #1: a disabled toggle requests NO scope — an unwired feature (here, a
// toggled-off enabledMail) is never requested.
func TestReviewGoogleScopeFromTogglesOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, ops, _, account := installOAuthBundle(t)

	mustPatch(t, ds, owner, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"enabledMail": false},
	})
	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if scope := scopeOf(t, consent); scope != "" {
		t.Fatalf("a disabled toggle still requested scope %q", scope)
	}
}

// #2: oauth/start is owner-gated — a bundle (connector) actor is refused.
func TestReviewGoogleOAuthStartOwnerGated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, ops, _, account := installOAuthBundle(t)
	_ = ds
	const connector = substrate.Actor("function:echo")
	_, err := ops.StartOAuth(ctx, connector, account.ID)
	wantErr(t, err, substrate.ErrForbidden, "non-owner oauth/start")
}

// #2: external create/delete of a config/account record is owner-only.
func TestReviewGoogleConfigAccountCreateOwnerGated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, _, account := installOAuthBundle(t)
	const connector = substrate.Actor("function:echo")

	if _, err := ds.Put(ctx, connector, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "intruder@example.com"},
	}); err == nil {
		t.Fatal("a non-owner created an account record")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "non-owner account create")
	}
	if _, err := ds.Delete(ctx, connector, account.Kind, account.ID); err == nil {
		t.Fatal("a non-owner deleted an account record")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "non-owner account delete")
	}
}

// #2: per-property ownership — tokenRef/tokenStatus are the OAuth facility's;
// syncToken is the connector's; the owner writes neither. The owner writes the
// account's email and toggles.
func TestReviewGooglePropertyOwnership(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, _, account := installOAuthBundle(t)

	for _, prop := range []map[string]any{
		{"tokenRef": "forged-ref"},
		{"tokenStatus": "connected"},
		{"syncToken": "forged-sync"},
	} {
		if _, err := ds.Patch(ctx, owner, account.Kind, account.ID, substrate.PatchInput{Properties: prop}); err == nil {
			t.Fatalf("owner wrote a host/connector-owned property %v", prop)
		} else {
			wantErr(t, err, substrate.ErrForbidden, "owner writes a restricted property")
		}
	}
	// A connector actor may write its own sync state, but not the owner's
	// account settings.
	const connector = substrate.Actor("function:echo")
	if _, err := ds.Patch(ctx, connector, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"syncToken": "s1"},
	}); err != nil {
		t.Fatalf("the connector could not write its own syncToken: %v", err)
	}
	if _, err := ds.Patch(ctx, connector, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"address": "hijack@example.com"},
	}); err == nil {
		t.Fatal("the connector wrote an owner-owned property")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "connector writes an owner property")
	}
	// The owner writes what is theirs.
	mustPatch(t, ds, owner, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"address": "owner@example.com", "enabledMail": true},
	})
}

// #3: the clientSecret never sits in the record's JSONB — the stored value
// is a sealed-store ref, and a raw database read shows no plaintext.
func TestReviewGoogleClientSecretSealedAtRest(t *testing.T) {
	t.Parallel()
	_, _, db, _, _, _ := installW3OAuthBundle(t)
	var props string
	if err := db.QueryRow(
		`SELECT props::text FROM records WHERE kind = $1 AND deleted_at IS NULL`, mbConfigType).
		Scan(&props); err != nil {
		t.Fatalf("read config props: %v", err)
	}
	if strings.Contains(props, "s3cret") {
		t.Fatalf("the client secret is plaintext at rest: %s", props)
	}
	if !strings.Contains(props, `"clientSecret": "secret:`) &&
		!strings.Contains(props, `"clientSecret":"secret:`) {
		t.Fatalf("the client secret is not a sealed-store ref: %s", props)
	}
}

// mbConfigOf returns the id of the bundle's single live config record.
func mbConfigOf(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{mbConfigType}},
	})
	if err != nil || len(page.Records) == 0 {
		t.Fatalf("no config record: %v", err)
	}
	return page.Records[0].ID
}

// scopeOf lifts the space-joined scope out of a consent URL.
func scopeOf(t *testing.T, consentURL string) string {
	t.Helper()
	u, err := url.Parse(consentURL)
	if err != nil {
		t.Fatalf("parse consent url: %v", err)
	}
	return u.Query().Get("scope")
}
