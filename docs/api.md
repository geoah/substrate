# The API

The substrate serves one set of operations for everything: four reads
(`record`, `records`, `search`, `changelog`) plus a watch stream, and five
mutations. REST serves all of them but `search`, which is the GraphQL query's
alone. REST is the supported developer interface and the whole of GraphQL is a
preview; [discovery](#discovery) says so per surface. A new kind never adds an
endpoint: the REST path pattern is the same routes for every package, and the
GraphQL schema is generated from the loaded kinds.
This page is both surfaces: the REST routes, the filter grammar, pagination,
the mutations, discovery, the generated GraphQL names, search and the errors.
Authentication has a page of its own,
[users and tokens](auth.md). The record model, the filter grammar and the
mutations hold identically over REST and GraphQL; where the two surfaces
differ, [REST and GraphQL](#rest-and-graphql) below lists how.

## REST resources

Every package serves the same routes; the collection segment is the kind's
**name**, so a record's path after `/api/v1/` is exactly its
[reference](data-model.md#kinds-and-references) value
([decision 0033](decisions/0033-the-path-grammar-has-no-separators.md),
[decision 0047](decisions/0047-a-kind-lives-in-a-package.md)):

```http
GET    /api/v1/{authority}/{package}/{kind}       # list, filter, watch
POST   /api/v1/{authority}/{package}/{kind}       # create, server assigns the id
GET    /api/v1/{authority}/{package}/{kind}/{id}
PATCH  /api/v1/{authority}/{package}/{kind}/{id}  # patch, including state transitions
PUT    /api/v1/{authority}/{package}/{kind}/{id}  # upsert at the given id
DELETE /api/v1/{authority}/{package}/{kind}/{id}  # soft delete; ?ifVersion= guards it
GET    /api/v1/{authority}/{package}/{kind}/{id}/incoming   # who points at this record
```

A method sent to the wrong shape answers `405` naming the spelling that works,
and never falls through to a write: `POST` at a record path used to drop the id
and create under a server-assigned one, and `PUT` at a collection created under
a random one. Both are refused now.

**There is no repository segment anywhere.** The bearer token names the
repository, so an address never has to, and there is nothing to get wrong.

The path carries a record's **full identity**: `{authority}/{package}/{kind}`
names the kind, `{id}` the id within it. Ids are unique per kind, so the same
id may exist in two collections as two unrelated records, and a resource read is
always scoped to its own collection. There is no cross-kind read by bare id
anywhere on the surface.

Every kind carries an authority and a package
([decision 0042](decisions/0042-every-kind-carries-an-authority.md),
[decision 0047](decisions/0047-a-kind-lives-in-a-package.md)), so the
two path shapes are told apart by segment count, in one place: a three-segment
path is a collection (`{authority}/{package}/{kind}`), a four-segment path a
record (`{authority}/{package}/{kind}/{id}`). There is no shape that leaves
either out, and no dot rule.

One id form needs care. A [kind declaration](vocabulary.md)'s id **is** a kind
reference, so it carries a `/`. A client percent-encodes it, and the API
decodes it exactly once:

```http
GET /api/v1/substrate.reamde.dev/core/kind/samples.substrate.reamde.dev%2Ftasks%2Ftask
```

Incoming references are a derived view of their own, paged separately so a
popular record's fan-in never inflates its document. The response is
`{"incoming": [{"property": …, "path": …,
"from": {"id", "kind", "title"}}], "cursor": …, "total": n}`, ordered by
source kind, then source record, then the property and its position within
it. `property` names the reference the source points with, and `path` locates
it where the reference sits inside an object or a keyed map, empty at the top
level. Every reference answers here, pinned or not. The `cursor` belongs to the
read that minted it: replaying one with a different `property`, `fromKind` or
target record is a `400`, not a short page. A merge INTO the target refuses
outstanding cursors the same way, because the merged-away record's pointers
join the order mid-walk; start the walk again.

## The flat record

Requests and responses carry the **flat record**: one JSON object with
`properties` and the server-set fields at the top level. The
four-key [envelope](data-model.md#the-envelope) is the YAML document form;
REST never wraps. `title`, `body`, and the temporal properties appear inside
`properties` and nowhere else, so `PutInput` and `PatchInput` accept them only
there. `PutInput` carries an optional top-level `id` and an optional
`ifVersion`; the CLI is what maps `metadata.id` and `metadata.ifVersion` onto
them. The `id` is how a POST to a collection names the record it creates; on a
`PUT` the path already names it, so the path is what the write addresses.

A worked sequence over the to-do list. Add a task (the kind comes from the
path):

```http
POST /api/v1/samples.substrate.reamde.dev/tasks/task
{"properties": {"name": "Buy milk", "dueAt": "2026-08-13T09:00:00Z"}}

→ 201 {"id": "kq3v9x2m41pf", "kind": "samples.substrate.reamde.dev/tasks/task",
       "properties": {"name": "Buy milk", "title": "Buy milk", "status": "open",
                      "dueAt": "2026-08-13T09:00:00Z"},
       "version": 1, "createdAt": "2026-08-04T10:00:00Z",
       "updatedAt": "2026-08-04T10:00:00Z"}
```

List what is open, soonest first (the filter is URL-encoded JSON, the grammar
is below):

```http
GET /api/v1/samples.substrate.reamde.dev/tasks/task
      ?filter={"properties":{"status":{"eq":"open"}}}&orderBy=dueAt

→ {"records": [...], "cursor": "eyJv…", "head": 4207, "generation": "7f3a0c2e9b1d4e6f"}
```

Complete one. A state change is just a patch, and the
[declaration](data-model.md#validation-and-state-machines) stamps
`completedAt`:

```http
PATCH /api/v1/samples.substrate.reamde.dev/tasks/task/kq3v9x2m41pf
{"properties": {"status": "done"}}
```

Read the person GitHub linked up. Single-record reads also carry
`propertyMeta` (per property: who wrote it, at which tier, and the
alternatives other sources assert,
[managed properties](projection.md#managed-properties) explains the
mechanism), and, if you asked by an id that was merged away, `canonicalId`
tells you where it went ([merges](projection.md#merges)):

```http
GET /api/v1/samples.substrate.reamde.dev/people/person/9f2k

→ {"id": "9f2k", "kind": "samples.substrate.reamde.dev/people/person",
   "properties": {"name": "Ada Lovelace", "emails": ["ada@example.com"]},
   "propertyMeta": {"name": {"manager": "console", "tier": "owner",
     "updatedAt": "2026-08-04T09:12:00Z",
     "alternatives": [
       {"actor": "function:providers.substrate.reamde.dev:github:githubsync",
        "value": "ada", "updatedAt": "2026-08-04T08:00:00Z"}]}}}
```

An alternative's `updatedAt` is its source record's, not the target's;
[reading provenance](projection.md#reading-provenance-propertymeta) has the
rule.

## The five mutations

The complete write surface, for every actor, forever. Each one addresses its
target by **full identity**: the kind beside the id (on REST the path's
`{authority}/{package}/{kind}` names the kind; on GraphQL the kind travels in
the mutation's arguments, as `kind` on `patch`, `delete` and `merge`, and inside
`input` on `put`), because an id is unique per kind, never per repository:

| Mutation | What it does                                                                                                                                                        |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `put`    | Create or upsert. Merges and never prunes: what the document omits is left alone.                                                                                   |
| `patch`  | Edit in place: properties, labels, annotations. A null value deletes a key. State [transitions](data-model.md#validation-and-state-machines) travel only this way. |
| `delete` | Soft delete: tombstones the record; hard deletion waits for finalizers to release.                                                                                  |
| `merge`  | Join two records of one kind; the loser's id resolves to the winner while the winner exists ([merges](projection.md#merges)).                                       |
| `split`  | Reverse one merge, restoring the loser from the merge record.                                                                                                       |

A pointer at another record is a property, so it is written by `put` and
`patch` like every other value, and a record and everything it points at commit
as one write. A mapping's subject reference is the one a generic write may not
change once the record exists; create-time resolution, `merge` and `split` are
what move it.

A `put` onto a tombstone restores that record: same id, same row, one
changelog row saying so. The tombstone lasts until the garbage collector's
next pass (every five minutes) purges it, or for as long as a finalizer holds
it ([operations](operations.md#what-happens-at-boot)); after the purge the id is free, the same `put` creates a fresh record with a new
history, and a reference another record still holds resolves to the new one.
Ids are stable while a record exists; they are not promised unique across
time, so a writer that composes an id from a provider's key may delete and
recreate at will.

A `put` addressed to a former id (the loser of a merge) is refused `409
conflict` naming the canonical id: a supplied id is the writer's own key, not
an address to resolve. Reads, `patch` and `delete` through a former id resolve
to the canonical record
([merges](projection.md#former-ids-resolve-to-the-winner)).

Every mutation takes an optional version precondition, and a stale one fails
the whole write with a `conflict` (`409`) and changes nothing. `put` and
`patch` take `ifVersion` in the body: the write applies only if the addressed
record's stored version equals it (a non-existent record is version 0). `delete`
takes it as the `?ifVersion=` query parameter, since a `DELETE` body is dropped
by enough clients to be no place for a guard; a delete addressed through a
former id compares the canonical record, the row the tombstone lands on. `merge`
takes `winnerVersion` and `loserVersion` in its body, each optional, each
holding that one participant; `split` takes `ifVersion` on the `recordmerge`
record alone, because the pair change with every edit after the merge and a
split keeps those edits. Every one of them is the safe read-then-conditional-write
primitive, and every one moves the version it checked, so a retry under the
same precondition is a `conflict` rather than a silent second effect.

```http
DELETE /api/v1/samples.substrate.reamde.dev/people/person/9f2k?ifVersion=4
POST   /api/v1/merge   {"kind": "samples.substrate.reamde.dev/people/person",
                        "winner": "9f2k", "loser": "7hd1",
                        "winnerVersion": 4, "loserVersion": 2}
POST   /api/v1/split   {"merge": "m3x8", "ifVersion": 1}
```

A record's own derived views hang off its path too, which is where
`…/{id}/incoming` sits. That follows one rule, written into the contract: **a
resource's operational verbs live at the resource**, its own
`{authority}/{package}/{kind}/{id}` path. That is
also why the trigger verbs live under `substrate.reamde.dev/core/trigger/…` — trigger
records are core's, so their verbs sit beside them.

### Idempotency and retries

A retried write is safe when the request names its own target. `put` with an id
is a primary-key upsert, so retrying it lands the same row. Any of the five
mutations under its version precondition (`ifVersion` on `put`, `patch`,
`delete` and `split`, `winnerVersion` and `loserVersion` on `merge`) is
compare-and-set: the second attempt sees the version it already moved and fails
`conflict`. A blob `PUT` is content addressed by its digest. The trigger
delivery path carries its own idempotency key, so a redelivered change applies
once.

A retried write is NOT safe on its own when the server assigns the identity or
the effect. A `POST /api/v1/{authority}/{package}/{kind}` with no id mints a
random id, so a client that retries after a timeout creates a second record.
`POST …/core/function/{name}/call` and `…/core/agent/{name}/call` run the body
again, with its effects. A retried `merge` or `split` under a version
precondition fails `conflict`, because the first attempt moved the versions,
and the client cannot tell that from a merge somebody else made.

Those five operations take the `Idempotency-Key` request header: the client's
name for one attempt, any string up to 255 bytes (a UUID is the usual choice).
A function or agent body hands its own external effects an idempotency key
derived from the client's, so a retry presents the same key downstream too.

```http
POST /api/v1/samples.substrate.reamde.dev/tasks/task
Idempotency-Key: 6f1c2e3a-9b0d-4c7e-8a21-5d3f0b9e7c44
Content-Type: application/json

{"properties": {"name": "file the report"}}
```

The contract, per key:

- The effect runs once. A repeat under the same key with the same body
  answers the first attempt's outcome, the same body and the same status code
  (`201` for the record a create, a merge or a split made, `200` for a call), for 24 hours from
  the moment the first attempt's outcome committed (not from when the request
  arrived); after that the key is free again. The key is
  looked up before the callable is resolved or admitted, so the repeat
  answers even after the function or agent was disabled, uninstalled or
  redeclared in between.
- The key binds to the repository and the operation, never to the token: a
  retry after `logout` and `login` still matches, and the same string sent to
  `/merge` and to a create is two keys.
- The same key with a different body is `409 conflict`. So is a repeat that
  arrives while the first function or agent call is still running: the
  server does not hold the second request open for a body of unknown length,
  and the client retries after the first answers. A create, merge or split
  runs inside the repository's one write transaction, so its repeat waits for
  that commit and then answers the stored outcome. A running call's claim on
  its key lasts its own deadline plus a minute of slack (a function call adds
  two minutes for provisioning a PEP 723 body before its timeout starts); a
  claim a dead server left behind is cleared when the repository next opens.
- A failed attempt stores nothing. A `422`, a `500 function_failed` or a
  connection lost before the commit leaves no key behind, and the retry runs
  the operation again.
- An agent call binds its key to the thread the moment the thread opens,
  because the loop's tool effects commit one by one before the run settles.
  A repeat after the first attempt failed mid-run, or after the server died
  before settling, is `409 conflict` naming the thread: the client reads the
  thread (its messages record every effect) and runs again under a new key.
  One key never opens two threads.
- A stored outcome is capped at 1 MiB. A larger one is not kept: the effect
  still ran once, and the repeat is `409 conflict` saying the outcome was not
  retained. For a create, merge or split the message names the record the
  first attempt wrote, so the client reads it instead.
- Agent chat streams and is excluded; there is no stored outcome to replay.
  `vocabulary/apply`, catalog import and `POST /tokens` do not take the header,
  and a `POST` to a vocabulary kind's collection (`kind`, `trait`, ...) refuses
  it (`422`).

The key store is a Postgres table, `idempotency_keys`, and not part of the
changelog: a repository restored from its directory alone
([Backups](operations.md#backups)) forgets every key, and the first retry
after such a restore runs once more. Keys survive a restart and a
`repository rebuild`, which replay nothing the keys answer for.

Whether a create needs the header depends on the kind. A kind no
`recordmapping` points at accepts a client-supplied id: `put` at
`…/{kind}/{id}` creates the record on the first attempt and upserts the same
row on the retry. A kind some mapping points at does not: `checkCreateID`
refuses a client id on a record that does not exist yet, a `validation` error
(`422`) saying "ids are server-assigned", because nothing external names a
subject ([0049](decisions/0049-the-owner-of-a-mappings-target-declares-it.md)).
The `people` and `tasks` samples ship mappings onto their own `person` and
`task`, so those two are server-assigned from the moment the mappings land
([Suggested mappings](bundles.md#suggested-mappings)), and `Idempotency-Key`
on the `POST` is the one safe create for them.

## The filter grammar

A filter is one JSON document, the same shape URL-encoded in REST's `?filter=`
and passed whole to GraphQL's `filter` argument:

```json
{"kinds": ["samples.substrate.reamde.dev/tasks/task"],
 "properties": {"status": {"eq": "open"},
                "dueAt": {"lt": "2026-08-11T00:00:00Z"}},
 "labels": {"owner/starred": {"eq": true}}}
```

- `kinds` names kinds by reference (implied by the path on a REST collection).
- `properties` carries one condition per property. The operator set is one
  rule, not a per-type table: `secret` and `digest` properties refuse
  filtering entirely, `reference` takes `eq`, `in`, `contains` and `exists`
  and is filtered by the PATH string it points at (not by the object the read
  serves), and every other declared property takes the full grammar (`eq`, `gt`,
  `gte`, `lt`, `lte`, `in`, `prefix`, `contains`, `exists`), compared as its
  declared [property type](data-model.md#property-types). State properties
  filter here like any other.
- `labels` matches the short, indexed metadata, and takes the **same condition
  objects** a property does: `{"owner/starred": {"eq": true}}`, never a bare
  value.
- `ids` narrows to a list of ids within the kinds already selected.
- `deleted` picks the tombstones: absent or `false` lists only live records,
  `true` lists only soft-deleted ones.
- `implements` selects every kind carrying one [trait](data-model.md#traits),
  across every package. Every arm narrows, so `implements` intersects with the
  kinds already in play rather than widening them: on a REST collection, whose
  path has already fixed the kind, use it to test that kind; use it on
  GraphQL's `records` for the cross-kind query. A pair that can match nothing
  is a `validation` error naming the mismatch, not an empty page.

A list also takes one shaping parameter beside the grammar:
`withAnnotations=1` adds each row's `annotations`. It is off by default so a
list of a heavily annotated record stays small.

Ordering is `orderBy` with camelCase columns (`dueAt`, `at:desc,createdAt`).
Only declared properties filter and order: **filterable, indexed, and declared
are the same set**, so a query that would be slow is one the grammar cannot
express.

A list parameter a given mode does not honor is a `bad_request` that names it,
never a silent success. On a collection list the path names the kind, so an
explicit `filter.kinds` conflicts and is refused (drop it, or list a different
collection). A `watch=1` stream ignores the list-query grammar, so
`filter`/`orderBy`/`first`/`after`/`withAnnotations` alongside it
are refused. An `incoming` read honors `first`/`after` and the `property` and
`fromKind` narrowings; `filter`/`orderBy` are refused. A misspelled ordering column is refused naming
the camelCase replacement, and a malformed filter document is refused naming
the field that would not decode.

## Pagination

Lists page forward with a keyset cursor carried behind one opaque token. You
pass `first` for the page size and, on the next request, the `cursor` a page
returned as `after`:

```http
GET /api/v1/samples.substrate.reamde.dev/tasks/task?first=50
→ {"records": [...], "cursor": "eyJv…", "head": 4211, "generation": "7f3a0c2e9b1d4e6f"}

GET /api/v1/samples.substrate.reamde.dev/tasks/task?first=50&after=eyJv…
→ {"records": [...], "cursor": "eyJv…", "head": 4211, "generation": "7f3a0c2e9b1d4e6f"}
```

The cursor is **opaque**, so treat it as a token and never parse it. Its
payload is a keyset position (the last row's sort-key values plus the
`(kind, id)` tiebreak), not an offset, so a deep page costs the same as a
shallow one, and the walk is stable under concurrent writes. The tiebreak is
the pair, not the id alone: an id is unique only within a kind, so a cross-kind
walk needs both for a strict total order. The **stability guarantee** is exact:
a cursor walk sees every row that existed for the whole walk exactly once. A
row inserted or deleted mid-walk may or may not appear; a row that lived
throughout is never skipped and never repeated. The token is bound to the
`orderBy` it was minted for, so replaying it against a different order is
rejected rather than silently mis-seeking. An exhausted list carries no
`cursor` at all over REST, and an empty one over GraphQL; both mean the same
thing, there is no next page.

Two continuation styles exist, and the parameter name says which you are
holding. The changelog uses real sequence numbers, `from` forward and `before`
backward, because a seq is a meaningful ordinal (its history response returns
a `cursor` seq to pass as the next `before`). Record lists and `incoming`
lists use an opaque cursor, passed back as `after`.

Every list response also carries the changelog **head** seq captured at the snapshot
it was served from, pinned once at the walk's start and carried through the
cursor, so every page of one walk reports the same head, and the history
**generation** that head belongs to. Page a collection, then resume a
[watch](changelog.md#watching) with `from={head}&generation={generation}`:
every listed row's change is at or before `head`, and the watch replays exactly
the changes after it, so the handoff has no gap and no double-see. A cursor
minted under one generation is refused after a restore replaces the history
(`422 validation`): list again.

## Discovery

`GET /.well-known/substrate/server.json` is discovery. It is unversioned and
unauthenticated, and opens no repository, so a client can call it before it
holds a token. The well-known path is what lets an outside system ask whether
a domain is a substrate at all before it speaks the rest of the contract, the
same way `/.well-known/openid-configuration` works for an OIDC issuer. It
reports: the served API versions; the server build; the
[changelog horizon](changelog.md#frames-and-the-horizon); the reference
grammar this deployment speaks; the authentication endpoints beside the
versioned API (`/register`, `/login`, `/tokens`, `/password`, `/totp`); what
registration asks for and whether it is open at all; the two request surfaces,
each with its endpoint and its compatibility; and a feature list. No dialect
is on the wire: the
[vocabulary](vocabulary.md#vocabulary-evolution-and-the-dialect-contract) and
[changelog](changelog.md#the-dialect-a-changelog-is-written-in) dialects are
stored per repository, and a binary too old for a store refuses to open it,
which surfaces as `unavailable`. That
feature list is what a client reads instead of trying a route to see whether
it exists: each entry names a feature, its stability and the `surfaces` that
serve it (`rest`, `graphql`, or both):

```json
{"versions": [{"name": "v1", "status": "served"}],
 "server": {"version": "…", "build": "…"},
 "changelog": {"horizon": 0},
 "features": [{"name": "triggers", "stability": "stable", "surfaces": ["rest"]},
              {"name": "changefeed", "stability": "stable", "surfaces": ["rest", "graphql"]},
              {"name": "search", "stability": "beta", "surfaces": ["graphql"]},
              {"name": "agents", "stability": "alpha", "surfaces": ["rest"]}],
 "surfaces": {"rest": {"endpoint": "/api/v1", "compatibility": "supported"},
              "graphql": {"endpoint": "/api/v1/graphql", "compatibility": "preview"}},
 "grammar": {"kind": "<authority>/<package>/<name>",
             "record": "<authority>/<package>/<kind>/<id>",
             "collection": "/api/v1/{authority}/{package}/{kind}[/{id}]",
             "actors": ["api", "console", "substratectl",
                        "bundle:<authority>:<package>",
                        "function:<authority>:<package>:<name>",
                        "agent:<authority>:<package>:<name>",
                        "substrate"]},
 "endpoints": {"register": "/register", "login": "/login", "tokens": "/tokens",
               "password": "/password", "totp": "/totp"},
 "registration": {"inviteRequired": true, "totpRequired": true}}
```

A feature's `surfaces` are the doors to its own operations, not to its
records: a trigger and a blob manifest are ordinary records and read on both
surfaces whatever the entry says, while `["rest"]` means the feature's verbs
(a replay, an install, a function call, a blob's bytes) have REST paths and no
GraphQL field. The example above is abridged; the full list, and it is the
same for every deployment of a given build, is `triggers`, `functions`,
`bundles`, `blobs`, `export`, `changefeed`, `search`, `embeddings` and
`agents`.

`surfaces` is the verdict per request surface, and it is a different axis from
a feature's stability. `compatibility` is `supported` on `rest`: the REST API
is the interface a client builds on, and a break there is announced, never
silent. It is `preview` on `graphql`: every part of the GraphQL surface, the
generated types, the root operations and the scalars, may change without a v1
wire break, so a client that posts to `/api/v1/graphql` pins the server
version. The two axes compose one way: a feature's `stability` says how far
that feature's shape has settled, and it binds on the REST door alone, because
the GraphQL door is `preview` for every feature whatever the feature stamps;
a `stable` `changefeed` freezes `GET …/changes` and does not make GraphQL's
`changelog` field stable
([decision 0053](decisions/0053-rest-is-supported-all-of-graphql-is-preview.md)).

`registration` is what the register door asks for. `registration.inviteRequired`
is `false` only on a deployment with
[no invite code configured](auth.md#the-invite-code), where the door reads
none; `registration.totpRequired` is `false` only where the second factor is
[switched off](auth.md#the-second-factor-can-be-switched-off-locally). Both
describe a local substrate, and a client reads them before it asks a person
for either code. Neither field is a verdict — the service refuses on its own
terms either way.

### What a feature's stability means

The stamp is a promise about **change**, not about quality: everything listed
is served and works today.

| Value | Means |
| ----- | ----- |
| `alpha` | The shape may change or be withdrawn with no v1 wire break. Pin the server version if you depend on it. |
| `beta` | Served and supported, and the shape is still moving before v1 freezes it. A break is announced, never silent. |
| `stable` | Frozen for v1. Changes are additive only. |

**Every feature of the supported REST surface that
[decision 0053](decisions/0053-rest-is-supported-all-of-graphql-is-preview.md)
names reports `stable`**: `triggers`, `functions`, `bundles`, `blobs` and
`changefeed`. `search` reports `beta`, because its only door is the preview
GraphQL surface. `export` reports `beta` because 0053 did not name it:
`GET /api/v1/export` streams the repository's recovery export, a tar of its
directory in the snapshot format ([backups](operations.md#backups)), and it
freezes by a decision of its own, not by age. `agents` and `embeddings` report
`alpha` because their shapes are still moving; a surface in their `surfaces`
says where they are served, not that they are frozen. `agents` is served on `rest`
and `embeddings` on `graphql` alone, where the semantic arm of
[`search`](#search) is its one door.

The list is a literal in the server (`internal/api/discovery.go`), and it is
every feature the build serves: one implementation serves them all, so there
is nothing per-deployment to compute. A client reads the list for the
stability stamps and the `surfaces`, not to find out whether a route is
there.

`embeddings` is listed like the rest. Discovery opens no repository, so it
does not answer the narrower question of whether the CALLER's repository
declares an [`llmprovider` row](agents.md): the first semantic query answers
that one, naming the property no row declares. An entry stands for every route
behind it, so `bundles` covers the lifecycle transitions and catalog install
together.

Send every request to the `/api/v1` prefix. It is the only prefix served,
and `versions` lists it with status `served`.

Within v1 the REST surface is **additive only**: fields and endpoints are
added, never removed or narrowed under the same version, except where a
feature reports `alpha`, which licenses withdrawing one of its routes
(`POST /api/v1/embeddings/reembed` was withdrawn that way, and re-embedding is
the operator's `substratectl repository reembed`). A deprecation is
signalled, not a silent break: a `Warning` HTTP header on the REST response,
with a minimum sunset window before removal. GraphQL makes no such promise
([REST and GraphQL](#rest-and-graphql) below). There is no Kubernetes-style
multi-version conversion machinery.

## REST and GraphQL

The two surfaces make different promises, and the difference decides what a
client may build on. Discovery states it as `surfaces`: `rest` is `supported`
and `graphql` is `preview`.

**REST is the supported developer interface.** Its routes, request and
response shapes, error codes and headers live under `/api/v1` and widen by
addition; a change that would break a client is announced, never silent, and
each feature's `stability` says how far its own shape has settled. A new kind
adds no route: the collection segment is the kind's name on the same route
pattern.

**GraphQL is a preview, all of it.** The schema is a projection derived from
the vocabulary and rebuilt per repository: installing a bundle adds types and
fields and uninstalling one takes them away, so a client reads it by
introspection instead of pinning it. The root operations (`record`, `records`,
`search`, `changelog` and the five mutations), `Record`'s own fields and the
scalars carry no promise either: they may change, and a schema element leaves
without a deprecation marker. A client that needs ranking posts to
`POST /api/v1/graphql` and pins the server version. The surface stays because
the engine executes it itself: the agent loop's
[`substrate.reamde.dev/core/graphql`](functions.md#host-functions) host
function reads a repository through the same schema and resolvers. The
generated half carries only the [declaration's own upgrade
rules](vocabulary.md#vocabulary-evolution-and-the-dialect-contract).

The two also do not serve the same set. Discovery says which serves what
(every `features` entry carries its `surfaces`), and this is the whole list:

| Operation | REST | GraphQL |
| --------- | ---- | ------- |
| Records of every kind: read, list, filter, and the five mutations | yes | yes |
| Ranked search, and its semantic (embedding) arm | no route | `search(q, mode, kinds, k)` |
| Changelog, forward from a seq | `GET …/changes?from=` | `changelog(from, filter, first)` |
| Changelog, newest-first backward page | `GET …/changes?before=` | no field |
| The live tail | `?watch=1` on a collection or on `…/changes`, ndjson | no subscription |
| One record's own history | `GET …/changes?recordKind=&recordId=` | `history(first)` on the record |
| Incoming references | `GET …/{id}/incoming` | no field |
| Per-property provenance, `propertyMeta` | single-record reads only | single-record reads only |
| Operational verbs: triggers, bundles, catalog, blobs, vocabulary apply, function and agent calls | yes | none |

GraphQL's whole read and write surface serves at one endpoint,
`POST /api/v1/graphql`, and its `filter` argument takes
[the filter grammar](#the-filter-grammar) whole. Every argument carries that
grammar in its **description**, so a client with only introspection to read
(an agent holding the `graphql` tool) gets the accepted keys, the condition
operators and a worked date range without leaving the endpoint, and a filter
key the grammar does not have is a `validation` error naming the keys it does.
`records(filter, orderBy, first, after)` is the list query and
`record(kind, id)` the single lookup, which takes both because identity is the
pair. A page carries the same `cursor`, `head` and `generation` a REST list
does, and no `total`: a keyset walk counts nothing.

```graphql
query ($f: JSON) {
  records(filter: $f, first: 20) {
    nodes { id kind title ... on Ada_example_com_Tasks_Task { status } }
    cursor
    head
  }
}
# variables:
# {"f": {"kinds": ["ada.example.com/tasks/task"],
#        "properties": {"status": {"eq": "open"}}}}
```

The five mutations spell their arguments out, and only `put` carries the whole
record: `put(input)`, `patch(kind, id, input, ifVersion)`,
`delete(kind, id, ifVersion)`,
`merge(kind, winner, loser, winnerVersion, loserVersion)` and
`split(mergeId, ifVersion)`. Every version argument is an optional `Long`
carrying the [precondition](#the-five-mutations) the REST body carries. Watch
`split`: its argument is `mergeId`, where the REST body key for the same value
is `merge`.

```graphql
mutation ($k: String!, $id: ID!, $in: JSON!) {
  patch(kind: $k, id: $id, input: $in) { id version }
}
# variables:
# {"k": "samples.substrate.reamde.dev/tasks/task", "id": "kq3v9x2m41pf",
#  "in": {"properties": {"status": "done"}}}
```

Search is the deliberate one. **Filtering is REST's job** and **ranking is the
GraphQL query's**: `?filter=` selects rows by predicate, `search` scores and
orders them, and the two answer different questions. A client that needs
ranking posts the `search` query to `POST /api/v1/graphql`; there is no other
door. `propertyMeta` is the other asymmetry: it is assembled per record, so a
list never carries it on either surface. Both are listed here rather than left
for a client to find out by trying, which is the rule: an asymmetry is written
down or it is a bug
([decision 0053](decisions/0053-rest-is-supported-all-of-graphql-is-preview.md)).

### Generated names and scalars

A GraphQL type name is a pure function of the kind's reference and where the
kind came from, so the schema is deterministic and installing one bundle can
never rename another kind. The rule has two arms. A **seeded kind keeps its
bare singular**: `substrate.reamde.dev/core/token` is `Token`. That is the core
package alone, because creation seeds core and nothing else: every sample a
repository imports, `people` and `tasks` included, installs as the repository's
own. Every **other kind carries its authority and its package**: the authority
folded (a dot becomes `_`, a hyphen becomes `__`, and a digit-first authority
gains a leading `_`), the package's word, then the singular, each TitleCased
and joined by underscores. The fold reads back unambiguously because an
authority never carries an underscore, so two authorities never share a name:
`my-host.example.com` is `My__host_example_com` and `myhost.example.com` is
`Myhost_example_com`. The repository `ada.example.com`
that imported the `tasks` sample has `Ada_example_com_Tasks_Task`, and the
installed Notion provider has `Providers_substrate_reamde_dev_Notion_Page`. The
underscore keeps every one of them out of reach of any seeded name, the
package keeps two packages apart that declare one singular, and the authority
keeps two authorities apart that publish a package of one word, so a name
never depends on which of its neighbours are installed and a later install
never renames an earlier kind
([decision 0058](decisions/0058-a-graphql-name-always-carries-the-authority.md)).
Interfaces follow the same determinism: one per trait that carries properties
(a pure marker trait adds none), and one per distinct state-property name
(`HasStatus`, `HasProminence`). They span packages, so "everything with a
status, anywhere" is one query.

Two kinds that still resolve to one name are **refused when the second is
declared**, at the same moment every other narrowing refusal happens, and a
kind whose computed name would land on a structural name (`Record`, `Change`,
`Reference`, a scalar, an interface) is refused at schema build with a
named error. Neither is ever silently renamed.

Three groups of fields are the **`Long`** scalar, a 64-bit signed integer
serialized as a JSON number: `version` and `seq`; the changelog resume seqs
`head` and `from` with the `ifVersion` precondition; and every `int`
property, scalar, repeated or a reference's link property. GraphQL's built-in
`Int` is 32-bit, and graphql-go serializes a value past 2^31-1 (about 2.1
billion) as `null` with no error. The engine accepts an `int` up to 2^53-1
([decision 0012](decisions/0012-numbers-are-exact-or-refused.md)) and a
repository's version or seq counter has no bound at all, so `Long` carries the
full int64 range on the wire. The `first` and `k` page sizes stay `Int`. A
`decimal` property is a `String` holding its exact digit string, never a
`Float`. A JavaScript client should read the 64-bit fields through a
64-bit-safe path if a counter can exceed 2^53, since a JSON number past that
loses precision in the browser's `Number`.

A number inside an inline JSON argument (`put(input: {properties: {count:
5}})`) reaches the engine as a number, exactly as the same value in a variable
does, and is held to the same types: an inline `price: 19.90` on a `decimal`
property is refused as a bare number (decision 0012), and an inline `id: 42` is
refused because `id` is a string. Write them as `price: "19.90"` and
`id: "42"`, as a variable or a REST body already must.

Property types render as their proper shapes. A `repeated` property is a GraphQL
list of its element type for every kind (`[Long]`, `[Float]`, `[Boolean]`,
`[String]`), not a bare scalar. An `object` property (inline structured fields)
renders as the `JSON` scalar, lossless, rather than flattening to `String`. A
`reference` property is its own generated OBJECT type, `<Kind><Property>Reference`:
`ref` is the referent's path as the named `Reference` scalar, `target` resolves
the referent itself (null when the pointer dangles), and each declared link
property is a typed field beside them. A reference that declares no link
properties generates the same object, so adding one later adds a field instead
of replacing a scalar. A client that wants the path alone selects `{ ref }`.

## Search

Search is one query, `search(q, mode, kinds, k)`, served over GraphQL and
nowhere else. It has two arms:

- **Lexical**, on by default for every kind. The title and every
  string-family property index into full-text search, weighted in three bands
  (title first, then declared string properties, then the rest), and `q` takes
  web-search syntax: bare words, quoted phrases, `-exclusions`. A property opts
  out with `fts: false`; secret-typed properties never index. Changing what a
  kind indexes re-indexes its existing records in the same apply, without
  moving their `version` or `updatedAt`.
- **Semantic**, strictly opt-in per property with `embed: true` (the shipped
  vocabulary opts in long prose: message and mail bodies, task and event
  descriptions, and transcripts). Opted-in text is chunked into overlapping windows
  and embedded **asynchronously** after commit, off a queue, so writes never
  wait on an embedding call. Vectors live in Postgres (pgvector) beside
  everything else, 1536 wide
  ([0026](decisions/0026-embedding-vectors-are-1536-wide-or-refused.md)).

`mode` picks `lexical`, `semantic`, or `hybrid` (the default): hybrid runs both
arms, normalizes each against its own best hit, and merges. The answer is
`hits` and `pending`. Every hit carries the record beside its raw per-arm
scores, `lexical` and `semantic`, so a caller can threshold rather than trust a
rank. `pending` is the number of properties the drain has yet to buy vectors
for, counted whenever the semantic arm was asked for: non-zero means the
ranking covers a partial index (a repository [restored from its
directory](operations.md#backups), a `reembed` in progress), and it falls to 0
as the drain buys. In a repository that has named no embeddings provider,
hybrid degrades to lexical and `semantic` reports an error rather than
pretending. While properties are queued and no vector from the resolved
provider and model has landed yet, `semantic` refuses with the `unavailable`
code and the count, so "no vectors yet" never reads as "no matches"; hybrid
returns its lexical arm alone. With nothing queued, a repository with nothing
embeddable returns no hits, and a row re-pointed at a model nobody ran
`substratectl repository reembed` for is refused naming the command.

**Which model bought the vectors is data, per repository.** The one
[`llmprovider`](agents.md#providers) row declaring `embedModel` is where a
repository buys them, each stored vector names that row and that model, and the
semantic arm scores only the currently resolved pair. Re-point the row and the
older vectors stop being scored rather than being ranked against the new ones:
cosine distance between two models' vectors is not a distance. `substratectl
--dsn … repository reembed <repository>` queues their replacement, which the
server's drain loop buys a batch at a time. There is no REST verb for it: it is
the operator's hat, on the box.

The substrate does retrieval only: it returns typed records with scores, and
anything generative built on top (a RAG loop, an assistant) is a client
reading this API like every other. [Functions](functions.md) run on the shared
runner and reach the same search through a host call, under their declared
read allowlist.

One ranking rule is built in: the shipped `person` carries a two-state
`prominence` machine (`utility` at birth, `known` once something promotes it,
an address-book sync or the owner), and search ranks `utility` people below
every `known` match, so the recruiter who emailed once never outranks a
friend. The demotion participates in the top-k ordering, so in a mixed-kind
search a high-scoring utility person can be pushed out of the `k` rows
entirely.

## Actors

Every write is attributed to an **actor**: what wrote the record. The domain is
closed, seven names:

```
console                                  a write from the console
substratectl                             a write from the command line
api                                      a write from a client holding a token, client unnamed
bundle:<authority>:<package>             an install, and the package's own hand
function:<authority>:<package>:<name>    a function's effects
agent:<authority>:<package>:<name>       an agent's effects
substrate                                the engine's own hand
```

A machine hand carries the full authority and the package, so two packages that
share a name under different authorities are two writers, and the segments are
colons because a `/` is reserved for label and annotation keys
([0047](decisions/0047-a-kind-lives-in-a-package.md)). `connector:<label>` was
a second spelling of the bundle hand and is retired
([0025](decisions/0025-an-actor-carries-the-full-authority.md)); entries
written under it keep it, because an actor is part of the hashed changelog
preimage.

A request names its actor with the optional `X-Substrate-Actor` header, and a
request that names none is `api`, which is exactly what the substrate knows
about it. This is **attribution, not authorization**: a token has full access
to its repository either way, so there is nothing an actor name could unlock.
What the header cannot do is claim one of the substrate's own writing hands —
`substrate`, anything under `substrate.`, a `bundle:`/`function:`/`agent:`
name, or the retired `connector:` spelling — because those write past checks a
request must not skip.
Naming one is `403 forbidden`.

Attribution is load-bearing three ways:

- **Provenance**: every accepted write records its actor as the property's
  manager ([managed properties](projection.md#managed-properties)), and every
  [changelog row](changelog.md) names who wrote. Because the actor is the
  caller's own claim, both also record the **principal** the server resolved:
  the id of the token the request authenticated with, which the header cannot
  touch.
- **Yield**: mapping recompute yields to any manager outside the machine tier,
  which is how a hand edit survives a sync.
- **Self-exclusion**: a [function](functions.md) never sees writes carrying
  its own actor, which is one half of what keeps functions from looping.

The manager **tier** a write holds at (owner, bundle, or machine) is explicit
data on the write context, never derived from an actor's name: the three
interactive clients (`api`, `console`, `substratectl`) write at the owner
tier. [Managed properties](projection.md#managed-properties) owns the tier
rules, who writes at which tier, and how recompute yields to them.

## The canonical envelope

The [document envelope](data-model.md#the-envelope) (kind, metadata, data,
status) is the one canonical representation of a record. The flat JSON that
REST and GraphQL carry is a lossless view of it: `properties`
lands under `data`, `metadata` holds the id and the authored key spaces, and the
server-set fields (version, timestamps, provenance) land under `status`. The
mapping round-trips exactly, so `substratectl get -o yaml` output applies back with no
edit, and a generic client can read, modify, and write the same object.

A reference is a property value like any other, so it round-trips the same way:
the object `{ref: "<kind>/<id>"}`, with any declared
[link properties](vocabulary.md#reference-properties) beside `ref`. That is what
a read serves, whether or not the declaration carries link properties; a bare
path string is accepted on the way in and stored as the object. A
read-modify-write of a record that points at others is a fixed point.

Every versioned write body and every filter document is decoded **strictly**.
An unknown key, a miscased key (a lowercase `ifversion` is not `ifVersion`), or
a duplicate key is a `bad_request` naming it, never a silently dropped
precondition or a broadened filter. Openness stays only inside the map-valued
fields that are meant to be open: `properties`, `labels`, `annotations`, an
object-typed or `json`-typed property, and the filter's per-property operators.
GraphQL's JSON-scalar inputs decode through the same strict path.

`PATCH` semantics are pinned:

- A property value **replaces whole**. The merge is key-wise across the
  property map only (depth one): patching `{"properties": {"a": {...}}}`
  replaces the whole value of `a`, it does not deep-merge into it. `labels`
  and `annotations` merge the same way, key by key.
- A top-level `null` **deletes** that property (or label or annotation).
- A literal `null` as a value is therefore **unwritable**: a null always means
  delete, so a property can never read back holding `null`.
- A state value among the properties is a **transition**, not a plain write:
  `{"properties": {"status": "done"}}` drives the state machine and stamps any
  declared clock.

Status codes follow the write: a create is `201`, an update or replace is
`200`, consistently across POST-to-collection and PUT-at-id.

## Webhooks

`POST /webhooks/{authority}/{trigger}` is the one route that takes a body and
no bearer. `authority` is the repository's authority (the name it publishes
under, chosen at registration) and `trigger` the id of a
`substrate.reamde.dev/core/trigger` record whose source is the `webhook` arm;
the request becomes the delivery's envelope and the trigger's callable runs in
the background ([functions](functions.md#the-delivery-envelope)). When the
trigger declares `source.webhook.key`, the request carries it as a trailing
path segment (`/webhooks/{authority}/{trigger}/{key}`), as `?key=` or as
`Authorization: Bearer <key>`; without one the endpoint is open to whoever can
reach the server.

| Answer | When |
| --- | --- |
| `202 {"fire": "hook-…"}` | the request is recorded in the repository's changelog and the fire is on its way; a restart resumes it under the same id, which names its `triggerrun` row |
| `404 not_found` | no such repository, trigger or key, a disabled trigger, or a trigger of another source: one answer for all of them |
| `413` | a non-multipart body over 1 MiB, or a multipart request over 32 MiB |
| `431` | headers over 16 KiB |
| `503` + `Retry-After` | more than eight deliveries in flight |

A multipart/form-data request's file parts are stored in the repository's
blob store (`PUT /api/v1/blobs`) before the fire; the callable reads their
digests. Nothing about the callable's output reaches the sender.

## Errors

Errors are one shape everywhere, with `problems` carrying the full list when a
batch or a multi-part validation refuses:

```json
{"error": {"code": "validation", "message": "…", "problems": ["…"]}}
```

Each problem is a `path: message` string (`props.name: required`). A
`problemDetails` array carries the same list split into `{path, message}`
objects, so a form maps a problem to the input it concerns without parsing the
prose; `problems` stays beside it for readers that do not need the split.

The code set is closed. The client-error codes:

| Code           | HTTP | When                                                                                             |
| -------------- | ---- | ------------------------------------------------------------------------------------------------ |
| `bad_request`  | 400  | A malformed request, an unknown field, or an unsupported list parameter.                         |
| `validation`   | 422  | An undeclared property, a malformed value, a type mismatch.                                      |
| `conflict`     | 409  | A version check failed (`ifVersion`); re-read and retry.                                         |
| `guard`        | 403  | A refused state transition, or a protected operation (a subject reference, a kind with live records). |
| `lossy`        | 403  | A declaration change would remove values from the fold, or a re-import would replace a sample copy edited since it was imported, and no confirmation for that plan came with it; preview the plan and confirm it ([bundles](bundles.md#install-and-lifecycle)). |
| `forbidden`    | 403  | The caller may not do this at all.                                                               |
| `auth`         | 401  | Missing, invalid, or expired token, or a refused login.                                          |
| `not_found`    | 404  | No such record; a former id is not this, it resolves ([merges](projection.md#merges)).          |
| `rate_limited` | 429  | Slow down; the response carries `Retry-After`.                                                   |

The server-error family is split so a client can tell "try again" from "never
going to work": `internal` (500, an unexpected fault), `function_failed` (500, a
callable's body faulted while running, distinct from `validation` so a caller
tells its own bad arguments from the function failing to execute), and
`unavailable` (503, always with a `Retry-After`). Two
cases are worth calling out: a well-formed token whose repository cannot be
opened answers `unavailable`, never a masked `401`, so a store the binary
cannot serve is diagnosable instead of looking like a bad credential; and a
`semantic` search over a repository whose vectors have not been bought yet
answers `unavailable` with the number of properties still queued, so an empty
index is never mistaken for an empty match; search has no REST route, so that
one reaches a client as the GraphQL error's `extensions.code`, or as the
function host's error.

The same problem object appears under `extensions` in a GraphQL error and in the
[watch stream](changelog.md)'s terminal error frame, so an error means the same
thing wherever it surfaces. An unmatched path under an API prefix is that same
object with `404 not_found` — never the console's HTML with a 200.

One more code lives on the changelog surface: `compacted` (410) answers a
`from=` or `before=` the changelog cannot resume, below the retention
[horizon](changelog.md#frames-and-the-horizon), above the head, or under a
history generation the server does not hold. Its problem object names the
current `head` and `generation`, telling a consumer that has fallen too far
behind, or resumes after a restore, to re-list rather than silently miss rows.

Every code a request can receive is above; those fourteen strings are the whole
closed set, and nothing else appears in `error.code`. The
[policy door](agents.md#the-policy-door)'s `gate` verdict is not one of them: it
holds an agent's write for the owner's review and surfaces as a tool result
inside the agent loop, never as an HTTP response
([0037](decisions/0037-gated-is-an-agent-loop-verdict-not-a-wire-code.md)).

Next: [users and tokens](auth.md), the way in and what a token is.
