package engine_test

// Regression gate for the connector "Add account" form fixes: a property may
// carry a human `displayName` and a string `enum` of allowed `values`, both
// survive the kind read the console/GraphQL consume, an out-of-enum
// value is rejected on write, and an account's email is populated by the OAuth
// facility from the grant (writer: oauth) rather than typed by the owner.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// displayName + enum values survive a round-trip on the type read, and an
// out-of-set enum value is refused on write.
func TestConnectorFormDisplayNameAndEnumSurviveTypeRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.AuthorityManifest(swAuthority, 0),
		swTypeDoc("gizmo", "gizmos", map[string]any{
			"cadence": map[string]any{
				"type":        "enum",
				"values":      []any{"off", "hourly", "daily"},
				"displayName": "Sync frequency",
				"description": "how often to sync",
			},
		}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The type read (the projection the console/GraphQL get) carries both the
	// human label and the enum's allowed values.
	row := mustGet(t, ds, "core.substrate.reamde.dev/kind", swAuthority+"/gizmo")
	props, _ := row.Properties["properties"].(map[string]any)
	cadence, _ := props["cadence"].(map[string]any)
	if cadence["displayName"] != "Sync frequency" {
		t.Fatalf("displayName did not survive the type read: %v", cadence)
	}
	if cadence["description"] != "how often to sync" {
		t.Fatalf("description did not survive the type read: %v", cadence)
	}
	// Enum values reach the console AS AUTHORED — bare scalars stay bare, a
	// labeled mapping stays labeled — because the row stores the declaration and
	// the declaration is what the author wrote. Both spellings are read by every
	// consumer of the stored form (the console's parseEnumValues).
	vals, _ := cadence["values"].([]any)
	if len(vals) != 3 {
		t.Fatalf("enum values did not survive the type read: %v", cadence["values"])
	}
	if vals[0] != "off" || vals[2] != "daily" {
		t.Fatalf("the stored enum values were rewritten: %#v", cadence["values"])
	}

	// A value in the set is accepted; one outside it is rejected on write.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: swAuthority + "/gizmo", Properties: map[string]any{"cadence": "daily"},
	})
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: swAuthority + "/gizmo", Properties: map[string]any{"cadence": "weekly"},
	})
	wantErr(t, err, substrate.ErrValidation, "out-of-enum value")
}

// The account's email is host-managed (writer: oauth): the owner cannot type
// it, the account is created without it, and the OAuth facility fills it from
// the connected Google account after the exchange.
func TestOAuthPopulatesAccountEmailFromGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	svc, ds := newDataset(t,
		engine.WithOAuth("test-state-key", "https://substrate.example/api/v1/core.substrate.reamde.dev/oauth/callback", p.ts.Client()),
		engine.WithCredentialKey("test-cred-key"),
	)
	docs := mbStandardDocs()
	mbWireEmail(docs)
	mbPointOAuthAt(docs, p.ts.URL)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: p.configProps()})

	// The owner cannot set the host-managed writer: oauth properties — not the
	// email (which comes from the grant) nor the granted scope set.
	for _, prop := range []map[string]any{
		{"email": "owner-typed@example.com", "enabledMail": true},
		{"grantedScopes": []any{"forged"}, "enabledMail": true},
	} {
		if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: mbAccountType, Properties: prop}); err == nil {
			t.Fatalf("owner set a writer: oauth property %v", prop)
		} else {
			wantErr(t, err, substrate.ErrForbidden, "owner writes a writer: oauth property")
		}
	}

	// The account is creatable WITHOUT an email — the user never types it.
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"enabledMail": true},
	})
	if account.Properties["email"] != nil {
		t.Fatalf("account minted with an email before any grant: %v", account.Properties["email"])
	}

	ops := bundler(t, ds)
	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	oc, ok := svc.(oauthCompleter)
	if !ok {
		t.Fatal("service does not implement the oauth completer seam")
	}
	if _, err := oc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}

	// The facility set the email from the connected account (the fake /userinfo
	// primary address), as its own actor — an owner never could.
	got := mustGet(t, ds, account.Kind, account.ID)
	if got.Properties["email"] != "connected@example.com" {
		t.Fatalf("email not populated from the grant: %v", got.Properties["email"])
	}
	if got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("tokenStatus: %v", got.Properties["tokenStatus"])
	}
}
