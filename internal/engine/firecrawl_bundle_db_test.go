package engine

// The Firecrawl bundle — a CAPABILITY BUNDLE (web search + scraping as
// callable agent tools), not an account integration. Two proofs, from the
// shipped closure at ../../kinds/firecrawl.bundles.substrate.reamde.dev:
//
//  1. TestFirecrawlBundleAdmitsSchema — the closure ADMITS through the schema
//     loader: the bundle declares one `connector` input injected into its
//     functions (no oauth2, no accountconfig — one bearer key, no OAuth
//     client, no per-user accounts),
//     the webdocument type carries the scrape's durable shape, and both
//     functions register as callables with input schemas (their own tool
//     cards). No DB, no network — pure schema admission. And no triggers:
//     the closure is bundle.yaml alone, so a passing install here IS the
//     zero-trigger admission proof.
//
//  2. TestFirecrawlBundleCallsTools — the zero-trigger closure installs into
//     a live repository and both functions run in call mode against a FAKE
//     Firecrawl server (the config's baseUrl points at it; real Firecrawl is
//     never dialed): websearch answers {title, url, snippet} hits and applies
//     ZERO effects; scrapepage caps the markdown at 24000 characters, writes
//     ONE webdocument keyed host.ids.url(url), and a re-scrape UPDATES that
//     document in place instead of minting a second.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner/substratefn"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	firecrawlExampleDir = "../../kinds/firecrawl.bundles.substrate.reamde.dev"
	firecrawlAuthority  = "firecrawl.bundles.substrate.reamde.dev"
	firecrawlBundleRow  = firecrawlAuthority + "/firecrawl"
	firecrawlConfigType = firecrawlAuthority + "/config"
	firecrawlDocType    = firecrawlAuthority + "/webdocument"
	firecrawlSearchFn   = firecrawlAuthority + "/websearch"
	firecrawlScrapeFn   = firecrawlAuthority + "/scrapepage"

	firecrawlTestKey = "fc-unit-test-key"
	firecrawlPageURL = "https://blog.example.com/how-substrates-compose"
)

// TestFirecrawlBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the body warm. Every assertion is a
// rule the loader enforces at admission time.
func TestFirecrawlBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	// The registry an install actually admits into: the seeded tree (core
	// alone) plus the shipped VOCABULARY bundles this repository imported —
	// what a closure declaring onto people/tasks/messaging/calendar/media
	// needs present, and what `requires:` names.
	reg, err := enginetest.SeededRegistry("../../kinds/core.substrate.reamde.dev")
	if err != nil {
		t.Fatalf("build the repository registry: %v", err)
	}
	data, err := os.ReadFile(firecrawlExampleDir + "/bundle.yaml")
	if err != nil {
		t.Fatalf("read bundle.yaml: %v", err)
	}
	docs, err := vocabulary.ParseStream(data)
	if err != nil {
		t.Fatalf("parse bundle.yaml: %v", err)
	}
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the bundle authority: %v", err)
	}
	if err := reg.InstallAll(authorities); err != nil {
		t.Fatalf("the bundle closure did not admit: %v", err)
	}

	// The bundle exists, it declares the one `connector` input injected into
	// its functions, and it ships no oauth2 manifest block — a capability
	// bundle, not an integration.
	b, ok := reg.BundleOf(firecrawlAuthority)
	if !ok {
		t.Fatalf("no bundle owns %s after install", firecrawlAuthority)
	}
	in, ok := b.Inputs["connector"]
	if !ok {
		t.Fatalf("bundle declares no connector input: %v", b.InputOrder)
	}
	if in.Kind != firecrawlConfigType {
		t.Fatalf("connector input kind = %q, want %q", in.Kind, firecrawlConfigType)
	}
	if in.Inject != vocabulary.BundleInputInjectFunctions {
		t.Fatalf("connector input inject = %q, want %q", in.Inject, vocabulary.BundleInputInjectFunctions)
	}
	if b.OAuth2 != nil {
		t.Fatalf("bundle carries an oauth2 block — a bearer-key bundle declares no OAuth client")
	}

	// The config type: deliberately neither oauth2 (no client creds) nor
	// accountconfig (no per-user accounts).
	cfg, ok := reg.ByIdentity(firecrawlConfigType)
	if !ok {
		t.Fatalf("config type %s missing", firecrawlConfigType)
	}
	if cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("%s implements oauth2 — the apiKey is a bearer token, not an OAuth client", firecrawlConfigType)
	}
	if cfg.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("%s implements accountconfig — this bundle has no connected accounts", firecrawlConfigType)
	}
	key, ok := cfg.Prop("apiKey")
	if !ok || !key.Secret() {
		t.Fatalf("apiKey is not a secret property: %+v", key)
	}
	if key.Writer != vocabulary.WriterOwner {
		t.Fatalf("apiKey writer = %q, want %q", key.Writer, vocabulary.WriterOwner)
	}

	// The config carries a displayTemplate so a console row reads as what it
	// is (fleet review F7).
	if cfg.DisplayTemplate == "" {
		t.Fatalf("%s declares no displayTemplate", firecrawlConfigType)
	}

	// The webdocument type carries the scrape's durable shape, every mirror
	// property labeled (fleet review F7). `title` stays the reserved
	// built-in — never a declared property.
	doc, ok := reg.ByIdentity(firecrawlDocType)
	if !ok {
		t.Fatalf("document type %s missing", firecrawlDocType)
	}
	for _, name := range []string{"url", "content", "truncated", "fetchedAt", "raw"} {
		p, ok := doc.Prop(name)
		if !ok {
			t.Fatalf("webdocument declares no %q property", name)
		}
		if p.DisplayName == "" {
			t.Fatalf("webdocument property %q carries no displayName", name)
		}
	}
	if _, ok := doc.Prop("title"); ok {
		t.Fatalf("webdocument declares title — the built-in owns it")
	}

	// Both callables registered, each its own tool card: a model-facing
	// description, a declared input shape, and the document type in emit
	// (scrapepage's write; websearch's required-non-empty ceiling).
	for _, name := range []string{firecrawlSearchFn, firecrawlScrapeFn} {
		fn, err := reg.ResolveFunction(name)
		if err != nil {
			t.Fatalf("function %s did not register: %v", name, err)
		}
		if fn.Description == "" || fn.Input == nil || fn.Output == nil {
			t.Fatalf("%s is not a full tool card: description=%q input=%v output=%v",
				name, fn.Description, fn.Input != nil, fn.Output != nil)
		}
		if len(fn.Caps.Emit) != 1 || fn.Caps.Emit[0] != firecrawlDocType {
			t.Fatalf("%s emit = %v, want [%s]", name, fn.Caps.Emit, firecrawlDocType)
		}
	}
}

