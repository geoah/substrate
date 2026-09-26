package engine

// A schedule trigger's `arguments` reach the function as
// `input["args"]` on every fire (decision 0106): held to the function's
// declared `arguments:` when the trigger is written, and again when it fires,
// so a body re-applied with a narrower signature parks the occurrence at once
// instead of running it with arguments it refuses.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAScheduleTriggerPassesItsArguments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCursorDataset(t)
	const pkg = "widgets.test.dev/widgets"
	install := func(argType string) {
		t.Helper()
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
			vocabulary.FunctionManifest(pkg, "rollup", map[string]any{
				"description": "mints one task per fire, named for its period",
				"runtime":     vocabulary.RuntimePython,
				"arguments": []any{
					map[string]any{"name": "period", "type": argType, "required": true},
				},
				"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
				"source": `
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "fire-" + fire["id"],
                         "properties": {"name": input["mode"] + " " + str(input["args"]["period"])}}]}
`,
			}),
			vocabulary.FunctionManifest(pkg, "bare", map[string]any{
				"description": "declares no arguments",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
				"source":      "def main(input, host):\n    return {}\n",
			}),
		}); err != nil {
			t.Fatalf("install functions: %v", err)
		}
	}
	install("string")
	startsAt := nowUTC().Add(-30 * time.Minute).Truncate(time.Minute)
	put := func(callable string, args any) (*substrate.Record, error) {
		props := map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
			}},
			"callable": callable,
		}
		if args != nil {
			props["arguments"] = args
		}
		return ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeTrigger, Properties: props})
	}
	rollup := vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/rollup")

	// Write-time admission: the arguments meet the function's declaration.
	for name, tc := range map[string]struct {
		callable string
		args     any
		want     string
	}{
		"a wrong type":       {rollup, map[string]any{"period": 7}, "period"},
		"a missing required": {rollup, map[string]any{}, "period"},
		"an undeclared name": {rollup, map[string]any{"period": "weekly", "extra": true}, "extra"},
		"not a map":          {rollup, []any{"weekly"}, "a map of argument name"},
		"a function that declares none": {
			vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/bare"),
			map[string]any{"period": "weekly"},
			"declares no arguments",
		},
	} {
		_, err := put(tc.callable, tc.args)
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: put answered %v, want a validation refusal naming %q", name, err, tc.want)
		}
	}

	tr, err := put(rollup, map[string]any{"period": "weekly"})
	if err != nil {
		t.Fatalf("put schedule trigger: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	rewind := func() {
		t.Helper()
		if _, err := ds.db.ExecContext(ctx, `
			UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, tr.ID, startsAt.Add(-time.Minute)); err != nil {
			t.Fatalf("rewind: %v", err)
		}
	}
	rewind()
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	fired, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "fire-"+fireID(startsAt))
	if err != nil {
		t.Fatalf("the occurrence did not fire: %v", err)
	}
	if got := fired.Properties["name"]; got != "schedule weekly" {
		t.Fatalf("the body read %q, want the trigger's argument in schedule mode", got)
	}

	// The function narrows under the trigger: the next fire parks after one
	// attempt, naming the argument, and never runs the body.
	install("int")
	rewind()
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("parked deliveries: %+v (%v)", failures, err)
	}
	if failures[0].Attempts != 1 || !strings.Contains(failures[0].LastError, "period") {
		t.Fatalf("the refused arguments did not park at once, naming the argument: %+v", failures[0])
	}
	if again, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", fired.ID); err != nil || again.Version != fired.Version {
		t.Fatalf("the body ran with arguments its function refuses: %+v (%v)", again, err)
	}
}
