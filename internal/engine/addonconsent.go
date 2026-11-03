package engine

import (
	"context"
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

// CompleteAppAddon uses the managed write path so a completion is replayable.
func (ds *dataset) CompleteAppAddon(ctx context.Context, tokenID, packageID string) error {
	if _, scoped := substrate.ScopesFrom(ctx); scoped {
		return fmt.Errorf("%w: only the owner completes an add-on approval", substrate.ErrForbidden)
	}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		row, err := t.loadRow(eref{Kind: kindToken, ID: tokenID}, true)
		if err != nil || row == nil || row.DeletedAt != nil {
			return err
		}
		addons, _ := row.Props["appAddons"].([]any)
		kept := make([]any, 0, len(addons))
		for _, value := range addons {
			addon, _ := value.(map[string]any)
			if addon["package"] != packageID {
				kept = append(kept, value)
			}
		}
		if len(kept) == len(addons) {
			return nil
		}
		_, err = t.put(substrate.PutInput{Kind: kindToken, ID: tokenID, Properties: map[string]any{"appAddons": kept}})
		return err
	})
}
