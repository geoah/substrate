package engine

// THE OBSERVED INDEX (decision 0160, migration 0013).
//
// Tables derived from the rows of every kind that binds core's `observed`
// trait, the way `refs` is derived. observed_streams gives each stream a
// private id; observed_segments keeps one row per record (its stream, its
// extent, what it holds and a fingerprint), never one per point: a record's
// points stay in its properties and every reader derives them again with
// deriveObserved. observed_rollups holds one row per stream form, tier,
// bucket and group, and observed_latest one per stream. A segment is a
// function of one record's properties and its kind's binding; a rollup row
// of its stream form's points in its bucket (a counter's also of the reading
// before it, decision 0157) and of the tiers its metric names; a latest row
// of its stream's points. Nothing else enters, no clock: that is what lets
// `repository rebuild` clear the tables and the background pass reproduce
// them from the records.
//
// The fold records what each written record derives (syncObserved, from
// foldRecordOp, and dropObservedOf, from the tombstone and the purge), and
// settleObserved applies it once, at the end of the transaction, under the
// observed lock: the segments whose fingerprint moved, then the rollup rows
// of every bucket of every tier their old and new points meet, then the
// latest rows of their streams. inTx and inRawTx call it. A replay derives
// nothing: the rebuild and the boot import queue every observed kind for
// the background re-derivation (queueObservedRederivation).
//
// A stream is (metric, kind, source, about, channel) and a summary's
// statistic (obsKey), so a night's average and its maximum keep a newest
// point each. A stream form is a stream's points of one form: what a gauge's
// hold runs across and a counter steps across. A rollup row is one bucket of
// a stream form, for one group: a code, a unit, the delta flag, a UTC offset
// and whether the offset was recorded.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// observedIndexVersion names the rules the three tables are derived by:
// deriveObserved and observed.GaugeHold for the points, observed.BucketBuilder
// for the rollup rows, latestOrder and obsLatestKey for the latest rows. A
// change to any of them that changes what a stored row derives bumps it, and
// every repository's next open re-derives every row of every observed kind
// (checkObservedIndex). A change that only adds a column bumps it too, since
// the stored rows hold the column's default. Version 2 is the unit graph
// switch (decision 0186): no point carries a base value, and a circular
// point's angle reads its unit's turn. Version 3 adds each segment's
// earliest available instant (first_available), which the latest read's
// `first` reads.
const observedIndexVersion = 3

// checkObservedIndex is the open ladder's step: where observed_index records
// older rules than this binary's, or none (a repository written before
// migration 0013), it requests the background re-derivation of every row of
// every observed kind and records this binary's version, in one transaction.
// The request is the pass's durable work list (index_reprojections), so a
// pass a shutdown stops resumes at the next open, and a row a write lands on
// meanwhile derives under these rules anyway. A rebuild and a boot import
// derive the tables whole and record the version themselves
// (rederiveObservedAll), so the open after one requests nothing.
//
// A read-only process skips it and serves the index it finds. A newer
// version than the binary's is left alone, as checkSearchIndex leaves one.
func (ds *dataset) checkObservedIndex(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	var have int
	err := ds.db.QueryRowContext(ctx, `SELECT version FROM observed_index`).Scan(&have)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		have = 0
	case err != nil:
		return fmt.Errorf("substrate/engine: repository %s: read the observed index version: %w", ds.info.ID, err)
	}
	if have >= observedIndexVersion {
		return nil
	}
	var kinds []string
	err = ds.inRawTx(ctx, func(t *txn) error {
		var err error
		if kinds, err = t.observedKindsHoldingRows(); err != nil {
			return err
		}
		owed := make([]indexReprojection, len(kinds))
		for i, kind := range kinds {
			owed[i] = indexReprojection{kind: kind, observed: indexFilter{moved: true, every: true}}
		}
		if err := t.requestReprojections(owed); err != nil {
			return err
		}
		return t.markObservedIndexed()
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: repository %s: request the observed index re-derivation: %w", ds.info.ID, err)
	}
	if len(kinds) > 0 {
		ds.svc.log.Info("substrate: the observed index was derived under older rules; re-deriving it behind the open",
			"repository", logSafeID(ds.info.ID), "from", have, "to", observedIndexVersion, "kinds", len(kinds))
	}
	return nil
}

