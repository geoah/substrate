---
status: proposed
date: 2026-10-19
decision-makers: George Antoniadis
---

# 0174. The semantic arm is indexed, approximate and bounded

## Context and Problem Statement

The semantic arm of search scored every stored vector of the resolved
provider and model on every query: `MAX(1 - (vec <=> q))` over `embeddings`,
grouped per record, with no index (migration 0001 left one out on purpose).
A production repository is about to hold about 407,000 chunks of 1536-wide
vectors, 2.4 GiB, in a Postgres on a host with 8 GB of memory and a 2 GB
`shared_buffers`. A full scan per query is seconds to minutes, and unlike
the lexical arm the semantic arm had no time budget. In hybrid mode, the
default every agent searches with, a failure to embed the query failed the
whole search, so an embeddings provider outage turned every agent search
into an error.

## Considered Options

- Keep the exact scan and add only a budget
- HNSW over the full-precision vectors
- HNSW over half-precision copies, rescored exactly
- IVFFlat over the vectors

## Decision Outcome

Chosen: HNSW over half-precision copies, rescored exactly, under the lexical
arm's budget, with hybrid falling back to the lexical arm.

Migration 0015 builds `embeddings_vec_hnsw_idx` on
`vec::halfvec(1536)` with `halfvec_cosine_ops`. The arm reads the nearest
`clamp(4k, 100, 1000)` chunks from it for an arm of k records, filtered by
the pair and the kinds as the scan walks the graph
(`hnsw.iterative_scan = relaxed_order`, `hnsw.ef_search` set to the read),
then rescores each candidate exactly against its stored full-precision
vector and keeps each record's best chunk. Half precision halves the index
(about 3 KB a chunk against 6 KB), which is the difference between an index
that may stay in memory on that host and one that cannot; the stored
vectors stay full precision, so a hit's score is the exact cosine.

The exact scan with a budget keeps every answer exact and makes a large
repository's semantic arm time out on every query: the budget would only
turn slow answers into words-only ones. HNSW on full precision doubles the
memory for a recall gain the exact rescoring mostly recovers. IVFFlat needs
data to train its lists on, so an index built on an empty table is useless,
and its lists go stale as the data grows until somebody rebuilds it; HNSW
needs neither.

The arm, the query's embedding included, runs under the service's search
budget (`DefaultSearchBudget`, 5 s). When it runs out, or the provider fails
to embed the query, hybrid answers from the lexical arm alone and its
`note` says the ranking is by words only and why. `capped` is not set: it
tells an agent to narrow the query, and a narrower query does not bring the
arm back. Semantic mode has no other arm and fails, `unavailable` for the
budget. A caller that went away gets its own cancellation, never a
words-only answer.

### Consequences

- Good, because a semantic query reads at most 1000 chunks from an index
  instead of every vector, whatever the repository holds.
- Good, because an embeddings provider outage or a slow index costs a
  hybrid search its meaning, not its answer.
- Bad, because recall is approximate: a record whose nearest chunk the graph
  walk does not reach is missing from the arm, and small data no longer
  proves large data ranks the same.
- Bad, because the migration builds the index inside the boot's transaction
  and holds writes to `embeddings` while it does. On a large existing table
  that is a long, memory-hungry step at upgrade; the migration names the
  `CREATE INDEX CONCURRENTLY` an operator runs first to make it a no-op.
- Bad, because one index serves every repository on the database. A
  repository holding a small share of it, or a search filtered to a rare
  kind, is found by the iterative scan walking past other rows, and
  `hnsw.max_scan_tuples` (20,000 by default) stops that walk: a very
  selective filter can return fewer candidates than asked, and fewer hits.
- Bad, because the reader needs pgvector 0.8 or later for the iterative
  scan.
- Bad, because the two arms run one after the other, each with its own
  budget, so a hybrid search can take up to twice the budget.

### Confirmation

`TestSemanticCandidateReadUsesTheHNSWIndex`
(internal/engine/semanticarm_db_test.go) plans the candidate read and
requires the index in the plan, so a reader whose expression stops matching
the index fails. `TestHybridSearchFallsBackToWordsWhenTheQueryEmbedFails`,
`TestHybridSearchFallsBackToWordsWhenTheSemanticArmRunsOutOfBudget` and
`TestSearchFailsWhenTheCallerCancelsDuringTheSemanticArm` hold the fallback,
the note and the errors. The existing semantic and hybrid tests hold the
ranking on small data.

## More Information

[docs/api.md](../api.md) says what a caller sees. Reopen if recall on real
data proves too low (raise the candidate count, or index full precision), if
the index outgrows memory on the hosts that run it (quantize further, or
partition by repository), or if selective filters starve often enough to
need an index per repository or per kind.
