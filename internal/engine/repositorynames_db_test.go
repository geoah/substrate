package engine_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A former name holds for 90 days from the rename that retired it (decision
// record 0287): it resolves to its repository, and no registration or rename
// elsewhere takes it. At the moment it lapses it stops resolving and is free,
// both at once. A second rename keeps the first former name to its own
// moment; renaming back makes a former name the name again. Deleting the
// repository frees its name and its former names at once. Each repository is
// registered by a person of its own, whose sign-in name no rename moves, so a
// repository named again after its hold is registered by another person.
func TestAFormerNameHoldsNinetyDaysAndADeletedNameIsFreeAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var clock atomic.Pointer[time.Time]
	at := func(when time.Time) { clock.Store(&when) }
	start := time.Now().UTC().Truncate(time.Second)
	at(start)
	svc, _ := newService(t, engine.WithTestNamesClock(func() time.Time { return *clock.Load() }))
	label := testdb.RepositoryLabel(t)
	name := func(s string) string { return label + "-" + s + ".example.com" }
	people := map[string]string{}
	register := func(n string) (string, error) {
		person := name(fmt.Sprintf("person%d", len(people)))
		if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: person, Repository: n, Password: testPassword, SkipTOTP: true}); err != nil {
			return "", err
		}
		id := idOf(t, svc, n)
		people[id] = person
		return id, nil
	}
	as := func(repository string) substrate.LoginInput {
		return substrate.LoginInput{Identity: people[idOf(t, svc, repository)], Repository: repository, Password: testPassword}
	}
	rename := func(from, to string) substrate.RepositoryNames {
		t.Helper()
		names, err := svc.RenameRepository(ctx, as(from), to)
		if err != nil {
			t.Fatalf("rename %s to %s: %v", from, to, err)
		}
		return names
	}
	resolves := func(n string) string {
		t.Helper()
		ds, err := svc.Dataset(ctx, n)
		if errors.Is(err, substrate.ErrNotFound) {
			return ""
		}
		if err != nil {
			t.Fatalf("open %s: %v", n, err)
		}
		return ds.Repository().ID
	}
	const hold = 90 * 24 * time.Hour

	// The hold, and the moment it ends.
	ada, err := register(name("ada"))
	if err != nil {
		t.Fatal(err)
	}
	got := rename(name("ada"), name("lovelace"))
	if got.ID != ada || got.Name != name("lovelace") || len(got.FormerNames) != 1 ||
		got.FormerNames[0].Name != name("ada") || !got.FormerNames[0].Until.Equal(start.Add(hold)) {
		t.Fatalf("the rename answered %+v, want %s named %s, holding %s until %s", got, ada, name("lovelace"), name("ada"), start.Add(hold))
	}
	// A record of its own, read by the former name's spelling through the
	// dataset that stays open across the hold: its names are read on every
	// fold, so the spelling stops answering when the name lapses.
	open, err := svc.Dataset(ctx, name("lovelace"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(name("lovelace")+"/notes", 1),
		vocabulary.KindManifest(name("lovelace")+"/notes", map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatalf("declare a kind of its own: %v", err)
	}
	note := vocabulary.DerivedMintedID("names", "note")
	if _, err := open.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "~/notes/note", ID: note, Properties: map[string]any{"text": "kept"}}); err != nil {
		t.Fatal(err)
	}
	for _, when := range []time.Time{start, start.Add(hold - time.Second)} {
		at(when)
		if id := resolves(name("ada")); id != ada {
			t.Errorf("at %s the former name resolves to %q, want %s", when, id, ada)
		}
		if _, err := open.Get(ctx, name("ada")+"/notes/note", note); err != nil {
			t.Errorf("at %s the open dataset no longer reads the former name's spelling: %v", when, err)
		}
		if _, err := register(name("ada")); err == nil {
			t.Errorf("at %s another registration took the held former name", when)
		}
	}
	other, err := register(name("other"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameRepository(ctx, as(name("other")), name("ada")); err == nil {
		t.Error("another repository's rename took the held former name")
	}
	at(start.Add(hold))
	if id := resolves(name("ada")); id != "" {
		t.Errorf("at the end of the hold the former name still resolves to %s", id)
	}
	if _, err := open.Get(ctx, name("ada")+"/notes/note", note); err == nil {
		t.Error("at the end of the hold the open dataset still reads the former name's spelling")
	}
	if _, err := open.Get(ctx, "~/notes/note", note); err != nil {
		t.Errorf("at the end of the hold the record is not read as ~: %v", err)
	}
	again, err := register(name("ada"))
	if err != nil {
		t.Fatalf("at the end of the hold the name is not free: %v", err)
	}
	if again == ada || again == other || resolves(name("ada")) != again {
		t.Errorf("the name registered again is %s, resolving to %s; want a new repository", again, resolves(name("ada")))
	}

	// A second rename, then back.
	at(start)
	bo, err := register(name("bo"))
	if err != nil {
		t.Fatal(err)
	}
	rename(name("bo"), name("bob"))
	at(start.Add(10 * 24 * time.Hour))
	got = rename(name("bob"), name("robert"))
	if len(got.FormerNames) != 2 || got.FormerNames[0].Name != name("bob") || got.FormerNames[1].Name != name("bo") ||
		!got.FormerNames[0].Until.Equal(start.Add(10*24*time.Hour+hold)) || !got.FormerNames[1].Until.Equal(start.Add(hold)) {
		t.Errorf("a second rename answered %+v, want bob held from it and bo still to its own moment", got.FormerNames)
	}
	for _, n := range []string{name("bo"), name("bob"), name("robert")} {
		if id := resolves(n); id != bo {
			t.Errorf("%s resolves to %q, want %s", n, id, bo)
		}
	}
	at(start.Add(20 * 24 * time.Hour))
	got = rename(name("robert"), name("bo"))
	if got.Name != name("bo") || len(got.FormerNames) != 2 || got.FormerNames[0].Name != name("robert") || got.FormerNames[1].Name != name("bob") {
		t.Errorf("renaming back answered %+v, want bo the name again and robert and bob held", got)
	}
	at(start.Add(hold + time.Hour))
	if id := resolves(name("bo")); id != bo {
		t.Errorf("after the first hold the name renamed back to resolves to %q, want %s: it is the name, not a former one", id, bo)
	}

	// Deletion frees the name and every former name at once.
	at(start)
	cy, err := register(name("cy"))
	if err != nil {
		t.Fatal(err)
	}
	rename(name("cy"), name("cyrus"))
	if err := svc.DeleteRepository(ctx, as(name("cyrus"))); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, n := range []string{name("cy"), name("cyrus")} {
		if id := resolves(n); id != "" {
			t.Errorf("after the delete %s resolves to %s", n, id)
		}
		if id, err := register(n); err != nil || id == cy {
			t.Errorf("after the delete %s is not free at once: %s, %v", n, id, err)
		}
	}
}

// idOf is the id of the repository a name resolves to.
func idOf(t *testing.T, svc substrate.Service, name string) string {
	t.Helper()
	ds, err := svc.Dataset(context.Background(), name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	return ds.Repository().ID
}

// An import under another name takes a name as a registration does (decision
// record 0287): a former name another repository still holds is refused, and
// one whose hold has lapsed is taken, the hold ending in the same
// transaction, so the name resolves to the imported repository alone.
func TestAnImportUnderAnotherNameTakesALapsedFormerNameOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var clock atomic.Pointer[time.Time]
	at := func(when time.Time) { clock.Store(&when) }
	start := time.Now().UTC().Truncate(time.Second)
	at(start)
	svc, _ := newService(t, engine.WithTestNamesClock(func() time.Time { return *clock.Load() }))
	label := testdb.RepositoryLabel(t)
	name := func(s string) string { return label + "-" + s + ".example.com" }
	const hold = 90 * 24 * time.Hour
	for _, n := range []string{name("ada"), name("stranger")} {
		if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: n, Repository: n, Password: testPassword, SkipTOTP: true}); err != nil {
			t.Fatal(err)
		}
	}
	ada := idOf(t, svc, name("ada"))
	if _, err := svc.RenameRepository(ctx, substrate.LoginInput{Identity: name("ada"), Repository: name("ada"), Password: testPassword}, name("lovelace")); err != nil {
		t.Fatal(err)
	}
	// The stranger's archive, kept past its deletion: the explicit import
	// is the way back.
	ds, err := svc.Dataset(ctx, name("stranger"))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	stranger := ds.Repository().ID
	root := engine.DataRootOf(svc)
	saved := t.TempDir()
	copyDir(t, root+"/archive", saved)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{Identity: name("stranger"), Repository: name("stranger"), Password: testPassword}); err != nil {
		t.Fatal(err)
	}
	copyDir(t, saved, root+"/archive")
	listed, err := engine.ListArchiveIDs(ctx, svc, stranger)
	if err != nil || len(listed) == 0 {
		t.Fatalf("the stranger's archives: %v (%v)", listed, err)
	}
	op := svc.(engine.Operator)

	at(start.Add(hold - time.Second))
	if _, err := op.ImportArchive(ctx, stranger, listed[0], name("ada")); err == nil {
		t.Fatal("an import took a former name another repository still holds")
	}
	at(start.Add(hold))
	repo, err := op.ImportArchive(ctx, stranger, listed[0], name("ada"))
	if err != nil {
		t.Fatalf("an import under a lapsed former name: %v", err)
	}
	if repo.ID != stranger || idOf(t, svc, name("ada")) != stranger {
		t.Fatalf("%s resolves to %s after the import, want %s", name("ada"), idOf(t, svc, name("ada")), stranger)
	}
	lovelace, err := svc.Dataset(ctx, name("lovelace"))
	if err != nil {
		t.Fatal(err)
	}
	if info := lovelace.Repository(); info.ID != ada || len(info.FormerNames) != 0 {
		t.Errorf("the former holder reads %+v, want %s with no former name", info, ada)
	}
	// However the clock runs from here, the name is the imported one's.
	at(start)
	if id := idOf(t, svc, name("ada")); id != stranger {
		t.Errorf("with the clock back inside the hold, %s resolves to %s, want %s", name("ada"), id, stranger)
	}
}

