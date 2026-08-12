package engine

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Scope is the repository every repository-scoped query runs under. It is the
// ONE way into the fold, the changelog and the side stores: a connection pool is
// opened FROM a scope (openScoped), the scope's id becomes the connection's
// `substrate.repository` setting, and the row level security policies on every
// repository-scoped table read that setting back. So there is no
// repository-scoped query path without a scope — not by discipline in the
// query strings, but because a connection that carries no scope inserts
// nothing (the column default raises) and selects nothing (the policy's
// predicate is NULL).
//
// The scope also keys the per-repository advisory locks (lockKey): the write
// serialization that used to be one lock for the whole database is now one
// lock per repository per purpose.
type Scope struct {
	// Repository is the opaque internal repository id — the `repositories`
	// row's primary key. It is never in a URL.
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

// openScoped opens a pool whose every connection is pinned to one repository
// and one role. The pin happens at connect time, not per statement: a
// connection in this pool can never serve another repository, so a pooled
// query that forgets its scope cannot borrow somebody else's.
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

// OpenScopedDB opens a raw pool pinned to one repository and one role — the
// exact pool shape a dataset runs on. Exported because a test that pokes at
// rows through a superuser connection would bypass row level security and
// measure nothing.
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
