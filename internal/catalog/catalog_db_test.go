package catalog_test

// The catalog install proof against a REAL engine: a shipped closure applied
// into a fresh repository lands its types, functions and delivery wiring through
// the same admission path an explicit apply uses, re-install is idempotent,
// and a non-owner is refused before anything is touched.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// credKey is a conforming credential key (base64 of 32 random bytes, ADR 0024),
// minted once per test binary so every Open here shares one key. Generated
// rather than committed, because a key in the tree is a key everyone has.
var credKey = func() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}()

// operatorOTP is the control plane's base32 TOTP seed (RFC 6238 Appendix B).
const operatorOTP = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

const (
	rlBundleAuthority = "samples.substrate.reamde.dev"
	rlBundlePackage   = "readinglist"
	rlBundleID        = rlBundleAuthority + "/" + rlBundlePackage
)

// The packages the reading-list closure declares against. Repository creation seeds
// core alone now, so a closure that subscribes to
// samples.substrate.reamde.dev/messaging/conversationmessage needs that
// package imported first, and messaging itself needs people. Importing them is
// the same verb a bundle install is.
var rlRequires = []string{"samples.substrate.reamde.dev/people", "samples.substrate.reamde.dev/messaging"}

func importVocabulary(t *testing.T, c *catalog.Catalog, ds substrate.Dataset, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, _, err := c.Install(context.Background(), substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("import %s: %v", id, err)
		}
	}
}

// bundleStatuser is the computed-status seam the engine dataset offers beyond
// the frozen substrate.Dataset — the truth about what landed after install.
type bundleStatuser interface {
	BundleStatus(ctx context.Context, id string) (substrate.BundleStatus, error)
}

func newDataset(t *testing.T) substrate.Dataset {
	t.Helper()
	dsn := testdb.NewSchema(t)
	svc, err := engine.Open(context.Background(), dsn,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev"),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(credKey),
	)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah.example.com")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return ds
}

func loadCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return c
}

// Every shipped bundle installs into ONE repository, requires first: the whole
// set has to coexist, so an unresolvable reference between two shipped
// packages surfaces here rather than on a user's substrate.
func TestShippedBundlesInstall(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	byID := map[string]*catalog.Bundle{}
	for _, b := range c.Bundles() {
		byID[b.ID] = b
	}
	installed := map[string]bool{}
	var install func(b *catalog.Bundle)
	install = func(b *catalog.Bundle) {
		if installed[b.ID] {
			return
		}
		installed[b.ID] = true
		for _, req := range b.Requires {
			rb, ok := byID[req]
			if !ok {
				t.Fatalf("%s requires %s, which no shipped bundle owns", b.ID, req)
			}
			install(rb)
		}
		if _, _, err := c.Install(ctx, substrate.ActorAPI, b.ID, ds); err != nil {
			t.Fatalf("install %s: %v", b.ID, err)
		}
	}
	for _, b := range c.Bundles() {
		install(b)
	}
	// Taking every one of them again changes nothing: no shipped closure
	// differs from the rows its first install wrote (issue #643).
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	for _, b := range c.Bundles() {
		if _, _, err := c.Install(ctx, substrate.ActorAPI, b.ID, ds); err != nil {
			t.Fatalf("re-install %s: %v", b.ID, err)
		}
		if after, err := ds.Head(ctx); err != nil {
			t.Fatalf("head: %v", err)
		} else if after != head {
			t.Errorf("re-installing the unchanged %s moved the changelog head %d -> %d", b.ID, head.Seq, after.Seq)
			head = after
		}
	}
	for _, b := range c.Bundles() {
		st, err := ds.(bundleStatuser).BundleStatus(ctx, b.ID)
		if err != nil {
			t.Fatalf("bundle status %s: %v", b.ID, err)
		}
		if !st.Installed {
			t.Errorf("%s not marked installed", b.ID)
		}
		if want := len(b.Closure.Kinds); st.Kinds != want {
			t.Errorf("%s kinds = %d, want %d", b.ID, st.Kinds, want)
		}
	}
}

