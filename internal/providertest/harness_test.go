// Package providertest holds the provider bundle suites: what the shipped
// YAML under kinds/providers.substrate.reamde.dev (and samples/firecrawl)
// does against a loopback fake of the provider's API.
//
// A suite belongs here and not in internal/engine when it proves a closure's
// behavior: every case installs a provider closure, which warms its PEP 723
// bodies through uv, and the engine package must not need uv to run its own
// tests.
//
// One harness, this file: the dataset opens, the uv gate, the closure install
// with the body's API base rewired at the loopback fake, the registry an
// admission test loads, the paged stepper, and the record readers. A provider
// file brings its own fake API and its own assertions and nothing else.
//
// The engine seam is engine.NewStepper (internal/engine/steptest.go): one
// invocation of a callable without the dispatcher, so the paged cursor is
// observable, and the same effect commit production runs. Everything else
// here rides the exported contract in internal/substrate.
package providertest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The trees a suite reads, relative to this package: the seeded core package,
// the shipped providers, the samples. Same depth as internal/engine, so the
// paths inside enginetest resolve here too.
const (
	coreKindsDir = "../../kinds/substrate.reamde.dev/core"
	providersDir = "../../kinds/providers.substrate.reamde.dev"
	samplesDir   = "../../samples"
)

// The core kinds a provider closure's install lands beside.
const (
	typeBundle   = "substrate.reamde.dev/core/bundle"
	typeKind     = "substrate.reamde.dev/core/kind"
	typeFunction = "substrate.reamde.dev/core/function"
	typeTrigger  = "substrate.reamde.dev/core/trigger"
	typeRun      = "substrate.reamde.dev/core/run"
)

// --- the gates ----------------------------------------------------------------

// requireUV skips a suite that needs a database and a warmed PEP 723 body.
// Every provider closure ships at least one body with dependencies, so the
// install itself is what wants uv.
func requireUV(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the closure's body warms through uv at install")
	}
}

// requirePython skips a suite whose bodies are dependency-free: they register
// into the shared python host without uv resolving anything.
func requirePython(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH — the bodies register into the shared host")
	}
}

