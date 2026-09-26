package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// Scope is the repository every repository-scoped query runs under. It is the
// ONE way into the fold, the changelog and the side stores: a repository's
// handle is opened FROM a scope (service.scopedDB), the scope's id becomes the
// `substrate.repository` setting of every connection the handle hands out, and
// the row level security policies on every repository-scoped table read that
// setting back. So there is no
// repository-scoped query path without a scope — not by discipline in the
// query strings, but because a connection that carries no scope inserts
// nothing (the column default raises) and selects nothing (the policy's
// predicate is NULL).
//
// The scope also keys the per-repository advisory locks (lockKey): write
// serialization is one lock per repository per purpose, never one for the
// whole database.
type Scope struct {
	// Repository is the repository id, the `repositories` row's primary key,
	// which is the repository's authority (`ada.example.com`). A REST path
	// never carries it; the token implies the repository.
	Repository string
}

// Valid reports whether the scope names a repository.
func (s Scope) Valid() bool { return s.Repository != "" }

func (s Scope) String() string { return s.Repository }

// lockKey composes a per-repository advisory-lock key: the same purpose in two
// repositories is two different locks.
func (s Scope) lockKey(name string) string { return s.Repository + "|" + name }

// The two Postgres roles. substrate_app is bound by row level
// security and is what every repository-scoped pool runs as; substrate_maint
// carries BYPASSRLS and is for registration, the repository lookup, seeding
// and rebuild.
//
// The background loops (main.go) take LESS than they are allowed: they
// spend the bypass on ENUMERATING repositories and nothing else, then do every
// repository's work on that repository's own scoped pool. So a GC sweep or a
// trigger pass is bound by the same policy a request is, and there is no
// unscoped write path in the engine at all.
const (
	roleApp   = "substrate_app"
	roleMaint = "substrate_maint"
)

// repositorySetting is the per-connection setting the RLS policies key on.
const repositorySetting = "substrate.repository"

// openScoped opens a pool of its own whose every connection is pinned to one
// repository and one role, at connect time. The engine's repositories share
// one pool instead (service.scopedDB); this shape is for OpenScopedDB, the
// raw seat tests and operator tooling take outside a service.
//
// role may be empty, which leaves the connection as the DSN's own user. That is
// reachable ONLY through WithInsecureAllowSuperuser (a dev/test escape); the
// production path fails closed at Open unless substrate_app exists as a
// NOSUPERUSER NOBYPASSRLS role and the pool is proven to assume it, because a
// superuser DSN would otherwise bypass FORCE ROW LEVEL SECURITY.
func openScoped(dsn string, sc Scope, role string) (*sql.DB, error) {
	if !sc.Valid() {
		return nil, fmt.Errorf("substrate/engine: refusing an unscoped connection pool: %w", errNoScope)
	}
	return openPool(dsn, sc.Repository, role)
}

// RoleApp and RoleMaint name the two Postgres roles for callers outside the
// package — operator tooling and the isolation tests, which have to issue raw
// SQL under the SAME security context the engine runs under or they prove
// nothing.
const (
	RoleApp   = roleApp
	RoleMaint = roleMaint
)

// DefaultRepositoryConnections is how many Postgres connections every
// repository of one process shares when WithRepositoryConnections is not
// given (SUBSTRATE_REPOSITORY_CONNECTIONS). With the admin pool's four and
// the maintenance pool's five and the two budgets below, one process holds at
// most 16 + 4 + 5 + 2 + 1 = 28.
const DefaultRepositoryConnections = 16

// MinRepositoryConnections is the smallest cap Open accepts: a repository
// gets half the cap (repositoryConnsPerHandle), and a repository needs two,
// one for a transaction and one for the transaction it waits on.
const MinRepositoryConnections = 4

// repositoryConnsPerHandle is what one repository may take of a shared pool
// of maxConns: half of it, so two busy repositories cannot hold every
// connection the others need, and never more than eight.
func repositoryConnsPerHandle(maxConns int) int { return min(8, maxConns/2) }

