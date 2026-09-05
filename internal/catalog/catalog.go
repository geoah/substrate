// Package catalog is the substrate's read model over the bundle closures
// SHIPPED in the binary — the registry a repository imports from. A catalog
// entry is one embedded PACKAGE directory: its identity, the schema CLOSURE
// (the core schema documents — the atomic install unit) and the DATA RECORDS
// it ships beside them (a provider's triggers, the llm sample's provider
// rows).
//
// Every entry carries a TIER, and the tier is the tree it came from (decision
// record 0048). A PROVIDER (kinds/providers.substrate.reamde.dev) INSTALLS
// under the authority that publishes it. A SAMPLE (samples/) IMPORTS: the
// closure is rehomed onto the repository's own authority first, so what lands
// is the repository's to edit.
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

// Bundle is one installable catalog entry: the shipped closure and the wire
// shape the console previews before taking it. The wire half is
// substrate.CatalogBundle, embedded so the wire golden can hold it
// (internal/substrate/wire_test.go); the closure documents stay here,
// unexported, and never marshal.
type Bundle struct {
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
}

// Closure and ShippedRecord are the catalog's names for the two nested wire
// shapes, so a caller reads catalog.Closure rather than the substrate
// spelling.
type (
	Closure       = substrate.CatalogClosure
	ShippedRecord = substrate.CatalogShippedRecord
)

// LandedID is the bundle id this closure lands under in a repository whose own
// authority is home. A provider keeps the id it publishes; a SAMPLE is rehomed
// on import, so what lands is its package under the repository's authority
// (decision record 0048) and the shipped id addresses the catalog entry alone.
func (b *Bundle) LandedID(homeAuthority string) string {
	if b.Tier != substrate.TierSample || homeAuthority == "" {
		return b.ID
	}
	return homeAuthority + "/" + b.Package
}

// HeldID is the id this closure ALREADY has in a repository that holds it, or
// "" when it holds neither. Both are asked because both can be there: an
// import lands the rehomed id, and installing a sample verbatim (still a
// sanctioned door until the providers stop requiring sample packages) lands
// the shipped one. A read that asked only for the rehomed id reported a
// verbatim-installed sample as available, and offered it again.
func (b *Bundle) HeldID(held map[string]bool, homeAuthority string) string {
	if landed := b.LandedID(homeAuthority); held[landed] {
		return landed
	}
	if held[b.ID] {
		return b.ID
	}
	return ""
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

// Load parses every shipped PACKAGE directory in the given roots. A package
// directory is any directory holding .yaml manifests, at whatever depth the
// root nests them: the shipped tree is kinds/<authority>/<package>/ and the
// samples are samples/<package>/, and both are read the same way. A directory
// that carries no bundle document is not a bundle and is skipped; one that
// fails to parse is dropped with a recorded warning rather than failing the
// whole catalog — a broken neighbor never bricks the shipped set.
//
// A root also says which TIER its closures are (decision record 0048): the
// tier is the tree a closure came from and nothing else, so nothing infers it
// from an authority's spelling.
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
	return c, nil
}

// Root is one shipped tree and the tier every closure in it takes.
type Root struct {
	// Tier is substrate.TierProvider or substrate.TierSample.
	Tier string
	FS   fs.FS
}

// ProviderRoot is a tree of published packages (kinds/), whose closures
// install under the authority they name.
func ProviderRoot(fsys fs.FS) Root { return Root{Tier: substrate.TierProvider, FS: fsys} }

// SampleRoot is a tree of copyable packages (samples/), whose closures import
// under the repository's own authority.
func SampleRoot(fsys fs.FS) Root { return Root{Tier: substrate.TierSample, FS: fsys} }

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
	b.Tier = root.Tier
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
		case isSchemaDoc(vocabulary.KindPackage(ref), typ):
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
			b.Package = mstr(data, "package")
			b.Description = mstr(data, "description")
			b.Inputs = mmap(data, "inputs")
			for _, rv := range mslice(data, "requires") {
				b.Requires = append(b.Requires, fmt.Sprint(rv))
			}
		case vocabulary.DocPackage:
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
	return b, nil
}

