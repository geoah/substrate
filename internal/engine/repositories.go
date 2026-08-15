package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The control plane is one table and one row per user: the
// user IS the `repositories` row. There is no control-plane repository, no
// `system` repository and no ledger of repositories kept as records — the ledger
// that used to live inside a repository's own store is this table, read and
// written on the BYPASSRLS maint pool and invisible to substrate_app.

// Repository is one row of the control-plane table.
type Repository struct {
	ID        string
	Username  string
	CreatedAt time.Time
	// DEK is the repository's data-encryption key, WRAPPED under the host
	// credential key. Nil marks a pre-DEK repository; open adopts one.
	DEK []byte
}

// scope is the repository's query scope.
func (r Repository) scope() Scope { return Scope{Repository: r.ID} }

// info renders the repository the way the read surfaces still describe it.
func (r Repository) info() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: r.ID, Name: r.Username, State: "active"}
}

// ensureRoles creates the two Postgres roles the isolation rests on. It is
// idempotent and race-safe: parallel test binaries share one cluster, so a
// duplicate_object is somebody else winning, not a failure.
//
// Creating a BYPASSRLS role needs superuser. If a database refuses, this
// returns an error — but the boot does NOT proceed on a warning: Open calls
// requireRoles next and FAILS CLOSED unless the roles are already present with
// the right attributes (a superuser DSN with no bound roles has no row level
// security at all). The dev/test escape is WithInsecureAllowSuperuser.
func ensureRoles(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`DO $$ BEGIN
			CREATE ROLE ` + roleApp + ` NOLOGIN;
		EXCEPTION WHEN duplicate_object THEN NULL; END $$`,
		`DO $$ BEGIN
			CREATE ROLE ` + roleMaint + ` NOLOGIN BYPASSRLS;
		EXCEPTION WHEN duplicate_object THEN NULL; END $$`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	// The engine's own user must be able to SET ROLE into both. A superuser
	// already can; granting membership is what makes an ordinary owner work.
	// A DO block takes no parameters, so the grantee is quoted here.
	var self string
	if err := db.QueryRowContext(ctx, `SELECT current_user`).Scan(&self); err != nil {
		return err
	}
	for _, role := range []string{roleApp, roleMaint} {
		if _, err := db.ExecContext(ctx,
			`DO $$ BEGIN
				GRANT `+role+` TO `+quoteIdent(self)+`;
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`); err != nil {
			return err
		}
	}
	return nil
}

// roleAttr is a Postgres role's isolation-relevant attributes. A role that
// does not exist has exists=false and the rest zero.
type roleAttr struct {
	exists bool
	super  bool
	bypass bool
}

// roleAttrs reads a role's rolsuper/rolbypassrls. Checking rolname alone (the
// old roleExists) was the hole: a pre-existing substrate_app carrying
// rolbypassrls would sail through and read every repository.
func roleAttrs(ctx context.Context, db *sql.DB, role string) (roleAttr, error) {
	var a roleAttr
	err := db.QueryRowContext(ctx,
		`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = $1`, role).Scan(&a.super, &a.bypass)
	if errors.Is(err, sql.ErrNoRows) {
		return roleAttr{}, nil
	}
	if err != nil {
		return roleAttr{}, err
	}
	a.exists = true
	return a, nil
}

// requireRoles is the fail-closed gate: both bound roles must exist with the
// EXACT attributes the isolation depends on, or Open refuses (engine.go).
func requireRoles(ctx context.Context, db *sql.DB) error {
	app, err := roleAttrs(ctx, db, roleApp)
	if err != nil {
		return err
	}
	maint, err := roleAttrs(ctx, db, roleMaint)
	if err != nil {
		return err
	}
	return validateRoleAttrs(app, maint)
}

// validateRoleAttrs is the pure predicate: substrate_app must be present and
// NEITHER superuser NOR bypassrls (it is what every scoped pool runs as, so it
// MUST be bound by the policies); substrate_maint must be present, NOT a
// superuser, and bypassrls (it legitimately reads across repositories — KI-7 —
// but must not be a superuser). A violation is a refusal to boot.
func validateRoleAttrs(app, maint roleAttr) error {
	switch {
	case !app.exists:
		return fmt.Errorf("substrate/engine: role %s is missing — the scoped pools would fall back to the DSN user, and a superuser DSN bypasses row level security; refusing to boot", roleApp)
	case app.super || app.bypass:
		return fmt.Errorf("substrate/engine: role %s must be NOSUPERUSER NOBYPASSRLS (has superuser=%v bypassrls=%v) — it would bypass the isolation it is meant to be bound by", roleApp, app.super, app.bypass)
	case !maint.exists:
		return fmt.Errorf("substrate/engine: role %s is missing; refusing to boot", roleMaint)
	case maint.super:
		return fmt.Errorf("substrate/engine: role %s must not be a SUPERUSER", roleMaint)
	case !maint.bypass:
		return fmt.Errorf("substrate/engine: role %s must carry BYPASSRLS (has bypassrls=%v)", roleMaint, maint.bypass)
	}
	return nil
}

