package engine

// A webhook trigger a publisher ships is enabled only with a key or a
// signature (decision 0248). Apple Health's lands off and keyless; the
// console's Resume, a patch of `enabled` alone, is refused; a write that keys
// it turns it on, and so does the app's approval; an upgrade leaves it as it
// was; one a binary from before the rule left open stays open and is
// reported; a package that ships one open is refused at install; and the
// owner's own trigger and a sample's keep decision 0045's open endpoint.

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/appsetup"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

const (
	appleHealthPackage = "providers.substrate.reamde.dev/applehealth"
	appleHealthHook    = "applehealth-webhook"
	ownersHookKey      = "an-owners-key-0123456789abcdef"
)

// appleHealthDataset is a fresh repository with Apple Health installed.
func appleHealthDataset(t *testing.T) *dataset {
	t.Helper()
	ds := openCoreDataset(t)
	installAppleHealth(t, ds)
	return ds
}

// installAppleHealth installs Apple Health through the catalog's install
// door, as the provider page does, after the health catalogue it requires,
// whose units a test repository does not hold until a test puts them.
func installAppleHealth(t *testing.T, ds *dataset) {
	t.Helper()
	if err := enginetest.InstallCatalogues(context.Background(), ds, "health"); err != nil {
		t.Fatalf("install the health catalogue: %v", err)
	}
	installShippedProvider(t, ds, appleHealthPackage)
}

// hookState is Apple Health's webhook trigger as stored: whether it is on,
// its key, and its source.
func hookState(t *testing.T, ds *dataset) (bool, string, map[string]any) {
	t.Helper()
	rec, err := ds.Get(context.Background(), typeTrigger, appleHealthHook)
	if err != nil {
		t.Fatalf("read %s: %v", appleHealthHook, err)
	}
	enabled, _ := rec.Properties["enabled"].(bool)
	source, _ := rec.Properties["source"].(map[string]any)
	return enabled, vocabulary.TriggerKey(rec.Properties), source
}

// withHook is source with its webhook arm's field set to v. The maps are
// copies, because source is a read's.
func withHook(source map[string]any, field string, v any) map[string]any {
	out := maps.Clone(source)
	webhook := maps.Clone(out["webhook"].(map[string]any))
	webhook[field] = v
	out["webhook"] = webhook
	return out
}

// putHook writes Apple Health's webhook trigger on or off from source, as
// the owner's `substratectl apply -f` of the record does.
func putHook(ctx context.Context, ds *dataset, enabled bool, source map[string]any) error {
	_, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: typeTrigger, ID: appleHealthHook, Properties: map[string]any{"enabled": enabled, "source": source},
	})
	return err
}

// requireOpenRefused fails unless err refuses leaving open the trigger pkg
// shipped, naming the trigger, the package and the key that lets it through.
func requireOpenRefused(t *testing.T, what string, err error, trigger, pkg string) {
	t.Helper()
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("%s: err = %v, want a validation refusal", what, err)
	}
	for _, want := range []string{"trigger " + trigger, pkg, "source.webhook.key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: the refusal does not name %q: %v", what, want, err)
		}
	}
}

// The console's Resume patches `enabled` alone, and on the keyless trigger a
// publisher shipped it is refused with the trigger, the package and the key
// named, leaving the trigger off. A write that sets a key with it turns it
// on; dropping the key while it is on is refused; a signature in place of the
// key is a check the door makes too; turning it off needs neither.
func TestAShippedWebhookTriggerIsEnabledOnlyWithAKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := appleHealthDataset(t)
	enabled, key, shipped := hookState(t, ds)
	if enabled || key != "" {
		t.Fatalf("the install left %s enabled %v under key %q, want it off and keyless", appleHealthHook, enabled, key)
	}

	_, err := ds.Patch(ctx, substrate.ActorPerson, typeTrigger, appleHealthHook, substrate.PatchInput{Properties: map[string]any{"enabled": true}})
	requireOpenRefused(t, "the console's Resume", err, appleHealthHook, appleHealthPackage)
	if enabled, _, _ := hookState(t, ds); enabled {
		t.Fatal("the refused Resume left the trigger on")
	}

	if err := putHook(ctx, ds, true, withHook(shipped, "key", ownersHookKey)); err != nil {
		t.Fatalf("turning it on with a key: %v", err)
	}
	if enabled, key, _ := hookState(t, ds); !enabled || key != ownersHookKey {
		t.Fatalf("after the keyed write: enabled %v under key %q, want on under the owner's", enabled, key)
	}
	requireOpenRefused(t, "dropping the key while it is on", putHook(ctx, ds, true, shipped), appleHealthHook, appleHealthPackage)
	if _, key, _ := hookState(t, ds); key != ownersHookKey {
		t.Fatalf("the refused write left key %q", key)
	}

	signed := withHook(shipped, "signature", map[string]any{
		"algorithm": "hmac-sha256", "signatureHeader": "X-Signature", "encoding": "hex",
		"secret": vocabulary.RecordPath(kindSecret, "health-signing"),
	})
	if err := putHook(ctx, ds, true, signed); err != nil {
		t.Fatalf("turning it on under a signature: %v", err)
	}
	if err := putHook(ctx, ds, false, shipped); err != nil {
		t.Fatalf("turning it off without a key: %v", err)
	}
	if enabled, key, _ := hookState(t, ds); enabled || key != "" {
		t.Fatalf("after turning it off: enabled %v under key %q", enabled, key)
	}
}

