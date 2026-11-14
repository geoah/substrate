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
	"cmp"
	"context"
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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/oauthflow"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

type options struct {
	outsideCode           string
	unconfinedOutsideCode bool
	kindsFS               fs.FS
	kindsDir              string
	registry              *vocabulary.Registry
	oauthKey              string
	oauthURL              string
	oauthHTTP             *http.Client
	credKey               string
	dataRoot              string
	// archive is where each repository's archive is written (WithArchive),
	// nil for none; archiveInterval and archivePieceBytes tune its passes.
	archive           archive.Store
	archiveSet        bool
	archiveCall       time.Duration
	archiveIdle       time.Duration
	archiveInterval   time.Duration
	archivePieceBytes int64
	archiveStepBytes  int64
	// archiveFault is the archive pass's test seam (WithTestArchiveFault).
	archiveFault func(stage string) error
	// shippedRefusal is the shipped upgrade's test seam
	// (WithTestShippedUpgradeRefusal).
	shippedRefusal func(repository string) []string
	// peopleFault is the start's people pass's test seam (identityboot.go).
	peopleFault func(repository string) error
	// rulesFault is the readers of the rules' test seam (ruleFault).
	rulesFault func(what string) error
	// repositoryConns caps the pool every repository shares
	// (WithRepositoryConnections).
	repositoryConns int
	// conversionCeiling is the work ceiling (WithConversionCeiling); the set
	// flag tells an explicit zero (no ceiling) from the default.
	conversionCeiling    int64
	conversionCeilingSet bool
	// orphanGrace is how long a mapping target must have carried its orphan
	// mark before the sweep may collect it (WithOrphanCollection). Zero or
	// less is the default: the mark is made, nothing is collected.
	orphanGrace time.Duration
	// laneWorkers is how many schedule fires one pass runs at once
	// (WithTriggerLaneWorkers); scheduleLaneWorkers when zero.
	laneWorkers int
	// chatTurnsPerPerson is how many detached chat turns one person runs at
	// once (WithChatTurnsPerPerson).
	chatTurnsPerPerson int
	// healthFailingAfter is how long a trigger's deliveries must all have
	// parked before its failing alert opens (WithHealthFailingAfter); zero or
	// less is DefaultHealthFailingAfter.
	healthFailingAfter time.Duration
	// catchUpBatch is the page size of the boot's table-to-file catch-up
	// (appendFromTable); rebuildBatch when not positive. Only a test sets it
	// (export_test.go), to put a transaction across a page boundary.
	catchUpBatch int
	// valuesBudget is how many earlier entries one change read's before
	// values may read (changevalues.go); valuesBudget when not positive. Only
	// a test sets it (export_test.go), to outrun it without thousands of
	// writes.
	valuesBudget int
	// searchMatchMax, when set, replaces both match caps a search reads
	// through the index under (bm25.go indexedMatch). Only a test sets it
	// (export_test.go), to put a small fixture on the row path.
	searchMatchMax    int
	searchMatchMaxSet bool
	// searchBudget is the wall clock one lexical search may spend
	// (WithSearchBudget); DefaultSearchBudget when not positive.
	searchBudget time.Duration
	// searchReadMax is the most matching rows one candidate pass reads
	// (bm25.go lexicalCandidates); searchReadMax when not positive. Only a
	// test sets it (export_test.go), to cut a pass on a small fixture.
	searchReadMax int
	// embedRequestInputs and embedRequestBytes cap one embeddings request
	// the drain sends (search.go embedRequestInputs); the defaults when not
	// positive. Only a test sets them (export_test.go), to split a request
	// on a small fixture.
	embedRequestInputs int
	embedRequestBytes  int
	blobs              blobbytes.Backend
	log                *slog.Logger
	// operatorDefaults is the operator defaults file (WithOperatorDefaults);
	// nil is none. operatorName is the site's name its refusals give.
	operatorDefaults *operatordefaults.Config
	operatorName     string
	// operatorInstaller is the catalog install the operator's `install:`
	// list runs through (WithOperatorInstaller); nil installs nothing.
	operatorInstaller OperatorInstaller
	// searchSemanticHook is the semantic arm's test seam (export_test.go
	// WithTestSemanticHook); nil in production.
	searchSemanticHook func(ctx context.Context, tx *sql.Tx)
	// insecureAllowSuperuser downgrades the fail-closed role check to a warning
	// (WithInsecureAllowSuperuser). Dev/test only; never the production default.
	insecureAllowSuperuser bool
	// insecureDisableTOTP stops verifying the second factor
	// (WithInsecureDisableTOTP). Dev/test only; never the production default.
	insecureDisableTOTP bool
	// dirReadOnly opens the service beside a running server
	// (WithDirectoryReadOnly): no migration, no boot check, no writer, no
	// write.
	dirReadOnly bool
	// skipWriterLease opens without the per-repository writer lease
	// (export_test.go WithTestSkipWriterLease), so a test can be a second
	// writer on purpose. TESTS ONLY: a server has no such option, because
	// being the second writer is the thing decision 0083 refuses.
	skipWriterLease bool
	// seedLLMProviders writes the three keyless vendor rows at creation and
	// as an open catch-up. Default true; OpenForTest sets false so upgrade
	// fixtures that count live llm/provider rows stay the rows they wrote.
	seedLLMProviders    bool
	seedLLMProvidersSet bool
	// seedUnits writes the shipped unit and conversion records at creation
	// and as an open catch-up (unitseed.go).
	seedUnits    bool
	seedUnitsSet bool
	// seedRecords writes the data records the seeded packages ship beside
	// their declarations at creation and as an open catch-up (recordseed.go).
	seedRecords    bool
	seedRecordsSet bool
	// noActorFill skips the open's actor fill (WithTestActorFillOff).
	noActorFill bool
	// perRecordSeed seeds the units one put at a time instead of in one bulk
	// run, the test seam that holds the two to one answer.
	perRecordSeed bool
	// bulkFault and seedPath are the bulk seed's test seams (export_test.go
	// WithTestBulkFault, WithTestSeedPath): a hook run after each table a
	// bulk run's flush writes, and one told which path a seed took.
	bulkFault func(stage string) error
	seedPath  func(repository, refusal string)
	// importFault and importBatch are the boot import's test seams
	// (seams.go WithTestImportFault): a hook run at each durable step of an
	// import, so a test can stop the process there, and a batch size below
	// rebuildBatch, so a small history spans more than one batch. The seams
	// compile into the binary and are inert unless set.
	importFault func(stage string) error
	// beforeDirectories runs between the keying's rows and its directories.
	// Tests only.
	beforeDirectories func(s *service)
	importBatch       int
	eraseFault        func(stage string) error
	// conversion replaces the conversion chain (conversion.go), so a test
	// converts through a step no release carries. Tests only.
	conversion []conversionStep
	// commitFault is a write's test seam (export_test.go): a hook run at
	// each durable step of commitAndMirror, around the manifest write that
	// precedes the first append in a new changelog dialect and around the
	// Postgres commit, so a test can fail a step or stop the process there.
	// Tests only.
	commitFault func(stage string) error
	// commitTx is the COMMIT's own test seam (dataset.go commitTx): it is
	// handed the COMMIT and answers in its place, so a test can hold a
	// COMMIT, or answer one before Postgres has. Tests only.
	commitTx func(commit func() error) error
	// tokenProof is the token cache's test seam (dataset.go settleTokens):
	// run before each attempt to prove a doubted commit ended, and an error
	// fails the attempt. Tests only.
	tokenProof func() error
	// tokenProofAttempt replaces how long one attempt at that proof waits.
	// Tests only.
	tokenProofAttempt time.Duration
	// afterProof is the factor doors' test seam (auth.go afterProof): a hook
	// run with the door's name once a request has proved the factors and
	// before it acts. Tests only.
	afterProof func(door string)
	// measureHook is the measured-value checks' test seam (measured.go): a
	// hook run with the writing actor each time one of them does work.
	// Tests only.
	measureHook func(actor substrate.Actor, check string)
	// changeReadHook is the change tail's test seam (seams.go
	// WithTestChangeReadHook): a hook run with the repository each time a
	// stream's Changes reads the changelog's rows. Tests only.
	changeReadHook func(repository string)
	// tailHeadHook runs once the change tail has read the head it opens at
	// (changetail.go tailHead). Tests only.
	tailHeadHook func()
	// searchReindexBatch and searchReindexHook are the open-time reindex's
	// test seams (export_test.go WithTestSearchReindex): a page size below
	// searchReindexBatch, so a few rows span several pages, and a hook run
	// before each page with the kind it is about to re-derive, so a test can
	// hold the reindex there or fail the page. Tests only.
	searchReindexBatch int
	searchReindexHook  func(ctx context.Context, kind string) error
	// reprojectionHook is the background index re-derivation's test seam
	// (export_test.go WithTestReprojectionHook): a hook run before each
	// page with the kind it is about to re-derive. Tests only.
	reprojectionHook func(ctx context.Context, kind string) error
	// reprojectionPageHook is the same pass's seam inside a page's
	// transaction (export_test.go WithTestReprojectionPageHook). Tests only.
	reprojectionPageHook func(ctx context.Context, kind string) error
	// inadmissible is the loader's test seam (export_test.go
	// WithTestInadmissible): the stored packages it refuses as a binary
	// whose contract tightened would, so a test parks a package without
	// editing a stored row the changelog would then disagree with. Tests
	// only.
	inadmissible map[string]bool
	// progressEvery is how often a long walk reports its position
	// (progress.go); the test seam WithTestProgressEvery lowers it so a
	// short history reports at all.
	progressEvery time.Duration
	// archivePaceHook is the archive passes' pacing seam (export_test.go
	// WithTestArchivePaceHook): run after each copy a pass paces, with the
	// repository and the bytes so far. Tests only.
	archivePaceHook func(repository string, bytes int64)
	// archiveBytesPerSecond caps the archive passes' rate
	// (WithArchiveBytesPerSecond); zero is no cap.
	archiveBytesPerSecond int64
	// archiveYieldMax bounds a pass's wait for invocations (export_test.go
	// WithTestArchiveYieldMax); zero is archiveYieldMax. Tests only.
	archiveYieldMax time.Duration
	// operator is OpenOperator's: a substratectl process, which writes no
	// archive (archive.go).
	operator bool
	// invokeHook is the runner's test seam (seams.go WithTestInvokeHook): a
	// hook run with a function's identity as its body is about to be invoked
	// (runner.go runCallableRaw), so a test can act while the body runs.
	// Inert unless set.
	invokeHook func(function string)
	// foreignPackages lifts decision 0210 (WithInsecureForeignPackages).
	foreignPackages bool
	// withoutSecrets names the repositories imported without their secrets
	// (lostsecrets.go WithImportWithoutSecrets).
	withoutSecrets []string
	// namesNow is the clock a former name lapses on (repositorynames.go);
	// the wall clock when nil.
	namesNow func() time.Time
	// healthNow is the clock the failing-trigger read measures a park's age
	// on (seams.go WithTestHealthClock); the wall clock when nil.
	healthNow func() time.Time
	// scheduleNow is the clock a schedule trigger's due occurrences are read
	// on (seams.go WithTestScheduleClock); the wall clock when nil.
	scheduleNow func() time.Time
	// retryNow is the clock the walk of temporary failures reads a retry's
	// due time on (seams.go WithTestRetryClock); the wall clock when nil.
	retryNow func() time.Time
	// now is the TOTP verifier's clock (seams.go WithTestTOTPClock); the wall
	// clock when nil. A test that spends one window's codes advances it
	// instead of sleeping through a real 30 second step.
	now func() time.Time
	// webhookNow is the instant the webhook door records as a request's
	// receipt (export_test.go WithTestWebhookClock); nowUTC when nil. A test
	// pins a receipt to the millisecond instead of racing the wall clock.
	webhookNow func() time.Time
	// queryTracer sees every statement the shared repository pool sends
	// (export_test.go WithTestQueryTracer), so a test can assert which
	// statements a read ran and not only what it answered. Tests only; nil
	// traces nothing.
	queryTracer pgx.QueryTracer
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

// WithArchiveBytesPerSecond caps the rate at which the process's archive
// passes copy, over every repository it writes
// (SUBSTRATE_ARCHIVE_BYTES_PER_SECOND, archivepace.go). Zero is no cap; not
// given is DefaultArchiveBytesPerSecond. The passes also pause while any
// function body or agent loop runs, whatever the cap, for at most 30 seconds
// at a time.
func WithArchiveBytesPerSecond(n int64) Option {
	return func(o *options) { o.archiveBytesPerSecond = n }
}

// WithRepositoryConnections caps the Postgres connections every repository
// of the process shares (SUBSTRATE_REPOSITORY_CONNECTIONS). One repository
// takes at most half of them, and never more than eight. Zero, or not given,
// is DefaultRepositoryConnections; a cap under MinRepositoryConnections
// refuses the Open.
func WithRepositoryConnections(n int) Option { return func(o *options) { o.repositoryConns = n } }

// WithConversionCeiling bounds the live records one declaration change may
// rewrite in its transaction (SUBSTRATE_CONVERSION_CEILING, decision 0067): a
// plan whose estimated work is above n is refused on both doors, and the
// previews report it as a blocker. Zero or less is no ceiling;
// DefaultConversionCeiling when the option is not given.
func WithConversionCeiling(n int64) Option {
	return func(o *options) { o.conversionCeiling, o.conversionCeilingSet = n, true }
}

// WithOrphanCollection turns the sweep's ORPHAN COLLECTION on and sets its
// grace window (SUBSTRATE_ORPHAN_GRACE): a mapping target that has carried
// its orphan mark for longer than grace, and that no live record points at,
// is tombstoned by the GC sweep and collected with it (orphans.go).
//
// Zero or less is off, which is the default and the shipped behavior: the
// mark is derived either way, so an owner can list what has been orphaned
// (`filter.orphaned`) on a deployment that collects nothing. Collection is
// opt-in because "the last source went" is also the shape of a connector
// outage and of a re-seed in flight, and the cost of being wrong is somebody's
// records.
func WithOrphanCollection(grace time.Duration) Option {
	return func(o *options) { o.orphanGrace = grace }
}

// WithTriggerLaneWorkers sets how many due schedule fires one repository's
// dispatcher pass runs at once, each of a different trigger
// (SUBSTRATE_TRIGGER_LANE_WORKERS, decision 0150). Zero, or not given, is
// the default of 4. A negative value refuses the Open, as the server's
// configuration does, and so does one above TriggerDeliverySlots, because
// no pass could ever run more than the process does.
func WithTriggerLaneWorkers(n int) Option { return func(o *options) { o.laneWorkers = n } }

// WithChatTurnsPerPerson sets how many detached chat turns one person runs at
// once (SUBSTRATE_CHAT_TURNS_PER_PERSON, agents.go ChatAgent). Zero, or not
// given, is maxRunningChatTurns.
func WithChatTurnsPerPerson(n int) Option { return func(o *options) { o.chatTurnsPerPerson = n } }

// WithHealthFailingAfter sets how long every delivery of a trigger must have
// parked, since its newest ok run, before the dispatcher opens the trigger's
// `trigger.failing/<trigger id>` alert (SUBSTRATE_HEALTH_FAILING_AFTER,
// health.go). Zero or less, or not given, is DefaultHealthFailingAfter.
func WithHealthFailingAfter(d time.Duration) Option {
	return func(o *options) { o.healthFailingAfter = d }
}

// WithSearchBudget sets the wall clock one lexical search may spend (bm25.go
// lexical): past it the relaxed top-up is dropped and the search answers what
// matched every word, marked capped, or fails when nothing was found yet. Zero
// or less, or not given, is DefaultSearchBudget.
func WithSearchBudget(d time.Duration) Option {
	return func(o *options) { o.searchBudget = d }
}

// WithArchive writes each repository's archive to store, behind its commits
// (archive.go). Without it the archive is <data root>/archive; nil writes
// none.
func WithArchive(store archive.Store) Option {
	return func(o *options) { o.archive, o.archiveSet = store, true }
}

// WithArchiveTimeouts bounds every call to the archive's store (archive.
// Bounded): call for a list, a delete or a get's response, idle for a body
// that stops moving. The defaults are archive.DefaultCallTimeout and
// archive.DefaultIdleTimeout.
func WithArchiveTimeouts(call, idle time.Duration) Option {
	return func(o *options) { o.archiveCall, o.archiveIdle = call, idle }
}

// WithArchiveInterval sets how often a repository's archive pass runs.
func WithArchiveInterval(d time.Duration) Option { return func(o *options) { o.archiveInterval = d } }

// WithDirectoryReadOnly opens the service as a second process beside a running
// server: the operator hat's `repository verify` and `reembed`. Open applies
// no schema migration: it refuses a database missing one this binary carries
// (ErrDatabaseOlder) as it refuses one a newer binary migrated
// (ErrDatabaseNewer). It builds no declared index and runs no boot check and
// no orphan sweep. A dataset runs no repository migration and refuses a
// repository with one pending, opens no changelog writer and refuses every
// inTx write with ErrDirectoryReadOnly, and VerifyRepository reports an
// incomplete tail or a table ahead of its file as findings instead of
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

// WithInsecureForeignPackages lifts decision 0210 (seed.go
// authorizeNewPackage): a token may create a package under any authority,
// which is the hand apply of a shipped provider's rewired files that the
// provider end-to-end suite runs. It exists for a local substrate
// (SUBSTRATE_INSECURE_ALLOW_FOREIGN_PACKAGES) until a development document
// replaces it; never set it where the substrate is reachable, because any
// token may then squat another authority's package names.
func WithInsecureForeignPackages(on bool) Option {
	return func(o *options) { o.foreignPackages = on }
}

// WithLLMProviderSeed controls whether CreateRepository and repository open
// write the three keyless vendor rows (openai, anthropic, gemini). Production
// Open defaults on. OpenForTest turns it off so fixtures that count live
// llm/provider rows see only the rows they wrote.
func WithLLMProviderSeed(on bool) Option {
	return func(o *options) { o.seedLLMProviders, o.seedLLMProvidersSet = on, true }
}

// WithUnitSeed controls whether CreateRepository and repository open write
// the shipped unit and conversion records (decision 0186, unitseed.go).
// Production Open defaults on. OpenForTest turns it off so the suite does not
// write several hundred records into every repository; a test that needs the
// units asks for them.
func WithUnitSeed(on bool) Option {
	return func(o *options) { o.seedUnits, o.seedUnitsSet = on, true }
}

// WithOperatorDefaults gives the service the operator's defaults file
// (decision 0235, internal/operatordefaults): an empty, operator-listed
// llm/provider row runs on the operator's transport, the operator's embeddings
// entry serves a repository whose rows claim none, and every such call is
// charged to the repository's allowance for the UTC day. name is the site's
// name (site.yaml), which the refusals and the operator defaults read give;
// empty reads as "this server". nil, or not given, is no operator defaults,
// and the service behaves exactly as it does without the feature.
func WithOperatorDefaults(c *operatordefaults.Config, name string) Option {
	return func(o *options) { o.operatorDefaults, o.operatorName = c, name }
}

// WithRecordSeed controls whether CreateRepository and repository open write
// the data records the seeded packages ship beside their declarations, such
// as the console's collection views (decision 0229, recordseed.go).
// Production Open defaults on. OpenForTest turns it off so fixtures that
// count records see only the ones they wrote; a test that needs them asks.
func WithRecordSeed(on bool) Option {
	return func(o *options) { o.seedRecords, o.seedRecordsSet = on, true }
}

type service struct {
	outsideCode           string
	unconfinedOutsideCode bool
	dsn                   string
	// operatorDefaults is the operator defaults file (WithOperatorDefaults);
	// nil is none.
	operatorDefaults *operatordefaults.Config
	// operatorName is the site's name the operator's refusals give; empty
	// reads as "this server" (operatorspend.go siteName).
	operatorName string
	// operatorInstaller runs the operator's `install:` list
	// (operatorinstall.go); nil installs nothing.
	operatorInstaller OperatorInstaller
	// operatorInstalls holds the last failed operator install per repository
	// and package, for the operator defaults read.
	operatorInstalls operatorInstallFailures
	// operatorNow is the allowance ledger's clock (operatorspend.go); never
	// nil, and a test sets it before the dataset's first operator-paid call.
	// The refused-key table's backoff runs on it too.
	operatorNow func() time.Time
	// keyRefusals is the keys a vendor refused lately, the operator's for
	// every repository and a person's own for theirs (keyrefusals.go).
	keyRefusals keyRefusals
	// admin is the DSN's own user: the DDL, the role setup, and the index
	// materialization the bound roles are not allowed to run.
	admin *sql.DB
	// maint is the BYPASSRLS pool (substrate_maint): the control-plane table,
	// the repository lookup and anything that must read across repositories.
	// It carries NO repository setting, so an accidental repository-scoped
	// insert through it raises instead of landing somewhere arbitrary.
	maint *sql.DB
	// repoPool is the one pool every repository's connections come from,
	// each pinned to its repository as it is handed out (scope.go scopedDB).
	// Connections held stay bounded by its cap however many repositories
	// the process has opened.
	repoPool *pgxpool.Pool
	// repoConnsPerHandle is what one repository may take of repoPool
	// (repositoryConnsPerHandle).
	repoConnsPerHandle int
	// migrationConns and catchUpConns budget the connections dialed outside
	// repoPool (scope.go MigrationConnections, CatchUpConnections).
	migrationConns, catchUpConns connBudget
	// testMigrationHook, when set (tests only), runs inside each repository
	// migration's transaction, with its own connection held.
	testMigrationHook func()
	// testGCPassHook, when set (tests only), runs after each GC pass's
	// victim query with the number of victims it found and before the pass
	// locks any of them (gc.go gcPass), so a test can restore a victim in
	// that window and count the passes a sweep runs.
	testGCPassHook func(victims int)
	// testOAuthCallBudget, when set (tests only), replaces oauthCallBudget,
	// so a test can run a token call past its bound without waiting 30 s.
	testOAuthCallBudget time.Duration
	// testOAuthBeforeStamp, when set (tests only), runs after a refused
	// refresh found its credential not replaced and before the refusal is
	// stamped on the account (oauth.go refreshCredential), so a test can land
	// a reconnect in that window.
	testOAuthBeforeStamp func()
	// testOAuthPoint, when set (tests only), runs at each oauthPoint* point
	// of the OAuth facility (operatoroauth.go), so a test can land a client
	// change or a reconnect in the window the point names.
	testOAuthPoint func(point string)
	// appRole is the role every repository-scoped pool assumes; empty when the
	// cluster would not let the engine create its roles.
	appRole string
	// unregisterMetrics drops the admin and maint pools' sql.DBStats
	// collectors (internal/metrics) when the service closes, so a test that
	// opens a service per case does not leave closed pools published.
	unregisterMetrics []func()

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
	// instanceID names this server to a domain's `_substrate` record
	// (instance.go, decision 0208).
	instanceID string
	// conversionCeiling is the most live records one declaration change may
	// rewrite (convert.go admitConversion); zero or less is no ceiling.
	conversionCeiling int64
	// orphanGrace is the sweep's orphan-collection window; zero or less
	// collects nothing (orphans.go collectOrphans).
	orphanGrace time.Duration
	// laneWorkers is how many schedule fires one pass's schedule lane runs
	// at once (functions.go scheduleLane).
	laneWorkers int
	// chatTurnsPerPerson is the chat turns one person runs at once.
	chatTurnsPerPerson int
	// deliverySlots is the process-wide semaphore every dispatcher pass
	// takes a slot of for each delivery it runs, record trigger walks and
	// schedule fires alike (functions.go takeDeliverySlot), so the
	// dispatcher never has more than TriggerDeliverySlots in flight however
	// many passes run.
	deliverySlots chan struct{}
	// healthFailingAfter is the failing window (health.go), always positive.
	healthFailingAfter time.Duration
	// foreignPackages is options.foreignPackages.
	foreignPackages bool
	// withoutSecrets is options.withoutSecrets, by id and by name.
	withoutSecrets map[string]bool
	// testNamesNow is the clock a former name lapses on, or nil for the
	// database's, run on the monotonic clock (namesNow).
	testNamesNow func() time.Time
	// namesBase is the database's clock when the service opened, and
	// namesStart this process's monotonic reading at that moment: namesNow
	// runs the one forward by the other.
	namesBase, namesStart time.Time
	// healthNow is the failing-trigger read's clock (health.go), never nil.
	healthNow func() time.Time
	// scheduleNow is the clock a schedule's due occurrences are read on
	// (dataset.scheduleNow).
	scheduleNow func() time.Time
	// retryNow is the temporary-failure walk's clock (temporaryretry.go),
	// never nil.
	retryNow func() time.Time
	// catchUpBatch is the page size of the table-to-file catch-up.
	catchUpBatch int
	// archive is the store each repository's archive goes to, nil for none
	// (archive.go): written by a server's passes, read by its boot check and
	// by an operator's verify. archiving is whether this process runs the
	// passes: the server's, not an operator's or a read-only process's.
	archive           archive.Store
	archiving         bool
	archiveInterval   time.Duration
	archivePieceBytes int64
	// archiveStepBytes bounds the blob bytes one pass copies, and
	// archiveProcess is this process's id in the record's rows: an object
	// no manifest selects is reused only by the process that put it.
	archiveStepBytes int64
	archiveProcess   string
	// testArchiveFault is the options' archive pass seam (archive.go).
	testArchiveFault func(stage string) error
	// testShippedRefusal is the options' shipped upgrade seam (seed.go).
	testShippedRefusal func(repository string) []string
	// testPeopleFault is the options' people pass seam (identityboot.go):
	// an error fails that repository's pass.
	testPeopleFault func(repository string) error
	// testRulesFault is the options' seam at the readers of the rules
	// (ruleFault).
	testRulesFault func(what string) error
	// valuesBudget is what one change read's before values may read.
	valuesBudget int
	// searchMatchMax replaces the search match caps; negative keeps them.
	searchMatchMax int
	// searchBudget is one lexical search's wall clock, always positive.
	searchBudget time.Duration
	// searchReadMax is what one candidate pass reads, always positive.
	searchReadMax int
	// embedRequestInputs and embedRequestBytes cap one embeddings request
	// the drain sends, always positive.
	embedRequestInputs int
	embedRequestBytes  int
	// unitGraphLoadHook, set only by a test, runs on a graph cache miss
	// between the revision read and the row read: the seam a concurrent
	// conversion commits in.
	unitGraphLoadHook func()
	// searchTopUpHook, set only by a test, runs before a lexical search's
	// relaxed top-up: the seam that spends the budget between the passes.
	searchTopUpHook func()
	// queryToolTimeout, set only by a test, replaces
	// DefaultAgentToolTimeoutSec when positive, so a test can show a function
	// call outliving the `query` default without waiting 30 s.
	queryToolTimeout time.Duration
	// searchScoreHook, set only by a test, runs before a lexical search
	// scores its candidates: the seam that spends the budget before scoring.
	searchScoreHook func()
	// searchPreRankHook, set only by a test, runs with the context the strict
	// candidate pass reads the vectors it pre-ranks under, before it reads
	// them: the seam a cold read of them spends the budget in.
	searchPreRankHook func(context.Context)
	// searchCountHook, set only by a test, runs with the context the strict
	// candidate pass counts its matches under, before it counts them: the
	// seam a count that reads vectors spends the budget in.
	searchCountHook func(context.Context)
	// searchIdentityHook, set only by a test, runs with the context the
	// strict candidate pass reads its matching identities under, before it
	// reads them: the seam a cold walk of a common word's rows spends the
	// budget in.
	searchIdentityHook func(context.Context)
	// searchSampleHook, set only by a test, runs with the context the strict
	// candidate pass reads its matches through the index under, past a count
	// or an identity read that ran out of its share, before it reads them:
	// the seam a lossy bitmap's recheck spends that read's deadline in.
	searchSampleHook func(context.Context)
	// searchSemanticHook, set only by a test, runs with the semantic arm's
	// context and transaction after the scan settings and before the
	// candidate read: the seam that runs a statement the arm's budget
	// cancels, or cancels the caller, there.
	searchSemanticHook func(ctx context.Context, tx *sql.Tx)
	// blobs is where blob bytes live (WithBlobStore); the fs backend under
	// the data root by default.
	blobs blobbytes.Backend
	// totpDisabled stops verifying the second factor (WithInsecureDisableTOTP):
	// the password is then the whole credential. Dev only.
	totpDisabled bool
	// now is the clock the TOTP verifier reads; nowUTC outside a test.
	now func() time.Time
	// webhookNow is the webhook door's receipt clock (webhooks.go
	// admitWebhook); nowUTC outside a test, never nil.
	webhookNow func() time.Time
	// readOnly is WithDirectoryReadOnly: this process is not the repository
	// directories' writer and must not become one (repodir.go).
	readOnly bool
	// lease is this process's claim on the repositories it writes
	// (writerlease.go). Nil on a read-only service, which claims nothing,
	// and on the test seam that opens a second writer on purpose.
	lease *writerLease
	log   *slog.Logger
	// seedLLMProviders writes the three keyless vendor rows at creation
	// and as an open catch-up.
	seedLLMProviders bool
	// seedUnits writes the shipped unit and conversion records at creation
	// and as an open catch-up (unitseed.go).
	seedUnits bool
	// seedRecords writes shippedRecords at creation and as an open catch-up
	// (recordseed.go).
	seedRecords bool
	// noActorFill skips the open's actor fill, for a test that holds a
	// repository as a binary from before it left it (actorfill.go).
	noActorFill bool
	// shippedRecords are the data records the shipped tree carries beside its
	// declarations, in file order; none for a service built from a registry.
	shippedRecords []vocabulary.ShippedRecord
	// shippedModels are the model ids the shipped llm/model records name:
	// public, so the usage series show them in clear (usagemetrics.go).
	shippedModels map[string]bool
	// bulkSeed writes a seed the repository holds none of in one bulk run
	// (bulkwrite.go); off only under the test seam WithTestPerRecordSeed.
	bulkSeed bool
	// unitGraphs holds each repository's unit graphs by revision
	// (unitgraph.go).
	unitGraphs unitGraphCache
	// tokens is the token lookups this process answered (tokencache.go),
	// and proofs the waits that lift a doubted commit's fence (dataset.go).
	tokens tokenCache
	proofs tokenProofs
	// bg counts and bounds every detached task the engine starts
	// (background.go); Close drains it before any pool closes.
	bg *background

	mu sync.Mutex
	// datasets is keyed by REPOSITORY ID, the authority: the repository's one
	// name, on disk, in the database and on the wire (decision 0052).
	datasets map[string]*dataset
	// opening is the per-repository singleflight (claimOpen): an id maps to
	// the channel the holder closes when it is done, either way. It exists
	// because the open ladder is not safe to run twice at once on one
	// repository, and because a creation writes the new repository's
	// directory with a writer of its own, which an open must not meet.
	opening map[string]chan struct{}

	// testFailAfterSeed, when set (tests only), forces createSeededRepository to
	// fail AFTER the seed transaction commits and BEFORE the control-plane row —
	// the exact crash window the erase-and-sweep guarantees cover.
	testFailAfterSeed func() error
	// testFailAfterRow, when set (tests only), fails a creation after its
	// control-plane row and its person are written.
	testFailAfterRow func() error
	// testIndexFault, when set (tests only), runs before each attempt to
	// write a person's index row after their actor committed (people.go
	// indexPerson); an error fails that attempt.
	testIndexFault func() error
	// testPersonInDoubt, when set (tests only), is told what the index's
	// reconciliation after an addition in doubt found: whether the live
	// actor names the identity.
	testPersonInDoubt func(named bool)
	// testAfterErase, when set (tests only), runs once an erase has
	// committed: a crash there must leave nothing the erase owed.
	testAfterErase func(repository string)
	// restoredCopies is, for each person a restore in this process wrote,
	// the version of the factors it wrote (archiveidentities.go): a row
	// still at that version is the restore's copy, which a later copy of
	// the person replaces.
	restoredMu     sync.Mutex
	restoredCopies map[string]time.Time
	// testOpenWaits, when set (tests only), runs when claimOpen finds the
	// repository's slot held and is about to wait for it.
	testOpenWaits func(repository string)
	// names is every repository's names, by id (namescache.go): the one
	// place in memory they are, and namesProofs its waiters for a COMMIT
	// whose answer was lost. testNamesCommit, when set (tests only), commits
	// a transaction that changes names in tx.Commit's place.
	names           namesCache
	namesProofs     namesProofs
	testNamesCommit func(tx *sql.Tx) error
	// testReconcileRead, when set (tests only), runs when a reconcile of a
	// repository's record has read the name from its row.
	testReconcileRead func(id, name string)
	// testOpenRead, when set (tests only), runs when an open has read the
	// repository's row under its slot and before it opens the dataset.
	testOpenRead func(repository string)
	// testImportFault and testImportBatch are the options' import seams
	// (repodir.go importFault, history.go insertFrom). Tests only.
	testImportFault func(stage string) error
	// testBeforeDirectories is options.beforeDirectories.
	testBeforeDirectories func(s *service)
	testImportBatch       int
	// testEraseFault is the options' deletion seam (repositories.go
	// eraseRepositoryOn). Tests only.
	testEraseFault func(stage string) error
	// keyingLeases are the writer leases the boot's keying took under
	// repositories' names, held until the boot check has published each of
	// those repositories under its id (repositoryid.go).
	keyingLeases []string
	// aside is every repository this process's start set aside, with why
	// (setaside.go): open refuses each, and Repositories leaves them out.
	aside setAsideSet
	// conversion is the conversion chain this service runs, and maxDialect
	// the changelog dialect it converts to and stamps (conversion.go): the
	// binary's own, or a test's.
	conversion []conversionStep
	maxDialect int
	// testCommitFault is the options' commit seam (dataset.go commit).
	// Tests only.
	testCommitFault func(stage string) error
	// testCommitTx and testTokenProof are the options' COMMIT and token
	// proof seams (dataset.go). Tests only.
	testCommitTx          func(commit func() error) error
	testTokenProof        func() error
	testTokenProofAttempt time.Duration
	// testAfterProof is the options' factor-door seam (auth.go). Tests only.
	testAfterProof func(door string)
	// testMeasureHook is the options' measured-value seam (measured.go).
	// Tests only.
	testMeasureHook func(actor substrate.Actor, check string)
	// testChangeReadHook is the options' change tail seam (changetail.go).
	// Tests only.
	testChangeReadHook func(repository string)
	// testTailHeadHook is the options' tailHeadHook. Tests only.
	testTailHeadHook func()
	// testBulkFault and testSeedPath are the options' bulk seed seams
	// (bulkwrite.go flushBulk, unitseed.go seedUnits). Tests only.
	testBulkFault func(stage string) error
	testSeedPath  func(repository, refusal string)
	// testInvokeHook is the options' runner seam (runner.go). Tests only.
	testInvokeHook func(function string)
	// searchReindexBatch and testSearchReindexHook are the options' reindex
	// seams (searchindex.go reindexKind); a batch of zero is
	// searchReindexBatch. Tests only.
	searchReindexBatch    int
	testSearchReindexHook func(ctx context.Context, kind string) error
	// testReprojectionHook is the options' reprojection hook
	// (reprojection.go reprojectKind). Tests only.
	testReprojectionHook func(ctx context.Context, kind string) error
	// testReprojectionPageHook is the options' hook inside a page's
	// transaction (reprojection.go reprojectKind). Tests only.
	testReprojectionPageHook func(ctx context.Context, kind string) error
	// testInadmissible is the options' loader seam (vocabularywrite.go
	// admitStored). Tests only.
	testInadmissible map[string]bool
	// progressEvery is how often a long walk of a changelog reports where
	// it is (progress.go).
	progressEvery time.Duration
	// archiveBucket is the token bucket every repository's archive passes
	// draw from (archivepace.go), at the configured bytes per second for the
	// whole process; zero is no cap.
	archiveBucket archiveBucket
	// invocations counts the function bodies and agent loops in flight in
	// this process, nested ones included (archivepace.go invocationStarted):
	// what an archive pass yields to.
	invocations atomic.Int64
	// archiveYieldMax is the longest a pass waits for invocations to return
	// before it copies on (archivepace.go yield).
	archiveYieldMax time.Duration
	// testArchivePaceHook is the options' archive pace hook. Tests only.
	testArchivePaceHook func(repository string, bytes int64)
}

// Open connects to Postgres, loads the schema files, ensures the two roles and
// runs the shared schema's DDL. It provisions nothing: a repository exists
// once its control-plane row does.
func Open(ctx context.Context, dsn string, opts ...Option) (substrate.Service, error) {
	s, err := open(ctx, dsn, opts...)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// OpenOperator opens the engine the way Open does and returns it with the
// operator hat's methods (operator.go). Only substratectl calls it.
func OpenOperator(ctx context.Context, dsn string, opts ...Option) (Operator, error) {
	s, err := open(ctx, dsn, append(opts, func(o *options) { o.operator = true })...)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func open(ctx context.Context, dsn string, opts ...Option) (*service, error) {
	o := options{log: slog.Default(), now: nowUTC, webhookNow: nowUTC, progressEvery: progressEvery, archiveBytesPerSecond: DefaultArchiveBytesPerSecond}
	for _, fn := range opts {
		fn(&o)
	}
	reg := o.registry
	var shippedRecords []vocabulary.ShippedRecord
	switch {
	case reg != nil:
	case o.kindsFS != nil, o.kindsDir != "":
		fsys := o.kindsFS
		if fsys == nil {
			fsys = os.DirFS(o.kindsDir)
		}
		r, records, err := vocabulary.LoadShipped(fsys)
		if err != nil {
			return nil, err
		}
		reg, shippedRecords = r, records
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
	instanceID, err := loadInstanceID(o.dataRoot, o.dirReadOnly)
	if err != nil {
		return nil, err
	}
	if o.catchUpBatch <= 0 {
		o.catchUpBatch = rebuildBatch
	}
	if o.archiveInterval <= 0 {
		o.archiveInterval = DefaultArchiveInterval
	}
	// The archive is on by default, under the data root, as the server's
	// SUBSTRATE_ARCHIVE is; WithArchive(nil) turns it off.
	if !o.archiveSet && o.dataRoot != "" {
		store, err := archive.NewDir(filepath.Join(o.dataRoot, "archive"))
		if err != nil {
			return nil, err
		}
		o.archive = store
	}
	// No call to the store waits on it forever: a store that accepts a
	// connection and never answers fails a call, and the boot, a pass or a
	// deletion goes on from there.
	if o.archive != nil {
		o.archive = markStoreFailures(archive.Bounded(o.archive, cmp.Or(o.archiveCall, archive.DefaultCallTimeout), cmp.Or(o.archiveIdle, archive.DefaultIdleTimeout)))
	}
	if o.archivePieceBytes <= 0 {
		o.archivePieceBytes = DefaultArchivePieceBytes
	}
	if o.archiveStepBytes <= 0 {
		o.archiveStepBytes = DefaultArchiveStepBytes
	}
	if o.valuesBudget <= 0 {
		o.valuesBudget = valuesBudget
	}
	if o.healthFailingAfter <= 0 {
		o.healthFailingAfter = DefaultHealthFailingAfter
	}
	if o.healthNow == nil {
		o.healthNow = time.Now
	}
	if o.scheduleNow == nil {
		o.scheduleNow = time.Now
	}
	if o.retryNow == nil {
		o.retryNow = time.Now
	}
	if !o.searchMatchMaxSet {
		o.searchMatchMax = -1
	}
	if o.searchBudget <= 0 {
		o.searchBudget = DefaultSearchBudget
	}
	if o.embedRequestInputs <= 0 {
		o.embedRequestInputs = embedRequestInputs
	}
	if o.embedRequestBytes <= 0 {
		o.embedRequestBytes = embedRequestBytes
	}
	if o.searchReadMax <= 0 {
		o.searchReadMax = searchReadMax
	}
	if !o.conversionCeilingSet {
		o.conversionCeiling = DefaultConversionCeiling
	}
	if o.laneWorkers == 0 {
		o.laneWorkers = scheduleLaneWorkers
	}
	if o.chatTurnsPerPerson == 0 {
		o.chatTurnsPerPerson = maxRunningChatTurns
	}
	if o.chatTurnsPerPerson < 0 {
		return nil, fmt.Errorf("substrate/engine: %d chat turns per person: at least 1, and 0 is the default of %d (WithChatTurnsPerPerson)", o.chatTurnsPerPerson, maxRunningChatTurns)
	}
	if o.laneWorkers < 0 || o.laneWorkers > TriggerDeliverySlots {
		return nil, fmt.Errorf("substrate/engine: %d trigger lane workers: a lane runs at least 1 and at most the %d deliveries the whole process runs at once, and 0 is the default of %d (WithTriggerLaneWorkers)", o.laneWorkers, TriggerDeliverySlots, scheduleLaneWorkers)
	}
	if !o.seedLLMProvidersSet {
		o.seedLLMProviders = true
	}
	if !o.seedUnitsSet {
		o.seedUnits = true
	}
	if !o.seedRecordsSet {
		o.seedRecords = true
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
	// database/sql keeps two idle connections by default, so a burst past two
	// would dial, authenticate and close the rest; idle ones go back to the
	// cluster on the shared pool's schedule.
	admin.SetMaxIdleConns(4)
	admin.SetConnMaxIdleTime(repositoryConnIdle)
	if o.blobs == nil {
		fsBlobs, err := blobbytes.NewFS(o.dataRoot)
		if err != nil {
			_ = admin.Close()
			return nil, err
		}
		o.blobs = fsBlobs
	}
	process, err := changelogfile.NewPassID()
	if err != nil {
		_ = admin.Close()
		return nil, err
	}
	s := &service{
		dsn:          dsn,
		admin:        admin,
		base:         reg,
		credKey:      credKey,
		credKeyID:    hostKeyID(credKey),
		dataRoot:     o.dataRoot,
		instanceID:   instanceID,
		catchUpBatch: o.catchUpBatch,
		valuesBudget: o.valuesBudget,
		blobs:        o.blobs,

		searchMatchMax: o.searchMatchMax,
		searchBudget:   o.searchBudget,
		searchReadMax:  o.searchReadMax,

		embedRequestInputs: o.embedRequestInputs,
		embedRequestBytes:  o.embedRequestBytes,

		searchSemanticHook: o.searchSemanticHook,

		conversionCeiling:  o.conversionCeiling,
		orphanGrace:        o.orphanGrace,
		healthFailingAfter: o.healthFailingAfter,
		healthNow:          o.healthNow,
		testNamesNow:       o.namesNow,
		retryNow:           o.retryNow,
		scheduleNow:        o.scheduleNow,
		foreignPackages:    o.foreignPackages,
		withoutSecrets:     lostSecretsList(o.withoutSecrets),
		laneWorkers:        o.laneWorkers,
		chatTurnsPerPerson: o.chatTurnsPerPerson,
		deliverySlots:      make(chan struct{}, TriggerDeliverySlots),
		totpDisabled:       o.insecureDisableTOTP,
		operatorDefaults:   o.operatorDefaults,
		operatorName:       o.operatorName,
		operatorInstaller:  o.operatorInstaller,
		operatorNow:        time.Now,
		outsideCode:        o.outsideCode, unconfinedOutsideCode: o.unconfinedOutsideCode,
		now:              o.now,
		webhookNow:       o.webhookNow,
		readOnly:         o.dirReadOnly,
		seedLLMProviders: o.seedLLMProviders,
		seedUnits:        o.seedUnits,
		seedRecords:      o.seedRecords,
		noActorFill:      o.noActorFill,
		shippedRecords:   shippedRecords,
		shippedModels:    shippedModelIDs(shippedRecords),
		bulkSeed:         !o.perRecordSeed,
		log:              o.log,
		bg:               newBackground(),
		datasets:         map[string]*dataset{},
		opening:          map[string]chan struct{}{},

		testImportFault:       o.importFault,
		testBeforeDirectories: o.beforeDirectories,
		testImportBatch:       o.importBatch,
		testEraseFault:        o.eraseFault,
		archive:               o.archive,
		archiving:             o.archive != nil && !o.dirReadOnly && !o.operator,
		archiveInterval:       o.archiveInterval,
		archivePieceBytes:     o.archivePieceBytes,
		archiveStepBytes:      o.archiveStepBytes,
		archiveProcess:        process,
		testArchiveFault:      o.archiveFault,
		testShippedRefusal:    o.shippedRefusal,
		testPeopleFault:       o.peopleFault,
		testRulesFault:        o.rulesFault,
		conversion:            conversionSteps,
		maxDialect:            maxChangelogDialect,
		testCommitFault:       o.commitFault,
		testAfterProof:        o.afterProof,
		testCommitTx:          o.commitTx,
		testTokenProof:        o.tokenProof,
		testTokenProofAttempt: o.tokenProofAttempt,
		proofs:                tokenProofs{slots: make(chan struct{}, tokenProofWaits)},
		testMeasureHook:       o.measureHook,
		testChangeReadHook:    o.changeReadHook,
		testTailHeadHook:      o.tailHeadHook,
		testBulkFault:         o.bulkFault,
		testSeedPath:          o.seedPath,
		progressEvery:         o.progressEvery,
		testInvokeHook:        o.invokeHook,

		archiveBucket:       archiveBucket{rate: o.archiveBytesPerSecond},
		archiveYieldMax:     cmp.Or(o.archiveYieldMax, archiveYieldMax),
		testArchivePaceHook: o.archivePaceHook,

		searchReindexBatch:       o.searchReindexBatch,
		testSearchReindexHook:    o.searchReindexHook,
		testReprojectionHook:     o.reprojectionHook,
		testReprojectionPageHook: o.reprojectionPageHook,
		testInadmissible:         o.inadmissible,
	}
	if o.conversion != nil {
		if err := checkConversionSteps(o.conversion); err != nil {
			_ = admin.Close()
			return nil, fmt.Errorf("substrate/engine: the conversion chain: %w", err)
		}
		s.conversion, s.maxDialect = o.conversion, chainDialect(o.conversion)
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
	//
	// A read-only process applies none (checkMigrated): it runs beside a live
	// server, and a newer binary migrating that server's database would close
	// the server's rollback. It refuses a database it would migrate, as the
	// runner refuses one a newer binary migrated.
	schemaGate := migrate
	if s.readOnly {
		schemaGate = checkMigrated
	}
	if err := schemaGate(ctx, admin); err != nil {
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
	// Four for the work, plus one the writer lease pins for the life of the
	// process (writerlease.go): a cap of four would leave three for a
	// registration that already holds one of them itself.
	maint.SetMaxOpenConns(5)
	// Every authenticated request reads the token here, so the pool keeps what
	// it opened rather than database/sql's default of two idle connections,
	// which made a burst of console requests dial and close the rest.
	maint.SetMaxIdleConns(5)
	maint.SetConnMaxIdleTime(repositoryConnIdle)
	s.maint = maint
	// The clock a former name lapses on: the database's, read once and run
	// forward on this process's monotonic clock, so a step of this machine's
	// wall clock moves no hold (repositorynames.go namesNow).
	if err := maint.QueryRowContext(ctx, `SELECT now()`).Scan(&s.namesBase); err != nil {
		_ = maint.Close()
		_ = admin.Close()
		return nil, fmt.Errorf("substrate/engine: read the database's clock: %w", err)
	}
	s.namesBase, s.namesStart = s.namesBase.UTC(), time.Now()
	repoConns := o.repositoryConns
	if repoConns == 0 {
		repoConns = DefaultRepositoryConnections
	}
	repoPool, err := openRepositoryPool(ctx, dsn, s.appRole, repoConns, o.queryTracer)
	if err != nil {
		_ = maint.Close()
		_ = admin.Close()
		return nil, err
	}
	s.repoPool = repoPool
	s.migrationConns = newConnBudget(MigrationConnections)
	s.catchUpConns = newConnBudget(CatchUpConnections)
	s.repoConnsPerHandle = repositoryConnsPerHandle(repoConns)
	// The two process-wide pools, published by role; a repository's pool
	// publishes itself by authority when it opens (openNew).
	s.unregisterMetrics = []func(){
		metrics.RegisterDBStats("admin", admin),
		metrics.RegisterDBStats("maint", maint),
		metrics.RegisterSetAside(s.aside.count),
		metrics.RegisterPoolStats("repositories", func() metrics.PoolStats {
			st := repoPool.Stat()
			return metrics.PoolStats{
				Max: st.MaxConns(), Total: st.TotalConns(), Acquired: st.AcquiredConns(), Idle: st.IdleConns(),
				Acquires: st.AcquireCount(), Waits: st.EmptyAcquireCount(), Canceled: st.CanceledAcquireCount(),
				WaitDuration: st.EmptyAcquireWaitTime().Seconds(),
			}
		}),
	}
	// Role EXISTENCE with the right attributes is one thing; that the pools
	// ACTUALLY assume them at runtime — and that the DSN user is not itself a
	// superuser slipping past — is the check "enforced, not disciplined" needs.
	// Assert the effective principal on the maintenance pool and on a probe
	// scoped pool (the shape every request rides), unless the dev escape hatch
	// deliberately runs degraded.
	if !degraded {
		if err := assertPoolPrincipal(ctx, maint, roleMaint, true); err != nil {
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, fmt.Errorf("substrate/engine: maintenance pool principal: %w", err)
		}
		if err := s.assertAppPoolPrincipal(ctx); err != nil {
			repoPool.Close()
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
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
	}
	// The shipped vocabulary's declared indexes, ONCE PER PROCESS, at boot
	// and not from the open path a request drives. The builds are
	// concurrent, as an apply's are (ensureIndices), so a boot beside a
	// running server does not stall that server's writes. What arrives later
	// (a bundle's kinds) is materialized by the schema write that admits it.
	// A read-only process builds none: it writes nothing under the running
	// server, and the server's own boot builds what its vocabulary declares.
	// The scratch indexes a dead rebuild left go first (sweepScratchIndexes).
	if !s.readOnly {
		if err := sweepScratchIndexes(ctx, admin); err != nil {
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
		if err := ensureIndices(ctx, admin, reg.Kinds(), indexProgress{}); err != nil {
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
	}
	// FAIL CLOSED on the credential key itself: a key that does not open what
	// this database already holds is refused HERE, not discovered one
	// repository at a time by whoever opens one first.
	if err := s.requireCredentialKeyOpens(ctx); err != nil {
		repoPool.Close()
		_ = maint.Close()
		_ = admin.Close()
		return nil, err
	}
	// The writer lease, before the boot check and so before anything is
	// written: one process writes a repository, and the second refuses to
	// open it rather than append to a directory of its own
	// (writerlease.go, decision 0083). A read-only process takes none — it
	// exists precisely to run beside the writer.
	if !s.readOnly && !o.skipWriterLease {
		s.lease = newWriterLease(maint, s.log)
		if err := s.leaseEveryRepository(ctx); err != nil {
			s.lease.close()
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
	}
	// Every repository's directory against its rows, before anything is
	// served (repodir.go): a crash left the file a transaction behind, a
	// restore left a directory with no row, or a wiped data root left a row
	// with no directory. A repository the two sides disagree on is set
	// aside (setaside.go). A read-only process skips it: the check writes,
	// and the server that owns the directories runs it at its own boot.
	if !s.readOnly {
		// Every repository a release before ids left keyed by its name takes
		// its id first, rows and directory (repositoryid.go), so the check
		// below finds each directory under the key its row names.
		if err := s.keyRepositories(ctx); err != nil {
			s.lease.close()
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
		if err := s.reconcileRepositories(ctx); err != nil {
			s.lease.close()
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
		// Every repository a release before identities left with its sign-in
		// as a record of its own gets an identity, from that record, before
		// anyone signs in (identityboot.go).
		if err := s.giveRepositoriesIdentities(ctx); err != nil {
			s.lease.close()
			repoPool.Close()
			_ = maint.Close()
			_ = admin.Close()
			return nil, err
		}
		// Every row the keying moved and every directory it imported is
		// published under its id now, so the names it held go.
		s.releaseKeyingLeases()
	}
	// A start that set aside every repository it found has nothing to serve
	// and stops as the check did before (setaside.go).
	if err := s.finishSetAside(ctx); err != nil {
		s.lease.close()
		repoPool.Close()
		_ = maint.Close()
		_ = admin.Close()
		return nil, err
	}
	return s, nil
}

// requireCredentialKeyOpens holds a configured credential key against the
// DEK wraps the store already carries. A host that starts with the WRONG key
// otherwise listens, answers /healthz, and fails every repository open after
// the fact, the shape a lost keys volume produces, where a fresh key is
// minted over a database full of repositories nothing can now unwrap. A wrap
// that names this very key and does not open is one repository's damage, and
// that repository is set aside (setaside.go).
//
// A KEYLESS service skips the check: a plain-marked wrap opens without a key,
// and a sealed one refuses at the repository open, which is what leaves the
// read-only operator commands (`repository list`, `inspect`, `verify`) usable
// against a database whose key the operator running them does not hold.
func (s *service) requireCredentialKeyOpens(ctx context.Context) error {
	if len(s.credKey) == 0 {
		return nil
	}
	type wrap struct {
		repo    Repository
		wrapped []byte
		keyID   string
	}
	rows, err := s.maint.QueryContext(ctx,
		`SELECT id, authority, dek, dek_key_id FROM repositories WHERE dek IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var wraps []wrap
	for rows.Next() {
		var w wrap
		var keyID sql.NullString
		if err := rows.Scan(&w.repo.ID, &w.repo.Authority, &w.wrapped, &keyID); err != nil {
			return err
		}
		w.keyID = keyID.String
		wraps = append(wraps, w)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Whether the host holds the right key is the server's question, and it
	// is asked of every wrap that names another key (or none) before any
	// repository is looked at alone: one that does not open says this
	// database was written under another key, and the start stops.
	var failures []string
	for _, w := range wraps {
		if w.keyID == s.credKeyID {
			continue
		}
		if _, err := s.unwrapDEK(w.wrapped, w.repo.ID, w.keyID); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("substrate/engine: SUBSTRATE_CREDENTIAL_KEY (id %s) does not open the DEK of %d of this database's %d repositories: %s. Every repository DEK wraps under this one key, so this database was written under a different one. Restore the key the wraps name, or point this host at the database that belongs to this key. Do NOT let a fresh key start over an existing database: no verb re-wraps a live repository's DEK under another host key, and every repository would be unopenable",
			s.credKeyID, len(failures), len(wraps), strings.Join(failures, "; "))
	}
	// A wrap that names THIS key and still does not open is damage, not a
	// foreign database, and the advice for the two is opposite: the first
	// must keep its key, the second must find another. Damage is one
	// repository's, so each such wrap is opened through the start's one gate.
	for _, w := range wraps {
		if w.keyID != s.credKeyID {
			continue
		}
		if err := s.through(ctx, rowPass(w.repo, setAsideAtKey), func() error {
			if _, err := s.unwrapDEK(w.wrapped, w.repo.ID, w.keyID); err != nil {
				return fmt.Errorf("substrate/engine: SUBSTRATE_CREDENTIAL_KEY (id %s) does not open the DEK of repository %s: %w. A wrap that names this very key and does not open is damaged, not foreign: the key is right, so keep it, and restore that repository's row in repositories from a database dump",
					s.credKeyID, w.repo.label(), err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
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
		runner.Shared.Reconcile(context.Background(), ds.info.ID, nil)
		ds.close()
	}
	// Emptied, so a second Close does not close one dataset twice: a test
	// that closes early and a cleanup that closes again are one process
	// releasing its leases once.
	s.datasets = map[string]*dataset{}
	// The leases before the pool that carries them: the release is a
	// statement on the pinned connection, and a closed pool cannot run one.
	s.lease.close()
	for _, unregister := range s.unregisterMetrics {
		unregister()
	}
	// After every dataset: a dataset's handle hands its connections back
	// here as it closes.
	if s.repoPool != nil {
		closeRepositoryPool(s.repoPool, repositoryPoolCloseWait, s.log)
	}
	err := s.maint.Close()
	if cerr := s.admin.Close(); err == nil {
		err = cerr
	}
	return err
}

// Repositories lists every repository but the ones the start set aside: the
// ledger is one table, and the background loops enumerate through it, so a
// set-aside repository is one no loop reaches (setaside.go).
func (s *service) Repositories(ctx context.Context) ([]substrate.RepositoryInfo, error) {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]substrate.RepositoryInfo, 0, len(repos))
	for _, r := range repos {
		if s.setAsideOf(r.ID, r.Authority) != nil {
			continue
		}
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
	if err := s.refuseSetAside(repo.ID, repo.Authority); err != nil {
		return nil, err
	}
	if !vocabulary.ValidRepositoryID(repo.ID) {
		return nil, fmt.Errorf("%w: repository %s", ErrRepositoryNotKeyed, repo.Authority)
	}
	// A repository whose deletion began is the deletion's (rule 7): a
	// server finishes it at its next start.
	if began, err := s.deletionBegan(ctx, repo.ID); err != nil {
		return nil, err
	} else if began {
		return nil, ErrRepositoryBeingDeleted
	}
	cached, release, err := s.claimOpen(ctx, repo.ID)
	if cached != nil || err != nil {
		return cached, err
	}
	// The row is read again under the slot: this open may have waited on a
	// creation that failed and erased the repository it had begun, and that
	// answer is not found, not the storage error the stale row would draw.
	fresh, err := s.repositoryByID(ctx, repo.ID)
	if err != nil {
		release()
		return nil, err
	}
	if s.testOpenRead != nil {
		s.testOpenRead(repo.ID)
	}
	ds, err := s.openNew(ctx, fresh)
	release()
	// The operator's install list, once per open of the repository in this
	// process, after the open is published: an install is an ordinary write
	// through the dataset, and other requests may use the repository while
	// it runs.
	if err == nil {
		s.runOperatorInstalls(ctx, ds)
	}
	return ds, err
}

// claimOpen is open's singleflight: it waits out whoever holds repository
// id's slot, then returns the cached dataset, or, when none is cached, takes
// the slot and returns its release. A creation takes the same slot
// (createSeededRepository), so an open that finds the new control-plane row
// waits for the directory write behind it instead of meeting that write's
// changelog writer lock. The check of the cache and the claim are one
// critical section: an open that missed the cache and claimed after another
// open published would open a second writer over the first one's directory.
func (s *service) claimOpen(ctx context.Context, id string) (*dataset, func(), error) {
	for {
		s.mu.Lock()
		if ds, ok := s.datasets[id]; ok {
			s.mu.Unlock()
			return ds, nil, nil
		}
		if inflight, ok := s.opening[id]; ok {
			s.mu.Unlock()
			if s.testOpenWaits != nil {
				s.testOpenWaits(id)
			}
			// Wait for whoever got there first, then look again: their dataset
			// is cached, or their failure is ours to retry.
			select {
			case <-inflight:
				continue
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
		inflight := make(chan struct{})
		s.opening[id] = inflight
		s.mu.Unlock()
		return nil, func() {
			s.mu.Lock()
			delete(s.opening, id)
			s.mu.Unlock()
			close(inflight)
		}, nil
	}
}

// claimErase is claimOpen for an erase: it closes and drops a cached dataset
// and takes the slot, so no open can start while the repository's rows go.
// It loops because an open may publish between the drop and the claim.
func (s *service) claimErase(ctx context.Context, id string) (func(), error) {
	for {
		cached, release, err := s.claimOpen(ctx, id)
		if err != nil {
			return nil, err
		}
		if cached == nil {
			return release, nil
		}
		s.mu.Lock()
		if s.datasets[id] == cached {
			delete(s.datasets, id)
		}
		s.mu.Unlock()
		cached.close()
	}
}

// openNew opens a repository's scoped pool and runs the open-time ladder. It
// is called under open's per-repository singleflight, never directly.
func (s *service) openNew(ctx context.Context, repo Repository) (*dataset, error) {
	sc := repo.scope()
	db, err := s.scopedDB(sc)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.label(), err)
	}
	// The repository's DEK for the dataset's lifetime, unwrapped from the
	// control-plane row.
	keys, err := s.repoKeys(ctx, repo.ID)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: unwrap DEK: %w", repo.label(), err)
	}
	dek := keys.dek
	dir, err := s.repositoryDir(repo.ID)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.label(), err)
	}
	ds := &dataset{
		svc:        s,
		db:         db,
		dek:        dek,
		scope:      sc,
		dir:        dir,
		archive:    newArchiveState(s.archiving),
		generation: repo.HistoryGeneration,
		// A dataset's registry starts EMPTY and is built from the repository's
		// OWN rows: the embedded tree seeded them once, at
		// creation, and has no standing here afterwards. Nothing re-projects
		// or prunes the tree at open — the only shipped write left is the
		// version-keyed upgrade below, which APPENDS.
		reg:   vocabulary.NewRegistry(),
		watch: newBroadcaster(),
		// The row's name is not kept: a dataset holds no name, and asks the
		// names cache by id when it needs one (namescache.go).
		info: substrate.RepositoryInfo{ID: repo.ID, State: repo.info().State},
	}
	ds.reg.SetNames(ds.answersTo)
	// The lease before the directory: the open ladder appends (the dialect
	// stamps, the shipped-vocabulary upgrade), so a process that is not this
	// repository's writer must be refused before the first of them and not
	// discover it at the append. The boot check took the lease on every
	// repository that had a row then; this covers the ones registered since.
	if err := s.lease.acquire(ctx, repo.ID); err != nil {
		ds.close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.label(), err)
	}
	// The history before every step that writes: a fold an import has not
	// finished is not served, and the ladder's appends must not land on it
	// (repodir.go).
	if err := ds.openHistory(ctx); err != nil {
		ds.close()
		return nil, fmt.Errorf("substrate/engine: open repository %s: %w", repo.label(), err)
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
	// The repository migrations first (repomigrate.go): code this binary runs
	// once per repository over the stored rows, before anything reads them
	// back as vocabulary, because what one rewrites may be the very rows the
	// next step cannot load as stored. Then the whole vocabulary rebuilds FROM
	// the rows, and only then does the shipped-vocabulary upgrade append what
	// a newer binary added (seed.go).
	for _, step := range []func(context.Context) error{
		ds.loadPeople,
		ds.runLandedRepositoryMigrations(repo.Authority),
		ds.loadStoredVocabulary,
		// The unit uses' rule before the seed: where the stored uses are
		// behind it, the request it writes makes the use guard refuse a
		// seed refresh that could take a unit from a value, rather than
		// judge it by uses an older rule derived (unitgraph.go).
		ds.checkUnitUses,
		// The units before the shipped upgrade, whose guards judge stored
		// quantities through the unit graph, and every later step that
		// re-derives or validates through it (unitseed.go). A repository
		// whose stored vocabulary predates the unit kinds is seeded right
		// after the upgrade that declares them.
		ds.ensureShippedUnits,
		ds.upgradeShippedVocabulary,
		// After the upgrade, which declares the alert kind its clearing
		// raises; a no-op unless an import left secrets behind.
		ds.clearSecretsLeftBehind,
		ds.checkSearchIndex,
		ds.checkObservedIndex,
		ds.loadObservedKinds,
		ds.checkIndexReprojections,
		ds.ensureDefaultProviders,
		// The shipped records after the upgrade, which declares the kinds a
		// newer binary's records are of (recordseed.go).
		ds.ensureShippedRecords,
		// The roles before the people, whose actors name them (roles.go).
		ds.ensureShippedRoles,
		// After the upgrade, which declares the identity a person's actor
		// names and the person a token acts for (people.go).
		ds.settlePeople,
		// After the people pass, which writes the person's own (actorfill.go).
		ds.fillActorRecords,
		// The repository's own record and its first person's actor after a
		// rename or an import under another name, once the people pass wrote
		// the person's actor (repositorynames.go).
		ds.reconcileRepositoryName,
		// The titles a template rendered from a name the local namespace's
		// conversion moved, once the stored vocabulary is loaded and the
		// name is final (localtitles.go).
		ds.renderLocalTitles,
		ds.ensureTriggerCursors,
		ds.reportOpenShippedWebhooks,
		ds.settleInterruptedSyncs,
		ds.settleInterruptedAgentRuns,
		ds.clearDeadReservations,
	} {
		if err := step(ctx); err != nil {
			ds.close()
			return nil, err
		}
	}
	// Bodies prepare at open exactly as they do at registration: each python
	// source registers into a process of its own.
	ds.warmFunctions()
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.datasets[repo.ID]; ok {
		ds.close()
		return prev, nil
	}
	// After the open, never inside it: the reindex and the reprojection take
	// minutes on a large repository, and every request on it waits for the
	// open (issue 719).
	ds.startSearchReindex()
	ds.startIndexReprojection()
	ds.startArchiver()
	s.datasets[repo.ID] = ds
	// Published by authority; ds.close drops it, so a repository closed and
	// reopened publishes its live pool and not the one it let go.
	unregisterDB := metrics.RegisterDBStats("repository:"+repo.ID, db)
	unregisterSpool := metrics.RegisterWebhookSpool(repo.ID, ds.spoolStats)
	unregisterInventory := metrics.RegisterInventory(repo.ID, ds.inventory)
	unregisterArchive := func() {}
	if ds.archive != nil {
		unregisterArchive = metrics.RegisterArchive(repo.ID, ds.archive.stats)
	}
	ds.unregisterMetrics = func() { unregisterDB(); unregisterSpool(); unregisterInventory(); unregisterArchive() }
	return ds, nil
}

// Dataset opens a repository's dataset by its id, or by its name.
func (s *service) Dataset(ctx context.Context, repository string) (substrate.Dataset, error) {
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, repo)
}

// CreateRepository creates a repository and its control-plane row: the user IS
// that row, and the repository it owns is born holding the shipped kinds.
// Registration (auth.go) is what calls it — a repository created any other
// way has no credential and therefore no way in.
func (s *service) CreateRepository(ctx context.Context, repository string) (substrate.RepositoryInfo, error) {
	repo, err := s.createSeededRepository(ctx, repository, nil)
	if err != nil {
		return substrate.RepositoryInfo{}, err
	}
	s.openForOperatorInstalls(ctx, repo)
	return repo.info(), nil
}

// createSeededRepository is THE creation act. The repository takes the
// authority the registration chose as its name, and a minted id as its key
// (decision 0261): the control-plane row's primary key, every scope, the DEK
// wrap's binding, the directory under the data root and the fs blob path.
//
// It is
// the seed of the shipped vocabulary, the repository's own description of
// itself, and whatever the caller adds — registration passes the recovery key
// and the first token — as ONE transaction in
// the new repository's changelog, followed by the `repositories` row that makes the
// user exist, and in the same control-plane transaction the person's identity
// and the row that lets it open the repository.
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
	return s.createSeededRepositoryFor(ctx, authority, nil, extra)
}

// createSeededRepositoryFor is createSeededRepository with the person it is
// created for: their identity, not yet written, is inserted with the
// repository's control-plane row and allowed to open it, in the one
// transaction that is the creation's commit point, so the person and the
// repository exist together or not at all (decision 0274).
func (s *service) createSeededRepositoryFor(ctx context.Context, authority string, person *identityRow, extra func(*txn) error) (Repository, error) {
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
	if _, err := s.repositoryByNameOn(ctx, cp, authority); err == nil {
		return zero, errAuthorityTaken(authority)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return zero, err
	}
	// A directory the start set aside before it had a row still holds its
	// name: the repair imports it under that name at the next start.
	if s.aside.holdsName(authority) {
		return zero, errAuthorityTaken(authority)
	}
	id, err := vocabulary.NewRepositoryID()
	if err != nil {
		return zero, err
	}
	// OPEN'S SLOT BEFORE THE ROW, held until the directory is written or the
	// failure erased (claimOpen). The row publishes the repository inside
	// this process as well: a background loop lists it and opens the
	// repository while reconcileRow below still appends the seed under a
	// writer of its own. Without the slot that open fails on the writer's
	// flock, or opens first, and then this creation's reconcile fails on
	// the open's flock and erases a repository the open already serves.
	// The slot is taken before the lease, so a failure hands the lease back
	// before a waiting open runs.
	cached, releaseOpen, err := s.claimOpen(ctx, id)
	if err != nil {
		return zero, err
	}
	if cached != nil {
		return zero, errAuthorityTaken(authority)
	}
	defer releaseOpen()
	// THE WRITER LEASE BEFORE THE SEED, and so before the control-plane row.
	// That row is the creation's commit point and the directory is written
	// after it, so a lease taken only at the directory step leaves a window
	// in which ANOTHER process sees the row, takes the lease and opens the
	// repository — and this creation's failure would then erase the
	// repository that process is already serving. Taken here, an
	// in-progress registration cannot be claimed by anybody, and
	// reconcileRow's own ask below finds it held by this process.
	leased, err := s.lease.acquireNew(ctx, id)
	// A creation that fails releases only the lease IT took: this process is
	// not the writer of a repository that does not exist. acquireNew reports
	// false for one this process already held, which is never this
	// creation's to hand back. Registered BEFORE the error is read, because
	// acquireNew can report both — a lease taken on a connection that
	// replaced a dead one, whose predecessor's leases are not back yet.
	defer func() {
		if leased {
			s.lease.release(id)
		}
	}()
	if err != nil {
		return zero, err
	}
	repo := Repository{ID: id, Authority: authority}
	// A creation starts the graph revision at 0 again, so whatever this
	// process cached under the key (a creation that failed) is not this
	// repository's (unitgraph.go).
	s.unitGraphs.forget(id)
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

	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return zero, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return zero, fmt.Errorf("substrate/engine: create repository %s: %w", authority, err)
	}
	// The creation dataset carries the BINARY's registry — the seed has to
	// resolve the kinds it is writing, and the repository has no rows yet.
	// After the seed commits, the dataset is thrown away and the repository is
	// opened the ordinary way: from its own rows.
	seedDS := &dataset{
		svc: s, db: db, dek: dek, scope: repo.scope(),
		reg: repo.registry(s.base.Clone()), watch: newBroadcaster(),
		info: substrate.RepositoryInfo{ID: repo.ID, State: repo.info().State}, creating: authority,
	}
	// inserted flips once the control-plane row is this creation's, and
	// decides what a failure may erase (eraseFailedCreation).
	inserted := false
	fail := func(stage string, cause error) (Repository, error) {
		// The person was inserted with the row, and is this creation's alone.
		var made string
		if inserted && person != nil {
			made = person.ID
		}
		if cerr := s.eraseFailedCreation(ctx, cp, repo.ID, inserted, made); cerr != nil {
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
		// Stamped with the closure it describes, because the manifest the
		// creation writes next is read from this table, and a repository that
		// is never opened must still say which dialect its rows need.
		if _, err := t.tx.ExecContext(t.ctx, vocabularyDialectStamp, maxVocabularyDialect); err != nil {
			return fmt.Errorf("substrate/engine: stamp vocabulary dialect %d: %w", maxVocabularyDialect, err)
		}
		// The shipped units, onto the kinds the seed just declared. A fresh
		// repository holds nothing they could contradict.
		if _, _, err := t.seedUnits(); err != nil {
			return err
		}
		// The records the seeded packages ship, onto the kinds the seed just
		// declared (recordseed.go).
		if err := t.seedShippedRecords(); err != nil {
			return err
		}
		// Every row a creation writes is indexed under this binary's rules,
		// so the first open has nothing to re-derive (searchindex.go,
		// unitgraph.go).
		if err := t.markSearchIndexed(); err != nil {
			return err
		}
		if err := t.markUnitUsesDerived(); err != nil {
			return err
		}
		// The repository's own read-only description of itself. `lifecycle` is
		// the state a creation is born into, so naming it is assertion, not
		// transition.
		// The repository's person and the system are actors from its
		// creation (decision 0270); every other hand's record is written when
		// it first writes.
		if err := t.ensureActors(substrate.ActorSystem, substrate.ActorPerson); err != nil {
			return err
		}
		// The roles, and the creator as the first admin (decision 0275).
		if err := t.ensureShippedRoles(); err != nil {
			return err
		}
		// The person the repository is created for is its first person, and
		// that actor names their identity (people.go).
		if person != nil {
			if err := t.namePersonActor(substrate.ActorPerson, person.ID, authority); err != nil {
				return err
			}
		}
		if err := t.givePersonRoles(substrate.ActorPerson, scope.RoleAdmin); err != nil {
			return err
		}
		if err := t.asActor(substrate.ActorSystem, func() error {
			if err := t.seedDefaultProviders(); err != nil {
				return err
			}
			// Born with its continuations in this binary's order: migration
			// 7 finds the mark at the first open and writes nothing. The put
			// below is the entry that carries it.
			if _, err := t.putAnnotation(eref{Kind: kindRepository, ID: repo.ID}, annContinuationOrder, substrate.IdentityOrder); err != nil {
				return err
			}
			// Born holding the record of every hand it links, which each
			// write ensures: the actor fill finds the mark and reads nothing.
			if _, err := t.putAnnotation(eref{Kind: kindRepository, ID: repo.ID}, annActorRecordsFilled, 1); err != nil {
				return err
			}
			// Born under `~`: no title it renders spells its name.
			if _, err := t.putAnnotation(eref{Kind: kindRepository, ID: repo.ID}, annTitlesRenderedLocal, 1); err != nil {
				return err
			}
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

	if _, err := s.insertRepositoryRow(ctx, cp, &repo, person); err != nil {
		return fail("control-plane row", err)
	}
	inserted = true
	if s.testFailAfterRow != nil {
		if err := s.testFailAfterRow(); err != nil {
			return fail("after the control-plane row", err)
		}
	}
	// The repository exists, so the lease is this process's to keep.
	leased = false
	return repo, nil
}

// validRepositoryName is the grammar a repository's name is held to at BOTH
// doors, registration (validRepositoryAuthority) and the boot import of a
// directory (repodir.go): the authority grammar every kind carries
// (vocabulary.ValidRepositoryAuthority), capped at MaxIDLen. The name is the
// id of the repository's own `core/authority` record, and a record id is at
// most MaxIDLen bytes where DNS would admit 253; a directory the import
// accepted past the cap would be a repository whose authority record no write
// could address.
func validRepositoryName(authority string) error {
	switch {
	case authority == "":
		return fmt.Errorf("%w: a repository needs an authority: a hostname you control, such as ada.example.com", substrate.ErrValidation)
	case !vocabulary.ValidRepositoryAuthority(authority):
		return fmt.Errorf("%w: authority %q must be a lowercase DNS-style name with at least two labels (ada.example.com)", substrate.ErrValidation, authority)
	case len(authority) > vocabulary.MaxIDLen:
		return fmt.Errorf("%w: authority %q is longer than %d bytes, the most a repository's name may be", substrate.ErrValidation, authority, vocabulary.MaxIDLen)
	}
	return nil
}

// validRepositoryAuthority is the door a repository's own authority passes
// through once, at creation: validRepositoryName, and one namespace refused on
// top. `substrate.reamde.dev` and everything under it is where the shipped
// vocabulary publishes, so a repository claiming a name there would be a
// user-owned authority that reads as shipped.
func validRepositoryAuthority(authority string) error {
	if err := validRepositoryName(authority); err != nil {
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

// newID mints a record id: a UUIDv7 written as 26 lowercase Crockford base32
// characters, the suffix form of a TypeID. The leading 48 bits are the
// creation time in milliseconds, so ids sort by when they were minted, and the
// 128 bits decode back to the UUID. A repository is not one of the minted
// records; its id is its authority.
func newID() (string, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return vocabulary.EncodeMintedID(u), nil
}

// derivedMintedID is vocabulary.DerivedMintedID: an id in the minted shape
// that a retry must name again (a gated create's target).
func derivedMintedID(parts ...string) string { return vocabulary.DerivedMintedID(parts...) }

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
