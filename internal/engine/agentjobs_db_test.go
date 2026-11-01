package engine

// Background calls (decision record 0200), driven by the fake model server:
// a background sub-agent held on its model turn is the slow job. The call
// answers with its handle at once, its end writes the tool row's `job` and a
// toolCompleted row together, and the decision loop delivers it: after the
// next batch of a run still going, as a resume of a settled thread (within
// `resume: never` and the hourly budget), or through `wait`. A stop cancels
// the jobs, and a job a restart interrupted settles `failed` at open.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const jobsPackage = "jobs.test.dev/jobs"

var jobsNote = jobsPackage + "/note"

// provisionJobAgents installs, on one provider row:
//   - `lead`: a background sub-agent `digger`, `wait`, and `query` over notes.
//   - `quiet`: the same sub-agent, with `resume: never`.
//   - `filer`: the `stamp` function as a background tool.
//   - `digger`: no tools; the tests hold its model turn.
func provisionJobAgents(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "jobsllm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-jobsllm",
			"pricing": []any{
				map[string]any{"model": "jlead", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "jquiet", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "jfiler", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "jdig", "inputPer1M": "1000", "outputPer1M": "1000"},
			},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row jobsllm: %v", err)
	}
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(jobsPackage, name, data)
	}
	digger := map[string]any{"ref": jobsPackage + "/digger", "background": true}
	docs := []map[string]any{
		vocabulary.PackageManifest(jobsPackage, 0),
		vocabulary.KindManifest(jobsPackage, map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.FunctionManifest(jobsPackage, "stamp", map[string]any{
			"description": "writes one note under the id you pass",
			"runtime":     vocabulary.RuntimePython,
			"execution":   vocabulary.FunctionExecutionBackground,
			"arguments":   []any{map[string]any{"name": "id", "type": "string", "required": true}},
			"permissions": map[string]any{"writes": []any{jobsNote}},
			"source": `
def main(input, host):
    nid = input["args"]["id"]
    return {"effects": [{"action": "put", "kind": "` + jobsNote + `", "id": nid, "properties": {"name": "stamped"}}],
            "output": {"stamped": nid}}
`,
		}),
		agent("lead", map[string]any{
			"provider": "jobsllm", "model": "jlead",
			"tools": []any{
				map[string]any{"function": vocabulary.HostFunctionWait},
				map[string]any{"function": vocabulary.HostFunctionQuery},
			},
			"subagents":   []any{digger},
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{jobsNote}}},
		}),
		agent("quiet", map[string]any{
			"provider": "jobsllm", "model": "jquiet", "resume": "never",
			"subagents": []any{digger},
		}),
		agent("filer", map[string]any{
			"provider": "jobsllm", "model": "jfiler",
			"tools":       []any{map[string]any{"function": jobsPackage + "/stamp"}},
			"permissions": map[string]any{"writes": []any{jobsNote}},
		}),
		agent("digger", map[string]any{"provider": "jobsllm", "model": "jdig"}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install jobs package: %v", err)
	}
	return ds, fake
}

// jobToolRow is the thread's tool row answering callID.
func jobToolRow(t *testing.T, ds *dataset, thread, callID string) map[string]any {
	t.Helper()
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "tool" && m["toolCallId"] == callID {
			return m
		}
	}
	return nil
}

// jobStateOf is a tool row's `job.state`.
func jobStateOf(row map[string]any) string {
	job, _ := row["job"].(map[string]any)
	s, _ := job["state"].(string)
	return s
}

// toolCompletedRows are the thread's toolCompleted envelopes.
func toolCompletedRows(t *testing.T, ds *dataset, thread string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] != msgRoleSystem {
			continue
		}
		env := map[string]any{}
		if c, _ := m["content"].(string); json.Unmarshal([]byte(c), &env) == nil && env["event"] == eventToolCompleted {
			out = append(out, env)
		}
	}
	return out
}

func threadStatusOf(t *testing.T, ds *dataset, thread string) (string, map[string]any) {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: typeThread, ID: thread})
	if err != nil || row == nil {
		t.Fatalf("thread %s: %v", thread, err)
	}
	s, _ := row.Props["status"].(string)
	return s, row.Props
}

