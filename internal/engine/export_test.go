package engine

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

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
// runs three times per binary: here, in TestRepositoryProvisioningAndProjections
// and in TestAssertPoolPrincipalRejectsSuperuser, which open testdb.NewSchema.
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
// database and open it again). The two that open testdb.NewSchema and
// migrate from empty are TestRepositoryProvisioningAndProjections and
// TestAssertPoolPrincipalRejectsSuperuser.
func MigratedDSN(t *testing.T) string {
	t.Helper()
	return migratedTemplate.Clone(t)
}

// WithTestCommitFault runs fn at each durable step of a write's commit
// (dataset.go commitAndMirror), five stages. Around the manifest write that
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
// the answer, and the write's error path runs.
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

// WriterLeaseKeySQL is the lease's advisory-lock key expression, for a test
// that takes or probes a lease from a session of its own. It is the expression
// docs/operations.md publishes for reading `pg_locks`.
const WriterLeaseKeySQL = writerLeaseKeySQL

// The commit stages WithTestCommitFault reports, in the order they run.
const (
	CommitBeforeManifest = commitBeforeManifest
	CommitAfterManifest  = commitAfterManifest
	CommitAfterPrepare   = commitAfterPrepare
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

// ErrSealedStoreBroken is what BreakSealedStore's store fails with.
var ErrSealedStoreBroken = errors.New("engine test: the sealed store is broken")

type brokenSealedStore struct{}

func (brokenSealedStore) Write(string, changelogfile.SealedRecord) error { return ErrSealedStoreBroken }
func (brokenSealedStore) Stage(string, changelogfile.SealedRecord) error { return ErrSealedStoreBroken }
func (brokenSealedStore) Commit(string, string) error                    { return ErrSealedStoreBroken }
func (brokenSealedStore) Delete(string, string) error                    { return ErrSealedStoreBroken }

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

// AdvisoryKeySQL is the engine's advisory-lock key expression (identity.go),
// for a test that takes one of the engine's locks by hand: a barrier test that
// composed the key itself would park on a lock nothing else takes.
const AdvisoryKeySQL = advisoryKeySQL

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

// ReceiveWebhookHeld is the public webhook door stopped right after its
// answer: the request is admitted and recorded, the fire id returned, and
// nothing fires, the state a process leaves when it dies after the 202. A
// test reopens the service to show the next open resumes it.
func ReceiveWebhookHeld(ctx context.Context, svc substrate.Service, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	return svc.(*service).receiveWebhook(ctx, authority, trigger, key, req, webhookFireHeld)
}

// WebhookPendingError is the error a recorded webhook request carries under
// the trigger's parked failures until its fire settles.
const WebhookPendingError = pendingWebhookError

// PoolStats is the repository pool's statistics, for a test that has to know
// every connection is held.
func PoolStats(ds substrate.Dataset) sql.DBStats { return ds.(*dataset).db.Stats() }
