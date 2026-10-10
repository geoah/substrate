package engine

// A provider that keeps one sync function per stream (Google's synccontacts,
// syncgmail, synccalendar and syncdrive) declares each function's stream,
// because a parked delivery names a trigger and a record, never a stream.
// The sync status read marks the stream of the parked trigger's callable
// `erroring` with the park's reason and leaves every other stream as the
// body stamped it; the account reads as before, `erroring` while a park is
// newer than its last completed run. A function that declares no stream
// marks the account alone. The parks are written straight into
// trigger_failures: the read is the subject, and the dispatcher's parking
// is pinned elsewhere (syncparked_db_test.go).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	streamSyncPkg  = "streams.test.dev/streams"
	streamSyncKind = streamSyncPkg + "/job"
)

// installStreamSync declares a `sync`-trait job kind, two functions that
// each keep one stream (`mail` and `files`), one that declares none, a
// record trigger on the kind for each and a schedule for the mail one. The
// record triggers fire on delete alone, which nothing here does, so no
// write of the test delivers anything. Each record is written with its
// streams `ok` at lastAt, its last completed run at lastAt too.
func installStreamSync(t *testing.T, ds *dataset, lastAt time.Time, records map[string][]string) {
	t.Helper()
	ctx := context.Background()
	connector := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "connector", "fts": false}
	}
	owner := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "owner", "fts": false}
	}
	fn := func(name, stream string) map[string]any {
		data := map[string]any{
			"description": "syncs one job",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{streamSyncKind}},
			"source":      "def main(input, host):\n    return {}\n",
		}
		if stream != "" {
			data["stream"] = stream
		}
		return vocabulary.FunctionManifest(streamSyncPkg, name, data)
	}
	callable := func(name string) string {
		return vocabulary.RecordPath("substrate.reamde.dev/core/function", streamSyncPkg+"/"+name)
	}
	onDelete := func(id, name string) enginetest.Trigger {
		return enginetest.Trigger{ID: id, Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{streamSyncKind}, "ops": []any{"delete"}}},
			"callable": callable(name),
		}}
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "streams", Authority: streamSyncPkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(streamSyncPkg, 0),
			vocabulary.ActorManifest(streamSyncPkg, vocabulary.PackageActor(streamSyncPkg)),
			vocabulary.KindManifest(streamSyncPkg, map[string]any{"singular": "job"}, map[string]any{
				"traits":          []any{vocabulary.TraitSyncCore},
				"displayTemplate": "{name}",
				"properties": map[string]any{
					"name":               map[string]any{"type": "string"},
					"syncState":          connector("string"),
					"syncMessage":        connector("string"),
					"lastSyncedAt":       connector("datetime"),
					"lastSyncStartedAt":  connector("datetime"),
					"lastSyncDurationMs": connector("int"),
					"syncRequestedAt":    owner("datetime"),
					"syncRequestedAck":   connector("datetime"),
					"syncPaused":         owner("bool"),
					"syncProgress":       connector("json"),
					"syncError":          connector("string"),
					"syncErrorAt":        connector("datetime"),
					"syncStreams":        connector("json"),
				},
			}),
			fn("mailsync", "mail"),
			fn("filessync", "files"),
			fn("plainsync", ""),
		},
		Triggers: []enginetest.Trigger{
			onDelete("mail-on-request", "mailsync"),
			onDelete("files-on-request", "filessync"),
			onDelete("plain-on-request", "plainsync"),
			{ID: "mail-scheduled", Properties: map[string]any{
				"enabled": true,
				"source": map[string]any{"schedule": map[string]any{
					"recurrence": "FREQ=YEARLY", "timezone": "UTC",
					"startsAt": nowUTC().Add(24 * time.Hour).Truncate(time.Minute).Format(time.RFC3339),
				}},
				"callable": callable("mailsync"),
			}},
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	actor := substrate.Actor(vocabulary.PackageActor(streamSyncPkg))
	at := lastAt.Format(time.RFC3339Nano)
	for id, names := range records {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: streamSyncKind, ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
		streams := map[string]any{}
		for _, name := range names {
			streams[name] = map[string]any{"state": "ok", "message": name + " is current", "lastAt": at}
		}
		setStreamSync(t, ds, actor, id, map[string]any{
			"syncState": "ok", "lastSyncedAt": at, "syncStreams": streams,
		})
	}
}