// uvProvisionFailed reports whether an install failed because uv could not
// build the environment a PEP 723 body declares — the offline machine, and
// the ONLY install failure a suite may skip on. The runner words that failure
// three ways and no others (internal/runner/pyhost.go provisionUV): uv gone
// from PATH after the gate read it, the resolve itself failing, and the
// resolved interpreter not coming back.
//
// Everything else is a hard failure, and the distinction is the whole point:
// a body with a syntax error, a bad import or a startup fault fails at
// REGISTRATION ("runner: python register: …", pyhost.go), and no admission
// test warms a body — bundleRegistry stops at the loader. A predicate wide
// enough to match "register" would turn every one of those into a skip and
// leave a broken closure with nothing red anywhere.
func uvProvisionFailed(err error) bool {
	s := err.Error()
	for _, marker := range []string{
		"runner: uv is not on PATH",
		"runner: uv sync:",
		"runner: uv python find",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// --- the repository -----------------------------------------------------------

// credentialKey is a conforming credential key (ADR 0024), minted once per
// test binary so every Open in the suite shares one. Generated at run time
// and never committed.
var credentialKey = mintCredentialKey()

func mintCredentialKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// migratedTemplate is the database each test copies: Open ran on it once,
// with no repository, so the copy holds the recorded migrations and the
// shipped indexes and nothing else. A copy beside an empty data root is a
// fresh install, and Open on the copy runs every boot step over it.
var migratedTemplate = testdb.NewTemplate("providertest", func(ctx context.Context, dsn string) error {
	root, err := os.MkdirTemp("", "substrate-providertest-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	svc, err := engine.Open(ctx, dsn,
		engine.WithKindsDir(coreKindsDir),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(credentialKey))
	if err != nil {
		return err
	}
	return svc.Close()
})

// newService opens a service over a fresh copy of the migrated template.
func newService(t *testing.T, opts ...engine.Option) substrate.Service {
	t.Helper()
	all := append([]engine.Option{
		engine.WithKindsDir(coreKindsDir),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(credentialKey),
	}, opts...)
	svc, err := engine.Open(context.Background(), migratedTemplate.Clone(t), all...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// newCoreDataset provisions a repository and stops there: the core package
// and nothing else, exactly as creation leaves it. A provider closure
// installs on it, because it names no kind outside its own package
// (decision record 0049).
func newCoreDataset(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset) {
	t.Helper()
	svc := newService(t, opts...)
	ctx := context.Background()
	// The repository id is the test's own name (testdb.Repository): the
	// runner keys function processes on it, so two parallel tests under one
	// name would share processes and either one's Close would retire the
	// other's mid-delivery.
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return svc, ds
}

// newDataset is newCoreDataset with the shipped SAMPLE packages imported —
// people, scheduling, tasks, messaging, calendar — which is what a
// repository a provider mirrors into actually looks like.
func newDataset(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset) {
	t.Helper()
	svc, ds := newCoreDataset(t, opts...)
	importVocabulary(t, ds)
	return svc, ds
}

// importVocabulary imports shipped sample packages by their bare name (all of
// them when none are named) through the ordinary install path. A closure's
// suggested mappings onto a provider's mirrors were dropped when the sample
// was imported ahead of the provider, so a test that wants them re-imports
// the sample after the install, which is what the console asks a reader for
// (decision records 0048 and 0049).
func importVocabulary(t *testing.T, ds substrate.Dataset, names ...string) {
	t.Helper()
	if err := enginetest.ImportVocabulary(context.Background(), ds, names...); err != nil {
		t.Fatalf("import the shipped vocabulary: %v", err)
	}
}

// oauthCallback is the callback URL the host OAuth facility signs its state
// against; loopback is the blessed test seam, and no consent page is opened.
const oauthCallback = "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback"

// newOAuthDataset is newDataset with the OAuth facility on: the fake-provider
// round trip needs the state key, the callback URL and the loopback HTTP
// client wired at engine open. The service comes back with it, because the
// callback carries no bearer and completes on the service, not the dataset.
func newOAuthDataset(t *testing.T, hc *http.Client) (substrate.Service, substrate.Dataset) {
	t.Helper()
	return newDataset(t, engine.WithOAuth("test-state-key", oauthCallback, hc))
}

// completeOAuth is the callback half of the connect flow: the signed state IS
// the authentication, so it resolves the repository itself.
func completeOAuth(t *testing.T, svc substrate.Service, state, code string) {
	t.Helper()
	if _, err := svc.CompleteOAuth(context.Background(), state, code); err != nil {
		t.Fatalf("oauth callback: %v", err)
	}
}

// --- the closure --------------------------------------------------------------

// loadDocs decodes every YAML document in one shipped file.
func loadDocs(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out []map[string]any
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if len(m) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out
}

// bundleRegistry loads the seeded tree plus the shipped samples and installs
// one shipped closure on top of it through the ordinary loader/resolver — the
// same admission the batch apply runs, minus the function-body warm. It is
// what every admission test starts from: no database, no uv.
func bundleRegistry(t *testing.T, dir string) *vocabulary.Registry {
	t.Helper()
	reg, err := enginetest.SeededRegistry(coreKindsDir)
	if err != nil {
		t.Fatalf("build the repository registry: %v", err)
	}
	data, err := os.ReadFile(dir + "/bundle.yaml")
	if err != nil {
		t.Fatalf("read bundle.yaml: %v", err)
	}
	docs, err := vocabulary.ParseStream(data)
	if err != nil {
		t.Fatalf("parse bundle.yaml: %v", err)
	}
	packages, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the bundle package: %v", err)
	}
	if err := reg.InstallAll(packages); err != nil {
		t.Fatalf("the bundle closure did not admit: %v", err)
	}
	return reg
}

// install applies one shipped closure into a live repository through the
// batch vocabulary verb, the ONE install path. rewire, when non-nil, edits
// the documents first: a static manifest cannot bake a dynamic httptest URL,
// so the loopback substitutions happen here.
//
// A uv resolve that cannot reach the network skips (uvProvisionFailed); every
// other apply error FAILS, including a body that will not register, because
// no admission test warms a body and a skip there would hide it.
func install(t *testing.T, ds substrate.Dataset, dir string, rewire func([]map[string]any)) {
	t.Helper()
	docs := loadDocs(t, dir+"/bundle.yaml")
	if rewire != nil {
		rewire(docs)
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs); err != nil {
		if uvProvisionFailed(err) {
			t.Skipf("uv could not resolve the closure's PEP 723 dependencies (offline?): %v", err)
		}
		t.Fatalf("install %s: %v", dir, err)
	}
}

// installRewired is install with body-source substitutions: each pair is one
// string in a function body and what to put in its place, and each is
// asserted to have matched (rewriteSource).
func installRewired(t *testing.T, ds substrate.Dataset, dir string, rewrites ...[2]string) {
	t.Helper()
	install(t, ds, dir, func(docs []map[string]any) {
		for _, r := range rewrites {
			rewriteSource(t, docs, r[0], r[1])
		}
	})
}

// installTriggers writes a closure's shipped delivery wiring as the ordinary
// data records it is. With no ids it writes every trigger the file holds;
// naming ids writes only those, which is how a test keeps the schedule out of
// a drain it wants to observe.
func installTriggers(t *testing.T, ds substrate.Dataset, dir string, ids ...string) {
	t.Helper()
	for _, m := range loadDocs(t, dir+"/triggers.yaml") {
		if len(ids) > 0 {
			meta, _ := m["metadata"].(map[string]any)
			id, _ := meta["id"].(string)
			if !slices.Contains(ids, id) {
				continue
			}
		}
		putDataDoc(t, ds, m)
	}
}

// putDataDoc applies one data-record manifest the way `substratectl apply`
// would.
func putDataDoc(t *testing.T, ds substrate.Dataset, m map[string]any) *substrate.Record {
	t.Helper()
	kind, _ := m["kind"].(string)
	meta, _ := m["metadata"].(map[string]any)
	id, _ := meta["id"].(string)
	data, _ := m["data"].(map[string]any)
	props, _ := data["properties"].(map[string]any)
	row, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: kind, ID: id, Properties: props,
	})
	if err != nil {
		t.Fatalf("put data doc %s/%s: %v", kind, id, err)
	}
	return row
}

// rewriteSource rewrites one string in every function body of a closure and
// FAILS when it matched nothing: a renamed constant then breaks loudly
// instead of silently leaving the shipped value under test. It is the API
// base seam (each body pins its provider origin and allows loopback as the
// test seam) and the constant seam (a bound worth twenty list pages in
// production takes a hundred invocations to reach, so a test lowers the bound
// rather than lowering what is asserted about it).
func rewriteSource(t *testing.T, docs []map[string]any, old, replacement string) {
	t.Helper()
	var hit bool
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		if data == nil || d["kind"] != vocabulary.CoreKind(vocabulary.DocFunction) {
			continue
		}
		src, ok := data["source"].(string)
		if !ok || !strings.Contains(src, old) {
			continue
		}
		data["source"] = strings.ReplaceAll(src, old, replacement)
		hit = true
	}
	if !hit {
		t.Fatalf("no function body declares %q — the fixture substitution broke", old)
	}
}

