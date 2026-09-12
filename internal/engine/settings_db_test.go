package engine_test

// A bundle's configuration as core `setting` and `secret` records (decision
// record 0076). Every invariant here is keyed on the ID PREFIX, which is the
// only thing that makes a record a given bundle's:
//
//   - a setting's `value` must parse as its `type`, and an empty value always
//     admits, because empty is the unfilled state every shipped setting
//     starts in;
//   - the bundle's functions receive them as `config.settings.<name>`, the
//     secret resolved to plaintext and held to the runner boundary by the
//     invocation scrubber;
//   - a required one still empty is a setup item on the bundle's status;
//   - purge takes them with the rest of the bundle's data, and a record under
//     another prefix is another bundle's.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	settingKind = "substrate.reamde.dev/core/setting"
	secretKind  = "substrate.reamde.dev/core/secret"

	stPackage  = "settings.bundles.substrate.reamde.dev/settings"
	stNoteType = stPackage + "/settingnote"
	stReportFn = stPackage + "/report"

	// The material the secret carries. The body reports its length and a
	// short prefix beside the value itself, so one call proves both halves:
	// it ARRIVED in plaintext, and the value itself does not leave.
	stMaterial = "sk-live-settings-should-never-escape"
)

// stReportSource returns what the bundle's settings look like from inside a
// body: the names it can see, the plain value, and three views of the secret.
const stReportSource = `
def main(input, host):
    s = (input.get("config") or {}).get("settings") or {}
    key = s.get("apiKey") or ""
    return {"effects": [], "output": {
        "names": sorted(s.keys()),
        "baseUrl": s.get("baseUrl") or "",
        "keyLen": len(key),
        "keyHead": key[:7],
        "keyRaw": key,
    }}
`

// installSettingsBundle stands up a bundle that declares NO input at all: its
// whole configuration is the setting and secret records a caller writes under
// its id, which is the shape decision record 0076 makes standard.
func installSettingsBundle(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newDataset(t)
	docs := []map[string]any{
		vocabulary.PackageManifest(stPackage, 0),
		vocabulary.BundleManifest(stPackage, map[string]any{
			"description": "the settings bundle",
			"installs":    []any{stNoteType, stReportFn},
		}),
		vocabulary.KindManifest(stPackage,
			map[string]any{"singular": "settingnote"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		vocabulary.FunctionManifest(stPackage, "report", map[string]any{
			"description": "reports what the bundle's settings look like from a body",
			"runtime":     vocabulary.RuntimePython,
			"source":      stReportSource,
			"permissions": map[string]any{"writes": []any{stNoteType}},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install the settings bundle: %v", err)
	}
	return ds
}

// A setting is one string, so its `type` is the only contract it has, and the
// write is where that contract is held.
func TestSettingWriteHoldsValueToItsType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	refused := map[string]map[string]any{
		"url":          {"type": "url", "value": "not a url"},
		"urlrelative":  {"type": "url", "value": "/just/a/path"},
		"int":          {"type": "int", "value": "twelve"},
		"intdecimal":   {"type": "int", "value": "1.5"},
		"bool":         {"type": "bool", "value": "yes"},
		"enumoutside":  {"type": "enum", "values": []any{"a", "b"}, "value": "c"},
		"enumnovalues": {"type": "enum", "value": "a"},
	}
	for name, props := range refused {
		_, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind: settingKind, ID: "bundles.example.com/demo/" + name, Properties: props,
		})
		wantRefusal(t, err, substrate.ErrValidation, "value")
	}

	admitted := map[string]map[string]any{
		"urlok":    {"type": "url", "value": "https://api.example.com"},
		"intok":    {"type": "int", "value": "-12"},
		"boolok":   {"type": "bool", "value": "false"},
		"enumok":   {"type": "enum", "values": []any{"a", "b"}, "value": "b"},
		"stringok": {"type": "string", "value": "anything at all"},
		// The UNFILLED state under a type nothing would parse: a bundle
		// could not ship a setting at all if this were refused.
		"emptyurl":  {"type": "url", "required": true},
		"emptyenum": {"type": "enum", "values": []any{"a"}},
	}
	for name, props := range admitted {
		if _, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind: settingKind, ID: "bundles.example.com/demo/" + name, Properties: props,
		}); err != nil {
			t.Fatalf("%s must admit: %v", name, err)
		}
	}

	// A `secret` carries no type: any string is credential material.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: secretKind, ID: "bundles.example.com/demo/token",
		Properties: map[string]any{"value": "not a url, not a number"},
	}); err != nil {
		t.Fatalf("a secret takes any string: %v", err)
	}
}

