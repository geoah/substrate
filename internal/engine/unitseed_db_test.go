package engine_test

// The unit seed (decision 0186, unitseed.go): registration writes the shipped
// units and conversions, every open fills what is absent, an owner's edit and
// deletion stand, a shipped record the owner's graph contradicts is skipped
// and reported, and the records travel through a rebuild and an import.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/engine/unitseed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// shippedCounts is how many units and conversions the seed files hold.
func shippedCounts() (units, conversions int) {
	return strings.Count(string(unitseed.Units()), "\nkind: "+vocabulary.KindUnit+"\n"),
		strings.Count(string(unitseed.Conversions()), "\nkind: "+vocabulary.KindConversion+"\n")
}

func unitsOf(t *testing.T, ds substrate.Dataset) substrate.UnitTable {
	t.Helper()
	tab, err := ds.Units(context.Background())
	if err != nil {
		t.Fatalf("units: %v", err)
	}
	return tab
}

func unitByID(tab substrate.UnitTable, id string) (substrate.RepositoryUnit, bool) {
	for _, u := range tab.Units {
		if u.ID == id {
			return u, true
		}
	}
	return substrate.RepositoryUnit{}, false
}

func conversionByID(tab substrate.UnitTable, id string) (substrate.RepositoryConversion, bool) {
	for _, c := range tab.Conversions {
		if c.ID == id {
			return c, true
		}
	}
	return substrate.RepositoryConversion{}, false
}

// seededRepository registers a repository with the seed on or off and
// answers the service, its dataset, the DSN and the data root.
func unitSeededRepository(t *testing.T, seed bool, opts ...engine.Option) (substrate.Service, substrate.Dataset, string, string) {
	t.Helper()
	svc, dsn := newService(t, append([]engine.Option{engine.WithUnitSeed(seed)}, opts...)...)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return svc, ds, dsn, engine.DataRootOf(svc)
}

// reopenSeeded closes svc and opens the same database and data root with the
// seed on, the shape of a restart under a binary that ships it.
func reopenSeeded(t *testing.T, svc substrate.Service, dsn, root string) (substrate.Service, substrate.Dataset) {
	t.Helper()
	_ = svc.Close()
	next := reopenWith(t, dsn, root, engine.WithUnitSeed(true))
	ds, err := next.Dataset(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatalf("reopen the dataset: %v", err)
	}
	return next, ds
}

// Registration writes every shipped unit and conversion in the creation
// transaction, as the seed actor, and the graph converts through them.
func TestRegistrationSeedsTheUnits(t *testing.T) {
	t.Parallel()
	_, ds, _, _ := unitSeededRepository(t, true)
	units, convs := shippedCounts()
	tab := unitsOf(t, ds)
	if len(tab.Units) != units || len(tab.Conversions) != convs || len(tab.Unseeded) != 0 {
		t.Fatalf("seeded %d units and %d conversions, skipped %v; want %d and %d", len(tab.Units), len(tab.Conversions), tab.Unseeded, units, convs)
	}
	if tab.Revision != int64(units+convs) {
		t.Errorf("revision = %d, want one per seeded record, %d", tab.Revision, units+convs)
	}
	if lb, _ := unitByID(tab, "lb"); lb.Code != "[lb_av]" || lb.Root == "" {
		t.Errorf("lb = %+v", lb)
	}
	got, err := ds.ConvertUnit(context.Background(), substrate.UnitConvert{Value: "1", From: "[lb_av]", To: "kg"})
	if err != nil || got.Value != "0.45359237" || !got.Exact || got.Unit != "kg" {
		t.Errorf("1 lb in kg = %+v, %v", got, err)
	}
	for _, c := range changesSince(t, ds, 0) {
		if (c.Kind == vocabulary.KindUnit || c.Kind == vocabulary.KindConversion) && c.Actor != substrate.ActorSeed {
			t.Fatalf("%s %s was written by %s, want the seed", c.Kind, c.RecordID, c.Actor)
		}
	}
}

