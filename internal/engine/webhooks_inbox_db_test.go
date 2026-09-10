package engine_test

// The webhook door records a request before it answers (decision 0068): the
// admitted request is a pending entry in the delivery ledger, the fire is
// that entry's retry, and a process that stops after the 202, or mid-fire
// before the effects committed, leaves the fire to the trigger dispatcher's
// next pass, which runs it under the fire id the sender was given. None of
// these tests is parallel: BlobUploadGrace is process-wide.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// hookHoldSource echoes like hookEchoSource, and sleeps first until a widget
// named `release` exists: the fire a test cancels mid-run.
var hookHoldSource = hookEchoSource + `
import time

def main(input, host):
    if host.records.get(WIDGET, "release") is None:
        time.sleep(30)
    return echo(input, host, "held-echo")
`

// heldAudio is the file part the multipart shape carries.
var heldAudio = []byte("bytes of a voice note, held until the fire runs")

// heldRequests are the two request shapes the door records: a raw body,
// spooled to the blob store at admission, and a multipart request whose file
// part was spooled before it and whose inline value is spooled with the
// body. Each says what the echo must carry back and which bytes must never
// reach the changelog: the raw body, the file part's bytes and the inline
// value.
var heldRequests = map[string]struct {
	req    substrate.WebhookRequest
	name   string
	blob   bool
	absent []string
}{
	"a json body": {req: jsonHook(`{"say":"stopped"}`, "held"), name: `{"say":"stopped"}`, absent: []string{`"say":"stopped"`}},
	"a multipart request": {
		req: substrate.WebhookRequest{
			Method: "POST", ContentType: "multipart/form-data",
			Headers: map[string]string{"x-github-event": "held"},
			Parts: []substrate.WebhookPart{
				{Name: "transcription", Value: "stopped"},
				{Name: "audio", Filename: "note.m4a", MediaType: "audio/mp4", Data: heldAudio},
			},
		},
		name: "stopped", blob: true, absent: []string{string(heldAudio), "stopped"},
	},
}

// newHeldHookDataset is a repository with one open webhook trigger whose body
// sleeps until a `release` widget exists, written here when released is set,
// plus the seat and root a reopen needs. Every invocation of the held body is
// announced on the returned channel as the runner is about to start it, so a
// test that must act mid-fire waits for that and never for a clock.
func newHeldHookDataset(t *testing.T, released bool) (substrate.Service, substrate.Dataset, string, string, <-chan struct{}) {
	t.Helper()
	ctx := context.Background()
	invoked := make(chan struct{}, 16)
	svc, dsn := newService(t, engine.WithTestInvokeHook(func(function string) {
		if strings.HasSuffix(function, "/hookhold") {
			select {
			case invoked <- struct{}{}:
			default:
			}
		}
	}))
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds)
	connector := fnConnector(
		[]enginetest.Trigger{hookTrigger("hook-held", webhookSource(""), "hookhold", true)},
		pyFn("hookhold", map[string]any{
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{widgetType}}},
		}, []any{widgetType}, hookHoldSource),
	)
	if err := enginetest.Install(ctx, ds, owner, connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	if released {
		releaseHook(t, ds)
	}
	return svc, ds, dsn, engine.DataRootOf(svc), invoked
}

// dispatch runs one trigger dispatcher pass over ds, the pass the server
// gives every repository, which is what resumes a pending request.
func dispatch(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if _, err := ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("dispatcher pass: %v", err)
	}
}

// awaitInvoked waits for the held body to be invoked once.
func awaitInvoked(t *testing.T, invoked <-chan struct{}) {
	t.Helper()
	select {
	case <-invoked:
	case <-time.After(30 * time.Second):
		t.Fatal("the held body was never invoked")
	}
}

// releaseHook lets the held body run through.
func releaseHook(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, ID: "release", Properties: map[string]any{"name": "release"}})
}

