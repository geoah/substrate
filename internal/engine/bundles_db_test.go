package engine_test

// The bundle model's acceptance gate (substrate-primitives §4, ticket 034):
// atomic closure install (a batch carrying a bundle document replaces the
// owned authority whole), the install-closure refusals, input resolution
// (bound edge, the id "default", the sole record — never a tie-break),
// disable-stops-delivery (reversibly, cursor standing), the
// refuse-breakage upgrade, uninstall-tears-the-authority-down (its types leave the
// registry, its triggers go, a data-bearing uninstall refuses with the count),
// and purge through the ordinary soft-delete flow.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	mbPackage     = "mail.bundles.substrate.reamde.dev/mail"
	mbConfigType  = mbPackage + "/mailconfig"
	mbAccountType = mbPackage + "/mailaccount"
	mbItemType    = mbPackage + "/mailitem"
	mbMessageType = mbPackage + "/mailmessage"
	mbMarkFn      = mbPackage + "/mark"
	mbEchoFn      = mbPackage + "/echo"
)

// mbConfigTypeDoc declares the bundle's client kind: oauth2, so it carries
// the standard client fields. Records of it are ordinary — any number may
// exist; the bundle's `client` input resolves one.
func mbConfigTypeDoc() map[string]any {
	return vocabulary.KindManifest(mbPackage,
		map[string]any{"singular": "mailconfig"},
		map[string]any{
			"traits": []any{"oauth2"},
			"properties": map[string]any{
				"authorizationEndpoint": map[string]any{"type": "url"},
				"tokenEndpoint":         map[string]any{"type": "url"},
				"revocationEndpoint":    map[string]any{"type": "url"},
				"clientId":              map[string]any{"type": "string"},
				"clientSecret":          map[string]any{"type": "secret"},
				"scopes":                map[string]any{"type": "string", "repeated": true},
			},
		})
}

func mbAccountTypeDoc() map[string]any {
	return vocabulary.KindManifest(mbPackage,
		map[string]any{"singular": "mailaccount"},
		map[string]any{
			"traits": []any{"accountconfig"},
			"properties": map[string]any{
				"tokenRef":      map[string]any{"type": "secret", "writer": "oauth"},
				"tokenStatus":   map[string]any{"type": "string", "writer": "oauth"},
				"grantedScopes": map[string]any{"type": "string", "repeated": true, "writer": "oauth"},
				"address":       map[string]any{"type": "email", "writer": "owner"},
				// A feature toggle the manifest maps to scopes.
				"enabledMail": map[string]any{"type": "bool", "writer": "owner"},
				// Connector-owned sync state.
				"syncToken": map[string]any{"type": "string", "writer": "connector"},
			},
		})
}

func mbItemTypeDoc() map[string]any {
	return vocabulary.KindManifest(mbPackage,
		map[string]any{"singular": "mailitem"},
		map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}})
}

func mbMessageTypeDoc() map[string]any {
	return vocabulary.KindManifest(mbPackage,
		map[string]any{"singular": "mailmessage"},
		map[string]any{"properties": map[string]any{"subject": map[string]any{"type": "string"}}})
}

func mbFnDoc(name, source string) map[string]any {
	return vocabulary.FunctionManifest(mbPackage, name, map[string]any{
		"description": "test function " + name,
		"runtime":     vocabulary.RuntimePython,
		"source":      source,
		"permissions": map[string]any{"writes": []any{mbMessageType}},
	})
}

const mbMarkSource = `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "mail.bundles.substrate.reamde.dev/mail/mailmessage",
                         "id": "m-" + c["id"], "properties": {"subject": "marked"}}]}
`

const mbEchoSource = `
def main(input, host):
    return {"effects": [], "output": {"config": input.get("config")}}
`

// mbDocs assembles the bundle's closure: header + actor + bundle + members.
// installs derives from the members unless overridden.
func mbDocs(installs []string, members ...map[string]any) []map[string]any {
	if installs == nil {
		for _, m := range members {
			meta, _ := m["metadata"].(map[string]any)
			installs = append(installs, meta["id"].(string))
		}
	}
	list := make([]any, 0, len(installs))
	for _, id := range installs {
		list = append(list, id)
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(mbPackage, 0),
		vocabulary.ActorManifest(mbPackage, vocabulary.PackageActor(mbPackage)),
		vocabulary.BundleManifest(mbPackage, map[string]any{
			"description": "the mail bundle",
			"inputs": map[string]any{
				// Injected on purpose: the oauth tests assert clientId
				// crosses into a function while clientSecret never does.
				"client": map[string]any{"kind": mbConfigType, "inject": "functions"},
			},
			"installs": list,
			// Trusted provider metadata. Static https here;
			// the OAuth round-trip fixtures rewrite the endpoints to their
			// loopback fake provider (mbPointOAuthAt).
			"oauth2": map[string]any{
				"clientInput":           "client",
				"authorizationEndpoint": "https://provider.example/authorize",
				"tokenEndpoint":         "https://provider.example/token",
				"revocationEndpoint":    "https://provider.example/revoke",
				"featureScopes": map[string]any{
					"enabledMail": map[string]any{"scopes": []any{"mail.read", "mail.send"}},
				},
			},
		}),
	}
	return append(docs, members...)
}

