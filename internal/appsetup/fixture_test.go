package appsetup

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The fixture catalog: app needs base at 2 first, and ships the account kind,
// a function, and three triggers (a webhook, one its provider signs, and a
// record trigger). The others each require what the catalog cannot install.
const (
	basePkg      = "example.com/base"
	appPkg       = "example.com/app"
	newerBasePkg = "example.com/newerbase"
	needsSample  = "example.com/needssample"
	needsMissing = "example.com/needsmissing"
	samplePkg    = "samples.example.com/notes"
	appAccount   = appPkg + "/account"
	appHook      = "app-webhook"
	signedHook   = "app-signed"
	baseFunction = vocabulary.PackageCore + "/function/" + basePkg + "/sync"
	appFunction  = vocabulary.PackageCore + "/function/" + appPkg + "/ingest"
)

// closure is one package's documents: its header at version, its bundle
// document with its requirements and their floors, and docs.
func closure(pkg string, version int, requires map[string]int, docs ...string) string {
	authority, name := vocabulary.SplitPackageRef(pkg)
	var b strings.Builder
	fmt.Fprintf(&b, "kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: %s\n  package: %s\n  version: %d\n",
		pkg, authority, name, version)
	fmt.Fprintf(&b, "---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: %s\n  package: %s\n  description: The %s package.\n",
		pkg, authority, name, name)
	if len(requires) > 0 {
		b.WriteString("  requires:\n")
		for _, req := range sortedKeys(requires) {
			fmt.Fprintf(&b, "    - %s\n", req)
		}
		b.WriteString("  requiresAtLeast:\n")
		for _, req := range sortedKeys(requires) {
			fmt.Fprintf(&b, "    %s: %d\n", req, requires[req])
		}
	}
	for _, d := range docs {
		b.WriteString("---\n" + d)
	}
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func kindDoc(ref, body string) string {
	authority, pkg, name := vocabulary.SplitKindRef(ref)
	return fmt.Sprintf("kind: substrate.reamde.dev/core/kind\nmetadata:\n  id: %s\ndata:\n  authority: %s\n  package: %s\n  name: %s\n%s",
		ref, authority, pkg, name, body)
}

func functionDoc(ref, writes string) string {
	authority, pkg, name := vocabulary.SplitKindRef(ref)
	return fmt.Sprintf("kind: substrate.reamde.dev/core/function\nmetadata:\n  id: %s\ndata:\n  authority: %s\n  package: %s\n  name: %s\n  permissions:\n    writes: %s\n",
		ref, authority, pkg, name, writes)
}

func triggerDoc(id, source, callable string, enabled bool) string {
	return fmt.Sprintf("kind: substrate.reamde.dev/core/trigger\nmetadata:\n  id: %s\ndata:\n  properties:\n    enabled: %v\n    source:\n%s    callable: %s\n",
		id, enabled, source, callable)
}

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	providers := fstest.MapFS{
		"example.com/base/bundle.yaml": {Data: []byte(closure(basePkg, 2, nil,
			kindDoc(basePkg+"/thing", "  label:\n    singular: Thing\n    plural: Things\n"),
			functionDoc(basePkg+"/sync", "[thing]"),
		))},
		"example.com/app/bundle.yaml": {Data: []byte(closure(appPkg, 4, map[string]int{basePkg: 2},
			kindDoc(appAccount, `  purpose: internal
  traits: [substrate.reamde.dev/core/accountconfig]
  properties:
    displayName: {type: string}
    nickname: {type: string, writer: owner}
    tokenRef: {type: secret}
    status: {type: string, managed: true}
    tokenStatus: {type: string, writer: oauth}
`),
			kindDoc(appPkg+"/item", "  properties:\n    note: {type: string}\n"),
			functionDoc(appPkg+"/ingest", "[item, example.com/base/*]"),
			triggerDoc(appHook, "      webhook: {}\n", appFunction, false),
			triggerDoc(signedHook, "      webhook:\n        signature:\n          algorithm: hmac-sha256\n", appFunction, true),
			triggerDoc("app-records", "      record:\n        kinds: [example.com/app/item]\n", appFunction, true),
		))},
		"example.com/newerbase/bundle.yaml":    {Data: []byte(closure(newerBasePkg, 1, map[string]int{basePkg: 3}))},
		"example.com/needssample/bundle.yaml":  {Data: []byte(closure(needsSample, 1, map[string]int{samplePkg: 1}))},
		"example.com/needsmissing/bundle.yaml": {Data: []byte(closure(needsMissing, 1, map[string]int{"example.com/gone": 1}))},
	}
	samples := fstest.MapFS{
		"samples.example.com/notes/bundle.yaml": {Data: []byte(closure(samplePkg, 1, nil))},
	}
	cat, err := catalog.Load(catalog.ProviderRoot(providers), catalog.SampleRoot(samples))
	if err != nil {
		t.Fatal(err)
	}
	if w := cat.Warnings(); len(w) > 0 {
		t.Fatalf("the fixture catalog dropped %v", w)
	}
	return cat
}