// rewriteOAuthEndpoints points a bundle document's TRUSTED oauth2 endpoints
// at a loopback fake, replacing every occurrence of each live prefix, and
// FAILS when no endpoint changed: a renamed or re-hosted endpoint then breaks
// loudly instead of leaving the test pointed at the live provider. The
// shipped file itself stays pinned to the live endpoints, so the substitution
// lands exactly where the trusted metadata is authored.
func rewriteOAuthEndpoints(t *testing.T, docs []map[string]any, baseURL string, livePrefixes ...string) {
	t.Helper()
	var changed []string
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		if data == nil || d["kind"] != vocabulary.CoreKind(vocabulary.DocBundle) {
			continue
		}
		o, _ := data["oauth2"].(map[string]any)
		if o == nil {
			t.Fatal("the bundle document carries no oauth2 block")
		}
		for k, v := range o {
			was, ok := v.(string)
			if !ok {
				continue
			}
			now := was
			for _, prefix := range livePrefixes {
				now = strings.ReplaceAll(now, prefix, baseURL)
			}
			if now == was {
				continue
			}
			o[k] = now
			changed = append(changed, k)
		}
	}
	if len(changed) == 0 {
		t.Fatalf("no oauth2 endpoint carries any of %v — nothing was pointed at %s",
			livePrefixes, baseURL)
	}
}

// --- what a closure declares ---------------------------------------------------

