// Package api is the substrate's HTTP surface: the versioned REST resource
// API, the GraphQL endpoint, the bootstrap/tenancy endpoints, and the watch
// streams. It talks to the store only through the substrate.Service
// contract.
package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/gql"
	"github.com/geoah/substrate/internal/substrate"
)

// APIVersion is the ONE served API version; it versions the
// transport, the filter grammar and the mutations, never the kinds. There is
// no alias prefix: nothing shipped under an earlier one, so nothing is owed a
// sunset window.
const APIVersion = "v1"

// coreAuthority publishes the substrate's own machinery kinds — the trigger
// and run vocabulary among them, so a resource's operational verbs
// hang off core beside the records they act on.
const coreAuthority = "core.substrate.reamde.dev"

// Config wires the HTTP layer.
type Config struct {
	Service substrate.Service
	// WebDir is the built SPA served at /; empty disables static serving.
	WebDir string
	// Catalog is the read model over the bundle closures shipped in the
	// binary — the "available integrations" the /catalog endpoints list and
	// install. Nil serves an empty catalog.
	Catalog *catalog.Catalog
	// ConsoleURL is the console the OAuth callback return-page reports back to:
	// its scheme+host is the postMessage targetOrigin, its full base the
	// fallback redirect target. Empty (local dev) posts to "*" and renders no
	// redirect.
	ConsoleURL string
	// MaxDialect is the binary's maximum schema dialect, reported by
	// GET /.well-known/substrate/server.json discovery. That endpoint touches no
	// repository, so it surfaces the binary max only: a repository's STORED
	// dialect lives in that repository's own `vocabulary_dialect` table and is
	// served nowhere. Zero when unset.
	MaxDialect int
	// InviteCode is the ONE door into a fresh substrate: registering with it
	// creates a user and their one repository. EMPTY TURNS REGISTRATION OFF —
	// the endpoints answer `unsupported`, exactly like any capability this
	// deployment lacks, and discovery reports `registration.open: false`.
	InviteCode string
	// TOTPDisabled mirrors the service's own dev escape hatch
	// (SUBSTRATE_INSECURE_DISABLE_TOTP): the second factor is not verified, so
	// the door stops DEMANDING a code — the credential changes take the
	// password alone, and discovery reports `registration.totpRequired: false`
	// so a client hides the field instead of asking for something nothing
	// checks. It changes no verification: refusing is the service's job either
	// way.
	TOTPDisabled bool
	// Now is an optional clock seam for the auth rate limiter.
	Now func() time.Time
	// AuthInterval is the minimum spacing between unauthenticated auth
	// requests per (client IP, username), per username and globally; zero
	// selects the default.
	AuthInterval time.Duration
}

type handler struct {
	svc          substrate.Service
	now          func() time.Time
	authRate     *rateLimiter
	inviteCode   string
	totpDisabled bool
	catalog      *catalog.Catalog
	consoleURL   string
	maxDialect   int

	// schemas is the GraphQL schema cache, one entry per repository, rebuilt
	// on registry-fingerprint changes (internal/gql owns the key and builder).
	schemas *gql.Cache
}

