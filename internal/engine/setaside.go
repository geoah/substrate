package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A REPOSITORY SET ASIDE AT THE START (decision 0281).
//
// The start opens nothing it has not checked: the credential check, the
// keying (repositoryid.go) and the boot check (repodir.go) each decide, per
// repository, whether it can be served. One that cannot is set aside, and the
// start goes on to the others. A set-aside repository is held here, in this
// process, with the failure that set it aside. Nothing about it is stored,
// and from then on this process writes nothing to it: open refuses it with
// substrate.ErrSetAside before it touches a byte, Repositories leaves it out
// so no loop reaches it, a registration may not take its name, and this
// process holds no writer lease on it. What the start committed for it
// before the failure stays: a keying's new id and converted manifest, a
// history conversion, part of an import. Each is resumable, and the next
// start carries it on.
//
// A process opened beside a running server (WithDirectoryReadOnly) runs no
// start, so it sets nothing aside, and reads the server's decision from the
// writer leases before it writes (refuseBesideTheServersSetAside). A process
// that opens as the writer takes every repository's lease before its start
// writes to any, and refuses beside a server (leaseEveryRepository).
// The next start decides again, so a repository repaired while the others
// served is served after a restart, and one left as it was is set aside
// again with the same failure.
//
// A REPOSITORY WHOSE FIRST OPEN FAILS IS SET ASIDE TOO (decision 0326,
// amending 0281). The open ladder runs after the start, at a repository's
// first open in the process, so the server listens without waiting for
// every repository's. A failure there of a step that reads or rewrites the
// repository's own rows (setsAside), of a kind failureOf reads as the
// repository's, sets it aside at setAsideAtOpen: the step's transaction has
// rolled back, and from then on no request and no loop opens it, so the step
// does not run again until the next start. A failure of any other kind
// answers that open as it always did, and the next open tries again. A
// commit whose answer was lost may have landed, which no rollback undoes; it
// is the server's kind (ErrWriteInDoubt), sets nothing aside, and the next
// open runs the step again, which a repository migration bears: it is
// idempotent and leaves its ledger row once it commits.
//
// WHAT STILL STOPS THE START is a failure of the server's own, which no
// repository's absence would cure: the start canceled, the database not
// answering, another process holding a repository's writer lease (decision
// 0083) or this one losing its own, any failure failureOf reads as the
// server's (a connection lost, a serialization failure, the database
// shutting down), and a start that set aside every repository it found. The last is a server with nothing to serve, whose
// failures are likelier the host's (a full disk, a data root mounted wrong)
// than each repository's, and it stops as the check always did, so a
// deployment of one repository sees no change.

// The step of the start that set a repository aside, the operator's first
// lead on what to repair.
const (
	setAsideAtKey     = "credential key" // its data key's wrap names this host's key and does not open
	setAsideAtLease   = "writer lease"   // its writer lease could not be taken for a reason of its own
	setAsideAtID      = "id"             // it could not take its id (repositoryid.go)
	setAsideAtTake    = "take"           // a take its operator recorded failed once it began to remove (archiveops.go)
	setAsideAtCheck   = "boot check"     // its rows: a 0.x directory held to them, an import or a conversion finished (repodir.go)
	setAsideAtImport  = "import"         // a 0.x directory with no row (repodir.go importDirectory)
	setAsideAtRestore = "restore"        // an archive with no row, or the copy staged for a take (repodir.go reconcileArchive)
	setAsideAtStore   = "archive store"  // the archive's store did not answer once the pass had written some of it: nothing of it is damaged (archivestore.go)
	setAsideAtPeople  = "people"         // its people: the index derived from its actors, or its identity made from its sign-in record (identityboot.go)
	setAsideAtOpen    = "first open"     // its first open, after the start: a repository migration, the load of its stored vocabulary or the shipped packages' upgrade (engine.go openNew)
)

