package engine

// The open's title pass (localtitles.go) on a repository whose titles the
// test spells as the engine spelled them before the local namespace: the
// name at every `~` that stands where a name stood, as a converted
// repository holds them, and the pass's mark taken away.

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// titlesRepository is a repository of its own, and open, which starts the
// engine over it again: the open is what renders the titles.
func titlesRepository(t *testing.T) (*dataset, func() *dataset) {
	t.Helper()
	ds, open, _ := titlesRepositoryWith(t)
	return ds, open
}

// titlesRepositoryWith is titlesRepository with opts on every start, and the
// database's DSN.
func titlesRepositoryWith(t *testing.T, opts ...Option) (*dataset, func() *dataset, string) {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	root := t.TempDir()
	var svc substrate.Service
	created := false
	open := func() *dataset {
		t.Helper()
		if svc != nil {
			_ = svc.Close()
		}
		var err error
		svc, err = OpenForTest(t, ctx, dsn, append([]Option{WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds)}, opts...)...)
		if err != nil {
			t.Fatalf("open the engine: %v", err)
		}
		if !created {
			if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatalf("create the repository: %v", err)
			}
			created = true
		}
		d, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatalf("open the repository: %v", err)
		}
		return d.(*dataset)
	}
	t.Cleanup(func() {
		if svc != nil {
			_ = svc.Close()
		}
	})
	return open(), open, dsn
}

func applyOwn(t *testing.T, ds *dataset, docs ...map[string]any) {
	t.Helper()
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, docs); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

func putOwn(t *testing.T, ds *dataset, kind, id string, props map[string]any) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
		t.Fatalf("put %s/%s: %v", kind, id, err)
	}
}

// asBefore is title as the engine spelled it before the local namespace.
func asBefore(title, name string) string {
	var b strings.Builder
	for i := range len(title) {
		if title[i] == '~' && tildeStands(title, i) {
			b.WriteString(name)
			continue
		}
		b.WriteByte(title[i])
	}
	return b.String()
}

func titleOf(t *testing.T, ds *dataset, kind, id string) string {
	t.Helper()
	var title string
	if err := ds.db.QueryRowContext(context.Background(), `SELECT title FROM records WHERE kind = $1 AND id = $2`, kind, id).Scan(&title); err != nil {
		t.Fatalf("read the title of %s/%s: %v", kind, id, err)
	}
	return title
}

// retitle stores title on the row alone, as a conversion that never edits a
// title leaves it.
func retitle(t *testing.T, ds *dataset, kind, id, title string) {
	t.Helper()
	if _, err := ds.db.ExecContext(context.Background(), `UPDATE records SET title = $3 WHERE kind = $1 AND id = $2`, kind, id, title); err != nil {
		t.Fatalf("retitle %s/%s: %v", kind, id, err)
	}
}

// unmark takes the pass's mark away, so the next open runs it.
func unmark(t *testing.T, ds *dataset) {
	t.Helper()
	res, err := ds.db.ExecContext(context.Background(), `DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3`,
		kindRepository, ds.info.ID, annTitlesRenderedLocal)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("the repository was registered without the title pass's mark (%d)", n)
	}
}

