// Package catalog is the substrate's read model over the bundle closures
// SHIPPED in the binary — the registry a repository imports from. A catalog
// entry is one embedded PACKAGE directory: its identity, the schema CLOSURE
// (the core schema documents — the atomic install unit) and the DATA RECORDS
// it ships beside them (a provider's triggers, the llm sample's provider
// rows).
//
// Every entry says who KEEPS it, and that is the tree it came from (decision
// records 0048 and 0258). A PUBLISHED package (kinds/) is kept by its
// publisher and lands under the authority that publishes it. A SAMPLE
// (samples/) is kept by the repository: the closure is rehomed onto the
// repository's own authority first, so what lands is the repository's to edit.
// What an entry is FOR is a second answer, read from its declarations
// (decision record 0238): whether it is a provider, what it brings, and which
// provider an add-on extends.
//
// A closure that declares ONTO another package names it in `requires:`, and
// admission refuses the install while that package is absent (vocabulary
// resolveBundle), naming what to import first.
//
// Install is a THIN wrapper over the existing admission path, not a parallel
// one: the closure rides ApplyVocabularyDocuments (one transaction, every
// document admitted or none, refuse-breakage on commit) exactly as an explicit
// `/vocabulary/apply` does, and the delivery wiring is PUT afterward the same
// way `substratectl apply` routes a non-schema document. Re-install is the
// bundle's own upgrade/refuse-breakage semantics — the whole-package re-apply
// — so it is idempotent by construction.
//
// Document-published packages resolve through the authority reader; binary
// entries always take precedence over a document (decision 0212).
package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

// Bundle is one installable catalog entry: the shipped closure and the wire
// shape the console previews before taking it. The wire half is
// substrate.CatalogBundle, embedded so the wire golden can hold it
// (internal/substrate/wire_test.go); the closure documents stay here,
// unexported, and never marshal.
type Bundle struct {
	AddonResults []substrate.AddonInstallResult `json:"-"`
	substrate.CatalogBundle

	// vocabularyDocs is the closure — the core schema documents (bundle.yaml),
	// applied atomically through ApplyVocabularyDocuments.
	vocabularyDocs []map[string]any
	// dataDocs is the data plane: ordinary records (a provider's triggers,
	// the llm sample's provider rows), each PUT after the closure lands.
	dataDocs []map[string]any
	// suggested are the closure's SUGGESTED MAPPINGS as the documents spell
	// them (suggested.go). The wire field of the same name carries the state
	// each one has in ONE repository, so it is filled per request and is empty
	// here.
	suggested []vocabulary.SuggestedMapping
	// kinds, functions and triggers are what the closure brings, as an
	// approval reads it (facts.go).
	kinds     []KindFact
	functions []FunctionFact
	triggers  []TriggerFact
	agents    []AgentFact
	mappings  []MappingFact
	// reach is what all of that code reaches, folded (reach.go).
	reach []Reach
	// origin is where a document-published bundle came from, nil for one
	// the binary ships (published.go).
	origin *Origin
	// dialect is the vocabulary dialect the closure's package header says it
	// needs, 0 where it states none (decision 0268).
	dialect int64
	// needs is the dialect the keys of the closure's package need, 0 where
	// every one is read by every dialect.
	needs int64
}

// Dialect is the vocabulary dialect the closure's package header says it
// needs, 0 where it states none: what a publisher lists beside the version.
func (b *Bundle) Dialect() int64 { return b.dialect }

// NeedsDialect is the dialect the keys of the closure's package need,
// whatever its header states: the least a publisher may list beside it.
func (b *Bundle) NeedsDialect() int64 { return b.needs }

// Closure and ShippedRecord are the catalog's names for the two nested wire
// shapes, so a caller reads catalog.Closure rather than the substrate
// spelling.
type (
	Closure       = substrate.CatalogClosure
	ShippedRecord = substrate.CatalogShippedRecord
)

// LandedID is the bundle id this closure lands under. A provider keeps the id
// it publishes; a SAMPLE is copied into the repository's local namespace
// (decision records 0048 and 0259), so what lands is `~/<package>`, the same
// in every repository, and the shipped id addresses the catalog entry alone.
func (b *Bundle) LandedID() string {
	if b.KeptBy != substrate.KeptByRepository {
		return b.ID
	}
	return vocabulary.PackageRef(vocabulary.Local, b.Package)
}

// HeldID is the id this closure ALREADY has in a repository that holds it, or
// "" when it holds neither. Both are asked because both can be there: an
// import lands the rehomed id, and installing a sample verbatim (still a
// sanctioned door until the providers stop requiring sample packages) lands
// the shipped one. A read that asked only for the rehomed id reported a
// verbatim-installed sample as available, and offered it again.
func (b *Bundle) HeldID(held map[string]bool) string {
	if landed := b.LandedID(); held[landed] {
		return landed
	}
	if held[b.ID] {
		return b.ID
	}
	return ""
}

// Catalog is the parsed set of shipped bundles, indexed by id.
type Catalog struct {
	reader       *authority.Reader
	addonPlanner AddonPlanner
	bundles      []*Bundle
	byID         map[string]*Bundle
	// warnings names the shipped directories that failed to parse — a
	// half-built or malformed example never bricks the catalog, but the
	// operator changelog should say it was dropped.
	warnings []string
	// plans is the upgrade previews Upgrade reuses while the repository's
	// changelog head has not moved (plancache.go).
	plans planCache
	// seeded is the packages the binary seeds outside the catalog
	// (LoadWithReader), which no document resolves.
	seeded map[string]bool
}

