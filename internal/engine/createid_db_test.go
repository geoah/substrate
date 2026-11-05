package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A person, an app or an agent creating a record does not name it: a slug
// chosen for what the record says stops matching once the record is
// retitled (decision 0228). The create takes a minted id, which the writer
// may mint or derive itself; updating a record by its id is not naming it;
// a writer that derives ids from data keeps doing so; an internal kind's id
// is a name. A seeded kind is held to it like an installed one.
func TestANewRecordTakesAMintedID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	const task = "samples.substrate.reamde.dev/tasks/task"
	props := func(name string) map[string]any { return map[string]any{"name": name} }

	for _, actor := range []substrate.Actor{
		substrate.ActorAPI, substrate.ActorConsole, substrate.ActorCLI,
		substrate.AppActor("workouts.example.com"),
		substrate.AgentActor("crew.example.com", "crew", "coach"),
	} {
		_, err := ds.Put(ctx, actor, substrate.PutInput{Kind: task, ID: "floor-first-full-body", Properties: props("Floor first")})
		wantErr(t, err, substrate.ErrValidation, string(actor)+" naming a new task")
	}
	// A seeded kind is no exception: an app writing a seeded source under a
	// readable id is refused, and under one derived from its key admitted.
	const source = "substrate.reamde.dev/observations/source"
	app := substrate.AppActor("sync.example.com")
	_, err := ds.Put(ctx, app, substrate.PutInput{Kind: source, ID: "apple-watch", Properties: map[string]any{"name": "Watch"}})
	wantErr(t, err, substrate.ErrValidation, "an app naming a new seeded source")
	derived := vocabulary.DerivedMintedID("apple", "device-42")
	if got := mustPut(t, ds, app, substrate.PutInput{Kind: source, ID: derived, Properties: map[string]any{"name": "Watch"}}); got.ID != derived {
		t.Fatalf("created %q, want the derived %q", got.ID, derived)
	}

	// No id: the substrate mints one.
	minted := mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: props("Floor first")})
	if !vocabulary.MintedID(minted.ID) {
		t.Fatalf("minted %q", minted.ID)
	}

	// An id in the minted shape, minted by the writer (an offline outbox).
	const own = "01h2xcejqtf2nbrexx3vqjhp41"
	if got := mustPut(t, ds, app, substrate.PutInput{Kind: task, ID: own, Properties: props("Offline")}); got.ID != own {
		t.Fatalf("created %q, want %q", got.ID, own)
	}

	// Writers that derive ids from data keep naming their records.
	fn := substrate.FunctionActor("providers.example.com", "sync", "pull")
	named := mustPut(t, ds, fn, substrate.PutInput{Kind: task, ID: "upstream-1047392", Properties: props("Synced")})
	for _, actor := range []substrate.Actor{substrate.ActorSystem, substrate.BundleActor("samples.substrate.reamde.dev", "tasks")} {
		mustPut(t, ds, actor, substrate.PutInput{Kind: task, ID: "by-" + string(actor[:6]), Properties: props("Seeded")})
	}

	// Addressing a record that exists is an update, whoever wrote it.
	if got := mustPut(t, ds, owner, substrate.PutInput{Kind: task, ID: named.ID, Properties: props("Renamed")}); got.Properties["name"] != "Renamed" {
		t.Fatalf("update by id wrote %v", got.Properties["name"])
	}

	// An internal kind's id is a name the substrate resolves by.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/llm/provider", ID: "mistral",
		Properties: map[string]any{"label": "Mistral", "wire": "openai"},
	})
}
