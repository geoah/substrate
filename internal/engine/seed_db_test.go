package engine_test

// Ticket B4: the embedded tree is a SEED, not an authority. Creation copies it
// into the repository's changelog once; open reads the repository's own rows back;
// a binary whose tree moved appends the difference as explicit upgrade
// entries, and a repository nobody opens is never touched.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
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

// authorityHeader is where a shipped authority declares itself. Core's header
// sits in core.yaml, because authority.yaml there declares the `authority` KIND.
func authorityHeader(tree, authority string) string {
	if authority == "core.substrate.reamde.dev" {
		return filepath.Join(tree, authority, "core.yaml")
	}
	return filepath.Join(tree, authority, "authority.yaml")
}

// declaredVersion is the version a shipped authority's header carries. Read from
// the TREE rather than written into these tests as a literal: bumping a shipped
// authority is an ordinary change, and it must not break the upgrade suite.
//
// The FIRST document only. core.yaml is a stream — the authority header, then
// actors and traits — and a later document that grows a `version:` must not be
// the one this answers with.
var reAuthorityVersion = regexp.MustCompile(`(?m)^  version: (\S+)$`)

func declaredVersion(t *testing.T, tree, authority string) string {
	t.Helper()
	path := authorityHeader(tree, authority)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	header, _, _ := strings.Cut(string(raw), "\n---")
	m := reAuthorityVersion.FindStringSubmatch(header)
	if m == nil {
		t.Fatalf("%s declares no version in its first document", path)
	}
	return m[1]
}

// bumpGroupVersion rewrites one shipped authority's declared version — the whole
// of what "the binary shipped a newer vocabulary" means. The header document
// alone, for declaredVersion's reason.
func bumpGroupVersion(t *testing.T, tree, authority, to string) {
	t.Helper()
	path := authorityHeader(tree, authority)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	from := declaredVersion(t, tree, authority)
	header, rest, split := strings.Cut(string(raw), "\n---")
	src := strings.Replace(header, "version: "+from, "version: "+to, 1)
	if split {
		src += "\n---" + rest
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
	svc, err := engine.Open(context.Background(), dsn, engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(tree))
	if err != nil {
		t.Fatalf("open the substrate: %v", err)
	}
	return svc
}

// Creation writes the shipped tree into the repository's OWN changelog, as ordinary
// entries under `bundle:core`, and the vocabulary the repository then speaks
// is read back from those rows — not from the tree.
func TestSeedIsWrittenAtCreation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
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
	// empty. (Open still seeds the create-only llmprovider row, which is data.)
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
		"people.substrate.reamde.dev/person", "tasks.substrate.reamde.dev/task",
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
		if v, _ := vocabulary.VersionValue(row.Properties["version"]); v < 1 {
			t.Fatalf("declaration %s %s carries no version", ref.typ, ref.id)
		}
	}
}

