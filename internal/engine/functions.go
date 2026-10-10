package engine

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The trigger dispatcher: every enabled trigger
// record owns delivery. An record-sourced trigger owns a changelog cursor —
// the dispatcher reads past it, filters by the source, evaluates the `when:`
// guard against the record's CURRENT state, runs the callable's body in the
// shared runner, and applies the returned effects through the ordinary write
// path under the CALLABLE's actor, cursor advance in the SAME transaction. A
// schedule-sourced trigger owns a fire state instead: due RRULE occurrences
// (oldest first, a bounded number per pass, stable fire ids) enter the same
// delivery path with mode `schedule` and no changelog row underneath. Serial
// per trigger, schedules in a lane beside the record sources (decision 0147),
// each trigger bounded per pass by triggerPassBudget; a failed delivery
// retries and then parks-and-advances, so one poisoned record never wedges
// a trigger's lag. Every settled delivery
// attempt writes one `run` record (ok / skipped / parked) under the system
// actor, in the transaction that commits the effects and the cursor motion
// it describes; parked runs are kept, everything else prunes to the newest
// runRetention per trigger. The cursor, the fire state, the parked failures
// and a paged drain's resume row are the DELIVERY LEDGER (delivery.go):
// every motion is a fold effect on a `delivery` changelog entry, so a
// restore folds them back.

const (
	// triggerBatch bounds one changelog read; the loop drains to head or to
	// the trigger's pass budget, whichever comes first.
	triggerBatch = 200
	// causalDepthCap parks a delivery whose triggering change sits at the end
	// of a caused_by chain this deep — host sub-Calls increment the same
	// counter, so a delivery at depth D may nest at most cap−D calls.
	causalDepthCap = 16
	// triggerAttempts is the total tries a delivery gets before parking.
	triggerAttempts = 3
	// runRetention is how many non-parked run rows one trigger keeps: the
	// newest 20 — enough to read a trigger's recent behavior off the console
	// without the ledger outgrowing the data it describes. Parked runs are
	// exempt: failures are kept until retried away.
	runRetention = 20
)

// triggerRetryBackoff spaces the retries; short, because a delivery is one
// bounded evaluation plus one transaction.
var triggerRetryBackoff = []time.Duration{25 * time.Millisecond, 100 * time.Millisecond}

// scheduleDrainPerPass bounds the occurrences one pass fires for one schedule
// trigger, oldest first, over every look of its schedule lane. A trigger
// that was down, disabled or restored fires every occurrence it missed, but
// over passes rather than in one burst at startup; nothing is coalesced
// away. A var, so a test can lower it.
var scheduleDrainPerPass = 10

// supersedeBatch bounds the parked fires one settlement retires
// (retireSupersededFires), oldest first, so its delivery entry stays far
// under changelogfile.MaxLineBytes (one unpark is about 100 bytes) and its
// transaction stays short; the rest retire at the next settled fire. A var,
// so a test can lower it.
var supersedeBatch = 1000

// triggerPassBudget bounds the wall-clock one dispatcher pass spends on one
// trigger: past it the trigger stops at the delivery in hand and the pass
// moves on, and the next pass resumes from the cursor (or fire state) that
// delivery left. Without it a record trigger drained to head inside one pass,
// so an enricher at seconds per delivery on a 54k-row lag held every other
// trigger and every schedule for hours (#638). Thirty seconds keeps a lone
// backlogged trigger delivering for most of the time (between two of its
// repository's passes lies at most the rest of a 5 s tick, plus any wait for
// one of substrated's dispatcher slots) while a repository with a handful of
// backlogged triggers still reaches every trigger within minutes. Schedules
// do not wait for that walk: they run in a lane of their own (decision
// 0147). The bound is per trigger, not per pass: a lane runs about the sum
// of its triggers' budgets, each overrun by at most the one delivery in
// hand. A constant, like the dispatcher's other intervals, and a var only so
// a test can lower it.
var triggerPassBudget = 30 * time.Second

// passDeadline is the moment a trigger's share of one pass ends; the zero
// value is no bound, which is what a wake by hand runs under.
type passDeadline time.Time

// newPassDeadline starts one trigger's budget now.
func newPassDeadline() passDeadline { return passDeadline(time.Now().Add(triggerPassBudget)) }

// spent reports whether the budget is used up.
func (d passDeadline) spent() bool {
	return !time.Time(d).IsZero() && !time.Now().Before(time.Time(d))
}

// The paged-checkpoint drain budget. A body that keeps
// returning `more` must be bounded on every axis, and the bound must span the
// WHOLE chain — automatic retries and already-committed pages included — so a
// runaway or hostile pager parks deterministically instead of occupying the
// single serial dispatcher for days. The cumulative counters persist on the
// paged_cursors row (pages, effects, bytes, started_at); a fresh pass reloads
// them and keeps counting, never resetting. Vars, not consts, so a test can
// lower them.
var (
	// maxPagesPerDrain is the cumulative re-invocation (call) cap across the
	// whole chain — a small cap, not the old ~10k that permitted ~500 hours.
	maxPagesPerDrain = 512
	// drainDeadline bounds the chain's wall-clock from its first committed
	// page, checked before every middle page: a drain that runs long parks and
	// resumes on retry rather than wedging the dispatcher.
	drainDeadline = 2 * time.Minute
	// maxDrainEffects bounds the cumulative effect count over the whole chain.
	maxDrainEffects = int64(200000)
	// maxDrainBytes bounds the cumulative effect bytes over the whole chain —
	// the write-traffic ceiling the per-frame cap alone never gave.
	maxDrainBytes = int64(256 << 20)
	// pagedSweepGrace keeps the orphan sweep off a row an in-flight drain is
	// still advancing: a live-trigger row with no parked failure is collected
	// only once it has gone this long untouched.
	pagedSweepGrace = time.Hour
)

// The paged-cursor delivery kinds — the lifecycle-owner discriminator on a
// paged_cursors row.
const (
	pagedKindRecord = "record"
	pagedKindFire   = "fire"
)

// errMaxPages marks the drain-cap park reason: a paged body never finished
// within the page cap. Deterministic — a retry reproduces it — so it parks
// immediately.
var errMaxPages = errors.New("paged drain exceeded the max-pages cap")

// errDrainBudget marks the other deterministic drain-budget park reasons: the
// cumulative effect, byte, or wall-clock ceiling. Like errMaxPages it parks
// immediately rather than repeating the drain.
var errDrainBudget = errors.New("paged drain exceeded a cumulative budget")

// errPagedParked wraps a drain error that must PARK IMMEDIATELY with the last
// committed resume cursor intact: a budget/cap exhaustion,
// or any page error once at least one page has committed. The delivery does not
// burn its remaining auto-retries re-running a chain that already made durable
// progress — the parked-failure retry (and the next dispatch, which reloads the
// resume cursor) continues from where the chain stands. errCursorMoved is NOT
// wrapped: that rolls the pass back and yields, it does not park.
var errPagedParked = errors.New("paged drain parked mid-chain")

// errCausalDepth marks the distinct park reason a chain cap produces: an
// error, observable, never a silent stop.
var errCausalDepth = errors.New("causal depth cap exceeded")

// errCallableGone marks a callable that stopped resolving between the pass's
// trigger load and the delivery: the package was uninstalled or pruned under
// a running pass. The delivery does not run, it does not park, and the cursor
// stands still — the answer the pass already gives a trigger whose callable
// was gone when it loaded.
var errCallableGone = errors.New("trigger callable no longer resolves")

// errCursorMoved marks a cursor (or schedule fire state) compare-and-swap
// that lost: a replay reset it or a concurrent dispatcher advanced it
// mid-pass. The losing transaction rolls back whole — effects never land
// under a cursor the pass no longer owns — and the pass yields; the next one
// resumes from wherever the cursor now points.
var errCursorMoved = errors.New("trigger cursor moved concurrently")

// errConflictYield marks a delivery that LOST a compare-and-set it had
// declared it could lose: an effect carrying `ifVersion` plus
// `onConflict: yield` found the record at another version, so the whole
// delivery rolled back and wrote nothing. It is not a failure — two
// invocations racing over one record is the normal shape of a sync whose
// on-request trigger fires while its scheduled one drains — so it settles as a
// SKIP with the conflict as its reason, the cursor (or fire state) moves past
// it, and nothing parks. The winner's write is the one that stands
// (decision 0093).
var errConflictYield = fmt.Errorf("%w: a guarded write yielded its version race", substrate.ErrConflict)

// declinedDelivery reports whether a delivery error means ANOTHER INVOCATION
// owns this work, rather than that this one failed: another dispatch holds the
// delivery, or a guarded write declared `onConflict: yield` and lost. Either
// way the transaction rolled back whole, so the delivery settles as a skip and
// the cursor (or fire state) moves past it.
//
// A yield wrapped in errPagedParked is NOT declined: the drain committed pages
// before it lost the race, so the chain is this pass's to park.
func declinedDelivery(err error) bool {
	if errors.Is(err, errPagedParked) {
		return false
	}
	return errors.Is(err, errClaimedElsewhere) || errors.Is(err, errConflictYield)
}

// ProcessTriggers runs one dispatcher pass in two lanes that run side by
// side (decision 0147). The record lane walks every enabled record trigger
// in id order, one at a time, each draining its backlog for at most
// triggerPassBudget. The schedule lane fires every enabled schedule
// trigger's due occurrences, up to laneWorkers triggers at once and one fire
// per trigger at a time (decision 0150), then looks again every
// scheduleLanePoll until the record lane is done, so a fire that falls due
// while a record backlog drains starts within one poll instead of after the
// whole walk. A trigger never has two deliveries in flight from one pass,
// and every delivery the pass runs holds one of the process's
// TriggerDeliverySlots. It returns the number of deliveries that applied
// effects. Only infrastructure errors and a record trigger's contained
// panic (passRecordTrigger) surface; eval and effect errors park.
func (ds *dataset) ProcessTriggers(ctx context.Context) (int, error) {
	// The whole pass is one observation, whatever it drained or fired.
	start := time.Now()
	defer func() { metrics.TriggerPassSeconds.Observe(time.Since(start).Seconds()) }()
	// Collect paged-cursor rows no live trigger, parked failure, or in-flight
	// drain owns anymore before the pass — best-effort, a sweep
	// error never blocks delivery.
	if err := ds.sweepPagedCursors(ctx); err != nil {
		ds.svc.log.Warn("substrate: sweeping orphaned paged cursors", "error", err)
	}
	// The webhook requests the door recorded and nothing fired: a stop after
	// the 202 or mid-fire, a restore, a trigger that runs again. Detached,
	// so a slow fire does not hold the pass (webhooks.go resumeWebhooks).
	ds.resumeWebhooks()
	triggers, err := ds.loadTriggers(ctx)
	if err != nil {
		return 0, err
	}
	ds.pruneActivity(triggers)
	schedules, records := ds.dispatchable(triggers, true)

	recordsDone := make(chan struct{})
	type laneResult struct {
		ran  int
		errs []error
	}
	scheduled := make(chan laneResult, 1)
	go func() {
		var res laneResult
		// The dispatcher's recover does not reach this goroutine, so the
		// lane carries its own and the pass reports the panic as an error.
		defer func() {
			if r := recover(); r != nil {
				ds.svc.log.Error("substrate: the schedule lane panicked and was contained; this pass fires no more schedules",
					"repository", ds.Repository().ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				res.errs = append(res.errs, fmt.Errorf("schedule lane panicked: %v", r))
			}
			scheduled <- res
		}()
		res.ran, res.errs = ds.scheduleLane(ctx, schedules, recordLaneIDs(records), recordsDone)
	}()
	// The pass ends only once the lane has stopped and its fire in hand has
	// settled, a panic in the record lane included: the dispatcher starts
	// this repository's next pass, and shutdown stops waiting, on this
	// return, and a lane left behind would fire the same triggers as the
	// next pass's.
	stopLane := sync.OnceValue(func() laneResult {
		close(recordsDone)
		return <-scheduled
	})
	defer stopLane()

	total := 0
	var errs []error
	for _, lt := range records {
		release, err := ds.svc.takeDeliverySlot(ctx, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("trigger %s: %w", lt.ID, err))
			break
		}
		// The slot is given back on a panic too: one that escapes
		// passRecordTrigger's own recover unwinds the pass to the
		// dispatcher's, and the process goes on with the slots it has.
		n, perr := func() (int, error) {
			defer release()
			return ds.passRecordTrigger(ctx, lt.trigger)
		}()
		total += n
		if n > 0 {
			metrics.TriggerDeliveries.WithLabelValues(lt.ID).Add(float64(n))
		}
		if perr != nil {
			errs = append(errs, fmt.Errorf("trigger %s: %w", lt.ID, perr))
		}
	}
	res := stopLane()
	return total + res.ran, errors.Join(append(errs, res.errs...)...)
}

