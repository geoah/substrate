package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// getRuns reads one runs page as the owner.
func getRuns(t *testing.T, env *dbEnv, query string) substrate.ChangeRunPage {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/v1/changes?runs=1&"+query, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	return decodeJSON[substrate.ChangeRunPage](t, rec)
}

// A run longer than one storage batch still comes back whole: the page does
// not end inside it, so the oldest run's count is exact.
func TestChangeRunsCountARunPastTheBatch(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	ctx := context.Background()
	base := changesHead(t, env).Seq
	created := map[string]any{"created": true}
	seedRun(env.ds, 3, substrate.ActorPerson, runPerson, substrate.OpPut, created, "")
	seedRun(env.ds, changeBatch+100, substrate.ActorPerson, runTask, substrate.OpPut, created, "")
	seedRun(env.ds, 2, substrate.ActorPerson, runPerson, substrate.OpPut, created, "")
	head := changesHead(t, env)

	page := getRuns(t, env, "first=2")
	if len(page.Runs) != 2 {
		t.Fatalf("runs = %+v, want 2", page.Runs)
	}
	newest, long := page.Runs[0], page.Runs[1]
	if newest.Count != 2 || newest.Kind != runPerson || newest.NewestSeq != base+changeBatch+105 {
		t.Fatalf("newest run = %+v", newest)
	}
	if long.Count != changeBatch+100 || long.Records != changeBatch+100 || long.Verb != substrate.RunVerbCreate ||
		long.Actor != vocabulary.ActorID(substrate.ActorPerson) || long.NewestSeq != base+changeBatch+103 || long.OldestSeq != base+4 || long.RecordID != "" {
		t.Fatalf("long run = %+v, want %d creates over seqs %d..%d", long, changeBatch+100, base+4, base+changeBatch+103)
	}
	oldest, err := env.ds.Changes(ctx, base+3, substrate.ChangeFilter{}, 1)
	if err != nil || len(oldest) != 1 {
		t.Fatalf("the long run's first entry: %+v %v", oldest, err)
	}
	if !long.OldestTS.Equal(oldest[0].TS) || long.OldestTS.After(long.NewestTS) {
		t.Fatalf("long run oldestTs = %v, want its first entry's %v", long.OldestTS, oldest[0].TS)
	}
	if page.Cursor != base+4 {
		t.Fatalf("cursor = %d, want %d (more rows lie below)", page.Cursor, base+4)
	}
	if page.Head != head.Seq || page.Head != base+changeBatch+105 || page.Generation != head.Generation {
		t.Fatalf("head = %d generation = %q, want %d under %q", page.Head, page.Generation, head.Seq, head.Generation)
	}

	// The cursor walks on to the run below, which the repository's own
	// creation writes lie under.
	page = getRuns(t, env, fmt.Sprintf("first=1&before=%d&generation=%s", page.Cursor, head.Generation))
	if len(page.Runs) != 1 || page.Runs[0].Count != 3 || page.Runs[0].Kind != runPerson || page.Runs[0].OldestSeq != base+1 || page.Cursor != base+1 {
		t.Fatalf("second page = %+v", page)
	}
	// The bottom has no cursor: under a filter only the test's writes match,
	// and the 3-person run is the last of them.
	page = getRuns(t, env, fmt.Sprintf("first=2&before=%d&generation=%s&kinds=%s,%s", base+4, head.Generation, runPerson, runTask))
	if len(page.Runs) != 1 || page.Runs[0].Count != 3 || page.Runs[0].OldestSeq != base+1 || page.Cursor != 0 {
		t.Fatalf("bottom page = %+v", page)
	}
}

