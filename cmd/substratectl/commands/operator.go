package commands

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	// The engine's Postgres driver. substratectl's operator hat opens the same
	// database the server does, so it registers the same `pgx` driver name —
	// once, because a Go package initializes once however many times it is
	// imported.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/config"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE OPERATOR HAT — on the box, through the database, never over HTTP.
//
// There is no admin user; the operator acts through `substratectl` on the box.
// So these commands take a DSN rather than a token: nothing reachable from the
// network resets an account or rebuilds a fold, and there is no privileged
// HTTP surface for a leaked token to find.
//
// THE REGISTRY IS DELIBERATELY EMPTY. The engine opens with the shipped
// vocabulary tree so that CREATING a repository seeds it and OPENING one
// appends whatever a newer binary added. substratectl does neither: it opens with an
// empty registry, which makes the boot-time upgrade a no-op by construction
// (engine/seed.go: no shipped authorities, nothing to write). An operator
// command must not quietly re-vocabulary somebody's repository because substratectl
// was built from a different commit than the server — the server's own boot is
// where that belongs. Every repository still reads its own kinds from its own
// rows, which is where they live.

// dsnEnv names the environment the server reads its database from
// (internal/config: DATABASE_URL). substratectl reads the same variable so that an
// operator on the box needs no second configuration.
const dsnEnv = "DATABASE_URL"

// credentialKeyEnv seals the sealed store. `user reset` WRITES sealed material,
// so substratectl must hold the same key the server runs with or the new rows land
// unsealed — readable, but not protected.
const credentialKeyEnv = "SUBSTRATE_CREDENTIAL_KEY"

// dsn resolves the operator database URL. Absent, every operator command
// refuses before it touches anything.
func (a *app) dsn() (string, error) {
	if a.dsnFlag != "" {
		return a.dsnFlag, nil
	}
	if v := firstEnv(dsnEnv, "SUBSTRATE_DATABASE_URL"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no database URL: pass --dsn or set %s — the operator commands act on the box's Postgres directly, never over HTTP", dsnEnv)
}

// openEngineReadOnly opens the engine BESIDE a running server: no boot check,
// no changelog writer, every write refused (engine.WithDirectoryReadOnly). It
// is what `repository verify` and `reembed` ride, because both must be safe
// against a live server and neither appends to a repository. Whatever
// credential key the environment holds is used; its absence is not fatal
// here, since nothing sealed is written.
func (a *app) openEngineReadOnly(ctx context.Context) (substrate.Service, error) {
	return a.openEngineWithKey(ctx, os.Getenv(credentialKeyEnv), true)
}

// openEngineExclusive opens the engine the way the server does, as the data
// root's one writer: the boot check runs and a dataset opens a changelog
// writer. It is for a command that rewrites a repository (rebuild) and it
// REFUSES beside a running server, because the server's writer holds the
// repository's lock (engine.ErrChangelogLocked). The credential key is
// optional: a rebuild re-links sealed material and never opens it.
func (a *app) openEngineExclusive(ctx context.Context) (substrate.Service, error) {
	return a.openEngineWithKey(ctx, os.Getenv(credentialKeyEnv), false)
}

// openEngineWrite opens the substrate engine for a command that WRITES sealed
// material (user reset). The credential key is REQUIRED, with no escape: a
// keyless engine would store the password hash and the TOTP seed under a
// plain-marked DEK wrap the server later accepts. The refusal is here, before
// a password is typed, rather than at the write. Like openEngineExclusive it
// needs the server stopped.
func (a *app) openEngineWrite(ctx context.Context) (substrate.Service, error) {
	credKey := os.Getenv(credentialKeyEnv)
	if credKey == "" {
		return nil, fmt.Errorf(
			"refusing to reset: set %s to the key the server runs with. The reset writes sealed material, and without the key it would land under a plain-marked wrap",
			credentialKeyEnv)
	}
	return a.openEngineWithKey(ctx, credKey, false)
}

// openEngineWithKey is the shared opener the three hats route through.
func (a *app) openEngineWithKey(ctx context.Context, credKey string, readOnly bool) (substrate.Service, error) {
	// A present key must be key material of the right shape, or the engine
	// refuses to open under it. Report that here, before the DSN, rather than
	// wrapped in an open error. An absent key is the read hat's keyless case
	// (rebuild), left to the engine.
	if credKey != "" {
		if err := config.ValidateCredentialKey(credKey); err != nil {
			return nil, err
		}
	}
	dsn, err := a.dsn()
	if err != nil {
		return nil, err
	}
	// The data root the server runs with, from the same variable: every
	// repository's files live under it, and the engine refuses to open
	// without one.
	data, err := config.LoadData()
	if err != nil {
		return nil, err
	}
	// The engine logs its boot at info; an operator command's output is its
	// own report, so only warnings and worse reach stderr.
	log := slog.New(slog.NewTextHandler(a.errOut, &slog.HandlerOptions{Level: slog.LevelWarn}))
	opts := []engine.Option{
		engine.WithRegistry(vocabulary.NewRegistry()),
		engine.WithDataRoot(data.Root),
		engine.WithChangelogSegmentBytes(data.ChangelogSegmentBytes),
		engine.WithCredentialKey(credKey),
		engine.WithLogger(log),
	}
	if readOnly {
		opts = append(opts, engine.WithDirectoryReadOnly())
	}
	svc, err := engine.Open(ctx, dsn, opts...)
	if err != nil {
		return nil, lockHint(fmt.Errorf("open the substrate database: %w", err))
	}
	return svc, nil
}

// lockHint says what a writer-lock refusal means to the person at the
// terminal: the server is running, and this command needs it stopped, while
// the read-only commands do not.
func lockHint(err error) error {
	if err == nil || !errors.Is(err, changelogfile.ErrLocked) {
		return err
	}
	return fmt.Errorf("%w\n(a server is running against this data root: `repository inspect` and `repository verify` run beside it; `repository rebuild` and `user reset` need it stopped first)", err)
}

// controlPlane opens a plain connection for the ONE control-plane table
// (`repositories`). It carries no repository setting and needs none: the table
// is outside row level security by design — it is the ledger that says which
// repositories exist.
func (a *app) controlPlane() (*sql.DB, error) {
	dsn, err := a.dsn()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open the substrate database: %w", err)
	}
	return db, nil
}

