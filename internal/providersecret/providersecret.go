// Package providersecret takes a repository's provider bearer back out of an
// endpoint's own words. A wire client is built from one resolved llm/provider
// row's key, and on a 401 the endpoint quotes the bearer it refused, so an
// error returned verbatim would carry that repository's key into a log, a
// record or an API response. Both wire clients need this (internal/llm buys
// completions, internal/embed buys embeddings), so the scrub lives here once,
// and so does the mark that tells a refused key from every other failure.
package providersecret

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// redacted stands in for whatever was removed, in both shapes.
const redacted = "<redacted>"

// maskedSecret matches a token an endpoint starred out itself. A provider's
// 401 body does NOT always quote a real key whole: OpenAI keeps a leading and
// a trailing fragment and replaces the middle with asterisks, so an exact
// match removes nothing and both fragments reach the sink. Three asterisks in
// a row inside one whitespace-delimited token is that shape, and nothing else
// in a wire error legitimately looks like it.
var maskedSecret = regexp.MustCompile(`\S*\*{3,}\S*`)

// Scrub takes the row's own bearer back out of an endpoint's words, in the two
// shapes endpoints send: quoted whole (a short key, or a gateway that does not
// mask), and self-masked with the middle starred out.
//
// What it does NOT catch: an endpoint that echoes a bare fragment with no
// asterisks to mark it. Nothing distinguishes that from prose, so this narrows
// the leak, it does not license the sink: a provider error still does not
// belong anywhere a repository other than the key's owner can read.
func Scrub(apiKey, s string) string {
	if apiKey != "" {
		s = strings.ReplaceAll(s, apiKey, redacted)
	}
	return maskedSecret.ReplaceAllString(s, redacted)
}

// Truncate cuts an endpoint's words to at most limit bytes without splitting
// anything Scrub removes: Scrub matches only a whole key and a whole starred
// token, so a fragment the cut left would pass it. A cut inside a
// whitespace-delimited token moves back to that token's start, which drops a
// starred token or a key the cut lands in, and a key that straddles the cut
// anyway (one holding whitespace) is dropped whole. Every key that starts
// before the cut must be whole in s, so the caller passes limit+len(apiKey)
// bytes where the endpoint sent that many.
func Truncate(apiKey string, s []byte, limit int) []byte {
	if len(s) <= limit {
		return s
	}
	cut := limit
	if !isSpace(rune(s[cut])) {
		cut = bytes.LastIndexFunc(s[:cut], isSpace) + 1
	}
	key := []byte(apiKey)
	for moved := len(key) > 0; moved; {
		moved = false
		for i := max(0, cut-len(key)+1); i < cut; i++ {
			if bytes.HasPrefix(s[i:], key) {
				cut, moved = i, true
				break
			}
		}
	}
	return s[:cut]
}

// isSpace is the whitespace maskedSecret's \S excludes.
func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	}
	return false
}

// KeyRefusedError is an endpoint's refusal of the key a request carried
// (RefusesKey). The wire clients rebuild every provider error as scrubbed
// text, so Status is the one fact about the refusal a caller can read without
// parsing the endpoint's words.
type KeyRefusedError struct {
	Status int
	err    error
}

func (e *KeyRefusedError) Error() string { return e.err.Error() }
func (e *KeyRefusedError) Unwrap() error { return e.err }

// RefusesKey reports whether an endpoint's failure answer refused the key
// itself, from its status and its error's type and code. Every call on a key
// it refuses meets the same answer, so a caller may mark the key failing for
// every request it carries.
//
// A 401 is the endpoint failing to authenticate the request, whatever its
// body says. A 403 refuses the key only where the error names it:
// `authentication_error` (Anthropic's type) or `invalid_api_key` (OpenAI's
// code). Every other 403 refuses one request and leaves the key working for
// the rest: OpenAI's `unsupported_country_region_territory`, a project
// without access to a model, a restricted key missing a permission,
// Anthropic's `permission_error` on one resource, a gateway's moderation
// refusal.
func RefusesKey(status int, errType, errCode string) bool {
	switch status {
	case http.StatusUnauthorized:
		return true
	case http.StatusForbidden:
		return errType == "authentication_error" || errCode == "invalid_api_key"
	}
	return false
}

// ErrorFields reads the type and code of the error an endpoint's body
// carries, `{"error": {"type": ..., "code": ...}}` as OpenAI, Anthropic and
// the gateways that copy them send it. A field that is absent or not a
// string, or a body that does not parse, reads "".
func ErrorFields(body []byte) (errType, errCode string) {
	var answer struct {
		Error struct {
			Type json.RawMessage `json:"type"`
			Code json.RawMessage `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return "", ""
	}
	_ = json.Unmarshal(answer.Error.Type, &errType)
	_ = json.Unmarshal(answer.Error.Code, &errCode)
	return errType, errCode
}

// MarkKeyRefused returns err marked as the endpoint refusing the key with
// status, and err unchanged when status is 0. The caller decides the refusal
// (RefusesKey). The mark keeps err whole, so a caller marks an error it has
// already scrubbed, or one it rebuilds with Scrub before it leaves the
// client.
func MarkKeyRefused(status int, err error) error {
	if err == nil || status == 0 {
		return err
	}
	return &KeyRefusedError{Status: status, err: err}
}

// KeyRefusal is the status of the key refusal err carries, or 0 for none.
func KeyRefusal(err error) int {
	var refused *KeyRefusedError
	if errors.As(err, &refused) {
		return refused.Status
	}
	return 0
}
