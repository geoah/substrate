package providertest

// The Firecrawl provider: web search and page scraping as tools an agent
// calls, with no account to connect. The proofs, from the shipped closure at
// ../../kinds/providers.substrate.reamde.dev/firecrawl:
//
//  1. TestFirecrawlProviderAdmitsSchema: the closure admits through the
//     schema loader. The bundle declares no input (one bearer key carried by
//     the core `secret` record it ships, decision record 0076), both
//     functions register as callables that offer themselves to agents under
//     the names the closure declares (record 0198), and webdocument titles
//     itself from `pageTitle`. No database, no network.
//
//  2. TestFirecrawlProviderInstallsSettings: the provider door. The catalog
//     install lands the two configuration records under the published
//     package's own id, the empty required key is the bundle's one setup
//     item, and filling it clears it. The sample door serves no Firecrawl.
//
//  3. TestFirecrawlProviderCallsTools: both functions run in call mode
//     against a FAKE Firecrawl (the baseUrl setting points at it on
//     loopback; real Firecrawl is never dialed). websearch answers hits and
//     writes nothing; webscrape behaves as the sample's scrapepage did: it
//     caps the markdown at 24000 characters, writes ONE webdocument keyed
//     host.ids.minted(url), and a re-scrape updates it in place.
//
//  4. TestFirecrawlProviderRefusesRedirects: a 3xx from the API is refused,
//     never followed, so the key never reaches the redirect's target.
//
// What the engine hands the bodies on the operator's key is
// internal/engine's (operatorfunction_db_test.go); where the bodies send it
// with no baseUrl beside it is TestBodyHelpersFirecrawl's.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

const (
	firecrawlDir      = providersDir + "/firecrawl"
	firecrawlPackage  = providersAuthority + "/firecrawl"
	firecrawlDocKind  = firecrawlPackage + "/webdocument"
	firecrawlSearchFn = firecrawlPackage + "/websearch"
	firecrawlScrapeFn = firecrawlPackage + "/webscrape"

	// The bundle's configuration as records: the ids ARE the ownership, so
	// they sit under the bundle's own id (decision record 0076).
	firecrawlKeyRecord  = firecrawlPackage + "/apiKey"
	firecrawlBaseRecord = firecrawlPackage + "/baseUrl"

	typeSetting = "substrate.reamde.dev/core/setting"
	typeSecret  = "substrate.reamde.dev/core/secret"

	firecrawlTestKey = "fc-unit-test-key"
	firecrawlPageURL = "https://blog.example.com/how-substrates-compose"
)

// TestFirecrawlProviderAdmitsSchema loads the builtin schema, then installs
// the closure on top of it through the ordinary loader/resolver: the same
// admission the batch apply runs, minus the body warm.
func TestFirecrawlProviderAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, firecrawlDir)

	b, ok := reg.BundleOf(firecrawlPackage)
	if !ok {
		t.Fatalf("the firecrawl bundle did not register")
	}
	if len(b.Inputs) != 0 {
		t.Fatalf("bundle declares inputs %v; its configuration is core setting/secret records", b.InputOrder)
	}
	if b.OAuth2 != nil {
		t.Fatalf("bundle carries an oauth2 block; a bearer-key bundle declares no OAuth client")
	}
	if _, ok := b.Modules["firecrawl_api.py"]; !ok {
		t.Fatalf("bundle ships modules %v, want firecrawl_api.py, the one place the key is sent from", b.Modules)
	}

	// The shipped configuration records: a required, empty `secret` for the
	// key and a url-typed `setting` pre-filled with the pinned origin, both
	// under the bundle's own id.
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
	if key == nil || key["kind"] != typeSecret || key["required"] != true || key["value"] != nil {
		t.Fatalf("%s = %+v, want a required core secret shipped empty", firecrawlKeyRecord, key)
	}
	base := settings[firecrawlBaseRecord]
	if base == nil || base["kind"] != typeSetting || base["type"] != "url" || base["value"] != "https://api.firecrawl.dev" {
		t.Fatalf("%s = %+v, want a url setting pinned at the Firecrawl origin", firecrawlBaseRecord, base)
	}

	// webdocument titles itself from its own property (record 0016), and
	// every property is labeled.
	doc := mustKind(t, reg, firecrawlDocKind)
	for _, name := range []string{"url", "pageTitle", "content", "truncated", "fetchedAt", "raw"} {
		p, ok := doc.Prop(name)
		if !ok || p.DisplayName == "" {
			t.Fatalf("webdocument property %q is missing or unlabeled", name)
		}
	}
	if doc.DisplayTemplate != "{pageTitle|url}" {
		t.Fatalf("webdocument displayTemplate = %q", doc.DisplayTemplate)
	}
	// The page is markdown, which the record page reads as the document's
	// body rather than a clamped row.
	if p, ok := doc.Prop("content"); !ok || p.Datatype != vocabulary.DatatypeMarkdown {
		t.Fatalf("webdocument content = %+v, want markdown", p)
	}

	// Both callables are full tool cards, offered to agents under the names
	// the closure declares, each with its effect.
	for name, want := range map[string]struct{ tool, effect string }{
		firecrawlSearchFn: {"web_search", vocabulary.FunctionEffectRead},
		firecrawlScrapeFn: {"web_scrape", vocabulary.FunctionEffectWrite},
	} {
		fn, err := reg.ResolveFunction(name)
		if err != nil {
			t.Fatalf("function %s did not register: %v", name, err)
		}
		if fn.Description == "" || fn.Input == nil || fn.Output == nil {
			t.Fatalf("%s is not a full tool card", name)
		}
		if fn.Offer == nil || fn.Offer.ToolName != want.tool || fn.Effect != want.effect {
			t.Fatalf("%s offer = %+v effect = %q, want %s and %s", name, fn.Offer, fn.Effect, want.tool, want.effect)
		}
		if strings.Join(fn.Caps.Network, ",") != "api.firecrawl.dev" {
			t.Fatalf("%s network = %v", name, fn.Caps.Network)
		}
	}
	scrape, _ := reg.ResolveFunction(firecrawlScrapeFn)
	mustEmit(t, scrape, firecrawlDocKind)
	search, _ := reg.ResolveFunction(firecrawlSearchFn)
	if len(search.Caps.Emit) != 0 {
		t.Fatalf("%s emit = %v; a read tool declares none", firecrawlSearchFn, search.Caps.Emit)
	}
}

