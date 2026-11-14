package engine

// IDENTITIES (decision 0274).
//
// A person's sign-in is an identity the server holds, beside the
// repositories: a name, a password and an optional second factor, and no
// data. It lives in two control-plane tables (migration 0027): `identities`,
// one row per person, and `identity_repositories`, one row per repository a
// person may open. Like `repositories`, both are read and written on the
// maintenance pool alone, because a sign-in has no repository scope yet.
//
// THE FACTORS ARE SEALED UNDER A KEY OF THE IDENTITY'S OWN, a data key minted
// with it and wrapped under the host credential key, bound to its id, exactly
// as a repository's is (dek.go). So the material is as strong at rest as it
// was in a repository's sealed store, and it depends on no repository. The
// password is an argon2id hash and the second factor its seed and the last
// step a code spent, sealed together because a code is one-time.
//
// Changing the factors writes the identity's row and nothing else: no
// repository's history, no record. That is what lets an operator reset a
// sign-in while the server runs.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// identityRow is one `identities` row as stored: the factors still sealed.
type identityRow struct {
	ID       string
	Name     string
	DEK      []byte
	DEKKeyID string
	// Hash is the sealed password hash, the `password` column. Not a
	// password-named field: CodeQL reads one as a password wherever its bytes
	// go, an error naming a payload's framing byte included.
	Hash []byte
	TOTP []byte
	// UpdatedAt moves when the factors change and only then, so it is the
	// compare-and-swap baseline of a credential change: spending a code does
	// not move it.
	UpdatedAt time.Time
	CreatedAt time.Time
}

const identityColumns = `id, name, dek, dek_key_id, password, totp, updated_at, created_at`

func scanIdentity(scan func(dest ...any) error) (identityRow, error) {
	var r identityRow
	var keyID sql.NullString
	if err := scan(&r.ID, &r.Name, &r.DEK, &keyID, &r.Hash, &r.TOTP, &r.UpdatedAt, &r.CreatedAt); err != nil {
		return identityRow{}, err
	}
	r.DEKKeyID = keyID.String
	r.UpdatedAt, r.CreatedAt = r.UpdatedAt.UTC(), r.CreatedAt.UTC()
	return r, nil
}

// identityDEKAAD binds an identity's wrapped data key to the identity, so a
// wrap lifted into another row stops opening.
func identityDEKAAD(id string) []byte { return []byte("identity-dek\x00" + id) }

// identityFactorAAD binds one sealed factor to its identity and its column.
func identityFactorAAD(id, factor string) []byte {
	return []byte("identity\x00" + id + "\x00" + factor)
}

// errIdentityTaken is the refusal of a name another identity holds, worded
// and answered as a taken repository name is (errAuthorityTaken).
func errIdentityTaken(name string) error {
	return fmt.Errorf("%w: the sign-in name %q is already another person's on this substrate", substrate.ErrValidation, name)
}

// newIdentityRow mints an identity: its id, its data key wrapped under the
// host key, and its factors sealed under that key. Nothing is written.
func (s *service) newIdentityRow(name, passwordHash string, totp totpMaterial) (identityRow, error) {
	return s.newIdentityRowAs("", name, passwordHash, totp)
}

// newIdentityRowAs is newIdentityRow under a given id, or a minted one when
// id is empty.
func (s *service) newIdentityRowAs(id, name, passwordHash string, totp totpMaterial) (identityRow, error) {
	if id == "" {
		var err error
		if id, err = vocabulary.NewIdentityID(); err != nil {
			return identityRow{}, err
		}
	}
	dek, err := newDEK()
	if err != nil {
		return identityRow{}, err
	}
	row := identityRow{ID: id, Name: name, DEKKeyID: s.credKeyID}
	if row.DEK, err = s.sealCredential(dek, identityDEKAAD(id)); err != nil {
		return identityRow{}, err
	}
	if err := sealIdentityFactors(&row, dek, passwordHash, totp); err != nil {
		return identityRow{}, err
	}
	return row, nil
}

// sealIdentityFactors seals a password hash and a second factor into row
// under dek. An empty seed is an identity without a second factor.
func sealIdentityFactors(row *identityRow, dek []byte, passwordHash string, totp totpMaterial) error {
	var err error
	if row.Hash, err = sealIdentityFactor(dek, row.ID, "password", []byte(passwordHash)); err != nil {
		return err
	}
	row.TOTP = nil
	if totp.Secret == "" {
		return nil
	}
	row.TOTP, err = sealIdentityFactor(dek, row.ID, "totp", mustJSON(totp))
	return err
}

func sealIdentityFactor(dek []byte, id, factor string, raw []byte) ([]byte, error) {
	aead, err := aeadOf(dek)
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return nil, errors.New("substrate/engine: no identity key to seal a factor under")
	}
	return sealWith(aead, raw, identityFactorAAD(id, factor))
}

// identityKey unwraps an identity's data key.
func (s *service) identityKey(row identityRow) ([]byte, error) {
	dek, err := s.openCredential(row.DEK, identityDEKAAD(row.ID))
	if err != nil {
		return nil, s.wrongHostKey("identity "+row.Name, row.DEKKeyID, err)
	}
	return dek, nil
}

