package authorize

// Who publishes an app (decision 0209). The client document's
// `substrate_authority` is a claim anybody can write. The server confirms it
// through the claimed authority's `_substrate` TXT record, which only
// whoever controls the name's DNS can publish: the record names the
// authority document, the document lists the exact client id, and the
// redirect goes to the authority or a name under it. Anything short of all
// of that is an unverified app, with the reason the owner reads.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/vocabulary"
)

// maxAuthorityDocument bounds an authority document: the plan measured a
// fourteen-package publisher at a few kilobytes of listing.
const maxAuthorityDocument = 64 << 10

// AuthorityDocument is what an authority publishes about itself at the URL
// its `_substrate` record names: its apps, by client id, and its packages.
// Phase 1 reads the apps alone; Packages is parsed and kept so a document
// written for the later phases is not refused.
type AuthorityDocument struct {
	Authority   string             `json:"authority"`
	Name        string             `json:"name,omitempty"`
	Description string             `json:"description,omitempty"`
	Packages    []AuthorityPackage `json:"packages,omitempty"`
	Apps        []AuthorityApp     `json:"apps,omitempty"`
}

// AuthorityPackage is one package an authority publishes, with its
// versions.
type AuthorityPackage struct {
	Package  string                    `json:"package"`
	AddonFor string                    `json:"addonFor,omitempty"`
	Versions []AuthorityPackageVersion `json:"versions,omitempty"`
}

// AuthorityPackageVersion is one immutable version of a published package:
// the closure's URL and the digest its bytes must hash to.
type AuthorityPackageVersion struct {
	Version int    `json:"version"`
	Closure string `json:"closure"`
	SHA256  string `json:"sha256"`
}

// AuthorityApp is one app that speaks for the authority.
type AuthorityApp struct {
	ClientID string     `json:"client_id"`
	Name     string     `json:"name,omitempty"`
	Packages []string   `json:"packages,omitempty"`
	Apple    *AppleApps `json:"apple,omitempty"`
}

// AppleApps names the signed Apple apps (team id and bundle id) the
// authority vouches for.
type AppleApps struct {
	AppIDs []string `json:"appIDs,omitempty"`
}

// ParseAuthorityDocument holds a fetched authority document to its shape:
// strict JSON, no key this server does not read, and the authority it names
// equal to the one looked up, so a record cannot borrow another name's
// document.
func ParseAuthorityDocument(body []byte, authority string) (AuthorityDocument, error) {
	var doc AuthorityDocument
	if err := strictjson.DecodeBytes(body, &doc, false); err != nil {
		return AuthorityDocument{}, refuse("the authority document of %s is not one this server reads: %v", authority, err)
	}
	if doc.Authority != authority {
		return AuthorityDocument{}, refuse("the authority document the record of %s names is %q's, not %s's", authority, truncate(doc.Authority, 200), authority)
	}
	doc.Name = truncate(strings.TrimSpace(doc.Name), maxNameLen)
	for i, a := range doc.Apps {
		if _, err := ParseClientID(a.ClientID); err != nil {
			return AuthorityDocument{}, refuse("the authority document of %s lists an app whose client_id this server refuses: %v", authority, err)
		}
		doc.Apps[i].Name = truncate(strings.TrimSpace(a.Name), maxNameLen)
	}
	for _, p := range doc.Packages {
		if !vocabulary.ValidPackage(p.Package) {
			return AuthorityDocument{}, refuse("the authority document of %s lists a package named %q, which is not a package name", authority, truncate(p.Package, 100))
		}
	}
	return doc, nil
}

// App is the document's entry for clientID, compared exactly.
func (d AuthorityDocument) App(clientID string) (AuthorityApp, bool) {
	for _, a := range d.Apps {
		if a.ClientID == clientID {
			return a, true
		}
	}
	return AuthorityApp{}, false
}

