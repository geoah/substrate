package engine

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// TestCredentialKey is a conforming credential key: standard-base64 of 32
// random bytes, the shape Open now demands (ADR 0024). It is minted once per
// test binary, so every Open in the suite shares one key and a reopen of the
// same database matches. Generated at run time and never committed, because a
// key checked into the tree is a key everyone has.
var TestCredentialKey = mintTestCredentialKey()

// TestCredentialKeyBytes is the 32-byte AES-256 key TestCredentialKey decodes
// to, for tests that unwrap a DEK the way the engine does.
var TestCredentialKeyBytes = mustDecodeTestCredentialKey(TestCredentialKey)

func mintTestCredentialKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func mustDecodeTestCredentialKey(key string) []byte {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		panic(err)
	}
	return raw
}

// DataRootOf is the data root a service was opened with, so a test can find
// a repository's directory (changelogfile.RepoDir) and damage or copy it.
func DataRootOf(svc substrate.Service) string { return svc.(*service).dataRoot }

// SetRepositoryConnsPerHandle caps what each repository opened from here on
// may take of the shared pool. At 1, a path that holds a connection and asks
// its repository for a second waits forever, which is what a saturated
// shared pool does in production.
func SetRepositoryConnsPerHandle(svc substrate.Service, n int) { svc.(*service).repoConnsPerHandle = n }

// SetTestMigrationHook runs fn inside every repository migration's
// transaction, with the migration's own connection held, for the
// repositories opened from here on.
func SetTestMigrationHook(svc substrate.Service, fn func()) { svc.(*service).testMigrationHook = fn }

// SharedScopedDB is a raw handle on the service's shared repository pool,
// pinned to one repository by the same connector every dataset rides.
func SharedScopedDB(svc substrate.Service, repository string) (*sql.DB, error) {
	return svc.(*service).scopedDB(Scope{Repository: repository})
}

// IdleRepositoryPins acquires every idle connection of the shared pool
// straight from it, bypassing the connector, and reports each one's
// repository setting. A released connection is unpinned before it is idle,
// so every value is empty.
func IdleRepositoryPins(ctx context.Context, svc substrate.Service) ([]string, error) {
	pool := svc.(*service).repoPool
	n := int(pool.Stat().IdleConns())
	var pins []string
	var held []*pgxpool.Conn
	defer func() {
		for _, c := range held {
			c.Release()
		}
	}()
	for range n {
		c, err := pool.Acquire(ctx)
		if err != nil {
			return pins, err
		}
		held = append(held, c)
		var pin string
		if err := c.QueryRow(ctx, `SELECT COALESCE(current_setting($1, true), '')`, repositorySetting).Scan(&pin); err != nil {
			return pins, err
		}
		pins = append(pins, pin)
	}
	return pins, nil
}

// migratedTemplate is the database MigratedDSN copies for each test
// (testdb.Template): Open ran on it once, with no repository, so the copy
// holds the recorded migrations, the roles' grants and the shipped indexes
// and nothing else. A copy beside an empty data root is exactly a fresh
// install (nothing on either side), and Open on the copy runs every boot
// step over it; what it skips is the DDL. The from-empty migration still
// runs here, in TestRepositoryProvisioningAndProjections and in
// TestAssertPoolPrincipalRejectsSuperuser, which open testdb.NewSchema, and
// once per embedded migration in TestEveryMigrationLeavesAFoldTheRebuildReproduces,
// which stages a schema at each one.
var migratedTemplate = testdb.NewTemplate("engine", func(ctx context.Context, dsn string) error {
	root, err := os.MkdirTemp("", "substrate-template-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	svc, err := Open(ctx, dsn,
		WithKindsDir(SeedKindsDir),
		WithDataRoot(root),
		WithCredentialKey(TestCredentialKey))
	if err != nil {
		return err
	}
	return svc.Close()
})

// MigratedDSN is a fresh database of the test's own on which the shipped
// migrations have already run, dropped when the test ends. It is what every
// test opens, migrate_db_test.go's included (those tamper with a migrated
// database and open it again). The ones that open testdb.NewSchema and
// migrate from empty are TestRepositoryProvisioningAndProjections,
// TestAssertPoolPrincipalRejectsSuperuser and
// TestEveryMigrationLeavesAFoldTheRebuildReproduces.
func MigratedDSN(t *testing.T) string {
	t.Helper()
	return migratedTemplate.Clone(t)
}

// MigrationSQL is the text of one embedded migration file, so a test can run
// it again against a schema it has arranged.
func MigrationSQL(name string) (string, error) {
	body, err := migrationsFS.ReadFile("migrations/" + name)
	return string(body), err
}

// Migration is one SQL migration the binary embeds, by the version and name
// schema_migrations records it under.
type Migration struct {
	Version int
	Name    string
}

// Migrations lists the SQL migrations the binary embeds, in the order the
// runner applies them.
func Migrations() ([]Migration, error) {
	ms, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	out := make([]Migration, 0, len(ms))
	for _, m := range ms {
		out = append(out, Migration{Version: m.Version, Name: m.Name})
	}
	return out, nil
}

