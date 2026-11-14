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
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/substrate"
)

// A failure no repository's absence would cure is the start's to stop on:
// setAside hands it back and sets nothing aside, before it asks the database
// anything.
func TestAFailureOfTheServersOwnIsNotSetAside(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, tc := range map[string]struct {
		ctx context.Context
		err error
	}{
		"the start canceled": {canceled, errors.New("substrate/engine: boot check: interrupted")},
		"a deadline":         {context.Background(), fmt.Errorf("boot check: %w", context.DeadlineExceeded)},
		"another writer":     {context.Background(), fmt.Errorf("boot check: %w", ErrRepositoryHasAnotherWriter)},
		"the lease lost":     {context.Background(), fmt.Errorf("boot check: %w", ErrWriterLeaseLost)},
		// The kinds failureOf reads as the server's, which the start's gate
		// and a first open share (decision 0326).
		"a connection lost":       {context.Background(), fmt.Errorf("boot check: %w", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET})},
		"a serialization failure": {context.Background(), fmt.Errorf("boot check: %w", &pgconn.PgError{Code: "40001"})},
	} {
		s := &service{}
		repo := Repository{ID: "rnd-0000000000000000000000000", Authority: "ada.example.com"}
		if err := s.setAside(tc.ctx, rowSetAside(repo, setAsideAtCheck, tc.err)); !errors.Is(err, tc.err) {
			t.Errorf("%s: setAside = %v, want the failure back", name, err)
		}
		if n := s.aside.count(); n != 0 {
			t.Errorf("%s: %d repositories set aside, want none", name, n)
		}
	}
}

// A set-aside repository is found by its id or its name and refused with the
// reason a client acts on; an empty argument finds nothing. A directory no row
// was proven to be only claims its name: it keeps a registration off it and
// takes no repository of that name down with it.
func TestASetAsideRepositoryIsRefusedByItsIDOrItsName(t *testing.T) {
	t.Parallel()
	s := &service{}
	s.aside.add(rowSetAside(Repository{ID: "rnd-a", Authority: "ada.example.com"}, setAsideAtCheck, errors.New("damaged")))
	s.aside.add(&setAsideRepository{keys: []string{"rnd-c"}, authority: "bob.example.com", claim: true, stage: setAsideAtImport})
	for _, at := range [][2]string{{"rnd-a", ""}, {"", "ada.example.com"}, {"rnd-a", "ada.example.com"}, {"rnd-c", ""}} {
		if err := s.refuseSetAside(at[0], at[1]); !errors.Is(err, substrate.ErrSetAside) {
			t.Errorf("refuseSetAside(%q, %q) = %v, want ErrSetAside", at[0], at[1], err)
		}
	}
	for _, at := range [][2]string{{"", ""}, {"rnd-b", "bob.example.com"}, {"", "bob.example.com"}} {
		if err := s.refuseSetAside(at[0], at[1]); err != nil {
			t.Errorf("refuseSetAside(%q, %q) = %v, want nil", at[0], at[1], err)
		}
	}
	if !s.aside.holdsName("bob.example.com") || !s.aside.holdsName("ada.example.com") || s.aside.holdsName("cat.example.com") {
		t.Error("the names held are not the two set aside")
	}
}

// retryable stands in for an error of pgx's transport, all of which carry a
// SafeToRetry method and none of which is exported.
type retryable struct{}

func (retryable) Error() string     { return "failed to receive message: unexpected EOF" }
func (retryable) SafeToRetry() bool { return false }

