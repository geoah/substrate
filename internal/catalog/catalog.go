// Package catalog is the substrate's read model over the bundle closures
// SHIPPED in the binary — the registry a repository imports from. A catalog
// entry is one embedded bundle directory (kinds/<authority>/): its identity, the
// schema CLOSURE (the core schema documents — the atomic install unit) and the
// DATA RECORDS it ships beside them (an extension's triggers, the llm example's
// provider rows).
//
// TWO KINDS OF ENTRY, one install path. A BUNDLE owns any authority that is not
// a bare org-domain label (`google.bundles.substrate.reamde.dev`), configures
// through its declared inputs and may ship callables. A VOCABULARY bundle owns a bare authority
// (`people.substrate.reamde.dev`, `tasks.substrate.reamde.dev`) and ships kinds and nothing else —
// it is the substrate's own vocabulary, which repository creation no longer
// seeds: a fresh repository holds core alone and imports the rest from here.
// A closure that declares ONTO another authority names it in `requires:`, and
// admission refuses the install while that authority is absent (schema
// resolveBundle), naming what to import first.
//
// Install is a THIN wrapper over the existing admission path, not a parallel
// one: the closure rides ApplyVocabularyDocuments (one transaction, every document
// admitted or none, refuse-breakage on commit) exactly as an explicit
// `schema/apply` does, and the delivery wiring is PUT afterward the same way
// `substratectl apply` routes a non-schema document. Re-install is the bundle's
// own upgrade/refuse-breakage semantics — the whole-authority re-apply — so it is
// idempotent by construction.
//
// The catalog is the set of closures baked in today; remote-URL / versioned
// install is ticket 011, out of scope.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Bundle is one installable catalog entry: the shipped closure and the
// metadata the console previews before installing it.
type Bundle struct {
	// ID is the bundle's record id — its owned authority carried as
	// "<authority>/<name>", matching substrate.BundleStatus.ID once installed.
	ID string `json:"id"`
	// Name is the categorized authority's first label ("web" of
	// "web.bundles.substrate.reamde.dev").
	Name string `json:"name"`
	// Authority is the owned authority the closure installs.
	Authority string `json:"authority"`
	// Description is the bundle document's description.
	Description string `json:"description"`
	// Version is the bundle authority's authority version (the closure's
	// version; a per-bundle semver is future — ticket 011). Zero means the
	// closure declares none.
	Version int64 `json:"version"`
	// Inputs are the bundle's declared configuration needs, verbatim from
	// the manifest: input name → {kind, inject?, description?}. A bundle
	// with no needs carries none, and the console previews nothing.
	Inputs map[string]any `json:"inputs,omitempty"`
	// Requires names the AUTHORITIES this bundle's closure declares against —
	// the vocabulary its mappings, edges and trigger subscriptions point at.
	// Vocabulary is IMPORTED now rather than seeded (repository creation seeds
	// core alone), so the console shows this before an install and admission
	// refuses the install when one is absent, naming what to import first.
	Requires []string `json:"requires,omitempty"`
	// Vocabulary marks a pure-vocabulary bundle: a bare authority the
	// substrate publishes, shipping kinds and nothing else. It is what the
	// creation seed used to write.
	Vocabulary bool `json:"vocabulary"`
	// Example is the curated catalog facet marking a bundle that exists to be
	// READ and run once: a worked demonstration of a substrate mechanism,
	// self-contained and safe to install on a fresh repository. It is
	// presentation metadata keyed by bundle id (exampleFacets), like
	// Integration, and for the same reason — nothing about a closure's shape
	// says "this is here to teach you something".
	Example bool `json:"example"`
	// Integration is the curated catalog facet marking a bundle whose
	// purpose is an ongoing connection to an external provider (the console's
	// Integration badge and filter). It is catalog PRESENTATION metadata keyed
	// by bundle id (integrationFacets), NOT part of the stored closure and NOT
	// derived from OAuth blocks, account types, or authority/name shape: a
	// token/webhook integration may declare no OAuth, and an account-shaped
	// bundle is not necessarily a provider integration.
	Integration bool `json:"integration"`
	// Closure enumerates what installing lands, for the detail preview.
	Closure Closure `json:"closure"`

	// vocabularyDocs is the closure — the core schema documents (bundle.yaml),
	// applied atomically through ApplyVocabularyDocuments.
	vocabularyDocs []map[string]any
	// dataDocs is the data plane: ordinary records (an extension's triggers,
	// the llm example's provider rows), each PUT after the closure lands.
	dataDocs []map[string]any
}

