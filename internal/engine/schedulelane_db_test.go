package engine

// A DUE SCHEDULE FIRE DOES NOT WAIT FOR THE RECORD TRIGGERS (decision 0147).
// A pass once ran its schedules and then its record triggers one after
// another, so an occurrence that fell due while backlogged record triggers
// each spent their budget waited for the rest of the walk and the next pass:
// hourly syncs started 5 to 11 minutes late on a small arm64 box. The pass
// now runs its schedules in a lane beside the record triggers, and the lane
// looks for due occurrences every scheduleLanePoll until the record lane is
// done. These tests drive passes back to back, as the dispatcher does, over
// record triggers whose every delivery spends the budget.

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const lanePkg = "widgets.test.dev/widgets"

// withScheduleLanePoll lowers the schedule lane's poll for one test.
// Package-level: a caller MUST NOT call t.Parallel.
func withScheduleLanePoll(d time.Duration) func() {
	prev := scheduleLanePoll
	scheduleLanePoll = d
	return func() { scheduleLanePoll = prev }
}

// laneDataset installs a record function that sleeps `slow` per delivery,
// `records` record triggers on widgets that run it, a schedule function that
// sleeps `fire` per fire, and `widgets` widgets each record trigger owes.
// Every record delivery mints a task of its own, so the tasks count the
// deliveries.
func laneDataset(t *testing.T, slow, fire time.Duration, records, widgets int, opts ...Option) *dataset {
	t.Helper()
	ctx := context.Background()
	ds := openCursorDataset(t, opts...)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(lanePkg, "slow", map[string]any{
			"description": "one slow fetch per widget",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source":      laneBody(slow, `"slow-" + env["change"]["id"] + "-" + str(time.time_ns())`),
		}),
		vocabulary.FunctionManifest(lanePkg, "sync", map[string]any{
			"description": "one slow sync per fire",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source":      laneBody(fire, `"fire-" + env["fire"]["id"]`),
		}),
	}); err != nil {
		t.Fatalf("install functions: %v", err)
	}
	for i := range records {
		// Ids that sort ahead of the schedule's: the old walk reached the
		// schedule before them only at a pass's start.
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeTrigger, ID: fmt.Sprintf("a-slow-%d", i),
			Properties: map[string]any{
				"source":   map[string]any{"record": map[string]any{"kinds": []any{lanePkg + "/widget"}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", lanePkg+"/slow"),
			},
		}); err != nil {
			t.Fatalf("put record trigger: %v", err)
		}
	}
	for i := range widgets {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: lanePkg + "/widget", Properties: map[string]any{"name": fmt.Sprintf("w%d", i)},
		}); err != nil {
			t.Fatalf("put widget: %v", err)
		}
	}
	return ds
}

// laneBody is a function body that sleeps `sleep` and puts the task whose
// id the Python expression `id` computes.
func laneBody(sleep time.Duration, id string) string {
	return fmt.Sprintf(`
import time

def main(input, host):
    time.sleep(%f)
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": %s, "properties": {"name": "lane"}}]}
`, sleep.Seconds(), id)
}

// installLaneSyncs installs one schedule function per name, each a callable
// of its own, so its fires run in a runner process of its own, and each
// sleeping `fire` per fire.
func installLaneSyncs(t *testing.T, ds *dataset, names []string, fire time.Duration) {
	t.Helper()
	docs := make([]map[string]any, 0, len(names))
	for _, name := range names {
		docs = append(docs, vocabulary.FunctionManifest(lanePkg, name, map[string]any{
			"description": "one slow sync per fire",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source":      laneBody(fire, fmt.Sprintf(`"fire-%s-" + env["fire"]["id"]`, name)),
		}))
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install schedule functions: %v", err)
	}
}

// putLaneSchedule writes the schedule trigger `id` on the sync function.
func putLaneSchedule(t *testing.T, ds *dataset, id, recurrence string, startsAt time.Time) {
	t.Helper()
	putLaneScheduleOn(t, ds, id, "sync", recurrence, startsAt)
}

// putLaneScheduleOn writes the schedule trigger `id` on the function
// `callable` of the lane package.
func putLaneScheduleOn(t *testing.T, ds *dataset, id, callable, recurrence string, startsAt time.Time) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: id,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": recurrence, "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", lanePkg+"/"+callable),
		},
	}); err != nil {
		t.Fatalf("put schedule trigger: %v", err)
	}
}

// passSpan is one dispatcher pass's wall-clock.
type passSpan struct{ start, end time.Time }