// A failure is read by its kind (decision 0326): what may pass with nothing
// changed is the server's, a refusal the engine names is the repository's,
// and everything else is unknown, the default, which a first open answers
// and tries again.
func TestAFailureIsReadByItsKind(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		want failureKind
	}{
		"a request canceled":                   {fmt.Errorf("migrate: %w", context.Canceled), failureServer},
		"a deadline":                           {fmt.Errorf("migrate: %w", context.DeadlineExceeded), failureServer},
		"a connection reset":                   {fmt.Errorf("migrate: %w", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), failureServer},
		"the driver's transport":               {fmt.Errorf("migrate: %w", retryable{}), failureServer},
		"a connection the pool dropped":        {fmt.Errorf("migrate: %w", driver.ErrBadConn), failureServer},
		"a connection done":                    {fmt.Errorf("migrate: %w", sql.ErrConnDone), failureServer},
		"a connection exception":               {&pgconn.PgError{Code: "08006"}, failureServer},
		"a serialization failure":              {&pgconn.PgError{Code: "40001"}, failureServer},
		"a deadlock":                           {&pgconn.PgError{Code: "40P01"}, failureServer},
		"too many connections":                 {&pgconn.PgError{Code: "53300"}, failureServer},
		"the database shutting down":           {&pgconn.PgError{Code: "57P01"}, failureServer},
		"a statement canceled":                 {&pgconn.PgError{Code: "57014"}, failureServer},
		"a system error":                       {&pgconn.PgError{Code: "58030"}, failureServer},
		"an internal error":                    {&pgconn.PgError{Code: "XX000"}, failureServer},
		"a commit in doubt":                    {fmt.Errorf("%w: %w", substrate.ErrWriteInDoubt, errors.New("the answer was lost")), failureServer},
		"not available yet":                    {fmt.Errorf("upgrade: %w", substrate.ErrUnavailable), failureServer},
		"an index being re-derived":            {substrate.ErrObservedRederiving, failureServer},
		"refused until a restart":              {fmt.Errorf("load: %w", substrate.ErrRestartRequired), failureServer},
		"a refusal wrapping a lost connection": {fmt.Errorf("%w: %w", substrate.ErrValidation, retryable{}), failureServer},
		"pgx's closed connection":              {fmt.Errorf("exec: %w", pgconn.ErrConnClosed), failureServer},
		"a connection reset, bare":             {fmt.Errorf("read: %w", syscall.ECONNRESET), failureServer},
		"a broken pipe":                        {syscall.EPIPE, failureServer},
		"a host unreachable":                   {fmt.Errorf("dial: %w", syscall.EHOSTUNREACH), failureServer},
		"a network mount gone":                 {&fs.PathError{Op: "read", Path: "/data/x", Err: syscall.ETIMEDOUT}, failureServer},
		"a full disk":                          {fmt.Errorf("write: %w", syscall.ENOSPC), failureServer},
		"a full disk, in a file":               {&fs.PathError{Op: "write", Path: "/data/x", Err: syscall.ENOSPC}, failureServer},
		"a quota":                              {syscall.EDQUOT, failureServer},
		"too many open files":                  {&fs.PathError{Op: "open", Path: "/data/x", Err: syscall.EMFILE}, failureServer},
		"no memory":                            {syscall.ENOMEM, failureServer},

		"a ledger this binary is behind":   {fmt.Errorf("migrate: %w", ErrRepositoryMigrationsNewer), failureRepository},
		"a ledger that diverges":           {fmt.Errorf("migrate: %w", errLedgerDiverges), failureRepository},
		"a declaration that does not load": {fmt.Errorf("load: %w", ErrDeclarationUntranslated), failureRepository},
		"a newer vocabulary dialect":       {fmt.Errorf("load: %w", ErrVocabularyDialectNewer), failureRepository},
		"a validation refusal":             {fmt.Errorf("upgrade: %w", &substrate.ValidationError{Problems: []string{"props.x: not declared"}}), failureRepository},
		"corrupt stored data":              {fmt.Errorf("read: %w", substrate.ErrCorrupt), failureRepository},
		"a migration's own refusal":        {fmt.Errorf("repository migration 9: %w", ownRefusal(errors.New("the seeded closure x does not admit after the rewrite: y"))), failureRepository},

		"a plain error":                {errors.New("the seeded closure x does not admit after the rewrite: y"), failureUnknown},
		"a database closed":            {fmt.Errorf("read: %w", errors.New("sql: database is closed")), failureUnknown},
		"a statement closed":           {errors.New("sql: statement is closed"), failureUnknown},
		"rows closed":                  {errors.New("sql: Rows are closed"), failureUnknown},
		"a connection closed twice":    {errors.New("sql: duplicate driverConn close"), failureUnknown},
		"a version conflict":           {fmt.Errorf("rewrite: %w", substrate.ErrConflict), failureUnknown},
		"no row":                       {fmt.Errorf("read: %w", sql.ErrNoRows), failureUnknown},
		"a constraint, which may race": {fmt.Errorf("rewrite: %w", &pgconn.PgError{Code: "23505"}), failureUnknown},
		"an exclusion, which may race": {&pgconn.PgError{Code: "23P01"}, failureUnknown},
		"a value the code built":       {&pgconn.PgError{Code: "22P02"}, failureUnknown},
		"an undefined table":           {&pgconn.PgError{Code: "42P01"}, failureUnknown},
		"a raised exception":           {&pgconn.PgError{Code: "P0001"}, failureUnknown},
		"an unexpected end":            {fmt.Errorf("read: %w", io.ErrUnexpectedEOF), failureUnknown},
		"a file":                       {&fs.PathError{Op: "open", Path: "x", Err: fs.ErrPermission}, failureUnknown},
		"an input or output error":     {&fs.PathError{Op: "read", Path: "/data/x", Err: syscall.EIO}, failureUnknown},
		"no such file":                 {&fs.PathError{Op: "open", Path: "/data/x", Err: syscall.ENOENT}, failureUnknown},
		"pgx's plain protocol error":   {errors.New("pipeline closed"), failureUnknown},
	} {
		if got := failureOf(tc.err); got != tc.want {
			t.Errorf("%s: failureOf(%v) = %d, want %d", name, tc.err, got, tc.want)
		}
	}
}

