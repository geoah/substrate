package appsetup

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestApprovalReachIncludesAnInstalledExternalCallable(t *testing.T) {
	const target = "other.example/private/run"
	const secret = "other.example/private/secret"
	body := closure(appPkg, 1, nil, `kind: substrate.reamde.dev/core/trigger
metadata:
  id: external-schedule
data:
  properties:
    callable: substrate.reamde.dev/core/function/other.example/private/run
    enabled: true
    source:
      schedule:
        cron: "0 * * * *"
`)
	cat, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{"app/bundle.yaml": {Data: []byte(body)}}))
	if err != nil {
		t.Fatal(err)
	}
	ds := newFakeDataset()
	kind := vocabulary.CoreKind(vocabulary.DocFunction)
	ds.records[recordKey(kind, target)] = &substrate.Record{Kind: kind, ID: target, Properties: map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{secret}}}}}
	row := planWith(t, ds, cat, []string{"read:" + appPkg + "/*"}, pkgDetail(appPkg, 1))[0]
	if row.Blocked == nil || row.Blocked.Reason != substrate.BlockReachesPastScopes || !strings.Contains(row.Blocked.Problem, secret) {
		t.Fatalf("external reach not blocked: %+v", row)
	}
	row = planWith(t, ds, cat, []string{"read:" + secret}, pkgDetail(appPkg, 1))[0]
	if row.Blocked != nil {
		t.Fatalf("exact scope blocked: %+v", row.Blocked)
	}
	delete(ds.records, recordKey(kind, target))
	if _, _, err := Plan(context.Background(), ds, cat, []substrate.AuthorizationDetail{pkgDetail(appPkg, 1)}, Options{}); err == nil {
		t.Fatal("missing callable accepted")
	}
}
