---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0108. Search ranks by BM25F and a kind's purpose

## Context and Problem Statement

The ranked read (`GET /api/v1/records?q=`) answered badly on the provider
fixture repositories. A search for `calendar` returned eight kind
declarations, functions and bundles and none of the 30 calendar events.
`ts_rank` has no inverse document frequency and no length normalization, so a
long Drive file that repeats a word scored near 1.0 against the record titled
by it. Every word was required, so a query with one word no record holds
answered nothing. Hybrid added the two arms after scaling each by its own top
hit, so the best semantic hit tied the best lexical one however weak it was.
[0105](0105-a-kind-declares-its-purpose.md) made `purpose` advisory and named
a search that ranks by it as the trigger for its own record.

## Considered Options

For the lexical ranking:

- `ts_rank` or `ts_rank_cd` with a length-normalization flag
- A BM25 extension (ParadeDB's `pg_search`, `pg_textsearch`, VectorChord-bm25)
- BM25F computed from what each `tsvector` already holds

For machinery in the results:

- A filter only, `filter.purposes`, left to each client
- A strict tier: every `primary` hit above every `internal` one
- A filter, plus a weight on the score by purpose

For hybrid:

- The max-normalized sum it had
- Reciprocal rank fusion

## Decision Outcome

Chosen: BM25F computed from the `tsvector`, a `filter.purposes` arm plus a
weight by purpose, and reciprocal rank fusion.

A normalization flag fixes length and not IDF, which is what lets a rare name
beat a common word. The extensions are not in the stock Postgres and pgvector
images an operator runs, and each is a second index to keep beside `fts`. A
`tsvector` holds each lexeme's positions and band labels, which are the
term frequencies and field lengths BM25F needs. So the engine
([bm25.go](../../internal/engine/bm25.go)) gathers candidates through the GIN
index, reads their per-band counts in one statement and computes the sum in
Go. The bands are the fields: title weight 3, short strings 1.5, prose 1.
Each band's length normalizes against its average over the rows holding that
band. The document frequency is counted per word at query time, capped at
20000, and the row count and averages are cached per repository for ten
minutes.

The ranked read's candidates are the relaxed query: a conjunction becomes the
disjunction of its words, exclusions kept. The rows matching every word still
rank first when the lexical arm ranks alone. `filter.search` and `match` are
predicates and keep the strict grammar.

A filter alone leaves every agent and API caller with the flood. A strict tier
buries the one internal record a person searched for by its exact name under
any weak data match. The weight (`primary` 1.0, `supporting` 0.8, `internal`
0.4) moves machinery down without hiding it, and `filter.purposes` removes it
where a client wants that. The console does this in everyday mode.

Reciprocal rank fusion needs no common scale, and BM25 and cosine have none.

The `person` prominence demotion ranked a `utility` person below every other
hit of any kind. Every synced contact is born `utility`, so with machinery in
the results a person searched for by name ranked below kind declarations. It
now keeps its rule within the kind (`utility` below every `known` person) and
halves the score against other kinds.

### Consequences

- Good, because a rare word outweighs a common one, repetition saturates, and
  a title match outranks a long text that repeats the word.
- Good, because no extension, index or table is added for the ranking itself,
  and the tests run on the Postgres image they already use.
- Good, because a query with one absent word still answers, and the answer
  says which hits matched every word by ranking them first.
- Bad, because the scoring reads every candidate's whole `tsvector`, so its
  cost grows with the pool (400 rows, more for a large `first`) and the length
  of what they hold.
- Bad, because the weights, the band parameters and the pool size are
  judgments, tuned on the provider fixtures and a synthetic corpus, not on
  anyone's real repository.
- Bad, because `purpose` is no longer advisory: a provider that marks its main
  kind `internal` now also ranks it lower for every caller, not only in a
  console's navigation.
- Bad, because a strong match on another kind now outranks a `utility`
  person, where before nothing did.
- Bad, because `lexical` in a hit's scores changes meaning, from `ts_rank` in
  [0, 1] to an unbounded BM25F sum, and a caller that thresholded the old
  value must rethink it.

### Confirmation

`TestSearchQuality` and `TestSearchScoresProseWhereProseIsRare`
(internal/engine/searchquality_db_test.go) hold each case above, and each was
shown to fail with the rule it pins removed. `TestBM25Score` and
`TestFuseArms` (internal/engine/searchtext_test.go) hold the arithmetic and
the ordering.

## More Information

This amends 0105's "the server stores the key and acts on nothing": the key,
its values and its default stand. The same change indexes the parts of email
addresses, URLs and paths and a folded copy of accented words, and re-derives
existing rows at the next open (search_index, migration 0008). Those follow
from the code and need no record. Reopen if a BM25 index ships in the stock
Postgres image, or if a real repository's candidate pool makes the scoring
statement slow.
