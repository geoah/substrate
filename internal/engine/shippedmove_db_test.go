package engine_test

// The boot upgrade's checks on a kind move (move.go, decision records 0078
// and 0257). They are asked while the old kind holds live rows, and the
// preview reports the lines the boot refused on, verbatim. An old kind left
// declared and empty, as every repository that predates a shipped move keeps
// one, is no move.

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const (
	gadgetKind = corePackage + "/gadget"
	widgetKind = corePackage + "/widget"
)

// gadgetRepository is a repository whose core declares `gadget` (one string
// property, `note`), holding the given number of live gadgets.
func gadgetRepository(t *testing.T, rows int) string {
	t.Helper()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	addShippedKind(t, tree, corePackage, "gadget")
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("shipping gadget is additive and must land: %v", err)
	}
	if rows == 0 {
		return dsn
	}
	svc := openTree(t, dsn, tree)
	ds, err := svc.Dataset(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	for range rows {
		mustPut(t, ds, owner, substrate.PutInput{Kind: gadgetKind, Properties: map[string]any{"note": "kept"}})
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	return dsn
}

// widgetTree is the next binary: core ships `widget`, moved from `gadget`,
// with the property block given.
func widgetTree(t *testing.T, properties string) string {
	t.Helper()
	tree := shippedTree(t)
	addShippedKind(t, tree, corePackage, "gadget")
	doc := "kind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + widgetKind +
		"\ndata:\n  authority: substrate.reamde.dev\n  package: core\n  names:\n    singular: widget" +
		"\n  displayTemplate: \"{title}\"\n  movedFrom: " + gadgetKind + "\n  properties:\n" + properties
	if err := os.WriteFile(filepath.Join(tree, corePackage, "widget.yaml"), []byte(doc), 0o600); err != nil {
		t.Fatalf("ship widget: %v", err)
	}
	bumpPackageVersion(t, tree, corePackage, "100")
	return tree
}

// bootWidget opens the repository under tree and answers the line the boot
// refused on ("" when it upgraded), the preview's blockers on the same open,
// and the open dataset.
func bootWidget(t *testing.T, dsn, tree string) (string, string, substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	var logs bytes.Buffer
	svc, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithKindsDir(tree), engine.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	return loggedRefusal(t, logs.String()), strings.Join(planCore(t, ds).Upgrade.Blockers, "; "), ds
}

func TestABootUpgradeRefusesAMoveItCannotHonor(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ properties, says string }{
		"a property the old kind declares and the new one does not": {
			properties: "    label:\n      type: string\n",
			says:       `kind ` + widgetKind + `: data.movedFrom names ` + gadgetKind + `, which declares "note" and this kind does not`,
		},
		"a property the new kind requires and the old one lacks": {
			properties: "    note:\n      type: string\n    level:\n      type: string\n      required: true\n",
			says:       `kind ` + widgetKind + `: data.movedFrom names ` + gadgetKind + `, which does not declare "level", and this kind requires it`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dsn := gadgetRepository(t, 1)
			refused, blockers, ds := bootWidget(t, dsn, widgetTree(t, tc.properties))
			if !strings.Contains(refused, tc.says) {
				t.Fatalf("the boot did not refuse the move:\n  logged %q\n  want %q", refused, tc.says)
			}
			if blockers != refused {
				t.Fatalf("preview blockers:\n  %s\nlogged refused:\n  %s", blockers, refused)
			}
			// Nothing moved: the gadget is where it was.
			if got := liveOf(t, ds, gadgetKind); len(got) != 1 {
				t.Fatalf("the refused move touched the gadgets: %d left", len(got))
			}
		})
	}
}

// An optional property only the new kind declares is admitted: the carried
// rows hold no value for it, and the move lands.
func TestABootUpgradeMovesRowsIntoAKindThatAddsAnOptionalProperty(t *testing.T) {
	t.Parallel()
	dsn := gadgetRepository(t, 1)
	refused, blockers, ds := bootWidget(t, dsn, widgetTree(t, "    note:\n      type: string\n    label:\n      type: string\n"))
	if refused != "" || blockers != "" {
		t.Fatalf("a move into a kind that adds an optional property was refused:\n  logged %q\n  preview %q", refused, blockers)
	}
	if got := liveOf(t, ds, widgetKind); len(got) != 1 || got[0].Properties["note"] != "kept" {
		t.Fatalf("the gadget did not arrive as a widget: %+v", got)
	}
	if got := liveOf(t, ds, gadgetKind); len(got) != 0 {
		t.Fatalf("%d gadgets stayed behind", len(got))
	}
}

// An old kind declared and empty carries nothing, so its new kind's upgrade
// is no move and refuses nothing, whatever the new kind has gained.
func TestABootUpgradeOfAKindMovedOutOfAnEmptyKindIsNotRefused(t *testing.T) {
	t.Parallel()
	dsn := gadgetRepository(t, 0)
	refused, blockers, ds := bootWidget(t, dsn, widgetTree(t,
		"    note:\n      type: string\n    label:\n      type: string\n    level:\n      type: string\n      required: true\n"))
	if refused != "" || blockers != "" {
		t.Fatalf("an upgrade out of an empty kind was refused:\n  logged %q\n  preview %q", refused, blockers)
	}
	if _, err := ds.KindByRef(context.Background(), widgetKind); err != nil {
		t.Fatalf("the widget kind did not land: %v", err)
	}
}

func liveOf(t *testing.T, ds substrate.Dataset, kind string) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{kind}}})
	if err != nil {
		t.Fatalf("list %s: %v", kind, err)
	}
	return page.Records
}
