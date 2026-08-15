package engine_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// The changelog is the truth and the records table is a fold of it. These tests hold
// the two halves of that claim: an entry carries the delta WITH ITS VALUES,
// and replaying every entry through the same fold reproduces the records table
// exactly — which is also the containment test, made runnable.

// rebuilder is the service's rebuild seam. It is a concrete engine method
// rather than part of the frozen substrate.Service interface: substratectl and the
// operator reach it, an ordinary library consumer does not.
type rebuilder interface {
	RebuildRepository(ctx context.Context, username string) (engine.RebuildReport, error)
}

// folded reads a repository's whole fold as one ordered document — the thing
// a rebuild must reproduce byte for byte.
type folded interface {
	FoldSnapshot(ctx context.Context) ([]byte, error)
}

func foldOf(t *testing.T, ds substrate.Dataset) []byte {
	t.Helper()
	f, ok := ds.(folded)
	if !ok {
		t.Fatal("the dataset does not expose its fold")
	}
	raw, err := f.FoldSnapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot the fold: %v", err)
	}
	return raw
}

// writeSomeHistory drives the ordinary write surface — creates, property
// changes, a state transition, labels, annotations, edges, a delete and the
// put that restores it — so the changelog under test holds one of everything the
// fold has to replay.
func writeSomeHistory(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)

	first := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{
			"title":       "Ship the fold",
			"body":        "the records table is a fold of the changelog",
			"description": "carry the values, not just the names",
			"dueAt":       due.Format(time.RFC3339),
			"url":         "https://example.com/1",
		},
		Labels: map[string]any{"owner/pinned": true},
	})
	second := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{"title": "Rebuild the repository", "description": "replay every entry"},
	})
	third := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{"title": "Collect me", "description": "and then go"},
	})

	// A property changes, one is removed, a label lands, an annotation lands.
	mustPatch(t, ds, owner, first.Kind, first.ID, substrate.PatchInput{
		Properties:  map[string]any{"description": "values, replayable", "url": nil},
		Labels:      map[string]any{"owner/pinned": nil, "owner/urgent": "yes"},
		Annotations: map[string]any{"owner/note": map[string]any{"why": "the payload"}},
	})
	// A declared transition, which stamps a property the writer never named.
	mustPatch(t, ds, owner, first.Kind, first.ID, substrate.PatchInput{
		Properties: map[string]any{"status": "done"},
	})
	// An edge, written by the link verb rather than inside a put.
	if err := ds.Link(ctx, owner, second.Kind, second.ID, "source",
		substrate.EdgeRef{Kind: first.Kind, ID: first.ID}, nil); err != nil {
		t.Fatalf("link: %v", err)
	}
	// A delete, and the put that brings the record back.
	if _, err := ds.Delete(ctx, owner, third.Kind, third.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: third.Kind, ID: third.ID,
		Properties: map[string]any{"title": "Collect me", "description": "restored"},
	})
	// And one that stays deleted.
	if _, err := ds.Delete(ctx, owner, second.Kind, second.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// TestRebuildReproducesTheFold is the containment test: clear the records
// table and everything derived with it, replay the whole changelog through the fold,
// and the store must come back bit for bit — the seed's schema rows included,
// since a repository's vocabulary is written into its own changelog like everything
// else.
func TestRebuildReproducesTheFold(t *testing.T) {
	t.Parallel()
	svc, ds := newDataset(t)
	writeSomeHistory(t, ds)

	before := foldOf(t, ds)
	head := maxSeq(t, ds)

	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	report, err := rb.RebuildRepository(context.Background(), "geoah")
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if report.Head != head {
		t.Fatalf("rebuild stopped at seq %d, the changelog's head is %d", report.Head, head)
	}
	if report.Entries == 0 || report.Records == 0 {
		t.Fatalf("rebuild replayed nothing: %+v", report)
	}

	after := foldOf(t, ds)
	if string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}

	// A rebuild is a replay, never a write: it appends nothing to the changelog.
	if got := maxSeq(t, ds); got != head {
		t.Fatalf("the rebuild moved the changelog's head from %d to %d", head, got)
	}
	// And the reads that run off the fold still answer.
	if e := mustGet(t, ds, "tasks.substrate.reamde.dev/task", firstTaskID(t, ds)); e == nil {
		t.Fatal("the rebuilt fold reads back empty")
	}
}