// A rename moves no hand (decision record 0259): a function of the
// repository's own writes as `function:~:<package>:<name>`, so its writes
// after a rename link the same actor record as before, and the repository
// holds one actor record for the hand, not one for each name it had.
func TestARenameLeavesOneActorRecordForEachOwnHand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	label := testdb.RepositoryLabel(t)
	before, after := label+"-before.example.com", label+"-after.example.com"
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: before, Repository: before, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest("~/notes", 1),
		vocabulary.KindManifest("~/notes", map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		vocabulary.FunctionManifest("~/notes", "stamp", map[string]any{
			"description": "writes one note",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"~/notes/note"}},
			"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "~/notes/note", "id": input["args"]["id"],
                         "properties": {"text": "stamped"}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("declare the notes: %v", err)
	}
	stamp := func(name, id string) {
		t.Helper()
		d, err := svc.Dataset(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.CallFunction(ctx, substrate.ActorPerson, "~/notes/stamp", map[string]any{"id": id}); err != nil {
			t.Fatalf("stamp under %s: %v", name, err)
		}
	}
	first, second := vocabulary.DerivedMintedID("hands", "first"), vocabulary.DerivedMintedID("hands", "second")
	stamp(before, first)
	if _, err := svc.RenameRepository(ctx, substrate.LoginInput{Identity: before, Repository: before, Password: testPassword}, after); err != nil {
		t.Fatal(err)
	}
	stamp(after, second)

	hand := vocabulary.ActorID(substrate.FunctionActor(vocabulary.Local, "notes", "stamp"))
	db, err := engine.OpenScopedDB(dsn, ds.Repository().ID, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var records int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND props->'function'->>'ref' = $2`,
		vocabulary.CoreKind(vocabulary.DocActor), vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), "~/notes/stamp")).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 1 {
		t.Errorf("the repository holds %d actor records of the stamp after the rename, want one", records)
	}
	for _, id := range []string{first, second} {
		var setBy string
		if err := db.QueryRowContext(ctx, `SELECT set_by FROM property_marks WHERE record_kind = '~/notes/note' AND record_id = $1 AND property = 'text'`, id).Scan(&setBy); err != nil {
			t.Fatalf("the mark of note %s: %v", id, err)
		}
		if setBy != hand {
			t.Errorf("note %s was set by %s, want the stamp's hand %s whatever the repository was called", id, setBy, hand)
		}
	}
}