// requestCarries reports whether any message of a model request holds text.
func requestCarries(req map[string]any, text string) bool {
	for _, m := range requestMessages(req) {
		for _, c := range messageParts(m) {
			if strings.Contains(c, text) {
				return true
			}
		}
	}
	return false
}

// A background sub-agent answers with its handle and the run settles without
// it; its end lands the toolCompleted row and the job's state together, rolls
// its spend onto the root thread, and resumes the settled thread with it.
func TestBackgroundSubagentAnswersWithAHandleAndResumes(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	ctx := context.Background()
	held, release := heldTurn(t)
	fake.script("jlead",
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig the archive"}`}}},
		fakeTurn{content: "started the dig"},
		fakeTurn{content: "the dig is done"},
	)
	fake.script("jdig", fakeTurn{content: "found the deed", release: held})

	start := time.Now()
	res, err := ds.CallAgent(ctx, jobsPackage+"/lead", "find the deed")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "started the dig" || res.Status != threadOK {
		t.Fatalf("result %+v", res)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("the run waited on its background call")
	}
	row := jobToolRow(t, ds, res.Thread, "call_0")
	if row == nil || jobStateOf(row) != jobWorking {
		t.Fatalf("tool row %v, want job working", row)
	}
	handle := map[string]any{}
	_ = json.Unmarshal([]byte(row["content"].(string)), &handle)
	if handle["job"] != "call_0" || handle["status"] != jobWorking {
		t.Fatalf("handle %v", handle)
	}
	_, before := threadStatusOf(t, ds, res.Thread)
	costBefore, _ := anyFloat(before["costUSD"])

	release()
	waitFor(t, "the resume's reply", func() bool {
		_, props := threadStatusOf(t, ds, res.Thread)
		return props[propThreadLastReply] == "the dig is done"
	})
	row = jobToolRow(t, ds, res.Thread, "call_0")
	if jobStateOf(row) != jobCompleted {
		t.Fatalf("job %v", row["job"])
	}
	if job, _ := row["job"].(map[string]any); job["finishedAt"] == nil {
		t.Fatalf("job carries no finishedAt: %v", job)
	}
	if row["subagentWrites"] == nil {
		t.Fatal("the sub-agent job's tool row carries no subagentWrites")
	}
	done := toolCompletedRows(t, ds, res.Thread)
	if len(done) != 1 || done[0]["toolCallId"] != "call_0" || done[0]["ok"] != true ||
		!strings.Contains(done[0]["output"].(string), "found the deed") {
		t.Fatalf("toolCompleted rows %v", done)
	}
	reqs := fake.requestsOf("jlead")
	if len(reqs) != 3 || !requestCarries(reqs[2], "found the deed") {
		t.Fatalf("the resume did not carry the job's result: %d requests", len(reqs))
	}
	// The digger's priced turn is on the root thread: its 10+5 tokens at
	// 1000 USD per million are 0.015, past the lead's own turns.
	_, after := threadStatusOf(t, ds, res.Thread)
	costAfter, _ := anyFloat(after["costUSD"])
	if costAfter-costBefore < 0.015 {
		t.Fatalf("root cost %v -> %v: the job's spend did not roll up", costBefore, costAfter)
	}
}

