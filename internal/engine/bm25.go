package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// THE LEXICAL ARM'S RANKING: BM25F over the weighted bands.
//
// Postgres ranks a tsvector with ts_rank, which counts how often the query's
// lexemes occur and weights them by band. It has no inverse document
// frequency, so a word every record holds counts as much as a name only one
// holds, and no length normalization, so a long Drive file that repeats a word
// saturates near 1.0 and outranks the record titled by it. BM25F is the
// standard answer to both: each band is a field with its own weight and its
// own length normalization, a word's weight falls with how many records hold
// it, and repetition saturates.
//
// Postgres carries no BM25, and the extensions that do are not in the stock
// image an operator runs, so the arm computes it from what a tsvector already
// holds: each lexeme's positions and their band labels. Candidates are
// gathered in two bounded passes (lexical: the rows matching every word, then
// a top-up from the relaxed query), capped at bm25Pool rows, their per-band
// occurrence counts and lengths are read in one statement, and the sum is
// computed here.
//
// A lexeme is a stem, so a band's length is its count of positions, and a
// prefix term counts a completion at half the weight of the word itself: `plan*`
// ranks `plan` above `planet`.

// bm25Band is one band's field parameters: its weight against the others and
// how strongly its length normalizes (0 none, 1 fully).
type bm25Band struct{ weight, b float64 }

// bm25Bands are A (the title), B (short strings), C (prose) and D (unused by
// the fold, present so a stray label scores rather than vanishes). A title is
// short by nature, so its length barely normalizes; prose is where repetition
// lives.
var bm25Bands = [4]bm25Band{{3.0, 0.3}, {1.5, 0.5}, {1.0, 0.75}, {1.0, 0.75}}

const (
	bm25K1 = 1.2
	// bm25Pool caps the candidates one lexical search scores. The pool is the
	// candidates matching every word first, then by ts_rank, so a cap only
	// ever drops rows that miss a word and rank low without IDF.
	bm25Pool = 400
	// searchReadMax is the most matching rows one candidate pass reads before
	// any ranking work (lexicalCandidates), newest first. Ranking reads each
	// row's whole vector; a pass that ranked every row a common word matched
	// was minutes on a large repository, and this many is tens of
	// milliseconds.
	searchReadMax = 2000
	// DefaultSearchBudget is the wall clock one lexical search may spend
	// (WithSearchBudget overrides it per service): past it the relaxed top-up
	// is dropped, scoring falls back to the pre-rank order, or the search
	// fails when nothing was found yet.
	DefaultSearchBudget = 5 * time.Second
	// bm25PrefixWeight is what a completion of a prefix term counts, against
	// the typed word itself.
	bm25PrefixWeight = 0.5
	// dfCap bounds each document-frequency count: past it a word is common
	// enough that its weight barely moves, and the count stops paying for
	// itself.
	dfCap = 20000
	// probeCost is roughly how many rows matching in place costs what one
	// match read through records_matching does, the count that picks the path
	// included: about 13 µs a match against 0.5 µs a row, measured on 150k
	// rows, and break-even near a thirtieth of the repository.
	probeCost = 32
	// indexedMatchMax is the most matches a list `search` filter reads
	// through records_matching. Past it the filter matches on the row, where
	// the list's own order stops at the page: newest first walks
	// records_created_at_idx, 0.3 ms for a word 5000 rows hold against 57 ms
	// through the function.
	indexedMatchMax = 500
	// statsTTL is how long one repository's collection statistics stand. They
	// move only the length normalization and the IDF, both slowly, so a
	// minutes-old average ranks the same as a fresh one.
	statsTTL = 10 * time.Minute
	// statsSample is roughly how many rows the average band lengths are read
	// from.
	statsSample = 2000
)

// searchStats is one repository's collection statistics: live rows and the
// average length of each band among the rows holding it.
type searchStats struct {
	n   float64
	avg [4]float64
	at  time.Time
}

// searchStatsCache holds one repository's statistics between searches.
type searchStatsCache struct {
	// mu guards s alone and is never held across a read.
	mu sync.Mutex
	s  searchStats
	// refresh is held by the one caller reading fresh statistics.
	refresh sync.Mutex
}

