package engine_test

// The operator's install list (docs/plans/host-defaults.md): each listed
// package is installed through the catalog at registration and at the first
// open under a file that lists it, and never again once the repository's
// changelog has held it, so an uninstall sticks across a restart, a rebuild
// and an export restored into an empty database. The package is the shipped
// Apple provider, which declares kinds only.

import (
	"context"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/kinds"
)

const operatorInstallPackage = "providers.substrate.reamde.dev/apple"

// countingInstaller is the catalog's install, counted per package.
type countingInstaller struct {
	cat   *catalog.Catalog
	mu    sync.Mutex
	calls map[string]int
}

func newCountingInstaller(t *testing.T) *countingInstaller {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()))
	if err != nil {
		t.Fatalf("load the catalog: %v", err)
	}
	return &countingInstaller{cat: cat, calls: map[string]int{}}
}

func (c *countingInstaller) install(ctx context.Context, ds substrate.Dataset, pkg string) error {
	c.mu.Lock()
	c.calls[pkg]++
	c.mu.Unlock()
	return c.cat.SeedInstall(ctx, ds, pkg)
}

func (c *countingInstaller) count(pkg string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[pkg]
}

// openInstalling opens a service over dsn and root with a defaults file that
// lists pkgs under install.
func openInstalling(t *testing.T, dsn, root string, inst *countingInstaller, pkgs ...string) substrate.Service {
	t.Helper()
	svc, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithDataRoot(root),
		engine.WithOperatorDefaults(&operatordefaults.Config{Install: pkgs}, "Example"),
		engine.WithOperatorInstaller(inst.install))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func holdsBundle(t *testing.T, ds substrate.Dataset, pkg string) bool {
	t.Helper()
	statuses, err := ds.BundleStatusesUncounted(context.Background())
	if err != nil {
		t.Fatalf("bundle statuses: %v", err)
	}
	for _, st := range statuses {
		if st.ID == pkg {
			return true
		}
	}
	return false
}

func mustDataset(t *testing.T, svc substrate.Service, repository string) substrate.Dataset {
	t.Helper()
	ds, err := svc.Dataset(context.Background(), repository)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return ds
}

// A registration installs the listed package before it returns. The
// repository holds it from then on without another install, and once its
// owner uninstalls it, no restart, rebuild or restore of an export into an
// empty database installs it again.
func TestOperatorInstallRunsAtRegistrationAndAnUninstallSticks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const repo = "ada.example.com"
	inst := newCountingInstaller(t)
	dsn, root := engine.MigratedDSN(t), t.TempDir()

	svc := openInstalling(t, dsn, root, inst, operatorInstallPackage)
	registerUser(t, svc, repo)
	if n := inst.count(operatorInstallPackage); n != 1 {
		t.Fatalf("the registration ran %d installs, want 1", n)
	}
	if !holdsBundle(t, mustDataset(t, svc, repo), operatorInstallPackage) {
		t.Fatal("the registered repository does not hold the listed package")
	}
	_ = svc.Close()

	svc = openInstalling(t, dsn, root, inst, operatorInstallPackage)
	ds := mustDataset(t, svc, repo)
	if n := inst.count(operatorInstallPackage); n != 1 {
		t.Fatalf("a restart ran another install (%d), but the repository holds the package", n)
	}
	if err := ds.UninstallBundle(ctx, operatorInstallPackage); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	// The sweep collects the bundle row's tombstone, as the gc loop does, so
	// only the changelog still says the repository held the package.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if got, err := ds.Get(ctx, "substrate.reamde.dev/core/bundle", operatorInstallPackage); err == nil {
		t.Fatalf("the uninstalled bundle row survived the sweep: %+v", got)
	}
	_ = svc.Close()

	stillGone := func(when string, ds substrate.Dataset) {
		t.Helper()
		if n := inst.count(operatorInstallPackage); n != 1 {
			t.Fatalf("%s: %d installs, want the uninstall to stick", when, n)
		}
		if holdsBundle(t, ds, operatorInstallPackage) {
			t.Fatalf("%s: the uninstalled package is back", when)
		}
	}
	svc = openInstalling(t, dsn, root, inst, operatorInstallPackage)
	stillGone("after a restart", mustDataset(t, svc, repo))

	if _, err := svc.(rebuilder).RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	_ = svc.Close()
	svc = openInstalling(t, dsn, root, inst, operatorInstallPackage)
	ds = mustDataset(t, svc, repo)
	stillGone("after a rebuild and a restart", ds)

	root2, _, _ := exportToRoot(t, ds)
	svc2 := openInstalling(t, engine.MigratedDSN(t), root2, inst, operatorInstallPackage)
	stillGone("after an export restored into an empty database", mustDataset(t, svc2, repo))
}

// A repository registered before the file listed the package receives it at
// its first open under the file that does, and only then.
func TestOperatorInstallRunsAtTheFirstOpenUnderAFileThatListsIt(t *testing.T) {
	t.Parallel()
	const repo = "ada.example.com"
	inst := newCountingInstaller(t)
	dsn, root := engine.MigratedDSN(t), t.TempDir()

	svc := openInstalling(t, dsn, root, inst)
	registerUser(t, svc, repo)
	if holdsBundle(t, mustDataset(t, svc, repo), operatorInstallPackage) {
		t.Fatal("a file listing nothing installed the package")
	}
	_ = svc.Close()

	svc = openInstalling(t, dsn, root, inst, operatorInstallPackage)
	if !holdsBundle(t, mustDataset(t, svc, repo), operatorInstallPackage) {
		t.Fatal("the first open under the file that lists the package did not install it")
	}
	_ = svc.Close()

	svc = openInstalling(t, dsn, root, inst, operatorInstallPackage)
	mustDataset(t, svc, repo)
	if n := inst.count(operatorInstallPackage); n != 1 {
		t.Fatalf("%d installs over three opens, want 1", n)
	}
}