// pendingHook asserts the trigger holds exactly one recorded request, the
// one the door answered with fid, that nothing has fired, and that the
// status counts it as pending and not parked.
func pendingHook(t *testing.T, ds substrate.Dataset, fid string) substrate.TriggerFailure {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.Get(ctx, widgetType, "held-echo"); err == nil {
		t.Fatal("the fire ran")
	}
	failures, err := ds.TriggerFailures(ctx, "hook-held")
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].FireID != fid || failures[0].LastError != engine.WebhookPendingError {
		t.Fatalf("recorded requests = %+v, want one pending entry for fire %s", failures, fid)
	}
	statuses, err := ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st.ID == "hook-held" && (st.Pending != 1 || st.Parked != 0) {
			t.Fatalf("status counts pending=%d parked=%d, want 1 and 0", st.Pending, st.Parked)
		}
	}
	return failures[0]
}

// okRunsAfterClose counts a fire id's OK run records from the tamperer's
// seat, once the service that ran them is closed and its background drained.
func okRunsAfterClose(t *testing.T, dsn, fid string) int {
	t.Helper()
	var n int
	if err := rawDB(t, dsn).QueryRow(`
		SELECT count(*) FROM records WHERE kind = 'substrate.reamde.dev/core/run' AND deleted_at IS NULL
		  AND props ->> 'fireId' = $1 AND props ->> 'status' = 'ok'`, fid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// awaitHookEcho polls for the echo widget the resumed fire writes under fid.
func awaitHookEcho(t *testing.T, ds substrate.Dataset, fid string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		rec, err := ds.Get(context.Background(), widgetType, "held-echo")
		if err == nil && rec.Properties["record"] == fid {
			return rec.Properties
		}
		if time.Now().After(deadline) {
			t.Fatalf("the resumed fire never landed under %s; last = %v (%v)", fid, rec, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// okRuns counts the OK run records a fire id owns.
func okRuns(t *testing.T, ds substrate.Dataset, fid string) int {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/run"}}, First: 200,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	n := 0
	for _, r := range page.Records {
		if r.DeletedAt == nil && r.Properties["fireId"] == fid && r.Properties["status"] == "ok" {
			n++
		}
	}
	return n
}

// deliveryLedger joins every `delivery` entry's payload as the table holds
// it, read from the tamperer's seat.
func deliveryLedger(t *testing.T, dsn string) string {
	t.Helper()
	var joined string
	if err := rawDB(t, dsn).QueryRow(`
		SELECT coalesce(string_agg(payload::text, ' '), '') FROM changelog WHERE op = 'delivery'`).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	return joined
}

// noDeliveryInFeed reads the public feed forward and backward and fails on a
// `delivery` entry in either.
func noDeliveryInFeed(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 100000)
	if err != nil {
		t.Fatal(err)
	}
	backward, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{}, 100000)
	if err != nil {
		t.Fatal(err)
	}
	changes = append(changes, backward...)
	for _, ch := range changes {
		if ch.Op == substrate.OpDelivery {
			t.Fatalf("a delivery entry reached the public feed: seq %d", ch.Seq)
		}
	}
}

// resumedHook asserts what one resumed fire left: the echo under the original
// fire id with the request's content, the entry retired, one OK run.
func resumedHook(t *testing.T, ds substrate.Dataset, fid, name string, blob bool) map[string]any {
	t.Helper()
	got := awaitHookEcho(t, ds, fid)
	if got["name"] != name || got["want"] != "held" || got["mode"] != "webhook" || got["op"] != "POST" {
		t.Fatalf("the resumed fire echoed %v", got)
	}
	digest, _ := got["target"].(string)
	if blob != strings.HasPrefix(digest, "blob-sha256-") {
		t.Fatalf("the resumed fire's file part = %q, want a blob digest: %v", digest, blob)
	}
	if blob {
		if _, data, err := ds.GetBlob(context.Background(), digest); err != nil || string(data) != string(heldAudio) {
			t.Fatalf("the file part's bytes at delivery: %v", err)
		}
	}
	if left, err := ds.TriggerFailures(context.Background(), "hook-held"); err != nil || len(left) != 0 {
		t.Fatalf("entries after the resumed fire = %+v, %v", left, err)
	}
	if n := okRuns(t, ds, fid); n != 1 {
		t.Fatalf("OK runs under %s = %d, want 1", fid, n)
	}
	return got
}

// The process stops right after the 202: the request is in the ledger with
// the body spooled by digest and only the kept headers, off the public feed,
// its blobs held past the upload grace; the next dispatcher pass fires it
// once under the fire id the sender was given, and a third process's pass
// finds nothing to fire.
func TestWebhookAcceptedRequestSurvivesAStopBeforeTheFire(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })
	for name, shape := range heldRequests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, ds, dsn, root, _ := newHeldHookDataset(t, true)
			fid, err := engine.ReceiveWebhookHeld(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
			if err != nil {
				t.Fatalf("receive: %v", err)
			}
			if !strings.HasPrefix(fid, "hook-") {
				t.Fatalf("fire id %q does not carry the public prefix", fid)
			}
			pendingHook(t, ds, fid)
			ledger := deliveryLedger(t, dsn)
			for _, absent := range shape.absent {
				if strings.Contains(ledger, absent) {
					t.Fatalf("the ledger carries the request's bytes: %q", absent)
				}
			}
			if !strings.Contains(ledger, `"x-github-event"`) || !strings.Contains(ledger, `"blob"`) {
				t.Fatalf("the recorded request lost its kept header or its blob reference:\n%s", ledger)
			}
			noDeliveryInFeed(t, ds)

			// Every blob the request spooled outlives the orphan sweep past
			// the grace: the pending entry holds it.
			digests := listBlobDigests(t, ds)
			if len(digests) == 0 {
				t.Fatal("the request spooled nothing")
			}
			if _, err := ds.RunGC(ctx); err != nil {
				t.Fatalf("gc: %v", err)
			}
			for _, digest := range digests {
				if _, _, err := ds.GetBlob(ctx, digest); err != nil {
					t.Fatalf("the sweep collected %s while the request was pending: %v", digest, err)
				}
			}

			// The stop, the open that follows it and the dispatcher's pass.
			if err := svc.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			svc2 := mustReopen(t, dsn, root)
			ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
			if err != nil {
				t.Fatalf("reopen the repository: %v", err)
			}
			dispatch(t, ds2)
			resumedHook(t, ds2, fid, shape.name, shape.blob)

			// A third process's pass has nothing to fire: no entry, and one
			// run still once its background has drained.
			if err := svc2.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			svc3 := mustReopen(t, dsn, root)
			ds3, err := svc3.Dataset(ctx, testdb.Repository(t))
			if err != nil {
				t.Fatalf("reopen the repository: %v", err)
			}
			dispatch(t, ds3)
			if err := svc3.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if n := okRunsAfterClose(t, dsn, fid); n != 1 {
				t.Fatalf("OK runs under %s after a third pass = %d, want 1", fid, n)
			}
		})
	}
}

