// Package engine implements substrate.Service and substrate.Dataset over
// Postgres: ONE schema shared by every repository, the seven mutations,
// machines, mapping recompute, search and the changelog.
//
// Isolation is enforced, not disciplined. Every
// repository-scoped table carries a `repository` column; a Scope opens the
// pool that pins it; row level security keyed on the connection's
// `substrate.repository` setting is what actually separates two repositories,
// so a query that forgets its repository refuses rather than leaks.
package engine

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/gql"
	"github.com/geoah/substrate/internal/oauthflow"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

type options struct {
	kindsFS   fs.FS
	kindsDir  string
	registry  *vocabulary.Registry
	oauthKey  string
	oauthURL  string
	oauthHTTP *http.Client
	credKey   string
	blobs     blobbytes.Backend
	log       *slog.Logger
	// insecureAllowSuperuser downgrades the fail-closed role check to a warning
	// (WithInsecureAllowSuperuser). Dev/test only; never the production default.
	insecureAllowSuperuser bool
	// insecureDisableTOTP stops verifying the second factor
	// (WithInsecureDisableTOTP). Dev/test only; never the production default.
	insecureDisableTOTP bool
}

// Option configures Open.
type Option func(*options)

// WithKindsFS loads the shipped schema manifests from fsys (every .yaml
// document under it, recursively).
func WithKindsFS(fsys fs.FS) Option { return func(o *options) { o.kindsFS = fsys } }

// WithKindsDir loads the shipped schema files from a directory.
func WithKindsDir(dir string) Option { return func(o *options) { o.kindsDir = dir } }

// WithRegistry supplies an already-loaded registry.
func WithRegistry(r *vocabulary.Registry) Option { return func(o *options) { o.registry = r } }

// WithLogger sets the logger background loops report through.
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.log = l } }

// WithOAuth enables the host OAuth facility: stateKey signs the flow state
// (HMAC), callbackURL is the one redirect URI providers send the browser
// back to, and hc — optional — is the HTTP client provider calls ride
// (tests point it at a fake provider). Without this option, StartOAuth
// refuses and the refresh loop is a no-op.
func WithOAuth(stateKey, callbackURL string, hc *http.Client) Option {
	return func(o *options) {
		o.oauthKey = stateKey
		o.oauthURL = callbackURL
		o.oauthHTTP = hc
	}
}

// WithCredentialKey seals the sealed store with AES-256-GCM, the key derived
// from any non-empty string, and the per-repository changelog signing seeds.
// Without it no repository can activate the mandatory signing, so creating a
// repository and opening a not-yet-activated one both refuse.
func WithCredentialKey(key string) Option { return func(o *options) { o.credKey = key } }

// WithBlobStore puts blob bytes somewhere other than the `blobs` bytea column.
// Without it the engine uses the postgres backend, where bytes and manifest
// still settle in one transaction and one database dump is still a whole
// backup. An external backend (fs, s3) trades both of those for bytes that
// never reach WAL; internal/blobbytes says what each one keeps.
func WithBlobStore(b blobbytes.Backend) Option { return func(o *options) { o.blobs = b } }

// WithInsecureAllowSuperuser DOWNGRADES the fail-closed role check to a loud
// warning: when the two bound roles are absent or misconfigured, Open proceeds
// with the pools running as the DSN's own user instead of refusing to boot. It
// exists ONLY for a dev/test database that runs as the postgres superuser
// without the roles provisioned. Never set it in production: under a superuser
// DSN with no bound roles there is NO row level security — every repository can
// read and write every other. The default, and every production path, fails
// closed.
func WithInsecureAllowSuperuser() Option {
	return func(o *options) { o.insecureAllowSuperuser = true }
}

