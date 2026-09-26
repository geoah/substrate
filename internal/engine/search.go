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
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	chunkSize    = 1500
	chunkOverlap = 200
	// vectorDim matches the embeddings column; a mismatched embedder is a
	// configuration error, not a runtime surprise.
	vectorDim = 1536
)

func (ds *dataset) Search(ctx context.Context, in substrate.SearchInput) (substrate.SearchResult, error) {
	var out substrate.SearchResult
	q := strings.TrimSpace(in.Q)
	if q == "" {
		return out, fmt.Errorf("%w: search needs a query", substrate.ErrValidation)
	}
	// The lexical arm ranks by the search grammar (tsquery.go), the same text
	// the list's `search` arm filters by; the semantic arm embeds the query as
	// typed, stars and all, because a prefix means nothing to an embedding.
	plan := parseSearch(q)
	if plan.empty() {
		return out, fmt.Errorf("%w: q: %q has no word to match", substrate.ErrValidation, q)
	}
	k := in.K
	if k <= 0 {
		k = 20
	}
	mode := in.Mode
	if mode == "" {
		mode = substrate.SearchHybrid
	}
	reg := ds.registry()
	types, restrict, err := searchKinds(reg, in.Kinds, in.Purposes)
	if err != nil {
		return out, err
	}
	if restrict && len(types) == 0 {
		// The purposes admit no kind this repository declares: nothing can
		// match, and neither arm needs asking.
		return out, nil
	}
	// The embeddings provider is the REPOSITORY's, resolved here rather than
	// held by the process: a repository that names none searches lexically,
	// and one whose row does not resolve says so instead of quietly returning
	// the lexical arm as if it were the whole answer.
	var provider *embedProvider
	if mode != substrate.SearchLexical {
		p, err := ds.resolveEmbedProvider(ctx)
		if err != nil {
			return out, err
		}
		provider = p
		// The queue is counted for every search that asked for the semantic
		// arm, provider or not: it is the one number that says how much of
		// the index the ranking below did not see.
		if out.Pending, err = ds.embedPending(ctx); err != nil {
			return out, err
		}
		if provider == nil {
			if mode == substrate.SearchSemantic {
				return out, fmt.Errorf("%w: semantic search needs an embeddings provider: no llm/provider row declares %s",
					substrate.ErrValidation, propEmbedModel)
			}
			mode = substrate.SearchLexical
		}
	}
	// The semantic arm runs only over vectors the resolved pair has: with
	// none there is nothing to score, so embedding the query would buy nothing
	// and a provider that is down (mid-restore, say) must not fail a hybrid
	// search whose lexical arm has an answer. Semantic mode says why instead.
	semanticArm := false
	if provider != nil {
		have, err := ds.pairHasVectors(ctx, provider)
		if err != nil {
			return out, err
		}
		if have {
			semanticArm = true
		} else if mode == substrate.SearchSemantic {
			return out, ds.refuseSemantic(ctx, provider, out.Pending)
		}
	}

	// Each arm is asked for more than k: the purpose weight and the fusion
	// reorder what the arms return, and a row an arm cut at k could have been
	// in the final k.
	armK := max(2*k, 40)
	var lex, sem map[eref]arm
	if mode == substrate.SearchLexical || mode == substrate.SearchHybrid {
		if lex, err = ds.lexical(ctx, plan, types, restrict, armK); err != nil {
			return out, err
		}
	}
	if semanticArm {
		if sem, err = ds.semantic(ctx, provider, q, types, restrict, armK); err != nil {
			return out, err
		}
	}
	ranked := fuseArms(lex, sem, func(id eref) string { return kindPurpose(reg, id.Kind) })
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	out.Hits = make([]substrate.Hit, 0, len(ranked))
	for _, r := range ranked {
		row, err := scanRecord(ds.db.QueryRowContext(ctx,
			`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`, r.id.Kind, r.id.ID))
		if err != nil {
			continue
		}
		e, err := ds.hydrate(ctx, ds.db, row, false)
		if err != nil {
			return out, err
		}
		out.Hits = append(out.Hits, substrate.Hit{Record: e, Lexical: r.lexical, Semantic: r.semantic})
	}
	return out, nil
}

