// Package domains reads the `_substrate` TXT record a domain publishes about
// itself: where the repository of that name is served (`server=`), the
// one-time code that proves it (`proof=`), and where the authority document
// of a publisher is (`doc=`, optionally pinned by `sha256=`). The record is
// the proof that whoever wrote it controls the name's DNS, which serving a
// file from a host does not show (docs/plans/authorities.md, decisions 1 to 3).
package domains

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Label is the DNS label the record lives under: `_substrate.<name>`.
const Label = "_substrate"

// Version opens every record this server reads. A TXT string that does not
// start with it belongs to something else at the same label and is skipped.
const Version = "v=substrate1"

// maxName bounds a looked-up name to DNS's own limit, so a request cannot make
// the server ask its resolver for something no zone can hold.
const maxName = 253

// Record is one parsed `_substrate` record. Keys this binary does not know are
// kept in Other and never refuse the record, so a later key can be published
// before every server reads it.
type Record struct {
	// Server is the origin (`https://host[:port]`) of the substrate holding
	// the repository of this name.
	Server string
	// Proof is the one-time code a Verify domain request showed the owner.
	Proof string
	// Doc is the URL of the authority document this name publishes.
	Doc string
	// SHA256 pins Doc's bytes, lower-case hex, when the publisher wants DNS
	// rather than the file's host to be the whole proof.
	SHA256 string
	// Other holds every key this binary does not read.
	Other map[string]string
}

// ErrMalformed marks a record that exists and cannot be read. A caller
// treats it as no proof at all, and may show its message to the owner.
var ErrMalformed = errors.New("the _substrate record is malformed")

// Parse reads the TXT strings answered at one `_substrate` label. found is
// false when no string opens with Version. More than one such string is an
// error, as atproto's handle resolution refuses two answers: a name that says
// two things says nothing.
//
// A resolver answers each TXT record as its character-strings concatenated,
// so a record split across 255-byte strings arrives whole.
func Parse(txts []string) (rec Record, found bool, err error) {
	var ours []string
	for _, t := range txts {
		t = strings.TrimSpace(t)
		if t == Version || strings.HasPrefix(t, Version+" ") {
			ours = append(ours, t)
		}
	}
	switch len(ours) {
	case 0:
		return Record{}, false, nil
	case 1:
	default:
		return Record{}, true, fmt.Errorf("%w: %d records open with %q, and a name may carry one", ErrMalformed, len(ours), Version)
	}
	seen := map[string]bool{}
	for _, field := range strings.Fields(strings.TrimPrefix(ours[0], Version)) {
		k, v, ok := strings.Cut(field, "=")
		if !ok || k == "" || v == "" {
			return Record{}, true, fmt.Errorf("%w: %q is not key=value", ErrMalformed, field)
		}
		if seen[k] {
			return Record{}, true, fmt.Errorf("%w: %q appears twice", ErrMalformed, k)
		}
		seen[k] = true
		switch k {
		case "server":
			origin, err := httpsOrigin(v)
			if err != nil {
				return Record{}, true, fmt.Errorf("%w: server=%s: %w", ErrMalformed, v, err)
			}
			rec.Server = origin
		case "proof":
			rec.Proof = v
		case "doc":
			if err := httpsURL(v); err != nil {
				return Record{}, true, fmt.Errorf("%w: doc=%s: %w", ErrMalformed, v, err)
			}
			rec.Doc = v
		case "sha256":
			b, err := hex.DecodeString(v)
			if err != nil || len(b) != 32 {
				return Record{}, true, fmt.Errorf("%w: sha256=%s is not 64 hex digits", ErrMalformed, v)
			}
			rec.SHA256 = strings.ToLower(v)
		default:
			if rec.Other == nil {
				rec.Other = map[string]string{}
			}
			rec.Other[k] = v
		}
	}
	if rec.SHA256 != "" && rec.Doc == "" {
		return Record{}, true, fmt.Errorf("%w: sha256= pins a document and the record names none with doc=", ErrMalformed)
	}
	return rec, true, nil
}

// httpsOrigin holds a server= value to an https origin: no path beyond `/`,
// no query, no fragment, no user info. It answers the origin without a
// trailing slash, the form a server compares against.
func httpsOrigin(v string) (string, error) {
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" {
		return "", errors.New("not an https origin")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("an origin carries no path, query, fragment or user")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

func httpsURL(v string) error {
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" {
		return errors.New("not an https URL")
	}
	if u.User != nil || u.Fragment != "" {
		return errors.New("a document URL carries no user or fragment")
	}
	return nil
}

// Resolver answers TXT lookups. *net.Resolver satisfies it; a test passes a
// Static.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Lookup resolves and parses the record of name. A name with no record, or
// whose label does not exist, answers found false and no error: absence is
// the common case, not a failure. Any other resolver error is returned, so a
// caller can tell "this name publishes nothing" from "the lookup failed".
//
// Lookup never walks up or down the tree: `_substrate.example.org` says
// nothing about `app.example.org`, or the other way round.
func Lookup(ctx context.Context, r Resolver, name string) (Record, bool, error) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if name == "" || len(name) > maxName || strings.ContainsAny(name, "/:@ ") || !strings.Contains(name, ".") {
		return Record{}, false, fmt.Errorf("%q is not a domain name", name)
	}
	txts, err := r.LookupTXT(ctx, Label+"."+name+".")
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return Record{}, false, nil
		}
		return Record{}, false, fmt.Errorf("looking up %s.%s: %w", Label, name, err)
	}
	return Parse(txts)
}

// Static is a Resolver over fixed answers, keyed by the full name looked up
// (`_substrate.example.org.`). A name it does not hold answers not found, as
// an NXDOMAIN does.
type Static map[string][]string

// LookupTXT answers the fixed strings for name.
func (s Static) LookupTXT(_ context.Context, name string) ([]string, error) {
	if txts, ok := s[name]; ok {
		return txts, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}