// The fire is canceled while it runs (a shutdown's drain): nothing parks,
// the entry stays pending, and the next dispatcher pass runs it under the
// original fire id.
func TestWebhookFireCancelledMidRunResumesAtTheNextOpen(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })
	for name, shape := range heldRequests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, ds, dsn, root, invoked := newHeldHookDataset(t, false)

			// The door with the fire inline, under a context the test cancels
			// once the runner has started the body, which sleeps until the
			// release: the fire is mid-run and returns without a park.
			fctx, cancel := context.WithCancel(ctx)
			type answer struct {
				fid string
				err error
			}
			done := make(chan answer, 1)
			go func() {
				fid, err := engine.ReceiveWebhookSync(fctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
				done <- answer{fid, err}
			}()
			awaitInvoked(t, invoked)
			cancel()
			got := <-done
			if got.err != nil {
				t.Fatalf("receive: %v", got.err)
			}
			pendingHook(t, ds, got.fid)
			if _, err := ds.RunGC(ctx); err != nil {
				t.Fatalf("gc: %v", err)
			}

			// Release the body, stop, open: the same request runs to its end.
			releaseHook(t, ds)
			if err := svc.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			svc2 := mustReopen(t, dsn, root)
			ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
			if err != nil {
				t.Fatalf("reopen the repository: %v", err)
			}
			dispatch(t, ds2)
			resumedHook(t, ds2, got.fid, shape.name, shape.blob)
		})
	}
}

