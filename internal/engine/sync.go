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
	// propLastSyncedAt is the body's, read here only to see a run finish
	// healthy (syncClearOnRecovery).
	propLastSyncedAt = "lastSyncedAt"
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
// change's kind does not bind the trait, when the callable is not the kind's
// own sync, when the record is gone (a delete delivers null, and there is
// nothing to stamp), or when the callable is an agent, whose loop is many
// transactions across model turns and settles through its own claim
// (settlement.claim) rather than the effects transaction the stamps ride.
//
// The kind's own sync is a function of the package that declares the kind:
// the bundle that owns the account is the one that syncs it. Another
// package's function triggered on the same record (an identity resolver, a
// mirror) is not a run of the sync, and stamping it would report its runs
// as the sync's and move the record's version on every one.
func (ds *dataset) syncStampFor(tr *trigger, ch substrate.Change, envelope map[string]any, started time.Time) *syncStamp {
	if tr.Callable == nil {
		return nil
	}
	ty, ok := ds.registry().ByIdentity(ch.Kind)
	if !ok || !ty.Implements(vocabulary.TraitSyncCore) {
		return nil
	}
	if tr.Callable.Package != ty.Package {
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
// record's deliveries of its own sync are skipped, so a pause stops the sync
// without the body knowing, and the skip is a settled attempt on the ledger.
// It is asked only where syncStampFor answered a stamp: the pause is the
// owner's hand on the sync, and another package's function on the record
// runs whatever the pause says.
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
// more than the engine knows, and its word stands. A run that ends `ok`,
// whoever wrote the word, clears an error pair older than the run: left in
// place, the last failure reads as current on an account that is healthy.
func (t *txn) syncSettleOK(s *syncStamp) error {
	return t.asSyncWriter(s.actor, func() error {
		row, err := t.loadRow(s.ref, false)
		if err != nil || row == nil || row.DeletedAt != nil {
			return err
		}
		props := map[string]any{propLastSyncDurationMs: s.durationMs(t.now)}
		state, _ := row.Props[propSyncState].(string)
		if state == substrate.SyncStateRunning {
			state = substrate.SyncStateOK
			props[propSyncState] = state
		}
		if state == substrate.SyncStateOK && syncErrorBefore(row.Props, s.started) {
			props[propSyncError] = nil
			props[propSyncErrorAt] = nil
		}
		_, err = t.patch(s.ref, substrate.PatchInput{Properties: props})
		return err
	})
}

// syncErrorBefore reports whether the row holds an error pair written before
// `at`, so an error the run itself recorded beside its `ok` stays. A pair
// with no readable instant is old.
func syncErrorBefore(p map[string]any, at time.Time) bool {
	if p[propSyncError] == nil && p[propSyncErrorAt] == nil {
		return false
	}
	errAt := syncTime(p, propSyncErrorAt)
	return errAt == nil || errAt.Before(at)
}

// syncClearOnRecovery completes a write by the kind's own package that
// finishes a healthy run: one that moves `lastSyncedAt` and leaves
// `syncState` at `ok` drops an error pair written before that instant, in the
// same write. A schedule-fired sync names no record, so the dispatcher's
// settle never sees it (0085), and without this an account that recovers on
// the schedule would show its last failure as current forever. A write that
// names either half of the pair has said what it means and keeps it, and
// another package's write is not a run of this sync (syncStampFor).
func (t *txn) syncClearOnRecovery(sp *applySpec) {
	if sp.existing == nil || !sp.ty.Implements(vocabulary.TraitSyncCore) || !t.writesAsPackage(sp.ty.Package) {
		return
	}
	if _, ok := sp.props[propSyncError]; ok {
		return
	}
	if _, ok := sp.props[propSyncErrorAt]; ok {
		return
	}
	synced := syncTime(sp.props, propLastSyncedAt)
	if synced == nil || jsonEqual(sp.props[propLastSyncedAt], sp.existing.Props[propLastSyncedAt]) {
		return
	}
	state, ok := sp.props[propSyncState].(string)
	if _, named := sp.props[propSyncState]; !named {
		state, ok = sp.existing.Props[propSyncState].(string)
	}
	if !ok || state != substrate.SyncStateOK || !syncErrorBefore(sp.existing.Props, *synced) {
		return
	}
	sp.props[propSyncError] = nil
	sp.props[propSyncErrorAt] = nil
}

// writesAsPackage reports whether the transaction writes under one of pkg's
// own hands: the package's bundle actor, or a function or agent it declares.
func (t *txn) writesAsPackage(pkg string) bool {
	authority, name := vocabulary.SplitPackageRef(pkg)
	a := string(t.actor)
	if a == string(substrate.BundleActor(authority, name)) {
		return true
	}
	suffix := authority + ":" + name + ":"
	return strings.HasPrefix(a, substrate.FunctionActorPrefix+suffix) ||
		strings.HasPrefix(a, substrate.AgentActorPrefix+suffix)
}

// syncClearErrorOnReconnect rides the OAuth facility's reconnect: a fresh
// grant replaces the one the last error was about, so the pair goes with it.
// The pair is `writer: connector`, which the facility's own actor may not
// write, so the clear is written under the kind's package actor at the
// bundle tier, the hand the open-time sweep uses. `syncState` is left alone:
// the next run is what says whether the account is healthy.
func (t *txn) syncClearErrorOnReconnect(ty *vocabulary.Kind, row *erow) error {
	if !ty.Implements(vocabulary.TraitSyncCore) {
		return nil
	}
	if row.Props[propSyncError] == nil && row.Props[propSyncErrorAt] == nil {
		return nil
	}
	authority, pkg := vocabulary.SplitPackageRef(ty.Package)
	actor := substrate.BundleActor(authority, pkg)
	return t.asSyncWriter(actor, func() error {
		_, err := t.patch(eref{Kind: ty.Identity, ID: row.ID}, substrate.PatchInput{Properties: map[string]any{
			propSyncError:   nil,
			propSyncErrorAt: nil,
		}})
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
// trigger whose source names the record's kind, and the parked deliveries
// of its sync with the newest one's reason, on the account and on the
// stream each parked trigger's callable declares. The triggers half is the
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
	reg := ds.registry()
	for _, ty := range kinds {
		var onKind []substrate.TriggerStatus
		var triggerIDs []string
		callables := map[string]bool{}
		// streamOf is the stream each trigger's CURRENT callable declares. A
		// park row names a trigger, never a function, so an edit that points
		// a trigger at another callable relabels the trigger's older parks
		// with the new callable's stream.
		streamOf := map[string]string{}
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
					callables[lt.CallableID] = true
					streamOf[lt.ID] = triggerStream(reg, lt.trigger)
					break
				}
			}
		}
		// A schedule or webhook trigger names no kind, but one that fires a
		// callable the kind's record triggers fire runs the same sync: a
		// provider's hourly tick syncs every due account. Its parks carry no
		// record, so they count on every record of the kind: one slow
		// account's park reads as `erroring` on every account of the
		// provider, and on the callable's stream of every account that
		// lists it, until each completes a later run (parkedSyncState,
		// parkedStreamStates).
		for _, lt := range triggers {
			if lt.Record == nil && lt.Err == nil && callables[lt.CallableID] {
				triggerIDs = append(triggerIDs, lt.ID)
				streamOf[lt.ID] = triggerStream(reg, lt.trigger)
			}
		}
		parks, err := ds.syncParks(ctx, triggerIDs)
		if err != nil {
			return nil, err
		}
		// byRecord is every park of a record summed, the "" key holding the
		// parks that named none; byStream is the newest park per record and
		// stream, for the triggers whose callable declares a stream.
		byRecord := map[string]syncParkGroup{}
		byStream := map[string]map[string]syncParkGroup{}
		for k, g := range parks {
			byRecord[k.record] = byRecord[k.record].add(g)
			stream := streamOf[k.trigger]
			if stream == "" {
				continue
			}
			if byStream[k.record] == nil {
				byStream[k.record] = map[string]syncParkGroup{}
			}
			byStream[k.record][stream] = byStream[k.record][stream].add(g)
		}
		shared := byRecord[""]
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
			own := byRecord[row.ID]
			st.Parked = own.count + shared.count
			// The account's own park wins a tie with a shared one: a park
			// that named this record is the surer word about it.
			latest := own
			if shared.at.After(latest.at) {
				latest = shared
			}
			if st.Parked > 0 {
				st.LastParkedError, st.LastParkedAt = parkedReason(latest.lastError, latest.at)
				parkedSyncState(&st)
				parkedStreamStates(&st, byStream[row.ID], byStream[""])
			}
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

// parkedSyncState makes a status whose deliveries parked after its last
// completed run say so: `erroring`, with the newest park's reason as its
// error and message. A fire the runner kills stamps nothing, so the
// record's own state is only what its last completed run said, and the
// parks are the only evidence of the fires since. A park older than the
// last completed run, or than the error the record itself holds, changes
// nothing: the record's word is the newer.
func parkedSyncState(st *substrate.SyncStatus) {
	if st.LastParkedAt == nil {
		return
	}
	if st.LastSyncedAt != nil && !st.LastParkedAt.After(*st.LastSyncedAt) {
		return
	}
	own := st.State == substrate.SyncStateErroring || st.State == substrate.SyncStateThrottled
	if own && st.ErrorAt != nil && !st.LastParkedAt.After(*st.ErrorAt) {
		return
	}
	st.State = substrate.SyncStateErroring
	st.Error, st.ErrorAt = "the sync's deliveries are parked: "+st.LastParkedError, st.LastParkedAt
	st.Message = st.Error
}

// parkedStreamStates marks each stream whose newest park is later than the
// stream's own last completed run (`lastAt`) as `erroring`, with the park's
// reason as its message, and leaves every other stream as the body stamped
// it. own is the parks that named this record, shared the parks that named
// none (a schedule fire), each keyed by the stream of the trigger's
// callable; a callable that declares no stream is in neither, and marks the
// account alone. A stream the record does not list yet is added for a park
// that named the record, which ran for this account; a shared park marks
// only a stream the record already lists, because a fire that named no
// account may never have run that stream for this one.
func parkedStreamStates(st *substrate.SyncStatus, own, shared map[string]syncParkGroup) {
	newest := make(map[string]syncParkGroup, len(own)+len(shared))
	for name, g := range own {
		newest[name] = g
	}
	for name, g := range shared {
		_, listed := st.Streams[name]
		if _, named := newest[name]; listed || named {
			newest[name] = newest[name].add(g)
		}
	}
	for name, g := range newest {
		s := st.Streams[name]
		if s.LastAt != nil && !g.at.After(*s.LastAt) {
			continue
		}
		if st.Streams == nil {
			st.Streams = map[string]substrate.SyncStream{}
		}
		s.State = substrate.SyncStateErroring
		s.Message, _ = parkedReason(g.lastError, g.at)
		st.Streams[name] = s
	}
}

// syncParkGroup is the parked deliveries of one (record, trigger) pair, or a
// sum of such groups: how many there are, and the newest one's error,
// instant and row id.
type syncParkGroup struct {
	count     int64
	lastError string
	at        time.Time
	id        int64
}

// add sums two groups and keeps the newer one's error: the later park, the
// higher row id on a tie, so a sum read out of a map is the same on every
// read.
func (g syncParkGroup) add(o syncParkGroup) syncParkGroup {
	sum := g
	if o.count > 0 && (g.count == 0 || o.at.After(g.at) || (o.at.Equal(g.at) && o.id > g.id)) {
		sum = o
	}
	sum.count = g.count + o.count
	return sum
}

// syncParkKey is the pair a park is grouped by: the record it named ("" for
// a fire that named none) and the trigger that parked it.
type syncParkKey struct {
	record  string
	trigger string
}

// triggerStream is the sync stream a trigger's callable declares, resolved
// against the registry so a trigger whose bundle is blocked still names its
// stream. An agent declares none.
func triggerStream(reg *vocabulary.Registry, tr *trigger) string {
	if tr.Callable != nil {
		return tr.Callable.Stream
	}
	if tr.CallableKind == callableKindAgent {
		return ""
	}
	if fn, err := reg.ResolveFunction(tr.CallableID); err == nil {
		return fn.Stream
	}
	return ""
}

// syncParks groups the parked deliveries of the given triggers by the record
// they name and the trigger that parked them, in one query. A webhook
// request still settling is pending, not parked, and is left out, as
// TriggerStatuses leaves it out of `parked`.
func (ds *dataset) syncParks(ctx context.Context, triggerIDs []string) (map[syncParkKey]syncParkGroup, error) {
	out := map[syncParkKey]syncParkGroup{}
	if len(triggerIDs) == 0 {
		return out, nil
	}
	ids, err := json.Marshal(triggerIDs)
	if err != nil {
		return nil, err
	}
	// A row this process is delivering is in flight, not parked
	// (presentFailure).
	running, err := ds.runningFailureIDs()
	if err != nil {
		return nil, err
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT DISTINCT ON (record_id, trigger_id) record_id, trigger_id,
		       count(*) OVER (PARTITION BY record_id, trigger_id), last_error, parked_at, id
		FROM trigger_failures
		WHERE trigger_id IN (SELECT jsonb_array_elements_text($1::jsonb)) AND last_error <> $2
		  AND id NOT IN (SELECT jsonb_array_elements_text($3::jsonb)::bigint)
		ORDER BY record_id, trigger_id, parked_at DESC, id DESC`, ids, pendingWebhookError, running)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k syncParkKey
		var g syncParkGroup
		if err := rows.Scan(&k.record, &k.trigger, &g.count, &g.lastError, &g.at, &g.id); err != nil {
			return nil, err
		}
		g.lastError = heldError(g.lastError)
		out[k] = g
	}
	return out, rows.Err()
}

// parkedReason is a parked delivery's error as a status carries it: the first
// line, bounded as a park bounds the record's syncError, and the instant.
func parkedReason(lastError string, at time.Time) (string, *time.Time) {
	u := at.UTC()
	return boundText(firstLine(lastError), syncErrorMax), &u
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