// mbStandardDocs is the full standard closure.
func mbStandardDocs() []map[string]any {
	return mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource))
}

// mbPointOAuthAt rewrites the bundle document's trusted oauth2 endpoints to a
// live (loopback) fake provider — the round-trip fixtures cannot bake a
// dynamic httptest URL into a static manifest, so they inject it here, exactly
// where the trusted metadata is authored.
func mbPointOAuthAt(docs []map[string]any, baseURL string) {
	for _, d := range docs {
		if d["kind"] != vocabulary.CoreKind(vocabulary.DocBundle) {
			continue
		}
		data, _ := d["data"].(map[string]any)
		o, _ := data["oauth2"].(map[string]any)
		o["authorizationEndpoint"] = baseURL + "/authorize"
		o["tokenEndpoint"] = baseURL + "/token"
		o["revocationEndpoint"] = baseURL + "/revoke"
		// Only when the bundle wired email derivation (mbWireEmail): the
		// endpoint pairs with emailProperty, so the untouched bundle stays free
		// of a dangling emailEndpoint.
		if o["emailProperty"] != nil {
			o["emailEndpoint"] = baseURL + "/userinfo"
		}
	}
}

// mbWireEmail turns on OAuth email derivation for the mail closure: it adds an
// `email` (writer: oauth) property to the account type and points the bundle's
// oauth2 block at that property. mbPointOAuthAt then rewrites the endpoint to
// the fake provider's /userinfo. The user never types email; the facility sets
// it from the grant.
func mbWireEmail(docs []map[string]any) {
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		if d["kind"] == vocabulary.CoreKind(vocabulary.DocKind) {
			if meta, _ := d["metadata"].(map[string]any); meta["id"] == mbAccountType {
				props, _ := data["properties"].(map[string]any)
				props["email"] = map[string]any{"type": "email", "writer": "oauth"}
			}
		}
		if d["kind"] == vocabulary.CoreKind(vocabulary.DocBundle) {
			o, _ := data["oauth2"].(map[string]any)
			o["emailProperty"] = "email"
		}
	}
}

// installMailBundle applies the standard closure into a fresh repository.
func installMailBundle(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	return ds
}

// mbTrigger binds mailitem creations to the mark function.
func mbTrigger(t *testing.T, ds substrate.Dataset) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trigger", ID: "on-mark-mail",
		Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{mbItemType}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", mbMarkFn),
		},
	})
}

// One atomic apply installs the whole closure: the bundle row lands beside
// its members, active immediately, and the status verbs see it.
func TestBundleInstallAtomic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)

	row := mustGet(t, ds, "substrate.reamde.dev/core/bundle", mbPackage)
	if row.Kind != "substrate.reamde.dev/core/bundle" {
		t.Fatalf("bundle row type: %s", row.Kind)
	}
	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("fresh bundle status: %+v", st)
	}
	// The client input has nothing to resolve yet, and the status says so —
	// per input, with a machine-readable code.
	if len(st.Inputs) != 1 || st.Inputs[0].Name != "client" || st.Inputs[0].Record != "" {
		t.Fatalf("fresh input status: %+v", st.Inputs)
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupMissing || st.Setup[0].Input != "client" {
		t.Fatalf("fresh setup: %+v", st.Setup)
	}
	if st.Kinds != 4 || st.Functions != 2 {
		t.Fatalf("closure counts: %+v", st)
	}

	// Traits are queryable interfaces: the client kind answers oauth2.
	types, err := ds.TypesImplementing(ctx, "substrate.reamde.dev/core/oauth2")
	if err != nil {
		t.Fatalf("implementors: %v", err)
	}
	found := false
	for _, ti := range types {
		if ti.Identity == mbConfigType {
			found = true
		}
	}
	if !found {
		t.Fatalf("oauth2 implementors missing %s: %+v", mbConfigType, types)
	}
}

