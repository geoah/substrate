# The API

The substrate serves one surface for everything: four reads (`record`,
`records`, `search`, `changelog`) plus a watch stream, and seven mutations. A new kind never adds an
endpoint: the REST path pattern is the same routes for every authority, and the
[GraphQL](graphql-and-search.md) schema is generated from the loaded kinds.
This page is the REST surface, the filter grammar, pagination, the mutations,
errors, and discovery. Authentication has a page of its own,
[users and tokens](auth.md). Everything here holds identically over REST and
GraphQL.

## REST resources

Every authority serves the same routes; the collection segment is the kind's
declared plural:

```http
GET    /api/v1/{authority}/{plural}          # list, filter, watch
POST   /api/v1/{authority}/{plural}          # create / upsert
GET    /api/v1/{authority}/{plural}/{id}
PATCH  /api/v1/{authority}/{plural}/{id}
PUT    /api/v1/{authority}/{plural}/{id}     # put addressed at id
DELETE /api/v1/{authority}/{plural}/{id}     # soft delete
GET    /api/v1/{authority}/{plural}/{id}/incoming   # paged reverse edges
```

**There is no repository segment anywhere.** The bearer token names the
repository, so an address never has to, and there is nothing to get wrong.

The path carries a record's **full identity**: `{authority}/{plural}` names the
kind, `{id}` the id within it. Ids are unique per kind, so the same id may
exist in two collections as two unrelated records, and a resource read is
always scoped to its own collection. There is no cross-kind read by bare id
anywhere on the surface.