// passRecordTrigger is one record trigger's turn in a pass. A panic in its
// delivery ends this trigger's turn and not the walk, so the triggers after
// it in id order still run; a panic that unwound the pass would starve every
// later trigger for as long as it recurs. The transaction the panic was in
// rolls back and the claim the delivery held in runningClaims is released
// on the way out. What committed before the panic stays: for a function
// delivery that is nothing, so the cursor stands where the last settled
// delivery left it and the next pass delivers the change again. An agent
// delivery commits its claim before its loop runs (agents.go), moving the
// cursor past the change in that transaction, and the loop commits as it
// goes; so after a panic in the loop or its completion the cursor, the
// claim row and the loop's writes stay, nothing redelivers by itself, and
// the released claim reads as interrupted under `…/parked` for a hand to
// retry or forget, as after a restart. The pass reports the panic as the
// trigger's error, on every pass it recurs. The deliveries the turn applied
// before the panic are not counted in the pass's total.
func (ds *dataset) passRecordTrigger(ctx context.Context, tr *trigger) (ran int, err error) {
	ds.markPassed(tr.ID)
	defer func() {
		if r := recover(); r != nil {
			ds.svc.log.Error("substrate: a record trigger's delivery panicked and was contained; the pass goes on to the next trigger",
				"repository", ds.Repository().ID, "trigger", tr.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			ran, err = 0, fmt.Errorf("delivery panicked: %v", r)
		}
	}()
	return ds.processRecordTrigger(ctx, tr, newPassDeadline())
}

// recordLaneIDs is the set of the record lane's triggers.
func recordLaneIDs(records []loadedTrigger) map[string]bool {
	ids := make(map[string]bool, len(records))
	for _, lt := range records {
		ids[lt.ID] = true
	}
	return ids
}

// scheduleLanePoll is how often the schedule lane looks for a due
// occurrence while the record lane still runs, so it bounds how late a fire
// starts behind a record backlog: one poll, plus a wait for a free worker
// while laneWorkers of the repository's other schedule triggers fire. Each
// look reads the trigger rows again, which is what an idle repository's
// pass costs every dispatcher tick, so a trigger written mid-pass fires on
// time too. A var only so a test can lower it.
var scheduleLanePoll = 5 * time.Second

// scheduleLaneWorkers is how many schedule fires one pass's schedule lane
// runs at once when WithTriggerLaneWorkers does not say
// (SUBSTRATE_TRIGGER_LANE_WORKERS). Each runs a different trigger, so a
// slow sync does not delay the next one due in the same repository.
var scheduleLaneWorkers = 4

// TriggerDeliverySlots is how many deliveries the dispatcher runs at once
// over every repository of the process: each record trigger's walk and each
// schedule fire holds one for as long as it runs (takeDeliverySlot). It is
// the ceiling the dispatcher's eight passes of two lanes held before a lane
// ran fires in parallel, so a host with many busy repositories runs no more
// runner processes and transactions than it did (decision 0150).
const TriggerDeliverySlots = 16

// takeDeliverySlot waits for one of the process's TriggerDeliverySlots and
// returns the func that gives it back. It gives up when ctx ends or stop
// closes, whichever is first; a nil stop never closes.
func (s *service) takeDeliverySlot(ctx context.Context, stop <-chan struct{}) (release func(), err error) {
	select {
	case s.deliverySlots <- struct{}{}:
		return func() { <-s.deliverySlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-stop:
		return nil, errLaneStopped
	}
}

// errLaneStopped is takeDeliverySlot's answer when the record lane finished
// while the schedule lane waited for a slot: the lane stops launching, and
// the trigger it waited for fires at the next pass.
var errLaneStopped = errors.New("schedule lane stopped")

// scheduleLane is a pass's schedule lane: the due occurrences of every
// schedule trigger in first, then of every enabled schedule trigger the
// rows hold at each poll, until recordsDone closes or ctx ends. The first
// round runs whole even when recordsDone is already closed, which is how a
// repository with no record triggers fires its schedules at all.
//
// Each trigger's fires run in a worker goroutine, at most laneWorkers at
// once and each holding a delivery slot, and a trigger with a worker
// running gets no second one, so its occurrences still fire oldest first
// and one at a time. Each trigger fires at most scheduleDrainPerPass
// occurrences over the whole pass, however many looks it lasts, so a
// trigger far behind catches up at the rate one pass allows. A trigger the
// record lane runs this pass is skipped, so one whose source changed
// mid-pass never runs in both lanes. A trigger whose delivery fails on
// every look reports its latest error once. The lane returns only after
// every worker it started has ended.
func (ds *dataset) scheduleLane(ctx context.Context, first []loadedTrigger, records map[string]bool, recordsDone <-chan struct{}) (int, []error) {
	var (
		wg sync.WaitGroup
		// mu guards everything below it: the workers write it as they end,
		// and the lane reads it before it starts one.
		mu     sync.Mutex
		total  int
		failed = map[string]error{}
		order  []string
		fired  = map[string]int{}
		busy   = map[string]bool{}
	)
	// fail records a trigger's latest error; the caller holds mu.
	fail := func(id string, err error) {
		if _, seen := failed[id]; !seen {
			order = append(order, id)
		}
		failed[id] = err
	}
	// A panic in the lane's own code unwinds through this join, so the
	// pass's recover never reports the lane done while a worker still runs.
	defer wg.Wait()
	workers := make(chan struct{}, ds.svc.laneWorkers)
	// launch starts a worker for each trigger in schedules that has fires
	// left and none running, waiting for a free worker and a delivery slot.
	// It reports false once ctx ends or stop closes; a nil stop never
	// closes.
	launch := func(schedules []loadedTrigger, stop <-chan struct{}) bool {
		for _, lt := range schedules {
			mu.Lock()
			left := scheduleDrainPerPass - fired[lt.ID]
			running := busy[lt.ID]
			mu.Unlock()
			if records[lt.ID] || left <= 0 || running {
				continue
			}
			select {
			case workers <- struct{}{}:
			case <-ctx.Done():
				return false
			case <-stop:
				return false
			}
			release, err := ds.svc.takeDeliverySlot(ctx, stop)
			if err != nil {
				<-workers
				return false
			}
			// Checked again with both held: a select picks at random among
			// its ready cases, so either wait above may have taken its token
			// after ctx ended or stop closed.
			if ctx.Err() != nil || closed(stop) {
				release()
				<-workers
				return false
			}
			mu.Lock()
			busy[lt.ID] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					// The pass's recover does not reach this goroutine, so
					// each worker carries its own, and a trigger that
					// panicked fires no more this pass.
					r := recover()
					mu.Lock()
					if r != nil {
						ds.svc.log.Error("substrate: a schedule fire panicked and was contained; this pass fires the trigger no more",
							"repository", ds.Repository().ID, "trigger", lt.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
						fired[lt.ID] = scheduleDrainPerPass
						fail(lt.ID, fmt.Errorf("schedule fire panicked: %v", r))
					}
					delete(busy, lt.ID)
					mu.Unlock()
					release()
					<-workers
				}()
				ds.markPassed(lt.ID)
				n, f, err := ds.processScheduleTrigger(ctx, lt, newPassDeadline(), left)
				if n > 0 {
					metrics.TriggerDeliveries.WithLabelValues(lt.ID).Add(float64(n))
				}
				mu.Lock()
				defer mu.Unlock()
				fired[lt.ID] += f
				total += n
				if err != nil {
					fail(lt.ID, err)
				}
			}()
		}
		return true
	}
	schedules := first
	// The first round does not watch recordsDone: a repository with no
	// record triggers has closed it before the lane starts.
	var stop <-chan struct{}
	for {
		if !launch(schedules, stop) {
			break
		}
		stop = recordsDone
		timer := time.NewTimer(scheduleLanePoll)
		select {
		case <-recordsDone:
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
		if ctx.Err() != nil || closed(recordsDone) {
			break
		}
		triggers, err := ds.loadTriggers(ctx)
		if err != nil {
			mu.Lock()
			fail("", err)
			mu.Unlock()
			break
		}
		schedules, _ = ds.dispatchable(triggers, false)
	}
	// Joined here as well as deferred: the errors below are read only once
	// every worker has written its own.
	wg.Wait()
	errs := make([]error, 0, len(order))
	for _, id := range order {
		if id == "" {
			errs = append(errs, fmt.Errorf("schedule lane: %w", failed[id]))
			continue
		}
		errs = append(errs, fmt.Errorf("trigger %s: %w", id, failed[id]))
	}
	return total, errs
}

// closed reports whether ch is closed, without waiting.
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// dispatchable splits a pass's triggers into the schedule and record
// triggers that run: enabled, parsed, and naming a callable that resolves.
// warn logs the ones skipped for a fault, once per pass, so the schedule
// lane's later loads pass false.
func (ds *dataset) dispatchable(triggers []loadedTrigger, warn bool) (schedules, records []loadedTrigger) {
	for _, lt := range triggers {
		if lt.Err != nil {
			if warn {
				ds.svc.log.Warn("substrate: trigger row does not parse — it is skipped, its cursor stands still",
					"trigger", lt.ID, "error", lt.Err)
			}
			continue
		}
		if !lt.Enabled {
			continue
		}
		if !lt.runnable() {
			if warn {
				ds.svc.log.Warn("substrate: trigger names a callable that no longer resolves — it is skipped, its cursor stands still",
					"trigger", lt.ID, "callable", lt.CallableID)
			}
			continue
		}
		switch {
		case lt.Schedule != nil:
			schedules = append(schedules, lt)
		case lt.Record != nil:
			records = append(records, lt)
		case lt.Webhook:
			// Webhook triggers deliver on wake only.
		}
	}
	return schedules, records
}

// --- record-sourced delivery ---------------------------------------------------

// processRecordTrigger delivers a record trigger's backlog from its cursor
// until a changelog read comes back empty or the deadline is spent. A spent
// deadline stops the drain between deliveries, never inside one, and before
// the scan position moves past a matched row still owed, so the cursor only
// ever stands past rows that were delivered or matched nothing.
func (ds *dataset) processRecordTrigger(ctx context.Context, tr *trigger, deadline passDeadline) (int, error) {
	cursor, err := ds.ensureCursor(ctx, tr.ID)
	if err != nil {
		return 0, err
	}
	ran := 0
	for {
		changes, scanned, err := ds.changesPast(ctx, tr, cursor)
		if err != nil {
			return ran, err
		}
		matched := matchChanges(tr, tr.selfActors(ds.registry()), changes)
		if tr.Record.Coalesce {
			matched = coalesceChanges(matched)
		}
		for i, ch := range matched {
			if i > 0 && deadline.spent() {
				return ran, nil
			}
			n, next, err := ds.deliverWithRetry(ctx, tr, ch, cursor)
			ran += n
			if errors.Is(err, errCursorMoved) {
				ds.logYield(tr.ID, "delivery", ch.Seq)
				return ran, nil
			}
			if errors.Is(err, errCallableGone) {
				ds.svc.log.Warn("substrate: trigger names a callable that no longer resolves — it is skipped, its cursor stands still",
					"trigger", tr.ID, "callable", tr.CallableID)
				return ran, nil
			}
			if err != nil {
				return ran, err
			}
			ds.markDelivered(tr.ID)
			cursor = next
		}
		// Trailing rows the source skipped, the ones the read filtered out
		// by kind included, move the SCAN position, the one cursor motion
		// outside the ledger (delivery.go): a crash or a restore before this
		// line only re-reads rows that do not match.
		if scanned > cursor {
			if err := ds.advanceCursor(ctx, tr, cursor, scanned); err != nil {
				if errors.Is(err, errCursorMoved) {
					ds.logYield(tr.ID, "scan", scanned)
					return ran, nil
				}
				return ran, err
			}
			cursor = scanned
		}
		if len(changes) == 0 || deadline.spent() {
			return ran, nil
		}
		// Loop until a read comes back empty rather than on a short batch:
		// the deliveries above appended the callable's own writes (and their
		// run rows) past the batch end, and the drain owes the cursor those
		// rows too — they are self- and type-excluded, so this terminates.
	}
}

// matchChanges filters one batch by the trigger's record source. Exclusion
// is by the CALLABLE's actor — a callable never sees its own writes,
// whatever trigger delivers them, by the run type, since every delivery
// writes a run row and a `*` subscription over runs would feed itself, and by
// the delivery entry, which is the ledger's own bookkeeping (delivery.go).
// (An agent's thread/message rows carry the agent's actor, so the same
// exclusion keeps an agent off its own transcript.) A function's self also
// covers the agents it may run (trigger.selfActors).
func matchChanges(tr *trigger, self map[substrate.Actor]bool, changes []substrate.Change) []substrate.Change {
	var out []substrate.Change
	for _, ch := range changes {
		if self[ch.Actor] || ch.Kind == typeTriggerRun || ch.Op == substrate.OpDelivery {
			continue
		}
		if !tr.Record.matches(ch.Kind, runner.OpOf(ch)) {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// coalesceChanges keeps the LAST pending change per record, in seq order:
// five changes to one record run once against current state and the cursor
// advances past all five. An earlier change this drops is always subsumed by
// a later one still past the cursor, so a crash mid-batch loses nothing.
//
// The coalescing key is the full (type, id) identity, never the bare id: an id
// is unique only within a type, so two matched types sharing an id are two
// distinct records — keying on id alone discarded one delivery while the
// cursor advanced past it, dropping it for good.
func coalesceChanges(changes []substrate.Change) []substrate.Change {
	last := map[string]int{}
	for i, ch := range changes {
		last[coalesceKey(ch)] = i
	}
	out := make([]substrate.Change, 0, len(last))
	for i, ch := range changes {
		if last[coalesceKey(ch)] == i {
			out = append(out, ch)
		}
	}
	return out
}

// coalesceKey is a change's (type, id) identity as a map key — the NUL
// separator cannot occur in either half, so distinct identities never collide.
func coalesceKey(ch substrate.Change) string { return ch.Kind + "\x00" + ch.RecordID }

// deliverWithRetry runs one delivery to completion: retry with backoff, then
// park-and-advance. It returns the cursor position the delivery left behind.
// Its own error return is infrastructure only.
func (ds *dataset) deliverWithRetry(ctx context.Context, tr *trigger, ch substrate.Change, from int64) (int, int64, error) {
	delivery, leave := ds.startRecordDelivery(tr.ID)
	// Deferred, so a delivery that panics leaves the running set too.
	defer leave()
	started := nowUTC()
	depth, err := ds.causalDepth(ctx, ch.Seq)
	if err != nil {
		return 0, from, err
	}
	if depth >= causalDepthCap {
		if err := ds.parkAndAdvance(ctx, tr, ch, from, 0, started, nil,
			fmt.Errorf("%w: change %d sits %d causes deep (cap %d)", errCausalDepth, ch.Seq, depth, causalDepthCap),
		); err != nil {
			return 0, from, err
		}
		return 0, ch.Seq, nil
	}
	var lastErr error
	attempts := triggerAttempts
	chain := ds.recordChainKey(tr.ID, ch.Seq)
	settle := ds.dispatchSettlement(tr, ch, from, started)
	settle.delivery = delivery
	// The claim an agent attempt takes is held through every attempt and
	// the park, so a retry by hand cannot start a second loop between them.
	defer settle.release()
	// An agent the body runs records its thread here, so a failed attempt
	// after it parks rather than running the agent again (agentThreads).
	threads := &agentThreads{settle: settle}
	ctx = withCallOrigin(ctx, callOrigin{threads: threads})
	for attempt := range triggerAttempts {
		settle.attempt = attempt + 1
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, from, ctx.Err()
			case <-time.After(triggerRetryBackoff[min(attempt-1, len(triggerRetryBackoff)-1)]):
			}
		}
		// Load the committed resume cursor before EVERY attempt:
		// once a page has committed, this attempt (or a post-crash redispatch,
		// which is just another deliverWithRetry) continues the drain from the
		// last cursor instead of replaying the chain from page zero and double-
		// applying its effects.
		resume, rerr := ds.loadPagedProgress(ctx, chain)
		if rerr != nil {
			return 0, from, rerr
		}
		res, err := ds.deliver(ctx, tr, ch, from, depth, resume, settle)
		cause := err
		err = agentRetryGate(threads, err)
		if err == nil {
			if res.skipped {
				// The guard said no: a skip is a settled attempt — record it
				// and advance the cursor in one transaction.
				if err := ds.recordSkipAndAdvance(ctx, tr, ch, from, started, res.reason); err != nil {
					return 0, from, err
				}
				return 0, ch.Seq, nil
			}
			return res.ran, ch.Seq, nil
		}
		if errors.Is(err, errCursorMoved) || errors.Is(err, errCallableGone) {
			return 0, from, err
		}
		if declinedDelivery(err) && threads.opened() != "" {
			// The body lost a race it declared it could lose after its agent
			// claimed the delivery (agentThreads.bind): the cursor moved with
			// the claim, so the skip retires the claim instead.
			if err := ds.skipClaimed(parkContext(ctx, threads), settle, runRecord{
				trigger: tr.ID, callable: tr.callablePath(), mode: runner.ModeRecord,
				seq: ch.Seq, recordID: ch.RecordID, status: runStatusSkipped,
				attempt: settle.attempt, startedAt: started,
				errMsg: fmt.Sprintf("%v (agent thread %s)", cause, threads.opened()),
			}); err != nil {
				return 0, from, err
			}
			return 0, ch.Seq, nil
		}
		if declinedDelivery(err) {
			// Another dispatch holds this delivery, or this one lost a race it
			// declared it could lose: either way the work is another
			// invocation's, this pass moves past it and says so. Nothing
			// committed — the transaction rolled back whole.
			if err := ds.recordSkipAndAdvance(ctx, tr, ch, from, started, err.Error()); err != nil {
				return 0, from, err
			}
			return 0, ch.Seq, nil
		}
		// Only the DISPATCHER's context ending aborts the pass: a
		// per-invocation runner timeout is a delivery failure that rides the
		// retries. A body whose agent opened a thread parks even then: its
		// claim already moved the cursor, and the park records why.
		if ctx.Err() != nil && !errors.Is(err, errAgentThreadOpened) {
			return 0, from, ctx.Err()
		}
		lastErr = err
		// A deterministic trip — a read or call outside the allowlist, a
		// blown budget — reproduces on every retry, so it parks immediately
		// (the errCausalDepth precedent). A timeout is not deterministic
		// (db load) and rides the attempts. A paged drain that already
		// committed pages (errPagedParked) also parks now: re-running it would
		// only resume the same chain, so leave it to the parked-failure retry.
		if runner.Deterministic(err) || errors.Is(err, errPagedParked) || errors.Is(err, errAgentThreadOpened) {
			attempts = attempt + 1
			break
		}
	}
	if err := ds.parkAndAdvance(parkContext(ctx, threads), tr, ch, from, attempts, started, settle.sync, lastErr); err != nil {
		return 0, from, err
	}
	return 0, ch.Seq, nil
}

// deliverResult is one settled deliver call.
type deliverResult struct {
	ran     int  // 1 when effects applied
	moved   bool // the cursor advanced (dispatch mode only)
	skipped bool // the when guard said no
	// reason says why a skip skipped when the guard was not the reason (a
	// paused sync); empty for a guard-false, which needs no words.
	reason  string
	effects map[string]int
	pages   int // committed pages when the body paged; 1 for a single-shot body
}

// settledResult is a delivery's outcome from its effect summary: ran when
// anything applied, moved when the dispatch owned the cursor.
func settledResult(advance bool, summary map[string]int, pages int) deliverResult {
	res := deliverResult{moved: advance, effects: summary, pages: pages}
	if len(summary) > 0 {
		res.ran = 1
	}
	return res
}

// settlement is what a delivery writes to complete: the ledger motion that
// acknowledges it (a cursor or fire-state advance), the retirement of the
// failure a retry re-runs, the delivery entry those ride, and the run record.
// A function delivery writes all of it in the transaction that commits its
// LAST effects (settle), so a crash cannot commit effects with no completion
// recorded. An agent delivery cannot: the loop's writes are many
// transactions across model turns. It writes the acknowledgement FIRST
// (claim), recording the delivery as in flight under a parked failure, then
// runs the loop, then retires the claim with the run record (complete). A
// crash between the two leaves the claim, which the next open parks as
// interrupted (settleInterruptedAgentRuns) for a person to retry or forget;
// nothing redelivers by itself and no effect commits without a recorded
// delivery state. A function
// body that runs an agent takes the same claim in the agent thread's
// transaction (agentThreads.bind), and its final transaction then retires
// the claim instead of acknowledging (settle). nil settles nothing: a manual
// run mints nothing durable.
type settlement struct {
	ds      *dataset
	trigger string
	// attempt is the attempt the run record names; the dispatch updates it
	// per try, the settlement living across the tries.
	attempt int
	// claimed is the claim this dispatch wrote on an earlier try, 0 before
	// the first; a claim found in the tables that is not this one is another
	// dispatch's (errClaimedElsewhere).
	claimed int64
	// held is the failure id this settlement holds in runningClaims, 0 when
	// it holds none; release gives it back.
	held int64
	// seq and fireID name the delivery: a record change's seq, or a fire's
	// id. A claim is found again by them (claimedFailure).
	seq    int64
	fireID string
	// acknowledge moves the cursor or fire state; nil on a retry, whose
	// failure is retired instead.
	acknowledge func(t *txn) error
	// retire is the parked failure a retry re-runs, 0 on a dispatch.
	retire int64
	// pending is the admitted request a webhook fire runs (webhooks.go
	// admitWebhook), the row `retire` names: a dispatch that is also a
	// retry. An agent fire's claim rewrites it as in flight, so a crash
	// mid-loop leaves a claim for a hand and not a pending entry the next
	// open would run again; nil everywhere else.
	pending *foldFailure
	// record writes the run record; nil on a retry.
	record func(t *txn, res deliverResult) error
	// started is when the dispatch began the delivery; zero on a retry,
	// which measures from its own start.
	started time.Time
	// sync is the delivery's hand on a `sync`-trait record (sync.go): set
	// by deliver once the guard passed on a record whose kind binds the
	// trait, nil for every other delivery.
	sync *syncStamp
	// supersedes is the occurrence a schedule fire delivers, dispatched or
	// retried, zero on every other delivery: once it settles, the trigger's
	// parked fires at or before it retire with it (retireSupersededFires).
	supersedes time.Time
	// superseded is the parked fires this settlement holds in runningClaims
	// while it retires them; release gives them back.
	superseded map[int64]bool
	// delivery is the running record delivery this settlement belongs to
	// (startRecordDelivery), nil on every other settlement. The claim it
	// holds is recorded there, so a status read counts the delivery once.
	delivery *recordDelivery
}

// settle is the function path: everything in one transaction with the
// effects.
func (s *settlement) settle(t *txn, res deliverResult) error {
	if s == nil {
		return nil
	}
	if err := s.ds.settlementFault(t); err != nil {
		return err
	}
	switch {
	case s.claimed != 0:
		// A function body that ran an agent claimed the delivery in the
		// thread's transaction (agentThreads.bind): the acknowledgement
		// landed there, and the claim retires here.
		if err := t.lockFailure(s.trigger, s.claimed); err != nil {
			return err
		}
		if err := t.unparkTx(s.trigger, s.claimed); err != nil {
			return err
		}
	case s.acknowledge != nil:
		if err := s.acknowledge(t); err != nil {
			return err
		}
	}
	if s.retire != 0 {
		if err := t.lockFailure(s.trigger, s.retire); err != nil {
			return err
		}
		if err := t.unparkTx(s.trigger, s.retire); err != nil {
			return err
		}
	}
	if err := s.retireSupersededFires(t); err != nil {
		return err
	}
	if err := t.settleDelivery(s.trigger); err != nil {
		return err
	}
	if s.record != nil {
		if err := s.record(t, res); err != nil {
			return err
		}
	}
	// The sync stamps ride the same commit as the effects and the run
	// record: a delivery cannot settle with its record still `running`.
	if s.sync != nil && s.sync.stamped {
		return t.syncSettleOK(s.sync)
	}
	return nil
}

// retireClaim retires the claim a function body's agent took
// (agentThreads.bind) without the completion's run record: the claimed
// failure a dispatch wrote, or the admitted webhook request's row that
// bind rewrote as in flight. The caller writes the run.
func (s *settlement) retireClaim(t *txn) error {
	id := s.claimed
	if id == 0 {
		id = s.retire
	}
	if id != 0 {
		if err := t.lockFailure(s.trigger, id); err != nil {
			return err
		}
		if err := t.unparkTx(s.trigger, id); err != nil {
			return err
		}
	}
	return t.settleDelivery(s.trigger)
}

// claim is the agent path's first transaction, before the loop: the
// acknowledgement, and the delivery recorded as in flight under a parked
// failure whose id is the claim's own delivery entry. An attempt that follows
// a failed one finds the claim the first attempt wrote and writes nothing; a
// retry's claim is the failure it re-runs. It returns the failure id the
// completion retires.
func (s *settlement) claim(t *txn) (int64, error) {
	if s.retire != 0 {
		if s.pending != nil {
			// The caller clears pending once this commits (claimAgentDelivery),
			// so a later attempt of the same fire skips the rewrite and a
			// rolled-back one repeats it.
			if err := t.lockFailure(s.trigger, s.retire); err != nil {
				return 0, err
			}
			row := *s.pending
			row.LastError = inFlightError
			if err := t.parkTx(s.trigger, row); err != nil {
				return 0, err
			}
			if err := t.settleDelivery(s.trigger); err != nil {
				return 0, err
			}
		}
		return s.retire, nil
	}
	if s.claimed != 0 {
		return s.claimed, nil
	}
	if id, ok, err := t.claimedFailure(s.trigger, s.seq, s.fireID); err != nil {
		return 0, err
	} else if ok {
		return 0, fmt.Errorf("%w: failure %d", errClaimedElsewhere, id)
	}
	id, err := t.reserveSeq()
	if err != nil {
		return 0, err
	}
	if s.acknowledge != nil {
		if err := s.acknowledge(t); err != nil {
			return 0, err
		}
	}
	if err := t.parkTx(s.trigger, foldFailure{
		ID: foldInt(id), Seq: foldInt(s.seq), FireID: s.fireID, LastError: inFlightError, ParkedAt: t.now,
	}); err != nil {
		return 0, err
	}
	// Held BEFORE the claim commits: a retry reads the row only after the
	// commit, and by then the id is taken.
	if err := s.acquire(id); err != nil {
		return 0, err
	}
	return id, t.appendDeliveryAt(s.trigger, id)
}

// acquire takes the in-process claim on a failure id (dataset.runningClaims):
// compare-and-swap, so a second hand on the same id answers ErrConflict and
// starts nothing. release gives it back; a settlement holds at most one.
func (s *settlement) acquire(id int64) error {
	if s.held == id {
		return nil
	}
	if s.held != 0 {
		return fmt.Errorf("substrate/engine: settlement of trigger %s already holds failure %d", s.trigger, s.held)
	}
	// Recorded on the delivery before runningClaims publishes the claim: a
	// status read takes its runningClaims snapshot first, so one that finds
	// the claim running finds it on the delivery too.
	s.ds.setDeliveryClaim(s.delivery, id)
	if _, taken := s.ds.runningClaims.LoadOrStore(id, struct{}{}); taken {
		s.ds.setDeliveryClaim(s.delivery, 0)
		return fmt.Errorf("%w: trigger %s failure %d is a delivery this process is still running", substrate.ErrConflict, s.trigger, id)
	}
	s.held = id
	return nil
}

// release gives the held claim back, once the completion, the park or the
// retry that held it has ended, and the parked fires a settlement retired.
func (s *settlement) release() {
	if s == nil {
		return
	}
	if s.held != 0 {
		s.ds.runningClaims.Delete(s.held)
		// Cleared after runningClaims lets go, the reverse of acquire.
		s.ds.setDeliveryClaim(s.delivery, 0)
		s.held = 0
	}
	for id := range s.superseded {
		s.ds.runningClaims.Delete(id)
	}
	s.superseded = nil
}

// retireSupersededFires retires, once a schedule occurrence settles, every
// parked fire of the same trigger at or before that occurrence (decision
// 0142). A schedule fire carries nothing a later one does not carry again,
// so a retry of the parked fire repeats work the settled one did: a
// provider's scheduled sync drains whatever is due when it runs. The rows
// are DELETED through the unpark fold a delivered retry writes, on the
// settling delivery's own entry, so a rebuild and a restore agree:
// trigger_failures has no state column to mark, and the fire's parked run
// row and the changelog keep the history. A row parked by an earlier binary
// is a row like any other and retires at the next settled fire.
//
// A record trigger never supersedes: each of its parks is one record's
// change, which no delivery of another change re-delivers. Nor is a webhook
// request's park retired (it carries its request as a payload), nor a fire
// whose id is not an occurrence (a wake), nor an agent claim, in flight or
// interrupted, which decision 0064 leaves to a person because its run may
// have written records. A row a hand is retrying now is held in
// runningClaims and keeps its retry. The rows this settlement retires are
// held the same way until it ends, so a retry of one answers conflict
// instead of running a fire whose row is being deleted.
func (s *settlement) retireSupersededFires(t *txn) error {
	if s.supersedes.IsZero() {
		return nil
	}
	// Held elsewhere: every id in runningClaims but the ones this settlement
	// took on an attempt that rolled back.
	held := []int64{}
	s.ds.runningClaims.Range(func(k, _ any) bool {
		if id, ok := k.(int64); ok && !s.superseded[id] {
			held = append(held, id)
		}
		return true
	})
	heldJSON, err := json.Marshal(held)
	if err != nil {
		return err
	}
	// A fire id is fireID's fixed-width UTC form, so under the C collation
	// its string order is its time order and the bound sits in the query.
	rows, err := t.query(`
		SELECT id FROM trigger_failures
		WHERE trigger_id = $1 AND payload IS NULL AND last_error NOT IN ($2, $3, $4, $5)
		  AND fire_id ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$'
		  AND fire_id COLLATE "C" <= $6
		  AND id NOT IN (SELECT jsonb_array_elements_text($7::jsonb)::bigint)
		ORDER BY id LIMIT $8`,
		s.trigger, pendingWebhookError, inFlightError, legacyInFlightError, interruptedAgentError,
		fireID(s.supersedes), string(heldJSON), supersedeBatch)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		// A hand that took the row since the snapshot keeps it.
		if !s.holdSuperseded(id) {
			continue
		}
		if err := t.unparkTx(s.trigger, id); err != nil {
			return err
		}
	}
	return nil
}

// holdSuperseded takes the in-process claim on a parked fire this settlement
// retires, false when another hand holds it. A settlement that rolled back
// and settles again already holds the claims it took the first time.
func (s *settlement) holdSuperseded(id int64) bool {
	if s.superseded[id] {
		return true
	}
	if id == s.held {
		return false
	}
	if _, taken := s.ds.runningClaims.LoadOrStore(id, struct{}{}); taken {
		return false
	}
	if s.superseded == nil {
		s.superseded = map[int64]bool{}
	}
	s.superseded[id] = true
	return true
}

// complete is the agent path's last transaction, after the loop settled: the
// claim retires, and the run record lands beside the delivery entry.
func (s *settlement) complete(t *txn, claim int64, res deliverResult) error {
	if err := s.ds.settlementFault(t); err != nil {
		return err
	}
	if err := t.lockFailure(s.trigger, claim); err != nil {
		return err
	}
	if err := t.unparkTx(s.trigger, claim); err != nil {
		return err
	}
	if err := s.retireSupersededFires(t); err != nil {
		return err
	}
	if err := t.settleDelivery(s.trigger); err != nil {
		return err
	}
	if s.record != nil {
		return s.record(t, res)
	}
	return nil
}

// dispatchSettlement settles a dispatched record delivery: the cursor advance
// from the position the pass read to the change's seq, then the OK run
// record.
func (ds *dataset) dispatchSettlement(tr *trigger, ch substrate.Change, from int64, started time.Time) *settlement {
	s := &settlement{
		ds: ds, trigger: tr.ID, seq: ch.Seq, started: started,
		acknowledge: func(t *txn) error { return t.advanceCursorTx(tr.ID, from, ch.Seq) },
	}
	s.record = func(t *txn, res deliverResult) error {
		return t.putSystemRun(runRecord{
			trigger: tr.ID, callable: tr.callablePath(), mode: runner.ModeRecord,
			seq: ch.Seq, recordID: ch.RecordID, status: runStatusOK,
			attempt: s.attempt, startedAt: started, effects: res.effects, pages: res.pages,
		}, true)
	}
	return s
}

// settlementFault runs the test seam a test installed (dataset.deliveryFault),
// nothing otherwise.
func (ds *dataset) settlementFault(t *txn) error {
	ds.mu.RLock()
	fault := ds.deliveryFault
	ds.mu.RUnlock()
	if fault == nil {
		return nil
	}
	return fault(t)
}

// deliver evaluates one change against current state and applies the
// effects. `from` is the cursor the dispatch read; a manual run and a parked
// retry pass a negative one, which selects the manual mode. `settle` commits
// with the last effects (dispatchSettlement, a retry's unpark), or nil for a
// run that settles nothing. `resume` is the paged-checkpoint seed cursor:
// nil for a fresh delivery, the last committed page for a retry of a parked
// drain. The guard, the body and the record load all run BEFORE the
// transaction opens: nothing evaluates while the changelog append lock is
// held.
func (ds *dataset) deliver(ctx context.Context, tr *trigger, ch substrate.Change, from int64, depth int, resume pagedProgress, settle *settlement) (deliverResult, error) {
	var res deliverResult
	advance := from >= 0
	// The body that runs is the one the LAST apply landed, not the one this
	// pass resolved when it loaded its triggers (triggers.go refreshCallable).
	if err := ds.refreshCallable(tr); err != nil {
		return res, err
	}
	envelope, err := ds.deliveryEnvelope(ctx, ch)
	if err != nil {
		return res, err
	}
	ok, err := evalWhen(ctx, tr, envelope)
	if err != nil {
		return res, err
	}
	if !ok {
		res.skipped = true
		return res, nil
	}
	// A record whose kind binds the `sync` trait, delivered to a function of
	// the kind's own package (sync.go syncStampFor): a pause skips the delivery
	// before the body runs, and the first attempt writes `running` in a
	// transaction of its own so the run is visible while it runs; the
	// settlement and the park write the other half (sync.go). A manual run
	// settles nothing, so it stamps nothing either.
	if settle != nil {
		if stamp := ds.syncStampFor(tr, ch, envelope, settle.started); stamp != nil {
			if syncPaused(envelope) {
				res.skipped = true
				res.reason = "sync paused: the record's syncPaused is set"
				return res, nil
			}
			if settle.sync == nil {
				settle.sync = stamp
			}
			if !settle.sync.stamped {
				if err := ds.syncStart(ctx, settle.sync, ch.Seq); err != nil {
					return res, err
				}
				settle.sync.stamped = true
			}
		}
	}
	mode := runner.ModeRecord
	if !advance {
		mode = runner.ModeManual
	}
	if tr.Agent != nil {
		// Admission under the lifecycle fence for the AGENT too:
		// held from here through the agent loop's last message, thread
		// settlement and cursor advance in deliverToAgent — a trigger loaded
		// before a disable cannot begin (or finish) its agent after the verb
		// returns.
		lctx, release, err := ds.admitCallable(ctx, tr.Agent.Package, tr.Agent.Identity())
		if err != nil {
			return res, err
		}
		defer release()
		return ds.deliverToAgent(lctx, tr, ch, depth, envelope, mode, advance, settle)
	}
	// Admission under the bundle lifecycle fence, held from here through the
	// effect commit below: disable/uninstall/purge take the exclusive side,
	// so a delivery that already passed admission finishes — effects and
	// cursor together — BEFORE the verb returns, and nothing admits after it
	// (bundles.go). The leased context flows into runCallable so nested host
	// Calls inherit the lease instead of re-locking.
	ctx, release, err := ds.admitCallable(ctx, tr.Callable.Package, tr.Callable.Identity())
	if err != nil {
		return res, err
	}
	defer release()
	in := runner.Input{
		Mode:        mode,
		Envelope:    envelope,
		CausalDepth: depth,
		// Repository-qualified: per-repository changelog sequences collide, so an
		// external deduper keyed on "<trigger>/<seq>" alone would suppress
		// one repository's call as a duplicate of another's. The same string keys
		// this delivery's paged resume cursor.
		IdempotencyKey: ds.recordChainKey(tr.ID, ch.Seq),
		// Resume seeds a delivery of an existing paged chain from its last
		// committed page; nil on a fresh delivery — a body that never pages
		// never sees it.
		Resume: resume.cursor,
	}
	// The change rides the invocation so an agent the body runs stamps it on
	// its rows, and the causal-depth walk sees through the agent.
	origin := callOriginOf(ctx)
	origin.causedBy = ch.Seq
	ctx = withCallOrigin(ctx, origin)
	effects, _, more, err := ds.runCallableRaw(ctx, tr.Callable, in)
	if err != nil {
		return res, err
	}
	actor := substrate.Actor(tr.Callable.Actor())
	// Paged path: the body returned a page (or this is a resumed drain). The
	// pages commit off the causal chain — each page's effects with its resume
	// cursor, the final page clearing the cursor and settling the delivery.
	if more != nil || resume.exists {
		owner := pagedOwner{triggerID: tr.ID, kind: pagedKindRecord, identity: strconv.FormatInt(ch.Seq, 10)}
		summary, pages, derr := ds.pagedDrain(ctx, tr.Callable, in, actor, ch.Seq, tr.Callable.Caps.Emit,
			owner, resume, pagedPage{effects: effects, more: more}, pagedSettlement(advance, settle))
		if derr != nil {
			return res, derr
		}
		return settledResult(advance, summary, pages), nil
	}
	// Non-paged: the ordinary single transaction, effects and the settlement
	// committing together.
	if len(effects) == 0 && settle == nil {
		return res, nil
	}
	effects, err = ds.holdEffects(ctx, tr.Callable, effects, in.IdempotencyKey)
	if err != nil {
		return res, err
	}
	err = ds.inTx(ctx, actor, false, func(t *txn) error {
		t.causedBy = ch.Seq
		if err := t.applyEffects(tr.Callable.Caps.Emit, effects); err != nil {
			return err
		}
		// After the apply, which is what settles each held effect.
		res = settledResult(advance, effectsSummary(effects), 1)
		return settle.settle(t, res)
	})
	if err != nil {
		return deliverResult{}, err
	}
	ds.judgeHeld(effects)
	return res, nil
}

// pagedSettlement adapts a settlement to the drain's final page, which knows
// the whole chain's summary and page count.
func pagedSettlement(advance bool, settle *settlement) func(t *txn, summary map[string]int, pages int) error {
	if settle == nil {
		return nil
	}
	return func(t *txn, summary map[string]int, pages int) error {
		return settle.settle(t, settledResult(advance, summary, pages))
	}
}

// evalWhen runs the trigger's guard against the envelope's three bindings; a
// missing guard passes.
func evalWhen(ctx context.Context, tr *trigger, envelope map[string]any) (bool, error) {
	if tr.Record == nil || tr.Record.program == nil {
		return true, nil
	}
	return evalWhenProgram(ctx, tr.Record.program, envelope)
}

// parkAndAdvance records the failure, the parked run and the cursor motion
// past the change in one transaction, so a crash cannot double-park — and
// the same compare-and-swap that guards a delivery guards the park. The
// failure's id is the seq of the delivery entry that parks it. An agent
// delivery already claimed the change (settlement.claim): its cursor moved
// then, so the park rewrites the claim with the error and moves nothing.
func (ds *dataset) parkAndAdvance(ctx context.Context, tr *trigger, ch substrate.Change, from int64, attempts int, started time.Time, sync *syncStamp, cause error) error {
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		id, claimed, err := t.claimedFailure(tr.ID, ch.Seq, "")
		if err != nil {
			return err
		}
		if !claimed {
			if id, err = t.reserveSeq(); err != nil {
				return err
			}
		}
		if err := t.parkTx(tr.ID, foldFailure{
			ID: foldInt(id), Seq: foldInt(ch.Seq), RecordID: ch.RecordID,
			Attempts: foldInt(attempts), LastError: cause.Error(), ParkedAt: t.now,
		}); err != nil {
			return err
		}
		if claimed {
			if err := t.settleDelivery(tr.ID); err != nil {
				return err
			}
		} else {
			if err := t.advanceCursorTx(tr.ID, from, ch.Seq); err != nil {
				return err
			}
			if err := t.appendDeliveryAt(tr.ID, id); err != nil {
				return err
			}
		}
		if err := t.putRun(runRecord{
			trigger: tr.ID, callable: tr.callablePath(), mode: runner.ModeRecord,
			seq: ch.Seq, recordID: ch.RecordID, status: runStatusParked,
			attempt: attempts, startedAt: started, errMsg: cause.Error(),
		}); err != nil {
			return err
		}
		// The park and the record's `erroring` commit together, so a reader
		// never meets a parked delivery whose record still says `running`.
		return t.syncPark(sync, cause)
	})
	if err != nil {
		return err
	}
	ds.svc.log.Warn("substrate: trigger delivery parked",
		"trigger", tr.ID, "callable", tr.CallableID, "seq", ch.Seq, "record", ch.RecordID, "error", cause)
	return nil
}