// The loader refuses an install whose declared members and `installs:`
// disagree, or whose oauth2 clientInput names a non-oauth2 kind.
func TestBundleInstallClosureRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// installs missing a declared member.
	short := mbDocs([]string{mbConfigType, mbAccountType, mbItemType, mbMessageType, mbMarkFn},
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource))
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, short); err == nil ||
		!strings.Contains(err.Error(), "closure is the package") {
		t.Fatalf("undeclared member must refuse: %v", err)
	}

	// installs naming a member the package does not declare.
	extra := mbDocs([]string{mbConfigType, mbAccountType, mbItemType, mbMessageType, mbMarkFn, mbEchoFn, mbPackage + "/ghost"},
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource))
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, extra); err == nil ||
		!strings.Contains(err.Error(), "closure is the package") {
		t.Fatalf("phantom install must refuse: %v", err)
	}

	// A package with members and NO bundle document installs: the `.bundles.`
	// authority suffix was a typo-catcher over the retired vocabulary/extension
	// split, and record 0047 dropped it. A bundle names its package, not the
	// other way round.
	headless := []map[string]any{
		vocabulary.PackageManifest(mbPackage, 0),
		vocabulary.ActorManifest(mbPackage, vocabulary.PackageActor(mbPackage)),
		mbMessageTypeDoc(),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, headless); err != nil {
		t.Fatalf("a package without a bundle document must install: %v", err)
	}

	// The oauth2 clientInput must name an input whose kind implements oauth2.
	badConfig := mbDocs([]string{mbAccountType, mbItemType, mbMessageType, mbMarkFn, mbEchoFn},
		mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource))
	for _, d := range badConfig {
		if d["kind"] == vocabulary.CoreKind(vocabulary.DocBundle) {
			d["data"].(map[string]any)["inputs"] = map[string]any{
				"client": map[string]any{"kind": mbMessageType},
			}
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, badConfig); err == nil ||
		!strings.Contains(err.Error(), "does not implement the oauth2 trait") {
		t.Fatalf("non-oauth2 clientInput must refuse: %v", err)
	}
}

// mbConfigProps is a minimal valid configuration record.
func mbConfigProps() map[string]any {
	return map[string]any{
		"authorizationEndpoint": "https://provider.example/authorize",
		"tokenEndpoint":         "https://provider.example/token",
		"clientId":              "client-1",
		"clientSecret":          "s3cret",
		"scopes":                []any{"mail.read"},
	}
}

// Input resolution, no singleton anywhere: one record resolves as the sole
// live row, a second makes the input AMBIGUOUS (surfaced, never tie-broken),
// bind chooses explicitly, unbind returns to the default rules, and the
// record named "default" wins where nothing is bound.
func TestBundleInputResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)

	first := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 0 {
		t.Fatalf("sole record must resolve: %+v %v", st, err)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Record != first.ID || st.Inputs[0].Via != substrate.InputViaSole {
		t.Fatalf("sole resolution: %+v", st.Inputs)
	}

	// A second record is an ordinary create — no cardinality is enforced —
	// and the input turns ambiguous until one is chosen.
	second := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	st, err = ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupAmbiguous {
		t.Fatalf("two records must read ambiguous: %+v %v", st, err)
	}

	// Bind is the explicit choice, and beats everything.
	if err := ds.(interface {
		BindBundleInput(ctx context.Context, id, input, record string) error
	}).BindBundleInput(ctx, mbPackage, "client", second.ID); err != nil {
		t.Fatalf("bind: %v", err)
	}
	st, err = ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 0 || st.Inputs[0].Record != second.ID || st.Inputs[0].Via != substrate.InputViaBound {
		t.Fatalf("bound resolution: %+v %v", st, err)
	}

	// Deleting the bound record leaves a DANGLING binding — a problem to
	// show, never silently papered over by the default rules.
	if _, err := ds.Delete(ctx, owner, second.Kind, second.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete bound: %v", err)
	}
	st, err = ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupDangling {
		t.Fatalf("dangling binding must surface: %+v %v", st, err)
	}

	// Unbind returns resolution to the default rules: the sole survivor.
	if err := ds.(interface {
		BindBundleInput(ctx context.Context, id, input, record string) error
	}).BindBundleInput(ctx, mbPackage, "client", ""); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	st, err = ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 0 || st.Inputs[0].Record != first.ID || st.Inputs[0].Via != substrate.InputViaSole {
		t.Fatalf("post-unbind resolution: %+v %v", st, err)
	}

	// The well-known id "default" beats the sole rule the moment it exists.
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, ID: "default", Properties: mbConfigProps()})
	st, err = ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 0 || st.Inputs[0].Record != "default" || st.Inputs[0].Via != substrate.InputViaDefault {
		t.Fatalf("default-id resolution: %+v %v", st, err)
	}

	// The secret never reads back.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{mbConfigType}}})
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("config list: %v %v", page, err)
	}
	if got := page.Records[0].Properties["clientSecret"]; got != "<redacted>" {
		t.Fatalf("clientSecret leaked: %v", got)
	}
}

// A BINDING FOLLOWS A MERGE. Nothing repoints a stored reference when a record
// loses a merge, so a binding written at the loser keeps naming it; the input
// resolves through the former-id trail to the winner rather than reporting a
// dangling choice at a record the owner deliberately merged.
func TestBundleInputBoundToAMergedRecordResolvesToTheWinner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	if err := ds.(interface {
		BindBundleInput(ctx context.Context, id, input, record string) error
	}).BindBundleInput(ctx, mbPackage, "client", loser.ID); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 0 {
		t.Fatalf("the merged binding must still resolve: %+v %v", st, err)
	}
	if st.Inputs[0].Record != winner.ID || st.Inputs[0].Via != substrate.InputViaBound {
		t.Fatalf("input resolved to %+v, want the merge winner %s", st.Inputs[0], winner.ID)
	}

	// Delete the winner: the input dangles again, and the message names BOTH
	// ends of the trail — the id the binding carries, and the record it merged
	// into — so rebinding is not a search.
	if _, err := ds.Delete(ctx, owner, winner.Kind, winner.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the winner: %v", err)
	}
	st, err = ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupDangling {
		t.Fatalf("a deleted winner must dangle: %+v %v", st, err)
	}
	for _, want := range []string{loser.ID, winner.ID, "merged into", "deleted"} {
		if !strings.Contains(st.Setup[0].Message, want) {
			t.Fatalf("the dangling message must name %q: %q", want, st.Setup[0].Message)
		}
	}
}