// An app's approval turns its webhook on (decision 0202) with one
// version-checked write that carries the key and `enabled` together, which
// the rule admits: approving an Apple Health set up installs the package and
// leaves its trigger on under a key.
func TestAnApprovalKeysAndEnablesAShippedWebhookTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	if err := enginetest.PutCatalogueUnits(ctx, ds, "health"); err != nil {
		t.Fatalf("put the units of the health catalogue the set up installs: %v", err)
	}
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Providers()), catalog.PublishedRoot(kinds.Catalogues()))
	if err != nil {
		t.Fatalf("load the catalog: %v", err)
	}
	details := []substrate.AuthorizationDetail{
		{Type: substrate.DetailPackage, Package: appleHealthPackage, Version: 1},
		{Type: substrate.DetailWebhook, Trigger: appleHealthHook},
	}
	opts := appsetup.Options{Approved: scope.FullApproval()}
	approved, _, err := appsetup.Plan(ctx, ds, cat, details, opts)
	if err != nil {
		t.Fatalf("plan the set up: %v", err)
	}
	if i := appsetup.FirstBlocked(approved); i >= 0 {
		t.Fatalf("the plan blocks row %d: %+v", i, approved[i].Blocked)
	}
	out := appsetup.NewRunner(cat).Run(ctx, ds, substrate.ActorPerson, details, approved, opts, appsetup.RunOptions{},
		func([]string) bool { return true })
	if out.Failure != nil {
		t.Fatalf("the set up stopped: %s", out.Failure.Message)
	}
	if enabled, key, _ := hookState(t, ds); !enabled || !webhookKeyPattern.MatchString(key) {
		t.Fatalf("the approval left %s enabled %v under key %q, want it on under a key", appleHealthHook, enabled, key)
	}
}

// A re-install or an upgrade writes the package's trigger again and leaves it
// as the owner left it: off and keyless stays so, on under a key stays on
// under that key. One a binary from before the rule left open takes the
// shipped document, off, because the install carries over only a keyed
// trigger's switch, and the publisher's hand may not write it open.
func TestAnUpgradeLeavesAShippedWebhookTriggerAsItWas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := appleHealthDataset(t)
	installShippedProvider(t, ds, appleHealthPackage)
	enabled, key, shipped := hookState(t, ds)
	if enabled || key != "" {
		t.Fatalf("a re-install left the trigger enabled %v under key %q, want it off and keyless", enabled, key)
	}

	if err := putHook(ctx, ds, true, withHook(shipped, "key", ownersHookKey)); err != nil {
		t.Fatalf("turning it on with a key: %v", err)
	}
	installShippedProvider(t, ds, appleHealthPackage)
	if enabled, key, _ := hookState(t, ds); !enabled || key != ownersHookKey {
		t.Fatalf("a re-install left the keyed trigger enabled %v under key %q", enabled, key)
	}

	if err := putHook(ctx, ds, false, shipped); err != nil {
		t.Fatalf("turning it off: %v", err)
	}
	if _, err := ds.patchInternal(ctx, substrate.ActorPerson, typeTrigger, appleHealthHook, substrate.PatchInput{Properties: map[string]any{"enabled": true}}); err != nil {
		t.Fatalf("leave it open, as a binary from before the rule could: %v", err)
	}
	installShippedProvider(t, ds, appleHealthPackage)
	if enabled, key, _ := hookState(t, ds); enabled || key != "" {
		t.Fatalf("a re-install over the open trigger left it enabled %v under key %q, want the shipped one", enabled, key)
	}
}