// searchStats returns the repository's collection statistics, reading them
// when the held ones are older than statsTTL. The averages come from a sample
// of about statsSample rows, because an exact average detoasts every row's
// vector.
//
// A band's average is over the rows that HAVE the band. Most kinds carry no
// prose, so an average over every row put the prose band near zero positions
// and read every record with a description as thirty times longer than
// usual, which crushed any match in it.
func (ds *dataset) searchStats(ctx context.Context) (searchStats, error) {
	return ds.statsCache.get(ctx, ds.readSearchStats)
}

// get answers the held statistics while they stand, and read's otherwise.
// The read runs outside mu: it samples the repository, and every lexical
// search would wait on it. One caller reads at a time; while it does, a
// caller holding stale statistics answers those, and a caller with none
// waits for the read and answers what it found.
func (c *searchStatsCache) get(ctx context.Context, read func(context.Context) (searchStats, error)) (searchStats, error) {
	held := c.held()
	if statsFresh(held) {
		return held, nil
	}
	if held.at.IsZero() {
		c.refresh.Lock()
	} else if !c.refresh.TryLock() {
		return held, nil
	}
	defer c.refresh.Unlock()
	// The caller that held refresh before this one may have read already.
	if held = c.held(); statsFresh(held) {
		return held, nil
	}
	s, err := read(ctx)
	if err != nil {
		return searchStats{}, err
	}
	c.mu.Lock()
	c.s = s
	c.mu.Unlock()
	return s, nil
}

func (c *searchStatsCache) held() searchStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s
}

// statsFresh reports whether held statistics still stand.
func statsFresh(s searchStats) bool { return !s.at.IsZero() && time.Since(s.at) < statsTTL }

// readSearchStats reads the repository's live row count and its band
// averages.
func (ds *dataset) readSearchStats(ctx context.Context) (searchStats, error) {
	var n int64
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return searchStats{}, fmt.Errorf("substrate/engine: search statistics: %w", err)
	}
	s := searchStats{n: float64(n), at: time.Now()}
	if n > 0 {
		avg, err := bandAverages(n, func(method string, pct float64) (int64, [4]float64, error) {
			return ds.sampleBandLengths(ctx, method, pct)
		})
		if err != nil {
			return searchStats{}, fmt.Errorf("substrate/engine: search statistics: %w", err)
		}
		s.avg = avg
	}
	return s, nil
}

// The ways sampleBandLengths reads the records table.
const (
	sampleEvery     = ""          // no TABLESAMPLE: every live row of the repository
	sampleSystem    = "SYSTEM"    // a share of the table's pages
	sampleBernoulli = "BERNOULLI" // a share of the rows of every page
)

// bandAverages reads the average band lengths of a repository of n live rows
// from a sample of about statsSample of them. The records table holds every
// repository, and the policy filters rows only after their page is read, so
// BERNOULLI reads every page of the table whatever the percentage; SYSTEM
// reads that percentage of the pages. A repository's rows can sit on few
// pages that a SYSTEM sample misses, so a sample of fewer than statsSample/4
// rows is read again with BERNOULLI. A repository of statsSample rows or
// fewer is read whole, without TABLESAMPLE, so its index finds the rows.
func bandAverages(n int64, sample func(method string, pct float64) (int64, [4]float64, error)) ([4]float64, error) {
	pct := 100 * float64(statsSample) / float64(n)
	if pct >= 100 {
		_, avg, err := sample(sampleEvery, 100)
		return avg, err
	}
	rows, avg, err := sample(sampleSystem, pct)
	if err != nil || rows >= statsSample/4 {
		return avg, err
	}
	_, avg, err = sample(sampleBernoulli, pct)
	return avg, err
}

