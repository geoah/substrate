package engine

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The trigger dispatcher (substrate-primitives §3): every enabled trigger
// record owns delivery. An record-sourced trigger owns a changelog cursor —
// the dispatcher reads past it, filters by the source, evaluates the `when:`
// guard against the record's CURRENT state, runs the callable's body in the
// shared runner, and applies the returned effects through the ordinary write
// path under the CALLABLE's actor, cursor advance in the SAME transaction. A
// schedule-sourced trigger owns a fire state instead: due RRULE occurrences
// (oldest first, a bounded number per pass, stable fire ids) enter the same
// delivery path with mode `schedule` and no changelog row underneath. Serial
// per trigger; a failed delivery retries and then parks-and-advances, so one
// poisoned record never wedges a trigger's lag. Every settled delivery
// attempt writes one `run` record (ok / skipped / parked) under the system
// actor, in the transaction that commits the effects and the cursor motion
// it describes; parked runs are kept, everything else prunes to the newest
// runRetention per trigger. The cursor, the fire state, the parked failures
// and a paged drain's resume row are the DELIVERY LEDGER (delivery.go):
// every motion is a fold effect on a `delivery` changelog entry, so a
// restore folds them back.

const (
	// triggerBatch bounds one changelog read; the loop drains to head.
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
// trigger, oldest first. A trigger that was down, disabled or restored fires
// every occurrence it missed, but over passes rather than in one burst at
// startup; nothing is coalesced away. A var, so a test can lower it.
var scheduleDrainPerPass = 10

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

// errCursorMoved marks a cursor (or schedule fire state) compare-and-swap
// that lost: a replay reset it or a concurrent dispatcher advanced it
// mid-pass. The losing transaction rolls back whole — effects never land
// under a cursor the pass no longer owns — and the pass yields; the next one
// resumes from wherever the cursor now points.
var errCursorMoved = errors.New("trigger cursor moved concurrently")

// ProcessTriggers runs one dispatcher pass: every enabled trigger drains its
// backlog (record sources) or fires its due occurrence (schedule sources).
// It returns the number of deliveries that applied effects. Only
// infrastructure errors surface; eval and effect errors park.
func (ds *dataset) ProcessTriggers(ctx context.Context) (int, error) {
	// Collect paged-cursor rows no live trigger, parked failure, or in-flight
	// drain owns anymore before the pass — best-effort, a sweep
	// error never blocks delivery.
	if err := ds.sweepPagedCursors(ctx); err != nil {
		ds.svc.log.Warn("substrate: sweeping orphaned paged cursors", "error", err)
	}
	triggers, err := ds.loadTriggers(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, lt := range triggers {
		if lt.Err != nil {
			ds.svc.log.Warn("substrate: trigger row does not parse — it is skipped, its cursor stands still",
				"trigger", lt.ID, "error", lt.Err)
			continue
		}
		if !lt.Enabled {
			continue
		}
		if !lt.runnable() {
			ds.svc.log.Warn("substrate: trigger names a callable that no longer resolves — it is skipped, its cursor stands still",
				"trigger", lt.ID, "callable", lt.CallableID)
			continue
		}
		var n int
		var perr error
		switch {
		case lt.Record != nil:
			n, perr = ds.processRecordTrigger(ctx, lt.trigger)
		case lt.Schedule != nil:
			n, perr = ds.processScheduleTrigger(ctx, lt)
		case lt.Webhook:
			// Webhook triggers deliver on wake only.
		}
		total += n
		if perr != nil {
			errs = append(errs, fmt.Errorf("trigger %s: %w", lt.ID, perr))
		}
	}
	return total, errors.Join(errs...)
}

// --- record-sourced delivery ---------------------------------------------------

func (ds *dataset) processRecordTrigger(ctx context.Context, tr *trigger) (int, error) {
	cursor, err := ds.ensureCursor(ctx, tr.ID)
	if err != nil {
		return 0, err
	}
	ran := 0
	for {
		changes, err := ds.changesPast(ctx, cursor)
		if err != nil {
			return ran, err
		}
		if len(changes) == 0 {
			return ran, nil
		}
		matched := matchChanges(tr, changes)
		if tr.Record.Coalesce {
			matched = coalesceChanges(matched)
		}
		for _, ch := range matched {
			n, next, err := ds.deliverWithRetry(ctx, tr, ch, cursor)
			ran += n
			if errors.Is(err, errCursorMoved) {
				return ran, nil
			}
			if err != nil {
				return ran, err
			}
			cursor = next
		}
		// Trailing rows the source skipped move the SCAN position, the one
		// cursor motion outside the ledger (delivery.go): a crash or a
		// restore before this line only re-reads rows that do not match.
		last := changes[len(changes)-1].Seq
		if last > cursor {
			if err := ds.advanceCursor(ctx, tr, cursor, last); err != nil {
				if errors.Is(err, errCursorMoved) {
					return ran, nil
				}
				return ran, err
			}
			cursor = last
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
// exclusion keeps an agent off its own transcript.)
func matchChanges(tr *trigger, changes []substrate.Change) []substrate.Change {
	self := substrate.Actor(tr.callableActor())
	var out []substrate.Change
	for _, ch := range changes {
		if ch.Actor == self || ch.Kind == typeRun || ch.Op == substrate.OpDelivery {
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
	started := nowUTC()
	depth, err := ds.causalDepth(ctx, ch.Seq)
	if err != nil {
		return 0, from, err
	}
	if depth >= causalDepthCap {
		if err := ds.parkAndAdvance(ctx, tr, ch, from, 0, started,
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
	// The claim an agent attempt takes is held through every attempt and
	// the park, so a retry by hand cannot start a second loop between them.
	defer settle.release()
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
		if err == nil {
			if res.skipped {
				// The guard said no: a skip is a settled attempt — record it
				// and advance the cursor in one transaction.
				if err := ds.recordSkipAndAdvance(ctx, tr, ch, from, started, ""); err != nil {
					return 0, from, err
				}
				return 0, ch.Seq, nil
			}
			return res.ran, ch.Seq, nil
		}
		if errors.Is(err, errCursorMoved) {
			return 0, from, err
		}
		if errors.Is(err, errClaimedElsewhere) {
			// Another dispatch holds this delivery: it runs it, this pass
			// moves past it and says so.
			if err := ds.recordSkipAndAdvance(ctx, tr, ch, from, started, err.Error()); err != nil {
				return 0, from, err
			}
			return 0, ch.Seq, nil
		}
		// Only the DISPATCHER's context ending aborts the pass: a
		// per-invocation runner timeout is a delivery failure that rides the
		// retries.
		if ctx.Err() != nil {
			return 0, from, ctx.Err()
		}
		lastErr = err
		// A deterministic trip — a read or call outside the allowlist, a
		// blown budget — reproduces on every retry, so it parks immediately
		// (the errCausalDepth precedent). A timeout is not deterministic
		// (db load) and rides the attempts. A paged drain that already
		// committed pages (errPagedParked) also parks now: re-running it would
		// only resume the same chain, so leave it to the parked-failure retry.
		if runner.Deterministic(err) || errors.Is(err, errPagedParked) {
			attempts = attempt + 1
			break
		}
	}
	if err := ds.parkAndAdvance(ctx, tr, ch, from, attempts, started, lastErr); err != nil {
		return 0, from, err
	}
	return 0, ch.Seq, nil
}

// deliverResult is one settled deliver call.
type deliverResult struct {
	ran     int  // 1 when effects applied
	moved   bool // the cursor advanced (dispatch mode only)
	skipped bool // the when guard said no
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
// crash between the two leaves the delivery listed under the trigger's
// parked failures as in flight, retried by hand; nothing redelivers by
// itself and no effect commits without a recorded delivery state. nil
// settles nothing: a manual run mints nothing durable.
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
	// record writes the run record; nil on a retry.
	record func(t *txn, res deliverResult) error
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
	if s.acknowledge != nil {
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
	if err := t.settleDelivery(s.trigger); err != nil {
		return err
	}
	if s.record != nil {
		return s.record(t, res)
	}
	return nil
}

// claim is the agent path's first transaction, before the loop: the
// acknowledgement, and the delivery recorded as in flight under a parked
// failure whose id is the claim's own delivery entry. An attempt that follows
// a failed one finds the claim the first attempt wrote and writes nothing; a
// retry's claim is the failure it re-runs. It returns the failure id the
// completion retires.
func (s *settlement) claim(t *txn) (int64, error) {
	if s.retire != 0 {
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
	if _, taken := s.ds.runningClaims.LoadOrStore(id, struct{}{}); taken {
		return fmt.Errorf("%w: trigger %s failure %d is a delivery this process is still running", substrate.ErrConflict, s.trigger, id)
	}
	s.held = id
	return nil
}

// release gives the held claim back, once the completion, the park or the
// retry that held it has ended.
func (s *settlement) release() {
	if s == nil || s.held == 0 {
		return
	}
	s.ds.runningClaims.Delete(s.held)
	s.held = 0
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
		ds: ds, trigger: tr.ID, seq: ch.Seq,
		acknowledge: func(t *txn) error { return t.advanceCursorTx(tr.ID, from, ch.Seq) },
	}
	s.record = func(t *txn, res deliverResult) error {
		return t.putSystemRun(runRecord{
			trigger: tr.ID, callable: tr.CallableID, mode: runner.ModeRecord,
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
	// (bundles.go, review #2). The leased context flows into runCallable so
	// nested host Calls inherit the lease instead of re-locking.
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
	res = settledResult(advance, effectsSummary(effects), 1)
	err = ds.inTx(ctx, actor, false, func(t *txn) error {
		t.causedBy = ch.Seq
		t.setEffectEmit(tr.Callable.Caps.Emit)
		if err := t.lockEffectTargets(effects); err != nil {
			return err
		}
		for _, ef := range effects {
			if err := t.applyEffect(ef); err != nil {
				return err
			}
		}
		return settle.settle(t, res)
	})
	if err != nil {
		return deliverResult{}, err
	}
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
func (ds *dataset) parkAndAdvance(ctx context.Context, tr *trigger, ch substrate.Change, from int64, attempts int, started time.Time, cause error) error {
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
		return t.putRun(runRecord{
			trigger: tr.ID, callable: tr.CallableID, mode: runner.ModeRecord,
			seq: ch.Seq, recordID: ch.RecordID, status: runStatusParked,
			attempt: attempts, startedAt: started, errMsg: cause.Error(),
		})
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
			trigger: tr.ID, callable: tr.CallableID, mode: runner.ModeRecord,
			seq: ch.Seq, recordID: ch.RecordID, status: runStatusSkipped,
			attempt: 1, startedAt: started, errMsg: reason,
		}); err != nil {
			return err
		}
		return t.pruneRuns(tr.ID)
	})
}

// --- schedule-sourced delivery ----------------------------------------------------

// processScheduleTrigger fires the occurrences due since the last one the
// trigger acknowledged, oldest first and at most scheduleDrainPerPass of
// them: each fire advances the fire state to its occurrence, so a pass that
// stops early (an error, a lost swap) leaves the rest due for the next.
func (ds *dataset) processScheduleTrigger(ctx context.Context, lt loadedTrigger) (int, error) {
	lastFire, err := ds.ensureScheduleState(ctx, lt.ID)
	if err != nil {
		return 0, err
	}
	due, err := lt.Schedule.dueFires(lt.CreatedAt, lastFire, nowUTC(), scheduleDrainPerPass)
	if err != nil {
		return 0, err
	}
	ran := 0
	for _, at := range due {
		n, err := ds.deliverFire(ctx, lt.trigger, runner.ModeSchedule, fireID(at), at, &lastFire, nil)
		ran += n
		if err != nil {
			return ran, err
		}
		lastFire = at
	}
	return ran, nil
}

// fireSettlement settles a dispatched fire: for a schedule occurrence the
// fire-state advance from the occurrence the pass read, then the OK run
// record. A webhook fire has no fire state, so on the function path its run
// record is the whole settlement and no delivery entry is appended.
func (ds *dataset) fireSettlement(tr *trigger, mode, fid string, at time.Time, lastFire *time.Time, started time.Time) *settlement {
	s := &settlement{ds: ds, trigger: tr.ID, fireID: fid}
	s.record = func(t *txn, res deliverResult) error {
		return t.putSystemRun(runRecord{
			trigger: tr.ID, callable: tr.CallableID, mode: mode,
			fireID: fid, status: runStatusOK, attempt: s.attempt,
			startedAt: started, effects: res.effects, pages: res.pages,
		}, true)
	}
	if lastFire != nil {
		s.acknowledge = func(t *txn) error { return t.advanceScheduleTx(tr.ID, *lastFire, at) }
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
// nil means the bare fire envelope.
func (ds *dataset) deliverFire(ctx context.Context, tr *trigger, mode, fid string, at time.Time, lastFire *time.Time, envelope map[string]any) (int, error) {
	started := nowUTC()
	var lastErr error
	attempts := triggerAttempts
	settle := ds.fireSettlement(tr, mode, fid, at, lastFire, started)
	defer settle.release()
	for attempt := range triggerAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(triggerRetryBackoff[min(attempt-1, len(triggerRetryBackoff)-1)]):
			}
		}
		settle.attempt = attempt + 1
		var applied int
		var err error
		if tr.Agent != nil {
			applied, err = ds.agentFire(ctx, tr, mode, fid, at, envelope, settle)
		} else {
			applied, err = ds.functionFire(ctx, tr, mode, fid, at, envelope, settle)
		}
		if err == nil {
			return applied, nil
		}
		if errors.Is(err, errClaimedElsewhere) {
			// Another dispatch holds this fire: move the fire state past it
			// and say so.
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
					trigger: tr.ID, callable: tr.CallableID, mode: mode, fireID: fid,
					status: runStatusSkipped, attempt: 1, startedAt: started, errMsg: err.Error(),
				})
			})
			if errors.Is(skip, errCursorMoved) {
				return 0, nil
			}
			return 0, skip
		}
		if errors.Is(err, errCursorMoved) {
			// Another dispatcher fired this occurrence; ours is a duplicate
			// and rolled back whole.
			return 0, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		lastErr = err
		if runner.Deterministic(err) || errors.Is(err, errPagedParked) {
			attempts = attempt + 1
			break
		}
	}
	// Park-and-advance, fire-shaped: the failure row keeps the fire id, and
	// the schedule state still moves — a poisoned occurrence never wedges the
	// ones behind it. A built envelope parks with the row in its parked form
	// (webhooks.go parkedEnvelope), so a retry re-delivers the request that
	// arrived rather than a bare fire.
	payload, err := ds.parkedEnvelope(ctx, envelope)
	if err != nil {
		return 0, err
	}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		// An agent fire already claimed the occurrence (settlement.claim):
		// the park rewrites the claim and moves the fire state no further.
		id, claimed, err := t.claimedFailure(tr.ID, 0, fid)
		if err != nil {
			return err
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
			trigger: tr.ID, callable: tr.CallableID, mode: mode,
			fireID: fid, status: runStatusParked, attempt: attempts,
			startedAt: started, errMsg: lastErr.Error(),
		})
	})
	if err != nil {
		if errors.Is(err, errCursorMoved) {
			return 0, nil
		}
		return 0, err
	}
	ds.svc.log.Warn("substrate: trigger fire parked",
		"trigger", tr.ID, "callable", tr.CallableID, "fire", fid, "error", lastErr)
	return 0, nil
}

// functionFire runs one schedule/webhook fire through the runner: effects
// and the settlement in one transaction, the effectively-once half. A paged
// body (a scheduled backfill) drains off the causal chain, the settlement
// landing only when the drain finishes.
func (ds *dataset) functionFire(ctx context.Context, tr *trigger, mode, fid string, at time.Time, envelope map[string]any, settle *settlement) (int, error) {
	// The lifecycle fence's shared side, admission through effect + fire-state
	// commit (bundles.go, review #2).
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
	res := settledResult(false, effectsSummary(effects), 1)
	err = ds.inTx(ctx, actor, false, func(t *txn) error {
		t.setEffectEmit(tr.Callable.Caps.Emit)
		if err := t.lockEffectTargets(effects); err != nil {
			return err
		}
		for _, ef := range effects {
			if err := t.applyEffect(ef); err != nil {
				return err
			}
		}
		return settle.settle(t, res)
	})
	if err != nil {
		return 0, err
	}
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
)

// runRecord is one settled delivery attempt, about to become a run record.
type runRecord struct {
	trigger   string
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
}

// putRun writes one run record inside the caller's transaction.
func (t *txn) putRun(r runRecord) error {
	id, err := newID()
	if err != nil {
		return err
	}
	props := map[string]any{
		"trigger":    vocabulary.RecordPath(typeTrigger, r.trigger),
		"callable":   r.callable,
		"mode":       r.mode,
		"status":     r.status,
		"attempt":    r.attempt,
		"startedAt":  r.startedAt.Format(time.RFC3339Nano),
		"finishedAt": t.now.Format(time.RFC3339Nano),
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
	_, err = t.put(substrate.PutInput{Kind: typeRun, ID: id, Properties: props})
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
		typeRun, vocabulary.RecordPath(typeTrigger, triggerID), runStatusParked, runRetention)
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
		if _, err := t.softDelete(eref{Kind: typeRun, ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// effectsSummary counts applied effects by action.
func effectsSummary(effects []effect) map[string]int {
	if len(effects) == 0 {
		return nil
	}
	out := map[string]int{}
	for _, ef := range effects {
		out[ef.Action]++
	}
	return out
}

// mergedSummary is a running summary plus one page's effects, as a new map:
// the paged drain's cross-page effect tally, computed before the page's
// transaction so the final page's settlement records the whole chain.
func mergedSummary(summary map[string]int, effects []effect) map[string]int {
	out := make(map[string]int, len(summary)+1)
	for k, v := range summary {
		out[k] = v
	}
	for _, ef := range effects {
		out[ef.Action]++
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

// pagedOwner is the lifecycle identity a paged_cursors row carries (review-p1
// #8): the trigger that owns the chain, and enough to match its parked failure.
type pagedOwner struct {
	triggerID string
	kind      string // pagedKindRecord | pagedKindFire
	identity  string // the seq (decimal) or the fire id
}

// pagedProgress is a chain's persisted state: the resume cursor and version
// (the CAS fence, review-p1 #1) plus the cumulative budget counters (review-p1
// #2). `exists` is false for a fresh chain — a drain that has committed nothing.
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
	startedAt := resume.startedAt
	if !haveRow {
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
		effects := page.effects
		nextPages := cumPages + 1
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

		merged := mergedSummary(summary, effects)
		err := ds.inTx(ctx, actor, false, func(t *txn) error {
			t.causedBy = causedBy
			t.setEffectEmit(emit)
			if err := t.lockEffectTargets(effects); err != nil {
				return err
			}
			for _, ef := range effects {
				if err := t.applyEffect(ef); err != nil {
					return err
				}
			}
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
			} else if err := t.advancePagedCursor(key, version, cursor, nextPages, nextEffects, nextBytes); err != nil {
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
				// intact rather than replaying the chain.
				return summary, pages, fmt.Errorf("%w: %w", errPagedParked, err)
			}
			// A fresh chain that committed nothing can safely retry from zero.
			return summary, pages, err
		}
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
	return fmt.Sprintf("%s/%s/%d", ds.Repository().Name, triggerID, seq)
}

// fireChainKey is the paged-cursor key for a schedule or webhook fire delivery.
func (ds *dataset) fireChainKey(triggerID, fireID string) string {
	return fmt.Sprintf("%s/%s/%s", ds.Repository().Name, triggerID, fireID)
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

// changesPast reads one raw batch past a cursor: every entry, the ledger's
// own `delivery` entries included, so the scan position moves past them and
// lag reads zero; matchChanges drops them. The public read hides them.
func (ds *dataset) changesPast(ctx context.Context, after int64) ([]substrate.Change, error) {
	b := &builder{}
	b.add(`seq > ` + b.arg(after))
	return ds.queryChanges(ctx, b, `seq`, triggerBatch)
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
// state), the head and the parked count.
func (ds *dataset) TriggerStatuses(ctx context.Context) ([]substrate.TriggerStatus, error) {
	var head int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
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
		case lt.Webhook:
			st.Kind = substrate.TriggerKindWebhook
			st.WebhookPath = webhookPath(ds.Repository().Authority, lt.ID)
		}
		if err := ds.db.QueryRowContext(ctx,
			`SELECT count(*) FROM trigger_failures WHERE trigger_id = $1`, lt.ID).Scan(&st.Parked); err != nil {
			return nil, err
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
// untouched, no run row (direct invocations mint nothing durable). The
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
// delivery attempt.
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
		return ds.deliverFire(ctx, tr, runner.ModeWebhook, "wake-"+wid, nowUTC(), nil, nil)
	case tr.Record != nil:
		return ds.processRecordTrigger(ctx, tr)
	case tr.Schedule != nil:
		return ds.processScheduleTrigger(ctx, loadedTrigger{trigger: tr, CreatedAt: createdAt})
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

// TriggerFailures lists a trigger's parked deliveries, oldest first.
func (ds *dataset) TriggerFailures(ctx context.Context, id string) ([]substrate.TriggerFailure, error) {
	if _, _, err := ds.triggerByID(ctx, id); err != nil {
		return nil, err
	}
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
		out = append(out, f)
	}
	return out, rows.Err()
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
	}
	if derr != nil {
		// The same failure, one attempt older: the ledger rewrites the row
		// under its id, so a restore holds the count and the error the last
		// retry left. A row another retry retired meanwhile is left gone,
		// and this retry answers not found.
		// The payload goes back through the parking policy: a row a binary
		// before the ledger parked holds every header, the query and the
		// body, none of which may enter the changelog.
		scrubbed, perr := ds.parkedPayload(ctx, f.Payload)
		if perr != nil {
			return 0, perr
		}
		f.Payload = scrubbed
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
			return t.settleDelivery(tr.ID)
		})
		if uerr != nil {
			return 0, uerr
		}
		return 0, derr
	}
	return n, nil
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
