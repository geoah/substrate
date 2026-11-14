package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Origin is where a document-published bundle came from (decision 0212): the
// authority document of its publisher and the closure that document lists.
// A bundle the binary ships has none.
type Origin struct {
	// AddonFor is the provider this optional package extends, from its document entry.
	AddonFor string
	// Authority is the publisher, and Name the document's own name for it.
	Authority string
	Name      string
	// DocumentURL is the document the package is listed in, `file:` and a
	// path for a development document.
	DocumentURL string
	// ClosureURL and SHA256 are the listed closure of the resolved version.
	ClosureURL string
	SHA256     string
	// FromFile is true when a file this server was configured to read
	// (SUBSTRATE_INSECURE_AUTHORITY_DOCS) stood in for the authority's DNS
	// record and document.
	FromFile bool
}

// ErrNotPublished wraps a refusal to resolve a package no authority document
// lists at the version asked for, or whose authority publishes no document
// this server can read. The wrapping error's text is the owner's to read.
var ErrNotPublished = errors.New("not published")

// ErrNewerDialect is a fetched closure whose package header states a
// vocabulary dialect this server does not read (decision 0268): the resolver
// and a refresh pass over that version to an older one.
var ErrNewerDialect = errors.New("written for a newer vocabulary dialect")

// Origin is where b came from when an authority document published it, nil
// when the binary ships it.
func (b *Bundle) Origin() *Origin { return b.origin }

// FromDocument is b as the authority document at origin published it: the
// same closure, answering Origin, and what it is for read as a document's
// package is read (a provider only with an account, an add-on for what the
// document names). A test of a reader of Origin builds one from a root; the
// document reader checks the add-on against the providers it ships.
func (b *Bundle) FromDocument(origin Origin) *Bundle {
	cp := *b
	cp.origin = &origin
	cp.Provider = slices.Contains(cp.Brings, substrate.BringsAccount)
	cp.AddonFor = origin.AddonFor
	return &cp
}

// Resolve answers the bundle id names: the closure the binary ships when it
// ships id, whatever its version, so the caller compares it with atLeast;
// otherwise the newest version id's authority document lists at atLeast or
// later (decision 0212).
func (c *Catalog) Resolve(ctx context.Context, id string, atLeast int64) (*Bundle, error) {
	if b, ok := c.byID[id]; ok {
		return b, nil
	}
	a, p := vocabulary.SplitPackageRef(id)
	if !vocabulary.ValidKindAuthority(a) || !vocabulary.ValidPackage(p) {
		return nil, fmt.Errorf("%w: %s is not a package id", ErrNotPublished, id)
	}
	// A package of the repository's own lives in its local namespace, which
	// no publisher lists (decision record 0259).
	if a == vocabulary.Local {
		return nil, fmt.Errorf("%w: %s is not in this server's catalog", ErrNotPublished, id)
	}
	if a == "providers.substrate.reamde.dev" || a == "samples.substrate.reamde.dev" || c.seeded[id] {
		return nil, fmt.Errorf("%w: %s must be shipped by this server", ErrNotPublished, id)
	}
	if c.reader == nil {
		return nil, fmt.Errorf("%w: %s is not in this server's catalog", ErrNotPublished, id)
	}
	source, err := c.reader.Read(ctx, a, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotPublished, err)
	}
	return c.resolveSource(ctx, id, atLeast, source)
}

// ships reports whether this server answers for id itself: a closure it
// carries, a package it seeds, or an id under the provider or sample
// authority, which only the binary publishes.
func (c *Catalog) ships(id string) bool {
	if _, ok := c.byID[id]; ok {
		return true
	}
	a, _ := vocabulary.SplitPackageRef(id)
	return a == "providers.substrate.reamde.dev" || a == "samples.substrate.reamde.dev" || c.seeded[id]
}

// resolveSource reads the newest version at or above floor that this server
// can read (decision 0268). A version whose listed dialect is above
// vocabulary.Dialect is passed over before it is fetched; one that lists none
// is fetched, and passed over too when its package header states a newer
// dialect (ErrNewerDialect). Any other refusal of a fetched version is the
// answer, so a broken release still says it is broken.
func (c *Catalog) resolveSource(ctx context.Context, id string, floor int64, source authority.Source) (*Bundle, error) {
	_, name := vocabulary.SplitPackageRef(id)
	var candidates []authority.AuthorityPackageVersion
	var unread error
	for _, p := range source.Document.Packages {
		if p.Package != name {
			continue
		}
		for _, v := range p.Versions {
			if int64(v.Version) < floor {
				continue
			}
			if !readableVersion(v) {
				if unread == nil {
					unread = fmt.Errorf("version %d: %s", v.Version, vocabulary.DialectRefusal(int64(v.Dialect)))
				}
				continue
			}
			candidates = append(candidates, v)
		}
	}
	slices.SortFunc(candidates, func(a, b authority.AuthorityPackageVersion) int { return b.Version - a.Version })
	for _, v := range candidates {
		b, err := c.readClosure(ctx, id, source, v)
		if errors.Is(err, ErrNewerDialect) {
			unread = err
			continue
		}
		return b, err
	}
	if unread != nil {
		return nil, fmt.Errorf("%w: %s lists no version at or above %d this server can read: %w", ErrNotPublished, id, floor, unread)
	}
	return nil, fmt.Errorf("%w: %s lists no version at or above %d", ErrNotPublished, id, floor)
}

