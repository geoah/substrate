// Package authorize holds the protocol half of app authorization (decision
// 0194): what a `client_id` and a `redirect_uri` may be, the fetch of an app's
// client ID metadata document, PKCE, and the in-memory lifecycle of a parked
// request and its one-time code. The HTTP half is internal/api; the scope
// grammar is internal/scope.
package authorize

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// maxURL bounds every URL this package accepts, so a request cannot park
// or a document cannot list an arbitrarily long one.
const maxURL = 2048

// Client is an app as its client_id names it.
type Client struct {
	// ID is the client_id exactly as sent; every later comparison is to this
	// string (CIMD §3: simple string comparison).
	ID   string
	Host string
	// Local is true for a loopback client_id: an app on the owner's own
	// computer, whose document is never fetched and which nothing verifies.
	Local bool
}

// ParseClientID holds a client_id to the two accepted shapes: an https URL
// with a path (a web app, which serves its metadata document there), or a
// loopback http URL (a local app).
func ParseClientID(raw string) (Client, error) {
	u, err := parseAbsolute(raw)
	if err != nil {
		return Client{}, fmt.Errorf("client_id %w", err)
	}
	if u.RawQuery != "" || u.ForceQuery {
		return Client{}, errors.New("client_id carries a query")
	}
	// The decoded path, so `.%2e` and a segment hidden behind `%2F` are seen
	// for what a server that normalizes the path would serve.
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return Client{}, errors.New("client_id carries a dot segment")
		}
	}
	switch u.Scheme {
	case "https":
		if u.Path == "" {
			return Client{}, errors.New("client_id has no path: an app's client id is the URL of its metadata document")
		}
		return Client{ID: raw, Host: u.Host}, nil
	case "http":
		if !loopbackHost(u.Hostname()) {
			return Client{}, errors.New("client_id is http on a host that is not loopback: a web app's client id is https")
		}
		return Client{ID: raw, Host: u.Host, Local: true}, nil
	default:
		return Client{}, fmt.Errorf("client_id scheme %q is not https or loopback http", u.Scheme)
	}
}

// ParseRedirect holds a redirect_uri to what this server ever navigates to:
// https, or http on a loopback host (RFC 8252 §7.3), absolute, with no
// userinfo and no fragment.
func ParseRedirect(raw string) (*url.URL, error) {
	u, err := parseAbsolute(raw)
	if err != nil {
		return nil, fmt.Errorf("redirect_uri %w", err)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && loopbackHost(u.Hostname()):
	default:
		return nil, errors.New("redirect_uri is not https or loopback http")
	}
	return u, nil
}

// parseAbsolute is the shape every URL here shares.
func parseAbsolute(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("is missing")
	}
	if len(raw) > maxURL {
		return nil, fmt.Errorf("is longer than %d bytes", maxURL)
	}
	if strings.ContainsAny(raw, "#\\ \t\r\n") {
		return nil, errors.New("carries a fragment, a backslash or whitespace")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("is not a URL")
	}
	if !u.IsAbs() || u.Opaque != "" || u.Host == "" {
		return nil, errors.New("is not an absolute URL with a host")
	}
	if u.User != nil {
		return nil, errors.New("carries userinfo")
	}
	// A host the page shows must read as the host the server dials: a
	// Unicode host is dialed as its punycode, so `аpple.com` (a Cyrillic а)
	// would head the approval page while the code went to a domain anybody
	// can register. An app names its host in ASCII, the A-label of an
	// internationalized name included.
	if strings.Contains(u.Host, "%") {
		return nil, errors.New("carries a percent-encoded host")
	}
	for i := range len(u.Host) {
		if u.Host[i] >= 0x80 {
			return nil, errors.New("carries a non-ASCII host: name it by its punycode (xn--) form")
		}
	}
	return u, nil
}

// loopbackHost reports whether host names the local machine: the two
// loopback literals or `localhost`.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && (ip.Equal(net.IPv4(127, 0, 0, 1)) || ip.Equal(net.IPv6loopback))
}

// RedirectListed reports whether redirect equals one of listed exactly, or,
// for a loopback http entry, equals it in everything but the port: a native
// app listens on whatever port the system hands it (RFC 8252 §7.3).
func RedirectListed(listed []string, redirect string) bool {
	got, err := ParseRedirect(redirect)
	if err != nil {
		return false
	}
	for _, entry := range listed {
		if entry == redirect {
			return true
		}
		want, err := ParseRedirect(entry)
		if err != nil || want.Scheme != "http" || got.Scheme != "http" {
			continue
		}
		if want.Hostname() == got.Hostname() && want.EscapedPath() == got.EscapedPath() &&
			want.RawQuery == got.RawQuery {
			return true
		}
	}
	return false
}

// LocalRedirect reports whether a local app's redirect is acceptable: a
// loopback http URL, any port, since a local app has no document to list it.
func LocalRedirect(redirect string) bool {
	u, err := ParseRedirect(redirect)
	return err == nil && u.Scheme == "http"
}

// HostOf is the host[:port] of an already parsed URL string, "" when it does
// not parse.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}