// setStreamSync patches a job's trait properties as the package's own
// sync writes them.
func setStreamSync(t *testing.T, ds *dataset, actor substrate.Actor, id string, props map[string]any) {
	t.Helper()
	if err := ds.inTx(context.Background(), actor, false, func(t *txn) error {
		return t.asSyncWriter(actor, func() error {
			_, err := t.patch(eref{Kind: streamSyncKind, ID: id}, substrate.PatchInput{Properties: props})
			return err
		})
	}); err != nil {
		t.Fatalf("patch %s: %v", id, err)
	}
}

// parkStreamSync writes one parked delivery of a trigger, naming a record or
// none (a schedule fire).
func parkStreamSync(t *testing.T, ds *dataset, id int64, triggerID, recordID, lastError string, at time.Time) {
	t.Helper()
	fireID := ""
	if recordID == "" {
		fireID = "fire-" + at.Format(time.RFC3339)
	}
	if _, err := ds.db.ExecContext(context.Background(), `
		INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at)
		VALUES ($1, $2, 0, $3, $4, 3, $5, $6)`, id, triggerID, fireID, recordID, lastError, at); err != nil {
		t.Fatalf("park %s: %v", triggerID, err)
	}
}

func streamSyncStatuses(t *testing.T, ds *dataset) map[string]substrate.SyncStatus {
	t.Helper()
	statuses, err := ds.SyncStatuses(context.Background())
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	out := map[string]substrate.SyncStatus{}
	for _, st := range statuses {
		out[st.ID] = st
	}
	return out
}

func TestSyncStatusMarksOnlyTheParkedStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	lastAt := nowUTC().Add(-time.Hour).Truncate(time.Second)
	installStreamSync(t, ds, lastAt, map[string][]string{
		"inbox":  {"mail", "files"},
		"outbox": {"files"},
	})

	// A record-sourced delivery of the mail sync parks on inbox.
	parked := lastAt.Add(10 * time.Minute)
	parkStreamSync(t, ds, -1, "mail-on-request", "inbox",
		"RuntimeError: mail: HTTP 403\nTraceback (most recent call last):", parked)

	failures, err := ds.TriggerFailures(ctx, "mail-on-request")
	if err != nil || len(failures) != 1 || failures[0].Stream != "mail" {
		t.Fatalf("mail-on-request parked = %+v (%v), want one naming the mail stream", failures, err)
	}

	got := streamSyncStatuses(t, ds)
	inbox := got["inbox"]
	if mail := inbox.Streams["mail"]; mail.State != substrate.SyncStateErroring || mail.Message != "RuntimeError: mail: HTTP 403" {
		t.Fatalf("inbox mail = %+v, want erroring with the park's first line", mail)
	}
	if files := inbox.Streams["files"]; files.State != substrate.SyncStateOK || files.Message != "files is current" {
		t.Fatalf("inbox files = %+v, want the body's ok left alone", files)
	}
	// The account reads as before: erroring with the park's reason, newer
	// than its last completed run (parkedSyncState).
	if inbox.State != substrate.SyncStateErroring || inbox.Parked != 1 ||
		!strings.Contains(inbox.Error, "RuntimeError: mail: HTTP 403") {
		t.Fatalf("inbox = state %q, parked %d, error %q; want the account erroring on the one park",
			inbox.State, inbox.Parked, inbox.Error)
	}
	// A park that named inbox is not outbox's.
	if outbox := got["outbox"]; outbox.State != substrate.SyncStateOK || outbox.Parked != 0 ||
		outbox.Streams["files"].State != substrate.SyncStateOK {
		t.Fatalf("outbox = %+v, want it untouched by inbox's park", outbox)
	}

	// A schedule fire names no record, so its park reaches every account,
	// and on each one only a stream the account already lists.
	scheduled := lastAt.Add(20 * time.Minute)
	parkStreamSync(t, ds, -2, "mail-scheduled", "", "run: runner: invocation exceeded 1m0s", scheduled)
	got = streamSyncStatuses(t, ds)
	inbox = got["inbox"]
	if mail := inbox.Streams["mail"]; mail.State != substrate.SyncStateErroring ||
		mail.Message != "run: runner: invocation exceeded 1m0s" || inbox.Parked != 2 {
		t.Fatalf("inbox mail = %+v, parked %d; want the newer scheduled park's reason over two parks", mail, inbox.Parked)
	}
	outbox := got["outbox"]
	if _, listed := outbox.Streams["mail"]; listed {
		t.Fatalf("outbox streams = %+v, want no mail stream added by a park that named no account", outbox.Streams)
	}
	if outbox.State != substrate.SyncStateErroring || outbox.Parked != 1 || outbox.Streams["files"].State != substrate.SyncStateOK {
		t.Fatalf("outbox = state %q, parked %d, streams %+v; want the account erroring on the schedule park, files ok",
			outbox.State, outbox.Parked, outbox.Streams)
	}

	// The mail stream completes a run after both parks: its own word stands
	// again, while the account still has no completed run since the parks.
	actor := substrate.Actor(vocabulary.PackageActor(streamSyncPkg))
	setStreamSync(t, ds, actor, "inbox", map[string]any{"syncStreams": map[string]any{
		"mail":  map[string]any{"state": "ok", "message": "mail caught up", "lastAt": scheduled.Add(time.Minute).Format(time.RFC3339Nano)},
		"files": map[string]any{"state": "ok", "message": "files is current", "lastAt": lastAt.Format(time.RFC3339Nano)},
	}})
	inbox = streamSyncStatuses(t, ds)["inbox"]
	if mail := inbox.Streams["mail"]; mail.State != substrate.SyncStateOK || mail.Message != "mail caught up" {
		t.Fatalf("inbox mail after a later completed run = %+v, want its own ok", mail)
	}
	if inbox.State != substrate.SyncStateErroring {
		t.Fatalf("inbox state = %q, want erroring until the account completes a run", inbox.State)
	}
}

