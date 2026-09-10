// Package engine implements substrate.Service and substrate.Dataset over
// Postgres: ONE schema shared by every repository, the five mutations,
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
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
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
	dataRoot  string
	// segmentBytes is the changelog segment size (WithChangelogSegmentBytes).
	segmentBytes int64
	// conversionCeiling is the work ceiling (WithConversionCeiling); the set
	// flag tells an explicit zero (no ceiling) from the default.
	conversionCeiling    int64
	conversionCeilingSet bool
	// catchUpBatch is the page size of the boot's table-to-file catch-up
	// (appendFromTable); rebuildBatch when not positive. Only a test sets it
	// (export_test.go), to put a transaction across a page boundary.
	catchUpBatch int
	blobs        blobbytes.Backend
	log          *slog.Logger
	// insecureAllowSuperuser downgrades the fail-closed role check to a warning
	// (WithInsecureAllowSuperuser). Dev/test only; never the production default.
	insecureAllowSuperuser bool
	// insecureDisableTOTP stops verifying the second factor
	// (WithInsecureDisableTOTP). Dev/test only; never the production default.
	insecureDisableTOTP bool
	// dirReadOnly opens the service beside a running server
	// (WithDirectoryReadOnly): no boot check, no writer, no write.
	dirReadOnly bool
	// importFault and importBatch are the boot import's test seams
	// (seams.go WithTestImportFault): a hook run at each durable step of an
	// import, so a test can stop the process there, and a batch size below
	// rebuildBatch, so a small history spans more than one batch. The seams
	// compile into the binary and are inert unless set.
	importFault func(stage string) error
	importBatch int
	// commitFault is a write's test seam (export_test.go): a hook run at
	// each durable step of commitAndMirror, around the manifest write that
	// precedes the first append in a new changelog dialect and around the
	// Postgres commit, so a test can fail a step or stop the process there.
	// Tests only.
	commitFault func(stage string) error
	// snapshotFault is the snapshot's test seam (export_test.go): a hook run
	// with the partial directory after each copy step (snapshot.go).
	snapshotFault func(stage, dir string) error
	// invokeHook is the runner's test seam (seams.go WithTestInvokeHook): a
	// hook run with a function's identity as its body is about to be invoked
	// (runner.go runCallableRaw), so a test can act while the body runs.
	// Inert unless set.
	invokeHook func(function string)
	// now is the TOTP verifier's clock (seams.go WithTestTOTPClock); the wall
	// clock when nil. A test that spends one window's codes advances it
	// instead of sleeping through a real 30 second step.
	now func() time.Time
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

// WithCredentialKey seals the sealed store with AES-256-GCM: every
// repository's DEK wraps under it. The key is standard-base64 of exactly 32
// bytes, the AES-256 key itself; Open refuses anything else (a passphrase is
// a dictionary-searchable key, ADR 0024). Empty is the keyless service, where
// the DEK wrap is stored plain-marked and the boot warning says so.
func WithCredentialKey(key string) Option { return func(o *options) { o.credKey = key } }

// WithDataRoot names the data root: the absolute directory that holds one
// subdirectory per repository under <root>/repositories (SUBSTRATE_DATA_ROOT
// in the server's configuration). Open refuses without it, because the
// repository directory is where the changelog segments, the sealed store and
// (by default) the blob bytes live, and there is no sensible default place
// for somebody's data.
func WithDataRoot(root string) Option { return func(o *options) { o.dataRoot = root } }

// WithChangelogSegmentBytes sets the size past which a repository's active
// changelog segment is finished and the next one opened
// (SUBSTRATE_CHANGELOG_SEGMENT_BYTES). changelogfile.DefaultSegmentBytes
// when not given or not positive.
func WithChangelogSegmentBytes(n int64) Option { return func(o *options) { o.segmentBytes = n } }

// WithConversionCeiling bounds the live records one declaration change may
// rewrite in its transaction (SUBSTRATE_CONVERSION_CEILING, decision 0067): a
// plan whose estimated work is above n is refused on both doors, and the
// previews report it as a blocker. Zero or less is no ceiling;
// DefaultConversionCeiling when the option is not given.
func WithConversionCeiling(n int64) Option {
	return func(o *options) { o.conversionCeiling, o.conversionCeilingSet = n, true }
}