// searchKinds resolves a search's kind narrowing: the named kinds, the kinds
// of the named purposes, or both intersected, because every filter arm
// narrows. restrict says whether any narrowing was asked for; a restriction
// that admits no kind is an empty list, and the caller answers empty.
func searchKinds(reg *vocabulary.Registry, kinds, purposes []string) ([]string, bool, error) {
	var named []*vocabulary.Kind
	for _, name := range kinds {
		t, err := reg.Resolve(name)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
		}
		named = append(named, t)
	}
	kept, restrict, err := narrowByPurpose(reg, named, purposes)
	if err != nil {
		return nil, false, err
	}
	out := make([]string, 0, len(kept))
	for _, t := range kept {
		out = append(out, t.Identity)
	}
	return out, restrict || len(named) > 0, nil
}

// narrowByPurpose keeps the kinds whose purpose is one of purposes: of named
// where it names any, of every kind the registry holds otherwise. restrict is
// false only when purposes is empty, so the caller can tell "no narrowing"
// from "narrowed to nothing". An unknown purpose is refused, naming the three.
func narrowByPurpose(reg *vocabulary.Registry, named []*vocabulary.Kind, purposes []string) ([]*vocabulary.Kind, bool, error) {
	if len(purposes) == 0 {
		return named, false, nil
	}
	want := map[string]bool{}
	for _, p := range purposes {
		if !vocabulary.IsPurpose(p) {
			return nil, false, fmt.Errorf("%w: filter.purposes: %q is not a purpose: %q, %q or %q",
				substrate.ErrValidation, p, vocabulary.PurposePrimary, vocabulary.PurposeSupporting, vocabulary.PurposeInternal)
		}
		want[p] = true
	}
	from := named
	if len(from) == 0 {
		from = reg.Kinds()
	}
	kept := make([]*vocabulary.Kind, 0, len(from))
	for _, t := range from {
		if want[t.PurposeOrPrimary()] {
			kept = append(kept, t)
		}
	}
	return kept, true, nil
}

// purposeWeights is the ranking prior a kind's declared purpose sets
// (decision record 0108): a record a person browses ranks above a detail of
// one, and both above machinery, by a factor rather than a tier, so a strong
// match on machinery still outranks a weak one on data. A kind the registry no
// longer declares reads as primary, as an undeclared purpose does.
var purposeWeights = map[string]float64{
	vocabulary.PurposePrimary:    1.0,
	vocabulary.PurposeSupporting: 0.8,
	vocabulary.PurposeInternal:   0.4,
}

func kindPurpose(reg *vocabulary.Registry, kind string) string {
	if t, ok := reg.ByIdentity(kind); ok {
		return t.PurposeOrPrimary()
	}
	return vocabulary.PurposePrimary
}

// demotedWeight scales a demoted hit's score against the hits of other kinds.
const demotedWeight = 0.5

// knownFirstWithinKind reorders, in place, the hits of each kind that holds a
// demoted one: the positions those hits occupy stay theirs, and the kind's
// undemoted hits take the earlier ones, each group keeping its order.
func knownFirstWithinKind(hits []rankedHit) {
	slots := map[string][]int{}
	demoted := map[string]bool{}
	for i, h := range hits {
		slots[h.id.Kind] = append(slots[h.id.Kind], i)
		demoted[h.id.Kind] = demoted[h.id.Kind] || h.demoted
	}
	for kind, idx := range slots {
		if !demoted[kind] {
			continue
		}
		group := make([]rankedHit, len(idx))
		for n, i := range idx {
			group[n] = hits[i]
		}
		sort.SliceStable(group, func(a, b int) bool { return !group[a].demoted && group[b].demoted })
		for n, i := range idx {
			hits[i] = group[n]
		}
	}
}

