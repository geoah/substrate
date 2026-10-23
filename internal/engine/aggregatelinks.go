package engine

// LINKED SOURCES ARE ONE DEVICE (decision 0187).
//
// A record of a kind binding core's `sourcelink` names, in `sources`, the
// sources that are one device reaching the repository by several routes.
// Under the priority policy the read takes each device's values of one metric
// and `about` from one source: the highest-ranked among those whose streams
// hold a value in the read's range and `from`. Links join transitively. The
// graph is read through the refs index from the read's own sources outward,
// so a link no selected source reaches is never loaded, and every reference
// it visits is charged to maxSamples, each lookup bounded by what is left.

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// sourceLinks is one read's graph of linked sources, extended as each metric
// of the read brings sources it has not met.
type sourceLinks struct {
	reg   *vocabulary.Registry
	kinds []string
	// linked are the sources some link names.
	linked map[string]bool
	// parent is the union-find forest over source record paths.
	parent map[string]string
	// expanded are the sources whose links have been read, and links the
	// link records read.
	expanded map[string]bool
	links    map[string]bool
}

// newSourceLinks is the read's link graph, nil where the policy is not
// priority or no kind binds `sourcelink`.
func (ds *dataset) newSourceLinks(p *aggPlan) *sourceLinks {
	if p.policy != substrate.SourcesPriority {
		return nil
	}
	var kinds []string
	for _, k := range ds.registry().Kinds() {
		if k.Implements(vocabulary.TraitSourceLinkCore) {
			kinds = append(kinds, k.Identity)
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	slices.Sort(kinds)
	return &sourceLinks{
		reg: ds.registry(), kinds: kinds, linked: map[string]bool{},
		parent: map[string]string{}, expanded: map[string]bool{}, links: map[string]bool{},
	}
}

func (l *sourceLinks) find(x string) string {
	for {
		p, ok := l.parent[x]
		if !ok || p == x {
			return x
		}
		if gp, ok := l.parent[p]; ok {
			l.parent[x] = gp
		}
		x = p
	}
}

func (l *sourceLinks) union(a, b string) {
	if ra, rb := l.find(a), l.find(b); ra != rb {
		l.parent[rb] = ra
	}
}

// The link lookups, each one index range of refs bounded by a LIMIT of the
// samples the read has left plus one, so no statement reads past the budget
// and none sorts: linksOfSQL finds the link records naming one source
// (refs_dst_idx), and sourcesOfSQL the sources one link names (the primary
// key). liveLinksSQL keeps the links not deleted, bounded by the ids it is
// given.
const (
	linksOfSQL = `SELECT src_kind, src FROM refs
		WHERE dst_kind = $1 AND dst = $2 AND src_kind = ANY($3::text[]) AND property = $4 LIMIT $5`
	sourcesOfSQL = `SELECT dst_kind, dst FROM refs
		WHERE src_kind = $1 AND src = $2 AND property = $3 LIMIT $4`
	liveLinksSQL = `SELECT r.kind, r.id FROM unnest($1::text[], $2::text[]) AS t(kind, id)
		JOIN records r ON r.kind = t.kind AND r.id = t.id AND r.deleted_at IS NULL`
)

// expand reads the links reachable from the streams' sources that the graph
// has not read, a source at a time outward, and answers samples with every
// link reference it read added. Each lookup reads at most the samples left
// plus one, and one more than are left refuses the read. Only a source whose
// kind binds `source` links.
func (l *sourceLinks) expand(ctx context.Context, tx *sql.Tx, streams []*aggStream, samples, maxSamples int64) (int64, error) {
	reg := l.reg
	var queue []string
	for _, s := range streams {
		if kind, _, ok := vocabulary.SplitRecordPath(s.source); ok && !l.expanded[s.source] && bindsSource(reg, kind) {
			l.expanded[s.source] = true
			queue = append(queue, s.source)
		}
	}
	tr := traceOf(ctx)
	// lookup reads one bounded statement's pairs and charges them.
	lookup := func(query string, args ...any) ([][2]string, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		left := maxSamples - samples
		rows, err := tx.QueryContext(ctx, query, append(args, left+1)...)
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: aggregate source links: %w", err)
		}
		var out [][2]string
		for rows.Next() {
			var k, id string
			if err := rows.Scan(&k, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, [2]string{k, id})
		}
		if err := finishRows(rows); err != nil {
			return nil, err
		}
		if tr != nil {
			tr.linkRows += int64(len(out))
		}
		if samples += int64(len(out)); samples > maxSamples {
			return nil, refuse("aggregate.budget.maxSamples", "the source links the priority policy reads hold more references than the %d samples the read has left; narrow the filter, or raise maxSamples (the cap is %d)",
				left, substrate.AggregateMaxSamplesCap)
		}
		return out, nil
	}
	for len(queue) > 0 {
		src := queue[0]
		queue = queue[1:]
		kind, id, _ := vocabulary.SplitRecordPath(src)
		found, err := lookup(linksOfSQL, kind, id, l.kinds, vocabulary.SourceLinkSources)
		if err != nil {
			return samples, err
		}
		var kinds, ids []string
		for _, f := range found {
			if link := vocabulary.RecordPath(f[0], f[1]); !l.links[link] {
				l.links[link] = true
				kinds, ids = append(kinds, f[0]), append(ids, f[1])
			}
		}
		if len(ids) == 0 {
			continue
		}
		rows, err := tx.QueryContext(ctx, liveLinksSQL, kinds, ids)
		if err != nil {
			return samples, fmt.Errorf("substrate/engine: aggregate source links: %w", err)
		}
		var live [][2]string
		for rows.Next() {
			var k, id string
			if err := rows.Scan(&k, &id); err != nil {
				_ = rows.Close()
				return samples, err
			}
			live = append(live, [2]string{k, id})
		}
		if err := finishRows(rows); err != nil {
			return samples, err
		}
		for _, link := range live {
			named, err := lookup(sourcesOfSQL, link[0], link[1], vocabulary.SourceLinkSources)
			if err != nil {
				return samples, err
			}
			first := ""
			for _, n := range named {
				if !bindsSource(reg, n[0]) {
					continue
				}
				member := vocabulary.RecordPath(n[0], n[1])
				l.linked[member] = true
				if first == "" {
					first = member
				} else {
					l.union(first, member)
				}
				if !l.expanded[member] {
					l.expanded[member] = true
					queue = append(queue, member)
				}
			}
		}
	}
	return samples, nil
}

// keep drops from eligible and context the streams of every linked source
// but its device's winner per metric and `about`. The winner is the
// lowest-ranked source among the candidates, the stream order breaking a
// tie, and among the context streams where no candidate is the device's:
// eligible are the streams with values in the read's range and `from`,
// candidates those of them with a value on the dates the read answers
// (linkCandidates), and context the streams the policy may also weigh.
func (l *sourceLinks) keep(eligible, candidates, context []*aggStream) ([]*aggStream, []*aggStream) {
	type key struct{ device, metric, about string }
	keyOf := func(s *aggStream) (key, bool) {
		if !l.linked[s.source] {
			return key{}, false
		}
		return key{l.find(s.source), s.metric, s.about}, true
	}
	winners := map[key]*aggStream{}
	pick := func(streams []*aggStream, only map[key]bool) {
		for _, s := range streams {
			k, ok := keyOf(s)
			if !ok || (only != nil && !only[k]) {
				continue
			}
			if w := winners[k]; w == nil || s.rank < w.rank || (s.rank == w.rank && aggStreamLess(s, w)) {
				winners[k] = s
			}
		}
	}
	pick(candidates, nil)
	missing := map[key]bool{}
	for _, s := range context {
		if k, ok := keyOf(s); ok && winners[k] == nil {
			missing[k] = true
		}
	}
	pick(context, missing)
	filter := func(streams []*aggStream) []*aggStream {
		if streams == nil {
			return nil
		}
		out := make([]*aggStream, 0, len(streams))
		for _, s := range streams {
			if k, ok := keyOf(s); ok && winners[k] != nil && winners[k].source != s.source {
				continue
			}
			out = append(out, s)
		}
		return out
	}
	return filter(eligible), filter(context)
}

// onDatesSQL keeps, of the pairs of a stream ($1, numbered $2, its scan
// starting at $3) and a form ($4), those holding an available value of the
// form whose recorded-local span meets [$6, $7), the read's dates as UTC
// midnights, within the scan's end $5: a value's local time is its instant
// plus its record's UTC offset. Each pair is one range of the stream index.
const onDatesSQL = `SELECT x.n FROM unnest($1::bigint[], $2::int[], $3::timestamptz[], $4::text[]) AS x(id, n, lo, form)
	WHERE EXISTS (SELECT 1 FROM observed_segments g
		WHERE g.stream = x.id AND g.form = x.form AND g.available > 0
		  AND g.first_ts >= x.lo AND g.first_ts < $5
		  AND g.end_ts + make_interval(mins => g.utc_offset_min) >= $6
		  AND g.first_ts + make_interval(mins => g.utc_offset_min) < $7)`

// linkCandidates are the eligible streams a linked device's winner is chosen
// among. A range of recorded-local dates scans the instants around them
// (recordedLocalDates), whose days are reduced and never answered, so there
// a candidate is a linked stream with a value on the dates themselves, and
// a source whose only values lie on the padding days hides nothing.
// Everywhere else every eligible stream is one.
func (ds *dataset) linkCandidates(ctx context.Context, tx *sql.Tx, p *aggPlan, eligible []*aggStream, windows [][2]time.Time) ([]*aggStream, error) {
	d := p.bucket.dates
	if d == nil || len(windows) == 0 {
		return eligible, nil
	}
	from, err := time.Parse(time.DateOnly, d.from)
	if err != nil {
		return nil, err
	}
	to, err := time.Parse(time.DateOnly, d.to)
	if err != nil {
		return nil, err
	}
	var ids []int64
	var ns []int
	var lo []time.Time
	var forms []string
	for i, s := range eligible {
		if p.links.linked[s.source] {
			ids, ns = append(ids, s.id), append(ns, i)
			lo, forms = append(lo, s.info().reachBack(windows[0][0])), append(forms, s.form)
		}
	}
	if len(ids) == 0 {
		return eligible, nil
	}
	rows, err := tx.QueryContext(ctx, onDatesSQL, ids, ns, lo, forms, windows[len(windows)-1][1], from, to)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate source links: %w", err)
	}
	held := map[int]bool{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			_ = rows.Close()
			return nil, err
		}
		held[n] = true
	}
	if err := finishRows(rows); err != nil {
		return nil, err
	}
	out := make([]*aggStream, 0, len(eligible))
	for i, s := range eligible {
		if !p.links.linked[s.source] || held[i] {
			out = append(out, s)
		}
	}
	return out, nil
}