// A binding is a changelog write like any other: clear the fold, replay, and
// the bound resolution must come back — the bind verb's edge and version bump
// ride its entry's fold ds.
func TestBundleInputBindSurvivesRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	chosen := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	if err := ds.(interface {
		BindBundleInput(ctx context.Context, id, input, record string) error
	}).BindBundleInput(ctx, mbPackage, "client", chosen.ID); err != nil {
		t.Fatalf("bind: %v", err)
	}
	rb, ok := svc.(interface {
		RebuildRepository(ctx context.Context, repository string) (engine.RebuildReport, error)
	})
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 0 {
		t.Fatalf("post-rebuild status: %+v %v", st, err)
	}
	if st.Inputs[0].Record != chosen.ID || st.Inputs[0].Via != substrate.InputViaBound {
		t.Fatalf("the binding did not survive the rebuild: %+v", st.Inputs)
	}
}

// Bind refuses while the bundle is disabled: bind IS configuration, and a
// disabled bundle's configuration is frozen.
func TestBundleInputBindFrozenWhileDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	row := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	err := ds.(interface {
		BindBundleInput(ctx context.Context, id, input, record string) error
	}).BindBundleInput(ctx, mbPackage, "client", row.ID)
	wantErr(t, err, substrate.ErrGuard, "bind while disabled")
}

// Disable stops delivery with the cursor standing still; enable resumes the
// backlog. Functions refuse invocation; accounts and config freeze.
func TestBundleDisableStopsDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	mbTrigger(t, ds)

	one := mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "one"}})
	process(t, ds)
	if got := mustGet(t, ds, mbMessageType, "m-"+one.ID); got.Properties["subject"] != "marked" {
		t.Fatalf("delivery before disable: %+v", got.Properties)
	}

	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	two := mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "two"}})
	process(t, ds)
	if _, err := ds.Get(ctx, mbMessageType, "m-"+two.ID); err == nil {
		t.Fatal("a disabled bundle's trigger delivered")
	}

	// Invocation refuses...
	if _, _, err := ds.CallFunction(ctx, mbEchoFn, map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled function call: %v", err)
	}
	// ...and accounts/config freeze, while plain data stays writable.
	_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "a@b.co"}})
	wantErr(t, err, substrate.ErrGuard, "frozen account create")
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "three"}})

	// Enable: the backlog delivers — the cursor never moved past it.
	if err := ds.EnableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("enable: %v", err)
	}
	process(t, ds)
	if got := mustGet(t, ds, mbMessageType, "m-"+two.ID); got.Properties["subject"] != "marked" {
		t.Fatalf("backlog after enable: %+v", got.Properties)
	}
}

// Upgrade is an atomic re-apply that refuses breakage: dropping a type with
// live rows, or a function live triggers reference, fails admission whole;
// dropping an unreferenced function prunes cleanly.
func TestBundleUpgradeRefusesBreakage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	mbTrigger(t, ds)
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "keep"}})

	// Dropping mailitem while a row lives.
	noItem := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource))
	_, err := ds.ApplyVocabularyDocuments(ctx, owner, noItem)
	wantErr(t, err, substrate.ErrGuard, "dropping a type with live rows")
	if !strings.Contains(err.Error(), "live records") {
		t.Fatalf("live-rows refusal: %v", err)
	}

	// Dropping mark while the trigger references it.
	noMark := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("echo", mbEchoSource))
	_, err = ds.ApplyVocabularyDocuments(ctx, owner, noMark)
	wantErr(t, err, substrate.ErrGuard, "dropping a referenced function")
	if !strings.Contains(err.Error(), "referenced by live trigger") {
		t.Fatalf("referenced-function refusal: %v", err)
	}

	// Dropping the unreferenced echo prunes cleanly, one atomic re-apply.
	noEcho := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource))
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, noEcho); err != nil {
		t.Fatalf("dropping an unreferenced function: %v", err)
	}
	if _, _, err := ds.CallFunction(ctx, mbEchoFn, nil); err == nil {
		t.Fatal("echo survived its removal")
	}
}

// hasType reports whether the registry's type listing carries an identity.
func hasType(t *testing.T, ds substrate.Dataset, identity string) bool {
	t.Helper()
	types, err := ds.Kinds(context.Background())
	if err != nil {
		t.Fatalf("types: %v", err)
	}
	for _, ti := range types {
		if ti.Identity == identity {
			return true
		}
	}
	return false
}

