package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A TEMPORARY FAILURE IS RETRIED OVER MINUTES (decision 0266). A delivery
// whose body failed temporarily (runner.ErrTemporary: the body raised
// host.Temporary, let a network error or a server's "try later" escape, or
// ran out of time) is not tried again in the pass that met it. It parks at
// once, its cursor or fire state moved past it as any park's is, under an
// error that says when it is retried, and no alert is raised. A walk each
// dispatcher pass runs every such row that is due, the way a retry by hand
// runs one. A retry that fails temporarily again parks under the next,
// longer gap. Past the last gap, or on any other failure, the row is set
// aside for a person, with the alert a park raises.
//
// Nothing is kept beside the ledger (decision 0064): the row's error says it
// waits, and its parked_at and attempts say until when, so a rebuild and a
// restore fold the same schedule back. Nothing waits in a lane, a delivery
// slot or a transaction either: the row stands, and the walk is detached.

// temporaryRetryGaps are the waits before each automatic retry, measured from
// the park that last wrote the row: eight retries over about an hour.
var temporaryRetryGaps = []time.Duration{
	15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
	4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute,
}

// temporaryRetryMark leads the error of a row that waits for its automatic
// retry: the walk selects rows by it, and a reader of the parked row sees
// when the retry comes.
const temporaryRetryMark = "temporary, retried automatically at "

// parkedError is the error a parked row carries for a failure at the given
// attempt count, the time the row is parked at, and whether the row waits for
// an automatic retry: a function's temporary failure while gaps remain. A
// waiting row is parked at the walk's clock, which is the clock its due time
// is read on (the wall clock outside a test); any other at now, the
// transaction's.
//
// Nothing that may run an agent waits: an agent trigger settles through its
// own claims, a body that ran one must not run it again unattended
// (errAgentThreadOpened), and a function granted an agent, or a call to
// another function, may run one on any attempt.
func (ds *dataset) parkedError(tr *trigger, cause error, attempts int, now time.Time) (string, time.Time, bool) {
	if !autoRetryable(tr) || errors.Is(cause, errAgentThreadOpened) || !runner.Temporary(cause) ||
		attempts < 1 || attempts > len(temporaryRetryGaps) {
		return cause.Error(), now, false
	}
	at := ds.svc.retryNow().UTC()
	due := at.Add(temporaryRetryGaps[attempts-1])
	return temporaryRetryMark + due.Format(time.RFC3339) + ": " + cause.Error(), at, true
}

// autoRetryable reports whether the trigger's callable, as it stands, is one
// the walk may run unattended: a function granted neither an agent nor a call
// to another function, either of which may run an agent.
func autoRetryable(tr *trigger) bool {
	return tr.Agent == nil && tr.Callable != nil && len(tr.Callable.Caps.Agents) == 0 && len(tr.Callable.Caps.Call) == 0
}

// errNotUnattended stops an unattended retry whose callable, as it stands when
// it is about to run, may run an agent: the row is set aside for a person.
var errNotUnattended = errors.New("the trigger's callable may run an agent, so it is not retried unattended")

// waitedCause is the error a waiting row's text carries after its mark and
// due time.
func waitedCause(lastError string) string {
	_, cause, ok := strings.Cut(strings.TrimPrefix(lastError, temporaryRetryMark), ": ")
	if !ok {
		return lastError
	}
	return cause
}

// retryDueAt is when a waiting row's automatic retry is due.
func retryDueAt(f foldFailure) time.Time {
	i := min(max(int(f.Attempts)-1, 0), len(temporaryRetryGaps)-1)
	return f.ParkedAt.Add(temporaryRetryGaps[i])
}

// retryTemporaryFailures runs the waiting rows that are due, oldest first,
// one after another in one detached task per dispatcher pass
// (ProcessTriggers), as resumeWebhooks does for the door's rows. A pass that
// finds the previous walk still running starts none. A read-only process
// retries nothing.
func (ds *dataset) retryTemporaryFailures() {
	if ds.svc.readOnly || !ds.retryingTemporary.CompareAndSwap(false, true) {
		return
	}
	if !ds.spawn("temporary retry", func(ctx context.Context) {
		defer ds.retryingTemporary.Store(false)
		ds.runDueRetries(ctx)
	}) {
		ds.retryingTemporary.Store(false)
	}
}