A [repository-local kind](data-model.md#kinds-and-references) has no authority
segment, so every route above exists one segment shorter — `/api/v1/tasks`,
`/api/v1/tasks/t9`. The two are told apart by inspection, in one place: an
authority is a DNS name and always carries a dot, a plural never does. So a
two-segment path is a qualified collection when its first segment is dotted and
a repository-local resource when it is not.

One id form needs care. A [kind declaration](vocabulary.md)'s id **is** a kind
reference, so it carries a `/`. A client percent-encodes it, and the API
decodes it exactly once:

```http
GET /api/v1/core.substrate.reamde.dev/kinds/tasks.substrate.reamde.dev%2Ftask
```

Reverse edges are a derived view of their own, paged separately so a popular
record's fan-in never inflates its document. The response is
`{"incoming": [{"rel": …, "via": "edge" | "reference", "createdAt": …,
"from": {"id", "kind", "title"}}], "cursor": …, "total": n}`, ordered by
`rel`, then source kind, then `via`, then newest first.

## The flat record

Requests and responses carry the **flat record**: one JSON object with
`properties`, `edges`, and the server-set fields at the top level. The
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
POST /api/v1/tasks.substrate.reamde.dev/tasks
{"properties": {"name": "Buy milk", "dueAt": "2026-08-13T09:00:00Z"}}

→ 201 {"id": "kq3v9x2m41pf", "kind": "tasks.substrate.reamde.dev/task",
       "properties": {"name": "Buy milk", "title": "Buy milk", "status": "open",
                      "dueAt": "2026-08-13T09:00:00Z"},
       "version": 1, "createdAt": "2026-08-04T10:00:00Z",
       "updatedAt": "2026-08-04T10:00:00Z"}
```

List what is open, soonest first (the filter is URL-encoded JSON, the grammar
is below):

```http
GET /api/v1/tasks.substrate.reamde.dev/tasks
      ?filter={"properties":{"status":{"eq":"open"}}}&orderBy=dueAt

→ {"records": [...], "cursor": "eyJv…", "head": 4207}
```

Complete one. A state change is just a patch, and the
[declaration](data-model.md#validation-and-state-machines) stamps
`completedAt`:

```http
PATCH /api/v1/tasks.substrate.reamde.dev/tasks/kq3v9x2m41pf
{"properties": {"status": "done"}}
```

Read the person GitHub linked up. Single-record reads also carry
`propertyMeta` (per property: who wrote it, at which tier, and the
alternatives other sources assert,
[managed properties](projection.md#managed-properties) explains the
mechanism), and, if you asked by an id that was merged away, `canonicalId`
tells you where it went ([merges](projection.md#merges)):

```http
GET /api/v1/people.substrate.reamde.dev/people/9f2k

→ {"id": "9f2k", "kind": "people.substrate.reamde.dev/person",
   "properties": {"name": "Ada Lovelace", "emails": ["ada@example.com"]},
   "propertyMeta": {"name": {"manager": "console", "tier": "owner",
     "updatedAt": "2026-08-04T09:12:00Z",
     "alternatives": [{"actor": "function:githubsync", "value": "ada",
                       "updatedAt": "2026-08-04T08:00:00Z"}]}}}
```

## The seven mutations

The complete write surface, for every actor, forever. Each one addresses its
target by **full identity**: the kind beside the id (on REST the path's
`{authority}/{plural}` names the kind; on GraphQL the kind travels in the
mutation's arguments, as `kind` on `patch`, `delete` and `merge`, as
`srcKind`/`dstKind` on `link` and `unlink`, and inside `input` on `put`),
because an id is unique per kind, never per repository:

| Mutation | What it does                                                                                                                                                        |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `put`    | Create or upsert. Merges and never prunes: what the document omits is left alone. Accepts inline edges, so a record and its edges commit as one unit.               |
| `patch`  | Edit in place: properties, labels, annotations. A null value deletes a key. State [transitions](data-model.md#validation-and-state-machines) travel only this way. |
| `delete` | Soft delete: tombstones the record; hard deletion waits for finalizers to release.                                                                                  |
| `link`   | Add one edge between two records.                                                                                                                                   |
| `unlink` | Remove one edge. Both refuse a mapping's subject edge, which only create-time resolution, `merge`, and `split` may move.                                            |
| `merge`  | Join two records of one kind; the loser's id resolves to the winner forever ([merges](projection.md#merges)).                                                      |
| `split`  | Reverse one merge, restoring the loser from the merge record.                                                                                                       |

A `put` onto a tombstone restores that record: same id, same row, one
changelog row saying so. It is undelete, not id reuse.

`put` and `patch` take an optional `ifVersion`: the write applies only if the
addressed record's stored version equals it (a non-existent record is version
0), else the whole write fails a `conflict`. It is the safe
read-then-conditional-write primitive.

**Edges over REST.** `link` and `unlink` are first-class REST verbs, not
GraphQL-only, so an edge change is a request against the resource whose edge it
is:

```http
POST   /api/v1/{authority}/{plural}/{id}/edges/{rel}
DELETE /api/v1/{authority}/{plural}/{id}/edges/{rel}
```

The body is an edge reference: `{kind, id}`, or a bare `{id}` where the
edge declaration already pins one target kind. A `link` body may also carry the
edge's own `properties`. Both return the refreshed source record. A `put` could
always add an edge inline; `DELETE` is how a REST client removes one.
`substratectl` has matching `link` and `unlink` commands.

This follows one rule, written into the contract: **a resource's operational
verbs live at the resource**, its own `{authority}/{plural}/{id}` path. That is
also why the trigger verbs live under `core.substrate.reamde.dev/triggers/…` — trigger
records are core's, so their verbs sit beside them.

## The filter grammar

A filter is one JSON document, the same shape URL-encoded in REST's `?filter=`
and passed whole to GraphQL's `filter` argument:

```json
{"kinds": ["tasks.substrate.reamde.dev/task"],
 "properties": {"status": {"eq": "open"},
                "dueAt": {"lt": "2026-08-11T00:00:00Z"}},
 "labels": {"owner/starred": {"eq": true}}}
```

- `kinds` names kinds by reference (implied by the path on a REST collection).
- `properties` carries one condition per property. The operator set is one
  rule, not a per-type table: `secret` and `digest` properties refuse
  filtering entirely, `reference` takes `eq`, `in`, `contains` and `exists`,
  and every other declared property takes the full grammar (`eq`, `gt`,
  `gte`, `lt`, `lte`, `in`, `prefix`, `contains`, `exists`), compared as its
  declared [property type](data-model.md#property-types). State properties
  filter here like any other.
- `labels` matches the short, indexed metadata, and takes the **same condition
  objects** a property does: `{"owner/starred": {"eq": true}}`, never a bare
  value.
- `ids` narrows to a list of ids within the kinds already selected.
- `edge` is the one-hop edge predicate: `{"rel", "to", "toKind"}`, records
  carrying an edge `rel` (omit it for any `rel`) at that target.
- `deleted` picks the tombstones: absent or `false` lists only live records,
  `true` lists only soft-deleted ones.
- `implements` selects every kind carrying one [trait](data-model.md#traits),
  across authorities. Every arm narrows, so `implements` intersects with the
  kinds already in play rather than widening them: on a REST collection, whose
  path has already fixed the kind, use it to test that kind; use it on
  GraphQL's `records` for the cross-kind query. A pair that can match nothing
  is a `validation` error naming the mismatch, not an empty page.

A list also takes two shaping parameters beside the grammar: `withEdges=1`
adds each row's `edges` map, and `withAnnotations=1` adds its `annotations`.
Both are off by default so a list of a fanned-out record stays small.

Ordering is `orderBy` with camelCase columns (`dueAt`, `at:desc,createdAt`).
Only declared properties filter and order: **filterable, indexed, and declared
are the same set**, so a query that would be slow is one the grammar cannot
express.

A list parameter a given mode does not honor is a `bad_request` that names it,
never a silent success. On a collection list the path names the kind, so an
explicit `filter.kinds` conflicts and is refused (drop it, or list a different
collection). A `watch=1` stream ignores the list-query grammar, so
`filter`/`orderBy`/`first`/`after`/`withEdges`/`withAnnotations` alongside it
are refused. A reverse-edge (`incoming`) read honors `first`/`after` and the `rel` and
`fromKind` narrowings; `filter`/`orderBy` are refused. A misspelled ordering column is refused naming
the camelCase replacement, and a malformed filter document is refused naming
the field that would not decode.

## Pagination

Lists page forward with a keyset cursor carried behind one opaque token. You
pass `first` for the page size and, on the next request, the `cursor` a page
returned as `after`:

```http
GET /api/v1/tasks.substrate.reamde.dev/tasks?first=50
→ {"records": [...], "cursor": "eyJv…", "head": 4211}

GET /api/v1/tasks.substrate.reamde.dev/tasks?first=50&after=eyJv…
→ {"records": [...], "cursor": "eyJv…", "head": 4211}
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
a `cursor` seq to pass as the next `before`). Record lists and reverse-edge
(`incoming`) lists use an opaque cursor, passed back as `after`.