// Uninstall tears the owned authority down when no data instances live: its types
// leave the registry, its wiring goes, its callables stop, and a read of a
// type — schema row or type resolution — 404s. (Ticket 034.)
func TestBundleUninstallTearsDownAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	mbTrigger(t, ds)

	// Baseline: the trigger fires, the type resolves, the bundle is listed.
	one := mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "one"}})
	process(t, ds)
	if got := mustGet(t, ds, mbMessageType, "m-"+one.ID); got.Properties["subject"] != "marked" {
		t.Fatalf("baseline delivery: %+v", got.Properties)
	}
	if !hasType(t, ds, mbItemType) {
		t.Fatal("mailitem missing from types before uninstall")
	}
	// Clear the data so the uninstall has nothing live to guard.
	if _, err := ds.Delete(ctx, owner, mbItemType, one.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete item: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, mbMessageType, "m-"+one.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete message: %v", err)
	}

	if err := ds.UninstallBundle(ctx, mbPackage); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	// The types are gone from the registry, by listing and by identity.
	if hasType(t, ds, mbItemType) || hasType(t, ds, mbMessageType) || hasType(t, ds, mbConfigType) {
		t.Fatal("a torn-down type still appears in types")
	}
	if _, err := ds.KindByRef(ctx, mbItemType); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("mailitem still resolves: %v", err)
	}
	// A fresh get of one of its types 404s — the type no longer resolves — and
	// a write can no longer address it.
	if _, err := ds.Get(ctx, mbItemType, one.ID); err == nil {
		t.Fatal("get of a torn-down type resolved")
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "no"}}); err == nil {
		t.Fatal("a write resolved a torn-down type")
	}
	// The kind's own schema row was pruned (tombstoned).
	if row, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", mbItemType); err != nil || row.DeletedAt == nil {
		t.Fatalf("mailitem schema row not pruned: %+v %v", row, err)
	}
	// The callable is gone, and the trigger row went with it — it cannot fire.
	if _, _, err := ds.CallFunction(ctx, mbEchoFn, map[string]any{}); err == nil {
		t.Fatal("a torn-down bundle's function ran")
	}
	if row, err := ds.Get(ctx, "substrate.reamde.dev/core/trigger", "on-mark-mail"); err != nil || row.DeletedAt == nil {
		t.Fatalf("the trigger was not torn down: %+v %v", row, err)
	}
	// The bundle stops being listed, and its row is pruned.
	statuses, err := ds.BundleStatuses(ctx)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	for _, s := range statuses {
		if s.ID == mbPackage {
			t.Fatalf("uninstalled bundle still listed: %+v", s)
		}
	}
	if row, err := ds.Get(ctx, "substrate.reamde.dev/core/bundle", mbPackage); err != nil || row.DeletedAt == nil {
		t.Fatalf("the bundle row was not pruned: %+v %v", row, err)
	}

	// Re-applying the closure is a fresh install: the authority comes back.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("re-install: %v", err)
	}
	if !hasType(t, ds, mbItemType) {
		t.Fatal("re-install did not restore mailitem")
	}
}

// Uninstall refuses while live DATA instances of the authority's types exist — a
// guard carrying the count — until a purge clears them; then it succeeds.
// (Ticket 034: disable, purge, uninstall is the destructive order.)
func TestBundleUninstallGuardsLiveData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	mbTrigger(t, ds)
	item := mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "kept"}})

	// The live instance refuses the uninstall, with the count.
	err := ds.UninstallBundle(ctx, mbPackage)
	wantErr(t, err, substrate.ErrGuard, "uninstall with a live instance")
	if !strings.Contains(err.Error(), "live records") {
		t.Fatalf("refusal must carry the count: %v", err)
	}
	// The refusal rolled back whole: the data, the type and the wiring survive.
	if got := mustGet(t, ds, item.Kind, item.ID); got.Properties["name"] != "kept" {
		t.Fatalf("data lost to a refused uninstall: %+v", got.Properties)
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/trigger", "on-mark-mail"); err != nil {
		t.Fatalf("the trigger was torn down by a refused uninstall: %v", err)
	}

	// Purge (after disable) clears the data; then the uninstall proceeds.
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := ds.PurgeBundle(ctx, mbPackage); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if err := ds.UninstallBundle(ctx, mbPackage); err != nil {
		t.Fatalf("uninstall after purge: %v", err)
	}
	if _, err := ds.KindByRef(ctx, mbItemType); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("mailitem still resolves after uninstall: %v", err)
	}
	if row, err := ds.Get(ctx, "substrate.reamde.dev/core/trigger", "on-mark-mail"); err != nil || row.DeletedAt == nil {
		t.Fatalf("the trigger was not torn down: %+v %v", row, err)
	}
}

