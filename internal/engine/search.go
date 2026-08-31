package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pgvector/pgvector-go"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	chunkSize    = 1500
	chunkOverlap = 200
	// vectorDim matches the embeddings column; a mismatched embedder is a
	// configuration error, not a runtime surprise.
	vectorDim = 1536
)

// The seam discovery asserts at runtime. Naming the shared symbol is what
// keeps the two sides together: without it a rename in internal/api would
// leave every deployment quietly reporting no embeddings.
var _ substrate.EmbeddingsReporter = (*service)(nil)

// EmbeddingsEnabled reports whether this engine serves embeddings at all, and
// it always does: the provider is a repository's own llmprovider row
// (resolveEmbedProvider), not a host setting an operator can omit, so there is
// no build of this engine where the feature is unreachable. Discovery is
// unauthenticated and opens no repository, so it cannot answer the narrower
// question (whether the CALLER's repository declares a row) and is not asked
// to: Search answers that one on the first query, naming the property no row
// declares. The seam stays because a Service that genuinely cannot embed
// (the API's fake, a future reader-only implementation) still reports false and
// discovery still drops the feature for it.
func (*service) EmbeddingsEnabled() bool { return true }

func (ds *dataset) Search(ctx context.Context, in substrate.SearchInput) ([]substrate.Hit, error) {
	q := strings.TrimSpace(in.Q)
	if q == "" {
		return nil, fmt.Errorf("%w: search needs a query", substrate.ErrValidation)
	}
	k := in.K
	if k <= 0 {
		k = 20
	}
	mode := in.Mode
	if mode == "" {
		mode = substrate.SearchHybrid
	}
	// The embeddings provider is the REPOSITORY's, resolved here rather than
	// held by the process: a repository that names none searches lexically,
	// and one whose row does not resolve says so instead of quietly returning
	// the lexical arm as if it were the whole answer.
	var provider *embedProvider
	if mode != substrate.SearchLexical {
		p, err := ds.resolveEmbedProvider(ctx)
		if err != nil {
			return nil, err
		}
		provider = p
		if provider == nil {
			if mode == substrate.SearchSemantic {
				return nil, fmt.Errorf("%w: semantic search needs an embeddings provider: no llmprovider row declares %s",
					substrate.ErrValidation, propEmbedModel)
			}
			mode = substrate.SearchLexical
		}
	}
	var types []string
	reg := ds.registry()
	for _, name := range in.Kinds {
		t, err := reg.Resolve(name)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
		}
		types = append(types, t.Identity)
	}

	scores := map[eref]*substrate.Hit{}
	demoted := map[eref]bool{}
	var order []eref
	touch := func(id eref) *substrate.Hit {
		h, ok := scores[id]
		if !ok {
			h = &substrate.Hit{}
			scores[id] = h
			order = append(order, id)
		}
		return h
	}

	if mode == substrate.SearchLexical || mode == substrate.SearchHybrid {
		lex, err := ds.lexical(ctx, q, types, k)
		if err != nil {
			return nil, err
		}
		for id, r := range lex {
			touch(id).Lexical = r.score
			demoted[id] = demoted[id] || r.demoted
		}
	}
	if mode == substrate.SearchSemantic || mode == substrate.SearchHybrid {
		sem, err := ds.semantic(ctx, provider, q, types, k)
		if err != nil {
			return nil, err
		}
		for id, r := range sem {
			touch(id).Semantic = r.score
			demoted[id] = demoted[id] || r.demoted
		}
	}
	if len(order) == 0 {
		return nil, nil
	}

	// Max-normalise each arm so the two scales merge without tuning.
	var maxLex, maxSem float64
	for _, id := range order {
		h := scores[id]
		if h.Lexical > maxLex {
			maxLex = h.Lexical
		}
		if h.Semantic > maxSem {
			maxSem = h.Semantic
		}
	}
	combined := make(map[eref]float64, len(order))
	for _, id := range order {
		h := scores[id]
		var c float64
		if maxLex > 0 {
			c += h.Lexical / maxLex
		}
		if maxSem > 0 {
			c += h.Semantic / maxSem
		}
		combined[id] = c
	}
	// Prominence demotion (MODEL §7): a person the machine still calls
	// `utility` ranks below every `known` match, however well they score.
	// The engine stays otherwise unopinionated — filtering is the client's.
	sort.SliceStable(order, func(i, j int) bool {
		if demoted[order[i]] != demoted[order[j]] {
			return !demoted[order[i]]
		}
		if combined[order[i]] != combined[order[j]] {
			return combined[order[i]] > combined[order[j]]
		}
		return order[i].less(order[j])
	})
	if len(order) > k {
		order = order[:k]
	}
	out := make([]substrate.Hit, 0, len(order))
	for _, id := range order {
		row, err := scanRecord(ds.db.QueryRowContext(ctx,
			`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`, id.Kind, id.ID))
		if err != nil {
			continue
		}
		e, err := ds.hydrate(ctx, ds.db, row, false)
		if err != nil {
			return nil, err
		}
		h := scores[id]
		h.Record = e
		out = append(out, *h)
	}
	return out, nil
}

