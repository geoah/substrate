package engine

// The operator's function secrets (decision 0235, operatorfunction.go): the
// key in the defaults file's functionSecrets reaches the listed consumers of
// the published package and nothing else, steps aside for the person's own
// key, never reaches a copy, and every call it pays for reserves and settles
// at its price, or at nothing when its body never started. A trigger of such
// a function waits at the allowance and delivers after the reset. Most of
// these tests resolve the invocation config directly; the ones that run a
// body run it held to loopback (requireConfinedBodies), so even a Firecrawl
// body a regression hands the operator's key cannot reach Firecrawl.
// internal/providertest holds what the bodies do with the key.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

const (
	firecrawlProvider = "providers.substrate.reamde.dev/firecrawl"
	// operatorFirecrawlKey is the operator's key in these fixtures; it is
	// never a real key.
	operatorFirecrawlKey = "fc-operator-key-fixture"
	ownFirecrawlKey      = "fc-own-key-fixture"
)

// firecrawlOperatorConfig is a defaults file that pays for consumers of the
// Firecrawl provider at price cents a call, under an allowance of cents.
func firecrawlOperatorConfig(allowance int64, price string, consumers ...string) *operatordefaults.Config {
	prices := map[string]string{}
	for _, c := range consumers {
		prices[c] = price
	}
	return &operatordefaults.Config{
		FunctionSecrets: map[string]*operatordefaults.FunctionSecret{
			firecrawlProvider: {APIKey: operatordefaults.NewSecret(operatorFirecrawlKey), Consumers: consumers, PricePerCall: prices},
		},
		Budget: operatordefaults.Budget{CentsPerDay: allowance},
	}
}

// requireConfinedBodies fails the test unless every body it starts is held to
// loopback (TestMain's runner.ConfineBodyEgress) on a host that enforces it:
// a Firecrawl body handed the operator's key with no baseUrl dials the real
// API, so a regression in the injection, the allowance or the body's own
// checks must meet a refused connect, not Firecrawl.
func requireConfinedBodies(t *testing.T) {
	t.Helper()
	if err := runner.Shared.BodyEgressConfined(); err != nil {
		t.Fatalf("a body run by this test could reach the internet: %v", err)
	}
}

// openFirecrawlDataset opens a repository under cfg with the shipped Firecrawl
// provider installed through the catalog's install door, as an owner does.
func openFirecrawlDataset(t *testing.T, cfg *operatordefaults.Config, opts ...Option) *dataset {
	t.Helper()
	requireConfinedBodies(t)
	ds := openInternalDataset(t, append([]Option{WithOperatorDefaults(cfg, "Example")}, opts...)...)
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now
	installShippedProvider(t, ds, firecrawlProvider)
	return ds
}

func installShippedProvider(t *testing.T, ds *dataset, id string) {
	t.Helper()
	c, err := catalog.Load(catalog.ProviderRoot(kinds.Providers()), catalog.PublishedRoot(kinds.Catalogues()))
	if err != nil {
		t.Fatalf("load the catalog: %v", err)
	}
	if _, _, err := c.Install(context.Background(), substrate.ActorAPI, id, ds); err != nil {
		t.Fatalf("install %s: %v", id, err)
	}
}

// functionConfig resolves the invocation config a call of fn would get.
func functionConfig(t *testing.T, ds *dataset, fn string) (map[string]any, []string, *operatorCharge, error) {
	t.Helper()
	f, err := ds.registry().ResolveFunction(fn)
	if err != nil {
		t.Fatalf("resolve %s: %v", fn, err)
	}
	return ds.resolveFunctionConfig(context.Background(), f)
}

// settingsOf is the `config.settings` a body would read.
func settingsOf(cfg map[string]any) map[string]any {
	s, _ := cfg["settings"].(map[string]any)
	return s
}

// installFirecrawlCopy lands what an earlier import of the Firecrawl sample
// left behind: the closure under the repository's own authority, its package
// `installed`, its page tool still `scrapepage`, and its two settings empty
// and pinned. offered marks both functions offered under the names a copy
// derives, as an owner who edited their copy would.
func installFirecrawlCopy(t *testing.T, ds *dataset, offered bool) string {
	t.Helper()
	ctx := context.Background()
	home := ds.Repository().Authority
	declarations, data := shippedDocuments(t, firecrawlProvider)
	rehome := func(v any) any { return rehomeValue(v, "providers.substrate.reamde.dev", home) }
	var docs []map[string]any
	for _, d := range declarations {
		d, _ = rehome(d).(map[string]any)
		body, _ := d["data"].(map[string]any)
		if body != nil {
			if _, isFn := body["offer"]; isFn {
				delete(body, "offer")
				if offered {
					body["offer"] = map[string]any{"agents": true}
				}
			}
		}
		docs = append(docs, d)
	}
	rename := func(v any) any { return rehomeValue(v, home+"/firecrawl/webscrape", home+"/firecrawl/scrapepage") }
	for i := range docs {
		docs[i], _ = rename(docs[i]).(map[string]any)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the copy: %v", err)
	}
	for _, d := range data {
		d, _ = rehome(d).(map[string]any)
		meta, _ := d["metadata"].(map[string]any)
		body, _ := d["data"].(map[string]any)
		props, _ := body["properties"].(map[string]any)
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: d["kind"].(string), ID: meta["id"].(string), Properties: props}); err != nil {
			t.Fatalf("put the copy's %v: %v", meta["id"], err)
		}
	}
	return home + "/firecrawl"
}