// THE START'S ONE GATE. Every pass of the start that touches a repository
// (reads its rows to change them, removes, erases, cleans up, takes a lease
// for, keys, imports or restores anything of it) does it through through():
//
//   - a repository that is aside, whoever set it aside and in whichever
//     earlier pass, is skipped, so it is changed by nothing from then on: no
//     row, no file, no object, no lease, and a take recorded for it stays
//     recorded and waits;
//   - a failure that is the repository's own sets it aside, with the pass's
//     stage, and the pass goes on to the next;
//   - only a failure that is nobody's (serverFailure, a database that no
//     longer answers) is returned, and it stops the start;
//   - the archive's store not answering is nobody's damage either: a pass
//     that has written nothing yet leaves the repository for a later start,
//     as an archive the store does not answer for is left (reconcileArchive),
//     and one that has written part of it sets it aside at setAsideAtStore,
//     which names the store, so its half-written rows serve nobody.
//
// A pass that ranges over repositories any other way is the bug this
// exists to make impossible.

// passOver names one repository for one pass of the start: the keys and the
// name the set-aside list is asked about, and what it records when the pass
// fails for the repository.
type passOver struct {
	// keys are what the repository is known by here: its id, or for a
	// directory from before ids the name it is kept under.
	keys []string
	// authority is a row's name, which a row's entry is matched by too; a
	// copy with no row leaves it empty and claims its name through entry.
	authority string
	stage     string
	// entry makes the set-aside entry from the failure, for a pass that
	// learns more of the repository as it goes (a claim's name, the id a
	// keying minted). Nil makes a row's entry of keys and authority.
	entry func(cause error) *setAsideRepository
}

// rowPass is a pass over a repository the database holds a row of.
func rowPass(repo Repository, stage string) passOver {
	return passOver{keys: []string{repo.ID}, authority: repo.Authority, stage: stage}
}

// copyPass is a pass over a copy of a repository with no row (an archive, a
// 0.x directory, the copy staged for a take whose rows went): it claims the
// name name gives, read only if the pass fails.
func copyPass(id, stage string, name func() string) passOver {
	return passOver{keys: []string{id}, stage: stage, entry: func(cause error) *setAsideRepository {
		return &setAsideRepository{keys: []string{id}, authority: name(), claim: true, stage: stage, cause: cause}
	}}
}

// openFailure marks a failure of a step of the open ladder that sets its
// repository aside (setsAside), with the step's name.
type openFailure struct {
	step string
	err  error
}

func (e *openFailure) Error() string { return e.err.Error() }
func (e *openFailure) Unwrap() error { return e.err }

// setsAside marks a step of the open ladder whose failure may be the
// repository's own, because it reads or rewrites the repository's rows: a
// repository migration, the load of its stored vocabulary, the shipped
// packages' upgrade. Only a failure failureOf reads as the repository's sets
// it aside; any other, and a failure of another step, answers that open and
// the next open runs the ladder again.
func setsAside(name string, step func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		err := step(ctx)
		if err != nil && ctx.Err() == nil && failureOf(err) == failureRepository {
			return &openFailure{step: name, err: err}
		}
		return err
	}
}

// setAsideAtOpen sets aside a repository whose first open failed at a step
// that sets it aside, and answers the open with the refusal every later one
// gets, its cause in the start's warning; a failure that is nobody's
// (setAside) is answered as it was, and sets nothing aside. A read-only process sets nothing aside: it runs no
// start, and the server beside it decides.
func (s *service) setAsideAtOpen(ctx context.Context, repo Repository, step string, cause error) error {
	if s.readOnly {
		return cause
	}
	if err := s.setAside(ctx, &setAsideRepository{keys: []string{repo.ID}, authority: repo.Authority, stage: setAsideAtOpen, step: step, cause: cause}); err != nil {
		return err
	}
	return s.refuseSetAside(repo.ID, repo.Authority)
}

// asideBefore reports whether the repository p names is aside already.
func (s *service) asideBefore(p passOver) bool {
	for _, k := range p.keys {
		if s.setAsideOf(k, "") != nil {
			return true
		}
	}
	return p.authority != "" && s.setAsideOf("", p.authority) != nil
}

// through runs one pass of the start over one repository (the gate, above).
func (s *service) through(ctx context.Context, p passOver, fn func() error) error {
	if s.asideBefore(p) {
		return nil
	}
	err := fn()
	if err == nil {
		return nil
	}
	if len(p.keys) > 0 {
		s.logInterrupted(ctx, p.keys[0])
	}
	r := &setAsideRepository{keys: p.keys, authority: p.authority, stage: p.stage, cause: err}
	if p.entry != nil {
		r = p.entry(err)
	}
	if storeDidNotAnswer(err) {
		r.stage = setAsideAtStore
	}
	return s.setAside(ctx, r)
}