func titlesMarked(t *testing.T, ds *dataset) bool {
	t.Helper()
	var ok bool
	if err := ds.db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3)`,
		kindRepository, ds.info.ID, annTitlesRenderedLocal).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func linesOf(t *testing.T, ds *dataset, kind, id string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(), `SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2`, kind, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A TITLE ITS WRITER GAVE, spelling the name, on a kind that declared a
// template only after the record's last write: the rendering says something
// else, so the title stays as typed and gets no line.
func TestTheTitlePassLeavesATitleItsWriterGave(t *testing.T) {
	t.Parallel()
	ds, open := titlesRepository(t)
	name := ds.name()
	pkg := name + "/notes"
	memo := func(data map[string]any) map[string]any {
		data["properties"] = map[string]any{"topic": map[string]any{"type": "string"}}
		return vocabulary.KindManifest(pkg, map[string]any{"singular": "memo"}, data)
	}
	applyOwn(t, ds, vocabulary.PackageManifest(pkg, 0), memo(map[string]any{}))
	id := vocabulary.DerivedMintedID("notes", "memo")
	typed := "Notes about " + name + "/notes/memo"
	putOwn(t, ds, "~/notes/memo", id, map[string]any{"title": typed, "topic": "~/notes/memo"})
	applyOwn(t, ds, vocabulary.PackageManifest(pkg, 0), memo(map[string]any{"displayTemplate": "{topic}"}))
	lines := linesOf(t, ds, "~/notes/memo", id)
	unmark(t, ds)

	ds = open()
	if got := titleOf(t, ds, "~/notes/memo", id); got != typed {
		t.Errorf("the memo is titled %q, want its writer's %q", got, typed)
	}
	if got := linesOf(t, ds, "~/notes/memo", id); got != lines {
		t.Errorf("the pass wrote %d lines of the memo, want none", got-lines)
	}
	if !titlesMarked(t, ds) {
		t.Error("the pass left no mark")
	}
}

// A STALE TITLE, spelling the name, of a kind whose template changed since
// the record's last write: the rendering is not the title respelled, so it
// stays stale and gets no line.
func TestTheTitlePassLeavesAStaleTitle(t *testing.T) {
	t.Parallel()
	ds, open := titlesRepository(t)
	name := ds.name()
	pkg := name + "/notes"
	note := func(template string) map[string]any {
		return vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"displayTemplate": template,
			"properties":      map[string]any{"label": map[string]any{"type": "string"}},
		})
	}
	applyOwn(t, ds, vocabulary.PackageManifest(pkg, 0), note("{label}"))
	id := vocabulary.DerivedMintedID("notes", "note")
	putOwn(t, ds, "~/notes/note", id, map[string]any{"label": "~/plan"})
	applyOwn(t, ds, vocabulary.PackageManifest(pkg, 0), note("Note: {label}"))
	stale := asBefore(titleOf(t, ds, "~/notes/note", id), name)
	if stale != name+"/plan" {
		t.Fatalf("the note was titled %q at its write", stale)
	}
	retitle(t, ds, "~/notes/note", id, stale)
	lines := linesOf(t, ds, "~/notes/note", id)
	unmark(t, ds)

	ds = open()
	if got := titleOf(t, ds, "~/notes/note", id); got != stale {
		t.Errorf("the note is titled %q, want the stale %q as stored", got, stale)
	}
	if got := linesOf(t, ds, "~/notes/note", id); got != lines {
		t.Errorf("the pass wrote %d lines of the note, want none", got-lines)
	}
}

// chain declares a link kind whose title renders the next link's, and puts
// labels as links, each the next one's predecessor, under ids that sort
// against the chain: the first id renders the second's title, so a round in
// id order reaches each link before the one it reads. It answers the ids
// and each link's title as written, and leaves the titles as before.
func chain(t *testing.T, ds *dataset, labels ...string) (ids, titles []string) {
	t.Helper()
	name := ds.name()
	pkg := name + "/chain"
	applyOwn(t, ds, vocabulary.PackageManifest(pkg, 0), vocabulary.KindManifest(pkg, map[string]any{"singular": "link"}, map[string]any{
		"displayTemplate": "[{label} {next}]",
		"properties": map[string]any{
			"label": map[string]any{"type": "string"},
			"next":  map[string]any{"type": "reference", "kind": pkg + "/link"},
		},
	}))
	for _, l := range labels {
		ids = append(ids, vocabulary.DerivedMintedID("chain", l))
	}
	sort.Strings(ids)
	titles = make([]string, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		props := map[string]any{"label": labels[i]}
		if i+1 < len(ids) {
			props["next"] = map[string]any{"ref": "~/chain/link/" + ids[i+1]}
		}
		putOwn(t, ds, "~/chain/link", ids[i], props)
		titles[i] = titleOf(t, ds, "~/chain/link", ids[i])
	}
	for i, id := range ids {
		before := asBefore(titles[i], name)
		if before == titles[i] {
			t.Fatalf("link %d is titled %q, which spells no name before", i, titles[i])
		}
		retitle(t, ds, "~/chain/link", id, before)
	}
	return ids, titles
}

// FIVE TITLES, each rendering the next one's, in an id order that runs
// against the chain: one round moves one link, from the far end, and the
// pass goes round until all five are moved before it sets its mark.
func TestTheTitlePassFollowsAChainOfTitlesToItsEnd(t *testing.T) {
	t.Parallel()
	ds, open := titlesRepository(t)
	ids, titles := chain(t, ds, "a", "b", "c", "d", "~")
	unmark(t, ds)

	ds = open()
	for i, id := range ids {
		if got := titleOf(t, ds, "~/chain/link", id); got != titles[i] {
			t.Errorf("link %d is titled %q, want %q", i, got, titles[i])
		}
	}
	if !titlesMarked(t, ds) {
		t.Error("the pass left no mark")
	}
}

// ONE LINE PER TITLE. Each link here spells the name itself and through
// every link after it, so a link moves in several rounds; the pass writes
// each title it moved once, and an open after it writes nothing.
func TestTheTitlePassWritesOneLinePerTitleItMoved(t *testing.T) {
	t.Parallel()
	ds, open := titlesRepository(t)
	ids, titles := chain(t, ds, "~ a", "~ b", "~ c")
	lines := make([]int, len(ids))
	for i, id := range ids {
		lines[i] = linesOf(t, ds, "~/chain/link", id)
	}
	unmark(t, ds)

	ds = open()
	for i, id := range ids {
		if got := titleOf(t, ds, "~/chain/link", id); got != titles[i] {
			t.Errorf("link %d is titled %q, want %q", i, got, titles[i])
		}
		if got := linesOf(t, ds, "~/chain/link", id) - lines[i]; got != 1 {
			t.Errorf("the pass wrote %d lines of link %d, want one", got, i)
		}
	}
	var all, again int
	if err := ds.db.QueryRowContext(context.Background(), `SELECT count(*) FROM changelog`).Scan(&all); err != nil {
		t.Fatal(err)
	}
	ds = open()
	if err := ds.db.QueryRowContext(context.Background(), `SELECT count(*) FROM changelog`).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != all {
		t.Errorf("an open after the pass wrote %d lines, want none", again-all)
	}
}

// THE PASS HOLDS ONLY WHAT IT WRITES. Titles that spell the name and do not
// move (a template over a plain text property) are read and let go: between
// the rounds and the writes another connection is granted a row lock on
// each at once, and the pass writes no line of them.
func TestTheTitlePassLocksOnlyWhatItWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var probe atomic.Pointer[func()]
	ds, open, dsn := titlesRepositoryWith(t, WithTestAfterTitleRounds(func() {
		if p := probe.Load(); p != nil {
			(*p)()
		}
	}))
	name := ds.name()
	pkg := name + "/notes"
	applyOwn(t, ds, vocabulary.PackageManifest(pkg, 0), vocabulary.KindManifest(pkg, map[string]any{"singular": "minute"}, map[string]any{
		"displayTemplate": "{text}",
		"properties":      map[string]any{"text": map[string]any{"type": "string"}},
	}))
	var minutes []string
	lines := map[string]int{}
	for i := range 20 {
		id := vocabulary.DerivedMintedID("minutes", strconv.Itoa(i))
		putOwn(t, ds, "~/notes/minute", id, map[string]any{"text": "minutes of " + name})
		minutes = append(minutes, id)
		lines[id] = linesOf(t, ds, "~/notes/minute", id)
	}
	// One title that moves, so the pass writes.
	ids, titles := chain(t, ds, "~")
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	repo := ds.info.ID
	probed := 0
	lock := func() {
		for _, id := range minutes {
			tx, err := raw.BeginTx(ctx, nil)
			if err != nil {
				t.Error(err)
				return
			}
			var one int
			err = tx.QueryRowContext(ctx, `SELECT 1 FROM records WHERE repository = $1 AND kind = $2 AND id = $3 FOR UPDATE NOWAIT`,
				repo, "~/notes/minute", id).Scan(&one)
			_ = tx.Rollback()
			if err != nil {
				t.Errorf("between the rounds and the writes, a lock on minute %s is not granted at once: %v", id, err)
			}
			probed++
		}
	}
	probe.Store(&lock)
	unmark(t, ds)

	ds = open()
	if probed != len(minutes) {
		t.Fatalf("the pass met %d of the %d minutes between its rounds and its writes", probed, len(minutes))
	}
	for _, id := range minutes {
		if got := titleOf(t, ds, "~/notes/minute", id); got != "minutes of "+name {
			t.Errorf("minute %s is titled %q", id, got)
		}
		if got := linesOf(t, ds, "~/notes/minute", id); got != lines[id] {
			t.Errorf("the pass wrote %d lines of minute %s, want none", got-lines[id], id)
		}
	}
	if got := titleOf(t, ds, "~/chain/link", ids[0]); got != titles[0] {
		t.Errorf("the link is titled %q, want %q", got, titles[0])
	}
}

// A `~` GLUED TO A LITERAL (`x{next}`) stands where a name stood: what
// precedes it is the same text in the title and its rendering. The head
// moves whole, over two rounds: its own `~` in the first, the one it reads
// through the tail in the second. The titles are spelled by hand.
func TestTheTitlePassMovesATildeGluedToALiteral(t *testing.T) {
	t.Parallel()
	ds, open := titlesRepository(t)
	name := ds.name()
	pkg := name + "/glued"
	applyOwn(t, ds, vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "tail"}, map[string]any{
			"displayTemplate": "{label}",
			"properties":      map[string]any{"label": map[string]any{"type": "string"}},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "head"}, map[string]any{
			"displayTemplate": "{label} x{next}",
			"properties": map[string]any{
				"label": map[string]any{"type": "string"},
				"next":  map[string]any{"type": "reference", "kind": pkg + "/tail"},
			},
		}))
	tail, head := vocabulary.DerivedMintedID("glued", "tail"), vocabulary.DerivedMintedID("glued", "head")
	putOwn(t, ds, "~/glued/tail", tail, map[string]any{"label": "~/tasks/run"})
	putOwn(t, ds, "~/glued/head", head, map[string]any{"label": "~", "next": map[string]any{"ref": "~/glued/tail/" + tail}})
	if got := titleOf(t, ds, "~/glued/head", head); got != "~ x~/tasks/run" {
		t.Fatalf("the head was titled %q at its write", got)
	}
	retitle(t, ds, "~/glued/tail", tail, name+"/tasks/run")
	retitle(t, ds, "~/glued/head", head, name+" x"+name+"/tasks/run")
	unmark(t, ds)

	ds = open()
	if got := titleOf(t, ds, "~/glued/tail", tail); got != "~/tasks/run" {
		t.Errorf("the tail is titled %q, want ~/tasks/run", got)
	}
	if got := titleOf(t, ds, "~/glued/head", head); got != "~ x~/tasks/run" {
		t.Errorf("the head is titled %q, want ~ x~/tasks/run", got)
	}
}