// TestFirecrawlBundleCallsTools installs the zero-trigger closure into a live
// repository and drives both tools in call mode against a fake Firecrawl server.
// The bodies are dependency-free python (no PEP 723), so the shared python3
// host is the only runtime requirement.
func TestFirecrawlBundleCallsTools(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH — the bodies register into the shared host")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	// The fake Firecrawl: bearer-checked /v2/search and /v2/scrape. The first
	// scrape answers markdown far past the 24000-char cap; the second answers
	// a short page, so a re-scrape provably rewrites the document.
	var scrapes atomic.Int32
	longBody := strings.Repeat("substrates compose from primitives. ", 900) // 32400 chars
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+firecrawlTestKey {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"success":false,"error":"unauthorized"}`)
			return
		}
		switch r.URL.Path {
		case "/v2/search":
			var req struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"success":false,"error":"bad request"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{"web": []map[string]any{
					{
						"title": "How substrates compose", "url": firecrawlPageURL,
						"description": "the primitive set, end to end",
					},
					{"title": "Bundles as tools", "url": "https://blog.example.com/bundles-as-tools"},
				}},
			})
		case "/v2/scrape":
			markdown := longBody
			if scrapes.Add(1) > 1 {
				markdown = "# updated\n\nthe second read"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"markdown": markdown,
					"metadata": map[string]any{"title": "How Substrates Compose", "sourceURL": firecrawlPageURL},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// The atomic install from the shipped manifest — bundle.yaml ALONE: the
	// closure ships zero triggers, and it admits.
	vocabularyDocs := loadYAMLDocs(t, firecrawlExampleDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		t.Fatalf("install the firecrawl bundle: %v", err)
	}
	for id, wantType := range map[string]string{
		firecrawlBundleRow:  "core.substrate.reamde.dev/bundle",
		firecrawlConfigType: "core.substrate.reamde.dev/kind",
		firecrawlDocType:    "core.substrate.reamde.dev/kind",
		firecrawlSearchFn:   "core.substrate.reamde.dev/function",
		firecrawlScrapeFn:   "core.substrate.reamde.dev/function",
	} {
		row, err := ds.Get(ctx, wantType, id)
		if err != nil {
			t.Fatalf("member %s did not install: %v", id, err)
		}
		if row.Kind != wantType {
			t.Fatalf("member %s is a %s, want %s", id, row.Kind, wantType)
		}
	}
	st, err := ds.BundleStatus(ctx, firecrawlAuthority)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("the zero-trigger bundle is not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Name != "connector" || st.Inputs[0].Kind != firecrawlConfigType {
		t.Fatalf("status inputs = %+v, want the one connector input", st.Inputs)
	}
	if st.Inputs[0].Record != "" || st.Inputs[0].Via != "" {
		t.Fatalf("connector input resolved with no config record created: %+v", st.Inputs[0])
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupMissing || st.Setup[0].Input != "connector" {
		t.Fatalf("status setup = %+v, want the one missing-input item", st.Setup)
	}
	if st.Functions != 2 {
		t.Fatalf("status functions = %d, want 2", st.Functions)
	}

	// Configure — first with a HOSTILE baseUrl: an owner-editable base must
	// never redirect the bearer key (fleet review F1 / codex H1). Both
	// bodies refuse before building a request; nothing is written.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: firecrawlConfigType, ID: "firecrawl",
		Properties: map[string]any{"apiKey": firecrawlTestKey, "baseUrl": "https://evil.example.com"},
	}); err != nil {
		t.Fatalf("create the firecrawl config: %v", err)
	}
	if st, err = ds.BundleStatus(ctx, firecrawlAuthority); err != nil {
		t.Fatalf("bundle status after the config record: %v", err)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Record != "firecrawl" || st.Inputs[0].Via != substrate.InputViaSole {
		t.Fatalf("connector input did not resolve to the sole record: %+v", st.Inputs)
	}
	if len(st.Setup) != 0 {
		t.Fatalf("status setup = %+v, want empty once the input resolves", st.Setup)
	}
	for fn, input := range map[string]map[string]any{
		firecrawlSearchFn: {"query": "anything"},
		firecrawlScrapeFn: {"url": firecrawlPageURL},
	} {
		_, applied, err := ds.CallFunction(ctx, fn, input)
		if err == nil || !strings.Contains(err.Error(), "pinned provider origin") {
			t.Fatalf("%s against a hostile baseUrl: err=%v, want the origin-pin refusal", fn, err)
		}
		if applied != 0 {
			t.Fatalf("%s against a hostile baseUrl applied %d effects", fn, applied)
		}
	}
	if n := countLiveOf(t, ds, firecrawlDocType); n != 0 {
		t.Fatalf("a refused call minted %d webdocuments", n)
	}

	// Re-point the base at the fake server: loopback is the blessed test
	// seam (any scheme, any port), so no body ever dials real Firecrawl.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, firecrawlConfigType, "firecrawl", substrate.PatchInput{
		Properties: map[string]any{"baseUrl": srv.URL},
	}); err != nil {
		t.Fatalf("re-point the firecrawl config at the fake: %v", err)
	}

	// scrapepage validates its url INPUT before hashing or dialing: only an
	// absolute https URL with a hostname and no embedded credentials passes.
	for url, wantErr := range map[string]string{
		"blog.example.com/how":                   "https",
		"http://blog.example.com/how":            "https",
		"https://":                               "hostname",
		"https://user:pass@blog.example.com/how": "credentials",
	} {
		_, applied, err := ds.CallFunction(ctx, firecrawlScrapeFn, map[string]any{"url": url})
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("scrapepage(%q): err=%v, want a refusal mentioning %q", url, err, wantErr)
		}
		if applied != 0 {
			t.Fatalf("scrapepage(%q) applied %d effects", url, applied)
		}
	}
	if n := scrapes.Load(); n != 0 {
		t.Fatalf("an invalid url reached the provider %d times — validation must run before the call", n)
	}
	if n := countLiveOf(t, ds, firecrawlDocType); n != 0 {
		t.Fatalf("an invalid url minted %d webdocuments — validation must run before hashing", n)
	}

	// websearch: hits come back shaped, and the call is EFFECTS-FREE.
	out, applied, err := ds.CallFunction(ctx, firecrawlSearchFn,
		map[string]any{"query": "how substrates compose", "limit": 2})
	if err != nil {
		t.Fatalf("call websearch: %v", err)
	}
	if applied != 0 {
		t.Fatalf("websearch applied %d effects — a read tool writes nothing", applied)
	}
	results, _ := out.(map[string]any)["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("websearch results: %v", out)
	}
	first, _ := results[0].(map[string]any)
	if first["title"] != "How substrates compose" || first["url"] != firecrawlPageURL ||
		first["snippet"] != "the primitive set, end to end" {
		t.Fatalf("websearch first hit: %v", first)
	}
	if n := countLiveOf(t, ds, firecrawlDocType); n != 0 {
		t.Fatalf("websearch minted %d webdocuments", n)
	}

	// scrapepage: the content caps at 24000, ONE webdocument lands under
	// host.ids.url(url), and the built-in title rides the put.
	docID := substratefn.URLID(firecrawlPageURL)
	out, applied, err = ds.CallFunction(ctx, firecrawlScrapeFn, map[string]any{"url": firecrawlPageURL})
	if err != nil {
		t.Fatalf("call scrapepage: %v", err)
	}
	if applied != 1 {
		t.Fatalf("scrapepage applied %d effects, want 1", applied)
	}
	om, _ := out.(map[string]any)
	if om["document"] != docID {
		t.Fatalf("scrapepage document = %v, want %s", om["document"], docID)
	}
	content, _ := om["content"].(string)
	if len(content) != 24000 || om["truncated"] != true {
		t.Fatalf("scrapepage cap: len=%d truncated=%v", len(content), om["truncated"])
	}
	doc, err := ds.Get(ctx, firecrawlDocType, docID)
	if err != nil {
		t.Fatalf("get the webdocument: %v", err)
	}
	if doc.Kind != firecrawlDocType || doc.Title != "How Substrates Compose" {
		t.Fatalf("webdocument shape: type=%s title=%q", doc.Kind, doc.Title)
	}
	if got, _ := doc.Properties["content"].(string); len(got) != 24000 {
		t.Fatalf("stored content len = %d, want 24000", len(got))
	}
	if doc.Properties["url"] != firecrawlPageURL || doc.Properties["truncated"] != true {
		t.Fatalf("webdocument properties: %v", doc.Properties)
	}
	if s, _ := doc.Properties["fetchedAt"].(string); s == "" {
		t.Fatalf("webdocument carries no fetchedAt")
	}
	if doc.Properties["raw"] == nil {
		t.Fatalf("webdocument carries no raw scrape metadata")
	}

	// Re-scrape: the SAME id updates in place (if_absent stays false) — one
	// document, new content, a moved version.
	if _, applied, err = ds.CallFunction(ctx, firecrawlScrapeFn, map[string]any{"url": firecrawlPageURL}); err != nil {
		t.Fatalf("re-scrape: %v", err)
	}
	if applied != 1 {
		t.Fatalf("re-scrape applied %d effects, want 1", applied)
	}
	updated, err := ds.Get(ctx, firecrawlDocType, docID)
	if err != nil {
		t.Fatalf("get the re-scraped webdocument: %v", err)
	}
	if got, _ := updated.Properties["content"].(string); !strings.Contains(got, "the second read") {
		t.Fatalf("re-scrape did not rewrite the content: %q", got)
	}
	if updated.Properties["truncated"] != false || updated.Version <= doc.Version {
		t.Fatalf("re-scrape shape: truncated=%v version %d -> %d",
			updated.Properties["truncated"], doc.Version, updated.Version)
	}
	if n := countLiveOf(t, ds, firecrawlDocType); n != 1 {
		t.Fatalf("re-scrape left %d webdocuments, want the one", n)
	}
}
