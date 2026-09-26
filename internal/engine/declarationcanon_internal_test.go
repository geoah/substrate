package engine

// The comparison behind "an unchanged closure keeps every version" (issue
// #643), held without a database: a manifest and the row it became compare
// equal, a real change never does, and a document the coercion cannot read
// compares as written.

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

const canonKind = "providers.substrate.reamde.dev/whoop/user"

func canonFunction(timeout string, writes ...any) vocabulary.Document {
	return vocabulary.Document{Kind: vocabulary.DocFunction, ID: "providers.substrate.reamde.dev/whoop/whoopsync", Data: map[string]any{
		"authority":   "providers.substrate.reamde.dev",
		"package":     "whoop",
		"timeout":     timeout,
		"permissions": map[string]any{"writes": writes},
	}}
}

func canonEqual(t *testing.T, canon func(vocabulary.Document) map[string]any, a, b vocabulary.Document) bool {
	t.Helper()
	return declarationDataEqual(canon(a), canon(b))
}

func TestDeclarationCanonicalizerComparesTheStoredForm(t *testing.T) {
	reg, err := seedRegistry()
	if err != nil {
		t.Fatalf("load the seed vocabulary: %v", err)
	}
	canon := declarationCanonicalizer(reg)
	stored := vocabulary.RecordPath(kindKind, canonKind)

	// The manifest spelling against the row spelling: equal.
	manifest := canonFunction("PT60S", canonKind)
	row := canonFunction("PT1M", map[string]any{vocabulary.ReferenceValueKey: stored})
	if !canonEqual(t, canon, manifest, row) {
		t.Errorf("a manifest and the row it became compare unequal:\n%v\n%v", canon(manifest), canon(row))
	}
	// A real change to either value alone: unequal.
	if canonEqual(t, canon, canonFunction("PT45S", canonKind), row) {
		t.Error("a changed duration compares equal")
	}
	other := "providers.substrate.reamde.dev/whoop/cycle"
	if canonEqual(t, canon, canonFunction("PT60S", other), row) {
		t.Error("a changed reference compares equal")
	}
	if canonEqual(t, canon, canonFunction("PT60S", canonKind, other), row) {
		t.Error("an added reference compares equal")
	}

	// A bundle input's `kind`, a reference inside a keyed map of objects.
	bundle := func(kind any) vocabulary.Document {
		return vocabulary.Document{Kind: vocabulary.DocBundle, ID: "providers.substrate.reamde.dev/whoop", Data: map[string]any{
			"authority": "providers.substrate.reamde.dev", "package": "whoop",
			"inputs": map[string]any{"client": map[string]any{"kind": kind, "inject": "functions"}},
		}}
	}
	if !canonEqual(t, canon, bundle(canonKind), bundle(map[string]any{vocabulary.ReferenceValueKey: stored})) {
		t.Error("a bundle input's bare kind and its stored reference compare unequal")
	}
	if canonEqual(t, canon, bundle(other), bundle(map[string]any{vocabulary.ReferenceValueKey: stored})) {
		t.Error("a changed bundle input kind compares equal")
	}

	// The canonicalizer copies: the catalog's cached documents are shared.
	before, _ := json.Marshal(manifest.Data)
	_ = canon(manifest)
	if after, _ := json.Marshal(manifest.Data); string(after) != string(before) {
		t.Errorf("the canonicalizer wrote into its input: %s -> %s", before, after)
	}
}

func TestDeclarationCanonicalizerFallsBackToTheDataAsWritten(t *testing.T) {
	reg, err := seedRegistry()
	if err != nil {
		t.Fatalf("load the seed vocabulary: %v", err)
	}
	unreadable := canonFunction("PT60S", canonKind)
	unreadable.Data["notAKey"] = "x" // coerceProps refuses an undeclared property
	for name, tc := range map[string]struct {
		reg *vocabulary.Registry
		doc vocabulary.Document
	}{
		"no registry":           {nil, canonFunction("PT60S", canonKind)},
		"unknown document kind": {reg, vocabulary.Document{Kind: "nosuchdoc", Data: map[string]any{"a": 1}}},
		"refused by coercion":   {reg, unreadable},
		"meta-kind not held":    {vocabulary.NewRegistry(), canonFunction("PT60S", canonKind)},
	} {
		if got := declarationCanonicalizer(tc.reg)(tc.doc); !reflect.DeepEqual(got, tc.doc.Data) {
			t.Errorf("%s: canonical data = %v, want the data as written %v", name, got, tc.doc.Data)
		}
	}
	// The fallback still tells a change apart: raw against raw.
	if declarationDataEqual(declarationCanonicalizer(nil)(canonFunction("PT60S", canonKind)),
		declarationCanonicalizer(nil)(canonFunction("PT1M", canonKind))) {
		t.Error("the fallback compares two different durations equal")
	}
}