// WithInsecureDisableTOTP STOPS VERIFYING THE SECOND FACTOR. Every door that
// takes a code — login, registration, the password change, the re-enrollment —
// accepts any code and an absent one, so the password is the only thing
// between a caller and the account. It exists for a local substrate that is
// wiped daily (SUBSTRATE_INSECURE_DISABLE_TOTP, which `mise run dev` sets);
// never set it where the substrate is reachable.
//
// A seed is still minted, still sealed and still carried through every
// credential rewrite, so turning this back off restores the factor rather than
// locking the user out of an account that has none.
func WithInsecureDisableTOTP() Option {
	return func(o *options) { o.insecureDisableTOTP = true }
}

// The seams *service satisfies beyond substrate.Service, asserted here for
// the same reason the dataset's are (dataset.go): a renamed method must break
// the build, not one endpoint at runtime.
var (
	_ substrate.Service          = (*service)(nil)
	_ substrate.OAuthCompleter   = (*service)(nil)
	_ substrate.RecoveryEnroller = (*service)(nil)
)

type service struct {
	dsn string
	// admin is the DSN's own user: the DDL, the role setup, and the index
	// materialization the bound roles are not allowed to run.
	admin *sql.DB
	// maint is the BYPASSRLS pool (substrate_maint): the control-plane table,
	// the repository lookup and anything that must read across repositories.
	// It carries NO repository setting, so an accidental repository-scoped
	// insert through it raises instead of landing somewhere arbitrary.
	maint *sql.DB
	// appRole is the role every repository-scoped pool assumes; empty when the
	// cluster would not let the engine create its roles.
	appRole string

	base *vocabulary.Registry
	// oauth runs the host connect/refresh flows for oauth2-trait bundles;
	// nil when WithOAuth was not given (StartOAuth then refuses).
	oauth *oauthflow.Client
	// credKey seals the sealed store (AES-256-GCM); empty stores plain.
	credKey []byte
	// blobs is where blob bytes live (WithBlobStore); the postgres backend by
	// default, which is the `blobs` bytea column this schema has always had.
	blobs blobbytes.Backend
	// totpDisabled stops verifying the second factor (WithInsecureDisableTOTP):
	// the password is then the whole credential. Dev only.
	totpDisabled bool
	log          *slog.Logger
	// gqlSchemas caches the agent loop's GraphQL schema per repository
	// (internal/gql owns the key and builder); the API layer holds its own.
	gqlSchemas *gql.Cache
	// bg counts and bounds every detached task the engine starts
	// (background.go); Close drains it before any pool closes.
	bg *background

	mu sync.Mutex
	// datasets is keyed by REPOSITORY ID, never by username: a username is a
	// lookup key, the id is the identity.
	datasets map[string]*dataset
	// opening is the per-repository singleflight: an id maps to the channel
	// the in-flight open closes when it is done, either way. It exists because
	// the open ladder is not safe to run twice at once on one repository.
	opening map[string]chan struct{}

	// testFailAfterSeed, when set (tests only), forces createSeededRepository to
	// fail AFTER the seed transaction commits and BEFORE the control-plane row —
	// the exact crash window the erase-and-sweep guarantees cover.
	testFailAfterSeed func() error
}