// WithDirectoryReadOnly opens the service as a second process beside a running
// server: the operator hat's `repository verify` and `reembed`. Open runs no
// boot check and no orphan sweep, a dataset opens no changelog writer and
// refuses every inTx write with ErrDirectoryReadOnly, and VerifyRepository
// reports an incomplete tail or a table ahead of its file as findings instead of
// repairing them. Without this option a second process on the same data root
// is a second writer, and the server's running writer refuses it with
// ErrChangelogLocked at the first repository it opens for writing.
func WithDirectoryReadOnly() Option { return func(o *options) { o.dirReadOnly = true } }

// WithBlobStore hands the engine the blob byte store to use, which is
// otherwise built here: the fs backend under the data root
// (<root>/repositories/<authority>/blobs). It exists so the operator hat and
// the tests can pass a store rooted somewhere else.
func WithBlobStore(b blobbytes.Backend) Option { return func(o *options) { o.blobs = b } }

// ErrNoDataRoot is Open's refusal when no data root was given, or the given
// one is not an absolute path.
var ErrNoDataRoot = errors.New("substrate/engine: no data root: set SUBSTRATE_DATA_ROOT (engine.WithDataRoot) to the absolute directory that holds every repository's files")

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
	_ substrate.Service        = (*service)(nil)
	_ substrate.OAuthCompleter = (*service)(nil)
	_ substrate.SeamReporter   = (*service)(nil)
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
	// credKeyID is hostKeyID(credKey): what every DEK wrap this host writes
	// names as its key, and what a failing unwrap is compared against.
	credKeyID string
	// dataRoot is the data root (WithDataRoot); <dataRoot>/repositories/<authority>
	// is one repository's directory (repodir.go).
	dataRoot string
	// segmentBytes is the size every changelog writer rotates at.
	segmentBytes int64
	// conversionCeiling is the most live records one declaration change may
	// rewrite (convert.go admitConversion); zero or less is no ceiling.
	conversionCeiling int64
	// catchUpBatch is the page size of the table-to-file catch-up.
	catchUpBatch int
	// blobs is where blob bytes live (WithBlobStore); the fs backend under
	// the data root by default.
	blobs blobbytes.Backend
	// totpDisabled stops verifying the second factor (WithInsecureDisableTOTP):
	// the password is then the whole credential. Dev only.
	totpDisabled bool
	// now is the clock the TOTP verifier reads; nowUTC outside a test.
	now func() time.Time
	// readOnly is WithDirectoryReadOnly: this process is not the repository
	// directories' writer and must not become one (repodir.go).
	readOnly bool
	log      *slog.Logger
	// gqlSchemas caches the agent loop's GraphQL schema per repository
	// (internal/gql owns the key and builder); the API layer holds its own.
	gqlSchemas *gql.Cache
	// bg counts and bounds every detached task the engine starts
	// (background.go); Close drains it before any pool closes.
	bg *background

	mu sync.Mutex
	// datasets is keyed by REPOSITORY ID, the authority: the repository's one
	// name, on disk, in the database and on the wire (decision 0052).
	datasets map[string]*dataset
	// opening is the per-repository singleflight: an id maps to the channel
	// the in-flight open closes when it is done, either way. It exists because
	// the open ladder is not safe to run twice at once on one repository.
	opening map[string]chan struct{}

	// testFailAfterSeed, when set (tests only), forces createSeededRepository to
	// fail AFTER the seed transaction commits and BEFORE the control-plane row —
	// the exact crash window the erase-and-sweep guarantees cover.
	testFailAfterSeed func() error
	// testImportFault and testImportBatch are the options' import seams
	// (repodir.go importEntries, refoldFromFiles). Tests only.
	testImportFault func(stage string) error
	testImportBatch int
	// testCommitFault is the options' commit seam (dataset.go
	// commitAndMirror, repodir.go writeManifestBeforeCommit). Tests only.
	testCommitFault func(stage string) error
	// testSnapshotFault is the options' snapshot seam (snapshot.go).
	testSnapshotFault func(stage, dir string) error
	// testInvokeHook is the options' runner seam (runner.go). Tests only.
	testInvokeHook func(function string)
}

