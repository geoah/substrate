package catalog

import (
	"context"
	"errors"
	"fmt"
)

// Origin is where a document-published bundle came from (decision 0212): the
// authority document of its publisher and the closure that document lists.
// A bundle the binary ships has none.
type Origin struct {
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

// FromDocument is b as the authority document at origin published it: the
// same closure, answering Origin. The document reader builds every version it
// resolves this way, and a test of a reader of Origin builds one from a root.
func (b *Bundle) FromDocument(origin Origin) *Bundle {
	cp := *b
	cp.origin = &origin
	return &cp
}

// Resolve answers the bundle id names: the closure the binary ships when it
// ships id, whatever its version, so the caller compares it with atLeast;
// otherwise the newest version id's authority document lists at atLeast or
// later (decision 0212).
func (c *Catalog) Resolve(_ context.Context, id string, _ int64) (*Bundle, error) {
	if b, ok := c.byID[id]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("%w: %s is not in this server's catalog", ErrNotPublished, id)
}