// recordSkipAndAdvance writes the skipped run and moves the cursor in one
// transaction: a guard-false is a settled attempt, not lag.
func (ds *dataset) recordSkipAndAdvance(ctx context.Context, tr *trigger, ch substrate.Change, from int64, started time.Time, reason string) error {
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.advanceCursorTx(tr.ID, from, ch.Seq); err != nil {
			return err
		}
		if err := t.settleDelivery(tr.ID); err != nil {
			return err
		}
		if err := t.putRun(runRecord{
			trigger: tr.ID, callable: tr.callablePath(), mode: runner.ModeRecord,
			seq: ch.Seq, recordID: ch.RecordID, status: runStatusSkipped,
			attempt: 1, startedAt: started, errMsg: reason,
		}); err != nil {
			return err
		}
		return t.pruneRuns(tr.ID)
	})
}

// skipClaimed settles a declined attempt whose body's agent had already
// claimed the delivery (agentThreads.bind): a guarded write that yielded its
// version race after the thread opened. The claim moved the cursor or fire
// state, so the skip retires the claim and writes the skipped run in one
// transaction, and nothing is left in flight (decision 0093, record 0121).
func (ds *dataset) skipClaimed(ctx context.Context, s *settlement, run runRecord) error {
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := s.retireClaim(t); err != nil {
			return err
		}
		if err := t.putRun(run); err != nil {
			return err
		}
		return t.pruneRuns(s.trigger)
	})
}

