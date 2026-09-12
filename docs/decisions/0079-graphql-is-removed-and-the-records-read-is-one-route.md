---
status: accepted
date: 2026-09-12
decision-makers: George Antoniadis
---

# 0079. GraphQL is removed, and the records read is one route

## Context and Problem Statement

The API served two surfaces over one set of records. REST listed one kind
at a time (`GET /api/v1/{authority}/{package}/{kind}`), read a record's
fan-in at a sub-resource (`…/{id}/incoming`) and paged a trait's records at
another (`…/trait/{id}/records`); GraphQL, at `POST /api/v1/graphql`, was
kept for the three things REST could not do: a cross-kind query, following a
reference in one round trip and ranked search
([0022](0022-rest-is-frozen-graphql-is-a-projection.md),
[0053](0053-rest-is-supported-all-of-graphql-is-preview.md)). Its one
consumer outside the engine was the agent loop's `graphql` and `mutate` host
functions. Keeping it cost a generated schema per repository, a naming rule
with its own record ([0058](0058-a-graphql-name-always-carries-the-authority.md)),
a 64-bit scalar, a type-name mirror in the console, a dependency, and a
second copy of every read and write to hold to the first. The proposal is
[docs/plans/api-consolidation.md](../plans/api-consolidation.md), drafted and
reviewed jointly; this record binds what it settled.

## Considered Options

- Remove GraphQL, and give REST its three reasons to exist: one cross-kind
  records route with list, ranked and watch modes, a `referencing` filter arm
  for the reverse read, and an `expand` parameter for the forward hop
- Keep GraphQL as the preview surface 0053 made it, and add nothing to REST
- Keep the per-kind collection routes beside a new cross-kind route
- Serve the reverse read as a sub-resource (`…/{id}/incoming`) and the
  forward hop inline on each record, rather than as a filter arm and a
  sidecar
- Let `POST /api/v1/records` accept a client-chosen `id`, as the collection
  `POST` did

## Decision Outcome

Chosen: remove GraphQL entirely, and fold every "read some records" door into
`GET /api/v1/records`. The route has **three modes** told apart by their
parameters: the list (`filter`, `orderBy`, `first`, `after`, `expand`,
`withAnnotations`), the ranked read (`q`, `mode`, `filter.kinds`, `first`)
and the tail (`watch=1`, `filter.kinds`, `from`, `generation`). The three
share `filter` and nothing else: each names the parameters and arms it honors
and refuses the rest by name, because a silently ignored parameter answers
unfiltered rows that look filtered. A ranked page carries `records`, `scores`
and `pending` and no `cursor`, `head` or `generation`, because a ranking opens
no single snapshot and claims none. `POST /api/v1/records` is the one
body-addressed write: it creates under a server-assigned id, the body naming
`kind`, and **refuses a supplied `id`** (`422`); a chosen id is `PUT` at the
record path, so there is one door per answer to "what does a repeat do".
The record path, `GET/PUT/PATCH/DELETE /api/v1/{authority}/{package}/{kind}/{id}`,
is unchanged.

**The reverse read is a filter arm, not a sub-resource.**
`filter.referencing: {ref, property?}` narrows the list to the records
pointing at one record, matched by its canonical id and every former id, and
the page carries a `matches` sidecar keyed by record path naming the property
(and nested path) each record pointed from. It answers in the same envelope
as every other list and composes with `kinds`, `properties`, `orderBy`,
paging and `expand`, which a sub-resource with its own row shape and `total`
never could.

**The forward hop is a sidecar, one hop, keyed by record path.**
`expand=a,b` names declared, non-keyed reference properties of the kinds the
filter admits; their referents come back once each under `included`, keyed by
the path as the pointing row wrote it. A dangling pointer has no entry, an
unknown property is `422`, and more than 500 referents is `422`. Sidecar,
not inline: the wire record is flat, with no `status` key to hide a join
under, so inline would be a new key on every record and a referent repeated
per row.

Gone: `POST /api/v1/graphql`, `internal/gql`, the generated names, the
`Long` scalar, the console's name fold, the `graphql` key in discovery's
`surfaces`, the per-kind collection routes and their `405` stubs,
`…/{id}/incoming`, `…/trait/{id}/records`, the reserved record id
`incoming`, and the `graphql` and `mutate` host functions. `query` takes the
route's grammar as its arguments; a new `write` host function takes
`{op: put|patch|delete, kind, id, input, ifVersion}`, is gated by the calling
agent's effective emit as `mutate` was, and is refused on the direct call API
like `mutate` was. The list cursor now binds the filter as well as the order,
and a cursor from another history generation answers `410 compacted` as the
changefeed does, never a `422` a client reads as its own mistake.

