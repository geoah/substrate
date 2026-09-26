package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// reindexSearch re-derives every row's `fts` when the rows were indexed under
// rules older than this binary's (searchIndexVersion), then records the
// version. It runs at open, after the stored vocabulary loads and the shipped
// upgrade lands, because the bands read each kind's declaration and only the
// loaded registry has every one.
//
// It moves nothing but `fts`, through the same reprojectFTS a kind edit uses:
// no version, no updated_at, no changelog entry, because no record changed,
// only the index over it. A read-only process skips it and serves the index
// it finds; the next writer brings it up to date.
//
// A newer version than the binary's is left alone: the newer rules indexed
// variants these rules do not, which costs this binary nothing to read.
func (ds *dataset) reindexSearch(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	var have int
	err := ds.db.QueryRowContext(ctx, `SELECT version FROM search_index`).Scan(&have)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		have = 1
	case err != nil:
		return fmt.Errorf("substrate/engine: repository %s: read the search index version: %w", ds.info.ID, err)
	}
	if have >= searchIndexVersion {
		return nil
	}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.lockKey(registryDepKey(ds)); err != nil {
			return err
		}
		rows, err := t.query(`SELECT DISTINCT kind FROM records`)
		if err != nil {
			return err
		}
		var kinds []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				_ = rows.Close()
				return err
			}
			kinds = append(kinds, k)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		if err := t.reprojectFTS(ds.registry(), kinds); err != nil {
			return err
		}
		return t.markSearchIndexed()
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: repository %s: re-derive the search index: %w", ds.info.ID, err)
	}
	ds.svc.log.Info("substrate: re-derived the search index",
		"repository", logSafeID(ds.info.ID), "from", have, "to", searchIndexVersion)
	return nil
}

// markSearchIndexed records that every row is indexed under this binary's
// rules.
func (t *txn) markSearchIndexed() error {
	_, err := t.exec(`
		INSERT INTO search_index (version) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET version = EXCLUDED.version, indexed_at = now()`,
		searchIndexVersion)
	return err
}