// TestFirecrawlProviderInstallsSettings takes the provider door: the catalog
// install lands the closure published, and its two configuration records
// under the package's own id. The sample door serves no Firecrawl any more.
func TestFirecrawlProviderInstallsSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	sampleCat, err := catalog.Load(catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the sample catalog: %v", err)
	}
	if _, served := sampleCat.ByID("samples.substrate.reamde.dev/firecrawl"); served {
		t.Fatal("the sample catalog still serves firecrawl")
	}

	installShipped(t, ds, firecrawlPackage)
	pkg, err := ds.Get(ctx, "substrate.reamde.dev/core/package", firecrawlPackage)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Properties["source"] != vocabulary.SourcePublished {
		t.Fatalf("package source = %v, want published", pkg.Properties["source"])
	}
	key, err := ds.Get(ctx, typeSecret, firecrawlKeyRecord)
	if err != nil {
		t.Fatalf("the install did not land %s: %v", firecrawlKeyRecord, err)
	}
	if key.Properties["required"] != true || key.Properties["displayName"] != "API key" {
		t.Fatalf("%s landed as %+v", firecrawlKeyRecord, key.Properties)
	}
	if v, held := key.Properties["value"]; held && v != "" {
		t.Fatalf("%s landed carrying a value", firecrawlKeyRecord)
	}
	base, err := ds.Get(ctx, typeSetting, firecrawlBaseRecord)
	if err != nil {
		t.Fatalf("the install did not land %s: %v", firecrawlBaseRecord, err)
	}
	if base.Properties["value"] != "https://api.firecrawl.dev" || base.Properties["type"] != "url" {
		t.Fatalf("%s landed as %+v", firecrawlBaseRecord, base.Properties)
	}

	st, err := ds.BundleStatus(ctx, firecrawlPackage)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || !st.Enabled || st.Functions != 2 || len(st.Inputs) != 0 {
		t.Fatalf("status = %+v", st)
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupSetting || st.Setup[0].Record != firecrawlKeyRecord ||
		!strings.Contains(st.Setup[0].Message, "API key") {
		t.Fatalf("status setup = %+v, want the one empty-secret item for %s", st.Setup, firecrawlKeyRecord)
	}

	filled, err := ds.Patch(ctx, substrate.ActorPerson, typeSecret, firecrawlKeyRecord, substrate.PatchInput{
		Properties: map[string]any{"value": firecrawlTestKey},
	})
	if err != nil {
		t.Fatalf("fill in the apiKey: %v", err)
	}
	if filled.Properties["value"] == firecrawlTestKey {
		t.Fatalf("the secret read back in plaintext")
	}
	if st, err = ds.BundleStatus(ctx, firecrawlPackage); err != nil || len(st.Setup) != 0 {
		t.Fatalf("status setup = %+v, %v, want empty once the key is filled", st.Setup, err)
	}
}

