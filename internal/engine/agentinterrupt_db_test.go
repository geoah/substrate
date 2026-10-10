package engine

// An agent run a stopped writer left behind: the next open settles its thread
// and parks its claim naming the stop, a run started after that open is
// listed as in flight, never as interrupted (settleInterruptedAgentRuns,
// presentFailure), and the open's first dispatcher pass reruns the parked
// delivery once (rerunInterruptedAgentRuns, decision 0151).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// restartableAgentDataset is openAgentDataset on a service restart() can stop
// and open again over the same database and data root: the next process.
// extra options apply to every process.
func restartableAgentDataset(t *testing.T, extra ...Option) (*dataset, *fakeLLM, func() *dataset) {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	opts := append([]Option{WithDataRoot(t.TempDir()), WithKindsDir(SeedKindsDir), WithCredentialKey(TestCredentialKey)}, extra...)
	svc, err := OpenForTest(t, ctx, dsn, opts...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds := d.(*dataset)
	importVocabulary(t, ds, "tasks")
	fake := provisionAgents(t, ds)
	restart := func() *dataset {
		t.Helper()
		if err := svc.Close(); err != nil {
			t.Fatalf("stop: %v", err)
		}
		svc, err = OpenForTest(t, ctx, dsn, opts...)
		if err != nil {
			t.Fatalf("restart engine: %v", err)
		}
		d, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatalf("reopen dataset: %v", err)
		}
		return d.(*dataset)
	}
	return ds, fake, restart
}

// keeperWebhook puts a webhook trigger running the keeper agent.
func keeperWebhook(t *testing.T, ds *dataset) string {
	t.Helper()
	tr, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"webhook": map[string]any{}},
			"callable": vocabulary.RecordPath(kindAgent, crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	return tr.ID
}

// heldKeeperFire starts one webhook fire whose first model turn holds until
// release is called, and returns once the loop is inside that turn: the claim
// has committed and the thread is `running`. stop cancels the fire and waits
// for it to return, the process dying mid-loop. Both run again at cleanup,
// before the fake server's Close, which waits on a held turn.
func heldKeeperFire(t *testing.T, ds *dataset, fake *fakeLLM, trigger string) (stop, release func()) {
	t.Helper()
	arrived := make(chan struct{})
	held := make(chan struct{})
	fake.script("keep", fakeTurn{content: "kept", arrived: arrived, release: held})
	fctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(held) }) }
	stop = func() {
		cancel()
		<-done
	}
	t.Cleanup(func() {
		release()
		stop()
	})
	go func() {
		defer close(done)
		_, _ = ds.svc.receiveWebhook(fctx, testdb.Repository(t), trigger, "", substrate.WebhookRequest{
			Method: "POST", ContentType: "application/json",
			Headers: map[string]string{"content-type": "application/json"},
			Body:    []byte(`{"say":"agent"}`),
		}, webhookFireInline)
	}()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the loop never reached its first model turn")
	}
	return stop, release
}

func triggerStatusOf(t *testing.T, ds *dataset, id string) substrate.TriggerStatus {
	t.Helper()
	statuses, err := ds.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	for _, st := range statuses {
		if st.ID == id {
			return st
		}
	}
	t.Fatalf("no status for trigger %s", id)
	return substrate.TriggerStatus{}
}