// Publisher is what the server could confirm about who publishes an app.
// Verified is true only when every step held; otherwise Reason says, for
// the owner, which step did not, and is empty for an app that claims no
// publisher at all.
type Publisher struct {
	Verified bool
	// Authority is the verified authority, or the one the app claims.
	Authority string
	// Name is the authority document's own name for the publisher, and
	// AppName its name for this app; both only when the document was read.
	Name    string
	AppName string
	Reason  string
	// Record is the `_substrate` record as the server read it, DocURL the
	// document it names and Listed whether that document lists the client
	// id: the technical details behind the verdict.
	Record string
	DocURL string
	Listed bool
	// FromFile is true when the document came from a file this server was
	// configured to read (SUBSTRATE_INSECURE_AUTHORITY_DOCS) in place of the
	// authority's DNS record and the fetch: the operator, not the
	// authority's DNS, vouched for it.
	FromFile bool
}

// Same reports whether p and q are the same answer to who publishes an app:
// both unverified, or both verified as the same authority by the same
// source. An approval and a token exchange hold the publisher a later check
// finds to the one the owner was shown.
func (p Publisher) Same(q Publisher) bool {
	if p.Verified != q.Verified {
		return false
	}
	return !p.Verified || (p.Authority == q.Authority && p.FromFile == q.FromFile)
}

// PublisherOptions configures a Verifier. Every field may be zero.
type PublisherOptions struct {
	// Resolver answers the TXT lookup; net.DefaultResolver when nil.
	Resolver domains.Resolver
	// Client fetches authority documents; ConfinedClient when nil.
	Client *http.Client
	Now    func() time.Time
	// LocalDocuments maps an authority to a file holding its document, read
	// in place of the DNS lookup and the fetch
	// (SUBSTRATE_INSECURE_AUTHORITY_DOCS, development only).
	LocalDocuments map[string]string
}

// Verifier confirms who publishes an app.
type Verifier struct {
	resolver domains.Resolver
	client   *http.Client
	now      func() time.Time
	local    map[string]string
	slots    chan struct{}

	mu    sync.Mutex
	cache map[string]cachedAuthority
}

type cachedAuthority struct {
	doc AuthorityDocument
	at  time.Time
}

// NewVerifier builds a Verifier.
func NewVerifier(o PublisherOptions) *Verifier {
	v := &Verifier{
		resolver: o.Resolver, client: o.Client, now: o.Now, local: o.LocalDocuments,
		slots: make(chan struct{}, fetchSlots), cache: map[string]cachedAuthority{},
	}
	if v.resolver == nil {
		v.resolver = net.DefaultResolver
	}
	if v.client == nil {
		v.client = ConfinedClient()
	}
	if v.now == nil {
		v.now = time.Now
	}
	return v
}

// CheckLocalDocuments reads every development document once, so a server
// started with a path it cannot read refuses to start rather than leaving
// the authority unverified at the first approval.
func CheckLocalDocuments(docs map[string]string) error {
	for authority, path := range docs {
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("the authority document for %s: %w", authority, err)
		}
		if _, err := ParseAuthorityDocument(body, authority); err != nil {
			return fmt.Errorf("the authority document for %s at %s: %w", authority, path, err)
		}
	}
	return nil
}

// Verify decides who publishes the app c, whose client document is doc, for
// a request whose code goes to redirect. A loopback client id or a loopback
// redirect is never verified: any program on the device can send either.
func (v *Verifier) Verify(ctx context.Context, c Client, doc Document, redirect string) Publisher {
	if c.Local {
		return Publisher{Reason: "the app runs on this device, and nothing confirms which program it is"}
	}
	p := Publisher{Authority: doc.Authority}
	if doc.RefusedAuthority != "" {
		p.Reason = fmt.Sprintf("its document claims the publisher %q, which is not an authority name", doc.RefusedAuthority)
		return p
	}
	if doc.Authority == "" {
		return p
	}
	authority := doc.Authority
	adoc, err := v.authorityDocument(ctx, authority, &p)
	if err != nil {
		p.Reason = err.Error()
		return p
	}
	p.Name = adoc.Name
	entry, listed := adoc.App(c.ID)
	p.Listed = listed
	if !listed {
		p.Reason = fmt.Sprintf("the authority document of %s does not list this app's client id", authority)
		return p
	}
	p.AppName = entry.Name
	if LocalRedirect(redirect) {
		p.Reason = "the access goes to a program on this device, and nothing confirms which program it is"
		return p
	}
	if !RedirectUnder(redirect, authority) {
		p.Reason = fmt.Sprintf("the access goes to %s, which is not %s or a name under it", HostOf(redirect), authority)
		return p
	}
	p.Verified = true
	return p
}