// The guard sees what the row WILL hold, not what the write happens to name:
// a write moving only the value is held to the STORED type, and one moving
// only the type is held to the STORED value. Either alone can break the pair.
func TestSettingTypeAndValueAreHeldTogetherAcrossWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	const id = "bundles.example.com/demo/endpoint"

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: id,
		Properties: map[string]any{"type": "url", "value": "https://api.example.com"},
	})

	_, err := ds.Patch(ctx, owner, settingKind, id, substrate.PatchInput{
		Properties: map[string]any{"value": "nonsense"},
	})
	wantRefusal(t, err, substrate.ErrValidation, "url")

	_, err = ds.Patch(ctx, owner, settingKind, id, substrate.PatchInput{
		Properties: map[string]any{"type": "int"},
	})
	wantRefusal(t, err, substrate.ErrValidation, "whole number")

	// Both at once is a legal retype.
	if _, err := ds.Patch(ctx, owner, settingKind, id, substrate.PatchInput{
		Properties: map[string]any{"type": "int", "value": "8080"},
	}); err != nil {
		t.Fatalf("retyping value and type together must admit: %v", err)
	}
}

// Injection AND scrubbing in one call: the body sees the secret's real length
// and a prefix of it, so it plainly arrived in plaintext, while the value
// itself comes back redacted, because the scrubber holds every surface that
// leaves the runner boundary. A record under another prefix never crosses.
func TestBundleSettingsInjectIntoFunctionsAndAreScrubbed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installSettingsBundle(t)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: stPackage + "/baseUrl",
		Properties: map[string]any{"type": "url", "value": "https://api.example.com"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: secretKind, ID: stPackage + "/apiKey",
		Properties: map[string]any{"value": stMaterial},
	})
	// Another bundle's prefix, and an id under nobody's: ownership is the
	// prefix and nothing else, so neither is this bundle's.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: "other.example.com/other/baseUrl",
		Properties: map[string]any{"value": "https://elsewhere.example.com"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: "loose", Properties: map[string]any{"value": "unowned"},
	})

	out, _, err := ds.CallFunction(ctx, stReportFn, map[string]any{})
	if err != nil {
		t.Fatalf("call report: %v", err)
	}
	om, _ := out.(map[string]any)
	names, _ := om["names"].([]any)
	if len(names) != 2 || names[0] != "apiKey" || names[1] != "baseUrl" {
		t.Fatalf("config.settings keys = %v, want exactly this bundle's two", names)
	}
	if om["baseUrl"] != "https://api.example.com" {
		t.Fatalf("config.settings.baseUrl = %v", om["baseUrl"])
	}
	// The secret ARRIVED as the material: the stored value is a sealed
	// reference of another length entirely, so this cannot pass on it.
	if n, _ := om["keyLen"].(float64); int(n) != len(stMaterial) {
		t.Fatalf("config.settings.apiKey reached the body at length %v, want %d", om["keyLen"], len(stMaterial))
	}
	if om["keyHead"] != stMaterial[:7] {
		t.Fatalf("config.settings.apiKey head = %v, want %q", om["keyHead"], stMaterial[:7])
	}
	// And it does not LEAVE: the one surface a caller reads is scrubbed.
	if om["keyRaw"] == stMaterial {
		t.Fatalf("the injected secret escaped through the function output")
	}
	if om["keyRaw"] != engine.Redacted {
		t.Fatalf("the output was not scrubbed in place: %v", om["keyRaw"])
	}

	// The row itself still never reads back over the API.
	row := mustGet(t, ds, secretKind, stPackage+"/apiKey")
	if row.Properties["value"] == stMaterial {
		t.Fatalf("the secret read back in plaintext over the API")
	}
}

