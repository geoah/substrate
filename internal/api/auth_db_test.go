package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// authChangeOf is the newest changelog entry the engine wrote for one record.
func authChangeOf(t *testing.T, env *dbEnv, kind, id string) substrate.Change {
	t.Helper()
	changes, err := env.ds.Changes(context.Background(), 0, substrate.ChangeFilter{Kinds: []string{kind}, RecordID: id}, 100)
	if err != nil {
		t.Fatalf("changes of %s/%s: %v", kind, id, err)
	}
	if len(changes) == 0 {
		t.Fatalf("no changelog entry for %s/%s", kind, id)
	}
	return changes[len(changes)-1]
}

// authCreate creates one person through the API under token, naming actor
// when it is not empty, and answers the engine's changelog entry for it.
func authCreate(t *testing.T, env *dbEnv, token, name, actor string) substrate.Change {
	t.Helper()
	var headers []string
	if actor != "" {
		headers = []string{actorHeader, actor}
	}
	rec := env.do(t, http.MethodPost, recordsPath, token,
		map[string]any{"kind": personKind, "properties": map[string]any{"name": name}}, headers...)
	wantStatus(t, rec, http.StatusCreated)
	return authChangeOf(t, env, personKind, decodeJSON[substrate.Record](t, rec).ID)
}

// The actor is ATTRIBUTION: the header names what wrote, defaulting to the
// owner, and the token constrains nothing. The engine records it on the
// write's changelog entry.
func TestActorDefaultsToOwnerAndHeaderNamesTheWriter(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))

	if got := authCreate(t, env, env.token, "Ada", "").Actor; got != substrate.ActorAPI {
		t.Fatalf("default actor = %q, want owner", got)
	}
	if got := authCreate(t, env, env.token, "Grace", "gmail.google.connectors.substrate.reamde.dev").Actor; got != "gmail.google.connectors.substrate.reamde.dev" {
		t.Fatalf("header actor = %q", got)
	}
}

// The actor is asserted, the PRINCIPAL is resolved: whatever door a caller
// names, the write carries the id of the token the bearer secret resolved to,
// and the engine stamps that on the changelog entry (#102). Two tokens of the
// same repository writing under the same actor are therefore still told
// apart.
func TestWriteCarriesTheResolvedTokenID(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	ctx := context.Background()
	first, firstSecret, err := env.ds.MintToken(ctx, substrate.MintTokenInput{Label: "first"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	second, secondSecret, err := env.ds.MintToken(ctx, substrate.MintTokenInput{Label: "second"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if got := authCreate(t, env, firstSecret, "Ada", "").Principal; got != first.ID {
		t.Fatalf("principal = %q, want the first token's id %q", got, first.ID)
	}

	// A different token, the same asserted actor: the principal moves, the
	// actor does not.
	change := authCreate(t, env, secondSecret, "Grace", string(substrate.ActorConsole))
	if change.Actor != substrate.ActorConsole {
		t.Fatalf("actor = %q, want console", change.Actor)
	}
	if change.Principal != second.ID {
		t.Fatalf("principal = %q, want the second token's id %q", change.Principal, second.ID)
	}
}

// The header's one refusal is the host's own namespace
// (TestActorHeaderRefusesTheHostNamespace): a name that merely RESEMBLES it
// is ordinary, and the write lands under it.
func TestActorHeaderAdmitsALookalikeOfTheHostNamespace(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	if got := authCreate(t, env, env.token, "fine", "substrateish.example.com").Actor; got != "substrateish.example.com" {
		t.Fatalf("actor = %q, want the lookalike as written", got)
	}
}