// The verb splits a put on its payload and reads a patch as an update, and a
// run that touched one record names it.
func TestChangeRunsGroupByVerb(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	created := map[string]any{"created": true}
	t1, t9 := mintedID("t1"), mintedID("t9")
	seedRun(env.ds, 1, substrate.ActorPerson, runTask, substrate.OpPut, created, t1)
	seedRun(env.ds, 1, substrate.ActorPerson, runTask, substrate.OpPut, created, t9)
	seedRun(env.ds, 1, substrate.ActorPerson, runTask, substrate.OpPut, nil, t1)
	seedRun(env.ds, 2, substrate.ActorPerson, runTask, substrate.OpPatch, nil, t1)
	seedRun(env.ds, 1, substrate.ActorPerson, runTask, substrate.OpDelete, nil, t9)
	seedRun(env.ds, 1, substrate.ActorPerson, runTask, substrate.OpPut, map[string]any{"restored": true}, t9)
	seedRun(env.ds, 1, substrate.ActorPerson, runTask, substrate.OpDelete, nil, t9)

	// The task kind alone: the repository's own creation writes are runs of
	// other kinds, under them.
	page := getRuns(t, env, "kinds="+runTask)
	type want struct {
		verb     string
		count    int
		records  int
		recordID string
	}
	wants := []want{
		{substrate.RunVerbDelete, 1, 1, t9},
		{substrate.RunVerbRestore, 1, 1, t9},
		{substrate.RunVerbDelete, 1, 1, t9},
		{substrate.RunVerbUpdate, 3, 1, t1},
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
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	base := changesHead(t, env).Seq
	created := map[string]any{"created": true}
	seedRun(env.ds, 2, substrate.ActorPerson, runTask, substrate.OpPut, created, "")
	seedRun(env.ds, 1, substrate.ActorPerson, runPerson, substrate.OpPut, created, "")
	seedRun(env.ds, 2, substrate.ActorPerson, runTask, substrate.OpPut, created, "")

	page := getRuns(t, env, "first=3")
	if len(page.Runs) != 3 || page.Runs[0].Kind != runTask || page.Runs[0].Count != 2 ||
		page.Runs[1].Kind != runPerson || page.Runs[1].Count != 1 || page.Runs[2].Kind != runTask || page.Runs[2].Count != 2 {
		t.Fatalf("unfiltered runs = %+v, want tasks, a person, tasks", page.Runs)
	}
	page = getRuns(t, env, "kinds="+runTask)
	if len(page.Runs) != 1 || page.Runs[0].Count != 4 || page.Runs[0].NewestSeq != base+5 || page.Runs[0].OldestSeq != base+1 {
		t.Fatalf("filtered runs = %+v, want one run of 4 over seqs %d..%d", page.Runs, base+1, base+5)
	}
}

// Two assistants on one device are one local app's actor: the runs split on
// the token behind them and the owner's read names each by its token, and so
// do the person's own runs, one for each of its tokens (decisions 0230 and
// 0270).
func TestChangeRunsSplitAndNameTheTokenBehindAnApp(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	app := func(label, client string) string {
		return env.mint(t, substrate.MintTokenInput{
			Label: label, ClientName: client, ClientID: "http://127.0.0.1:7777/" + label,
			AppLocal: true, Scopes: []string{"write:" + runTask},
		})
	}
	claude, codex := app("claude.ai", "Claude Code"), app("chatgpt.com", "Codex")
	cursor := env.mint(t, substrate.MintTokenInput{Label: "Cursor"})
	tok1, tok2 := env.mint(t, substrate.MintTokenInput{Label: "tok1"}), env.mint(t, substrate.MintTokenInput{Label: "tok2"})
	create := func(n int, token string, headers ...string) {
		t.Helper()
		for range n {
			rec := env.do(t, http.MethodPost, recordsPath, token, map[string]any{
				"kind": runTask, "properties": map[string]any{"name": "a task"},
			}, headers...)
			wantStatus(t, rec, http.StatusCreated)
		}
	}
	create(2, claude)
	create(3, codex)
	create(1, cursor)
	create(2, tok1)
	create(1, tok2)

	page := getRuns(t, env, "first=10&kinds="+runTask)
	want := []struct {
		actor substrate.Actor
		count int
		name  string
	}{{substrate.ActorPerson, 1, "tok2"}, {substrate.ActorPerson, 2, "tok1"}, {substrate.ActorPerson, 1, "Cursor"}, {substrate.AppActorLocal, 3, "Codex"}, {substrate.AppActorLocal, 2, "Claude Code"}}
	if len(page.Runs) != len(want) {
		t.Fatalf("runs = %+v, want %d", page.Runs, len(want))
	}
	for i, w := range want {
		if r := page.Runs[i]; r.Actor != vocabulary.ActorID(w.actor) || r.Count != w.count || r.TokenName != w.name {
			t.Fatalf("run %d = %+v, want %s ×%d named %q", i, r, w.actor, w.count, w.name)
		}
	}
	// An assistant reading the history names no token.
	assistant := env.mint(t, substrate.MintTokenInput{
		Label: "assistant", Scopes: []string{"mcp"}, AppLocal: true,
		ClientID: "https://claude.ai/oauth/claude-code-client-metadata", ClientName: "Claude Code",
	})
	runs := runsAs(t, env.testEnv, assistant)
	if len(runs) == 0 {
		t.Fatal("an assistant's read answered no runs")
	}
	for _, r := range runs {
		if r.Token != "" || r.TokenName != "" {
			t.Fatalf("an assistant's read named a token: %+v", r)
		}
	}
}