// New builds the router.
func New(cfg Config) http.Handler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	interval := cfg.AuthInterval
	if interval == 0 {
		interval = defaultAuthInterval
	}
	h := &handler{
		svc:          cfg.Service,
		now:          now,
		authRate:     newRateLimiter(interval, now),
		inviteCode:   cfg.InviteCode,
		totpDisabled: cfg.TOTPDisabled,
		catalog:      cfg.Catalog,
		consoleURL:   strings.TrimRight(cfg.ConsoleURL, "/"),
		maxDialect:   cfg.MaxDialect,
		schemas:      gql.NewCache(),
	}

	r := chi.NewRouter()
	// NO middleware.RealIP. It rewrote RemoteAddr from X-Forwarded-For,
	// X-Real-IP or True-Client-IP whether or not anything in front of this
	// server sets them, so a caller could name its own address by sending a
	// header (GHSA-3fxj-6jh8-hvhx and friends; chi deprecated it in v5.3.0).
	// Nothing here wanted the rewrite: peerAddress records the TRANSPORT peer,
	// which is what the rate limiter keys on, and no other handler reads
	// RemoteAddr. Anything that needs a client address behind a proxy has to
	// name which proxies it trusts first.
	r.Use(peerAddress, middleware.RequestID, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// GET /.well-known/substrate/server.json is discovery: unversioned,
	// unauthenticated, and DB-free like /healthz. It replaces 501-style
	// feature detection, and its well-known path is what lets a caller ask
	// "is this domain a substrate at all" before it knows anything else.
	r.Get("/.well-known/substrate/server.json", h.getDiscovery)

	// The door, BESIDE the API and outside every version prefix. No repository
	// segment: registration has none yet, and everything after it takes one
	// from the token. The console owns the GET side of these paths — its
	// /login and /register pages — which is why an unmatched METHOD here falls
	// through to the SPA below.
	r.Post("/register/enroll", h.postRegisterBegin)
	r.Post("/register", h.postRegister)
	r.Post("/login", h.postLogin)
	// The credential changes carry the password-factor rule: both current
	// factors in the body, a bearer token refused. They are
	// unauthenticated in the bearer sense on purpose.
	r.Post("/password", h.postPassword)
	r.Post("/totp/enroll", h.postTOTPBegin)
	r.Post("/totp", h.postTOTP)
	// One-time recovery-key enrollment, for repositories that predate it:
	// registration is the ordinary door. It carries the password-factor rule
	// like the credential changes above: enrollment claims the repository's
	// only recovery slot and hands out an offline decryption key, so a
	// bearer token is not evidence here.
	r.Post("/recovery/enroll", h.postRecoveryEnroll)
	// Tokens: minting and revoking need a token already, so these sit behind
	// the ordinary bearer check. Revoking is a record delete either way — the
	// same write the generic surface performs.
	r.Group(func(r chi.Router) {
		r.Use(h.requireAuth)
		r.Post("/tokens", h.postMintToken)
		r.Get("/tokens", h.getTokens)
		r.Delete("/tokens/{id}", h.deleteToken)
	})

	r.Route("/api/"+APIVersion, h.mountResources)

	// An unmatched path is a 404 — but WHICH 404 depends on where it sits. A
	// path under an API prefix answers the standard JSON problem object with
	// `not_found`: a mistyped API path must be distinguishable from
	// a successful read, and serving the console's HTML with a 200 makes every
	// client's error handling a lie. Only NON-API paths fall through to the SPA,
	// whose deep links are the reason the fallback exists at all.
	fallback := http.NotFound
	if cfg.WebDir != "" {
		fallback = spaHandler(cfg.WebDir)
	}
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if isAPIPath(req.URL.Path) {
			writeError(w, http.StatusNotFound, codeNotFound, "no such API path: "+req.URL.Path)
			return
		}
		fallback(w, req)
	})
	// A wrong METHOD on a console route is the console's route, not an error:
	// `/login` and `/register` are POST here and PAGES there, so a GET on one
	// must serve the SPA rather than a 405 the browser cannot render. Under an
	// API prefix the method really is wrong, and says so.
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		if isAPIPath(req.URL.Path) {
			writeError(w, http.StatusMethodNotAllowed, codeBadRequest,
				req.Method+" is not supported at "+req.URL.Path)
			return
		}
		fallback(w, req)
	})
	return r
}

