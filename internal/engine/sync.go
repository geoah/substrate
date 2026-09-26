package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The core `sync` trait's properties the ENGINE writes. The rest of the
// trait (`syncMessage`, `syncProgress`, `syncStreams`, `lastSyncedAt`, the
// request pair) is the body's, through its effects, and the owner's
// (`syncRequestedAt`, `syncPaused`); the dispatcher stamps only what it alone
// can know — that a delivery of the record began, how it settled, and how
// long it took (decision 0085).
const (
	propSyncState          = "syncState"
	propSyncPaused         = "syncPaused"
	propSyncError          = "syncError"
	propSyncErrorAt        = "syncErrorAt"
	propLastSyncStartedAt  = "lastSyncStartedAt"
	propLastSyncDurationMs = "lastSyncDurationMs"
)

// syncErrorMax bounds the error text a park stamps onto the record: a
// runner traceback is the parked failure's to keep whole, the record carries
// the line a person reads — the first one, cut at this many bytes.
const syncErrorMax = 500

// syncStamp is one record-sourced delivery's hand on a `sync`-trait record:
// the record delivered, the actor whose writes the stamps are, and when the
// delivery began. The stamps are written under the CALLABLE's own actor at
// the bundle tier, exactly as the body's effects are, for two reasons: the
// binding kind declares the trait's properties `writer: connector`, which is
// that tier's role, and a record trigger never delivers a write carrying its
// own callable's actor, so the stamp of a delivery cannot fire the trigger
// that made it.
type syncStamp struct {
	ref     eref
	actor   substrate.Actor
	started time.Time
	// stamped is set once `running` was written: the settle and the park
	// write their half only after a start wrote its.
	stamped bool
}

// syncStampFor answers the stamp a delivery of ch carries: nil when the
// change's kind does not bind the trait, when the record is gone (a delete
// delivers null, and there is nothing to stamp), or when the callable is an
// agent, whose loop is many transactions across model turns and settles
// through its own claim (settlement.claim) rather than the effects
// transaction the stamps ride.
func (ds *dataset) syncStampFor(tr *trigger, ch substrate.Change, envelope map[string]any, started time.Time) *syncStamp {
	if tr.Callable == nil {
		return nil
	}
	ty, ok := ds.registry().ByIdentity(ch.Kind)
	if !ok || !ty.Implements(vocabulary.TraitSyncCore) {
		return nil
	}
	if envelope["record"] == nil {
		return nil
	}
	if started.IsZero() {
		started = nowUTC()
	}
	return &syncStamp{
		ref:     eref{Kind: ch.Kind, ID: ch.RecordID},
		actor:   substrate.Actor(tr.Callable.Actor()),
		started: started,
	}
}

// syncPaused reads the owner's pause off the delivered record: a paused
// record's deliveries are skipped, so a pause stops the sync without the
// body knowing, and the skip is a settled attempt on the ledger.
func syncPaused(envelope map[string]any) bool {
	record, _ := envelope["record"].(map[string]any)
	props, _ := record["properties"].(map[string]any)
	paused, _ := props[propSyncPaused].(bool)
	return paused
}

// syncStart writes `running` and the start instant in a transaction of its
// own, BEFORE the body runs: a reader sees the run while it runs. It is
// caused by the delivered change, so the causal-depth cap counts it.
func (ds *dataset) syncStart(ctx context.Context, s *syncStamp, seq int64) error {
	return ds.inTx(ctx, s.actor, false, func(t *txn) error {
		t.causedBy = seq
		return t.asSyncWriter(s.actor, func() error {
			_, err := t.patch(s.ref, substrate.PatchInput{Properties: map[string]any{
				propSyncState:         substrate.SyncStateRunning,
				propLastSyncStartedAt: s.started.Format(time.RFC3339Nano),
			}})
			// Deleted or collected since the envelope was read: t.patch
			// refuses it under the record lock, before writing anything, and
			// the settle and the park skip it the same way.
			if errors.Is(err, substrate.ErrNotFound) {
				return nil
			}
			return err
		})
	})
}

// syncSettleOK rides the transaction that commits the delivery's last
// effects: the duration always, and `ok` only where the body left the state
// at `running` — a body that wrote `throttled` or `erroring` itself has said
// more than the engine knows, and its word stands.
func (t *txn) syncSettleOK(s *syncStamp) error {
	return t.asSyncWriter(s.actor, func() error {
		row, err := t.loadRow(s.ref, false)
		if err != nil || row == nil || row.DeletedAt != nil {
			return err
		}
		props := map[string]any{propLastSyncDurationMs: s.durationMs(t.now)}
		if state, _ := row.Props[propSyncState].(string); state == substrate.SyncStateRunning {
			props[propSyncState] = substrate.SyncStateOK
		}
		_, err = t.patch(s.ref, substrate.PatchInput{Properties: props})
		return err
	})
}

