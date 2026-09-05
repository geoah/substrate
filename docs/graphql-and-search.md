# GraphQL and search

**The schema is a per-repository projection, and it is not frozen.** It is
generated from the kinds that repository has loaded, so two repositories on one
deployment serve different types, and installing a bundle changes yours.
Introspect it; do not hold a copy. The generation rules below (the naming arms,
the scalars, the interfaces) are also pre-v1: they may change with the
vocabulary, which is why the `search` feature reports `beta` in
[discovery](api.md#what-a-features-stability-means). The frozen-for-v1 promise
belongs to [REST](api.md), when it is made at all.

The whole read/write surface also serves at one endpoint,
`POST /api/v1/graphql`. Filters take the same JSON grammar as
[REST](api.md#the-filter-grammar):

```graphql
query ($f: JSON) {
  records(filter: $f, first: 20) {
    nodes { id kind title ... on Tasks_Task { status } }
    cursor
    head
  }
}
# variables:
# {"f": {"kinds": ["samples.substrate.reamde.dev/tasks/task"],
#        "properties": {"status": {"eq": "open"}}}}
```

`records(filter, orderBy, first, after)` is the list query, and `kinds` is
where a filter narrows by kind, on both `records` and the changelog. The
arguments carry that grammar in their **descriptions**, so a client with only
introspection to read (an agent holding the `graphql` tool) gets the accepted
keys, the condition operators and a worked date range without leaving the
endpoint; a filter key the grammar does not have is a `validation` error that
names the keys it does. `cursor`
is the same opaque keyset token REST returns (pass it back as `after`), and
`head` is the changelog head seq at the snapshot, for a gapless handoff to
`watch`. There is no `total`: a keyset walk counts nothing, so the page tells
you what it read and whether there is more, never how many there are. The `changelog(from, filter, first)` query
resumes forward from a transparent `seq` (the arg is `from`, not an opaque
cursor), returns `{changes, from}`, and echoes the last seq back as `from`.

Every declared kind gets a generated GraphQL type with its properties as
fields; interfaces span packages (`Temporal`, `HasStatus`: everything with
that [trait](traits.md) or that state, so "everything with a
status, anywhere" is one query). Single-record lookups are ref-addressed:
identity is the (kind, id) pair, so `record` takes both:

```graphql
query { record(kind: "samples.substrate.reamde.dev/tasks/task", id: "kq3v9x2m41pf") { id title } }
```

Mutations are the five: `put patch delete merge split`. `put`
carries the whole record in its `input`; every other one names the kind beside
the id:

```graphql
mutation ($k: String!, $id: ID!, $in: JSON!) {
  patch(kind: $k, id: $id, input: $in) { id version }
}
# {"k": "samples.substrate.reamde.dev/tasks/task", "id": "kq3v9x2m41pf",
#  "in": {"properties": {"status": "done"}}}
```

`patch(kind, id, input, ifVersion)` and `delete(kind, id)` address one record;
`merge(kind, winner, loser)` joins two
records of one kind, and `split(mergeId)` undoes one by the `recordmerge`
record's id.

## Generated names and scalars

A GraphQL type name is a pure function of the kind's reference and where the
kind came from, so the schema is deterministic and installing one bundle can
never rename another kind. The rule has two arms. A **seeded kind keeps its
bare singular**: `substrate.reamde.dev/core/token` is `Token`. That is the core
package alone, because creation seeds core and nothing else — every sample a
repository imports, `people` and `tasks` included, installs as the repository's
own. An **installed kind is package-prefixed**: the package's word, TitleCased,
an underscore, then the singular. Two packages may declare the same singular
and stay distinct: `providers.substrate.reamde.dev/notion/page` is `Notion_Page`
and `samples.substrate.reamde.dev/web/page` is `Web_Page`, and the underscore
keeps both out of reach of any seeded name. Two AUTHORITIES publishing a
package of one word are the one ambiguity left, and there the authority's
leading label joins the front for every kind of both packages
(`Acme_Tasks_Task`, `Samples_Tasks_Task`), so a kind's name never depends on
which of its neighbours are installed
([decision 0047](decisions/0047-a-kind-lives-in-a-package.md)). Interfaces
follow the same determinism: one per trait that carries properties (a pure
marker trait adds none), and one per distinct state-property name
(`HasStatus`, `HasProminence`).

Two kinds that still resolve to one name are **refused when the second is
declared**, at the same moment every other narrowing refusal happens, and a
kind whose computed name would land on a structural name (`Record`, `Change`,
`Reference`, a scalar, an interface) is refused at schema build with a
named error. Neither is ever silently renamed.

`version` and `seq` (and the changelog resume seqs, `head` and `from`, and the
`ifVersion` precondition) are the **`Long`** scalar, a 64-bit signed integer
serialized as a JSON number. GraphQL's built-in `Int` is 32-bit, so a
repository's version or seq counter would overflow past 2^31 (about 2.1
billion); `Long` carries the full int64 range on the wire. `first` page sizes
stay `Int`. A JavaScript client should read the
64-bit fields through a 64-bit-safe path if a counter can exceed 2^53, since a
JSON number past that loses precision in the browser's `Number`.

Property types render as their proper shapes. A `repeated` property is a GraphQL
list of its element type for every kind (`[Int]`, `[Float]`, `[Boolean]`,
`[String]`), not a bare scalar. An `object` property (inline structured fields)
renders as the `JSON` scalar, lossless, rather than flattening to `String`. A
`reference` property is its own generated OBJECT type, `<Kind><Property>Reference`:
`ref` is the referent's path as the named `Reference` scalar, `target` resolves
the referent itself (null when the pointer dangles), and each declared link
property is a typed field beside them. A reference that declares no link
properties generates the same object, so adding one later adds a field instead
of replacing a scalar. A client that wants the path alone selects `{ ref }`.

## Search

Search is one query, `search(q, mode, kinds, k)`, served over GraphQL. It has
two arms:

- **Lexical**, on by default for every kind. The title and every
  string-family property index into full-text search, weighted in three bands
  (title first, then declared string properties, then the rest), and `q` takes
  web-search syntax: bare words, quoted phrases, `-exclusions`. A property opts
  out with `fts: false`; secret-typed properties never index.
- **Semantic**, strictly opt-in per property with `embed: true` (the shipped
  vocabulary opts in long prose: message and mail bodies, task and event
  descriptions, and transcripts). Opted-in text is chunked into overlapping windows
  and embedded **asynchronously** after commit, off a queue, so writes never
  wait on an embedding call. Vectors live in Postgres (pgvector) beside
  everything else, 1536 wide
  ([0026](decisions/0026-embedding-vectors-are-1536-wide-or-refused.md)).

`mode` picks `lexical`, `semantic`, or `hybrid` (the default): hybrid runs both
arms, normalizes each against its own best hit, and merges. Every hit carries
the record beside its raw per-arm scores, `lexical` and `semantic`, so a caller
can threshold rather than trust a rank. In a repository that has named no
embeddings provider, hybrid degrades to lexical and `semantic` reports an error
rather than pretending.

**Which model bought the vectors is data, per repository.** The one
[`llmprovider`](agents.md#providers) row declaring `embedModel` is where a
repository buys them, each stored vector names that row and that model, and the
semantic arm scores only the currently resolved pair. Re-point the row and the
older vectors stop being scored rather than being ranked against the new ones:
cosine distance between two models' vectors is not a distance. `substratectl
--dsn … repository reembed <username>` and `POST
/api/v1/embeddings/reembed` queue their replacement,
which the server's drain loop buys a batch at a time.

There are two honest boundaries. There is no REST search endpoint: filtering is
REST's job (`?filter=`), ranking is the GraphQL query's, and discovery says so
rather than leaving a client to try a route: the `search` feature reports
`"surfaces": ["graphql"]` (and `embeddings`, listed only where an embedder is
configured, reports the same), and
[REST and GraphQL](api.md#rest-and-graphql) lists every other difference
between the two surfaces. And the substrate does
retrieval only: it returns typed records with scores, and anything generative
built on top (a RAG loop, an assistant) is a client reading this API like every
other. [Functions](functions.md) run on the shared runner and reach the same
search through a host call, under their declared read allowlist.

One ranking rule is built in: the shipped `person` carries a two-state
`prominence` machine (`utility` at birth, `known` once something promotes it,
an address-book sync or the owner), and search ranks `utility` people below
every `known` match, so the recruiter who emailed once never outranks a
friend. The demotion participates in the top-k ordering, so in a mixed-kind
search a high-scoring utility person can be pushed out of the `k` rows
entirely.

Next: [changelog and watch](changelog.md), the one changelog every write lands in.