// Purge is the explicit second verb: refused while live, and a soft delete
// of the whole owned authority's data — GC collects what nothing holds.
func TestBundlePurgeDeletesData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "one"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "two"}})

	if _, err := ds.PurgeBundle(ctx, mbPackage); err == nil {
		t.Fatal("purging a live bundle must refuse")
	}
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	purged, err := ds.PurgeBundle(ctx, mbPackage)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged != 3 {
		t.Fatalf("purged %d, want 3", purged)
	}
	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil || st.LiveRecords != 0 || len(st.Setup) == 0 {
		t.Fatalf("post-purge status: %+v %v", st, err)
	}
	// Nothing holds the tombstones: GC hard-deletes them.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	// Idempotent: nothing left to purge.
	if again, err := ds.PurgeBundle(ctx, mbPackage); err != nil || again != 0 {
		t.Fatalf("re-purge: %d %v", again, err)
	}
}

// bundleLocalTraitDoc renders a trait document declared inside the mail bundle package.
func bundleLocalTraitDoc(name string, props map[string]any) map[string]any {
	authority, pkg := vocabulary.SplitPackageRef(mbPackage)
	data := map[string]any{
		"authority": authority, "package": pkg,
		"description": "a bundle-local trait named like a core one",
	}
	if len(props) > 0 {
		data["properties"] = props
	}
	return map[string]any{
		"kind":     vocabulary.CoreKind(vocabulary.DocTrait),
		"metadata": map[string]any{"id": mbPackage + "/" + name},
		"data":     data,
	}
}

// A bundle whose oauth2 clientInput kind binds a LOCAL trait named "oauth2"
// declares no client at all: the host key is the resolved identity
// substrate.reamde.dev/core/oauth2, and a same-named local trait does not count.
func TestShadowOAuth2TraitFailsBundleAdmission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := mbDocs(nil,
		bundleLocalTraitDoc("oauth2", map[string]any{"clientId": "string", "clientSecret": "secret"}),
		vocabulary.KindManifest(mbPackage,
			map[string]any{"singular": "mailconfig"},
			map[string]any{
				// Resolves in-authority FIRST: this binds the local shadow, never core.
				"traits":     []any{"oauth2"},
				"properties": map[string]any{"note": map[string]any{"type": "string"}},
			}),
		mbMessageTypeDoc())
	_, err := ds.ApplyVocabularyDocuments(ctx, owner, docs)
	if err == nil || !strings.Contains(err.Error(), "does not implement the oauth2 trait") {
		t.Fatalf("a shadow oauth2 trait satisfied bundle admission: %v", err)
	}
}

// A bundle-local trait named "accountconfig" shadows the core one for the
// authority's own bindings — and the host then treats NONE of its records as
// connected accounts: no OAuth, no status counts, no runner injection, no
// core-trait query hits.
func TestShadowAccountConfigTraitIsNotAnAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _ := newDatasetWithSchemaDB(t,
		engine.WithOAuth("test-state-key", "https://substrate.example/cb", nil))
	docs := mbDocs(nil,
		mbConfigTypeDoc(),
		bundleLocalTraitDoc("accountconfig", nil),
		// Binds bare "accountconfig" — in-authority resolution finds the SHADOW.
		mbAccountTypeDoc(),
		mbMessageTypeDoc(),
		mbFnDoc("echo", mbEchoSource))
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	shadow := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "shadow@example.com"},
	})

	// OAuth refuses: the shadow trait is not the core accountconfig, so the
	// id resolves within NO accountconfig implementor type — a not-found,
	// never an account.
	if _, err := ds.StartOAuth(ctx, owner, shadow.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("StartOAuth on a shadow-trait record: %v", err)
	}
	// Status counts no accounts.
	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Accounts != 0 {
		t.Fatalf("a shadow-trait record counted as an account: %+v", st)
	}
	// The core trait query does not list the shadow-bound kind.
	types, err := ds.TypesImplementing(ctx, "substrate.reamde.dev/core/accountconfig")
	if err != nil {
		t.Fatalf("implementors: %v", err)
	}
	for _, ti := range types {
		if ti.Identity == mbAccountType {
			t.Fatalf("shadow-bound kind answers the core trait query: %+v", types)
		}
	}
	// The BARE name is now ambiguous — core and shadow both declare it — and
	// an ambiguous bare filter errors instead of aggregating look-alikes.
	if _, err := ds.TypesImplementing(ctx, "accountconfig"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous trait") {
		t.Fatalf("ambiguous bare trait filter: %v", err)
	}
	// The runner injects no shadow records as accounts.
	out, _, err := ds.CallFunction(ctx, mbEchoFn, map[string]any{})
	if err != nil {
		t.Fatalf("call echo: %v", err)
	}
	cfg, _ := out.(map[string]any)["config"].(map[string]any)
	if cfg == nil {
		t.Fatalf("no config in output: %v", out)
	}
	if accounts, _ := cfg["accounts"].([]any); len(accounts) != 0 {
		t.Fatalf("the runner injected shadow-trait records as accounts: %v", accounts)
	}
}