// mustKind is one declared kind off the registry, with the miss folded into
// the test.
func mustKind(t *testing.T, reg *vocabulary.Registry, ident string) *vocabulary.Kind {
	t.Helper()
	kind, ok := reg.ByIdentity(ident)
	if !ok {
		t.Fatalf("kind %s missing", ident)
	}
	return kind
}

// assertBundleInput asserts the bundle owning pkg declares one input of that
// name at that kind, injected the named way, and hands the bundle back. An
// OAuth client input carries inject "": it is facility-read, never handed to
// a body. A bearer-key input carries BundleInputInjectFunctions.
func assertBundleInput(t *testing.T, reg *vocabulary.Registry, pkg, input, kind, inject string) *vocabulary.Bundle {
	t.Helper()
	b, ok := reg.BundleOf(pkg)
	if !ok {
		t.Fatalf("no bundle owns %s after install", pkg)
	}
	in, ok := b.Inputs[input]
	if !ok {
		t.Fatalf("bundle declares no %s input: %v", input, b.InputOrder)
	}
	if in.Kind != kind {
		t.Fatalf("%s input kind = %q, want %q", input, in.Kind, kind)
	}
	if in.Inject != inject {
		t.Fatalf("%s input inject = %q, want %q", input, in.Inject, inject)
	}
	return b
}

// mustEmit asserts a function's emit ceiling names every identity: a kind
// missing here is a write the engine refuses at effect decode.
func mustEmit(t *testing.T, fn *vocabulary.Function, want ...string) {
	t.Helper()
	have := map[string]bool{}
	for _, kind := range fn.Caps.Emit {
		have[kind] = true
	}
	for _, kind := range want {
		if !have[kind] {
			t.Fatalf("%s emit %v does not name %s — the write would be refused",
				fn.Identity(), fn.Caps.Emit, kind)
		}
	}
}

// mustRead asserts a function's read allowlist names every identity.
func mustRead(t *testing.T, fn *vocabulary.Function, want ...string) {
	t.Helper()
	if fn.Caps.Reads == nil {
		t.Fatalf("%s declares no reads", fn.Identity())
	}
	have := map[string]bool{}
	for _, kind := range fn.Caps.Reads.Kinds {
		have[kind] = true
	}
	for _, kind := range want {
		if !have[kind] {
			t.Fatalf("%s reads %v does not name %s", fn.Identity(), fn.Caps.Reads.Kinds, kind)
		}
	}
}

// mustProps asserts a kind declares every named property.
func mustProps(t *testing.T, kind *vocabulary.Kind, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, ok := kind.Prop(name); !ok {
			t.Fatalf("%s declares no property %q — the body writes it", kind.Identity, name)
		}
	}
}

// --- what an install landed ----------------------------------------------------

// assertMembers asserts every member of a closure landed as its own record of
// the named core kind.
func assertMembers(t *testing.T, ds substrate.Dataset, members map[string]string) {
	t.Helper()
	ctx := context.Background()
	for id, wantKind := range members {
		row, err := ds.Get(ctx, wantKind, id)
		if err != nil {
			t.Fatalf("member %s did not install: %v", id, err)
		}
		if row.Kind != wantKind {
			t.Fatalf("member %s is a %s, want %s", id, row.Kind, wantKind)
		}
	}
}

// assertTriggers asserts every named trigger landed as a trigger record.
func assertTriggers(t *testing.T, ds substrate.Dataset, ids ...string) {
	t.Helper()
	ctx := context.Background()
	for _, id := range ids {
		row, err := ds.Get(ctx, typeTrigger, id)
		if err != nil {
			t.Fatalf("trigger %s did not install: %v", id, err)
		}
		if row.Kind != typeTrigger {
			t.Fatalf("trigger %s is a %s", id, row.Kind)
		}
	}
}

