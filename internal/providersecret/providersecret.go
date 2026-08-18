// Package providersecret takes a repository's provider bearer back out of an
// endpoint's own words. A wire client is built from one resolved llmprovider
// row's key, and on a 401 the endpoint quotes the bearer it refused, so an
// error returned verbatim would carry that repository's key into a log, a
// record or an API response. Both wire clients need this (internal/llm buys
// completions, internal/embed buys embeddings), so the scrub lives here once.
package providersecret

import (
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
