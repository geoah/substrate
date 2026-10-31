package api

import (
	"context"
	"crypto/rand"
	"net/http"
)

// consolePolicy is the Content-Security-Policy every response carries unless
// its handler sets a narrower one (the OAuth return page does). The console
// is a built bundle served from this origin with no inline script, so scripts
// are held to 'self' with no exception. zod probes Function("") once at load
// and falls back to its interpreter when the policy refuses it, so a browser
// reports exactly one script-src violation per console load, and that one is
// expected.
//
// Styles need 'unsafe-inline': CodeMirror (through style-mod) and tiptap both
// insert <style> elements into the document head at runtime, and neither is
// given a nonce, because the shell is a static file and a nonce would mean
// templating it per response. An injected style can restyle the page but
// cannot run code.
//
// img-src and font-src take data: because the bundler inlines an asset under
// 4 KiB as a data URI. frame-ancestors 'none' is the reason the policy exists
// at all: the console's bearer token sits in same-origin localStorage, so no
// other origin may frame the console and drive it.
const consolePolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; object-src 'none'; base-uri 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"

// hstsPolicy asks a browser to reach this host over HTTPS alone for a year.
// It is sent on every response, TLS or not, because the server never sees the
// TLS: a terminator in front of it does (docs/operations.md), and a browser
// ignores the header on a plain-HTTP response, so a loopback laptop is not
// pinned to a scheme it cannot serve.
const hstsPolicy = "max-age=31536000; includeSubDomains"

// securityHeaders sets the browser-facing protections on every response,
// before the handler runs, so a 404, a panic's 500 and the SPA shell carry
// them as much as an API read does. A handler may replace one (the OAuth
// return page replaces the CSP) but never needs to add one.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", hstsPolicy)
		h.Set("Content-Security-Policy", consolePolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		// The legacy twin of frame-ancestors, for a browser that predates it.
		h.Set("X-Frame-Options", "DENY")
		// A console URL names a repository's records by id; a link out of it
		// must not hand that path to the site it opens.
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// corsMaxAge is how long, in seconds, a browser may reuse a preflight's
// answer. Browsers clamp it (Chromium to two hours), so a day means "as long
// as you allow".
const corsMaxAge = "86400"

// corsExposed are the response headers a cross-origin script may read beyond
// the CORS-safelisted ones: the ones this API sets for a client to act on.
const corsExposed = "ETag, Location, Retry-After, Content-Disposition, WWW-Authenticate, " + requestIDHeader

// cors lets a script on any origin call this server. The wildcard is safe
// only because nothing here authenticates by ambient credential: every
// authenticated route reads a bearer token the caller attaches itself, and no
// response sets a cookie. Access-Control-Allow-Credentials must therefore
// never be sent; a cookie-based session would turn this wildcard into a
// cross-site request forgery door.
//
// A preflight (OPTIONS carrying Access-Control-Request-Method) is answered
// here, before routing, because no route declares OPTIONS and the router
// would otherwise answer it with a 405 or the console shell. The requested
// headers are echoed rather than listed, so a new request header never needs
// a change here.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The authorization endpoint is a page the browser navigates to,
		// never a script's call, and RFC 9700 §2.6 keeps CORS off it.
		if r.Header.Get("Origin") == "" || r.URL.Path == "/oauth/authorize" {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE")
			if req := r.Header.Get("Access-Control-Request-Headers"); req != "" {
				h.Set("Access-Control-Allow-Headers", req)
			}
			h.Set("Access-Control-Max-Age", corsMaxAge)
			h.Add("Vary", "Access-Control-Request-Headers")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.Set("Access-Control-Expose-Headers", corsExposed)
		next.ServeHTTP(w, r)
	})
}

// noStore marks a route's every response, refusals included, as never to be
// written to any cache. It wraps each route that hands back a credential or
// takes one in: a password, a TOTP code or seed, a bearer secret, the recovery
// key, the OAuth state and the export archive. The header is set before the
// handler runs, so a rate-limit refusal or an auth failure carries it too.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// scriptNonce is a fresh CSP nonce for one response: 26 base32 characters
// (130 bits) from crypto/rand, an alphabet the CSP nonce grammar accepts.
func scriptNonce() string {
	return rand.Text()
}

// requestIDHeader carries the id the server minted for a request back on its
// response, so a client's report of a failure names the server's log line
// about it: every line requestLogAttrs builds carries the same id as
// `request`.
const requestIDHeader = "X-Request-Id"

// requestIDLen is the length of a request id: 16 base32 characters, 80
// random bits.
const requestIDLen = 16

// requestID mints a fresh random id for every request, binds it to the
// request's context and sets it on the response before the handler runs. An
// inbound X-Request-Id is ignored: an id the caller chose could repeat
// another request's and forge the join, and the id must carry nothing about
// the host or how many requests it has served.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := rand.Text()[:requestIDLen]
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

// requestIDFrom returns the request's id, empty outside the router.
func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}
