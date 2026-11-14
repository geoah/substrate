package engine

// Conversion step 4 end to end: a repository whose history spells `body` as
// the dialects before 4 did, restored from its archive into a fresh database,
// converts on import, folds with its body among its properties, is found by a
// word in its body as it was before, and loses a body its kind never
// declared, which a read never served. A record of a quarantined package
// keeps such a body, as a listing served it, until its kind is usable again,
// and loses it then.
//
// This binary writes no line in dialect 3, so the test writes the repository
// here under the chain below step 4, spells its lines back in the table as
// dialect 3 did (spellBodyAsDialect3), the inverse of the step, and archives
// them: the conversion must give back exactly the lines this binary wrote.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	bodyPackage = "bodyconv.test.dev/letters"
	bodyLetter  = bodyPackage + "/letter"
	bodyMemo    = bodyPackage + "/memo"
	// bodyArchive is the package quarantined at the conversion's first open.
	bodyArchive = "bodyconv.test.dev/archive"
	bodyNote    = bodyArchive + "/note"
)

// bodyArchiveDocs declares the archive package: a note with no body.
func bodyArchiveDocs() []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(bodyArchive, 0),
		vocabulary.KindManifest(bodyArchive, map[string]any{"singular": "note"}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}
}

// spellBodyAsDialect3 rewrites a record effect's body the way the dialects
// before 4 carried it: a value out of `set` into `delta.body`, a clear out of `del`
// as `""`. orphan, when set, is a body written onto the delta as a binary
// before decision 0041 wrote one onto any kind.
func spellBodyAsDialect3(t *testing.T, e *changelogfile.Entry, orphan string) bool {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	var effects []map[string]json.RawMessage
	if err := json.Unmarshal(payload[foldPayloadKey], &effects); err != nil {
		return false
	}
	spelled := false
	for _, eff := range effects {
		if string(eff["kind"]) != `"record"` || eff["delta"] == nil {
			continue
		}
		var delta map[string]json.RawMessage
		if err := json.Unmarshal(eff["delta"], &delta); err != nil {
			t.Fatal(err)
		}
		set := map[string]json.RawMessage{}
		if raw, ok := delta["set"]; ok {
			_ = json.Unmarshal(raw, &set)
		}
		var del []string
		if raw, ok := delta["del"]; ok {
			_ = json.Unmarshal(raw, &del)
		}
		switch {
		case set["body"] != nil:
			delta["body"] = set["body"]
			delete(set, "body")
		case slices.Contains(del, "body"):
			delta["body"] = json.RawMessage(`""`)
			del = slices.DeleteFunc(del, func(s string) bool { return s == "body" })
		case orphan != "":
			delta["body"], _ = json.Marshal(orphan)
		default:
			continue
		}
		delete(delta, "set")
		delete(delta, "del")
		if len(set) > 0 {
			delta["set"], _ = json.Marshal(set)
		}
		if len(del) > 0 {
			delta["del"], _ = json.Marshal(del)
		}
		eff["delta"], _ = json.Marshal(delta)
		spelled = true
	}
	if !spelled {
		return false
	}
	payload[foldPayloadKey], _ = json.Marshal(effects)
	raw, _ := json.Marshal(payload)
	canon, err := changelogfile.CanonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	e.Payload = canon
	return true
}

// chainBelowBody is the chain a source is written under: every step this
// binary carries but step 4, so the source is in dialect 3.
func chainBelowBody() Option {
	return func(o *options) {
		o.conversion = slices.DeleteFunc(slices.Clone(conversionSteps), func(s conversionStep) bool { return s.Dialect >= 4 })
	}
}

