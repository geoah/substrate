package engine_test

// Ticket B4: the embedded tree is a SEED, not an authority. Creation copies it
// into the repository's changelog once; open reads the repository's own rows back;
// a binary whose tree moved appends the difference as explicit upgrade
// entries, and a repository nobody opens is never touched.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/kinds"
)

// declarationKinds are the record types a kind DECLARATION stores as — what
// the seed writes and an upgrade appends to.
var declarationKinds = map[string]bool{
	"core.substrate.reamde.dev/authority":     true,
	"core.substrate.reamde.dev/kind":          true,
	"core.substrate.reamde.dev/trait":         true,
	"core.substrate.reamde.dev/propertytype":  true,
	"core.substrate.reamde.dev/recordmapping": true,
	"core.substrate.reamde.dev/function":      true,
	"core.substrate.reamde.dev/agent":         true,
	"core.substrate.reamde.dev/bundle":        true,
	"core.substrate.reamde.dev/actor":         true,
}

// shippedTree copies the embedded schema tree into a directory the test owns,
// so a test can play "binary N" and "binary N+1" against the same database.
func shippedTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// kinds.Seed() and not the directory on disk: its root holds the authority
	// directory, which is the shape the copied tree has to keep — the tests
	// that use it write into `<dir>/<authority>/`.
	if err := os.CopyFS(dir, kinds.Seed()); err != nil {
		t.Fatalf("copy the shipped tree: %v", err)
	}
	return dir
}