// setAsidePing bounds the database check a failure is weighed by.
const setAsidePing = 5 * time.Second

// setAsideRepository is one repository the start set aside, and why.
type setAsideRepository struct {
	// keys are the keys it is known by: its id, and for a directory from
	// before ids the name its directory is kept under.
	keys []string
	// authority is its name, where anything readable named it.
	authority string
	// claim says no row was proven to be it: a directory with no row, or one
	// from before ids the keying could not prove a row's. Its name keeps a
	// registration off it and matches no repository, because a row holding
	// that name is another repository until a data key says otherwise.
	claim bool
	stage string
	// step is the step of the open ladder that failed, for a repository set
	// aside at its first open: the refusal names it, so a client and an
	// operator command learn which repair it needs without the log.
	step  string
	cause error
}

// setAsideSet is every repository the start set aside. The start writes it
// and every open reads it, and a registration may race the end of the start.
type setAsideSet struct {
	mu   sync.RWMutex
	list []*setAsideRepository
}

func (a *setAsideSet) add(r *setAsideRepository) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.list = append(a.list, r)
}

// of returns the entry keyed id, or the one of a row named authority; an
// empty argument matches nothing.
func (a *setAsideSet) of(id, authority string) *setAsideRepository {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, r := range a.list {
		if (id != "" && slices.Contains(r.keys, id)) || (authority != "" && !r.claim && r.authority == authority) {
			return r
		}
	}
	return nil
}

// holdsName reports whether anything set aside is named authority, a claim
// included.
func (a *setAsideSet) holdsName(authority string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, r := range a.list {
		if r.authority == authority {
			return true
		}
	}
	return false
}

func (a *setAsideSet) all() []*setAsideRepository {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]*setAsideRepository(nil), a.list...)
}

// count is the metric's reading (metrics.RegisterSetAside).
func (a *setAsideSet) count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.list)
}

// setAsideOf is the start's entry for a repository, by its id or its name.
func (s *service) setAsideOf(id, authority string) *setAsideRepository {
	return s.aside.of(id, authority)
}

// refuseSetAside is open's refusal of a repository the start set aside. Its
// message is the one a client shows, so it says what happened and who acts,
// and leaves the failure itself to the operator's log.
func (s *service) refuseSetAside(id, authority string) error {
	r := s.setAsideOf(id, authority)
	if r == nil {
		return nil
	}
	name := r.authority
	if name == "" {
		name = r.keys[0]
	}
	when := "when it started"
	if r.stage == setAsideAtOpen {
		when = "when it first opened it (" + r.step + " failed)"
	}
	return fmt.Errorf("%w: the server found %s damaged %s, so it serves the others and writes nothing to this one until a restart finds it whole; the server's operator repairs it and restarts the server",
		substrate.ErrSetAside, name, when)
}

// refuseBesideTheServersSetAside is the check a process opened beside a
// running server (WithDirectoryReadOnly) makes before it writes, as the
// re-embed's queue rows do. That process ran no start of its own, and the
// server's set-aside list is the server's memory; what it can read is the
// writer leases (RepositoryStates): a running server holds the lease of every
// repository it serves and none of one it set aside. Work queued for that one
// is work no loop does until a restart finds it whole, written into a
// repository the start found damaged. A process that is the writer needs no
// such check: its own start decided, open holds it to that, and beside a
// running server its start stops at the first lease the server holds.
func (s *service) refuseBesideTheServersSetAside(ctx context.Context, id, authority string) error {
	if !s.readOnly {
		return nil
	}
	states, err := RepositoryStates(ctx, s.maint, "", nil)
	if err != nil {
		return err
	}
	for _, st := range states {
		if st.ID == id && st.State == StateSetAside {
			return fmt.Errorf("%w: the running server set %s aside when it started, or its start has not reached it yet, so it never opens it, and this writes nothing to it either; once its operator repairs it and restarts the server, run this again",
				substrate.ErrSetAside, authority)
		}
	}
	return nil
}

