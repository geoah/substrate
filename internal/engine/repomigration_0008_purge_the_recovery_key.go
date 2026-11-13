package engine

// Repository migration 8: the record that held the recovery wrap goes
// (decision 0264).
//
// A repository registered before 0264 holds one `core/recoverykey` record,
// id `self`, which conversion step 2 already emptied of its wrap. Nothing
// reads it any more, so it is purged as the system, the way a delete with
// `purge` collects a record: tombstoned, then collected. The kind stays
// declared and empty, because the boot upgrade never prunes a declaration
// and a retired name a repository still declares would refuse every later
// upgrade of core (shippedupgrade.go heldRetirementGuards). A repository with
// no such record, and a second run, write nothing.

import (
	"database/sql"
	"errors"

	"github.com/geoah/substrate/internal/substrate"
)

// kindRecoveryKey is the kind that held the recovery wrap: shipped until
// decision 0264, still declared by every repository registered before it,
// and written by nothing.
const (
	kindRecoveryKey = "substrate.reamde.dev/core/recoverykey"
	recoveryKeyID   = "self"
)

func migratePurgeTheRecoveryKey(t *txn) (string, error) {
	var deleted bool
	err := t.row(`SELECT deleted_at IS NOT NULL FROM records WHERE kind = $1 AND id = $2`,
		kindRecoveryKey, recoveryKeyID).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return "no core/recoverykey record", nil
	}
	if err != nil {
		return "", err
	}
	ref := eref{Kind: kindRecoveryKey, ID: recoveryKeyID}
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
	return "purged core/recoverykey/self", nil
}