// A bundle that ships no settings gets no `settings` key at all: an empty map
// would say the bundle has configuration nobody filled in, which is a
// different thing from having none.
func TestABundleWithNoSettingsInjectsNoSettingsKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installSettingsBundle(t)

	out, _, err := ds.CallFunction(ctx, stReportFn, map[string]any{})
	if err != nil {
		t.Fatalf("call report: %v", err)
	}
	names, _ := out.(map[string]any)["names"].([]any)
	if len(names) != 0 {
		t.Fatalf("config.settings = %v on a bundle that ships none", names)
	}
}

// A required setting or secret with no value is a setup item on the bundle's
// status, coded `setting`, naming the record and the heading the bundle
// shipped. One that is not required, or one already filled, is not.
func TestEmptyRequiredSettingIsASetupItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installSettingsBundle(t)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: secretKind, ID: stPackage + "/apiKey",
		Properties: map[string]any{"required": true, "displayName": "API key"},
	})
	// Required and FILLED: nothing stands between the bundle and a run.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: stPackage + "/baseUrl",
		Properties: map[string]any{"required": true, "type": "url", "value": "https://api.example.com"},
	})
	// Empty, but not required.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: stPackage + "/label",
		Properties: map[string]any{"displayName": "Label"},
	})
	// Required and empty, but under ANOTHER bundle's prefix.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: secretKind, ID: "other.example.com/other/apiKey",
		Properties: map[string]any{"required": true},
	})

	st, err := ds.BundleStatus(ctx, stPackage)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if len(st.Setup) != 1 {
		t.Fatalf("setup = %+v, want the one empty required secret", st.Setup)
	}
	item := st.Setup[0]
	if item.Code != substrate.SetupSetting || item.Record != stPackage+"/apiKey" || item.Kind != secretKind {
		t.Fatalf("setup item = %+v, want the empty required secret", item)
	}
	if !strings.Contains(item.Message, "API key") {
		t.Fatalf("the message %q does not name the heading the bundle shipped", item.Message)
	}

	if _, err := ds.Patch(ctx, owner, secretKind, stPackage+"/apiKey", substrate.PatchInput{
		Properties: map[string]any{"value": stMaterial},
	}); err != nil {
		t.Fatalf("fill in the key: %v", err)
	}
	if st, err = ds.BundleStatus(ctx, stPackage); err != nil {
		t.Fatalf("bundle status after the key landed: %v", err)
	}
	if len(st.Setup) != 0 {
		t.Fatalf("setup = %+v, want empty once the required secret is filled", st.Setup)
	}
}

// Purge takes the bundle's configuration with the rest of its data; another
// bundle's settings stay, because they were never this bundle's.
func TestPurgeRemovesTheBundlesSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installSettingsBundle(t)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: stPackage + "/baseUrl",
		Properties: map[string]any{"type": "url", "value": "https://api.example.com"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: secretKind, ID: stPackage + "/apiKey",
		Properties: map[string]any{"value": stMaterial},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: settingKind, ID: "other.example.com/other/baseUrl",
		Properties: map[string]any{"value": "https://elsewhere.example.com"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: stNoteType, Properties: map[string]any{"text": "a note"},
	})

	if err := ds.DisableBundle(ctx, stPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	purged, err := ds.PurgeBundle(ctx, stPackage)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	// The note, the setting and the secret: the configuration counts as the
	// data it is.
	if purged != 3 {
		t.Fatalf("purged %d, want the note plus the two configuration records", purged)
	}
	// Purge is the ordinary SOFT delete, so the proof is the tombstone: a
	// deleted record still reads until the GC sweep collects it.
	for _, gone := range [][2]string{
		{settingKind, stPackage + "/baseUrl"},
		{secretKind, stPackage + "/apiKey"},
	} {
		row, err := ds.Get(ctx, gone[0], gone[1])
		if err != nil {
			t.Fatalf("read %s after the purge: %v", gone[1], err)
		}
		if row.DeletedAt == nil {
			t.Fatalf("purge left %s live", gone[1])
		}
	}
	kept, err := ds.Get(ctx, settingKind, "other.example.com/other/baseUrl")
	if err != nil {
		t.Fatalf("purge took another bundle's setting: %v", err)
	}
	if kept.DeletedAt != nil {
		t.Fatalf("purge tombstoned another bundle's setting")
	}
	// Idempotent: nothing of its own is left to take.
	if again, err := ds.PurgeBundle(ctx, stPackage); err != nil || again != 0 {
		t.Fatalf("re-purge: %d %v", again, err)
	}
}