// shippedDocuments reads one shipped provider directory's documents, split
// as the catalog splits them: the declarations the batch apply takes, and the
// data records put beside them.
func shippedDocuments(t *testing.T, pkg string) (declarations, data []map[string]any) {
	t.Helper()
	fsys := kinds.Served()
	entries, err := fs.ReadDir(fsys, pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		raw, err := fs.ReadFile(fsys, pkg+"/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var m map[string]any
			if err := dec.Decode(&m); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s/%s: %v", pkg, e.Name(), err)
			}
			kind, _ := m["kind"].(string)
			if vocabulary.KindPackage(kind) == vocabulary.PackageCore && vocabulary.VocabularyDocumentKind(vocabulary.KindName(kind)) {
				declarations = append(declarations, m)
			} else {
				data = append(data, m)
			}
		}
	}
	return declarations, data
}

// rehomeValue replaces old with replacement in every string of v.
func rehomeValue(v any, old, replacement string) any {
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, old, replacement)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = rehomeValue(e, old, replacement)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = rehomeValue(e, old, replacement)
		}
		return out
	}
	return v
}

// assertKeyNeverStored walks every place a repository keeps anything, the
// records, the changelog and the data root a recovery export is a tar of,
// for key.
func assertKeyNeverStored(t *testing.T, ds *dataset, key string) {
	t.Helper()
	for _, q := range []string{
		`SELECT count(*) FROM records WHERE props::text LIKE '%' || $1 || '%' OR title LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM changelog WHERE payload::text LIKE '%' || $1 || '%'`,
	} {
		var n int
		if err := ds.db.QueryRow(q, key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d rows hold the operator's key: %s", n, q)
		}
	}
	if err := filepath.WalkDir(ds.svc.dataRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte(key)) {
			t.Fatalf("%s holds the operator's key", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The operator's key reaches the consumers the file names of the published
// package, alone in config.settings and armed in the scrubber, with the
// call's price reserved; a function the file does not name, another
// package's function and every store get nothing.
func TestOperatorFunctionKeyReachesOnlyTheListedConsumers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openFirecrawlDataset(t, firecrawlOperatorConfig(1000, "0.25", "websearch"))
	// The person wrote a base URL; it never rides an operator-paid call.
	mustPatchInternal(t, ds, kindSetting, firecrawlProvider+"/baseUrl", map[string]any{"value": "http://127.0.0.1:9"})

	cfg, secrets, charge, err := functionConfig(t, ds, firecrawlProvider+"/websearch")
	if err != nil {
		t.Fatalf("websearch config: %v", err)
	}
	if got := settingsOf(cfg); len(got) != 1 || got[operatorFunctionSecretName] != operatorFirecrawlKey {
		t.Fatalf("websearch settings = %v, want the operator's key alone", got)
	}
	if !slices.Contains(secrets, operatorFirecrawlKey) {
		t.Fatal("the operator's key is not armed in the invocation scrubber")
	}
	if charge == nil || charge.cents != 0.25 {
		t.Fatalf("charge = %+v, want one reserved at 0.25 cents", charge)
	}
	if l := operatorLedger(t, ds); len(l) != 1 || l[0].slot != substrate.SlotFunction || l[0].ceiling != 0.25 || l[0].settled != -1 {
		t.Fatalf("ledger = %+v, want one open function reservation at 0.25", l)
	}
	charge.settle(ctx, ds, true)
	if l := operatorLedger(t, ds); len(l) != 1 || l[0].settled != 0.25 {
		t.Fatalf("ledger after the call = %+v, want it settled at the price", l)
	}

	// webscrape is the same package's, and the file does not name it.
	cfg, _, charge, err = functionConfig(t, ds, firecrawlProvider+"/webscrape")
	if err != nil {
		t.Fatalf("webscrape config: %v", err)
	}
	if got := settingsOf(cfg); got[operatorFunctionSecretName] != nil || got["baseUrl"] != "http://127.0.0.1:9" || charge != nil {
		t.Fatalf("webscrape settings = %v, charge = %+v, want the person's settings and no key", got, charge)
	}

	// Another package's function with an empty apiKey of its own.
	const other = "other.test.dev/keyed"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(other, 0),
		vocabulary.BundleManifest(other, map[string]any{"description": "a keyed bundle", "installs": []any{other + "/websearch"}}),
		vocabulary.FunctionManifest(other, "websearch", map[string]any{
			"description": "searches", "runtime": vocabulary.RuntimePython,
			"source": "def main(input, host):\n    return {\"output\": {}}\n",
		}),
	}); err != nil {
		t.Fatalf("install the other bundle: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kindSecret, ID: other + "/apiKey", Properties: map[string]any{"required": true}}); err != nil {
		t.Fatal(err)
	}
	cfg, _, charge, err = functionConfig(t, ds, other+"/websearch")
	if err != nil {
		t.Fatalf("other config: %v", err)
	}
	if got := settingsOf(cfg); got[operatorFunctionSecretName] != nil || charge != nil {
		t.Fatalf("another package's settings = %v, charge = %+v, want no key", got, charge)
	}
	if n := len(operatorLedger(t, ds)); n != 1 {
		t.Fatalf("ledger rows = %d, want the one websearch call", n)
	}

	// The secret record stays the person's and empty, and the key is in no
	// store.
	rec, err := ds.Get(ctx, kindSecret, firecrawlProvider+"/apiKey")
	if err != nil {
		t.Fatal(err)
	}
	if v, held := rec.Properties[propSettingValue]; held && v != "" {
		t.Fatalf("the shipped secret reads %v", v)
	}
	assertKeyNeverStored(t, ds, operatorFirecrawlKey)
}

// The person's own key wins, and an empty one is none: the operator's key
// returns. While the operator covers the key, the bundle asks for none.
func TestOperatorFunctionKeyStepsAsideForTheOwnKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openFirecrawlDataset(t, firecrawlOperatorConfig(1000, "0.1", "websearch", "webscrape"))

	st, err := ds.BundleStatus(ctx, firecrawlProvider)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Setup) != 0 {
		t.Fatalf("setup = %+v, want nothing asked while the operator covers the key", st.Setup)
	}

	mustPatchInternal(t, ds, kindSecret, firecrawlProvider+"/apiKey", map[string]any{"value": ownFirecrawlKey})
	for _, fn := range []string{"websearch", "webscrape"} {
		cfg, _, charge, err := functionConfig(t, ds, firecrawlProvider+"/"+fn)
		if err != nil {
			t.Fatalf("%s config: %v", fn, err)
		}
		if got := settingsOf(cfg); got[operatorFunctionSecretName] != ownFirecrawlKey || got["baseUrl"] != "https://api.firecrawl.dev" || charge != nil {
			t.Fatalf("%s on the own key: settings = %v, charge = %+v", fn, got, charge)
		}
	}
	if n := len(operatorLedger(t, ds)); n != 0 {
		t.Fatalf("a call on the person's key wrote %d ledger rows", n)
	}

	mustPatchInternal(t, ds, kindSecret, firecrawlProvider+"/apiKey", map[string]any{"value": ""})
	cfg, _, charge, err := functionConfig(t, ds, firecrawlProvider+"/websearch")
	if err != nil {
		t.Fatal(err)
	}
	if got := settingsOf(cfg); got[operatorFunctionSecretName] != operatorFirecrawlKey || charge == nil {
		t.Fatalf("an emptied own key: settings = %v, charge = %+v, want the operator's", got, charge)
	}
	charge.settle(ctx, ds, false)
	if l := operatorLedger(t, ds); len(l) != 1 || l[0].settled != 0 {
		t.Fatalf("ledger = %+v, want a call that never ran settled at nothing", l)
	}
}