// A repository registered without the seed receives it at its next open, and
// a second open writes nothing.
func TestEveryOpenFillsAbsentUnits(t *testing.T) {
	t.Parallel()
	svc, ds, dsn, root := unitSeededRepository(t, false)
	if tab := unitsOf(t, ds); len(tab.Units) != 0 {
		t.Fatalf("a repository registered without the seed holds %d units", len(tab.Units))
	}
	_, ds = reopenSeeded(t, svc, dsn, root)
	units, convs := shippedCounts()
	if tab := unitsOf(t, ds); len(tab.Units) != units || len(tab.Conversions) != convs {
		t.Fatalf("after the open: %d units and %d conversions, want %d and %d", len(tab.Units), len(tab.Conversions), units, convs)
	}
}

// Repeated opens of a seeded repository append nothing.
func TestRepeatedOpensSeedNothing(t *testing.T) {
	t.Parallel()
	svc, ds, dsn, root := unitSeededRepository(t, true)
	head := maxSeq(t, ds)
	for range 2 {
		svc, ds = reopenSeeded(t, svc, dsn, root)
		if got := maxSeq(t, ds); got != head {
			t.Fatalf("an open moved the head from %d to %d", head, got)
		}
	}
	if tab := unitsOf(t, ds); len(tab.Unseeded) != 0 {
		t.Errorf("unseeded = %v", tab.Unseeded)
	}
}

// The owner's edit and deletion stand at the next open; an edit another
// writer made is refreshed.
func TestSeedLeavesAnOwnersEditAndDeletion(t *testing.T) {
	t.Parallel()
	svc, ds, dsn, root := unitSeededRepository(t, true)
	ctx := context.Background()
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindUnit, "lb", substrate.PatchInput{Properties: map[string]any{"name": "Pfund"}}); err != nil {
		t.Fatalf("the owner's edit: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "oz-kg", substrate.DeleteInput{}); err != nil {
		t.Fatalf("the owner's deletion: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorSystem, vocabulary.KindUnit, "g", substrate.PatchInput{Properties: map[string]any{"name": "GRAM"}}); err != nil {
		t.Fatalf("another writer's edit: %v", err)
	}
	_, ds = reopenSeeded(t, svc, dsn, root)
	tab := unitsOf(t, ds)
	if lb, _ := unitByID(tab, "lb"); lb.Name != "Pfund" {
		t.Errorf("lb's name = %q, want the owner's Pfund", lb.Name)
	}
	if _, ok := conversionByID(tab, "oz-kg"); ok {
		t.Error("the conversion the owner deleted came back")
	}
	if g, _ := unitByID(tab, "g"); g.Name != "gram" {
		t.Errorf("g's name = %q, want the shipped gram back", g.Name)
	}
	if len(tab.Unseeded) != 0 {
		t.Errorf("unseeded = %v", tab.Unseeded)
	}
}

// A shipped record the owner's graph contradicts is skipped and listed, the
// conversions that name a skipped unit with it, and the open succeeds with
// the rest seeded.
func TestSeedSkipsAConflictingShippedEdge(t *testing.T) {
	t.Parallel()
	svc, ds, dsn, root := unitSeededRepository(t, false)
	for _, in := range []substrate.PutInput{
		// The owner's own pound answers the shipped lb's code.
		{Kind: vocabulary.KindUnit, ID: "pound", Properties: map[string]any{"name": "pound", "code": "[lb_av]"}},
		// The owner's kg and g, joined by a factor the shipped g-kg contradicts.
		{Kind: vocabulary.KindUnit, ID: "kg", Properties: map[string]any{"name": "kilogram", "code": "kg"}},
		{Kind: vocabulary.KindUnit, ID: "g", Properties: map[string]any{"name": "gram", "code": "g"}},
		{Kind: vocabulary.KindConversion, ID: "gram-kg", Properties: map[string]any{
			"from": map[string]any{"ref": vocabulary.KindUnit + "/g"}, "to": map[string]any{"ref": vocabulary.KindUnit + "/kg"}, "factor": "0.002",
		}},
	} {
		mustPut(t, ds, substrate.ActorAPI, in)
	}
	_, ds = reopenSeeded(t, svc, dsn, root)
	tab := unitsOf(t, ds)
	skipped := map[string]string{}
	for _, s := range tab.Unseeded {
		skipped[s.Kind+"/"+s.ID] = s.Reason
	}
	if r := skipped[vocabulary.KindUnit+"/lb"]; !strings.Contains(r, "[lb_av]") {
		t.Errorf("lb skipped for %q, want the spelling named", r)
	}
	if r := skipped[vocabulary.KindConversion+"/g-kg"]; r == "" {
		t.Errorf("g-kg was not skipped: %v", tab.Unseeded)
	}
	if r := skipped[vocabulary.KindConversion+"/lb-kg"]; !strings.Contains(r, "lb") {
		t.Errorf("lb-kg skipped for %q, want the missing unit named", r)
	}
	if _, ok := unitByID(tab, "oz"); !ok {
		t.Error("the rest of the seed did not land: no oz")
	}
	if _, ok := conversionByID(tab, "oz-kg"); !ok {
		t.Error("the rest of the seed did not land: no oz-kg")
	}
	if c, _ := conversionByID(tab, "gram-kg"); c.Factor != "0.002" {
		t.Errorf("the owner's conversion = %+v", c)
	}
}

// installPointers declares one kind whose `to` references a unit record, a
// use of that unit the guard reads from refs.
func installPointers(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	const pkg = "units.test.dev/pointers"
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "pointers", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "pointer"}, map[string]any{
				"properties": map[string]any{"to": map[string]any{"type": "reference", "kind": vocabulary.KindUnit}},
			}),
		},
	}); err != nil {
		t.Fatalf("install the pointer kind: %v", err)
	}
	return pkg + "/pointer"
}

