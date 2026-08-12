// Package api is the substrate's HTTP surface: the versioned REST resource
// API, the GraphQL endpoint, the bootstrap/tenancy endpoints, and the watch
// streams. It talks to the store only through the substrate.Service
// contract.
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/geoah/substrate/internal/catalog"
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
	// MaxDialect is the binary's maximum schema dialect, reported
	// by GET /api discovery. GET /api touches no repository, so it surfaces the
	// binary max only: a repository's STORED dialect lives in that repository's own
	// `vocabulary_dialect` table and is served nowhere. Zero when unset.
	MaxDialect int
	// InviteCode is the ONE door into a fresh substrate: registering with it
	// creates a user and their one repository. EMPTY TURNS REGISTRATION OFF —
	// the endpoints answer `unsupported`, exactly like any capability this
	// deployment lacks.
	InviteCode string
	// Now is an optional clock seam for the auth rate limiter and lockout.
	Now func() time.Time
	// AuthInterval is the minimum spacing between unauthenticated auth
	// requests per (client IP, username), per username and globally; zero
	// selects the default.
	AuthInterval time.Duration
}

type handler struct {
	svc        substrate.Service
	now        func() time.Time
	authRate   *rateLimiter
	authLock   *lockout
	inviteCode string
	catalog    *catalog.Catalog
	consoleURL string
	maxDialect int

	schemaMu    sync.Mutex
	schemaCache map[string]*cachedSchema
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
		svc:         cfg.Service,
		now:         now,
		authRate:    newRateLimiter(interval, now),
		authLock:    newLockout(now),
		inviteCode:  cfg.InviteCode,
		catalog:     cfg.Catalog,
		consoleURL:  strings.TrimRight(cfg.ConsoleURL, "/"),
		maxDialect:  cfg.MaxDialect,
		schemaCache: map[string]*cachedSchema{},
	}

	r := chi.NewRouter()
	// peerAddress runs before RealIP: the rate limiter must key on the
	// transport peer, which no request header can vary.
	r.Use(peerAddress, middleware.RequestID, middleware.RealIP, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// GET /api is discovery: unversioned, unauthenticated, and DB-free like
	// /healthz. It replaces 501-style feature detection.
	r.Get("/api", h.getDiscovery)

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
// console: the discovery endpoint itself and everything under a versioned
// prefix. It matches on the segment boundary, so a console route that merely
// STARTS with the letters (/apiary) is still the SPA's.
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
			r.Post("/"+coreAuthority+"/bundles/{id}/disable", h.postBundleVerb(bundleOps.DisableBundle))
			r.Post("/"+coreAuthority+"/bundles/{id}/enable", h.postBundleVerb(bundleOps.EnableBundle))
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

// spaHandler serves the built web app with an index.html fallback so
// client-side routes deep-link correctly.
func spaHandler(dir string) http.HandlerFunc {
	fs := http.FileServer(http.Dir(dir))
	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	}
}