// newFirecrawlFake is a bearer-checked Firecrawl: /v2/search answers two hits,
// and /v2/scrape answers markdown past the 24000-character cap the first time
// and a short page after, so a re-scrape provably rewrites the document.
func newFirecrawlFake(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
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
	t.Cleanup(srv.Close)
	return srv, &scrapes
}

// installFirecrawlWithKey installs the provider and fills in the person's own
// key, the base URL left as shipped. Every case that runs a Firecrawl body
// comes through here, so each one first proves the body cannot reach the real
// API whatever it is handed (requireConfinedBodies).
func installFirecrawlWithKey(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	requireConfinedBodies(t)
	installShipped(t, ds, firecrawlPackage)
	if _, err := ds.Patch(context.Background(), substrate.ActorPerson, typeSecret, firecrawlKeyRecord, substrate.PatchInput{
		Properties: map[string]any{"value": firecrawlTestKey},
	}); err != nil {
		t.Fatalf("fill in the firecrawl apiKey: %v", err)
	}
}

// requireConfinedBodies fails the test unless every body it starts is held to
// loopback (TestMain's runner.ConfineBodyEgress) on a host that enforces it:
// a Firecrawl body whose origin check or settings a regression broke would
// otherwise send its key to the real API.
func requireConfinedBodies(t *testing.T) {
	t.Helper()
	if err := runner.Shared.BodyEgressConfined(); err != nil {
		t.Fatalf("a Firecrawl body run by this test could reach the real API: %v", err)
	}
}

func setFirecrawlBase(t *testing.T, ds substrate.Dataset, value string) {
	t.Helper()
	if _, err := ds.Patch(context.Background(), substrate.ActorPerson, typeSetting, firecrawlBaseRecord, substrate.PatchInput{
		Properties: map[string]any{"value": value},
	}); err != nil {
		t.Fatalf("set the firecrawl baseUrl to %q: %v", value, err)
	}
}