// TestRebuildIsIdempotent: rebuilding a rebuilt repository changes nothing.
// The fold is a function of the changelog, so applying it twice must land in the
// same place — anything else means an effect is being replayed relative to
// the wall clock or to state the changelog does not hold.
func TestRebuildIsIdempotent(t *testing.T) {
	t.Parallel()
	svc, ds := newDataset(t)
	writeSomeHistory(t, ds)
	rb := svc.(rebuilder)

	if _, err := rb.RebuildRepository(context.Background(), "geoah"); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	once := foldOf(t, ds)
	if _, err := rb.RebuildRepository(context.Background(), "geoah"); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	if twice := foldOf(t, ds); string(once) != string(twice) {
		t.Fatalf("rebuilding twice is not rebuilding once\n%s", firstDifference(once, twice))
	}
}

// TestRebuildKeeps64BitIntegers: a delta value past 2^53 must refold exactly.
// The live fold marshals the written int64 into the store and the entry's
// payload alike; a replay that decodes that payload through float64 rounds
// 2^53+1 to its even neighbor and folds a value the changelog never held.
// An annotation is the vehicle because it carries arbitrary JSON unmodified:
// a declared int property refuses the value at the door (asInt), and a label
// flattens through coerceLabels' round trip before it ever reaches the fold.
func TestRebuildKeeps64BitIntegers(t *testing.T) {
	t.Parallel()
	svc, ds := newDataset(t)
	const big = int64(1<<53 + 1)
	exact := strconv.FormatInt(big, 10)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind:        "tasks.substrate.reamde.dev/task",
		Properties:  map[string]any{"title": "Count past the mantissa"},
		Annotations: map[string]any{"owner/count": big},
	})

	before := foldOf(t, ds)
	if !strings.Contains(string(before), exact) {
		t.Fatalf("the live fold does not hold %s: the value rounded before the rebuild could", exact)
	}

	if _, err := svc.(rebuilder).RebuildRepository(context.Background(), "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after := foldOf(t, ds)
	if !strings.Contains(string(after), exact) {
		t.Fatalf("the rebuilt fold rounded %s away\n%s", exact, firstDifference(before, after))
	}
	if string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
}

// TestLogEntryCarriesValues: v0's payload named the properties that moved and
// nothing else, so nothing could replay it. An entry now carries what they
// BECAME.
func TestLogEntryCarriesValues(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	before := maxSeq(t, ds)
	e := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{"title": "Carry the values", "description": "a delta, with values"},
	})

	var entry substrate.Change
	for _, ch := range changesSince(t, ds, before) {
		if ch.RecordID == e.ID {
			entry = ch
		}
	}
	if entry.Seq == 0 {
		t.Fatal("the put wrote no changelog entry")
	}
	if names, _ := entry.Payload["properties"].([]any); len(names) == 0 {
		t.Fatalf("the entry names no properties: %v", entry.Payload)
	}
	set := recordDeltaOf(t, entry)
	if got := set["description"]; got != "a delta, with values" {
		t.Fatalf("the entry's delta carries description = %v, want the written value", got)
	}
	if got := set["title"]; got != "Carry the values" {
		t.Fatalf("the entry's delta carries title = %v, want the written value", got)
	}

	// A patch carries only what moved — a delta, not a snapshot.
	mid := maxSeq(t, ds)
	mustPatch(t, ds, owner, e.Kind, e.ID, substrate.PatchInput{
		Properties: map[string]any{"description": "moved"},
	})
	for _, ch := range changesSince(t, ds, mid) {
		if ch.RecordID != e.ID {
			continue
		}
		set := recordDeltaOf(t, ch)
		if set["description"] != "moved" {
			t.Fatalf("the patch's delta carries description = %v", set["description"])
		}
		if _, carried := set["title"]; carried {
			t.Fatalf("the patch's delta carries an unchanged title: %v", set)
		}
	}
}

// mergedPair is the graph a merge has to carry across: two books, each with
// its own edges, labels, annotations and manager rows, an edition whose SUBJECT
// edge points at the loser, and a trail from an earlier merge — so the rewrite
// under test moves something of every kind the side stores hold.
type mergedPair struct {
	winner, loser, edition, author *substrate.Record
}