// syncPark rides the transaction that parks the delivery: `erroring`, the
// error text and when, and the duration the attempts took. A body's own
// `erroring` is overwritten with the park's cause, which is the later and
// the truer word.
func (t *txn) syncPark(s *syncStamp, cause error) error {
	if s == nil {
		return nil
	}
	return t.asSyncWriter(s.actor, func() error {
		row, err := t.loadRow(s.ref, false)
		if err != nil || row == nil || row.DeletedAt != nil {
			return err
		}
		_, err = t.patch(s.ref, substrate.PatchInput{Properties: map[string]any{
			propSyncState:          substrate.SyncStateErroring,
			propSyncError:          boundText(firstLine(cause.Error()), syncErrorMax),
			propSyncErrorAt:        t.now.Format(time.RFC3339Nano),
			propLastSyncDurationMs: s.durationMs(t.now),
		}})
		return err
	})
}

func (s *syncStamp) durationMs(now time.Time) int64 {
	d := now.Sub(s.started).Milliseconds()
	if d < 0 {
		return 0
	}
	return d
}

// asSyncWriter runs fn as the callable's own hand at the bundle tier — the
// write context function dispatch builds (write.go setEffectEmit): the
// dispatcher KNOWS it is stamping installed code's run, so the tier is
// stated, never re-derived from the actor.
func (t *txn) asSyncWriter(actor substrate.Actor, fn func() error) error {
	return t.asActor(actor, func() error {
		prev := t.tier
		t.tier = substrate.TierBundle
		defer func() { t.tier = prev }()
		return fn()
	})
}

// settleInterruptedSyncs is the open-time sweep: a record left at `running`
// was mid-delivery when this repository's writer stopped, and no delivery is
// in flight at open (0083: one writer per repository), so the state is a
// lie until something rewrites it. It becomes `erroring`, naming the stop,
// under the kind's own package actor — the hand installed code writes with
// when no callable is running.
func (ds *dataset) settleInterruptedSyncs(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	for _, ty := range ds.registry().Kinds() {
		if !ty.Implements(vocabulary.TraitSyncCore) {
			continue
		}
		rows, err := ds.db.QueryContext(ctx, `
			SELECT id FROM records
			WHERE kind = $1 AND deleted_at IS NULL AND props->>$2 = $3
			ORDER BY id`, ty.Identity, propSyncState, substrate.SyncStateRunning)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
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
		authority, pkg := vocabulary.SplitPackageRef(ty.Package)
		actor := substrate.BundleActor(authority, pkg)
		for _, id := range ids {
			ref := eref{Kind: ty.Identity, ID: id}
			err := ds.inTx(ctx, actor, false, func(t *txn) error {
				return t.asSyncWriter(actor, func() error {
					_, err := t.patch(ref, substrate.PatchInput{Properties: map[string]any{
						propSyncState:   substrate.SyncStateErroring,
						propSyncError:   "interrupted: the server stopped during the run",
						propSyncErrorAt: t.now.Format(time.RFC3339Nano),
					}})
					return err
				})
			})
			if err != nil {
				return fmt.Errorf("settle interrupted sync %s/%s: %w", ty.Identity, id, err)
			}
			ds.svc.log.Warn("substrate: sync interrupted by a stop, marked erroring", "kind", ty.Identity, "record", id)
		}
	}
	return nil
}