// rowSetAside is the entry of a repository the database holds a row of.
func rowSetAside(repo Repository, stage string, cause error) *setAsideRepository {
	return &setAsideRepository{keys: []string{repo.ID}, authority: repo.Authority, stage: stage, cause: cause}
}

// errDirectoryIsAnothers is the keying's proof that a directory from before
// ids is not the repository whose row holds its name: both data keys opened,
// and they differ.
var errDirectoryIsAnothers = errors.New("substrate/engine: the directory is another repository's")

// errAlreadyAside is the keying's refusal to touch a directory proven to be
// a repository this start already set aside (keyingReaches).
var errAlreadyAside = errors.New("substrate/engine: the repository is set aside")

// keyingSetAside is the entry of a directory from before ids that could not
// take its id. It is set aside with the row of id, when keyDirectory tied it
// to one: by the keying's own record, or by a proof of its data key. Where
// neither holds it claims its name and nothing else, and no row is set aside
// for it, whatever its manifest says in the clear: a creation time, or an id
// it does not prove.
func (s *service) keyingSetAside(ctx context.Context, name, id string, cause error) *setAsideRepository {
	r := &setAsideRepository{keys: []string{name}, authority: name, claim: true, stage: setAsideAtID, cause: cause}
	if id != "" {
		r.keys = append(r.keys, id)
		if _, err := s.repositoryByID(ctx, id); err == nil {
			r.claim = false
		}
	}
	return r
}

// setAside sets one repository aside after the step of the start that failed
// for it, or returns the failure when it is the server's own and the start
// must stop.
func (s *service) setAside(ctx context.Context, r *setAsideRepository) error {
	if serverFailure(ctx, r.cause) || s.lease.err() != nil {
		return r.cause
	}
	// A database that stopped answering fails every repository after the
	// first, and setting each aside would start a server that serves none.
	ping, cancel := context.WithTimeout(ctx, setAsidePing)
	defer cancel()
	if err := s.maint.PingContext(ping); err != nil {
		return r.cause
	}
	s.aside.add(r)
	// This process will not write it, so it holds no lease under any of its
	// keys, and the operator's list reads it as set aside (substratectl
	// repository list).
	for _, key := range r.keys {
		s.lease.release(key)
	}
	attrs := []any{"repository", logSafeID(r.authority), "key", logSafeID(strings.Join(r.keys, ",")), "stage", r.stage}
	if r.step != "" {
		attrs = append(attrs, "step", r.step)
	}
	attrs = append(attrs, "error", r.cause.Error())
	if r.stage == setAsideAtOpen {
		s.log.Warn("substrate: repository set aside at its first open; the server serves the others and never opens it again until a restart finds it whole", attrs...)
	} else {
		s.log.Warn("substrate: repository set aside at start-up; the server serves the others and never opens it until a restart finds it whole", attrs...)
	}
	return nil
}

// serverFailure is a failure no repository's absence would cure.
// The archive's store bounds its own calls (archive.Bounded), so a store
// that timed out says context canceled without the start being canceled.
func serverFailure(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	if storeDidNotAnswer(err) {
		return false
	}
	return failureOf(err) == failureServer
}

// failureKind is what a failure of a step of the start or of a first open is
// read as, by the kind of its error (failureOf, decision 0326).
type failureKind int

const (
	// failureUnknown is an error whose kind does not tell, and the default:
	// a plain error, database/sql's own untyped errors, pgx's plain protocol
	// errors, a version conflict, no row, a statement the data fails (the
	// classes 22 and 23, which a race with another session or a parameter
	// the code built can raise as well), a database refusal of no class
	// below, a file's error and the operating system's other numbers (EIO,
	// ENOENT). The start sets its repository aside, as its passes read files
	// whose damage has no kind of its own; a first open answers it and tries
	// again at the next one, the lesser harm.
	failureUnknown failureKind = iota
	// failureServer may pass with nothing changed: the start or a request
	// canceled, another writer or a lost lease, a connection lost, refused,
	// reset or timed out (the network's numbers, bare or inside a file's
	// error on a network mount), the host's limits (a full disk, a quota,
	// open files, memory), a statement canceled, a serialization failure or
	// a deadlock, a resource limit, the database shutting down, a commit
	// whose answer was lost, and what says by name that it is not available
	// yet or until a restart. It stops the start, and a first open answers
	// it and tries again at the next one.
	failureServer
	// failureRepository cannot pass without a change to the repository or to
	// the binary, and is named, never inferred: a ledger this binary cannot
	// continue, a stored declaration in a shape or dialect this binary cannot
	// read, a validation refusal (a stored closure that does not load, a
	// shipped upgrade a row's values refuse), stored data that is corrupt,
	// and a migration's own refusal, marked where it is returned
	// (ownRefusal).
	failureRepository
)