func TestInstallLandsClosureAndIsIdempotent(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	// Missing vocabulary is refused BEFORE anything is touched, naming the
	// authority to import first — the whole point of `requires:`.
	_, _, err := c.Install(ctx, substrate.ActorAPI, rlBundleID, ds)
	if err == nil {
		t.Fatal("installed a closure whose required vocabulary is absent")
	}
	if !strings.Contains(err.Error(), "samples.substrate.reamde.dev/messaging") {
		t.Errorf("refusal does not name the missing authority: %v", err)
	}
	importVocabulary(t, c, ds, rlRequires...)

	b, _, err := c.Install(ctx, substrate.ActorAPI, rlBundleID, ds)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if b.ID != rlBundleID {
		t.Fatalf("installed %q, want %q", b.ID, rlBundleID)
	}

	// The types are present after install.
	for _, ident := range []string{
		"samples.substrate.reamde.dev/readinglist/digest",
		"samples.substrate.reamde.dev/readinglist/page",
	} {
		if _, err := ds.KindByRef(ctx, ident); err != nil {
			t.Errorf("type %s absent after install: %v", ident, err)
		}
	}

	// The computed status is the truth about what landed: four functions, two
	// types across the owned authority.
	st, err := ds.(bundleStatuser).BundleStatus(ctx, rlBundleID)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if st.Functions != 4 {
		t.Errorf("functions = %d, want 4", st.Functions)
	}
	if st.Kinds != 2 {
		t.Errorf("kinds = %d, want 2", st.Kinds)
	}
	if !st.Installed {
		t.Error("bundle not marked installed")
	}

	// The delivery wiring landed as ordinary data records.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/trigger"}},
		First:  50,
	})
	if err != nil {
		t.Fatalf("list triggers: %v", err)
	}
	if got := len(page.Records); got != 4 {
		t.Errorf("triggers = %d, want 4", got)
	}

	// INSTALL IS A COPY: the manifests landed in the repository's own
	// changelog as entries attributed to the BUNDLE —
	// `bundle:samples.substrate.reamde.dev:web`, the full authority and the
	// package (records 0025 and 0047), not to the owner who asked for them and not to the catalog,
	// which is a source and never an authority.
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{
		Actors: []substrate.Actor{substrate.BundleActor(rlBundleAuthority, rlBundlePackage)},
		Kinds:  []string{"substrate.reamde.dev/core/kind"},
	}, 100)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("the install wrote no declaration entries under the bundle's actor")
	}

	// Re-install is the bundle's own whole-authority re-apply: idempotent, no
	// error, and nothing new appears. An unchanged closure writes nothing, so
	// neither the package nor any kind moves its version, however many times
	// it is taken (issue #643).
	before := closureVersions(t, ds, b)
	for i := 2; i <= 3; i++ {
		if _, _, err := c.Install(ctx, substrate.ActorAPI, rlBundleID, ds); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
		assertVersionsKept(t, fmt.Sprintf("install %d", i), before, closureVersions(t, ds, b))
	}
	st2, err := ds.(bundleStatuser).BundleStatus(ctx, rlBundleID)
	if err != nil {
		t.Fatalf("bundle status after re-install: %v", err)
	}
	if st2.Functions != st.Functions || st2.Kinds != st.Kinds {
		t.Errorf("re-install changed the closure: %+v -> %+v", st, st2)
	}
	page2, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/trigger"}},
		First:  50,
	})
	if err != nil {
		t.Fatalf("list triggers after re-install: %v", err)
	}
	if got := len(page2.Records); got != 4 {
		t.Errorf("triggers after re-install = %d, want 4", got)
	}
}