// dispatchLoop runs passes back to back, the way the dispatcher runs one
// repository's passes while they are long, until the returned stop is
// called; stop waits for the pass in flight and returns every pass's span.
func dispatchLoop(t *testing.T, ds *dataset) (stop func() []passSpan) {
	t.Helper()
	ctx := context.Background()
	quit := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var spans []passSpan
	var passErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-quit:
				return
			default:
			}
			began := time.Now()
			if _, err := ds.ProcessTriggers(ctx); err != nil {
				mu.Lock()
				passErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			spans = append(spans, passSpan{began, time.Now()})
			mu.Unlock()
		}
	}()
	return func() []passSpan {
		close(quit)
		wg.Wait()
		if passErr != nil {
			t.Fatalf("process: %v", passErr)
		}
		return spans
	}
}

// laneRun is one settled run of a trigger.
type laneRun struct {
	fireID            string
	started, finished time.Time
}

// okRuns lists a trigger's ok runs, oldest start first.
func okRuns(t *testing.T, ds *dataset, triggerID string) []laneRun {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds: []string{typeTriggerRun},
			Properties: map[string]substrate.Cond{
				"trigger": {Eq: vocabulary.RecordPath("substrate.reamde.dev/core/trigger", triggerID)},
				"status":  {Eq: runStatusOK},
			},
		},
		First: 500,
	})
	if err != nil {
		t.Fatalf("runs of %s: %v", triggerID, err)
	}
	var out []laneRun
	for _, rec := range page.Records {
		r := laneRun{fireID: fmt.Sprint(rec.Properties["fireId"])}
		for key, into := range map[string]*time.Time{"startedAt": &r.started, "finishedAt": &r.finished} {
			at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(rec.Properties[key]))
			if err != nil {
				t.Fatalf("run %s %s: %v", rec.ID, key, err)
			}
			*into = at
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b laneRun) int { return a.started.Compare(b.started) })
	return out
}