// A copy of the retired sample, under the repository's own authority, never
// receives the operator's key, even from a file that names it: its package is
// installed, not published, so the repository could edit what the key
// reaches.
func TestOperatorFunctionKeyNeverReachesACopy(t *testing.T) {
	t.Parallel()
	ds := openFirecrawlDataset(t, firecrawlOperatorConfig(1000, "0.1", "websearch", "scrapepage"))
	copyPkg := installFirecrawlCopy(t, ds, false)
	// A file the boot would refuse (it names no shipped provider) still
	// reaches no copy.
	ds.svc.operatorDefaults.FunctionSecrets[copyPkg] = ds.svc.operatorDefaults.FunctionSecrets[firecrawlProvider]

	for _, fn := range []string{"websearch", "scrapepage"} {
		cfg, _, charge, err := functionConfig(t, ds, copyPkg+"/"+fn)
		if err != nil {
			t.Fatalf("copy %s config: %v", fn, err)
		}
		if got := settingsOf(cfg); got[operatorFunctionSecretName] != nil || charge != nil {
			t.Fatalf("copy %s: settings = %v, charge = %+v, want no key", fn, got, charge)
		}
	}
	if n := len(operatorLedger(t, ds)); n != 0 {
		t.Fatalf("a copy's call wrote %d ledger rows", n)
	}
	got, err := ds.OperatorDefaults(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if it := operatorItem(t, got, "function/"+copyPkg); it.Source != substrate.SourceNone || it.Readiness != substrate.ReadinessNotSetUp {
		t.Fatalf("the copy's item = %+v, want no key resolving", it)
	}
}

// At the allowance a call is refused before its body runs, with the
// allowance's words, never handed an empty key; a call that ran settles at
// its price whatever its body answered.
func TestOperatorFunctionKeyRefusedAtTheAllowance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openFirecrawlDataset(t, firecrawlOperatorConfig(1, "0.6", "websearch", "webscrape"))

	// The body refuses the url before it builds a request; the call ran, so
	// it settles at the price. A body that lost the check would post to
	// Firecrawl's own origin with the operator's key, and the confined egress
	// refuses that connect, so the error below would not be this one.
	_, _, err := ds.CallFunction(ctx, substrate.ActorAPI, firecrawlProvider+"/webscrape", map[string]any{"url": "http://not-https.example.com"})
	if err == nil || !strings.Contains(err.Error(), "url must be an absolute https:// URL") {
		t.Fatalf("webscrape of an http url = %v, want the body's refusal", err)
	}
	if l := operatorLedger(t, ds); len(l) != 1 || l[0].ceiling != 0.6 || l[0].settled != 0.6 {
		t.Fatalf("ledger = %+v, want one call settled at 0.6", l)
	}

	_, _, err = ds.CallFunction(ctx, substrate.ActorAPI, firecrawlProvider+"/websearch", map[string]any{"query": "anything"})
	if !errors.Is(err, substrate.ErrAllowance) || !strings.Contains(err.Error(), "today's allowance from Example") ||
		!strings.Contains(err.Error(), "add your own key") {
		t.Fatalf("websearch past the allowance = %v, want the allowance refusal", err)
	}
	if n := len(operatorLedger(t, ds)); n != 1 {
		t.Fatalf("ledger rows = %d, want the refused call to reserve nothing", n)
	}
	got, err := ds.OperatorDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Operator.Allowance; a.UsedCents != 1 || len(a.BySlot) != 1 || a.BySlot[0].Slot != substrate.SlotFunction {
		t.Fatalf("allowance = %+v, want the function slot's cent", a)
	}
	if it := operatorItem(t, got, "function/"+firecrawlProvider); it.Source != substrate.SourceOperator || !it.OperatorOffers ||
		it.Readiness != substrate.ReadinessReady || it.Target != firecrawlProvider || it.Slot != substrate.SlotFunction || it.Label != "Firecrawl" {
		t.Fatalf("firecrawl item at the limit = %+v", it)
	}
}