// Closure is what installing a bundle lands, by kind — the detail preview the
// console shows before installing. EVERY member of it is a record: a kind, a
// function and an agent are records of the core meta-kinds, and Records are the
// ordinary data rows the same transaction writes beside them. The lists are
// split because a reader asks different questions of each, not because the
// things differ in nature.
type Closure struct {
	Kinds []string `json:"kinds"`
	// KindDescriptions is each kind's declared description, keyed by identity
	// — what the closure's kinds ARE, before an install has put them in the
	// registry a reader could look them up in. Omitted where a kind declares
	// none, so the map is only as big as the prose.
	KindDescriptions map[string]string `json:"kindDescriptions,omitempty"`
	Functions        []string          `json:"functions"`
	Agents           []string          `json:"agents"`
	// Mappings answers "what will this project onto the vocabulary I already
	// have" — the question a reader asks before importing an integration.
	Mappings []string `json:"mappings"`
	// Records are the DATA records the install writes after the declarations
	// land: an extension's triggers, the llm example's two keyless provider
	// rows. They are ordinary records the moment they exist — editable,
	// deletable — and half of what a bundle DOES arrives this way, so a preview
	// that named only the declarations would hide the very row the reader is
	// about to be told to go and key.
	Records []ShippedRecord `json:"records"`
}

// ShippedRecord is one data record a bundle ships. A data record's identity is
// its KIND and its id together (a declaration's is one reference), so both
// travel, and the console addresses the record from them.
type ShippedRecord struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// integrationFacets curates the `integration` catalog facet per bundle id.
// This is catalog-owned PRESENTATION metadata (the console's Integration badge
// and filter), not part of the stored bundle closure and deliberately NOT
// derived from OAuth blocks, account types, or authority/name shape — the naming
// decision keeps this an explicit, curated map so a token/webhook integration
// with no OAuth still classifies and an account-shaped non-provider bundle
// does not. A bundle absent from the map is not an integration. A future
// remote-registry format can carry the same facet once its compatibility rules
// exist.
// exampleFacets curates the `example` facet — the bundles the console groups
// under Examples. They are ordinary bundles: installable, uninstallable, and
// no different at runtime from any other. The facet only says what they are
// FOR — and it marks the one part of the shipped set OUTSIDE the stable
// vocabulary (decision record 0015): an example's declarations may change or
// leave without an upgrade path.
var exampleFacets = map[string]bool{
	// The two provider rows a substrate needs before an agent can run, plus an
	// agent chain to prove them.
	"llm.examples.substrate.reamde.dev/llm": true,
	// The smallest bundle showing an agent calling functions as tools and
	// delegating to a sub-agent.
	"notes.bundles.substrate.reamde.dev/notes": true,
	// The URL harvester: triggers, a sub-agent chain, and a change request an
	// owner accepts. A bigger read than the other two.
	"web.bundles.substrate.reamde.dev/web": true,
}

var integrationFacets = map[string]bool{
	"google.bundles.substrate.reamde.dev/google": true,
	"github.bundles.substrate.reamde.dev/github": true,
	"linear.bundles.substrate.reamde.dev/linear": true,
	"notion.bundles.substrate.reamde.dev/notion": true,
	"whoop.bundles.substrate.reamde.dev/whoop":   true,
	"beeper.bundles.substrate.reamde.dev/beeper": true,
	// firecrawl is a capability bundle (callable web-search/scrape tools
	// behind an API key) — no provider account is connected and nothing syncs
	// from the provider, so like the web harvester it is not an integration.
	"firecrawl.bundles.substrate.reamde.dev/firecrawl": false,
	"web.bundles.substrate.reamde.dev/web":             false,
}

// Catalog is the parsed set of shipped bundles, indexed by id.
type Catalog struct {
	bundles []*Bundle
	byID    map[string]*Bundle
	// warnings names the shipped directories that failed to parse — a
	// half-built or malformed example never bricks the catalog, but the
	// operator changelog should say it was dropped.
	warnings []string
}