// Open connects to Postgres, loads the schema files, ensures the two roles and
// runs the shared schema's DDL. It provisions nothing: a repository exists
// once its control-plane row does.
func Open(ctx context.Context, dsn string, opts ...Option) (substrate.Service, error) {
	o := options{log: slog.Default()}
	for _, fn := range opts {
		fn(&o)
	}
	reg := o.registry
	switch {
	case reg != nil:
	case o.kindsFS != nil:
		r, err := vocabulary.LoadFS(o.kindsFS)
		if err != nil {
			return nil, err
		}
		reg = r
	case o.kindsDir != "":
		r, err := vocabulary.LoadDir(o.kindsDir)
		if err != nil {
			return nil, err
		}
		reg = r
	default:
		return nil, errors.New("substrate/engine: no schema source (WithKindsFS/WithKindsDir/WithRegistry)")
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: open postgres: %w", err)
	}
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		return nil, fmt.Errorf("substrate/engine: ping postgres: %w", err)
	}
	// The admin pool is for DDL and nothing else, so it is capped like the
	// maintenance one rather than left at database/sql's unlimited default —
	// a burst of DDL must not be able to take every connection the cluster
	// has. It assumes NO role: it exists precisely because the bound roles own
	// nothing and may not create, so the DSN's own user is the point.
	admin.SetMaxOpenConns(4)
	if o.blobs == nil {
		o.blobs = blobbytes.NewPostgres()
	}
	s := &service{
		dsn:          dsn,
		admin:        admin,
		base:         reg,
		credKey:      deriveCredentialKey(o.credKey),
		blobs:        o.blobs,
		totpDisabled: o.insecureDisableTOTP,
		log:          o.log,
		gqlSchemas:   gql.NewCache(),
		bg:           newBackground(),
		datasets:     map[string]*dataset{},
		opening:      map[string]chan struct{}{},
	}
	if o.oauthKey != "" || o.oauthURL != "" {
		// An empty HMAC key would make every state "signature" forgeable —
		// and the state is the unauthenticated callback's sole authentication
		// — so a half-configured facility refuses the boot instead of running
		// with worthless states (main.go's dev fallback mints a random key).
		if o.oauthKey == "" {
			_ = admin.Close()
			return nil, errors.New("substrate/engine: WithOAuth needs a non-empty state key — an empty HMAC key makes every oauth state forgeable")
		}
		s.oauth = &oauthflow.Client{
			StateKey:    []byte(o.oauthKey),
			CallbackURL: o.oauthURL,
			HTTP:        o.oauthHTTP,
		}
	}
	if len(s.credKey) == 0 {
		s.log.Warn("substrate: no credential key (WithCredentialKey) — stored provider tokens are not sealed")
	}
	if s.totpDisabled {
		s.log.Warn("substrate: TOTP VERIFICATION IS OFF (SUBSTRATE_INSECURE_DISABLE_TOTP) — a password is the whole credential; this is a local-development setting")
	}
	// FAIL CLOSED on the isolation roles. The scoped pools run as substrate_app
	// (bound by FORCE ROW LEVEL SECURITY) and the maintenance pool as
	// substrate_maint (BYPASSRLS but NOT a superuser). If those roles are absent
	// or misconfigured, the pools would fall back to the DSN's own user — and the
	// production DSN is a superuser, which BYPASSES FORCE ROW LEVEL SECURITY, so
	// every repository could read and write every other. So Open REFUSES unless
	// both roles exist with exactly the right attributes; the dev escape hatch
	// (WithInsecureAllowSuperuser) turns the refusal into a warning and nothing
	// else.
	ensureErr := ensureRoles(ctx, admin)
	degraded := false
	if err := requireRoles(ctx, admin); err != nil {
		if !o.insecureAllowSuperuser {
			_ = admin.Close()
			if ensureErr != nil {
				return nil, fmt.Errorf("%w (creating the roles also failed: %w)", err, ensureErr)
			}
			return nil, err
		}
		s.log.Error("substrate: INSECURE — the bound roles are missing or misconfigured and WithInsecureAllowSuperuser is set; row level security is NOT enforced (a superuser DSN bypasses it)",
			"error", err)
		degraded = true
	}
	maintRole := roleMaint
	if degraded {
		maintRole = ""
	} else {
		s.appRole = roleApp
	}
	// The DDL runs as the DSN's own user: the bound roles own nothing and may
	// not create. The grants inside it hand the tables to the two roles.
	if err := migrate(ctx, admin); err != nil {
		_ = admin.Close()
		return nil, err
	}
	maint, err := openMaint(dsn, maintRole)
	if err != nil {
		_ = admin.Close()
		return nil, err
	}
	if err := maint.PingContext(ctx); err != nil {
		_ = maint.Close()
		_ = admin.Close()
		return nil, fmt.Errorf("substrate/engine: open the maintenance pool: %w", err)
	}
	maint.SetMaxOpenConns(4)
	s.maint = maint
	// Role EXISTENCE with the right attributes is one thing; that the pools
	// ACTUALLY assume them at runtime — and that the DSN user is not itself a
	// superuser slipping past — is the check "enforced, not disciplined" needs.
	// Assert the effective principal on the maintenance pool and on a probe
	// scoped pool (the shape every request rides), unless the dev escape hatch
	// deliberately runs degraded.
	if !degraded {
		if err := assertPoolPrincipal(ctx, maint, roleMaint, true); err != nil {
			_ = maint.Close()
			_ = admin.Close()
			return nil, fmt.Errorf("substrate/engine: maintenance pool principal: %w", err)
		}
		if err := s.assertAppPoolPrincipal(ctx); err != nil {
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
	}
	// A backend switch on a store that already holds bytes is refused here,
	// before anything is served: half the blobs would 404 otherwise, and a
	// 404 reads like a deletion.
	if err := s.checkBlobBackend(ctx); err != nil {
		_ = maint.Close()
		_ = admin.Close()
		return nil, err
	}
	// Reclaim any repository-scoped rows a registration that crashed between its
	// scoped commit and its control-plane insert left behind (createSeededRepository
	// commits the repository's own rows FIRST and the control-plane row LAST, so
	// a crash in that window orphans rows under an id no lookup can name).
	if err := s.sweepOrphans(ctx); err != nil {
		_ = maint.Close()
		_ = admin.Close()
		return nil, err
	}
	// The shipped vocabulary's declared indexes, ONCE PER PROCESS. A
	// plain CREATE INDEX locks the shared records table for every repository,
	// so it is taken here — at boot, before anything is served — and not from
	// the open path a request drives. What arrives later (a bundle's
	// kinds) is materialized by the schema write that admits it.
	if err := ensureIndices(ctx, admin, reg.Kinds()); err != nil {
		_ = maint.Close()
		_ = admin.Close()
		return nil, err
	}
	// FAIL CLOSED on the credential key itself: a key that does not open what
	// this database already holds is refused HERE, not discovered one
	// repository at a time by whoever opens one first.
	if err := s.requireCredentialKeyOpens(ctx); err != nil {
		_ = maint.Close()
		_ = admin.Close()
		return nil, err
	}
	return s, nil
}