// The read lists one function item per package the file names: the
// operator's while the repository holds no key, the person's once it does,
// and none while the provider is not installed.
func TestOperatorDefaultsReportsFunctionSecrets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t, WithOperatorDefaults(firecrawlOperatorConfig(100, "0.1", "websearch"), "Example"))
	read := func() substrate.OperatorDefault {
		t.Helper()
		got, err := ds.OperatorDefaults(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return operatorItem(t, got, "function/"+firecrawlProvider)
	}
	if it := read(); it.Source != substrate.SourceNone || !it.OperatorOffers || it.Readiness != substrate.ReadinessNotSetUp ||
		!strings.Contains(it.Message, "is not installed") {
		t.Fatalf("before the install = %+v", it)
	}
	installShippedProvider(t, ds, firecrawlProvider)
	if it := read(); it.Source != substrate.SourceOperator || it.Readiness != substrate.ReadinessReady || it.Message != "" {
		t.Fatalf("installed = %+v", it)
	}
	mustPatchInternal(t, ds, kindSecret, firecrawlProvider+"/apiKey", map[string]any{"value": ownFirecrawlKey})
	if it := read(); it.Source != substrate.SourceOwn || !it.OperatorOffers || it.Readiness != substrate.ReadinessReady {
		t.Fatalf("on the own key = %+v", it)
	}
}

// A package the file lists under both install and functionSecrets whose
// install failed reads install-failed with the installer's message on its
// function item, as an oauth item does, until the package is installed.
func TestOperatorDefaultsReportsAFailedFunctionInstall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := firecrawlOperatorConfig(100, "0.1", "websearch")
	cfg.Install = []string{firecrawlProvider}
	installer := func(context.Context, substrate.Dataset, string) error {
		return errors.New("the catalog is unreachable")
	}
	ds := openInternalDataset(t, WithOperatorDefaults(cfg, "Example"), WithOperatorInstaller(installer))
	read := func() substrate.OperatorDefault {
		t.Helper()
		got, err := ds.OperatorDefaults(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return operatorItem(t, got, "function/"+firecrawlProvider)
	}
	if it := read(); it.Source != substrate.SourceNone || !it.OperatorOffers || it.Readiness != substrate.ReadinessInstallFailed ||
		!strings.Contains(it.Message, "the catalog is unreachable") {
		t.Fatalf("after the failed install = %+v", it)
	}
	installShippedProvider(t, ds, firecrawlProvider)
	if it := read(); it.Source != substrate.SourceOperator || it.Readiness != substrate.ReadinessReady || it.Message != "" {
		t.Fatalf("installed = %+v", it)
	}
}

// The seeded assistant takes both Firecrawl tools by its `toolsFrom`, under
// the names the provider declares, and a copy offering the names a copy
// derives sits beside them instead of shadowing them.
func TestDefaultAgentTakesFirecrawlsTools(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t, WithLLMSampleSeed(true))
	fake := provisionAgents(t, ds)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "openai",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-openai"},
	}); err != nil {
		t.Fatal(err)
	}
	installShippedProvider(t, ds, firecrawlProvider)
	installFirecrawlCopy(t, ds, true)

	fake.script("gpt-5", fakeTurn{content: "hello"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, testdb.Repository(t)+"/llm/substrate", substrate.ChatInput{Message: "hi"}, nil); err != nil {
		t.Fatalf("chat: %v", err)
	}
	reqs := fake.requestsOf("gpt-5")
	if len(reqs) != 1 {
		t.Fatalf("completions: %d, want 1", len(reqs))
	}
	names := toolNamesSent(t, reqs[0])
	for _, name := range []string{"web_search", "web_scrape", "firecrawl_websearch", "firecrawl_scrapepage"} {
		if !slices.Contains(names, name) {
			t.Fatalf("tools = %v, want %s among them", names, name)
		}
	}
}

// --- the allowance and the runner, on a metered bundle that dials nothing ---

const (
	meterPackage = "meter.example.com/meter"
	meterReading = meterPackage + "/reading"
	meterPing    = meterPackage + "/ping"
	// meterAccount binds the core `sync` trait, so a delivery of `stamp`,
	// the kind's own package's function, stamps the record's sync state.
	meterAccount = meterPackage + "/account"
	// operatorMeterKey is the operator's key for the meter bundle in these
	// fixtures; it is never a real key.
	operatorMeterKey = "meter-operator-key-fixture"
)

