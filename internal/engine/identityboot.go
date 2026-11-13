package engine

// THE BOOT GIVES EVERY REPOSITORY ITS IDENTITY (decision 0274).
//
// A release before identities kept a repository's sign-in as a record of the
// repository, `core/credential` at id `self`, holding two refs into its
// sealed store: the argon2id password hash and the second factor's seed and
// step, sealed under the repository's data key. The boot gives each
// repository that holds such a record and no identity one, before anything is
// served, as step 6's keying gives it an id (repositoryid.go):
//
//   - the identity is named after the repository, which was the name its
//     person signed in with. A name another identity already holds (only a
//     repository restored after a person took it) gets the repository's id in
//     front, `<id>.<name>`, and the boot logs it, because a person must not
//     be let into a repository that is not theirs;
//   - the hash and the seed are opened under the repository's key and sealed
//     again under the identity's own (identities.go), unchanged, so the same
//     password and the same authenticator sign in after the boot;
//   - the identity and the row that lets it open the repository commit in ONE
//     control-plane transaction, so a crash leaves a repository with its
//     identity or without one, and the next boot gives one to whatever is
//     left. A repository with an identity is passed over;
//   - a record whose material does not read or open costs that repository
//     its person and no other's: the boot logs it and goes on, the
//     repository refuses to open until one is given (repomigration_0012),
//     and `user reset` naming the repository gives it one.
//
// The record itself and its sealed rows are the repository's history, so they
// go at the repository's first open, as ordinary writes (repomigration_0012),
// and so does the person each existing token acts for, once the shipped
// upgrade declares it (nameTokensPeople).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// kindCredential is the record that held a repository's sign-in until
// decision 0274: shipped until then, still declared by every repository
// registered before it, written by nothing and purged at its first open.
const (
	kindCredential = "substrate.reamde.dev/core/credential"
	credentialID   = "self"
)

// giveRepositoriesIdentities gives every repository its people before the
// server listens: the index derived from its person actors, which an
// import, a restore, a conversion or the boot check may have brought without
// it (people.go derivePeople), and, for a repository that holds a sign-in
// record and no identity, its identity from that record.
//
// It is a pass of the start, so it goes through the start's one gate
// (setaside.go), one repository at a time: a repository set aside before is
// skipped, its index and its sign-in record left as they are (an identity it
// already has still proves its factors at the door, which then answers that
// the repository is set aside), and one whose pass fails is set aside at
// setAsideAtPeople while the others go on.
func (s *service) giveRepositoriesIdentities(ctx context.Context) error {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: give repositories their identities: %w", err)
	}
	for _, repo := range repos {
		if err := s.through(ctx, rowPass(repo, setAsideAtPeople), func() error {
			return s.giveRepositoryIdentities(ctx, repo)
		}); err != nil {
			return err
		}
	}
	return nil
}

// giveRepositoryIdentities is the pass over one repository.
func (s *service) giveRepositoryIdentities(ctx context.Context, repo Repository) error {
	if s.testPeopleFault != nil {
		if err := s.testPeopleFault(repo.ID); err != nil {
			return err
		}
	}
	if err := s.derivePeople(ctx, repo.ID); err != nil {
		return fmt.Errorf("substrate/engine: give repository %s its people: %w", repo.label(), err)
	}
	people, err := repositoryIdentities(ctx, s.maint, repo.ID)
	if err != nil {
		return fmt.Errorf("substrate/engine: give repository %s its identity: %w", repo.label(), err)
	}
	if len(people) > 0 {
		return nil
	}
	err = s.identityFromCredential(ctx, repo)
	var unread unreadableSignIn
	if errors.As(err, &unread) {
		// The repository refuses to open until a reset names it, which the
		// server takes while it runs: it is not set aside.
		s.log.Error("substrate: a repository's sign-in record does not open: nobody signs in to it until `substratectl user reset` names it",
			"repository", repo.label(), "error", unread.err)
		return nil
	}
	if err != nil {
		return fmt.Errorf("substrate/engine: give repository %s its identity: %w", repo.label(), err)
	}
	return nil
}

