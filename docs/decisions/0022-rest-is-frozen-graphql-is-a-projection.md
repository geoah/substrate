---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0022. REST is the frozen v1 contract; GraphQL is a derived projection

## Context and Problem Statement

Discovery advertised `search` as a stable feature while no REST route served
it ([#129](https://github.com/geoah/substrate/issues/129)): search is the
GraphQL query's alone, so a client that read `stability: stable` as "a route
exists" went looking for one that never shipped. The same question sits behind
every other place the surfaces differ (reverse edges, the watch tail,
`propertyMeta`), and the v1 freeze is close
([#126](https://github.com/geoah/substrate/issues/126)), so what each surface
promises has to be in the contract before third parties script against it.

## Considered Options

- Add a REST search route, so every advertised feature is served on both
  surfaces
- Mark each feature with the surfaces that serve it, and write down the rule
  that permits the asymmetry
- Drop `search` from the discovery feature list

## Decision Outcome

Chosen: mark the surfaces per feature and write the rule down. REST is the
frozen v1 contract, additive-only under `/api/v1`, and a client may pin its
paths and field names. GraphQL is a projection derived from the vocabulary and
rebuilt per repository, so its generated types follow whatever kinds are
installed and a client reads them by introspection; only the structural half
(the four reads, the seven mutations, `Record`'s own fields, the scalars)
carries REST's additive promise. `featureInfo` gains `surfaces` (`rest`,
`graphql`, or both), non-empty for every entry, and `search` and `embeddings`
carry `graphql` alone. Surfaces describe a feature's own verbs, never its
records: every kind's records read on both surfaces regardless. Two entries
change what they claim. `search` names the surface that serves it, and
`embeddings` is listed only where the deployment has an embedder configured,
because without one nothing drains the embed queue and the semantic arm
refuses.

Adding a REST search route would freeze a ranking API at the moment its shape
is least settled: hybrid scoring, the per-arm `lexical` and `semantic` scores
and the `prominence` demotion are all pre-v1, and a frozen route pins the hit
shape plus the `mode` and `k` semantics into an additive-only surface.
Filtering answers "which rows match" and REST already serves it (`?filter=`);
ranking answers "which rows are best" and the GraphQL query serves that.
Dropping `search` from the feature list would lose the only honest answer to
"does this deployment search at all".

### Consequences

- Good, because a client picks its surface from discovery instead of probing
  for a route, which is what the feature list exists for.
- Good, because search's shape stays free to change under an introspectable
  schema until a REST route is worth freezing, and adding one later is
  additive.
- Bad, because a REST-only client (curl, a shell script, an HTTP-only runtime)
  cannot search at all and has to speak GraphQL for it.
- Bad, because two surfaces with different promises is more contract to hold:
  every feature added from here on has to answer where it is served, and
  the asymmetry list in `docs/api.md` is maintained by hand.
- Bad, because `surfaces` lands in a document that is additive-only, so a
  value that turns out wrong is expensive to correct.

### Confirmation

`TestDiscoveryFeaturesNameTheirSurfaces` in `internal/api` pins the surface
list for every feature, refuses an empty one and fails on a feature it does
not know; `TestSearchHasNoRESTRoute` pins the generic-collection 404 the
gql-only marker claims, and `TestDiscoveryOmitsEmbeddingsWithoutAnEmbedder`
pins the conditional entry. The asymmetry table lives in
[the API page](../api.md#rest-and-graphql); nothing checks that it stays
complete, so that half is held by review.

## More Information

Revisit when a REST client needs retrieval without GraphQL, or when the hit
shape settles enough that freezing `/search` costs nothing. Either is an
additive change to the surfaces list, not a reversal of this record.
