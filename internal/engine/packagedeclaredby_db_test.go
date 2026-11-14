package engine_test

// A package row names the actor that first declared it (decision record
// 0109): the engine stamps the transaction's actor on the row it creates,
// keeps it across every later apply, and no document may write it.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const kindPackageRef = "substrate.reamde.dev/core/package"

func TestPackageRecordsTheActorThatDeclaredIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	const declarer = substrate.ActorPerson
	if _, err := ds.ApplyVocabularyDocuments(ctx, declarer, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("gizmo", map[string]any{"label": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := mustGet(t, ds, kindPackageRef, swPackage).Properties["declaredBy"]; !linksActor(got, declarer) {
		t.Fatalf("declaredBy = %v, want a link to %q", got, declarer)
	}

	// A later apply by another hand changes the package and not its declarer.
	edited := vocabulary.PackageManifest(swPackage, 0)
	edited["data"].(map[string]any)["description"] = "gizmos, edited"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		edited,
		swTypeDoc("gizmo", map[string]any{"label": map[string]any{"type": "string"}}),
		swTypeDoc("gadget", map[string]any{"label": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	row := mustGet(t, ds, kindPackageRef, swPackage)
	if row.Properties["description"] != "gizmos, edited" {
		t.Fatalf("the re-apply did not land: %v", row.Properties)
	}
	if got := row.Properties["declaredBy"]; !linksActor(got, declarer) {
		t.Fatalf("declaredBy after a re-apply by %s = %v, want %q", substrate.ActorPerson, got, declarer)
	}

	// The seed declares core under the seed actor.
	if got := mustGet(t, ds, kindPackageRef, "substrate.reamde.dev/core").Properties["declaredBy"]; !linksActor(got, substrate.ActorSeed) {
		t.Fatalf("core declaredBy = %v, want %q", got, substrate.ActorSeed)
	}
}

func TestPackageDocumentCannotWriteDeclaredBy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	forged := vocabulary.PackageManifest(swPackage, 0)
	forged["data"].(map[string]any)["declaredBy"] = "agent:example.com:notes:notekeeper"
	_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{forged})
	if err == nil || !strings.Contains(err.Error(), "declaredBy") {
		t.Fatalf("a document writing declaredBy was not refused by name: %v", err)
	}
	// Nor the client id an app's ownership is compared on, nor a takeover's
	// mark (decision 0205), by a document or by a put of the row.
	for _, key := range []string{"declaredByClient", "takenOverFrom"} {
		forged := vocabulary.PackageManifest(swPackage, 0)
		forged["data"].(map[string]any)[key] = "https://workouts.example.org/client.json"
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{forged}); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("a document writing %s was not refused by name: %v", key, err)
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{vocabulary.PackageManifest(swPackage, 0)}); err != nil {
		t.Fatalf("declare the package: %v", err)
	}
	for _, key := range []string{"declaredByClient", "takenOverFrom"} {
		_, err := ds.Patch(ctx, substrate.ActorPerson, kindPackageRef, swPackage, substrate.PatchInput{Properties: map[string]any{key: "https://workouts.example.org/client.json"}})
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("a patch writing %s was not refused by name: %v", key, err)
		}
	}
}

// An agent that declares a package through the generic PUT door is named as
// its declarer, and a package re-declared after a delete names the new hand.
// The agent writes as a run does, through the bounded write under its
// ceiling, acting for the admin: a declaration is a control kind, which an
// agent writes only where its ceiling names it (decision 0276), so the same
// put under a glob, or with no ceiling and no declaration of its own, is
// refused.
func TestAgentPutOfPackageRecordsTheAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	authority, name := vocabulary.SplitPackageRef(swPackage)
	agent := substrate.AgentActor(authority, name, "notekeeper")
	in := substrate.PutInput{
		Kind:       kindPackageRef,
		ID:         swPackage,
		Properties: map[string]any{"authority": authority, "package": name},
	}
	run := substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)
	if _, err := engine.PutBounded(run, ds, agent, in, []string{"*"}); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("agent put under a glob: %v, want refused by the one check", err)
	}
	if _, err := ds.Put(run, agent, in); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("agent put with no ceiling and no declaration: %v, want refused by the one check", err)
	}
	if _, err := engine.PutBounded(run, ds, agent, in, []string{kindPackageRef}); err != nil {
		t.Fatalf("agent put: %v", err)
	}
	if got := mustGet(t, ds, kindPackageRef, swPackage).Properties["declaredBy"]; !linksActor(got, agent) {
		t.Fatalf("declaredBy = %v, want a link to %q", got, agent)
	}

	if _, err := ds.Delete(ctx, substrate.ActorPerson, kindPackageRef, swPackage, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
	}); err != nil {
		t.Fatalf("re-declare: %v", err)
	}
	if got := mustGet(t, ds, kindPackageRef, swPackage).Properties["declaredBy"]; !linksActor(got, substrate.ActorPerson) {
		t.Fatalf("declaredBy after a re-declare = %v, want %q", got, substrate.ActorPerson)
	}
}

// linksActor reports whether a stored value links the actor record of a hand
// (decision 0270).
func linksActor(v any, hand substrate.Actor) bool {
	ref, _ := v.(map[string]any)
	return ref[vocabulary.ReferenceValueKey] == vocabulary.ActorRef(vocabulary.ActorID(hand))["ref"]
}