// fakeDataset is a repository in memory: its bundles, its registry, its
// records, the installs the catalog's door ran, and the upgrade preview it
// answers. Any verb it does not model panics.
type fakeDataset struct {
	substrate.Dataset

	mu       sync.Mutex
	statuses map[string]substrate.BundleStatus
	kinds    map[string]substrate.KindInfo
	records  map[string]*substrate.Record
	installs []string
	head     int64
	previews int
	preview  substrate.BundleUpgrade
	// previewErr fails every preview; beforeInstall runs as an install of
	// the package it names starts; afterPut runs once a write landed;
	// putErr refuses a write it answers an error for.
	previewErr    error
	beforeInstall func(id string)
	afterPut      func(in substrate.PutInput)
	putErr        func(in substrate.PutInput) error
}

func newFakeDataset() *fakeDataset {
	return &fakeDataset{
		statuses: map[string]substrate.BundleStatus{}, kinds: map[string]substrate.KindInfo{},
		records: map[string]*substrate.Record{},
	}
}

func recordKey(kind, id string) string { return kind + "\x00" + id }

func (d *fakeDataset) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: "ada.example.com", Authority: "ada.example.com"}
}

// Head moves at every read, so no preview is answered from the catalog's
// cache and each test's preview is the one counted.
func (d *fakeDataset) Head(context.Context) (substrate.ChangelogHead, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.head++
	return substrate.ChangelogHead{Seq: d.head, Generation: "g"}, nil
}

func (d *fakeDataset) Kinds(context.Context) ([]substrate.KindInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]substrate.KindInfo, 0, len(d.kinds))
	for _, k := range d.kinds {
		out = append(out, k)
	}
	return out, nil
}

func (d *fakeDataset) BundleStatusesUncounted(context.Context) ([]substrate.BundleStatus, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]substrate.BundleStatus, 0, len(d.statuses))
	for _, st := range d.statuses {
		out = append(out, st)
	}
	return out, nil
}

func (d *fakeDataset) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.previews++
	return d.preview, d.previewErr
}

func (d *fakeDataset) Get(_ context.Context, kind, id string) (*substrate.Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec, ok := d.records[recordKey(kind, id)]
	if !ok {
		return nil, fmt.Errorf("%w: %s %s", substrate.ErrNotFound, kind, id)
	}
	cp := *rec
	cp.Properties = deepCopy(rec.Properties)
	return &cp, nil
}

// Put merges into the row it finds and holds ifVersion to it, a tombstone
// included, as the engine's does.
func (d *fakeDataset) Put(_ context.Context, _ substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	if d.putErr != nil {
		if err := d.putErr(in); err != nil {
			return nil, err
		}
	}
	d.mu.Lock()
	held := d.records[recordKey(in.Kind, in.ID)]
	version := int64(0)
	if held != nil {
		version = held.Version
	}
	if in.IfVersion != nil && *in.IfVersion != version {
		d.mu.Unlock()
		return nil, fmt.Errorf("%w: %s is at version %d", substrate.ErrConflict, in.ID, version)
	}
	props := map[string]any{}
	if held != nil {
		props = deepCopy(held.Properties)
	}
	maps.Copy(props, deepCopy(in.Properties))
	rec := &substrate.Record{Kind: in.Kind, ID: in.ID, Version: version + 1, Properties: props}
	d.records[recordKey(in.Kind, in.ID)] = rec
	cp := *rec
	d.mu.Unlock()
	if d.afterPut != nil {
		d.afterPut(in)
	}
	return &cp, nil
}