// Load parses every shipped bundle directory in fsys (each a top-level
// directory of manifests). A directory that carries no bundle document is not
// a bundle and is skipped; a directory that fails to parse is dropped with a
// recorded warning rather than failing the whole catalog — a broken neighbor
// never bricks the shipped set.
func Load(fsys fs.FS) (*Catalog, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("catalog: read root: %w", err)
	}
	c := &Catalog{byID: map[string]*Bundle{}}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := loadBundle(fsys, e.Name())
		if err != nil {
			c.warnings = append(c.warnings, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		if b == nil {
			continue // not a bundle directory
		}
		if _, dup := c.byID[b.ID]; dup {
			c.warnings = append(c.warnings, fmt.Sprintf("%s: duplicate bundle id %s", e.Name(), b.ID))
			continue
		}
		// The integration facet is curated catalog presentation metadata,
		// applied by id AFTER the closure parses — it is not read from the
		// stored bundle document (a bundle absent from the map is not an
		// integration).
		b.Integration = integrationFacets[b.ID]
		b.Example = exampleFacets[b.ID]
		c.bundles = append(c.bundles, b)
		c.byID[b.ID] = b
	}
	sort.Slice(c.bundles, func(i, j int) bool { return c.bundles[i].ID < c.bundles[j].ID })
	return c, nil
}

// loadBundle reads one directory's manifests into a Bundle, or nil when the
// directory holds no bundle document.
func loadBundle(fsys fs.FS, dir string) (*Bundle, error) {
	files, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var docs []map[string]any
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, f.Name()))
		if err != nil {
			return nil, err
		}
		fileDocs, err := decodeDocs(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name(), err)
		}
		docs = append(docs, fileDocs...)
	}
	return bundleFromDocs(docs)
}

