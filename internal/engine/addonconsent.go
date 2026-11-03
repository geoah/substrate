package engine

import (
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The repository writer serializes this check with token revocation.
func (t *txn) requireAddonConsent(id string) error {
	row, err := t.loadRow(eref{Kind: kindToken, ID: id}, true)
	if err != nil {
		return err
	}
	if row == nil || row.DeletedAt != nil {
		return fmt.Errorf("%w: the add-on's approving token is no longer live", substrate.ErrForbidden)
	}
	if raw, ok := row.Props["expiresAt"]; ok {
		stamp, _ := raw.(string)
		expires, err := time.Parse(time.RFC3339, stamp)
		if err != nil || !expires.After(time.Now()) {
			return fmt.Errorf("%w: the add-on's approving token has expired", substrate.ErrForbidden)
		}
	}
	return nil
}
