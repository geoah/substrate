package engine_test

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/sandboxtest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestOutsideInstalledFunctionCannotReachUndeclaredHost(t *testing.T) {
	const pkg = "publisher.example.com/outside"
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.BundleManifest(pkg, map[string]any{"description": "outside code", "installs": []any{pkg + "/probe"}}),
		vocabulary.FunctionManifest(pkg, "probe", map[string]any{"description": "probe undeclared egress", "runtime": "python", "permissions": map[string]any{"network": []any{"api.example.com"}}, "source": `import urllib.request

def main(input, host):
    urllib.request.urlopen("https://other.example.com/", timeout=2)
    return {"output": "escaped"}
`}),
	}
	parsed, parseErr := vocabulary.ParseDocuments(docs)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if _, err := vocabulary.BuildPackages(parsed, vocabulary.SourcePublished); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "linux" {
		t.Skip("outside-function confinement requires the Linux connect gate")
	}
	report := runner.Shared.Sandbox().Report()
	if !report.FS() || !report.Seccomp || !report.ConnectGate {
		sandboxtest.Unavailablef(t, "outside code requires full confinement: %s", report)
	}
	tree := shippedTree(t)
	// The catalog work package owns these core declarations; the fixture is
	// independently runnable on the common base, before that branch merges.
	patchShipped(t, coreKind(tree, "package.yaml"), func(s string) string {
		if strings.Contains(s, "    publishedFrom:") {
			return s
		}
		return strings.Replace(s, "  properties:\n", "  properties:\n    publishedFrom:\n      type: string\n      managed: true\n    publishedDigest:\n      type: string\n      managed: true\n", 1)
	})
	ctx := context.Background()
	svc, err := engine.OpenForTest(t, ctx, engine.MigratedDSN(t), engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsFS(os.DirFS(tree)))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if _, err = svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.InstallBundleClosure(ctx, substrate.BundleActor("publisher.example.com", "outside"), docs, nil, substrate.BundleInstall{Published: true, ShippedVersion: 1, PublishedFrom: "https://publisher.example.com/authority.json", PublishedDigest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ds.CallFunction(ctx, substrate.ActorPerson, pkg+"/probe", nil)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("undeclared host must fail at proxy: %v", err)
	}
}
