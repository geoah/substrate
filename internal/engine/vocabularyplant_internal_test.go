package engine

// The seams the vocabulary tests need and no public door offers, compiled into
// the test binary alone.
//
// PlantDeclarationRow stands a declaration row up as some OTHER binary's
// projection left it — a property that binary never declared, a version ahead of
// the tree's. No live path writes those states, and the write path refuses
// several of them, so the seam writes through the FOLD: the row and its
// changelog entry land together and RebuildRepository reproduces the planted
// state exactly.

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// PlantDeclarationRow replaces one declaration row's properties WHOLE, through
// the fold, so the row and the changelog entry that describes it land together
// and RebuildRepository reproduces exactly the planted state. Coercion is
// deliberately skipped: the shape these tests need is one this binary's
// declarations refuse.
func (ds *dataset) PlantDeclarationRow(ctx context.Context, kindIdent, id string, props map[string]any) error {
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		row, err := t.loadRow(eref{Kind: kindIdent, ID: id}, true)
		if err != nil {
			return err
		}
		if row == nil {
			return fmt.Errorf("substrate/engine: no %s row %s to plant onto", kindIdent, id)
		}
		before := row.clone()
		row.Props = props
		res, err := t.foldRow(before, row, false, false)
		if err != nil || !res.changed {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpPatch, id, kindIdent,
			map[string]any{"properties": sortedKeys(props)})
	})
}

// ApplyVocabularyWithRemoval admits upserts AND one removal as a single
// admission unit. No door outside the engine does: /vocabulary/apply only
// upserts, the generic delete only removes, and a bundle re-apply drops a
// declaration only by leaving it out of a closure. It is the one shape that can
// write a declaration row of a category the same batch stops declaring, which is
// what the final dropped-kind guard is there for. removeShort is the manifest
// short name ("kind"), as deleteVocabularyRecord builds it, and removePackage
// the package identity the removed declaration lives in.
func (ds *dataset) ApplyVocabularyWithRemoval(ctx context.Context, actor substrate.Actor, docs []map[string]any, removeShort, removeID, removePackage string) error {
	parsed, err := parseVocabularyDocs(docs)
	if err != nil {
		return err
	}
	authority, name := vocabulary.SplitPackageRef(removePackage)
	_, err = ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{
		docs: parsed,
		deletes: []vocabulary.Document{{
			Kind: removeShort, ID: removeID,
			Data: map[string]any{"authority": authority, "package": name},
		}},
	})
	return err
}
