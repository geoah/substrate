package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The history as run summaries (`runs=1`): consecutive rows of the filtered
// feed with one actor, kind and verb, each read to its end.

const (
	runTask   = "samples.substrate.reamde.dev/tasks/task"
	runPerson = "samples.substrate.reamde.dev/people/person"
)

// seedRun commits n changes by one actor to one kind, oldest first, each on
// its own record unless record is set.
func seedRun(ds *fakeDataset, n int, actor substrate.Actor, kind string, op substrate.Op, payload map[string]any, record string) {
	for range n {
		id := record
		if id == "" {
			id = fmt.Sprintf("r%d", len(ds.changes)+1)
		}
		ds.commit(substrate.Change{
			TS: time.Unix(int64(len(ds.changes)+1), 0).UTC(), Actor: actor, Op: op,
			RecordID: id, Kind: kind, Payload: payload,
		})
		<-ds.signals
	}
}

func getRuns(t *testing.T, env *testEnv, query string) substrate.ChangeRunPage {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/v1/changes?runs=1&"+query, env.svc.token(fakeRepository), nil)
	wantStatus(t, rec, http.StatusOK)
	return decodeJSON[substrate.ChangeRunPage](t, rec)
}

// A run longer than one storage batch still comes back whole: the page does
// not end inside it, so the oldest run's count is exact.
func TestChangeRunsCountARunPastTheBatch(t *testing.T) {
	env := newTestEnv(t)
	ds := env.svc.datasets[fakeRepository]
	created := map[string]any{"created": true}
	seedRun(ds, 3, substrate.ActorAPI, runPerson, substrate.OpPut, created, "")
	seedRun(ds, changeBatch+100, "console", runTask, substrate.OpPut, created, "")
	seedRun(ds, 2, substrate.ActorAPI, runPerson, substrate.OpPut, created, "")

	page := getRuns(t, env, "first=2")
	if len(page.Runs) != 2 {
		t.Fatalf("runs = %+v, want 2", page.Runs)
	}
	newest, long := page.Runs[0], page.Runs[1]
	if newest.Count != 2 || newest.Kind != runPerson || newest.NewestSeq != int64(changeBatch+105) {
		t.Fatalf("newest run = %+v", newest)
	}
	if long.Count != changeBatch+100 || long.Records != changeBatch+100 || long.Verb != substrate.RunVerbCreate ||
		long.Actor != "console" || long.NewestSeq != int64(changeBatch+103) || long.OldestSeq != 4 || long.RecordID != "" {
		t.Fatalf("long run = %+v, want %d creates over seqs 4..%d", long, changeBatch+100, changeBatch+103)
	}
	if !long.OldestTS.Equal(time.Unix(4, 0)) {
		t.Fatalf("long run oldestTs = %v", long.OldestTS)
	}
	if page.Cursor != 4 {
		t.Fatalf("cursor = %d, want 4 (more rows lie below)", page.Cursor)
	}
	if page.Head != int64(changeBatch+105) || page.Generation != ds.generation {
		t.Fatalf("head = %d generation = %q", page.Head, page.Generation)
	}

	// The cursor walks on to the run below, and the bottom has no cursor.
	page = getRuns(t, env, fmt.Sprintf("first=2&before=%d&generation=%s", page.Cursor, ds.generation))
	if len(page.Runs) != 1 || page.Runs[0].Count != 3 || page.Runs[0].OldestSeq != 1 || page.Cursor != 0 {
		t.Fatalf("second page = %+v", page)
	}
}

// The verb splits a put on its payload and reads a patch as an update, and a
// run that touched one record names it.
func TestChangeRunsGroupByVerb(t *testing.T) {
	env := newTestEnv(t)
	ds := env.svc.datasets[fakeRepository]
	seedRun(ds, 2, substrate.ActorAPI, runTask, substrate.OpPut, map[string]any{"created": true}, "")
	seedRun(ds, 1, substrate.ActorAPI, runTask, substrate.OpPut, nil, "t1")
	seedRun(ds, 2, substrate.ActorAPI, runTask, substrate.OpPatch, nil, "t1")
	seedRun(ds, 1, substrate.ActorAPI, runTask, substrate.OpPut, map[string]any{"restored": true}, "t9")
	seedRun(ds, 1, substrate.ActorAPI, runTask, substrate.OpDelete, nil, "t9")

	page := getRuns(t, env, "")
	type want struct {
		verb     string
		count    int
		records  int
		recordID string
	}
	wants := []want{
		{substrate.RunVerbDelete, 1, 1, "t9"},
		{substrate.RunVerbRestore, 1, 1, "t9"},
		{substrate.RunVerbUpdate, 3, 1, "t1"},
		{substrate.RunVerbCreate, 2, 2, ""},
	}
	if len(page.Runs) != len(wants) {
		t.Fatalf("runs = %+v, want %d", page.Runs, len(wants))
	}
	for i, w := range wants {
		got := page.Runs[i]
		if got.Verb != w.verb || got.Count != w.count || got.Records != w.records || got.RecordID != w.recordID {
			t.Fatalf("run %d = %+v, want %+v", i, got, w)
		}
	}
	if page.Cursor != 0 {
		t.Fatalf("cursor = %d on an exhausted walk", page.Cursor)
	}
}

// Runs are consecutive in the FILTERED feed: a row the filter drops does not
// break a run around it.
func TestChangeRunsFoldOverTheFilter(t *testing.T) {
	env := newTestEnv(t)
	ds := env.svc.datasets[fakeRepository]
	created := map[string]any{"created": true}
	seedRun(ds, 2, substrate.ActorAPI, runTask, substrate.OpPut, created, "")
	seedRun(ds, 1, substrate.ActorAPI, runPerson, substrate.OpPut, created, "")
	seedRun(ds, 2, substrate.ActorAPI, runTask, substrate.OpPut, created, "")

	page := getRuns(t, env, "")
	if len(page.Runs) != 3 {
		t.Fatalf("unfiltered runs = %+v, want 3", page.Runs)
	}
	page = getRuns(t, env, "kinds="+runTask)
	if len(page.Runs) != 1 || page.Runs[0].Count != 4 || page.Runs[0].NewestSeq != 5 || page.Runs[0].OldestSeq != 1 {
		t.Fatalf("filtered runs = %+v, want one run of 4 over seqs 1..5", page.Runs)
	}
}

// `runs` is a history read with one value, held to the same cursor rules.
func TestChangeRunsRefusals(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	seedChanges(env.svc.datasets[fakeRepository], 3)
	for _, q := range []string{"runs=true", "runs=1&watch=1", "runs=1&from=1", "runs=1&first=0"} {
		rec := env.do(t, http.MethodGet, "/api/v1/changes?"+q, tok, nil)
		wantStatus(t, rec, http.StatusBadRequest)
	}
	// A continuation without its generation is the history page's compacted
	// refusal, not an answer from another history.
	rec := env.do(t, http.MethodGet, "/api/v1/changes?runs=1&before=2", tok, nil)
	wantStatus(t, rec, http.StatusGone)
}
