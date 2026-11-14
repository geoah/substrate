package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The control plane is one table and one row per user: the
// user IS the `repositories` row. There is no control-plane repository, no
// `system` repository and no ledger of repositories kept as records: the
// ledger is this table, read and written on the BYPASSRLS maint pool and
// invisible to substrate_app.

// Repository is one row of the control-plane table.
type Repository struct {
	// ID is the repository's key (decision 0261): minted at creation
	// (vocabulary.NewRepositoryID), never shown as a name and never changed.
	// It is the primary key, the scope every repository-scoped query runs
	// under, the DEK wrap's binding and the name of the directory under the
	// data root. A row a release before ids wrote holds the name here until
	// the boot gives it an id (repositoryid.go).
	ID string
	// Authority is the repository's name: the DNS-style name chosen at
	// registration, unique across the substrate, what its user signs in with,
	// and the home of the kinds its user declares (decision 0046).
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
	// DEKKeyID names the host key DEK is wrapped under (hostKeyID). Empty
	// under no key (0059).
	DEKKeyID string
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

// label names the repository in a refusal an operator reads: by the name they
// know it by, and by the id its directory, its rows and its writer lease are
// keyed by, which is what they look it up under.
func (r Repository) label() string {
	if r.Authority == "" || r.Authority == r.ID {
		return r.ID
	}
	return r.Authority + " (id " + r.ID + ")"
}

// info renders the repository the way the read surfaces still describe it.
func (r Repository) info() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: r.ID, Authority: r.Authority, State: "active"}
}