// bumpGroupVersion rewrites one shipped authority's declared version — the whole
// of what "the binary shipped a newer vocabulary" means. Core's header sits in
// core.yaml, because authority.yaml there declares the `authority` KIND.
func bumpGroupVersion(t *testing.T, tree, authority, from, to string) {
	t.Helper()
	path := filepath.Join(tree, authority, "authority.yaml")
	if authority == "core.substrate.reamde.dev" {
		path = filepath.Join(tree, authority, "core.yaml")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := strings.Replace(string(raw), "version: "+from, "version: "+to, 1)
	if src == string(raw) {
		t.Fatalf("%s does not declare version %s", path, from)
	}
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// addShippedType drops a new record type into a shipped authority.
func addShippedType(t *testing.T, tree, authority, singular, plural string) {
	t.Helper()
	doc := "kind: core.substrate.reamde.dev/kind\nmetadata:\n  id: " + authority + "/" + singular +
		"\ndata:\n  authority: " + authority + "\n  names:\n    singular: " + singular +
		"\n    plural: " + plural + "\n  displayTemplate: \"{title}\"\n  properties:\n    note:\n      type: string\n"
	if err := os.WriteFile(filepath.Join(tree, authority, singular+".yaml"), []byte(doc), 0o600); err != nil {
		t.Fatalf("add a shipped type: %v", err)
	}
}

func openTree(t *testing.T, dsn, tree string) substrate.Service {
	t.Helper()
	svc, err := engine.Open(context.Background(), dsn, engine.WithKindsDir(tree))
	if err != nil {
		t.Fatalf("open the substrate: %v", err)
	}
	return svc
}

// Creation writes the shipped tree into the repository's OWN changelog, as ordinary
// entries under `bundle:core`, and the vocabulary the repository then speaks
// is read back from those rows — not from the tree.
func TestSeedIsWrittenAtCreation(t *testing.T) {
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The seed is COMPLETE at creation: the changelog's head is already where it
	// will be, before anything has opened the repository.
	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	var atCreation int64
	if err := raw.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&atCreation); err != nil {
		t.Fatal(err)
	}
	if atCreation == 0 {
		t.Fatal("creation wrote no changelog entries")
	}

	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// …and opening it writes no DECLARATION at all: the tree does not
	// re-assert, and the upgrade diff against the binary that seeded it is
	// empty. (Open still seeds the create-only llm tier rows, which are data.)
	for _, ch := range changesSince(t, ds, atCreation) {
		if strings.HasSuffix(ch.Kind, ".core.substrate.reamde.dev") && declarationKinds[ch.Kind] {
			t.Fatalf("opening a freshly seeded repository re-wrote declaration %s %s", ch.Kind, ch.RecordID)
		}
	}

	seeded := 0
	for _, ch := range changesSince(t, ds, 0) {
		if ch.Actor == substrate.ActorSeed {
			seeded++
		}
	}
	if seeded == 0 {
		t.Fatal("creation wrote no seed entries under bundle:core")
	}
	// The shipped kinds are live vocabulary, and they are live because their
	// ROWS say so: the declaration is a record.
	if _, err := ds.KindByRef(ctx, "core.substrate.reamde.dev/trigger"); err != nil {
		t.Fatalf("the seeded trigger kind: %v", err)
	}
	decl, err := ds.Get(ctx, "core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/trigger")
	if err != nil {
		t.Fatalf("the trigger declaration record: %v", err)
	}
	if decl.Properties["version"] == nil || decl.Properties["version"] == "" {
		t.Fatalf("a declaration record carries no version: %v", decl.Properties)
	}
	// THE SEED IS CORE ALONE: the domain vocabulary is a vocabulary bundle a
	// user imports, so a fresh repository holds no authority but core and no
	// kind outside it.
	for _, id := range []string{
		"people.substrate.reamde.dev/person", "tasks.substrate.reamde.dev/task", "media.substrate.reamde.dev/movie",
		"messaging.substrate.reamde.dev/conversation", "calendar.substrate.reamde.dev/calendarevent",
	} {
		if _, err := ds.KindByRef(ctx, id); err == nil {
			t.Fatalf("creation seeded %s — vocabulary is imported, not seeded", id)
		}
	}
	declared, err := ds.Kinds(ctx)
	if err != nil {
		t.Fatalf("kinds: %v", err)
	}
	for _, k := range declared {
		if k.Authority != "core.substrate.reamde.dev" {
			t.Errorf("a freshly created repository speaks %s — only core is seeded", k.Identity)
		}
	}
	// Every declaration kind carries one, not just record types.
	for _, ref := range []struct{ typ, id string }{
		{"core.substrate.reamde.dev/authority", "core.substrate.reamde.dev"},
		{"core.substrate.reamde.dev/trait", "core.substrate.reamde.dev/temporal"},
		{"core.substrate.reamde.dev/actor", "api"},
	} {
		row, err := ds.Get(ctx, ref.typ, ref.id)
		if err != nil {
			t.Fatalf("declaration %s %s: %v", ref.typ, ref.id, err)
		}
		if v, _ := row.Properties["version"].(string); v == "" {
			t.Fatalf("declaration %s %s carries no version", ref.typ, ref.id)
		}
	}
}

// The done-when of B4: a repository created on binary N and opened on binary
// N+1 shows upgrade entries in its changelog and the new vocabulary; opening it
// again writes nothing (convergent, idempotent); and a repository never
// opened again stays untouched.
func TestBootUpgradeAppendsTheDifferenceOnceAndOnlyWhereOpened(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	tree := shippedTree(t)

	// --- binary N: two repositories are created and their seed is the tree.
	svc1 := openTree(t, dsn, tree)
	for _, name := range []string{"opened", "asleep"} {
		if _, err := svc1.CreateRepository(ctx, name); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	dsAsleep, err := svc1.Dataset(ctx, "asleep")
	if err != nil {
		t.Fatal(err)
	}
	asleepHead := maxSeq(t, dsAsleep)
	dsOpened, err := svc1.Dataset(ctx, "opened")
	if err != nil {
		t.Fatal(err)
	}
	head := maxSeq(t, dsOpened)
	if _, err := dsOpened.KindByRef(ctx, "core.substrate.reamde.dev/widget"); err == nil {
		t.Fatal("binary N already ships the widget type")
	}
	_ = svc1.Close()

	// --- binary N+1: the core authority gains a kind and a version.
	addShippedType(t, tree, "core.substrate.reamde.dev", "widget", "widgets")
	bumpGroupVersion(t, tree, "core.substrate.reamde.dev", "v1alpha1", "v1beta1")

	svc2 := openTree(t, dsn, tree)
	ds2, err := svc2.Dataset(ctx, "opened")
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	// The new vocabulary is live…
	if _, err := ds2.KindByRef(ctx, "core.substrate.reamde.dev/widget"); err != nil {
		t.Fatalf("the upgraded vocabulary: %v", err)
	}
	// …and it arrived as EXPLICIT ENTRIES, attributed to the substrate.
	upgrade := changesSince(t, ds2, head)
	if len(upgrade) == 0 {
		t.Fatal("the upgrade wrote no entries")
	}
	sawWidget := false
	for _, ch := range upgrade {
		if ch.Actor != substrate.ActorSystem {
			t.Fatalf("upgrade entry %d is attributed to %q", ch.Seq, ch.Actor)
		}
		if ch.RecordID == "core.substrate.reamde.dev/widget" {
			sawWidget = true
		}
	}
	if !sawWidget {
		t.Fatal("the upgrade entries do not include the new declaration")
	}
	// The stored declarations carry the new version — the diff's own input.
	authority, err := ds2.Get(ctx, "core.substrate.reamde.dev/authority", "core.substrate.reamde.dev")
	if err != nil {
		t.Fatal(err)
	}
	if authority.Properties["version"] != "v1beta1" {
		t.Fatalf("the stored authority version = %v", authority.Properties["version"])
	}
	// Untouched authorities stayed untouched: the diff is per authority, not a re-assert.
	for _, ch := range upgrade {
		if strings.HasSuffix(ch.RecordID, ".people.substrate.reamde.dev") || ch.RecordID == "people.substrate.reamde.dev" {
			t.Fatalf("the upgrade touched an unchanged authority: %s", ch.RecordID)
		}
	}
	afterUpgrade := maxSeq(t, ds2)
	_ = svc2.Close()

	// --- CONVERGENT: the same binary again writes nothing at all.
	svc3 := openTree(t, dsn, tree)
	ds3, err := svc3.Dataset(ctx, "opened")
	if err != nil {
		t.Fatal(err)
	}
	if got := maxSeq(t, ds3); got != afterUpgrade {
		t.Fatalf("a second open on the same binary appended %d entries", got-afterUpgrade)
	}

	// --- A REPOSITORY NEVER OPENED STAYS UNTOUCHED: `asleep` was created on
	// binary N and nothing has opened it since, so its changelog has not moved and
	// its vocabulary is still binary N's.
	rawAsleep, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "asleep"), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rawAsleep.Close() })
	var head2 int64
	if err := rawAsleep.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head2); err != nil {
		t.Fatal(err)
	}
	if head2 != asleepHead {
		t.Fatalf("an unopened repository's changelog moved from %d to %d", asleepHead, head2)
	}
	var version string
	if err := rawAsleep.QueryRowContext(ctx,
		`SELECT props->>'version' FROM records WHERE kind = $1 AND id = $2`,
		"core.substrate.reamde.dev/authority", "core.substrate.reamde.dev").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "v1alpha1" {
		t.Fatalf("an unopened repository's vocabulary moved to %s", version)
	}

	// …and it upgrades the moment it IS opened, from wherever it stood.
	dsAwake, err := svc3.Dataset(ctx, "asleep")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dsAwake.KindByRef(ctx, "core.substrate.reamde.dev/widget"); err != nil {
		t.Fatalf("the sleeper did not upgrade when it opened: %v", err)
	}
	_ = svc3.Close()
}