// isSchemaDoc mirrors the loader's split (and substratectl's): a schema document IS a
// record of one of the core meta-kinds; everything else is a data record.
func isSchemaDoc(pkg, name string) bool {
	return pkg == vocabulary.PackageCore && vocabulary.VocabularyDocumentKind(name)
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
//
// A SAMPLE takes Import instead, which lands the same closure under the
// repository's own authority. Install still admits one, verbatim under the
// placeholder: nothing in the tree needs that door now that no provider names
// a sample package, but a repository that wants the shipped vocabulary under
// the shipped authority may still ask for it, which is what
// `authorizeNewPackage` sanctions. It takes the same suggested-mapping filter
// an import does.
func (c *Catalog) Install(ctx context.Context, actor substrate.Actor, id string, ds substrate.Dataset) (*Bundle, []substrate.SuggestedMapping, error) {
	b, err := c.installable(actor, id)
	if err != nil {
		return nil, nil, err
	}
	// INSTALL IS A COPY: the catalog's manifests are written
	// into the repository's own changelog under the BUNDLE's actor, not the
	// requester's. Installing is an owner action, which is the check above,
	// but what lands in the changelog is the bundle's declarations, attributed to
	// the bundle, exactly as the core tree's seed is attributed to
	// `bundle:core`. The catalog is the source; the changelog is the truth, and
	// nothing on the serving path reads the catalog again.
	//
	// A PROVIDER install says so: its packages land `source: published`, which
	// is what closes them to the repository's token afterwards, and what
	// promotes a provider an earlier install left `installed`. The TIER decides
	// it, so nothing here restates a list of ids. The same closure applied by
	// hand (`substratectl apply -f` of these very files) carries no tier and
	// stays the repository's own, which record 0047 sanctions and which is the
	// only way to hold a provider's declarations open to editing.
	opts := substrate.BundleInstall{Published: b.Tier == substrate.TierProvider}
	// A sample installed VERBATIM takes the same suggested-mapping filter an
	// import does: the mapping's `from` is a provider package either way, and
	// admission refuses it either way while that package is absent. The
	// report is SHIPPED-spelled, because that is what this door applies.
	vocabularyDocs, report, err := b.admitted(ctx, ds, viewShipped)
	if err != nil {
		return nil, nil, err
	}
	if err := install(ctx, ds, substrate.BundleActor(b.Authority, b.Package), vocabularyDocs, b.dataDocs, opts); err != nil {
		return nil, nil, err
	}
	return b, report, nil
}

// Import is the SAMPLE door (decision record 0048): the same atomic admission
// Install runs, over a closure rehomed onto the repository's OWN authority
// first. `samples.substrate.reamde.dev/tasks/task` lands as
// `ada.example.com/tasks/task`, owned by the repository that imported it:
// `source: installed`, writable through the API, never offered an upgrade.
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
	b, err := c.installable(actor, id)
	if err != nil {
		return nil, nil, err
	}
	if b.Tier != substrate.TierSample {
		return nil, nil, fmt.Errorf("%w: %s is a provider, which installs under the authority that publishes it: use install, not import",
			substrate.ErrValidation, b.ID)
	}
	home := ds.Repository().Authority
	if home == "" {
		return nil, nil, fmt.Errorf("%w: this repository has no authority of its own, so there is nowhere to import %s to",
			substrate.ErrValidation, b.ID)
	}
	// The suggested mappings are decided BEFORE the rehome, over the shipped
	// documents, and the report that comes back with them is what this door
	// answers: it names the rehomed ids, because those are the declarations
	// the apply below writes.
	kept, report, err := b.admitted(ctx, ds, viewRehomed)
	if err != nil {
		return nil, nil, err
	}
	vocabularyDocs, err := vocabulary.RehomeAuthority(kept, b.Authority, home)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %w", substrate.ErrValidation, b.ID, err)
	}
	dataDocs, err := vocabulary.RehomeAuthority(b.dataDocs, b.Authority, home)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %w", substrate.ErrValidation, b.ID, err)
	}
	if left := vocabulary.AuthorityMentions(append(append([]map[string]any{}, vocabularyDocs...), dataDocs...), b.Authority); len(left) > 0 {
		return nil, nil, &substrate.ValidationError{
			Problems: []string{fmt.Sprintf("%s still mentions %s after the rehome, so it would declare under an authority this repository does not own",
				strings.Join(left, ", "), b.Authority)},
		}
	}
	// The changelog is attributed to the bundle under the authority it LANDS
	// in: an imported sample is the repository's own vocabulary, so nothing in
	// its history names the placeholder it was authored under.
	//
	// It lands `installed`, never `published`: what a sample leaves behind
	// belongs to the repository, and `published` is exactly the origin whose
	// declarations the repository's own token may not write (record 0048).
	if err := install(ctx, ds, substrate.BundleActor(home, b.Package), vocabularyDocs, dataDocs, substrate.BundleInstall{}); err != nil {
		return nil, nil, err
	}
	return b, report, nil
}

