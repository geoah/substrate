// Package api is the substrate's HTTP surface: the versioned REST resource
// API, the registration and login doors, and the watch streams. It talks to
// the store only through the substrate.Service contract.
package api

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/appsetup"
	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/substrate"
)

// APIVersion is the ONE served API version; it versions the
// transport, the filter grammar and the mutations, never the kinds. There is
// no alias prefix: nothing shipped under an earlier one, so nothing is owed a
// sunset window.
const APIVersion = "v1"

// corePackage is the core PACKAGE: the substrate's own machinery kinds, the
// trigger and run vocabulary among them, so a resource's operational verbs
// hang off core beside the records they act on. coreAuthorityName is the
// authority half of it, which a KindInfo carries beside the package's own
// word.
const (
	coreAuthorityName = "substrate.reamde.dev"
	corePackage       = coreAuthorityName + "/core"
	llmPackage        = coreAuthorityName + "/llm"
)

// kindBundleIdentity is the bundle kind's reference. A PATCH of a bundle record
// runs its lifecycle transition (disable/enable/uninstall/purge) instead of a
// generic property write, because those are the substrate-owned state the
// lifecycle ops maintain behind the engine's exclusive fence (decision 0019).
const kindBundleIdentity = corePackage + "/bundle"

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
	// fallback redirect target. Empty posts to the page's own origin, where
	// this server serves the console, and renders no redirect.
	ConsoleURL string
	// InviteCode gates the ONE door into a fresh substrate: set, the register
	// endpoints admit only a request that presents it. EMPTY, THE DOOR READS
	// NO CODE — anyone who reaches it may create a repository — and discovery
	// reports `registration.inviteRequired: false` so a client hides the
	// field instead of asking for something nothing reads. The local
	// substrate is the one deployment meant to run that way.
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
	// requests per (client IP, repository), per repository and globally; zero
	// selects the default.
	AuthInterval time.Duration
	// Metrics mounts the Prometheus exposition at GET /metrics, unauthenticated
	// and DB-free like /healthz (SUBSTRATE_METRICS=1). The instruments record
	// either way; this decides only whether the port SERVES them. Off, the
	// path is unrouted and falls through like any other unknown one. A
	// deployment that turns it on keeps the path off its ingress: it is for
	// the scraper on the pod network, not the internet.
	Metrics bool
	// Draining closes when the server starts to shut down. The watch streams
	// and the export never end on their own, and http.Server.Shutdown waits
	// for every open request, so they end when it closes: a watch after
	// flushing what it has (its client resumes from its cursor), an export
	// cut short. Nil never closes.
	Draining <-chan struct{}
	// ClientDocuments reads an app's client ID metadata document for the
	// authorization flow (decision 0194). Nil is the real fetcher, which
	// dials public addresses alone; a test supplies documents from memory.
	ClientDocuments clientDocuments
	// PublicURL is this server's origin as apps reach it
	// (SUBSTRATE_PUBLIC_URL): the issuer of the authorization flow whatever
	// Host a request names. Empty derives the issuer from the Host.
	PublicURL string
}