// A trigger a binary from before the rule left open is neither turned off nor
// refused when the repository opens under this one: the open logs it, naming
// the trigger and its package, and the door keeps it as it was. The next
// write that leaves it open is refused, whatever it changes; turning it off
// is not.
func TestAnOpenShippedWebhookTriggerIsReportedAndKept(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logs := &syncBuffer{}
	dsn := MigratedDSN(t)
	opts := []Option{
		WithDataRoot(t.TempDir()), WithKindsFS(SeedKinds), WithCredentialKey(TestCredentialKey),
		WithLogger(slog.New(slog.NewTextHandler(logs, nil))),
	}
	svc, err := OpenForTest(t, ctx, dsn, opts...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	open := func() *dataset {
		t.Helper()
		d, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatalf("open dataset: %v", err)
		}
		return d.(*dataset)
	}
	ds := open()
	installAppleHealth(t, ds)
	if _, err := ds.patchInternal(ctx, substrate.ActorPerson, typeTrigger, appleHealthHook, substrate.PatchInput{Properties: map[string]any{"enabled": true}}); err != nil {
		t.Fatalf("leave it open, as a binary from before the rule could: %v", err)
	}
	const reported = "a webhook trigger a package shipped is enabled with no key"
	if strings.Contains(logs.String(), reported) {
		t.Fatalf("reported before the repository opened again: %s", logs.String())
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if svc, err = OpenForTest(t, ctx, dsn, opts...); err != nil {
		t.Fatalf("restart engine: %v", err)
	}
	ds = open()
	var line string
	for l := range strings.Lines(logs.String()) {
		if strings.Contains(l, reported) {
			line = l
		}
	}
	if !strings.Contains(line, "trigger="+appleHealthHook) || !strings.Contains(line, "package="+appleHealthPackage) {
		t.Fatalf("the open's log reports %q, want the trigger and its package named: %s", line, logs.String())
	}
	if enabled, key, _ := hookState(t, ds); !enabled || key != "" {
		t.Fatalf("the open left the trigger enabled %v under key %q, want it as it was", enabled, key)
	}
	if _, err := svc.CheckWebhook(ctx, testdb.Repository(t), appleHealthHook, ""); err != nil {
		t.Fatalf("the door refused the trigger it admitted before: %v", err)
	}

	_, err = ds.Patch(ctx, substrate.ActorPerson, typeTrigger, appleHealthHook, substrate.PatchInput{Properties: map[string]any{"label": "Apple Health"}})
	requireOpenRefused(t, "a label written over the open trigger", err, appleHealthHook, appleHealthPackage)
	if _, err := ds.Patch(ctx, substrate.ActorPerson, typeTrigger, appleHealthHook, substrate.PatchInput{Properties: map[string]any{"enabled": false}}); err != nil {
		t.Fatalf("turning the open trigger off: %v", err)
	}
}

// A package that ships a webhook trigger on, with no key and no signature,
// is refused whole at install, naming the trigger, so no endpoint opens. The
// same closure shipping it off installs. A closure the repository holds as
// its own, as an import holds a sample, lands its trigger open as shipped.
func TestAPackageShippingAnOpenWebhookTriggerIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const authority = "hooks.example.com"
	closure := func(name string, enabled bool) ([]map[string]any, []substrate.PutInput) {
		pkg := vocabulary.PackageRef(authority, name)
		return []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.BundleManifest(pkg, map[string]any{"description": "relays a webhook", "installs": []any{pkg + "/relay"}}),
			vocabulary.FunctionManifest(pkg, "relay", map[string]any{
				"description": "answers a delivery and writes nothing", "runtime": vocabulary.RuntimePython,
				"source": "def main(input, host):\n    return {\"output\": {}}\n",
			}),
		}, []substrate.PutInput{{Kind: typeTrigger, ID: name + "-hook", Properties: map[string]any{
			"enabled": enabled, "source": map[string]any{"webhook": map[string]any{}},
			"callable": vocabulary.RecordPath(kindFunction, pkg+"/relay"),
		}}}
	}
	ds := openCoreDataset(t)
	published := substrate.BundleInstall{Published: true}

	docs, data := closure("relay", true)
	_, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(authority, "relay"), docs, data, published)
	requireOpenRefused(t, "a published closure shipping its webhook on", err, "relay-hook", authority+"/relay")
	if _, held := ds.registry().PackageByName(authority + "/relay"); held {
		t.Fatal("the refused install left its package declared")
	}
	docs, data = closure("relay", false)
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(authority, "relay"), docs, data, published); err != nil {
		t.Fatalf("the closure shipping its webhook off: %v", err)
	}

	docs, data = closure("copy", true)
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(authority, "copy"), docs, data, substrate.BundleInstall{}); err != nil {
		t.Fatalf("a closure the repository holds as its own, shipping its webhook on: %v", err)
	}
	rec, err := ds.Get(ctx, typeTrigger, "copy-hook")
	if err != nil {
		t.Fatalf("read copy-hook: %v", err)
	}
	if rec.Properties["enabled"] != true || vocabulary.TriggerKey(rec.Properties) != "" {
		t.Fatalf("copy-hook landed %v, want it on and keyless as shipped", rec.Properties)
	}
}

// The owner's own webhook trigger keeps decision 0045's open endpoint, even
// on a callable a publisher shipped, and so does one an imported sample
// shipped, which the import made the owner's copy: Pebble's arrives on and
// keyless. Each turns off and on again with no key.
func TestAnOwnersOwnWebhookTriggerMayStayOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := appleHealthDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: typeTrigger, ID: "my-health-hook", Properties: map[string]any{
		"enabled":  true,
		"source":   map[string]any{"webhook": map[string]any{}},
		"callable": vocabulary.RecordPath(kindFunction, appleHealthPackage+"/applehealthingest"),
	}}); err != nil {
		t.Fatalf("the owner's own keyless trigger: %v", err)
	}
	importVocabulary(t, ds, "pebble")
	for _, id := range []string{"my-health-hook", "pebble-webhook"} {
		for _, on := range []bool{false, true} {
			if _, err := ds.Patch(ctx, substrate.ActorPerson, typeTrigger, id, substrate.PatchInput{Properties: map[string]any{"enabled": on}}); err != nil {
				t.Fatalf("turning %s enabled %v: %v", id, on, err)
			}
		}
	}
}
