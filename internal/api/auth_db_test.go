package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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

// authCreate creates one person through the API under token, sending the
// retired actor header with actor when it is not empty, and answers the
// engine's changelog entry for it.
func authCreate(t *testing.T, env *dbEnv, token, name, actor string) substrate.Change {
	t.Helper()
	var headers []string
	if actor != "" {
		headers = []string{retiredActorHeader, actor}
	}
	rec := env.do(t, http.MethodPost, recordsPath, token,
		map[string]any{"kind": personKind, "properties": map[string]any{"name": name}}, headers...)
	wantStatus(t, rec, http.StatusCreated)
	return authChangeOf(t, env, personKind, decodeJSON[substrate.Record](t, rec).ID)
}

// A full-access token writes as the repository's person, and no request
// names its actor (decision 0270): the header a client once named one by is
// not read, so a claim of the substrate's own hands, which the door used to
// refuse, lands as the person's write like any other.
func TestAFullAccessTokenWritesAsThePerson(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	person := vocabulary.ActorID(substrate.ActorPerson)
	for _, claim := range []string{
		"", "console", "gmail.google.connectors.substrate.reamde.dev",
		"substrate", "substrate.oauth", "bundle:samples.substrate.reamde.dev:tasks", "app:notes.example.org",
	} {
		change := authCreate(t, env, env.token, "Ada "+claim, claim)
		if change.Actor != person || change.OnBehalfOf != "" {
			t.Fatalf("a write claiming %q: actor %q on behalf of %q, want the person %q on behalf of no one",
				claim, change.Actor, change.OnBehalfOf, person)
		}
	}
}

// The write carries the id of the token the bearer secret resolved to, and
// the engine stamps it on the changelog entry (#102). Two tokens of the same
// repository, both the person's, are therefore still told apart.
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

	if got := authCreate(t, env, firstSecret, "Ada", "").Token; got != first.ID {
		t.Fatalf("token = %q, want the first token's id %q", got, first.ID)
	}

	// A different token, the same person: the token moves, the actor does
	// not.
	change := authCreate(t, env, secondSecret, "Grace", "")
	if change.Actor != vocabulary.ActorID(substrate.ActorPerson) {
		t.Fatalf("actor = %q, want the person", change.Actor)
	}
	if change.Token != second.ID {
		t.Fatalf("token = %q, want the second token's id %q", change.Token, second.ID)
	}
}