// SyncStatuses reads every `sync`-trait record's synchronization: the
// trait's properties off the row, joined with the status of each record
// trigger whose source names the record's kind. The triggers half is the
// same computation TriggerStatuses answers, done once for the whole list.
func (ds *dataset) SyncStatuses(ctx context.Context) ([]substrate.SyncStatus, error) {
	var kinds []*vocabulary.Kind
	for _, ty := range ds.registry().Kinds() {
		if ty.Implements(vocabulary.TraitSyncCore) {
			kinds = append(kinds, ty)
		}
	}
	out := []substrate.SyncStatus{}
	if len(kinds) == 0 {
		return out, nil
	}
	statuses, err := ds.TriggerStatuses(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]substrate.TriggerStatus, len(statuses))
	for _, st := range statuses {
		byID[st.ID] = st
	}
	triggers, err := ds.loadTriggers(ctx)
	if err != nil {
		return nil, err
	}
	for _, ty := range kinds {
		var onKind []substrate.TriggerStatus
		var triggerIDs []string
		for _, lt := range triggers {
			if lt.Record == nil || lt.Err != nil {
				continue
			}
			for _, pat := range lt.Record.Kinds {
				if vocabulary.MatchTypeGlob(pat, ty.Identity) {
					if st, ok := byID[lt.ID]; ok {
						onKind = append(onKind, st)
					}
					triggerIDs = append(triggerIDs, lt.ID)
					break
				}
			}
		}
		// The record's own parked deliveries: the failures on the kind's
		// triggers that name it, keyed by id, in one query per kind.
		parkedByRecord := map[string]int64{}
		if len(triggerIDs) > 0 {
			ids, err := json.Marshal(triggerIDs)
			if err != nil {
				return nil, err
			}
			prows, err := ds.db.QueryContext(ctx, `
				SELECT record_id, count(*) FROM trigger_failures
				WHERE trigger_id IN (SELECT jsonb_array_elements_text($1::jsonb)) AND record_id <> ''
				GROUP BY record_id`, ids)
			if err != nil {
				return nil, err
			}
			for prows.Next() {
				var id string
				var n int64
				if err := prows.Scan(&id, &n); err != nil {
					_ = prows.Close()
					return nil, err
				}
				parkedByRecord[id] = n
			}
			_ = prows.Close()
			if err := prows.Err(); err != nil {
				return nil, err
			}
		}
		rows, err := ds.db.QueryContext(ctx, `SELECT `+recordCols+` FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, ty.Identity)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			row, err := scanRecord(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			st := syncStatusOf(row)
			st.Parked = parkedByRecord[row.ID]
			st.Triggers = append([]substrate.TriggerStatus{}, onKind...)
			out = append(out, st)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// syncStatusOf projects the trait's properties off one row. Every read is
// lenient: the trait contracts datatypes, but a row written before its kind
// bound the trait, or by a body that mis-shaped `syncProgress`, still lists
// with what it has rather than failing the whole read.
func syncStatusOf(row *erow) substrate.SyncStatus {
	p := row.Props
	st := substrate.SyncStatus{
		Kind:               row.Kind,
		ID:                 row.ID,
		Title:              row.Title,
		State:              syncStr(p, propSyncState),
		Message:            syncStr(p, "syncMessage"),
		Paused:             syncBool(p, propSyncPaused),
		LastSyncedAt:       syncTime(p, "lastSyncedAt"),
		LastSyncStartedAt:  syncTime(p, propLastSyncStartedAt),
		LastSyncDurationMs: syncInt(p, propLastSyncDurationMs),
		RequestedAt:        syncTime(p, "syncRequestedAt"),
		RequestedAck:       syncTime(p, "syncRequestedAck"),
		Error:              syncStr(p, propSyncError),
		ErrorAt:            syncTime(p, propSyncErrorAt),
	}
	if st.State == "" {
		st.State = substrate.SyncStateNever
	}
	if prog, ok := p["syncProgress"].(map[string]any); ok {
		st.Progress = &substrate.SyncProgress{
			Phase:   syncStr(prog, "phase"),
			Done:    syncInt(prog, "done"),
			Total:   syncInt(prog, "total"),
			Pending: syncInt(prog, "pending"),
		}
	}
	if streams, ok := p["syncStreams"].(map[string]any); ok && len(streams) > 0 {
		st.Streams = make(map[string]substrate.SyncStream, len(streams))
		for name, raw := range streams {
			s, _ := raw.(map[string]any)
			st.Streams[name] = substrate.SyncStream{
				Cursor:       s["cursor"],
				LastAt:       syncTime(s, "lastAt"),
				Pending:      syncInt(s, "pending"),
				State:        syncStr(s, "state"),
				Message:      syncStr(s, "message"),
				RequestedAck: syncTime(s, "requestedAck"),
			}
		}
	}
	return st
}

func syncStr(p map[string]any, name string) string {
	s, _ := p[name].(string)
	return s
}

func syncBool(p map[string]any, name string) bool {
	b, _ := p[name].(bool)
	return b
}

// propInt reads a number however the row holds it: a Go int64 from a fresh
// write, a float64 or json.Number from the fold, a numeric string from a
// hand-written document.
func syncInt(p map[string]any, name string) int64 {
	switch v := p[name].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

func syncTime(p map[string]any, name string) *time.Time {
	s, _ := p[name].(string)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// firstLine is the message before its first newline: a runner error carries
// the body's traceback after the line that names the failure.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// boundText cuts a message to at most n bytes on a rune boundary.
func boundText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && cut < len(s) && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}
