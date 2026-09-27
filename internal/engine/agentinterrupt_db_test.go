package engine

// An agent run a stopped writer left behind: the next open settles its thread
// and parks its claim naming the stop, and a run started after that open is
// listed as in flight, never as interrupted (settleInterruptedAgentRuns,
// presentFailure).

import (
	"context"
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
func restartableAgentDataset(t *testing.T) (*dataset, *fakeLLM, func() *dataset) {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	opts := []Option{WithDataRoot(t.TempDir()), WithKindsDir(SeedKindsDir), WithCredentialKey(TestCredentialKey)}
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
	// A claim an earlier binary wrote, in the words it wrote.
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at)
		VALUES (-9, $1, 0, 'fire-legacy', '', 0, $2, $3)`, trigger, legacyInFlightError, nowUTC()); err != nil {
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
	if err != nil || len(failures) != 2 {
		t.Fatalf("failures after the restart = %+v (%v), want the two interrupted claims", failures, err)
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
	if st := triggerStatusOf(t, ds, trigger); st.Parked != 2 || st.InFlight != 0 || st.Pending != 0 {
		t.Fatalf("status after the restart = %+v, want two parked and nothing in flight", st)
	}
	// Nothing reruns it by itself; a hand's retry runs the agent again.
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 1 {
		t.Fatalf("the restart redelivered the run: %d threads", got)
	}
	var fired int64
	for _, f := range failures {
		if f.FireID != "fire-legacy" {
			fired = f.ID
		}
	}
	fake.script("keep", fakeTurn{content: "kept"})
	if _, err := ds.RetryTriggerFailure(ctx, trigger, fired); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if left, err := ds.TriggerFailures(ctx, trigger); err != nil || len(left) != 1 || left[0].FireID != "fire-legacy" {
		t.Fatalf("failures after the retry = %+v (%v), want the legacy claim alone", left, err)
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
