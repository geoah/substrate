package synth

import (
	"crypto/sha1" //nolint:gosec // a file-name digest, as the mock server computes it
	"encoding/hex"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// The mock server (internal/providere2e/runner/mockserver.py) answers a
// request from a file named for it, and this is that rule in Go, so an
// emitter writes recordings the server will find. The rule, from the
// server's docstring: `<METHOD>_<path-slug>[__<query-slug>].json`; the path
// is percent-decoded, stripped of its outer slashes, every character outside
// `[A-Za-z0-9._-]` becomes `_` and runs of `_` collapse; the query pairs lose
// the secret parameters, sort by key then value, render `key-value` sanitized
// the same way and join with `_`; a query slug over 80 characters becomes
// its first 60 (right-trimmed of `_`), a `-` and the first 8 hex of its
// sha1. recording_test.go pins this against names the Python produced.

// SecretParams never reach a file name.
var SecretParams = map[string]bool{
	"access_token": true, "oauth_token": true, "token": true, "key": true, "api_key": true, "apikey": true,
	"client_secret": true, "code": true, "refresh_token": true, "id_token": true, "signature": true, "sig": true,
}

// PaginationParams are the pairs the server drops on its second try, so a
// page-one recording named without them answers a request that carries them.
// A page-two recording must therefore be named for a parameter NOT in this
// set (WHOOP's `nextToken`), or carry the exact query (Google Fit's
// `pageToken`, which the sync sends beside `limit`).
var PaginationParams = map[string]bool{
	"pageToken": true, "page_token": true, "nextPageToken": true, "cursor": true, "next_cursor": true,
	"page": true, "per_page": true, "offset": true, "after": true, "before": true, "startAt": true, "start": true,
	"maxResults": true, "max_results": true, "limit": true, "count": true, "oldest": true, "latest": true,
	"inclusive": true, "syncToken": true, "sync_token": true, "startIndex": true, "pageSize": true,
}

const slugMax = 80

var (
	unsafeRune = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	underRuns  = regexp.MustCompile(`_+`)
)

func sanitize(s string) string {
	return strings.Trim(underRuns.ReplaceAllString(unsafeRune.ReplaceAllString(s, "_"), "_"), "_")
}

// PathSlug is the path part of a recording name.
func PathSlug(path string) string {
	if u, err := url.PathUnescape(path); err == nil {
		path = u
	}
	s := sanitize(strings.Trim(path, "/"))
	if s == "" {
		return "root"
	}
	return s
}

// Pair is one query pair.
type Pair struct{ Key, Value string }

// QuerySlug is the query part of a recording name, empty when nothing
// survives the secret filter.
func QuerySlug(pairs []Pair) string {
	var kept []Pair
	for _, p := range pairs {
		if !SecretParams[p.Key] {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Key != kept[j].Key {
			return kept[i].Key < kept[j].Key
		}
		return kept[i].Value < kept[j].Value
	})
	parts := make([]string, 0, len(kept))
	for _, p := range kept {
		parts = append(parts, sanitize(p.Key+"-"+p.Value))
	}
	slug := strings.Join(parts, "_")
	if len(slug) > slugMax {
		sum := sha1.Sum([]byte(slug)) //nolint:gosec // a file-name digest, as the mock server computes it
		slug = strings.TrimRight(slug[:60], "_") + "-" + hex.EncodeToString(sum[:])[:8]
	}
	return slug
}

// RecordingName is the file that answers the request: the contract between
// an emitter and the mock server.
func RecordingName(method, path string, pairs ...Pair) string {
	name := strings.ToUpper(method) + "_" + PathSlug(path)
	if q := QuerySlug(pairs); q != "" {
		name += "__" + q
	}
	return name + ".json"
}

// Query turns url.Values into pairs.
func Query(v url.Values) []Pair {
	var out []Pair
	for k, vs := range v {
		for _, s := range vs {
			out = append(out, Pair{k, s})
		}
	}
	return out
}
