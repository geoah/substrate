package catalog_test

// The catalog install proof against a REAL engine: a shipped closure applied
// into a fresh repository lands its types, functions and delivery wiring through
// the same admission path an explicit apply uses, re-install is idempotent,
// and a non-owner is refused before anything is touched.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/kinds"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// operatorOTP is the control plane's base32 TOTP seed (RFC 6238 Appendix B).
const operatorOTP = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

const (
	webBundleAuthority = "web.bundles.substrate.reamde.dev"
	webBundleID        = webBundleAuthority + "/web"
)

// The VOCABULARY bundles the web closure declares against. Repository creation
// seeds core alone now, so a closure that subscribes to
// messaging.substrate.reamde.dev/conversationmessage needs that authority imported first
// — and messaging itself needs people. Importing them is the same verb an
// bundle install is.
var webRequires = []string{"people.substrate.reamde.dev/people", "messaging.substrate.reamde.dev/messaging"}

func importVocabulary(t *testing.T, c *catalog.Catalog, ds substrate.Dataset, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := c.Install(context.Background(), substrate.ActorAPI, id, ds); err != nil {
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
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey("test-cred-key"),
	)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return ds
}

func loadCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(kinds.Bundles())
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return c
}

// Every shipped VOCABULARY bundle installs into one repository, requires
// first: the whole set has to coexist, so a GraphQL-name collision or an
// unresolvable edge between two vocabulary authorities surfaces here rather
// than on a user's substrate.
func TestVocabularyBundlesInstall(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	byAuthority := map[string]*catalog.Bundle{}
	for _, b := range c.Bundles() {
		byAuthority[b.Authority] = b
	}
	installed := map[string]bool{}
	var install func(b *catalog.Bundle)
	install = func(b *catalog.Bundle) {
		if installed[b.ID] {
			return
		}
		installed[b.ID] = true
		for _, req := range b.Requires {
			rb, ok := byAuthority[req]
			if !ok {
				t.Fatalf("%s requires %s, which no shipped bundle owns", b.ID, req)
			}
			install(rb)
		}
		if _, err := c.Install(ctx, substrate.ActorAPI, b.ID, ds); err != nil {
			t.Fatalf("install %s: %v", b.ID, err)
		}
	}
	for _, b := range c.Bundles() {
		if !b.Vocabulary {
			continue
		}
		install(b)
	}
	for _, b := range c.Bundles() {
		if !b.Vocabulary {
			continue
		}
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
	_, err := c.Install(ctx, substrate.ActorAPI, webBundleID, ds)
	if err == nil {
		t.Fatal("installed a closure whose required vocabulary is absent")
	}
	if !strings.Contains(err.Error(), "messaging.substrate.reamde.dev") {
		t.Errorf("refusal does not name the missing authority: %v", err)
	}
	importVocabulary(t, c, ds, webRequires...)

	b, err := c.Install(ctx, substrate.ActorAPI, webBundleID, ds)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if b.ID != webBundleID {
		t.Fatalf("installed %q, want %q", b.ID, webBundleID)
	}

	// The types are present after install.
	for _, ident := range []string{
		"web.bundles.substrate.reamde.dev/config",
		"web.bundles.substrate.reamde.dev/page",
	} {
		if _, err := ds.KindByRef(ctx, ident); err != nil {
			t.Errorf("type %s absent after install: %v", ident, err)
		}
	}

	// The computed status is the truth about what landed: four functions, two
	// types across the owned authority.
	st, err := ds.(bundleStatuser).BundleStatus(ctx, webBundleID)
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
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/trigger"}},
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
	// `bundle:web.bundles.substrate.reamde.dev`, the full authority (record
	// 0025) — not to the owner who asked for them and not to the catalog,
	// which is a source and never an authority.
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{
		Actors: []substrate.Actor{substrate.BundleActor(webBundleAuthority)},
		Kinds:  []string{"core.substrate.reamde.dev/kind"},
	}, 100)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("the install wrote no declaration entries under the bundle's actor")
	}

	// Re-install is the bundle's own whole-authority re-apply: idempotent, no
	// error, and nothing new appears.
	if _, err := c.Install(ctx, substrate.ActorAPI, webBundleID, ds); err != nil {
		t.Fatalf("re-install: %v", err)
	}
	st2, err := ds.(bundleStatuser).BundleStatus(ctx, webBundleID)
	if err != nil {
		t.Fatalf("bundle status after re-install: %v", err)
	}
	if st2.Functions != st.Functions || st2.Kinds != st.Kinds {
		t.Errorf("re-install changed the closure: %+v -> %+v", st, st2)
	}
	page2, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/trigger"}},
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
	importVocabulary(t, loadCatalog(t), ds, webRequires...)

	// The real web closure, copied out with one EXTRA trigger whose callable
	// resolves to nothing — admission fails inside the install transaction.
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "web.bundles.substrate.reamde.dev")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcDir := "../../kinds/web.bundles.substrate.reamde.dev"
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read example dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(bundleDir, e.Name()), raw, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}
	broken := "authority: core.substrate.reamde.dev\n" +
		"type: trigger\n" +
		"metadata: {id: web-broken-on-message}\n" +
		"data:\n" +
		"  properties:\n" +
		"    enabled: true\n" +
		"    source:\n" +
		"      record:\n" +
		"        types: [messaging.substrate.reamde.dev/conversationmessage]\n" +
		"        ops: [create]\n" +
		"    callable: {kind: core.substrate.reamde.dev/function, id: web.bundles.substrate.reamde.dev/doesnotexist}\n"
	if err := os.WriteFile(filepath.Join(bundleDir, "zz-broken.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := catalog.Load(os.DirFS(dir))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}

	if _, err := c.Install(ctx, substrate.ActorAPI, webBundleID, ds); err == nil {
		t.Fatal("install with a broken trigger succeeded, want an admission error")
	}

	// Rolled back: the closure's types never landed — no live half-install.
	for _, ident := range []string{
		"web.bundles.substrate.reamde.dev/config",
		"web.bundles.substrate.reamde.dev/page",
	} {
		if _, err := ds.KindByRef(ctx, ident); !errors.Is(err, substrate.ErrNotFound) {
			t.Errorf("type %s present after a rolled-back install: %v", ident, err)
		}
	}
	// And none of the delivery wiring — good triggers included — committed.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/trigger"}},
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

	_, err := c.Install(ctx, substrate.FunctionActor("reader.bundles.example.com", "sync"), webBundleID, ds)
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("non-owner install error = %v, want ErrForbidden", err)
	}
	// Refused before anything is touched: the closure's types never appeared.
	if _, err := ds.KindByRef(ctx, "web.bundles.substrate.reamde.dev/config"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("type present after a refused install: %v", err)
	}
}
