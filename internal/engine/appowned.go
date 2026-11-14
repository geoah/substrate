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
	"github.com/geoah/substrate/internal/vocabulary"
)

// TakeOverPackage requires the owner's hand and an exact confirmation.
// A document-published package becomes installed and immediately editable;
// its provenance records the publisher it left. Legacy app ownership stamps
// are cleared without changing the app's token or access.
func (ds *dataset) TakeOverPackage(ctx context.Context, actor substrate.Actor, pkg, confirm string) (*substrate.Record, error) {
	if actor == "" || substrate.ReservedActor(actor) {
		return nil, fmt.Errorf("%w: taking over a package is the owner's, and %s is not the owner's hand", substrate.ErrForbidden, handWords(actor))
	}
	if confirm != pkg {
		return nil, fmt.Errorf("%w: confirm must repeat the package identity, %q, to take it over", substrate.ErrValidation, pkg)
	}
	pkg = vocabulary.LocalizeRef(pkg, ds.names()...)
	ds.vocabularyWriteMu.Lock()
	defer ds.vocabularyWriteMu.Unlock()
	var out *substrate.Record
	err := ds.inTx(ctx, actor, true, func(t *txn) error {
		if err := t.lockKey(registryDepKey(ds)); err != nil {
			return err
		}
		row, err := t.loadRow(eref{Kind: kindPackage, ID: pkg}, false)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return fmt.Errorf("%w: package %s", substrate.ErrNotFound, pkg)
		}
		owner := substrate.PackageOwnerOf(row.Props, t.actorKeyOf)
		published, _ := row.Props["publishedFrom"].(string)
		if !owner.App() && published == "" {
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
		props := map[string]any{propPackageDeclaredBy: vocabulary.ActorRef(actorID(actor))}
		if ty.Props[propPackageTakenOverFrom] != nil {
			props[propPackageTakenOverFrom] = owner.String()
		}
		if ty.Props[propPackageDeclaredByClient] != nil {
			props[propPackageDeclaredByClient] = nil
		}
		if published != "" {
			authority, _ := vocabulary.SplitPackageRef(pkg)
			props["source"] = "installed"
			props["takenOverFrom"] = authority
			props["publishedFrom"] = nil
		}
		out, err = t.putKind(ty, substrate.PutInput{Kind: kindPackage, ID: pkg, Properties: props})
		if err != nil {
			return err
		}
		if _, live := ds.registry().PackageByName(pkg); published != "" && live {
			built, unparsed, err := ds.storedPackages(ctx, t.tx, func(name string) bool { return name != pkg })
			if err != nil {
				return err
			}
			if len(unparsed) != 0 {
				return fmt.Errorf("%w: taken-over package %s no longer parses", substrate.ErrValidation, pkg)
			}
			candidate := ds.registry().Clone()
			candidate.Remove(pkg)
			if err := candidate.InstallAll(built); err != nil {
				return err
			}
			t.publishReg = candidate
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
