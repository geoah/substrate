package blobbytes

// AWS Signature Version 4, the four requests this backend makes and no more.
// It is written here rather than pulled in as an SDK because the whole surface
// is PUT, GET, HEAD, DELETE and one listing over bodies that are already in
// memory: no multipart, no streaming signer, no retry policy, no credential
// chain. Every S3-compatible endpoint speaks it, and the MinIO test proves it
// against a real one.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// emptyPayloadHash is sha256 of no bytes, which every request without a body
// signs.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

const (
	signAlgorithm = "AWS4-HMAC-SHA256"
	signService   = "s3"
	// The two time formats SigV4 uses: the basic ISO 8601 stamp on the
	// request, and the date alone in the credential scope.
	signTimeFormat = "20060102T150405Z"
	signDateFormat = "20060102"
)

// sign adds the date, payload-hash and Authorization headers. path is the
// canonical URI (already the exact path the request will send) and rawQuery is
// the exact query string it will send, so what is signed and what is sent
// cannot drift.
func (s *S3) sign(req *http.Request, path, rawQuery, payloadHash string) error {
	now := time.Now().UTC()
	stamp := now.Format(signTimeFormat)
	date := now.Format(signDateFormat)

	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if s.cfg.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", s.cfg.SessionToken)
	}

	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	headers := map[string]string{
		"host":                 req.URL.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           stamp,
	}
	if s.cfg.SessionToken != "" {
		signed = append(signed, "x-amz-security-token")
		headers["x-amz-security-token"] = s.cfg.SessionToken
	}
	sort.Strings(signed)

	var canonicalHeaders strings.Builder
	for _, h := range signed {
		canonicalHeaders.WriteString(h)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(headers[h]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(signed, ";")

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalPath(path),
		rawQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := date + "/" + s.cfg.Region + "/" + signService + "/aws4_request"
	stringToSign := strings.Join([]string{
		signAlgorithm,
		stamp,
		scope,
		hashHex([]byte(canonicalRequest)),
	}, "\n")

	key := signingKey(s.cfg.SecretAccessKey, date, s.cfg.Region)
	signature := hex.EncodeToString(hmacSHA256(key, []byte(stringToSign)))
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		signAlgorithm, s.cfg.AccessKeyID, scope, signedHeaders, signature))
	return nil
}

// signingKey derives the date/region/service key the signature is taken with.
// The secret itself never signs anything directly.
func signingKey(secret, date, region string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	k = hmacSHA256(k, []byte(region))
	k = hmacSHA256(k, []byte(signService))
	return hmacSHA256(k, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// canonicalPath escapes each path segment and leaves the separators alone. S3
// signs the path as sent, un-normalized, so this must not collapse `//` or
// resolve `..` — neither of which a checked digest or repository id can hold.
func canonicalPath(path string) string {
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = awsEscape(seg)
	}
	return strings.Join(segments, "/")
}

// canonicalQuery builds the query string SigV4 signs: parameters sorted by
// name, both halves escaped RFC 3986. The request sends this exact string, so
// there is nothing for the endpoint's own canonicalization to disagree with.
func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		vs := append([]string(nil), values[name]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, awsEscape(name)+"="+awsEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// awsEscape percent-encodes everything outside RFC 3986's unreserved set.
// net/url's escapers each differ from that set somewhere (a space becomes `+`,
// a `/` or a `~` is left alone), and a single character's difference is a
// signature mismatch, so this spells the set out.
func awsEscape(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0xf])
		}
	}
	return b.String()
}