func writeAMergeablePair(t *testing.T, ds substrate.Dataset) mergedPair {
	t.Helper()
	ctx := context.Background()

	// The shelf fixture: a `book` kind whose records merge, and a
	// `bookedition` kind whose `work` edge names the loser so the merge has an
	// edge outside the pair to re-point.
	installShelf(t, ds)
	const shelf = enginetest.ShelfAuthority

	author := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "Ada Lovelace"},
	})
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind:        shelf + "/book",
		Properties:  map[string]any{"title": "Notes on the Engine", "subtitle": "a winner"},
		Labels:      map[string]any{"owner/shelf": "analytical"},
		Annotations: map[string]any{"owner/note": "the winner's own"},
		Edges: []substrate.EdgeInput{
			{Rel: "author", To: substrate.EdgeRef{Kind: author.Kind, ID: author.ID}},
		},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: shelf + "/book",
		// `description` is the winner's gap: its manager row migrates.
		Properties:  map[string]any{"title": "Notes on the Engine", "description": "the loser's own"},
		Labels:      map[string]any{"owner/format": "octavo"},
		Annotations: map[string]any{"owner/note": "the loser's, newer", "owner/extra": 7},
	})
	// An earlier merge, so the loser carries a trail the second one flattens.
	older := mustPut(t, ds, owner, substrate.PutInput{
		Kind: shelf + "/book", Properties: map[string]any{"title": "Notes on the Engine"},
	})
	if _, err := ds.Merge(ctx, owner, loser.Kind, loser.ID, older.ID); err != nil {
		t.Fatalf("the first merge: %v", err)
	}
	// A source record whose SUBJECT edge names the loser: the merge re-points
	// it at the winner, and it is why the resync's scope is more than the pair.
	edition := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       shelf + "/bookedition",
		Properties: map[string]any{"format": "print", "language": "en"},
		Edges: []substrate.EdgeInput{
			{Rel: "work", To: substrate.EdgeRef{Kind: loser.Kind, ID: loser.ID}},
			{Rel: "narrator", To: substrate.EdgeRef{Kind: author.Kind, ID: author.ID}},
		},
	})
	return mergedPair{winner: winner, loser: loser, edition: edition, author: author}
}

// TestRebuildReproducesAMerge: a merge rewrites the graph around the pair it
// joins with set-shaped statements, so its entry carries the resync effect —
// the after-state of every side-store row the rewrite touched. A repository
// that merged must rebuild bit for bit like any other.
func TestRebuildReproducesAMerge(t *testing.T) {
	t.Parallel()
	svc, ds := newDataset(t)
	ctx := context.Background()
	writeSomeHistory(t, ds)
	pair := writeAMergeablePair(t, ds)

	if _, err := ds.Merge(ctx, owner, pair.winner.Kind, pair.winner.ID, pair.loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// The merge moved what the test claims it moved, or the rebuild below
	// would be reproducing an empty rewrite.
	ed := mustGet(t, ds, pair.edition.Kind, pair.edition.ID)
	if work := edgeTarget(ed, "work"); work != pair.winner.ID {
		t.Fatalf("the merge left the edition's subject on %q, want the winner %q", work, pair.winner.ID)
	}

	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	report, err := svc.(rebuilder).RebuildRepository(ctx, "geoah")
	if err != nil {
		t.Fatalf("rebuild a merged repository: %v", err)
	}
	if report.Head != head {
		t.Fatalf("rebuild stopped at seq %d, the changelog's head is %d", report.Head, head)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
}

// TestRebuildReproducesASplit: and the undo replays too — split rewrites the
// same graph in the other direction, and carries its own resync.
func TestRebuildReproducesASplit(t *testing.T) {
	t.Parallel()
	svc, ds := newDataset(t)
	ctx := context.Background()
	pair := writeAMergeablePair(t, ds)

	rec, err := ds.Merge(ctx, owner, pair.winner.Kind, pair.winner.ID, pair.loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// The owner curates the winner between the merge and the split, so the
	// split's own "this was rewritten since" decisions are in the changelog too.
	mustPatch(t, ds, owner, pair.winner.Kind, pair.winner.ID, substrate.PatchInput{
		Labels:      map[string]any{"owner/shelf": "curated"},
		Annotations: map[string]any{"owner/note": "curated after the merge"},
	})
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	ed := mustGet(t, ds, pair.edition.Kind, pair.edition.ID)
	if work := edgeTarget(ed, "work"); work != pair.loser.ID {
		t.Fatalf("the split left the edition's subject on %q, want the loser %q", work, pair.loser.ID)
	}

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild a split repository: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
	// And twice lands in the same place: the resync is a snapshot, so applying
	// it again must not drift.
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("rebuilding a split repository twice is not rebuilding it once\n%s", firstDifference(before, after))
	}
}

// TestRebuildRefusesWhatItCannotReplay: a merge entry replays through the
// resync effect it carries. One that carries none describes the moved sets for
// split's undo and nothing the fold can act on — a rebuild must say so rather
// than produce a fold that is quietly not the changelog's.
func TestRebuildRefusesWhatItCannotReplay(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds)
	a := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "Ada Lovelace"},
	})
	b := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "A. Lovelace"},
	})
	if _, err := ds.Merge(ctx, owner, a.Kind, a.ID, b.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// A merge entry as an older binary wrote it: the moved sets, no resync.
	stripResyncEffects(t, dsn, ds)

	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err == nil {
		t.Fatal("the rebuild replayed a merge nothing describes")
	}
	// And it refused without touching the fold.
	if e := mustGet(t, ds, a.Kind, a.ID); e == nil {
		t.Fatal("the refused rebuild emptied the fold")
	}
}