// unreadableSignIn is a credential record whose material does not read or
// open, under the repository's key.
type unreadableSignIn struct{ err error }

func (u unreadableSignIn) Error() string { return u.err.Error() }

// identityFromCredential makes repo's identity from its credential record,
// when it holds one.
func (s *service) identityFromCredential(ctx context.Context, repo Repository) error {
	m, ok, err := s.credentialMaterial(ctx, repo)
	if err != nil {
		return unreadableSignIn{err}
	}
	if !ok {
		return nil
	}
	name := repo.Authority
	if _, err := s.identityByName(ctx, s.maint, name); err == nil {
		name = repo.ID + "." + repo.Authority
		s.log.Error("substrate: a repository's sign-in name is another person's: it signs in under another name",
			"repository", repo.label(), "identity", name)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return err
	}
	person, err := s.newIdentityRow(name, m.passwordHash, m.totp)
	if err != nil {
		return err
	}
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertIdentity(ctx, tx, &person); err != nil {
		return err
	}
	if err := linkIdentity(ctx, tx, person.ID, repo.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.log.Info("substrate: a repository's sign-in is an identity now", "repository", repo.label(), "identity", name)
	return nil
}

// credentialMaterial reads a repository's credential record and opens the two
// sealed rows it names under the repository's key, in one snapshot, so a
// first open that adopts a key between two reads cannot split them. A
// repository with no live credential record answers false.
func (s *service) credentialMaterial(ctx context.Context, repo Repository) (authMaterial, bool, error) {
	var m authMaterial
	tx, err := s.maint.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return m, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var props []byte
	err = tx.QueryRowContext(ctx, `
		SELECT props FROM records
		WHERE repository = $1 AND kind = $2 AND id = $3 AND deleted_at IS NULL`,
		repo.ID, kindCredential, credentialID).Scan(&props)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	// hashRef, not a password-named field: the ref is no secret, and a field
	// named for one makes CodeQL treat every error that names it as one.
	var refs struct {
		HashRef string `json:"passwordRef"`
		SeedRef string `json:"totpRef"`
	}
	if err := json.Unmarshal(props, &refs); err != nil {
		return m, false, fmt.Errorf("the credential record does not read: %w", err)
	}
	keys, err := s.repoKeysOn(ctx, tx, repo.ID)
	if err != nil {
		return m, false, err
	}
	open := func(ref, what string) ([]byte, error) {
		var payload []byte
		var owner eref
		if err := tx.QueryRowContext(ctx,
			`SELECT payload, record_kind, record_id FROM sealed WHERE repository = $1 AND ref = $2`,
			repo.ID, ref).Scan(&payload, &owner.Kind, &owner.ID); err != nil {
			return nil, fmt.Errorf("the credential's %s: %w", what, err)
		}
		raw, err := openRepoPayload(payload, keys.dek, sealedAAD(ref, owner.Kind, owner.ID))
		if err != nil {
			return nil, fmt.Errorf("the credential's %s does not open: %w", what, err)
		}
		return raw, nil
	}
	hash, err := open(refs.HashRef, "hash")
	if err != nil {
		return m, false, err
	}
	m.passwordHash = string(hash)
	if refs.SeedRef != "" {
		raw, err := open(refs.SeedRef, "second factor")
		if err != nil {
			return m, false, err
		}
		if err := json.Unmarshal(raw, &m.totp); err != nil {
			return m, false, fmt.Errorf("the credential's second factor does not read: %w", err)
		}
	}
	return m, true, nil
}

// unnamedTokens are the live token records that name no identity.
func (t *txn) unnamedTokens() ([]string, error) {
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL
		AND COALESCE(props->>'identity', '') = '' ORDER BY id`, kindToken)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	return ids, rows.Close()
}
