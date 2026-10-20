package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Trigger health (#879): a trigger whose runs since its newest ok run have
// all parked, for longer than the server's window, while at least one of
// those parked runs still has its delivery standing in the ledger, opens the
// alert keyed `trigger.failing/<trigger id>` at level error (alerts.go,
// decision records 0148 and 0177). The streak is read off the run rows:
// every delivery writes one, a hand retry included, and retention keeps the
// newest ok run and the parks after it (decisions 0152 and 0178). The
// ledger only says which of those parked runs still stand: a streak with no
// park left standing is one a person dismissed. The dispatcher raises the
// alert at the end of a pass. It resolves in the settling transaction of the
// trigger's next ok delivery, in the transaction that ends the streak's last
// standing park (a forget, a retry that skipped, a retirement), and at the
// next pass when no park of the streak stands or the trigger is disabled or
// deleted. A status read derives health from the open alert alone.

// DefaultHealthFailingAfter is the failing window when WithHealthFailingAfter
// is not given (SUBSTRATE_HEALTH_FAILING_AFTER's default).
const DefaultHealthFailingAfter = time.Hour

// alertKeyTriggerFailing prefixes the key of a trigger's failing alert; the
// trigger's id follows it.
const alertKeyTriggerFailing = "trigger.failing/"

// failingAlertKey is the key of a trigger's failing alert.
func failingAlertKey(triggerID string) string { return alertKeyTriggerFailing + triggerID }

// healthReadMax bounds how often a pass reads the triggers' health: the read
// walks every parked run of a trigger since its newest ok run, which
// retention keeps however many there are (decision 0178), and counts its
// ledger rows. A window shorter than ten minutes reads ten times per window,
// so an alert opens within a tenth of the window, or a minute, of its end.
const healthReadMax = time.Minute

// failingRead is one trigger's failing alert as it stands, and its parked
// runs of the current episode: after its newest ok run, from the oldest of
// them whose delivery still stands in the ledger, and from the open alert's
// firstSeenAt (the episode's oldest park) or after the closed alert's last
// resolve, so a refresh never counts an earlier episode's parks again. It
// holds how many, the oldest and the newest, and the newest one's error in
// full. A parked run stands while a ledger row of the trigger carries its
// delivery (its seq, or its fireId on a fire) and is not an admitted webhook
// request not yet run: a park, and the same row while a retry of it is in
// flight, so a retry under way keeps the alert raisable. A failed retry
// writes one more parked run of the same delivery and moves nothing back.
// standing is how many of the counted runs stand. A park forgotten or
// retried away no longer stands, so the parked runs before the oldest that
// does are not counted, and a park before the newest ok run never is
// (decision 0177).
type failingRead struct {
	triggerID string
	open      bool
	updated   time.Time
	count     int64
	oldest    time.Time
	newest    time.Time
	reason    string
	standing  int64
}

// due reports whether the read calls for a write of the alert at now: a
// parked run since the bound, the oldest of them older than the window, and
// the alert not open, or open, last written longer than alertRepeatInterval
// ago, with a run parked since that write.
//
// The resolve bounds the streak because the owner who resolves an alert by
// hand has seen the parks before it: only a park after it reopens the alert.
// A streak whose every park was forgotten or retried away is dismissed the
// same way, so it raises nothing until a park stands again (decision 0177).
func (r failingRead) due(now time.Time, window time.Duration) bool {
	if r.count == 0 || r.standing == 0 || now.Sub(r.oldest) < window {
		return false
	}
	if !r.open {
		return true
	}
	return now.Sub(r.updated) >= alertRepeatInterval && r.newest.After(r.updated)
}

// healthDue claims this pass's read of the triggers' health, at most one per
// healthReadMax (or a tenth of the window) on the health clock. A clock that
// stepped back reads at once.
func (ds *dataset) healthDue(now time.Time) bool {
	every := min(ds.svc.healthFailingAfter/10, healthReadMax)
	last := ds.healthReadAt.Load()
	if since := now.Sub(time.Unix(0, last)); last != 0 && since >= 0 && since < every {
		return false
	}
	return ds.healthReadAt.CompareAndSwap(last, now.UnixNano())
}

// raiseFailingAlerts is a pass's last step: every trigger the dispatcher runs
// whose runs since its newest ok run all parked, for longer than the window,
// with a park still standing, raises its failing alert. A disabled trigger,
// and one whose callable no longer resolves, runs nothing and raises
// nothing. The same read resolves the alerts nothing holds open any longer
// (decision 0177): a trigger with no park standing, a disabled trigger, and
// a trigger that is gone, whose parked-deliveries alert resolves with it.
func (ds *dataset) raiseFailingAlerts(ctx context.Context, triggers []loadedTrigger) error {
	now := ds.svc.healthNow().UTC()
	if !ds.healthDue(now) {
		return nil
	}
	window := ds.svc.healthFailingAfter
	active := map[string]*trigger{}
	enabled := map[string]bool{}
	ids := make([]string, 0, len(triggers))
	for _, lt := range triggers {
		ids = append(ids, lt.ID)
		enabled[lt.ID] = lt.Enabled
		if lt.Err == nil && lt.Enabled && lt.runnable() {
			active[lt.ID] = lt.trigger
		}
	}
	reads, err := readFailing(ctx, ds.db, ids)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range reads {
		if ctx.Err() != nil {
			break
		}
		var err error
		switch tr := active[r.triggerID]; {
		case tr != nil && r.due(now, window):
			err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				// Read again under the changelog lock, which every settlement
				// takes: an ok run that settled since the read above is seen.
				again, err := readFailing(t.ctx, t.tx, []string{tr.ID})
				if err != nil || len(again) != 1 || !again[0].due(now, window) {
					return err
				}
				return t.raiseFailingAlert(tr, again[0], window)
			})
		case r.open && r.standing == 0:
			err = ds.resolveStaleFailing(ctx, r.triggerID, "no parked delivery stands", func(t *txn) (bool, error) {
				again, err := readFailing(t.ctx, t.tx, []string{r.triggerID})
				return err == nil && len(again) == 1 && again[0].open && again[0].standing == 0, err
			})
		case r.open && !enabled[r.triggerID]:
			// A trigger that does not parse loads disabled. One whose
			// callable merely does not resolve keeps its alert: its parks
			// stand, and its status carries the error.
			err = ds.resolveStaleFailing(ctx, r.triggerID, "the trigger is disabled", func(t *txn) (bool, error) {
				tr, err := t.alertTrigger(r.triggerID)
				return err == nil && (tr == nil || !tr.Enabled), err
			})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("trigger %s: %w", r.triggerID, err))
		}
	}
	if ctx.Err() == nil {
		if err := ds.resolveGoneTriggerAlerts(ctx, enabled); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// resolveStaleFailing resolves one trigger's failing alert in a transaction
// of its own, once still confirms it under the changelog lock.
func (ds *dataset) resolveStaleFailing(ctx context.Context, triggerID, reason string, still func(t *txn) (bool, error)) error {
	resolved := false
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		ok, err := still(t)
		if err != nil || !ok {
			return err
		}
		resolved = true
		return t.resolveFailingAlert(triggerID)
	})
	if err == nil && resolved {
		ds.svc.log.Info("substrate: failing-trigger alert resolved", "trigger", triggerID, "reason", reason)
	}
	return err
}