// The connections dialed OUTSIDE the shared pool, each behind a
// process-wide budget of its own so the documented bound holds: at most
// MigrationConnections repository migrations run at once, and at most
// CatchUpConnections commit-time catch-ups. A process therefore holds at
// most SUBSTRATE_REPOSITORY_CONNECTIONS + 4 (admin) + 5 (maintenance) +
// MigrationConnections + CatchUpConnections connections.
//
// The two budgets are separate on purpose. A migration takes its slot
// before it holds any connection, then its own connection, then (0001 and
// 0002, frozen) a shared one. A catch-up already holds a shared connection
// inside its commit when it takes its slot. One budget for both would let
// migrations holding it wait on shared connections that commits hold while
// they wait on it.
const (
	MigrationConnections = 2
	CatchUpConnections   = 1
)

// connBudget is a counting semaphore over connections dialed outside the
// shared pool. A nil budget bounds nothing (a service a test built by hand).
type connBudget chan struct{}

func newConnBudget(n int) connBudget { return make(connBudget, n) }

// take waits for a slot or for ctx, and returns the release.
func (b connBudget) take(ctx context.Context) (func(), error) {
	if b == nil {
		return func() {}, nil
	}
	select {
	case b <- struct{}{}:
		return func() { <-b }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// repositoryConnUnpinTimeout bounds the reset a released connection runs
// before it is idle in the pool again.
const repositoryConnUnpinTimeout = 5 * time.Second

// repositoryConnIdle is how long an unused shared connection stays open, so
// an idle process gives its connections back to the cluster.
const repositoryConnIdle = 5 * time.Minute

// openRepositoryPool opens the ONE pool every repository of the process
// draws its connections from, as the bound role (issue #640: a pool per
// repository that nothing closed grew with every registration until Postgres
// refused every client). Its connections carry the role and nothing else;
// the repository setting is written by scopedConnector each time one is
// handed to a repository, and nothing but scopedConnector acquires from it.
func openRepositoryPool(ctx context.Context, dsn, role string, maxConns int) (*pgxpool.Pool, error) {
	if maxConns < MinRepositoryConnections {
		return nil, fmt.Errorf("substrate/engine: the repository pool's cap is %d: it must be at least %d, so each repository gets two connections", maxConns, MinRepositoryConnections)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: parse dsn: %w", err)
	}
	cfg.MaxConns = int32(min(maxConns, 1<<30)) //nolint:gosec // bounded just above
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = repositoryConnIdle
	// A released connection is unpinned before it is idle, so no idle
	// connection carries the last repository's setting. scopedConnector pins
	// every acquisition anyway; this is the second wall, and it costs one
	// round trip per release, run by pgxpool off the releasing goroutine. A
	// connection whose reset fails is destroyed rather than pooled.
	cfg.AfterRelease = func(c *pgx.Conn) bool {
		ctx, cancel := context.WithTimeout(context.Background(), repositoryConnUnpinTimeout)
		defer cancel()
		_, err := c.Exec(ctx, `SELECT set_config($1, '', false)`, repositorySetting)
		return err == nil
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		if role == "" {
			return nil
		}
		if _, err := c.Exec(ctx, `SET ROLE `+quoteIdent(role)); err != nil {
			return fmt.Errorf("assume role %s: %w", role, err)
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: open the repository pool: %w", err)
	}
	return pool, nil
}

// repositoryPoolCloseWait is how long Close waits for the shared pool's
// acquired connections to come back.
const repositoryPoolCloseWait = 10 * time.Second

// closeRepositoryPool closes the shared pool, waiting at most wait for the
// connections still acquired: pgxpool.Close blocks until every one is
// returned, and a task or a handler that outlived the shutdown would
// otherwise hold the process open. What was still held is logged.
func closeRepositoryPool(pool *pgxpool.Pool, wait time.Duration, log *slog.Logger) {
	done := make(chan struct{})
	go func() {
		pool.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
		st := pool.Stat()
		log.Error("substrate: the repository pool did not close in time; leaving its connections to the process exit",
			"wait", wait, "acquired", st.AcquiredConns(), "total", st.TotalConns())
	}
}

// scopedDB is one repository's handle on the shared pool: a *sql.DB whose
// every connection is pinned to the scope before the handle sees it. It keeps
// NO idle connection of its own, so every checkout is a fresh acquisition and
// a fresh pin, and a connection goes back to the shared pool the moment the
// handle is done with it. Closing the handle closes nothing shared.
func (s *service) scopedDB(sc Scope) (*sql.DB, error) {
	if !sc.Valid() {
		return nil, fmt.Errorf("substrate/engine: refusing an unscoped connection pool: %w", errNoScope)
	}
	db := sql.OpenDB(scopedConnector{inner: stdlib.GetPoolConnector(s.repoPool), repository: sc.Repository})
	db.SetMaxIdleConns(0)
	db.SetMaxOpenConns(s.repoConnsPerHandle)
	return db, nil
}

// scopedConnector acquires from the shared pool and pins the connection to
// one repository. The pin is written on EVERY acquisition, never assumed
// from the connection's last user, and a connection whose pin fails is
// closed rather than handed out or returned: an unpinned connection selects
// nothing, and one still pinned to its last repository would read that
// repository's rows.
type scopedConnector struct {
	inner      driver.Connector
	repository string
}

func (c scopedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	dc, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	conn, ok := dc.(*stdlib.Conn)
	if !ok {
		_ = dc.Close()
		return nil, fmt.Errorf("substrate/engine: the repository pool handed out a %T", dc)
	}
	if _, err := conn.Conn().Exec(ctx, `SELECT set_config($1, $2, false)`, repositorySetting, c.repository); err != nil {
		// Closed first, so the release below destroys it instead of pooling it.
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
		_ = dc.Close()
		return nil, fmt.Errorf("substrate/engine: pin the connection to repository %s: %w", c.repository, err)
	}
	return dc, nil
}

func (c scopedConnector) Driver() driver.Driver { return c.inner.Driver() }

// OpenScopedDB opens a raw pool of its own pinned to one repository and one
// role, at connect time. A dataset rides the shared pool instead
// (service.scopedDB), under the same role and the same setting, so the rows
// this pool sees are the rows a dataset sees. Exported because a test that
// pokes at rows through a superuser connection would bypass row level
// security and measure nothing.
func OpenScopedDB(dsn, repository, role string) (*sql.DB, error) {
	return openScoped(dsn, Scope{Repository: repository}, role)
}

// openMaint opens the BYPASSRLS pool: no repository setting, so an accidental
// repository-scoped INSERT through it raises instead of landing somewhere
// arbitrary, and an accidental SELECT sees everything on purpose.
func openMaint(dsn, role string) (*sql.DB, error) { return openPool(dsn, "", role) }

func openPool(dsn, repository, role string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: parse dsn: %w", err)
	}
	db := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(func(ctx context.Context, c *pgx.Conn) error {
		if repository != "" {
			if _, err := c.Exec(ctx, `SELECT set_config($1, $2, false)`, repositorySetting, repository); err != nil {
				return fmt.Errorf("pin the connection's repository: %w", err)
			}
		}
		if role != "" {
			// SET ROLE last: the setting above must be written by a role that
			// certainly may, and nothing after this point needs more than the
			// bound role has.
			if _, err := c.Exec(ctx, `SET ROLE `+quoteIdent(role)); err != nil {
				return fmt.Errorf("assume role %s: %w", role, err)
			}
		}
		return nil
	}))
	return db, nil
}

// errNoScope is the internal marker for "a repository-scoped path was reached
// without a repository". It should never surface to a caller.
var errNoScope = fmt.Errorf("substrate/engine: no repository scope")
