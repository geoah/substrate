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
	"github.com/geoah/substrate/internal/vocabulary"
)

// packageOwner is the app that owns pkg, read off its live package row's
// `declaredBy` and `declaredByClient` (substrate.PackageOwnerOf): the zero
// value for a package nobody holds, one the owner, an agent or the substrate
// declared, or one created before the stamp. Ownership is read from the row
// on every write, never cached: the row is the changelog's, so a rebuild, an
// export and a takeover all carry it, and a repository that already holds an
// app's package needs no migration to have it owned.
func (ds *dataset) packageOwner(ctx context.Context, q dbx, pkg string) (substrate.PackageOwner, error) {
	var by, client sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT props->>'`+propPackageDeclaredBy+`', props->>'`+propPackageDeclaredByClient+`' FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		kindPackage, pkg).Scan(&by, &client)
	if errors.Is(err, sql.ErrNoRows) {
		return substrate.PackageOwner{}, nil
	}
	if err != nil {
		return substrate.PackageOwner{}, err
	}
	return substrate.PackageOwnerOf(map[string]any{
		propPackageDeclaredBy: by.String, propPackageDeclaredByClient: client.String,
	}), nil
}

// appOwnedPackages is every live package an app owns, by identity: what the
// dependency guard reads (appDependentGuards).
func (ds *dataset) appOwnedPackages(ctx context.Context, q dbx) (map[string]substrate.PackageOwner, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, props->>'`+propPackageDeclaredBy+`', props->>'`+propPackageDeclaredByClient+`' FROM records
		  WHERE kind = $1 AND deleted_at IS NULL AND props->>'`+propPackageDeclaredBy+`' LIKE $2`,
		kindPackage, substrate.AppActorPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]substrate.PackageOwner{}
	for rows.Next() {
		var id string
		var by, client sql.NullString
		if err := rows.Scan(&id, &by, &client); err != nil {
			return nil, err
		}
		if o := substrate.PackageOwnerOf(map[string]any{propPackageDeclaredBy: by.String, propPackageDeclaredByClient: client.String}); o.App() {
			out[id] = o
		}
	}
	return out, rows.Err()
}

// appDependentGuards refuses a batch that would leave an app's package
// requiring what the candidate no longer holds (decision 0205). The batch
// does not touch the app's package, so the chokepoint does not stop it, and
// the candidate rebuilds only what the batch touches: uninstalling health,
// which training requires, would otherwise land, and training would meet the
// missing requirement only at its next apply, or be quarantined at the next
// open. The app cannot fix that and the owner cannot edit the package, so the
// batch is refused instead, naming the app's package and who keeps it. The
// owner takes the app's package over, or removes it, first. A package the
// owner keeps is not guarded here: its owner can mend it.
func appDependentGuards(owned map[string]substrate.PackageOwner, candidate *vocabulary.Registry, touched map[string]bool) []string {
	var out []string
	for _, id := range sortedKeys(owned) {
		if touched[id] {
			continue
		}
		g, ok := candidate.PackageByName(id)
		if !ok || g.Bundle == nil {
			continue
		}
		for _, req := range g.Bundle.Requires {
			if !touched[req] {
				continue
			}
			held, ok := candidate.PackageByName(req)
			floor, pinned := g.Bundle.RequiresAtLeast[req]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s requires %s, and %s is managed by the app %s, which cannot be changed to do without it: take %s over, or remove it, first",
					id, req, id, owned[id], id))
			case pinned && vocabulary.CompareVersions(held.Version, floor) < 0:
				out = append(out, fmt.Sprintf("%s requires %s at version %d or later, and %s is managed by the app %s: take %s over, or remove it, first",
					id, req, floor, id, owned[id], id))
			}
		}
	}
	return out
}

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
		props := map[string]any{propPackageDeclaredBy: string(actor)}
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
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