// stripResyncEffects removes the resync effect from every merge and split entry
// — the shape of an entry written before merge could describe its rewrite.
func stripResyncEffects(t *testing.T, dsn string, ds substrate.Dataset) {
	t.Helper()
	db, err := engine.OpenScopedDB(dsn, repositoryIDOf(t, ds), engine.RoleMaint)
	if err != nil {
		t.Fatalf("open the maintenance pool: %v", err)
	}
	defer func() { _ = db.Close() }()
	// `effect` rather than `op`: the array's alias must not shadow the changelog's own
	// `op` column, or the aggregate below reads the outer row instead.
	if _, err := db.Exec(`
		UPDATE changelog SET payload = jsonb_set(payload, '{fold}', coalesce(
			(SELECT jsonb_agg(effect.value) FROM jsonb_array_elements(payload->'fold') AS effect
			 WHERE effect.value->>'kind' <> 'resync'), '[]'::jsonb))
		WHERE changelog.op IN ('merge', 'split') AND payload ? 'fold'`); err != nil {
		t.Fatalf("strip the resync effects: %v", err)
	}
}

// repositoryIDOf is the opaque internal id a scoped pool is opened on.
func repositoryIDOf(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	if id := ds.Repository().ID; id != "" {
		return id
	}
	t.Fatal("the dataset does not name its repository")
	return ""
}

// edgeTarget reads the single target of one rel off a projected record.
func edgeTarget(e *substrate.Record, rel string) string {
	for _, ref := range e.Edges[rel] {
		return ref.ID
	}
	return ""
}

// recordDeltaOf lifts the `set` half of an entry's record effect: the changed
// properties WITH their values.
func recordDeltaOf(t *testing.T, ch substrate.Change) map[string]any {
	t.Helper()
	raw, err := json.Marshal(ch.Payload["fold"])
	if err != nil {
		t.Fatalf("the entry carries no fold: %v", err)
	}
	var ops []struct {
		Kind  string `json:"kind"`
		Delta *struct {
			Set   map[string]any `json:"set"`
			Title *string        `json:"title"`
			Body  *string        `json:"body"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(raw, &ops); err != nil {
		t.Fatalf("unreadable fold: %v", err)
	}
	out := map[string]any{}
	for _, op := range ops {
		if op.Kind != "record" || op.Delta == nil {
			continue
		}
		for k, v := range op.Delta.Set {
			out[k] = v
		}
		if op.Delta.Title != nil {
			out["title"] = *op.Delta.Title
		}
		if op.Delta.Body != nil {
			out["body"] = *op.Delta.Body
		}
	}
	if len(out) == 0 {
		t.Fatalf("seq %d carries no record delta: %v", ch.Seq, ch.Payload)
	}
	return out
}

// firstDifference reports where two fold snapshots part company, so a failure
// names the row rather than dumping two documents.
func firstDifference(a, b []byte) string {
	as, bs := string(a), string(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		lo := max(0, i-200)
		hiA := min(len(as), i+200)
		hiB := min(len(bs), i+200)
		return "before: …" + as[lo:hiA] + "\n\nafter:  …" + bs[lo:hiB]
	}
	if len(as) == len(bs) {
		return "(identical)"
	}
	return "the snapshots differ in length"
}

func firstTaskID(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"tasks.substrate.reamde.dev/task"}}, First: 1,
	})
	if err != nil || page == nil || len(page.Records) == 0 {
		t.Fatalf("list tasks: %v", err)
	}
	return page.Records[0].ID
}