// resolveGoneTriggerAlerts resolves the open failing and parked-deliveries
// alerts of every trigger that is no longer loaded: deleting a trigger drops
// its ledger rows (dropTriggerBookkeeping) but leaves its alerts, which
// nothing else would resolve. loaded holds every live trigger's id.
func (ds *dataset) resolveGoneTriggerAlerts(ctx context.Context, loaded map[string]bool) error {
	gone := map[string]bool{}
	for _, prefix := range []string{alertKeyTriggerFailing, alertKeyTriggerParked} {
		open, err := openTriggerAlerts(ctx, ds.db, prefix)
		if err != nil {
			return err
		}
		for id := range open {
			if _, ok := loaded[id]; !ok {
				gone[id] = true
			}
		}
	}
	var errs []error
	for id := range gone {
		if ctx.Err() != nil {
			break
		}
		resolved := false
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			tr, err := t.alertTrigger(id)
			if err != nil || tr != nil {
				return err
			}
			resolved = true
			// Nothing stands for a deleted trigger, so this resolves the
			// parked-deliveries alert and, with it, the failing one.
			return t.settleParkedAlert(id)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("trigger %s: %w", id, err))
			continue
		}
		if resolved {
			ds.svc.log.Info("substrate: alerts of a deleted trigger resolved", "trigger", id)
		}
	}
	return errors.Join(errs...)
}

