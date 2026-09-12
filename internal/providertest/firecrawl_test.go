package providertest

// The Firecrawl bundle — a CAPABILITY BUNDLE (web search + scraping as
// callable agent tools), not an account integration. Three proofs, from the
// shipped closure at ../../samples/firecrawl:
//
//  1. TestFirecrawlBundleAdmitsSchema — the closure ADMITS through the schema
//     loader: the bundle declares NO input at all (no oauth2, no
//     accountconfig, no connector kind — one bearer key carried by the core
//     `secret` record it ships, decision record 0076), the webdocument type
//     carries the scrape's durable shape, and both functions register as
//     callables with input schemas (their own tool cards). No DB, no
//     network — pure schema admission. And no triggers: the closure is
//     bundle.yaml alone, so a passing install here IS the zero-trigger
//     admission proof.
//
//  2. TestFirecrawlBundleImportsSettings — the SAMPLE door: importing the
//     closure lands the two configuration records rehomed onto the
//     repository's own authority, the empty required key is the bundle's one
//     setup item, and filling it clears it.
//
//  3. TestFirecrawlBundleCallsTools — the zero-trigger closure installs into
//     a live repository and both functions run in call mode against a FAKE
//     Firecrawl server (the baseUrl setting points at it; real Firecrawl is
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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const (
	firecrawlDir      = samplesDir + "/firecrawl"
	firecrawlPackage  = "samples.substrate.reamde.dev/firecrawl"
	firecrawlDocType  = firecrawlPackage + "/webdocument"
	firecrawlSearchFn = firecrawlPackage + "/websearch"
	firecrawlScrapeFn = firecrawlPackage + "/scrapepage"

	// The bundle's configuration as records: the ids ARE the ownership, so
	// they sit under the bundle's own id (decision record 0076).
	firecrawlKeyRecord  = firecrawlPackage + "/apiKey"
	firecrawlBaseRecord = firecrawlPackage + "/baseUrl"

	typeSetting = "substrate.reamde.dev/core/setting"
	typeSecret  = "substrate.reamde.dev/core/secret"

	firecrawlTestKey = "fc-unit-test-key"
	firecrawlPageURL = "https://blog.example.com/how-substrates-compose"
)

// TestFirecrawlBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the body warm. Every assertion is a
// rule the loader enforces at admission time.
func TestFirecrawlBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, firecrawlDir)

	// The bundle exists, declares NO input and no oauth2 manifest block: its
	// configuration is the two core records it ships, not a kind of its own
	// (decision record 0076).
	b, ok := reg.BundleOf(firecrawlPackage)
	if !ok {
		t.Fatalf("the firecrawl bundle did not register")
	}
	if len(b.Inputs) != 0 {
		t.Fatalf("bundle declares inputs %v — its configuration is core setting/secret records", b.InputOrder)
	}
	if b.OAuth2 != nil {
		t.Fatalf("bundle carries an oauth2 block — a bearer-key bundle declares no OAuth client")
	}
	if _, ok := reg.ByIdentity(firecrawlPackage + "/config"); ok {
		t.Fatalf("the bundle still declares a config kind — the key and the base URL are core records now")
	}

	// The shipped configuration records: a required, empty `secret` for the
	// key and a url-typed `setting` pre-filled with the pinned origin. Both
	// ids sit under the bundle's own id, which is the ONLY thing that makes
	// them this bundle's.
	settings := map[string]map[string]any{}
	for _, d := range loadDocs(t, firecrawlDir+"/settings.yaml") {
		meta, _ := d["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		data, _ := d["data"].(map[string]any)
		props, _ := data["properties"].(map[string]any)
		kind, _ := d["kind"].(string)
		props["kind"] = kind
		settings[id] = props
	}
	key := settings[firecrawlKeyRecord]
	if key == nil || key["kind"] != typeSecret {
		t.Fatalf("%s is not a core secret: %+v", firecrawlKeyRecord, key)
	}
	if key["required"] != true {
		t.Fatalf("%s is not required — a bundle that cannot run without a key says so", firecrawlKeyRecord)
	}
	if key["value"] != nil {
		t.Fatalf("%s ships a value — a shipped credential is empty for the user to fill", firecrawlKeyRecord)
	}
	base := settings[firecrawlBaseRecord]
	if base == nil || base["kind"] != typeSetting {
		t.Fatalf("%s is not a core setting: %+v", firecrawlBaseRecord, base)
	}
	if base["type"] != "url" || base["value"] != "https://api.firecrawl.dev" {
		t.Fatalf("%s ships type=%v value=%v, want a url pinned at the Firecrawl origin", firecrawlBaseRecord, base["type"], base["value"])
	}

	// The webdocument type carries the scrape's durable shape, every mirror
	// property labeled (fleet review F7). `title` stays the reserved
	// built-in — never a declared property.
	doc := mustKind(t, reg, firecrawlDocType)
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
	// description and a declared input shape.
	for _, name := range []string{firecrawlSearchFn, firecrawlScrapeFn} {
		fn, err := reg.ResolveFunction(name)
		if err != nil {
			t.Fatalf("function %s did not register: %v", name, err)
		}
		if fn.Description == "" || fn.Input == nil || fn.Output == nil {
			t.Fatalf("%s is not a full tool card: description=%q input=%v output=%v",
				name, fn.Description, fn.Input != nil, fn.Output != nil)
		}
	}
	// Emit is where the two DIFFER, and the closure ships un-faked: scrapepage
	// writes the document, websearch writes nothing and declares nothing. The
	// fake `emit` it used to carry — with a comment apologizing for it — existed
	// only because the loader demanded a non-empty allowlist.
	scrape, err := reg.ResolveFunction(firecrawlScrapeFn)
	if err != nil {
		t.Fatal(err)
	}
	if len(scrape.Caps.Emit) != 1 || scrape.Caps.Emit[0] != firecrawlDocType {
		t.Fatalf("%s emit = %v, want [%s]", firecrawlScrapeFn, scrape.Caps.Emit, firecrawlDocType)
	}
	search, err := reg.ResolveFunction(firecrawlSearchFn)
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Caps.Emit) != 0 {
		t.Fatalf("%s emit = %v — a pure function declares none", firecrawlSearchFn, search.Caps.Emit)
	}
}