Every list response also carries the changelog **head** seq captured at the snapshot
it was served from, pinned once at the walk's start and carried through the
cursor, so every page of one walk reports the same head. Page a collection,
then resume a [watch](changelog.md) from `head`: every listed row's change is
at or before `head`, and the watch replays exactly the changes after it, so the
handoff has no gap and no double-see.

## Discovery

`GET /.well-known/substrate/server.json` is discovery. It is unversioned and
unauthenticated, and opens no repository, so a client can call it before it
holds a token. The well-known path is what lets an outside system ask whether
a domain is a substrate at all before it speaks the rest of the contract, the
same way `/.well-known/openid-configuration` works for an OIDC issuer. It
reports: the served API versions; the server build; the binary's maximum
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract);
the [changelog horizon](changelog.md#frames-and-the-horizon); the reference
grammar this deployment speaks; the authentication endpoints beside the
versioned API (`/register`, `/login`, `/tokens`, `/password`, `/totp`); what
registration asks for and whether it is open at all; and a feature list. (A
repository's own stored dialect never appears on the wire; a binary too old
for a store refuses to open it, which surfaces as `unavailable`.) That
feature list is what replaces probing for 501s: each entry names a feature
and its stability, and the agent surface reports `alpha`:

```json
{"versions": [{"name": "v1", "status": "served"}],
 "server": {"version": "…", "build": "…"},
 "vocabulary": {"maxDialect": 2, "note": "…"},
 "changelog": {"horizon": 0},
 "features": [{"name": "triggers", "stability": "stable"},
              {"name": "agents", "stability": "alpha"}],
 "grammar": {"kind": "<authority>/<name> | <name>",
             "record": "<authority>/<kind>/<id> | <kind>/<id>",
             "collection": "/api/v1/{authority}/{plural}[/{id}] | /api/v1/{plural}[/{id}]",
             "actors": ["api", "console", "substratectl", "bundle:<authority>",
                        "function:<authority>:<name>", "agent:<authority>:<name>",
                        "substrate"]},
 "endpoints": {"register": "/register", "login": "/login", "tokens": "/tokens",
               "password": "/password", "totp": "/totp"},
 "registration": {"open": true, "totpRequired": true}}
```

`registration` is what the register door asks for, and whether it is even
open. `registration.open` is `false` only on a deployment with no invite code
configured — the register endpoints answer `unsupported` either way, this
just lets a client say so before trying. `registration.totpRequired` is what
a client reads before it asks a person for a code: it is `false` only where
the second factor is
[switched off](auth.md#the-second-factor-can-be-switched-off-locally), which is
a local substrate. Neither field is a verdict — the service refuses on its
own terms either way.

Send every request to the `/api/v1` prefix. Today it is the only prefix
served, and `versions` is where
that is said: were a deployment ever to answer on a second one, it would be
listed there as `deprecated` with the prefix that replaces it, and every
response on it would carry a `Warning` header (RFC 7234 warn-code 299) naming
that replacement.

Within v1 the surface is **additive only**: fields and endpoints are added,
never removed or narrowed under the same version. A deprecation is signalled,
not a silent break: a `Warning` HTTP header on the REST response and
`@deprecated` on the GraphQL schema element, each with a minimum sunset window
before removal. There is no Kubernetes-style multi-version conversion
machinery.

## Actors

Every write is attributed to an **actor**: what wrote the record. The domain is
closed, seven names:

```
console                       a write from the console
substratectl                  a write from the command line
api                           a write from a client holding a token, client unnamed
bundle:<authority>            an install, and the authority's own hand
function:<authority>:<name>   a function's effects
agent:<authority>:<name>      an agent's effects
substrate                     the engine's own hand
```

A machine hand carries the full authority, so two bundles that share a first
label are two writers. `connector:<label>` was a second spelling of
`bundle:<authority>` and is retired
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
REST and GraphQL carry is a lossless view of it: `properties` and `edges`
land under `data`, `metadata` holds the id and the authored key spaces, and the
server-set fields (version, timestamps, provenance) land under `status`. The
mapping round-trips exactly, so `substratectl get -o yaml` output applies back with no
edit, and a generic client can read, modify, and write the same object.

Edges are one shape in the canonical envelope, a **list** of
`{rel, to, properties}` in both directions. The flat read record groups the
same edges into a `rel`-keyed map for convenience; that map converts to
and from the list without loss (the list is the map flattened, one entry per
target, in a stable order), and the list is what a write carries. So a
read-modify-write of a record with edges is a fixed point.

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

## Errors

Errors are one shape everywhere, with `problems` carrying the full list when a
batch or a multi-part validation refuses:

```json
{"error": {"code": "validation", "message": "…", "problems": ["…"]}}
```

The code set is closed. The client-error codes:

| Code           | HTTP | When                                                                                             |
| -------------- | ---- | ------------------------------------------------------------------------------------------------ |
| `bad_request`  | 400  | A malformed request, an unknown field, or an unsupported list parameter.                         |
| `validation`   | 422  | An undeclared property, a malformed value, a type mismatch.                                      |
| `conflict`     | 409  | A version check failed (`ifVersion`); re-read and retry.                                         |
| `guard`        | 403  | A refused state transition, or a protected operation (a subject edge, a kind with live records). |
| `forbidden`    | 403  | The caller may not do this at all.                                                               |
| `auth`         | 401  | Missing, invalid, or expired token, or a refused login.                                          |
| `not_found`    | 404  | No such record; a former id is not this, it resolves ([merges](projection.md#merges)).          |
| `rate_limited` | 429  | Slow down; the response carries `Retry-After`.                                                   |

The server-error family is split so a client can tell "try again" from "never
going to work": `internal` (500, an unexpected fault), `unsupported` (501, a
feature this deployment does not offer, the thing `GET
/.well-known/substrate/server.json` detection replaces), and `unavailable`
(503, always with a `Retry-After`). One
case is worth calling out: a well-formed token whose repository cannot be
opened answers `unavailable`, never a masked `401`, so a store the binary
cannot serve is diagnosable instead of looking like a bad credential.

The same problem object appears under `extensions` in a GraphQL error and in the
[watch stream](changelog.md)'s terminal error frame, so an error means the same
thing wherever it surfaces. An unmatched path under an API prefix is that same
object with `404 not_found` — never the console's HTML with a 200.

One more code lives on the changelog surface: `compacted` (410) answers a
`from=` below the retention [horizon](changelog.md#frames-and-the-horizon),
telling a consumer that has fallen too far behind to re-list rather than
silently miss rows. That is the whole closed set; nothing else appears in
`error.code`.

Next: [users and tokens](auth.md), the way in and what a token is.