// failureOf reads err's kind. The server's kind is read first, so a refusal
// that wraps a lost connection is the server's, and what matches nothing is
// unknown.
func failureOf(err error) failureKind {
	var (
		pg      *pgconn.PgError
		refusal *migrationRefusal
		path    *fs.PathError
		errno   syscall.Errno
		network net.Error
	)
	switch {
	case serverKind(err):
		return failureServer
	case errors.As(err, &pg):
		switch class := pg.Code[:min(2, len(pg.Code))]; class {
		case "08", "40", "53", "57", "58", "XX":
			return failureServer
		}
		return failureUnknown
	case errors.Is(err, ErrRepositoryMigrationsNewer), errors.Is(err, errLedgerDiverges),
		errors.Is(err, ErrDeclarationUntranslated), errors.Is(err, ErrVocabularyDialectNewer),
		errors.Is(err, substrate.ErrValidation), errors.Is(err, substrate.ErrCorrupt),
		errors.As(err, &refusal):
		return failureRepository
	// An operating system's error number satisfies net.Error too, so a file's
	// is told before the network's.
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.As(err, &path), errors.As(err, &errno):
		return failureUnknown
	case errors.As(err, &network):
		return failureServer
	}
	return failureUnknown
}

// serverKind reports the errors failureOf reads as the server's by their
// type or by their name, before anything else. pgx 5's transport errors that
// carry a SafeToRetry method (its errors for a failed read or write, a
// connection already busy or closed, a timeout, a context already done) are
// read by that method; pgconn.ErrConnClosed is a plain exported sentinel,
// read by name; a plain protocol error of pgx carries neither, and is
// unknown. Through database/sql, pgx's driver answers a closed connection
// with driver.ErrBadConn.
func serverKind(err error) bool {
	var (
		connect   *pgconn.ConnectError
		transport interface{ SafeToRetry() bool }
		op        *net.OpError
		errno     syscall.Errno
	)
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrRepositoryHasAnotherWriter) || errors.Is(err, ErrWriterLeaseLost) ||
		errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) ||
		errors.Is(err, pgconn.ErrConnClosed) ||
		errors.Is(err, substrate.ErrWriteInDoubt) || errors.Is(err, substrate.ErrUnavailable) ||
		errors.Is(err, substrate.ErrRestartRequired) ||
		errors.As(err, &connect) || errors.As(err, &transport) || errors.As(err, &op) ||
		(errors.As(err, &errno) && slices.Contains(serverErrnos, errno))
}

// serverErrnos are the operating system's numbers that are the server's
// wherever they appear, bare or inside a file's error (a network mount): the
// network's, and the host's limits. A full disk is never one repository's
// damage. EIO and ENOENT are not among them.
var serverErrnos = []syscall.Errno{
	syscall.ECONNRESET, syscall.EPIPE, syscall.ETIMEDOUT, syscall.ECONNREFUSED, syscall.ECONNABORTED,
	syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ENETDOWN,
	syscall.ENOSPC, syscall.EDQUOT, syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM,
}

// migrationRefusal marks an error a repository migration's own code returned
// refusing the rows it met (ownRefusal): the repository's, told where it is
// returned and never by elimination.
type migrationRefusal struct{ err error }

func (e *migrationRefusal) Error() string { return e.err.Error() }
func (e *migrationRefusal) Unwrap() error { return e.err }