// sampleBandLengths reads, over the live rows a sample keeps, how many there
// are and each band's average length among the rows holding the band.
// method is one of the sample constants, never caller input.
func (ds *dataset) sampleBandLengths(ctx context.Context, method string, pct float64) (int64, [4]float64, error) {
	from, args := `records r`, []any{}
	if method != sampleEvery {
		from += ` TABLESAMPLE ` + method + ` ($1)`
		args = append(args, pct)
	}
	var rows int64
	var a, b, c, d *float64
	// The sample is taken before any vector is read, and each sampled row
	// is counted on its own: grouping the unnested rows of every row cost
	// a second per 100 000 rows, this a tenth of that.
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*), avg(nullif(l.a, 0)), avg(nullif(l.b, 0)), avg(nullif(l.c, 0)), avg(nullif(l.d, 0))
		FROM `+from+`
		CROSS JOIN LATERAL (
			SELECT count(*) FILTER (WHERE w = 'A') AS a, count(*) FILTER (WHERE w = 'B') AS b,
			       count(*) FILTER (WHERE w = 'C') AS c, count(*) FILTER (WHERE w = 'D') AS d
			FROM unnest(r.fts) u CROSS JOIN LATERAL unnest(u.weights) w
		) l
		WHERE r.deleted_at IS NULL`, args...).Scan(&rows, &a, &b, &c, &d); err != nil {
		return 0, [4]float64{}, err
	}
	var avg [4]float64
	for i, v := range []*float64{a, b, c, d} {
		if v != nil {
			avg[i] = *v
		}
	}
	return rows, avg, nil
}

// bm25Doc is one candidate as the statement reads it: its band lengths and,
// per scoring term, its weighted occurrences in each band.
type bm25Doc struct {
	id      eref
	whole   bool // matches the query as typed, every word
	demoted bool
	length  [4]float64
	tf      map[int][4]float64 // term index to per-band occurrences
}

// bm25Score is the BM25F sum over the terms: per term, the band-normalized
// occurrences saturate through k1 and scale by the term's IDF.
func bm25Score(d bm25Doc, idf []float64, st searchStats) float64 {
	var score float64
	for i, tf := range d.tf {
		var w float64
		for band, occ := range tf {
			if occ == 0 {
				continue
			}
			p := bm25Bands[band]
			norm := 1.0
			if st.avg[band] > 0 {
				norm = 1 - p.b + p.b*d.length[band]/st.avg[band]
			}
			w += p.weight * occ / norm
		}
		if w > 0 && i < len(idf) {
			score += idf[i] * w * (bm25K1 + 1) / (bm25K1 + w)
		}
	}
	return score
}

// bm25IDF is the Robertson-Sparck Jones weight with the +1 that keeps a word
// held by more than half the rows positive.
func bm25IDF(n, df float64) float64 {
	if df > n {
		df = n
	}
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

// lexicalCap says where a lexical search stopped short of ranking every
// match: a pass met more matching rows than it reads (readCapped, at readMax),
// or the relaxed top-up ran out of the budget and the strict pass's
// candidates answered alone (budget), or BM25F scoring ran out of it and the
// candidates answered in their pre-rank order (unscored).
type lexicalCap struct {
	readCapped bool
	readMax    int
	budget     bool
	unscored   bool
}

// lexicalCand is one candidate as a pass found it: whole when the strict pass
// did, and its pre-rank, the length-normalized ts_rank the pass ordered by.
type lexicalCand struct {
	id      eref
	whole   bool
	demoted bool
	rank    float64
}

// preRankNorm is ts_rank's normalization for the pre-rank: bit 1 divides the
// rank by 1 + log2 of the document's length in positions. Without it a long
// Drive file or mail repeating the words outranks a short record naming them,
// fills the pool, and is also the costliest row to score. Bit 2 (divide by the
// length) would drop long documents from the pool whatever they match, and
// bit 32 only rescales, so the logarithm is the one that stops rewarding
// length without punishing it.
const preRankNorm = 1

// lexicalPass is what both candidate passes of one search share.
type lexicalPass struct {
	plan     searchPlan
	types    []string
	restrict bool
	// readMax is the most matching rows a pass reads before any ranking.
	readMax int
	// matchMax is the match count past which a pass matches on the row
	// instead of through records_matching.
	matchMax int
}

// lexical ranks the records the plan matches, each scored by BM25F. The
// candidates come from two bounded passes: the rows matching the query as
// typed, every word, and, only when those are fewer than the pool, a top-up
// from the rows matching any word and not every word. Each pass reads at most
// readMax matching rows, newest first, before any ts_rank or BM25 work, so a
// query whose words half the repository holds costs what a rare one does: a
// pass that ranked the whole relaxed match set took 172 s on a production
// repository for `flight booking confirmation`.
//
// The whole arm runs under the service's search budget (DefaultSearchBudget).
// A top-up that runs out of it is dropped and the strict candidates answer,
// marked capped. Scoring that runs out of it falls back to the order the
// passes already hold, strict candidates first and then by pre-rank, marked
// capped: that order costs no statement, so scoring may spend every
// millisecond left. A budget spent before any candidate was found is
// substrate.ErrUnavailable naming it.
func (ds *dataset) lexical(ctx context.Context, plan searchPlan, types []string, restrict bool, k int) (map[eref]arm, lexicalCap, error) {
	budget := ds.svc.searchBudget
	began := time.Now()
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var capped lexicalCap
	failed := func(err error) (map[eref]arm, lexicalCap, error) {
		if parent.Err() == nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("%w: search ran past its %s budget before it ranked anything: use one or two distinctive words (a name, a code, a number)",
				substrate.ErrUnavailable, budget)
		}
		return nil, capped, err
	}

	terms := plan.terms()
	st, err := ds.searchStats(ctx)
	if err != nil {
		return failed(err)
	}
	idf, err := ds.termIDF(ctx, terms, st)
	if err != nil {
		return failed(err)
	}
	pool := max(bm25Pool, 4*k)
	pass := lexicalPass{plan: plan, types: types, restrict: restrict, readMax: ds.svc.searchReadMax}
	capped.readMax = pass.readMax
	// A pass counts its matches through records_matching, capped, and reads
	// them through it while there are few: each is a probe back to its row.
	// Past the count it matches on the row instead, walking the repository
	// newest first, where a common word fills the read quickly.
	pass.matchMax = max(int(st.n/probeCost), pass.readMax)
	if ds.svc.searchMatchMax >= 0 {
		pass.matchMax = ds.svc.searchMatchMax
	}
	cands, over, err := ds.lexicalCandidates(ctx, pass, false, pool)
	if err != nil {
		return failed(err)
	}
	capped.readCapped = over
	if len(cands) < pool && plan.relaxes() {
		if hook := ds.svc.searchTopUpHook; hook != nil {
			hook()
		}
		// The top-up leaves a fifth of the budget to score what was found.
		tctx, tcancel := context.WithDeadline(ctx, began.Add(budget-budget/5))
		more, over, err := ds.lexicalCandidates(tctx, pass, true, pool-len(cands))
		for i := range more {
			more[i].whole = false
		}
		expired := tctx.Err() != nil
		tcancel()
		switch {
		case err == nil:
			cands = append(cands, more...)
			capped.readCapped = capped.readCapped || over
		case expired && parent.Err() == nil:
			capped.budget = true
		default:
			return failed(err)
		}
	}
	out := map[eref]arm{}
	if len(cands) == 0 {
		return out, capped, nil
	}
	if hook := ds.svc.searchScoreHook; hook != nil {
		hook()
	}
	docs, err := ds.scoreCandidates(ctx, plan, terms, cands)
	if err != nil {
		if ctx.Err() == nil || parent.Err() != nil {
			return failed(err)
		}
		capped.unscored = true
		for _, c := range cands {
			out[c.id] = arm{score: c.rank, demoted: c.demoted, whole: c.whole}
		}
		return out, capped, nil
	}
	for id, d := range docs {
		out[id] = arm{score: bm25Score(*d, idf, st), demoted: d.demoted, whole: d.whole}
	}
	return out, capped, nil
}

// lexicalCandidates is one candidate pass: the rows the strict query matches,
// or with relaxed the rows the relaxed query matches and the strict one does
// not. It reads at most readMax+1 matching rows, newest first, and only those
// are ranked, by ts_rank normalized by preRankNorm, keeping take. Every
// candidate comes back whole; the caller knows which pass found it. over
// reports that more rows matched than the pass read.
func (ds *dataset) lexicalCandidates(ctx context.Context, pass lexicalPass, relaxed bool, take int) ([]lexicalCand, bool, error) {
	probe := &builder{}
	few, err := indexedMatch(ctx, ds.db, probe, pass.plan.expr(probe, relaxed), pass.matchMax)
	if err != nil {
		return nil, false, err
	}
	b := &builder{}
	q := pass.plan.expr(b, relaxed)
	clause := ""
	if relaxed {
		clause += ` AND NOT (r.fts @@ ` + pass.plan.expr(b, false) + `)`
	}
	if pass.restrict {
		clause += ` AND r.kind = ANY(` + b.textArray(pass.types) + `)`
	}
	source := `records r WHERE r.deleted_at IS NULL AND r.fts @@ ` + q
	if few {
		source = `records_matching(` + q + `, NULL) m
			JOIN records r ON r.kind = m.kind AND r.id = m.id
			WHERE r.deleted_at IS NULL`
	}
	// The inner read stops at readMax+1 rows before anything is ranked: on
	// the row it walks records_created_at_idx backwards and matches each row
	// it meets, so the rows it reads are about readMax times the share of the
	// repository the query misses. The one row past readMax only says the
	// pass was cut; the newest readMax are ranked.
	rows, err := ds.db.QueryContext(ctx, `
		SELECT n.kind, n.id, n.seen, n.demoted, n.rank FROM (
			SELECT m.kind, m.id, m.demoted, ts_rank(m.fts, `+pass.plan.expr(b, true)+`, `+fmt.Sprint(preRankNorm)+`) AS rank,
			       count(*) OVER () AS seen,
			       row_number() OVER (ORDER BY m.created_at DESC, m.kind, m.id) AS nth
			FROM (
				SELECT r.kind, r.id, r.fts, r.created_at, `+demotion("r")+` AS demoted FROM `+source+clause+`
				ORDER BY r.created_at DESC
				LIMIT `+b.arg(pass.readMax+1)+`
			) m
		) n
		WHERE n.nth <= `+b.arg(pass.readMax)+`
		ORDER BY n.rank DESC, n.kind, n.id
		LIMIT `+b.arg(take), b.args...)
	if err != nil {
		return nil, false, fmt.Errorf("substrate/engine: lexical search: candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []lexicalCand
	seen := 0
	for rows.Next() {
		c := lexicalCand{whole: true}
		if err := rows.Scan(&c.id.Kind, &c.id.ID, &seen, &c.demoted, &c.rank); err != nil {
			return nil, false, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("substrate/engine: lexical search: candidates: %w", err)
	}
	return out, seen > pass.readMax, nil
}

// scoreCandidates reads, in one statement, each candidate's band lengths and
// each (candidate, term) pair's weighted occurrences per band. A term's exact
// lexemes are its word under `english`, the dictionary the row was indexed
// with; a prefix term's starts are the word under `simple`, unstemmed, for the
// reason tsquery.go gives.
func (ds *dataset) scoreCandidates(ctx context.Context, plan searchPlan, terms []scoreTerm, cands []lexicalCand) (map[eref]*bm25Doc, error) {
	b := &builder{}
	strict := plan.expr(b, false)
	kinds, ids := make([]string, len(cands)), make([]string, len(cands))
	for i, c := range cands {
		kinds[i], ids[i] = c.id.Kind, c.id.ID
	}
	words := make([]string, len(terms))
	prefix := make([]bool, len(terms))
	for i, t := range terms {
		words[i], prefix[i] = t.word, t.prefix
	}
	wordsArg, prefixArg := b.arg(words), b.arg(prefix)
	rows, err := ds.db.QueryContext(ctx, `
		WITH cand AS (
			SELECT r.kind, r.id, r.fts, r.fts @@ `+strict+` AS whole, `+demotion("r")+` AS demoted
			FROM unnest(`+b.textArray(kinds)+`, `+b.textArray(ids)+`) AS c(kind, id)
			JOIN records r ON r.kind = c.kind AND r.id = c.id
			WHERE r.deleted_at IS NULL
		), terms AS (
			SELECT t.i::int AS i,
			       ARRAY(SELECT lexeme FROM unnest(to_tsvector('english', t.word))) AS exact,
			       CASE WHEN t.prefix THEN ARRAY(SELECT lexeme FROM unnest(to_tsvector('simple', t.word)))
			            ELSE '{}'::text[] END AS starts
			FROM unnest(`+wordsArg+`::text[], `+prefixArg+`::bool[]) WITH ORDINALITY AS t(word, prefix, i)
		), lens AS (
			SELECT c.kind, c.id,
			       count(*) FILTER (WHERE w = 'A') AS la, count(*) FILTER (WHERE w = 'B') AS lb,
			       count(*) FILTER (WHERE w = 'C') AS lc, count(*) FILTER (WHERE w = 'D') AS ld
			FROM cand c CROSS JOIN LATERAL unnest(c.fts) u CROSS JOIN LATERAL unnest(u.weights) w
			GROUP BY c.kind, c.id
		), occ AS (
			SELECT c.kind, c.id, t.i,
			       CASE WHEN u.lexeme = ANY(t.exact) THEN 1.0 ELSE `+fmt.Sprint(bm25PrefixWeight)+` END AS m,
			       u.weights
			FROM cand c CROSS JOIN LATERAL unnest(c.fts) u
			JOIN terms t ON u.lexeme = ANY(t.exact)
			             OR EXISTS (SELECT 1 FROM unnest(t.starts) s WHERE starts_with(u.lexeme, s))
		), tf AS (
			SELECT o.kind, o.id, o.i,
			       coalesce(sum(o.m) FILTER (WHERE w = 'A'), 0) AS ta, coalesce(sum(o.m) FILTER (WHERE w = 'B'), 0) AS tb,
			       coalesce(sum(o.m) FILTER (WHERE w = 'C'), 0) AS tc, coalesce(sum(o.m) FILTER (WHERE w = 'D'), 0) AS td
			FROM occ o CROSS JOIN LATERAL unnest(o.weights) w
			GROUP BY o.kind, o.id, o.i
		)
		SELECT c.kind, c.id, c.whole, c.demoted,
		       coalesce(l.la, 0), coalesce(l.lb, 0), coalesce(l.lc, 0), coalesce(l.ld, 0),
		       coalesce(tf.i, 0), coalesce(tf.ta, 0), coalesce(tf.tb, 0), coalesce(tf.tc, 0), coalesce(tf.td, 0)
		FROM cand c
		LEFT JOIN lens l ON l.kind = c.kind AND l.id = c.id
		LEFT JOIN tf ON tf.kind = c.kind AND tf.id = c.id`, b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: lexical search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	docs := map[eref]*bm25Doc{}
	for rows.Next() {
		var id eref
		var whole, demoted bool
		var l [4]float64
		var i int
		var tf [4]float64
		if err := rows.Scan(&id.Kind, &id.ID, &whole, &demoted, &l[0], &l[1], &l[2], &l[3],
			&i, &tf[0], &tf[1], &tf[2], &tf[3]); err != nil {
			return nil, err
		}
		d, ok := docs[id]
		if !ok {
			d = &bm25Doc{id: id, whole: whole, demoted: demoted, length: l, tf: map[int][4]float64{}}
			docs[id] = d
		}
		// ORDINALITY counts from 1; 0 is the LEFT JOIN's absent term.
		if i > 0 {
			d.tf[i-1] = tf
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

// termIDF reads each term's document frequency in one statement, each count
// capped at dfCap inside records_matching, and turns it into the term's IDF.
func (ds *dataset) termIDF(ctx context.Context, terms []scoreTerm, st searchStats) ([]float64, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	b := &builder{}
	cols := make([]string, len(terms))
	capArg := b.arg(dfCap)
	for i, t := range terms {
		expr := searchOperand{words: []string{t.word}, prefix: t.prefix}.sql(b)
		cols[i] = `(SELECT count(*) FROM records_matching(` + expr + `, ` + capArg + `))`
	}
	dfs := make([]int64, len(terms))
	dest := make([]any, len(terms))
	for i := range dfs {
		dest[i] = &dfs[i]
	}
	if err := ds.db.QueryRowContext(ctx, `SELECT `+strings.Join(cols, ", "), b.args...).Scan(dest...); err != nil {
		return nil, fmt.Errorf("substrate/engine: lexical search: document frequencies: %w", err)
	}
	idf := make([]float64, len(terms))
	for i, df := range dfs {
		idf[i] = bm25IDF(st.n, float64(df))
	}
	return idf, nil
}

// indexedMatch reports whether the tsquery q, SQL over b's arguments, matches
// at most limit live rows of the repository, counting through records_matching
// and stopping at limit+1.
func indexedMatch(ctx context.Context, x dbx, b *builder, q string, limit int) (bool, error) {
	var n int
	if err := x.QueryRowContext(ctx,
		`SELECT count(*) FROM records_matching(`+q+`, `+b.arg(limit+1)+`)`,
		b.args...).Scan(&n); err != nil {
		return false, fmt.Errorf("substrate/engine: search: count matches: %w", err)
	}
	return n <= limit, nil
}