// Open connects to Postgres, loads the schema files, ensures the two roles and
// runs the shared schema's DDL. It provisions nothing: a repository exists
// once its control-plane row does.
func Open(ctx context.Context, dsn string, opts ...Option) (substrate.Service, error) {
	o := options{log: slog.Default(), now: nowUTC}
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

	credKey, err := deriveCredentialKey(o.credKey)
	if err != nil {
		return nil, err
	}

	// The data root, before any connection: a boot that cannot hold its
	// files must not touch the database. The repositories directory is made
	// here so a repository's own directory is always one level below a
	// directory the engine owns, with the mode that keeps the bytes private.
	if o.dataRoot == "" || !filepath.IsAbs(o.dataRoot) {
		return nil, fmt.Errorf("%w (got %q)", ErrNoDataRoot, o.dataRoot)
	}
	if err := os.MkdirAll(filepath.Join(o.dataRoot, changelogfile.RepositoriesDir), 0o700); err != nil {
		return nil, fmt.Errorf("substrate/engine: create %s under the data root: %w", changelogfile.RepositoriesDir, err)
	}
	if o.segmentBytes <= 0 {
		o.segmentBytes = changelogfile.DefaultSegmentBytes
	}
	if o.catchUpBatch <= 0 {
		o.catchUpBatch = rebuildBatch
	}
	if !o.conversionCeilingSet {
		o.conversionCeiling = DefaultConversionCeiling
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
		fsBlobs, err := blobbytes.NewFS(o.dataRoot)
		if err != nil {
			_ = admin.Close()
			return nil, err
		}
		o.blobs = fsBlobs
	}
	s := &service{
		dsn:          dsn,
		admin:        admin,
		base:         reg,
		credKey:      credKey,
		credKeyID:    hostKeyID(credKey),
		dataRoot:     o.dataRoot,
		segmentBytes: o.segmentBytes,
		catchUpBatch: o.catchUpBatch,
		blobs:        o.blobs,

		conversionCeiling: o.conversionCeiling,
		totpDisabled:      o.insecureDisableTOTP,
		now:               o.now,
		readOnly:          o.dirReadOnly,
		log:               o.log,
		gqlSchemas:        gql.NewCache(),
		bg:                newBackground(),
		datasets:          map[string]*dataset{},
		opening:           map[string]chan struct{}{},

		testImportFault:   o.importFault,
		testImportBatch:   o.importBatch,
		testCommitFault:   o.commitFault,
		testSnapshotFault: o.snapshotFault,
		testInvokeHook:    o.invokeHook,
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
	// Reclaim any repository-scoped rows a registration that crashed between its
	// scoped commit and its control-plane insert left behind (createSeededRepository
	// commits the repository's own rows FIRST and the control-plane row LAST, so
	// a crash in that window orphans rows under an id no lookup can name). Not
	// from a read-only process: beside a running server those rows may be a
	// registration in flight, not a crash.
	if !s.readOnly {
		if err := s.sweepOrphans(ctx); err != nil {
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
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
	// Every repository's directory against its rows, before anything is
	// served (repodir.go): a crash left the file a transaction behind, a
	// restore left a directory with no row, or a wiped data root left a row
	// with no directory. A repository the two sides disagree on refuses the
	// boot. A read-only process skips it: the check writes, and the server
	// that owns the directories runs it at its own boot.
	if !s.readOnly {
		if err := s.reconcileRepositories(ctx); err != nil {
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
	}
	return s, nil
}

// requireCredentialKeyOpens holds a configured credential key against the
// DEK wraps the store already carries. A host that starts with the WRONG key
// otherwise listens, answers /healthz, and fails every repository open after
// the fact, the shape a lost keys volume produces, where a fresh key is
// minted over a database full of repositories nothing can now unwrap.
//
// A KEYLESS service skips the check: a plain-marked wrap opens without a key,
// and a sealed one refuses at the repository open, which is what leaves the
// read-only operator commands (`repository list`, `inspect`, `verify`) usable
// against a database whose key the operator running them does not hold.
func (s *service) requireCredentialKeyOpens(ctx context.Context) error {
	if len(s.credKey) == 0 {
		return nil
	}
	rows, err := s.maint.QueryContext(ctx,
		`SELECT id, dek, dek_key_id FROM repositories WHERE dek IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var checked int
	var failures []string
	// A wrap that names THIS key and still does not open is damage, not a
	// foreign database, and the advice for the two is opposite: the first
	// must keep its key, the second must find another.
	damaged := false
	for rows.Next() {
		var id string
		var wrapped []byte
		var keyID sql.NullString
		if err := rows.Scan(&id, &wrapped, &keyID); err != nil {
			return err
		}
		checked++
		if _, err := s.unwrapDEK(wrapped, id, keyID.String); err != nil {
			failures = append(failures, err.Error())
			if keyID.String == s.credKeyID {
				damaged = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(failures) == 0 {
		return nil
	}
	advice := "Every repository DEK wraps under this one key, so this database was written under a different one. Restore the key the wraps name, or point this host at the database that belongs to this key. Do NOT let a fresh key start over an existing database: no verb re-wraps a live repository's DEK under another host key, and every repository would be unopenable"
	if damaged {
		advice = "A wrap that names this very key and does not open is damaged, not foreign: the key is right, so keep it, and restore that repository's row from a database dump or its directory from a copy (a copy re-wraps through its recovery key: `substratectl repository rewrap`)"
	}
	return fmt.Errorf("substrate/engine: SUBSTRATE_CREDENTIAL_KEY (id %s) does not open the DEK of %d of this database's %d repositories: %s. %s",
		s.credKeyID, len(failures), checked, strings.Join(failures, "; "), advice)
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
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.ID, err)
	}
	db.SetMaxOpenConns(8)
	// The repository's DEK for the dataset's lifetime, unwrapped from the
	// control-plane row.
	keys, err := s.repoKeys(ctx, repo.ID)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: unwrap DEK: %w", repo.ID, err)
	}
	dek := keys.dek
	dir, err := s.repositoryDir(repo.ID)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.ID, err)
	}
	ds := &dataset{
		svc:        s,
		db:         db,
		dek:        dek,
		scope:      sc,
		dir:        dir,
		generation: repo.HistoryGeneration,
		// A dataset's registry starts EMPTY and is built from the repository's
		// OWN rows: the embedded tree seeded them once, at
		// creation, and has no standing here afterwards. Nothing re-projects
		// or prunes the tree at open — the only shipped write left is the
		// version-keyed upgrade below, which APPENDS.
		reg:   vocabulary.NewRegistry(),
		watch: newBroadcaster(),
		info:  repo.info(),
	}
	// The directory before every step that writes: the writer opens at the
	// file's head, which must be the table's, or the ladder's first append
	// would land on a file that is not at the seq it claims (repodir.go).
	if err := ds.openDirectory(ctx); err != nil {
		ds.close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.ID, err)
	}
	// The changelog dialect gate runs next, ahead of every step that writes:
	// a binary that cannot replay this history must not extend it either. It
	// only reads; the claim is written by the first transaction that appends
	// (changelogdialect.go). This is the entries' half of the downgrade gate,
	// beside dialect.go's gate over the stored declaration rows.
	if err := ds.gateChangelogDialect(ctx); err != nil {
		ds.close()
		return nil, err
	}
	// The stored rows speak one DIALECT: the gate in dialect.go refuses a
	// store newer than this binary with a named error and stamps an
	// unstamped one, before anything reads declaration rows back.
	if err := ds.gateVocabularyDialect(ctx); err != nil {
		ds.close()
		return nil, err
	}
	// The manifest, after both gates, the re-key and the vocabulary stamp, and
	// before the first append: it says what the row and the stamps say, and
	// the transaction that claims the changelog dialect rewrites it from what
	// is remembered here (repodir.go writeManifestBeforeCommit).
	if !s.readOnly {
		m, err := s.ensureManifest(ctx, dir, repo, db)
		if err != nil {
			ds.close()
			return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.ID, err)
		}
		ds.manifest = m
	}
	// Then the whole vocabulary rebuilds FROM the rows, and only then does the
	// shipped-vocabulary upgrade append what a newer binary added (seed.go).
	for _, step := range []func(context.Context) error{
		ds.loadStoredVocabulary,
		ds.upgradeShippedVocabulary,
		ds.ensureTriggerCursors,
		ds.clearDeadReservations,
	} {
		if err := step(ctx); err != nil {
			ds.close()
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

// Dataset opens a repository's dataset by its id, the authority.
func (s *service) Dataset(ctx context.Context, repository string) (substrate.Dataset, error) {
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, repo)
}

// DatasetSeams reports which optional extensions a dataset of this engine
// satisfies (substrate.SeamReporter), for a discovery document that opens no
// repository. The value is the typed nil of the type Dataset returns, so the
// answer is the method set itself and can never disagree with it; nothing
// calls a method on it.
func (s *service) DatasetSeams() substrate.Dataset { return (*dataset)(nil) }

// CreateRepository creates a repository and its control-plane row: the user IS
// that row, and the repository it owns is born holding the shipped kinds.
// Registration (auth.go) is what calls it — a repository created any other
// way has no credential and therefore no way in.
func (s *service) CreateRepository(ctx context.Context, repository string) (substrate.RepositoryInfo, error) {
	repo, err := s.createSeededRepository(ctx, repository, nil)
	if err != nil {
		return substrate.RepositoryInfo{}, err
	}
	return repo.info(), nil
}

// createSeededRepository is THE creation act. The repository's id IS its
// authority (decision 0046): the control-plane row's primary key, every scope,
// the DEK wrap's binding, the directory under the data root and the fs blob
// path are all the one name the registration chose. Nothing is minted.
//
// It is
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
// control-plane row — the row every lookup starts from — is written LAST. A failure anywhere before it leaves rows
// under an authority no login, token or listing can ever name, and they are
// deleted on the way out; a failure at the row itself does the same. There is
// no order in which a HALF-CREATED USER can be observed: the user exists
// exactly when the row does, and by then the repository is complete.
//
// THE DIRECTORY COMES LAST. The repository directory under the data root
// (repodir.go) is written from the tables AFTER the control-plane row, not
// beside the seed: the seed dataset has no writer. So a crash before the row
// leaves scoped rows and no directory, which sweepOrphans reclaims, and a
// crash after the row leaves a row with no directory or a partial one, which
// the boot check writes out (case 5) or catches up (case 2). Neither state
// needs a rule of its own, and a directory can never exist for a repository
// that does not.
//
// ONE REGISTRATION PER AUTHORITY AT A TIME. The authority is the scope the
// seed writes under, and the lookup below is all that stands between two
// registrations and one scope: without a lock both pass it, both seed rows
// under the same authority, one inserts the control-plane row and the
// other's cleanup erases the winner's rows, changelog and directory as its
// own. So the authority's registration lock (lockRegistration) is held from
// before the lookup to the return, on the one maint connection every
// control-plane statement here runs on, and the second registrant runs its
// lookup after the first's row exists and is refused before it writes a byte.
// The cleanup is ownership-checked on top (eraseFailedCreation): a creation
// that wrote no row erases nothing while a row holds the authority.
func (s *service) createSeededRepository(ctx context.Context, authority string, extra func(*txn) error) (Repository, error) {
	var zero Repository
	if err := validRepositoryAuthority(authority); err != nil {
		return zero, err
	}
	cp, unlock, err := s.lockRegistration(ctx, authority)
	if err != nil {
		return zero, err
	}
	defer unlock()
	// A cheap early no: the primary key below is the truth, and it is what a
	// race actually loses on (insertRepositoryRow names the same refusal when
	// it does). The authority is checked BEFORE anything is written under it,
	// because rows land in its scope, and a scope that already belongs to
	// somebody would be somebody else's data.
	if _, err := s.repositoryByIDOn(ctx, cp, authority); err == nil {
		return zero, errAuthorityTaken(authority)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return zero, err
	}
	repo := Repository{ID: authority, Authority: authority}
	// The DEK is born with the repository: the seed transaction below already
	// writes sealed material (the credential, at registration), and it seals
	// under this key from the first byte. The control-plane row wraps it
	// under the host key at the commit point and names that key; nothing the
	// repository will ever hold is sealed any other way (0059).
	dek, err := newDEK()
	if err != nil {
		return zero, err
	}
	if repo.DEK, err = s.wrapDEK(dek, repo.ID); err != nil {
		return zero, err
	}
	repo.DEKKeyID = s.credKeyID

	db, err := openScoped(s.dsn, repo.scope(), s.appRole)
	if err != nil {
		return zero, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return zero, fmt.Errorf("substrate/engine: create repository %s: %w", authority, err)
	}
	db.SetMaxOpenConns(8)
	// The creation dataset carries the BINARY's registry — the seed has to
	// resolve the kinds it is writing, and the repository has no rows yet.
	// After the seed commits, the dataset is thrown away and the repository is
	// opened the ordinary way: from its own rows.
	seedDS := &dataset{
		svc: s, db: db, dek: dek, scope: repo.scope(),
		reg: s.base.Clone(), watch: newBroadcaster(), info: repo.info(),
	}
	// inserted flips once the control-plane row is this creation's, and
	// decides what a failure may erase (eraseFailedCreation).
	inserted := false
	fail := func(stage string, cause error) (Repository, error) {
		if cerr := s.eraseFailedCreation(ctx, cp, repo.ID, inserted); cerr != nil {
			s.log.Error("substrate: could not erase a half-made repository",
				"repository", repo.ID, "stage", stage, "error", cerr)
		}
		return zero, cause
	}
	// ONE transaction: the seed, the self-description, and the caller's part.
	// The seed's entries carry `bundle:core` — the shipped tree's own hand —
	// while the auth material the caller writes carries the substrate's, so
	// the changelog says which is which.
	if err := seedDS.inTx(ctx, substrate.ActorSeed, true, func(t *txn) error {
		if err := t.seedShippedSchema(s.base); err != nil {
			return err
		}
		// The repository's own read-only description of itself. `lifecycle` is
		// the state a creation is born into, so naming it is assertion, not
		// transition (MODEL §11.4).
		if err := t.asActor(substrate.ActorSystem, func() error {
			_, err := t.put(substrate.PutInput{
				Kind: kindRepository, ID: repo.ID,
				Properties: map[string]any{"name": authority, "authority": authority, "lifecycle": "active"},
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
		seedDS.close()
		return fail("seed", err)
	}
	seedDS.close()

	// The crash window this cleanup guards, made reachable for a test: the seed
	// has committed, the control-plane row has not.
	if s.testFailAfterSeed != nil {
		if err := s.testFailAfterSeed(); err != nil {
			return fail("after the seed", err)
		}
	}

	if err := s.insertRepositoryRow(ctx, cp, &repo); err != nil {
		return fail("control-plane row", err)
	}
	inserted = true
	// The directory, from the tables the seed just committed. A failure here
	// is a failed registration like any other: the row is erased with the
	// rows and the directory, so nothing half-made survives the call.
	if _, err := s.reconcileRow(ctx, repo, false); err != nil {
		return fail("directory", fmt.Errorf("substrate/engine: write the repository directory of %s: %w", authority, err))
	}
	return repo, nil
}

// validRepositoryID is the grammar a repository id is held to at BOTH doors,
// registration (validRepositoryAuthority) and the boot import of a directory
// (repodir.go): the authority grammar every kind carries
// (vocabulary.ValidRepositoryAuthority), capped at MaxIDLen. The authority is
// the repository's id everywhere (decision record 0052), including the id of
// its self-description record, and a record id is at most MaxIDLen bytes
// where DNS would admit 253; a directory the import accepted past the cap
// would be a repository whose self-description no write could address.
func validRepositoryID(authority string) error {
	switch {
	case authority == "":
		return fmt.Errorf("%w: a repository needs an authority: a hostname you control, such as ada.example.com", substrate.ErrValidation)
	case !vocabulary.ValidRepositoryAuthority(authority):
		return fmt.Errorf("%w: authority %q must be a lowercase DNS-style name with at least two labels (ada.example.com)", substrate.ErrValidation, authority)
	case len(authority) > vocabulary.MaxIDLen:
		return fmt.Errorf("%w: authority %q is longer than %d bytes, the most a repository id may be", substrate.ErrValidation, authority, vocabulary.MaxIDLen)
	}
	return nil
}

// validRepositoryAuthority is the door a repository's own authority passes
// through once, at creation: validRepositoryID, and one namespace refused on
// top. `substrate.reamde.dev` and everything under it is where the shipped
// vocabulary publishes, so a repository claiming a name there would be a
// user-owned authority that reads as shipped.
func validRepositoryAuthority(authority string) error {
	if err := validRepositoryID(authority); err != nil {
		return err
	}
	if authority == publisherAuthority || strings.HasSuffix(authority, "."+publisherAuthority) {
		return fmt.Errorf("%w: authority %q is under %s, where the shipped vocabulary publishes; a repository's authority is its own name", substrate.ErrValidation, authority, publisherAuthority)
	}
	return nil
}

// publisherAuthority is the suffix every shipped authority carries
// (`substrate.reamde.dev/core`, `providers.substrate.reamde.dev/google`), and
// so the one no repository may claim.
const publisherAuthority = "substrate.reamde.dev"

// errAuthorityTaken is the refusal a registration meets when its name is
// somebody's: spelled once, because the early lookup and the row insert's
// unique violation must say the same thing.
func errAuthorityTaken(authority string) error {
	return fmt.Errorf("%w: authority %q is already owned by another repository on this substrate", substrate.ErrValidation, authority)
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newID mints a bare 12-character lowercase base32 record ID: tokens, refs,
// fires and the other minted records. A repository is not one of them; its id
// is its authority.
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