// decodeDocs splits a `---`-separated manifest into raw envelope maps,
// skipping comment-only documents.
func decodeDocs(raw []byte) ([]map[string]any, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	var out []map[string]any
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(m) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// bundleFromDocs assembles a Bundle from a directory's documents, splitting the
// two planes the loader has always split: core schema documents ride the batch
// apply verb; everything else is a data record PUT afterward. Returns nil when
// the directory carries no bundle document.
func bundleFromDocs(docs []map[string]any) (*Bundle, error) {
	b := &Bundle{}
	var version int64
	found := false
	for _, d := range docs {
		authority, typ := vocabulary.SplitKindRef(mstr(d, "kind"))
		id := docID(d)
		switch {
		case isSchemaDoc(authority, typ):
			b.vocabularyDocs = append(b.vocabularyDocs, d)
		default:
			// EVERY data document, not the trigger alone: whatever the install
			// PUTs is what the reader will find in their repository afterward.
			b.dataDocs = append(b.dataDocs, d)
			b.Closure.Records = append(b.Closure.Records, ShippedRecord{
				Kind: mstr(d, "kind"), ID: id,
			})
		}
		data := mmap(d, "data")
		switch typ {
		case vocabulary.DocBundle:
			found = true
			b.ID = id
			b.Authority = mstr(data, "authority")
			b.Description = mstr(data, "description")
			b.Inputs = mmap(data, "inputs")
			for _, rv := range mslice(data, "requires") {
				b.Requires = append(b.Requires, fmt.Sprint(rv))
			}
			b.Vocabulary = vocabulary.ValidVocabularyAuthority(b.Authority)
		case vocabulary.DocAuthority:
			if v := mversion(data, "version"); v > 0 {
				version = v
			}
		case vocabulary.DocKind:
			b.Closure.Kinds = append(b.Closure.Kinds, id)
			if desc := mstr(data, "description"); desc != "" {
				if b.Closure.KindDescriptions == nil {
					b.Closure.KindDescriptions = map[string]string{}
				}
				b.Closure.KindDescriptions[id] = desc
			}
		case vocabulary.DocFunction:
			b.Closure.Functions = append(b.Closure.Functions, id)
		case vocabulary.DocAgent:
			b.Closure.Agents = append(b.Closure.Agents, id)
		case vocabulary.DocRecordMapping:
			b.Closure.Mappings = append(b.Closure.Mappings, id)
		}
	}
	if !found {
		return nil, nil
	}
	if b.Authority == "" {
		return nil, errors.New("bundle document names no authority")
	}
	b.Name = vocabulary.KindName(b.ID)
	b.Version = version
	return b, nil
}

// isSchemaDoc mirrors the loader's split (and substratectl's): a schema document IS a
// record of one of the core meta-kinds; everything else is a data record.
func isSchemaDoc(authority, name string) bool {
	return authority == vocabulary.AuthorityCore && vocabulary.VocabularyDocumentKind(name)
}

// Bundles lists the shipped bundles, id-ordered.
func (c *Catalog) Bundles() []*Bundle { return c.bundles }

// ByID returns one shipped bundle.
func (c *Catalog) ByID(id string) (*Bundle, bool) {
	b, ok := c.byID[id]
	return b, ok
}

// Warnings names the shipped directories dropped during Load.
func (c *Catalog) Warnings() []string { return c.warnings }

// Install applies a shipped bundle's closure into ds's repository as ONE atomic
// admission: EVERY shipped data document is converted and validated BEFORE the
// schema apply, then the schema closure and the delivery wiring commit as a
// single repository transaction (InstallBundleClosure), so a malformed or
// unadmittable trigger rolls the whole install back instead of leaving a live
// schema half-install a retry would silently mutate. It is
// owner-gated — installing arbitrary bundle code is a human-owner action — and
// idempotent: the closure re-apply is the bundle's own upgrade semantics and
// the delivery wiring upserts in place.
func (c *Catalog) Install(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset) (*Bundle, error) {
	if !substrate.HumanActors[actor] {
		return nil, fmt.Errorf("%w: installing a bundle is the repository user's action, not a machine's", substrate.ErrForbidden)
	}
	b, ok := c.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	inst, ok := ds.(substrate.BundleInstaller)
	if !ok {
		return nil, errors.New("catalog: this dataset cannot install bundle closures")
	}
	// Pre-admit every data document BEFORE the schema apply: a malformed
	// delivery-wiring envelope fails here, before anything is touched, rather
	// than after the schema closure has already committed.
	dataInputs := make([]substrate.PutInput, 0, len(b.dataDocs))
	for _, d := range b.dataDocs {
		in, err := dataPutInput(d)
		if err != nil {
			return nil, err
		}
		dataInputs = append(dataInputs, in)
	}
	// INSTALL IS A COPY: the catalog's manifests are written
	// into the repository's own changelog under the BUNDLE's actor, not the
	// requester's. Installing is an owner action — that is the check above —
	// but what lands in the changelog is the bundle's declarations, attributed to
	// the bundle, exactly as the core tree's seed is attributed to
	// `bundle:core`. The catalog is the source; the changelog is the truth, and
	// nothing on the serving path reads the catalog again.
	if _, err := inst.InstallBundleClosure(ctx, substrate.BundleActor(b.Name), b.vocabularyDocs, dataInputs); err != nil {
		return nil, err
	}
	return b, nil
}

// Upgrade previews what re-installing a shipped bundle over ds's stored
// declarations would do: the version motion and the blockers, computed by the
// dataset against the same closure Install applies. A dataset that offers no
// preview answers nil, not an error: the catalog still lists and installs
// there, it just cannot say what an install would change.
func (c *Catalog) Upgrade(ctx context.Context, id string, ds substrate.Dataset) (*substrate.BundleUpgrade, error) {
	b, ok := c.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	p, ok := ds.(substrate.BundleUpgradePlanner)
	if !ok {
		return nil, nil
	}
	plan, err := p.PlanBundleUpgrade(ctx, b.vocabularyDocs)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// dataPutInput turns one data-record manifest (a trigger) into a PutInput the
// ordinary write path accepts — the envelope's authority settles the type
// identity, its properties travel as authored. The shipped delivery wiring is
// property-only (triggers), which is all this carries.
func dataPutInput(d map[string]any) (substrate.PutInput, error) {
	kind := mstr(d, "kind")
	if kind == "" {
		return substrate.PutInput{}, fmt.Errorf("data document missing kind")
	}
	data := mmap(d, "data")
	return substrate.PutInput{
		Kind:       kind,
		ID:         docID(d),
		Properties: mmap(data, "properties"),
		Labels:     mmap(d["metadata"], "labels"),
	}, nil
}

// docID reads metadata.id.
func docID(d map[string]any) string { return mstr(d["metadata"], "id") }

func mstr(m any, key string) string {
	mm, ok := m.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := mm[key].(string)
	return s
}

// mversion reads a declaration version: an integer (0 when absent or not
// one), through the one reader (vocabulary.VersionValue) so a closure and
// the engine cannot disagree about what a version is.
func mversion(m any, key string) int64 {
	mm, ok := m.(map[string]any)
	if !ok {
		return 0
	}
	v, _ := vocabulary.VersionValue(mm[key])
	return v
}

func mslice(m any, key string) []any {
	mm, ok := m.(map[string]any)
	if !ok {
		return nil
	}
	out, _ := mm[key].([]any)
	return out
}

func mmap(m any, key string) map[string]any {
	mm, ok := m.(map[string]any)
	if !ok {
		return nil
	}
	out, _ := mm[key].(map[string]any)
	return out
}