// Load parses every shipped PACKAGE directory in the given roots. A package
// directory is any directory holding .yaml manifests, at whatever depth the
// root nests them: the shipped tree is kinds/<authority>/<package>/ and the
// samples are samples/<package>/, and both are read the same way. A directory
// that carries no bundle document is not a bundle and is skipped; one that
// fails to parse is dropped with a recorded warning rather than failing the
// whole catalog — a broken neighbor never bricks the shipped set.
//
// A root also says who KEEPS its closures (decision record 0048): that is the
// tree a closure came from and nothing else, so nothing infers it from an
// authority's spelling.
func Load(roots ...Root) (*Catalog, error) {
	c := &Catalog{byID: map[string]*Bundle{}}
	for _, root := range roots {
		dirs, err := packageDirs(root.FS)
		if err != nil {
			return nil, err
		}
		for _, dir := range dirs {
			if err := c.load(root, dir); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(c.bundles, func(i, j int) bool { return c.bundles[i].ID < c.bundles[j].ID })
	// After every root: an add-on may sort before the provider it extends.
	for _, b := range c.bundles {
		b.AddonFor = c.addonFor(b)
	}
	return c, nil
}

// addonFor is the provider b extends: the first package b requires that the
// binary ships as a provider, when b is published and brings no account of
// its own. A package with an account is a provider in its own right whatever
// it requires (WHOOP requires health), and a sample is the user's copy, which
// extends nothing it would be listed under.
func (c *Catalog) addonFor(b *Bundle) string {
	if b.KeptBy != substrate.KeptByPublisher || slices.Contains(b.Brings, substrate.BringsAccount) {
		return ""
	}
	for _, id := range b.Requires {
		if dep, ok := c.byID[id]; ok && dep.Provider {
			return id
		}
	}
	return ""
}

// documentAddonFor is the provider a package from an authority document
// extends: what its document names, when the binary ships that as a provider,
// else what addonFor reads off its requirements. A named package the console
// cannot list it under (a catalogue, a provider from another document) makes
// it no add-on at all, so it keeps the place its own role gives it; the
// approval still reads the document's word for 0216's wait.
func (c *Catalog) documentAddonFor(b *Bundle) string {
	named := ""
	if b.origin != nil {
		named = b.origin.AddonFor
	}
	if named == "" {
		return c.addonFor(b)
	}
	if dep, ok := c.byID[named]; ok && dep.Provider {
		return named
	}
	return ""
}

// LoadWithReader adds document-published packages to the binary catalog.
// seeded names the packages the binary seeds into every repository outside
// the catalog (core, llm, console, observations): with the catalog's own
// closures they are everything the binary ships, and a document never
// resolves one of them, so whoever can change a hosted file or a DNS answer
// cannot replace what the binary ships (decision 0221).
func LoadWithReader(reader *authority.Reader, seeded []string, roots ...Root) (*Catalog, error) {
	c, err := Load(roots...)
	if err != nil {
		return nil, err
	}
	c.reader = reader
	c.seeded = map[string]bool{}
	for _, id := range seeded {
		c.seeded[id] = true
	}
	return c, nil
}

// Root is one shipped tree and who keeps every closure in it.
type Root struct {
	// KeptBy is substrate.KeptByPublisher or substrate.KeptByRepository.
	KeptBy string
	// Provider says every closure in the tree is a provider: the publisher
	// put it among its providers, which is the one statement a closure that
	// declares no account (Apple's devices push) can make.
	Provider bool
	FS       fs.FS
}

// ProviderRoot is a tree of providers (kinds/providers.…): published packages
// that connect the substrate to an outside service.
func ProviderRoot(fsys fs.FS) Root {
	return Root{KeptBy: substrate.KeptByPublisher, Provider: true, FS: fsys}
}

// PublishedRoot is a tree of published packages that are not providers (the
// seed authority's catalogues), whose closures install under the authority
// they name.
func PublishedRoot(fsys fs.FS) Root { return Root{KeptBy: substrate.KeptByPublisher, FS: fsys} }

// Shipped is the binary's three trees, each with the role its closures take
// (decision records 0048 and 0238): the providers, the catalogues beside the
// seed, and the samples. Load the shipped catalog through it: a tree passed
// under the wrong root reads a catalogue as a provider, or the reverse.
func Shipped() []Root {
	return []Root{ProviderRoot(kinds.Providers()), PublishedRoot(kinds.Catalogues()), SampleRoot(samples.Samples())}
}

// SampleRoot is a tree of copyable packages (samples/), whose closures are copied
// under the repository's own authority.
func SampleRoot(fsys fs.FS) Root { return Root{KeptBy: substrate.KeptByRepository, FS: fsys} }

// packageDirs lists the directories holding manifests, in lexical order.
func packageDirs(fsys fs.FS) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(name, ".yaml") {
			return nil
		}
		dir := path.Dir(name)
		if dir == "." || seen[dir] {
			return nil
		}
		seen[dir] = true
		out = append(out, dir)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("catalog: read root: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

func (c *Catalog) load(root Root, dir string) error {
	b, err := loadBundle(root.FS, dir)
	if err != nil {
		c.warnings = append(c.warnings, fmt.Sprintf("%s: %v", dir, err))
		return nil
	}
	if b == nil {
		return nil // not a bundle directory
	}
	if _, dup := c.byID[b.ID]; dup {
		c.warnings = append(c.warnings, fmt.Sprintf("%s: duplicate bundle id %s", dir, b.ID))
		return nil
	}
	b.KeptBy = root.KeptBy
	b.Provider = root.Provider || b.KeptBy == substrate.KeptByPublisher && slices.Contains(b.Brings, substrate.BringsAccount)
	c.bundles = append(c.bundles, b)
	c.byID[b.ID] = b
	return nil
}

// loadBundle reads one package directory's manifests into a Bundle, or nil
// when the directory holds no bundle document.
//
// A closure is the package's own directory PLUS the documents in the
// directories above it, which is where an authority document sits: one
// `authority` manifest per authority, beside the packages it owns, so
// installing a package also lands the row that says what its authority is
// without a copy of that document in every package.
func loadBundle(fsys fs.FS, dir string) (*Bundle, error) {
	var docs []map[string]any
	for _, d := range append(ancestors(dir), dir) {
		fileDocs, err := dirDocs(fsys, d)
		if err != nil {
			return nil, err
		}
		docs = append(docs, fileDocs...)
	}
	return bundleFromDocs(docs)
}

// ancestors lists dir's parent directories inside the root, outermost first.
func ancestors(dir string) []string {
	var out []string
	for d := path.Dir(dir); ; d = path.Dir(d) {
		out = append([]string{d}, out...)
		if d == "." {
			break
		}
	}
	return out
}

// dirDocs decodes every manifest directly in one directory.
func dirDocs(fsys fs.FS, dir string) ([]map[string]any, error) {
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
	return docs, nil
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
		ref := mstr(d, "kind")
		typ := vocabulary.KindName(ref)
		id := docID(d)
		switch {
		case vocabulary.IsDeclarationRef(ref):
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
		// A DECLARATION is a record of one of the CORE meta-kinds, exactly as
		// the vocabulary/data split above reads it. Switching on the kind's
		// last word alone would count a data record of some other package's
		// kind named `trait` or `agent` as a declaration of this closure.
		if !vocabulary.IsDeclarationRef(ref) {
			typ = ""
		}
		switch typ {
		case vocabulary.DocBundle:
			found = true
			b.ID = id
			b.Authority = mstr(data, "authority")
			b.Package = mstr(data, "package")
			b.Description = mstr(data, "description")
			b.OAuthHosts = oauthHosts(mmap(data, "oauth2"))
			b.Inputs = catalogInputs(mmap(data, "inputs"))
			for _, rv := range mslice(data, "requires") {
				b.Requires = append(b.Requires, fmt.Sprint(rv))
			}
			// The floor under each requirement (decision record 0070), read
			// through the one version reader; the loader has already held
			// every key to a listed requirement and every value to a version.
			for pkg, raw := range mmap(data, "requiresAtLeast") {
				if v, ok := vocabulary.VersionValue(raw); ok && v > 0 {
					if b.RequiresAtLeast == nil {
						b.RequiresAtLeast = map[string]int64{}
					}
					b.RequiresAtLeast[pkg] = v
				}
			}
		case vocabulary.DocPackage:
			if v := mversion(data, "version"); v > 0 {
				version = v
			}
		case vocabulary.DocKind:
			b.Closure.Kinds = append(b.Closure.Kinds, id)
			describe(&b.Closure.KindDescriptions, id, mstr(data, "description"))
			describe(&b.Closure.KindPurposes, id, mstr(data, "purpose"))
		case vocabulary.DocTrait:
			b.Closure.Traits = append(b.Closure.Traits, id)
			describe(&b.Closure.TraitDescriptions, id, mstr(data, "description"))
		case vocabulary.DocFunction:
			b.Closure.Functions = append(b.Closure.Functions, id)
			describe(&b.Closure.FunctionDescriptions, id, mstr(data, "description"))
		case vocabulary.DocAgent:
			b.Closure.Agents = append(b.Closure.Agents, id)
			describe(&b.Closure.AgentDescriptions, id, mstr(data, "description"))
		case vocabulary.DocRecordMapping:
			b.Closure.Mappings = append(b.Closure.Mappings, id)
		}
		// A trigger is a DATA record, so it is matched on the kind it is a
		// record OF rather than on a document word. The closure lists it apart
		// from the other data rows because the deliveries are what make a
		// bundle run without anybody pressing anything.
		if ref == kindTrigger {
			b.Closure.Triggers = append(b.Closure.Triggers, id)
			describe(&b.Closure.TriggerCallables, id, mstr(mmap(data, "properties"), "callable"))
		}
	}
	// The SUGGESTED half of that mapping list: the ones declared onto this
	// package's own kinds FROM another package's, which the two doors admit
	// only where this repository can resolve them. Read off the documents,
	// because nothing marks one (decision record 0049). The wire field stays
	// empty: a state belongs to a repository, not to the shipped closure.
	b.suggested = vocabulary.SuggestedMappings(docs)
	if !found {
		return nil, nil
	}
	if b.Authority == "" || b.Package == "" {
		return nil, errors.New("bundle document names no package")
	}
	b.Name = b.Package
	b.Version = version
	b.readFacts()
	return b, nil
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

// Unmet is a requirement installing a bundle needs that the catalog cannot
// install: the repository holds it below the floor the requiring bundle puts
// under it, or not at all, and the catalog ships no provider under its id.
// Bundle is the sample it ships under the id, which is the owner's to import,
// nil where it ships nothing.
type Unmet struct {
	ID     string
	Floor  int64
	Bundle *Bundle
	// Err is why its authority's document gave no closure to install at the
	// floor: the document did not read, it lists no version at or above the
	// floor, or the loader refused the closure, as it refuses one written
	// against a function interface this server does not run. Nil where the
	// catalog has nowhere to read it from, or it is a sample.
	Err error
}

// unmetReason is the refusal of a requirement whose document gave no
// closure, the reason as words: wrapping it would read the requiring bundle
// as the one not published.
func unmetReason(b *Bundle, u Unmet) error {
	return fmt.Errorf("%w: %s requires %s, which this server cannot install: %s", substrate.ErrValidation, b.ID, u.ID, u.Err.Error())
}

// Requirements is what installing the shipped bundle id would install first,
// in order: the list Install runs and the upgrade preview stages, so a third
// reader cannot disagree with either. Unmet is what the same walk passed over
// as nothing it can install, which the install's admission refuses on.
func (c *Catalog) Requirements(ctx context.Context, id string, ds substrate.Dataset) ([]*Bundle, []Unmet, error) {
	b, err := c.ResolveFor(ctx, id, 0, ds)
	if err != nil {
		return nil, nil, err
	}
	return c.walkRequirements(ctx, b, ds)
}

// Install applies a shipped bundle's closure into ds's repository as ONE atomic
// admission: EVERY shipped data document is converted and validated BEFORE the
// schema apply, then the schema closure and the delivery wiring commit as a
// single repository transaction (InstallBundleClosure), so a malformed or
// unadmittable trigger rolls the whole install back instead of leaving a live
// schema half-install a retry would silently mutate. It is
// owner-gated — installing arbitrary bundle code is a human-owner action — and
// idempotent: the closure re-apply is the bundle's own upgrade semantics and
// the delivery wiring upserts in place.
//
// A SAMPLE takes Import instead, which lands the same closure under the
// repository's own authority. Install still admits one, verbatim under the
// placeholder: nothing in the tree needs that door now that no provider names
// a sample package, but a repository that wants the shipped vocabulary under
// the shipped authority may still ask for it. The install writes as the
// bundle, a substrate path, so `authorizeNewPackage` (decision 0210) does not
// bind it. It takes the same suggested-mapping filter an import does.
func (c *Catalog) Install(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset) (*Bundle, []substrate.SuggestedMapping, error) {
	return c.InstallConfirmed(ctx, actor, id, ds, nil)
}

// InstallConfirmed is Install carrying the caller's consent to a lossy
// conversion plan (decision 0067): the hash and changelog head the upgrade
// preview (Upgrade) answered. The engine refuses a lossy plan without one, or
// with one for another plan; a lossless plan ignores it.
func (c *Catalog) InstallConfirmed(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset, confirm *substrate.ConversionConfirm) (*Bundle, []substrate.SuggestedMapping, error) {
	return c.InstallReviewed(ctx, actor, id, ds, confirm, nil)
}

// PublishedCandidate binds consent to the exact document-published code reviewed.
type PublishedCandidate struct {
	Version int64  `json:"version"`
	Digest  string `json:"digest"`
}

// InstallReviewed checks the candidate before any requirement or package lands.
// The same resolved closure is checked and installed; it is never fetched twice.
func (c *Catalog) InstallReviewed(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset, confirm *substrate.ConversionConfirm, expect *PublishedCandidate) (*Bundle, []substrate.SuggestedMapping, error) {
	return c.installReviewed(ctx, actor, id, ds, confirm, expect, nil)
}

// InstallExpectations binds the whole install set. A nil candidate names a
// binary-shipped package; document-published packages require version and digest.
type InstallExpectations map[string]*PublishedCandidate

// InstallApproved refuses a changed install set before any package is written.
func (c *Catalog) InstallApproved(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset, expect InstallExpectations) (*Bundle, []substrate.SuggestedMapping, error) {
	if len(expect) == 0 {
		return nil, nil, fmt.Errorf("%w: an install requires its approved candidates", substrate.ErrConflict)
	}
	return c.installReviewed(ctx, actor, id, ds, nil, nil, expect)
}

func (c *Catalog) installReviewed(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset, confirm *substrate.ConversionConfirm, expect *PublishedCandidate, closure InstallExpectations) (*Bundle, []substrate.SuggestedMapping, error) {
	b, err := c.installable(ctx, actor, id, ds)
	if err != nil {
		return nil, nil, err
	}
	return c.installResolved(ctx, b, ds, confirm, expect, closure)
}

// SeedInstall installs a published package the way Install does, for the operator's
// install list (the defaults file's `install:`). Like SeedImport it does not
// require a human actor: it runs at registration and at a repository's open,
// with no request behind it, and the changelog is attributed to the bundle as
// any install's is. It refuses anything but a published package, because the
// list gives a repository published packages and nothing it would own.
func (c *Catalog) SeedInstall(ctx context.Context, ds substrate.Dataset, id string) error {
	b, ok := c.byID[id]
	if !ok {
		return fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	if b.KeptBy != substrate.KeptByPublisher {
		return fmt.Errorf("%w: %s is not a published package", substrate.ErrValidation, b.ID)
	}
	resolved, err := c.ResolveFor(ctx, id, 0, ds)
	if err != nil {
		return err
	}
	_, _, err = c.installResolved(ctx, resolved, ds, nil, nil, nil)
	return err
}

// installResolved is the install every door shares once its gate passed and
// the candidate resolved.
func (c *Catalog) installResolved(ctx context.Context, b *Bundle, ds substrate.Dataset, confirm *substrate.ConversionConfirm, expect *PublishedCandidate, closure InstallExpectations) (*Bundle, []substrate.SuggestedMapping, error) {
	if expect != nil && (b.origin == nil || expect.Version != b.Version || expect.Digest != b.origin.SHA256) {
		return nil, nil, fmt.Errorf("%w: the publisher listed another version since you reviewed this one; review it again", substrate.ErrConflict)
	}
	requirements, unmet, err := c.walkRequirements(ctx, b, ds)
	if err != nil {
		return nil, nil, err
	}
	// A requirement whose document gave no closure is refused here, with the
	// document's reason, before any requirement lands: the engine's admission
	// would say only that the repository lacks it.
	for _, u := range unmet {
		if u.Err != nil {
			return nil, nil, unmetReason(b, u)
		}
	}
	// An unmet requirement is refused here only where the whole install set
	// must be the approved one; every other door leaves it to the engine,
	// whose refusal names what the repository holds instead.
	if closure != nil && len(unmet) > 0 {
		return nil, nil, fmt.Errorf("%w: unresolved requirement %s", substrate.ErrValidation, unmet[0].ID)
	}
	if closure != nil {
		candidates := append(slices.Clone(requirements), b)
		// A retry may skip approved dependencies now installed. Every
		// candidate still to be written must retain its approved identity.
		for _, candidate := range candidates {
			want, named := closure[candidate.ID]
			matches := named && want == nil && candidate.origin == nil
			if named && want != nil && candidate.origin != nil {
				matches = want.Version == candidate.Version && want.Digest == candidate.origin.SHA256
			}
			if !matches {
				return nil, nil, fmt.Errorf("%w: %s changed since approval; review it again", substrate.ErrConflict, candidate.ID)
			}
		}
	}
	// Requirements commit individually, but only after the entire resolved
	// install set matches consent. Never resolve a checked candidate again.
	for _, dep := range requirements {
		if _, err := c.installed(ctx, dep, ds, nil); err != nil {
			return nil, nil, fmt.Errorf("install %s, which %s requires: %w", dep.ID, b.ID, err)
		}
	}
	// INSTALL IS A COPY: the catalog's manifests are written
	// into the repository's own changelog under the BUNDLE's actor, not the
	// requester's. Installing is an owner action, which is the check above,
	// but what lands in the changelog is the bundle's declarations, attributed to
	// the bundle, exactly as the core tree's seed is attributed to
	// `bundle:core`. The catalog is the source; the changelog is the truth, and
	// nothing on the serving path reads the catalog again.
	//
	// A PUBLISHED install says so: its packages land `source: published`,
	// which is what closes them to the repository's token afterwards, and what
	// promotes a package an earlier install left `installed`. Who KEEPS it
	// decides, so nothing here restates a list of ids. The same closure applied
	// by hand (`substratectl apply -f` of these very files) says no keeper and
	// stays the repository's own, which record 0047 sanctions and which is the
	// only way to hold a published package's declarations open to editing.
	//
	// A PROVIDER install records the shipped version it took
	// (ShippedVersion): the package's stored version is the API's to move,
	// stored+1 on a change, so over a package a hand apply ran past the
	// shipped line it lands above the shipped number, and the upgrade preview
	// reads the stamp to offer the next shipped closure all the same. Only a
	// provider, because only a published package is closed to the token: a
	// verbatim sample stays editable, and a stamp that kept offering it the
	// next shipped closure would offer to replace edits nothing records
	// (there is no digest to tell an edited copy by).
	report, err := c.installed(ctx, b, ds, confirm)
	if err != nil {
		return nil, nil, err
	}
	result := *b
	result.AddonResults = c.installAddons(ctx, ds, append(requirements, b))
	return &result, report, nil
}

func (c *Catalog) installed(ctx context.Context, b *Bundle, ds substrate.Dataset, confirm *substrate.ConversionConfirm) ([]substrate.SuggestedMapping, error) {
	var expected int64
	var narrows bool
	// A closure a publisher's document serves renames no kind, whatever this
	// repository holds: every door it arrives through (an install, a
	// refresh, an app's approval) installs it here.
	if refusals := b.RenameRefusals(); b.origin != nil && len(refusals) > 0 {
		return nil, fmt.Errorf("%w: %s", substrate.ErrValidation, strings.Join(refusals, "; "))
	}
	if b.origin != nil {
		row, err := ds.Get(ctx, vocabulary.PackageCore+"/package", b.ID)
		if err != nil && !errors.Is(err, substrate.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			expected = mversion(row.Properties, "shippedVersion")
		}
		if err == nil && mstr(row.Properties, "publishedFrom") != "" && b.Version > mversion(row.Properties, "shippedVersion") {
			old, err := c.heldClosure(ctx, b, ds)
			if err != nil {
				return nil, err
			}
			if removed := RemovedKinds(old, b); len(removed) > 0 {
				return nil, fmt.Errorf("%w: %s", substrate.ErrValidation, removedKindRefusal(b, strings.Join(removed, ", ")))
			}
			narrows, _ = CompareClosures(old, b)

			plan, err := c.publishedUpgrade(ctx, b, ds)
			if err != nil {
				return nil, err
			}
			if len(plan.Holders) > 0 {
				return nil, fmt.Errorf("%w: connected app builds hold back %s version %d", substrate.ErrConflict, b.ID, b.Version)
			}
		}
	}
	opts := substrate.BundleInstall{Published: b.KeptBy == substrate.KeptByPublisher, Confirm: confirm}
	if b.KeptBy == substrate.KeptByPublisher {
		opts.ShippedVersion = b.Version
		if b.origin != nil {
			opts.PublishedFrom = b.origin.DocumentURL
			opts.PublishedDigest = b.origin.SHA256
			opts.ExpectedPublishedVersion = expected
			opts.PublishedNarrows = narrows
		}
	}
	// A sample installed VERBATIM takes the same suggested-mapping filter an
	// import does: the mapping's `from` is a provider package either way, and
	// admission refuses it either way while that package is absent. The
	// report is SHIPPED-spelled, because that is what this door applies.
	vocabularyDocs, report, err := b.admitted(ctx, ds, viewShipped)
	if err != nil {
		return nil, err
	}
	if err := install(ctx, ds, substrate.BundleActor(b.Authority, b.Package), vocabularyDocs, b.dataDocs, opts); err != nil {
		return nil, err
	}
	return report, nil
}

// Import is the SAMPLE door (decision record 0048): the same atomic admission
// Install runs, over a closure rehomed onto the repository's OWN authority
// first. `samples.substrate.reamde.dev/tasks/task` lands as
// `ada.example.com/tasks/task`, owned by the repository that imported it:
// `source: installed`, writable through the API, and offered the shipped
// upgrade through the stamp below, which a re-import takes (decision record
// 0070).
//
// The rehoming is a walk over the decoded documents, so it reaches every
// string one carries: the ids, the declared authority, the reference pins,
// `installs` and `requires`, a function's `writes`, a trigger's selectors, a
// mapping's `from`/`to`, and the authority a function's source spells inside
// its own text. A document that still mentions the placeholder afterwards is
// refused rather than admitted, because it would declare under an authority
// this repository does not own.
//
// Requirements are not special-cased: the rehomed `requires:` names the
// repository's own packages, so ordinary admission refuses an import whose
// sibling sample has not been imported yet, naming what to import first.
//
// SUGGESTED MAPPINGS are: a sample ships one per provider it knows, onto a
// kind of its own, and the import keeps only the ones whose provider this
// repository holds. The rest are dropped with their `installs:` entries and
// reported `waiting`, because a mapping naming an absent kind is refused by
// admission and would cost the reader the whole import. Installing the
// provider and importing again lands them; that second import REPLACES the
// package, which is what a re-import always does (decision record 0048).
func (c *Catalog) Import(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset) (*Bundle, []substrate.SuggestedMapping, error) {
	return c.ImportConfirmed(ctx, actor, id, ds, nil)
}

// ImportConfirmed is Import carrying the caller's consent to a lossy
// conversion plan (decision 0067). A re-import replaces the package, so a
// kind the repository edited since can lose a property's values here; the
// same rule as the install door's applies, and without a consent the engine
// refuses the lossy plan and names it.
func (c *Catalog) ImportConfirmed(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset, confirm *substrate.ConversionConfirm) (*Bundle, []substrate.SuggestedMapping, error) {
	if !substrate.OwnersHand(ctx, actor) {
		return nil, nil, fmt.Errorf("%w: importing is the owner's action", substrate.ErrForbidden)
	}
	b, ok := c.ByID(id)
	if !ok {
		return nil, nil, fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}

	if b.KeptBy != substrate.KeptByRepository {
		return nil, nil, fmt.Errorf("%w: %s is a published package, which installs under the authority that publishes it: use install, not import",
			substrate.ErrValidation, b.ID)
	}
	return c.importSample(ctx, b, ds, confirm)
}

// SeedImport lands a sample the way Import does, for the creation seed. It
// does not require a human actor: the repository does not yet have a token
// that could take the door, and the changelog is attributed to the landed
// bundle the same way a later import would be.
func (c *Catalog) SeedImport(ctx context.Context, ds substrate.Dataset, id string) error {
	b, ok := c.byID[id]
	if !ok {
		return fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	if b.KeptBy != substrate.KeptByRepository {
		return fmt.Errorf("%w: %s is not a sample", substrate.ErrValidation, b.ID)
	}
	_, _, err := c.importSample(ctx, b, ds, nil)
	return err
}

func (c *Catalog) importSample(ctx context.Context, b *Bundle, ds substrate.Dataset, confirm *substrate.ConversionConfirm) (*Bundle, []substrate.SuggestedMapping, error) {
	home := ds.Repository().Authority
	if home == "" {
		return nil, nil, fmt.Errorf("%w: this repository has no authority of its own, so there is nowhere to import %s to",
			substrate.ErrValidation, b.ID)
	}
	kept, report, err := b.admitted(ctx, ds, viewRehomed)
	if err != nil {
		return nil, nil, err
	}
	vocabularyDocs, err := vocabulary.LocalizeAuthority(kept, b.Authority, home)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %w", substrate.ErrValidation, b.ID, err)
	}
	// The records beside the declarations go as shipped: the install folds
	// the sample's authority in them where the write reads a name (Origin).
	dataDocs := b.dataDocs
	if left := vocabulary.AuthorityMentions(vocabularyDocs, b.Authority); len(left) > 0 {
		return nil, nil, &substrate.ValidationError{
			Problems: []string{fmt.Sprintf("%s still mentions %s after the rehome, so it would declare under an authority this repository does not own",
				strings.Join(left, ", "), b.Authority)},
		}
	}
	opts := substrate.BundleInstall{Origin: b.ID, OriginVersion: b.Version, Confirm: confirm}
	if err := install(ctx, ds, substrate.BundleActor(home, b.Package), vocabularyDocs, dataDocs, opts); err != nil {
		return nil, nil, err
	}
	return b, report, nil
}

// requirementsToInstall is what installing b installs first, in order:
// every package b requires, transitively, that the repository lacks or holds
// below the requiring bundle's floor and that the catalog PUBLISHES (a
// first-party catalogue or a provider, record 0192), each after its own
// requirements, so installing a provider brings the catalogue its adapter
// writes. A required SAMPLE is left out, for the admission to refuse as
// before: a copy is the owner's to take. A requirement on the path being
// walked is a cycle, which the closure at its head installs. Install and the
// upgrade preview both walk it (walkRequirements), so the two cannot disagree
// on what a click brings.
func (c *Catalog) requirementsToInstall(ctx context.Context, b *Bundle, ds substrate.Dataset) ([]*Bundle, error) {
	order, _, err := c.walkRequirements(ctx, b, ds)
	return order, err
}

// walkRequirements is requirementsToInstall with what it passed over: each
// requirement of a bundle it walks that the repository does not hold at the
// floor and that the catalog cannot install, once, in the order it met them.
func (c *Catalog) walkRequirements(ctx context.Context, b *Bundle, ds substrate.Dataset) ([]*Bundle, []Unmet, error) {
	// Nothing to walk, so nothing to read: the preview asks this for every
	// provider entry on every listing, and most require nothing.
	if len(b.Requires) == 0 {
		return nil, nil, nil
	}
	held, err := heldVersions(ctx, ds)
	if err != nil {
		return nil, nil, err
	}
	var order []*Bundle
	var unmet []Unmet
	placed := map[string]bool{}
	visiting := map[string]bool{b.ID: true}
	var walk func(*Bundle)
	walk = func(b *Bundle) {
		for _, req := range b.Requires {
			if visiting[req] || placed[req] {
				continue
			}
			if v, has := held[req]; has && v >= b.RequiresAtLeast[req] {
				continue
			}
			// A document resolves at the floor or not at all. The binary
			// answers what it ships whatever the floor, and a version under
			// it is placed all the same: the planner reads it as a server
			// update, and the admission refuses it.
			dep, err := c.ResolveFor(ctx, req, b.RequiresAtLeast[req], ds)
			if err != nil || dep.KeptBy != substrate.KeptByPublisher {
				// Placed too, so a second bundle requiring it reports it
				// no second time.
				placed[req] = true
				u := Unmet{ID: req, Floor: b.RequiresAtLeast[req], Bundle: dep}
				if err != nil && c.reader != nil && !c.ships(req) {
					u.Err = err
				}
				unmet = append(unmet, u)
				continue
			}
			visiting[req] = true
			walk(dep)
			delete(visiting, req)
			// Placed once: a later floor above the version it ships is the
			// admission's to refuse, and installing it twice would not raise it.
			placed[req] = true
			held[req] = dep.Version
			order = append(order, dep)
		}
	}
	walk(b)
	return order, unmet, nil
}

// heldVersions is the stored version of every installed bundle. It reads the
// uncounted statuses: the upgrade preview asks this once per provider entry
// on every catalog listing, and the counted read scans every live row of
// every installed kind (issue #1086).
func heldVersions(ctx context.Context, ds substrate.Dataset) (map[string]int64, error) {
	statuses, err := ds.BundleStatusesUncounted(ctx)
	if err != nil {
		return nil, err
	}
	held := map[string]int64{}
	for _, st := range statuses {
		if st.Installed {
			held[st.ID] = st.Version
			if st.ShippedVersion > 0 {
				held[st.ID] = st.ShippedVersion
			}
		}
	}
	return held, nil
}

// installable is the gate both doors share: the owner check and the lookup.
func (c *Catalog) installable(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset) (*Bundle, error) {
	if !substrate.OwnersHand(ctx, actor) {
		return nil, fmt.Errorf("%w: taking a bundle is the repository user's action, not a machine's", substrate.ErrForbidden)
	}
	return c.ResolveFor(ctx, id, 0, ds)
}

// install is the one admission both doors run: every shipped data document is
// converted and validated BEFORE the schema apply, then the schema closure and
// the delivery wiring commit as a single repository transaction. `opts` is
// where its keeper arrives: `Published` for a published package, zero for a
// sample.
func install(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, vocabularyDocs, dataDocs []map[string]any, opts substrate.BundleInstall) error {
	// Pre-admit every data document BEFORE the schema apply: a malformed
	// delivery-wiring envelope fails here, before anything is touched, rather
	// than after the schema closure has already committed.
	dataInputs := make([]substrate.PutInput, 0, len(dataDocs))
	for _, d := range dataDocs {
		in, err := dataPutInput(d)
		if err != nil {
			return err
		}
		dataInputs = append(dataInputs, in)
	}
	_, err := ds.InstallBundleClosure(ctx, actor, vocabularyDocs, dataInputs, opts)
	return err
}

// Upgrade previews what taking a shipped bundle again over ds's stored
// declarations would do: the version motion and the blockers, computed by the
// dataset against the same closure the door applies.
//
// A PROVIDER is previewed as shipped: its closure landed verbatim and the
// install verb re-applies it verbatim. A SAMPLE is previewed as the import
// door would land it, rehomed onto the repository's own authority, and only
// when `held`, the status of the copy this repository holds for the entry, is
// stamped with this entry's id as its origin (decision record 0070): a package
// with no stamp is one the user declared by hand, or a copy taken before the
// stamp existed, and neither is anything the shipped sample may claim to
// upgrade. The plan then reads the stamp itself: the closure moved since the
// copy when the shipped version is past `originVersion`, and the copy was
// edited when its stored digest is no longer `originDigest`, which the
// re-import would discard (BundleUpgrade.DiscardsEdits). `held` is ignored
// for a provider.
//
// The preview is reused while ds's changelog head has not moved since it was
// counted (planCache), so a listing read twice stages each closure once. The
// cached plan is the whole plan, hash and changelog seq included, and the
// doors never read it: they recompute the plan inside their transaction and
// admit a confirmation against that (decision 0067).
func (c *Catalog) Upgrade(ctx context.Context, id string, ds substrate.Dataset, held *substrate.BundleStatus) (*substrate.BundleUpgrade, error) {
	b, err := c.ResolveFor(ctx, id, 0, ds)
	if err != nil {
		return nil, err
	}
	if b.origin != nil {
		return c.publishedUpgrade(ctx, b, ds)
	}
	// The gate is `held`, which the cache key does not carry, so it runs
	// before the cache is asked.
	if b.KeptBy == substrate.KeptByRepository && (held == nil || held.Origin != b.ID || ds.Repository().Authority == "") {
		return nil, nil
	}
	return c.plans.plan(ctx, ds, b.ID, func() (*substrate.BundleUpgrade, error) {
		return c.planUpgrade(ctx, b, ds)
	})
}

// planUpgrade counts the preview Upgrade answers, uncached.
func (c *Catalog) planUpgrade(ctx context.Context, b *Bundle, ds substrate.Dataset) (*substrate.BundleUpgrade, error) {
	var vocabularyDocs []map[string]any
	if b.KeptBy == substrate.KeptByRepository {
		home := ds.Repository().Authority
		// The same closure Import lands: the suggested mappings this
		// repository can resolve, rehomed. A closure that still names the
		// placeholder afterwards is one Import refuses, so it blocks here.
		kept, _, err := b.admitted(ctx, ds, viewRehomed)
		if err != nil {
			return nil, err
		}
		if vocabularyDocs, err = vocabulary.LocalizeAuthority(kept, b.Authority, home); err != nil {
			return &substrate.BundleUpgrade{Blockers: []string{err.Error()}}, nil
		}
		if left := vocabulary.AuthorityMentions(vocabularyDocs, b.Authority); len(left) > 0 {
			return &substrate.BundleUpgrade{Blockers: []string{fmt.Sprintf(
				"%s still mentions %s after the rehome, so it would declare under an authority this repository does not own",
				strings.Join(left, ", "), b.Authority)}}, nil
		}
		plan, err := ds.PlanBundleUpgrade(ctx, vocabularyDocs)
		if err != nil {
			return nil, err
		}
		return &plan, nil
	}
	// The preview is of what the DOOR would apply, so it runs over the same
	// filtered closure. In the shipped tree the filter changes nothing for a
	// provider, which declares no suggested mapping; it is not a no-op in
	// general, and the version-motion fixtures that load a sample closure as
	// a provider root are where the difference shows: previewing a dropped
	// mapping reports it as a blocker and hides the real ones.
	//
	// The requirements the install would bring first are staged ahead of the
	// closure, so a catalogue the click installs is not reported as a missing
	// requirement, and what bringing it would break is. The closure's own
	// bundle document comes last, which is the one the plan previews.
	requirements, unmet, err := c.walkRequirements(ctx, b, ds)
	if err != nil {
		return nil, err
	}
	for _, dep := range append(requirements, b) {
		docs, _, err := dep.admitted(ctx, ds, viewShipped)
		if err != nil {
			return nil, err
		}
		vocabularyDocs = append(vocabularyDocs, docs...)
	}
	plan, err := ds.PlanBundleUpgrade(ctx, vocabularyDocs)
	if err != nil {
		return nil, err
	}
	triggers, err := outsideTriggerChanges(ctx, ds, b)
	if err != nil {
		return nil, err
	}
	plan.CodeChanges = append(plan.CodeChanges, triggers...)
	// The install refuses a requirement whose document gave no closure
	// (installResolved), so the preview names the same reason.
	for _, u := range unmet {
		if u.Err != nil {
			plan.Blockers = append(plan.Blockers, strings.TrimPrefix(unmetReason(b, u).Error(), substrate.ErrValidation.Error()+": "))
		}
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

// kindTrigger is the kind a shipped trigger record IS one of.
const kindTrigger = vocabulary.TriggerKind

// describe records one closure member's prose under its id, allocating the map
// only when there is prose: an absent description is omitted from the wire
// rather than carried as an empty string a reader would meet as a blank line.
func describe(into *map[string]string, id, text string) {
	if text == "" {
		return
	}
	if *into == nil {
		*into = map[string]string{}
	}
	(*into)[id] = text
}

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

// catalogInputs types the manifest's `inputs:` map. The loader has already
// held each input to its closed key set, so a key it does not name is not
// dropped here by accident: it never reached this point.
func catalogInputs(raw map[string]any) map[string]substrate.CatalogInput {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]substrate.CatalogInput, len(raw))
	for name, v := range raw {
		out[name] = substrate.CatalogInput{
			Kind:        mstr(v, "kind"),
			Inject:      mstr(v, "inject"),
			Description: mstr(v, "description"),
		}
	}
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