// A writer other than the owner retargets the shipped oz-kg to two custom
// units that records then use. The next open's refresh of oz-kg would split
// them, so it is skipped and listed, open after open, while the rest of the
// seed lands: a deleted shipped unit and its conversion come back and an
// edited one is repaired.
func TestSeedSkipsARefreshThatSplitsUsedUnits(t *testing.T) {
	t.Parallel()
	svc, ds, dsn, root := unitSeededRepository(t, true)
	pointer := installPointers(t, ds)
	ctx := context.Background()
	unit := func(id string) map[string]any { return map[string]any{"ref": vocabulary.KindUnit + "/" + id} }
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: vocabulary.KindUnit, ID: "widget", Properties: map[string]any{"name": "widget"}})
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: vocabulary.KindUnit, ID: "gadget", Properties: map[string]any{"name": "gadget"}})
	mustPut(t, ds, substrate.ActorSystem, substrate.PutInput{Kind: vocabulary.KindConversion, ID: "oz-kg", Properties: map[string]any{
		"from": unit("widget"), "to": unit("gadget"), "factor": "2",
	}})
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: pointer, ID: "w", Properties: map[string]any{"to": unit("widget")}})
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: pointer, ID: "g", Properties: map[string]any{"to": unit("gadget")}})
	for _, ref := range []struct{ kind, id string }{{vocabulary.KindConversion, "st-kg"}, {vocabulary.KindUnit, "st"}} {
		if _, err := ds.Delete(ctx, substrate.ActorSystem, ref.kind, ref.id, substrate.DeleteInput{}); err != nil {
			t.Fatalf("another writer deletes %s: %v", ref.id, err)
		}
	}
	mustPatch(t, ds, substrate.ActorSystem, vocabulary.KindUnit, "g", substrate.PatchInput{Properties: map[string]any{"name": "GRAM"}})

	for open := range 2 {
		svc, ds = reopenSeeded(t, svc, dsn, root)
		tab := unitsOf(t, ds)
		if c, _ := conversionByID(tab, "oz-kg"); c.From != "widget" || c.To != "gadget" {
			t.Errorf("open %d: oz-kg = %+v, want it left from widget to gadget", open, c)
		}
		if len(tab.Unseeded) != 1 || tab.Unseeded[0].ID != "oz-kg" || !strings.Contains(tab.Unseeded[0].Reason, "apart") {
			t.Errorf("open %d: unseeded = %+v, want oz-kg alone, naming the split", open, tab.Unseeded)
		}
		if _, ok := unitByID(tab, "st"); !ok {
			t.Errorf("open %d: the deleted shipped unit st did not come back", open)
		}
		if _, ok := conversionByID(tab, "st-kg"); !ok {
			t.Errorf("open %d: the deleted shipped conversion st-kg did not come back", open)
		}
		if g, _ := unitByID(tab, "g"); g.Name != "gram" {
			t.Errorf("open %d: g's name = %q, want the shipped gram back", open, g.Name)
		}
	}
}