// A job that finishes while its run goes is read after the run's next batch
// of tool results, and the settled run does not resume for it again.
func TestBackgroundFunctionReachesTheRunningLoop(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	ctx := context.Background()
	arrived := make(chan struct{})
	held, release := heldTurn(t)
	fake.script("jfiler",
		fakeTurn{calls: []fakeCall{{name: "stamp", args: `{"id":"n1"}`}}},
		// Held until the job's end is a row, then one more batch: a second
		// stamp's handle, after which the first one's result follows.
		fakeTurn{calls: []fakeCall{{name: "stamp", args: `{"id":"n2"}`}}, arrived: arrived, release: held},
		fakeTurn{content: "filed"},
		fakeTurn{content: "and the second one"},
	)
	type out struct {
		res *substrate.AgentResult
		err error
	}
	got := make(chan out, 1)
	go func() {
		res, err := ds.CallAgent(ctx, jobsPackage+"/filer", "file it")
		got <- out{res, err}
	}()
	<-arrived
	var thread string
	waitFor(t, "the first job's end", func() bool {
		threads := threadsOfAgent(t, ds, jobsPackage+"/filer")
		if len(threads) == 0 {
			return false
		}
		thread = threads[0]
		row := jobToolRow(t, ds, thread, "call_0")
		return row != nil && jobStateOf(row) == jobCompleted
	})
	release()
	r := <-got
	if r.err != nil {
		t.Fatal(r.err)
	}
	reqs := fake.requestsOf("jfiler")
	if len(reqs) < 3 || !requestCarries(reqs[2], eventToolCompleted) || !requestCarries(reqs[2], `stamped\":\"n1`) {
		t.Fatal("the third turn did not carry the first job's result")
	}
	if _, err := ds.Get(ctx, jobsNote, "n1"); err != nil {
		t.Fatalf("the background function's effect: %v", err)
	}
	// The second job ended after the reply, so it resumes the thread; the
	// first, already read, does not resume it twice.
	waitFor(t, "the second job's resume", func() bool {
		_, props := threadStatusOf(t, ds, thread)
		return props[propThreadLastReply] == "and the second one"
	})
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.requestsOf("jfiler")); n != 4 {
		t.Fatalf("%d model calls, want 4", n)
	}
}

// threadsOfAgent lists an agent's thread ids, oldest first.
func threadsOfAgent(t *testing.T, ds *dataset, identity string) []string {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND `+referencePathSQL("props", "agent")+` = $2 ORDER BY created_at, id`,
		typeThread, vocabulary.RecordPath(kindAgent, identity))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// `wait` holds the loop until the job ends and answers its result; the run
// does not resume for a job it already read.
func TestWaitAnswersAFinishedJob(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	ctx := context.Background()
	waiting := make(chan struct{})
	held, release := heldTurn(t)
	fake.script("jlead",
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig"}`}}},
		fakeTurn{calls: []fakeCall{{name: "wait", args: `{"jobs":["call_0"],"timeoutSeconds":30}`}}, arrived: waiting},
		fakeTurn{content: "the deed is found"},
	)
	fake.script("jdig", fakeTurn{content: "found the deed", release: held})
	go func() {
		<-waiting
		release()
	}()
	res, err := ds.CallAgent(ctx, jobsPackage+"/lead", "find the deed")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "the deed is found" {
		t.Fatalf("result %+v", res)
	}
	waitRow := jobToolRow(t, ds, res.Thread, "call_0")
	if jobStateOf(waitRow) != jobCompleted {
		t.Fatalf("job %v", waitRow["job"])
	}
	reqs := fake.requestsOf("jlead")
	if !requestCarries(reqs[2], "found the deed") {
		t.Fatal("wait did not answer the job's result")
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.requestsOf("jlead")); n != 3 {
		t.Fatalf("%d model calls, want 3: a job wait answered resumed the thread", n)
	}
	if threads, err := ds.unconsumedResolutions(ctx); err != nil || len(threads) != 0 {
		t.Fatalf("unconsumed %v %v", threads, err)
	}
}

func TestWaitJobBound(t *testing.T) {
	now := time.Now()
	if got := waitJobBound(0, now.Add(100*time.Second), now); got != 80*time.Second {
		t.Fatalf("no timeout: %v, want the run's time less its reserve", got)
	}
	if got := waitJobBound(5*time.Second, now.Add(100*time.Second), now); got != 5*time.Second {
		t.Fatalf("asked 5s: %v", got)
	}
	if got := waitJobBound(time.Hour, now.Add(10*time.Second), now); got != 8*time.Second {
		t.Fatalf("asked past the deadline: %v", got)
	}
	if got := waitJobBound(time.Second, now.Add(-time.Second), now); got != 0 {
		t.Fatalf("past the deadline: %v", got)
	}
}

