package engine

// A READ OF THE CHANGES BY A POSITION CARRIES THE HORIZON. A reader that
// holds a position in the changelog (a stream's cursor, a forward list's
// `from`, the tail's window) reads the rows after it together with the
// repository's horizon, in one snapshot, and a batch whose position is below
// that horizon is refused, never served: the forgetting that raised it
// (compactpass.go forgetDeletions) removed a deletion past the position, and
// a reader moving past it would never hear of the deletion, its copy keeping
// the record for good. The horizon and the history move together, in the
// forgetting's one transaction, so one snapshot reads them as they stood
// together; a horizon read before the rows would let a forgetting land
// between the two. The refusal is the list-again answer
// (substrate.ErrStaleHistory): the API ends a stream with the `compacted`
// frame, and answers a forward list 410, both naming the head.
//
// TestEveryReadOfTheChangesByAPositionCarriesTheHorizon holds every read of
// the history by `seq >` in this package to this, or to a floor of its own.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// heldBelow reports whether a reader holds a position below the horizon.
// Seq 0 holds none: a reader from the start of the history keeps no copy,
// and a forgotten record left no entry for it to read.
func heldBelow(after, horizon int64) bool { return after > 0 && after < horizon }

// belowHorizon is the refusal of a read after a position below the horizon.
func belowHorizon(after, horizon int64) error {
	return fmt.Errorf("%w: seq %d is below the repository's horizon %d: a deletion after it was forgotten", substrate.ErrStaleHistory, after, horizon)
}

// changesAbove is the read of the changes after a position a reader holds:
// the builder's rows in seq order, refused below the horizon of the snapshot
// they would be read from.
func (ds *dataset) changesAbove(ctx context.Context, b *builder, after int64, limit int, values bool) ([]substrate.Change, error) {
	out, horizon, err := ds.queryChangesWithHorizon(ctx, b, after, `seq`, limit, values)
	if err != nil {
		return nil, err
	}
	if heldBelow(after, horizon) {
		return nil, belowHorizon(after, horizon)
	}
	return out, nil
}

// queryChangesWithHorizon reads the horizon and then the builder's rows in
// one REPEATABLE READ snapshot. A position held below the horizon reads no
// rows: after is the caller's, to refuse. The values, derived from records,
// are read after the snapshot.
func (ds *dataset) queryChangesWithHorizon(ctx context.Context, b *builder, after int64, order string, limit int, values bool) ([]substrate.Change, int64, error) {
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var horizon int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT horizon FROM compaction_state), 0)`).Scan(&horizon); err != nil {
		return nil, 0, err
	}
	if heldBelow(after, horizon) {
		return nil, horizon, nil
	}
	rows, err := changeRows(ctx, tx, b, order, limit)
	if err != nil {
		return nil, 0, err
	}
	out, effects, err := collectChangeEffects(rows, values)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	if values {
		if err := ds.deriveValues(ctx, out, effects); err != nil {
			return nil, 0, err
		}
	}
	return out, horizon, nil
}

// changeRows runs one changelog page over the builder's predicates on q.
func changeRows(ctx context.Context, q dbx, b *builder, order string, limit int) (*sql.Rows, error) {
	if limit <= 0 {
		limit = 100
	}
	return q.QueryContext(ctx, `
		SELECT `+changeColumns+` FROM changelog
		WHERE `+strings.Join(b.where, " AND ")+`
		ORDER BY `+order+` LIMIT `+b.arg(limit), b.args...)
}
