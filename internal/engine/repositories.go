package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

// The control plane is one table and one row per user: the
// user IS the `repositories` row. There is no control-plane repository, no
// `system` repository and no ledger of repositories kept as records — the ledger
// that used to live inside a repository's own store is this table, read and
// written on the BYPASSRLS maint pool and invisible to substrate_app.

// Repository is one row of the control-plane table.
type Repository struct {
	// ID is the repository's authority (decision 0046): the DNS-style name
	// chosen at registration, unique across the substrate, permanent, and the
	// home of the kinds its user declares. It is the primary key, the scope
	// every repository-scoped query runs under, the DEK wrap's binding and the
	// name of the directory under the data root.
	ID       string
	Username string
	// Authority always equals ID: the column predates the decision to make the
	// authority the id, and a landed migration is never edited, so it stays
	// and migration 0015 holds the two equal.
	Authority string
	CreatedAt time.Time
	// DEK is the repository's data-encryption key, WRAPPED under the host
	// credential key. Nil marks a pre-DEK repository; open adopts one.
	DEK []byte
	// HistoryGeneration names the numbering of this repository's changelog:
	// a change cursor is resumable only under the generation it was read
	// from (decision 0056). It is minted where the row is written, at
	// registration and at import, and nothing else changes it, so a restart
	// and a rebuild keep every cursor while an import resets them once.
	HistoryGeneration string
	// DEKKeyID names the host key DEK is wrapped under (hostKeyID). Empty for
	// a wrap written before the id was recorded or under no key; the first
	// open under a keyed host fills it in (0059).
	DEKKeyID string
	// SealedDEKOnly records that every payload in the sealed store is bound
	// under DEK: no plain and no host-key-sealed payload remains, so a read
	// refuses both (0059). Set at creation and by the re-key at open.
	SealedDEKOnly bool
}

// newHistoryGeneration mints a history generation: a random id in the record
// id alphabet, compared for equality and never a proof, so two imports never
// collide and nothing else is asked of it.
func newHistoryGeneration() (string, error) {
	g, err := newID()
	if err != nil {
		return "", fmt.Errorf("substrate/engine: mint a history generation: %w", err)
	}
	return g, nil
}

// scope is the repository's query scope.
func (r Repository) scope() Scope { return Scope{Repository: r.ID} }

// info renders the repository the way the read surfaces still describe it.
func (r Repository) info() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: r.ID, Name: r.Username, Authority: r.Authority, State: "active"}
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