// installHolds declares one kind holding a quantity, for the tests here that
// store a value.
func installHolds(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	const pkg = "units.test.dev/holds"
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "holds", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "hold"}, map[string]any{
				"properties": map[string]any{"weight": map[string]any{"type": "quantity"}},
			}),
		},
	}); err != nil {
		t.Fatalf("install the hold kind: %v", err)
	}
	return pkg + "/hold"
}

// A rebuild derives the unit uses and the revision again, the same rows, and
// the guard reads them as before.
func TestUnitUsesSurviveARebuild(t *testing.T) {
	t.Parallel()
	svc, ds, _, _ := unitSeededRepository(t, true)
	hold := installHolds(t, ds)
	ctx := context.Background()
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: hold, ID: "h", Properties: map[string]any{
		"weight": map[string]any{"value": "150", "unit": "[lb_av]"},
	}})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "oz-kg", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete an unused conversion: %v", err)
	}
	before := foldOf(t, ds)
	if !strings.Contains(string(before), `"unit": "lb"`) || !strings.Contains(string(before), `"unit_graph"`) {
		t.Fatalf("the fold snapshot lacks the unit tables:\n%.2000s", before)
	}
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(after) != string(before) {
		t.Fatalf("the rebuilt fold differs\n%s", firstDifference(before, after))
	}
	_, err := ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-kg", substrate.DeleteInput{})
	if err != nil {
		t.Fatalf("delete lb-kg, which no used unit needs: %v", err)
	}
	_, err = ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindUnit, "lb", substrate.DeleteInput{})
	if !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("delete a used unit after the rebuild: err = %v, want a conflict", err)
	}
}

// The repository directory carries the seeded units: a boot import into an
// empty database comes back with the same fold, and its open seeds nothing.
func TestExportAndImportKeepTheSeededUnits(t *testing.T) {
	t.Parallel()
	svc, ds, _, root := unitSeededRepository(t, true)
	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, vocabulary.KindUnit, "lb", substrate.PatchInput{Properties: map[string]any{"name": "Pfund"}}); err != nil {
		t.Fatalf("the owner's edit: %v", err)
	}
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	svc2 := reopenWith(t, engine.MigratedDSN(t), root2, engine.WithUnitSeed(true))
	ds2, err := svc2.Dataset(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the imported fold differs\n%s", firstDifference(before, after))
	}
	if got := maxSeq(t, ds2); got != head {
		t.Errorf("the imported repository's open moved the head from %d to %d", head, got)
	}
	if lb, _ := unitByID(unitsOf(t, ds2), "lb"); lb.Name != "Pfund" {
		t.Errorf("lb's name after the import = %q", lb.Name)
	}
}