// isAPIPath reports whether a path addresses the API surface rather than the
// console: the bare /api segment and everything under it. It matches on the
// segment boundary, so a console route that merely STARTS with the letters
// (/apiary) is still the SPA's. Discovery sits at
// /.well-known/substrate/server.json, outside this surface entirely — a typo
// there falls through to the SPA fallback exactly as a typo'd /healthz would.
func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// mountResources builds the versioned resource tree under /api/v1.
func (h *handler) mountResources(r chi.Router) {
	{
		// The OAuth callback carries no bearer: the provider redirects the
		// browser here, and the HMAC-signed state IS the authentication.
		r.Get("/"+coreAuthority+"/oauth/callback", h.getOAuthCallback)

		r.Group(func(r chi.Router) {
			r.Use(h.requireAuth)

			r.Post("/graphql", h.postGraphQL)

			// The batch schema verb (schema is records): every document
			// admitted or none, one transaction, activation on commit.
			r.Post("/"+coreAuthority+"/vocabulary/apply", h.applyVocabulary)

			r.Post("/"+coreAuthority+"/recordmerges", h.postMerges)
			r.Post("/"+coreAuthority+"/recordsplits", h.postSplits)
			r.Get("/"+coreAuthority+"/changes", h.getChanges)

			// Trigger delivery bookkeeping: status is computed, a replay is
			// a cursor reset, a run is one synthesized delivery, a wake is
			// an immediate scan. Triggers are core.substrate.reamde.dev records, so the
			// verbs live at that resource — the substrate
			// maintains its own delivery plumbing, so it publishes it.
			h.mountTriggerVerbs(r, coreAuthority)
			// Bundle lifecycle: status is computed; disable/enable and
			// uninstall are reversible runtime state; purge tombstones the
			// owned authority's data via the finalizer flow. The bundle ROWS are
			// ordinary schema records under /core.substrate.reamde.dev/bundles.
			r.Get("/"+coreAuthority+"/bundles/status", h.getBundleStatuses)
			r.Get("/"+coreAuthority+"/bundles/{id}/status", h.getBundleStatus)
			r.Post("/"+coreAuthority+"/bundles/{id}/disable", h.postBundleVerb(substrate.BundleOps.DisableBundle))
			r.Post("/"+coreAuthority+"/bundles/{id}/enable", h.postBundleVerb(substrate.BundleOps.EnableBundle))
			r.Post("/"+coreAuthority+"/bundles/{id}/bind", h.postBundleBind)
			r.Post("/"+coreAuthority+"/bundles/{id}/uninstall", h.postBundleUninstall)
			r.Post("/"+coreAuthority+"/bundles/{id}/purge", h.postBundlePurge)
			// The catalog: the installable bundle closures shipped in the
			// binary. List and detail are repository reads (installed reflects
			// this repository); install is an owner action that applies the
			// closure through the same schema/apply admission path. Remote-URL
			// / versioned install is future.
			r.Get("/"+coreAuthority+"/catalog", h.getCatalog)
			r.Get("/"+coreAuthority+"/catalog/{id}", h.getCatalogItem)
			r.Post("/"+coreAuthority+"/catalog/{id}/install", h.postCatalogInstall)
			// Traits as host-recognized interfaces: the types implementing
			// one, and their records — the console's "account configs" view.
			r.Get("/"+coreAuthority+"/traits/{id}/implementors", h.getTraitImplementors)
			r.Get("/"+coreAuthority+"/traits/{id}/records", h.getTraitRecords)
			// The host OAuth facility's authenticated half: start a connect
			// flow for an account record.
			r.Post("/"+coreAuthority+"/oauth/start", h.postOAuthStart)
			// The content-addressed blob store: store bytes under
			// their digest, stream them back, both repository-scoped. A blob's
			// manifest is an ordinary `blob` record under /core.substrate.reamde.dev/blobs.
			r.Put("/blobs", h.putBlob)
			r.Put("/blobs/{digest}", h.putBlob)
			r.Get("/blobs/{digest}", h.getBlob)

			// The callable invocation API: manual invoke with arbitrary input.
			r.Post("/"+coreAuthority+"/functions/{name}/call", h.postFunctionCall)
			// Agents: the same call API, plus chat — the one loop streaming.
			r.Post("/"+coreAuthority+"/agents/{name}/call", h.postAgentCall)
			r.Post("/"+coreAuthority+"/agents/{name}/chat", h.postAgentChat)

			// The collection path IS the kind reference:
			// {authority}/{plural} for a published kind, {plural} alone for
			// a repository-local one. An authority is a DNS name and always
			// carries a dot; a plural never does, so a two-segment path is
			// unambiguous — `addressed` is the ONE place that reads the
			// difference, and every handler below takes its address from it.
			r.Get("/{a1}", h.listCollection)
			r.Post("/{a1}", h.createInCollection)

			r.Get("/{a1}/{a2}", h.getCollectionOrResource)
			r.Post("/{a1}/{a2}", h.createInCollection)
			r.Put("/{a1}/{a2}", h.putResource)
			r.Patch("/{a1}/{a2}", h.patchResource)
			r.Delete("/{a1}/{a2}", h.deleteResource)

			// Sub-resources hang off the record, at both address depths. A
			// static segment beats a parameter in chi's tree, so a record
			// whose id is literally `incoming` or `edges` is not addressable
			// here — the one corner this shape costs.
			r.Get("/{a1}/{a2}/incoming", h.getIncoming)
			r.Post("/{a1}/{a2}/edges/{rel}", h.linkEdge)
			r.Delete("/{a1}/{a2}/edges/{rel}", h.unlinkEdge)
			r.Get("/{a1}/{a2}/{a3}/incoming", h.getIncoming)
			// Edge mutation lives at the resource: link and unlink
			// an outgoing edge {rel} with an EdgeRef body. A put could add an
			// edge but never remove one; DELETE closes that gap.
			r.Post("/{a1}/{a2}/{a3}/edges/{rel}", h.linkEdge)
			r.Delete("/{a1}/{a2}/{a3}/edges/{rel}", h.unlinkEdge)

			r.Get("/{a1}/{a2}/{a3}", h.getResource)
			r.Put("/{a1}/{a2}/{a3}", h.putResource)
			r.Patch("/{a1}/{a2}/{a3}", h.patchResource)
			r.Delete("/{a1}/{a2}/{a3}", h.deleteResource)
		})
	}
}

