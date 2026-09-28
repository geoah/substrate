package engine

// A provider's scheduled sync fires with no record under it, so the
// dispatcher's settle never runs, and the body's own write is the only
// report of a healthy run. That write clears an error pair older than the
// `lastSyncedAt` it moves, where it leaves `syncState` at `ok` and comes from
// the kind's own package.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestSyncScheduledRunClearsAnOlderError(t *testing.T) {
	t.Parallel()
	errAt := nowUTC().Add(-2 * time.Hour).Truncate(time.Second)
	for _, tc := range []struct {
		name      string
		state     string
		syncedAt  string
		wantError bool
	}{
		{name: "recovered", state: substrate.SyncStateOK, syncedAt: "now"},
		{name: "throttled", state: substrate.SyncStateThrottled, syncedAt: "now", wantError: true},
		{name: "stale-finish", state: substrate.SyncStateOK, syncedAt: errAt.Add(-time.Hour).Format(time.RFC3339), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds := openInternalDataset(t)
			const pkg = "widgets.test.dev/widgets"
			const jobKind = pkg + "/job"
			syncedAt := `"` + tc.syncedAt + `"`
			if tc.syncedAt == "now" {
				syncedAt = `datetime.datetime.now(datetime.timezone.utc).isoformat()`
			}
			installScheduledJobSync(t, ds, pkg, jobKind,
				"import datetime\n"+
					"def main(input, host):\n"+
					"    return {\"effects\": [{\"action\": \"patch\", \"kind\": \""+jobKind+"\", \"id\": \"inbox\",\n"+
					"        \"properties\": {\"syncState\": \""+tc.state+"\", \"lastSyncedAt\": "+syncedAt+"}}]}\n")
			seedSyncError(t, ds, pkg, jobKind, errAt)
			fireScheduleOnce(t, ds)

			got := mustReadJob(t, ds, jobKind)
			if got.Properties["syncState"] != tc.state {
				t.Fatalf("syncState = %v, want %s: %v", got.Properties["syncState"], tc.state, got.Properties)
			}
			_, hasError := got.Properties["syncError"]
			_, hasErrorAt := got.Properties["syncErrorAt"]
			if hasError != tc.wantError || hasErrorAt != tc.wantError {
				t.Fatalf("syncError present %v, syncErrorAt present %v, want %v: %v",
					hasError, hasErrorAt, tc.wantError, got.Properties)
			}
		})
	}
}

// TestSyncForeignWriteKeepsTheError: another package's hand moving
// `lastSyncedAt` beside an `ok` is not a run of the account's sync, and the
// error pair stays.
func TestSyncForeignWriteKeepsTheError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	const pkg = "widgets.test.dev/widgets"
	const jobKind = pkg + "/job"
	installScheduledJobSync(t, ds, pkg, jobKind, "def main(input, host):\n    return {}\n")
	errAt := nowUTC().Add(-2 * time.Hour).Truncate(time.Second)
	seedSyncError(t, ds, pkg, jobKind, errAt)

	for _, actor := range []substrate.Actor{
		substrate.FunctionActor("mirror.test.dev", "mirror", "copy"),
		substrate.BundleActor("widgets.test.dev", "widgetsplus"),
	} {
		err := ds.inTx(ctx, actor, false, func(t *txn) error {
			return t.asSyncWriter(actor, func() error {
				_, err := t.patch(eref{Kind: jobKind, ID: "inbox"}, substrate.PatchInput{Properties: map[string]any{
					"syncState":    substrate.SyncStateOK,
					"lastSyncedAt": t.now.Format(time.RFC3339Nano),
				}})
				return err
			})
		})
		if err != nil {
			t.Fatalf("write as %s: %v", actor, err)
		}
		got := mustReadJob(t, ds, jobKind)
		if got.Properties["syncError"] == nil || got.Properties["syncErrorAt"] == nil {
			t.Fatalf("a write as %s cleared the error pair: %v", actor, got.Properties)
		}
	}
}

// installScheduledJobSync installs pkg's job kind bound to the sync trait
// and one hourly schedule firing jobsync, whose body is source, then puts the
// job `inbox`.
func installScheduledJobSync(t *testing.T, ds *dataset, pkg, jobKind, source string) {
	t.Helper()
	ctx := context.Background()
	connector := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "connector", "fts": false}
	}
	owner := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "owner", "fts": false}
	}
	// Half an hour back: exactly one occurrence lies between the anchor and
	// now once the fire state is rewound to it.
	startsAt := nowUTC().Add(-30 * time.Minute).Truncate(time.Minute)
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "widgets", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "job"}, map[string]any{
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
			vocabulary.FunctionManifest(pkg, "jobsync", map[string]any{
				"description": "the job's scheduled sync",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{jobKind}},
				"source":      source,
			}),
		},
		Triggers: []enginetest.Trigger{
			{ID: "job-scheduled", Properties: map[string]any{
				"enabled": true,
				"source": map[string]any{"schedule": map[string]any{
					"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
				}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/jobsync"),
			}},
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: jobKind, ID: "inbox", Properties: map[string]any{"name": "inbox"},
	}); err != nil {
		t.Fatalf("put job: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE trigger_schedule SET fired_at = $1`, startsAt.Add(-time.Minute)); err != nil {
		t.Fatalf("rewind: %v", err)
	}
}

// seedSyncError writes the error pair a failed run leaves, under the
// package's own hand, the way the open-time sweep writes it.
func seedSyncError(t *testing.T, ds *dataset, pkg, jobKind string, at time.Time) {
	t.Helper()
	authority, name := vocabulary.SplitPackageRef(pkg)
	actor := substrate.BundleActor(authority, name)
	err := ds.inTx(context.Background(), actor, false, func(t *txn) error {
		return t.asSyncWriter(actor, func() error {
			_, err := t.patch(eref{Kind: jobKind, ID: "inbox"}, substrate.PatchInput{Properties: map[string]any{
				"syncState":   substrate.SyncStateErroring,
				"syncError":   "upstream returned HTTP 500",
				"syncErrorAt": at.Format(time.RFC3339Nano),
			}})
			return err
		})
	})
	if err != nil {
		t.Fatalf("seed the error pair: %v", err)
	}
}

// fireScheduleOnce runs the overdue occurrence installScheduledJobSync
// rewound to, and requires it to have settled without a park.
func fireScheduleOnce(t *testing.T, ds *dataset) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	failures, err := ds.TriggerFailures(ctx, "job-scheduled")
	if err != nil || len(failures) != 0 {
		t.Fatalf("the scheduled sync parked: %+v (%v)", failures, err)
	}
}

func mustReadJob(t *testing.T, ds *dataset, jobKind string) *substrate.Record {
	t.Helper()
	got, err := ds.Get(context.Background(), jobKind, "inbox")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	return got
}