// openMeterDataset opens a repository whose operator pays for the published
// meter bundle's two functions, `stamp` at 0.6 cents a call and `fussy` at
// 0.5, under an allowance of allowance cents a day, on a clock the test
// moves, beside a `sync`-trait account kind. Neither body opens a socket. `stamp` writes one reading per call,
// named for the record or the occurrence it was delivered. `fussy` refuses
// every registration of its body after the first, so a call after its process
// is retired fails before the body starts.
func openMeterDataset(t *testing.T, allowance int64) (*dataset, *testClock) {
	t.Helper()
	requireConfinedBodies(t)
	ctx := context.Background()
	cfg := &operatordefaults.Config{
		FunctionSecrets: map[string]*operatordefaults.FunctionSecret{
			meterPackage: {
				APIKey: operatordefaults.NewSecret(operatorMeterKey), Consumers: []string{"stamp", "fussy"},
				PricePerCall: map[string]string{"stamp": "0.6", "fussy": "0.5"},
			},
		},
		Budget: operatordefaults.Budget{CentsPerDay: allowance},
	}
	ds := openInternalDataset(t, WithOperatorDefaults(cfg, "Example"))
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now
	named := map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}
	docs := []map[string]any{
		vocabulary.PackageManifest(meterPackage, 1),
		vocabulary.BundleManifest(meterPackage, map[string]any{
			"description": "a metered bundle",
			"installs":    []any{meterPackage + "/stamp", meterPackage + "/fussy", meterReading, meterPing, meterAccount},
		}),
		vocabulary.KindManifest(meterPackage, map[string]any{"singular": "reading"}, named),
		vocabulary.KindManifest(meterPackage, map[string]any{"singular": "ping"}, named),
		vocabulary.KindManifest(meterPackage, map[string]any{"singular": "account"}, meterSyncKind()),
		vocabulary.FunctionManifest(meterPackage, "stamp", map[string]any{
			"description": "writes one reading per delivery", "runtime": vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{meterReading}},
			"source": `
def main(input, host):
    env = input.get("envelope") or {}
    if env.get("fire"):
        rid = "fire-" + env["fire"]["id"]
    else:
        rid = "rec-" + env["record"]["id"]
    return {"effects": [{"action": "put", "kind": "` + meterReading + `", "id": rid,
                         "properties": {"name": rid}}]}
`,
		}),
		vocabulary.FunctionManifest(meterPackage, "fussy", map[string]any{
			"description": "registers once", "runtime": vocabulary.RuntimePython,
			"source": `
import os
# The work dir outlives this test binary, so the mark names the binary.
_MARK = os.path.join(os.environ["TMPDIR"], "registered-under-%d" % os.getppid())
if os.path.exists(_MARK):
    raise RuntimeError("fussy registers once")
open(_MARK, "w").close()

def main(input, host):
    return {"output": {"ran": True}}
`,
		}),
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(meterPackage)), docs, nil,
		substrate.BundleInstall{Published: true}); err != nil {
		t.Fatalf("install the meter bundle: %v", err)
	}
	return ds, clock
}

// meterSyncKind declares the account kind's properties the way a provider's
// account kind binds the `sync` trait: the owner's two hands `writer: owner`,
// the rest `writer: connector`.
func meterSyncKind() map[string]any {
	props := map[string]any{"name": map[string]any{"type": "string"}}
	for name, typ := range map[string]string{
		"syncState": "string", "syncMessage": "string", "lastSyncedAt": "datetime",
		"lastSyncStartedAt": "datetime", "lastSyncDurationMs": "int", "syncRequestedAck": "datetime",
		"syncProgress": "json", "syncError": "string", "syncErrorAt": "datetime", "syncStreams": "json",
	} {
		props[name] = map[string]any{"type": typ, "writer": "connector", "fts": false}
	}
	for name, typ := range map[string]string{"syncRequestedAt": "datetime", "syncPaused": "bool"} {
		props[name] = map[string]any{"type": typ, "writer": "owner", "fts": false}
	}
	return map[string]any{
		"traits":          []any{vocabulary.TraitSyncCore},
		"displayTemplate": "{name}",
		"properties":      props,
	}
}

// spendFunctionCents settles one function reservation of cents, today, as a
// call that already ran.
func spendFunctionCents(t *testing.T, ds *dataset, cents float64) {
	t.Helper()
	r, err := ds.reserveOperator(context.Background(), substrate.SlotFunction, cents)
	if err != nil {
		t.Fatalf("reserve %v cents: %v", cents, err)
	}
	ds.settleOperator(context.Background(), r, cents)
}

// meterTrigger writes a trigger of `stamp` from source and answers its id.
func meterTrigger(t *testing.T, ds *dataset, source map[string]any) string {
	t.Helper()
	rec, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: typeTrigger, Properties: map[string]any{
		"enabled": true, "source": source,
		"callable": vocabulary.RecordPath(kindFunction, meterPackage+"/stamp"),
	}})
	if err != nil {
		t.Fatalf("put the trigger: %v", err)
	}
	return rec.ID
}

// requireReading fails unless the reading id exists, or, with want false,
// unless it does not.
func requireReading(t *testing.T, ds *dataset, id string, want bool) {
	t.Helper()
	_, err := ds.Get(context.Background(), meterReading, id)
	switch {
	case want && err != nil:
		t.Fatalf("reading %s was not written: %v", id, err)
	case !want && err == nil:
		t.Fatalf("reading %s was written while the trigger was held", id)
	case !want && !errors.Is(err, substrate.ErrNotFound):
		t.Fatal(err)
	}
}

// requireNoFailures fails if the trigger parked anything.
func requireNoFailures(t *testing.T, ds *dataset, triggerID string) {
	t.Helper()
	failures, err := ds.TriggerFailures(context.Background(), triggerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("the trigger parked %+v; an allowance hold parks nothing", failures)
	}
}

// heldText is the trigger's status `held`.
func heldText(t *testing.T, ds *dataset, triggerID string) string {
	t.Helper()
	statuses, err := ds.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st.ID == triggerID {
			return st.Held
		}
	}
	t.Fatalf("no status for trigger %s", triggerID)
	return ""
}