type handler struct {
	svc          substrate.Service
	now          func() time.Time
	authRate     *rateLimiter
	inviteCode   string
	totpDisabled bool
	catalog      *catalog.Catalog
	consoleURL   string
	draining     <-chan struct{}
	// The authorization flow's state, all of it in memory: the parked
	// requests and codes, the document fetcher, and the per-address pace on
	// the three bearer-less endpoints, kept apart from the door's limiter so
	// a flood here costs no owner a login.
	authz     *authorize.Store
	fetcher   clientDocuments
	parkRate  *rateLimiter
	publicURL string
	// setups runs approvals' set ups, one per repository at a time, and
	// checks is the last set up check each token got (appsetup.go).
	setups *appsetup.Runner
	checks checkMemo
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
		draining:     cfg.Draining,
		authz:        authorize.NewStore(now),
		fetcher:      cfg.ClientDocuments,
		parkRate:     newRateLimiter(interval, now),
		publicURL:    strings.TrimRight(cfg.PublicURL, "/"),
		setups:       appsetup.NewRunner(cfg.Catalog),
	}
	if h.fetcher == nil {
		h.fetcher = authorize.NewFetcher(now)
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
	//
	// securityHeaders is first so that every response carries them, the
	// router's own 404 and 405 and a recovered panic's 500 among them. cors
	// is next, for the same reason, and because it answers a preflight before
	// the router can refuse the OPTIONS method.
	// requestID sits outside recoverer so a panic's log line names the
	// request, and both sit inside httpMetrics so a recovered panic is
	// counted as the 500 it answered.
	r.Use(securityHeaders, cors, httpMetrics, peerAddress, requestID, recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if cfg.Metrics {
		// Beside /healthz and outside every prefix: the scraper reads it with
		// no token, so it must never sit under the bearer check — and a
		// deployment must never route it from outside (operations.md).
		r.Get("/metrics", metrics.Handler().ServeHTTP)
	}

	// GET /.well-known/substrate/server.json is discovery: unversioned,
	// unauthenticated, and DB-free like /healthz. Its well-known path is what
	// lets a caller ask "is this domain a substrate at all" before it knows
	// anything else.
	r.Get("/.well-known/substrate/server.json", h.getDiscovery)
	// App authorization (decision 0194), beside the door: the two metadata
	// documents a standard OAuth client reads (RFC 8414, RFC 9728), the
	// authorization endpoint the browser is sent to, and the two endpoints
	// the app itself calls. None takes a bearer; every answer carries a
	// credential or the way to one, so none may be stored.
	r.Get(asMetadataPath, h.getAuthorizationServerMetadata)
	r.Get(prMetadataPath, h.getProtectedResourceMetadata)
	r.Group(func(r chi.Router) {
		r.Use(noStore)
		r.Get("/oauth/authorize", h.getAuthorize)
		r.Post("/oauth/token", h.postOAuthToken)
		r.Post("/oauth/revoke", h.postOAuthRevoke)
	})

	// The door, BESIDE the API and outside every version prefix. No repository
	// segment: registration has none yet, and everything after it takes one
	// from the token. The console owns the GET side of these paths — its
	// /login and /register pages — which is why an unmatched METHOD here falls
	// through to the SPA below. Every route in this group takes or hands back
	// a credential (a password, a TOTP seed, a bearer secret, the recovery
	// key), so none of its responses may be stored.
	r.Group(func(r chi.Router) {
		r.Use(noStore)
		r.Post("/register/enroll", h.postRegisterBegin)
		r.Post("/register", h.postRegister)
		r.Post("/login", h.postLogin)
		// The credential changes carry the password-factor rule: both current
		// factors in the body, a bearer token refused. They are
		// unauthenticated in the bearer sense on purpose.
		r.Post("/password", h.postPassword)
		r.Post("/totp/enroll", h.postTOTPBegin)
		r.Post("/totp", h.postTOTP)
		// Tokens: minting and revoking need a token already, so these sit
		// behind the ordinary bearer check. Revoking is a record delete either
		// way — the same write the generic surface performs.
		r.Group(func(r chi.Router) {
			r.Use(h.requireAuth)
			r.Post("/tokens", h.postMintToken)
			r.Get("/tokens", h.getTokens)
			r.Delete("/tokens/{id}", h.deleteToken)
		})
	})
	// The public webhook door (decision 0045), beside the other bearer-less
	// routes and outside /api so no kind can shadow it. The path names the
	// repository's authority and the trigger; the trigger's own key, when it
	// declares one, is the credential (the trailing segment, `?key=` or a
	// bearer header). A GET falls through to the SPA like /login does.
	r.Post("/webhooks/{authority}/{trigger}", h.postWebhook)
	r.Post("/webhooks/{authority}/{trigger}/{key}", h.postWebhook)

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

// mountResources builds the versioned resource tree under /api/v1 (decision
// 0033). Three groups: non-record endpoints at the version root, core-record
// actions and sub-resources, and the generic record surface last.
func (h *handler) mountResources(r chi.Router) {
	{
		// The OAuth callback carries no bearer: the provider redirects the
		// browser here, and the HMAC-signed state IS the authentication. It
		// sits at the version root, not under an authority, so no kind can
		// shadow it. The state and the code are one-time, and the page names
		// the connected record, so no copy of it may be stored.
		r.With(noStore).Get("/oauth/callback", h.getOAuthCallback)

		// The two bearer routes whose answer is itself a secret. noStore
		// wraps the bearer check, so its 401 is not stored either.
		r.Group(func(r chi.Router) {
			r.Use(noStore, h.requireAuth)
			// The owner's recovery export: the repository directory as of
			// one committed point, as a tar (decision 0069).
			r.Get(exportRoute, h.getExport)
			// The host OAuth facility's authenticated half: start a connect
			// flow for an account record. The consent URL it answers carries
			// the signed state, which is that flow's credential.
			r.Post("/oauth/start", h.postOAuthStart)
		})

		r.Group(func(r chi.Router) {
			r.Use(h.requireAuthAllowingApps)
			// THE SCOPED SURFACE (decision 0194): the only routes an app's
			// token reaches, each held to its scopes by the scopedDataset the
			// bearer check hands it. Every other route sits behind
			// requireAuth, which refuses that token before its handler runs.
			//
			// THE RECORDS ROUTE: every list, the ranked read and the tail, one
			// grammar (records.go); and the body-addressed create.
			r.Get(recordsRoute, h.getRecords)
			r.Post(recordsRoute, h.postRecords)
			// The repository's unit graph, and one conversion through it
			// (units.go).
			r.Get(unitsRoute, h.getUnits)
			r.Get(unitsConvertRoute, h.getUnitsConvert)
			// The change feed: an app's token reaches its live tail alone,
			// held to the records it reads (watch.go, decision 0195).
			r.Get("/changes", h.getChanges)
			// Chat with an agent and reach its running turn: an app's token
			// reaches them for the agents its `agents:` scopes name, and
			// never the call API beside them (decision 0195). Five segments,
			// so the record path below never shadows them.
			r.Post("/"+corePackage+"/agent/{name}/chat", h.postAgentChat)
			r.Get("/"+llmPackage+"/thread/{id}/events", h.getThreadEvents)
			r.Post("/"+llmPackage+"/thread/{id}/stop", h.postThreadStop)
			// A webhook's parked deliveries, for the trigger an app's
			// `webhook:` scope names (docs/plans/app-setup.md); the rest
			// of the trigger verbs are the owner's (mountTriggerVerbs).
			r.Get("/"+corePackage+"/trigger/{id}/parked", h.getTriggerParked)
			// Storing a blob, for a token that writes a kind declaring a
			// blob-ref property (scopedDataset.PutBlob). Reading one stays
			// the owner's, below.
			r.Put("/blobs", h.putBlob)
			r.Put("/blobs/{digest}", h.putBlob)
			// Whether an app is still set up, asked with its own token
			// (appsetup.go). The static segment wins over the owner's
			// /authorizations/{request} below, whichever group holds it.
			r.With(noStore).Post("/authorizations/check", h.postAuthorizationCheck)

			// THE RECORD PATH. It IS the record's reference: the kind's three
			// segments ({authority}/{package}/{kind}, decisions 0042, 0047) and
			// the id. Nothing binds a shorter path: there is no collection
			// route (every list is /records), so an unknown word answers the
			// router's JSON 404 and a wrong method on a reserved word its JSON
			// 405, never the console's index.html.
			r.Get("/{a1}/{a2}/{a3}/{a4}", h.getResource)
			r.Put("/{a1}/{a2}/{a3}/{a4}", h.putResource)
			r.Patch("/{a1}/{a2}/{a3}/{a4}", h.patchResource)
			r.Delete("/{a1}/{a2}/{a3}/{a4}", h.deleteResource)
		})

		r.Group(func(r chi.Router) {
			r.Use(h.requireAuth)
			// The approval page's three calls: an app's token never
			// approves scopes, its own or another app's (authorize.go).
			// An approval's answer carries a code, so none of the three may be
			// stored.
			r.With(noStore).Get("/authorizations/{request}", h.getAuthorization)
			r.With(noStore).Post("/authorizations/{request}", h.postAuthorization)
			r.With(noStore).Delete("/authorizations/{request}", h.deleteAuthorization)

			// NON-RECORD ENDPOINTS, at the version root. Each is repository-wide
			// and names no kind, so it leaves the kind namespace entirely: a
			// two-segment kind reference can never collide with a one-segment
			// reserved word, which is what lets them drop their authority prefix
			// without a separator, and it keeps the
			// `substrate.reamde.dev/core/recordmerge` and `/recordsplit`
			// kinds reachable while merge and split sit here (#202).

			// The batch vocabulary verb (a declaration is a record): every
			// document admitted or none, one transaction, activation on commit.
			// It stays distinct because a batch of declarations is not record
			// data and the vocabulary apply admission is not the generic write path.
			r.Post("/vocabulary/apply", h.applyVocabulary)
			// The apply's preview: the guard lines it would refuse on and the
			// conversion plan it would run, with the hash and changelog head a
			// lossy plan's confirmation names (decision 0067). Writes nothing.
			r.Post("/vocabulary/plan", h.planVocabulary)
			// The boot upgrade's preview: what this binary would move in the
			// shipped packages here, and the guard lines it refused on.
			r.Get("/vocabulary/upgrade", h.getVocabularyUpgrade)
			// Merge joins two records of one kind; split reverses a merge.
			// Creating the command record performs the operation, so these are
			// actions, not the recordmerge/recordsplit collections.
			r.Post("/merge", h.postMerges)
			r.Post("/split", h.postSplits)
			// The catalog: the bundle closures shipped in the binary. List and
			// detail are repository reads (installed reflects this
			// repository); the two writes are the two TIERS' doors (decision
			// record 0048), both owner actions applying the closure through
			// the same vocabulary-apply admission path. `install` takes a
			// PROVIDER under the authority that publishes it; `import` takes a
			// SAMPLE under this repository's own authority.
			r.Get("/catalog", h.getCatalog)
			r.Get("/catalog/{id}", h.getCatalogItem)
			r.Post("/catalog/{id}/install", h.postCatalogInstall)
			r.Post("/catalog/{id}/import", h.postCatalogImport)
			// The content-addressed blob store: stream bytes back by digest,
			// repository-scoped (the PUTs that store them sit on the scoped
			// surface above). A blob's manifest is an ordinary `blob` record
			// under /substrate.reamde.dev/core/blob.
			r.Get("/blobs/{digest}", h.getBlob)

			// CORE-RECORD ACTIONS AND SUB-RESOURCES. An ACTION hangs one level
			// below the record ({name}/call, {id}/bind), where no id can sit, so
			// a record whose id is `call` or `bind` stays addressable. A
			// COLLECTION-level verb (`bundle/status`, `trigger/status`) does sit
			// where an id would, so it shadows a record with that id — but the
			// bundle and trigger kinds are system-managed and refuse a generic
			// write, so no unreadable row can land there.
			//
			// Trigger delivery bookkeeping: status is computed, a replay is a
			// cursor reset, a run is one synthesized delivery, a wake is an
			// immediate scan. Triggers are substrate.reamde.dev/core records, so
			// the verbs live at that record.
			h.mountTriggerVerbs(r, corePackage)
			// Cross-kind, so at the version root like /records: one row per
			// record of any kind binding the core `sync` trait.
			r.Get("/sync/status", h.getSyncStatus)
			// Bundle status is computed; enable/disable, uninstall and purge are
			// runtime STATE the substrate owns, reached by a PATCH of the bundle
			// record (patchBundleLifecycle) rather than verbs. `bind` resolves
			// an input to a record and is not a lifecycle state, so it stays a
			// sub-path.
			bundle := "/" + corePackage + "/bundle"
			r.Get(bundle+"/status", h.getBundleStatuses)
			r.Get(bundle+"/{id}/status", h.getBundleStatus)
			r.Post(bundle+"/{id}/bind", h.postBundleBind)
			// Traits as host-recognized interfaces: the kinds implementing one.
			// Their records are `GET /records?filter={"implements": …}`.
			trait := "/" + corePackage + "/trait/{id}"
			r.Get(trait+"/implementors", h.getTraitImplementors)
			// The callable invocation API: manual invoke with arbitrary input.
			r.Post("/"+corePackage+"/function/{name}/call", h.postFunctionCall)
			// Agents: the same call API. Chat, the one loop streaming, and
			// the thread routes that reach a detached turn again (decision
			// 0169) sit on the scoped surface above.
			r.Post("/"+corePackage+"/agent/{name}/call", h.postAgentCall)
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
// `/kinds/samples.substrate.reamde.dev/people`), so a dot on its own may never be read
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
// plain 404 into a MIME-type parse error the app cannot report on.
func spaHandler(dir string) http.HandlerFunc {
	root := filepath.Clean(dir)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	index := filepath.Join(root, "index.html")
	gz := newGzipAssets()
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
				if gz.serve(w, r, file, info) {
					return
				}
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

// gzipExts are the asset types worth compressing: text a browser parses.
// Fonts and images are compressed formats already. Each has a type in Go's
// own table, because a gzip body cannot be sniffed for one.
var gzipExts = map[string]bool{
	".js": true, ".mjs": true, ".css": true,
	".svg": true, ".json": true, ".wasm": true,
}

const (
	// gzipMaxFile is the largest asset compressed. The console's biggest
	// chunk is about 2 MB; a file far past that is not a console asset worth
	// holding in memory twice.
	gzipMaxFile = 16 << 20
	// gzipMaxHeld bounds the compressed copies held at once. A rebuild under
	// a running server mints new chunk names and the old copies are never
	// asked for again, so past the bound every held copy is dropped and the
	// live names are compressed afresh.
	gzipMaxHeld = 64 << 20
)

// gzipAssets serves a gzip copy of the console's content-hashed assets,
// compressed once per file and held in memory. It is not middleware: the
// watch and chat streams flush per event and must reach the client as they
// are written, and index.html is small and never cached.
type gzipAssets struct {
	mu   sync.Mutex
	held map[string]*gzipAsset
	// size is the sum of every held asset's counted bytes.
	size int
	// compress and maxHeld are gzipFile and gzipMaxHeld; a test swaps them.
	compress func(file string, info os.FileInfo) ([]byte, error)
	maxHeld  int
}

// gzipAsset is one file's compressed copy, keyed by the stat it was read
// under so a file replaced in place is compressed again. done closes once
// body and err are set: every request that arrives meanwhile waits on the one
// compression rather than reading and compressing the file again, so a burst
// of cold requests costs one copy of the file, not one per request. A nil
// body is a file gzip did not shrink.
type gzipAsset struct {
	modTime time.Time
	size    int64
	done    chan struct{}
	body    []byte
	err     error
	// counted is what this asset adds to gzipAssets.size, under its mu. The
	// compressing request alone writes body, and only before done closes, so
	// nothing else may read body without waiting on done.
	counted int
}

func newGzipAssets() *gzipAssets {
	return &gzipAssets{held: map[string]*gzipAsset{}, compress: gzipFile, maxHeld: gzipMaxHeld}
}

// serve answers r with the gzip copy of file when the client accepts gzip
// and reports whether it did; on false the caller serves the file as it is.
func (g *gzipAssets) serve(w http.ResponseWriter, r *http.Request, file string, info os.FileInfo) bool {
	// ServeFile refuses a URL path with a `..` segment; so does this, by
	// leaving the request to it.
	if slices.Contains(strings.Split(r.URL.Path, "/"), "..") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(file))
	ctype := mime.TypeByExtension(ext)
	if !gzipExts[ext] || ctype == "" || info.Size() > gzipMaxFile {
		return false
	}
	// Both answers carry Vary, so a shared cache never hands the compressed
	// body to a client that did not ask for it.
	w.Header().Add("Vary", "Accept-Encoding")
	if !acceptsGzip(strings.Join(r.Header.Values("Accept-Encoding"), ",")) {
		return false
	}
	body, err := g.body(file, info)
	if err != nil || body == nil {
		return false
	}
	// Set before ServeContent, so it never sniffs the type off gzip bytes.
	w.Header().Set("Content-Type", ctype)
	// The copy is served whole. A range of it would be a range of gzip bytes,
	// and several ranges a multipart body under a gzip Content-Encoding no
	// client can decode; ignoring Range is always a valid answer, and a 200
	// with the whole body is what a browser asks a script for anyway.
	if r.Header.Get("Range") != "" {
		r = r.Clone(r.Context())
		r.Header.Del("Range")
		r.Header.Del("If-Range")
	}
	http.ServeContent(gzipBody{w}, r, file, info.ModTime(), bytes.NewReader(body))
	return true
}

// gzipBody marks ServeContent's 200 as the gzip copy, and only the 200: a
// 304 or a 412 carries no body, so a Content-Encoding there names bytes that
// never come. ServeContent sets the 200's Content-Length itself while
// Content-Encoding is still unset. It drops the Accept-Ranges ServeContent
// always sets, because the copy is served whole.
type gzipBody struct{ http.ResponseWriter }

func (w gzipBody) WriteHeader(code int) {
	w.Header().Del("Accept-Ranges")
	if code == http.StatusOK {
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.ResponseWriter.WriteHeader(code)
}

// body returns file's compressed copy, compressing it on the first request
// under this stat.
func (g *gzipAssets) body(file string, info os.FileInfo) ([]byte, error) {
	g.mu.Lock()
	held := g.held[file]
	if held != nil && held.size == info.Size() && held.modTime.Equal(info.ModTime()) {
		g.mu.Unlock()
		<-held.done
		return held.body, held.err
	}
	if held != nil {
		g.size -= held.counted
	}
	asset := &gzipAsset{modTime: info.ModTime(), size: info.Size(), done: make(chan struct{})}
	g.held[file] = asset
	g.mu.Unlock()

	func() {
		// Closed even if compress panics, so no waiter hangs on it.
		defer close(asset.done)
		asset.body, asset.err = g.compress(file, info)
	}()

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held[file] != asset {
		return asset.body, asset.err
	}
	if asset.err != nil {
		// A failed read is not remembered: the next request tries again.
		delete(g.held, file)
		return nil, asset.err
	}
	if g.size+len(asset.body) > g.maxHeld {
		// Drop every copy held. An asset still compressing counts nothing
		// yet and stays, so the requests waiting on it, and the next ones,
		// still share its one compression.
		for name, other := range g.held {
			if other.counted > 0 {
				delete(g.held, name)
			}
		}
		g.size = 0
	}
	asset.counted = len(asset.body)
	g.size += asset.counted
	return asset.body, nil
}

// errAssetChanged is a file that no longer matches the stat its request
// read: the copy would carry the old Last-Modified, so the request serves
// the file as it is now instead.
var errAssetChanged = errors.New("asset changed while it was compressed")

// gzipFile compresses one file, or answers nil when gzip does not shrink it.
// The bytes it reads must be the ones info describes, before and after the
// read, and no more than gzipMaxFile of them.
func gzipFile(file string, info os.FileInfo) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	same := func() bool {
		now, err := f.Stat()
		return err == nil && now.Size() == info.Size() && now.ModTime().Equal(info.ModTime())
	}
	if !same() {
		return nil, errAssetChanged
	}
	raw, err := io.ReadAll(io.LimitReader(f, gzipMaxFile+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != info.Size() || !same() {
		return nil, errAssetChanged
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if buf.Len() >= len(raw) {
		return nil, nil
	}
	return buf.Bytes(), nil
}

// acceptsGzip reports whether an Accept-Encoding value admits gzip: named
// (or as x-gzip) with a non-zero weight, or else covered by a `*` with one
// (RFC 9110, section 12.5.3). A weight that does not parse admits nothing.
func acceptsGzip(header string) bool {
	gzipQ, anyQ := -1.0, -1.0
	for part := range strings.SplitSeq(header, ",") {
		coding, params, _ := strings.Cut(part, ";")
		q := 1.0
		for param := range strings.SplitSeq(params, ";") {
			key, value, ok := strings.Cut(param, "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				f = 0
			}
			q = f
		}
		switch strings.ToLower(strings.TrimSpace(coding)) {
		case "gzip", "x-gzip":
			gzipQ = max(gzipQ, q)
		case "*":
			anyQ = max(anyQ, q)
		}
	}
	if gzipQ >= 0 {
		return gzipQ > 0
	}
	return anyQ > 0
}