// assertUnresolvedInput asserts the computed bundle status a freshly
// installed closure has: installed, enabled, its function count, and its one
// input named but unresolved — no config record exists yet, so the status
// carries the setup item that says so.
func assertUnresolvedInput(t *testing.T, ds substrate.Dataset, pkg, input, kind string, functions int) {
	t.Helper()
	st, err := ds.BundleStatus(context.Background(), pkg)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("bundle not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Name != input || st.Inputs[0].Kind != kind {
		t.Fatalf("status inputs = %+v, want the one %s input", st.Inputs, input)
	}
	if st.Inputs[0].Record != "" || st.Inputs[0].Via != "" {
		t.Fatalf("%s input resolved with no config record created: %+v", input, st.Inputs[0])
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupMissing || st.Setup[0].Input != input {
		t.Fatalf("status setup = %+v, want the one missing-input item", st.Setup)
	}
	if st.Functions != functions {
		t.Fatalf("status functions = %d, want %d", st.Functions, functions)
	}
}

// --- reading records -----------------------------------------------------------

// mustGet is Get with the error folded into the test.
func mustGet(t *testing.T, ds substrate.Dataset, kind, id string) *substrate.Record {
	t.Helper()
	row, err := ds.Get(context.Background(), kind, id)
	if err != nil {
		t.Fatalf("get %s %s: %v", kind, id, err)
	}
	return row
}

// mustPut is Put under the API actor with the error folded into the test.
func mustPut(t *testing.T, ds substrate.Dataset, in substrate.PutInput) *substrate.Record {
	t.Helper()
	row, err := ds.Put(context.Background(), substrate.ActorAPI, in)
	if err != nil {
		t.Fatalf("put %s/%s: %v", in.Kind, in.ID, err)
	}
	return row
}

// storedRefPath reads a stored reference value as the record path it names,
// in either shape: the flat path a declaration without link data stores, or
// the {ref, <props>...} object one with `properties:` stores.
func storedRefPath(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if s, ok := t[vocabulary.ReferenceValueKey].(string); ok {
			return s
		}
	}
	return ""
}

// refIDs reads a record's reference property as the record ids it names, in
// order, so an assertion about a pointer does not have to know whether the
// declaration carries link data.
func refIDs(row *substrate.Record, name string) []string {
	v := row.Properties[name]
	list, repeated := v.([]any)
	if !repeated {
		list = []any{v}
	}
	var out []string
	for _, item := range list {
		p := storedRefPath(item)
		if p == "" {
			continue
		}
		_, id, _ := vocabulary.SplitRecordPath(p)
		out = append(out, id)
	}
	return out
}

// listLive pages every live record of one kind.
func listLive(t *testing.T, ds substrate.Dataset, kind string) []*substrate.Record {
	t.Helper()
	ctx := context.Background()
	var out []*substrate.Record
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{kind}}, First: 500}
	for {
		page, err := ds.List(ctx, q)
		if err != nil {
			t.Fatalf("list %s: %v", kind, err)
		}
		out = append(out, page.Records...)
		if page.Cursor == "" {
			return out
		}
		q.After = page.Cursor
	}
}

// countLive counts the live records of one kind.
func countLive(t *testing.T, ds substrate.Dataset, kind string) int {
	t.Helper()
	return len(listLive(t, ds, kind))
}

// countLiveWithRef counts the live records of one kind whose reference
// property names one record path. The STORED value is a record path, never
// the bare id a sync body wrote.
func countLiveWithRef(t *testing.T, ds substrate.Dataset, kind, name, path string) int {
	t.Helper()
	var n int
	for _, row := range listLive(t, ds, kind) {
		if storedRefPath(row.Properties[name]) == path {
			n++
		}
	}
	return n
}