// --- schedule-sourced delivery ----------------------------------------------------

// processScheduleTrigger fires the occurrences due since the last one the
// trigger acknowledged, oldest first and at most limit of them, stopping
// early once the deadline is spent. It returns the deliveries that applied
// effects and the occurrences it dispatched: each fire advances the
// fire state to its occurrence, so a pass that stops early (an error, a lost
// swap, the budget) leaves the rest due for the next. Parked fires of the
// trigger do not hold a due one back: every occurrence is dispatched, and
// one that settles retires the parks at or before it (retireSupersededFires).
func (ds *dataset) processScheduleTrigger(ctx context.Context, lt loadedTrigger, deadline passDeadline, limit int) (ran, fired int, err error) {
	lastFire, err := ds.ensureScheduleState(ctx, lt.ID)
	if err != nil {
		return 0, 0, err
	}
	due, err := lt.Schedule.dueFires(lt.CreatedAt, lastFire, nowUTC(), limit)
	if err != nil {
		return 0, 0, err
	}
	for i, at := range due {
		if i > 0 && deadline.spent() {
			return ran, fired, nil
		}
		// An occurrence waits for the lane's next look, for a free lane
		// worker and for a delivery slot (scheduleLane); the log line is
		// where an operator meets that wait before the fire settles.
		if late := nowUTC().Sub(at); late > scheduleLateAfter {
			ds.svc.log.Info("substrate: schedule fire dispatched late",
				"trigger", lt.ID, "fire", fireID(at), "late", late.Round(time.Second))
		}
		// done runs on a panic too: the lane contains a fire's panic, and a
		// count left raised would show the occurrence in flight until the
		// process restarts.
		n, settled, err := func() (int, bool, error) {
			done := ds.startFire(lt.ID, fireID(at))
			defer done()
			return ds.deliverFire(ctx, lt.trigger, runner.ModeSchedule, fireID(at), at, &lastFire, nil, nil)
		}()
		ran += n
		fired++
		if errors.Is(err, errCallableGone) {
			ds.svc.log.Warn("substrate: trigger names a callable that no longer resolves — it is skipped, its fire state stands still",
				"trigger", lt.ID, "callable", lt.CallableID)
			return ran, fired, nil
		}
		if err != nil {
			return ran, fired, err
		}
		if settled {
			ds.markDelivered(lt.ID)
		}
		lastFire = at
	}
	return ran, fired, nil
}

// scheduleLateAfter is how far past its occurrence a schedule fire may begin
// before processScheduleTrigger logs it as late.
const scheduleLateAfter = time.Minute

// scheduleOwedCap bounds the occurrences a status read counts for one
// schedule trigger, so a trigger far behind costs a status read no more than
// a few passes' worth.
const scheduleOwedCap = 100

// fireKey names one schedule occurrence of one trigger in dataset.firing.
func fireKey(triggerID, fid string) string { return triggerID + "\x00" + fid }

// startFire marks one schedule occurrence as being delivered by this
// process until the returned func runs. A count, not a set: a wake by hand
// and the dispatcher may both reach one occurrence, and the fire state's
// compare-and-swap lets one of them through.
func (ds *dataset) startFire(triggerID, fid string) func() {
	key := fireKey(triggerID, fid)
	ds.firingMu.Lock()
	if ds.firing == nil {
		ds.firing = map[string]int{}
	}
	ds.firing[key]++
	ds.firingMu.Unlock()
	return func() {
		ds.firingMu.Lock()
		if ds.firing[key]--; ds.firing[key] <= 0 {
			delete(ds.firing, key)
		}
		ds.firingMu.Unlock()
	}
}

// isFiring reports whether this process is delivering the occurrence now.
func (ds *dataset) isFiring(triggerID, fid string) bool {
	ds.firingMu.Lock()
	defer ds.firingMu.Unlock()
	return ds.firing[fireKey(triggerID, fid)] > 0
}

// triggerActivity is what this process saw of one trigger's dispatch
// (dataset.activity). None of it is stored: TriggerStatuses reads it to say
// whether the dispatcher reaches a trigger and what it runs now.
type triggerActivity struct {
	// lastPassAt is when a dispatcher pass last reached the trigger: its
	// turn in the record lane, or a look of the schedule lane.
	lastPassAt time.Time
	// lastDeliveredAt is when a delivery of it last settled (ran, skipped
	// or parked past), a wake's and a hand retry's included; a fire that
	// lost its fire state settled nothing (deliverFire).
	lastDeliveredAt time.Time
	// delivering is the record deliveries of it running now, a pass's and a
	// wake's alike. A function delivery writes no row until it settles, so
	// without it a status read shows a delivery in hand as nothing in
	// flight.
	delivering map[*recordDelivery]struct{}
}

// recordDelivery is one record delivery this process runs now
// (triggerActivity.delivering).
type recordDelivery struct {
	// claim, under dataset.activityMu, is the failure id the delivery's own
	// agent claim holds in runningClaims, 0 while it holds none. A status
	// read leaves that one row out of its running count, since it counts
	// the delivery already; any other running row of the trigger, a hand's
	// retry of the same change included, still counts.
	claim int64
}

// activityLocked is the trigger's entry, made on first use. The caller
// holds ds.activityMu.
func (ds *dataset) activityLocked(triggerID string) *triggerActivity {
	if ds.activity == nil {
		ds.activity = map[string]*triggerActivity{}
	}
	a := ds.activity[triggerID]
	if a == nil {
		a = &triggerActivity{}
		ds.activity[triggerID] = a
	}
	return a
}

// markPassed records that a dispatcher pass reached the trigger now.
func (ds *dataset) markPassed(triggerID string) {
	ds.activityMu.Lock()
	defer ds.activityMu.Unlock()
	ds.activityLocked(triggerID).lastPassAt = nowUTC()
}

// markDelivered records that a delivery of the trigger settled now.
func (ds *dataset) markDelivered(triggerID string) {
	ds.activityMu.Lock()
	defer ds.activityMu.Unlock()
	ds.activityLocked(triggerID).lastDeliveredAt = nowUTC()
}

// startRecordDelivery counts one record delivery of the trigger as running
// until the returned func runs.
func (ds *dataset) startRecordDelivery(triggerID string) (*recordDelivery, func()) {
	d := &recordDelivery{}
	ds.activityMu.Lock()
	a := ds.activityLocked(triggerID)
	if a.delivering == nil {
		a.delivering = map[*recordDelivery]struct{}{}
	}
	a.delivering[d] = struct{}{}
	ds.activityMu.Unlock()
	return d, func() {
		ds.activityMu.Lock()
		defer ds.activityMu.Unlock()
		delete(a.delivering, d)
	}
}

// setDeliveryClaim records the failure id a record delivery's own claim
// holds, 0 for none. A nil delivery is a settlement no record delivery
// tracks.
func (ds *dataset) setDeliveryClaim(d *recordDelivery, id int64) {
	if d == nil {
		return
	}
	ds.activityMu.Lock()
	defer ds.activityMu.Unlock()
	d.claim = id
}

// activityOf copies the trigger's activity for a status read: the two
// moments, zero where nothing happened yet, how many record deliveries of
// it run now, and the failure ids their own claims hold.
func (ds *dataset) activityOf(triggerID string) (lastPass, lastDelivered time.Time, delivering int, claims []int64) {
	ds.activityMu.Lock()
	defer ds.activityMu.Unlock()
	a := ds.activity[triggerID]
	if a == nil {
		return time.Time{}, time.Time{}, 0, nil
	}
	for d := range a.delivering {
		if d.claim != 0 {
			claims = append(claims, d.claim)
		}
	}
	return a.lastPassAt, a.lastDeliveredAt, len(a.delivering), claims
}

// pruneActivity drops the entries of triggers the pass no longer loads and
// nothing delivers, so a repository whose triggers come and go does not
// grow the map.
func (ds *dataset) pruneActivity(triggers []loadedTrigger) {
	live := make(map[string]bool, len(triggers))
	for _, lt := range triggers {
		live[lt.ID] = true
	}
	ds.activityMu.Lock()
	defer ds.activityMu.Unlock()
	for id, a := range ds.activity {
		if !live[id] && len(a.delivering) == 0 {
			delete(ds.activity, id)
		}
	}
}

// logYield records, at debug, a record trigger's pass ending on a lost
// cursor swap: a replay or another dispatcher moved the cursor from under
// it. The pass rolls back and moves on without an error or a park, so this
// line is the only trace of the stop. swap says which swap lost: the
// delivery's acknowledgement, or the scan past rows that matched nothing.
func (ds *dataset) logYield(triggerID, swap string, seq int64) {
	ds.svc.log.Debug("substrate: record trigger yielded: its cursor moved under this pass",
		"repository", ds.Repository().ID, "trigger", triggerID, "swap", swap, "seq", seq)
}