// The repository directory alone, imported into an empty database, brings
// the pending entry back with its blobs, and the first dispatcher pass after
// the import fires it once.
func TestWebhookAcceptedRequestRestoresIntoAFreshDatabase(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })
	ctx := context.Background()
	shape := heldRequests["a multipart request"]
	svc, ds, _, root, _ := newHeldHookDataset(t, true)
	fid, err := engine.ReceiveWebhookHeld(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	pendingHook(t, ds, fid)
	id := ds.Repository().ID
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Copy the directory into a fresh database.
	src, err := changelogfile.RepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	root2 := t.TempDir()
	dst, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	svc2 := mustReopen(t, engine.MigratedDSN(t), root2)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	dispatch(t, ds2)
	resumedHook(t, ds2, fid, shape.name, shape.blob)
	noDeliveryInFeed(t, ds2)
}

// An entry a hand retried while the fire was pending is delivered once: the
// retry retires it, and the dispatcher pass that follows finds nothing under
// its id and runs nothing.
func TestWebhookPendingEntryRetiredByAHandIsNotResumed(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })
	ctx := context.Background()
	shape := heldRequests["a json body"]
	svc, ds, dsn, _, _ := newHeldHookDataset(t, true)
	fid, err := engine.ReceiveWebhookHeld(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	entry := pendingHook(t, ds, fid)
	if _, err := ds.RetryTriggerFailure(ctx, "hook-held", entry.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got := hookEcho(t, ds, "held-echo")
	if got["record"] != fid {
		t.Fatalf("the retry echoed %v", got)
	}
	if left, err := ds.TriggerFailures(ctx, "hook-held"); err != nil || len(left) != 0 {
		t.Fatalf("entries after the retry = %+v, %v", left, err)
	}
	dispatch(t, ds)
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	var writes int
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = 'held-echo'`, widgetType).Scan(&writes); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("the echo was written %d times, want once", writes)
	}
	// A retry mints no run record, and the pass minted none either.
	if n := okRunsAfterClose(t, dsn, fid); n != 0 {
		t.Fatalf("OK runs under %s = %d, want none", fid, n)
	}
}

// A retry by hand of an entry whose fire this process is running is refused,
// and the fire settles once.
func TestWebhookPendingEntryRefusesAHandRetryWhileItRuns(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })
	ctx := context.Background()
	svc, ds, _, _, invoked := newHeldHookDataset(t, false)
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = engine.ReceiveWebhookSync(fctx, svc, testdb.Repository(t), "hook-held", "", heldRequests["a json body"].req)
		close(done)
	}()
	// The body is running: the fire holds the entry, and the entry is the
	// one row the trigger has.
	awaitInvoked(t, invoked)
	failures, err := ds.TriggerFailures(ctx, "hook-held")
	if err != nil || len(failures) != 1 {
		t.Fatalf("recorded requests under the running fire = %+v (%v)", failures, err)
	}
	entry := failures[0]
	if _, err := ds.RetryTriggerFailure(ctx, "hook-held", entry.ID); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a hand retry of a running entry: %v, want ErrConflict", err)
	}
	cancel()
	<-done
}
