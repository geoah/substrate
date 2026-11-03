package engine

// A package an app declared under an earlier binary's `declare:` scope
// carries the app in its `declaredBy` and `declaredByClient` stamps (decision
// 0205). The lock those stamps held is retired (decision 0214): every hand
// writes the package's declarations as it writes the owner's own.
// TakeOverPackage, below, stays as the owner's deliberate way to rewrite the
// stamps so the row no longer names the app; the catalog extends it to a
// document-published package.

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// TakeOverPackage makes an app's package the owner's: the package row's
// `declaredBy` becomes the hand that took it over, `takenOverFrom` names the
// app it was taken from and `declaredByClient` is cleared, so the chokepoint no
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
		owner := substrate.PackageOwnerOf(row.Props)
		if !owner.App() {
			return fmt.Errorf("%w: %s is not managed by an app: its declarations are already yours to change", substrate.ErrConflict, pkg)
		}
		ty, err := ds.resolveType(kindPackage)
		if err != nil {
			return err
		}
		// The row says who it was taken from, so the answer and the
		// changelog entry both name the app, and the app's client id leaves
		// the row with its ownership. A `package` kind from before the two
		// properties (a withheld upgrade) carries the old actor in the
		// entry's delta alone.
		props := map[string]any{propPackageDeclaredBy: string(actor)}
		if ty.Props[propPackageTakenOverFrom] != nil {
			props[propPackageTakenOverFrom] = owner.String()
		}
		if ty.Props[propPackageDeclaredByClient] != nil {
			props[propPackageDeclaredByClient] = nil
		}
		out, err = t.putKind(ty, substrate.PutInput{Kind: kindPackage, ID: pkg, Properties: props})
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