// MigrateThrough runs the boot's migration runner over the schema the DSN
// pins and stops after the migration numbered through. Each migration is
// recorded as the boot records it, so a later Open applies only the rest.
func MigrateThrough(ctx context.Context, dsn string, through int) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return migrateThrough(ctx, db, through)
}

// FoldTables are the tables RebuildRepository clears and fills again: the
// ones the replay writes (foldTables) and property_offers, which the rebuild
// derives from the replayed rows (rederiveOffers).
func FoldTables() []string {
	return append(slices.Clone(foldTables), "property_offers")
}

// FoldSnapshotOver is FoldSnapshot read through db, a connection row level
// security does not bind. With no repository predicate the planner cannot
// take a table's rows in key order from its primary key, so each ORDER BY
// must decide the order on its own.
func FoldSnapshotOver(ctx context.Context, db *sql.DB) ([]byte, error) {
	snap, err := foldSnapshot(ctx, db)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(snap, "", "  ")
}

// WithTestCommitFault runs fn at each durable step of a write's commit
// (dataset.go commitAndMirror), six stages. Around the manifest write that
// precedes the first append in a new changelog dialect (repodir.go
// writeManifestBeforeCommit): CommitBeforeManifest just before the write,
// where an error stands for the write failing, and CommitAfterManifest once
// it is written, where an error is the process dying there; either rolls the
// transaction back. Then CommitAfterPrepare, after the sealed files are staged
// and the changelog lines are prepared and before Postgres commits, and
// CommitAfterCommit, after Postgres committed and before the staged files are
// renamed into place and the file's final newline: an error from fn at either
// ends the write there and runs nothing after it, the shape of a process
// dying at that step, so the directory is left exactly as the crash would
// leave it. CommitInDoubt is not a crash: the hook's error is taken as the
// commit's answer after Postgres committed, the shape of a connection lost at
// the answer, and the write's error path runs. CommitUnpublished is the same
// for a write that publishes a registry, taken before the registry swaps, so
// the process keeps serving the declarations the commit replaced.
func WithTestCommitFault(fn func(stage string) error) Option {
	return func(o *options) { o.commitFault = fn }
}

// WithTestSkipWriterLease opens the service WITHOUT the per-repository writer
// lease, so a test can be a repository's SECOND writer on purpose — the
// condition decision 0083 refuses at open and catchUpBeforePrepare still
// repairs when it arises anyway. Two tests need it: the one that proves the
// cross-writer catch-up, and the ones that prove the same-data-root flock,
// which the lease would otherwise refuse one layer earlier. A server has no
// such option.
func WithTestSkipWriterLease() Option {
	return func(o *options) { o.skipWriterLease = true }
}

// WithTestPerRecordSeed seeds the shipped units one put at a time, the path a
// repository holding some of them takes, so a test can hold the bulk run to
// it.
func WithTestPerRecordSeed() Option {
	return func(o *options) { o.perRecordSeed = true }
}

// WriterLeaseKeySQL is the lease's advisory-lock key expression, for a test
// that takes or probes a lease from a session of its own. It is the expression
// docs/operations.md publishes for reading `pg_locks`.
const WriterLeaseKeySQL = writerLeaseKeySQL

// SearchIndexVersion is the search index rule version this binary writes
// (searchtext.go).
const SearchIndexVersion = searchIndexVersion

// The commit stages WithTestCommitFault reports, in the order they run.
const (
	CommitBeforeManifest = commitBeforeManifest
	CommitAfterManifest  = commitAfterManifest
	CommitAfterPrepare   = commitAfterPrepare
	CommitUnpublished    = commitUnpublished
	CommitInDoubt        = commitInDoubt
	CommitAfterCommit    = commitAfterCommit
)

// WithTestSnapshotFault installs a hook the snapshot runs after each copy
// step with the partial directory it is building (snapshot.go). A test
// damages a copied file there, or returns an error to fail the snapshot
// mid-copy; a nil hook is the production path.
func WithTestSnapshotFault(fn func(stage, dir string) error) Option {
	return func(o *options) { o.snapshotFault = fn }
}

// WithTestProgressEvery sets how often a long walk reports its position
// (progress.go). Zero reports at every tick, so a test with a short history
// sees the lines a two-hour verify prints every thirty seconds.
func WithTestProgressEvery(every time.Duration) Option {
	return func(o *options) { o.progressEvery = every }
}

// WithTestDigestHook runs fn with each finished changelog segment whose
// bytes a check of a repository's directory hashes: the server's digest
// behind the open (segmentdigest.go), a snapshot's read-back of what it
// copied, and an open of segments no earlier check and no sidecar vouched
// for (progress.go checkProgress). It can run on more than one goroutine at
// once.
func WithTestDigestHook(fn func(repository, segment string)) Option {
	return func(o *options) { o.digestHook = fn }
}

// WithTestDigestPaceHook runs fn after each read the background digest
// paces (segmentdigest.go digestPacer), with the repository and the bytes
// read so far, so a test can say how far the digest moved while something
// else ran.
func WithTestDigestPaceHook(fn func(repository string, bytes int64)) Option {
	return func(o *options) { o.digestPaceHook = fn }
}