// assertPoolPrincipal confirms a pool's connections RUN as wantRole, are NOT
// superusers and carry the expected bypass attribute — the runtime half of the
// guarantee. Role attributes say what COULD be assumed; current_user says what
// IS, and a superuser DSN that never assumed a role is exactly what this
// catches.
func assertPoolPrincipal(ctx context.Context, db *sql.DB, wantRole string, wantBypass bool) error {
	var user, isSuper string
	var bypass bool
	if err := db.QueryRowContext(ctx, `
		SELECT current_user,
		       current_setting('is_superuser'),
		       (SELECT rolbypassrls FROM pg_roles WHERE rolname = current_user)`).
		Scan(&user, &isSuper, &bypass); err != nil {
		return err
	}
	if user != wantRole {
		return fmt.Errorf("effective principal is %q, want %q", user, wantRole)
	}
	if isSuper == "on" {
		return fmt.Errorf("effective principal %q is a SUPERUSER — it bypasses row level security", user)
	}
	if bypass != wantBypass {
		return fmt.Errorf("effective principal %q has bypassrls=%v, want %v", user, bypass, wantBypass)
	}
	return nil
}

// assertAppPoolPrincipal opens a throwaway scoped pool — the exact shape every
// request rides — and asserts its principal is substrate_app, not a superuser
// and not bypassing RLS. The scoped pools open lazily per repository, so this
// is the boot-time proof that the one they will run as is safe.
func (s *service) assertAppPoolPrincipal(ctx context.Context) error {
	probe, err := openPool(s.dsn, "_principal_probe", s.appRole)
	if err != nil {
		return err
	}
	defer func() { _ = probe.Close() }()
	if err := assertPoolPrincipal(ctx, probe, roleApp, false); err != nil {
		return fmt.Errorf("substrate/engine: application pool principal: %w", err)
	}
	return nil
}

// insertRepositoryRow writes the control-plane row — THE COMMIT POINT OF A
// CREATION (engine.go createSeededRepository). Everything the repository
// contains is already committed when this runs, so the user exists exactly
// when this row does: the unique index on the username is what a racing
// registration loses on, and losing it costs the loser nothing but the rows
// it erases on the way out.
func (s *service) insertRepositoryRow(ctx context.Context, r *Repository) error {
	err := s.maint.QueryRowContext(ctx, `
		INSERT INTO repositories (id, username, dek) VALUES ($1, $2, $3)
		RETURNING created_at`, r.ID, r.Username, r.DEK).Scan(&r.CreatedAt)
	if err != nil {
		return fmt.Errorf("substrate/engine: create repository %q: %w", r.Username, err)
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return nil
}

// eraseRepository erases a repository whole: every repository-scoped row, then
// the control-plane row if one was ever written. It exists for ONE caller — a
// creation that failed — because "a failed registration creates nothing" is a
// promise, and it must keep that promise even when the request that triggered
// the failure has already gone away.
//
//   - It runs on a context DECOUPLED from the caller's (context.WithoutCancel
//     plus a bounded budget): a registration is frequently failing BECAUSE the
//     client canceled, and the cleanup must not be canceled with it.
//   - Every delete is ONE transaction, so a partial wipe cannot itself leave
//     orphans, and it runs on the maintenance pool with an explicit repository
//     predicate, since no scope survives the failure.
//   - It VERIFIES the scope is empty afterwards — scoped tables AND the
//     control-plane row — and returns an error if anything survived, so a caller
//     can changelog it and the boot sweeper can reclaim what a crash left.
func (s *service) eraseRepository(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	// Forget any cached dataset first, so nothing writes into the scope mid-wipe.
	s.mu.Lock()
	if ds, ok := s.datasets[id]; ok {
		delete(s.datasets, id)
		ds.close()
	}
	s.mu.Unlock()

	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range repositoryScopedTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE repository = $1`, id); err != nil {
			return fmt.Errorf("substrate/engine: erase repository %s (%s): %w", id, table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE id = $1`, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s (repositories): %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: commit: %w", id, err)
	}
	if residue, err := s.repositoryResidue(ctx, id); err != nil {
		return err
	} else if residue != "" {
		return fmt.Errorf("substrate/engine: erase repository %s left rows in %s", id, residue)
	}
	return nil
}

