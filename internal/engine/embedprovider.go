package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/embed"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
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
	rows, err := ds.db.QueryContext(ctx,
		`SELECT id FROM records
		   WHERE kind = $1 AND deleted_at IS NULL
		     AND coalesce(props->>'`+propEmbedModel+`', '') <> ''
		   ORDER BY id`, typeProvider)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: find the embeddings provider: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	switch len(ids) {
	case 0:
		return nil, nil
	case 1:
	default:
		// Unreachable by design: every path that can make a row live and
		// claiming goes through admitProviderRow — the ordinary write
		// (write.go) and the split that resurrects a merged-away row
		// (merge.go). This stays because the cost of being wrong about that
		// is silent: picking one of two claimants would re-embed a whole
		// repository against a coin toss, and the vectors would not say so.
		return nil, fmt.Errorf("%w: llmprovider rows %s each declare %s — a repository buys embeddings from one row, so clear it from all but one",
			substrate.ErrValidation, strings.Join(ids, ", "), propEmbedModel)
	}
	return ds.openEmbedProvider(ctx, ids[0])
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
		   AND coalesce(r.props->>$2, '') <> '' `+stale+`
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
	// LOCK ORDER. This is taken before the transaction's changelog append,
	// while a provider row written by a post-apply effect or a mapping shell
	// takes it after one, so two such transactions in the same repository can
	// deadlock. Postgres detects it and aborts one, and the cost is a
	// transient error after deadlock_timeout rather than a hang. It needs
	// user-authored vocabulary that writes llmprovider rows from an effect,
	// and the `credential` lock (auth.go) already has the same shape, so the
	// ordering is a known class rather than this claim's own bug.
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