// rrfK is reciprocal rank fusion's constant: a row's fused score is the sum,
// over the arms that ranked it, of 1/(rrfK + rank). 60 is the value the method
// was published with and the one every engine that fuses this way ships.
const rrfK = 60

// rankedHit is one row's place in the final ranking.
type rankedHit struct {
	id                eref
	lexical, semantic float64
	demoted           bool
	// whole is the lexical arm's tier, kept only when that arm ranks alone.
	whole bool
	score float64
}

// fuseArms orders the rows the arms returned. Each arm is ranked on its own
// terms: the lexical arm puts the rows matching every word first, then orders
// by BM25F; the semantic arm by cosine. One arm alone keeps its own score, and
// the lexical arm its tier: a row matching every word ranks above any that
// misses one. Two arms fuse by reciprocal rank, because a BM25 sum and a
// cosine are on scales no normalization makes comparable (the max-normalized
// sum this replaces let the best semantic hit tie the best lexical one however
// weak it was). The purpose weight then scales the score.
//
// The prominence demotion is two rules. Among the hits of one kind, a person
// the machine still calls `utility` ranks below every `known` one, however
// well they score: the recruiter who emailed once never outranks a friend.
// Across kinds a demoted hit's score is weighed down (demotedWeight) and
// competes with the rest, so a person born `utility` a moment ago, as every
// synced contact is, still outranks the machinery their name happens to match.
func fuseArms(lex, sem map[eref]arm, purpose func(eref) string) []rankedHit {
	hits := map[eref]*rankedHit{}
	touch := func(id eref, a arm) *rankedHit {
		h, ok := hits[id]
		if !ok {
			h = &rankedHit{id: id}
			hits[id] = h
		}
		h.demoted = h.demoted || a.demoted
		return h
	}
	both := len(lex) > 0 && len(sem) > 0
	for rank, id := range armOrder(lex, true) {
		h := touch(id, lex[id])
		h.lexical = lex[id].score
		if both {
			h.score += 1.0 / float64(rrfK+rank+1)
		} else {
			h.score, h.whole = lex[id].score, lex[id].whole
		}
	}
	for rank, id := range armOrder(sem, false) {
		h := touch(id, sem[id])
		h.semantic = sem[id].score
		if both {
			h.score += 1.0 / float64(rrfK+rank+1)
		} else {
			h.score = sem[id].score
		}
	}
	out := make([]rankedHit, 0, len(hits))
	for _, h := range hits {
		h.score *= purposeWeights[purpose(h.id)]
		if h.demoted {
			h.score *= demotedWeight
		}
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].whole != out[j].whole {
			return out[i].whole
		}
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id.less(out[j].id)
	})
	knownFirstWithinKind(out)
	return out
}

// armOrder is one arm's own ranking: by score, the lexical arm's rows
// matching every word first.
func armOrder(a map[eref]arm, tiered bool) []eref {
	ids := make([]eref, 0, len(a))
	for id := range a {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		x, y := a[ids[i]], a[ids[j]]
		if tiered && x.whole != y.whole {
			return x.whole
		}
		if x.score != y.score {
			return x.score > y.score
		}
		return ids[i].less(ids[j])
	})
	return ids
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
	// whole is the lexical arm's: the row matches the query as typed, every
	// word, and not only the relaxed candidate query.
	whole bool
}

