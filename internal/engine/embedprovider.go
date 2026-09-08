package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/embed"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// WHERE VECTORS ARE BOUGHT. Completions name their provider row on the agent;
// embeddings have no agent to name one, so the DECLARATION is the selection:
// the one llmprovider row that carries `embedModel` is the repository's
// embeddings provider. Exactly one row may carry it, held at the write
// (admitProviderRow), so resolution is a lookup and never a policy.
//
// Nothing is process-wide. Two repositories on one deployment can embed
// against two endpoints with two keys and two models, and re-pointing one of
// them is a patch to one record.

// propEmbedModel is the property whose presence names the embeddings provider.
const propEmbedModel = "embedModel"

// embedProvider is one repository's resolved embeddings provider: the client
// that buys the vectors, and the pair that stamps them.
type embedProvider struct {
	// id is the llmprovider row, and model the model id as sent. The two
	// together are what `embeddings.provider`/`embeddings.model` store and
	// what the semantic query filters on, so a vector some other pair produced
	// is never scored against one this pair did.
	id     string
	model  string
	client *embed.Client
}

// Embed and Dimension make embedProvider a substrate.Embedder, so the queue
// drains through the resolved row exactly as it used to drain through the
// process-wide client.
func (p *embedProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return p.client.Embed(ctx, texts)
}

func (p *embedProvider) Dimension() int { return p.client.Dimension() }

// resolveEmbedProvider reads the repository's embeddings provider. It returns
// (nil, nil) when no row declares an embedModel: a repository that has named
// no provider does not embed, which is a state and not a failure — the queue
// idles, search stays lexical, and boot never depended on any of it.
func (ds *dataset) resolveEmbedProvider(ctx context.Context) (*embedProvider, error) {
	claims, err := embedClaims(ctx, ds.db)
	if err != nil {
		return nil, err
	}
	switch len(claims) {
	case 0:
		return nil, nil
	case 1:
	default:
		ids := make([]string, len(claims))
		for i, c := range claims {
			ids[i] = c.id
		}
		// Unreachable by design: every path that can make a row live and
		// claiming goes through admitProviderRow — the ordinary write
		// (write.go) and the split that resurrects a merged-away row
		// (merge.go). This stays because the cost of being wrong about that
		// is silent: picking one of two claimants would re-embed a whole
		// repository against a coin toss, and the vectors would not say so.
		return nil, fmt.Errorf("%w: llmprovider rows %s each declare %s — a repository buys embeddings from one row, so clear it from all but one",
			substrate.ErrValidation, strings.Join(ids, ", "), propEmbedModel)
	}
	return ds.openEmbedProvider(ctx, claims[0].id)
}

// embedClaim is one live llmprovider row declaring embedModel: the pair a
// vector it buys is stamped with (embedProvider.id, embedProvider.model).
type embedClaim struct{ id, model string }

