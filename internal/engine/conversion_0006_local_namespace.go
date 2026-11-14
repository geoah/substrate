package engine

// Conversion step 6: a repository's own kinds move to the local namespace
// (decision 0259).
//
// History written before the local namespace held a repository's own
// packages under its name: `ada.example.com/tasks/task` in a line's kind, an
// effect's ref, a reference value, a declaration and a token's scopes. The
// step moves each to `~` by the rule the write path applies to a write
// (localnamespace.go): localizeLine for the lines, carryLocal for the rows
// outside history that bind an own kind (a sealed value's binding, a
// vector's key, a stored Idempotency-Key outcome). The name is the row's, the authority the lines were written
// under: an import under another name takes only a history already in this
// dialect (ImportArchive), so the two are the same name wherever this runs.
// The repository's own hands move with them: an actor record id derived from
// a hand that carried the name becomes the id of the same hand under `~`,
// wherever a line holds one (localhands.go).

import (
	"context"
	"database/sql"
)

// localNamespaceDialect is the changelog dialect from which a history holds
// the repository's own kinds under `~` and spells its name nowhere a
// conversion reads it.
const localNamespaceDialect = 6

func convertLocalNamespace(r conversionRepository) (conversionPass, error) {
	versions, err := r.Declarations()
	if err != nil {
		return conversionPass{}, err
	}
	refs := referencePositionsOf(versions, r.Authority)
	actors, err := r.Actors()
	if err != nil {
		return conversionPass{}, err
	}
	moves := actorMovesOf(r.Authority, versions, actors)
	return conversionPass{
		Line: localizeLine(r.Authority, refs, moves),
		Carry: func(ctx context.Context, tx *sql.Tx) error {
			return carryLocal(ctx, tx, r.Authority, refs, moves, r.Reseal, r.SecretsLeftBehind)
		},
	}, nil
}
