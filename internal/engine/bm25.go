package engine

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
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
// gathered by the index (the relaxed query, capped at bm25Pool rows, the ones
// matching every word first), their per-band occurrence counts and lengths are
// read in one statement, and the sum is computed here.
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
	// bm25PrefixWeight is what a completion of a prefix term counts, against
	// the typed word itself.
	bm25PrefixWeight = 0.5
	// dfCap bounds each document-frequency count: past it a word is common
	// enough that its weight barely moves, and the count stops paying for
	// itself.
	dfCap = 20000
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
	mu sync.Mutex
	s  searchStats
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
	c := &ds.statsCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.at.IsZero() && time.Since(c.s.at) < statsTTL {
		return c.s, nil
	}
	var n int64
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return searchStats{}, fmt.Errorf("substrate/engine: search statistics: %w", err)
	}
	s := searchStats{n: float64(n), at: time.Now()}
	if n > 0 {
		pct := math.Min(100, 100*float64(statsSample)/float64(n))
		var a, b, cc, d *float64
		// The sample is taken before any vector is read, and each sampled row
		// is counted on its own: grouping the unnested rows of every row cost
		// a second per 100 000 rows, this a tenth of that.
		if err := ds.db.QueryRowContext(ctx, `
			SELECT avg(nullif(l.a, 0)), avg(nullif(l.b, 0)), avg(nullif(l.c, 0)), avg(nullif(l.d, 0))
			FROM records r TABLESAMPLE BERNOULLI ($1)
			CROSS JOIN LATERAL (
				SELECT count(*) FILTER (WHERE w = 'A') AS a, count(*) FILTER (WHERE w = 'B') AS b,
				       count(*) FILTER (WHERE w = 'C') AS c, count(*) FILTER (WHERE w = 'D') AS d
				FROM unnest(r.fts) u CROSS JOIN LATERAL unnest(u.weights) w
			) l
			WHERE r.deleted_at IS NULL`, pct).Scan(&a, &b, &cc, &d); err != nil {
			return searchStats{}, fmt.Errorf("substrate/engine: search statistics: %w", err)
		}
		for i, v := range []*float64{a, b, cc, d} {
			if v != nil {
				s.avg[i] = *v
			}
		}
	}
	c.s = s
	return s, nil
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

// lexical ranks the records the plan matches: candidates by the relaxed
// query, those matching the query as typed first, each scored by BM25F.
func (ds *dataset) lexical(ctx context.Context, plan searchPlan, types []string, restrict bool, k int) (map[eref]arm, error) {
	terms := plan.terms()
	st, err := ds.searchStats(ctx)
	if err != nil {
		return nil, err
	}
	idf, err := ds.termIDF(ctx, terms, st)
	if err != nil {
		return nil, err
	}

	b := &builder{}
	strict := plan.expr(b, false)
	relaxed := plan.expr(b, true)
	clause := ""
	if restrict {
		clause = ` AND kind = ANY(` + b.textArray(types) + `)`
	}
	words := make([]string, len(terms))
	prefix := make([]bool, len(terms))
	for i, t := range terms {
		words[i], prefix[i] = t.word, t.prefix
	}
	wordsArg, prefixArg := b.arg(words), b.arg(prefix)
	pool := max(bm25Pool, 4*k)
	// One statement: the candidate pool, each candidate's band lengths, and
	// each (candidate, term) pair's weighted occurrences per band. A term's
	// exact lexemes are its word under `english`, the dictionary the row was
	// indexed with; a prefix term's starts are the word under `simple`,
	// unstemmed, for the reason tsquery.go gives.
	rows, err := ds.db.QueryContext(ctx, `
		WITH cand AS (
			SELECT kind, id, fts, fts @@ `+strict+` AS whole, `+demotion("records")+` AS demoted
			FROM records
			WHERE deleted_at IS NULL AND fts @@ `+relaxed+clause+`
			ORDER BY whole DESC, ts_rank(fts, `+relaxed+`) DESC, kind, id
			LIMIT `+b.arg(pool)+`
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
	out := make(map[eref]arm, len(docs))
	for id, d := range docs {
		out[id] = arm{score: bm25Score(*d, idf, st), demoted: d.demoted, whole: d.whole}
	}
	return out, nil
}

// termIDF reads each term's document frequency in one statement, each count
// capped at dfCap, and turns it into the term's IDF.
func (ds *dataset) termIDF(ctx context.Context, terms []scoreTerm, st searchStats) ([]float64, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	b := &builder{}
	cols := make([]string, len(terms))
	capArg := b.arg(dfCap)
	for i, t := range terms {
		expr := searchOperand{words: []string{t.word}, prefix: t.prefix}.sql(b)
		cols[i] = `(SELECT count(*) FROM (SELECT 1 FROM records WHERE deleted_at IS NULL AND fts @@ ` + expr + ` LIMIT ` + capArg + `) s)`
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