func (d *fakeDataset) InstallBundleClosure(_ context.Context, _ substrate.Actor, docs []map[string]any, data []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	var id string
	var kinds []substrate.KindInfo
	for _, doc := range docs {
		md, _ := doc["metadata"].(map[string]any)
		ref, _ := md["id"].(string)
		body, _ := doc["data"].(map[string]any)
		switch doc["kind"] {
		case vocabulary.CoreKind(vocabulary.DocBundle):
			id = ref
		case vocabulary.CoreKind(vocabulary.DocKind):
			kinds = append(kinds, substrate.KindInfo{Identity: ref, Source: vocabulary.SourcePublished, Definition: body})
		}
	}
	if d.beforeInstall != nil {
		d.beforeInstall(id)
	}
	d.mu.Lock()
	d.installs = append(d.installs, id)
	d.statuses[id] = substrate.BundleStatus{ID: id, Installed: true, Enabled: true, Version: opts.ShippedVersion, ShippedVersion: opts.ShippedVersion}
	for _, k := range kinds {
		d.kinds[k.Identity] = k
	}
	d.mu.Unlock()
	for _, in := range data {
		if _, err := d.Put(context.Background(), substrate.ActorSystem, in); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// installed marks a package installed at version, stamped as a provider
// install stamps it, its kinds in the registry.
func (d *fakeDataset) installed(cat *catalog.Catalog, id string, version int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.statuses[id] = substrate.BundleStatus{ID: id, Installed: true, Enabled: true, Version: version, ShippedVersion: version}
	if b, ok := cat.ByID(id); ok {
		for _, k := range b.Kinds() {
			d.kinds[k.Identity] = substrate.KindInfo{Identity: k.Identity, Source: vocabulary.SourcePublished, Definition: k.Definition, Label: k.Label}
		}
	}
}

// declared marks a package the owner declared by hand at version: no
// provider stamp, and its kinds the repository's own.
func (d *fakeDataset) declared(cat *catalog.Catalog, id string, version int64) {
	d.installed(cat, id, version)
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.statuses[id]
	st.ShippedVersion = 0
	d.statuses[id] = st
	for ref, k := range d.kinds {
		if vocabulary.KindPackage(ref) == id {
			k.Source = vocabulary.SourceInstalled
			d.kinds[ref] = k
		}
	}
}

func (d *fakeDataset) put(rec substrate.Record) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records[recordKey(rec.Kind, rec.ID)] = &rec
}

func (d *fakeDataset) record(kind, id string) *substrate.Record {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.records[recordKey(kind, id)]
}

func (d *fakeDataset) installedSoFar() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.installs...)
}

func deepCopy(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if m, ok := v.(map[string]any); ok {
			v = deepCopy(m)
		}
		out[k] = v
	}
	return out
}

// detail is one authorization detail.
func pkgDetail(id string, version int64) substrate.AuthorizationDetail {
	return substrate.AuthorizationDetail{Type: substrate.DetailPackage, Package: id, Version: version}
}

func accountDetail(id string, props map[string]any) substrate.AuthorizationDetail {
	return substrate.AuthorizationDetail{Type: substrate.DetailAccount, Kind: appAccount, ID: id, Properties: props}
}

func webhookDetail(trigger string) substrate.AuthorizationDetail {
	return substrate.AuthorizationDetail{Type: substrate.DetailWebhook, Trigger: trigger}
}

func plan(t *testing.T, ds substrate.Dataset, cat *catalog.Catalog, details ...substrate.AuthorizationDetail) []substrate.AuthorizationSetup {
	t.Helper()
	rows, _, err := Plan(context.Background(), ds, cat, details, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// wantBlocked fails unless block is reason, with fix, its sentence one the
// owner reads as one: a capital, no id, a full stop.
func wantBlocked(t *testing.T, what string, block *substrate.AuthorizationBlock, reason, fix string) {
	t.Helper()
	if block == nil || block.Reason != reason || block.Fix != fix {
		t.Fatalf("%s blocked = %+v, want %s with fix %q", what, block, reason, fix)
	}
	if p := block.Problem; p == "" || p[0] < 'A' || p[0] > 'Z' || !strings.HasSuffix(p, ".") || strings.ContainsAny(p, ":/") {
		t.Fatalf("%s says %q, not a sentence free of ids", what, p)
	}
}