// drainTriggers runs the one delivery mechanism to quiescence.
func drainTriggers(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	for range 40 {
		n, err := ds.ProcessTriggers(ctx)
		if err != nil {
			t.Fatalf("process triggers: %v", err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("the trigger chain did not drain")
}

// parkedFailures counts the parked deliveries every trigger holds.
func parkedFailures(t *testing.T, ds substrate.Dataset) int64 {
	t.Helper()
	statuses, err := ds.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	var n int64
	for _, st := range statuses {
		n += st.Parked
	}
	return n
}

// --- driving a sync ------------------------------------------------------------

// stepper drives one sync body page by page through the runner with no
// trigger machinery, so the paged-checkpoint CURSOR itself is observable. It
// is engine.Stepper with the errors folded into the test.
type stepper struct {
	t  *testing.T
	s  *engine.Stepper
	n  int
	id string
}

// newStepper resolves one callable and hands back a stepper over it. cfg is
// the injected config the invocation receives: the accounts to walk and their
// tokens (stepConfig builds the usual one).
func newStepper(t *testing.T, ds substrate.Dataset, fnID string, cfg map[string]any) *stepper {
	t.Helper()
	s, err := engine.NewStepper(ds, fnID, cfg)
	if err != nil {
		t.Fatalf("step %s: %v", fnID, err)
	}
	return &stepper{t: t, s: s, id: fnID}
}

// setConfig replaces the injected config between steps: a feature toggle
// flipped mid-drain must not shift the walk.
func (s *stepper) setConfig(cfg map[string]any) { s.s.SetConfig(cfg) }

// setEnvelope puts a delivery envelope on every following step, the way the
// dispatcher carries one across a paged chain.
func (s *stepper) setEnvelope(env map[string]any) { s.s.SetEnvelope(env) }

// step runs ONE invocation of the chain: resume is the previous page's cursor
// (nil for a fresh delivery). It returns the staged effects, the output and
// the continuation cursor (nil when drained) WITHOUT committing anything.
func (s *stepper) step(resume any) ([]engine.StepEffect, any, map[string]any) {
	s.t.Helper()
	s.n++
	effects, out, cur, err := s.s.Step(context.Background(), resume)
	if err != nil {
		s.t.Fatalf("%s step %d: %v", s.id, s.n, err)
	}
	return effects, out, cur
}

// apply commits the last step's effects the way the dispatcher does.
func (s *stepper) apply() {
	s.t.Helper()
	if err := s.s.Apply(context.Background()); err != nil {
		s.t.Fatalf("%s: apply the effects of step %d: %v", s.id, s.n, err)
	}
}

// drain steps until the chain completes WITHOUT applying anything, returning
// the LAST invocation's effects (the one carrying the account stamp) and how
// many steps ran.
func (s *stepper) drain(resume any) ([]engine.StepEffect, int) {
	s.t.Helper()
	for i := 0; ; i++ {
		if i > 40 {
			s.t.Fatalf("the paged chain did not drain in 40 steps")
		}
		effects, _, cur := s.step(resume)
		if cur == nil {
			return effects, s.n
		}
		resume = cur
	}
}

// drainApplying steps until the chain completes, APPLYING each page's effects
// the way the dispatcher does, and returns every effect the run produced.
func (s *stepper) drainApplying(resume any) []engine.StepEffect {
	s.t.Helper()
	var all []engine.StepEffect
	for i := 0; ; i++ {
		if i > 40 {
			s.t.Fatalf("the paged chain did not drain in 40 steps")
		}
		effects, _, cur := s.step(resume)
		all = append(all, effects...)
		s.apply()
		if cur == nil {
			return all
		}
		resume = cur
	}
}

// accountStamp finds the connector stamp patch one account got in a run's
// effects, the newest one when a run stamped more than once.
func accountStamp(t *testing.T, effects []engine.StepEffect, kind, id string) map[string]any {
	t.Helper()
	for i := len(effects) - 1; i >= 0; i-- {
		ef := &effects[i]
		if ef.Action == "patch" && ef.Kind == kind && ef.ID == id {
			return ef.Properties
		}
	}
	t.Fatalf("no %s stamp patch for %s in %d effects", kind, id, len(effects))
	return nil
}

// stampOf is accountStamp by id alone, for a run that stamps two accounts of
// one kind and cares which.
func stampOf(effects []engine.StepEffect, id string) map[string]any {
	for i := len(effects) - 1; i >= 0; i-- {
		if effects[i].Action == "patch" && effects[i].ID == id {
			return effects[i].Properties
		}
	}
	return nil
}

// stepConfig is the injected config a sync invocation receives: one account,
// its properties, and a live token. A body reads the token off the account
// entry, never off a record.
func stepConfig(kind, id string, props map[string]any) map[string]any {
	return map[string]any{
		"accounts": []any{map[string]any{
			"id": id, "type": kind, "properties": props, "token": fakeToken,
		}},
	}
}

// syncProps is one account's properties for a step, the shipped defaults
// (hourly, last30d) plus whatever the case adds and whichever feature toggles
// it names.
func syncProps(extra map[string]any, toggles ...string) map[string]any {
	props := map[string]any{"syncFrequency": "hourly", "backfillDepth": "last30d"}
	for _, toggle := range toggles {
		props[toggle] = true
	}
	maps.Copy(props, extra)
	return props
}

// resyncHandPackage is the test's own bundle-tier hand: a declared function
// that never runs, installed only so its actor resolves at the bundle tier.
const (
	resyncHandPackage = "resync.example.com/hand"
	resyncHandFn      = resyncHandPackage + "/clear"
)

// installResyncHand installs the hand resyncAccount writes under.
func installResyncHand(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI,
		[]map[string]any{
			vocabulary.PackageManifest(resyncHandPackage, 1),
			vocabulary.FunctionManifest(resyncHandPackage, "clear", map[string]any{
				"description": "a bundle-tier hand for the re-sync clear; never called",
				"runtime":     vocabulary.RuntimePython,
				"source":      "def main(input, host):\n    return {}\n",
			}),
		}); err != nil {
		t.Fatalf("install the re-sync hand: %v", err)
	}
}

// resyncAccount clears an account's completion marker and drains: the removal
// is itself an account update the on-connect guard matches, so the one patch
// both resets the guard and fires the re-sync.
//
// Two constraints decide whose hand it is. `lastSyncedAt` is
// `writer: connector`, so only a BUNDLE-tier actor may clear it, and a
// declared function's actor is that tier. And a callable never sees its own
// writes (the dispatcher's self-exclusion), so the clear cannot be stamped as
// the sync itself. The hand is therefore a declared function of the TEST's
// own: installResyncHand.
func resyncAccount(t *testing.T, ds substrate.Dataset, kind, id string) {
	t.Helper()
	hand := substrate.FunctionActor(vocabulary.SplitKindRef(resyncHandFn))
	if _, err := ds.Patch(context.Background(), hand, kind, id, substrate.PatchInput{
		Properties: map[string]any{"lastSyncedAt": nil},
	}); err != nil {
		t.Fatalf("clear lastSyncedAt: %v", err)
	}
	drainTriggers(t, ds)
}

// --- the loopback fakes --------------------------------------------------------

// fakeToken is the access token every fake API accepts, and the one a
// rewired closure's OAuth exchange mints.
const fakeToken = "at-1"

// fakeAPI is the plumbing every provider fake shares: the httptest server,
// the request log two kinds of assertion read (a query for a window, a path
// for an id), and the bearer check that proves the body sends its token. A
// provider's fake embeds it and brings its own handlers.
type fakeAPI struct {
	ts *httptest.Server

	mu      sync.Mutex
	paths   []string
	queries []string
	counts  map[string]int
}

// serve puts mux on loopback for the length of the test.
func (f *fakeAPI) serve(t *testing.T, mux http.Handler) {
	t.Helper()
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
}

// record logs one request: the escaped path and the raw query, apart, because
// an id can live in either.
func (f *fakeAPI) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path)
	f.queries = append(f.queries, r.URL.RawQuery)
}