// TestFirecrawlProviderCallsTools drives both tools in call mode against the
// fake. The bodies are dependency-free python, so the shared python3 host is
// the only runtime requirement.
func TestFirecrawlProviderCallsTools(t *testing.T) {
	t.Parallel()
	requirePython(t)
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	srv, scrapes := newFirecrawlFake(t)
	installFirecrawlWithKey(t, ds)

	// A base URL the person edited never moves the key: anything but the
	// pinned origin on 443, or loopback, refuses before a request is built.
	for _, hostile := range []string{
		"https://evil.example.com",
		"https://api.firecrawl.dev:8443",
		"http://api.firecrawl.dev",
		"https://api.firecrawl.dev.evil.example.com",
	} {
		setFirecrawlBase(t, ds, hostile)
		for fn, input := range map[string]map[string]any{
			firecrawlSearchFn: {"query": "anything"},
			firecrawlScrapeFn: {"url": firecrawlPageURL},
		} {
			_, applied, err := ds.CallFunction(ctx, substrate.ActorPerson, fn, input)
			if err == nil || !strings.Contains(err.Error(), "pinned provider origin") || applied != 0 {
				t.Fatalf("%s against baseUrl %s: err=%v applied=%d, want the origin-pin refusal", fn, hostile, err, applied)
			}
		}
	}
	setFirecrawlBase(t, ds, "https://api.firecrawl.dev/v2")
	if _, _, err := ds.CallFunction(ctx, substrate.ActorPerson, firecrawlSearchFn, map[string]any{"query": "x"}); err == nil || !strings.Contains(err.Error(), "carries a path") {
		t.Fatalf("websearch against a base with a path: %v, want the refusal", err)
	}
	if n := countLive(t, ds, firecrawlDocKind); n != 0 {
		t.Fatalf("a refused call minted %d webdocuments", n)
	}

	// Loopback is the test seam: the fake.
	setFirecrawlBase(t, ds, srv.URL)

	// webscrape validates its url INPUT before hashing or dialing.
	for url, wantErr := range map[string]string{
		"blog.example.com/blog/how":                   "https",
		"http://blog.example.com/how":                 "https",
		"https://":                                    "hostname",
		"https://user:pass@blog.example.com/blog/how": "credentials",
	} {
		_, applied, err := ds.CallFunction(ctx, substrate.ActorPerson, firecrawlScrapeFn, map[string]any{"url": url})
		if err == nil || !strings.Contains(err.Error(), wantErr) || applied != 0 {
			t.Fatalf("webscrape(%q): err=%v applied=%d, want a refusal mentioning %q", url, err, applied, wantErr)
		}
	}
	if n := scrapes.Load(); n != 0 {
		t.Fatalf("an invalid url reached the provider %d times", n)
	}

	// websearch: hits come back shaped, and the call writes nothing.
	out, applied, err := ds.CallFunction(ctx, substrate.ActorPerson, firecrawlSearchFn,
		map[string]any{"query": "how substrates compose", "limit": 2})
	if err != nil {
		t.Fatalf("call websearch: %v", err)
	}
	if applied != 0 {
		t.Fatalf("websearch applied %d effects", applied)
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

	// webscrape: the content caps at 24000, ONE webdocument lands under
	// host.ids.minted(url), titled by the page's own title.
	docID := vocabulary.DerivedMintedID(firecrawlPageURL)
	out, applied, err = ds.CallFunction(ctx, substrate.ActorPerson, firecrawlScrapeFn, map[string]any{"url": firecrawlPageURL})
	if err != nil {
		t.Fatalf("call webscrape: %v", err)
	}
	if applied != 1 {
		t.Fatalf("webscrape applied %d effects, want 1", applied)
	}
	om, _ := out.(map[string]any)
	content, _ := om["content"].(string)
	if om["document"] != docID || om["title"] != "How Substrates Compose" || len(content) != 24000 || om["truncated"] != true {
		t.Fatalf("webscrape output: document=%v title=%v len=%d truncated=%v", om["document"], om["title"], len(content), om["truncated"])
	}
	doc, err := ds.Get(ctx, firecrawlDocKind, docID)
	if err != nil {
		t.Fatalf("get the webdocument: %v", err)
	}
	if doc.Title != "How Substrates Compose" || doc.Properties["pageTitle"] != "How Substrates Compose" {
		t.Fatalf("webdocument title=%q pageTitle=%v", doc.Title, doc.Properties["pageTitle"])
	}
	if got, _ := doc.Properties["content"].(string); len(got) != 24000 {
		t.Fatalf("stored content len = %d, want 24000", len(got))
	}
	if doc.Properties["url"] != firecrawlPageURL || doc.Properties["truncated"] != true || doc.Properties["raw"] == nil {
		t.Fatalf("webdocument properties: %v", doc.Properties)
	}
	if s, _ := doc.Properties["fetchedAt"].(string); s == "" {
		t.Fatalf("webdocument carries no fetchedAt")
	}

	// Re-scrape: the SAME id updates in place.
	if _, applied, err = ds.CallFunction(ctx, substrate.ActorPerson, firecrawlScrapeFn, map[string]any{"url": firecrawlPageURL}); err != nil || applied != 1 {
		t.Fatalf("re-scrape: applied=%d err=%v", applied, err)
	}
	updated, err := ds.Get(ctx, firecrawlDocKind, docID)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := updated.Properties["content"].(string); !strings.Contains(got, "the second read") ||
		updated.Properties["truncated"] != false || updated.Version <= doc.Version {
		t.Fatalf("re-scrape: content=%q truncated=%v version %d -> %d", got, updated.Properties["truncated"], doc.Version, updated.Version)
	}
	if n := countLive(t, ds, firecrawlDocKind); n != 1 {
		t.Fatalf("re-scrape left %d webdocuments, want the one", n)
	}
}

// TestFirecrawlProviderRefusesRedirects answers every call with a redirect to
// a second loopback server. The bodies refuse it: the call fails naming the
// redirect, and the target never sees a request, so never the key.
func TestFirecrawlProviderRefusesRedirects(t *testing.T) {
	t.Parallel()
	requirePython(t)
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	installFirecrawlWithKey(t, ds)

	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
	}))
	t.Cleanup(target.Close)
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+r.URL.Path, code)
		}))
		setFirecrawlBase(t, ds, redirector.URL)
		for fn, input := range map[string]map[string]any{
			firecrawlSearchFn: {"query": "anything"},
			firecrawlScrapeFn: {"url": firecrawlPageURL},
		} {
			_, applied, err := ds.CallFunction(ctx, substrate.ActorPerson, fn, input)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "redirect") || applied != 0 {
				t.Fatalf("%s answered %d: err=%v applied=%d, want the redirect refused", fn, code, err, applied)
			}
		}
		redirector.Close()
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("the redirect's target received %d requests", n)
	}
}
