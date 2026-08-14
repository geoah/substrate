package engine

// The seams the vocabulary tests need and no public door offers, compiled into
// the test binary alone.
//
// The declaration-planting pair stands a repository up as an OLDER binary's
// projection left it: declaration rows in the dialect-1 shape, their authored
// content inside a `definition` blob with the projected mirrors beside it. No
// live path writes that state — this binary's projection writes the typed shape,
// and its write path REFUSES a `definition` property because no core declaration
// declares one any more — so the seam writes through the fold, exactly as the
// rung does.

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

// DialectOneProps is the DIALECT-1 shape of a typed declaration row: the
// declaration's own data map back inside a `definition` blob, with the mirror
// columns the pre-typed projection wrote beside it. It is the inverse of
// authorityDeclarations as that projection wrote it, kept here rather than in
// the ladder because nothing but a test ever needs to write backwards.
func (ds *dataset) DialectOneProps(kindIdent, id string, props map[string]any) map[string]any {
	short := vocabularyRecordKinds[kindIdent]
	out := map[string]any{"name": localNameOf(id)}
	// The engine's own properties are the same in both dialects; the authored
	// ones move into the blob.
	for k, v := range props {
		if engineOwned(short, k) {
			out[k] = v
		}
	}
	switch short {
	case vocabulary.DocAuthority, kindActorLocal:
		// Neither ever carried a blob: their whole content is the two or three
		// properties above, and only the `name` mirror is dialect-1's own.
		for k, v := range props {
			if !columnBackedProp[k] {
				out[k] = v
			}
		}
		return out
	}
	data := declarationData(short, props)
	out["definition"] = data
	out["authority"] = data["authority"]
	switch short {
	case vocabulary.DocKind:
		names, _ := data["names"].(map[string]any)
		out["plural"] = names["plural"]
		out["version"] = props["version"]
	case vocabulary.DocPropertyType:
		out["base"] = data["base"]
	case vocabulary.DocRecordMapping:
		out["from"], out["to"], out["edge"] = data["from"], data["to"], data["edge"]
	case vocabulary.DocAgent:
		// The mirrors: the callable tools and the sub-agent identities, projected
		// beside the blob for a reader that would not parse it.
		out["description"], out["prompt"] = data["description"], data["prompt"]
		out["provider"], out["model"] = data["provider"], data["model"]
		if v, ok := data["subagentOnly"]; ok {
			out["subagentOnly"] = v
		}
		var functions []any
		for _, tv := range asAnyList(data["tools"]) {
			entry, _ := tv.(map[string]any)
			if callable, ok := entry["callable"]; ok {
				functions = append(functions, callable)
			}
		}
		if len(functions) > 0 {
			out["functions"] = functions
		}
		if subagents := asAnyList(data["agents"]); len(subagents) > 0 {
			out["subagents"] = subagents
		}
	}
	return out
}

// asAnyList reads a jsonb list property, nil-safe.
func asAnyList(v any) []any {
	out, _ := v.([]any)
	return out
}

// ApplyVocabularyWithRemoval admits upserts AND one removal as a single
// admission unit. No door outside the engine does: /vocabulary/apply only
// upserts, the generic delete only removes, and a bundle re-apply drops a
// declaration only by leaving it out of a closure. It is the one shape that can
// write a declaration row of a category the same batch stops declaring, which is
// what the final dropped-kind guard is there for. removeShort is the manifest
// short name ("kind"), as deleteVocabularyRecord builds it.
func (ds *dataset) ApplyVocabularyWithRemoval(ctx context.Context, actor substrate.Actor, docs []map[string]any, removeShort, removeID, removeAuthority string) error {
	parsed, err := parseVocabularyDocs(docs)
	if err != nil {
		return err
	}
	_, err = ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{
		docs: parsed,
		deletes: []vocabulary.Document{{
			Kind: removeShort, ID: removeID,
			Data: map[string]any{"authority": removeAuthority},
		}},
	})
	return err
}