// WithTestDigestYieldMax sets the longest the background digest waits for
// invocations to return before one read (segmentdigest.go yield), in place
// of digestYieldMax.
func WithTestDigestYieldMax(d time.Duration) Option {
	return func(o *options) { o.digestYieldMax = d }
}

// SegmentDigestDone is closed when the dataset's background digest of its
// changelog segments has returned, finished or stopped; a dataset whose open
// started none returns a closed channel.
func SegmentDigestDone(ds substrate.Dataset) <-chan struct{} {
	d := ds.(*dataset)
	d.digestMu.Lock()
	done := d.digestDone
	d.digestMu.Unlock()
	if done != nil {
		return done
	}
	closed := make(chan struct{})
	close(closed)
	return closed
}

// WithTestOperator opens the service as OpenOperator opens substratectl's:
// no repository's open digests its changelog segments in the background, so
// a test that damages the source a snapshot reads sees the snapshot's own
// checks and not a latch the digest set first.
func WithTestOperator() Option {
	return func(o *options) { o.operator = true }
}

// WithTestSearchReindex sets the open-time reindex's page size (zero keeps
// searchReindexBatch) and a hook it runs before each page with the kind it is
// about to re-derive (searchindex.go reindexKind). A hook that blocks holds
// the reindex there; it should return when ctx ends, which is the dataset
// closing. An error from it fails the page, as a database error would.
func WithTestSearchReindex(batch int, hook func(ctx context.Context, kind string) error) Option {
	return func(o *options) { o.searchReindexBatch, o.searchReindexHook = batch, hook }
}

// WithTestInadmissible has the loader refuse the named stored packages at
// every open and import, as a binary whose contract tightened would, so a
// test parks a package whose stored rows and changelog still agree.
func WithTestInadmissible(packages ...string) Option {
	return func(o *options) {
		o.inadmissible = map[string]bool{}
		for _, p := range packages {
			o.inadmissible[p] = true
		}
	}
}

// WithTestQueryTracer installs tr on the repository pool every dataset of the
// service draws from, so it sees each statement a read or a write sends, the
// background work of every open repository's included. The pool calls tr from
// every connection at once, so it must be safe for concurrent use.
func WithTestQueryTracer(tr pgx.QueryTracer) Option {
	return func(o *options) { o.queryTracer = tr }
}

// EmbedBatchQuery is the drain's batch read (search.go embedBatchQuery), for a
// test that plans it.
const EmbedBatchQuery = embedBatchQuery

// WithTestEmbedRequestCaps caps one embeddings request the drain sends at
// inputs chunks and bytes bytes of text, so a small fixture spans several
// requests. Zero keeps a default.
func WithTestEmbedRequestCaps(inputs, bytes int) Option {
	return func(o *options) { o.embedRequestInputs, o.embedRequestBytes = inputs, bytes }
}

// WithTestReprojectionHook sets a hook the background index re-derivation
// runs before each page with the kind it is about to re-derive
// (reprojection.go reprojectKind). A hook that blocks holds the pass there;
// it should return when ctx ends, which is the dataset closing. An error
// from it fails the page, as a database error would.
func WithTestReprojectionHook(hook func(ctx context.Context, kind string) error) Option {
	return func(o *options) { o.reprojectionHook = hook }
}

// WithTestReprojectionPageHook sets a hook the background index
// re-derivation runs inside each page's transaction, after the page's
// statements and before its commit, with the kind it re-derived
// (reprojection.go reprojectKind). A hook that blocks holds the page's locks
// there; it should return when ctx ends. An error from it fails the page.
func WithTestReprojectionPageHook(hook func(ctx context.Context, kind string) error) Option {
	return func(o *options) { o.reprojectionPageHook = hook }
}

// WithTestWebhookClock is the clock the webhook door records each request's
// receipt by (webhooks.go admitWebhook), and nothing else, so a test that
// measures a stored request's skew from its receipt names that instant to
// the millisecond instead of sleeping to it.
func WithTestWebhookClock(now func() time.Time) Option {
	return func(o *options) { o.webhookNow = now }
}

// IndexReprojectionDone is closed when the dataset's latest background index
// re-derivation has returned, finished or stopped; a dataset that started
// none returns a closed channel.
func IndexReprojectionDone(ds substrate.Dataset) <-chan struct{} {
	return ds.(*dataset).reproject.finished()
}