func TestARestartSettlesTheAgentRunItInterrupted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, restart := restartableAgentDataset(t)
	trigger := keeperWebhook(t, ds)
	stop, release := heldKeeperFire(t, ds, fake, trigger)
	stop()
	release()
	threads := agentThreadsOf(t, ds, "keeper")
	if len(threads) != 1 || threads[0]["status"] != threadRunning {
		t.Fatalf("keeper threads before the restart = %+v, want one left running", threads)
	}
	// A claim an earlier binary wrote, and an interrupted run it parked, in
	// the words it wrote.
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at)
		VALUES (-9, $1, 0, 'fire-legacy', '', 0, $2, $4), (-8, $1, 0, 'fire-legacy-parked', '', 1, $3, $4)`,
		trigger, legacyInFlightError, legacyInterruptedAgentError, nowUTC()); err != nil {
		t.Fatal(err)
	}

	ds = restart()

	threads = agentThreadsOf(t, ds, "keeper")
	if len(threads) != 1 || threads[0]["status"] != threadError || threads[0]["reason"] != interruptedThreadReason {
		t.Fatalf("keeper threads after the restart = %+v, want one settled to error naming the stop", threads)
	}
	if fin, _ := threads[0]["finishedAt"].(string); fin == "" {
		t.Fatalf("the settled thread has no finishedAt: %+v", threads[0])
	}
	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 3 {
		t.Fatalf("failures after the restart = %+v (%v), want the two interrupted claims and the legacy park", failures, err)
	}
	for _, f := range failures {
		if f.Running || f.LastError != interruptedAgentError || f.Attempts != 1 {
			t.Fatalf("failure %+v after the restart, want an interrupted run parked after one attempt", f)
		}
	}
	// The rewrite is stored, not only presented: the rows are no longer
	// claims, and the ledger carries them.
	var claims, ledger int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM trigger_failures WHERE trigger_id = $1 AND last_error <> $2`,
		trigger, interruptedAgentError).Scan(&claims); err != nil || claims != 0 {
		t.Fatalf("stored rows not rewritten: %d (%v)", claims, err)
	}
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog WHERE op = $1 AND record_id = $2 AND payload::text LIKE $3`,
		string(substrate.OpDelivery), trigger, "%interrupted: the server stopped%").Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("delivery entries carrying the rewrite: %d (%v), want one", ledger, err)
	}
	if st := triggerStatusOf(t, ds, trigger); st.Parked != 3 || st.InFlight != 0 || st.Pending != 0 {
		t.Fatalf("status after the restart = %+v, want three parked and nothing in flight", st)
	}
	// The open itself reruns nothing: the first dispatcher pass does
	// (TestTheFirstPassAfterARestartRerunsAnInterruptedAgentRun), and no
	// pass runs here. A hand's retry runs the agent again before that pass.
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 1 {
		t.Fatalf("the open redelivered the run before any pass: %d threads", got)
	}
	var fired int64
	for _, f := range failures {
		if !strings.HasPrefix(f.FireID, "fire-legacy") {
			fired = f.ID
		}
	}
	fake.script("keep", fakeTurn{content: "kept"})
	if _, err := ds.RetryTriggerFailure(ctx, trigger, fired); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if left, err := ds.TriggerFailures(ctx, trigger); err != nil || len(left) != 2 ||
		!strings.HasPrefix(left[0].FireID, "fire-legacy") || !strings.HasPrefix(left[1].FireID, "fire-legacy") {
		t.Fatalf("failures after the retry = %+v (%v), want the two legacy rows alone", left, err)
	}
}

// interruptedKeeperRun leaves one keeper webhook delivery interrupted by a
// stop and opens the next process over it. The dataset returned is that
// process before its first dispatcher pass, the failure is the interrupted
// delivery's parked row, and logs collects every process's log.
func interruptedKeeperRun(t *testing.T, logs *syncBuffer) (ds *dataset, fake *fakeLLM, restart func() *dataset, trigger string, failure int64) {
	t.Helper()
	ds, fake, restart = restartableAgentDataset(t, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	trigger = keeperWebhook(t, ds)
	stop, release := heldKeeperFire(t, ds, fake, trigger)
	stop()
	release()

	ds = restart()

	failures, err := ds.TriggerFailures(context.Background(), trigger)
	if err != nil || len(failures) != 1 || failures[0].LastError != interruptedAgentError || failures[0].Attempts != 1 {
		t.Fatalf("failures after the restart = %+v (%v), want one interrupted run at attempt 1", failures, err)
	}
	return ds, fake, restart, trigger, failures[0].ID
}

// waitRerunWalked waits until the open's rerun walk has returned.
func waitRerunWalked(t *testing.T, ds *dataset) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for ds.interruptedRerun.Load() != rerunWalked {
		if time.Now().After(deadline) {
			t.Fatalf("the rerun walk never returned: stage %d", ds.interruptedRerun.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// rerunLogLines is every log line about the rerun of one failure.
func rerunLogLines(logs *syncBuffer, failure int64) []string {
	var out []string
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if strings.Contains(line, "a restart interrupted") && strings.Contains(line, fmt.Sprintf("failure=%d ", failure)) {
			out = append(out, line)
		}
	}
	return out
}

// storedFailure reads a failure row's stored error and attempt count, what
// the next open's sweep and rerun read.
func storedFailure(t *testing.T, ds *dataset, failure int64) (string, int) {
	t.Helper()
	var lastError string
	var attempts int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT last_error, attempts FROM trigger_failures WHERE id = $1`, failure).Scan(&lastError, &attempts); err != nil {
		t.Fatalf("read failure %d: %v", failure, err)
	}
	return lastError, attempts
}

