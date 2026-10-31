package authorize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/geoah/substrate/internal/egress"
)

// The fetch of an app's metadata document is a server dial of a URL anybody
// may name, so it has a transport of its own rather than egress.Transport:
// that one honors the operator's SUBSTRATE_EGRESS_ALLOW (a private provider
// endpoint an owner configured) and the environment's proxy, and either would
// let a client_id reach what the operator opened for something else.
const (
	// maxDocument is CIMD §8.7's bound: a document is a few hundred bytes.
	maxDocument  = 5 << 10
	fetchTimeout = 5 * time.Second
	// fetchSlots bounds the fetches in flight across the whole server.
	fetchSlots = 4
	// cacheFor is how long a fetched document is believed; a failure is
	// never cached (CIMD §5.2).
	cacheFor   = 10 * time.Minute
	maxCached  = 256
	maxNameLen = 200
)

// Document is the part of an app's client ID metadata document this server
// reads.
type Document struct {
	ClientID     string
	RedirectURIs []string
	// Name and URI are the app's claims about itself, shown as claims: Name
	// is cut to maxNameLen, URI is kept only when it is an https URL.
	Name string
	URI  string
}

// FetchError is a refusal the owner may read: it names what failed (a status,
// a timeout, a field) and never an address the dial reached or was refused.
type FetchError struct{ msg string }

func (e *FetchError) Error() string { return e.msg }

// NewFetchError is a refusal with msg, for a document source other than the
// fetcher (a test's) to answer the way the fetcher does.
func NewFetchError(msg string) error { return &FetchError{msg: msg} }

func refuse(format string, args ...any) error {
	return &FetchError{msg: fmt.Sprintf(format, args...)}
}

// Fetcher fetches and caches app documents.
type Fetcher struct {
	client *http.Client
	slots  chan struct{}
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	doc Document
	at  time.Time
}

// NewFetcher builds the fetcher with its confined transport.
func NewFetcher(now func() time.Time) *Fetcher {
	if now == nil {
		now = time.Now
	}
	return newFetcher(&http.Client{Transport: confinedTransport(), Timeout: fetchTimeout}, now)
}

func newFetcher(c *http.Client, now func() time.Time) *Fetcher {
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Fetcher{client: c, slots: make(chan struct{}, fetchSlots), now: now, cache: map[string]cached{}}
}

// confinedTransport dials public unicast addresses and nothing else, checked
// on the RESOLVED address at connect so a DNS answer cannot steer it, and
// uses no proxy.
func confinedTransport() *http.Transport {
	d := &net.Dialer{Timeout: fetchTimeout}
	d.Control = func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil || !Public(ap.Addr()) {
			return errors.New("destination refused")
		}
		return nil
	}
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            d.DialContext,
		TLSHandshakeTimeout:    fetchTimeout,
		ResponseHeaderTimeout:  fetchTimeout,
		MaxResponseHeaderBytes: 16 << 10,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           4,
		IdleConnTimeout:        30 * time.Second,
	}
}

// special is every RFC 6890 special-use range the standard library calls
// global unicast, which egress.Blocked therefore lets through: documentation,
// benchmarking, the IETF protocol block, the reserved class E, and the IPv6
// protocol, documentation and discard blocks.
var special = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "3fff::/20", "100::/64", "5f00::/16",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// nat64 and sixToFour carry an IPv4 a gateway delivers to, so the special
// ranges are checked on that IPv4 too (egress.Blocked does the same for its
// own list).
var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour = netip.MustParsePrefix("2002::/16")
)

// Public reports whether a document fetch may connect to addr.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	if egress.Blocked(addr) {
		return false
	}
	candidates := []netip.Addr{addr}
	if addr.Is6() {
		b := addr.As16()
		switch {
		case nat64.Contains(addr):
			candidates = append(candidates, netip.AddrFrom4([4]byte(b[12:16])))
		case sixToFour.Contains(addr):
			candidates = append(candidates, netip.AddrFrom4([4]byte(b[2:6])))
		}
	}
	for _, a := range candidates {
		for _, p := range special {
			if p.Contains(a) {
				return false
			}
		}
	}
	return true
}

