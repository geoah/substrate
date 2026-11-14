package engine

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
)

// The seams a test outside this package drives: the acceptance drill in
// internal/testenv opens the engine the way substrated does and has to kill a
// boot import at a batch boundary and move the TOTP clock, which a test-only
// file (export_test.go) could not give it. Each installs a hook the
// production path already carries; none changes what the engine does when
// the hook is absent.
//
// The stepper in steptest.go is the second kind of seam and stays in its own
// file: it does not install a hook, it DRIVES a production path (one
// invocation of a callable, then the dispatcher's own effect commit,
// txn.applyEffects) with the dispatcher's bookkeeping left off. The
// distinction is what each one promises — a hook here promises the engine
// behaves as if it were absent, the stepper there promises only the write
// half it shares with production.

// WithTestTOTPClock is the clock the TOTP verifier reads (auth.go totpVerify
// callers), and nothing else: the record timestamps stay on the wall clock.
// A test that has spent one window's codes advances it one step instead of
// sleeping through a real 30 second window.
func WithTestTOTPClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// WithTestHealthClock is the clock the dispatcher's failing-trigger read
// measures a park's age on (health.go), and nothing else: the run rows and
// every other record timestamp stay on the wall clock. A test advances it
// past the window instead of sleeping through one.
func WithTestHealthClock(now func() time.Time) Option {
	return func(o *options) { o.healthNow = now }
}

// WithTestNamesClock is the clock a repository's former name lapses on
// (repositorynames.go), and nothing else. A test advances it past the hold
// instead of waiting 90 days.
func WithTestNamesClock(now func() time.Time) Option {
	return func(o *options) { o.namesNow = now }
}

// WithTestRetryClock is the clock the walk of temporary failures reads a
// retry's due time on (temporaryretry.go), and nothing else: the parked row's
// time and every record timestamp stay on the wall clock. A test advances it
// past a gap instead of sleeping through one.
func WithTestRetryClock(now func() time.Time) Option {
	return func(o *options) { o.retryNow = now }
}

// WithTestScheduleClock is the clock a schedule trigger's due occurrences
// are read on (dataset.scheduleNow): what a pass fires and what a status
// read counts as owed. Every record timestamp stays on the wall clock. A
// test holds it inside one hour, so an hourly rule's latest occurrence does
// not move under it.
func WithTestScheduleClock(now func() time.Time) Option {
	return func(o *options) { o.scheduleNow = now }
}

// TOTPPeriod is the verifier's step, for a test that moves its clock one.
const TOTPPeriod = totpPeriod

// TestClock is a TOTP clock a test hands WithTestTOTPClock: the wall clock
// plus what Advance has added, so a code spent in one window is followed by
// the next window's code without a real 30 second wait.
type TestClock struct {
	mu     sync.Mutex
	offset time.Duration
}

// Now is the wall clock plus the advance.
func (c *TestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset).UTC()
}