func TestTheFirstPassAfterARestartRerunsAnInterruptedAgentRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs syncBuffer
	ds, fake, _, trigger, failure := interruptedKeeperRun(t, &logs)
	fake.script("keep", fakeTurn{content: "kept"})

	// One pass, and no hand.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	waitRerunWalked(t, ds)

	if left, err := ds.TriggerFailures(ctx, trigger); err != nil || len(left) != 0 {
		t.Fatalf("failures after the rerun = %+v (%v), want none", left, err)
	}
	threads := agentThreadsOf(t, ds, "keeper")
	if len(threads) != 2 || threads[0]["status"] != threadError || threads[1]["status"] != threadOK {
		t.Fatalf("keeper threads after the rerun = %+v, want the interrupted one and the rerun's, settled ok", threads)
	}
	if st := triggerStatusOf(t, ds, trigger); st.Parked != 0 || st.InFlight != 0 || st.Pending != 0 {
		t.Fatalf("status after the rerun = %+v, want nothing parked or in flight", st)
	}
	// The row was rewritten to attempt 2 on a delivery entry of its own
	// before the rerun's first write.
	var marked, firstRerunWrite int64
	if err := ds.db.QueryRowContext(ctx, `
		SELECT coalesce(max(seq), 0) FROM changelog
		WHERE op = $1 AND record_id = $2 AND payload::text LIKE $3`,
		string(substrate.OpDelivery), trigger, "%started it once more%").Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if err := ds.db.QueryRowContext(ctx, `SELECT min(seq) FROM changelog WHERE kind = $1 AND record_id = $2`,
		typeThread, threads[1]["__id"]).Scan(&firstRerunWrite); err != nil {
		t.Fatal(err)
	}
	if marked == 0 || marked >= firstRerunWrite {
		t.Fatalf("rerun mark at seq %d, want a delivery entry below the rerun's first write %d", marked, firstRerunWrite)
	}
	lines := rerunLogLines(&logs, failure)
	if len(lines) != 1 || !strings.Contains(lines[0], "reran an agent delivery") || !strings.Contains(lines[0], "outcome=ok") ||
		!strings.Contains(lines[0], "trigger="+trigger) {
		t.Fatalf("rerun log lines = %q, want one naming the trigger, the failure and outcome=ok", lines)
	}
}