// DrainIndexReprojection waits until the repository owes no index
// re-derivation: no row is left in `index_reprojections`, which the pass
// deletes only after a kind's last page has committed. It reads the table
// rather than IndexReprojectionDone, because a vocabulary apply writes a
// request after the open and a run that finished before it closed the
// channel already. A request still owed after a minute fails the test: the
// pass was never started for it, or it cannot finish.
func DrainIndexReprojection(t testing.TB, ds substrate.Dataset) {
	t.Helper()
	d := ds.(*dataset)
	deadline := time.Now().Add(time.Minute)
	for {
		pending, err := d.pendingReprojections(context.Background())
		if err != nil {
			t.Fatalf("read the index re-derivations owed: %v", err)
		}
		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			kinds := make([]string, 0, len(pending))
			for _, p := range pending {
				kinds = append(kinds, p.kind)
			}
			t.Fatalf("the index re-derivation still owes %v after a minute", kinds)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// SearchReindexDone is closed when the dataset's latest reindex has returned,
// finished or stopped; a dataset that started none returns a closed channel.
func SearchReindexDone(ds substrate.Dataset) <-chan struct{} {
	return ds.(*dataset).reindex.finished()
}

// SeedKindsDir is the shipped SEED AUTHORITY, relative to this package — core
// and llm together (record 0077): what every test open loads unless it brings
// a patched tree.
const SeedKindsDir = "../../kinds/substrate.reamde.dev"

// testClocks holds one TestClock per test name (ClockOf), so a subtest and a
// repeated run (-count=N) start at zero.
var testClocks sync.Map

// ClockOf is the test's clock, made on first use and forgotten when the test
// ends.
func ClockOf(t *testing.T) *TestClock {
	t.Helper()
	key := t.Name()
	c, loaded := testClocks.LoadOrStore(key, &TestClock{})
	if !loaded {
		t.Cleanup(func() { testClocks.Delete(key) })
	}
	return c.(*TestClock)
}

// OpenForTest is the ONE way a test opens a service: Open with the shipped
// core kinds, the binary's credential key and the test's TOTP clock, then the
// caller's options, which win where they name the same thing (a patched
// kinds tree, another key). It does not close the service: the callers
// differ on when.
func OpenForTest(t *testing.T, ctx context.Context, dsn string, opts ...Option) (substrate.Service, error) {
	t.Helper()
	all := append([]Option{
		WithKindsDir(SeedKindsDir),
		WithCredentialKey(TestCredentialKey),
		WithTestTOTPClock(ClockOf(t).Now),
		WithLLMSampleSeed(false),
		WithLLMProviderSeed(false),
		WithUnitSeed(false),
	}, opts...)
	return Open(ctx, dsn, all...)
}

// The snapshot stages WithTestSnapshotFault reports, in the order they run.
const (
	SnapshotAfterChangelog = snapshotAfterChangelog
	SnapshotAfterCopy      = snapshotAfterCopy
)

// BreakSealedStore replaces a dataset's sealed-file writer with one that
// fails every call, under its mutex, so the next write that touches the
// sealed table is refused the way a full disk would refuse it: before its
// transaction commits, with nothing durable anywhere (ErrDirectoryWrite).
func BreakSealedStore(ds substrate.Dataset) {
	d := ds.(*dataset)
	d.writerMu.Lock()
	defer d.writerMu.Unlock()
	d.sealed = brokenSealedStore{}
}

// RepairSealedStore puts a dataset's sealed-file writer back to the files
// themselves, undoing BreakSealedStore.
func RepairSealedStore(ds substrate.Dataset) {
	d := ds.(*dataset)
	d.writerMu.Lock()
	defer d.writerMu.Unlock()
	d.sealed = nil
}

// BreakSealedStoreTimes replaces a dataset's sealed-file writer with one
// whose Stage fails the next n calls and then writes the files as usual.
func BreakSealedStoreTimes(ds substrate.Dataset, n int) {
	d := ds.(*dataset)
	d.writerMu.Lock()
	defer d.writerMu.Unlock()
	d.sealed = &failingSealedStore{left: n}
}

type failingSealedStore struct {
	fileSealedStore
	mu   sync.Mutex
	left int
}

func (s *failingSealedStore) Stage(repoDir string, rec changelogfile.SealedRecord) error {
	s.mu.Lock()
	fail := s.left > 0
	if fail {
		s.left--
	}
	s.mu.Unlock()
	if fail {
		return ErrSealedStoreBroken
	}
	return s.fileSealedStore.Stage(repoDir, rec)
}

// ErrSealedStoreBroken is what BreakSealedStore's store fails with.
var ErrSealedStoreBroken = errors.New("engine test: the sealed store is broken")

type brokenSealedStore struct{}

func (brokenSealedStore) Write(string, changelogfile.SealedRecord) error { return ErrSealedStoreBroken }

func (brokenSealedStore) Stage(string, changelogfile.SealedRecord) error { return ErrSealedStoreBroken }

func (brokenSealedStore) Commit(string, string) error { return ErrSealedStoreBroken }

func (brokenSealedStore) Delete(string, string) error { return ErrSealedStoreBroken }

// BreakSealedStoreAfterStage replaces a dataset's sealed-file writer with one
// whose Stage writes the pending file and then fails, the shape of a rename
// that landed and a directory fsync that did not: the write must be refused
// and the pending file must not be left behind.
func BreakSealedStoreAfterStage(ds substrate.Dataset) {
	d := ds.(*dataset)
	d.writerMu.Lock()
	defer d.writerMu.Unlock()
	d.sealed = stagedThenFails{}
}

type stagedThenFails struct{ fileSealedStore }

func (s stagedThenFails) Stage(repoDir string, rec changelogfile.SealedRecord) error {
	if err := s.fileSealedStore.Stage(repoDir, rec); err != nil {
		return err
	}
	return ErrSealedStoreBroken
}

// ImportIncomplete reports whether the repository's import-progress marker
// is set, read through the tamperer's seat.
func ImportIncomplete(ctx context.Context, db dbx) (bool, error) {
	_, incomplete, err := importIncomplete(ctx, db)
	return incomplete, err
}

// WithCatchUpBatch sets how many changelog rows one page of the boot's
// table-to-file catch-up reads (appendFromTable), so a test can put a
// transaction across a page boundary without writing five hundred entries.
func WithCatchUpBatch(n int) Option { return func(o *options) { o.catchUpBatch = n } }

// WithValuesBudget sets how many earlier entries one change read's before
// values may read (changevalues.go), so a test can outrun it, merge and split
// history included, with a few dozen writes.
func WithValuesBudget(n int) Option { return func(o *options) { o.valuesBudget = n } }

// WithSearchMatchMax sets the match count past which a search matches on the
// row instead of through records_matching (bm25.go indexedMatch), so a test
// can read a small fixture down either path: 0 puts every matching query on
// the row.
func WithSearchMatchMax(n int) Option {
	return func(o *options) { o.searchMatchMax, o.searchMatchMaxSet = n, true }
}

// WithTestSemanticHook runs fn with the semantic arm's context and
// transaction after its scan settings and before the candidate read (search.go
// semantic). A hook that runs a slow statement on tx spends the arm's budget
// inside the database, as a slow index scan would; one that cancels the
// caller's context stands for the request going away mid-arm.
func WithTestSemanticHook(fn func(ctx context.Context, tx *sql.Tx)) Option {
	return func(o *options) { o.searchSemanticHook = fn }
}

// explainLiteral renders one of the candidate read's arguments (the query vector,
// a text, the kinds array) as a quoted SQL literal.
func explainLiteral(v any) (string, error) {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	switch x := v.(type) {
	case string:
		return quote(x), nil
	case []string:
		elems := make([]string, len(x))
		for i, e := range x {
			elems[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(e) + `"`
		}
		return quote("{" + strings.Join(elems, ",") + "}"), nil
	case pgvector.Vector:
		parts := make([]string, len(x.Slice()))
		for i, f := range x.Slice() {
			parts[i] = strconv.FormatFloat(float64(f), 'g', -1, 32)
		}
		return quote("[" + strings.Join(parts, ",") + "]"), nil
	}
	return "", fmt.Errorf("no literal for %T", v)
}

// SemanticCandidatePlan is the EXPLAIN of the semantic arm's candidate read
// (search.go semanticCandidateQuery) for an arm of k records, planned on the
// repository's own pool under the arm's own scan settings
// (semanticScanSettings). priceOutSorts also turns sorts off: on a near-empty
// table a primary-key scan and a sort is the cheaper plan, and that plan says
// nothing about the index, so pricing it out shows whether the HNSW index can
// serve the read at all. generic plans the read as a prepared statement's
// generic plan, the one a connection's statement cache can settle on after a
// few runs. kinds, when not empty, restricts the read as a search's
// filter.kinds does.
func SemanticCandidatePlan(ctx context.Context, d substrate.Dataset, kinds []string, k int, priceOutSorts, generic bool) (string, error) {
	ds := d.(*dataset)
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	n := semanticCandidates(k)
	if err := semanticScanSettings(ctx, tx, n); err != nil {
		return "", err
	}
	if priceOutSorts {
		if _, err := tx.ExecContext(ctx, `SET LOCAL enable_sort = off`); err != nil {
			return "", err
		}
	}
	q := make([]float32, vectorDim)
	q[0] = 1
	b := &builder{}
	vec := b.arg(pgvector.NewVector(q))
	query := semanticCandidateQuery(b, vec, &embedProvider{id: "vectors", model: "text-embedding-3-small"}, kinds, len(kinds) > 0, n)
	explain, args := `EXPLAIN `+query, b.args
	if generic {
		// A prepared statement outlives the transaction, so it is named for
		// this call and deallocated before the rollback.
		name := fmt.Sprintf("semantic_candidates_%d", time.Now().UnixNano())
		for _, stmt := range []string{`SET LOCAL plan_cache_mode = force_generic_plan`, `PREPARE ` + name + ` AS ` + query} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return "", err
			}
		}
		defer func() { _, _ = tx.ExecContext(ctx, `DEALLOCATE `+name) }()
		// EXPLAIN EXECUTE takes its arguments as literals, not binds.
		lits := make([]string, len(args))
		for i, a := range args {
			lit, err := explainLiteral(a)
			if err != nil {
				return "", err
			}
			lits[i] = lit
		}
		explain, args = `EXPLAIN EXECUTE `+name+`(`+strings.Join(lits, ", ")+`)`, nil
	}
	rows, err := tx.QueryContext(ctx, explain, args...)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		plan = append(plan, line)
	}
	return strings.Join(plan, "\n"), rows.Err()
}

