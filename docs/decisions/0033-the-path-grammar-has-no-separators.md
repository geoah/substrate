---
status: accepted
date: 2026-08-17
decision-makers: George Antoniadis
---

# 0033. The path grammar carries no separators; a record's URL is its reference

## Context and Problem Statement

The REST path grammar had three defects before v1 froze it
([#202](https://github.com/geoah/substrate/issues/202)). `POST
/api/v1/{plural}/{id}` against a repository-local kind resolved the collection,
dropped `{id}` and created a record under a server-assigned id; `PUT
/api/v1/{authority}/{plural}` was the mirror, a create under a random id. A
client that believed it was upserting accumulated duplicate records. Records,
non-record endpoints and future verbs also shared one namespace: `/graphql`
and `/blobs` took a whole first segment, `recordmerge` and `recordsplit` are
shipped kinds whose collections `POST /{core}/recordmerges` shadowed, and every
verb added after v1 would remove an id from a collection.

An earlier attempt ([#249](https://github.com/geoah/substrate/pull/249))
reserved a `-` segment for verbs (`/{kind}/{id}/-/incoming`). The repository
owner rejected any separator in a URL. This record settles the grammar without
one.

## Considered Options

- Reserve a `-` verb segment at every depth, so a verb can never sit where an
  id can (#249).
- No separator: the collection segment is the kind name, sub-resources hang
  one level below the id, non-record endpoints leave the kind namespace for
  the top level, and lifecycle becomes record state.

## Decision Outcome

Chosen: the separator-free grammar. Three rules carry it.

1. **The collection segment is the kind name, not a plural.** A record's path
   after `/api/v1/` is `{authority}/{kind}/{id}` (or `{kind}/{id}` for a
   repository-local kind), which is exactly the string a `reference` property
   stores (`vocabulary.RecordPath`). One string names a record in a URL, in a
   stored reference and in the changelog. Plurals are gone from routing.

2. **Non-record endpoints leave the kind namespace for the top level.**
   `/api/v1/graphql`, `/changes`, `/blobs`, `/catalog`, `/vocabulary/apply`,
   `/oauth/callback`, `/oauth/start`, `/merge` and `/split` sit at the version
   root, never under an authority and never as a first-segment collection. A
   two-segment kind reference (`{authority}/{kind}`) can never collide with a
   one-segment reserved word, so no separator is needed to keep them apart, and
   `core.substrate.reamde.dev/recordmerge` and `/recordsplit` are reachable
   again because the merge and split actions moved to `/merge` and `/split`.

3. **Actions are sub-resources of the record they act on; lifecycle is
   record state.** A verb hangs one level below the id, where no id can be:
   `POST /{core}/function/{name}/call`, `/agent/{name}/call`,
   `/agent/{name}/chat`, `GET /{kind}/{id}/incoming`, `POST|DELETE
   /{kind}/{id}/edges/{rel}`, the same shape Kubernetes uses for `scale` and
   `exec`. A bundle's enable, disable, uninstall and purge are not verbs but
   transitions of the bundle record's own runtime state, which the substrate
   owns ([0019](0019-a-lifecycle-is-a-state-machine-only-where-the-substrate-owns-it.md)):
   they move to `PATCH /{core}/bundle/{id}` carrying the state change
   (`disabled`, `uninstalled`, `purging`), the managed properties the engine
   already writes. `bind` and the OAuth start are not lifecycle states, so they
   stay sub-paths of the bundle record.

`POST` to a record path and `PUT`/`PATCH`/`DELETE` to a collection path each
answer `405` naming the spelling that works, and never fall through to a write,
which kills both silent creates.

### Consequences

- Good: a record's served URL, its stored reference value and its changelog
  path are one string, so a client that has a reference has a URL.
- Good: both silent creates are structurally impossible; a shape mismatch is a
  `405`, not a create.
- Good: no `%` and no new separator enters the id or authority alphabet, which
  keeps the reserved character budget
  ([0014](0014-authorities-widen-only-outside-the-id-alphabet.md)).
- Bad: a sub-resource name (`incoming`, `edges`) and a top-level reserved word
  (`graphql`, `merge`) shadow a record whose id or a repository-local kind
  whose name is spelled the same. The set is small, fixed and documented; a
  separator would have removed the corner at the cost the owner refused.
- Bad: every client URL changes at once. This is one breaking change
  (`feat(api)!`), taken before v1 so no shipped client is owed a migration.

### Confirmation

`internal/api/grammar_test.go` asserts both silent creates answer `405` and
write nothing, and that a record's URL equals `vocabulary.RecordPath(kind, id)`.
`.mise/docscheck.sh` greps the retired URL shapes (a plural collection, a
`/{core}/catalog`, a `/{core}/vocabulary`) so a doc cannot reintroduce them.
The `addressed` dispatch in `internal/api/rest.go` is the one place the two
path shapes are told apart.

## More Information

Replaces #249 and its unmerged draft record. `apply` stays a distinct
`/vocabulary/apply` endpoint on purpose: a batch of declarations is not record
data, and the schema-apply admission is not the generic write path. A general
transaction primitive (many record writes in one atomic call) is possible
post-v1; it is not this change, which only moves paths. Reopen if kind identity
moves to URLs (#0014's reserved widening), which would revisit whether an
authority is still one path segment.
