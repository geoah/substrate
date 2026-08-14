# GraphQL and search

The whole read/write surface also serves at one endpoint,
`POST /api/v1/graphql`. Filters take the same JSON grammar as
[REST](api.md#the-filter-grammar):

```graphql
query ($f: JSON) {
  records(filter: $f, first: 20) {
    nodes { id kind title ... on Task { status } }
    cursor
    head
  }
}
# variables:
# {"f": {"kinds": ["tasks.substrate.reamde.dev/task"],
#        "properties": {"status": {"eq": "open"}}}}
```

`records(filter, orderBy, first, after)` is the list query, and `kinds` is
where a filter narrows by kind, on both `records` and the changelog. `cursor`
is the same opaque keyset token REST returns (pass it back as `after`), and
`head` is the changelog head seq at the snapshot, for a gapless handoff to
`watch`. There is no `total`: a keyset walk counts nothing, so the page tells
you what it read and whether there is more, never how many there are. The `changelog(from, filter, first)` query
resumes forward from a transparent `seq` (the arg is `from`, not an opaque
cursor), returns `{changes, from}`, and echoes the last seq back as `from`.

Every declared kind gets a generated GraphQL type with its properties as
fields; interfaces span authorities (`Temporal`, `HasStatus`: everything with
that [trait](data-model.md#traits) or that state, so "everything with a
status, anywhere" is one query). Single-record lookups are ref-addressed:
identity is the (kind, id) pair, so `record` takes both:

```graphql
query { record(kind: "tasks.substrate.reamde.dev/task", id: "kq3v9x2m41pf") { id title } }
```

Mutations are the seven — `put patch delete link unlink merge split`. `put`
carries the whole record in its `input`; every other one names the kind beside
the id, and the two ends of an edge each name their own:

```graphql
mutation ($k: String!, $id: ID!, $in: JSON!) {
  patch(kind: $k, id: $id, input: $in) { id version }
}
# {"k": "tasks.substrate.reamde.dev/task", "id": "kq3v9x2m41pf",
#  "in": {"properties": {"status": "done"}}}
```

`patch(kind, id, input, ifVersion)` and `delete(kind, id)` address one record;
`link(rel, srcKind, src, dstKind, dst, props)` and its `unlink` twin take a
kind reference per endpoint, `dstKind` required on a `to: any` edge and
supplied by the declaration otherwise; `merge(kind, winner, loser)` joins two
records of one kind, and `split(mergeId)` undoes one by the `recordmerge`
record's id.

## Generated names and scalars

A GraphQL type name is a pure function of the kind's reference and where the
kind came from, never of which other kinds happen to be installed, so the
schema is deterministic and installing one bundle can never rename another
kind. The rule has three arms. A **repository-local kind capitalizes**:
`task` is `Task`. A **shipped kind keeps its bare singular**:
`people.substrate.reamde.dev/person` is `Person`, `calendar.substrate.reamde.dev/calendarevent` is
`Calendarevent`. An **installed kind is always authority-prefixed**: the
leading label of its authority, TitleCased, an underscore, then the singular.
Two bundles may declare the same singular and stay distinct, because the
full reference separates them: `notion.bundles.substrate.reamde.dev/page` is
`Notion_Page` and `web.bundles.substrate.reamde.dev/page` is `Web_Page`, and the
underscore keeps both out of reach of any bare name. Interfaces follow the
same determinism: one per trait that carries properties (a pure marker
trait adds none), and one per distinct state-property name
(`HasStatus`, `HasProminence`).

Two kinds that still resolve to one name are **refused when the second is
declared**, at the same moment every other narrowing refusal happens, and a
kind whose computed name would land on a structural name (`Record`, `Change`,
`Edge`, `Reference`, a scalar, an interface) is refused at schema build with a
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
`reference` property is the `Reference` SCALAR: the referent's path,
`<kind>/<id>`, one string rather than a pair. It stays a named scalar so a
client can still tell a pointer from prose and deep-link it.

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
  descriptions, transcripts, and media summaries). Opted-in text is chunked into overlapping windows
  and embedded **asynchronously** after commit, off a queue, so writes never
  wait on an embedding call. Vectors live in Postgres (pgvector) beside
  everything else, and the embedding model is deployment configuration, not
  schema.

`mode` picks `lexical`, `semantic`, or `hybrid` (the default): hybrid runs both
arms, normalizes each against its own best hit, and merges. Every hit carries
the record beside its raw per-arm scores, `lexical` and `semantic`, so a caller
can threshold rather than trust a rank. On a deployment with no embedder
configured, hybrid degrades to lexical and `semantic` reports an error rather
than pretending.

There are two honest boundaries. There is no REST search endpoint: filtering is REST's
job (`?filter=`), searching is the GraphQL query's. And the substrate does
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