// The done-when of B4: a repository created on binary N and opened on binary
// N+1 shows upgrade entries in its changelog and the new vocabulary; opening it
// again writes nothing (convergent, idempotent); and a repository never
// opened again stays untouched.
func TestBootUpgradeAppendsTheDifferenceOnceAndOnlyWhereOpened(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	tree := shippedTree(t)

	// --- binary N: two repositories are created and their seed is the tree.
	svc1 := openTree(t, dsn, tree)
	for _, name := range []string{"opened", "asleep"} {
		if _, err := svc1.CreateRepository(ctx, name, name+".example.com"); err != nil {
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
	bumpGroupVersion(t, tree, "core.substrate.reamde.dev", "99")

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
	if v, _ := vocabulary.VersionValue(authority.Properties["version"]); v != 99 {
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
	if want := declaredVersion(t, shippedTree(t), "core.substrate.reamde.dev"); version != want {
		t.Fatalf("an unopened repository's vocabulary moved to %s, not %s", version, want)
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

// kindDecl is the record kind a kind DECLARATION stores as, spelled once for
// the projection test below.
const kindDecl = "core.substrate.reamde.dev/kind"

// declarationPlanter is the internal-write seam a declaration row is rewritten
// through (vocabularyplant_internal_test.go). No public door offers it: the
// shape these tests plant is one the write path refuses.
type declarationPlanter interface {
	PlantDeclarationRow(ctx context.Context, kindIdent, id string, props map[string]any) error
}

func planter(t *testing.T, ds substrate.Dataset) declarationPlanter {
	t.Helper()
	p, ok := ds.(declarationPlanter)
	if !ok {
		t.Fatal("the dataset does not expose the declaration-planting seam")
	}
	return p
}

// declarationRows lists every declaration row a repository holds, by kind.
func declarationRows(t *testing.T, ds substrate.Dataset) map[string][]*substrate.Record {
	t.Helper()
	out := map[string][]*substrate.Record{}
	for _, kind := range []string{
		"core.substrate.reamde.dev/authority", "core.substrate.reamde.dev/actor",
		"core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/trait",
		"core.substrate.reamde.dev/propertytype", "core.substrate.reamde.dev/recordmapping",
		"core.substrate.reamde.dev/function", "core.substrate.reamde.dev/agent",
		"core.substrate.reamde.dev/bundle",
	} {
		page, err := ds.List(context.Background(), substrate.Query{
			Filter: substrate.Filter{Kinds: []string{kind}}, First: 500,
		})
		if err != nil {
			t.Fatalf("list %s: %v", kind, err)
		}
		if len(page.Records) == 0 {
			continue // this repository holds none of that kind
		}
		out[kind] = page.Records
	}
	return out
}

// plantKindDeclarationsWithout stands the repository up as a binary that never
// declared `prop` on core's `kind` left it: `prop` absent from core's own `kind`
// declaration, absent from every kind declaration ROW because that binary's
// projection never wrote it, and that declaration at `version` — ahead of the
// tree's, so the boot upgrade keeps it. Every other declaration keeps the
// version the tree ships, so the upgrade leaves those alone too.
func plantKindDeclarationsWithout(t *testing.T, ds substrate.Dataset, prop string, version int64) {
	t.Helper()
	p := planter(t, ds)
	rows := declarationRows(t, ds)[kindDecl]
	if len(rows) == 0 {
		t.Fatalf("the repository holds no %s row", kindDecl)
	}
	seen := false
	for _, row := range rows {
		props := map[string]any{}
		for k, v := range row.Properties {
			if k != prop {
				props[k] = v
			}
		}
		if row.ID == kindDecl {
			seen = true
			declared, _ := props["properties"].(map[string]any)
			if _, ok := declared[prop]; !ok {
				t.Fatalf("core's kind declaration does not declare %q to begin with", prop)
			}
			narrowed := map[string]any{}
			for k, v := range declared {
				if k != prop {
					narrowed[k] = v
				}
			}
			props["properties"] = narrowed
			props["version"] = version
		}
		if err := p.PlantDeclarationRow(context.Background(), kindDecl, row.ID, props); err != nil {
			t.Fatalf("plant %s %s: %v", kindDecl, row.ID, err)
		}
	}
	if !seen {
		t.Fatalf("the repository holds no %s declaration of its own", kindDecl)
	}
}

// THE MIXED CASE, and the other half of the projection's registry rule.
//
// A stored declaration at or ahead of the binary's is never downgraded, so the
// upgrade SKIPS it and its definition survives the commit. The rows the same
// upgrade does write are therefore still that declaration's business: validating
// them against the binary's candidate would admit rows the surviving declaration
// rejects, and the repository would open holding rows its own live kind refuses.
// projectionKind resolves a row's kind from the candidate only when this
// projection installs that kind's declaration, so here it resolves the stored
// one and the row does not land.
func TestBootUpgradeHoldsRowsToADeclarationItKeeps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	tree := shippedTree(t)

	svc1 := openTree(t, dsn, tree)
	if _, err := svc1.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}

	// The sibling declaration the upgrade will write, picked from the store: any
	// core kind but `kind` itself whose declaration carries a `description`, which
	// is the property the newer stored `kind` declaration below stops declaring.
	sibling := ""
	for _, row := range declarationRows(t, ds1)[kindDecl] {
		if desc, _ := row.Properties["description"].(string); row.ID != kindDecl && desc != "" {
			sibling = row.ID
			break
		}
	}
	if sibling == "" {
		t.Fatal("no core kind declaration carries a description; the fixture needs one")
	}

	// The repository as a binary AHEAD of this one left it: its `kind` declaration
	// stops declaring `description` and stands at a version the tree cannot touch,
	// and its projection wrote no `description` on any declaration row.
	plantKindDeclarationsWithout(t, ds1, "description", 99)
	// …and ONE sibling declaration with its version deleted, which compares below
	// every version, so the upgrade has something to write into core at all. The
	// row it would project carries `description`.
	stale := mustGet(t, ds1, kindDecl, sibling)
	props := map[string]any{}
	for k, v := range stale.Properties {
		if k != "version" {
			props[k] = v
		}
	}
	if err := planter(t, ds1).PlantDeclarationRow(ctx, kindDecl, sibling, props); err != nil {
		t.Fatalf("plant the stale sibling: %v", err)
	}
	_ = svc1.Close()

	svc2 := openTree(t, dsn, tree)
	t.Cleanup(func() { _ = svc2.Close() })
	_, err = svc2.Dataset(ctx, "geoah")
	// What matters is the invariant below: no row lands that the surviving
	// declaration rejects. Today the projection's refusal is how it holds, so the
	// upgrade fails rather than writing such a row.
	if err == nil {
		t.Fatal("the upgrade admitted a row the declaration it kept does not declare")
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("the refusal must name the undeclared property: %v", err)
	}

	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	var carrying int
	if err := raw.QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND props ? 'description'`,
		kindDecl).Scan(&carrying); err != nil {
		t.Fatal(err)
	}
	if carrying != 0 {
		t.Fatalf("%d declaration rows carry a property the stored declaration does not declare", carrying)
	}
	// And the newer stored declaration stands: nothing was downgraded.
	var version string
	if err := raw.QueryRowContext(ctx,
		`SELECT props->>'version' FROM records WHERE kind = $1 AND id = $1`, kindDecl).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "99" {
		t.Fatalf("the stored declaration version = %q, want 99", version)
	}
}

// Only same-or-newer wins: a binary whose tree went BACKWARDS never rewrites a
// repository's declarations, and never prunes what it stopped shipping.
func TestBootUpgradeNeverDowngrades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	tree := shippedTree(t)
	addShippedType(t, tree, "core.substrate.reamde.dev", "widget", "widgets")
	bumpGroupVersion(t, tree, "core.substrate.reamde.dev", "99")

	svc1 := openTree(t, dsn, tree)
	if _, err := svc1.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	head := maxSeq(t, ds1)
	_ = svc1.Close()

	// The older binary: the widget type is gone from its tree and the
	// authority's version is back to the one the tree ships.
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
	if v, _ := vocabulary.VersionValue(authority.Properties["version"]); v != 99 {
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
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// A user's own kind: theirs to declare.
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		{
			"kind":     "core.substrate.reamde.dev/authority",
			"metadata": map[string]any{"id": "mine.example.com"},
			"data":     map[string]any{"version": 1},
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