// seedState is everything the seed wrote in one repository, as text a second
// repository's seed can be compared with: the unit and conversion rows, their
// managers, refs and unit uses, the graph revision, and their changelog
// entries numbered from the seed's first. Every timestamp is held to the
// transaction's one instant instead of compared.
func seedState(t *testing.T, svc substrate.Service, repo string) string {
	t.Helper()
	db, err := engine.SharedScopedDB(svc, repo)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{vocabulary.KindUnit, vocabulary.KindConversion}
	var out strings.Builder
	dump := func(what, q string) {
		t.Helper()
		rows, err := db.QueryContext(context.Background(), q, kinds)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		defer func() { _ = rows.Close() }()
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			out.WriteString(what)
			for _, v := range vals {
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				out.WriteString(fmt.Sprintf("|%v", v))
			}
			out.WriteByte('\n')
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatalf("%s: the seed wrote none", what)
		}
	}
	dump("record", `SELECT kind, id, title, body, states::text, props::text, labels::text, finalizers::text, fts::text,
		version, kind_version, deleted_at IS NULL, created_at = updated_at,
		created_at = (SELECT ts FROM changelog c WHERE c.kind = r.kind AND c.record_id = r.id)
		FROM records r WHERE kind = ANY($1::text[]) ORDER BY kind, id`)
	dump("manager", `SELECT m.record_kind, m.record_id, m.property, m.actor, m.tier, m.principal, m.updated_at = r.created_at
		FROM property_managers m JOIN records r ON r.kind = m.record_kind AND r.id = m.record_id
		WHERE m.record_kind = ANY($1::text[]) ORDER BY 1, 2, 3`)
	dump("ref", `SELECT src_kind, src, property, path, ord, dst_kind, dst, props::text FROM refs
		WHERE src_kind = ANY($1::text[]) ORDER BY 1, 2, 3, 4, 5`)
	dump("revision", `SELECT revision, cardinality($1::text[]) FROM unit_graph`)
	dump("entry", `SELECT seq - (SELECT min(seq) FROM changelog WHERE kind = ANY($1::text[])), actor, principal, op, kind, record_id,
		payload::text, caused_by, hash IS NOT NULL, txn = (SELECT max(seq) FROM changelog)
		FROM changelog WHERE kind = ANY($1::text[]) ORDER BY seq`)
	var uses int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM unit_uses WHERE kind = ANY($1::text[])`, kinds).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&out, "uses|%d\n", uses)
	return out.String()
}

// A registration takes the seed in one bulk run (bulkwrite.go), and what it
// writes is what the per-record path writes: the same rows, managers, refs,
// unit uses, graph revision and changelog entries, payloads included. A
// rebuild replays the bulk run's entries one effect at a time to the fold it
// left.
func TestBulkSeedMatchesThePerRecordPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := engine.MigratedDSN(t), t.TempDir()
	// One server at a time per database: the bulk repository's, then the
	// per-record one's, over one data root.
	paths := &seedPaths{}
	register := func(repo string, opts ...engine.Option) substrate.Service {
		t.Helper()
		svc := reopenWith(t, dsn, root, append([]engine.Option{engine.WithUnitSeed(true), engine.WithTestSeedPath(paths.hook)}, opts...)...)
		if _, err := svc.CreateRepository(ctx, repo); err != nil {
			t.Fatalf("create %s: %v", repo, err)
		}
		return svc
	}
	bulkRepo, singleRepo := testdb.Repository(t), testdb.RepositoryLabel(t)+"1.example.com"
	bulkSvc := register(bulkRepo)
	paths.want(t, "the bulk registration", "")
	bulk := seedState(t, bulkSvc, bulkRepo)
	ds, err := bulkSvc.Dataset(ctx, bulkRepo)
	if err != nil {
		t.Fatal(err)
	}
	units, convs := shippedCounts()
	if tab := unitsOf(t, ds); len(tab.Units) != units || len(tab.Conversions) != convs || tab.Revision != int64(units+convs) {
		t.Fatalf("the bulk seed holds %d units and %d conversions at revision %d", len(tab.Units), len(tab.Conversions), tab.Revision)
	}
	rebuiltEqualsLive(t, bulkSvc, ds, "after the bulk seed")
	_ = bulkSvc.Close()
	singleSvc := register(singleRepo, engine.WithTestPerRecordSeed())
	paths.want(t, "the per-record registration", engine.BulkRefusedOff)
	if single := seedState(t, singleSvc, singleRepo); bulk != single {
		t.Fatalf("the bulk seed differs from the per-record one\n%s", firstDifference([]byte(single), []byte(bulk)))
	}
}

// A repository that holds a unit of the owner's and none of the shipped
// records takes them in one bulk run at its open: a record outside the plan
// does not hold the run back. The open ends with every shipped record and a
// fold a rebuild reproduces. TestASeedOverAHeldRecordGoesOneByOne holds the
// other path.
func TestTheOpenSeedsInBulkBesideAnOwnersUnit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn, root := unitSeededRepository(t, false)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: vocabulary.KindUnit, ID: "furlong", Properties: map[string]any{"name": "furlong"}}); err != nil {
		t.Fatalf("put a unit of the owner's: %v", err)
	}
	_ = svc.Close()
	paths := &seedPaths{}
	svc = reopenWith(t, dsn, root, engine.WithUnitSeed(true), engine.WithTestSeedPath(paths.hook))
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	paths.want(t, "the open", "")
	units, convs := shippedCounts()
	if tab := unitsOf(t, ds); len(tab.Units) != units+1 || len(tab.Conversions) != convs {
		t.Fatalf("after the open: %d units and %d conversions, want %d and %d", len(tab.Units), len(tab.Conversions), units+1, convs)
	}
	rebuiltEqualsLive(t, svc, ds, "after the open's seed")
}
