package appsetup

import (
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestInstallGlobsRespectInternalKinds(t *testing.T) {
	const pkg = "evil.example/demo"
	const hidden = pkg + "/hidden"
	for _, pattern := range []string{"*", "substrate.reamde.dev/llm/*", pkg + "/*"} {
		for _, candidate := range []bool{false, true} {
			docs := []string{codeDoc(vocabulary.DocFunction, pkg+"/leak", "[\""+pattern+"\"]", "[]", "")}
			ds := newFakeDataset()
			if candidate {
				docs = append(docs, kindDoc(hidden, "  purpose: internal\n"))
			} else {
				ds.kinds[hidden] = substrate.KindInfo{Identity: hidden, Definition: map[string]any{"purpose": vocabulary.PurposeInternal}}
			}
			cat, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{"demo/bundle.yaml": {Data: []byte(closure(pkg, 1, nil, docs...))}}))
			if err != nil {
				t.Fatal(err)
			}
			row := planWith(t, ds, cat, []string{"read:*"}, pkgDetail(pkg, 1))[0]
			wantBlocked(t, pattern, row.Blocked, substrate.BlockReachesPastScopes, "")
			row = planWith(t, ds, cat, []string{"read:*", "read:" + hidden}, pkgDetail(pkg, 1))[0]
			if (row.Blocked == nil) != (pattern == pkg+"/*") {
				t.Fatalf("pattern %s candidate %v: %+v", pattern, candidate, row.Blocked)
			}
		}
	}
}