// demotionExpr is the prominence rank a search arm orders by: true for a
// person the prominence machine has not promoted. A type declaring no such
// machine has no `prominence` state, so the expression is false for it — no
// registry lookup, no index, one jsonb probe per candidate row.
const demotionExpr = `(%s.states->>'prominence' IS NOT NULL AND %s.states->>'prominence' <> 'known')`

func demotion(alias string) string { return fmt.Sprintf(demotionExpr, alias, alias) }

// arm is one search arm's result for a record.
type arm struct {
	score   float64
	demoted bool
}

func (ds *dataset) lexical(ctx context.Context, q string, types []string, k int) (map[eref]arm, error) {
	b := &builder{}
	qarg := b.arg(q)
	clause := ""
	if len(types) > 0 {
		clause = ` AND kind IN ` + b.jsonArray(types)
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, id, ts_rank(fts, websearch_to_tsquery('english', `+qarg+`)) AS rank,
		       `+demotion("records")+` AS demoted
		FROM records
		WHERE deleted_at IS NULL AND fts @@ websearch_to_tsquery('english', `+qarg+`)`+clause+`
		ORDER BY demoted, rank DESC LIMIT `+b.arg(k), b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: lexical search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[eref]arm{}
	for rows.Next() {
		var id eref
		var a arm
		if err := rows.Scan(&id.Kind, &id.ID, &a.score, &a.demoted); err != nil {
			return nil, err
		}
		out[id] = a
	}
	return out, rows.Err()
}

func (ds *dataset) semantic(ctx context.Context, provider *embedProvider, q string, types []string, k int) (map[eref]arm, error) {
	vecs, err := provider.Embed(ctx, []string{q})
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: embed query: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("substrate/engine: embedder returned %d vectors for 1 text", len(vecs))
	}
	b := &builder{}
	vec := b.arg(pgvector.NewVector(vecs[0]))
	clause := ""
	if len(types) > 0 {
		clause = ` AND e.kind IN ` + b.jsonArray(types)
	}
	// ONLY the resolved pair's vectors are scored. Cosine distance between two
	// models' vectors is not a distance, so a half-finished re-embed returns
	// fewer hits rather than a ranking mixed across models, and a vector whose
	// producing model is unknown (the empty provenance a row stored before
	// migration 0008 carries) is never scored at all.
	prov := ` AND em.provider = ` + b.arg(provider.id) + ` AND em.model = ` + b.arg(provider.model)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT em.record_kind, em.record_id, MAX(1 - (em.vec <=> `+vec+`)) AS sim,
		       bool_or(`+demotion("e")+`) AS demoted
		FROM embeddings em JOIN records e ON e.kind = em.record_kind AND e.id = em.record_id
		WHERE e.deleted_at IS NULL`+prov+clause+`
		GROUP BY em.record_kind, em.record_id ORDER BY demoted, sim DESC LIMIT `+b.arg(k), b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: semantic search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[eref]arm{}
	for rows.Next() {
		var id eref
		var a arm
		if err := rows.Scan(&id.Kind, &id.ID, &a.score, &a.demoted); err != nil {
			return nil, err
		}
		out[id] = a
	}
	return out, rows.Err()
}

// ProcessEmbedQueue drains up to batch pending pairs. Each
// job snapshots its queue GENERATION, embeds the current text OUTSIDE any
// transaction (the embed is a slow network call), then writes the vectors and
// drops the row in one transaction ONLY WHILE that generation still stands.
//
// An edit during a slow embed re-enqueues and increments the generation
// (rows.go enqueueEmbed), so the finalize sees a bumped generation, leaves the
// newer job pending and DOES NOT commit the stale vectors — the edit is never
// lost to a slow embed, and two workers cannot both write, because the finalize
// takes the queue row FOR UPDATE and the loser finds it already drained.
//
// The embeddings provider is the repository's own row, resolved on every pass:
// a repository that names none drains nothing and reports 0, which is why the
// drain loop runs for every repository whether or not any of them embeds.
func (ds *dataset) ProcessEmbedQueue(ctx context.Context, batch int) (int, error) {
	provider, err := ds.resolveEmbedProvider(ctx)
	if err != nil {
		return 0, err
	}
	if provider == nil {
		return 0, nil
	}
	// The write refuses a model of another width, so this is the second gate,
	// for a row that predates the rule or a client the table's width no longer
	// describes (decision record 0026).
	if provider.Dimension() != vectorDim {
		return 0, fmt.Errorf("%w: llmprovider row %q model %q is %d wide, storage expects %d",
			substrate.ErrValidation, provider.id, provider.model, provider.Dimension(), vectorDim)
	}
	if batch <= 0 {
		batch = 20
	}
	rows, err := ds.db.QueryContext(ctx,
		`SELECT record_kind, record_id, property, generation FROM embed_queue ORDER BY enqueued_at LIMIT $1`, batch)
	if err != nil {
		return 0, err
	}
	type item struct {
		id   eref
		prop string
		gen  int64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id.Kind, &it.id.ID, &it.prop, &it.gen); err != nil {
			_ = rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	_ = rows.Close()

	done := 0
	for _, it := range items {
		plan, err := ds.computeEmbedding(ctx, provider, it.id, it.prop)
		if err != nil {
			return done, err
		}
		applied, err := ds.commitEmbedding(ctx, provider, it.id, it.prop, it.gen, plan)
		if err != nil {
			return done, err
		}
		if applied {
			done++
		}
	}
	return done, nil
}

// embedWrite is one chunk's vector, ready for the finalize transaction.
type embedWrite struct {
	chunk int
	hash  string
	vec   []float32
}

// embedPlan is what a job WOULD write, computed outside the transaction so the
// slow embed holds no lock. skip drops the queue row and touches no vectors
// (the record went away); drop removes the property's vectors (it is no longer
// embeddable); otherwise deleteBeyond trims the shrunk tail and writes carry
// the changed chunks.
type embedPlan struct {
	skip         bool
	drop         bool
	deleteBeyond int
	writes       []embedWrite
}

// computeEmbedding reads the current text and embeds the changed chunks. It
// makes NO writes: the generation it was computed against is only confirmed at
// commitEmbedding, so nothing here can outrun a concurrent edit.
func (ds *dataset) computeEmbedding(ctx context.Context, provider *embedProvider, id eref, prop string) (*embedPlan, error) {
	row, err := scanRecord(ds.db.QueryRowContext(ctx,
		`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = $2`, id.Kind, id.ID))
	if err != nil {
		// The record went away; drop the queue row and touch no vectors.
		return &embedPlan{skip: true}, nil
	}
	ty, ok := ds.registry().ByIdentity(row.Kind)
	if !ok {
		return &embedPlan{skip: true}, nil
	}
	p, ok := ty.Prop(prop)
	if !ok || !p.Embed {
		return &embedPlan{drop: true}, nil
	}
	chunks := chunkText(scalarString(row.Props[prop]))
	plan := &embedPlan{deleteBeyond: len(chunks)}
	for i, c := range chunks {
		sum := sha256.Sum256([]byte(c))
		hash := hex.EncodeToString(sum[:])
		var have, haveProvider, haveModel string
		err := ds.db.QueryRowContext(ctx,
			`SELECT text_hash, provider, model FROM embeddings WHERE record_kind = $1 AND record_id = $2 AND property = $3 AND chunk = $4`,
			id.Kind, id.ID, prop, i).Scan(&have, &haveProvider, &haveModel)
		// The PROVENANCE is half the skip: unchanged text embedded by another
		// pair is exactly what a re-embed exists to replace, so a stored chunk
		// is kept only when its text and its producing pair both still stand.
		if err == nil && have == hash && haveProvider == provider.id && haveModel == provider.model {
			continue
		}
		plan.writes = append(plan.writes, embedWrite{chunk: i, hash: hash})
	}
	if len(plan.writes) == 0 {
		return plan, nil
	}
	pending := make([]string, len(plan.writes))
	for n, w := range plan.writes {
		pending[n] = chunks[w.chunk]
	}
	vecs, err := provider.Embed(ctx, pending)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: embed %s.%s: %w", id.ID, prop, err)
	}
	if len(vecs) != len(pending) {
		return nil, fmt.Errorf("substrate/engine: embedder returned %d vectors for %d chunks", len(vecs), len(pending))
	}
	for n := range plan.writes {
		plan.writes[n].vec = vecs[n]
	}
	return plan, nil
}

// commitEmbedding writes a job's plan and drops its queue row, but ONLY while
// the generation it was computed against still stands. The queue row is taken
// FOR UPDATE: a bumped generation means the text changed under a slow embed, so
// the stale plan is discarded and the newer generation left pending; a missing
// row means another worker already drained it. Returns whether the job applied.
func (ds *dataset) commitEmbedding(ctx context.Context, provider *embedProvider, id eref, prop string, gen int64, plan *embedPlan) (bool, error) {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var cur int64
	err = tx.QueryRowContext(ctx,
		`SELECT generation FROM embed_queue WHERE record_kind = $1 AND record_id = $2 AND property = $3 FOR UPDATE`,
		id.Kind, id.ID, prop).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit() // already drained, or purged
	}
	if err != nil {
		return false, err
	}
	if cur != gen {
		// The text changed while we embedded: leave the newer generation pending
		// and never write the stale vectors.
		return false, tx.Commit()
	}
	switch {
	case plan.skip:
		// nothing to write; the queue row goes below
	case plan.drop:
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM embeddings WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
			id.Kind, id.ID, prop); err != nil {
			return false, err
		}
	default:
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM embeddings WHERE record_kind = $1 AND record_id = $2 AND property = $3 AND chunk >= $4`,
			id.Kind, id.ID, prop, plan.deleteBeyond); err != nil {
			return false, err
		}
		for _, w := range plan.writes {
			// The pair is stamped in the same statement as the vector, so a
			// stored vector whose provenance is wrong is not a state this
			// path can produce.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO embeddings (record_kind, record_id, property, chunk, text_hash, vec, provider, model)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (repository, record_kind, record_id, property, chunk) DO UPDATE SET
					text_hash = EXCLUDED.text_hash, vec = EXCLUDED.vec,
					provider = EXCLUDED.provider, model = EXCLUDED.model`,
				id.Kind, id.ID, prop, w.chunk, w.hash, pgvector.NewVector(w.vec),
				provider.id, provider.model); err != nil {
				return false, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM embed_queue WHERE record_kind = $1 AND record_id = $2 AND property = $3 AND generation = $4`,
		id.Kind, id.ID, prop, gen); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// chunkText splits prose into overlapping windows.
func chunkText(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	runes := []rune(s)
	if len(runes) <= chunkSize {
		return []string{s}
	}
	var out []string
	step := chunkSize - chunkOverlap
	for start := 0; start < len(runes); start += step {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return out
}
