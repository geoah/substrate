package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// SignOutEverywhere verifies both current factors, as a credential change
// does, and ends every token the person holds (decision 0272), in every
// repository their identity may open (decision 0274): a token a leaked one
// could have minted, whichever door minted it, so the sign-out leaves the
// leak no way back. With newPassword set the password changes first, from
// the same verification, because the code it spends is spent once. The
// password and the tokens are separate writes: a sign-out that fails after
// the password changed says so, and running it again with the new password
// finishes it. The tokens end under the identity's row, held to the factors
// this request proved or its own change wrote, so a sign-out proved before
// another change of them ends nothing.
func (s *service) SignOutEverywhere(ctx context.Context, in substrate.LoginInput, newPassword string) (int, error) {
	if newPassword != "" {
		if err := validPassword(newPassword); err != nil {
			return 0, err
		}
	}
	m, err := s.verifyFactors(ctx, in)
	if err != nil {
		return 0, err
	}
	s.afterProof("signout")
	proved := m.version
	if newPassword != "" {
		if proved, err = s.rewritePassword(ctx, m, newPassword); err != nil {
			if errors.Is(err, substrate.ErrWriteInDoubt) {
				return 0, signOutIncomplete(newPassword, err)
			}
			return 0, err
		}
	}
	repos, err := s.identityRepositories(ctx, s.maint, m.identity)
	if err != nil {
		return 0, signOutIncomplete(newPassword, err)
	}
	// Every repository opens before the row is held: an open runs the
	// ladder, which must not wait on a sign-in waiting on the row.
	opened := make([]*dataset, 0, len(repos))
	for _, repo := range repos {
		ds, err := s.open(ctx, repo)
		if err != nil {
			return 0, signOutIncomplete(newPassword, err)
		}
		opened = append(opened, ds)
	}
	hold, err := s.holdFactors(ctx, m.identity, proved)
	if err != nil {
		return 0, signOutIncomplete(newPassword, err)
	}
	defer func() { _ = hold.Rollback() }()
	ended := 0
	for _, ds := range opened {
		people, err := repositoryIdentities(ctx, hold, ds.info.ID)
		if err != nil {
			return ended, signOutIncomplete(newPassword, err)
		}
		n, err := ds.endTokens(ctx, m.identity, people)
		ended += n
		if err != nil {
			return ended, signOutIncomplete(newPassword, err)
		}
	}
	return ended, nil
}

// signOutIncomplete marks a failure after the password changed, which the
// door words as such, whatever the cause.
func signOutIncomplete(newPassword string, err error) error {
	if newPassword == "" {
		return err
	}
	return fmt.Errorf("%w: %w", substrate.ErrSignOutIncomplete, err)
}

// endTokens deletes every live token of this repository that acts for the
// identity, and every one that acts for nobody among people, the identities
// that may open the repository, in one transaction, as a revoke deletes one,
// and answers how many. A token
// naming nobody is one the boot could not give a person (identityboot.go),
// and one naming a stranger came back with a restore from a server whose
// people are not this one's: ending them with the repository's sign-out is
// what keeps them from outliving one. Each delete
// folds a token record, so the token cache empties before the sign-out
// answers, and the commit wakes every stream, which reads its token again and
// ends (internal/api).
func (ds *dataset) endTokens(ctx context.Context, person string, people []string) (int, error) {
	// An empty list, never NULL: `= ANY(NULL)` is NULL, which would keep the
	// tokens of a repository nobody may open.
	people = append([]string{}, people...)
	var ended int
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL
			AND (COALESCE(props->>'identity', '') = $2 OR NOT COALESCE(props->>'identity', '') = ANY($3::text[]))
			ORDER BY id`, kindToken, person, people)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := t.softDelete(eref{Kind: kindToken, ID: id}); err != nil {
				return err
			}
		}
		ended = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return ended, nil
}
