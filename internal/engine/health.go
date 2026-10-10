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
// all parked, for longer than the server's window, opens the alert keyed
// `trigger.failing/<trigger id>` at level error (alerts.go, decision record
// 0148). The streak is read off the run rows alone: every delivery writes
// one, a hand retry included, and retention keeps the newest ok run
// (decision 0152). The dispatcher raises the alert at the end of a pass; the
// settling transaction of the trigger's next ok delivery resolves it. A
// status read derives health from the open alert alone.

// DefaultHealthFailingAfter is the failing window when WithHealthFailingAfter
// is not given (SUBSTRATE_HEALTH_FAILING_AFTER's default).
const DefaultHealthFailingAfter = time.Hour

// alertKeyTriggerFailing prefixes the key of a trigger's failing alert; the
// trigger's id follows it.
const alertKeyTriggerFailing = "trigger.failing/"

// failingAlertKey is the key of a trigger's failing alert.
func failingAlertKey(triggerID string) string { return alertKeyTriggerFailing + triggerID }

// healthReadMax bounds how often a pass reads the triggers' health: the read
// walks every parked run of a trigger that has any, and parked runs are
// never pruned. A window shorter than ten minutes reads ten times per window,
// so an alert opens within a tenth of the window, or a minute, of its end.
const healthReadMax = time.Minute

// rowsQuerier is the read readFailing makes, which a pool and a transaction
// both answer: the pass reads outside any transaction, so a healthy
// repository takes no changelog lock, and again inside the one that writes.
type rowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// failingRead is one trigger's failing alert as it stands, and its parked
// runs of the current episode: after its newest ok run, and from the open
// alert's firstSeenAt (the episode's oldest park) or after the closed
// alert's last resolve, so a refresh never counts an earlier episode's parks
// again. It holds how many, the oldest and the newest, and the newest one's
// error in full.
type failingRead struct {
	triggerID string
	open      bool
	updated   time.Time
	count     int64
	oldest    time.Time
	newest    time.Time
	reason    string
}

// due reports whether the read calls for a write of the alert at now: a
// parked run since the bound, the oldest of them older than the window, and
// the alert not open, or open, last written longer than alertRepeatInterval
// ago, with a run parked since that write.
//
// The resolve bounds the streak because the owner who resolves an alert by
// hand has seen the parks before it: only a park after it reopens the alert.
func (r failingRead) due(now time.Time, window time.Duration) bool {
	if r.count == 0 || now.Sub(r.oldest) < window {
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
// raises its failing alert. A disabled trigger, and one whose callable no
// longer resolves, runs nothing and raises nothing.
func (ds *dataset) raiseFailingAlerts(ctx context.Context, triggers []loadedTrigger) error {
	now := ds.svc.healthNow().UTC()
	if !ds.healthDue(now) {
		return nil
	}
	window := ds.svc.healthFailingAfter
	byID := map[string]*trigger{}
	ids := make([]string, 0, len(triggers))
	for _, lt := range triggers {
		if lt.Err != nil || !lt.Enabled || !lt.runnable() {
			continue
		}
		byID[lt.ID] = lt.trigger
		ids = append(ids, lt.ID)
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
		if !r.due(now, window) {
			continue
		}
		tr := byID[r.triggerID]
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			// Read again under the changelog lock, which every settlement
			// takes: an ok run that settled since the read above is seen.
			again, err := readFailing(t.ctx, t.tx, []string{tr.ID})
			if err != nil || len(again) != 1 || !again[0].due(now, window) {
				return err
			}
			return t.raiseFailingAlert(tr, again[0], window)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("trigger %s: %w", r.triggerID, err))
		}
	}
	return errors.Join(errs...)
}

// readFailing reads each trigger's failing alert and parked-run streak in
// one statement. Each trigger's runs are read through the run kind's
// [trigger, status] index: the newest ok run, then the parked runs after it.
func readFailing(ctx context.Context, q rowsQuerier, triggerIDs []string) ([]failingRead, error) {
	if len(triggerIDs) == 0 {
		return nil, nil
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
	ref := referencePathSQL("props", "trigger")
	rows, err := q.QueryContext(ctx, `
		SELECT t.id, a.props->>'state', a.updated_at, a.deleted_at, p.n, p.oldest, p.newest, p.reason
		FROM jsonb_to_recordset($1::jsonb) AS t(id text, alert text, path text)
		LEFT JOIN records a ON a.kind = $2 AND a.id = t.alert
		LEFT JOIN LATERAL (
			SELECT max((props->>'finishedAt')::timestamptz) AS at FROM records
			WHERE kind = $3 AND deleted_at IS NULL AND `+ref+` = t.path AND props->>'status' = $4
		) ok ON true
		LEFT JOIN LATERAL (
			SELECT count(*) AS n, min(runs.at) AS oldest, max(runs.at) AS newest,
			       (array_agg(runs.reason ORDER BY runs.at DESC))[1] AS reason
			FROM (
				SELECT (props->>'finishedAt')::timestamptz AS at, props->>'reason' AS reason FROM records
				WHERE kind = $3 AND deleted_at IS NULL AND `+ref+` = t.path AND props->>'status' = $5
			) runs
			WHERE runs.at > COALESCE(ok.at, '-infinity'::timestamptz)
			  AND CASE
				WHEN a.id IS NULL THEN true
				WHEN a.deleted_at IS NULL AND a.props->>'state' = $6
				  THEN runs.at >= COALESCE((a.props->>'firstSeenAt')::timestamptz, '-infinity'::timestamptz)
				ELSE runs.at > COALESCE(a.deleted_at, a.updated_at)
			  END
		) p ON true`,
		string(arg), kindAlert, typeTriggerRun, runStatusOK, runStatusParked, alertStateOpen)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []failingRead
	for rows.Next() {
		var r failingRead
		var state, reason sql.NullString
		var updated, deleted, oldest, newest sql.NullTime
		if err := rows.Scan(&r.triggerID, &state, &updated, &deleted, &r.count, &oldest, &newest, &reason); err != nil {
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
// success clears it whatever a later read finds. A trigger with no open
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
	h := triggerHealth{failingSince: map[string]time.Time{}, lastOK: map[string]time.Time{}}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT props->>'key', props->>'firstSeenAt' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'state' = $2
		  AND starts_with(props->>'key', $3)`,
		kindAlert, alertStateOpen, alertKeyTriggerFailing)
	if err != nil {
		return h, err
	}
	for rows.Next() {
		var key string
		var first sql.NullString
		if err := rows.Scan(&key, &first); err != nil {
			_ = rows.Close()
			return h, err
		}
		// An unparseable firstSeenAt still reads as failing, with no instant.
		at, _ := time.Parse(time.RFC3339Nano, first.String)
		h.failingSince[strings.TrimPrefix(key, alertKeyTriggerFailing)] = at.UTC()
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return h, err
	}
	ref := referencePathSQL("props", "trigger")
	rows, err = ds.db.QueryContext(ctx, `
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