// A data-document (delivery-wiring) admission failure rolls the WHOLE install
// back: the schema closure and every trigger commit as one transaction, so a
// trigger the candidate registry cannot admit — here one naming a callable that
// does not exist — leaves no live half-installed schema behind.
func TestInstallRollsBackOnBrokenDeliveryWiring(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	importVocabulary(t, loadCatalog(t), ds, rlRequires...)

	// The real web closure, copied out with one EXTRA trigger whose callable
	// resolves to nothing — admission fails inside the install transaction.
	// The copy mirrors the samples root: the authority manifest at the root,
	// one package directory beside it, which is the shape the loader reads.
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, rlBundlePackage)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyManifests := func(srcDir, dstDir string) {
		t.Helper()
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			t.Fatalf("read %s: %v", srcDir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			if err := os.WriteFile(filepath.Join(dstDir, e.Name()), raw, 0o644); err != nil {
				t.Fatalf("write %s: %v", e.Name(), err)
			}
		}
	}
	copyManifests("../../samples", dir)
	copyManifests("../../samples/readinglist", bundleDir)
	broken := "kind: substrate.reamde.dev/core/trigger\n" +
		"metadata: {id: readinglist-broken-on-message}\n" +
		"data:\n" +
		"  properties:\n" +
		"    enabled: true\n" +
		"    source:\n" +
		"      record:\n" +
		"        kinds: [samples.substrate.reamde.dev/messaging/conversationmessage]\n" +
		"        ops: [create]\n" +
		"    callable: substrate.reamde.dev/core/function/samples.substrate.reamde.dev/readinglist/doesnotexist\n"
	if err := os.WriteFile(filepath.Join(bundleDir, "zz-broken.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := catalog.Load(catalog.SampleRoot(os.DirFS(dir)))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}

	if _, _, err := c.Install(ctx, substrate.ActorAPI, rlBundleID, ds); err == nil {
		t.Fatal("install with a broken trigger succeeded, want an admission error")
	}

	// Rolled back: the closure's types never landed — no live half-install.
	for _, ident := range []string{
		"samples.substrate.reamde.dev/readinglist/digest",
		"samples.substrate.reamde.dev/readinglist/page",
	} {
		if _, err := ds.KindByRef(ctx, ident); !errors.Is(err, substrate.ErrNotFound) {
			t.Errorf("type %s present after a rolled-back install: %v", ident, err)
		}
	}
	// And none of the delivery wiring — good triggers included — committed.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/trigger"}},
		First:  50,
	})
	if err != nil {
		t.Fatalf("list triggers: %v", err)
	}
	if got := len(page.Records); got != 0 {
		t.Errorf("triggers = %d after a rolled-back install, want 0", got)
	}
}

func TestInstallRefusesNonOwner(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	_, _, err := c.Install(ctx, substrate.FunctionActor("reader.example.com", "reader", "sync"), rlBundleID, ds)
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("non-owner install error = %v, want ErrForbidden", err)
	}
	// Refused before anything is touched: the closure's types never appeared.
	if _, err := ds.KindByRef(ctx, "samples.substrate.reamde.dev/readinglist/digest"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("type present after a refused install: %v", err)
	}
}

// closureVersions reads the stored version of a bundle's owned package and of
// every kind its closure declares, keyed by declaration id.
func closureVersions(t *testing.T, ds substrate.Dataset, b *catalog.Bundle) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	st, err := ds.(bundleStatuser).BundleStatus(ctx, b.ID)
	if err != nil {
		t.Fatalf("bundle status %s: %v", b.ID, err)
	}
	out := map[string]int64{"package " + b.ID: st.Version}
	for _, ref := range b.Closure.Kinds {
		k, err := ds.KindByRef(ctx, ref)
		if err != nil {
			t.Fatalf("kind %s: %v", ref, err)
		}
		out["kind "+ref] = k.Version
	}
	return out
}

// assertVersionsKept fails for every declaration whose version moved.
func assertVersionsKept(t *testing.T, step string, before, after map[string]int64) {
	t.Helper()
	for id, v := range before {
		if after[id] != v {
			t.Errorf("%s: %s version = %d, want %d", step, id, after[id], v)
		}
	}
}

// Taking an unchanged PROVIDER again is a no-op (issue #643): the closure
// equals the stored one, so the install appends nothing to the changelog and
// every declaration keeps the version the first install landed, which is the
// shipped one. A tool may install on every run to stay current.
func TestReinstallingAnUnchangedProviderWritesNothing(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	const id = "providers.substrate.reamde.dev/whoop"
	b, ok := c.ByID(id)
	if !ok {
		t.Fatalf("the shipped catalog no longer carries %s", id)
	}
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	first := closureVersions(t, ds, b)
	if got := first["package "+id]; got != b.Version {
		t.Errorf("install 1: package version = %d, want the shipped %d", got, b.Version)
	}
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	for i := 2; i <= 3; i++ {
		if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
		assertVersionsKept(t, fmt.Sprintf("install %d", i), first, closureVersions(t, ds, b))
		after, err := ds.Head(ctx)
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		if after != head {
			t.Errorf("install %d moved the changelog head %+v -> %+v", i, head, after)
		}
	}
}
