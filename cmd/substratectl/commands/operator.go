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

// openEngineRead opens the substrate engine with whatever credential key the
// environment holds; its absence is not fatal HERE. Rebuild re-links existing
// sealed material and never opens it, and reseal enforces its own key
// requirement in the engine (a keyless reseal writes nothing but markers, so
// it refuses loudly there). The caller closes it.
func (a *app) openEngineRead(ctx context.Context) (substrate.Service, error) {
	return a.openEngineWithKey(ctx, os.Getenv(credentialKeyEnv))
}

// openEngineWrite opens the substrate engine for a command that WRITES sealed
// material (user reset). A valid credential key is REQUIRED: without it the
// server would store the password hash and TOTP seed as a plain-marked payload
// it later accepts — persistent silent plaintext of the account's factors — so
// the command REFUSES rather than defaulting an omitted production secret into
// plaintext. `allowUnsealed` is the explicit dev/test escape: it stores plain,
// loudly, and is never the default.
func (a *app) openEngineWrite(ctx context.Context, allowUnsealed bool) (substrate.Service, error) {
	credKey := os.Getenv(credentialKeyEnv)
	if credKey == "" && !allowUnsealed {
		return nil, fmt.Errorf(
			"refusing to write unsealed credential material: set %s to the key the server runs with (or pass --allow-unsealed for a dev database)",
			credentialKeyEnv)
	}
	if credKey == "" {
		fmt.Fprintf(a.errOut,
			"warning: %s is unset and --allow-unsealed was given — the new sealed material is stored in PLAINTEXT\n",
			credentialKeyEnv)
	}
	return a.openEngineWithKey(ctx, credKey)
}

// openEngineWithKey is the shared opener the read/write hats route through.
func (a *app) openEngineWithKey(ctx context.Context, credKey string) (substrate.Service, error) {
	dsn, err := a.dsn()
	if err != nil {
		return nil, err
	}
	// The engine logs its boot at info; an operator command's output is its
	// own report, so only warnings and worse reach stderr.
	log := slog.New(slog.NewTextHandler(a.errOut, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc, err := engine.Open(ctx, dsn,
		engine.WithRegistry(vocabulary.NewRegistry()),
		engine.WithCredentialKey(credKey),
		engine.WithLogger(log))
	if err != nil {
		return nil, fmt.Errorf("open the substrate database: %w", err)
	}
	return svc, nil
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
	CreatedAt time.Time
}

func listRepositoryRows(ctx context.Context, db *sql.DB) ([]repositoryRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, created_at FROM repositories ORDER BY created_at, id`)
	if err != nil {
		return nil, controlPlaneError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []repositoryRow
	for rows.Next() {
		var r repositoryRow
		if err := rows.Scan(&r.ID, &r.Username, &r.CreatedAt); err != nil {
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
		`SELECT id, username, created_at FROM repositories WHERE username = $1`, username).
		Scan(&r.ID, &r.Username, &r.CreatedAt)
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

// resetter and rebuilder are the two engine seams the operator hat needs. Both
// are deliberately OFF substrate.Service — nothing reachable from the network
// should be able to call them — so substratectl asks for them by shape.
type resetter interface {
	ResetUser(ctx context.Context, username, newPassword string) (substrate.TOTPEnrollment, error)
}

type rebuilder interface {
	RebuildRepository(ctx context.Context, username string) (engine.RebuildReport, error)
}

// forceRebuilder is the escape hatch's OWN seam: rebuilding from history the
// chain refuses is a distinct act, so it is a distinct method rather than a
// flag the ordinary path could trip over.
type forceRebuilder interface {
	RebuildRepositoryUnverified(ctx context.Context, username string) (engine.RebuildReport, error)
}

type resealer interface {
	ResealRepository(ctx context.Context, username string) (engine.ResealReport, error)
}

type verifier interface {
	VerifyRepository(ctx context.Context, username string) (engine.VerifyReport, error)
}

// seamMissing is what an engine build without one of the operator seams gets:
// a named refusal rather than a nil dereference.
func seamMissing(what string) error {
	return fmt.Errorf("this build's engine does not implement %s", what)
}
