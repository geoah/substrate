package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// SignOutEverywhere verifies both current factors, as a credential change
// does, and ends every token the person holds (decision 0272): a token a
// leaked one could have minted, whichever door minted it, so the sign-out
// leaves the leak no way back. With newPassword set the password changes
// first, from the same verification, because the code it spends is spent
// once. The two are separate writes: a sign-out that fails after the
// password changed says so, and running it again with the new password
// finishes it. A password write in doubt (decision 0271) may have changed the
// password, so it is incomplete too, and is not followed by the sign-out:
// the error says the password may have changed, never that it did.
func (s *service) SignOutEverywhere(ctx context.Context, in substrate.LoginInput, newPassword string) (int, error) {
	if newPassword != "" {
		if err := validPassword(newPassword); err != nil {
			return 0, err
		}
	}
	repo, material, err := s.verifyFactors(ctx, in)
	if err != nil {
		return 0, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return 0, err
	}
	if newPassword != "" {
		if err := ds.rewritePassword(ctx, in.Repository, material, newPassword); err != nil {
			if errors.Is(err, substrate.ErrWriteInDoubt) {
				return 0, fmt.Errorf("%w: %w", substrate.ErrSignOutIncomplete, err)
			}
			return 0, err
		}
	}
	n, err := ds.endTokens(ctx, substrate.PersonOf(ctx, repo.ID))
	if err != nil && newPassword != "" {
		return 0, fmt.Errorf("%w: %w", substrate.ErrSignOutIncomplete, err)
	}
	return n, err
}

// endTokens deletes every live token the person holds, in one transaction,
// as a revoke deletes one, and answers how many. A repository's tokens are
// all its one person's today; when a token names the person it signs in,
// this is where it selects by person. Each delete folds a token record, so
// the token cache empties before the sign-out answers, and the commit wakes
// every stream, which reads its token again and ends (internal/api).
func (ds *dataset) endTokens(ctx context.Context, person string) (int, error) {
	var ended int
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, kindToken)
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