// Only same-or-newer wins: a binary whose tree went BACKWARDS never rewrites a
// repository's declarations, and never prunes what it stopped shipping.
func TestBootUpgradeNeverDowngrades(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	tree := shippedTree(t)
	addShippedType(t, tree, "core.substrate.reamde.dev", "widget", "widgets")
	bumpGroupVersion(t, tree, "core.substrate.reamde.dev", "v1alpha1", "v2")

	svc1 := openTree(t, dsn, tree)
	if _, err := svc1.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	head := maxSeq(t, ds1)
	_ = svc1.Close()

	// The older binary: the widget type is gone from its tree and the authority's
	// version is back to v1alpha1.
	older := shippedTree(t)
	svc2 := openTree(t, dsn, older)
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open on the older binary: %v", err)
	}
	if got := maxSeq(t, ds2); got != head {
		t.Fatalf("an older binary appended %d entries", got-head)
	}
	authority, err := ds2.Get(ctx, "core.substrate.reamde.dev/authority", "core.substrate.reamde.dev")
	if err != nil {
		t.Fatal(err)
	}
	if authority.Properties["version"] != "v2" {
		t.Fatalf("the stored version was downgraded to %v", authority.Properties["version"])
	}
	// The kind the older tree does not ship is still the repository's: nothing
	// prunes it, and it is still live vocabulary.
	if _, err := ds2.KindByRef(ctx, "core.substrate.reamde.dev/widget"); err != nil {
		t.Fatalf("an older binary pruned a kind the repository holds: %v", err)
	}
}

// The authority chokepoint: shipped declarations are the substrate's to write
// (seed, upgrade, install); the repository's user owns everything else.
func TestDeclarationAuthority(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	// A user's own kind: theirs to declare.
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		{
			"kind":     "core.substrate.reamde.dev/authority",
			"metadata": map[string]any{"id": "mine.example.com"},
			"data":     map[string]any{"version": "v1"},
		},
		{
			"kind":     "core.substrate.reamde.dev/kind",
			"metadata": map[string]any{"id": "mine.example.com/gadget"},
			"data": map[string]any{
				"authority": "mine.example.com",
				"names":     map[string]any{"singular": "gadget", "plural": "gadgets"},
				"properties": map[string]any{
					"label": map[string]any{"type": "string"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("a user may not declare their own kind: %v", err)
	}

	// Shipped vocabulary: refused, for every actor a request can name. An
	// IMPORTED vocabulary bundle is shipped vocabulary too — the binary
	// publishes it, the registry only delivered it — so the chokepoint holds
	// it exactly as it holds core.
	shipped := []map[string]any{
		{
			"kind":     "core.substrate.reamde.dev/kind",
			"metadata": map[string]any{"id": "tasks.substrate.reamde.dev/task"},
			"data": map[string]any{
				"authority": "tasks.substrate.reamde.dev",
				"names":     map[string]any{"singular": "task", "plural": "tasks"},
				"properties": map[string]any{
					"title": map[string]any{"type": "string"},
				},
			},
		},
	}
	for _, actor := range []substrate.Actor{owner, "api", "console"} {
		_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, actor, shipped)
		wantErr(t, err, substrate.ErrForbidden, "actor "+string(actor)+" rewrites shipped vocabulary")
	}
	// And a request cannot dress itself as a substrate path to get around it.
	for _, actor := range []substrate.Actor{substrate.ActorSeed, substrate.BundleActor("core"), substrate.ActorSystem} {
		if !substrate.ReservedActor(actor) {
			t.Fatalf("actor %q is claimable by a request", actor)
		}
	}
}