func TestMergeAndSplitRefuseFrozenBundleRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)

	a1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "a1@x.co"}})
	a2 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "a2@x.co"}})

	// Disabled: accounts are frozen — merge refuses like put/patch/delete do.
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: a1.Kind, Winner: a1.ID, Loser: a2.ID})
	wantErr(t, err, substrate.ErrGuard, "merge of frozen accounts")
	if err := ds.EnableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("enable: %v", err)
	}
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: a1.Kind, Winner: a1.ID, Loser: a2.ID})
	if err != nil {
		t.Fatalf("merge while live: %v", err)
	}
	// Disabled again: the split would resurrect a frozen account.
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_, err = ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID})
	wantErr(t, err, substrate.ErrGuard, "split resurrecting a frozen account")
	if err := ds.EnableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID}); err != nil {
		t.Fatalf("split while live: %v", err)
	}

	// Uninstall refuses while the merged account still lives — a guard with the
	// count — so a merge/split cannot strand data behind a torn-down kind. It
	// tears the authority down only once purge has cleared the data.
	i1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "i1"}})
	_ = mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "i2"}})
	err = ds.UninstallBundle(ctx, mbPackage)
	wantErr(t, err, substrate.ErrGuard, "uninstall with live data")
	if !strings.Contains(err.Error(), "live records") {
		t.Fatalf("uninstall refusal must carry the count: %v", err)
	}
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := ds.PurgeBundle(ctx, mbPackage); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if err := ds.UninstallBundle(ctx, mbPackage); err != nil {
		t.Fatalf("uninstall after purge: %v", err)
	}
	// The kind is gone: a merge no longer resolves it.
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: i1.Kind, Winner: i1.ID, Loser: "whatever"}); err == nil {
		t.Fatal("merge resolved a torn-down kind")
	}
}

// Merge EFFECTS pass the same admission: a function outside the bundle, with
// the merge grant and emit on the account kind, cannot merge a disabled
// bundle's frozen accounts either — the effect path and the direct verb are
// one code path, and both refuse.
func TestAMergeEffectRefusesFrozenBundleRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	const toolPackage = "wtool.test.dev/wtool"
	mergerDocs := []map[string]any{
		vocabulary.PackageManifest(toolPackage, 0),
		vocabulary.ActorManifest(toolPackage, vocabulary.PackageActor(toolPackage)),
		vocabulary.FunctionManifest(toolPackage, "merger", map[string]any{
			"description": "merges two mail accounts",
			"runtime":     vocabulary.RuntimePython,
			"source": `
def main(input, host):
    a = input["args"]
    return {"effects": [{"action": "merge", "kind": "` + mbAccountType + `",
                         "id": a["winner"], "loser": a["loser"]}]}
`,
			"permissions": map[string]any{"writes": []any{mbAccountType}, "mutations": []any{"merge"}},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, mergerDocs); err != nil {
		t.Fatalf("install merger: %v", err)
	}
	a1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "e1@x.co"}})
	a2 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "e2@x.co"}})
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	args := map[string]any{"winner": a1.ID, "loser": a2.ID}
	_, _, err := ds.CallFunction(ctx, toolPackage+"/merger", args)
	if err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("a merge effect bypassed the bundle freeze: %v", err)
	}
	// Both accounts still live and unmerged.
	if got := mustGet(t, ds, a2.Kind, a2.ID); got.DeletedAt != nil {
		t.Fatalf("frozen loser was merged away: %+v", got)
	}
	// Enabled again, the same effect lands.
	if err := ds.EnableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, n, err := ds.CallFunction(ctx, toolPackage+"/merger", args); err != nil || n != 1 {
		t.Fatalf("merge effect while live: %d %v", n, err)
	}
}

// A split that resurrects a second record of an input's kind is an ordinary
// resurrection now — no cardinality is enforced anywhere — and the input
// simply reads AMBIGUOUS until one record is bound or named "default". The
// merge record is fabricated the way legacy/imported data would carry it.
func TestSplitResurrectingASecondInputReadsAmbiguous(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, db := newDatasetWithSchemaDB(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	c1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	if _, err := ds.Delete(ctx, owner, c1.Kind, c1.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete c1: %v", err)
	}
	c2 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})

	// The fabricated merge record: c1 was "merged into" c2 before this data
	// arrived here.
	const rec = "fabmerge0001"
	if _, err := db.Exec(`
		INSERT INTO records (id, kind, props)
		VALUES ($1, 'substrate.reamde.dev/core/recordmerge',
			jsonb_build_object('moved', '{}'::jsonb, 'winner', $2::text, 'loser', $3::text))`,
		rec, mbConfigType+"/"+c2.ID, mbConfigType+"/"+c1.ID); err != nil {
		t.Fatalf("insert merge record: %v", err)
	}
	// The row is planted behind the engine's back, so its projection in the
	// refs index has to be planted with it: split reads the pair off the record
	// and the reverse reads read the index.
	for property, dst := range map[string]string{"winner": c2.ID, "loser": c1.ID} {
		if _, err := db.Exec(`
			INSERT INTO refs (src_kind, src, property, path, ord, dst_kind, dst)
			VALUES ('substrate.reamde.dev/core/recordmerge', $1, $2, '', 0, $3, $4)`,
			rec, property, mbConfigType, dst); err != nil {
			t.Fatalf("insert the %s row: %v", property, err)
		}
	}

	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec}); err != nil {
		t.Fatalf("split resurrecting a second record of an input's kind: %v", err)
	}
	got := mustGet(t, ds, c1.Kind, c1.ID)
	if got.DeletedAt != nil {
		t.Fatalf("c1 not resurrected: %+v", got)
	}
	// Two live records, none bound or named "default": the input is
	// ambiguous — surfaced per input, never tie-broken, never a refusal of
	// the split itself.
	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil || len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupAmbiguous {
		t.Fatalf("post-split status: %+v %v", st, err)
	}
}

