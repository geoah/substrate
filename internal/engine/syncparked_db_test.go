package engine

// A provider's scheduled sync fires its sync function with no record under
// it, so the park it leaves names no record. The sync status read still
// counts that park on the account, because the schedule fires the callable
// the account kind's record triggers fire, and it carries the newest park's
// reason, one line, beside the count. A park of an unrelated schedule is not
// the account's.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestSyncStatusCountsAParkedScheduleFire(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	const pkg = "widgets.test.dev/widgets"
	const jobKind = pkg + "/job"
	connector := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "connector", "fts": false}
	}
	owner := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "owner", "fts": false}
	}
	failing := func(name, message string) map[string]any {
		return vocabulary.FunctionManifest(pkg, name, map[string]any{
			"description": "fails every run",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{jobKind}},
			"source":      "def main(input, host):\n    raise RuntimeError(\"" + message + "\")\n",
		})
	}
	callable := func(name string) string {
		return vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/"+name)
	}
	// Half an hour back: exactly one occurrence lies between the anchor and
	// now once the fire state is rewound to it.
	startsAt := nowUTC().Add(-30 * time.Minute).Truncate(time.Minute)
	schedule := map[string]any{"schedule": map[string]any{
		"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
	}}
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
			failing("jobsync", "upstream returned HTTP 500"),
			failing("other", "the other tool broke"),
		},
		Triggers: []enginetest.Trigger{
			// The on-request trigger: it ties jobsync to the job kind, and
			// fires on no write this test makes.
			{ID: "job-on-request", Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{jobKind}, "ops": []any{"update"}}},
				"callable": callable("jobsync"),
			}},
			{ID: "job-scheduled", Properties: map[string]any{
				"enabled": true, "source": schedule, "callable": callable("jobsync"),
			}},
			{ID: "other-scheduled", Properties: map[string]any{
				"enabled": true, "source": schedule, "callable": callable("other"),
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
	// One occurrence overdue on each schedule; both park, fire-shaped.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_schedule SET fired_at = $1`, startsAt.Add(-time.Minute)); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	for _, id := range []string{"job-scheduled", "other-scheduled"} {
		failures, err := ds.TriggerFailures(ctx, id)
		if err != nil || len(failures) != 1 || failures[0].RecordID != "" {
			t.Fatalf("%s parked deliveries: %+v (%v), want one naming no record", id, failures, err)
		}
	}
	if failures, err := ds.TriggerFailures(ctx, "job-on-request"); err != nil || len(failures) != 0 {
		t.Fatalf("the record trigger parked: %+v (%v)", failures, err)
	}

	statuses, err := ds.SyncStatuses(ctx)
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if len(statuses) != 1 || statuses[0].ID != "inbox" {
		t.Fatalf("sync statuses = %+v, want the one job", statuses)
	}
	st := statuses[0]
	if st.Parked != 1 {
		t.Fatalf("parked = %d, want the scheduled sync's one park and not the other tool's", st.Parked)
	}
	if !strings.Contains(st.LastParkedError, "upstream returned HTTP 500") || strings.Contains(st.LastParkedError, "\n") {
		t.Fatalf("lastParkedError = %q, want the scheduled sync's error on one line", st.LastParkedError)
	}
	if st.LastParkedAt == nil || st.LastParkedAt.Before(startsAt) {
		t.Fatalf("lastParkedAt = %v, want the park's instant", st.LastParkedAt)
	}

	triggers, err := ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	for _, tr := range triggers {
		if tr.ID != "job-scheduled" {
			continue
		}
		if tr.Parked != 1 || !strings.Contains(tr.LastParkedError, "upstream returned HTTP 500") || tr.LastParkedAt == nil {
			t.Fatalf("job-scheduled status = %+v, want its one park and the reason", tr)
		}
	}
}
