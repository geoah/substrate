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
// it visits is charged to maxSamples.

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

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

// linkRoundSQL reads, for the frontier's sources, every live link record of
// the link kinds that names one and was not read before, with each source it
// names.
const linkRoundSQL = `WITH hit AS (
	SELECT DISTINCT f.src_kind, f.src FROM refs f
	JOIN unnest($1::text[], $2::text[]) AS t(kind, id) ON f.dst_kind = t.kind AND f.dst = t.id
	JOIN records r ON r.kind = f.src_kind AND r.id = f.src AND r.deleted_at IS NULL
	WHERE f.src_kind = ANY($3::text[]) AND f.property = $4
	  AND NOT (f.src_kind || '/' || f.src = ANY($5::text[])))
SELECT h.src_kind, h.src, g.dst_kind, g.dst FROM hit h
JOIN refs g ON g.src_kind = h.src_kind AND g.src = h.src AND g.property = $4
ORDER BY h.src_kind, h.src, g.path, g.ord`

// expand reads the links reachable from the streams' sources that the graph
// has not read, a round per step outward, and answers samples with every
// link reference it read added, refused past maxSamples. Only a source whose
// kind binds `source` links.
func (l *sourceLinks) expand(ctx context.Context, tx *sql.Tx, streams []*aggStream, samples, maxSamples int64) (int64, error) {
	reg := l.reg
	var frontier []string
	for _, s := range streams {
		if kind, _, ok := vocabulary.SplitRecordPath(s.source); ok && !l.expanded[s.source] && bindsSource(reg, kind) {
			l.expanded[s.source] = true
			frontier = append(frontier, s.source)
		}
	}
	for len(frontier) > 0 {
		if err := ctx.Err(); err != nil {
			return samples, err
		}
		kinds, ids := make([]string, len(frontier)), make([]string, len(frontier))
		for i, f := range frontier {
			kinds[i], ids[i], _ = vocabulary.SplitRecordPath(f)
		}
		seen := make([]string, 0, len(l.links))
		for k := range l.links {
			seen = append(seen, k)
		}
		rows, err := tx.QueryContext(ctx, linkRoundSQL, kinds, ids, l.kinds, vocabulary.SourceLinkSources, seen)
		if err != nil {
			return samples, fmt.Errorf("substrate/engine: aggregate source links: %w", err)
		}
		frontier = frontier[:0]
		first := map[string]string{}
		for rows.Next() {
			var lk, lid, sk, sid string
			if err := rows.Scan(&lk, &lid, &sk, &sid); err != nil {
				_ = rows.Close()
				return samples, err
			}
			if samples++; samples > maxSamples {
				_ = rows.Close()
				return samples, refuse("aggregate.budget.maxSamples", "the source links the priority policy reads hold more references than the %d samples the read has left; narrow the filter, or raise maxSamples (the cap is %d)",
					maxSamples, substrate.AggregateMaxSamplesCap)
			}
			link := vocabulary.RecordPath(lk, lid)
			l.links[link] = true
			if !bindsSource(reg, sk) {
				continue
			}
			src := vocabulary.RecordPath(sk, sid)
			l.linked[src] = true
			if f, ok := first[link]; ok {
				l.union(f, src)
			} else {
				first[link] = src
			}
			if !l.expanded[src] {
				l.expanded[src] = true
				frontier = append(frontier, src)
			}
		}
		if err := finishRows(rows); err != nil {
			return samples, err
		}
	}
	return samples, nil
}

// keep drops from eligible and context the streams of every linked source
// but its device's winner per metric and `about`. The winner is the
// lowest-ranked source among the eligible streams, the stream order breaking
// a tie, and among the context streams where no eligible stream is the
// device's: eligible are the streams with values in the read's range and
// `from`, context those the policy may also weigh.
func (l *sourceLinks) keep(eligible, context []*aggStream) ([]*aggStream, []*aggStream) {
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
	pick(eligible, nil)
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