// fenceWaiterSource polls for a flag record, then emits one message: the barrier
// that keeps an invocation in flight while a lifecycle verb races it.
const fenceWaiterSource = `
import time
def main(input, host):
    for _ in range(150):
        got = host.get("mail.bundles.substrate.reamde.dev/mail/mailitem", "fence-flag")
        if got:
            return {"effects": [{"action": "put", "kind": "mail.bundles.substrate.reamde.dev/mail/mailmessage",
                                 "id": "fence-done", "properties": {"subject": "done"}}]}
        time.sleep(0.05)
    return {"effects": []}
`

func fenceWaiterDoc() map[string]any {
	return vocabulary.FunctionManifest(mbPackage, "waiter", map[string]any{
		"description": "waits for the fence flag",
		"runtime":     vocabulary.RuntimePython,
		"source":      fenceWaiterSource,
		"timeout":     "PT20S",
		"permissions": map[string]any{
			"writes": []any{mbMessageType},
			"reads":  map[string]any{"kinds": []any{mbItemType}, "budgets": map[string]any{"calls": 500}},
		},
	})
}

// Disable takes the exclusive side of the per-bundle fence: an invocation
// already past admission commits its effects BEFORE the disable returns, and
// the next invocation refuses.
func TestDisableDrainsAnAdmittedInvocation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		fenceWaiterDoc())
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install: %v", err)
	}
	const waiterFn = mbPackage + "/waiter"

	type callRes struct {
		effects int
		err     error
	}
	callDone := make(chan callRes, 1)
	go func() {
		_, n, err := ds.CallFunction(ctx, waiterFn, map[string]any{})
		callDone <- callRes{n, err}
	}()
	time.Sleep(1 * time.Second) // the invocation is admitted and polling

	disableDone := make(chan error, 1)
	go func() { disableDone <- ds.DisableBundle(ctx, mbPackage) }()
	select {
	case err := <-disableDone:
		select {
		case r := <-callDone:
			t.Fatalf("disable returned while an admitted invocation was in flight: %v (call settled early: %d %v)", err, r.effects, r.err)
		default:
			t.Fatalf("disable returned while an admitted invocation was in flight: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		// Blocked at the fence, as it must be.
	}

	// Release the barrier: the invocation finishes and commits, THEN the
	// disable lands.
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, ID: "fence-flag", Properties: map[string]any{"name": "go"}})
	select {
	case err := <-disableDone:
		if err != nil {
			t.Fatalf("disable: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("disable never returned")
	}
	// The invocation's effect committed before the disable returned.
	if got := mustGet(t, ds, mbMessageType, "fence-done"); got.Properties["subject"] != "done" {
		t.Fatalf("drained invocation's effect: %+v", got.Properties)
	}
	r := <-callDone
	if r.err != nil || r.effects != 1 {
		t.Fatalf("drained invocation: %d %v", r.effects, r.err)
	}
	// And the next admission refuses.
	if _, _, err := ds.CallFunction(ctx, waiterFn, map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("post-disable invocation: %v", err)
	}
}

// Enable refuses the purging transition: an interrupted purge leaves the
// marker standing, and only a purge run to completion clears it.
func TestEnableRefusesAnInterruptedPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, db := newDatasetWithSchemaDB(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "x"}})
	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// Simulate a purge that died mid-run: the marker is set, the data is
	// half-gone or whole — either way the bundle must not come live.
	if _, err := db.Exec(`
		UPDATE records SET props = jsonb_set(props, '{purging}', 'true') WHERE id = $1`, mbPackage); err != nil {
		t.Fatalf("fabricate interrupted purge: %v", err)
	}
	if err := ds.EnableBundle(ctx, mbPackage); err == nil || !errors.Is(err, substrate.ErrGuard) ||
		!strings.Contains(err.Error(), "purging") {
		t.Fatalf("enable during purge: %v", err)
	}
	// A purge run to completion clears the marker; enable then works.
	if _, err := ds.PurgeBundle(ctx, mbPackage); err != nil {
		t.Fatalf("re-purge: %v", err)
	}
	if err := ds.EnableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("enable after a completed purge: %v", err)
	}
}