// embedClaims is every live row claiming the embeddings job, by id, read
// through q so the import can ask its own transaction.
func embedClaims(ctx context.Context, q dbx) ([]embedClaim, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, props->>'`+propEmbedModel+`' FROM records
		   WHERE kind = $1 AND deleted_at IS NULL
		     AND coalesce(props->>'`+propEmbedModel+`', '') <> ''
		   ORDER BY id`, typeProvider)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: find the embeddings provider: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []embedClaim
	for rows.Next() {
		var c embedClaim
		if err := rows.Scan(&c.id, &c.model); err != nil {
			return nil, err
		}
		c.model = strings.TrimSpace(c.model)
		out = append(out, c)
	}
	return out, rows.Err()
}

// openEmbedProvider builds the client for one named row. It goes through
// resolveProvider, so an embeddings row is held to the same wire rules, secret
// opening and header handling as the rows completions are bought from.
func (ds *dataset) openEmbedProvider(ctx context.Context, id string) (*embedProvider, error) {
	pc, err := ds.resolveProvider(ctx, id)
	if err != nil {
		return nil, err
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: id})
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("%w: llmprovider row %q does not resolve", substrate.ErrValidation, id)
	}
	model, _ := row.Props[propEmbedModel].(string)
	model = strings.TrimSpace(model)
	if err := checkEmbedWire(id, pc.wire, model); err != nil {
		return nil, err
	}
	if err := checkEmbedModel(id, model); err != nil {
		return nil, err
	}
	client, err := embed.New(embed.Config{
		BaseURL: pc.cfg.BaseURL,
		APIKey:  pc.cfg.APIKey,
		Model:   model,
		Headers: pc.cfg.Headers,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: llmprovider row %q: %w", substrate.ErrValidation, id, err)
	}
	return &embedProvider{id: id, model: model, client: client}, nil
}

// Reembed enqueues the properties the resolved pair did not embed. It buys no
// vectors: the queue is the unit of work and ProcessEmbedQueue is what pays,
// so a re-embed interrupted halfway resumes on the next drain with no state of
// its own to lose.
//
// The queue row is written the same way a live edit writes it (rows.go
// enqueueEmbed): an existing row's generation is BUMPED rather than replaced,
// so a re-embed cannot commit vectors on behalf of a job that was already in
// flight against older text.
func (ds *dataset) Reembed(ctx context.Context, all bool) (substrate.ReembedReport, error) {
	provider, err := ds.resolveEmbedProvider(ctx)
	if err != nil {
		return substrate.ReembedReport{}, err
	}
	if provider == nil {
		return substrate.ReembedReport{}, fmt.Errorf("%w: nothing to re-embed against: no llmprovider row declares %s",
			substrate.ErrValidation, propEmbedModel)
	}
	report := substrate.ReembedReport{Provider: provider.id, Model: provider.model, All: all}
	// Which properties are embeddable is the REGISTRY's answer, so a kind that
	// gained an embeddable property since the last drain is included without
	// anything else being told.
	for _, ty := range ds.registry().Kinds() {
		for name, p := range ty.Props {
			if p == nil || !p.Embed {
				continue
			}
			n, err := ds.enqueueReembed(ctx, provider, ty.Identity, name, all)
			if err != nil {
				return report, err
			}
			report.Enqueued += n
		}
	}
	return report, nil
}

// enqueueReembed queues one kind's one embeddable property. The stale test is
// per property rather than per chunk: a property's chunks are written in one
// transaction, so they share a pair, and "no chunk from the resolved pair"
// covers both the property another pair embedded and the one nothing has.
func (ds *dataset) enqueueReembed(ctx context.Context, provider *embedProvider, kind, prop string, all bool) (int, error) {
	args := []any{kind, prop}
	stale := ``
	if !all {
		stale = `AND NOT EXISTS (
		    SELECT 1 FROM embeddings em
		     WHERE em.record_kind = r.kind AND em.record_id = r.id AND em.property = $2
		       AND em.provider = $3 AND em.model = $4)`
		args = append(args, provider.id, provider.model)
	}
	res, err := ds.db.ExecContext(ctx, `
		INSERT INTO embed_queue (record_kind, record_id, property, generation, enqueued_at)
		SELECT r.kind, r.id, $2, 1, now()
		  FROM records r
		 WHERE r.kind = $1 AND r.deleted_at IS NULL
		   AND coalesce(btrim(r.props->>$2), '') <> '' `+stale+`
		ON CONFLICT (repository, record_kind, record_id, property) DO UPDATE
		    SET generation = embed_queue.generation + 1, enqueued_at = EXCLUDED.enqueued_at`,
		args...)
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: enqueue re-embed for %s.%s: %w", kind, prop, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// reconcileEmbeddings converges the vectors a database already holds with the
// records an import folded, and queues what is missing, all through q, the
// import's transaction. Per embeddable property: a stored chunk whose record
// is purged, whose property is now empty or whose text_hash is not the current
// chunk's is deleted, and a property without a complete set of current-hash
// chunks is queued, stamped `at` (the import's clock, so every live edit after
// it sorts ahead in the drain). Into an empty database that is every property;
// over an older dump restored beside a newer directory it is only what
// changed, so a restore does not re-buy a repository. Current means the text
// AND the pair: a chunk bought by a row or model the folded records no longer
// resolve (the directory re-pointed the row while the text stood) is stale
// too, or the restored repository would answer the `reembed` refusal until an
// operator ran it by hand. Returns how many properties were queued.
//
// The registry decides what is embeddable, and it is also the gap: a closure
// the import parks (loadDeclarationsForReplay leaves out what does not admit)
// has no kinds here, so its records are neither reconciled nor queued until
// the closure admits and a `reembed` runs. The live write skips the same
// records for the same reason.
func (ds *dataset) reconcileEmbeddings(ctx context.Context, q dbx, at time.Time) (int, error) {
	// No row, or two claiming rows, is no pair, and then no stored vector is
	// current: whatever pair bought it, the folded records do not name it.
	claims, err := embedClaims(ctx, q)
	if err != nil {
		return 0, err
	}
	var pair *embedClaim
	if len(claims) == 1 {
		pair = &claims[0]
	}
	total := 0
	reg := ds.registry()
	embeddable := map[[2]string]bool{}
	for _, ty := range reg.Kinds() {
		for name, p := range ty.Props {
			if p == nil || !p.Embed {
				continue
			}
			embeddable[[2]string{ty.Identity, name}] = true
			n, err := reconcileEmbeddable(ctx, q, ty.Identity, name, pair, at)
			if err != nil {
				return total, fmt.Errorf("substrate/engine: reconcile embeddings of %s.%s: %w", ty.Identity, name, err)
			}
			total += n
		}
	}
	if err := pruneUnembeddable(ctx, q, reg, embeddable); err != nil {
		return total, err
	}
	return total, nil
}

// pruneUnembeddable deletes the chunks and the queue rows of every
// (kind, property) the database holds either of for that the registry no
// longer embeds: the directory turned `embed` off, dropped the property, or
// dropped the kind. Nothing else would ever remove them (`reembed` walks the
// same registry), semantic() would keep scoring the vectors, and a queue row
// nothing bought yet would stand as a false pending count until a drain, which
// without a provider never comes. A kind the registry does not know is left
// alone: that is a parked closure, whose records are neither reconciled nor
// queued (reconcileEmbeddings), and whose vectors are its own until it admits.
func pruneUnembeddable(ctx context.Context, q dbx, reg *vocabulary.Registry, embeddable map[[2]string]bool) error {
	rows, err := q.QueryContext(ctx, `
		SELECT record_kind, property FROM embeddings
		UNION
		SELECT record_kind, property FROM embed_queue`)
	if err != nil {
		return fmt.Errorf("substrate/engine: list embedded pairs: %w", err)
	}
	var stale [][2]string
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			_ = rows.Close()
			return err
		}
		if embeddable[pair] {
			continue
		}
		if _, known := reg.ByIdentity(pair[0]); !known {
			continue
		}
		stale = append(stale, pair)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, pair := range stale {
		for _, table := range []string{"embeddings", "embed_queue"} {
			if _, err := q.ExecContext(ctx,
				`DELETE FROM `+table+` WHERE record_kind = $1 AND property = $2`, pair[0], pair[1]); err != nil {
				return fmt.Errorf("substrate/engine: prune %s of %s.%s: %w", table, pair[0], pair[1], err)
			}
		}
	}
	return nil
}

func reconcileEmbeddable(ctx context.Context, q dbx, kind, prop string, pair *embedClaim, at time.Time) (int, error) {
	want, err := currentChunkHashes(ctx, q, kind, prop)
	if err != nil {
		return 0, err
	}
	have, err := storedChunkHashes(ctx, q, kind, prop)
	if err != nil {
		return 0, err
	}
	var gone []string
	current := map[string]bool{}
	for id, stored := range have {
		hashes, live := want[id]
		if !live {
			gone = append(gone, id)
			continue
		}
		if chunksCurrent(stored, hashes, pair) {
			current[id] = true
			continue
		}
		// Only the chunks that are no longer current go; the drain keeps the
		// rest through the same hash and pair and buys what is missing.
		for i, c := range stored {
			if chunkCurrent(c, i, hashes, pair) {
				continue
			}
			if _, err := q.ExecContext(ctx,
				`DELETE FROM embeddings WHERE record_kind = $1 AND record_id = $2 AND property = $3 AND chunk = $4`,
				kind, id, prop, i); err != nil {
				return 0, err
			}
		}
	}
	if len(gone) > 0 {
		if _, err := q.ExecContext(ctx,
			`DELETE FROM embeddings WHERE record_kind = $1 AND property = $2 AND record_id = ANY($3)`,
			kind, prop, gone); err != nil {
			return 0, err
		}
	}
	// A queue row for a record the desired set no longer holds (purged, or
	// its value gone) is not work: without this it would stand as a false
	// pending count until a drain dropped it, and with no provider that drain
	// never comes.
	live := make([]string, 0, len(want))
	queue := make([]string, 0, len(want))
	for id := range want {
		live = append(live, id)
		if !current[id] {
			queue = append(queue, id)
		}
	}
	if _, err := q.ExecContext(ctx,
		`DELETE FROM embed_queue WHERE record_kind = $1 AND property = $2 AND NOT (record_id = ANY($3))`,
		kind, prop, live); err != nil {
		return 0, err
	}
	if len(queue) == 0 {
		return 0, nil
	}
	sort.Strings(queue)
	// Written the way a live edit writes it (rows.go enqueueEmbed): a row the
	// dump already held is bumped, never replaced.
	if _, err := q.ExecContext(ctx, `
		INSERT INTO embed_queue (record_kind, record_id, property, generation, enqueued_at)
		SELECT $1, unnest($2::text[]), $3, 1, $4
		ON CONFLICT (repository, record_kind, record_id, property) DO UPDATE
		    SET generation = embed_queue.generation + 1, enqueued_at = EXCLUDED.enqueued_at`,
		kind, queue, prop, at); err != nil {
		return 0, err
	}
	return len(queue), nil
}

// currentChunkHashes is every record's chunk hashes for one property, by
// record id, hashed from the value the way the drain hashes it
// (computeEmbedding: scalarString, then chunkText), so a repeated property
// compares as its joined text and not as JSON array text. A record whose
// value is absent or blank is left out: chunkText would give it no chunks, so
// queuing it would only have the drain drop the row. Tombstones are included:
// the live path keeps a tombstone's vectors until the purge, so the import
// does too, and an undelete comes back searchable.
func currentChunkHashes(ctx context.Context, q dbx, kind, prop string) (map[string][]string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, props->$2 FROM records WHERE kind = $1 AND props ? $2`, kind, prop)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("decode %s of %s: %w", prop, id, err)
		}
		text := scalarString(v)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out[id] = chunkHashes(text)
	}
	return out, rows.Err()
}