// observedKindsHoldingRows is every kind whose stored declaration binds
// core's `observed` and that holds a row, live or tombstoned. It reads the
// declaration rows rather than the registry, so a parked package's kinds,
// whose rows the fold still derives (fold.go foldView), are in it.
func (t *txn) observedKindsHoldingRows() ([]string, error) {
	rows, err := t.query(`
		SELECT d.id FROM records d
		WHERE d.kind = $1 AND d.deleted_at IS NULL
		  AND EXISTS (
			SELECT 1 FROM jsonb_array_elements_text(
				CASE WHEN jsonb_typeof(d.props->'traits') = 'array' THEN d.props->'traits' ELSE '[]'::jsonb END) tr
			WHERE tr = $2 OR starts_with(tr, $2 || '('))
		  AND EXISTS (SELECT 1 FROM records r WHERE r.kind = d.id)
		ORDER BY d.id`, kindKind, vocabulary.TraitObservedCore)
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, kind)
	}
	return out, finishRows(rows)
}

// queueObservedRederivation is a replay's last step. The replay derived no
// observed row, so every row of every observed kind is owed the background
// re-derivation, which the rebuild or the import starts once it commits, and
// the index is recorded as this binary's rules': the work that makes it so is
// queued, and an aggregate read over a kind it owes is refused until it is
// done (checkObservedReady). It answers how many kinds it queued.
func (t *txn) queueObservedRederivation() (int, error) {
	kinds, err := t.observedKindsHoldingRows()
	if err != nil {
		return 0, err
	}
	owed := make([]indexReprojection, len(kinds))
	for i, kind := range kinds {
		owed[i] = indexReprojection{kind: kind, observed: indexFilter{moved: true, every: true}}
	}
	if err := t.requestReprojections(owed); err != nil {
		return 0, err
	}
	return len(kinds), t.markObservedIndexed()
}

// markObservedIndexed records that the three tables were derived under this
// binary's rules.
func (t *txn) markObservedIndexed() error {
	_, err := t.exec(`
		INSERT INTO observed_index (version) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET version = EXCLUDED.version, derived_at = now()`,
		observedIndexVersion)
	return err
}

// obsStream is a stream's references and channel: what a gauge's hold and a
// counter's steps run across beside the form, and what a read groups by.
type obsStream struct {
	metric, kind, source, about, channel string
}

// obsKey is one row of observed_streams: a stream and a summary's statistic,
// "" for every other form, so a night's average and its maximum are two
// streams with a newest point each.
type obsKey struct {
	obsStream
	statistic string
}

// obsSF is one stream id's points of one form: what a rollup row belongs to.
type obsSF struct {
	stream int64
	form   string
}

// obsSpan is a closed range of instants [lo, hi] a transaction's segment
// writes touched.
type obsSpan struct{ lo, hi time.Time }

// observedTouch is what a transaction's observed writes left to settle.
type observedTouch struct {
	// all is a replay's: it derives nothing, since a rebuild clears the
	// tables and leaves every row to the background re-derivation.
	all bool
	// ops is the derived rows of each record the transaction wrote, nil for
	// one that derives none (a tombstone, an unbound kind), applied to the
	// segments once at the end of the transaction (settleObserved). The last
	// write of a record in the transaction is the one that counts.
	ops map[eref]*observedRows
	// force re-derives a segment whose fingerprint did not move: the
	// background pass, whose rules or tiers may have.
	force bool
	// metrics are the metric record paths whose rollup tiers the
	// transaction changed, owed a background re-derivation of their rows.
	metrics map[string]bool
}

func (t *txn) observedTouched() *observedTouch {
	if t.observed == nil {
		t.observed = &observedTouch{ops: map[eref]*observedRows{}, metrics: map[string]bool{}}
	}
	return t.observed
}

// bindsObserved reports whether a kind binds core's `observed`.
func bindsObserved(ty *vocabulary.Kind) bool {
	if ty == nil {
		return false
	}
	_, ok := observedBinding(ty)
	return ok
}