// countTasks counts the live tasks whose id starts with prefix.
func countTasks(t *testing.T, ds *dataset, prefix string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND id LIKE $2`,
		"samples.substrate.reamde.dev/tasks/task", prefix+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestADueScheduleFireStartsWhileRecordTriggersDrain(t *testing.T) {
	// Three record triggers, each owing four widgets at half a second a
	// delivery, and a budget of 1.5 s: a pass runs about five seconds, every
	// trigger spending its whole budget. The hourly occurrence falls due one
	// to two seconds into the first pass.
	const records, widgets = 3, 4
	const bound = time.Second
	defer withTriggerPassBudget(1500 * time.Millisecond)()
	defer withScheduleLanePoll(100 * time.Millisecond)()
	ds := laneDataset(t, 500*time.Millisecond, 0, records, widgets)
	due := time.Now().UTC().Add(1500 * time.Millisecond).Truncate(time.Second).Add(time.Second)
	putLaneSchedule(t, ds, "b-hourly", "FREQ=HOURLY", due)

	stop := dispatchLoop(t, ds)
	deadline := time.Now().Add(30 * time.Second)
	for countTasks(t, ds, "slow-") < records*widgets || len(okRuns(t, ds, "b-hourly")) == 0 {
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("delivered %d of %d widgets and %d fires in 30 s",
				countTasks(t, ds, "slow-"), records*widgets, len(okRuns(t, ds, "b-hourly")))
		}
		time.Sleep(50 * time.Millisecond)
	}
	spans := stop()

	runs := okRuns(t, ds, "b-hourly")
	if len(runs) != 1 || runs[0].fireID != fireID(due) {
		t.Fatalf("ok runs %+v, want the one occurrence %s", runs, fireID(due))
	}
	if n := countTasks(t, ds, "fire-"); n != 1 {
		t.Fatalf("the occurrence applied %d times, want once", n)
	}
	// The measure is only worth something if a pass was mid-walk at the due
	// time with more than the bound still to go: then a fire that waited for
	// the record triggers would have started past the bound.
	i := slices.IndexFunc(spans, func(s passSpan) bool { return !s.start.After(due) && s.end.After(due) })
	if i < 0 {
		t.Fatalf("no pass was running at the due time %s: %+v", due, spans)
	}
	if left := spans[i].end.Sub(due); left <= bound {
		t.Fatalf("the pass running at the due time ended %s after it, too soon to measure against %s", left, bound)
	}
	delay := runs[0].started.Sub(due)
	t.Logf("the fire started %s after its due time; the pass running then ended %s after it",
		delay.Round(time.Millisecond), spans[i].end.Sub(due).Round(time.Millisecond))
	if delay > bound {
		t.Fatalf("the fire started %s after its due time, want within %s", delay, bound)
	}

	// The record triggers made progress in the pass the fire ran in, and every
	// widget was delivered once by each, none parked.
	for r := range records {
		id := fmt.Sprintf("a-slow-%d", r)
		if failures, err := ds.TriggerFailures(context.Background(), id); err != nil || len(failures) != 0 {
			t.Fatalf("%s parked deliveries: %+v (%v)", id, failures, err)
		}
		if got := len(okRuns(t, ds, id)); got != widgets {
			t.Fatalf("%s wrote %d ok runs, want one per widget (%d)", id, got, widgets)
		}
	}
}

func TestAScheduleLaneRunsOneFireOfATriggerAtATime(t *testing.T) {
	// A fire takes 1.2 s and the trigger is due every second, so occurrences
	// fall due while the one before still runs. The lane fires them oldest
	// first, never two at once, each once, and the record trigger drains
	// beside it.
	const widgets = 6
	defer withTriggerPassBudget(time.Second)()
	defer withScheduleLanePoll(50 * time.Millisecond)()
	ds := laneDataset(t, 200*time.Millisecond, 1200*time.Millisecond, 1, widgets)
	putLaneSchedule(t, ds, "b-secondly", "FREQ=SECONDLY", time.Now().UTC().Truncate(time.Second))

	stop := dispatchLoop(t, ds)
	deadline := time.Now().Add(30 * time.Second)
	for countTasks(t, ds, "slow-") < widgets || len(okRuns(t, ds, "b-secondly")) < 4 {
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("delivered %d of %d widgets and %d fires in 30 s",
				countTasks(t, ds, "slow-"), widgets, len(okRuns(t, ds, "b-secondly")))
		}
		time.Sleep(50 * time.Millisecond)
	}
	stop()

	runs := okRuns(t, ds, "b-secondly")
	seen := map[string]bool{}
	for i, r := range runs {
		if seen[r.fireID] {
			t.Fatalf("occurrence %s ran twice: %+v", r.fireID, runs)
		}
		seen[r.fireID] = true
		if i == 0 {
			continue
		}
		prev := runs[i-1]
		if r.started.Before(prev.finished) {
			t.Fatalf("occurrence %s started at %s, before %s finished at %s", r.fireID, r.started, prev.fireID, prev.finished)
		}
		// Oldest first, and none skipped: each fire is the next second.
		next, err := time.Parse(time.RFC3339, prev.fireID)
		if err != nil {
			t.Fatal(err)
		}
		if want := fireID(next.Add(time.Second)); r.fireID != want {
			t.Fatalf("occurrence %s ran after %s, want %s", r.fireID, prev.fireID, want)
		}
	}
	if n := countTasks(t, ds, "fire-"); n != len(runs) {
		t.Fatalf("%d fires applied for %d ok runs", n, len(runs))
	}
	if got := len(okRuns(t, ds, "a-slow-0")); got != widgets {
		t.Fatalf("the record trigger wrote %d ok runs beside the busy lane, want %d", got, widgets)
	}
}

// panickingLog is a slog handler that panics on the record lane's line for
// a contained panic, so the panic leaves passRecordTrigger's own recover and
// unwinds the pass, the way any panic outside a delivery still would.
type panickingLog struct{ slog.Handler }

func (h panickingLog) Handle(ctx context.Context, r slog.Record) error {
	if strings.HasPrefix(r.Message, "substrate: a record trigger's delivery panicked") {
		panic("the log handler panicked")
	}
	return h.Handler.Handle(ctx, r)
}

func (h panickingLog) WithAttrs(attrs []slog.Attr) slog.Handler {
	return panickingLog{h.Handler.WithAttrs(attrs)}
}

func (h panickingLog) WithGroup(name string) slog.Handler {
	return panickingLog{h.Handler.WithGroup(name)}
}

func TestAPanickedPassStopsItsScheduleLane(t *testing.T) {
	// A record delivery panics in the first pass, and the log line that
	// reports its containment panics too, so a panic unwinds the pass to a
	// recover like the dispatcher's. The occurrence falls due after that
	// pass has returned: a lane the pass left behind would fire it with no
	// pass running.
	defer withScheduleLanePoll(50 * time.Millisecond)()
	ds := laneDataset(t, 0, 0, 1, 2, WithLogger(slog.New(panickingLog{slog.Default().Handler()})))
	due := time.Now().UTC().Add(1500 * time.Millisecond).Truncate(time.Second).Add(time.Second)
	putLaneSchedule(t, ds, "b-hourly", "FREQ=HOURLY", due)
	var once sync.Once
	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error {
		once.Do(func() { panic("record delivery panicked") })
		return nil
	}
	ds.mu.Unlock()

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_, _ = ds.ProcessTriggers(context.Background())
		return nil
	}()
	if recovered != "the log handler panicked" {
		t.Fatalf("the pass ended with %v, want the log handler's panic", recovered)
	}
	if held := len(ds.svc.deliverySlots); held != 0 {
		t.Fatalf("%d delivery slots still held after the panicked pass returned", held)
	}
	if time.Now().After(due) {
		t.Fatalf("the pass returned after the due time %s: too late to tell a leaked lane apart", due)
	}
	time.Sleep(time.Until(due) + time.Second)
	if runs := okRuns(t, ds, "b-hourly"); len(runs) != 0 {
		t.Fatalf("the occurrence fired %d times after the panicked pass returned: its schedule lane kept running", len(runs))
	}

	// The next pass fires it, once.
	processOnce(t, ds)
	if runs := okRuns(t, ds, "b-hourly"); len(runs) != 1 || runs[0].fireID != fireID(due) {
		t.Fatalf("ok runs %+v after the next pass, want the one occurrence %s", runs, fireID(due))
	}
}

func TestAScheduleLaneFiresAtMostOnePassOfMissedOccurrences(t *testing.T) {
	// Thirty occurrences are overdue and a pass may fire three. The record
	// lane runs long enough for the schedule lane to look many times; it
	// still fires three in the pass, not three a look.
	const drain = 3
	defer withScheduleDrain(drain)()
	defer withScheduleLanePoll(50 * time.Millisecond)()
	ds := laneDataset(t, 300*time.Millisecond, 0, 1, 5)
	startsAt := time.Now().UTC().Add(-30 * time.Hour).Truncate(time.Hour)
	putLaneSchedule(t, ds, "b-hourly", "FREQ=HOURLY", startsAt)
	processOnce(t, ds) // initializes the fire state: nothing due yet
	rewindSchedule(t, ds, "b-hourly", startsAt.Add(-time.Minute))
	// Widgets written now give the record lane a fresh backlog to drain.
	for i := range 5 {
		if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
			Kind: lanePkg + "/widget", Properties: map[string]any{"name": fmt.Sprintf("late%d", i)},
		}); err != nil {
			t.Fatalf("put widget: %v", err)
		}
	}

	began := time.Now()
	processOnce(t, ds)
	if took := time.Since(began); took < 10*scheduleLanePoll {
		t.Fatalf("the pass took %s, too short for the lane to look again", took)
	}
	if runs := okRuns(t, ds, "b-hourly"); len(runs) != drain {
		t.Fatalf("one pass fired %d occurrences, want %d", len(runs), drain)
	}
	if got, want := firedAtOf(t, ds, "b-hourly"), startsAt.Add((drain-1)*time.Hour); !got.Equal(want) {
		t.Fatalf("fire state %s after one pass, want the third occurrence %s", got, want)
	}
}

// waitPast sleeps until a little past at, failing the test if at is already
// behind: a fixture that took longer than its margin measures nothing.
func waitPast(t *testing.T, at time.Time) {
	t.Helper()
	if time.Now().After(at) {
		t.Fatalf("the fixture finished after the due time %s: too late to measure", at)
	}
	time.Sleep(time.Until(at) + 50*time.Millisecond)
}

func TestAScheduleLaneStartsTheDueFiresOfSeveralTriggersTogether(t *testing.T) {
	// Five schedule triggers, each on a callable of its own whose fire takes
	// 1.2 s, fall due at one instant (#883). One at a time, the last would
	// start about 4.8 s after the first. The lane runs four at once and
	// starts the fifth as one ends, about 1.2 s in, so all five start within
	// 3 s, and never more than four run at once.
	const fire = 1200 * time.Millisecond
	const bound = 3 * time.Second
	names := []string{"synca", "syncb", "syncc", "syncd", "synce"}
	ds := laneDataset(t, 0, 0, 0, 0)
	installLaneSyncs(t, ds, names, fire)
	due := time.Now().UTC().Add(2500 * time.Millisecond).Truncate(time.Second).Add(time.Second)
	for _, name := range names {
		putLaneScheduleOn(t, ds, "b-"+name, name, "FREQ=HOURLY", due)
	}
	processOnce(t, ds) // initializes every fire state: nothing is due yet
	waitPast(t, due)
	processOnce(t, ds)

	var runs []laneRun
	for _, name := range names {
		got := okRuns(t, ds, "b-"+name)
		if len(got) != 1 || got[0].fireID != fireID(due) {
			t.Fatalf("b-%s ok runs %+v, want the one occurrence %s", name, got, fireID(due))
		}
		runs = append(runs, got[0])
	}
	slices.SortFunc(runs, func(a, b laneRun) int { return a.started.Compare(b.started) })
	spread := runs[len(runs)-1].started.Sub(runs[0].started)
	t.Logf("the five fires started within %s of each other", spread.Round(time.Millisecond))
	if spread > bound {
		t.Fatalf("the five fires started %s apart, want within %s: %+v", spread, bound, runs)
	}
	for _, r := range runs {
		running := 0
		for _, o := range runs {
			if !o.started.After(r.started) && o.finished.After(r.started) {
				running++
			}
		}
		if running > scheduleLaneWorkers {
			t.Fatalf("%d fires ran at once when %s started, want at most %d: %+v", running, r.fireID, scheduleLaneWorkers, runs)
		}
	}
}

func TestAScheduleOnlyRepositoryFiresEveryDueTrigger(t *testing.T) {
	// No record triggers, so the record lane is done before the schedule
	// lane starts. The lane still runs its first round whole: six triggers,
	// more than it runs at once, each fire their due occurrence in the one
	// pass, and the pass returns only once every fire has settled.
	const triggers = 6
	ds := laneDataset(t, 0, 200*time.Millisecond, 0, 0)
	due := time.Now().UTC().Add(2500 * time.Millisecond).Truncate(time.Second).Add(time.Second)
	for i := range triggers {
		putLaneSchedule(t, ds, fmt.Sprintf("b-hourly-%d", i), "FREQ=HOURLY", due)
	}
	processOnce(t, ds) // initializes every fire state: nothing is due yet
	waitPast(t, due)
	processOnce(t, ds)

	for i := range triggers {
		id := fmt.Sprintf("b-hourly-%d", i)
		if runs := okRuns(t, ds, id); len(runs) != 1 || runs[0].fireID != fireID(due) {
			t.Fatalf("%s ok runs %+v when the pass returned, want the one occurrence %s", id, runs, fireID(due))
		}
	}
	if held := len(ds.svc.deliverySlots); held != 0 {
		t.Fatalf("%d delivery slots still held after the pass returned", held)
	}
}

func TestAPanickedScheduleFireIsContainedToItsTrigger(t *testing.T) {
	// One of two due fires panics in its settlement, in a worker the pass's
	// recover does not reach. The worker's own recover contains it: the pass
	// returns an error naming the panic instead of panicking, the other
	// fire settles in the same pass, every delivery slot is given back, no
	// occurrence is left counted as in flight, and the next pass fires the
	// occurrence the panic interrupted.
	ds := laneDataset(t, 0, 0, 0, 0)
	installLaneSyncs(t, ds, []string{"synca", "syncb"}, 0)
	due := time.Now().UTC().Add(2500 * time.Millisecond).Truncate(time.Second).Add(time.Second)
	putLaneScheduleOn(t, ds, "b-synca", "synca", "FREQ=HOURLY", due)
	putLaneScheduleOn(t, ds, "b-syncb", "syncb", "FREQ=HOURLY", due)
	processOnce(t, ds) // initializes both fire states: nothing is due yet
	var once sync.Once
	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error {
		once.Do(func() { panic("schedule fire panicked") })
		return nil
	}
	ds.mu.Unlock()
	waitPast(t, due)

	var passErr error
	recovered := func() (r any) {
		defer func() { r = recover() }()
		_, passErr = ds.ProcessTriggers(context.Background())
		return nil
	}()
	if recovered != nil {
		t.Fatalf("the pass panicked: %v", recovered)
	}
	if passErr == nil || !strings.Contains(passErr.Error(), "schedule fire panicked") {
		t.Fatalf("the pass returned %v, want the contained panic", passErr)
	}
	if held := len(ds.svc.deliverySlots); held != 0 {
		t.Fatalf("%d delivery slots still held after the pass returned", held)
	}
	for _, id := range []string{"b-synca", "b-syncb"} {
		if ds.isFiring(id, fireID(due)) {
			t.Fatalf("%s's occurrence %s is still counted as in flight after the pass returned", id, fireID(due))
		}
	}
	a, b := okRuns(t, ds, "b-synca"), okRuns(t, ds, "b-syncb")
	if len(a)+len(b) != 1 {
		t.Fatalf("ok runs %+v and %+v beside the panic, want the other trigger's one fire", a, b)
	}

	processOnce(t, ds)
	for _, id := range []string{"b-synca", "b-syncb"} {
		if runs := okRuns(t, ds, id); len(runs) != 1 || runs[0].fireID != fireID(due) {
			t.Fatalf("%s ok runs %+v after the next pass, want the one occurrence %s", id, runs, fireID(due))
		}
	}
}