// repositoryResidue names the first table still holding a row for id, or "" if
// the erase was clean. It is the verification eraseRepository owes: a promise
// that "a failed registration creates nothing" that nobody checks is a promise
// that quietly breaks.
func (s *service) repositoryResidue(ctx context.Context, id string) (string, error) {
	for _, table := range repositoryScopedTables {
		var n int
		if err := s.maint.QueryRowContext(ctx,
			`SELECT count(*) FROM `+table+` WHERE repository = $1`, id).Scan(&n); err != nil {
			return "", err
		}
		if n > 0 {
			return table, nil
		}
	}
	var n int
	if err := s.maint.QueryRowContext(ctx,
		`SELECT count(*) FROM repositories WHERE id = $1`, id).Scan(&n); err != nil {
		return "", err
	}
	if n > 0 {
		return "repositories", nil
	}
	return "", nil
}

// sweepOrphans erases repository-scoped rows whose repository has NO
// control-plane row — the durable residue a registration that CRASHED between
// its scoped commit and its control-plane insert leaves behind. Such rows are
// unreachable: no login, token or listing can ever name a repository with no
// `repositories` row, so they are pure garbage the ordering's crash window
// cannot otherwise reclaim. It runs ONCE at boot, on the maintenance pool, in
// one transaction.
func (s *service) sweepOrphans(ctx context.Context) error {
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("substrate/engine: sweep orphans: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var swept int64
	for _, table := range repositoryScopedTables {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM `+table+` WHERE repository NOT IN (SELECT id FROM repositories)`)
		if err != nil {
			return fmt.Errorf("substrate/engine: sweep orphans (%s): %w", table, err)
		}
		if n, err := res.RowsAffected(); err == nil {
			swept += n
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("substrate/engine: sweep orphans: commit: %w", err)
	}
	if swept > 0 {
		s.log.Warn("substrate: swept rows of repositories with no control-plane row (a crashed registration)", "rows", swept)
	}
	return nil
}

// repositoryScopedTables is every table carrying a `repository` column — the
// same set the migrations put row level security on (0001, plus chain_epochs
// in 0005). A rollback that missed one would leave rows nothing can ever
// reach again.
var repositoryScopedTables = []string{
	"records", "edges", "former_ids", "annotations", "property_managers",
	"property_offers", "changelog", "chain_epochs", "embeddings", "embed_queue",
	"trigger_cursors", "trigger_failures", "trigger_schedule", "sealed",
	"oauth_flows", "paged_cursors", "blobs", "vocabulary_dialect",
	"vocabulary_promotions",
}

func (s *service) repositoryByUsername(ctx context.Context, username string) (Repository, error) {
	return s.scanRepository(s.maint.QueryRowContext(ctx,
		`SELECT id, username, created_at, dek FROM repositories WHERE username = $1`, username), username)
}

func (s *service) repositoryByID(ctx context.Context, id string) (Repository, error) {
	return s.scanRepository(s.maint.QueryRowContext(ctx,
		`SELECT id, username, created_at, dek FROM repositories WHERE id = $1`, id), id)
}

func (s *service) scanRepository(row *sql.Row, what string) (Repository, error) {
	var r Repository
	if err := row.Scan(&r.ID, &r.Username, &r.CreatedAt, &r.DEK); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repository{}, fmt.Errorf("%w: repository %q", substrate.ErrNotFound, what)
		}
		return Repository{}, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

func (s *service) listRepositories(ctx context.Context) ([]Repository, error) {
	rows, err := s.maint.QueryContext(ctx,
		`SELECT id, username, created_at FROM repositories ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Repository
	for rows.Next() {
		var r Repository
		if err := rows.Scan(&r.ID, &r.Username, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = r.CreatedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// The bootstrap TOTP credential that used to live in this table — the
// totp_secret/step/fails/locked_until columns and the four methods around
// them — is GONE (B3). A user's factors are the credential record and its
// sealed rows; there is nothing about a user in the control plane but the
// username and the day they arrived.
