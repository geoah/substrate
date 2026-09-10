package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// The deterministic id algorithm, in Go: the MIRROR of host.py's `ids.external`
// and `ids.url`, which is what a function body actually calls. A body composes
// the ids of what it writes, so a harvest that runs twice writes one record;
// the engine's tests need the same id the body will compute in order to assert
// what landed, and that is what these two functions are for. Nothing in the
// serving path calls them.
//
// The two implementations are held together from both ends: testdata/id_vectors.json
// pins these functions to values host.py minted (ids_test.go), and the engine's
// bundle tests compare a Python body's actual writes against ExternalID here,
// so a divergence fails a suite rather than silently splitting one record in
// two.

// ExternalID is a stable id for one external record: provider + account + its
// id. The provider becomes a human-readable slug in front of the digest;
// hashing the whole key removes the truncate-a-URL collision foot-gun.
func ExternalID(provider, account, externalID string) string {
	slug := clip(slugify(provider), 48)
	digest := idHash(provider, account, externalID)
	if slug == "" {
		return digest
	}
	return slug + "-" + digest
}

// URLID is a stable id for one URL. SAFE V1: the exact URL is hashed with only
// surrounding-ASCII-whitespace trimming — NO canonicalization, so distinct
// spellings (case, default port, trailing slash, query/fragment bytes) are
// DISTINCT ids by design; a lossy normalizer that merged `?next=/` with
// `?next=` would silently drop one page. A structural canonicalizer, when
// needed, arrives as a separate, clearly named helper.
func URLID(rawurl string) string {
	u := strings.Trim(rawurl, " \t\n\r\f\v")
	slug := clip(slugify("url"), 48)
	digest := idHash("url", "", u)
	if slug == "" {
		return digest
	}
	return slug + "-" + digest
}

// idHash length-prefixes each part so ("ab","c") and ("a","bc") can never
// collide — the foot-gun a truncated URL slug walked straight into.
func idHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		b := []byte(p)
		h.Write([]byte(strconv.Itoa(len(b))))
		h.Write([]byte(":"))
		h.Write(b)
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// slugify lowercases (ASCII only — no Unicode folding) and keeps ONLY ASCII
// [a-z0-9], collapsing every other run to one dash. ASCII-only so the slug is
// byte-identical to host.py's: a Unicode letter or digit is DROPPED, both
// because Python drops it and because the engine's id alphabet is ASCII.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// clip trims to n bytes; the slug is ASCII, so a byte clip is a rune clip and
// stays byte-identical to host.py's code-point clip.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