// TestFirecrawlBundleCallsTools installs the zero-trigger closure into a live
// repository and drives both tools in call mode against a fake Firecrawl server.
// The bodies are dependency-free python (no PEP 723), so the shared python3
// host is the only runtime requirement.
func TestFirecrawlBundleCallsTools(t *testing.T) {
	t.Parallel()
	requirePython(t)
	ctx := context.Background()
	_, ds := newDataset(t)

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
	vocabularyDocs := loadDocs(t, firecrawlDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		t.Fatalf("install the firecrawl bundle: %v", err)
	}
	assertMembers(t, ds, map[string]string{
		firecrawlPackage:  typeBundle,
		firecrawlDocType:  typeKind,
		firecrawlSearchFn: typeFunction,
		firecrawlScrapeFn: typeFunction,
	})
	// The two configuration records ride in as the ordinary data they are.
	for _, d := range loadDocs(t, firecrawlDir+"/settings.yaml") {
		putDataDoc(t, ds, d)
	}
	st, err := ds.BundleStatus(ctx, firecrawlPackage)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("the zero-trigger bundle is not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if len(st.Inputs) != 0 {
		t.Fatalf("status inputs = %+v — the bundle declares none", st.Inputs)
	}
	// The shipped key is required and empty, so it is the bundle's one setup
	// item; the pre-filled baseUrl is not required and contributes none.
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupSetting ||
		st.Setup[0].Record != firecrawlKeyRecord || st.Setup[0].Kind != typeSecret {
		t.Fatalf("status setup = %+v, want the one empty-secret item for %s", st.Setup, firecrawlKeyRecord)
	}
	if !strings.Contains(st.Setup[0].Message, "API key") {
		t.Fatalf("the setup message %q does not name the secret's displayName", st.Setup[0].Message)
	}
	if st.Functions != 2 {
		t.Fatalf("status functions = %d, want 2", st.Functions)
	}

	// Configure — first with a HOSTILE baseUrl: an owner-editable base must
	// never redirect the bearer key (fleet review F1 / codex H1). Both
	// bodies refuse before building a request; nothing is written.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeSecret, firecrawlKeyRecord, substrate.PatchInput{
		Properties: map[string]any{"value": firecrawlTestKey},
	}); err != nil {
		t.Fatalf("fill in the firecrawl apiKey: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeSetting, firecrawlBaseRecord, substrate.PatchInput{
		Properties: map[string]any{"value": "https://evil.example.com"},
	}); err != nil {
		t.Fatalf("re-point the firecrawl baseUrl: %v", err)
	}
	if st, err = ds.BundleStatus(ctx, firecrawlPackage); err != nil {
		t.Fatalf("bundle status after the key landed: %v", err)
	}
	if len(st.Setup) != 0 {
		t.Fatalf("status setup = %+v, want empty once the required secret is filled", st.Setup)
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
	if n := countLive(t, ds, firecrawlDocType); n != 0 {
		t.Fatalf("a refused call minted %d webdocuments", n)
	}

	// Re-point the base at the fake server: loopback is the blessed test
	// seam (any scheme, any port), so no body ever dials real Firecrawl.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeSetting, firecrawlBaseRecord, substrate.PatchInput{
		Properties: map[string]any{"value": srv.URL},
	}); err != nil {
		t.Fatalf("re-point the firecrawl baseUrl at the fake: %v", err)
	}

	// scrapepage validates its url INPUT before hashing or dialing: only an
	// absolute https URL with a hostname and no embedded credentials passes.
	for url, wantErr := range map[string]string{
		"blog.example.com/blog/how":                   "https",
		"http://blog.example.com/how":                 "https",
		"https://":                                    "hostname",
		"https://user:pass@blog.example.com/blog/how": "credentials",
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
	if n := countLive(t, ds, firecrawlDocType); n != 0 {
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
	if n := countLive(t, ds, firecrawlDocType); n != 0 {
		t.Fatalf("websearch minted %d webdocuments", n)
	}

	// scrapepage: the content caps at 24000, ONE webdocument lands under
	// host.ids.url(url), and the built-in title rides the put.
	docID := runner.URLID(firecrawlPageURL)
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
	if n := countLive(t, ds, firecrawlDocType); n != 1 {
		t.Fatalf("re-scrape left %d webdocuments, want the one", n)
	}
}

// TestFirecrawlBundleImportsSettings takes the SAMPLE door: the catalog import
// rehomes the whole closure onto the repository's own authority, and the two
// configuration records go with it — ids and all, because ownership IS the id
// prefix (decision record 0076). The empty required key is the bundle's one
// setup item, and filling it clears it.
func TestFirecrawlBundleImportsSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	cat, err := catalog.Load(catalog.SampleRoot(os.DirFS(samplesDir)))
	if err != nil {
		t.Fatalf("load the sample catalog: %v", err)
	}
	if _, _, err := cat.Import(ctx, substrate.ActorAPI, firecrawlPackage, ds); err != nil {
		t.Fatalf("import the firecrawl sample: %v", err)
	}

	// The rehomed ids: the placeholder authority is gone, and the bundle's
	// own id is what the records sit under.
	home := testdb.Repository(t)
	keyID := home + "/firecrawl/apiKey"
	baseID := home + "/firecrawl/baseUrl"
	key, err := ds.Get(ctx, typeSecret, keyID)
	if err != nil {
		t.Fatalf("the imported closure did not land %s: %v", keyID, err)
	}
	if key.Properties["required"] != true || key.Properties["displayName"] != "API key" {
		t.Fatalf("%s landed as %+v, want the required, named key", keyID, key.Properties)
	}
	if v, held := key.Properties["value"]; held && v != "" {
		t.Fatalf("%s landed carrying a value %v — a shipped credential is empty", keyID, v)
	}
	base, err := ds.Get(ctx, typeSetting, baseID)
	if err != nil {
		t.Fatalf("the imported closure did not land %s: %v", baseID, err)
	}
	if base.Properties["value"] != "https://api.firecrawl.dev" || base.Properties["type"] != "url" {
		t.Fatalf("%s landed as %+v, want the pinned url", baseID, base.Properties)
	}

	// The bundle's status: one setup item, for the empty required secret.
	st, err := ds.BundleStatus(ctx, home+"/firecrawl")
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupSetting || st.Setup[0].Record != keyID {
		t.Fatalf("status setup = %+v, want the one empty-secret item for %s", st.Setup, keyID)
	}

	// Filling it clears the item, and nothing reads the secret back.
	filled, err := ds.Patch(ctx, substrate.ActorAPI, typeSecret, keyID, substrate.PatchInput{
		Properties: map[string]any{"value": firecrawlTestKey},
	})
	if err != nil {
		t.Fatalf("fill in the imported apiKey: %v", err)
	}
	if filled.Properties["value"] == firecrawlTestKey {
		t.Fatalf("the secret read back in plaintext — a secret-typed property never does")
	}
	if st, err = ds.BundleStatus(ctx, home+"/firecrawl"); err != nil {
		t.Fatalf("bundle status after the key landed: %v", err)
	}
	if len(st.Setup) != 0 {
		t.Fatalf("status setup = %+v, want empty once the required secret is filled", st.Setup)
	}
}