// controlPlane is the maint-pool handle the creation path runs its
// control-plane statements on: the pool itself, or the one connection the
// registration lock is held on (lockRegistration), which is what lets a
// registration finish without a second maint connection.
type controlPlane interface {
	dbx
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// registrationLockKeySQL is the advisory-lock key for the authority in $1,
// composed the way advisoryKeySQL (identity.go) is: the schema joins it, so
// two substrates sharing a database in separate schemas do not serialize each
// other's registrations, and `register` names the purpose, so it is not one
// of the repository's own locks.
const registrationLockKeySQL = `hashtext(current_schema() || '|register|' || $1)::bigint`

// lockRegistration takes the SESSION-level advisory lock that serializes the
// registrations for one authority, on a maint connection pinned for the
// caller's whole creation. Session, not transaction, because the creation
// spans several transactions in two pools; pinned, because a session lock
// released from another pooled connection is a silent no-op (the dialect
// gate's lesson, dialect.go). The returned connection is where the caller
// runs every control-plane statement, so a registration holds ONE maint
// connection and no number of them can wait on each other for a second. The
// unlock runs on a context of its own, since the creation it ends may have
// failed because the caller's died, and a connection whose unlock failed is
// discarded rather than returned to the pool still holding the lock.
func (s *service) lockRegistration(ctx context.Context, authority string) (*sql.Conn, func(), error) {
	conn, err := s.maint.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("substrate/engine: registration lock for %s: %w", authority, err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(`+registrationLockKeySQL+`)`, authority); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("substrate/engine: registration lock for %s: %w", authority, err)
	}
	unlock := func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(`+registrationLockKeySQL+`)`, authority); err != nil {
			s.log.Error("substrate: could not release the registration lock; discarding its connection",
				"authority", authority, "error", err)
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
	return conn, unlock, nil
}

// insertRepositoryRow writes the control-plane row — THE COMMIT POINT OF A
// CREATION (engine.go createSeededRepository). Everything the repository
// contains is already committed when this runs, so the user exists exactly
// when this row does. The registration lock keeps two registrations for one
// authority apart; the unique index on the username is what a racing
// registration of one username under two authorities loses on, and losing it
// costs the loser nothing but the rows under its own authority, which it
// erases on the way out.
func (s *service) insertRepositoryRow(ctx context.Context, cp controlPlane, r *Repository) error {
	generation, err := newHistoryGeneration()
	if err != nil {
		return err
	}
	r.HistoryGeneration = generation
	err = cp.QueryRowContext(ctx, `
		INSERT INTO repositories (id, username, authority, dek, history_generation, dek_key_id, sealed_dek_only)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`, r.ID, r.Username, r.Authority, r.DEK, r.HistoryGeneration, nullString(r.DEKKeyID), r.SealedDEKOnly).Scan(&r.CreatedAt)
	if err != nil {
		if taken := repositoryRowTaken(err, r); taken != nil {
			return taken
		}
		return fmt.Errorf("substrate/engine: create repository %q: %w", r.Username, err)
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return nil
}

// sqlstateUniqueViolation is Postgres's SQLSTATE for a unique or primary-key
// violation.
const sqlstateUniqueViolation = "23505"

// repositoryRowTaken names a unique violation on the control-plane insert the
// way the early lookups do: the primary key (the authority) and the authority
// index are one refusal, the username index the other. The race that reaches
// here is the one the lookups could not see, and the caller must not learn
// less from it than from the lookup.
func repositoryRowTaken(err error, r *Repository) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != sqlstateUniqueViolation {
		return nil
	}
	switch pgErr.ConstraintName {
	case "repositories_pkey", "repositories_authority_key":
		return errAuthorityTaken(r.Authority)
	case "repositories_username_key":
		return errUsernameTaken(r.Username)
	}
	return nil
}

// eraseFailedCreation is the cleanup of a creation that failed, and it erases
// only what that creation owns. A creation owns its authority's scope once it
// has written the control-plane row (inserted); before that it owns the scope
// only while NO row holds the authority, because a row it did not write means
// another registration holds the authority and every scoped row under it is
// that repository's. The registration lock (lockRegistration) makes the second
// case unreachable, and this is the check that holds if it ever is not, since
// erasing there would delete a live repository's rows, changelog and directory.
// The lookup runs on a context of its own, like the erase, because the
// request behind the failure may be gone.
func (s *service) eraseFailedCreation(ctx context.Context, cp controlPlane, id string, inserted bool) error {
	if !inserted {
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, err := s.repositoryByIDOn(lookupCtx, cp, id)
		if err == nil {
			return fmt.Errorf("substrate/engine: a `repositories` row for %s exists and this creation did not write it; its rows are left alone", id)
		}
		if !errors.Is(err, substrate.ErrNotFound) {
			return fmt.Errorf("substrate/engine: erase repository %s: look up its row: %w", id, err)
		}
	}
	return s.eraseRepositoryOn(ctx, cp, id)
}

// eraseRepository erases a repository whole: every repository-scoped row, then
// the control-plane row if one was ever written. It exists for ONE caller, a
// creation that failed (eraseFailedCreation, which decides whether the
// creation owns what it is about to erase), because "a failed registration
// creates nothing" is a promise, and it must keep that promise even when the
// request that triggered the failure has already gone away.
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
//   - The DIRECTORY GOES FIRST, then the rows. A crash between the two leaves
//     a row with no directory, which the next boot writes out again from the
//     tables (repodir.go, case 5): consistent, and complete. The other order
//     would leave a directory with no row, which the next boot IMPORTS as a
//     repository whose registration was reported failed.
func (s *service) eraseRepository(ctx context.Context, id string) error {
	return s.eraseRepositoryOn(ctx, s.maint, id)
}

// eraseRepositoryOn is eraseRepository on a given control-plane handle: the
// registration's locked connection, so the cleanup needs no second one.
func (s *service) eraseRepositoryOn(ctx context.Context, cp controlPlane, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	// Forget any cached dataset first, so nothing writes into the scope mid-wipe.
	s.mu.Lock()
	if ds, ok := s.datasets[id]; ok {
		delete(s.datasets, id)
		ds.close()
	}
	s.mu.Unlock()

	dir, err := changelogfile.RepoDir(s.dataRoot, id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: remove its directory: %w", id, err)
	}

	tx, err := cp.BeginTx(ctx, nil)
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
	if residue, err := repositoryResidue(ctx, cp, id); err != nil {
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
func repositoryResidue(ctx context.Context, q dbx, id string) (string, error) {
	for _, table := range repositoryScopedTables {
		var n int
		if err := q.QueryRowContext(ctx,
			`SELECT count(*) FROM `+table+` WHERE repository = $1`, id).Scan(&n); err != nil {
			return "", err
		}
		if n > 0 {
			return table, nil
		}
	}
	var n int
	if err := q.QueryRowContext(ctx,
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
// same set the migrations put row level security on (0001, plus
// changelog_dialect in 0009 and import_progress in 0016; chain_epochs came in
// 0005 and left in 0014). A rollback that missed one would leave rows nothing
// can ever reach again.
var repositoryScopedTables = []string{
	"records", "refs", "former_ids", "annotations", "property_managers",
	"property_offers", "changelog", "embeddings", "embed_queue",
	"trigger_cursors", "trigger_failures", "trigger_schedule", "sealed",
	"oauth_flows", "paged_cursors", "blobs", "vocabulary_dialect",
	"vocabulary_promotions", "changelog_dialect", "import_progress",
}

func (s *service) repositoryByUsername(ctx context.Context, username string) (Repository, error) {
	return s.repositoryByUsernameOn(ctx, s.maint, username)
}

func (s *service) repositoryByID(ctx context.Context, id string) (Repository, error) {
	return s.repositoryByIDOn(ctx, s.maint, id)
}

// repositoryByUsernameOn and repositoryByIDOn are the lookups on a given
// maint handle, for the creation path, which runs them on the connection the
// registration lock is held on.
func (s *service) repositoryByUsernameOn(ctx context.Context, q dbx, username string) (Repository, error) {
	return s.scanRepository(q.QueryRowContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories WHERE username = $1`, username), username)
}

func (s *service) repositoryByIDOn(ctx context.Context, q dbx, id string) (Repository, error) {
	return s.scanRepository(q.QueryRowContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories WHERE id = $1`, id), id)
}

// repositoryColumns is the column list scanRepositoryRow reads, in its order.
const repositoryColumns = `id, username, authority, created_at, dek, history_generation, dek_key_id, sealed_dek_only`

// scanRepositoryRow reads one row of repositoryColumns.
func scanRepositoryRow(scan func(dest ...any) error) (Repository, error) {
	var r Repository
	var keyID sql.NullString
	if err := scan(&r.ID, &r.Username, &r.Authority, &r.CreatedAt, &r.DEK, &r.HistoryGeneration, &keyID, &r.SealedDEKOnly); err != nil {
		return Repository{}, err
	}
	r.DEKKeyID = keyID.String
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

// repositoryByAuthority finds the repository that owns an authority, which is
// the repository whose id it is. Two repositories on one substrate cannot own
// the same one: a kind reference names its authority and nothing else, so a
// shared authority would be two homes for one name.
func (s *service) repositoryByAuthority(ctx context.Context, authority string) (Repository, error) {
	return s.repositoryByID(ctx, authority)
}

func (s *service) scanRepository(row *sql.Row, what string) (Repository, error) {
	r, err := scanRepositoryRow(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repository{}, fmt.Errorf("%w: repository %q", substrate.ErrNotFound, what)
		}
		return Repository{}, err
	}
	return r, nil
}

func (s *service) listRepositories(ctx context.Context) ([]Repository, error) {
	rows, err := s.maint.QueryContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Repository
	for rows.Next() {
		r, err := scanRepositoryRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// The bootstrap TOTP credential that used to live in this table — the
// totp_secret/step/fails/locked_until columns and the four methods around
// them — is GONE (B3). A user's factors are the credential record and its
// sealed rows; there is nothing about a user in the control plane but the
// username and the day they arrived.