// ownRefusal marks err, returned by a repository migration, as the
// migration's own refusal, unless its chain carries anything else: an error
// of the database (a typed one, or database/sql's own untyped ones, each of
// which begins "sql: " or "driver: "), of the network, of a file or of the
// context, or one of the server's: a commit in doubt, not available yet or
// until a restart, a version conflict, the writer lease. A migration whose
// write met one of those is not refusing its rows.
func ownRefusal(err error) error {
	var (
		pg      *pgconn.PgError
		path    *fs.PathError
		errno   syscall.Errno
		network net.Error
	)
	if serverKind(err) || errors.As(err, &pg) || errors.As(err, &path) || errors.As(err, &errno) || errors.As(err, &network) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, substrate.ErrConflict) || errors.Is(err, sql.ErrNoRows) ||
		anyInChain(err, databaseSQLLeaf) {
		return err
	}
	return &migrationRefusal{err: err}
}

// databaseSQLLeaf reports an error at the end of a chain that database/sql or
// its driver interface returned untyped ("sql: database is closed", "sql:
// Rows are closed"), which only its words tell.
func databaseSQLLeaf(err error) bool {
	if _, ok := err.(interface{ Unwrap() error }); ok {
		return false
	}
	if _, ok := err.(interface{ Unwrap() []error }); ok {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "sql: ") || strings.HasPrefix(msg, "driver: ")
}

// anyInChain reports whether f holds for err or anything it wraps, through
// joined errors too.
func anyInChain(err error, f func(error) bool) bool {
	if err == nil {
		return false
	}
	if f(err) {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range joined.Unwrap() {
			if anyInChain(e, f) {
				return true
			}
		}
		return false
	}
	return anyInChain(errors.Unwrap(err), f)
}

// directoryName is the name of the repository a directory under the data
// root holds: its key, for one from before ids, else its manifest's, or ""
// when none reads. It is what keeps a set-aside import's name from a
// registration.
func directoryName(dataRoot, key string) string {
	if !vocabulary.ValidRepositoryID(key) {
		return key
	}
	dir, err := changelogfile.RepoDir(dataRoot, key)
	if err != nil {
		return ""
	}
	m, err := changelogfile.ReadManifest(dir)
	if err != nil {
		return ""
	}
	return m.Authority
}

// finishSetAside is the start's last word on what it set aside: nothing, a
// line naming each beside the ones it serves, or, when it set aside every
// repository it found, the refusal of the start with each one's failure.
func (s *service) finishSetAside(ctx context.Context) error {
	aside := s.aside.all()
	if len(aside) == 0 {
		return nil
	}
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: list repositories after the boot check: %w", err)
	}
	serving := 0
	for _, r := range repos {
		if s.setAsideOf(r.ID, r.Authority) == nil {
			serving++
		}
	}
	if serving == 0 {
		if len(aside) == 1 {
			return aside[0].cause
		}
		causes := make([]error, 0, len(aside))
		for _, r := range aside {
			causes = append(causes, r.cause)
		}
		return errors.Join(causes...)
	}
	names := make([]string, 0, len(aside))
	for _, r := range aside {
		names = append(names, logSafeID(r.keys[0]))
	}
	s.log.Warn("substrate: started with repositories set aside; each is named above with its failure",
		"setAside", len(aside), "serving", serving, "repositories", strings.Join(names, ","))
	return nil
}

