package engine

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Alerts (decision record 0148): one core/alert record per ongoing problem,
// opened, updated and resolved by the engine inside the transaction that
// observes the problem, as ordinary record writes through the changelog. The
// record id is derived from the problem's key, so a repeat updates one row
// and a rebuild replays the same writes.

const kindAlert = "substrate.reamde.dev/core/alert"

// The alert kind's enum values, as its declaration spells them.
const (
	alertLevelError    = "error"
	alertStateOpen     = "open"
	alertStateResolved = "resolved"
)

// alertRepeatInterval bounds how often a problem that keeps happening rewrites
// its alert: a repeat this soon after the row's last write, at the same level
// on an open row, writes nothing. Without it a trigger parking every few
// seconds writes an alert row, and wakes every record trigger over alerts, on
// every park. A var so a test can shorten it.
var alertRepeatInterval = 5 * time.Minute

const alertKeyTriggerParked = "trigger.parked/"

type alertRaise struct {
	key     string
	level   string
	summary string
	detail  string
	// about is the record paths the problem is about. A path whose kind the
	// repository does not declare is dropped at the write, because a reference
	// to an unknown kind fails validation and would fail the transaction that
	// raised the alert.
	about []string
	count int64
}

// alertID is the record id of the alert keyed key. A key inside the record id
// alphabet that carries no "~" is the id itself, so the id reads as the
// problem it names. Any other key is mapped into the alphabet and suffixed
// with "~" and a hash of the whole key. The two forms never collide, because
// only the second carries a "~".
func alertID(key string) string {
	if vocabulary.ValidID(key) && !strings.Contains(key, "~") {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	suffix := "~" + hex.EncodeToString(sum[:8])
	slug := strings.Map(func(r rune) rune {
		if alertIDRune(r) {
			return r
		}
		return '-'
	}, key)
	// The alphabet's first character is a letter or a digit.
	if slug == "" || !alphanumeric(rune(slug[0])) {
		slug = "alert-" + slug
	}
	if limit := vocabulary.MaxIDLen - len(suffix); len(slug) > limit {
		slug = slug[:limit]
	}
	return slug + suffix
}

// alertIDRune reports whether r may stand in a mapped alert id: the record id
// alphabet less "~", which marks the hashed form.
func alertIDRune(r rune) bool {
	return alphanumeric(r) || strings.ContainsRune("._:@/-", r)
}

func alphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// raiseAlert opens or updates the alert keyed a.key inside the caller's
// transaction, under actor: the actor of the callable the alert is about, so
// a record trigger never receives an alert about its own callable
// (matchChanges) and a notifier that fails cannot feed itself. A repeat on an
// open row at the same level within alertRepeatInterval of the row's last
// write changes nothing; a level change, and a recurrence on a resolved or
// deleted row, always writes. A recurrence restarts firstSeenAt.
func (t *txn) raiseAlert(actor substrate.Actor, a alertRaise) error {
	if _, err := t.resolveType(kindAlert); err != nil {
		// A repository whose vocabulary does not declare the kind yet: an
		// alert must never fail the park that raised it.
		return nil
	}
	id := alertID(a.key)
	// No row lock: the transaction already holds the changelog lock, which
	// orders every writer of this repository, and a lock taken here would come
	// before the registry-dependency lock the put takes.
	row, err := t.loadRow(eref{Kind: kindAlert, ID: id}, false)
	if err != nil {
		return err
	}
	open := row != nil && row.DeletedAt == nil && row.Props["state"] == alertStateOpen
	if open && row.Props["level"] == a.level && t.now.Sub(row.UpdatedAt) < alertRepeatInterval {
		return nil
	}
	now := t.now.Format(time.RFC3339Nano)
	props := map[string]any{
		"key":        a.key,
		"level":      a.level,
		"summary":    a.summary,
		"lastSeenAt": now,
		"count":      a.count,
		"state":      alertStateOpen,
	}
	if !open {
		props["firstSeenAt"] = now
	}
	if a.detail != "" {
		props["detail"] = a.detail
	}
	// Each path once: a repeated reference naming one record twice is refused
	// (normalizeReferencesIn), and a trigger over core/trigger or
	// core/function parks its own row, which the alert already names.
	about := make([]any, 0, len(a.about))
	seen := map[string]bool{}
	for _, path := range a.about {
		if seen[path] {
			continue
		}
		seen[path] = true
		if kind, _, ok := vocabulary.SplitRecordPath(path); ok {
			if _, err := t.resolveType(kind); err == nil {
				about = append(about, path)
			}
		}
	}
	props["about"] = about
	return t.writeAlert(actor, id, props)
}

// resolveAlert resolves the open alert keyed key, under actor (raiseAlert
// says why), with the properties in also riding the same write. An alert
// that is absent, deleted or already resolved writes nothing.
func (t *txn) resolveAlert(actor substrate.Actor, key string, also map[string]any) error {
	return t.updateOpenAlert(actor, key, func(*erow) (map[string]any, error) {
		props := map[string]any{"state": alertStateResolved}
		for name, v := range also {
			props[name] = v
		}
		return props, nil
	})
}

// updateOpenAlert writes the properties change returns onto the open alert
// keyed key; an alert that is absent, deleted or resolved, and a change that
// returns nothing, write nothing.
func (t *txn) updateOpenAlert(actor substrate.Actor, key string, change func(row *erow) (map[string]any, error)) error {
	if _, err := t.resolveType(kindAlert); err != nil {
		return nil
	}
	id := alertID(key)
	row, err := t.loadRow(eref{Kind: kindAlert, ID: id}, false)
	if err != nil {
		return err
	}
	if row == nil || row.DeletedAt != nil || row.Props["state"] != alertStateOpen {
		return nil
	}
	props, err := change(row)
	if err != nil || len(props) == 0 {
		return err
	}
	return t.writeAlert(actor, id, props)
}

// writeAlert puts the alert's properties as the engine's own write under
// actor: internal, so no generic-surface guard applies, and attributed to the
// callable the alert is about.
func (t *txn) writeAlert(actor substrate.Actor, id string, props map[string]any) error {
	prevInternal := t.internal
	t.internal = true
	defer func() { t.internal = prevInternal }()
	return t.asActor(actor, func() error {
		_, err := t.put(substrate.PutInput{Kind: kindAlert, ID: id, Properties: props})
		return err
	})
}

// --- the parked-deliveries alert -------------------------------------------

func parkedAlertKey(triggerID string) string { return alertKeyTriggerParked + triggerID }

// parkedCount counts a trigger's parked deliveries as the trigger status
// counts a row nothing is delivering: every failure row but an admitted
// webhook request not yet run, and but a claim this process holds, which is
// an agent run under way. A claim no run in this process holds was left by a
// run that ended without settling, and waits for a person like any park.
// Unlike the status, a held row that already carries its error counts: it is
// the park being written, by a dispatch or by a retry that failed again.
func (t *txn) parkedCount(triggerID string) (int64, error) {
	held, err := t.ds.runningFailureIDs()
	if err != nil {
		return 0, err
	}
	var n int64
	err = t.row(`
		SELECT count(*) FROM trigger_failures
		WHERE trigger_id = $1 AND last_error <> $2
		  AND NOT (last_error IN ($3, $4)
		           AND id IN (SELECT jsonb_array_elements_text($5::jsonb)::bigint))`,
		triggerID, pendingWebhookError, inFlightError, legacyInFlightError, held).Scan(&n)
	return n, err
}

// raiseParkedAlert raises the trigger's parked-deliveries alert, at level
// error, in the transaction that parks one of its deliveries, after the
// delivery entry: count is the trigger's parked deliveries, detail the park's
// error. record is the parked change's record path, empty for a fire.
func (t *txn) raiseParkedAlert(tr *trigger, record string, cause error) error {
	n, err := t.parkedCount(tr.ID)
	if err != nil {
		return err
	}
	about := []string{vocabulary.RecordPath(typeTrigger, tr.ID), tr.callablePath()}
	if record != "" {
		about = append(about, record)
	}
	return t.raiseAlert(alertActorOf(tr), alertRaise{
		key:     parkedAlertKey(tr.ID),
		level:   alertLevelError,
		summary: fmt.Sprintf("Trigger %s has parked deliveries to %s %s", tr.ID, tr.CallableKind, tr.CallableID),
		detail:  cause.Error(),
		about:   about,
		count:   n,
	})
}

// settleParkedAlert follows a transaction that retired parked deliveries of a
// trigger (a retry that delivered, a forget, a settled schedule fire's
// retirement of older parks): the open parked-deliveries alert takes the new
// count, and resolves once none stand. It runs after the delivery entry.
func (t *txn) settleParkedAlert(triggerID string) error {
	actor, err := t.alertActorOfTrigger(triggerID)
	if err != nil {
		return err
	}
	count, err := t.parkedCount(triggerID)
	if err != nil {
		return err
	}
	key := parkedAlertKey(triggerID)
	if count == 0 {
		return t.resolveAlert(actor, key, map[string]any{"count": count})
	}
	return t.updateOpenAlert(actor, key, func(row *erow) (map[string]any, error) {
		if stored, ok := alertCount(row.Props["count"]); ok && stored == count {
			return nil, nil
		}
		return map[string]any{"count": count}, nil
	})
}

// alertActorOf is the actor an alert about the trigger's callable is written
// under: the callable's own, or the engine's when the callable does not
// resolve, which then cannot run to receive it.
func alertActorOf(tr *trigger) substrate.Actor {
	if a := tr.callableActor(); a != "" {
		return substrate.Actor(a)
	}
	return substrate.ActorSystem
}

// alertActorOfTrigger is alertActorOf for a trigger named by id. A trigger
// row that is gone or does not parse answers the engine's actor.
func (t *txn) alertActorOfTrigger(triggerID string) (substrate.Actor, error) {
	tr, err := t.alertTrigger(triggerID)
	if err != nil || tr == nil {
		return substrate.ActorSystem, err
	}
	return alertActorOf(tr), nil
}

// alertTrigger reads a trigger named by id inside the transaction, its
// callable resolved against the transaction's declarations; nil when the row
// is gone or does not parse.
func (t *txn) alertTrigger(triggerID string) (*trigger, error) {
	row, err := t.loadRow(eref{Kind: typeTrigger, ID: triggerID}, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return nil, err
	}
	tr, err := parseTrigger(triggerID, row.Props)
	if err != nil {
		return nil, nil
	}
	tr.resolveCallable(t.declarations())
	return tr, nil
}

// raiseInterruptedAlert raises the parked-deliveries alert of a trigger whose
// agent claims the open-time sweep just rewrote as interrupted runs
// (settleInterruptedClaims), after the delivery entry: the claims were never
// parked by parkAndAdvance, so without it a crash during a first delivery
// leaves parked work and no alert. seq is a rewritten claim's changelog seq,
// 0 for a fire, and names the record the alert is about.
func (t *txn) raiseInterruptedAlert(triggerID string, seq int64) error {
	tr, err := t.alertTrigger(triggerID)
	if err != nil || tr == nil {
		return err
	}
	record := ""
	if seq > 0 {
		var kind, id string
		err := t.row(`SELECT kind, record_id FROM changelog WHERE seq = $1`, seq).Scan(&kind, &id)
		switch {
		case err == nil:
			record = vocabulary.RecordPath(kind, id)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
	}
	return t.raiseParkedAlert(tr, record, errors.New(interruptedAgentError))
}

// alertCount reads a stored count property, whatever number shape the row
// decoded it as.
func alertCount(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}
