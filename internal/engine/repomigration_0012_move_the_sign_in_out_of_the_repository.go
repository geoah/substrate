package engine

// Repository migration 12: the sign-in leaves the repository (decision 0274).
//
// The boot gave a repository registered before identities its identity, from
// its `core/credential` record (identityboot.go). What is left of the sign-in
// in the repository is that record, purged here with its sealed rows, the way
// a delete with `purge` collects a record. Its lines stay in the history:
// they hold two refs and never the material.
//
// The kind stays declared and empty, because the boot upgrade never prunes a
// declaration. A repository with no credential record, and a second run,
// write nothing. One whose record the boot could not turn into an identity
// (its material did not open, or a process skipped the boot) refuses to open
// until one is given, because purging the record would lose the only copy of
// its sign-in. The person each existing token
// acts for is written after the shipped upgrade declares it
// (nameTokensPeople).

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

func migrateMoveTheSignInOutOfTheRepository(t *txn) (string, error) {
	var deleted bool
	err := t.row(`SELECT deleted_at IS NOT NULL FROM records WHERE kind = $1 AND id = $2`,
		kindCredential, credentialID).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return "no core/credential record", nil
	}
	if err != nil {
		return "", err
	}
	if !deleted && len(t.ds.people) == 0 {
		return "", fmt.Errorf("%w: repository %s holds its sign-in as a record nobody may open with yet: `substratectl user reset` naming the repository gives it a person",
			substrate.ErrUnavailable, t.ds.info.ID)
	}
	ref := eref{Kind: kindCredential, ID: credentialID}
	if !deleted {
		if err := t.migrateTombstone(ref); err != nil {
			return "", err
		}
	}
	if err := t.hardDelete(ref); err != nil {
		return "", err
	}
	if err := t.appendChange(substrate.ActorSystem, substrate.OpGC, ref.ID, ref.Kind,
		map[string]any{"reason": "purged"}); err != nil {
		return "", err
	}
	return "purged core/credential/self", nil
}