// readableVersion reports whether this server reads the dialect a listed
// version states. A version that states none is read, and its closure's own
// header is the check.
func readableVersion(v authority.AuthorityPackageVersion) bool {
	return int64(v.Dialect) <= vocabulary.Dialect
}

func (c *Catalog) readClosure(ctx context.Context, id string, source authority.Source, v authority.AuthorityPackageVersion) (*Bundle, error) {
	raw, err := c.reader.Closure(ctx, source, v)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotPublished, id, err)
	}
	b, err := ParseClosure(raw, id, int64(v.Version))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotPublished, id, err)
	}
	// A version listed below what its closure needs would send a server
	// older than its keys to fetch it and stop on one, instead of passing it
	// over to an older version. It is a broken release, refused as such.
	if v.Dialect > 0 && int64(v.Dialect) < b.needs {
		return nil, fmt.Errorf("%w: %s: version %d is listed at vocabulary dialect %d, and its closure needs dialect %d", ErrNotPublished, id, v.Version, v.Dialect, b.needs)
	}
	b.origin = &Origin{Authority: source.Document.Authority, Name: source.Document.Name, DocumentURL: source.DocURL, FromFile: source.FromFile, ClosureURL: v.Closure, SHA256: strings.ToLower(v.SHA256)}
	for _, entry := range source.Document.Packages {
		if entry.Package == b.Package {
			b.origin.AddonFor = entry.AddonFor
			break
		}
	}
	b.AddonFor = c.documentAddonFor(b)
	b.Publisher = &substrate.AuthorizationPackagePublisher{Authority: b.origin.Authority, Name: b.origin.Name, DocumentURL: b.origin.DocumentURL, FromFile: b.origin.FromFile, Digest: b.origin.SHA256}
	return b, nil
}

// ResolveFor refuses a document owned by the receiving repository before any fetch.
func (c *Catalog) ResolveFor(ctx context.Context, id string, floor int64, ds substrate.Dataset) (*Bundle, error) {
	a, _ := vocabulary.SplitPackageRef(id)
	if _, binary := c.byID[id]; !binary && c.reader != nil && slices.Contains(ds.Repository().LocalNames(), a) {
		return nil, fmt.Errorf("%w: %s is your own authority; declare the package here", ErrNotPublished, id)
	}
	return c.Resolve(ctx, id, floor)
}

// ParseClosure checks the boundary before the engine admits the declarations.
// Versions live on the package header, as in the binary's bundle directories.
func ParseClosure(raw []byte, id string, version int64) (*Bundle, error) {
	docs, err := decodeDocs(raw)
	if err != nil {
		return nil, err
	}
	count := 0
	var declarations []map[string]any
	for _, d := range docs {
		ref := mstr(d, "kind")
		data := mmap(d, "data")
		if vocabulary.IsDeclarationRef(ref) {
			declarations = append(declarations, d)
			if mstr(data, "authority")+"/"+mstr(data, "package") != id {
				return nil, fmt.Errorf("declaration %s is outside %s", docID(d), id)
			}
			if ref == vocabulary.PackageCore+"/bundle" {
				count++
				if docID(d) != id {
					return nil, fmt.Errorf("bundle id must be %s", id)
				}
			}
		} else if vocabulary.KindPackage(ref) != id && !packageMachinery(ref, docID(d), id) {
			return nil, fmt.Errorf("record %s/%s is outside %s", ref, docID(d), id)
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("a closure needs exactly one bundle for %s", id)
	}
	// The dialect before the declarations: a header written for a newer one
	// is that, and not whatever key of it this loader would refuse further
	// down. Only a closure with one header for the package, at the version
	// listed, is passed over on its dialect; any other is refused below as
	// the broken release it is.
	var headers []map[string]any
	for _, d := range declarations {
		if mstr(d, "kind") == vocabulary.CoreKind(vocabulary.DocPackage) && docID(d) == id {
			headers = append(headers, d)
		}
	}
	if len(headers) == 1 {
		data := mmap(headers[0], "data")
		if needs, ok := vocabulary.VersionValue(data["dialect"]); ok && needs > vocabulary.Dialect {
			if stated, _ := vocabulary.VersionValue(data["version"]); stated != version {
				return nil, fmt.Errorf("closure version %d does not match listed version %d", stated, version)
			}
			return nil, fmt.Errorf("%w: %s", ErrNewerDialect, vocabulary.DialectRefusal(needs))
		}
	}
	parsed, err := vocabulary.ParseDocuments(declarations)
	if err != nil {
		return nil, err
	}
	pkgs, err := vocabulary.BuildPackages(parsed, vocabulary.SourcePublished)
	if err != nil {
		return nil, err
	}
	b, err := bundleFromDocs(docs)
	if err != nil {
		return nil, err
	}
	for _, g := range pkgs {
		if g.Identity == id {
			b.dialect, b.needs = g.Dialect, g.NeedsDialect
		}
	}
	if b.Version != version || version < 1 {
		return nil, fmt.Errorf("closure version %d does not match listed version %d", b.Version, version)
	}
	b.KeptBy = substrate.KeptByPublisher
	b.Provider = slices.Contains(b.Brings, substrate.BringsAccount)
	return b, nil
}

// Shared machinery belongs to a package by its id. Authentication records
// and arbitrary records of another package never belong to this closure.
func packageMachinery(kind, record, id string) bool {
	if !strings.HasPrefix(record, id+"/") {
		return false
	}
	switch kind {
	case vocabulary.TriggerKind, vocabulary.PackageCore + "/setting", vocabulary.PackageCore + "/secret", "substrate.reamde.dev/console/recordview":
		return true
	}
	return false
}
