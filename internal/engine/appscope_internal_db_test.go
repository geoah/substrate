package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A token whose stored grant is damaged fails closed: read as absent, it
// would be full access. Only a MISSING `scopes` property is full access; a
// JSON null, an empty list, a non-string entry or a scope that does not
// parse are all refused at Authenticate, and the listing refuses rather than
// showing the token as full access.
func TestADamagedGrantNeverReadsAsFullAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	for name, damage := range map[string]string{
		"null":       `null`,
		"empty":      `[]`,
		"not a list": `"read:*"`,
		"non-string": `[null]`,
		"unparsed":   `["everything"]`,
		"unresolved": `["read:~/tasks/*"]`,
		"auth kind":  `["read:substrate.reamde.dev/core/token"]`,
	} {
		t.Run(name, func(t *testing.T) {
			tok, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{
				Label: "app", Scopes: []string{"read:ada.example.com/tasks/*"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ds.svc.maint.ExecContext(ctx,
				`UPDATE records SET props = jsonb_set(props, '{scopes}', $1::jsonb) WHERE kind = $2 AND id = $3`,
				damage, kindToken, tok.ID); err != nil {
				t.Fatalf("damage the grant: %v", err)
			}
			if _, _, err := ds.svc.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
				t.Fatalf("a token whose scopes are %s authenticated: %v", damage, err)
			}
			if _, err := ds.Tokens(ctx); !errors.Is(err, substrate.ErrCorrupt) {
				t.Fatalf("the listing with a damaged grant = %v, want it refused as corrupt", err)
			}
			if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, tok.ID, substrate.DeleteInput{}); err != nil {
				t.Fatalf("the owner revokes the damaged token: %v", err)
			}
		})
	}
}

// An app's actor holds at the owner tier, however the registry reads, and
// its request goes ahead of background writers at the write gate like the
// owner's own; the owner-only actions stay shut through txn.scoped.
func TestAnAppsActorHoldsAtTheOwnerTierAndWritesInTheForeground(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	app := substrate.AppActor("notes.example.org")
	if got := ds.actorTier(app); got != substrate.TierOwner {
		t.Fatalf("an app's actor resolves to the %s tier, want owner", got)
	}
	if !ds.ownerFirst(ctx, app, false) {
		t.Fatal("an app's write waits behind background writers")
	}
	if ds.ownerFirst(ctx, substrate.Actor("function:x.example.com:p:f"), false) {
		t.Fatal("installed code went ahead of background writers: the test proves nothing")
	}
}

// An app's search scores a hit by that record alone: records of kinds it
// cannot read move the owner's score (their words change a term's weight and
// the average length) and leave the app's where it was.
func TestAnAppsSearchScoresAHitByItselfAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "a", "flight plan", "")
	app := substrate.WithScopes(ctx, []string{"read:" + scoreTaskKind})
	score := func(ctx context.Context) float64 {
		t.Helper()
		res, err := ds.Search(ctx, substrate.SearchInput{Q: "flight", Mode: substrate.SearchLexical, Kinds: []string{scoreTaskKind}})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range res.Hits {
			if h.Record != nil && h.Record.ID == "a" {
				return h.Lexical
			}
		}
		t.Fatalf("the search found no hit a: %+v", res.Hits)
		return 0
	}
	ownerBefore, appBefore := score(ctx), score(app)
	const hidden = "samples.substrate.reamde.dev/tasks/project"
	for i := range 40 {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: hidden, ID: fmt.Sprintf("p%d", i),
			Properties: map[string]any{"name": "flight flight " + strings.Repeat("long hidden words ", 30)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if score(ctx) == ownerBefore {
		t.Fatal("the hidden records left the owner's score where it was: the test proves nothing")
	}
	if got := score(app); got != appBefore {
		t.Fatalf("an app's score moved from %v to %v with records it cannot read", appBefore, got)
	}
}

// A chat an app opens under an `agents:` scope (decision 0195) is the app's
// message and the agent's turn: the API clears the scoped mark before the
// engine, the user message is written under the app's actor, and the app
// renames the thread under its mark as the owner would.
func TestAChatAnAppOpensIsItsMessageAndItsThreadToRename(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "hello"})
	app := substrate.WithScopes(ctx, []string{"agents:*"})
	actor := substrate.AppActor("notes.example.org")

	res, err := ds.ChatAgent(substrate.WithoutScopes(app), actor, crewPackage+"/chatter", "", "hi", nil)
	if err != nil {
		t.Fatalf("the app's chat: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("the turn settled %+v", res)
	}
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{Kinds: []string{typeMessage}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 || changes[0].Actor != actor {
		t.Fatalf("the first message row = %+v, want the user message under %s", changes, actor)
	}
	if _, err := ds.Patch(app, actor, typeThread, res.Thread, substrate.PatchInput{
		Properties: map[string]any{"subject": "Renamed"},
	}); err != nil {
		t.Fatalf("the app's rename: %v", err)
	}
	if got := threadProps(t, ds, res.Thread)["subject"]; got != "Renamed" {
		t.Fatalf("the thread's subject = %v", got)
	}
}