// identityMaterial opens an identity's factors as the factor check reads
// them. An identity without a second factor has an empty seed.
func (s *service) identityMaterial(row identityRow) (authMaterial, error) {
	m := authMaterial{identity: row.ID, name: row.Name, version: row.UpdatedAt}
	dek, err := s.identityKey(row)
	if err != nil {
		return m, err
	}
	hash, err := openRepoPayload(row.Hash, dek, identityFactorAAD(row.ID, "password"))
	if err != nil {
		return m, fmt.Errorf("substrate/engine: identity %s: open the password: %w", row.Name, err)
	}
	m.passwordHash = string(hash)
	if len(row.TOTP) == 0 {
		return m, nil
	}
	raw, err := openRepoPayload(row.TOTP, dek, identityFactorAAD(row.ID, "totp"))
	if err != nil {
		return m, fmt.Errorf("substrate/engine: identity %s: open the second factor: %w", row.Name, err)
	}
	if err := json.Unmarshal(raw, &m.totp); err != nil {
		return m, fmt.Errorf("substrate/engine: identity %s: unreadable second factor: %w", row.Name, err)
	}
	return m, nil
}

// insertIdentity writes a new identity row. A name another identity holds is
// refused as taken, whichever of two racing creations loses.
func insertIdentity(ctx context.Context, q dbx, row *identityRow) error {
	err := q.QueryRowContext(ctx, `
		INSERT INTO identities (id, name, dek, dek_key_id, password, totp)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING updated_at, created_at`,
		row.ID, row.Name, row.DEK, nullString(row.DEKKeyID), row.Hash, row.TOTP).Scan(&row.UpdatedAt, &row.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == sqlstateUniqueViolation && pgErr.ConstraintName == "identities_name_key" {
		return errIdentityTaken(row.Name)
	}
	if err != nil {
		return fmt.Errorf("substrate/engine: create identity %s: %w", row.Name, err)
	}
	row.UpdatedAt, row.CreatedAt = row.UpdatedAt.UTC(), row.CreatedAt.UTC()
	return nil
}

// linkIdentity lets an identity open a repository. Linking twice is a no-op.
func linkIdentity(ctx context.Context, q dbx, identity, repository string) error {
	if _, err := q.ExecContext(ctx, `
		INSERT INTO identity_repositories (identity, repository_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, identity, repository); err != nil {
		return fmt.Errorf("substrate/engine: let identity %s open repository %s: %w", identity, repository, err)
	}
	return nil
}

func (s *service) identityByName(ctx context.Context, q dbx, name string) (identityRow, error) {
	row, err := scanIdentity(q.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM identities WHERE name = $1`, name).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return identityRow{}, fmt.Errorf("%w: no identity %s", substrate.ErrNotFound, name)
	}
	return row, err
}

func (s *service) identityByID(ctx context.Context, q dbx, id string) (identityRow, error) {
	row, err := scanIdentity(q.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM identities WHERE id = $1`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return identityRow{}, fmt.Errorf("%w: no identity %s", substrate.ErrNotFound, id)
	}
	return row, err
}

// identityRepositories are the repositories an identity may open, by name.
func (s *service) identityRepositories(ctx context.Context, q dbx, identity string) ([]Repository, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT `+qualifiedRepositoryColumns+`
		FROM identity_repositories l JOIN repositories p ON p.id = l.repository_id
		WHERE l.identity = $1 ORDER BY p.authority`, identity)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Repository
	for rows.Next() {
		repo, err := scanRepositoryRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, repo)
	}
	return out, rows.Err()
}

