---
status: accepted
date: 2026-08-17
decision-makers: George Antoniadis
---

# 0020. Embedding vectors are 1536 wide or the provider row is refused

## Context and Problem Statement

Embeddings used to come from one process-wide embedder built at boot from
`SUBSTRATE_LLM_BASE_URL` / `SUBSTRATE_LLM_API_KEY` / `SUBSTRATE_LLM_EMBED_MODEL`,
so the model was an operator's choice and could not change without a redeploy
([#98](https://github.com/geoah/substrate/issues/98)). Moving the choice into an
`llmprovider` record makes the model a repository's data, and a record edit is
not a redeploy: it lands in milliseconds, from the console, with no restart to
notice it. The `embeddings.vec` column is `public.vector(1536)`
(`0001_init.up.sql`), so a row naming `text-embedding-3-large` (3072 wide) would
make every insert fail at the storage layer, one queue drain at a time, with the
record already written and accepted.

## Considered Options

- Keep `vector(1536)`, and refuse at the write any `embedModel` whose native
  output width is not 1536.
- Keep `vector(1536)`, and send OpenAI's `dimensions: 1536` request parameter so
  a wider model is truncated to fit.
- Store the width per row: make the column untyped `public.vector` and record
  each vector's width beside its model.

## Decision Outcome

Chosen: keep `vector(1536)` and refuse the row at the write, because the width
is a storage fact one repository cannot vary, and the write is the only place a
person is present to read the refusal.

The check runs on the merged `llmprovider` row, in the same admission block that
already holds a `trigger` row to its guard and its callable (`write.go`). A row
naming `embedModel` must name a model the engine knows the width of, and that
width must be 1536; anything else refuses, with the model, its width and the
column's width in the message. The known-model table is `internal/embed`'s
`modelDimensions`, the same table `embed.New` already refused unknown models
against at boot, so this narrows nothing that worked before: it moves an
existing refusal from boot to the write, where the person who caused it is
standing.

Requesting `dimensions: 1536` was rejected because an OpenAI-wire gateway is
free to ignore a request field it does not implement, and many do. The answer
then carries 3072 floats, the insert fails, and the failure surfaces in a
background drain loop rather than at the edit that caused it. A parameter whose
effect cannot be checked before the vectors are bought is not a guard.

Storing the width per row was rejected because it makes a mixed table legal.
`vector(1536) <=> vector(3072)` is an error in Postgres, not a low score, so a
repository holding both widths cannot run one semantic query over its own
records; every query would have to filter to a single width first, which is the
fixed width again with more code and a worse failure. An untyped column also
gives up any future HNSW or IVFFlat index, which pgvector builds only on a typed
one.

### Consequences

- Good, because a repository cannot store a vector its own search query cannot
  compare: the width is fixed at the column and asserted at the write.
- Good, because the refusal names the model and both widths, so whoever edits
  the row learns what to name instead without reading the schema.
- Good, because it keeps a vector index possible later; a typed column is what
  pgvector wants.
- Bad, because the model set is closed to what `modelDimensions` lists. A
  1536-wide model nobody has added, and a self-hosted embedder at 768 or 1024,
  are both refused until someone edits Go. Adding another 1536-wide model is one
  line; supporting another width is a migration and a successor to this record.
- Bad, because the width is asserted from a table of model names rather than
  measured. A gateway serving something else under the name
  `text-embedding-3-small` passes the write check, and the mismatch surfaces at
  the drain, where the queue refuses an answer of the wrong length.

### Confirmation

`TestEmbedProviderRowRefusals` in `internal/engine` covers the width refusal (a
row naming a 3072-wide model refuses, naming both widths) and the unknown-model
refusal. `ProcessEmbedQueue` keeps its own width assertion as the second gate,
covered by `TestEmbedQueueRefusesWrongWidth`.

## More Information

The provenance columns that ride with this change (`embeddings.provider`,
`embeddings.model`, migration `0008`) are a separate mechanism and not a
decision: they are what lets `reembed` find the vectors an old model produced,
and what keeps the semantic query from scoring two models' vectors against each
other. Revisit this record when a wanted embedding model is not 1536 wide and
the answer stops being "name a different model".
