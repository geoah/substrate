package catalog

import (
	"context"
	"errors"
	"fmt"
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

// Origin is where b came from when an authority document published it, nil
// when the binary ships it.
func (b *Bundle) Origin() *Origin { return b.origin }

// Resolve answers the bundle id names: the closure the binary ships when it
// ships id, whatever its version, so the caller compares it with atLeast;
// otherwise the newest version id's authority document lists at atLeast or
// later (decision 0212).
func (c *Catalog) Resolve(ctx context.Context, id string, atLeast int64) (*Bundle, error) {
	if b, ok := c.byID[id]; ok {
		return b, nil
	}
	a, p := vocabulary.SplitPackageRef(id)
	if !vocabulary.ValidAuthority(a) || !vocabulary.ValidPackage(p) {
		return nil, fmt.Errorf("%w: %s is not a package id", ErrNotPublished, id)
	}
	if a == "providers.substrate.reamde.dev" || a == "samples.substrate.reamde.dev" {
		return nil, fmt.Errorf("%w: %s must be shipped by this server", ErrNotPublished, id)
	}
	if c.reader == nil {
		return nil, fmt.Errorf("%w: %s has no authority document reader", ErrNotPublished, id)
	}
	source, err := c.reader.Read(ctx, a, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotPublished, err)
	}
	return c.resolveSource(ctx, id, atLeast, source)
}

func (c *Catalog) resolveSource(ctx context.Context, id string, floor int64, source authority.Source) (*Bundle, error) {
	_, name := vocabulary.SplitPackageRef(id)
	var newest authority.AuthorityPackageVersion
	for _, p := range source.Document.Packages {
		if p.Package != name {
			continue
		}
		for _, v := range p.Versions {
			if int64(v.Version) >= floor && v.Version > newest.Version {
				newest = v
			}
		}
	}
	if newest.Version == 0 {
		return nil, fmt.Errorf("%w: %s lists no version at or above %d", ErrNotPublished, id, floor)
	}
	return c.readClosure(ctx, id, source, newest)
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
	b.origin = &Origin{Authority: source.Document.Authority, Name: source.Document.Name, DocumentURL: source.DocURL, FromFile: source.FromFile, ClosureURL: v.Closure, SHA256: strings.ToLower(v.SHA256)}
	for _, entry := range source.Document.Packages {
		if entry.Package == b.Package {
			b.origin.AddonFor = entry.AddonFor
			break
		}
	}
	b.Publisher = &substrate.AuthorizationPackagePublisher{Authority: b.origin.Authority, Name: b.origin.Name, DocumentURL: b.origin.DocumentURL, FromFile: b.origin.FromFile}
	return b, nil
}

// ResolveFor refuses a document owned by the receiving repository before any fetch.
func (c *Catalog) ResolveFor(ctx context.Context, id string, floor int64, ds substrate.Dataset) (*Bundle, error) {
	a, _ := vocabulary.SplitPackageRef(id)
	if _, binary := c.byID[id]; !binary && c.reader != nil && a == ds.Repository().Authority {
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
		if isSchemaDoc(vocabulary.KindPackage(ref), vocabulary.KindName(ref)) {
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
	parsed, err := vocabulary.ParseDocuments(declarations)
	if err != nil {
		return nil, err
	}
	if _, err := vocabulary.BuildPackages(parsed, vocabulary.SourcePublished); err != nil {
		return nil, err
	}
	b, err := bundleFromDocs(docs)
	if err != nil {
		return nil, err
	}
	if b.Version != version || version < 1 {
		return nil, fmt.Errorf("closure version %d does not match listed version %d", b.Version, version)
	}
	b.Tier = substrate.TierProvider
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