// assetPrefix is where the console's build puts every content-hashed file, so
// it is both the one URL space that may be cached forever and the one that must
// never fall back to index.html.
const assetPrefix = "/assets/"

// assetExts are the extensions a MISSING path outside /assets/ must still 404
// on: the browser asked for code, and HTML is not a worse-shaped answer to that
// but an unrelated one ("'text/html' is not a valid JavaScript MIME type"
// instead of "404"). The set is deliberately narrow, both ways. A console
// route's last segment legitimately carries a dot (a kind reference,
// `/kinds/people.substrate.reamde.dev`), so a dot on its own may never be read
// as an extension; and a non-API path that ends `.json` is served to the
// console today (a wrong METHOD on `/.well-known/substrate/server.json` falls
// through here), so nothing but code belongs in the set.
var assetExts = map[string]bool{
	".js": true, ".mjs": true, ".cjs": true, ".css": true,
	".map": true, ".wasm": true,
}

const (
	// cacheImmutable is for the content-hashed files alone: the URL names that
	// exact body, so a new build is a new URL rather than a changed one.
	cacheImmutable = "public, max-age=31536000, immutable"
	// cacheNever is for index.html and every fallback of it. `no-cache` alone is
	// not enough: it still permits a CONDITIONAL reuse, and ServeFile's
	// Last-Modified has one-second resolution, so an index.html replaced within
	// the same second answers 304 and the tab keeps the dead chunk names this
	// handler exists to stop serving.
	cacheNever = "no-store, no-cache"
)

// spaHandler serves the built web app: a real file as itself, and any other
// path as index.html so client-side routes deep-link.
//
// A path that names a static asset is the ONE thing the fallback does not
// cover. A tab left open across a rebuild asks for the chunk hashes it was
// built with, and those files are gone: answering them with index.html turns a
// plain 404 into a MIME-type parse error the app cannot report on, which is
// exactly how the console's lazy YAML lens used to fail after a deploy.
func spaHandler(dir string) http.HandlerFunc {
	root := filepath.Clean(dir)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	index := filepath.Join(root, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		// A URL path never legitimately carries a backslash, and path.Clean does
		// not read one as a separator: left in, it reaches filepath as one on the
		// platforms where it IS one, and `/..\..\x.js` walks out of the directory
		// before the cleaning below has seen a thing.
		if strings.Contains(r.URL.Path, `\`) {
			http.NotFound(w, r)
			return
		}
		// ONE canonical path for both the stat and the serve. The two used to
		// disagree (a cleaned filesystem path decided, the raw URL was served),
		// and disagreeing decisions is how a handler answers about one file
		// while serving another.
		rel := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		// Confinement is checked BEFORE the stat, not left to ServeFile: an
		// answer that differs for a file outside the directory is an existence
		// oracle over the whole filesystem even when no byte of it is served.
		// filepath.IsLocal is the guard the scanner also credits: the cleaned
		// relative path may not escape, be absolute, or be empty.
		if rel != "/" && !filepath.IsLocal(strings.TrimPrefix(rel, "/")) {
			http.NotFound(w, r)
			return
		}
		file := filepath.Join(root, filepath.FromSlash(rel))
		if file != root && !strings.HasPrefix(file, root+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(file)
		switch {
		case err == nil && !info.IsDir():
			if strings.HasPrefix(rel, assetPrefix) {
				w.Header().Set("Cache-Control", cacheImmutable)
			} else {
				w.Header().Set("Cache-Control", cacheNever)
			}
			http.ServeFile(w, r, file)
		case strings.HasPrefix(rel, assetPrefix) || assetExts[strings.ToLower(path.Ext(rel))]:
			http.NotFound(w, r)
		default:
			// index.html names the current chunk hashes, so a stored copy is a tab
			// that cannot learn about a deploy.
			w.Header().Set("Cache-Control", cacheNever)
			http.ServeFile(w, r, index)
		}
	}
}
