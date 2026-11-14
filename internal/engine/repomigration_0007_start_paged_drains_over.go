package engine

// Repository migration 7: every paged drain a function left half way starts
// over.
//
// A paged drain keeps the body's own continuation between pages (the resume
// row of paged_cursors), and a body that pages through the repository's
// records keeps a list token there: the Google provider's Gmail sweep and the
// WHOOP scan do. A list token's keys compare in byte order since migration
// 0021, and one minted before it is refused as list again (keysetVersion 2),
// so such a drain resumed from its saved token would fail on every retry and
// stay parked. The engine cannot tell a list token from any other
// continuation inside a body's JSON, so every drain with a saved
// continuation starts over: its resume cursor becomes null, which
// loadPagedProgress reads as a chain with no position, run again from its
// first page with a fresh budget. A drain over an outside service repeats
// the pages it had done, once; a sweep over the repository's own records
// visits each again, which is what a sweep is for.
//
// It is a write of the delivery ledger, through the fold (pageTx), on a
// delivery entry of the drain's trigger (settleDelivery), as the system, so
// the history holds it and a rebuild replays it; the version
// moves on, so a dispatcher that read the row before cannot advance it from
// the old one. A drain with no saved continuation is left as it is.
//
// THE MARK. The ledger that says this migration ran is the database's, and an
// import or a restore puts the repository into a database whose ledger has
// nothing for it, so the migration runs again there. The drains it then
// finds may have been saved after it first ran, in byte order, and starting
// them over would lose a parked drain's place. What it needs to know is the
// order the repository's saved continuations are of, and that is a fact of
// the repository, so it lives in the repository's history: an annotation on
// the repository's own record (annContinuationOrder), set to
// substrate.IdentityOrder in the same transaction, also when there was
// nothing to start over. A repository this binary creates is born with it
// (createSeededRepository), so its first open writes nothing either. A run
// that finds the mark at that order or above writes nothing. A directory
// written before the mark existed has none, so its drains start over, which
// is right: they were saved in the old order.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// annContinuationOrder is the mark migration 7 leaves on the repository's
// record: the substrate.IdentityOrder its saved continuations are of.
const annContinuationOrder = "substrate/continuation-order"

func migrateStartPagedDrainsOver(t *txn) (string, error) {
	self := eref{Kind: kindRepository, ID: t.ds.info.ID}
	order, err := t.continuationOrder(self)
	if err != nil {
		return "", err
	}
	if order >= substrate.IdentityOrder {
		return fmt.Sprintf("the repository's saved continuations are of order %d already", order), nil
	}
	rows, err := t.query(`
		SELECT chain, trigger_id, kind, identity, version FROM paged_cursors
		WHERE cursor IS NOT NULL AND cursor <> 'null'::jsonb
		ORDER BY chain`)
	if err != nil {
		return "", fmt.Errorf("read the paged drains: %w", err)
	}
	type drain struct {
		chain   string
		owner   pagedOwner
		version int64
	}
	var drains []drain
	for rows.Next() {
		var d drain
		if err := rows.Scan(&d.chain, &d.owner.triggerID, &d.owner.kind, &d.owner.identity, &d.version); err != nil {
			_ = rows.Close()
			return "", err
		}
		drains = append(drains, d)
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, d := range drains {
		if err := t.pageTx(d.owner, d.chain, nil, d.version+1, 0, 0, 0, t.now); err != nil {
			return "", fmt.Errorf("start drain %s over: %w", d.chain, err)
		}
		if err := t.settleDelivery(d.owner.triggerID); err != nil {
			return "", fmt.Errorf("start drain %s over: %w", d.chain, err)
		}
	}
	if _, err := t.putAnnotation(self, annContinuationOrder, substrate.IdentityOrder); err != nil {
		return "", fmt.Errorf("mark the repository's continuations: %w", err)
	}
	if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, self.ID, self.Kind, nil); err != nil {
		return "", fmt.Errorf("mark the repository's continuations: %w", err)
	}
	if len(drains) == 0 {
		return "no paged drain holds a saved continuation", nil
	}
	return fmt.Sprintf("%d paged drains start over from their first page", len(drains)), nil
}

// continuationOrder reads the mark: 0 where the repository's history never set
// it.
func (t *txn) continuationOrder(self eref) (int, error) {
	var raw []byte
	err := t.row(`SELECT value FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3`,
		self.Kind, self.ID, annContinuationOrder).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read the repository's continuation order: %w", err)
	}
	var order float64
	if err := json.Unmarshal(raw, &order); err != nil {
		return 0, fmt.Errorf("the repository's continuation order %s is no number: %w", raw, err)
	}
	return int(order), nil
}
