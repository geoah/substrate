package engine

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE CONFIG IS PINNED WITH THE INPUT (decision record 0265). `input.config`
// is the part of the input the engine resolves from the repository: the
// bundle, its injected inputs, its settings and its accounts with their
// tokens. It is built here by resolveFunctionConfig, the call an invocation
// makes, over a provider with an OAuth client, one account connected through
// the person's own client, one never connected, and settings; then with the
// client deleted, so the connected account's token is refused; then for a
// bundle with no account kind. Each config is read back through JSON, as a
// body reads it. The one key no state of the fixture reaches is a view's
// `secretErrors`, which a secret-typed property that will not open leaves:
// that view is built by injectedRecordConfig, the builder every input and
// account goes through, and joins the variants as an input and an account
// would carry it.
//
// A view's `properties` are the package's own names and stay free, but for
// `title`, which the engine puts among them for a record that has one: a
// view of a titled record is built by the same builder and joins the
// variants too.
//
// Regenerate with `go test ./internal/engine/ -run TestFunctionConfigMatchesTheGolden -update-function-input`.

const functionConfigGolden = "testdata/function_config.golden.json"

func TestFunctionConfigMatchesTheGolden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _, connected := openOpMail(t)
	opPutOwnClient(t, ds, "default", opOwnClientWithBase)
	opConnect(t, ds, connected)
	opPutAccount(t, ds, "a2")
	for kind, props := range map[string]map[string]any{
		kindSetting: {"type": "url", "value": "https://api.example.com"},
		kindSecret:  {"value": "material"},
	} {
		id := opMailPackage + "/baseUrl"
		if kind == kindSecret {
			id = opMailPackage + "/apiKey"
		}
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}

	config := func(pkg string) map[string]any {
		t.Helper()
		cfg, _, _, err := ds.resolveFunctionConfig(ctx, &vocabulary.Function{Package: pkg})
		if err != nil {
			t.Fatalf("resolve the config of %s: %v", pkg, err)
		}
		if cfg == nil {
			t.Fatalf("%s resolves no config: it is no bundle here", pkg)
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var read map[string]any
		if err := json.Unmarshal(raw, &read); err != nil {
			t.Fatal(err)
		}
		return read
	}
	withClient := config(opMailPackage)
	if _, err := ds.Delete(ctx, substrate.ActorPerson, opMailConfig, fid("default"), substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the own client: %v", err)
	}
	withoutClient := config(opMailPackage)
	// A bundle with one kind and no account kind.
	const plain = "plain.example.com/plain"
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor("plain.example.com", "plain"), []map[string]any{
		vocabulary.PackageManifest(plain, 1),
		vocabulary.BundleManifest(plain, map[string]any{"installs": []any{plain + "/note"}}),
		vocabulary.KindManifest(plain, map[string]any{"singular": "note"}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}, nil, substrate.BundleInstall{}); err != nil {
		t.Fatalf("install a bundle with no account kind: %v", err)
	}
	noAccounts := config(plain)

	secretKind, ok := ds.registry().ByIdentity(kindSecret)
	if !ok {
		t.Fatal("no secret kind")
	}
	broken, _ := ds.injectedRecordConfig(ctx, secretKind, &erow{ID: "x", Kind: kindSecret, Version: 1, Props: map[string]any{"value": secretRefPrefix + "missing"}})
	if broken["secretErrors"] == nil {
		t.Fatalf("a secret that will not open left no secretErrors: %v", broken)
	}
	raw, err := json.Marshal(broken)
	if err != nil {
		t.Fatal(err)
	}
	var brokenView map[string]any
	if err := json.Unmarshal(raw, &brokenView); err != nil {
		t.Fatal(err)
	}
	withBrokenSecret := map[string]any{
		"bundle":   opMailPackage,
		"inputs":   map[string]any{"client": brokenView},
		"accounts": []any{brokenView},
	}
	// No record of the fixture has a title, so a view of one that does is
	// built the same way, by the builder every input and account goes
	// through, and joins the variants as an input and an account.
	withTitle, _ := ds.injectedRecordConfig(ctx, secretKind, &erow{ID: "t", Kind: kindSecret, Version: 1, Title: "A titled record", Props: map[string]any{}})
	raw, err = json.Marshal(withTitle)
	if err != nil {
		t.Fatal(err)
	}
	var titledView map[string]any
	if err := json.Unmarshal(raw, &titledView); err != nil {
		t.Fatal(err)
	}
	withTitledRecord := map[string]any{
		"bundle":   opMailPackage,
		"inputs":   map[string]any{"client": titledView},
		"accounts": []any{titledView},
	}

	// The states the fixture is meant to reach, so the golden cannot pass on
	// a fixture that stopped reaching them.
	accounts := func(cfg map[string]any) []any { a, _ := cfg["accounts"].([]any); return a }
	hasKey := func(entries []any, key string) bool {
		for _, e := range entries {
			if m, ok := e.(map[string]any); ok && m[key] != nil {
				return true
			}
		}
		return false
	}
	switch {
	case withClient["inputs"] == nil || withClient["settings"] == nil:
		t.Fatalf("the config with the own client carries no inputs or settings: %v", withClient)
	case !hasKey(accounts(withClient), "token"):
		t.Fatalf("no account carries a token: %v", withClient["accounts"])
	case !hasKey(accounts(withoutClient), "tokenError"):
		t.Fatalf("no account carries a tokenError once the client changed: %v", withoutClient["accounts"])
	case noAccounts["accounts"] != nil:
		t.Fatalf("a bundle with no account kind carries accounts %v, want null", noAccounts["accounts"])
	}
	// A view's `title` is the engine's key among the package's own: the
	// fixture reaches a record with a title and one without.
	titled, untitled := 0, 0
	for _, cfg := range []map[string]any{withClient, withoutClient, withBrokenSecret, withTitledRecord} {
		views := accounts(cfg)
		if inputs, ok := cfg["inputs"].(map[string]any); ok {
			for _, v := range inputs {
				views = append(views, v)
			}
		}
		for _, v := range views {
			view, _ := v.(map[string]any)
			props, _ := view["properties"].(map[string]any)
			if _, ok := props["title"].(string); ok {
				titled++
			} else {
				untitled++
			}
		}
	}
	if titled == 0 || untitled == 0 {
		t.Fatalf("the fixture reaches %d views with a title and %d without, want both", titled, untitled)
	}

	shapes := map[string]map[string]string{}
	mapShape(shapes, "input.config", []map[string]any{withClient, withoutClient, noAccounts, withBrokenSecret, withTitledRecord})
	got, err := json.MarshalIndent(shapes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *updateFunctionInput {
		if err := os.WriteFile(functionConfigGolden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(functionConfigGolden)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with -update-function-input)", functionConfigGolden, err)
	}
	if string(got) != string(want) {
		t.Fatalf("the config a function body is handed moved, and %s did not.\n"+
			"A body reads these keys, so a change to them is a change to the function interface: an addition stays on interface %d, anything else needs a new version (docs/functions.md#the-interface).\n"+
			"Review the change, then regenerate with -update-function-input.\n\ngot:\n%s", functionConfigGolden, vocabulary.FunctionInterface, got)
	}
}