// repositoryRow is the control plane, whole: the user IS this row.
type repositoryRow struct {
	ID        string
	Username  string
	Authority string
	CreatedAt time.Time
}

func listRepositoryRows(ctx context.Context, db *sql.DB) ([]repositoryRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, authority, created_at FROM repositories ORDER BY created_at, id`)
	if err != nil {
		return nil, controlPlaneError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []repositoryRow
	for rows.Next() {
		var r repositoryRow
		if err := rows.Scan(&r.ID, &r.Username, &r.Authority, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = r.CreatedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func repositoryRowByUsername(ctx context.Context, db *sql.DB, username string) (repositoryRow, error) {
	var r repositoryRow
	err := db.QueryRowContext(ctx,
		`SELECT id, username, authority, created_at FROM repositories WHERE username = $1`, username).
		Scan(&r.ID, &r.Username, &r.Authority, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return repositoryRow{}, fmt.Errorf("no user %q on this substrate", username)
	}
	if err != nil {
		return repositoryRow{}, controlPlaneError(err)
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

// controlPlaneError names the one failure an operator will actually hit: a DSN
// pointing at a database no substrate has ever opened.
func controlPlaneError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w\n(if this database has never run a substrate, there is no control-plane table to read)", err)
}

// openScoped opens a pool pinned to one repository, exactly as a dataset's is:
// the connection carries the repository setting the row level security
// policies key on, and it ASSUMES substrate_app — because FORCE ROW LEVEL
// SECURITY does not bind a SUPERUSER, and an operator's DSN is very often
// exactly that. Without the role, `repository inspect` on a superuser DSN would
// silently count every repository's rows and report them as one user's.
//
// So this REFUSES rather than silently summing: it requires the role and
// confirms the connection actually assumes it (and is not a superuser slipping
// past). An isolated read that cannot be isolated is an error, not a warning.
func (a *app) openScoped(ctx context.Context, repository string) (*sql.DB, error) {
	dsn, err := a.dsn()
	if err != nil {
		return nil, err
	}
	scoped, err := engine.OpenScopedDB(dsn, repository, engine.RoleApp)
	if err != nil {
		return nil, err
	}
	if err := assertScopedAppPrincipal(ctx, scoped); err != nil {
		_ = scoped.Close()
		return nil, fmt.Errorf(
			"cannot assume the %s role for an isolated read (%w)\ninspect must not sum every repository as the DSN's own user — ensure the substrate roles exist and this DSN may SET ROLE %s",
			engine.RoleApp, err, engine.RoleApp)
	}
	return scoped, nil
}

// assertScopedAppPrincipal confirms the scoped connection runs as substrate_app
// and is not a superuser, so the row level security policies actually bind it.
func assertScopedAppPrincipal(ctx context.Context, db *sql.DB) error {
	var user, isSuper string
	if err := db.QueryRowContext(ctx,
		`SELECT current_user, current_setting('is_superuser')`).Scan(&user, &isSuper); err != nil {
		return err
	}
	if user != engine.RoleApp {
		return fmt.Errorf("effective principal is %q, not %s", user, engine.RoleApp)
	}
	if isSuper == "on" {
		return fmt.Errorf("effective principal %q is a superuser", user)
	}
	return nil
}

// The operator hat's seams are engine.Resetter, engine.Rebuilder and
// engine.Verifier. All are deliberately OFF substrate.Service — nothing reachable from the network
// should be able to call them — so substratectl asks for them by shape, and
// the engine asserts each against *service at compile time.

// seamMissing is what an engine build without one of the operator seams gets:
// a named refusal rather than a nil dereference.
func seamMissing(what string) error {
	return fmt.Errorf("this build's engine does not implement %s", what)
}