// requireCredentialKeyOpens holds a configured credential key against the
// signing seeds the store already carries. A host that starts with the WRONG
// key otherwise listens, answers /healthz, and fails every repository open
// after the fact — the shape a lost keys volume produces, where a fresh key
// is minted over a database full of repositories nothing can now unwrap.
//
// A KEYLESS service skips the check and keeps its own refusals: it cannot
// create or open a repository at all (openRepository, createSeededRepository),
// which is what leaves the read-only operator commands — `repository list`,
// `inspect`, `verify` — usable against a database whose key the operator
// running them does not hold.
func (s *service) requireCredentialKeyOpens(ctx context.Context) error {
	if len(s.credKey) == 0 {
		return nil
	}
	rows, err := s.maint.QueryContext(ctx,
		`SELECT id, signing_key FROM repositories WHERE signing_key IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var checked int
	var sealed []string
	for rows.Next() {
		var id string
		var wrapped []byte
		if err := rows.Scan(&id, &wrapped); err != nil {
			return err
		}
		checked++
		if _, err := s.openSigningSeed(wrapped); err != nil {
			sealed = append(sealed, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(sealed) == 0 {
		return nil
	}
	return fmt.Errorf("substrate/engine: SUBSTRATE_CREDENTIAL_KEY does not open the signing seed of %d of this database's %d repositories (%s): this is the key that seals every signing seed and every repository DEK, so this database was written under a different one. Restore the original key, or point this host at the database that belongs to this key. Do NOT let a fresh key start over an existing database: nothing here can be re-keyed and every repository would be unopenable",
		len(sealed), checked, strings.Join(sealed, ", "))
}

func (s *service) Close() error {
	// The detached tasks (background.go) hold this service's pools, so they are
	// refused, canceled and drained FIRST: closing a pool under one of them
	// would pull the connection out from under a live transaction.
	s.stopBackground(backgroundDrainTimeout)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ds := range s.datasets {
		// Retire this repository's function processes with it. They are
		// children of THIS process in their own process groups, so nothing else
		// reclaims them: the pool's idle TTL is ten minutes, and a server that
		// exits first leaves them orphaned. Reconcile against an empty live set
		// is "this repository runs nothing now", which is true once it closes.
		runner.Shared.Reconcile(context.Background(), ds.Repository().ID, nil)
		ds.close()
	}
	err := s.maint.Close()
	if cerr := s.admin.Close(); err == nil {
		err = cerr
	}
	return err
}

// Repositories lists every repository. It replaces the control-plane dataset
// the background loops used to enumerate through: the ledger is one table now.
func (s *service) Repositories(ctx context.Context) ([]substrate.RepositoryInfo, error) {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]substrate.RepositoryInfo, 0, len(repos))
	for _, r := range repos {
		out = append(out, r.info())
	}
	return out, nil
}

// open returns the cached dataset for a repository, opening it exactly once
// however many callers ask at once. The ladder underneath is not idempotent
// under concurrency — two opens of the same repository would both run the
// dialect promotion and the shipped-vocabulary upgrade, and the four loops
// plus a request are enough to make that happen — so the SECOND caller waits
// for the first's answer instead of racing it.
func (s *service) open(ctx context.Context, repo Repository) (*dataset, error) {
	for {
		s.mu.Lock()
		if ds, ok := s.datasets[repo.ID]; ok {
			s.mu.Unlock()
			return ds, nil
		}
		if inflight, ok := s.opening[repo.ID]; ok {
			s.mu.Unlock()
			// Wait for whoever got there first, then look again: their dataset
			// is cached, or their failure is ours to retry.
			select {
			case <-inflight:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		inflight := make(chan struct{})
		s.opening[repo.ID] = inflight
		s.mu.Unlock()

		ds, err := s.openNew(ctx, repo)

		s.mu.Lock()
		delete(s.opening, repo.ID)
		s.mu.Unlock()
		close(inflight)
		return ds, err
	}
}

// openNew opens a repository's scoped pool and runs the open-time ladder. It
// is called under open's per-repository singleflight, never directly.
func (s *service) openNew(ctx context.Context, repo Repository) (*dataset, error) {
	sc := repo.scope()
	db, err := openScoped(s.dsn, sc, s.appRole)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.Username, err)
	}
	db.SetMaxOpenConns(8)
	// The repository's DEK, unwrapped for the dataset's lifetime; a pre-DEK
	// repository adopts one here, compare-and-swap against a concurrent open.
	dek, err := s.repoDEK(ctx, repo.ID)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: unwrap DEK: %w", repo.Username, err)
	}
	if dek == nil {
		if dek, err = s.adoptDEK(ctx, repo.ID); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("substrate/engine: open repository %s: adopt DEK: %w", repo.Username, err)
		}
	}
	// The signing state loads BEFORE the ladder: the backfill signs what the
	// durable mark already covers. A key that cannot open (a keyless host
	// facing an activated repository) leaves reads working and makes every
	// append refuse (settleChain), loudly, rather than failing the open.
	signing, err := s.loadSigningState(ctx, repo.ID)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: signing state: %w", repo.Username, err)
	}
	var signKey ed25519.PrivateKey
	if signing.signedFrom > 0 {
		signKey, err = s.openSigningSeed(signing.wrappedSeed)
		if err != nil {
			s.log.Error("substrate: changelog signing is ACTIVE for this repository but the key cannot open — every write will refuse until the credential key is restored",
				"repository", repo.ID, "error", err)
			signKey = nil
		}
	}
	ds := &dataset{
		svc:   s,
		db:    db,
		dek:   dek,
		scope: sc,
		signState: datasetSigning{
			key: signKey, public: signing.public, signedFrom: signing.signedFrom,
		},
		// A dataset's registry starts EMPTY and is built from the repository's
		// OWN rows: the embedded tree seeded them once, at
		// creation, and has no standing here afterwards. Nothing re-projects
		// or prunes the tree at open — the only shipped write left is the
		// version-keyed upgrade below, which APPENDS.
		reg:   vocabulary.NewRegistry(),
		watch: newBroadcaster(),
		info:  repo.info(),
	}
	// The chain backfill runs FIRST: everything below appends, and an append
	// needs a hashed head to chain from.
	if err := ds.backfillChain(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Signing activation comes SECOND, after the backfill (the activation
	// epoch needs a hashed head) and BEFORE the ladder below, because the
	// ladder APPENDS: the dialect promotion and the shipped-vocabulary
	// upgrade both write changelog entries, and settleChain refuses an
	// unsigned append. Activating after them would make the first open of an
	// unactivated store refuse its own upgrade, which is every open, forever.
	// Activation is MANDATORY and has no exception: a keyless host cannot
	// activate — the seed would store in the clear — so it refuses the open.
	if ds.signing().signedFrom == 0 {
		if len(s.credKey) == 0 {
			_ = db.Close()
			return nil, fmt.Errorf("substrate/engine: open repository %s: changelog signing is mandatory and needs SUBSTRATE_CREDENTIAL_KEY; the signing seed seals under it", repo.Username)
		}
		signing, key, err := s.adoptSigning(ctx, ds)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("substrate/engine: open repository %s: activate signing: %w", repo.Username, err)
		}
		ds.setSigning(datasetSigning{key: key, public: signing.public, signedFrom: signing.signedFrom})
	}
	// A crash between activation's mark and its epoch leaves the attestation
	// missing forever; the open is the sanctioned place to record it late.
	if err := s.ensureActivationEpoch(ctx, ds); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: activation epoch: %w", repo.Username, err)
	}
	// The stored rows speak one DIALECT: the gate in dialect.go refuses a
	// store newer than this binary with a named error and stamps an older one,
	// before anything reads declaration rows back. Then the whole vocabulary
	// rebuilds FROM the rows, and only then does the shipped-vocabulary
	// upgrade append what a newer binary added (seed.go). Every entry these
	// steps append is at or after the activation seq, so every one is signed.
	for _, step := range []func(context.Context) error{
		ds.promoteSchemaDialect,
		ds.loadStoredVocabulary,
		ds.upgradeShippedVocabulary,
		ds.ensureTriggerCursors,
	} {
		if err := step(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	// Bodies prepare at open exactly as they do at registration: Go builds
	// hit the cache, python sources register into the shared host.
	ds.warmFunctions()
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.datasets[repo.ID]; ok {
		ds.close()
		return prev, nil
	}
	s.datasets[repo.ID] = ds
	return ds, nil
}

// Dataset opens a repository's dataset by its user's username.
func (s *service) Dataset(ctx context.Context, username string) (substrate.Dataset, error) {
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, repo)
}

// CreateRepository creates a repository and its control-plane row: the user IS
// that row, and the repository it owns is born holding the shipped kinds.
// Registration (auth.go) is what calls it — a repository created any other
// way has no credential and therefore no way in.
func (s *service) CreateRepository(ctx context.Context, name string) (substrate.RepositoryInfo, error) {
	repo, _, err := s.createSeededRepository(ctx, name, nil)
	if err != nil {
		return substrate.RepositoryInfo{}, err
	}
	return repo.info(), nil
}

// createSeededRepository is THE creation act:
// the seed of the shipped vocabulary, the repository's own description of
// itself, and whatever the caller adds — registration passes the sealed
// material, the credential record and the first token — as ONE transaction in
// the new repository's changelog, followed by the `repositories` row that makes the
// user exist.
//
// ATOMICITY. The two sides live in two pools by construction: the repository's
// own rows are written by `substrate_app` under the repository's scope, and
// the control-plane table is only visible to `substrate_maint`. So instead of
// a transaction that cannot exist, the ORDER carries the guarantee: everything
// the repository contains commits first, in one transaction, and the
// control-plane row — the row every lookup starts from, and the unique index
// on the username — is written LAST. A failure anywhere before it leaves rows
// under an id no login, token or listing can ever name, and they are deleted
// on the way out; a failure at the row itself does the same. There is no order
// in which a HALF-CREATED USER can be observed: the user exists exactly when
// the row does, and by then the repository is complete.
//
// The returned key is the repository's freshly minted signing key (nil on a
// keyless insecure creation). It exists nowhere unsealed but here:
// registration is the one caller that hands its seed to the user, once.
func (s *service) createSeededRepository(ctx context.Context, name string, extra func(*txn) error) (Repository, ed25519.PrivateKey, error) {
	var zero Repository
	if !vocabulary.ValidRepositoryName(name) {
		return zero, nil, fmt.Errorf("%w: username %q must match [a-z][a-z0-9]{1,29}", substrate.ErrValidation, name)
	}
	// A cheap early no: the unique index below is the authority, and it is
	// what a race actually loses on.
	if _, err := s.repositoryByUsername(ctx, name); err == nil {
		return zero, nil, fmt.Errorf("%w: user %q already exists", substrate.ErrValidation, name)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return zero, nil, err
	}
	id, err := newID()
	if err != nil {
		return zero, nil, err
	}
	// The id is minted BEFORE anything is written under it, so it is checked
	// before anything is written under it too: rows land in a scope, and a
	// scope that already belongs to somebody would be somebody else's data.
	if _, err := s.repositoryByID(ctx, id); err == nil {
		return zero, nil, fmt.Errorf("substrate/engine: minted a repository id that already exists")
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return zero, nil, err
	}
	repo := Repository{ID: id, Username: name}
	// The DEK is born with the repository: the seed transaction below already
	// writes sealed material (the credential, at registration), and it seals
	// under this key from the first byte. The control-plane row wraps it
	// under the host key at the commit point.
	dek, err := newDEK()
	if err != nil {
		return zero, nil, err
	}
	if repo.DEK, err = s.wrapDEK(dek); err != nil {
		return zero, nil, err
	}

	// Signing is born WITH the repository, not activated after it: the seed
	// transaction below writes the vocabulary, the self-description and the
	// caller's auth material, and every one of those entries must be inside
	// the guarantee, so the key is minted here and signed_from_seq is 1. A
	// keyless host cannot mint (the seed would store in the clear) and
	// refuses the creation.
	if len(s.credKey) == 0 {
		return zero, nil, fmt.Errorf("substrate/engine: create repository %s: changelog signing is mandatory and needs SUBSTRATE_CREDENTIAL_KEY; the signing seed seals under it", name)
	}
	wrapped, signKey, err := s.mintSigningSeed()
	if err != nil {
		return zero, nil, err
	}
	repo.SigningKey = wrapped
	repo.SigningPublic = signKey.Public().(ed25519.PublicKey)
	repo.SignedFrom = 1

	db, err := openScoped(s.dsn, repo.scope(), s.appRole)
	if err != nil {
		return zero, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return zero, nil, fmt.Errorf("substrate/engine: create repository %s: %w", name, err)
	}
	db.SetMaxOpenConns(8)
	// The creation dataset carries the BINARY's registry — the seed has to
	// resolve the kinds it is writing, and the repository has no rows yet.
	// After the seed commits, the dataset is thrown away and the repository is
	// opened the ordinary way: from its own rows.
	seedDS := &dataset{
		svc: s, db: db, dek: dek, scope: repo.scope(),
		reg: s.base.Clone(), watch: newBroadcaster(), info: repo.info(),
		signState: datasetSigning{
			key: signKey, public: repo.SigningPublic, signedFrom: repo.SignedFrom,
		},
	}
	fail := func(err error) (Repository, ed25519.PrivateKey, error) {
		seedDS.close()
		if cerr := s.eraseRepository(ctx, repo.ID); cerr != nil {
			s.log.Error("substrate: could not erase a half-made repository; the boot sweep will reclaim it",
				"repository", repo.ID, "error", cerr)
		}
		return zero, nil, err
	}
	// ONE transaction: the seed, the self-description, and the caller's part.
	// The seed's entries carry `bundle:core` — the shipped tree's own hand —
	// while the auth material the caller writes carries the substrate's, so
	// the changelog says which is which.
	if err := seedDS.inTx(ctx, substrate.ActorSeed, true, func(t *txn) error {
		// The birth activation epoch, in the same transaction as the entries
		// it covers: signed from seq 1, over an empty chain (no heads).
		if repo.SignedFrom > 0 {
			ep := chainEpoch{
				At: t.now, Reason: epochActivate, FromSeq: repo.SignedFrom,
				PublicKey: repo.SigningPublic, SignedFrom: repo.SignedFrom,
			}
			if err := t.recordEpoch(ep, signKey); err != nil {
				return err
			}
		}
		if err := t.seedShippedSchema(s.base); err != nil {
			return err
		}
		// The repository's own read-only description of itself. `lifecycle` is
		// the state a creation is born into, so naming it is assertion, not
		// transition (MODEL §11.4).
		if err := t.asActor(substrate.ActorSystem, func() error {
			_, err := t.put(substrate.PutInput{
				Kind: kindRepository, ID: repo.ID,
				Properties: map[string]any{"name": name, "lifecycle": "active"},
			})
			return err
		}); err != nil {
			return err
		}
		if extra == nil {
			return nil
		}
		return t.asActor(substrate.ActorSystem, func() error { return extra(t) })
	}); err != nil {
		return fail(err)
	}
	seedDS.close()

	// The crash window this cleanup guards, made reachable for a test: the seed
	// has committed, the control-plane row has not.
	if s.testFailAfterSeed != nil {
		if err := s.testFailAfterSeed(); err != nil {
			if cerr := s.eraseRepository(ctx, repo.ID); cerr != nil {
				s.log.Error("substrate: could not erase after a forced post-seed failure",
					"repository", repo.ID, "error", cerr)
			}
			return zero, nil, err
		}
	}

	if err := s.insertRepositoryRow(ctx, &repo); err != nil {
		if cerr := s.eraseRepository(ctx, repo.ID); cerr != nil {
			s.log.Error("substrate: could not erase after a control-plane insert failure; the boot sweep will reclaim it",
				"repository", repo.ID, "error", cerr)
		}
		return zero, nil, err
	}
	if repo.SignedFrom > 0 {
		// The same PIN adoptSigning logs: the (public key, signed-from) pair
		// an operator writes down outside the database.
		s.log.Info("substrate: changelog signing ACTIVATED — pin this pair outside the database; it is what a verifier trusts",
			"repository", repo.ID,
			"publicKey", hex.EncodeToString(repo.SigningPublic),
			"signedFromSeq", repo.SignedFrom)
	}
	return repo, signKey, nil
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newID mints a bare 12-character lowercase base32 record ID.
func newID() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.ToLower(b32.EncodeToString(raw))[:12], nil
}

// derivedID is the deterministic ID form: 12 characters of base32(sha256).
func derivedID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return strings.ToLower(b32.EncodeToString(sum[:]))[:12]
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

var (
	_ substrate.Service = (*service)(nil)
	_ substrate.Dataset = (*dataset)(nil)
)