// WithSearchReadMax sets the most matching rows one lexical candidate pass
// reads before ranking (bm25.go lexicalCandidates), so a test can cut a pass
// on a fixture of a few rows.
func WithSearchReadMax(n int) Option { return func(o *options) { o.searchReadMax = n } }

// AdvisoryKeySQL is the engine's advisory-lock key expression (identity.go),
// for a test that takes one of the engine's locks by hand: a barrier test that
// composed the key itself would park on a lock nothing else takes.
const AdvisoryKeySQL = advisoryKeySQL

// ChangelogLockKey is the $1 of AdvisoryKeySQL for a dataset's changelog lock
// (rows.go changelogLockKey), the first lock every write takes, for a barrier
// test that holds it from a session of its own.
func ChangelogLockKey(ds substrate.Dataset) string {
	return ds.(*dataset).scope.lockKey(changelogLockKey)
}

// CommitBudget is how long a write's commit phase may take before its
// transaction is rolled back (dataset.go commitBudget), for a test that holds
// a write inside that phase.
const CommitBudget = commitBudget

// BreakChangelogWriter closes a dataset's changelog writer under its mutex, so
// the next write's prepare fails the way a full disk would: the write is
// refused before its transaction commits, neither store takes it, and the
// dataset latches ErrChangelogFileBehind, because a failed writer serves
// nothing until a restart. The closed writer also releases the directory's
// lock, so a reopened service can take it.
func BreakChangelogWriter(ds substrate.Dataset) {
	d := ds.(*dataset)
	d.writerMu.Lock()
	defer d.writerMu.Unlock()
	_ = d.writer.Close()
}