// observedShape is the part of a kind's declaration the derivation reads:
// the variant it binds. Two declarations with the same shape derive the same
// points from the same row, so a vocabulary change re-derives a kind's points
// only where it moves (reprojection.go).
func observedShape(reg kindLookup, ident string) string {
	ty, ok := reg.ByIdentity(ident)
	if !ok || !bindsObserved(ty) {
		return ""
	}
	b, _ := observedBinding(ty)
	return b.Variant
}

// observedRows is what one record derives: its stream, its form, the
// columns every point of the record shares, and its points.
type observedRows struct {
	key       obsKey
	form      string
	id        string
	unit      string
	delta     bool
	holdMs    *int64
	resetRule *string
	resetAt   *time.Time
	wrapAt    *string
	circular  bool
	points    []obsPoint
	// turn is one full circle in the row's unit as the repository's unit
	// graph reads it (decision 0186), nil for a unit with none, which a
	// circular point's angle divides by.
	turn *float64
}

// obsPoint is one point of a record. offsetKnown is false where the record
// carried no UTC offset and offset is 0 for want of one.
type obsPoint struct {
	ord         int
	ts, tsEnd   time.Time
	offset      int64
	offsetKnown bool
	value       *float64
	exact       *string
	code        string
	// comparator is the quantity's comparator (`<0.01`), "" for an exact
	// number: it rides on the point, so a reader answers a censored value
	// with it and never as its bound alone.
	comparator string
}

// available is vocabulary.ObservedPointAvailable, the definition every reader
// of the index shares; availablePoint is the same test in SQL.
func (p obsPoint) available() bool {
	return vocabulary.ObservedPointAvailable(p.value != nil, p.code)
}

// deriveObserved computes a live row's points from its properties and its
// kind's binding alone. It is lenient on purpose: a row the write path
// admitted derives what it can read, and anything it cannot read derives no
// point rather than failing a replay. A single record derives a point only
// when it holds a value or a code; one that says why it has none
// (`absentReason`) or keeps a reading as received derives nothing.
//
// units is the repository's unit graph, which a circular point reads its
// unit's turn from; a nil graph holds no turn.
func deriveObserved(ty *vocabulary.Kind, row *erow, units *vocabulary.UnitGraph) *observedRows {
	if !bindsObserved(ty) || row == nil || row.At == nil || row.DeletedAt != nil {
		return nil
	}
	b, _ := observedBinding(ty)
	props := row.Props
	form, _ := props["form"].(string)
	if !slices.Contains(vocabulary.ObservedForms, form) {
		return nil
	}
	r := &observedRows{id: row.ID, form: form, key: obsKey{obsStream: obsStream{
		metric:  referencePathOf(props["metric"]),
		kind:    row.Kind,
		source:  referencePathOf(props["source"]),
		about:   referencePathOf(props["about"]),
		channel: stringProp(props, "channel"),
	}, statistic: stringProp(props, "statistic")}}
	if r.key.metric == "" {
		return nil
	}
	r.delta, _ = props["delta"].(bool)
	r.circular, _ = props["circular"].(bool)
	if h, err := vocabulary.ParseISODuration(stringProp(props, "hold")); err == nil && stringProp(props, "hold") != "" {
		ms := h.Milliseconds()
		r.holdMs = &ms
	}
	r.resetRule = optString(props, "resetRule")
	r.resetAt = parseTimeValue(stringProp(props, "resetAt"))
	r.wrapAt = optString(props, "wrapAt")
	switch b.Variant {
	case vocabulary.ObservedSingle:
		p, unit, ok := singlePoint(row)
		if !ok {
			return nil
		}
		r.unit = unit
		r.points = []obsPoint{p}
		if form == vocabulary.FormGauge {
			r.holdMs = gaugeHoldMs(p, r.holdMs)
		}
	case vocabulary.ObservedPacked:
		r.unit = stringProp(props, "unit")
		r.points = tracePoints(row, r)
	default:
		return nil
	}
	if len(r.points) == 0 {
		return nil
	}
	if units != nil && r.circular {
		if turn := units.Turn(r.unit); turn != nil {
			if f, _ := turn.Float64(); f > 0 {
				r.turn = &f
			}
		}
	}
	return r
}

