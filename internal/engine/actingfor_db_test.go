package engine

// WHOM LATER WORK ACTS FOR (decision 0270). Work that goes on after the
// request that started it has returned acts for whoever the line that
// created its own record names: a job's tool row, an approval, the
// resolution that starts a resumed turn, the request a judge decides, and the
// line that gave a thread's run its lease. Each test opens its thread for no
// one, as a trigger's run does, and has the person continue it, so the
// thread's first line names no one and a reader of it acts for no one.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// continuedByThePerson opens a thread of agent for no one, then has the
// person continue it with message. The opening takes the agent's next
// scripted turn.
func continuedByThePerson(t *testing.T, ds *dataset, agent, message string) string {
	t.Helper()
	opened, err := ds.CallAgent(context.Background(), agent, "open")
	if err != nil {
		t.Fatalf("open the thread for no one: %v", err)
	}
	var behalf string
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT on_behalf_of FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 1`,
		typeThread, opened.Thread).Scan(&behalf); err != nil {
		t.Fatal(err)
	}
	if behalf != "" {
		t.Fatalf("the thread opened for %q, want no one: the test proves nothing", behalf)
	}
	person := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	if _, err := ds.ChatAgent(person, substrate.ActorPerson, agent, substrate.ChatInput{Thread: opened.Thread, Message: message}, func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("the person continues the thread: %v", err)
	}
	return opened.Thread
}

// allForThePerson fails on every line after seq that does not act for the
// person, and on none at all.
func allForThePerson(t *testing.T, ds *dataset, after int64, what string) {
	t.Helper()
	later := actingForSince(t, ds, after)
	if len(later) == 0 {
		t.Fatalf("%s wrote nothing: the test proves nothing", what)
	}
	person := actorID(substrate.ActorPerson)
	for seq, line := range later {
		if line[1] != person {
			t.Errorf("seq %d (%s), %s, acts for %q, want the person", seq, line[0], what, line[1])
		}
	}
}

// A background job the person's turn started in a thread opened for no one,
// and the turn its end resumes, act for the person: the job's tool row and
// its end's row are the person's turn's.
func TestAJobInAContinuedThreadActsForThePersonWhoseTurnStartedIt(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	held, release := heldTurn(t)
	fake.script("jlead",
		fakeTurn{content: "ready"},
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig the archive"}`}}},
		fakeTurn{content: "started the dig"},
		fakeTurn{content: "the dig is done"},
	)
	fake.script("jdig", fakeTurn{content: "found the deed", release: held})
	thread := continuedByThePerson(t, ds, jobsPackage+"/lead", "find the deed")
	foreground := maxSeqOf(t, ds)
	release()
	waitFor(t, "the resume's reply", func() bool {
		_, props := threadStatusOf(t, ds, thread)
		return props[propThreadLastReply] == "the dig is done"
	})
	allForThePerson(t, ds, foreground, "written by the job and its resume")
}

// A job a restart interrupted settles for whom its tool row's line names.
func TestAnInterruptedJobSettlesForThePersonWhoseTurnStartedIt(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	held, _ := heldTurn(t)
	fake.script("jlead",
		fakeTurn{content: "ready"},
		fakeTurn{calls: []fakeCall{{name: "digger", args: `{"input":"dig"}`}}},
		fakeTurn{content: "started"},
	)
	fake.script("jdig", fakeTurn{content: "never", release: held})
	thread := continuedByThePerson(t, ds, jobsPackage+"/lead", "dig")
	before := maxSeqOf(t, ds)
	if err := ds.settleInterruptedJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state := jobStateOf(jobToolRow(t, ds, thread, "call_0")); state != jobFailed {
		t.Fatalf("job %s, want failed", state)
	}
	allForThePerson(t, ds, before, "written by the settlement")
}

