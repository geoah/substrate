package engine

// A package an app declared under its `declare:` scope is the app's (decision
// 0205): authorizeAppOwnedWrite (seed.go) refuses every other hand at the
// declaration chokepoint, and TakeOverPackage, below, is the owner's one
// deliberate way to make it theirs.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// packageOwner is the app that owns pkg: its live package row's `declaredBy`
// where that names an app, empty otherwise (a package nobody holds, one the
// owner, an agent or the substrate declared, or one created before the
// stamp). Ownership is read from the row on every write, never cached: the
// row is the changelog's, so a rebuild, an export and a takeover all carry
// it, and a repository that already holds an app's package needs no
// migration to have it owned.
func (ds *dataset) packageOwner(ctx context.Context, q dbx, pkg string) (substrate.Actor, error) {
	var by sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT props->>'`+propPackageDeclaredBy+`' FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		kindPackage, pkg).Scan(&by)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if a := substrate.Actor(by.String); substrate.IsAppActor(a) {
		return a, nil
	}
	return "", nil
}

// TakeOverPackage makes an app's package the owner's: the package row's
// `declaredBy` becomes the hand that took it over, so the chokepoint no
// longer names an app and the owner's doors write the package's declarations
// as they write any of their own. It is deliberate on both ends: confirm must
// repeat the package identity, and only the owner's own hand may take it (a
// door the request names, never an app, an agent, a function, a bundle or
// the substrate). Nothing else moves: no declaration, no record, no token.
// The app keeps its token and its scope, and its next apply into the package
// is refused, which is how it learns.
func (ds *dataset) TakeOverPackage(ctx context.Context, actor substrate.Actor, pkg, confirm string) (*substrate.Record, error) {
	if actor == "" || substrate.ReservedActor(actor) {
		return nil, fmt.Errorf("%w: taking over a package is the owner's, and %q is not the owner's hand", substrate.ErrForbidden, actor)
	}
	if confirm != pkg {
		return nil, fmt.Errorf("%w: confirm must repeat the package identity, %q, to take it over", substrate.ErrValidation, pkg)
	}
	ds.vocabularyWriteMu.Lock()
	defer ds.vocabularyWriteMu.Unlock()
	var out *substrate.Record
	err := ds.inTx(ctx, actor, true, func(t *txn) error {
		row, err := t.loadRow(eref{Kind: kindPackage, ID: pkg}, false)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return fmt.Errorf("%w: package %s", substrate.ErrNotFound, pkg)
		}
		owner, _ := row.Props[propPackageDeclaredBy].(string)
		if !substrate.IsAppActor(substrate.Actor(owner)) {
			return fmt.Errorf("%w: %s is not managed by an app: its declarations are already yours to change", substrate.ErrConflict, pkg)
		}
		ty, err := ds.resolveType(kindPackage)
		if err != nil {
			return err
		}
		out, err = t.putKind(ty, substrate.PutInput{Kind: kindPackage, ID: pkg, Properties: map[string]any{
			propPackageDeclaredBy: string(actor),
		}})
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
