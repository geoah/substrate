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
// the old one. A drain with no saved continuation, and a second run, write
// nothing.

import "fmt"

func migrateStartPagedDrainsOver(t *txn) (string, error) {
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
	if len(drains) == 0 {
		return "no paged drain holds a saved continuation", nil
	}
	return fmt.Sprintf("%d paged drains start over from their first page", len(drains)), nil
}