// storedChunk is what an embeddings row says about the vector it holds: the
// text it embeds and the pair that bought it.
type storedChunk struct{ hash, provider, model string }

// storedChunkHashes is every stored chunk for one property, by record id then
// chunk index.
func storedChunkHashes(ctx context.Context, q dbx, kind, prop string) (map[string]map[int]storedChunk, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT record_id, chunk, text_hash, provider, model FROM embeddings WHERE record_kind = $1 AND property = $2`, kind, prop)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]map[int]storedChunk{}
	for rows.Next() {
		var id string
		var chunk int
		var c storedChunk
		if err := rows.Scan(&id, &chunk, &c.hash, &c.provider, &c.model); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[int]storedChunk{}
		}
		out[id][chunk] = c
	}
	return out, rows.Err()
}

// chunkCurrent reports whether one stored chunk is the current text's chunk i
// bought by the current pair.
func chunkCurrent(c storedChunk, i int, hashes []string, pair *embedClaim) bool {
	return pair != nil && i < len(hashes) && hashes[i] == c.hash &&
		c.provider == pair.id && c.model == pair.model
}

// chunksCurrent reports whether the stored chunks are exactly the current
// text's under the current pair: one row per chunk, each current, none beyond.
func chunksCurrent(stored map[int]storedChunk, hashes []string, pair *embedClaim) bool {
	if len(stored) != len(hashes) {
		return false
	}
	for i := range hashes {
		c, ok := stored[i]
		if !ok || !chunkCurrent(c, i, hashes, pair) {
			return false
		}
	}
	return true
}

// checkEmbedWire refuses a row whose wire sells no embeddings. Only the openai
// wire has an embeddings endpoint: anthropic sells none at all, and azure's is
// a per-deployment path no adapter here builds.
func checkEmbedWire(id string, wire llm.Wire, model string) error {
	policy, known := wire.Policy()
	if !known {
		return fmt.Errorf("%w: llmprovider row %q declares %s %q but no valid wire — one of %s",
			substrate.ErrValidation, id, propEmbedModel, model, llm.WireNames())
	}
	if !policy.Embeddings {
		return fmt.Errorf("%w: llmprovider row %q declares %s %q on wire %q, which has no embeddings endpoint — embeddings are bought on the openai wire, so point %s at a row whose wire is openai",
			substrate.ErrValidation, id, propEmbedModel, model, wire, propEmbedModel)
	}
	return nil
}

// checkEmbedModel refuses a model the stored column cannot hold. The width is
// fixed at vector(1536) and a model of any other width is refused rather than
// truncated (decision record 0026).
func checkEmbedModel(id, model string) error {
	dim, ok := embed.ModelDim(model)
	if !ok {
		return fmt.Errorf("%w: llmprovider row %q declares %s %q, whose width is not known here — name one of %s",
			substrate.ErrValidation, id, propEmbedModel, model, strings.Join(embed.KnownModels(), ", "))
	}
	if dim != embed.Dim {
		return fmt.Errorf("%w: llmprovider row %q declares %s %q, which is %d wide; stored vectors are %d wide, and the width is not truncated to fit",
			substrate.ErrValidation, id, propEmbedModel, model, dim, embed.Dim)
	}
	return nil
}

// admitProviderRow holds an llmprovider row to the embeddings rules AT THE
// WRITE, where the person who caused the refusal is present to read it. A row
// that names no embedModel is an ordinary completions row and passes
// untouched.
func (t *txn) admitProviderRow(id string, props map[string]any) error {
	model, _ := props[propEmbedModel].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	wire, _ := props["wire"].(string)
	if err := checkEmbedWire(id, llm.Wire(wire), model); err != nil {
		return err
	}
	if err := checkEmbedModel(id, model); err != nil {
		return err
	}
	// One row per repository declares it, so the check is a claim: the
	// exclusive lock is what stops two concurrent writes from both finding no
	// other claimant and both landing.
	//
	// LOCK ORDER. The changelog lock is already held: inTx takes it before
	// anything else (rows.go changelogLockKey), so this key is never taken
	// ahead of an append by one transaction and behind it by another, which
	// is the cycle an effect-written provider row used to be able to form.
	if err := t.lockKey("embedprovider"); err != nil {
		return err
	}
	var other string
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT id FROM records
		   WHERE kind = $1 AND id <> $2 AND deleted_at IS NULL
		     AND coalesce(props->>'`+propEmbedModel+`', '') <> ''
		   ORDER BY id LIMIT 1`, typeProvider, id).Scan(&other)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("substrate/engine: check the embeddings provider claim: %w", err)
	}
	if other != "" {
		return fmt.Errorf("%w: llmprovider row %q already declares %s — a repository buys embeddings from one row, so clear it there before naming it here",
			substrate.ErrValidation, other, propEmbedModel)
	}
	return nil
}