// An approval the person's turn asked for in a thread opened for no one: the
// approved call, its report and the turn the report resumes act for the
// person.
func TestAnApprovedCallInAContinuedThreadActsForThePersonWhoseTurnAskedForIt(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	fake.script("amail",
		fakeTurn{content: "ready"},
		fakeTurn{calls: []fakeCall{{name: "send", args: `{"text":"hello"}`}}},
		fakeTurn{content: "It is waiting for your approval."},
		fakeTurn{content: "Sent."},
	)
	thread := continuedByThePerson(t, ds, apprPackage+"/mailer", "send hello")
	held := approvalsOf(t, ds, thread)
	if len(held) != 1 {
		t.Fatalf("approvals on the thread = %d, want 1", len(held))
	}
	if err := decideApproval(t, ds, substrate.ActorPerson, held[0].ID, approvalApproved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	decided := maxSeqOf(t, ds)
	waitFor(t, "the resume's reply", func() bool {
		_, props := threadStatusOf(t, ds, thread)
		return props[propThreadLastReply] == "Sent."
	})
	if !sentLive(t, ds, "hello") {
		t.Fatal("the approved call did not run: the test proves nothing")
	}
	allForThePerson(t, ds, decided, "written after the decision")
}

// An approval a restart cut off is reported for whom its line names.
func TestAnInterruptedApprovalIsReportedForThePersonWhoseTurnAskedForIt(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	ctx := context.Background()
	fake.script("amail",
		fakeTurn{content: "ready"},
		fakeTurn{calls: []fakeCall{{name: "send", args: `{"text":"cut"}`}}},
		fakeTurn{content: "waiting"},
	)
	thread := continuedByThePerson(t, ds, apprPackage+"/mailer", "send cut")
	a := approvalsOf(t, ds, thread)[0]
	// Approved and claimed, the settle left out: what a stop between the
	// claim and the end leaves.
	version := a.Version
	if err := ds.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
		_, err := tx.patch(eref{Kind: vocabulary.KindLLMApproval, ID: a.ID}, substrate.PatchInput{
			Properties: map[string]any{propApprovalState: approvalApproved}, IfVersion: &version,
		})
		tx.afterCommit = nil
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := ds.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
		tx.approvalRun = true
		_, err := tx.patch(eref{Kind: vocabulary.KindLLMApproval, ID: a.ID}, substrate.PatchInput{
			Properties: map[string]any{propApprovalState: approvalRunning},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := maxSeqOf(t, ds)
	if err := ds.settleInterruptedApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if got := approvalRow(t, ds, a.ID); got.States[propApprovalState] != approvalFailed {
		t.Fatalf("approval after the sweep %v", got.States)
	}
	allForThePerson(t, ds, before, "written by the report")
}

// A thread whose run a restart interrupted, or whose lease ran out, settles
// for whom the line that gave that run its lease names: the claim of the
// turn, not the thread's first line. A run that started for no one settles
// for no one.
func TestAThreadsSettlementActsForWhomItsRunsLeaseLineNames(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	fake.script("jlead", fakeTurn{content: "ready"}, fakeTurn{content: "hello back"})
	thread := continuedByThePerson(t, ds, jobsPackage+"/lead", "hello")
	ag, err := ds.registry().ResolveAgent(jobsPackage + "/lead")
	if err != nil {
		t.Fatal(err)
	}
	// claim writes what claimThread writes for a run acting as ctx says.
	claim := func(ctx context.Context, until time.Time) {
		t.Helper()
		if err := ds.inTx(ctx, substrate.Actor(ag.Actor()), false, func(tx *txn) error {
			_, err := tx.patch(eref{Kind: typeThread, ID: thread}, substrate.PatchInput{Properties: map[string]any{
				"status": threadRunning, "leaseUntil": until.Format(time.RFC3339Nano),
			}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	person := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	settledFor := func(after int64) string {
		t.Helper()
		var behalf string
		if err := ds.db.QueryRowContext(context.Background(), `
			SELECT on_behalf_of FROM changelog WHERE kind = $1 AND record_id = $2 AND seq > $3 ORDER BY seq DESC LIMIT 1`,
			typeThread, thread, after).Scan(&behalf); err != nil {
			t.Fatalf("read the settlement's line: %v", err)
		}
		return behalf
	}

	claim(person, nowUTC().Add(time.Hour))
	before := maxSeqOf(t, ds)
	if err := ds.settleInterruptedThreads(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := settledFor(before); got != actorID(substrate.ActorPerson) {
		t.Fatalf("the interrupted thread settled for %q, want the person", got)
	}

	claim(person, nowUTC().Add(-time.Minute))
	before = maxSeqOf(t, ds)
	if err := ds.settleLostThreads(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := settledFor(before); got != actorID(substrate.ActorPerson) {
		t.Fatalf("the lost thread settled for %q, want the person", got)
	}

	claim(context.Background(), nowUTC().Add(time.Hour))
	before = maxSeqOf(t, ds)
	if err := ds.settleInterruptedThreads(substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)); err != nil {
		t.Fatal(err)
	}
	if got := settledFor(before); got != "" {
		t.Fatalf("a run for no one settled for %q", got)
	}
}

// The sweep resumes a settled thread whose resolution's notice was lost: the
// turn acts for whom the resolution's line names, not the thread's first line.
func TestTheSweepsResumeActsForWhomTheResolutionsLineNames(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	fake.script("jlead", fakeTurn{content: "ready"}, fakeTurn{content: "noted"})
	opened, err := ds.CallAgent(context.Background(), jobsPackage+"/lead", "open")
	if err != nil {
		t.Fatal(err)
	}
	person := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	if err := ds.inTx(person, substrate.ActorSystem, true, func(tx *txn) error {
		return tx.putThreadSystemRow(opened.Thread, map[string]any{"event": "recordResolved"}, nil)
	}); err != nil {
		t.Fatal(err)
	}
	before := maxSeqOf(t, ds)
	if n, err := ds.SweepResolutions(context.Background()); err != nil || n != 1 {
		t.Fatalf("the sweep resumed %d threads (%v), want 1", n, err)
	}
	waitFor(t, "the resume's reply", func() bool {
		_, props := threadStatusOf(t, ds, opened.Thread)
		return props[propThreadLastReply] == "noted"
	})
	allForThePerson(t, ds, before, "written by the resumed turn")
}

// A judge acts for whom the request it decides names: the agent's write the
// policy held, which the person's turn made.
func TestTheJudgeActsForWhomTheRequestsLineNames(t *testing.T) {
	ds, fake := openAgentDataset(t)
	gatePolicyWithJudge(t, ds, "judged-widgets", map[string]any{"autoAccept": 0.9})
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", fid("w-judged"), map[string]any{"name": "wanted"})}}},
		fakeTurn{content: "held, waiting on the judge."},
		fakeTurn{content: "the judge let it through."},
	)
	fake.script("vjudge", fakeTurn{content: `{"verdict":"accept","confidence":0.97,"rationale":"small and honest"}`})
	person := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	if _, err := ds.CallAgent(person, crewPackage+"/editor", "make a widget"); err != nil {
		t.Fatal(err)
	}
	req := onlyPatchRequest(t, ds)
	created := firstSeqOf(t, ds, vocabulary.KindRecordPatchRequest, req.ID)
	judgedAnnotation(t, ds, req.ID)
	waitUntil(t, "the judged accept landing", func() bool {
		got, err := ds.Get(context.Background(), crewPackage+"/widget", fid("w-judged"))
		return err == nil && got.Properties["name"] == "wanted"
	})
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT seq, on_behalf_of FROM changelog WHERE seq > $1 AND (record_id = $2 OR kind = $3)`,
		created, req.ID, crewPackage+"/widget")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var seq int64
		var behalf string
		if err := rows.Scan(&seq, &behalf); err != nil {
			t.Fatal(err)
		}
		n++
		if behalf != actorID(substrate.ActorPerson) {
			t.Errorf("seq %d, the judge's decision or what it applied, acts for %q, want the person", seq, behalf)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("the judge wrote nothing on the request: the test proves nothing")
	}
}

func firstSeqOf(t *testing.T, ds *dataset, kind, id string) int64 {
	t.Helper()
	var seq int64
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT min(seq) FROM changelog WHERE kind = $1 AND record_id = $2`, kind, id).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// A record whose creating line is gone acts for no one, whatever the
// context carried; a read that fails is an error, never no one. The table
// holds no line for the id read here, as it will hold none for a record
// whose history compaction folded away.
func TestWorkWhoseLineIsGoneActsForNoOneAndAFailedReadIsAnError(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	fake.script("jlead", fakeTurn{content: "ready"})
	person := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	opened, err := ds.CallAgent(person, jobsPackage+"/lead", "open")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ds.actingFor(context.Background(), typeThread, opened.Thread)
	if err != nil || substrate.OnBehalfOfFrom(got) != substrate.ActorPerson {
		t.Fatalf("the thread's line acts for %q (%v), want the person", substrate.OnBehalfOfFrom(got), err)
	}
	gone := fid("thread-gone")
	for name, read := range map[string]func(context.Context, string) (context.Context, error){
		"its creating line": func(ctx context.Context, id string) (context.Context, error) {
			return ds.actingFor(ctx, typeThread, id)
		},
		"its run's lease line": func(ctx context.Context, id string) (context.Context, error) {
			return ds.actingForRun(ctx, id)
		},
	} {
		got, err := read(person, gone)
		if err != nil || substrate.OnBehalfOfFrom(got) != "" {
			t.Errorf("%s gone: acts for %q (%v), want no one", name, substrate.OnBehalfOfFrom(got), err)
		}
		canceled, cancel := context.WithCancel(person)
		cancel()
		if _, err := read(canceled, opened.Thread); err == nil {
			t.Errorf("%s: a failed read answered no error", name)
		}
	}
}