// seen is the recorded queries, which is where a window clause lands.
func (f *fakeAPI) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.queries)
}

// seenPaths is the recorded paths, which is where a get names its id.
func (f *fakeAPI) seenPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.paths)
}

// bump counts one named event (a page served, a get answered).
func (f *fakeAPI) bump(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		f.counts = map[string]int{}
	}
	f.counts[name]++
}

// count is how many times bump saw one name.
func (f *fakeAPI) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[name]
}

// writeJSON answers one request with a JSON body.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// bearer refuses a request that does not carry the fake's token, which is how
// a test proves the body sent it.
func bearer(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+fakeToken {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

// --- fixture instants ----------------------------------------------------------

// ago and ahead date every fixture RELATIVE to now. Hard-coded instants rot:
// a row seeded at a fixed date as "inside the last-30-days window" falls out
// of it thirty days after the test was written, and the assertion that the
// sweep spared it starts failing on a calendar.
func ago(d time.Duration) string {
	return time.Now().UTC().Add(-d).Truncate(time.Second).Format(time.RFC3339)
}

func ahead(d time.Duration) string {
	return time.Now().UTC().Add(d).Truncate(time.Second).Format(time.RFC3339)
}

// millisAgo is the same instant as an epoch-millis timestamp (Gmail's
// internalDate).
func millisAgo(d time.Duration) string {
	return fmt.Sprint(time.Now().UTC().Add(-d).UnixMilli())
}

// sortedKeys is a map's keys in order, for an error message that has to be
// stable.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