// leaseEveryRepository takes the writer lease of every repository with a row
// before the start keys, checks or imports any of them, so a start beside
// another writer has held every repository or written to none. Taken as the
// start reached each, the first lease a running server held refused the start
// only after it had written every repository before that one, and a server
// holds no lease of one it set aside, so a writer command beside it caught
// that one up or keyed it and was then refused. The boot check takes the same
// leases again, and setAside gives back the one of a repository it sets
// aside. What the credential check already set aside is never written, and
// is left unleased.
func (s *service) leaseEveryRepository(ctx context.Context) error {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: list repositories for their writer leases: %w", err)
	}
	for _, repo := range repos {
		if err := s.through(ctx, rowPass(repo, setAsideAtLease), func() error {
			if err := s.lease.acquire(ctx, repo.ID); err != nil {
				return fmt.Errorf("substrate/engine: repository %s, before this start wrote to any repository: %w", repo.label(), err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// The states the operator's list reads (RepositoryStates).
const (
	// StateServing is a repository a running server holds the writer lease
	// of: one it serves.
	StateServing = "serving"
	// StateSetAside is a repository a running server holds no lease of, while
	// it holds others: its start set it aside, or has not reached it yet.
	StateSetAside = "set aside"
	// StateStopped is a repository while no server runs.
	StateStopped = "stopped"
	// StateNotImported is a 0.x directory or an archive with no row while no
	// server runs: the next start imports or restores it.
	StateNotImported = "not imported"
)

// RepositoryState is one repository as the operator's list shows it: a row,
// or a 0.x directory under the data root or an archive with none.
type RepositoryState struct {
	ID        string
	Authority string
	State     string
	// Directory says it has no row: only its 0.x directory or its archive is
	// there.
	Directory bool
}

// ErrArchiveUnlisted is RepositoryStates' answer when the archive's store
// does not answer its listing: the states it returns beside it are the
// database's rows and the data root's directories, without any repository
// the archive alone holds.
var ErrArchiveUnlisted = errors.New("substrate/engine: the archive did not answer its listing")

// RepositoryStates reads each repository's state beside a running server,
// from who holds the writer leases (decision 0083) in pg_locks: a server
// holds the lease of every repository it serves and none of one it set aside.
// It writes nothing. With a data root it also lists each 0.x directory there
// with no row, and with an archive each repository the archive holds a
// restorable manifest of and the database no row of: a running server's start
// set each aside, since it imports or restores every other. Its name is its
// manifest's, where one reads.
func RepositoryStates(ctx context.Context, db *sql.DB, dataRoot string, store archive.Store) ([]RepositoryState, error) {
	rows, err := db.QueryContext(ctx, `
WITH k AS (SELECT r.id, r.authority, r.created_at, `+strings.Replace(writerLeaseKeySQL, "$1", "r.id", 1)+` AS key FROM repositories r)
SELECT k.id, k.authority, l.pid IS NOT NULL
  FROM k
  LEFT JOIN pg_locks l ON l.locktype = 'advisory' AND l.objsubid = 1 AND l.granted
   AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
   AND l.classid = ((k.key >> 32) & 4294967295)::oid
   AND l.objid = (k.key & 4294967295)::oid
 ORDER BY k.created_at, k.id`)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the writer leases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RepositoryState
	held := map[int]bool{}
	running := false
	for rows.Next() {
		var r RepositoryState
		var leased bool
		if err := rows.Scan(&r.ID, &r.Authority, &leased); err != nil {
			return nil, err
		}
		held[len(out)] = leased
		running = running || leased
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasRow := make(map[string]bool, len(out))
	for i := range out {
		hasRow[out[i].ID] = true
		switch {
		case held[i]:
			out[i].State = StateServing
		case running:
			out[i].State = StateSetAside
		default:
			out[i].State = StateStopped
		}
	}
	unrowed := func(key, name string) {
		d := RepositoryState{ID: key, Authority: name, State: StateNotImported, Directory: true}
		if running {
			d.State = StateSetAside
		}
		hasRow[key] = true
		out = append(out, d)
	}
	if dataRoot != "" {
		keys, err := changelogfile.ListRepositoryDirs(dataRoot)
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			dir, err := changelogfile.RepoDir(dataRoot, key)
			if err != nil {
				return nil, err
			}
			// A directory with no history is a repository's blobs and spool,
			// not a copy a start imports.
			if hasRow[key] || !changelogfile.HasHistory(dir) {
				continue
			}
			unrowed(key, directoryName(dataRoot, key))
		}
	}
	if store != nil {
		all, err := archivedRepositories(ctx, store)
		if err != nil {
			// What the database says stands without the store: the rows and
			// the directories are listed, and the caller says the archive did
			// not answer.
			return out, fmt.Errorf("%w: %s: %w", ErrArchiveUnlisted, store.String(), err)
		}
		for _, id := range sortedKeys(all) {
			if hasRow[id] {
				continue
			}
			if _, _, err := restorable(all[id]); err != nil {
				continue
			}
			unrowed(id, archivedName(ctx, store, id))
		}
	}
	return out, nil
}
