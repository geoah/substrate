package engine

// Repository migration 3: the observed index of rows written before it
// existed.
//
// Migration 0012 creates the observed index's three tables empty, and the
// fold fills them from each write of a kind that binds core's `observed`
// trait (observedindex.go). Rows written before it have no points, while a
// rebuild derives theirs, so the index and the changelog would disagree
// until each row's next write. Which kinds bind the trait is a fact of the
// repository's own declarations, which a SQL migration cannot read, so this
// one does: it reads the stored kind declarations that bind the trait and
// hold a row, and requests the background re-derivation of every row of each
// (reprojection.go), which runs once the open completes and derives each
// row's points under the published declarations.
//
// It writes no record and no changelog entry: the points are an index over
// the rows, like `fts`, and the request is the pass's work list, not the
// fold. Run again over an imported directory, it requests the derivation the
// import's replay already made, which lands at the same rows.

import (
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

func migrateDeriveObservedIndex(t *txn) (string, error) {
	rows, err := t.query(`
		SELECT d.id FROM records d
		WHERE d.kind = $1 AND d.deleted_at IS NULL
		  AND EXISTS (
			SELECT 1 FROM jsonb_array_elements_text(
				CASE WHEN jsonb_typeof(d.props->'traits') = 'array' THEN d.props->'traits' ELSE '[]'::jsonb END) tr
			WHERE tr = $2 OR starts_with(tr, $2 || '('))
		  AND EXISTS (SELECT 1 FROM records r WHERE r.kind = d.id)
		ORDER BY d.id`, kindKind, vocabulary.TraitObservedCore)
	if err != nil {
		return "", err
	}
	var owed []indexReprojection
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			_ = rows.Close()
			return "", err
		}
		owed = append(owed, indexReprojection{kind: kind, observed: indexFilter{moved: true, every: true}})
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	if err := t.requestReprojections(owed); err != nil {
		return "", err
	}
	return fmt.Sprintf("requested the observed index of %d kind(s)", len(owed)), nil
}