// Fetch returns the document a web app's client_id serves, from the cache
// when it is fresh. A local client is never fetched.
func (f *Fetcher) Fetch(ctx context.Context, c Client) (Document, error) {
	if c.Local {
		return Document{}, errors.New("a local app has no document")
	}
	f.mu.Lock()
	hit, ok := f.cache[c.ID]
	f.mu.Unlock()
	if ok && f.now().Sub(hit.at) < cacheFor {
		return hit.doc, nil
	}
	// A wait for a slot is bounded like the fetch itself, so a burst of
	// requests queues for seconds, never for as long as a client holds on.
	wait := time.NewTimer(fetchTimeout)
	defer wait.Stop()
	select {
	case f.slots <- struct{}{}:
	case <-wait.C:
		return Document{}, refuse("the app's document could not be fetched: the server is busy, try again")
	case <-ctx.Done():
		return Document{}, refuse("the app's document could not be fetched: the server is busy, try again")
	}
	defer func() { <-f.slots }()
	doc, err := f.fetch(ctx, c.ID)
	if err != nil {
		return Document{}, err
	}
	f.mu.Lock()
	if len(f.cache) >= maxCached {
		f.evictOldest()
	}
	f.cache[c.ID] = cached{doc: doc, at: f.now()}
	f.mu.Unlock()
	return doc, nil
}

func (f *Fetcher) evictOldest() {
	var oldest string
	var at time.Time
	for k, v := range f.cache {
		if oldest == "" || v.at.Before(at) {
			oldest, at = k, v.at
		}
	}
	delete(f.cache, oldest)
}

func (f *Fetcher) fetch(ctx context.Context, clientID string) (Document, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return Document{}, refuse("the app's client id is not a URL this server can fetch")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) && ue.Timeout() {
			return Document{}, refuse("the app's document could not be fetched: it timed out")
		}
		return Document{}, refuse("the app's document could not be fetched: the connection failed or was refused")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Document{}, refuse("the app's document could not be fetched: it answered %d, not 200", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument+1))
	if err != nil {
		return Document{}, refuse("the app's document could not be read")
	}
	if len(body) > maxDocument {
		return Document{}, refuse("the app's document is larger than %d bytes", maxDocument)
	}
	return ParseDocument(body, clientID)
}

// ParseDocument holds a fetched document to what this server needs from it.
func ParseDocument(body []byte, clientID string) (Document, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return Document{}, refuse("the app's document is not a JSON object")
	}
	id, _ := raw["client_id"].(string)
	if id != clientID {
		return Document{}, refuse("the app's document names client_id %q, not the URL it was fetched from", truncate(id, 200))
	}
	// A public client holds no secret and no key: a document that claims one
	// is a confidential client this server cannot authenticate (CIMD §4.1).
	for _, k := range []string{"client_secret", "client_secret_expires_at", "jwks", "jwks_uri"} {
		if _, ok := raw[k]; ok {
			return Document{}, refuse("the app's document carries %s: an app here is a public client and holds no secret or key", k)
		}
	}
	if m, ok := raw["token_endpoint_auth_method"]; ok && m != "none" {
		return Document{}, refuse("the app's document declares token_endpoint_auth_method %v: only none is supported", m)
	}
	if !listsOrAbsent(raw, "grant_types", "authorization_code") || !listsOrAbsent(raw, "response_types", "code") {
		return Document{}, refuse("the app's document declares grant or response types without the authorization code flow")
	}
	list, ok := raw["redirect_uris"].([]any)
	if !ok || len(list) == 0 {
		return Document{}, refuse("the app's document lists no redirect_uris")
	}
	doc := Document{ClientID: id}
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			return Document{}, refuse("the app's document lists a redirect_uri that is not a string")
		}
		if _, err := ParseRedirect(s); err != nil {
			return Document{}, refuse("the app's document lists a redirect_uri this server refuses: %v", err)
		}
		doc.RedirectURIs = append(doc.RedirectURIs, s)
	}
	if name, ok := raw["client_name"].(string); ok {
		doc.Name = truncate(strings.TrimSpace(name), maxNameLen)
	}
	if uri, ok := raw["client_uri"].(string); ok {
		if u, err := parseAbsolute(uri); err == nil && u.Scheme == "https" {
			doc.URI = uri
		}
	}
	return doc, nil
}

// listsOrAbsent reports whether key is absent or a list that contains want.
func listsOrAbsent(raw map[string]any, key, want string) bool {
	v, ok := raw[key]
	if !ok {
		return true
	}
	list, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range list {
		if e == want {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
