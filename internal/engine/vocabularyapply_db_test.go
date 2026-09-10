package engine

// What a vocabulary apply does inside its own transaction: it prepares every
// added or changed function body before it activates anything, so an
// uncompilable body fails admission and the batch leaves no half-installed
// package behind; and it recomputes a changed mapping's target kind against
// the candidate, so the offers and values its commit publishes are already
// what a rebuild under the published closure derives.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestSchemaApplyRejectsUnpreparableBody(t *testing.T) {
	t.Parallel()
	// A vocabulary apply prepares every added or changed body BEFORE
	// activation — invalid python fails admission with the register error,
	// and nothing activates.
	ds := newRaceDataset(t)
	ctx := context.Background()
	const badPackage = "broken.test.dev/broken"
	manifest := func(source string) enginetest.Manifest {
		return enginetest.Manifest{
			Name: "broken", Authority: badPackage,
			Manifests: []map[string]any{
				vocabulary.PackageManifest(badPackage, 0),
				vocabulary.ActorManifest(badPackage, vocabulary.PackageActor(badPackage)),
				vocabulary.FunctionManifest(badPackage, "mangle", map[string]any{
					"description": "a body that must compile at registration",
					"runtime":     vocabulary.RuntimePython,
					"source":      source,
					"permissions": map[string]any{"writes": []any{raceWidget}},
				}),
			},
		}
	}
	err := enginetest.Install(ctx, ds, substrate.ActorAPI,
		manifest("def main(input, host)\n    return {}\n")) // missing colon
	if err == nil || !strings.Contains(err.Error(), "failed to prepare") {
		t.Fatalf("invalid source admitted: %v", err)
	}
	if _, ok := ds.registry().PackageByName(badPackage); ok {
		t.Fatal("the failed batch activated")
	}
	// The corrected body admits.
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI,
		manifest("def main(input, host):\n    return {}\n")); err != nil {
		t.Fatalf("valid source refused: %v", err)
	}
	if _, ok := ds.registry().PackageByName(badPackage); !ok {
		t.Fatal("the corrected batch did not activate")
	}
}

// A vocabulary apply that removes a mapping recomputes the target kind INSIDE
// its transaction, against the candidate, so the offers and the values its
// commit publishes are already what a rebuild under the published closure
// derives. Here the contact mapping is the person kind's only one, so the
// machine-held name is released rather than recomputed. The state is read
// through beforePublish, after the commit and before anything else runs: a
// process that died right there must leave nothing for a rebuild to disagree
// with, and a rebuild would keep a stale value, since values are changelog
// entries.
func TestApplyPublishesAChangedMappingsOffersAndValues(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	installContactSource(t, ds)

	// Sam by hand with an address only, so the contact's name lands at the
	// machine tier and its second address is an offer beside the owner's list.
	sam, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: subjPerson, Properties: map[string]any{"emails": []any{"sam@acme.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Put(ctx, csrcActor, substrate.PutInput{
		Kind: csrcContact, ID: "c-sam", Properties: map[string]any{"email": "sam@acme.com", "name": "Samuel"},
	}); err != nil {
		t.Fatal(err)
	}
	type state struct {
		offers, managers int
		name             *string
	}
	read := func() state {
		var st state
		if err := ds.db.QueryRowContext(ctx, `
			SELECT (SELECT count(*) FROM property_offers WHERE record_kind = $1 AND record_id = $2),
			       (SELECT count(*) FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'name'),
			       (SELECT props->>'name' FROM records WHERE kind = $1 AND id = $2)`,
			sam.Kind, sam.ID).Scan(&st.offers, &st.managers, &st.name); err != nil {
			t.Fatal(err)
		}
		return st
	}
	if st := read(); st.name == nil || *st.name != "Samuel" || st.managers != 1 {
		t.Fatalf("the contact did not fill Sam's name at the machine tier, so the test would prove nothing: %+v", st)
	}

	var atPublish *state
	ds.mu.Lock()
	ds.beforePublish = func(*txn) { st := read(); atPublish = &st }
	ds.mu.Unlock()
	defer func() {
		ds.mu.Lock()
		ds.beforePublish = nil
		ds.mu.Unlock()
	}()
	// The people closure again, with no mapping: the apply prunes contactperson,
	// which was the person kind's last.
	if err := enginetest.DeclareMappings(ctx, ds); err != nil {
		t.Fatalf("apply the people closure without the contact mapping: %v", err)
	}
	for _, at := range []struct {
		when string
		st   *state
	}{{"at publish", atPublish}, {"after the apply", func() *state { st := read(); return &st }()}} {
		if at.st == nil {
			t.Fatalf("%s: no reading", at.when)
		}
		if at.st.offers != 0 {
			t.Fatalf("%s: %d offer rows for a mapping the commit removed", at.when, at.st.offers)
		}
		if at.st.name != nil || at.st.managers != 0 {
			name := "<nil>"
			if at.st.name != nil {
				name = *at.st.name
			}
			t.Fatalf("%s: the machine-held name survived its mapping: name=%s managers=%d", at.when, name, at.st.managers)
		}
	}
}