// Advance moves the clock forward by d.
func (c *TestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

// WithTestImportFault runs fn at each durable step of a boot import
// (repodir.go importEntries): after every batch of changelog rows commits,
// with ImportAfterBatch, and inside the refold's one transaction, after its
// first replay and before its commit, with RefoldAfterFirstReplay and
// RefoldBeforeCommit. An error from fn ends the boot there, which is the
// shape of a process dying at that step. A batch above zero replaces
// rebuildBatch for the import's row batches, so a short history spans
// several.
func WithTestImportFault(batch int, fn func(stage string) error) Option {
	return func(o *options) { o.importFault, o.importBatch = fn, batch }
}

// WithTestRepositoryMigrationFault runs fn inside each repository
// migration's transaction (repomigrate.go), after the migration's own writes
// and before its ledger row, with the repository's id and the migration's
// version. An error from fn fails that migration, and its transaction rolls
// back, as a migration that refuses the rows it meets does.
func WithTestRepositoryMigrationFault(fn func(repository string, version int) error) Option {
	return func(o *options) { o.migrationFault = fn }
}

// WithTestReplay runs page after each page of every replay (rebuild.go
// txn.rebuild) with the seq it reached, and end at the replay's end, both
// inside its transaction and before it commits, so a test can act beside a
// replay and read what its transaction does: Postgres's counts of its own
// reads, or the plans its statements get. Either may be nil. Tests only.
func WithTestReplay(page func(seq int64, tx *sql.Tx), end func(tx *sql.Tx)) Option {
	return func(o *options) { o.replayPage, o.replayEnd = page, end }
}

// WithTestStatisticsLockTimeout replaces the ten minutes the reset before a
// replay and the ANALYZE after it wait for a table's lock, so a test can hold
// the lock past it. Tests only.
func WithTestStatisticsLockTimeout(d time.Duration) Option {
	return func(o *options) { o.statisticsLockTimeout = d }
}

// WithTestReplaySQL runs fn with every statement a replay runs through its
// transaction, so a test can hold the list of the fold's statements to what
// it checks. Tests only.
func WithTestReplaySQL(fn func(sqlText string)) Option {
	return func(o *options) { o.replaySQL = fn }
}

// WithTestActorFillOff skips the open's actor fill (actorfill.go), so a
// test can hold a repository as a binary from before the fill left it: the
// actor records its history links and migration 9 never wrote still
// missing, and no mark.
func WithTestActorFillOff() Option {
	return func(o *options) { o.noActorFill = true }
}

// WithTestRulesFault runs fn before each read of the rules a check holds a
// hand to (ruleFault): a token's row, a person's roles, an app's grant, the
// holders of a piece of work. An error fails that read, as a database that
// did not answer would.
func WithTestRulesFault(fn func(what string) error) Option {
	return func(o *options) { o.rulesFault = fn }
}

// ruleFault is the rules' read seam (WithTestRulesFault): nil outside tests.
func (s *service) ruleFault(what string) error {
	if s.testRulesFault == nil {
		return nil
	}
	return s.testRulesFault(what)
}

// WithTestEraseFault runs fn at each durable step of a repository's
// deletion (repositories.go eraseRepositoryOn); an error stops the deletion
// there, as a process killed there would.
func WithTestEraseFault(fn func(stage string) error) Option {
	return func(o *options) { o.eraseFault = fn }
}

// The deletion's stages WithTestEraseFault reports.
const (
	EraseAfterBegan         = eraseAfterBegan
	EraseAfterMark          = eraseAfterMark
	EraseAfterArchiveObject = eraseAfterArchiveObject
	EraseAfterArchive       = eraseAfterArchive
	EraseAfterDirectory     = eraseAfterDirectory
)

// The import stages WithTestImportFault reports.
const (
	ImportAfterRow         = importAfterRow
	ImportAfterSealed      = importAfterSealed
	ImportBeforeCleanup    = importBeforeCleanup
	CleanupSetAside        = changelogfile.RemoveHistorySetAside
	CleanupPartly          = changelogfile.RemoveHistoryPartly
	ImportAfterBatch       = importAfterBatch
	RefoldAfterFirstReplay = refoldAfterFirstReplay
	RefoldBeforeCommit     = refoldBeforeCommit
	TakeAfterStaged        = takeAfterStaged
	TakeAfterStaging       = takeAfterStaging
	// The conversion's, which an import runs between its rows going in and
	// their fold, and the boot check runs over a live repository.
	ConversionBeforeCommit = conversionBeforeCommit
	ConversionAfterCommit  = conversionAfterCommit
)

// WithTestInvokeHook runs fn with a function's identity as the runner is
// about to invoke its body (runner.go runCallableRaw): the moment a test
// that must act mid-fire (stop the server, retry it by hand) can wait for.
func WithTestInvokeHook(fn func(function string)) Option {
	return func(o *options) { o.invokeHook = fn }
}

// WithTestConversionStep adds one step to the conversion chain, a dialect
// past the chain the binary carries, which rewrites every line with line: a
// step no release ships, so a test runs a chain that is not empty over what
// it imports, as the kept directories' comparison does with the difference
// it expects of that step. Tests only.
func WithTestConversionStep(name string, line func(e *changelogfile.Entry) error) Option {
	return func(o *options) {
		base := o.conversion
		if base == nil {
			base = conversionSteps
		}
		o.conversion = append(slices.Clone(base), conversionStep{Dialect: chainDialect(base) + 1, Name: name, Line: line})
	}
}

// WithTestNameDoorsOpen lets a rename and an import under another name take
// a name the repository holds a package under, which their early refusals
// refuse (repositorynames.go), so a test can build the state the spellings
// hold to.
func WithTestNameDoorsOpen() Option {
	return func(o *options) { o.nameDoorsOpen = true }
}

// WithTestAfterTitleRounds runs fn inside the open's title pass
// (localtitles.go) once its rounds have ended and before it writes, so a
// test can see what the pass holds then.
func WithTestAfterTitleRounds(fn func()) Option {
	return func(o *options) { o.afterTitleRounds = fn }
}

// WithTestAfterProof runs fn with the door's name (`login`, `password`,
// `totp`, `signout`, `delete`, `register`) once a request has proved the
// factors and before it acts, so a test can hold it there while the factors
// change.
func WithTestAfterProof(fn func(door string)) Option {
	return func(o *options) { o.afterProof = fn }
}

// WithTestCommitTx hands every COMMIT to fn, which answers in its place: it
// may run commit, hold it, run it in its own time and answer first, or not
// run it at all (the transaction is then rolled back).
func WithTestCommitTx(fn func(commit func() error) error) Option {
	return func(o *options) { o.commitTx = fn }
}

// WithTestTokenProof runs fn before each attempt to prove that a commit in
// doubt which moved a token has ended; an error fails that attempt, and the
// next one follows.
func WithTestTokenProof(fn func() error) Option {
	return func(o *options) { o.tokenProof = fn }
}

// WithTestTokenProofAttempt is how long one attempt at that proof waits for
// the lock before it counts as failed.
func WithTestTokenProofAttempt(d time.Duration) Option {
	return func(o *options) { o.tokenProofAttempt = d }
}

// WithTestCommitInDoubt answers a write's commit with an error after Postgres
// committed it, for each write fail reports true: the shape of a connection
// lost at the commit's answer (dataset.go commit), the CommitInDoubt stage of
// the in-package commit seam. The write's error path runs as it would for a
// real loss.
func WithTestCommitInDoubt(fail func() bool) Option {
	return func(o *options) {
		o.commitFault = func(stage string) error {
			if stage == commitInDoubt && fail() {
				return errTestCommitInDoubt
			}
			return nil
		}
	}
}

var errTestCommitInDoubt = errors.New("substrate/engine: the test lost the commit's answer")

// WithTestChangeReadHook runs fn with the repository each time a stream
// following the changelog's tail (substrate.WithTailRead) reads the table's
// rows (changetail.go): the read every stream of a repository shares, or the
// read of one whose cursor is below the shared window.
func WithTestChangeReadHook(fn func(repository string)) Option {
	return func(o *options) { o.changeReadHook = fn }
}

// WithTestArchiveFault runs fn at each durable step of an archive pass, so a
// test stops the pass there by returning an error, as a process killed there
// stops it, and WithArchivePieceBytes cuts pieces small, so a short history
// spans several. Tests only.
func WithTestArchiveFault(fn func(stage string) error) Option {
	return func(o *options) { o.archiveFault = fn }
}

// WithArchivePieceBytes sets the size archive pieces are cut at.
func WithArchivePieceBytes(n int64) Option { return func(o *options) { o.archivePieceBytes = n } }

// The archive pass's stages WithTestArchiveFault reports.
const (
	ArchiveAfterSpool     = archiveAfterSpool
	ArchiveAfterSnapshot  = archiveAfterSnapshot
	ArchiveAfterListing   = archiveAfterListing
	ArchiveAfterObjectRow = archiveAfterObjectRow
	ArchiveAfterObjectPut = archiveAfterObjectPut
	ArchiveAfterBlobs     = archiveAfterBlobs
	ArchiveAfterSealed    = archiveAfterSealed
	ArchiveAfterPiece     = archiveAfterPiece
	ArchiveAfterPending   = archiveAfterPending
	ArchiveAfterManifest  = archiveAfterManifest
	ArchiveAfterPublished = archiveAfterPublished
	ArchiveAfterMark      = archiveAfterMark
	ArchiveAfterDeleteRow = archiveAfterDeleteRow
	ArchiveAfterDeleteHit = archiveAfterDeleteHit
	ArchiveAfterDelete    = archiveAfterDelete
)

// WithArchiveStepBytes sets how many blob bytes a pass copies before it
// publishes (DefaultArchiveStepBytes).
func WithArchiveStepBytes(n int64) Option { return func(o *options) { o.archiveStepBytes = n } }

// WithTestEndOfTransactionCheck adds a check to every transaction's
// end-of-transaction checks (dataset.go inTxOn), beside the declared units
// and the metric rules: check is handed whether the declarations the
// transaction leaves hold a package, and its error refuses the transaction,
// as one of those checks would.
func WithTestEndOfTransactionCheck(check func(declares func(pkg string) bool) error) Option {
	return func(o *options) { o.endOfTxCheck = check }
}

// WithTestShippedUpgradeRefusal adds refusal lines to a repository's shipped
// upgrade guards (seed.go), so an open refuses the upgrade as a binary did
// before a guard was mended: the repository serves on the declarations it
// stored, and the next open without the hook upgrades it. refuse answers the
// lines for one repository's id, none to let it upgrade.
func WithTestShippedUpgradeRefusal(refuse func(repository string) []string) Option {
	return func(o *options) { o.shippedRefusal = refuse }
}

// WithTestCompactionRetention answers how long each kind's history is kept,
// in the place of the kinds' own retention (compactpass.go keptFor): keep
// 0 keeps nothing past a record's latest state, all every entry. Tests only.
func WithTestCompactionRetention(kept func(kind string) (all bool, keep time.Duration)) Option {
	return func(o *options) {
		o.compactionKept = func(kind string) historyKept {
			all, keep := kept(kind)
			return historyKept{all: all, keep: keep}
		}
	}
}

// ActorPosition is one place a line or a record holds a hand where the
// engine writes one (localhands.go: actorIDPositions, actorRefPositions,
// actorPayloads), with the value that stands there.
type ActorPosition struct {
	// Name is "<where>/<path>": where is "payload", an effect's kind or a
	// record's kind, as the tables key them.
	Name string
	// Ref is whether the engine writes a reference value at an actor record
	// there, not a bare id.
	Ref   bool
	Value any
}

// ActorPositions is every position the engine writes a hand at, without a
// value, so a test can hold every one of them to having been seen. Tests
// only.
func ActorPositions() []ActorPosition {
	var out []ActorPosition
	for _, where := range sortedKeys(actorIDPositions) {
		for _, path := range actorIDPositions[where] {
			out = append(out, ActorPosition{Name: where + "/" + path})
		}
	}
	for _, kind := range sortedKeys(actorRefPositions) {
		for _, prop := range actorRefPositions[kind] {
			out = append(out, ActorPosition{Name: kind + "/" + prop, Ref: true})
		}
	}
	return out
}

// ActorPositionsInPayload is every value a line's payload holds at a
// position the engine writes a hand at, walked as the local namespace's
// conversion walks it (actorMover.payload): the payload's own positions,
// each effect's by its kind, what each record effect sets by the record's
// kind, and a payload a record keeps of its own. Tests only.
func ActorPositionsInPayload(p map[string]any) []ActorPosition {
	var out []ActorPosition
	actorPositionsInPayload(p, &out)
	return out
}

// ActorPositionsInRecord is every value a record of kind holds, in its
// properties, at a position the engine writes a hand at. Tests only.
func ActorPositionsInRecord(kind string, props map[string]any) []ActorPosition {
	var out []ActorPosition
	actorPositionsInRecord(kind, props, &out)
	return out
}

func actorPositionsInPayload(p map[string]any, out *[]ActorPosition) {
	for _, path := range actorIDPositions["payload"] {
		actorValuesAt(p, path, func(v any) { *out = append(*out, ActorPosition{Name: "payload/" + path, Value: v}) })
	}
	effects, _ := p[foldPayloadKey].([]any)
	for _, raw := range effects {
		ef, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := ef["kind"].(string)
		for _, path := range actorIDPositions[kind] {
			actorValuesAt(ef, path, func(v any) { *out = append(*out, ActorPosition{Name: kind + "/" + path, Value: v}) })
		}
		ref, _ := ef["ref"].(string)
		delta, _ := ef["delta"].(map[string]any)
		if set, ok := delta["set"].(map[string]any); ok {
			actorPositionsInRecord(ref, set, out)
		}
	}
}

// WithTestCompactionClock is the compaction pass's clock: entries are past
// their retention by it. Tests only.
func WithTestCompactionClock(now func() time.Time) Option {
	return func(o *options) { o.compactionNow = now }
}

// The compaction pass's stages, as its test seam names them
// (WithTestCompactionFault): inside a batch's transaction, before its
// commit; after a batch committed; once every batch of a kind's window
// committed, before its mark moves; inside a batch of the foreign-effects
// index's derivation; inside a batch that forgets deletions; and between a
// permanent delete's or a forget's commit and its erasure.
const (
	CompactionInBatch    = compactionInBatch
	CompactionAfterBatch = compactionAfterBatch
	CompactionBeforeMark = compactionBeforeMark
	CompactionInIndex    = compactionInIndex
	CompactionInForget   = compactionInForget
	CompactionToForget   = compactionToForget
	ErasureBetween       = erasureBetween
)

// WithTestPurgeKeepsHistory makes a permanent delete keep the record's
// history, as a release before the erasure did: what the compaction pass
// forgets as an old deletion, and what a repository migration reads of a
// purged record. Tests only.
func WithTestPurgeKeepsHistory() Option {
	return func(o *options) { o.purgeKeepsHistory = true }
}

// WithTestCompactionFault hands each stage of a compaction pass to fn; an
// error from it stops the pass there, as a server killed at that stage
// would, and a transaction it stops in rolls back. Tests only.
func WithTestCompactionFault(fn func(stage string) error) Option {
	return func(o *options) { o.compactionFault = fn }
}

// WithTestCompactionBatches bounds the pass's batches at records and lines
// in the place of compactionBatchRecords and compactionBatchLines, 0 keeping
// the bound, and tells seen, when it is not nil, of every batch a pass
// committed, a forgetting's included: its kind, its records, the seq it
// compacted them up to and the distinct lines it took as input for its
// records (CompactReport.read), not everything its transaction read. Tests
// only.
func WithTestCompactionBatches(records, lines int, seen func(kind string, ids []string, upTo int64, lines int)) Option {
	return func(o *options) { o.compactionBatches, o.compactionBatch = [2]int{records, lines}, seen }
}

func actorPositionsInRecord(kind string, set map[string]any, out *[]ActorPosition) {
	for _, path := range actorIDPositions[kind] {
		actorValuesAt(set, path, func(v any) { *out = append(*out, ActorPosition{Name: kind + "/" + path, Value: v}) })
	}
	for _, prop := range actorRefPositions[kind] {
		actorValuesAt(set, prop, func(v any) { *out = append(*out, ActorPosition{Name: kind + "/" + prop, Ref: true, Value: v}) })
	}
	if field, ok := actorPayloads[kind]; ok {
		if inner, ok := set[field].(map[string]any); ok {
			actorPositionsInPayload(inner, out)
		}
	}
}

// actorValuesAt hands emit every value at path in v, walked as actorMover.at
// walks it: into objects by key, through lists, and "*" into every value of
// an object.
func actorValuesAt(v any, path string, emit func(any)) {
	switch x := v.(type) {
	case []any:
		for _, y := range x {
			actorValuesAt(y, path, emit)
		}
	case map[string]any:
		if path == "" {
			emit(x)
			return
		}
		key, rest, _ := strings.Cut(path, "/")
		if key == "*" {
			for _, y := range x {
				actorValuesAt(y, rest, emit)
			}
			return
		}
		if y, ok := x[key]; ok {
			actorValuesAt(y, rest, emit)
		}
	default:
		if path == "" && v != nil {
			emit(v)
		}
	}
}

// ActorPositionsInMarks is every value the marks of a record read hold at
// the engine's mark positions (actorIDPositions["mark"]), each served as a
// link to an actor record, not the bare id a line holds there. Tests only.
func ActorPositionsInMarks(marks map[string]any) []ActorPosition {
	var out []ActorPosition
	for _, property := range sortedKeys(marks) {
		for _, path := range actorIDPositions["mark"] {
			actorValuesAt(marks[property], path, func(v any) {
				out = append(out, ActorPosition{Name: "mark/" + path, Ref: true, Value: v})
			})
		}
	}
	return out
}