// A function that declares no stream (Slack's and Beeper's, which keep
// several in one body) parks the account alone: every stream reads as the
// body stamped it, and a stream the record does not list is not added.
func TestSyncStatusAStreamlessParkMarksTheAccountAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	lastAt := nowUTC().Add(-time.Hour).Truncate(time.Second)
	installStreamSync(t, ds, lastAt, map[string][]string{"inbox": {"mail", "files"}})

	parkStreamSync(t, ds, -1, "plain-on-request", "inbox", "RuntimeError: plain broke", lastAt.Add(10*time.Minute))

	failures, err := ds.TriggerFailures(ctx, "plain-on-request")
	if err != nil || len(failures) != 1 || failures[0].Stream != "" {
		t.Fatalf("plain-on-request parked = %+v (%v), want one naming no stream", failures, err)
	}
	inbox := streamSyncStatuses(t, ds)["inbox"]
	if inbox.State != substrate.SyncStateErroring || inbox.Parked != 1 ||
		!strings.Contains(inbox.Error, "RuntimeError: plain broke") {
		t.Fatalf("inbox = state %q, parked %d, error %q; want the account erroring on the park",
			inbox.State, inbox.Parked, inbox.Error)
	}
	if len(inbox.Streams) != 2 {
		t.Fatalf("inbox streams = %+v, want the two the record lists", inbox.Streams)
	}
	for name, s := range inbox.Streams {
		if s.State != substrate.SyncStateOK || s.Message != name+" is current" {
			t.Fatalf("inbox %s = %+v, want the body's ok left alone", name, s)
		}
	}
}

// A park that named the account and a schedule park at the same instant:
// the account reports its own park, whatever the row ids, as it did before
// parks were grouped per trigger. Both still count.
func TestSyncStatusTheAccountsOwnParkWinsATie(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	lastAt := nowUTC().Add(-time.Hour).Truncate(time.Second)
	installStreamSync(t, ds, lastAt, map[string][]string{"inbox": {"mail", "files"}})

	at := lastAt.Add(10 * time.Minute)
	parkStreamSync(t, ds, 10, "plain-on-request", "inbox", "RuntimeError: the account's own park", at)
	parkStreamSync(t, ds, 11, "mail-scheduled", "", "RuntimeError: the schedule's park", at)

	inbox := streamSyncStatuses(t, ds)["inbox"]
	if inbox.Parked != 2 {
		t.Fatalf("inbox parked = %d, want both parks counted", inbox.Parked)
	}
	if inbox.LastParkedError != "RuntimeError: the account's own park" ||
		!strings.Contains(inbox.Error, "the account's own park") || inbox.Message != inbox.Error {
		t.Fatalf("inbox lastParkedError %q, error %q, message %q; want the account's own park on a tie",
			inbox.LastParkedError, inbox.Error, inbox.Message)
	}
}
