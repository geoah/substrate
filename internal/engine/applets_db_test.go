package engine

// The applets package: an applet is checked at its write, and Moss's designer
// writes one and previews what it draws.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const designerID = "substrate.reamde.dev/assistant/designer"

// padsApplet is a one-screen applet over the scratch pads openMossDataset
// declares: a list of their names.
func padsApplet(pad string) map[string]any {
	return map[string]any{
		"name":    "Pads",
		"runtime": "a2ui",
		"sources": map[string]any{"pads": map[string]any{"kind": pad, "orderBy": "name"}},
		"screens": map[string]any{
			"home": map[string]any{
				"title":   "Pads",
				"sources": []any{"pads"},
				"components": []any{
					map[string]any{"id": "root", "component": "List", "children": map[string]any{"path": "/data/pads", "componentId": "row"}},
					map[string]any{"id": "row", "component": "Text", "text": map[string]any{"@path": "properties/name"}},
				},
			},
		},
	}
}

func TestAnAppletIsCheckedAgainstTheCatalogAndTheKindsAtItsWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _, repo := openMossDataset(t)
	pad := repo + "/scratch/pad"

	bad := padsApplet(pad)
	bad["sources"].(map[string]any)["pads"].(map[string]any)["orderBy"] = "shade"
	screen := bad["screens"].(map[string]any)["home"].(map[string]any)
	screen["components"].([]any)[1].(map[string]any)["id"] = "pad-row"
	_, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: applet.Kind, ID: "pads", Properties: bad})
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("a broken applet was admitted: %v", err)
	}
	joined := strings.Join(ve.Problems, "\n")
	for _, want := range []string{
		`props.sources.pads.orderBy[0]: ` + pad + ` declares no property "shade"`,
		`props.screens.home.components[1].id: "pad-row" is not an A2UI id`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("want %q among the problems:\n%s", want, joined)
		}
	}

	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: applet.Kind, ID: "pads", Properties: padsApplet(pad)}); err != nil {
		t.Fatalf("a sound applet was refused: %v", err)
	}
}

// The designer writes an applet through its named grant and previews it: the
// outline shows the records the screen read.
func TestTheDesignerWritesAnAppletAndPreviewsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	pad := repo + "/scratch/pad"
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: pad, Properties: map[string]any{"name": "groceries"}}); err != nil {
		t.Fatal(err)
	}
	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{{"write", toolArgs(t, map[string]any{
			"op": "put", "kind": applet.Kind, "id": "pads", "input": map[string]any{"properties": padsApplet(pad)},
		})}}},
		fakeTurn{calls: []fakeCall{{"preview", toolArgs(t, map[string]any{"id": "pads"})}}},
		fakeTurn{content: "[Pads](/applets/pads)"},
	)
	res, err := ds.CallAgent(ctx, designerID, "an applet listing my pads")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	if len(rows) != 2 {
		t.Fatalf("tool rows = %d, want write and preview", len(rows))
	}
	if rows[0]["ok"] != true {
		t.Fatalf("write: %v", rows[0]["content"])
	}
	preview, _ := rows[1]["content"].(string)
	if !strings.Contains(preview, `(1 items at /data/pads)`) || !strings.Contains(preview, `row Text text=@/data/pads/0/properties/name=\"groceries\"`) {
		t.Fatalf("preview: %s", preview)
	}
	if _, err := ds.Get(ctx, applet.Kind, "pads"); err != nil {
		t.Fatalf("the applet did not land: %v", err)
	}
}

// A preview reads under the agent's grants all the way down: a referent
// the agent may not read is dropped from what an expanded source carries.
func TestAPreviewHoldsExpandedRecordsToTheAgentsReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _, repo := openMossDataset(t)
	pad, link := repo+"/scratch/pad", repo+"/scratch/link"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.KindManifest(repo+"/scratch", map[string]any{"singular": "link"}, map[string]any{
			"description":     "A link to a pad.",
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "what the link is called"},
				"pad":  map[string]any{"type": "reference", "kind": pad, "description": "the pad it points at"},
			},
		}),
	}); err != nil {
		t.Fatalf("declare the link: %v", err)
	}
	p, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: pad, Properties: map[string]any{"name": "secret", "note": "not yours"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: link, Properties: map[string]any{
		"name": "to the pad", "pad": pad + "/" + p.ID,
	}}); err != nil {
		t.Fatal(err)
	}
	reads := agentReads{ds: ds, allows: func(kind string) bool { return kind == vocabulary.LocalizeRef(link, repo) }}
	page, err := reads.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{link}}, Expand: []string{"pad"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || len(page.Included) != 0 {
		t.Fatalf("records %d, included %v: the pad reached a preview whose agent reads links alone", len(page.Records), page.Included)
	}
	if _, err := reads.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: link}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a preview wrote: %v", err)
	}
}
