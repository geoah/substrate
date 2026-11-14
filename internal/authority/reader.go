// Package authority reads publishers' documents and immutable closures.
package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	fetchSlots   = 4
	fetchTimeout = 5 * time.Second
	cacheFor     = 10 * time.Minute
	maxCached    = 256
	// maxCachedClosureBytes bounds the closures kept by digest: a closure
	// is immutable under its sha256, so a hit is always the bytes a fetch
	// would return, and the bound is only memory.
	maxCachedClosureBytes = 32 << 20
	maxNameLen            = 200
	MaxClosure            = 4 << 20
)

func refuse(format string, args ...any) error { return fmt.Errorf(format, args...) }
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// maxAuthorityDocument bounds an authority document: the plan measured a
// fourteen-package publisher at a few kilobytes of listing.
const maxAuthorityDocument = 64 << 10

// AuthorityDocument is what an authority publishes about itself at the URL
// its `_substrate` record names: its apps, by client id, and its packages.
type AuthorityDocument struct {
	Authority   string `json:"authority"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// MovedTo is the authority this publisher says it moved to (decision
	// record 0293). A server shows it beside the publisher's packages and
	// never follows it: nothing installs from it and nothing moves.
	MovedTo  string             `json:"movedTo,omitempty"`
	Packages []AuthorityPackage `json:"packages,omitempty"`
	Apps     []AuthorityApp     `json:"apps,omitempty"`
}

// AuthorityPackage is one package an authority publishes, with its
// versions.
type AuthorityPackage struct {
	Package  string                    `json:"package"`
	AddonFor string                    `json:"addonFor,omitempty"`
	Versions []AuthorityPackageVersion `json:"versions,omitempty"`
}

// AuthorityPackageVersion is one immutable version of a published package:
// the closure's URL, the digest its bytes must hash to, and the vocabulary
// dialect its declarations need (decision 0268), which a server reads before
// it fetches the closure so it can pass over a version written for a newer
// one. A server older than the field ignores it.
type AuthorityPackageVersion struct {
	Version int    `json:"version"`
	Closure string `json:"closure"`
	SHA256  string `json:"sha256"`
	Dialect int    `json:"dialect,omitempty"`
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
// typed JSON, ignoring keys this server does not read, and the authority it names
// equal to the one looked up, so a record cannot borrow another name's
// document.
func ParseAuthorityDocument(body []byte, authority string) (AuthorityDocument, error) {
	var doc AuthorityDocument
	raw, err := knownJSON(body, reflect.TypeFor[AuthorityDocument]())
	if err != nil {
		return doc, err
	}
	body = raw
	if err := json.Unmarshal(body, &doc); err != nil {
		return AuthorityDocument{}, refuse("the authority document of %s is not one this server reads: %v", authority, err)
	}
	if doc.Authority != authority {
		return AuthorityDocument{}, refuse("the authority document the record of %s names is %q's, not %s's", authority, truncate(doc.Authority, 200), authority)
	}
	doc.Name = truncate(strings.TrimSpace(doc.Name), maxNameLen)
	// A pointer that names no other authority is not one: it is left out,
	// and the document is read, as a key this server does not read would be.
	if doc.MovedTo != "" && (!vocabulary.ValidKindAuthority(doc.MovedTo) || doc.MovedTo == vocabulary.Local || doc.MovedTo == authority) {
		doc.MovedTo = ""
	}
	for i, a := range doc.Apps {
		if _, err := parseClientID(a.ClientID); err != nil {
			return AuthorityDocument{}, refuse("the authority document of %s lists an app whose client_id this server refuses: %v", authority, err)
		}
		doc.Apps[i].Name = truncate(strings.TrimSpace(a.Name), maxNameLen)
	}
	seen := map[string]bool{}
	for _, p := range doc.Packages {
		if seen[p.Package] {
			return AuthorityDocument{}, fmt.Errorf("duplicate package %s", p.Package)
		}
		seen[p.Package] = true
		if err := validatePackage(p); err != nil {
			return AuthorityDocument{}, err
		}
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

// Options configures a Reader. Every field may be zero.
type Options struct {
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

// Reader confirms who publishes an app.
type Reader struct {
	resolver domains.Resolver
	client   *http.Client
	now      func() time.Time
	local    map[string]string
	slots    chan struct{}

	mu    sync.Mutex
	cache map[string]cachedAuthority
	// closures are the verified closure bytes by their sha256, oldest first
	// in closureOrder: the approval page plans on every read, and each plan
	// resolves its published packages again.
	closures     map[string][]byte
	closureOrder []string
	closureBytes int
}

type cachedAuthority struct {
	doc AuthorityDocument
	at  time.Time
}

// NewReader builds a Reader.
func NewReader(o Options) *Reader {
	v := &Reader{
		resolver: o.Resolver, client: o.Client, now: o.Now, local: o.LocalDocuments,
		slots: make(chan struct{}, fetchSlots), cache: map[string]cachedAuthority{},
		closures: map[string][]byte{},
	}
	if v.resolver == nil {
		v.resolver = net.DefaultResolver
	}
	if v.client == nil {
		v.client = egress.ConfinedClient()
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
		body, err := readFile(path, maxAuthorityDocument)
		if err != nil {
			return fmt.Errorf("the authority document for %s: %w", authority, err)
		}
		if _, err := ParseAuthorityDocument(body, authority); err != nil {
			return fmt.Errorf("the authority document for %s at %s: %w", authority, path, err)
		}
	}
	return nil
}

// Source identifies the document and how the reader obtained it.
type Source struct {
	Document AuthorityDocument
	Record   string
	DocURL   string
	FromFile bool
}

func (v *Reader) Read(ctx context.Context, name string, fresh bool) (Source, error) {
	s := Source{}
	doc, err := v.read(ctx, name, fresh, &s)
	s.Document = doc
	if err == nil && !s.FromFile {
		for _, p := range doc.Packages {
			for _, v := range p.Versions {
				u, e := url.Parse(v.Closure)
				if e != nil || !validHTTPS(u) {
					return s, fmt.Errorf("package %s needs an HTTPS closure URL", p.Package)
				}
			}
		}
	}
	return s, err
}

// authorityDocument reads authority's document: from a development file
// when one is configured, otherwise through its `_substrate` record and the
// URL it names, from the cache when a success is fresh. It fills p's record
// and document URL as it learns them.
func (v *Reader) read(ctx context.Context, authority string, fresh bool, p *Source) (AuthorityDocument, error) {
	if path, ok := v.local[authority]; ok {
		p.Record = "none: SUBSTRATE_INSECURE_AUTHORITY_DOCS names a local file"
		p.DocURL = "file:" + path
		p.FromFile = true
		body, err := readFile(path, maxAuthorityDocument)
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
	if !fresh && ok && v.now().Sub(hit.at) < cacheFor {
		return hit.doc, nil
	}
	body, err := v.fetch(ctx, rec.Doc, maxAuthorityDocument)
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

func (v *Reader) fetch(ctx context.Context, raw string, limit int) ([]byte, error) {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, refuse("the authority document could not be read")
	}
	if len(body) > limit {
		return nil, refuse("the authority document is larger than %d bytes", limit)
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

func validatePackage(p AuthorityPackage) error {
	if p.AddonFor != "" {
		a, n, ok := strings.Cut(p.AddonFor, "/")
		if !ok || !vocabulary.ValidAuthority(a) || !vocabulary.ValidPackage(n) {
			return fmt.Errorf("invalid addonFor %q", p.AddonFor)
		}
	}
	seen := map[int]bool{}
	for _, v := range p.Versions {
		if v.Version <= 0 || seen[v.Version] {
			return fmt.Errorf("package %s lists a duplicate or nonpositive version", p.Package)
		}
		if v.Dialect < 0 {
			return fmt.Errorf("package %s version %d needs a dialect of at least 1", p.Package, v.Version)
		}
		seen[v.Version] = true
		digest, err := hex.DecodeString(v.SHA256)
		if err != nil || len(digest) != 32 {
			return fmt.Errorf("package %s version %d needs a sha256 of 64 hex characters", p.Package, v.Version)
		}
		if v.Closure == "" {
			return fmt.Errorf("package %s version %d has no closure", p.Package, v.Version)
		}
		if u, err := url.Parse(v.Closure); err != nil || (u.IsAbs() && !validHTTPS(u)) {
			return fmt.Errorf("invalid closure URL %q", v.Closure)
		}
	}
	return nil
}

func validHTTPS(u *url.URL) bool {
	return u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && u.Opaque == ""
}

func parseClientID(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || strings.ContainsAny(raw, "#\\ \t\r\n") {
		return nil, errors.New("invalid client_id URL")
	}
	local := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if (!validHTTPS(u) && !local) || u.RawQuery != "" || u.ForceQuery || u.User != nil || u.Hostname() == "local" || (u.Scheme == "https" && u.Path == "") {
		return nil, errors.New("invalid client_id URL")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return nil, errors.New("client_id carries a dot segment")
		}
	}
	return u, nil
}

func readFile(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(b) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return b, err
}

// Closure reads only the bytes the document pins.
func (v *Reader) Closure(ctx context.Context, source Source, version AuthorityPackageVersion) ([]byte, error) {
	// Where the closure is listed is checked before the cache is asked: a
	// document naming a location this server refuses resolves nothing, even
	// for bytes it already holds under the same digest.
	if source.FromFile {
		if !filepath.IsLocal(version.Closure) {
			return nil, errors.New("a development closure must stay in the document's directory")
		}
	} else if u, e := url.Parse(version.Closure); e != nil || !validHTTPS(u) {
		return nil, errors.New("a published closure needs an HTTPS URL")
	}
	// Only a fetched closure is cached: a development file is cheap to read
	// again, and reading it is what holds it inside the document's
	// directory.
	digest := strings.ToLower(version.SHA256)
	if !source.FromFile {
		v.mu.Lock()
		cached, hit := v.closures[digest]
		v.mu.Unlock()
		if hit {
			return cached, nil
		}
	}
	var body []byte
	var err error
	if source.FromFile {
		root, openErr := os.OpenRoot(filepath.Dir(strings.TrimPrefix(source.DocURL, "file:")))
		if openErr != nil {
			return nil, openErr
		}
		defer func() { _ = root.Close() }()
		f, openErr := root.Open(version.Closure)
		if openErr != nil {
			return nil, openErr
		}
		defer func() { _ = f.Close() }()
		body, err = io.ReadAll(io.LimitReader(f, MaxClosure+1))
		if len(body) > MaxClosure {
			return nil, fmt.Errorf("closure exceeds %d bytes", MaxClosure)
		}
	} else {
		body, err = v.fetch(ctx, version.Closure, MaxClosure)
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), version.SHA256) {
		return nil, errors.New("the closure does not match its published sha256")
	}
	if !source.FromFile {
		v.keepClosure(digest, body)
	}
	return body, nil
}

// keepClosure caches verified closure bytes by digest, dropping the oldest
// past maxCachedClosureBytes.
func (v *Reader) keepClosure(digest string, body []byte) {
	if len(body) > maxCachedClosureBytes {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.closures[digest]; ok {
		return
	}
	for v.closureBytes+len(body) > maxCachedClosureBytes && len(v.closureOrder) > 0 {
		oldest := v.closureOrder[0]
		v.closureOrder = v.closureOrder[1:]
		v.closureBytes -= len(v.closures[oldest])
		delete(v.closures, oldest)
	}
	v.closures[digest] = body
	v.closureOrder = append(v.closureOrder, digest)
	v.closureBytes += len(body)
}