Keeping GraphQL kept a second surface alive for one in-tree consumer, and
every read and write had to be right twice. Keeping the per-kind collection
routes beside the new one would have left two spellings of one list, with two
cursor grammars and two watch doors to hold to each other. A sub-resource for
the reverse read fixed a row shape (`{property, path, from}`, `total`) that
the list's own grammar could not narrow or order. A `POST` that honored an
`id` made the collection `POST` and `PUT` two doors to one upsert with two
answers about a retry; the surviving `POST` mints, the surviving `PUT`
addresses.

**What this does to earlier records.** Of
[0033](0033-the-path-grammar-has-no-separators.md), the record-URL half
stands whole: a record's path is its reference value, non-record endpoints
sit at the version root, actions hang one level below the id, lifecycle is
record state. Its first rule's "collection segment is the kind name" and the
`GET /{kind}/{id}/incoming` example no longer describe a route: there is no
collection path and no incoming sub-resource. Of
[0042](0042-every-kind-carries-an-authority.md) and
[0047](0047-a-kind-lives-in-a-package.md), the sentences that tell a
collection from a record by segment count (two versus three in 0042, three
versus four in 0047) retire with the collection path: a record path is
always the kind's three segments and the id, and a shorter path names
nothing. Every other sentence of the three stands. This record supersedes
0053 (there is no second surface to grade) and 0058 (there is no generated
name to disambiguate). 0022, which 0053 had superseded, points here now,
because a superseded record's successor must itself be accepted.

### Consequences

- Good, because a client, the console, the CLI and an agent's `query` tool
  learn one grammar, and a cross-kind read, a reverse read, a forward hop and
  a ranked read are parameters of one route rather than four doors.
- Good, because the graph is readable in one round trip over REST, which
  0053 recorded as GraphQL's surviving reason and its standing cost.
- Good, because the engine holds one implementation of every read and write;
  there is no second copy to drift, no generated schema to rebuild per
  repository, and no type-name rule to mirror.
- Good, because a `POST` cannot be mistaken for an upsert, and a stale list
  cursor is told apart from a client's own mistake.
- Bad, because every client that listed a kind at its collection path, posted
  to it, read `/incoming` or posted to `/graphql` breaks at once, with no
  compatibility window: the records route is a new spelling, `POST /records`
  refuses the id the collection `POST` took, and discovery's `surfaces` loses
  a key. This is the break the release title carries.
- Bad, because the three modes are one route and not one grammar: a reader
  learns which parameters each mode refuses, and the refusals are the
  documentation.
- Bad, because the ranked read and the tail narrow by `filter.kinds` alone; a
  property predicate on a ranking or a tail is a later step, and until then a
  client filters the top-k or the frames itself.
- Bad, because the console's fan-in drawer lost `total` and its per-property
  grouping in one response: it issues one request per property and counts
  what it gets.

### Confirmation

`internal/api`: the records route's tests pin each mode's parameter set and
its refusals by name, the `404` on an unknown kind in `filter.kinds`, the
`referencing` page with its `matches`, the `expand` sidecar with its
dangling, unknown-property and over-cap cases, the `422` on an `id` in a
`POST /records` body, the `410` on a stale list cursor, and that a
three-segment path answers `404` on every method.
`TestDiscoveryNamesEachSurfaceWithItsCompatibility` pins `surfaces` as the one
`rest` key. `internal/engine`: the list cursor's filter binding and the
`write` tool's emit gate and call-API refusal. `internal/substrate/wire_test.go`
holds the page, ranked page and `matches`/`included` fields in
`wire.golden.json`, and the console's vitest holds `types.ts` to it. The
`.mise/docscheck.sh` rule under "the second surface is gone" refuses
`graphql`, `GraphQL` and `gql`, and the route spellings `/incoming` and
`trait/{id}/records`, on every reader-facing page.

## More Information

Supersedes [0053](0053-rest-is-supported-all-of-graphql-is-preview.md) and
[0058](0058-a-graphql-name-always-carries-the-authority.md), and is the
successor [0022](0022-rest-is-frozen-graphql-is-a-projection.md) now names;
retires the
collection-path sentences of 0033, 0042 and 0047 named above, which
otherwise stand. The plan is
[docs/plans/api-consolidation.md](../plans/api-consolidation.md); the
reader-facing contract is [docs/api.md](../api.md). Revisit if a second
request surface is wanted again (discovery's `surfaces` stayed keyed for it),
if the ranked read or the tail needs property predicates pushed into the
ranking or the frame filter, or if a two-hop or single-record `expand` is
asked for, each of which is additive to this route.