// openTriggerAlerts reads the open alerts whose key starts with prefix, a
// trigger's id following it: the trigger id to the alert's firstSeenAt, zero
// when it does not parse.
func openTriggerAlerts(ctx context.Context, q dbx, prefix string) (map[string]time.Time, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT props->>'key', props->>'firstSeenAt' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'state' = $2
		  AND starts_with(props->>'key', $3)`,
		kindAlert, alertStateOpen, prefix)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]time.Time{}
	for rows.Next() {
		var key string
		var first sql.NullString
		if err := rows.Scan(&key, &first); err != nil {
			return nil, err
		}
		at, _ := time.Parse(time.RFC3339Nano, first.String)
		out[strings.TrimPrefix(key, prefix)] = at.UTC()
	}
	return out, rows.Err()
}

// readFailing reads each trigger's failing alert and parked-run streak in
// one statement. Each trigger's runs are read through refs where they can be
// (triggerRunsSQL): the newest ok run, then the parked runs after it. q is
// the pool or a transaction: the pass reads outside any transaction, so a
// healthy repository takes no changelog lock, and again inside the one that
// writes.
func readFailing(ctx context.Context, q dbx, triggerIDs []string) ([]failingRead, error) {
	if len(triggerIDs) == 0 {
		return nil, nil
	}
	viaRefs, err := runsViaRefs(ctx, q)
	if err != nil {
		return nil, err
	}
	type target struct {
		ID    string `json:"id"`
		Alert string `json:"alert"`
		Path  string `json:"path"`
	}
	targets := make([]target, 0, len(triggerIDs))
	for _, id := range triggerIDs {
		targets = append(targets, target{ID: id, Alert: alertID(failingAlertKey(id)), Path: vocabulary.RecordPath(typeTrigger, id)})
	}
	arg, err := json.Marshal(targets)
	if err != nil {
		return nil, err
	}
	args := []any{string(arg), kindAlert, typeTriggerRun, runStatusOK, runStatusParked, alertStateOpen, pendingWebhookError}
	if viaRefs {
		args = append(args, typeTrigger)
	}
	runs := triggerRunsSQL(viaRefs, "$3", "$8", "t.id", "t.path")
	rows, err := q.QueryContext(ctx, `
		SELECT t.id, a.props->>'state', a.updated_at, a.deleted_at, p.n, p.oldest, p.newest, p.reason, p.standing
		FROM jsonb_to_recordset($1::jsonb) AS t(id text, alert text, path text)
		LEFT JOIN records a ON a.kind = $2 AND a.id = t.alert
		LEFT JOIN LATERAL (
			SELECT max((run.props->>'finishedAt')::timestamptz) AS at `+runs+` AND run.props->>'status' = $4
		) ok ON true
		LEFT JOIN LATERAL (
			SELECT count(*) AS n, min(runs.at) AS oldest, max(runs.at) AS newest,
			       (array_agg(runs.reason ORDER BY runs.at DESC))[1] AS reason,
			       count(*) FILTER (WHERE runs.stands) AS standing
			FROM (
				SELECT runs.*, min(runs.at) FILTER (WHERE runs.stands) OVER () AS since
				FROM (
					SELECT (run.props->>'finishedAt')::timestamptz AS at, run.props->>'reason' AS reason,
					       EXISTS (
					         SELECT 1 FROM trigger_failures f WHERE f.trigger_id = t.id AND f.last_error <> $7
					           AND CASE WHEN f.fire_id = '' THEN f.seq::text = run.props->>'seq'
					                    ELSE f.fire_id = run.props->>'fireId' END) AS stands
					`+runs+` AND run.props->>'status' = $5
				) runs
				WHERE runs.at > COALESCE(ok.at, '-infinity'::timestamptz)
				  AND CASE
					WHEN a.id IS NULL THEN true
					WHEN a.deleted_at IS NULL AND a.props->>'state' = $6
					  THEN runs.at >= COALESCE((a.props->>'firstSeenAt')::timestamptz, '-infinity'::timestamptz)
					ELSE runs.at > COALESCE(a.deleted_at, a.updated_at)
				  END
			) runs
			WHERE runs.at >= COALESCE(runs.since, 'infinity'::timestamptz)
		) p ON true`,
		args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []failingRead
	for rows.Next() {
		var r failingRead
		var state, reason sql.NullString
		var updated, deleted, oldest, newest sql.NullTime
		if err := rows.Scan(&r.triggerID, &state, &updated, &deleted, &r.count, &oldest, &newest, &reason, &r.standing); err != nil {
			return nil, err
		}
		r.open = state.String == alertStateOpen && !deleted.Valid
		r.updated, r.oldest, r.newest, r.reason = updated.Time, oldest.Time.UTC(), newest.Time.UTC(), reason.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// raiseFailingAlert opens or updates the trigger's failing alert, under its
// callable's actor (alertActorOf): count is the streak's parked runs, detail
// the newest one's error, and firstSeenAt, when the alert opens, the oldest
// park.
func (t *txn) raiseFailingAlert(tr *trigger, r failingRead, window time.Duration) error {
	return t.raiseAlert(alertActorOf(tr), alertRaise{
		key:   failingAlertKey(tr.ID),
		level: alertLevelError,
		summary: fmt.Sprintf("Trigger %s has failed every delivery to %s %s for over %s",
			tr.ID, tr.CallableKind, tr.CallableID, windowText(window)),
		detail: r.reason,
		about:  []string{vocabulary.RecordPath(typeTrigger, tr.ID), tr.callablePath()},
		count:  r.count,
		since:  r.oldest,
	})
}

// resolveFailingAlert resolves the trigger's failing alert inside the
// settling transaction of an ok delivery, after the delivery entry, so one
// success clears it whatever a later read finds. settleParkedAlert calls it
// once a trigger's last park ends, and the pass for an alert nothing holds
// open (decision 0177). A trigger with no open
// failing alert writes nothing and loads no trigger row.
func (t *txn) resolveFailingAlert(triggerID string) error {
	if _, err := t.resolveType(kindAlert); err != nil {
		return nil
	}
	key := failingAlertKey(triggerID)
	row, err := t.loadRow(eref{Kind: kindAlert, ID: alertID(key)}, false)
	if err != nil {
		return err
	}
	if row == nil || row.DeletedAt != nil || row.Props["state"] != alertStateOpen {
		return nil
	}
	actor, err := t.alertActorOfTrigger(triggerID)
	if err != nil {
		return err
	}
	return t.resolveAlert(actor, key, nil)
}

// windowText renders the window as a person writes it: 1h, 90m, 1h30m.
func windowText(d time.Duration) string {
	s := d.Round(time.Second).String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// --- the status reads ----------------------------------------------------------

// triggerHealth is what the status reads join onto each trigger, keyed by
// trigger id: the open failing alerts' firstSeenAt, and the newest ok run's
// finishedAt.
type triggerHealth struct {
	failingSince map[string]time.Time
	lastOK       map[string]time.Time
}

// readTriggerHealth reads both halves for every trigger at once.
func (ds *dataset) readTriggerHealth(ctx context.Context) (triggerHealth, error) {
	h := triggerHealth{lastOK: map[string]time.Time{}}
	// An unparseable firstSeenAt still reads as failing, with no instant.
	failing, err := openTriggerAlerts(ctx, ds.db, alertKeyTriggerFailing)
	if err != nil {
		return h, err
	}
	h.failingSince = failing
	ref := referencePathSQL("props", "trigger")
	rows, err := ds.db.QueryContext(ctx, `
		SELECT `+ref+`, max((props->>'finishedAt')::timestamptz) FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'status' = $2 AND `+ref+` IS NOT NULL
		GROUP BY 1`,
		typeTriggerRun, runStatusOK)
	if err != nil {
		return h, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var path string
		var at time.Time
		if err := rows.Scan(&path, &at); err != nil {
			return h, err
		}
		if _, id, ok := vocabulary.SplitRecordPath(path); ok {
			h.lastOK[id] = at.UTC()
		}
	}
	return h, rows.Err()
}

// apply takes health from the open alert and never from the run rows, so a
// status and the alert list cannot disagree.
func (h triggerHealth) apply(st *substrate.TriggerStatus) {
	st.Health = substrate.HealthOK
	if since, ok := h.failingSince[st.ID]; ok {
		st.Health = substrate.HealthFailing
		if !since.IsZero() {
			st.FailingSince = &since
		}
	}
	if at, ok := h.lastOK[st.ID]; ok {
		st.LastOkAt = &at
	}
}

// syncHealth sets a sync status's health from the triggers whose parks it
// counts: failing when any is, since the oldest of theirs, last ok at the
// newest of theirs.
func syncHealth(st *substrate.SyncStatus, byID map[string]substrate.TriggerStatus, triggerIDs []string) {
	st.Health = substrate.HealthOK
	for _, id := range triggerIDs {
		tr, ok := byID[id]
		if !ok {
			continue
		}
		if tr.Health == substrate.HealthFailing {
			st.Health = substrate.HealthFailing
			if tr.FailingSince != nil && (st.FailingSince == nil || tr.FailingSince.Before(*st.FailingSince)) {
				since := *tr.FailingSince
				st.FailingSince = &since
			}
		}
		if tr.LastOkAt != nil && (st.LastOkAt == nil || tr.LastOkAt.After(*st.LastOkAt)) {
			at := *tr.LastOkAt
			st.LastOkAt = &at
		}
	}
}
