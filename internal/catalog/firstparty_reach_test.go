package catalog

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/geoah/substrate/kinds"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The scopes the first-party apps need for the packages they ask to have
// set up (decision 0215), held to the shipped closures: what each package
// and its requirements reach, every entry covered by the app's scopes,
// with an internal kind covered by its exact scope alone. A shipped
// closure that comes to reach more fails here before it fails on the
// owner's approval page, and the lists are what the apps ask for.

// syncScopes is what Substrate Sync asks for: its own apple kinds, and
// everything the applehealth package's code reaches, the two internal
// kinds by name.
var syncScopes = []string{
	"write:providers.substrate.reamde.dev/apple/*",
	"write:providers.substrate.reamde.dev/applehealth/*",
	"write:providers.substrate.reamde.dev/applehealth/account",
	"write:providers.substrate.reamde.dev/applehealth/export",
	"write:substrate.reamde.dev/health/*",
	"write:substrate.reamde.dev/observations/*",
}

// workoutsScopes is what Substrate Workouts asks for beside its own
// training kinds: the health catalogue ships no code, and its coach agent
// reads the health, observations and clinical kinds below and writes its
// training kinds (apps/apple/WorkoutsKit/Sources/WorkoutsSync/Coach.swift).
var workoutsScopes = []string{
	"write:substrate.reamde.dev/training/*",
	"read:substrate.reamde.dev/health/*",
	"read:substrate.reamde.dev/observations/*",
	"read:substrate.reamde.dev/clinical/condition",
	"read:substrate.reamde.dev/clinical/medicationrequest",
	"read:substrate.reamde.dev/clinical/medicationdispense",
}

// trainingPurposes is the declared purpose of each training kind, which
// the app's site publishes rather than the binary ships
// (apps/apple/SubstrateWorkouts/Package, as of 2026-11-03): none is
// internal, so the training write glob covers them all.
var trainingPurposes = map[string]string{
	"substrate.reamde.dev/training/exercise": vocabulary.PurposePrimary,
	"substrate.reamde.dev/training/memory":   vocabulary.PurposePrimary,
	"substrate.reamde.dev/training/routine":  vocabulary.PurposePrimary,
	"substrate.reamde.dev/training/session":  vocabulary.PurposePrimary,
	"substrate.reamde.dev/training/movement": vocabulary.PurposeSupporting,
	"substrate.reamde.dev/training/set":      vocabulary.PurposeSupporting,
	"substrate.reamde.dev/training/setup":    vocabulary.PurposeSupporting,
}

// coachReach is the workout coach's reach as its agent declares it once it
// ships in the trainingcoach closure (decision 0217).
var coachReach = []Reach{
	{Pattern: "substrate.reamde.dev/training/exercise", Write: true},
	{Pattern: "substrate.reamde.dev/training/setup", Write: true},
	{Pattern: "substrate.reamde.dev/training/routine", Write: true},
	{Pattern: "substrate.reamde.dev/training/memory", Write: true},
	{Pattern: "substrate.reamde.dev/training/session"},
	{Pattern: "substrate.reamde.dev/training/set"},
	{Pattern: "substrate.reamde.dev/health/workout"},
	{Pattern: "substrate.reamde.dev/health/sleep"},
	{Pattern: "substrate.reamde.dev/observations/observation"},
	{Pattern: "substrate.reamde.dev/observations/metric"},
	{Pattern: "substrate.reamde.dev/observations/source"},
	{Pattern: "substrate.reamde.dev/observations/sourcelink"},
	{Pattern: "substrate.reamde.dev/observations/trace"},
	{Pattern: "substrate.reamde.dev/clinical/condition"},
	{Pattern: "substrate.reamde.dev/clinical/medicationrequest"},
	{Pattern: "substrate.reamde.dev/clinical/medicationdispense"},
}

// shippedPurposes is the declared purpose of every kind the binary ships,
// the seeded packages included.
func shippedPurposes(t *testing.T, cat *Catalog) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, b := range cat.Bundles() {
		for _, k := range b.Kinds() {
			out[k.Identity] = k.Purpose
		}
	}
	err := fs.WalkDir(kinds.Seed(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		raw, err := fs.ReadFile(kinds.Seed(), path)
		if err != nil {
			return err
		}
		docs, err := vocabulary.ParseStream(raw)
		if err != nil {
			return err
		}
		for _, doc := range docs {
			if doc.Kind == vocabulary.DocKind {
				out[doc.ID], _ = doc.Data["purpose"].(string)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the seed: %v", err)
	}
	return out
}

// uncovered is every entry of reach the scopes do not cover.
func uncovered(t *testing.T, scopes []string, purposes map[string]string, reach []Reach) []string {
	t.Helper()
	approved, err := scope.ApprovalOf(scopes)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range reach {
		purpose, known := purposes[r.Pattern]
		internal := !vocabulary.IsTypeGlob(r.Pattern) && (!known || purpose == vocabulary.PurposeInternal)
		if !approved.Covers(r.Pattern, r.Write, internal) {
			out = append(out, scope.Covering(r.Pattern, r.Write))
		}
	}
	return out
}

func TestTheFirstPartyAppsScopesCoverThePackagesTheyAskFor(t *testing.T) {
	cat, err := Load(ProviderRoot(kinds.Bundles()))
	if err != nil {
		t.Fatal(err)
	}
	purposes := shippedPurposes(t, cat)
	for k, p := range trainingPurposes {
		purposes[k] = p
	}
	var closure func(id string) []Reach
	closure = func(id string) []Reach {
		b, ok := cat.ByID(id)
		if !ok {
			t.Fatalf("the catalog ships no %s", id)
		}
		var out []Reach
		for _, req := range b.Requires {
			out = append(out, closure(req)...)
		}
		return append(out, b.Reach()...)
	}
	for name, c := range map[string]struct {
		scopes   []string
		packages []string
		reach    []Reach
	}{
		"Substrate Sync": {
			scopes: syncScopes, packages: []string{"providers.substrate.reamde.dev/apple", "providers.substrate.reamde.dev/applehealth"},
		},
		"Substrate Workouts": {scopes: workoutsScopes, packages: []string{"substrate.reamde.dev/health"}, reach: coachReach},
	} {
		t.Run(name, func(t *testing.T) {
			reach := c.reach
			for _, pkg := range c.packages {
				reach = append(reach, closure(pkg)...)
			}
			if missing := uncovered(t, c.scopes, purposes, reach); len(missing) > 0 {
				t.Fatalf("%s's scopes leave uncovered: %s", name, strings.Join(missing, ", "))
			}
		})
	}
	// Each exact scope in Sync's list is there because a glob does not
	// reach the internal kind it names: without it, that kind alone is
	// uncovered, read and written.
	for _, kind := range []string{"providers.substrate.reamde.dev/applehealth/account", "providers.substrate.reamde.dev/applehealth/export"} {
		var fewer []string
		for _, held := range syncScopes {
			if held != "write:"+kind {
				fewer = append(fewer, held)
			}
		}
		missing := uncovered(t, fewer, purposes, closure("providers.substrate.reamde.dev/applehealth"))
		if len(missing) == 0 {
			t.Fatalf("without write:%s, Sync covers everything, so the exact scope is not needed", kind)
		}
		for _, m := range missing {
			if !strings.HasSuffix(m, ":"+kind) {
				t.Fatalf("without write:%s, Sync leaves uncovered %v", kind, missing)
			}
		}
	}
}