// singlePoint is a single record's one point: its value or its code, at
// `at`, spanning to `endsAt` where it has one. A gauge's value holds no
// longer than that span (gaugeHoldMs).
func singlePoint(row *erow) (obsPoint, string, bool) {
	props := row.Props
	p := obsPoint{ts: *row.At, tsEnd: *row.At, code: stringProp(props, "code")}
	if row.EndsAt != nil && row.EndsAt.After(*row.At) {
		p.tsEnd = *row.EndsAt
	}
	if off, ok := vocabulary.ObservedOffsetOf(props); ok {
		p.offset, p.offsetKnown = off, true
	}
	var unit string
	if q, ok := props["value"].(map[string]any); ok {
		digits, _ := q[vocabulary.QuantityValue].(string)
		unit, _ = q[vocabulary.QuantityUnit].(string)
		if f, err := strconv.ParseFloat(digits, 64); err == nil && digits != "" {
			p.value, p.exact = &f, &digits
			p.comparator, _ = q[vocabulary.QuantityComparator].(string)
		}
	}
	if p.value == nil {
		unit = ""
	}
	return p, unit, p.value != nil || p.code != ""
}

// gaugeHoldMs is a single gauge's hold in milliseconds, its endsAt included
// (observed.GaugeHold).
func gaugeHoldMs(p obsPoint, holdMs *int64) *int64 {
	var hold *time.Duration
	if holdMs != nil {
		d := time.Duration(*holdMs) * time.Millisecond
		hold = &d
	}
	h := observed.GaugeHold(p.ts, p.tsEnd, hold)
	if h == nil {
		return nil
	}
	ms := h.Milliseconds()
	return &ms
}

// tracePoints expands a packed record into its points. Every point takes the
// record's one `utcOffset`, and none is known where the record has none: the
// record's ends say nothing about when or how often the offset changed inside
// the period, so a writer splits a trace where the offset changes (decision
// 0154) rather than the index guessing a transition. A gauge's and a
// counter's point is an instant, and an amount's spans to the next point (to
// `at` plus `every` with regular spacing, to `endsAt` for the last).
func tracePoints(row *erow, r *observedRows) []obsPoint {
	props := row.Props
	pts, err := vocabulary.TracePoints(*row.At, props)
	if err != nil {
		return nil
	}
	end := *row.At
	if row.EndsAt != nil {
		end = *row.EndsAt
	}
	off, known := vocabulary.ObservedOffsetOf(props)
	var every time.Duration
	if s := stringProp(props, "every"); s != "" {
		every, _ = vocabulary.ParseISODuration(s)
	}
	out := make([]obsPoint, 0, len(pts))
	for i, tp := range pts {
		p := obsPoint{ord: i, ts: tp.At, tsEnd: tp.At, code: tp.Code, offset: off, offsetKnown: known}
		if r.form == vocabulary.FormAmount {
			switch {
			case every > 0:
				p.tsEnd = tp.At.Add(every)
			case i+1 < len(pts):
				p.tsEnd = pts[i+1].At
			default:
				p.tsEnd = end
			}
			if p.tsEnd.After(end) {
				p.tsEnd = end
			}
		}
		if tp.Value != "" {
			f, err := strconv.ParseFloat(tp.Value, 64)
			if err != nil {
				continue
			}
			digits := tp.Value
			p.value, p.exact = &f, &digits
		}
		out = append(out, p)
	}
	return out
}

func stringProp(props map[string]any, name string) string {
	s, _ := props[name].(string)
	return s
}

func optString(props map[string]any, name string) *string {
	if s, ok := props[name].(string); ok && s != "" {
		return &s
	}
	return nil
}

// --- the fold's hooks ---