func TestAnAgentRunInterruptedDuringItsRerunStaysParked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs syncBuffer
	ds, fake, restart, trigger, failure := interruptedKeeperRun(t, &logs)
	arrived := make(chan struct{})
	held := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	fake.script("keep", fakeTurn{content: "kept", arrived: arrived, release: held})

	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the rerun never reached its first model turn")
	}
	// The rerun runs: its row already reads attempt 2, it lists as running,
	// and a hand's retry answers conflict and starts no second loop.
	if lastError, attempts := storedFailure(t, ds, failure); lastError != rerunAgentError || attempts != 2 {
		t.Fatalf("stored row under the rerun = %q at attempt %d, want the rerun mark at attempt 2", lastError, attempts)
	}
	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 1 || !failures[0].Running {
		t.Fatalf("failures under the rerun = %+v (%v), want the one row running", failures, err)
	}
	if _, err := ds.RetryTriggerFailure(ctx, trigger, failure); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a hand's retry under the rerun answered %v, want conflict", err)
	}
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 2 {
		t.Fatalf("keeper threads under the rerun: %d, want the interrupted one and the rerun's", got)
	}

	// The server stops again, mid-rerun.
	ds = restart()
	release()

	failures, err = ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 1 || failures[0].Running || failures[0].LastError != rerunAgentError || failures[0].Attempts != 2 {
		t.Fatalf("failures after the second stop = %+v (%v), want the rerun mark at attempt 2", failures, err)
	}
	if lines := rerunLogLines(&logs, failure); len(lines) != 1 || !strings.Contains(lines[0], "outcome=canceled") {
		t.Fatalf("rerun log lines after the second stop = %q, want one with outcome=canceled", lines)
	}

	// This open's first pass reruns nothing: the row stays for a hand.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	waitRerunWalked(t, ds)
	threads := agentThreadsOf(t, ds, "keeper")
	if len(threads) != 2 || threads[0]["status"] != threadError || threads[1]["status"] != threadError {
		t.Fatalf("keeper threads after the next open's pass = %+v, want the two interrupted runs and no third", threads)
	}
	if lastError, attempts := storedFailure(t, ds, failure); lastError != rerunAgentError || attempts != 2 {
		t.Fatalf("stored row after the next open's pass = %q at attempt %d, want the rerun mark at attempt 2", lastError, attempts)
	}
	if lines := rerunLogLines(&logs, failure); len(lines) != 1 {
		t.Fatalf("rerun log lines after the next open's pass = %q, want only the first rerun's", lines)
	}
	if st := triggerStatusOf(t, ds, trigger); st.Parked != 1 || st.InFlight != 0 {
		t.Fatalf("status after the next open's pass = %+v, want one parked", st)
	}
}

func TestAHandRetryRacingTheRerunRunsTheDeliveryOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs syncBuffer
	ds, fake, _, trigger, failure := interruptedKeeperRun(t, &logs)
	arrived := make(chan struct{})
	held := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	fake.script("keep", fakeTurn{content: "kept", arrived: arrived, release: held})

	// A hand retries the delivery before the first pass, and its loop holds.
	retried := make(chan error, 1)
	go func() {
		_, err := ds.RetryTriggerFailure(ctx, trigger, failure)
		retried <- err
	}()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the hand's retry never reached its first model turn")
	}

	// The pass finds the row held and leaves it to the hand.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	waitRerunWalked(t, ds)
	if lines := rerunLogLines(&logs, failure); len(lines) != 1 || !strings.Contains(lines[0], "outcome=running") {
		t.Fatalf("rerun log lines = %q, want one with outcome=running", lines)
	}
	if lastError, attempts := storedFailure(t, ds, failure); lastError != interruptedAgentError || attempts != 1 {
		t.Fatalf("stored row under the hand's retry = %q at attempt %d, want it untouched by the rerun", lastError, attempts)
	}

	release()
	select {
	case err := <-retried:
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the hand's retry never returned")
	}
	if left, err := ds.TriggerFailures(ctx, trigger); err != nil || len(left) != 0 {
		t.Fatalf("failures after the retry = %+v (%v), want none", left, err)
	}
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 2 {
		t.Fatalf("keeper threads: %d, want the interrupted one and the hand's", got)
	}
	if got := len(fake.requestsOf("keep")); got != 2 {
		t.Fatalf("model requests: %d, want the interrupted run's and the hand's", got)
	}
}