// registry names reg as this repository's, so its lookups take the
// repository's name as another spelling of `~` (decision record 0259).
func (r Repository) registry(reg *vocabulary.Registry) *vocabulary.Registry {
	reg.SetHome(r.Authority)
	return reg
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

// assertAppPoolPrincipal takes a throwaway scoped handle on the shared
// repository pool (the exact shape every request rides) and asserts its
// principal is substrate_app, not a superuser and not bypassing RLS. This is
// the boot-time proof that what every repository runs as is safe.
func (s *service) assertAppPoolPrincipal(ctx context.Context) error {
	probe, err := s.scopedDB(Scope{Repository: "_principal_probe"})
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
// discarded rather than returned to the pool still holding the lock. The
// second registrant polls for the lock (lockAdvisory) rather than blocking
// on it: the creation holding it imports a sample, the import may build an
// index, and that concurrent build waits out the snapshot a blocked
// registrant would keep for as long as it waits.
func (s *service) lockRegistration(ctx context.Context, authority string) (*sql.Conn, func(), error) {
	conn, err := s.maint.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("substrate/engine: registration lock for %s: %w", authority, err)
	}
	if err := lockAdvisory(ctx, conn, registrationLockKeySQL, []any{authority}, nil); err != nil {
		// A round trip that failed may have been granted the lock first.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
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
// authority apart, and a racing registration that gets past it loses on the
// primary key instead; losing costs the loser nothing but the rows under its
// own authority, which it erases on the way out.
//
// A creation for a person (registration) inserts their identity and the row
// that lets it open the repository in the same transaction, so a registration
// that loses its name to a racing one creates no person either. The row and
// the end of any lapsed hold another repository kept on the name commit as
// one (takeFormerName), and the repository that held it is answered ("" for
// none).
func (s *service) insertRepositoryRow(ctx context.Context, cp controlPlane, r *Repository, person *identityRow) (string, error) {
	generation, err := newHistoryGeneration()
	if err != nil {
		return "", err
	}
	r.HistoryGeneration = generation
	tx, err := cp.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("substrate/engine: create repository %q: %w", r.ID, err)
	}
	defer func() { _ = tx.Rollback() }()
	took, err := takeFormerName(ctx, tx, r.Authority)
	if err != nil {
		return "", fmt.Errorf("substrate/engine: create repository %q: %w", r.ID, err)
	}
	// The repository that held the name as a lapsed former name answers to it
	// no more: its names are fenced around this COMMIT (namescache.go).
	var fenced []string
	if took != "" {
		if err := lockNames(ctx, tx, took); err != nil {
			return "", fmt.Errorf("substrate/engine: create repository %q: %w", r.ID, err)
		}
		fenced = append(fenced, took)
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO repositories (id, authority, dek, history_generation, dek_key_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at`, r.ID, r.Authority, r.DEK, r.HistoryGeneration, nullString(r.DEKKeyID)).Scan(&r.CreatedAt)
	if err != nil {
		if taken := repositoryRowTaken(err, r); taken != nil {
			return "", taken
		}
		return "", fmt.Errorf("substrate/engine: create repository %q: %w", r.ID, err)
	}
	if person != nil {
		if err := insertIdentity(ctx, tx, person); err != nil {
			return "", err
		}
		if err := linkIdentity(ctx, tx, person.ID, r.ID); err != nil {
			return "", err
		}
	}
	if err := s.commitFencingNames(tx, false, fenced...); err != nil {
		return "", fmt.Errorf("substrate/engine: create repository %q: %w", r.ID, err)
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return took, nil
}

// sqlstateUniqueViolation is Postgres's SQLSTATE for a unique or primary-key
// violation.
const sqlstateUniqueViolation = "23505"

// repositoryRowTaken names a unique violation on the control-plane insert the
// way the early lookup does: the primary key (the authority) and the authority
// index are the same refusal. The race that reaches here is the one the lookup
// could not see, and the caller must not learn less from it than from the
// lookup.
func repositoryRowTaken(err error, r *Repository) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != sqlstateUniqueViolation {
		return nil
	}
	switch pgErr.ConstraintName {
	case "repositories_pkey", "repositories_authority_key":
		return errAuthorityTaken(r.Authority)
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
// request behind the failure may be gone. person is the identity the
// creation inserted with its row, if any: it goes in the erase's own
// transaction, so no crash leaves its name held by no repository.
func (s *service) eraseFailedCreation(ctx context.Context, cp controlPlane, id string, inserted bool, person string) error {
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
	return s.eraseRepositoryOn(ctx, cp, id, eraseFor{person: person})
}

// ErrRepositoryBeingDeleted is the refusal of a repository whose deletion
// began and did not finish (decision 0279, rule 7): the server finishes it
// at its next start, once the archive answers.
var ErrRepositoryBeingDeleted = fmt.Errorf("%w: the repository's deletion began; the server finishes it at its next start", substrate.ErrNotFound)

// The deletion's durable steps, as its test seam names them
// (testEraseFault).
const (
	eraseAfterBegan         = "after the row said the deletion began"
	eraseAfterMark          = "after the archive's deletion mark was written"
	eraseAfterArchiveObject = "after one of the archive's objects was deleted"
	eraseAfterArchive       = "after the archive was deleted"
	eraseAfterDirectory     = "after the directory was removed"
)

func (s *service) eraseStage(stage string) error {
	if s.testEraseFault == nil {
		return nil
	}
	return s.testEraseFault(stage)
}

// eraseRepository erases a repository whole: every repository-scoped row, then
// the control-plane row if one was ever written. It exists for two callers: a
// creation that failed (eraseFailedCreation, which decides whether the
// creation owns what it is about to erase), because "a failed registration
// creates nothing" is a promise, and it must keep that promise even when the
// request that triggered the failure has already gone away; and an owner
// deleting their repository (auth.go DeleteRepository), which holds the
// registration lock and the open slot around it.
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
//   - The ARCHIVE AND THE DIRECTORY GO FIRST, then the rows. A crash between
//     the two leaves a row with neither, whose next archive pass writes the
//     archive again from the tables. The other order would leave an archive
//     with no row, which the next boot RESTORES as a repository that was
//     deleted, or whose registration was reported failed.
func (s *service) eraseRepository(ctx context.Context, id string) error {
	return s.eraseRepositoryOn(ctx, s.maint, id, eraseFor{})
}

// eraseFor names whom an erase is for. person's place in the repository,
// and their identity when the repository was its last, go in the
// transaction that records the deletion began, so no crash leaves a name
// held by a repository being deleted. proved is the version of the factors a
// person's delete proved: that transaction holds the identity's row to it
// before it records anything (factorsStandOn).
type eraseFor struct {
	person string
	proved *time.Time
}

// eraseRepositoryOn is eraseRepository on a given control-plane handle: the
// registration's locked connection, so the cleanup needs no second one.
func (s *service) eraseRepositoryOn(ctx context.Context, cp controlPlane, id string, by eraseFor) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	// Forget any cached dataset first, so nothing writes into the scope mid-wipe.
	s.mu.Lock()
	if ds, ok := s.datasets[id]; ok {
		delete(s.datasets, id)
		ds.close()
	}
	s.mu.Unlock()

	// The row says the deletion began before anything is removed: from here
	// no archive pass runs on it, and a boot that finds the row finishes the
	// deletion (rule 7). A person's fence runs in the transaction that
	// records it, so a change of the factors either committed first, and the
	// fence refuses with the repository whole, or waits for that commit and
	// meets a deletion already begun, which nothing undoes.
	tx, err := cp.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	// The locks in the order a sign-in takes them, the person's row before
	// the repository's changelog lock (holdFactors, then the mint), so the
	// two cannot wait on each other. The changelog lock is what proves a
	// COMMIT below whose answer is lost has ended (commitFencingTokens).
	switch {
	case by.proved != nil:
		if err := factorsStandOn(ctx, tx, by.person, *by.proved); err != nil {
			return err
		}
	case by.person != "":
		if _, err := tx.ExecContext(ctx, `SELECT 1 FROM identities WHERE id = $1 FOR UPDATE`, by.person); err != nil {
			return fmt.Errorf("substrate/engine: erase repository %s: %w", id, err)
		}
	}
	if err := lockTokens(ctx, tx, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE repositories SET deletion_began_at = coalesce(deletion_began_at, now()) WHERE id = $1`, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: record that its deletion began: %w", id, err)
	}
	// The deletion ends any take of the repository: no boot replaces it
	// from a copy staged before.
	if _, err := tx.ExecContext(ctx, `DELETE FROM archive_takes WHERE repository_id = $1`, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: end its take: %w", id, err)
	}
	if by.person != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM identity_repositories WHERE repository_id = $1 AND identity = $2`, id, by.person); err != nil {
			return fmt.Errorf("substrate/engine: erase repository %s (its person's place): %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM identities i WHERE i.id = $1
			AND NOT EXISTS (SELECT 1 FROM identity_repositories l WHERE l.identity = i.id)`, by.person); err != nil {
			return fmt.Errorf("substrate/engine: erase repository %s (its person's identity): %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: record that its deletion began: %w", id, err)
	}
	if err := s.eraseStage(eraseAfterBegan); err != nil {
		return err
	}
	if err := s.eraseArchive(ctx, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: delete its archive: %w", id, err)
	}
	if err := s.eraseStage(eraseAfterArchive); err != nil {
		return err
	}
	if err := s.eraseLocal(ctx, cp, id); err != nil {
		return err
	}
	// Named by no take now; a boot removes it too.
	_ = os.RemoveAll(filepath.Join(s.dataRoot, takeRootDir, id))
	return nil
}

// eraseLocal removes what the server holds of a repository: its directory,
// then every row, in one transaction. A deletion runs it after the archive's
// part; a take of the archive runs it alone, the archive left whole.
func (s *service) eraseLocal(ctx context.Context, cp controlPlane, id string) error {
	dir, err := changelogfile.RepoDir(s.dataRoot, id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: remove its directory: %w", id, err)
	}
	if err := s.eraseStage(eraseAfterDirectory); err != nil {
		return err
	}

	tx, err := cp.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	// The repository's tokens go in this transaction: the changelog lock
	// first, so the token cache can prove it ended (commitFencingTokens).
	if err := lockTokens(ctx, tx, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: %w", id, err)
	}
	for _, table := range repositoryScopedTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE repository = $1`, id); err != nil {
			return fmt.Errorf("substrate/engine: erase repository %s (%s): %w", id, table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM identity_repositories WHERE repository_id = $1`, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s (identity_repositories): %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE id = $1`, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s (repositories): %w", id, err)
	}
	// The repository's tokens, its people's places in it and its names go
	// with it, all fenced around the one COMMIT (tokencache.go,
	// namescache.go).
	if err := lockNames(ctx, tx, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: %w", id, err)
	}
	if err := s.commitErasing(tx, id); err != nil {
		return fmt.Errorf("substrate/engine: erase repository %s: commit: %w", id, err)
	}
	if s.testAfterErase != nil {
		s.testAfterErase(id)
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
// same set 0001_init puts row level security on. A rollback that missed one
// would leave rows nothing can ever reach again.
var repositoryScopedTables = []string{
	"records", "refs", "former_ids", "annotations", "property_marks",
	"property_offers", "changelog", "embeddings", "embed_queue",
	"trigger_cursors", "trigger_failures", "trigger_schedule", "sealed",
	"oauth_flows", "paged_cursors", "vocabulary_dialect", "repository_migrations",
	"changelog_dialect", "import_progress",
	"idempotency_keys", "search_index", "index_reprojections",
	"observed_streams", "observed_segments", "observed_rollups", "observed_latest", "observed_index",
	"unit_graph", "unit_uses", "unit_uses_index",
	"operator_spend_reservations", "archive_records", "archive_objects",
}

func (s *service) repositoryByID(ctx context.Context, id string) (Repository, error) {
	return s.repositoryByIDOn(ctx, s.maint, id)
}

// repositoryByIDOn is the lookup on a given maint handle, for the creation
// path, which runs it on the connection the registration lock is held on.
func (s *service) repositoryByIDOn(ctx context.Context, q dbx, id string) (Repository, error) {
	return s.scanRepository(q.QueryRowContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories WHERE id = $1`, id), id)
}

// repositoryColumns is the column list scanRepositoryRow reads, in its order.
const repositoryColumns = `id, authority, created_at, dek, history_generation, dek_key_id`

// scanRepositoryRow reads one row of repositoryColumns.
func scanRepositoryRow(scan func(dest ...any) error) (Repository, error) {
	var r Repository
	var keyID sql.NullString
	if err := scan(&r.ID, &r.Authority, &r.CreatedAt, &r.DEK, &r.HistoryGeneration, &keyID); err != nil {
		return Repository{}, err
	}
	r.DEKKeyID = keyID.String
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

// repositoryByKey finds a repository by its id or by its name, which never
// look alike: an id holds no dot and a name always does. It is the lookup of
// every door a caller names a repository at (an operator command, a webhook
// address, a dataset a caller asks for).
func (s *service) repositoryByKey(ctx context.Context, key string) (Repository, error) {
	if vocabulary.ValidRepositoryID(key) {
		return s.repositoryByID(ctx, key)
	}
	return s.repositoryByName(ctx, key)
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

// A person's factors are their identity's (identities.go), never a column in
// this table: a repository row holds nothing about the people who open it.
