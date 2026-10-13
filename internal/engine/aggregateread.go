package engine

// The aggregate read's run (aggregate.go): the state of one metric read, the
// evidence each bucket and group reduces, and the reduction of every group
// into rows. aggregateplan.go decides where each group is served from,
// aggregatereads.go reads the index, aggregateraw.go and aggregaterollup.go
// fill the evidence from raw points and from rollup rows, and
// aggregatevalues.go answers the reducers.

import (
	"context"
	"database/sql"
	"math/big"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggRun is one metric read in progress.
type aggRun struct {
	ds      *dataset
	ctx     context.Context
	tx      *sql.Tx
	p       *aggPlan
	u       *aggUnits
	buckets []*aggBucket
	meta    aggMeta
	// rows are each rollup-served stream's rows, by stream.
	rows map[*aggStream][]observed.Row
	// rowGroups maps a rollup row's group key to its group.
	rowGroups map[string]obsGroup
	// carries are the points current at each window's start, by stream and
	// in segments order, for a rollup-served gauge.
	carries map[*aggStream][]*observed.Point
	// series are the raw points read, by stream.
	series map[*aggStream]*aggSeries
	// windows are the spans the read takes points from (aggPlan.windows);
	// rowSpans are the rollup rows they meet, and fragments the bucket ends
	// that cut a row (plan).
	windows, rowSpans, fragments [][2]time.Time
	// bounds are the records mode's window starts and ends, sorted, where a
	// carry walked across empty rows is cut (chunkEnd).
	bounds []time.Time
	// reach is a tree over the records-mode windows in start order, each
	// node the latest end of the windows below it (windowsMeeting).
	reach []time.Time
	// spanReach is, in the records mode, how far before a window each
	// stream's amounts may start and reach into it (amountReach).
	spanReach map[obsStream]time.Duration
	// nSeries is the series the read was budgeted for (seriesOf).
	nSeries int
	// samples is the raw points and rollup rows the read's statements have
	// counted so far, against maxSamples (charge).
	samples int64
	// steps counts the reduction's steps between context checks (canceled).
	steps everyThousand
}

// aggSeries is one stream's raw points: each span read, with the point
// current at its start and, for a counter, the last available point before
// it.
type aggSeries struct {
	segs []aggSegment
}

// aggSegment is one span of a stream's raw points.
type aggSegment struct {
	from, to time.Time
	carry    *aggPoint
	prev     *aggPoint
	points   []aggPoint
}

// aggEvidence is what one row reduces: raw members and held spans, or merged
// rollup statistics and held sums.
type aggEvidence struct {
	form     string
	raw      bool
	members  []aggPoint
	spans    []observed.Span
	stats    observed.Stats
	held     map[string]observed.HeldSum
	steps    aggSteps
	sources  map[string]bool
	coverage int64
	// extents are a rollup-served amount's pieces of coverage, joined into
	// coverage once every row and cut end has merged (amountCoverage).
	extents []observed.Extent
	// firstStream and lastStream are the streams stats.First and stats.Last
	// came from, the last key of pointBefore.
	firstStream, lastStream int
	count                   int64
	bounded                 int64
	// offsetless is the members placed by the fallback zone.
	offsetless int64
	// prorated is the members a records-mode window cut, each counted by
	// its part inside the window.
	prorated int64
	// ends are a recorded-local row's own first and last points and held
	// time, which its start and end are written at.
	ends rowEnds
}

// aggSteps are a counter's steps in one row: their increase, exactly, the
// resets among them and the time they span.
type aggSteps struct {
	inc     *big.Rat
	resets  int64
	covered int64
	any     bool
}

func (s *aggSteps) add(d *big.Rat, reset bool, covered int64) {
	if s.inc == nil {
		s.inc = new(big.Rat)
	}
	s.inc.Add(s.inc, d)
	if reset {
		s.resets++
	}
	s.covered += covered
	s.any = true
}

func newEvidence(form string, raw bool) *aggEvidence {
	return &aggEvidence{form: form, raw: raw, sources: map[string]bool{}, held: map[string]observed.HeldSum{}}
}

// aggOut is one output row before fill and rolling.
type aggOut struct {
	bucket int
	group  *aggGroup
	code   string
	row    substrate.AggregateRow
}

// eval reduces every group of the run into its cells, raw and summary, by
// bucket and code. Every group of every metric of the read is evaluated
// before any row is written, because a recorded-local read creates buckets
// as it goes (aggregateMetric).
func (r *aggRun) eval(groups []*aggGroup) (raws, sums []aggCells, err error) {
	raws, sums = make([]aggCells, len(groups)), make([]aggCells, len(groups))
	for gi, g := range groups {
		if len(g.raw) > 0 && r.p.source != substrate.AggregateFromSummaries {
			if g.rollups {
				raws[gi], err = r.evalRollups(g)
			} else {
				raws[gi], err = r.evalRaw(g, g.raw, g.form)
			}
			if err != nil {
				return nil, nil, err
			}
		}
		if len(g.sums) > 0 && r.p.source != substrate.AggregateFromRaw {
			if sums[gi], err = r.evalRaw(g, g.sums, vocabulary.FormSummary); err != nil {
				return nil, nil, err
			}
		}
	}
	return raws, sums, nil
}

// assemble writes the rows of every group's cells, in bucket order over the
// read's buckets, and counts the series (a group and a code) that hold a
// row, which the read holds to maxGroups.
func (r *aggRun) assemble(groups []*aggGroup, raws, sums []aggCells) (out []aggOut, series int, err error) {
	for gi, g := range groups {
		seen := map[string]bool{}
		for bi, b := range r.buckets {
			if err := r.canceled(); err != nil {
				return nil, 0, err
			}
			codes := map[string]bool{}
			for c := range raws[gi][b] {
				codes[c] = true
			}
			for c := range sums[gi][b] {
				codes[c] = true
			}
			for _, code := range sortedKeys(codes) {
				ev, from := r.choose(b, raws[gi][b][code], sums[gi][b][code])
				if ev == nil {
					continue
				}
				if !seen[code] {
					seen[code] = true
					series++
				}
				row, err := r.row(b, g, code, ev, from)
				if err != nil {
					return nil, 0, err
				}
				out = append(out, aggOut{bucket: bi, group: g, code: code, row: row})
			}
		}
	}
	return out, series, nil
}

// choose picks a bucket's evidence: raw points or summaries, never both;
// prefer-raw takes raw where the bucket holds a raw point and its coverage
// reaches minCoverage. A gauge's coverage counts a value held in from before
// the bucket, which alone is no reading of it.
func (r *aggRun) choose(b *aggBucket, raw, sums *aggEvidence) (*aggEvidence, string) {
	switch {
	case raw == nil && sums == nil:
		return nil, ""
	case sums == nil:
		return raw, substrate.AggregateFromRaw
	case raw == nil:
		return sums, substrate.AggregateFromSummaries
	}
	from, to := r.p.rangeOf(b)
	if length := b.length(&raw.ends, from, to).Milliseconds(); raw.count > 0 && length > 0 && float64(raw.coverage)/float64(length) >= r.p.minCoverage {
		return raw, substrate.AggregateFromRaw
	}
	return sums, substrate.AggregateFromSummaries
}

// codeKey is the row a point or a rollup row of a code lands in.
func (r *aggRun) codeKey(code string) string {
	if r.p.grouped[substrate.GroupCode] {
		return code
	}
	return ""
}

// admits reports whether filter.properties.code lets a code through.
func (r *aggRun) admits(code string) bool { return r.p.codes == nil || r.p.codes[code] }

// aggCells holds the evidence of each bucket and code. It is keyed by the
// bucket itself: a recorded-local read creates buckets as points name them.
type aggCells map[*aggBucket]map[string]*aggEvidence

func cell(m aggCells, b *aggBucket, code, form string, raw bool) *aggEvidence {
	if m[b] == nil {
		m[b] = map[string]*aggEvidence{}
	}
	ev := m[b][code]
	if ev == nil {
		ev = newEvidence(form, raw)
		m[b][code] = ev
	}
	return ev
}

// canceled counts one step of the reduction and answers the read's context
// error on the first step and every thousand after it, whichever loop takes
// them: the statement timeout bounds the SQL, not the work after it, so a
// long reduction stops when its caller does. A step is one point, span, row,
// slot or interval this package copies or walks, or one bucket or window a
// search compares; what runs in observed (a reducer, a sort, a priority
// resolution) checks the context through its Context forms, as often.
func (r *aggRun) canceled() error { return r.steps.check(r.ctx) }