// logFireYield is logYield for a fire whose fire state moved under it: the
// fire rolls back and settles nothing, without an error.
func (ds *dataset) logFireYield(triggerID, swap, fid string) {
	ds.svc.log.Debug("substrate: trigger fire yielded: its fire state moved under this delivery",
		"repository", ds.Repository().ID, "trigger", triggerID, "swap", swap, "fire", fid)
}

// owedFires counts a schedule trigger's occurrences that are due and not yet
// settled or parked past: the ones this process is delivering now (inFlight)
// and the rest (pending), which wait for a dispatcher pass to reach them.
// At most scheduleOwedCap are counted.
func (ds *dataset) owedFires(lt loadedTrigger, lastFire time.Time) (pending, inFlight int64, err error) {
	due, err := lt.Schedule.dueFires(lt.CreatedAt, lastFire, nowUTC(), scheduleOwedCap)
	if err != nil {
		return 0, 0, err
	}
	for _, at := range due {
		if ds.isFiring(lt.ID, fireID(at)) {
			inFlight++
		} else {
			pending++
		}
	}
	return pending, inFlight, nil
}

// fireSettlement settles a dispatched fire: for a schedule occurrence the
// fire-state advance from the occurrence the pass read, then the OK run
// record. A webhook fire has no fire state, so on the function path its run
// record is the whole settlement and no delivery entry is appended.
func (ds *dataset) fireSettlement(tr *trigger, mode, fid string, at time.Time, lastFire *time.Time, started time.Time) *settlement {
	s := &settlement{ds: ds, trigger: tr.ID, fireID: fid}
	s.record = func(t *txn, res deliverResult) error {
		return t.putSystemRun(runRecord{
			trigger: tr.ID, callable: tr.callablePath(), mode: mode,
			fireID: fid, status: runStatusOK, attempt: s.attempt,
			startedAt: started, effects: res.effects, pages: res.pages,
		}, true)
	}
	if lastFire != nil {
		s.acknowledge = func(t *txn) error { return t.advanceScheduleTx(tr.ID, *lastFire, at) }
		s.supersedes = at.UTC()
	}
	return s
}

// deliverFire runs one schedule occurrence or webhook fire through the
// delivery path: same retries, same park, mode schedule/webhook, no
// changelog row underneath. lastFire non-nil means the schedule fire state
// advances compare-and-swap in the same transaction as the effects — a
// concurrent dispatcher cannot double-fire an occurrence, which is what
// makes the stable fire id idempotent. envelope is the delivery's envelope
// when the caller built one (a public webhook delivery carries its request);
// nil means the bare fire envelope. pending is the ledger row an admitted
// webhook request stands in (webhooks.go admitWebhook): the settlement
// retires it with the effects, and a park rewrites it under its own id
// rather than reserving another; nil for every other fire. settled is true
// when this call settled the fire (ran, skipped or parked) and false when it
// lost the fire state to another dispatcher or failed, so a caller stamps a
// delivery only for the first.
func (ds *dataset) deliverFire(ctx context.Context, tr *trigger, mode, fid string, at time.Time, lastFire *time.Time, envelope map[string]any, pending *foldFailure) (applied int, settled bool, err error) {
	started := nowUTC()
	// As in deliver: the live body, resolved now rather than at the pass's
	// trigger load. Once, outside the attempt loop — a chain of retries runs
	// one body.
	if err := ds.refreshCallable(tr); err != nil {
		return 0, false, err
	}
	var lastErr error
	attempts := triggerAttempts
	settle := ds.fireSettlement(tr, mode, fid, at, lastFire, started)
	defer settle.release()
	if pending != nil {
		settle.retire, settle.pending = int64(pending.ID), pending
		// Held before anything runs: a resume racing the door's own spawn,
		// or a hand's retry, loses the swap and starts no body.
		if err := settle.acquire(settle.retire); err != nil {
			return 0, false, err
		}
	}
	// As in deliverWithRetry: a function body that ran an agent parks on
	// its first failure after the thread opened (agentThreads).
	threads := &agentThreads{settle: settle}
	fctx := withCallOrigin(ctx, callOrigin{threads: threads})
	for attempt := range triggerAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, false, ctx.Err()
			case <-time.After(triggerRetryBackoff[min(attempt-1, len(triggerRetryBackoff)-1)]):
			}
		}
		settle.attempt = attempt + 1
		var applied int
		var err, cause error
		if tr.Agent != nil {
			applied, err = ds.agentFire(ctx, tr, mode, fid, at, envelope, settle)
		} else {
			applied, err = ds.functionFire(fctx, tr, mode, fid, at, envelope, settle)
			cause = err
			err = agentRetryGate(threads, err)
		}
		if err == nil {
			return applied, true, nil
		}
		if declinedDelivery(err) && threads.opened() != "" {
			// As in deliverWithRetry: the agent's claim already moved the
			// fire state (or rewrote the admitted request as in flight), so
			// the skip retires the claim.
			err := ds.skipClaimed(parkContext(ctx, threads), settle, runRecord{
				trigger: tr.ID, callable: tr.callablePath(), mode: mode, fireID: fid,
				status: runStatusSkipped, attempt: settle.attempt, startedAt: started,
				errMsg: fmt.Sprintf("%v (agent thread %s)", cause, threads.opened()),
			})
			return 0, err == nil, err
		}
		if declinedDelivery(err) {
			// Another dispatch holds this fire, or this one lost a guarded
			// write's version race it declared it could lose: move the fire
			// state past it and say so. Either way nothing committed.
			skip := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				if lastFire != nil {
					if err := t.advanceScheduleTx(tr.ID, *lastFire, at); err != nil {
						return err
					}
				}
				if err := t.settleDelivery(tr.ID); err != nil {
					return err
				}
				return t.putRun(runRecord{
					trigger: tr.ID, callable: tr.callablePath(), mode: mode, fireID: fid,
					status: runStatusSkipped, attempt: 1, startedAt: started, errMsg: err.Error(),
				})
			})
			if errors.Is(skip, errCursorMoved) {
				ds.logFireYield(tr.ID, "skip", fid)
				return 0, false, nil
			}
			return 0, skip == nil, skip
		}
		if errors.Is(err, errCursorMoved) {
			// Another dispatcher fired this occurrence; ours is a duplicate
			// and rolled back whole.
			ds.logFireYield(tr.ID, "delivery", fid)
			return 0, false, nil
		}
		if pending != nil && errors.Is(err, errFailureRetired) {
			// The admitted request's row is gone: a hand retired it, so its
			// delivery landed, and there is nothing to run again or park.
			return 0, false, err
		}
		if ctx.Err() != nil && !errors.Is(err, errAgentThreadOpened) {
			return 0, false, ctx.Err()
		}
		lastErr = err
		if runner.Deterministic(err) || errors.Is(err, errPagedParked) || errors.Is(err, errTriggerArguments) || errors.Is(err, errAgentThreadOpened) {
			attempts = attempt + 1
			break
		}
	}
	// Park-and-advance, fire-shaped: the failure row keeps the fire id, and
	// the schedule state still moves — a poisoned occurrence never wedges the
	// ones behind it. A built envelope parks with the row in its parked form
	// (webhooks.go parkedEnvelope), so a retry re-delivers the request that
	// arrived rather than a bare fire, narrowed by the trigger's declared
	// header set as it stands now: the same set the admission used unless
	// the declaration changed meanwhile, and then the narrower of the two,
	// because a row already narrowed cannot grow a header back. A pending row
	// already holds that form, and the delivered envelope is not read back
	// into one: the fire read
	// the body into it (fireEnvelope), and the bytes must not enter the
	// changelog.
	// A body whose agent opened a thread parks even when the dispatcher is
	// stopping (deliverWithRetry).
	ctx = parkContext(ctx, threads)
	var payload json.RawMessage
	if pending != nil {
		payload = pending.Payload
	} else {
		var err error
		if payload, err = ds.parkedEnvelope(ctx, envelope, tr.WebhookHeaders); err != nil {
			return 0, false, err
		}
	}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		// An agent fire already claimed the occurrence (settlement.claim):
		// the park rewrites the claim and moves the fire state no further.
		id, claimed, err := t.claimedFailure(tr.ID, 0, fid)
		if err != nil {
			return err
		}
		if !claimed && pending != nil {
			// The admitted request's own row, held FOR UPDATE so a row a
			// hand retired meanwhile is not brought back: the park rewrites
			// it, and no fire state moves.
			if err := t.lockFailure(tr.ID, int64(pending.ID)); err != nil {
				return err
			}
			id, claimed = int64(pending.ID), true
		}
		if !claimed {
			if id, err = t.reserveSeq(); err != nil {
				return err
			}
		}
		if err := t.parkTx(tr.ID, foldFailure{
			ID: foldInt(id), FireID: fid, Attempts: foldInt(attempts),
			LastError: lastErr.Error(), ParkedAt: t.now, Payload: payload,
		}); err != nil {
			return err
		}
		if claimed {
			if err := t.settleDelivery(tr.ID); err != nil {
				return err
			}
		} else {
			if lastFire != nil {
				if err := t.advanceScheduleTx(tr.ID, *lastFire, at); err != nil {
					return err
				}
			}
			if err := t.appendDeliveryAt(tr.ID, id); err != nil {
				return err
			}
		}
		return t.putRun(runRecord{
			trigger: tr.ID, callable: tr.callablePath(), mode: mode,
			fireID: fid, status: runStatusParked, attempt: attempts,
			startedAt: started, errMsg: lastErr.Error(),
		})
	})
	if err != nil {
		if errors.Is(err, errCursorMoved) {
			ds.logFireYield(tr.ID, "park", fid)
			return 0, false, nil
		}
		return 0, false, err
	}
	ds.svc.log.Warn("substrate: trigger fire parked",
		"trigger", tr.ID, "callable", tr.CallableID, "fire", fid, "error", lastErr)
	return 0, true, nil
}

// functionFire runs one schedule/webhook fire through the runner: effects
// and the settlement in one transaction, the effectively-once half. A paged
// body (a scheduled backfill) drains off the causal chain, the settlement
// landing only when the drain finishes.
func (ds *dataset) functionFire(ctx context.Context, tr *trigger, mode, fid string, at time.Time, envelope map[string]any, settle *settlement) (int, error) {
	// The lifecycle fence's shared side, admission through effect + fire-state
	// commit (bundles.go).
	ctx, release, err := ds.admitCallable(ctx, tr.Callable.Package, tr.Callable.Identity())
	if err != nil {
		return 0, err
	}
	defer release()
	// Load the committed resume cursor before invoking: a fire
	// redelivery of a paged chain continues from the last committed page, not
	// from page zero. functionFire runs once per deliverFire attempt, so this
	// reloads on every automatic retry.
	key := ds.fireChainKey(tr.ID, fid)
	resume, err := ds.loadPagedProgress(ctx, key)
	if err != nil {
		return 0, err
	}
	env, err := ds.fireEnvelope(ctx, envelope, fid, at)
	if err != nil {
		return 0, err
	}
	in := runner.Input{
		Mode:           mode,
		Envelope:       env,
		IdempotencyKey: key,
		Resume:         resume.cursor,
	}
	// The trigger's arguments, held to the body resolved for THIS fire: an
	// apply since the trigger was written may have changed what the function
	// takes. A retry of a parked occurrence reads the trigger as it stands,
	// so fixing the record is what lets the retry deliver.
	if tr.Arguments != nil {
		if err := checkTriggerArguments(tr, tr.Callable); err != nil {
			return 0, fmt.Errorf("%w: trigger %s: %w", errTriggerArguments, tr.ID, err)
		}
		in.Args = tr.Arguments
	}
	effects, _, more, err := ds.runCallableRaw(ctx, tr.Callable, in)
	if err != nil {
		return 0, err
	}
	actor := substrate.Actor(tr.Callable.Actor())
	if more != nil || resume.exists {
		owner := pagedOwner{triggerID: tr.ID, kind: pagedKindFire, identity: fid}
		summary, _, derr := ds.pagedDrain(ctx, tr.Callable, in, actor, 0, tr.Callable.Caps.Emit,
			owner, resume, pagedPage{effects: effects, more: more}, pagedSettlement(false, settle))
		if derr != nil {
			return 0, derr
		}
		return settledResult(false, summary, 0).ran, nil
	}
	if len(effects) == 0 && settle == nil {
		return 0, nil
	}
	effects, err = ds.holdEffects(ctx, tr.Callable, effects, key)
	if err != nil {
		return 0, err
	}
	var res deliverResult
	err = ds.inTx(ctx, actor, false, func(t *txn) error {
		if err := t.applyEffects(tr.Callable.Caps.Emit, effects); err != nil {
			return err
		}
		// After the apply, which is what settles each held effect.
		res = settledResult(false, effectsSummary(effects), 1)
		return settle.settle(t, res)
	})
	if err != nil {
		return 0, err
	}
	ds.judgeHeld(effects)
	return res.ran, nil
}

// ensureScheduleState reads a schedule trigger's fire state, creating it AT
// NOW on first sight and recording that in the ledger: the first fire is the
// next occurrence after the trigger is first seen, never a backfill, and a
// restore comes back to the occurrence last acknowledged, not to now.
// Creation normally happens in the trigger row's own transaction
// (initTriggerBookkeeping); this is the dispatch-time backstop, and a
// read-only process, which appends nothing, answers now without recording.
func (ds *dataset) ensureScheduleState(ctx context.Context, id string) (time.Time, error) {
	var at time.Time
	err := ds.db.QueryRowContext(ctx,
		`SELECT fired_at FROM trigger_schedule WHERE trigger_id = $1`, id).Scan(&at)
	if err == nil {
		return at.UTC(), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	if ds.svc.readOnly {
		return nowUTC(), nil
	}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		// Under the changelog lock, look again: a concurrent pass may have
		// initialized it since the read above.
		if err := t.lockChangelog(); err != nil {
			return err
		}
		err := t.row(`SELECT fired_at FROM trigger_schedule WHERE trigger_id = $1`, id).Scan(&at)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		at = t.now
		if err := t.setScheduleTx(id, at); err != nil {
			return err
		}
		return t.settleDelivery(id)
	})
	return at.UTC(), err
}

// --- run rows -----------------------------------------------------------------

const (
	runStatusOK      = "ok"
	runStatusSkipped = "skipped"
	runStatusParked  = "parked"
	// runStatusFailed is a call run's alone: the body ran and failed, so
	// nothing applied. A delivery that fails is retried and parks instead.
	runStatusFailed = "failed"
)

// runRecord is one settled delivery attempt, about to become a run record.
type runRecord struct {
	trigger string // empty on a call run, which no trigger fired
	// callable is the callable's RECORD path, not its id: the run stores it
	// twice (putRun says why) and one field is what keeps the two agreeing.
	callable  string
	mode      string
	seq       int64
	fireID    string
	recordID  string
	status    string
	attempt   int
	startedAt time.Time
	errMsg    string
	effects   map[string]int
	pages     int // committed pages for a paged (backfill) delivery; >1 only when the body paged
	// A call run's audit (callrun.go): the door the call came through, the
	// token behind it, and what it returned.
	caller    substrate.Actor
	principal string
	output    *callOutput
}

