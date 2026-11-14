package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An actor record a transaction wrote and then rolled back is not cached as
// standing: the next write by the hand writes it again, so no committed
// write links a record the store does not hold (decision 0270).
func TestARolledBackActorRecordIsWrittenAgain(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()
	app := substrate.AppActor("rollback.example.org")
	id := actorID(app)

	rollback := errors.New("the install fails after its first declaration")
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.ensureActors(app); err != nil {
			return err
		}
		// A second ask in the same transaction finds the record it wrote.
		if err := t.ensureActors(app); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("the transaction ended %v, want the rollback", err)
	}
	if ds.actorLive(id) {
		t.Fatal("a rolled-back actor record is cached as standing")
	}
	if _, err := ds.Get(ctx, kindActor, id); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the rolled-back actor record reads %v, want not found", err)
	}

	_ = putAs(t, ds, app, substrate.PutInput{
		Kind: typeTierProfile, Properties: map[string]any{"name": "after the retry"},
	})
	rec, err := ds.Get(ctx, kindActor, id)
	if err != nil || rec.Properties["type"] != substrate.ActorTypeApp {
		t.Fatalf("the app's write linked %s, whose record reads %+v, %v", id, rec, err)
	}
	if !ds.actorLive(id) {
		t.Fatal("a committed actor record is not cached")
	}
}

// A function or an agent an update of its package removes is a hand whose
// code is gone, exactly as an uninstall leaves it: its actor record is
// disabled in the update's transaction, and declaring it again enables it.
func TestAnUpdateThatRemovesACallableDisablesItsActor(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()
	const pkg = "callables.test.dev/callables"
	fn := func(name string) map[string]any {
		return vocabulary.FunctionManifest(pkg, name, map[string]any{
			"description": "a callable an update may remove",
			"runtime":     vocabulary.RuntimePython,
			"source":      "def main(input, host):\n    return {}\n",
		})
	}
	ag := vocabulary.AgentManifest(pkg, "helper", map[string]any{
		"description": "an agent an update may remove", "provider": "anthropic", "model": "claude-x",
		"prompt": "You help.",
	})
	// A whole-package apply, as an update of the package is: the bundle
	// document lists what the package holds, and the rest is pruned.
	apply := func(docs ...map[string]any) {
		t.Helper()
		var installs []any
		for _, d := range docs {
			installs = append(installs, d["metadata"].(map[string]any)["id"])
		}
		batch := append([]map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.BundleManifest(pkg, map[string]any{"description": "a package an update shrinks", "installs": installs}),
		}, docs...)
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, batch); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	disabled := func(key substrate.Actor) bool {
		t.Helper()
		rec, err := ds.Get(ctx, kindActor, actorID(key))
		if err != nil {
			t.Fatalf("read the actor of %s: %v", key, err)
		}
		return rec.Properties["disabled"] == true
	}
	kept := substrate.FunctionActor("callables.test.dev", "callables", "kept")
	dropped := substrate.FunctionActor("callables.test.dev", "callables", "dropped")
	agent := substrate.AgentActor("callables.test.dev", "callables", "helper")

	apply(fn("kept"), fn("dropped"), ag)
	for _, key := range []substrate.Actor{kept, dropped, agent} {
		if disabled(key) {
			t.Fatalf("%s is disabled at its install", key)
		}
	}
	apply(fn("kept"))
	if disabled(kept) {
		t.Fatal("the update disabled the function it kept")
	}
	for _, key := range []substrate.Actor{dropped, agent} {
		if !disabled(key) {
			t.Fatalf("the update removed %s and left its actor enabled", key)
		}
		if ds.actorLive(actorID(key)) {
			t.Fatalf("the update removed %s and its actor stays cached as standing", key)
		}
	}
	apply(fn("kept"), fn("dropped"), ag)
	for _, key := range []substrate.Actor{dropped, agent} {
		if disabled(key) {
			t.Fatalf("declaring %s again left its actor disabled", key)
		}
	}
}

func putAs(t *testing.T, ds *dataset, actor substrate.Actor, in substrate.PutInput) *substrate.Record {
	t.Helper()
	rec, err := ds.Put(context.Background(), actor, in)
	if err != nil {
		t.Fatalf("put as %s: %v", actor, err)
	}
	return rec
}

// Repository migration 9 names the hand behind every id the history links,
// a record the collector purged included: an app whose only token was
// revoked and purged is read back from the history of that token, and its
// actor record is written disabled.
func TestTheActorMigrationNamesAnAppWhoseTokenWasPurged(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	// A token purged by a release before the erasure, whose history stays.
	ds := newTierDataset(t, WithTestPurgeKeepsHistory())
	ctx := context.Background()
	tok, _, err := ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "purged", ClientID: "https://purged.example.org/client.json", ClientName: "Purged",
		Scopes: []string{"write:" + typeTierProfile},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := substrate.AppActor("purged.example.org")
	putAs(t, ds, app, substrate.PutInput{Kind: typeTierProfile, Properties: map[string]any{"name": "by the app"}})
	if _, err := ds.Delete(ctx, substrate.ActorPerson, kindToken, tok.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("revoke and purge: %v", err)
	}
	if _, err := ds.Get(ctx, kindToken, tok.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the token reads %v after its purge, want not found", err)
	}
	// A repository from before the actor records holds none for the app.
	id := actorID(app)
	if _, err := ds.db.ExecContext(ctx, `DELETE FROM records WHERE kind = $1 AND id = $2`, kindActor, id); err != nil {
		t.Fatal(err)
	}
	ds.forgetActorLive(id)

	var did string
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		var err error
		did, err = migrateLinkActors(t)
		return err
	}); err != nil {
		t.Fatalf("migration 9: %v", err)
	}
	rec, err := ds.Get(ctx, kindActor, id)
	if err != nil || rec.Properties["type"] != substrate.ActorTypeApp ||
		rec.Properties["publisher"] != "purged.example.org" || rec.Properties["disabled"] != true {
		t.Fatalf("the purged app's actor record = %+v, %v (the migration %s)", rec, err, did)
	}
}