// embedPending is how many (record, property) pairs wait in the repository's
// embed queue: the semantic index's backlog, reported beside every ranking
// that asked for the semantic arm (substrate.SearchResult.Pending).
func (ds *dataset) embedPending(ctx context.Context) (int, error) {
	var n int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM embed_queue`).Scan(&n); err != nil {
		return 0, fmt.Errorf("substrate/engine: count the embed queue: %w", err)
	}
	return n, nil
}

// pairHasVectors reports whether the resolved pair has any vector to score;
// one EXISTS, the whole cost on the happy path.
func (ds *dataset) pairHasVectors(ctx context.Context, provider *embedProvider) (bool, error) {
	var have bool
	if err := ds.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM embeddings WHERE provider = $1 AND model = $2)`,
		provider.id, provider.model).Scan(&have); err != nil {
		return false, fmt.Errorf("substrate/engine: semantic search: %w", err)
	}
	return have, nil
}

// refuseSemantic is the answer to a semantic search whose resolved pair has no
// vectors, so an empty index is never read as "no matches". Work in the queue
// (pending, counted by the caller) is substrate.ErrUnavailable with the count:
// a repository restored from its directory (whose vectors were never in the
// directory, only its queue rows are) or a re-embed the drain has not reached,
// and the number falls as the drain buys. No work and vectors from another
// pair is a row re-pointed at a model nobody ran `substratectl repository
// reembed` for: nothing will change by itself, so that is
// substrate.ErrValidation naming the operator command exactly, as
// the missing-row refusal above names the property. No work and no vectors at
// all is a repository with nothing embeddable: nil, and the caller answers
// empty. Hybrid never comes here: its lexical arm is the documented answer
// while the semantic arm has nothing, and Pending says how much it is missing.
func (ds *dataset) refuseSemantic(ctx context.Context, provider *embedProvider, pending int) error {
	if pending > 0 {
		return fmt.Errorf("%w: semantic search has no vectors yet from llm/provider %q model %q: %d properties pending in the embed queue",
			substrate.ErrUnavailable, provider.id, provider.model, pending)
	}
	var others bool
	if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM embeddings)`).Scan(&others); err != nil {
		return fmt.Errorf("substrate/engine: semantic search: %w", err)
	}
	if others {
		return fmt.Errorf("%w: semantic search has no vectors from llm/provider %q model %q and the stored vectors are another pair's: run substratectl repository reembed to replace them",
			substrate.ErrValidation, provider.id, provider.model)
	}
	return nil
}

func (ds *dataset) semantic(ctx context.Context, provider *embedProvider, q string, types []string, restrict bool, k int) (map[eref]arm, error) {
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
	if restrict {
		clause = ` AND e.kind = ANY(` + b.textArray(types) + `)`
	}
	// ONLY the resolved pair's vectors are scored. Cosine distance between two
	// models' vectors is not a distance, so a half-finished re-embed returns
	// fewer hits rather than a ranking mixed across models, and a vector whose
	// producing model is unknown (the empty provenance) is never scored at all.
	prov := ` AND em.provider = ` + b.arg(provider.id) + ` AND em.model = ` + b.arg(provider.model)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT em.record_kind, em.record_id, MAX(1 - (em.vec <=> `+vec+`)) AS sim,
		       bool_or(`+demotion("e")+`) AS demoted
		FROM embeddings em JOIN records e ON e.kind = em.record_kind AND e.id = em.record_id
		WHERE e.deleted_at IS NULL`+prov+clause+`
		GROUP BY em.record_kind, em.record_id ORDER BY sim DESC LIMIT `+b.arg(k), b.args...)
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
		return 0, fmt.Errorf("%w: llm/provider row %q model %q is %d wide, storage expects %d",
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
		hash := chunkHash(c)
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

// chunkHash is the text_hash an embeddings row stores for one chunk: what the
// drain skips on (computeEmbedding) and what an import compares against
// (reconcileEmbeddings), so the two can never disagree about "unchanged".
func chunkHash(chunk string) string {
	sum := sha256.Sum256([]byte(chunk))
	return hex.EncodeToString(sum[:])
}

// chunkHashes is chunkHash over every chunk of a property's text, in order.
func chunkHashes(text string) []string {
	chunks := chunkText(text)
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = chunkHash(c)
	}
	return out
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