func TestABodyInAHistoryFromBeforeDialectFourConvertsAndIsFoundAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := convertSource{dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t)}
	src.archive = filepath.Join(src.root, "archive")
	svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)),
		WithArchiveInterval(time.Hour), chainBelowBody())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	info, err := svc.CreateRepository(ctx, src.repo)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	src.id = info.ID
	ds, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(bodyPackage, 0),
		vocabulary.KindManifest(bodyPackage, map[string]any{"singular": "letter"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"body": map[string]any{"type": "text"},
			},
		}),
		vocabulary.KindManifest(bodyPackage, map[string]any{"singular": "memo"}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, bodyArchiveDocs()); err != nil {
		t.Fatalf("install the archive: %v", err)
	}
	put := func(kind, id string, props map[string]any) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: fid(id), Properties: props}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	put(bodyLetter, "falcon", map[string]any{"name": "Falcon", "body": "the peregrine falcon dives"})
	put(bodyLetter, "owl", map[string]any{"name": "Owl", "body": "an owl hunts at dusk"})
	put(bodyMemo, "orphan", map[string]any{"name": "Left over"})
	put(bodyNote, "kept", map[string]any{"name": "Kept while quarantined"})
	if _, err := ds.Patch(ctx, substrate.ActorPerson, bodyLetter, fid("falcon"), substrate.PatchInput{
		Properties: map[string]any{"body": "the peregrine falcon stoops and dives"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, bodyLetter, fid("owl"), substrate.PatchInput{
		Properties: map[string]any{"body": nil},
	}); err != nil {
		t.Fatal(err)
	}
	// The history as this binary wrote it, then spelled in the table as the
	// dialects before 4 wrote it, each row with its own checksum again, and
	// archived so: the copy a release before this one leaves.
	want := walkedHistory(ctx, t, ds.(*dataset).db)
	spelled, orphanSeq, keptSeq := 0, int64(0), int64(0)
	for _, w := range want {
		e := w
		orphan := ""
		if e.Kind == bodyMemo && e.Op == string(substrate.OpPut) {
			orphan, orphanSeq = "a word written before 0041", e.Seq
		}
		if e.Kind == bodyNote && e.Op == string(substrate.OpPut) {
			orphan, keptSeq = "kept through the quarantine", e.Seq
		}
		if !spellBodyAsDialect3(t, &e, orphan) {
			continue
		}
		spelled++
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rawDBOf(t, src.dsn).ExecContext(ctx, `UPDATE changelog SET payload = $3::jsonb, hash = $4 WHERE repository = $1 AND seq = $2`,
			src.id, e.Seq, []byte(e.Payload), sum[:]); err != nil {
			t.Fatal(err)
		}
	}
	if spelled != 6 || orphanSeq == 0 || keptSeq == 0 {
		t.Fatalf("spelled %d lines as dialect 3 (orphans at %d and %d), want the three bodies, the clear and the two orphans", spelled, orphanSeq, keptSeq)
	}
	if err := ds.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatalf("archive at dialect 3: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	restored := filepath.Join(t.TempDir(), "archive")
	if err := copyTree(src.archive, restored); err != nil {
		t.Fatal(err)
	}
	restoredDSN, restoredRoot := MigratedDSN(t), t.TempDir()
	reopen := func() *service {
		t.Helper()
		svc, err := OpenForTest(t, ctx, restoredDSN, WithDataRoot(restoredRoot), WithArchive(archiveDir(t, restored)),
			WithArchiveInterval(time.Hour))
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		return svc.(*service)
	}
	svc2 := reopen()
	// Before the first open, the archive's stored declaration stops loading
	// under this binary, and no open has marked its package quarantined yet:
	// the load at this very open is the first to refuse it, after repository
	// migration 11 has run.
	owner, err := sql.Open("pgx", restoredDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if _, err := owner.ExecContext(ctx, `UPDATE records SET props = jsonb_set(props, '{properties,name,type}', '"no-such-type"')
		WHERE repository = $1 AND kind = $2 AND id = $3`, src.id, kindKind, bodyNote); err != nil {
		t.Fatal(err)
	}
	quarantined := func() bool {
		t.Helper()
		var flagged sql.NullBool
		if err := owner.QueryRowContext(ctx, `SELECT (props ->> $4)::boolean FROM records WHERE repository = $1 AND kind = $2 AND id = $3`,
			src.id, kindPackage, bodyArchive, propPackageQuarantined).Scan(&flagged); err != nil {
			t.Fatal(err)
		}
		return flagged.Bool
	}
	if quarantined() {
		t.Fatal("the archive package is marked quarantined before the open that refuses it")
	}
	d2, err := svc2.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	ds2 := d2.(*dataset)
	if stamp, err := readChangelogDialect(ctx, ds2.db); err != nil || stamp != maxChangelogDialect {
		t.Fatalf("stamp %d (%v), want %d", stamp, err, maxChangelogDialect)
	}

	// Every line converts back to what this binary wrote, but the orphan's,
	// which carries its body among its properties now.
	converted := walkedHistory(ctx, t, ds2.db)
	for _, e := range converted {
		if e.Seq > int64(len(want)) {
			continue
		}
		w := want[e.Seq-1]
		// The table prints a payload as jsonb does, with a space after each
		// colon and comma.
		var compact bytes.Buffer
		if err := json.Compact(&compact, e.Payload); err != nil {
			t.Fatalf("seq %d: %v", e.Seq, err)
		}
		switch e.Seq {
		case keptSeq:
			if !bytes.Contains(compact.Bytes(), []byte(`"body":"kept through the quarantine"`)) {
				t.Errorf("seq %d: the quarantined record's body is not among its properties: %s", e.Seq, e.Payload)
			}
		case orphanSeq:
			if !bytes.Contains(compact.Bytes(), []byte(`"set":{"body":"a word written before 0041"`)) {
				t.Errorf("seq %d: the orphan's body is not among its properties: %s", e.Seq, e.Payload)
			}
		default:
			if !bytes.Equal(e.Payload, w.Payload) {
				t.Errorf("seq %d converted to\n%s\nthis binary wrote\n%s", e.Seq, e.Payload, w.Payload)
			}
		}
	}

	falcon, err := ds2.Get(ctx, bodyLetter, fid("falcon"))
	if err != nil {
		t.Fatal(err)
	}
	if falcon.Properties["body"] != "the peregrine falcon stoops and dives" {
		t.Fatalf("the falcon's body is %v after the conversion", falcon.Properties["body"])
	}
	owl, err := ds2.Get(ctx, bodyLetter, fid("owl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, has := owl.Properties["body"]; has {
		t.Fatalf("the owl's cleared body came back: %v", owl.Properties["body"])
	}

	// A record found by a word in its body is found by it after.
	res, err := ds2.Search(ctx, substrate.SearchInput{Q: "stoops", Mode: substrate.SearchLexical, Kinds: []string{bodyLetter}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Record.ID != fid("falcon") {
		t.Fatalf("a word of the converted body found %v, want the falcon", res.Hits)
	}

	// The orphan's body is gone from the record, as a read never served it,
	// by a write of the system's the history keeps.
	orphan, err := ds2.Get(ctx, bodyMemo, fid("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	if _, has := orphan.Properties["body"]; has {
		t.Fatalf("a body its kind never declared is served after the conversion: %v", orphan.Properties["body"])
	}
	res, err = ds2.Search(ctx, substrate.SearchInput{Q: "0041", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("the orphan's body is searchable: %v", res.Hits)
	}
	var dropped int
	if err := rawDB(t, ds2).QueryRowContext(ctx,
		`SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND actor = $3 AND seq > $4`,
		bodyMemo, fid("orphan"), vocabulary.ActorID(substrate.ActorSystem), len(want)).Scan(&dropped); err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("%d system writes dropped the orphan's body, want 1", dropped)
	}

	// The quarantined package's record keeps its body, and a listing serves
	// it as it did before the conversion.
	page, err := ds2.List(ctx, substrate.Query{First: 500})
	if err != nil {
		t.Fatal(err)
	}
	var kept *substrate.Record
	for _, r := range page.Records {
		if r.ID == fid("kept") {
			kept = r
		}
	}
	if kept == nil || kept.Properties["body"] != "kept through the quarantine" {
		t.Fatalf("the quarantined package's record lists as %+v, want its body kept", kept)
	}
	if !quarantined() {
		t.Fatal("the open did not quarantine the archive package it refused")
	}

	// A second open does nothing: migration 11 ran once, the refused package
	// keeps its body, and the history is as the first open left it.
	if err := svc2.Close(); err != nil {
		t.Fatal(err)
	}
	svc2 = reopen()
	t.Cleanup(func() { _ = svc2.Close() })
	d2, err = svc2.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open the restored repository again: %v", err)
	}
	ds2 = d2.(*dataset)
	if again := walkedHistory(ctx, t, ds2.db); len(again) != len(converted) {
		t.Fatalf("the second open wrote %d lines, want none", len(again)-len(converted))
	}
	page, err = ds2.List(ctx, substrate.Query{First: 500})
	if err != nil {
		t.Fatal(err)
	}
	kept = nil
	for _, r := range page.Records {
		if r.ID == fid("kept") {
			kept = r
		}
	}
	if kept == nil || kept.Properties["body"] != "kept through the quarantine" {
		t.Fatalf("after a second open the quarantined package's record lists as %+v, want its body kept", kept)
	}
	// Repaired, its kind is usable again, and a body that kind does not
	// declare is dropped then, by a write of the system's.
	if _, err := ds2.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, bodyArchiveDocs()); err != nil {
		t.Fatalf("re-install the archive: %v", err)
	}
	note, err := ds2.Get(ctx, bodyNote, fid("kept"))
	if err != nil {
		t.Fatal(err)
	}
	if _, has := note.Properties["body"]; has {
		t.Fatalf("a body its usable kind does not declare is served after the repair: %v", note.Properties["body"])
	}
	if err := rawDB(t, ds2).QueryRowContext(ctx,
		`SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND actor = $3`,
		bodyNote, fid("kept"), vocabulary.ActorID(substrate.ActorSystem)).Scan(&dropped); err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("%d system writes dropped the repaired record's body, want 1", dropped)
	}
}

// A package this binary refuses for the first time at the open that runs
// migration 11 keeps its bodies through it: the migration holds a kind
// usable only where the open's own admission admits its package, and no mark
// says so yet. A later binary that admits it again lifts the quarantine at
// its open, which makes its kinds usable, and a body migration 11 kept for
// them goes then, once.
func TestABodyKeptForARefusedPackageGoesWhenAnOpenLiftsTheQuarantine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, name := MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	open := func(opts ...Option) *service {
		t.Helper()
		svc, err := OpenForTest(t, ctx, dsn, append([]Option{WithDataRoot(root)}, opts...)...)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc.(*service)
	}
	s := open()
	info, err := s.CreateRepository(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, bodyArchiveDocs()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: bodyNote, ID: fid("kept"), Properties: map[string]any{"name": "Kept"}}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// What a converted history leaves on a record whose kind declares no
	// body, with migration 11 not yet run and no quarantine mark anywhere.
	owner, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if _, err := owner.ExecContext(ctx,
		`UPDATE records SET props = props || '{"body": "a word kept through the quarantine"}' WHERE repository = $1 AND kind = '`+bodyNote+`'`,
		info.ID); err != nil {
		t.Fatal(err)
	}
	MakeMigrationPending(t, dsn, name, 11)
	systemWrites := func() int {
		t.Helper()
		var n int
		if err := owner.QueryRowContext(ctx,
			`SELECT count(*) FROM changelog WHERE repository = $1 AND kind = $2 AND record_id = $3 AND actor = $4`,
			info.ID, bodyNote, fid("kept"), vocabulary.ActorID(substrate.ActorSystem)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	listed := func(d substrate.Dataset) *substrate.Record {
		t.Helper()
		page, err := d.List(ctx, substrate.Query{First: 500})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page.Records {
			if r.ID == fid("kept") {
				return r
			}
		}
		t.Fatal("the note is not listed")
		return nil
	}

	// The binary that runs migration 11 refuses the package, at this open
	// and for the first time.
	s = open(WithTestInadmissible(bodyArchive))
	d, err = s.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if kept := listed(d); kept.Properties["body"] != "a word kept through the quarantine" {
		t.Fatalf("a refused package's record lists as %+v after migration 11, want its body kept", kept)
	}
	if n := systemWrites(); n != 0 {
		t.Fatalf("%d system writes touched the refused package's record, want none", n)
	}
	_ = s.Close()

	// A binary that admits it again lifts the quarantine at its open.
	s = open()
	t.Cleanup(func() { _ = s.Close() })
	d, err = s.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	note, err := d.Get(ctx, bodyNote, fid("kept"))
	if err != nil {
		t.Fatal(err)
	}
	if _, has := note.Properties["body"]; has {
		t.Fatalf("a body its kind hides is served once the open lifted the quarantine: %v", note.Properties["body"])
	}
	if n := systemWrites(); n != 1 {
		t.Fatalf("%d system writes dropped the body at the lift, want 1", n)
	}
}

// walkedHistory reads a repository's changelog table as the conversion walks
// it: each row's line under the checksum it was stored with (walkedLine), the
// seqs one after another from 1, and every transaction framed as verify.go
// holds it (a txn on every line, never below its own seq, the same on every
// line until the one that ends it, and the table ending outside one). A right
// payload under a wrong checksum fails it.
func walkedHistory(ctx context.Context, t *testing.T, db dbx) []changelogfile.Entry {
	t.Helper()
	var out []changelogfile.Entry
	var openTxn, after int64
	for {
		page, err := scanChecksumPage(ctx, db, after, 500)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			e, err := walkedLine(row)
			if err != nil {
				t.Fatal(err)
			}
			if e.Seq != after+1 {
				t.Fatalf("seq %d follows seq %d", e.Seq, after)
			}
			switch {
			case e.Txn < e.Seq:
				t.Fatalf("seq %d ends its transaction at %d, before itself", e.Seq, e.Txn)
			case openTxn != 0 && e.Txn != openTxn:
				t.Fatalf("seq %d carries txn %d inside the transaction ending at %d", e.Seq, e.Txn, openTxn)
			case e.Txn == e.Seq:
				openTxn = 0
			default:
				openTxn = e.Txn
			}
			out = append(out, e)
			after = e.Seq
		}
	}
	if openTxn != 0 {
		t.Fatalf("the table ends inside the transaction ending at seq %d", openTxn)
	}
	return out
}