// A migration's own refusal is marked only where its error carries nothing
// else: a migration whose write met the database, the network, a file, the
// context or one of the server's conditions is not refusing its rows, and
// keeps that kind.
func TestAMigrationsOwnRefusalIsMarkedOnlyWhereItIsOwn(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		want failureKind
	}{
		"its own words":             {errors.New("a rewritten closure does not admit"), failureRepository},
		"its own words, joined":     {errors.Join(errors.New("rewrite a"), errors.New("rewrite b")), failureRepository},
		"a version conflict":        {fmt.Errorf("rewrite x: %w", substrate.ErrConflict), failureUnknown},
		"a commit in doubt":         {fmt.Errorf("%w: %w", substrate.ErrWriteInDoubt, errors.New("lost")), failureServer},
		"not available yet":         {fmt.Errorf("rewrite x: %w", substrate.ErrUnavailable), failureServer},
		"a lease lost":              {fmt.Errorf("rewrite x: %w", ErrWriterLeaseLost), failureServer},
		"a database closed":         {fmt.Errorf("rewrite x: %w", errors.New("sql: database is closed")), failureUnknown},
		"a closed database, joined": {errors.Join(errors.New("rewrite a"), fmt.Errorf("rewrite b: %w", errors.New("sql: database is closed"))), failureUnknown},
		"no row":                    {fmt.Errorf("read x: %w", sql.ErrNoRows), failureUnknown},
		"a serialization failure":   {fmt.Errorf("rewrite x: %w", &pgconn.PgError{Code: "40001"}), failureServer},
		"a raised exception":        {fmt.Errorf("rewrite x: %w", &pgconn.PgError{Code: "P0001"}), failureUnknown},
		"a constraint":              {fmt.Errorf("rewrite x: %w", &pgconn.PgError{Code: "23505"}), failureUnknown},
		"a full disk":               {fmt.Errorf("write x: %w", syscall.ENOSPC), failureServer},
		"a connection reset":        {fmt.Errorf("rewrite x: %w", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), failureServer},
		"a file":                    {fmt.Errorf("read x: %w", &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}), failureUnknown},
		"a request canceled":        {fmt.Errorf("rewrite x: %w", context.Canceled), failureServer},
	} {
		if got := failureOf(ownRefusal(tc.err)); got != tc.want {
			t.Errorf("%s: failureOf(ownRefusal(%v)) = %d, want %d", name, tc.err, got, tc.want)
		}
	}
}

// Real errors of the driver, made without a database: a connection refused
// is the server's, and a closed database/sql handle, whose error is untyped,
// is unknown and never marked a migration's own refusal.
func TestRealDriverErrorsAreReadByTheirKind(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", "postgres://nobody:none@127.0.0.1:1/none?connect_timeout=2&sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	refused := db.PingContext(ctx)
	if refused == nil {
		t.Fatal("a connection to a port nobody listens on was made")
	}
	if got := failureOf(refused); got != failureServer {
		t.Errorf("a connection refused (%v) reads as %d, want the server's", refused, got)
	}
	_ = db.Close()
	_, closed := db.ExecContext(ctx, `SELECT 1`)
	if closed == nil || closed.Error() != "sql: database is closed" {
		t.Fatalf("an exec on a closed handle = %v, want database/sql's own", closed)
	}
	if got := failureOf(closed); got != failureUnknown {
		t.Errorf("a closed handle reads as %d, want unknown", got)
	}
	if got := failureOf(ownRefusal(fmt.Errorf("rewrite x: %w", closed))); got != failureUnknown {
		t.Errorf("a migration that met a closed handle reads as %d, want unknown, unmarked", got)
	}
}