// A record trigger of a function on the operator's key waits at the
// allowance: the change is not claimed, nothing parks, its status says held,
// and the first pass after the reset delivers that same change with no hand's
// retry. A call the allowance refuses after the walk let it through, its room
// taken by a call racing it, leaves the cursor on the change too.
func TestAnOperatorPaidRecordTriggerWaitsForTheReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := openMeterDataset(t, 1)
	trigger := meterTrigger(t, ds, map[string]any{"record": map[string]any{"kinds": []any{meterPing}}})
	// A ping's id is minted (decision 0228); stamp names its reading
	// rec-<that id>.
	ping := func(name string) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: meterPing, ID: fid(name), Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
	}
	process := func() {
		t.Helper()
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatalf("process: %v", err)
		}
	}

	ping("a")
	process()
	requireReading(t, ds, "rec-"+fid("a"), true)

	// 0.6 of the 1 cent is spent: the next call does not fit.
	ping("b")
	process()
	process()
	requireReading(t, ds, "rec-"+fid("b"), false)
	requireNoFailures(t, ds, trigger)
	if held := heldText(t, ds, trigger); !strings.HasPrefix(held, allowancePrefix) {
		t.Fatalf("status held = %q, want the allowance's words", held)
	}

	// The walk holds before the claim.
	tr, _, err := ds.triggerByID(ctx, trigger)
	if err != nil {
		t.Fatal(err)
	}
	if _, held, err := ds.holdDelivery(ctx, tr); err != nil || !held {
		t.Fatalf("the walk's hold = %v, %v; want the delivery held before its claim", held, err)
	}

	// The race: the walk let the delivery through, then another call took the
	// room before this one reserved. Nothing runs, the cursor stays on b.
	cursor, err := ds.ensureCursor(ctx, trigger)
	if err != nil {
		t.Fatal(err)
	}
	changes, _, err := ds.changesPast(ctx, tr, cursor)
	if err != nil {
		t.Fatal(err)
	}
	matched := matchChanges(tr, tr.selfActors(ds.registry()), changes)
	if len(matched) != 1 || matched[0].RecordID != fid("b") {
		t.Fatalf("the change owed is %+v, want b's", matched)
	}
	_, next, err := ds.deliverWithRetry(ctx, tr, matched[0], cursor)
	if !errors.Is(err, errUnstartedAtAllowance) || !errors.Is(err, substrate.ErrAllowance) || next != cursor {
		t.Fatalf("a delivery refused at its reservation = next %d (cursor %d), %v; want the cursor kept and the allowance's refusal", next, cursor, err)
	}
	requireReading(t, ds, "rec-"+fid("b"), false)
	requireNoFailures(t, ds, trigger)

	clock.Set(time.Date(2026, 11, 7, 0, 0, 1, 0, time.UTC))
	if held := heldText(t, ds, trigger); held != "" {
		t.Fatalf("status held after the reset = %q", held)
	}
	process()
	requireReading(t, ds, "rec-"+fid("b"), true)
	requireNoFailures(t, ds, trigger)
}