// putRun writes one run record inside the caller's transaction.
func (t *txn) putRun(r runRecord) error {
	id, err := newID()
	if err != nil {
		return err
	}
	// The callable lands twice from the one path: `callableRef`, the reference
	// a `referencing` read follows to every run of one callable, and
	// `callable`, the deprecated bare id every reader has always seen. The
	// reference declares no `mustExist` on purpose — a callable uninstalled
	// between the fire and the settle must not fail this audit row's write.
	_, callableID, ok := vocabulary.SplitRecordPath(r.callable)
	if !ok {
		return fmt.Errorf("run callable %q is not a record path", r.callable)
	}
	props := map[string]any{
		"callable":    callableID,
		"callableRef": r.callable,
		"mode":        r.mode,
		"status":      r.status,
		"attempt":     r.attempt,
		"startedAt":   r.startedAt.Format(time.RFC3339Nano),
		"finishedAt":  t.now.Format(time.RFC3339Nano),
	}
	if r.trigger != "" {
		props["trigger"] = vocabulary.RecordPath(typeTrigger, r.trigger)
	}
	if r.caller != "" {
		props["caller"] = string(r.caller)
	}
	if r.principal != "" {
		props["principal"] = r.principal
	}
	if r.output != nil {
		props["outputBytes"] = r.output.bytes
		if r.output.kept {
			props["output"] = r.output.value
		}
	}
	if r.seq > 0 {
		props["seq"] = r.seq
	}
	if r.fireID != "" {
		props["fireId"] = r.fireID
	}
	if r.recordID != "" {
		props["record"] = r.recordID
	}
	// A paged (backfill) delivery drained more than one page; the durable
	// per-chain progress lives in paged_cursors, this is its ledger echo.
	if r.pages > 1 {
		props["pages"] = r.pages
	}
	if r.errMsg != "" {
		props["reason"] = r.errMsg
	}
	if len(r.effects) > 0 {
		summary := make(map[string]any, len(r.effects))
		for k, v := range r.effects {
			summary[k] = v
		}
		props["effects"] = summary
	}
	_, err = t.put(substrate.PutInput{Kind: typeTriggerRun, ID: id, Properties: props})
	return err
}

// putSystemRun writes a run record from inside another hand's transaction
// (a callable's effects, a retry's), as the engine's own write under the
// system actor, and prunes the trigger's ledger when asked. The delivery
// transaction carries it beside the effects it describes, so a crash cannot
// commit effects with no completion recorded.
func (t *txn) putSystemRun(r runRecord, prune bool) error {
	prevInternal := t.internal
	t.internal = true
	defer func() { t.internal = prevInternal }()
	return t.asActor(substrate.ActorSystem, func() error {
		if err := t.putRun(r); err != nil {
			return err
		}
		if !prune {
			return nil
		}
		return t.pruneRuns(r.trigger)
	})
}