// A stop of a thread with no running turn cancels its jobs and settles the
// thread `stopped`, so the canceled job's end resumes nothing.
func TestStopCancelsBackgroundJobs(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	ctx := context.Background()
	held, _ := heldTurn(t)
	fake.script("jlead",
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig"}`}}},
		fakeTurn{content: "started"},
	)
	fake.script("jdig", fakeTurn{content: "never", release: held})
	res, err := ds.CallAgent(ctx, jobsPackage+"/lead", "find the deed")
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := ds.StopAgentTurn(ctx, substrate.ActorAPI, res.Thread)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != threadStopped {
		t.Fatalf("stop %+v", stopped)
	}
	waitFor(t, "the job's cancel", func() bool {
		return jobStateOf(jobToolRow(t, ds, res.Thread, "call_0")) == jobCanceled
	})
	done := toolCompletedRows(t, ds, res.Thread)
	if len(done) != 1 || done[0]["ok"] != false || !strings.Contains(done[0]["output"].(string), "stopped by") {
		t.Fatalf("toolCompleted %v", done)
	}
	time.Sleep(300 * time.Millisecond)
	if status, _ := threadStatusOf(t, ds, res.Thread); status != threadStopped {
		t.Fatalf("thread %s, want stopped", status)
	}
	if n := len(fake.requestsOf("jlead")); n != 2 {
		t.Fatalf("%d model calls, want 2", n)
	}
	if _, err := ds.StopAgentTurn(ctx, substrate.ActorAPI, res.Thread); err == nil {
		t.Fatal("a second stop found something to stop")
	}
}

// A job's end resumes nothing past `resume: never` or the hourly budget: the
// row lands, and the thread stays as it settled.
func TestBackgroundJobResumeIsBounded(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	ctx := context.Background()

	held, release := heldTurn(t)
	fake.script("jquiet",
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig"}`}}},
		fakeTurn{content: "started"},
	)
	fake.script("jdig", fakeTurn{content: "found", release: held})
	quiet, err := ds.CallAgent(ctx, jobsPackage+"/quiet", "dig")
	if err != nil {
		t.Fatal(err)
	}
	release()
	waitFor(t, "quiet's job", func() bool { return len(toolCompletedRows(t, ds, quiet.Thread)) == 1 })

	held2, release2 := heldTurn(t)
	fake.script("jlead",
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig"}`}}},
		fakeTurn{content: "started"},
	)
	fake.script("jdig", fakeTurn{content: "found", release: held2})
	lead, err := ds.CallAgent(ctx, jobsPackage+"/lead", "dig")
	if err != nil {
		t.Fatal(err)
	}
	// The hour's budget, spent before the job ends.
	for i := 0; i <= resumeBudgetPerHour; i++ {
		if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			return tx.putThreadSystemRow(lead.Thread, map[string]any{"event": "recordResolved"}, nil)
		}); err != nil {
			t.Fatal(err)
		}
	}
	release2()
	waitFor(t, "lead's job", func() bool { return len(toolCompletedRows(t, ds, lead.Thread)) == 1 })
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.requestsOf("jquiet")); n != 2 {
		t.Fatalf("quiet: %d model calls, want 2", n)
	}
	if n := len(fake.requestsOf("jlead")); n != 2 {
		t.Fatalf("lead: %d model calls, want 2", n)
	}
}

// A tool row a stopped writer left `working` settles `failed` at open, with a
// toolCompleted row saying the job was interrupted.
func TestInterruptedJobSettlesFailed(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	ctx := context.Background()
	held, _ := heldTurn(t)
	fake.script("jlead",
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig"}`}}},
		fakeTurn{content: "started"},
	)
	fake.script("jdig", fakeTurn{content: "never", release: held})
	res, err := ds.CallAgent(ctx, jobsPackage+"/lead", "dig")
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.settleInterruptedJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if state := jobStateOf(jobToolRow(t, ds, res.Thread, "call_0")); state != jobFailed {
		t.Fatalf("job %s, want failed", state)
	}
	done := toolCompletedRows(t, ds, res.Thread)
	if len(done) != 1 || !strings.Contains(done[0]["output"].(string), "interrupted") {
		t.Fatalf("toolCompleted %v", done)
	}
}
