package engine

// Repository migration 6: the repository's own record takes its id
// (decision 0261).
//
// `substrate.reamde.dev/core/repository` is the repository's description of
// itself, one record, and it was written under the repository's name, which
// was its id. A repository now has an id of its own, the boot gave one to
// every repository a release before ids made (repositoryid.go), and the
// record moves to it: the same properties, states and labels under the id,
// and the record under the name deleted. A repository made since is born with
// the record under its id and has nothing to move, and so does a second run.
//
// Ordinary record writes, as the system: a put that creates the record, with
// the manager row of each property the creation writes, and a delete.

import (
	"fmt"
	"maps"
	"slices"

	"github.com/geoah/substrate/internal/substrate"
)

func migrateKeyTheRepositoryRecordByItsID(t *txn) (string, error) {
	id, name := t.ds.info.ID, t.ds.info.Authority
	if id == name {
		return "", fmt.Errorf("repository %s has no id yet: the boot gives it one before any open", name)
	}
	old, err := t.loadRow(eref{Kind: kindRepository, ID: name}, true)
	if err != nil {
		return "", err
	}
	if old == nil || old.DeletedAt != nil {
		return "the repository's record is under its id", nil
	}
	moved, err := t.loadRow(eref{Kind: kindRepository, ID: id}, true)
	if err != nil {
		return "", err
	}
	if moved == nil {
		row := &erow{
			ID: id, Kind: kindRepository, Title: old.Title, Body: old.Body,
			States: maps.Clone(old.States), Props: maps.Clone(old.Props), Labels: maps.Clone(old.Labels),
			KindVersion: old.KindVersion,
		}
		if _, err := t.foldRow(nil, row, false, false); err != nil {
			return "", err
		}
		props := slices.Sorted(maps.Keys(row.Props))
		for _, p := range props {
			if err := t.setManager(row.ref(), p, substrate.ActorSystem, substrate.TierMachine); err != nil {
				return "", err
			}
		}
		if err := t.appendChange(substrate.ActorSystem, substrate.OpPut, id, kindRepository,
			map[string]any{"created": true, "properties": props}); err != nil {
			return "", err
		}
	}
	if err := t.migrateTombstone(eref{Kind: kindRepository, ID: name}); err != nil {
		return "", err
	}
	return fmt.Sprintf("moved the repository's record from %s to %s", name, id), nil
}
