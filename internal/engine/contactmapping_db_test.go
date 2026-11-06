package engine_test

// Core's mappings onto contacts (decision record 0232). Each waits until its
// provider is installed; the install switches it on, and a repository whose
// provider was installed before the binary shipped the mapping has its
// mirrors linked by the boot upgrade that brings it. The provider here is a
// stand-in for the shipped Linear provider: the one kind its mapping reads,
// under the provider's own identity.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const linearUser = "providers.substrate.reamde.dev/linear/user"

// linearSync is the hand a mirror arrives under: the provider's sync
// function, which names its rows by Linear's own keys (decision 0228).
const linearSync = substrate.Actor(substrate.FunctionActorPrefix + "providers.substrate.reamde.dev:linear:linearsync")

// installLinearStandIn declares the Linear provider's `user` mirror.
func installLinearStandIn(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	docs := []map[string]any{
		{
			"kind":     vocabulary.CoreKind(vocabulary.DocPackage),
			"metadata": map[string]any{"id": "providers.substrate.reamde.dev/linear"},
			"data": map[string]any{
				"authority": "providers.substrate.reamde.dev", "package": "linear", "version": 1,
			},
		},
		{
			"kind":     vocabulary.CoreKind(vocabulary.DocKind),
			"metadata": map[string]any{"id": linearUser},
			"data": map[string]any{
				"authority": "providers.substrate.reamde.dev", "package": "linear",
				"names":           map[string]any{"singular": "user"},
				"purpose":         "supporting",
				"displayTemplate": "{name|email}",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string"},
					"displayName": map[string]any{"type": "string"},
					"email":       map[string]any{"type": "email"},
					"jobTitle":    map[string]any{"type": "string"},
				},
			},
		},
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorSystem, docs); err != nil {
		t.Fatalf("install the Linear stand-in: %v", err)
	}
}

// contactOf reads the contact a Linear user's `contact` slot names, "" when
// it names none.
func contactOf(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	kind, cid, ok := vocabulary.SplitRecordPath(refPathValue(mustGet(t, ds, linearUser, id), "contact"))
	if !ok {
		return ""
	}
	if kind != vocabulary.KindContact {
		t.Fatalf("linear user %s names a %s, want a contact", id, kind)
	}
	return cid
}

// Installing the provider switches its mapping on: a mirror whose address a
// contact carries links to it, one nobody carries mints a contact, and the
// owner's own card, which no mirror describes, keeps what they wrote and is
// not an orphan.
func TestInstallingAProviderMapsItsPeopleOntoContacts(t *testing.T) {
	t.Parallel()
	_, ds := newCoreDataset(t)
	mine := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       vocabulary.KindContact,
		Properties: map[string]any{"name": "Me", "contactType": "person", "prominence": "known"},
	})
	ada := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       vocabulary.KindContact,
		Properties: map[string]any{"name": "Ada Lovelace", "emails": []any{"ada@example.com"}},
	})

	installLinearStandIn(t, ds)
	mustPut(t, ds, linearSync, substrate.PutInput{Kind: linearUser, ID: "u-ada", Properties: map[string]any{
		"name": "Ada King", "email": "ada@example.com", "jobTitle": "Analyst",
	}})
	mustPut(t, ds, linearSync, substrate.PutInput{Kind: linearUser, ID: "u-grace", Properties: map[string]any{
		"name": "Grace Hopper", "displayName": "grace", "email": "grace@example.com",
	}})

	if got := contactOf(t, ds, "u-ada"); got != ada.ID {
		t.Fatalf("u-ada links %q, want the contact carrying its address (%s)", got, ada.ID)
	}
	if c := mustGet(t, ds, vocabulary.KindContact, ada.ID); c.Properties["jobTitle"] != "Analyst" || c.Properties["name"] != "Ada Lovelace" {
		t.Fatalf("Ada after the link = %v: the job title fills in, the owner's name stands", c.Properties)
	}
	graceID := contactOf(t, ds, "u-grace")
	if graceID == "" || graceID == ada.ID {
		t.Fatalf("u-grace links %q, want a contact minted for it", graceID)
	}
	grace := mustGet(t, ds, vocabulary.KindContact, graceID)
	if grace.Properties["name"] != "Grace Hopper" || grace.Properties["nickname"] != "grace" {
		t.Fatalf("the minted contact = %v", grace.Properties)
	}
	if grace.Properties["prominence"] != "utility" {
		t.Fatalf("a minted contact's prominence = %v, want utility", grace.Properties["prominence"])
	}

	if me := mustGet(t, ds, vocabulary.KindContact, mine.ID); me.Properties["contactType"] != "person" || me.Properties["name"] != "Me" {
		t.Fatalf("the owner's own card after the install = %v", me.Properties)
	}
	for _, o := range orphans(t, ds, vocabulary.KindContact) {
		if o.ID == mine.ID || o.ID == ada.ID {
			t.Fatalf("%s is marked an orphan", o.ID)
		}
	}
}

// A repository that installed the provider under a binary without the
// mapping has its mirrors linked by the boot upgrade that ships it.
func TestTheBootUpgradeLinksMirrorsSyncedBeforeTheMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Binary N: core without the mappings, one version back.
	older := shippedTree(t)
	if err := os.Remove(filepath.Join(older, corePackage, "contactmappings.yaml")); err != nil {
		t.Fatalf("drop the mappings from the older tree: %v", err)
	}
	current := declaredVersion(t, older, corePackage)
	bumpPackageVersion(t, older, corePackage, "1")
	svc, dsn := newService(t, engine.WithKindsDir(older), engine.WithCredentialKey(engine.TestCredentialKey))
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create the repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the dataset: %v", err)
	}
	installLinearStandIn(t, ds)
	mustPut(t, ds, linearSync, substrate.PutInput{Kind: linearUser, ID: "u-ada", Properties: map[string]any{
		"name": "Ada Lovelace", "email": "ada@example.com",
	}})
	if err := svc.Close(); err != nil {
		t.Fatalf("close binary N: %v", err)
	}

	// Binary N+1 ships them.
	tree := shippedTree(t)
	if got := declaredVersion(t, tree, corePackage); got != current {
		t.Fatalf("the shipped core is at %s, the older tree was cut from %s", got, current)
	}
	next := openTree(t, dsn, tree)
	ds, err = next.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open under binary N+1: %v", err)
	}
	id := contactOf(t, ds, "u-ada")
	if id == "" {
		t.Fatal("the mirror synced before the mapping is still unlinked after the upgrade")
	}
	if c := mustGet(t, ds, vocabulary.KindContact, id); c.Properties["name"] != "Ada Lovelace" {
		t.Fatalf("the contact minted for the mirror = %v", c.Properties)
	}
}
