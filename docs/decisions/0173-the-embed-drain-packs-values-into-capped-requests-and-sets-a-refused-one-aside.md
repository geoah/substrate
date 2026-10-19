---
status: proposed
date: 2026-10-19
decision-makers: George Antoniadis
---

# 0173. The embed drain packs values into capped requests and sets a refused one aside

## Context and Problem Statement

The embed queue drained 64 (record, property) jobs a minute per repository,
each job one embeddings request holding every changed chunk of its property,
and a pass stopped at its first error (`internal/engine/search.go`
`ProcessEmbedQueue`, `cmd/substrated/main.go` `drainEmbeds`). A repository
that names an embeddings provider after years of writes meets all three
limits at once: geoah.me holds 299,008 queued jobs, about 82M tokens. At 64
jobs a minute that is 3.3 days whatever the endpoint's speed. A long value
(a meeting transcript) passes OpenAI's 2,048-input or 300,000-token request
cap, or the client's 30 s timeout, and is refused. And because the drain
takes the oldest job first and stops at the first error, that job stays at
the head of the queue and nothing behind it is ever embedded.

## Considered Options

- Keep one request per job, count each job's failures and drop it after a
  few (an attempts column on `embed_queue`)
- Embed several jobs at once on parallel workers
- Pack the changed chunks of a batch's jobs into requests under explicit
  caps, retry a refused request job by job and move a refused job to the back
  of the queue, and keep draining for most of each tick

## Decision Outcome

Chosen: pack, set aside, and keep draining.

A pass plans every job it took (reads only), then packs whole jobs' changed
chunks into requests of at most 256 inputs and 250,000 bytes of text. A token
is at least one byte of UTF-8, so the byte cap keeps a request under OpenAI's
token cap in any language. A job whose chunks pass a cap on their own gets
requests of its own. The client marks an endpoint's refusal of the input
(HTTP 400, 413 or 422) as `embed.ErrRejected`. A gateway answers 400 for a
model it does not route as readily as for an input it cannot take, so after a
refusal the pass sends the endpoint one known-good input: if that is refused
too, the endpoint is refusing, and the pass stops with the error and moves
nothing. Otherwise the refusal is about the values. A refused request of
several jobs is retried one job at a time; a job refused on its own keeps its
generation, moves to the back of the queue (`enqueued_at` set to now, and
only while the generation it planned stands) and is logged as a warning. Any
other failure (a timeout, a 429 or 5xx, a refused key) still stops the pass,
because it says the endpoint cannot be used right now. The drain loop takes a
batch from each repository in turn, and again, until each applied nothing in
its last batch or 50 seconds have passed, checking the deadline before every
batch and starting each tick at the next repository. Migration 0014 indexes
the queue by `(repository, enqueued_at)`, so a batch reads its own rows
instead of sorting the whole queue.

Parallel workers were not taken: packing reaches the same throughput with one
request in flight, which suits a small gateway and a rate limit, and keeps
the commits in queue order. An attempts column was not taken: once requests
fit the caps no shipped input is refused, so a refused value is rare, and
moving it back needs no migration.

### Consequences

- Good, because a backlog drains at the endpoint's speed: a batch of 64 short
  values is one request instead of 64, and a tick takes as many batches as
  fit in 50 seconds.
- Good, because a long value is split into requests the endpoint accepts,
  and a value it refuses no longer stops the queue behind it.
- Bad, because a refused value is retried, and warned about, each time the
  queue reaches it again, until its text changes or its kind stops embedding
  it.
- Bad, because a refusal costs one more request, the known-good check, once
  per pass.
- Bad, because a batch in flight at the deadline finishes, so a tick can run
  past 50 seconds by one batch, and longer when a refused batch is retried
  one job at a time.
- Bad, because during a backlog the drain now holds the database and the
  endpoint for most of every minute instead of a few seconds.

### Confirmation

`TestEmbedQueueBatchesJobsIntoRequests`,
`TestEmbedQueueSetsARejectedValueAside`,
`TestEmbedQueueStopsOnAnUnusableEndpoint`,
`TestEmbedQueueStopsWhenTheEndpointRefusesEverything`,
`TestEmbedQueueKeepsAnEditMadeDuringARefusal` and
`TestEmbedBatchReadUsesTheQueueOrderIndex` (`internal/engine`),
`TestEmbedTellsARejectedInputFromAnUnusableEndpoint` (`internal/embed`), and
`TestTheEmbedDrainTakesBatchesUntilTheQueuesAreEmpty`,
`TestTheEmbedDrainStopsAtTheDeadline`,
`TestTheEmbedDrainChecksTheDeadlineBeforeEveryBatch` and
`TestTheEmbedDrainRotatesItsFirstRepository` (`cmd/substrated`).