// pruneRuns enforces the retention: the newest runRetention non-parked runs
// per trigger stay, older ones tombstone. Parked runs are exempt — failures
// are kept.
func (t *txn) pruneRuns(triggerID string) error {
	rows, err := t.query(`
		SELECT id FROM records
		WHERE kind = $1 AND deleted_at IS NULL
		  AND `+referencePathSQL("props", "trigger")+` = $2 AND props->>'status' <> $3
		ORDER BY created_at DESC, id DESC OFFSET $4`,
		typeTriggerRun, vocabulary.RecordPath(typeTrigger, triggerID), runStatusParked, runRetention)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		stale = append(stale, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, id := range stale {
		if _, err := t.softDelete(eref{Kind: typeTriggerRun, ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// effectsSummary counts applied effects by action, and the ones the policy
// door held as `gate`, the key the agent loop's tally uses.
func effectsSummary(effects []effect) map[string]int {
	if len(effects) == 0 {
		return nil
	}
	out := map[string]int{}
	for _, ef := range effects {
		out[summaryAction(ef)]++
	}
	return out
}

// summaryAction reads a held effect after its transaction applied it: a
// create-only put the door held over a live target wrote no request, so it
// counts as the put it would have been.
func summaryAction(ef effect) string {
	if ef.held() {
		return "gate"
	}
	return ef.Action
}

// mergedSummary is a running summary plus one page's effects, as a new map:
// the paged drain's cross-page effect tally, computed in the page's
// transaction once its effects applied, so the final page's settlement
// records the whole chain.
func mergedSummary(summary map[string]int, effects []effect) map[string]int {
	out := make(map[string]int, len(summary)+1)
	for k, v := range summary {
		out[k] = v
	}
	for _, ef := range effects {
		out[summaryAction(ef)]++
	}
	return out
}

// --- paged-checkpoint drain --------------------------------------
//
// A delivery body may return a PAGE — its effects plus a `more.cursor` opaque
// resume token — meaning "commit this and re-invoke me". The host drains the
// pages OFF THE CAUSAL CHAIN: each page's effects commit together with the
// resume cursor (paged_cursors), the FINAL page (no `more`) clears the cursor
// and moves the delivery's own cursor/fire state, and every re-invoke carries
// the SAME causalDepth — self-continuation must not spend the causal-depth-16
// budget, which is the whole point of a paged invocation over a self-emit. A
// page failure or the max-pages cap leaves the last committed cursor intact,
// so the trigger's retry handle resumes from there rather than restarting the
// backfill from zero. Non-paged deliveries never engage any of this: a body
// that returns no `more` takes the ordinary single-transaction path and never
// touches paged_cursors.

// pagedPage is one drained page: the decoded effects and the continuation
// (nil once the body is drained).
type pagedPage struct {
	effects []effect
	more    *runner.Continuation
}

// pagedOwner is the lifecycle identity a paged_cursors row carries: the
// trigger that owns the chain, and enough to match its parked failure.
type pagedOwner struct {
	triggerID string
	kind      string // pagedKindRecord | pagedKindFire
	identity  string // the seq (decimal) or the fire id
}

// pagedProgress is a chain's persisted state: the resume cursor and version
// (the CAS fence) plus the cumulative budget counters. `exists` is false for
// a fresh chain, a drain that has committed nothing.
type pagedProgress struct {
	cursor    any
	version   int64
	pages     int64
	effects   int64
	bytes     int64
	startedAt time.Time
	exists    bool
}

// pagedDrain commits `first` and every subsequent page a paged body returns.
// baseInput carries the invocation shape (mode, envelope, causalDepth,
// idempotency key) — its Resume is rewritten per page and its CausalDepth is
// held CONSTANT across the whole chain. `owner` stamps the row's lifecycle
// identity; `resume` seeds the CAS fence and the cumulative budget from the
// persisted row (zero value for a fresh chain). `commit` is the delivery's
// settlement, run inside the FINAL page's transaction only with the whole
// chain's summary and this pass's page count (nil for a manual run). It
// returns the merged effect summary and the number of pages committed THIS
// pass. A returned error parked mid-chain: errCursorMoved yields (the pass no
// longer owns the chain), errPagedParked parks with the last committed cursor
// intact, and a fresh pre-commit error rides the caller's ordinary retry.
func (ds *dataset) pagedDrain(ctx context.Context, fn *vocabulary.Function, baseInput runner.Input, actor substrate.Actor, causedBy int64, emit []string, owner pagedOwner, resume pagedProgress, first pagedPage, commit func(t *txn, summary map[string]int, pages int) error) (map[string]int, int, error) {
	key := baseInput.IdempotencyKey
	summary := map[string]int{}
	page := first

	// The CAS fence + cumulative budget, seeded from the persisted row so both
	// span the WHOLE chain across retries. `haveRow`/`version` track ownership:
	// the first middle page of a fresh chain CLAIMS an absent row, every later
	// page (and the final delete) swaps only from the exact version it last saw.
	haveRow := resume.exists
	version := resume.version
	cumPages := resume.pages
	cumEffects := resume.effects
	cumBytes := resume.bytes
	// A fresh chain, and one loadPagedProgress started over, begins its
	// deadline now.
	startedAt := resume.startedAt
	if startedAt.IsZero() {
		startedAt = nowUTC()
	}
	deadline := startedAt.Add(drainDeadline)
	committedAny := resume.exists // a prior pass may already have committed pages

	pages := 0
	for {
		done := page.more == nil
		var cursor any
		if !done {
			cursor = page.more.Cursor
		}
		nextPages := cumPages + 1
		// The door holds this page's effects under a key naming the page, so
		// a page re-run after a park holds the same effect as the same
		// request.
		effects, err := ds.holdEffects(ctx, fn, page.effects, fmt.Sprintf("%s/page/%d", key, nextPages))
		if err != nil {
			if committedAny {
				return summary, pages, fmt.Errorf("%w: %w", errPagedParked, err)
			}
			return summary, pages, err
		}
		nextEffects := cumEffects + int64(len(effects))
		nextBytes := cumBytes + effectsBytes(effects)

		// A MIDDLE page extends the chain, so it must fit the cumulative budget
		// AND the deadline first. Exhaustion is a DETERMINISTIC
		// immediate park — never a retryable error that repeats the drain — so
		// it wraps errPagedParked regardless of whether this pass committed
		// anything. The FINAL page always commits: it ends the drain, and
		// parking on it would strand the chain forever.
		if !done {
			if reason := drainOverBudget(fn, nextPages, nextEffects, nextBytes, deadline); reason != nil {
				return summary, pages, fmt.Errorf("%w: %w", errPagedParked, reason)
			}
		}

		var merged map[string]int
		err = ds.inTx(ctx, actor, false, func(t *txn) error {
			t.causedBy = causedBy
			if err := t.applyEffects(emit, effects); err != nil {
				return err
			}
			// After the apply, which is what settles each held effect.
			merged = mergedSummary(summary, effects)
			if done {
				// Drained: drop the resume cursor — under the SAME version
				// CAS, so a chain another dispatcher advanced is not cleared
				// out from under it, and settle the delivery. Effects and
				// completion commit together; the settlement's delivery entry
				// carries the unpage, or settleDelivery below does.
				if haveRow {
					if err := t.clearPagedCursorCAS(key, version); err != nil {
						return err
					}
				}
				if commit != nil {
					if err := commit(t, merged, pages+1); err != nil {
						return err
					}
				}
				return t.settleDelivery(owner.triggerID)
			}
			// A middle page advances only the RESUME cursor, never the delivery
			// cursor. The first page of a fresh chain claims an absent row; every
			// later page swaps from the version it last saw. A missed swap is
			// errCursorMoved — two dispatchers draining one chain cannot both
			// commit. The row's motion rides this page's delivery entry.
			if !haveRow {
				if err := t.claimPagedCursor(key, owner, cursor, nextPages, nextEffects, nextBytes, startedAt); err != nil {
					return err
				}
			} else if err := t.advancePagedCursor(key, version, cursor, nextPages, nextEffects, nextBytes, startedAt); err != nil {
				return err
			}
			return t.settleDelivery(owner.triggerID)
		})
		if err != nil {
			if errors.Is(err, errCursorMoved) {
				// The pass lost the chain: roll back whole and yield. NOT a park.
				return summary, pages, err
			}
			if committedAny {
				// A page error after durable progress parks with the cursor
				// intact rather than replaying the chain. A yielded guarded
				// write parks here too: once pages have committed, this pass
				// OWNS the chain, the duplicated work the claim exists to
				// prevent has already happened, and dropping the drain
				// silently would strand those pages. errPagedParked is what
				// declinedDelivery reads to tell the two apart.
				return summary, pages, fmt.Errorf("%w: %w", errPagedParked, err)
			}
			// A fresh chain that committed nothing can safely retry from zero
			// — or, for a yielded claim on the FIRST page, decline outright:
			// the drain never started, so there is nothing to resume.
			return summary, pages, err
		}
		ds.judgeHeld(effects)
		summary = merged
		pages++
		cumPages, cumEffects, cumBytes = nextPages, nextEffects, nextBytes
		committedAny = true
		if !done {
			haveRow = true
			version++ // claim seeds version 1; advance set version = version + 1
		}
		if done {
			return summary, pages, nil
		}
		// Re-invoke OFF THE CAUSAL CHAIN: same causalDepth, the cursor handed
		// straight back. No changelog write sits between pages, so the causal
		// depth of the body's effects never grows across the drain.
		in := baseInput
		in.Resume = cursor
		effs, _, more, err := ds.runCallableRaw(ctx, fn, in)
		if err != nil {
			// A re-invoke error always sits after ≥1 committed page: park with
			// the cursor intact.
			return summary, pages, fmt.Errorf("%w: %w", errPagedParked, err)
		}
		page = pagedPage{effects: effs, more: more}
	}
}

// effectsBytes approximates one page's effect payload size for the cumulative
// byte budget — the marshaled effect list, good enough to bound write traffic.
func effectsBytes(effects []effect) int64 {
	if len(effects) == 0 {
		return 0
	}
	raw, err := json.Marshal(effects)
	if err != nil {
		return 0
	}
	return int64(len(raw))
}

// drainOverBudget reports the first cumulative bound a middle page would breach
// — the page cap, the effect count, the effect bytes, or the wall-clock
// deadline — as a deterministic park reason, or nil when the page fits.
func drainOverBudget(fn *vocabulary.Function, pages, effects, bytes int64, deadline time.Time) error {
	switch {
	case pages > int64(maxPagesPerDrain):
		return fmt.Errorf("%w: %s reached the %d-page cap", errMaxPages, fn.Identity(), maxPagesPerDrain)
	case effects > maxDrainEffects:
		return fmt.Errorf("%w: %s emitted %d effects over the chain (cap %d)", errDrainBudget, fn.Identity(), effects, maxDrainEffects)
	case bytes > maxDrainBytes:
		return fmt.Errorf("%w: %s emitted %d effect bytes over the chain (cap %d)", errDrainBudget, fn.Identity(), bytes, maxDrainBytes)
	case nowUTC().After(deadline):
		return fmt.Errorf("%w: %s ran past the %s drain deadline", errDrainBudget, fn.Identity(), drainDeadline)
	}
	return nil
}

// loadPagedProgress reads a chain's persisted resume cursor, CAS version and
// cumulative budget counters; a zero value with exists=false when no row. Every
// delivery of an existing chain — retry, redispatch, replay — reads this before
// it invokes and feeds it back into the CAS fence and budget.
//
// A null cursor is a chain with no position, and it is STARTED OVER: the body
// runs from its first page, and the budget, which measures progress along a
// cursor, starts again with it, deadline included; only the version is kept,
// so the fence still holds. A replay writes a null cursor where the ledger
// named the cursor by hash and no kept bytes match it (decision 0141): a
// drain that stopped between pages without parking comes back that way from
// an import into an empty database. Without the fresh budget such a chain
// would park again at its first middle page, since its deadline is measured
// from a first page long past. A body that pages with no cursor at all is
// re-invoked from its first page every page anyway, so it is the same case,
// and each retry by hand gives it one more bounded drain.
func (ds *dataset) loadPagedProgress(ctx context.Context, chain string) (pagedProgress, error) {
	var (
		raw []byte
		p   pagedProgress
	)
	err := ds.db.QueryRowContext(ctx, `
		SELECT cursor, version, pages, effects, bytes, started_at
		FROM paged_cursors WHERE chain = $1`, chain).
		Scan(&raw, &p.version, &p.pages, &p.effects, &p.bytes, &p.startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return pagedProgress{}, nil
	}
	if err != nil {
		return pagedProgress{}, err
	}
	if err := json.Unmarshal(raw, &p.cursor); err != nil {
		return pagedProgress{}, err
	}
	if p.cursor == nil {
		return pagedProgress{version: p.version, exists: true}, nil
	}
	p.startedAt = p.startedAt.UTC()
	p.exists = true
	return p, nil
}

// orphanPagedSQL selects a paged row with no lifecycle owner, over the alias
// `pc` with the staleness horizon in $1: a row whose trigger no longer lives,
// or a stale row (untouched past the sweep grace, so not an in-flight drain)
// whose trigger keeps no matching parked failure to resume it.
const orphanPagedSQL = `(NOT EXISTS (
			SELECT 1 FROM records e
			WHERE e.kind = 'substrate.reamde.dev/core/trigger' AND e.id = pc.trigger_id AND e.deleted_at IS NULL)
		   OR (pc.updated_at < $1
		       AND NOT EXISTS (
			SELECT 1 FROM trigger_failures f
			WHERE f.trigger_id = pc.trigger_id
			  AND ((pc.kind = 'record' AND f.seq::text = pc.identity)
			    OR (pc.kind = 'fire' AND f.fire_id = pc.identity)))))`

// sweepPagedCursors collects paged rows with no lifecycle owner, one ledger
// entry per trigger they named: a row's removal is a fold effect like its
// claim, so a restore does not bring an orphan back. A finding-#1 race that
// leaves a row behind an advanced delivery cursor is caught by the same
// stale-and-unreferenced arm. Each row is checked again under its lock
// before it goes, so a drain that took the row back since the scan keeps it.
func (ds *dataset) sweepPagedCursors(ctx context.Context) error {
	horizon := nowUTC().Add(-pagedSweepGrace)
	rows, err := ds.db.QueryContext(ctx, `SELECT chain, trigger_id FROM paged_cursors pc WHERE `+orphanPagedSQL, horizon)
	if err != nil {
		return err
	}
	byTrigger := map[string][]string{}
	for rows.Next() {
		var chain, triggerID string
		if err := rows.Scan(&chain, &triggerID); err != nil {
			_ = rows.Close()
			return err
		}
		byTrigger[triggerID] = append(byTrigger[triggerID], chain)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, triggerID := range sortedKeys(byTrigger) {
		chains := byTrigger[triggerID]
		if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			for _, chain := range chains {
				var one int
				err := t.row(`SELECT 1 FROM paged_cursors pc WHERE chain = $2 AND `+orphanPagedSQL+` FOR UPDATE`, horizon, chain).Scan(&one)
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return err
				}
				if err := t.unpageTx(triggerID, chain); err != nil {
					return err
				}
			}
			return t.settleDelivery(triggerID)
		}); err != nil {
			return err
		}
	}
	return nil
}

// recordChainKey is the paged-cursor key for a record-change delivery, and its
// idempotency key: repository-qualified because per-repository changelog seqs collide.
func (ds *dataset) recordChainKey(triggerID string, seq int64) string {
	return fmt.Sprintf("%s/%s/%d", ds.Repository().ID, triggerID, seq)
}

// fireChainKey is the paged-cursor key for a schedule or webhook fire delivery.
func (ds *dataset) fireChainKey(triggerID, fireID string) string {
	return fmt.Sprintf("%s/%s/%s", ds.Repository().ID, triggerID, fireID)
}

// --- cursors ---------------------------------------------------------------

// ensureCursor reads a trigger's cursor, creating it AT HEAD on first sight
// and recording that in the ledger: a newly created trigger reacts to what
// happens next, history is an explicit replay, and a restore comes back to
// the position last acknowledged rather than to the restored head. The
// position is the delivery entry's own seq, so the entry never reads as
// pending. Creation normally happens in the trigger row's own transaction
// (initTriggerBookkeeping), so a write between creation and the first
// dispatch is never skipped; this is the dispatch-time backstop, and a
// read-only process, which appends nothing, answers the head without
// recording.
func (ds *dataset) ensureCursor(ctx context.Context, triggerID string) (int64, error) {
	var seq int64
	err := ds.db.QueryRowContext(ctx,
		`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, triggerID).Scan(&seq)
	if err == nil {
		return seq, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if ds.svc.readOnly {
		return tableChangelogHead(ctx, ds.db)
	}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		next, err := t.reserveSeq()
		if err != nil {
			return err
		}
		// Under the changelog lock, look again: a concurrent pass may have
		// initialized it since the read above.
		err = t.row(`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, triggerID).Scan(&seq)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		seq = next
		if err := t.setCursorTx(triggerID, next); err != nil {
			return err
		}
		return t.appendDeliveryAt(triggerID, next)
	})
	return seq, err
}

// ensureTriggerCursors initializes every live trigger's bookkeeping at the
// current head. Runs at repository-open — the backstop for rows that predate the
// in-transaction initialization. A read-only process appends nothing, so it
// leaves the backstop to the server.
func (ds *dataset) ensureTriggerCursors(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	triggers, err := ds.loadTriggers(ctx)
	if err != nil {
		return err
	}
	for _, lt := range triggers {
		if lt.Err != nil {
			continue
		}
		if lt.Record != nil {
			if _, err := ds.ensureCursor(ctx, lt.ID); err != nil {
				return err
			}
		}
		if lt.Schedule != nil {
			if _, err := ds.ensureScheduleState(ctx, lt.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// advanceCursor moves the SCAN position: the cursor past a batch tail whose
// rows matched nothing, compare-and-swap on the seq the pass read so a
// replay's reset (the deliberate rewind) or a concurrent dispatcher's advance
// is never clobbered by an in-flight pass. It is the one cursor motion
// outside the ledger (delivery.go): it acknowledges no delivery, and a
// restore that loses it re-reads rows that deliver nothing. The
// acknowledging motion is advanceCursorTx, inside the effects transaction.
//
// It is also fenced on the trigger record's version the pass loaded: an edit
// of the trigger pins the cursor into the ledger (delivery.go
// pinTriggerCursor) and bumps the record, so a pass that scanned rows under
// the OLD source and lands its advance after the pin would leave the table
// past what the ledger says, and a restore would deliver those rows under the
// new source. With the version in the swap, that pass ends with
// errCursorMoved and the next one re-scans under the new definition.
func (ds *dataset) advanceCursor(ctx context.Context, tr *trigger, from, to int64) error {
	res, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_cursors SET seq = $3, updated_at = $4
		WHERE trigger_id = $1 AND seq = $2
		  AND EXISTS (SELECT 1 FROM records WHERE kind = $5 AND id = $1 AND version = $6 AND deleted_at IS NULL)`,
		tr.ID, from, to, nowUTC(), typeTrigger, tr.Version)
	return cursorMoved(res, err)
}

// cursorMoved turns a swap that matched no row into errCursorMoved.
func cursorMoved(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errCursorMoved
	}
	return nil
}

// changesPast reads one batch past a cursor for one record trigger, and
// returns it with the scan position the read covers. The read is bounded by
// the trigger's kinds (#637): Postgres returns only entries of a kind the
// source can match (triggerRead), so a trigger over one small kind drains in
// proportion to that kind's entries and not to the whole changelog. Every
// other entry past the cursor, up to the head read first, is one the source
// could never match, so a short batch covers through that head and the
// caller moves the scan position there; a full batch covers through its last
// entry. Sequence order is commit-visibility order (docs/changelog.md), so
// every entry at or under the head is visible to the batch read that follows
// it.
//
// A `*` source reads every entry, the ledger's own `delivery` entries
// included; matchChanges drops them, and the public read hides them.
func (ds *dataset) changesPast(ctx context.Context, tr *trigger, after int64) ([]substrate.Change, int64, error) {
	var head int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return nil, after, err
	}
	if head <= after {
		return nil, after, nil
	}
	kinds, every, err := ds.sourceKinds(ctx, tr.Record.Kinds)
	if err != nil {
		return nil, after, err
	}
	if !every && len(kinds) == 0 {
		return nil, head, nil
	}
	if every {
		kinds = nil
	}
	query, args := triggerRead(kinds, after, head)
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, after, err
	}
	changes, err := collectChanges(rows)
	if err != nil {
		return nil, after, err
	}
	if len(changes) == triggerBatch {
		return changes, changes[len(changes)-1].Seq, nil
	}
	return changes, head, nil
}

// triggerRead builds the dispatcher's batch read of the entries in
// (after, head], at most triggerBatch of them in seq order. With no kinds it
// reads every entry. With kinds it reads each kind as its own branch,
// `kind = $n AND seq > after ORDER BY seq LIMIT batch`, which walks that
// kind's range of changelog_kind_seq_idx from the cursor in seq order, and
// joins the branches with UNION ALL under one `ORDER BY seq LIMIT batch`,
// which Postgres runs as a Merge Append over the branches: no Sort, and at
// most one batch of entries read from each kind, however dense the kind is in
// the changelog. A single `kind = ANY(...)` does not give that bound: an
// array on the index's second column cannot return rows in seq order, so the
// planner either sorts every remaining entry of the kinds or walks the
// primary key and filters out every entry of another kind (#637 review).
func triggerRead(kinds []string, after, head int64) (string, []any) {
	const cols = `SELECT seq, ts, actor, op, record_id, kind, payload, hash FROM changelog`
	args := []any{after, head, triggerBatch}
	if len(kinds) == 0 {
		return cols + ` WHERE seq > $1 AND seq <= $2 ORDER BY seq LIMIT $3`, args
	}
	branch := func(n int) string {
		return cols + ` WHERE kind = $` + strconv.Itoa(n) + ` AND seq > $1 AND seq <= $2 ORDER BY seq LIMIT $3`
	}
	if len(kinds) == 1 {
		return branch(4), append(args, kinds[0])
	}
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		args = append(args, k)
		parts[i] = `(` + branch(len(args)) + `)`
	}
	return strings.Join(parts, ` UNION ALL `) + ` ORDER BY seq LIMIT $3`, args
}

// sourceKinds turns a record source's kind globs into the exact kinds the
// dispatcher's read names. every is true for a `*` source, which reads the
// whole changelog. A package or authority glob matches against the kinds the
// changelog holds, read at the moment of the batch, so a kind whose first
// entry landed mid-drain is still read; the globs are matched in Go
// (vocabulary.MatchTypeGlob, the matcher matchChanges uses) rather than as a
// range in SQL, because a prefix is a contiguous range only under the C
// collation and the column carries the database's.
func (ds *dataset) sourceKinds(ctx context.Context, pats []string) (kinds []string, every bool, err error) {
	// Each kind is named once: triggerRead reads one branch per kind, so a
	// kind named twice (listed twice, or exact and under a glob) would
	// deliver its entries twice.
	seen := map[string]bool{}
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			kinds = append(kinds, k)
		}
	}
	var globs []string
	for _, pat := range pats {
		switch {
		case pat == "*":
			return nil, true, nil
		case strings.HasSuffix(pat, "/*"):
			globs = append(globs, pat)
		default:
			add(pat)
		}
	}
	if len(globs) == 0 {
		return kinds, false, nil
	}
	logged, err := ds.changelogKinds(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, k := range logged {
		for _, pat := range globs {
			if vocabulary.MatchTypeGlob(pat, k) {
				add(k)
				break
			}
		}
	}
	return kinds, false, nil
}

const changelogKindsQuery = `
		WITH RECURSIVE k(kind) AS (
			(SELECT kind FROM changelog ORDER BY kind LIMIT 1)
			UNION ALL
			SELECT (SELECT c.kind FROM changelog c WHERE c.kind > k.kind ORDER BY c.kind LIMIT 1)
			FROM k WHERE k.kind IS NOT NULL
		)
		SELECT kind FROM k WHERE kind IS NOT NULL`

// changelogKinds lists the distinct kinds the changelog holds. A recursive
// skip scan: each step is one probe of changelog_kind_seq_idx for the next
// kind above the last, so the read costs one probe per distinct kind and not
// one row per entry.
func (ds *dataset) changelogKinds(ctx context.Context) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, changelogKindsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// causalDepth walks caused_by from a change to the direct write that started
// the chain, bounded by the cap.
func (ds *dataset) causalDepth(ctx context.Context, seq int64) (int, error) {
	depth := 0
	cur := seq
	for depth <= causalDepthCap {
		var causedBy sql.NullInt64
		err := ds.db.QueryRowContext(ctx,
			`SELECT caused_by FROM changelog WHERE seq = $1`, cur).Scan(&causedBy)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !causedBy.Valid) {
			return depth, nil
		}
		if err != nil {
			return depth, err
		}
		depth++
		cur = causedBy.Int64
	}
	return depth, nil
}

// --- the verbs (status, replay, run, wake, parked, retry) ------------------------

// TriggerStatuses computes per-trigger delivery state: nothing is stored on
// the trigger record itself — status derives from the cursor (or fire
// state), the head and the parked count. An admitted webhook request whose
// fire has not settled (webhooks.go pendingWebhookError) is counted as
// pending, not parked: a healthy door is not a trigger giving up. A row this
// process is delivering now (presentFailure) is counted as in flight, not
// parked, for the same reason. A schedule occurrence that is due and not yet
// settled or parked past counts the same way: in flight while this process
// delivers it, pending while it waits for a dispatcher pass to reach it. A
// record delivery this process runs now counts as in flight once: the one
// claim row its own agent claim holds (recordDelivery.claim) is left out of
// the running rows, and every other running row still counts.
// The last pass and the last settled delivery come from this process's
// memory (triggerActivity).
func (ds *dataset) TriggerStatuses(ctx context.Context) ([]substrate.TriggerStatus, error) {
	var head int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return nil, err
	}
	running, err := ds.runningFailureIDs()
	if err != nil {
		return nil, err
	}
	triggers, err := ds.loadTriggers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]substrate.TriggerStatus, 0, len(triggers))
	for _, lt := range triggers {
		st := substrate.TriggerStatus{
			ID: lt.ID, Callable: lt.CallableID, Enabled: lt.Enabled, Head: head,
		}
		var owedPending, owedInFlight int64
		lastPass, lastDelivered, delivering, claims := ds.activityOf(lt.ID)
		if !lastPass.IsZero() {
			st.LastPassAt = &lastPass
		}
		if !lastDelivered.IsZero() {
			st.LastDeliveredAt = &lastDelivered
		}
		claimsJSON, err := json.Marshal(append([]int64{}, claims...))
		if err != nil {
			return nil, err
		}
		if lt.Err != nil {
			st.Error = lt.Err.Error()
		} else if !lt.runnable() {
			// runnable(), not Callable == nil: an AGENT-backed trigger resolves
			// into lt.Agent and leaves lt.Callable nil, so the narrower test
			// reported every shipped agent trigger as unresolvable while it was
			// dispatching perfectly. This is the SAME predicate the dispatcher
			// skips on, which is what makes the status honest.
			st.Error = fmt.Sprintf("callable %s does not resolve", lt.CallableID)
		}
		switch {
		case lt.Record != nil:
			st.Kind = substrate.TriggerKindRecord
			var seq sql.NullInt64
			err := ds.db.QueryRowContext(ctx,
				`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, lt.ID).Scan(&seq)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				// Never dispatched: it will initialize at head, so lag reads 0.
				st.Cursor = head
			case err != nil:
				return nil, err
			default:
				st.Cursor = seq.Int64
			}
			st.Lag = head - st.Cursor
		case lt.Schedule != nil:
			st.Kind = substrate.TriggerKindSchedule
			var at time.Time
			err := ds.db.QueryRowContext(ctx,
				`SELECT fired_at FROM trigger_schedule WHERE trigger_id = $1`, lt.ID).Scan(&at)
			if err == nil {
				utc := at.UTC()
				st.LastFire = &utc
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			// The occurrences due and not yet settled: a trigger the
			// dispatcher skips (disabled, unparseable, unresolvable) owes
			// none it is going to run, and its Error or Enabled says why.
			if st.LastFire != nil && lt.Enabled && st.Error == "" {
				if owedPending, owedInFlight, err = ds.owedFires(lt, *st.LastFire); err != nil {
					return nil, err
				}
			}
		case lt.Webhook:
			st.Kind = substrate.TriggerKindWebhook
			st.WebhookPath = webhookPath(ds.Repository().Authority, lt.ID)
		}
		if err := ds.db.QueryRowContext(ctx, `
			WITH f AS (
				SELECT id, last_error, id IN (SELECT jsonb_array_elements_text($3::jsonb)::bigint) AS running
				FROM trigger_failures WHERE trigger_id = $1
			)
			SELECT count(*) FILTER (WHERE last_error <> $2 AND NOT running),
			       count(*) FILTER (WHERE last_error = $2),
			       count(*) FILTER (WHERE last_error <> $2 AND running
			                          AND id NOT IN (SELECT jsonb_array_elements_text($4::jsonb)::bigint))
			FROM f`, lt.ID, pendingWebhookError, running, string(claimsJSON)).Scan(&st.Parked, &st.Pending, &st.InFlight); err != nil {
			return nil, err
		}
		st.Pending += owedPending
		st.InFlight += owedInFlight + int64(delivering)
		if st.Parked > 0 {
			var lastErr string
			var at time.Time
			err := ds.db.QueryRowContext(ctx, `
				SELECT last_error, parked_at FROM trigger_failures
				WHERE trigger_id = $1 AND last_error <> $2
				  AND id NOT IN (SELECT jsonb_array_elements_text($3::jsonb)::bigint)
				ORDER BY parked_at DESC, id DESC LIMIT 1`, lt.ID, pendingWebhookError, running).Scan(&lastErr, &at)
			// No row is a park retired since the count, which has no reason
			// left to give.
			switch {
			case err == nil:
				st.LastParkedError, st.LastParkedAt = parkedReason(heldError(lastErr), at)
			case !errors.Is(err, sql.ErrNoRows):
				return nil, err
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// ReplayTrigger sets an record-sourced trigger's cursor — retrospective runs
// are cursor resets, made safe by idempotent effects and no-op suppression.
func (ds *dataset) ReplayTrigger(ctx context.Context, id string, from int64) error {
	tr, _, err := ds.triggerByID(ctx, id)
	if err != nil {
		return err
	}
	if tr.Record == nil {
		return fmt.Errorf("%w: trigger %s has no changelog cursor — replay is for record sources", substrate.ErrValidation, id)
	}
	if from < 0 {
		return fmt.Errorf("%w: replay from %d — the cursor is a seq, at least 0", substrate.ErrValidation, from)
	}
	// The reset and the drop below are one ledger entry, so a restore comes
	// back to the rewound position and not to the delivery it undid.
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.setCursorTx(id, from); err != nil {
			return err
		}
		// A replay rewinds the delivery cursor, so any in-flight paged chain
		// for this trigger's record deliveries is obsolete: drop it, and the
		// re-delivery mints a fresh chain from the new cursor.
		rows, err := t.query(`SELECT chain FROM paged_cursors WHERE trigger_id = $1 AND kind = $2`, id, pagedKindRecord)
		if err != nil {
			return err
		}
		var chains []string
		for rows.Next() {
			var chain string
			if err := rows.Scan(&chain); err != nil {
				_ = rows.Close()
				return err
			}
			chains = append(chains, chain)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		for _, chain := range chains {
			if err := t.unpageTx(id, chain); err != nil {
				return err
			}
		}
		return t.settleDelivery(id)
	})
}

// RunTrigger synthesizes one delivery of a record's current state through a
// trigger — the record's latest change replayed through the callable, cursor
// untouched, no run row (a manual run mints nothing durable). The
// source filter is deliberately not applied — a manual run is the owner's
// hand — but the guard still is: manual runs answer "would it fire".
func (ds *dataset) RunTrigger(ctx context.Context, id, recordKind, recordID string) (int, error) {
	tr, _, err := ds.triggerByID(ctx, id)
	if err != nil {
		return 0, err
	}
	if !tr.runnable() {
		return 0, fmt.Errorf("%w: trigger %s: callable %s does not resolve", substrate.ErrValidation, id, tr.CallableID)
	}
	ty, err := ds.resolveType(recordKind)
	if err != nil {
		return 0, err
	}
	ch, err := ds.latestChangeOf(ctx, ty.Identity, recordID)
	if err != nil {
		return 0, err
	}
	depth, err := ds.causalDepth(ctx, ch.Seq)
	if err != nil {
		return 0, err
	}
	res, err := ds.deliver(ctx, tr, ch, -1, depth, pagedProgress{}, nil)
	return res.ran, err
}

// WakeTrigger runs a trigger's scan NOW: a webhook trigger delivers one
// fire, an record trigger drains its backlog, a schedule trigger checks its
// due occurrence. The webhook fire id is minted per wake — one POST, one
// delivery attempt. A wake is somebody's hand, so it runs without the
// dispatcher's per-trigger budget: a record trigger drains to head.
func (ds *dataset) WakeTrigger(ctx context.Context, id string) (int, error) {
	tr, createdAt, err := ds.triggerByID(ctx, id)
	if err != nil {
		return 0, err
	}
	if !tr.Enabled {
		return 0, fmt.Errorf("%w: trigger %s is disabled", substrate.ErrValidation, id)
	}
	if !tr.runnable() {
		return 0, fmt.Errorf("%w: trigger %s: callable %s does not resolve", substrate.ErrValidation, id, tr.CallableID)
	}
	switch {
	case tr.Webhook:
		wid, err := newID()
		if err != nil {
			return 0, err
		}
		ran, _, err := ds.deliverFire(ctx, tr, runner.ModeWebhook, "wake-"+wid, nowUTC(), nil, nil, nil)
		return ran, err
	case tr.Record != nil:
		return ds.processRecordTrigger(ctx, tr, passDeadline{})
	case tr.Schedule != nil:
		ran, _, err := ds.processScheduleTrigger(ctx, loadedTrigger{trigger: tr, CreatedAt: createdAt}, passDeadline{}, scheduleDrainPerPass)
		return ran, err
	}
	return 0, nil
}

// latestChangeOf reads the newest changelog row for one record, addressed by
// its full (type, id) identity. A trigger's own delivery entries are
// addressed to it and are not changes to it.
func (ds *dataset) latestChangeOf(ctx context.Context, typ, recordID string) (substrate.Change, error) {
	return ds.oneChange(ctx, `
		SELECT seq, ts, actor, op, record_id, kind, payload, hash FROM changelog
		WHERE kind = $1 AND record_id = $2 AND op <> $3 ORDER BY seq DESC LIMIT 1`,
		fmt.Sprintf("record %s has no changes", recordID), typ, recordID, string(substrate.OpDelivery))
}

// TriggerFailures lists a trigger's parked deliveries, oldest first, each
// naming the stream of the trigger's current callable, as SyncStatuses
// attributes it.
func (ds *dataset) TriggerFailures(ctx context.Context, id string) ([]substrate.TriggerFailure, error) {
	tr, _, err := ds.triggerByID(ctx, id)
	if err != nil {
		return nil, err
	}
	stream := triggerStream(ds.registry(), tr)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at
		FROM trigger_failures WHERE trigger_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.TriggerFailure
	for rows.Next() {
		var f substrate.TriggerFailure
		if err := rows.Scan(&f.ID, &f.Trigger, &f.Seq, &f.FireID, &f.RecordID, &f.Attempts, &f.LastError, &f.ParkedAt); err != nil {
			return nil, err
		}
		f.ParkedAt = f.ParkedAt.UTC()
		f.Stream = stream
		ds.presentFailure(&f)
		out = append(out, f)
	}
	return out, rows.Err()
}

// presentFailure says where a listed row's delivery stands. A row this
// process holds in runningClaims is being delivered now: a claim, a retry
// by hand or a webhook fire. A claim it does NOT hold belongs to a run that
// ended without settling (its dispatch was canceled or died), so the row
// reads as interrupted; the stored claim stays until a retry or a forget
// ends it, or the next open rewrites it (settleInterruptedAgentRuns).
func (ds *dataset) presentFailure(f *substrate.TriggerFailure) {
	if _, running := ds.runningClaims.Load(f.ID); running {
		f.Running = true
		return
	}
	f.LastError = heldError(f.LastError)
}

// heldError is the error of a row nothing in this process is delivering: a
// claim there reads as interrupted, every other error as stored.
func heldError(lastError string) string {
	if lastError == inFlightError {
		return interruptedAgentError
	}
	return lastError
}

// runningFailureIDs is every failure id this process is delivering now, as
// the JSON array TriggerStatuses hands its count query.
func (ds *dataset) runningFailureIDs() (string, error) {
	ids := []int64{}
	ds.runningClaims.Range(func(k, _ any) bool {
		if id, ok := k.(int64); ok {
			ids = append(ids, id)
		}
		return true
	})
	raw, err := json.Marshal(ids)
	return string(raw), err
}

// RetryTriggerFailure re-runs one parked delivery against current state: on
// success the failure retires in the transaction that commits the retry's
// last effects, on failure it stays with the new error and attempt count.
// The cursor (or fire state) is already past it, so nothing advances.
func (ds *dataset) RetryTriggerFailure(ctx context.Context, id string, failureID int64) (int, error) {
	tr, _, err := ds.triggerByID(ctx, id)
	if err != nil {
		return 0, err
	}
	if !tr.runnable() {
		return 0, fmt.Errorf("%w: trigger %s: callable %s does not resolve", substrate.ErrValidation, id, tr.CallableID)
	}
	f := foldFailure{ID: foldInt(failureID)}
	var payload []byte
	err = ds.db.QueryRowContext(ctx, `
		SELECT seq, fire_id, record_id, attempts, last_error, payload FROM trigger_failures WHERE id = $1 AND trigger_id = $2`,
		failureID, id).Scan(&f.Seq, &f.FireID, &f.RecordID, &f.Attempts, &f.LastError, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: trigger %s has no parked failure %d", substrate.ErrNotFound, id, failureID)
	}
	if err != nil {
		return 0, err
	}
	f.Payload = json.RawMessage(payload)
	settle := &settlement{ds: ds, trigger: tr.ID, seq: int64(f.Seq), fireID: f.FireID, retire: failureID}
	// A retried occurrence that settles supersedes the parks at or before
	// it, as a dispatched one does.
	if tr.Schedule != nil && len(payload) == 0 {
		if at, err := time.Parse(time.RFC3339, f.FireID); err == nil {
			settle.supersedes = at.UTC()
		}
	}
	// The failure is held in this process before anything runs and until the
	// retirement or the re-park ends: a second retry of the same failure, or
	// a retry of a claim whose dispatch is still running, answers conflict
	// and starts no body and no loop.
	if err := settle.acquire(failureID); err != nil {
		return 0, err
	}
	defer settle.release()
	var n int
	var derr error
	if f.FireID != "" {
		var envelope map[string]any
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &envelope); err != nil {
				return 0, fmt.Errorf("substrate: parked failure %d: envelope: %w", failureID, err)
			}
		}
		n, derr = ds.retryFire(ctx, tr, f.FireID, envelope, settle)
	} else {
		ch, err := ds.changeAt(ctx, int64(f.Seq))
		if err != nil {
			return 0, err
		}
		depth, err := ds.causalDepth(ctx, ch.Seq)
		if err != nil {
			return 0, err
		}
		// Resume a parked paged drain from its last committed page: the cursor
		// (if any) is keyed by this delivery's idempotency key, and feeds the
		// CAS fence and cumulative budget.
		resume, err := ds.loadPagedProgress(ctx, ds.recordChainKey(tr.ID, ch.Seq))
		if err != nil {
			return 0, err
		}
		var res deliverResult
		res, derr = ds.deliver(ctx, tr, ch, -1, depth, resume, settle)
		n = res.ran
		if derr == nil && res.skipped {
			// THE GUARD NO LONGER MATCHES, and a skip is a settled delivery,
			// not a silent success: `deliver` returns before the effect
			// transaction it would have settled in, so the retirement is
			// written here or the row survives a retry that answered "retried"
			// (issue #579 — twenty deliveries of a fixed body, every one
			// re-parked at attempt 1 because the request they carried had
			// since been satisfied).
			if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				return settle.settle(t, res)
			}); err != nil {
				return 0, err
			}
			ds.markRetried(tr)
			return 0, nil
		}
	}
	if derr != nil {
		// The same failure, one attempt older: the ledger rewrites the row
		// under its id, so a restore holds the count and the error the last
		// retry left. A row another retry retired meanwhile is left gone,
		// and this retry answers not found.
		uerr := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			if err := t.lockFailure(tr.ID, failureID); err != nil {
				return err
			}
			f.Attempts++
			f.LastError = derr.Error()
			f.ParkedAt = t.now
			if err := t.parkTx(tr.ID, f); err != nil {
				return err
			}
			if err := t.settleDelivery(tr.ID); err != nil {
				return err
			}
			return t.syncPark(settle.sync, derr)
		})
		if uerr != nil {
			return 0, uerr
		}
		// The retry's OUTCOME, classified: the delivery ran and failed again,
		// the row stands one attempt older, and the caller is told so with
		// the new error's first line (the row keeps the whole text). Bare,
		// the body's error is no sentinel the API knows, and a hand retrying
		// a parked delivery whose body still fails would be answered 500
		// "internal error" with the reason logged and nowhere else.
		return 0, fmt.Errorf("%w: trigger %s: parked delivery %d ran again and failed, it stays parked at attempt %d: %s",
			substrate.ErrParked, tr.ID, failureID, int(f.Attempts), firstLine(derr.Error()))
	}
	ds.markRetried(tr)
	return n, nil
}

