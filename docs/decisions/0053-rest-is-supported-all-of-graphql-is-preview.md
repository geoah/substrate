---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0053. REST is the supported interface; all of GraphQL is preview

## Context and Problem Statement

Discovery (`GET /.well-known/substrate/server.json`) stamps one `stability`
per feature and says nothing per surface, so `changefeed` reads `beta` for
`rest` and `graphql` together and a client cannot tell which of the two it may
build on ([#361](https://github.com/geoah/substrate/issues/361)).
[0022](0022-rest-is-frozen-graphql-is-a-projection.md) gave GraphQL's
"structural half" (the reads, the mutations, `Record`'s own fields, the
scalars) REST's additive promise. Nothing held it: the schema emits no
`@deprecated`, the documented mutation count drifted from seven to five
without a test noticing, and the naming
([#382](https://github.com/geoah/substrate/issues/382)) and integer scalar
([#383](https://github.com/geoah/substrate/issues/383)) fixes both change the
half that was promised frozen. The release tracker
([#360](https://github.com/geoah/substrate/issues/360)) needs the verdict
recorded before the REST compatibility promise is written, and separately from
it.

## Considered Options

- A top-level discovery object naming each surface with its endpoint and a
  compatibility verdict; per-feature `stability` stays the maturity axis
- A per-surface stability on every `features` entry
- A fourth `stability` value, `preview`, stamped on the GraphQL-only features
- Keep 0022's structural-half promise and hold it with a schema snapshot test

## Decision Outcome

Chosen: the top-level object. REST is the supported developer interface, and
every part of GraphQL, the generated types, the root operations and the
scalars alike, is a preview that may change without a v1 wire break. Discovery
carries `surfaces`, keyed by the names a feature's `surfaces` list already
uses: `rest` at `/api/v1` with `compatibility: supported`, `graphql` at
`/api/v1/graphql` with `compatibility: preview`. Feature `stability` keeps its
three values and its meaning (how far one feature's shape has settled), `alpha`
stays on `agents` and `embeddings`, and no REST feature moves to `stable` here:
that follows the P0 wire changes, and `changefeed` waits for
[#373](https://github.com/geoah/substrate/issues/373),
[#374](https://github.com/geoah/substrate/issues/374) and the minimum
[#377](https://github.com/geoah/substrate/issues/377) contract. The REST
compatibility text (paths, fields, errors, pagination, PUT merge) is
[#131](https://github.com/geoah/substrate/issues/131). `embeddings` lists
`rest` beside `graphql`, because `POST /api/v1/embeddings/reembed` is a REST
verb. The GraphQL code stays: `internal/engine/agentgql.go` and the
`substrate.reamde.dev/core/graphql` host function execute it, and a client
that needs ranking still posts to it under preview terms.

Compatibility is a property of a surface, so repeating it on every feature
would say the same thing eight times and give a feature served on both
surfaces two stamps to keep aligned. A fourth stability value would spend the
one additive-only vocabulary 0022 warned is expensive to correct, and it would
let a feature say "preview" while the question is which door, not which
feature. Keeping the structural-half promise would block #382 and #383 or
break the promise on the first of them to land; a snapshot test would have
held a contract no client outside the engine depends on.

Of 0022, this supersedes the structural-half sentence and the `@deprecated`
sunset promise the API page derived from it. The rest stands and is restated
here: every feature names its `surfaces`, the list is never empty, `search` is
served on GraphQL alone, a REST search route is not added while the hit shape
is unsettled, and surfaces describe a feature's own verbs, never its records.

### Consequences

- Good, because a client reads one object to pick its surface, and a preview
  surface names its endpoint so it is locatable without the docs.
- Good, because #382 and #383 can change GraphQL names and scalars as a
  preview, announced, without a v1 wire break.
- Good, because the docs stop promising a `@deprecated` marker nothing emits.
- Bad, because `supported` is not `stable`: every REST feature still reports
  `beta`, so a client has the surface verdict before it has the field-level
  promise, and the two stay separate axes it must read together.
- Bad, because a REST-only client still cannot search; 0022's cost is kept.
- Bad, because `compatibility` is a second closed vocabulary (`supported`,
  `preview`) beside `stability`, and both are held by hand.

### Confirmation

`TestDiscoveryNamesEachSurfaceWithItsCompatibility` in `internal/api` pins the
object, its two values and that the advertised GraphQL endpoint answers;
`TestDiscoveryFeaturesNameTheirSurfaces` pins `embeddings` on both surfaces;
`TestDiscoveryStampsEachFeatureStability` pins that no stamp moved. The
`.mise/docscheck.sh` rule under "the retired GraphQL promise" refuses `seven
mutations`, `structural half` and `@deprecated` on every reader-facing page.

## More Information

Supersedes the structural-half sentence of
[0022](0022-rest-is-frozen-graphql-is-a-projection.md); its per-feature
`surfaces` marker and its search reasoning are carried forward above. Revisit
when a client outside the engine depends on the GraphQL schema after #382 and
#383 settle; a promise for it would be a new record. The `beta` to `stable`
flip on REST is tracker step 4 of #360, with #131.