func TestAnAgentRunStartedAfterARestartIsListedInFlight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, restart := restartableAgentDataset(t)
	trigger := keeperWebhook(t, ds)
	stop, release := heldKeeperFire(t, ds, fake, trigger)
	stop()
	release()

	ds = restart()

	_, releaseLive := heldKeeperFire(t, ds, fake, trigger)
	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 2 {
		t.Fatalf("failures under the running loop = %+v (%v), want the interrupted run and the live one", failures, err)
	}
	var live, dead int
	for _, f := range failures {
		switch {
		case f.Running:
			live++
			if strings.Contains(f.LastError, "restart") || strings.Contains(f.LastError, "interrupted") {
				t.Fatalf("the live run reads as interrupted: %+v", f)
			}
		case f.LastError == interruptedAgentError:
			dead++
		default:
			t.Fatalf("failure %+v is neither the live run nor the interrupted one", f)
		}
	}
	if live != 1 || dead != 1 {
		t.Fatalf("failures = %+v, want one running and one interrupted", failures)
	}
	if st := triggerStatusOf(t, ds, trigger); st.Parked != 1 || st.InFlight != 1 || st.LastParkedError != interruptedAgentError {
		t.Fatalf("status under the running loop = %+v, want one parked, interrupted, and one in flight", st)
	}

	// The live run settles: its row goes, the interrupted one stays.
	releaseLive()
	deadline := time.Now().Add(30 * time.Second)
	for {
		left, err := ds.TriggerFailures(ctx, trigger)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) == 1 && left[0].LastError == interruptedAgentError {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("failures after the live run settled = %+v", left)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// A run this process started and lost without settling (its fire was
	// canceled) is no longer held, so it reads as interrupted too.
	stop, release = heldKeeperFire(t, ds, fake, trigger)
	stop()
	release()
	failures, err = ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 2 {
		t.Fatalf("failures after the canceled run = %+v (%v)", failures, err)
	}
	for _, f := range failures {
		if f.Running || f.LastError != interruptedAgentError {
			t.Fatalf("failure %+v after the canceled run, want interrupted", f)
		}
	}
	if st := triggerStatusOf(t, ds, trigger); st.Parked != 2 || st.InFlight != 0 {
		t.Fatalf("status after the canceled run = %+v, want two parked", st)
	}
}

// The rerun is a retry the server runs, so it delivers to the trigger as the
// trigger stands then: a trigger edited to another agent since the claim
// reruns the parked fire on that agent, and the log line names it.
func TestARerunRunsTheCallableTheTriggerNamesNow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs syncBuffer
	ds, fake, restart := restartableAgentDataset(t, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	trigger := keeperWebhook(t, ds)
	stop, release := heldKeeperFire(t, ds, fake, trigger)
	stop()
	release()
	scribe := vocabulary.RecordPath(kindAgent, crewPackage+"/scribe")
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: trigger,
		Properties: map[string]any{
			"source":   map[string]any{"webhook": map[string]any{}},
			"callable": scribe,
		},
	}); err != nil {
		t.Fatalf("point the trigger at the scribe: %v", err)
	}

	ds = restart()

	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 1 || failures[0].LastError != interruptedAgentError {
		t.Fatalf("failures after the restart = %+v (%v), want the keeper's interrupted run", failures, err)
	}
	fake.script("sub", fakeTurn{content: "noted"})
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	waitRerunWalked(t, ds)

	if left, err := ds.TriggerFailures(ctx, trigger); err != nil || len(left) != 0 {
		t.Fatalf("failures after the rerun = %+v (%v), want none", left, err)
	}
	if keeper := agentThreadsOf(t, ds, "keeper"); len(keeper) != 1 || keeper[0]["status"] != threadError {
		t.Fatalf("keeper threads = %+v, want the interrupted run alone", keeper)
	}
	if scribes := agentThreadsOf(t, ds, "scribe"); len(scribes) != 1 || scribes[0]["status"] != threadOK {
		t.Fatalf("scribe threads = %+v, want the rerun, settled ok", scribes)
	}
	lines := rerunLogLines(&logs, failures[0].ID)
	if len(lines) != 1 || !strings.Contains(lines[0], "callable="+scribe+" ") || !strings.Contains(lines[0], "outcome=ok") {
		t.Fatalf("rerun log lines = %q, want one naming the scribe with outcome=ok", lines)
	}
}