// RedirectUnder reports whether redirect is https on the host authority or a
// name under it.
func RedirectUnder(redirect, authority string) bool {
	u, err := ParseRedirect(redirect)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == authority || strings.HasSuffix(host, "."+authority)
}

// authorityDocument reads authority's document: from a development file
// when one is configured, otherwise through its `_substrate` record and the
// URL it names, from the cache when a success is fresh. It fills p's record
// and document URL as it learns them.
func (v *Verifier) authorityDocument(ctx context.Context, authority string, p *Publisher) (AuthorityDocument, error) {
	if path, ok := v.local[authority]; ok {
		p.Record = "none: SUBSTRATE_INSECURE_AUTHORITY_DOCS names a local file"
		p.DocURL = "file:" + path
		p.FromFile = true
		body, err := os.ReadFile(path)
		if err != nil {
			return AuthorityDocument{}, fmt.Errorf("the local authority document of %s could not be read", authority)
		}
		return ParseAuthorityDocument(body, authority)
	}
	rec, found, err := domains.Lookup(ctx, v.resolver, authority)
	switch {
	case errors.Is(err, domains.ErrMalformed):
		return AuthorityDocument{}, fmt.Errorf("the _substrate record of %s cannot be read: %w", authority, err)
	case err != nil:
		return AuthorityDocument{}, fmt.Errorf("the _substrate record of %s could not be looked up", authority)
	case !found:
		return AuthorityDocument{}, fmt.Errorf("%s publishes no _substrate record", authority)
	}
	p.Record = recordText(rec)
	if rec.Doc == "" {
		return AuthorityDocument{}, fmt.Errorf("the _substrate record of %s names no authority document (doc=)", authority)
	}
	p.DocURL = rec.Doc
	key := authority + " " + rec.Doc + " " + rec.SHA256
	v.mu.Lock()
	hit, ok := v.cache[key]
	v.mu.Unlock()
	if ok && v.now().Sub(hit.at) < cacheFor {
		return hit.doc, nil
	}
	body, err := v.fetch(ctx, rec.Doc)
	if err != nil {
		return AuthorityDocument{}, err
	}
	if rec.SHA256 != "" {
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != rec.SHA256 {
			return AuthorityDocument{}, fmt.Errorf("the authority document of %s does not match the sha256 its _substrate record pins", authority)
		}
	}
	doc, err := ParseAuthorityDocument(body, authority)
	if err != nil {
		return AuthorityDocument{}, err
	}
	v.mu.Lock()
	if len(v.cache) >= maxCached {
		var oldest string
		var at time.Time
		for k, c := range v.cache {
			if oldest == "" || c.at.Before(at) {
				oldest, at = k, c.at
			}
		}
		delete(v.cache, oldest)
	}
	v.cache[key] = cachedAuthority{doc: doc, at: v.now()}
	v.mu.Unlock()
	return doc, nil
}

func (v *Verifier) fetch(ctx context.Context, raw string) ([]byte, error) {
	wait := time.NewTimer(fetchTimeout)
	defer wait.Stop()
	select {
	case v.slots <- struct{}{}:
	case <-wait.C:
		return nil, refuse("the authority document could not be fetched: the server is busy, try again")
	case <-ctx.Done():
		return nil, refuse("the authority document could not be fetched: the server is busy, try again")
	}
	defer func() { <-v.slots }()
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, refuse("the authority document's URL is not one this server can fetch")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) && ue.Timeout() {
			return nil, refuse("the authority document could not be fetched: it timed out")
		}
		return nil, refuse("the authority document could not be fetched: the connection failed or was refused")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, refuse("the authority document could not be fetched: it answered %d, not 200", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAuthorityDocument+1))
	if err != nil {
		return nil, refuse("the authority document could not be read")
	}
	if len(body) > maxAuthorityDocument {
		return nil, refuse("the authority document is larger than %d bytes", maxAuthorityDocument)
	}
	return body, nil
}

// recordText renders a parsed record the way a publisher writes it, for the
// approval page's technical details.
func recordText(r domains.Record) string {
	parts := []string{domains.Version}
	for _, kv := range [][2]string{{"server", r.Server}, {"doc", r.Doc}, {"sha256", r.SHA256}} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+kv[1])
		}
	}
	return strings.Join(parts, " ")
}