// runDueRetries is one walk of the waiting rows. A row whose trigger is
// disabled, does not resolve or is held at its allowance stays waiting, and
// the first pass that finds it runnable retries it. One whose trigger's
// callable may now run an agent is set aside for a person. Each row is read again
// under its hold, so one a hand retried or forgot meanwhile is left alone.
func (ds *dataset) runDueRetries(ctx context.Context) {
	waiting, err := ds.waitingRetries(ctx)
	if err != nil {
		ds.svc.log.Error("substrate: deliveries waiting for a retry could not be read",
			"repository", ds.info.ID, "error", err)
		return
	}
	now := ds.svc.retryNow().UTC()
	for _, w := range waiting {
		if ctx.Err() != nil {
			return
		}
		if now.Before(retryDueAt(w.row)) {
			continue
		}
		if _, running := ds.runningClaims.Load(int64(w.row.ID)); running {
			continue
		}
		tr, _, err := ds.triggerByID(ctx, w.trigger)
		if err != nil || !tr.Enabled || !tr.runnable() {
			continue
		}
		if ds.refuseAtAllowance(ctx, tr) != nil {
			continue
		}
		ds.retryWaiting(ctx, tr, int64(w.row.ID))
	}
}

// retryWaiting retries one waiting row under its hold.
func (ds *dataset) retryWaiting(ctx context.Context, tr *trigger, id int64) {
	settle := &settlement{ds: ds, trigger: tr.ID}
	if err := settle.acquire(id); err != nil {
		return
	}
	defer settle.release()
	// Read again under the hold: a hand that retried the row meanwhile left it
	// one attempt older and due later, or retired it.
	f, err := scanFailure(ds.db.QueryRowContext(ctx, failureRowSQL, id, tr.ID), tr.ID, id)
	if err != nil || !strings.HasPrefix(f.LastError, temporaryRetryMark) ||
		ds.svc.retryNow().UTC().Before(retryDueAt(f)) {
		return
	}
	// The trigger's callable is read as it stands now: one edited since the
	// park to a function that may run an agent is not run unattended. A
	// record delivery reads it again as it runs (deliver), and checks that
	// one, which is the one it runs.
	if !autoRetryable(tr) {
		ds.setAsideWaiting(ctx, tr, f)
		return
	}
	settle.unattended = true
	_, err = ds.retryHeldFailure(ctx, tr, f, settle)
	if errors.Is(err, errNotUnattended) {
		ds.setAsideWaiting(ctx, tr, f)
		return
	}
	if err != nil && !errors.Is(err, substrate.ErrParked) {
		ds.svc.log.Warn("substrate: the automatic retry of a temporary failure did not run",
			"repository", ds.info.ID, "trigger", logSafeID(tr.ID), "failure", id, "error", err)
	}
}

// setAsideWaiting turns a waiting row into a park for a person, under its own
// id and attempt count, with the error it waited on and the alert a park
// raises: the walk does not run its trigger's callable unattended.
func (ds *dataset) setAsideWaiting(ctx context.Context, tr *trigger, f foldFailure) {
	cause := waitedCause(f.LastError)
	var record string
	if f.FireID == "" {
		if ch, err := ds.changeAt(ctx, int64(f.Seq)); err == nil {
			record = vocabulary.RecordPath(ch.Kind, ch.RecordID)
		}
	}
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.lockFailure(tr.ID, int64(f.ID)); err != nil {
			return err
		}
		f.LastError = cause
		if err := t.parkTx(tr.ID, f); err != nil {
			return err
		}
		if err := t.settleDelivery(tr.ID); err != nil {
			return err
		}
		return t.raiseParkedAlert(tr, record, errors.New(cause))
	})
	if err != nil {
		ds.svc.log.Warn("substrate: a temporary failure whose callable may now run an agent could not be set aside",
			"repository", ds.info.ID, "trigger", logSafeID(tr.ID), "failure", int64(f.ID), "error", err)
	}
}

// waitingRetry is one row that waits for its automatic retry.
type waitingRetry struct {
	trigger string
	row     foldFailure
}

// waitingRetries reads every row that waits for its automatic retry, oldest
// first.
func (ds *dataset) waitingRetries(ctx context.Context) ([]waitingRetry, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, trigger_id, attempts, parked_at
		FROM trigger_failures WHERE left(last_error, length($1)) = $1 ORDER BY id`, temporaryRetryMark)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []waitingRetry
	for rows.Next() {
		var w waitingRetry
		if err := rows.Scan(&w.row.ID, &w.trigger, &w.row.Attempts, &w.row.ParkedAt); err != nil {
			return nil, err
		}
		w.row.ParkedAt = w.row.ParkedAt.UTC()
		out = append(out, w)
	}
	return out, rows.Err()
}