// A function body that runs an agent claims its delivery when the agent's
// thread opens (decision 0121). A process that dies there leaves the claim,
// the next open parks it as interrupted, and the first pass reruns the
// delivery, so the body runs again from its start (decision 0151).
func TestTheFirstPassAfterARestartRerunsAFunctionBodysInterruptedAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs syncBuffer
	ds, fake, restart := restartableAgentDataset(t, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	installRelay(t, ds, fake)
	noter := vocabulary.RecordPath("substrate.reamde.dev/core/function", relayPackage+"/noter")
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": noter,
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	arrived := make(chan struct{})
	held := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	fake.script("sub", fakeTurn{content: "noted", arrived: arrived, release: held})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-crash", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	dying := ds
	passed := make(chan struct{})
	go func() {
		defer close(passed)
		_, _ = dying.ProcessTriggers(ctx)
	}()
	select {
	case <-arrived:
	case <-time.After(60 * time.Second):
		t.Fatal("the body's agent never reached its first model turn")
	}

	// The process dies with the scribe's thread open and the claim
	// committed: from here it commits nothing, so the body cannot park the
	// delivery on its way out, as a killed process would not.
	dying.writerMu.Lock()
	dying.fileErr = errors.New("test: the process died")
	dying.writerMu.Unlock()
	ds = restart()
	release()
	select {
	case <-passed:
	case <-time.After(60 * time.Second):
		t.Fatal("the dead process's pass never returned")
	}

	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 || failures[0].LastError != interruptedAgentError || failures[0].Attempts != 1 {
		t.Fatalf("failures after the restart = %+v (%v), want the body's claim parked as interrupted", failures, err)
	}
	if scribes := agentThreadsOf(t, ds, "scribe"); len(scribes) != 1 || scribes[0]["status"] != threadError {
		t.Fatalf("scribe threads after the restart = %+v, want the interrupted one settled to error", scribes)
	}
	fake.script("sub", fakeTurn{content: "noted"})
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	waitRerunWalked(t, ds)

	if left, err := ds.TriggerFailures(ctx, tr.ID); err != nil || len(left) != 0 {
		t.Fatalf("failures after the rerun = %+v (%v), want none", left, err)
	}
	if scribes := agentThreadsOf(t, ds, "scribe"); len(scribes) != 2 || scribes[1]["status"] != threadOK {
		t.Fatalf("scribe threads after the rerun = %+v, want the body's agent run again and settled ok", scribes)
	}
	lines := rerunLogLines(&logs, failures[0].ID)
	if len(lines) != 1 || !strings.Contains(lines[0], "callable="+noter+" ") || !strings.Contains(lines[0], "outcome=ok") {
		t.Fatalf("rerun log lines = %q, want one naming the function with outcome=ok", lines)
	}
}

// An agent at a spend cap is not rerun: refused inside its loop, the rerun
// would park at attempt 2 having run nothing. The row stays as the sweep
// left it, and the next open reruns it once the cap admits the agent.
func TestARerunWaitsWhileItsAgentIsAtASpendCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs syncBuffer
	ds, fake, restart, trigger, failure := interruptedKeeperRun(t, &logs)
	setCap := func(ds *dataset, cents string) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: kindSetting, ID: spendSettingID,
			Properties: map[string]any{"value": cents, "type": "int"},
		}); err != nil {
			t.Fatalf("put the repository spend cap %s: %v", cents, err)
		}
	}
	setCap(ds, "0")

	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	waitRerunWalked(t, ds)
	if lastError, attempts := storedFailure(t, ds, failure); lastError != interruptedAgentError || attempts != 1 {
		t.Fatalf("stored row at the cap = %q at attempt %d, want it as the sweep left it", lastError, attempts)
	}
	if got := len(fake.requestsOf("keep")); got != 1 {
		t.Fatalf("model requests at the cap: %d, want only the interrupted run's", got)
	}
	if lines := rerunLogLines(&logs, failure); len(lines) != 1 || !strings.Contains(lines[0], "outcome=capped") {
		t.Fatalf("rerun log lines at the cap = %q, want one with outcome=capped", lines)
	}

	setCap(ds, "100000")
	ds = restart()
	fake.script("keep", fakeTurn{content: "kept"})
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	waitRerunWalked(t, ds)
	if left, err := ds.TriggerFailures(ctx, trigger); err != nil || len(left) != 0 {
		t.Fatalf("failures after the next open's rerun = %+v (%v), want none", left, err)
	}
	if lines := rerunLogLines(&logs, failure); len(lines) != 2 || !strings.Contains(lines[1], "outcome=ok") {
		t.Fatalf("rerun log lines after the next open = %q, want the capped one and then outcome=ok", lines)
	}
}