// markRetried stamps a hand retry that settled as the trigger's last
// delivery. A webhook trigger carries no stamp (TriggerStatus.LastDeliveredAt).
func (ds *dataset) markRetried(tr *trigger) {
	if !tr.Webhook {
		ds.markDelivered(tr.ID)
	}
}

// ForgetTriggerFailure DROPS one parked delivery without running it: the
// operator has judged it stale, and nothing else can remove it (issue #579).
//
// A retry cannot serve this. It needs a callable that still resolves and a
// delivery that can still be made, and the rows that outlive their usefulness
// are exactly the ones where neither holds: a package uninstalled, a record
// long deleted, a body whose effects landed by another route. The retirement
// is the same one a delivered retry writes — an unpark through the fold — so a
// rebuild and a restore both agree the delivery is over.
//
// It takes the running claim first, so a forget cannot race a retry that is
// already running the same failure, and it does NOT resolve the trigger's
// callable: a parked row whose bundle is gone is the case this exists for.
func (ds *dataset) ForgetTriggerFailure(ctx context.Context, id string, failureID int64) error {
	settle := &settlement{ds: ds, trigger: id, retire: failureID}
	if err := settle.acquire(failureID); err != nil {
		return err
	}
	defer settle.release()
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.lockFailure(id, failureID); err != nil {
			return err
		}
		if err := t.unparkTx(id, failureID); err != nil {
			return err
		}
		return t.settleDelivery(id)
	})
	if err != nil {
		return err
	}
	ds.svc.log.Info("substrate: parked delivery forgotten", "trigger", id, "failure", failureID)
	return nil
}

// retryFire re-invokes one parked schedule/webhook fire, same fire id, fire
// state untouched (it advanced when the park did): the settlement is the
// caller's, retiring the failure. envelope is the parked delivery's own
// envelope when the row kept one, so a webhook retry carries the request
// that arrived.
func (ds *dataset) retryFire(ctx context.Context, tr *trigger, fid string, envelope map[string]any, settle *settlement) (int, error) {
	mode := runner.ModeWebhook
	if tr.Schedule != nil {
		mode = runner.ModeSchedule
	}
	at := nowUTC()
	if t, err := time.Parse(time.RFC3339, fid); err == nil {
		at = t
	}
	if tr.Agent != nil {
		return ds.agentFire(ctx, tr, mode, fid, at, envelope, settle)
	}
	return ds.functionFire(ctx, tr, mode, fid, at, envelope, settle)
}

// changeAt reads one changelog row by seq.
func (ds *dataset) changeAt(ctx context.Context, seq int64) (substrate.Change, error) {
	return ds.oneChange(ctx, `
		SELECT seq, ts, actor, op, record_id, kind, payload, hash FROM changelog
		WHERE seq = $1`,
		fmt.Sprintf("changelog seq %d", seq), seq)
}

// oneChange runs a single-row changelog query; no row is ErrNotFound with
// the caller's description.
func (ds *dataset) oneChange(ctx context.Context, query, missing string, args ...any) (substrate.Change, error) {
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return substrate.Change{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return substrate.Change{}, fmt.Errorf("%w: %s", substrate.ErrNotFound, missing)
	}
	ch, err := scanChange(rows)
	if err != nil {
		return substrate.Change{}, err
	}
	return ch, rows.Err()
}

// scanChange reads one changelog row from a query over the eight columns.
func scanChange(rows *sql.Rows) (substrate.Change, error) {
	var c substrate.Change
	var actor, op string
	var raw, hash []byte
	if err := rows.Scan(&c.Seq, &c.TS, &actor, &op, &c.RecordID, &c.Kind, &raw, &hash); err != nil {
		return c, err
	}
	c.Actor = substrate.Actor(actor)
	c.Op = substrate.Op(op)
	c.TS = c.TS.UTC()
	if len(raw) > 0 {
		// Number-preserving, not a plain Unmarshal: the payload is the fold's
		// replay input, and float64 would round an integer past 2^53, so a
		// rebuild would fold a value the changelog never held.
		if payload, err := decodeNumberPreserving(raw); err == nil {
			c.Payload = payload
		}
	}
	if len(hash) > 0 {
		c.Hash = hex.EncodeToString(hash)
	}
	return c, nil
}