// syncObserved records what a record's row now derives, for settleObserved
// to apply to its segment at the end of the transaction. A tombstoned row
// derives nothing, and neither does a kind that does not bind the trait, so
// the segment such a row left goes.
func (t *txn) syncObserved(ref eref, ty *vocabulary.Kind, row *erow) error {
	touch := t.observedTouched()
	if touch.all {
		return nil
	}
	var rows *observedRows
	if bindsObserved(ty) && row.DeletedAt == nil {
		units, err := t.observedUnits()
		if err != nil {
			return err
		}
		rows = deriveObserved(ty, row, units)
	}
	touch.ops[ref] = rows
	return nil
}

// derivesObserved reports whether a row of the kind can hold a segment: a
// kind that binds the trait, or one this binary no longer declares, whose
// stale segment syncObserved removes. A replay derives nothing: the rebuild
// leaves every row to the background re-derivation.
func (t *txn) derivesObserved(ty *vocabulary.Kind) bool {
	if t.observed != nil && t.observed.all {
		return false
	}
	return ty == nil || bindsObserved(ty)
}

// dropObservedOf removes the segment of a record leaving the fold (a
// tombstone, a purge) whatever its kind's declaration now says. A kind that
// stopped binding `observed` keeps its rows' old segments until the
// background pass reaches each row (reprojection.go), and a purge removes the
// row that pass would have found, so the record's own exit is the last moment
// its segment can go. A kind that does not bind the trait asks the primary
// key first, so an ordinary record costs one probe and takes no observed
// lock. A replay derives nothing, so it drops nothing.
func (t *txn) dropObservedOf(ref eref) error {
	if t.observed != nil && t.observed.all {
		return nil
	}
	ty, _ := t.derivationView().ByIdentity(ref.Kind)
	if !bindsObserved(ty) {
		var holds bool
		if err := t.row(`SELECT EXISTS (SELECT 1 FROM observed_segments WHERE kind = $1 AND record_id = $2)`,
			ref.Kind, ref.ID).Scan(&holds); err != nil {
			return fmt.Errorf("substrate/engine: observed segment of %s %s: %w", ref.Kind, ref.ID, err)
		}
		if !holds {
			return nil
		}
	}
	t.observedTouched().ops[ref] = nil
	return nil
}

// bindsMetric reports whether a kind binds core's `metric`, whose records'
// `rollups` name the tiers their streams are rolled up in.
func bindsMetric(ty *vocabulary.Kind) bool {
	return ty != nil && ty.Implements(vocabulary.TraitMetricCore)
}

// noteMetricTiers records a metric record whose rollup tiers a write moved,
// from before to what row now names: the rows of its streams are owed a
// background re-derivation (settleObserved queues it), since the tiers each
// row was rolled up under are the metric's. A replay notes nothing; the
// rebuild re-derives every row.
func (t *txn) noteMetricTiers(ref eref, before []time.Duration, row *erow) {
	if t.observed != nil && t.observed.all {
		return
	}
	if slices.Equal(before, rowTiers(row)) {
		return
	}
	t.observedTouched().metrics[vocabulary.RecordPath(ref.Kind, ref.ID)] = true
}

// metricTiersOf is the rollup tiers a record of a metric kind names before a
// tombstone or a purge takes it, nil for a record of any other kind.
func (t *txn) metricTiersOf(ref eref) ([]time.Duration, error) {
	if t.observed != nil && t.observed.all {
		return nil, nil
	}
	ty, _ := t.derivationView().ByIdentity(ref.Kind)
	if !bindsMetric(ty) {
		return nil, nil
	}
	row, err := t.loadRow(ref, false)
	if err != nil {
		return nil, err
	}
	return rowTiers(row), nil
}

// rowTiers are the rollup widths a metric record names, the default where it
// is missing or deleted, which is what a stream of it is then rolled up in.
func rowTiers(row *erow) []time.Duration {
	if row == nil || row.DeletedAt != nil {
		return vocabulary.MetricContractOf(map[string]any{}).Rollups
	}
	return vocabulary.MetricContractOf(row.Props).Rollups
}

// observedUnits is the graph the transaction derives observed points
// through.
func (t *txn) observedUnits() (*vocabulary.UnitGraph, error) {
	at, err := t.unitGraph()
	if err != nil {
		return nil, err
	}
	return at.graph, nil
}