// SealedAAD builds the additional data a sealed-store row binds to, so a test
// opens a payload the way the engine does (ADR 0023).
func SealedAAD(ref, recordKind, recordID string) []byte { return sealedAAD(ref, recordKind, recordID) }

// DEKAAD builds the additional data the control-plane DEK wrap binds to.
func DEKAAD(repoID string) []byte { return dekAAD(repoID) }

// SealWithKey seals raw under key bound to aad, the way the host credential
// key wraps a DEK, so a test can build a directory another binary wrote.
func SealWithKey(key, raw, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return sealWith(aead, raw, aad)
}

// refPaths reads a record's reference property as the record paths it names, in
// order. It is the tests' one reader of a stored reference, so a test asserting
// on a pointer does not have to know whether the declaration carries link data:
// both shapes answer here.
func refPaths(e *substrate.Record, name string) []string {
	v := e.Properties[name]
	list, repeated := v.([]any)
	if !repeated {
		list = []any{v}
	}
	var out []string
	for _, item := range list {
		if p := referencePathOf(item); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// refPath is refPaths for a single-valued reference, "" when it names nothing.
func refPath(e *substrate.Record, name string) string {
	if paths := refPaths(e, name); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

// refIDs is refPaths with the kind stripped off each path.
func refIDs(e *substrate.Record, name string) []string {
	paths := refPaths(e, name)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		_, id, _ := vocabulary.SplitRecordPath(p)
		out = append(out, id)
	}
	return out
}

// refID is refIDs for a single-valued reference, "" when it names nothing.
func refID(e *substrate.Record, name string) string {
	if ids := refIDs(e, name); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// ReceiveWebhookSync is the public webhook door with the fire run inline
// rather than handed to the background supervisor, so a test asserts on what
// the delivery wrote the moment the call returns.
func ReceiveWebhookSync(ctx context.Context, svc substrate.Service, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	return svc.(*service).receiveWebhook(ctx, authority, trigger, key, req, webhookFireInline)
}

// ReceiveWebhookHeld is the public webhook door with the drain run inline
// and stopped right after it: the request is recorded as a pending entry,
// the fire id returned, and nothing fires, the state a process leaves when
// it dies between the drain's commit and the fire. A test reopens the
// service to show the next open resumes it.
func ReceiveWebhookHeld(ctx context.Context, svc substrate.Service, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	return svc.(*service).receiveWebhook(ctx, authority, trigger, key, req, webhookFireHeld)
}

// ReceiveWebhookSpooled is the public webhook door stopped right after its
// 202: the request is in the spool and nothing has drained it, the state a
// process leaves when it dies between the answer and the drain.
func ReceiveWebhookSpooled(ctx context.Context, svc substrate.Service, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	return svc.(*service).receiveWebhook(ctx, authority, trigger, key, req, webhookFireSpooled)
}

// WithWebhookSpoolBounds sets the per-repository spool bounds for one test.
// The bounds are process-wide, so a test that calls it is not parallel.
func WithWebhookSpoolBounds(t interface{ Cleanup(func()) }, maxEntries int, maxBytes int64) {
	prevEntries, prevBytes := webhookSpoolMaxEntries, webhookSpoolMaxBytes
	webhookSpoolMaxEntries, webhookSpoolMaxBytes = maxEntries, maxBytes
	t.Cleanup(func() { webhookSpoolMaxEntries, webhookSpoolMaxBytes = prevEntries, prevBytes })
}

// WithPruneBatch sets how many runs one settling transaction tombstones
// (pruneBatch) for one test. The bound is process-wide, so a test that
// calls it is not parallel.
func WithPruneBatch(t interface{ Cleanup(func()) }, n int) {
	prev := pruneBatch
	pruneBatch = n
	t.Cleanup(func() { pruneBatch = prev })
}

// SpoolStatsOf is the webhook spool's reading for one repository, as its
// metrics report it.
func SpoolStatsOf(ds substrate.Dataset) (entries int, bytes int64, oldest time.Time) {
	st := ds.(*dataset).spoolStats()
	return st.Entries, st.Bytes, st.Oldest
}

// WebhookPendingError is the error a recorded webhook request carries under
// the trigger's parked failures until its fire settles.
const WebhookPendingError = pendingWebhookError

// PoolStats is the repository pool's statistics, for a test that has to know
// every connection is held.
func PoolStats(ds substrate.Dataset) sql.DBStats { return ds.(*dataset).db.Stats() }

// HoldVocabularyWrites takes the dataset's vocabulary write mutex, as a batch
// in flight holds it, and returns what releases it.
func HoldVocabularyWrites(ds substrate.Dataset) (release func()) {
	d := ds.(*dataset)
	d.vocabularyWriteMu.Lock()
	return d.vocabularyWriteMu.Unlock
}

// WidenAggregateSlots lets n aggregate reads run at once in this process. The
// suite runs many repositories' small reads in parallel in one process, which
// the production bound (aggregateSlots, sized for one server's memory) would
// queue past its wait.
func WidenAggregateSlots(n int) { aggregateSem = make(chan struct{}, n) }

// SetAggregateDeadline bounds every aggregate read in this process to d
// until the test ends. A test calling it must not run in parallel.
func SetAggregateDeadline(t testing.TB, d time.Duration) {
	saved := aggregateDeadline
	aggregateDeadline = d
	t.Cleanup(func() { aggregateDeadline = saved })
}

// AggregateRaw makes every aggregate read under the context take raw points
// where rollup rows could serve, so a test holds the rollup answer against
// the raw one.
func AggregateRaw(ctx context.Context) context.Context {
	return context.WithValue(ctx, aggregateRawKey{}, true)
}

// AggregateTrace counts what an aggregate read took from observed_rollups
// and observed_points.
type AggregateTrace struct{ t *aggTrace }

// RollupRows is the rollup rows the read took.
func (a AggregateTrace) RollupRows() int64 { return a.t.rollupRows }

// RawPoints is the raw points the read took.
func (a AggregateTrace) RawPoints() int64 { return a.t.rawPoints }

// MergedRows is the rollup rows the reads' reductions merged.
func (a AggregateTrace) MergedRows() int64 { return a.t.mergedRows }

// AfterPlan sets a hook each metric read under the trace's context runs once
// its plan has read every row and point it reduces, before the reduction.
func (a AggregateTrace) AfterPlan(hook func()) { a.t.afterPlan = hook }

// BeforeReducer sets a hook each metric read under the trace's context runs
// as each reducer of a row begins, after the read's last check of its context
// before it.
func (a AggregateTrace) BeforeReducer(hook func()) { a.t.beforeReducer = hook }

// Rows is the rows the reads assembled, every reducer answered.
func (a AggregateTrace) Rows() int64 { return a.t.rows }

// PlaceSteps is the steps the property reads counted placing their records.
func (a AggregateTrace) PlaceSteps() int64 { return a.t.placeSteps }

// DiscoverBatch sends form discovery's pairs of a stream and a window n at
// a time.
func (a AggregateTrace) DiscoverBatch(n int) { a.t.discoverBatch = n }

// DiscoverPairs is the most pairs one form discovery statement read.
func (a AggregateTrace) DiscoverPairs() int { return a.t.discoverPairs }

// AggregateTraced counts what each aggregate read under the context takes.
func AggregateTraced(ctx context.Context) (context.Context, AggregateTrace) {
	t := &aggTrace{}
	return context.WithValue(ctx, aggregateTraceKey{}, t), AggregateTrace{t: t}
}

// ExplainPlacedReads answers, by name, the plan of each statement a metric
// read finds and reads one stream's segments meeting [from, to) with, as the
// read binds them, under EXPLAIN ANALYZE on db with sequential scans priced
// out, so a small table shows the rows each index range reads.
func ExplainPlacedReads(ctx context.Context, db *sql.DB, placeEnd bool, metric, kind, source, form string, from, to time.Time) (map[string]string, error) {
	var id, extent int64
	if err := db.QueryRowContext(ctx, `SELECT id, max_extent_ms FROM observed_streams
		WHERE metric = $1 AND kind = $2 AND source = $3 AND about = '' AND channel = '' AND statistic = ''`,
		metric, kind, source).Scan(&id, &extent); err != nil {
		return nil, err
	}
	a, b, lo := []time.Time{from}, []time.Time{to}, []time.Time{from.Add(-time.Duration(extent) * time.Millisecond)}
	statements := []struct {
		name, query string
		args        []any
	}{
		{"discover", discoverSegmentsSQL, []any{[]int64{id}, []int{1}, lo, a, b, []int{1}, []string{form}}},
		{"read", spanSegmentsSQL, []any{id, form, lo, a, b}},
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, st := range statements {
		rows, err := tx.QueryContext(ctx, `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF) `+st.query, st.args...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", st.name, err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				return nil, err
			}
			plan = append(plan, line)
		}
		if err := finishRows(rows); err != nil {
			return nil, fmt.Errorf("%s: %w", st.name, err)
		}
		out[st.name] = strings.Join(plan, "\n")
	}
	return out, nil
}

// SetOAuthClock points the OAuth facility's clock (its state expiry and its
// refresh backoff) at now, so a test moves past a backoff step instead of
// sleeping through it. Call it before the service's first OAuth use.
func SetOAuthClock(svc substrate.Service, now func() time.Time) { svc.(*service).oauth.Now = now }

// OAuthRefreshHands is how many callers hold or wait for the refresh turn of
// any credential in ds's repository, so a test that holds one refresh at the
// provider can wait until every other caller it started has queued behind it.
func OAuthRefreshHands(ds substrate.Dataset) int {
	repository := ds.(*dataset).scope.Repository
	oauthRefreshes.mu.Lock()
	defer oauthRefreshes.mu.Unlock()
	n := 0
	for k, e := range oauthRefreshes.entries {
		if k.repository == repository {
			n += e.users
		}
	}
	return n
}

// SetOAuthCallBudget shortens the bound one refresh's provider call runs
// under, so a test can run a call past it.
func SetOAuthCallBudget(svc substrate.Service, d time.Duration) {
	svc.(*service).testOAuthCallBudget = d
}

// SetOAuthBeforeStamp runs fn after a refused refresh found its credential
// not replaced and before the refusal is stamped on the account.
func SetOAuthBeforeStamp(svc substrate.Service, fn func()) {
	svc.(*service).testOAuthBeforeStamp = fn
}

// LoseWriterLease makes svc refuse every write as a process that lost its
// writer lease does, until the returned func is called. It holds the lease's
// mutex meanwhile, so no heartbeat retakes the lease under the test.
func LoseWriterLease(svc substrate.Service) (restore func()) {
	l := svc.(*service).lease
	l.mu.Lock()
	l.lost.Store(true)
	return sync.OnceFunc(func() {
		l.lost.Store(false)
		l.mu.Unlock()
	})
}

// OAuthUnstoredTokens is how many credentials of ds's repository hold a
// refreshed token in memory because its store failed. A test calls it once
// every refresh it started has returned.
func OAuthUnstoredTokens(ds substrate.Dataset) int {
	repository := ds.(*dataset).scope.Repository
	oauthRefreshes.mu.Lock()
	defer oauthRefreshes.mu.Unlock()
	n := 0
	for k, e := range oauthRefreshes.entries {
		if k.repository == repository && e.pending != nil {
			n++
		}
	}
	return n
}

// ObservedPoint is one point the observed index derives from a stored
// record's segment, as a test reads it: the index keeps no point, so this is
// deriveObserved run again over the records the segments name.
type ObservedPoint struct {
	Kind, RecordID                                  string
	Ord                                             int
	Metric, Source, About, Channel, Statistic, Form string
	TS, TSEnd                                       time.Time
	Offset                                          int64
	OffsetKnown                                     bool
	Unit                                            string
	Delta, Circular                                 bool
	Value                                           *float64
	Exact                                           *string
	Code, Comparator                                string
	HoldMs                                          *int64
	ResetRule, WrapAt                               *string
	ResetAt                                         *time.Time
}

// ObservedPoints derives every point of every segment of the repository,
// in (kind, record id, ord) order.
func ObservedPoints(ctx context.Context, ds substrate.Dataset) ([]ObservedPoint, error) {
	d := ds.(*dataset)
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	at, err := d.unitGraph(ctx, tx)
	if err != nil {
		return nil, err
	}
	st := newObsStore(ctx, tx, d.liveFoldView(), at.graph)
	segs, err := st.scanSegments(`SELECT ` + segmentColumns + ` FROM observed_segments ORDER BY kind, record_id`)
	if err != nil {
		return nil, err
	}
	if err := st.derive(segs); err != nil {
		return nil, err
	}
	var out []ObservedPoint
	for _, g := range segs {
		r := st.rows[g.ref]
		if r == nil {
			continue
		}
		for _, p := range r.points {
			out = append(out, ObservedPoint{
				Kind: r.key.kind, RecordID: r.id, Ord: p.ord,
				Metric: r.key.metric, Source: r.key.source, About: r.key.about, Channel: r.key.channel,
				Statistic: r.key.statistic, Form: r.form, TS: p.ts, TSEnd: p.tsEnd, Offset: p.offset, OffsetKnown: p.offsetKnown,
				Unit: r.unit, Delta: r.delta, Circular: r.circular, Value: p.value, Exact: p.exact,
				Code: p.code, Comparator: p.comparator, HoldMs: r.holdMs, ResetRule: r.resetRule, WrapAt: r.wrapAt, ResetAt: r.resetAt,
			})
		}
	}
	return out, nil
}
