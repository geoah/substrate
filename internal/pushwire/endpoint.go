package pushwire

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// MaxRelay bounds a relay's address, in characters.
const MaxRelay = 2048

// ErrRelay is a relay address Endpoint refuses.
var ErrRelay = errors.New("a push relay is an https URL with a host, an optional path, and no user info, query or fragment")

// Endpoint builds the address of path (PathRegister or PathPush) under
// relay, the `push.relay` an authority document names: the value as written,
// less one trailing "/" of its path, then path. Two relays are the same when
// Endpoint gives the same address for both; nothing else about them is
// normalized, so the server, the app and the publisher's tooling compare the
// same strings. relay is an https URL with a host and a port, if any, from 1
// to 65535, an optional path, and no user info, query or fragment, of at most
// MaxRelay characters.
func Endpoint(relay, path string) (string, error) {
	if !strings.HasPrefix(path, "/") || len(relay) > MaxRelay || strings.ContainsAny(relay, "?#\\") {
		return "", ErrRelay
	}
	u, err := url.Parse(relay)
	if err != nil || u.Scheme != "https" || !strings.HasPrefix(relay, "https://") || u.Host == "" || u.Hostname() == "" ||
		u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", ErrRelay
	}
	if port := u.Port(); port != "" || strings.HasSuffix(u.Host, ":") {
		if p, err := strconv.ParseUint(port, 10, 16); err != nil || p == 0 {
			return "", ErrRelay
		}
	}
	for i := 0; i < len(relay); i++ {
		if relay[i] <= ' ' || relay[i] >= 0x7f {
			return "", ErrRelay
		}
	}
	return strings.TrimSuffix(relay, "/") + path, nil
}

// SameRelay reports whether a and b name the same relay: both valid, and
// Endpoint gives one address for both.
func SameRelay(a, b string) bool {
	x, err := Endpoint(a, PathPush)
	if err != nil {
		return false
	}
	y, err := Endpoint(b, PathPush)
	return err == nil && x == y
}