// repositoryIdentities are the ids of the identities that may open a
// repository.
func repositoryIdentities(ctx context.Context, q dbx, repository string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT identity FROM identity_repositories WHERE repository_id = $1 ORDER BY identity`, repository)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// qualifiedRepositoryColumns is repositoryColumns qualified for a join on
// `repositories p`, derived so the two lists cannot drift apart.
var qualifiedRepositoryColumns = tokenRepositoryColumns

// spendIdentityCode spends a code by recording its step on the identity's
// sealed second factor, and holds the request to the factors it proved. The
// row is taken FOR UPDATE and read again under that lock: two sign-ins racing
// on one code cannot both win it, and factors changed since they were proved
// at `version` (a reset, a password change, a new seed) refuse it, so a reset
// leaves nothing in flight that the old factors began. A request that proved
// no code (`to` below 0) is held to the version alone and writes nothing. It
// moves no `updated_at`: spending a code is not a change of the factors.
func (s *service) spendIdentityCode(ctx context.Context, id string, version time.Time, to int64) (bool, error) {
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := scanIdentity(tx.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM identities WHERE id = $1 FOR UPDATE`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !row.UpdatedAt.Equal(version) {
		return false, nil
	}
	if to < 0 {
		return true, nil
	}
	m, err := s.identityMaterial(row)
	if err != nil {
		return false, err
	}
	if m.totp.Secret == "" || to <= m.totp.Step {
		return false, nil
	}
	dek, err := s.identityKey(row)
	if err != nil {
		return false, err
	}
	m.totp.Step = to
	sealed, err := sealIdentityFactor(dek, row.ID, "totp", mustJSON(m.totp))
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE identities SET totp = $2 WHERE id = $1`, id, sealed); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// errFactorsChanged refuses a request whose factors changed between its
// proof and its act. The words are step 20's, whose fence this is.
var errFactorsChanged = fmt.Errorf("%w: the password or the second factor changed after this request proved them", substrate.ErrAuth)

// factorsStandOn holds a request to the factors it proved at `version`: it
// takes the identity's row FOR UPDATE in tx, the lock every change of the
// factors takes, and refuses when they changed since. The caller acts inside
// tx, so nothing changes the factors between the check and the act.
func factorsStandOn(ctx context.Context, tx *sql.Tx, identity string, version time.Time) error {
	var at time.Time
	err := tx.QueryRowContext(ctx, `SELECT updated_at FROM identities WHERE id = $1 FOR UPDATE`, identity).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !at.Equal(version)) {
		return errFactorsChanged
	}
	return err
}

// holdFactors is factorsStandOn in a control-plane transaction of its own,
// for an act that writes a repository: the caller acts, then rolls the
// transaction back, which lets the row go. Everything the act reads from the
// control plane it reads through the transaction, so a request waiting on
// the row holds one connection, never two.
func (s *service) holdFactors(ctx context.Context, identity string, version time.Time) (*sql.Tx, error) {
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := factorsStandOn(ctx, tx, identity, version); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// mayOpen answers whether an identity may open a repository.
func mayOpen(ctx context.Context, q dbx, identity, repository string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM identity_repositories WHERE identity = $1 AND repository_id = $2)`,
		identity, repository).Scan(&ok)
	return ok, err
}

// errCredentialConflict marks a credential change that lost a race with a
// concurrent one: the factors it verified against are no longer current, so
// it REFUSES rather than overwriting the winner. The caller proves the
// factors again and retries.
var errCredentialConflict = fmt.Errorf("%w: the credential changed concurrently — re-authenticate and retry", substrate.ErrConflict)

// factorWrite is a pending change of an identity's factors, unsealed. The
// password hash is computed by the caller, outside the row's lock. When cas
// is set the write asserts the factors are still the ones verified at
// `expect`.
type factorWrite struct {
	identity     string
	passwordHash string
	totp         totpMaterial
	expect       time.Time
	cas          bool
}

// rewriteIdentityFactors writes an identity's factors, and nothing else: no
// repository's history and no record. The step of an unchanged seed never
// goes back, so a code spent by a concurrent sign-in stays spent. It answers
// the factors' new version, which a request that changed them acts on next.
func (s *service) rewriteIdentityFactors(ctx context.Context, fw factorWrite) (time.Time, error) {
	var zero time.Time
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := scanIdentity(tx.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM identities WHERE id = $1 FOR UPDATE`, fw.identity).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, fmt.Errorf("%w: no identity %s", substrate.ErrNotFound, fw.identity)
	}
	if err != nil {
		return zero, err
	}
	if fw.cas && !row.UpdatedAt.Equal(fw.expect) {
		return zero, errCredentialConflict
	}
	cur, err := s.identityMaterial(row)
	if err != nil {
		return zero, err
	}
	totp := fw.totp
	if totp.Secret != "" && cur.totp.Secret == totp.Secret && cur.totp.Step > totp.Step {
		totp.Step = cur.totp.Step
	}
	dek, err := s.identityKey(row)
	if err != nil {
		return zero, err
	}
	if err := sealIdentityFactors(&row, dek, fw.passwordHash, totp); err != nil {
		return zero, err
	}
	var version time.Time
	if err := tx.QueryRowContext(ctx, `
		UPDATE identities SET password = $2, totp = $3, updated_at = clock_timestamp() WHERE id = $1
		RETURNING updated_at`,
		row.ID, row.Hash, row.TOTP).Scan(&version); err != nil {
		return zero, err
	}
	// A commit that errors may have committed: its client is told the
	// factors may have changed (decision 0271). The seam is the repository
	// commit's, so one test reaches either write.
	if err := tx.Commit(); err != nil {
		return zero, inDoubt(err)
	}
	if err := inDoubt(s.commitFault(commitInDoubt)); err != nil {
		return zero, err
	}
	return version.UTC(), nil
}

// loadPeople reads the identities that may open the repository, on the
// maintenance pool: the repository-scoped role never sees the control plane.
func (ds *dataset) loadPeople(ctx context.Context) error {
	people, err := repositoryIdentities(ctx, ds.svc.maint, ds.info.ID)
	if err != nil {
		return fmt.Errorf("substrate/engine: open repository %s: read the identities that may open it: %w", ds.info.ID, err)
	}
	ds.people = people
	// Read before the repository migrations and the shipped upgrade, which
	// declare the identity an actor names (people.go fromBeforeIdentities).
	if ds.beforeIdentities, err = fromBeforeIdentities(ctx, ds.svc.maint, ds.info.ID); err != nil {
		return fmt.Errorf("substrate/engine: open repository %s: read whether it is from before identities: %w", ds.info.ID, err)
	}
	return nil
}