// A schedule trigger of a function on the operator's key waits at the
// allowance the same way: the fire state stays on the occurrence, nothing
// parks, and the first pass after the reset fires that occurrence, not the
// next one. A call refused at its reservation after the walk let it through
// leaves the fire state too.
func TestAnOperatorPaidScheduleTriggerWaitsForTheReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := openMeterDataset(t, 1)
	startsAt := nowUTC().Add(-30 * time.Minute).Truncate(time.Minute)
	trigger := meterTrigger(t, ds, map[string]any{"schedule": map[string]any{
		"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
	}})
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	before := startsAt.Add(-time.Minute)
	if _, err := ds.db.ExecContext(ctx, `UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, trigger, before); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	firedAt := func() time.Time {
		t.Helper()
		var at time.Time
		if err := ds.db.QueryRowContext(ctx, `SELECT fired_at FROM trigger_schedule WHERE trigger_id = $1`, trigger).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at.UTC()
	}
	occurrence := "fire-" + fireID(startsAt)

	spendFunctionCents(t, ds, 0.6)
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	requireReading(t, ds, occurrence, false)
	requireNoFailures(t, ds, trigger)
	if got := firedAt(); !got.Equal(before) {
		t.Fatalf("the held occurrence moved the fire state to %s", got)
	}
	if held := heldText(t, ds, trigger); !strings.HasPrefix(held, allowancePrefix) {
		t.Fatalf("status held = %q, want the allowance's words", held)
	}

	// The walk holds before the claim; and the race, as in the record case.
	tr, _, err := ds.triggerByID(ctx, trigger)
	if err != nil {
		t.Fatal(err)
	}
	if _, held, err := ds.holdDelivery(ctx, tr); err != nil || !held {
		t.Fatalf("the walk's hold = %v, %v; want the occurrence held before its claim", held, err)
	}
	last := before
	_, settled, err := ds.deliverFire(ctx, tr, runner.ModeSchedule, fireID(startsAt), startsAt, &last, nil, nil)
	if !errors.Is(err, errUnstartedAtAllowance) || settled {
		t.Fatalf("a fire refused at its reservation = settled %v, %v; want it unsettled with the allowance's refusal", settled, err)
	}
	requireReading(t, ds, occurrence, false)
	requireNoFailures(t, ds, trigger)
	if got := firedAt(); !got.Equal(before) {
		t.Fatalf("the refused fire moved the fire state to %s", got)
	}

	clock.Set(time.Date(2026, 11, 7, 0, 0, 1, 0, time.UTC))
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	requireReading(t, ds, occurrence, true)
	requireNoFailures(t, ds, trigger)
	if got := firedAt(); !got.Equal(startsAt) {
		t.Fatalf("after the reset the fire state is %s, want the held occurrence %s", got, startsAt)
	}
}

// A delivery to a `sync` record that the allowance refuses at its
// reservation, after the walk let it through, leaves the record as it was:
// the `running` its start wrote is taken back, because every pass until the
// reset stops at the hold and never reaches the record. A pass while the
// allowance stays spent leaves it so, and the first pass after the reset runs
// the sync.
func TestAnOperatorPaidSyncHeldAtItsReservationLeavesItsRecordAsItWas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := openMeterDataset(t, 1)
	trigger := meterTrigger(t, ds, map[string]any{"record": map[string]any{"kinds": []any{meterAccount}}})
	process := func() {
		t.Helper()
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	syncOf := func() (any, any) {
		t.Helper()
		rec, err := ds.Get(ctx, meterAccount, fid("acct"))
		if err != nil {
			t.Fatal(err)
		}
		return rec.Properties["syncState"], rec.Properties["lastSyncStartedAt"]
	}
	asItWas := func(when string, firstStart any) {
		t.Helper()
		if state, started := syncOf(); state != substrate.SyncStateOK || started != firstStart {
			t.Fatalf("%s: syncState %v started %v, want ok from the first run's start %v", when, state, started, firstStart)
		}
	}

	// The first run fits, and spends 0.6 of the 1 cent.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: meterAccount, ID: fid("acct"), Properties: map[string]any{"name": "first"}}); err != nil {
		t.Fatal(err)
	}
	process()
	state, firstStart := syncOf()
	if state != substrate.SyncStateOK || firstStart == nil {
		t.Fatalf("the first run left syncState %v started %v, want ok with its start", state, firstStart)
	}

	// The owner's edit is owed a run that does not fit: the walk holds it.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, meterAccount, fid("acct"), substrate.PatchInput{Properties: map[string]any{"name": "second"}}); err != nil {
		t.Fatal(err)
	}
	process()
	asItWas("held by the walk", firstStart)

	// The race: the walk let the delivery through, its start wrote
	// `running`, then the reservation was refused.
	tr, _, err := ds.triggerByID(ctx, trigger)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := ds.ensureCursor(ctx, trigger)
	if err != nil {
		t.Fatal(err)
	}
	changes, _, err := ds.changesPast(ctx, tr, cursor)
	if err != nil {
		t.Fatal(err)
	}
	matched := matchChanges(tr, tr.selfActors(ds.registry()), changes)
	if len(matched) != 1 || matched[0].RecordID != fid("acct") {
		t.Fatalf("the change owed is %+v, want the account's edit", matched)
	}
	_, next, err := ds.deliverWithRetry(ctx, tr, matched[0], cursor)
	if !errors.Is(err, errUnstartedAtAllowance) || next != cursor {
		t.Fatalf("a delivery refused at its reservation = next %d (cursor %d), %v; want the cursor kept and the allowance's refusal", next, cursor, err)
	}
	asItWas("refused at the reservation", firstStart)
	requireNoFailures(t, ds, trigger)

	process()
	asItWas("a pass while the allowance is spent", firstStart)
	requireNoFailures(t, ds, trigger)

	clock.Set(time.Date(2026, 11, 7, 0, 0, 1, 0, time.UTC))
	process()
	if state, started := syncOf(); state != substrate.SyncStateOK || started == nil || started == firstStart {
		t.Fatalf("after the reset syncState %v started %v, want ok from a new run (the first started %v)", state, started, firstStart)
	}
	requireNoFailures(t, ds, trigger)
}

// The take-back of a start the allowance refused at its reservation
// (syncUnstart) puts back only that start: a write to the `sync` record that
// lands between the refused start and the take-back keeps its word. One is a
// later delivery's start, which moves lastSyncStartedAt; the other is the
// body's own `throttled`, which leaves it as the refused start wrote it. The
// take-back with nothing between is the test above.
func TestAnOperatorPaidSyncHeldAtItsReservationKeepsALaterWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openMeterDataset(t, 1)
	trigger := meterTrigger(t, ds, map[string]any{"record": map[string]any{"kinds": []any{meterAccount}}})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: meterAccount, ID: fid("acct"), Properties: map[string]any{"name": "first"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	tr, _, err := ds.triggerByID(ctx, trigger)
	if err != nil {
		t.Fatal(err)
	}
	ch := substrate.Change{Kind: meterAccount, RecordID: fid("acct")}
	envelope := map[string]any{"record": map[string]any{"id": fid("acct")}}
	// start writes one delivery's `running`, as deliver does before the
	// reservation.
	start := func(at time.Time) *syncStamp {
		t.Helper()
		s := ds.syncStampFor(tr, ch, envelope, at)
		if s == nil {
			t.Fatal("a delivery of the account's own sync carries no stamp")
		}
		if err := ds.syncStart(ctx, s, 0); err != nil {
			t.Fatalf("start: %v", err)
		}
		s.stamped = true
		return s
	}
	syncOf := func() (any, *time.Time) {
		t.Helper()
		rec, err := ds.Get(ctx, meterAccount, fid("acct"))
		if err != nil {
			t.Fatal(err)
		}
		return rec.Properties[propSyncState], syncTime(rec.Properties, propLastSyncStartedAt)
	}
	unstart := func(s *syncStamp) {
		t.Helper()
		if err := ds.syncUnstart(ctx, s, 0); err != nil {
			t.Fatalf("take back the start: %v", err)
		}
	}
	if state, _ := syncOf(); state != substrate.SyncStateOK {
		t.Fatalf("the first run left syncState %v, want ok", state)
	}

	at := nowUTC()
	refused := start(at)
	later := start(at.Add(time.Second))
	unstart(refused)
	if state, started := syncOf(); state != substrate.SyncStateRunning || started == nil || !started.Equal(later.started) {
		t.Fatalf("after a later start, the take-back left syncState %v started %v; want running from %v", state, started, later.started)
	}

	refused = start(at.Add(2 * time.Second))
	if err := ds.inTx(ctx, refused.actor, false, func(tx *txn) error {
		return tx.asSyncWriter(refused.actor, func() error {
			_, err := tx.patch(refused.ref, substrate.PatchInput{Properties: map[string]any{propSyncState: substrate.SyncStateThrottled}})
			return err
		})
	}); err != nil {
		t.Fatalf("the body's throttled: %v", err)
	}
	unstart(refused)
	if state, started := syncOf(); state != substrate.SyncStateThrottled || started == nil || !started.Equal(refused.started) {
		t.Fatalf("after the body's throttled, the take-back left syncState %v started %v; want throttled from %v", state, started, refused.started)
	}
}

// A webhook trigger of a function on the operator's key waits at the
// allowance too: the admitted request stays pending, neither run nor parked,
// through the hold, through a fire refused at its reservation after the hold
// let it through, and through another walk of the pending requests; the first
// walk after the reset delivers it under its own fire id.
func TestAnOperatorPaidWebhookTriggerWaitsForTheReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := openMeterDataset(t, 1)
	trigger := meterTrigger(t, ds, map[string]any{"webhook": map[string]any{}})
	spendFunctionCents(t, ds, 0.6)
	pending := func() []pendingWebhook {
		t.Helper()
		rows, err := ds.pendingWebhooks(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	stillPending := func(when, fid string) foldFailure {
		t.Helper()
		rows := pending()
		if len(rows) != 1 || rows[0].trigger != trigger || rows[0].row.FireID != fid || rows[0].row.Attempts != 0 {
			t.Fatalf("%s: pending requests = %+v, want fire %s pending at no attempts", when, rows, fid)
		}
		return rows[0].row
	}

	fid, err := ds.svc.receiveWebhook(ctx, testdb.Repository(t), trigger, "", substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"content-type": "application/json"},
		Body:    []byte(`{"n":1}`),
	}, webhookFireInline)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	reading := "fire-" + fid
	stillPending("held at its fire", fid)
	requireReading(t, ds, reading, false)
	if held := heldText(t, ds, trigger); !strings.HasPrefix(held, allowancePrefix) {
		t.Fatalf("status held = %q, want the allowance's words", held)
	}

	// The race: the hold let the fire through, then the reservation was
	// refused. Nothing runs and the request stays pending.
	tr, _, err := ds.triggerByID(ctx, trigger)
	if err != nil {
		t.Fatal(err)
	}
	row := stillPending("before the race", fid)
	var envelope map[string]any
	if err := json.Unmarshal(row.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if _, settled, err := ds.deliverFire(ctx, tr, runner.ModeWebhook, row.FireID, row.ParkedAt, nil, envelope, &row); !errors.Is(err, errUnstartedAtAllowance) || settled {
		t.Fatalf("a fire refused at its reservation = settled %v, %v; want it unsettled with the allowance's refusal", settled, err)
	}
	stillPending("refused at the reservation", fid)
	requireReading(t, ds, reading, false)

	ds.runPendingWebhooks(ctx)
	stillPending("a walk while the allowance is spent", fid)
	requireReading(t, ds, reading, false)

	clock.Set(time.Date(2026, 11, 7, 0, 0, 1, 0, time.UTC))
	ds.runPendingWebhooks(ctx)
	requireReading(t, ds, reading, true)
	if rows := pending(); len(rows) != 0 {
		t.Fatalf("after the reset the request is still pending: %+v", rows)
	}
	requireNoFailures(t, ds, trigger)
}

// A hand's wake or retry of a held function trigger is told so, with the
// allowance's words, before anything runs.
func TestAHandsWakeOfAnOperatorPaidTriggerIsRefusedAtTheAllowance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openMeterDataset(t, 1)
	trigger := meterTrigger(t, ds, map[string]any{"record": map[string]any{"kinds": []any{meterPing}}})
	spendFunctionCents(t, ds, 0.6)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: meterPing, ID: fid("w"), Properties: map[string]any{"name": "w"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.WakeTrigger(ctx, trigger); !errors.Is(err, substrate.ErrAllowance) || !strings.Contains(err.Error(), "today's allowance from Example") {
		t.Fatalf("a wake at the allowance = %v, want the allowance's refusal", err)
	}
	requireReading(t, ds, "rec-"+fid("w"), false)
}

// A call whose body never started settles at nothing, at the root and as a
// host call: here the runner fails to register the body again after its
// process was retired. A body that started settles at the price, whatever it
// answered.
func TestAnOperatorPaidCallTheRunnerNeverStartedSettlesAtNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openMeterDataset(t, 100)
	const relay = "relay.example.com/relay"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(relay, 0),
		vocabulary.BundleManifest(relay, map[string]any{"description": "calls fussy", "installs": []any{relay + "/relay"}}),
		vocabulary.FunctionManifest(relay, "relay", map[string]any{
			"description": "calls fussy and reports how it went", "runtime": vocabulary.RuntimePython,
			"permissions": map[string]any{"call": []any{meterPackage + "/fussy"}},
			"source": `
def main(input, host):
    try:
        host.call("` + meterPackage + `/fussy", {})
    except Exception as e:
        return {"output": {"nested": str(e)}}
    return {"output": {"nested": "ran"}}
`,
		}),
	}); err != nil {
		t.Fatalf("install the relay: %v", err)
	}
	settled := func() []float64 {
		t.Helper()
		var out []float64
		for _, r := range operatorLedger(t, ds) {
			out = append(out, r.settled)
		}
		return out
	}

	// The body registered at install and runs: the price.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, meterPackage+"/fussy", nil); err != nil {
		t.Fatalf("the first call: %v", err)
	}
	// Every process of the repository is retired; fussy cannot register again.
	runner.Shared.Reconcile(ctx, ds.Repository().ID, nil)
	_, _, err := ds.CallFunction(ctx, substrate.ActorAPI, meterPackage+"/fussy", nil)
	if err == nil || !strings.Contains(err.Error(), "fussy registers once") {
		t.Fatalf("a call after the retirement = %v, want the failed registration", err)
	}
	out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, relay+"/relay", nil)
	if err != nil {
		t.Fatalf("the relay: %v", err)
	}
	if nested, _ := out.(map[string]any)["nested"].(string); !strings.Contains(nested, "fussy registers once") {
		t.Fatalf("the relay's host call answered %q, want the failed registration", nested)
	}
	if got, want := settled(), []float64{0.5, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("settled = %v, want %v: the price for the call that ran, nothing for the two that never started", got, want)
	}
}