// installable is the gate both doors share: the owner check and the lookup.
func (c *Catalog) installable(actor substrate.Actor, id string) (*Bundle, error) {
	if !substrate.HumanActors[actor] {
		return nil, fmt.Errorf("%w: taking a bundle is the repository user's action, not a machine's", substrate.ErrForbidden)
	}
	b, ok := c.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	return b, nil
}

// install is the one admission both doors run: every shipped data document is
// converted and validated BEFORE the schema apply, then the schema closure and
// the delivery wiring commit as a single repository transaction. `opts` is
// where the tier arrives: `Published` for a provider, zero for a sample.
func install(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, vocabularyDocs, dataDocs []map[string]any, opts substrate.BundleInstall) error {
	inst, ok := ds.(substrate.BundleInstaller)
	if !ok {
		return errors.New("catalog: this dataset cannot install bundle closures")
	}
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
	_, err := inst.InstallBundleClosure(ctx, actor, vocabularyDocs, dataInputs, opts)
	return err
}

// Upgrade previews what re-installing a shipped bundle over ds's stored
// declarations would do: the version motion and the blockers, computed by the
// dataset against the same closure Install applies. A dataset that offers no
// preview answers nil, not an error: the catalog still lists and installs
// there, it just cannot say what an install would change.
//
// A SAMPLE has no upgrade to preview, ever: what it landed belongs to the
// repository, which may have edited it, and re-importing would replace the
// package wholesale rather than merge (decision record 0048). So the answer is
// nil before the dataset is asked, and no offer reaches the console.
func (c *Catalog) Upgrade(ctx context.Context, id string, ds substrate.Dataset) (*substrate.BundleUpgrade, error) {
	b, ok := c.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	if b.Tier == substrate.TierSample {
		return nil, nil
	}
	p, ok := ds.(substrate.BundleUpgradePlanner)
	if !ok {
		return nil, nil
	}
	// The preview is of what the DOOR would apply, so it runs over the same
	// filtered closure. In the shipped tree the filter changes nothing here,
	// because only a provider is previewed (the tier gate above) and a
	// provider declares no suggested mapping; it is not a no-op in general,
	// and the version-motion fixtures that load a sample closure as a
	// provider root are where the difference shows: previewing a dropped
	// mapping reports it as a blocker and hides the real ones.
	vocabularyDocs, _, err := b.admitted(ctx, ds, viewShipped)
	if err != nil {
		return nil, err
	}
	plan, err := p.PlanBundleUpgrade(ctx, vocabularyDocs)
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
