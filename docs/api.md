# The API

The substrate serves one REST surface, under `/api/v1`, and every client
speaks it: the console, the CLI and an agent's `query` tool all learn the one
grammar written here. Records are read at **one route**, `GET /api/v1/records`,
which lists, ranks or tails them depending on its parameters, and are written
at the **record path**, `/api/v1/{authority}/{package}/{kind}/{id}`, which is
the record's reference value. A new kind never adds an endpoint: the routes
are the same for every package. This page is the surface: the records route and
its four modes, the record path, the filter grammar, pagination, the five
mutations, discovery, search and the errors. Authentication has a page of its
own, [users and tokens](auth.md).

## REST resources

Two shapes carry every record read and write. The **records route** is the
one list, and the **record path** is one record's URL: the kind's three
segments and the id, which is exactly its
[reference](data-model.md#kinds-and-references) value under `/api/v1/`
([decision 0033](decisions/0033-the-path-grammar-has-no-separators.md),
[decision 0047](decisions/0047-a-kind-lives-in-a-package.md),
[decision 0079](decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md)):

```http
GET    /api/v1/records?filter&orderBy&first&after|offset&expand&withAnnotations&count  # the list
GET    /api/v1/records?q&mode&filter&first                                  # the ranked read
GET    /api/v1/records?watch=1&filter&from&generation                       # the tail
POST   /api/v1/records                            # create; the body names `kind`, the server assigns the id
GET    /api/v1/{authority}/{package}/{kind}/{id}  # takes no query parameter
PUT    /api/v1/{authority}/{package}/{kind}/{id}  # upsert at the given id
PATCH  /api/v1/{authority}/{package}/{kind}/{id}  # patch, including state transitions
DELETE /api/v1/{authority}/{package}/{kind}/{id}  # soft delete; ?ifVersion= guards it, ?purge=true collects it now
```

There is no per-kind list route: `/api/v1/{authority}/{package}/{kind}` names
nothing and answers the router's JSON `404`, and a wrong method at a path that
exists answers `405`. Neither ever falls through to a write.

**There is no repository segment anywhere.** The bearer token names the
repository, so an address never has to, and there is nothing to get wrong.

The record path carries a record's **full identity**: `{authority}/{package}/{kind}`
names the kind, `{id}` the id within it. Ids are unique per kind, so the same
id may exist under two kinds as two unrelated records, and a record read is
always scoped to its kind. There is no cross-kind read by bare id anywhere on
the surface; a list that spans kinds names each one in `filter.kinds`.

One id form needs care. A [kind declaration](vocabulary.md)'s id **is** a kind
reference, so it carries a `/`. A client percent-encodes it, and the API
decodes it exactly once:

```http
GET /api/v1/substrate.reamde.dev/core/kind/samples.substrate.reamde.dev%2Ftasks%2Ftask
```

### The records route

`GET /api/v1/records` is every "read some records" question, in **four modes**
told apart by their parameters. The four share `filter`
([the grammar](#the-filter-grammar)), and that is where the sharing ends: each
mode names the parameters and the filter arms it honors and **refuses the rest
by name** (`400 bad_request`), because a silently ignored parameter returns
unfiltered rows that look filtered. One route to look at and one filter to
learn, not one uniform grammar.

| Mode | Selected by | Parameters | Filter arms | Answer |
| --- | --- | --- | --- | --- |
| **list** | none of `q`, `watch=1`, `aggregate` and `downsample` | `filter`, `orderBy`, `first`, `after` or `offset`, `expand`, `withAnnotations`, `count` | all of them | `{records, cursor?, head, generation, included?, matches?, count?}` |
| **ranked** | `q` | `q`, `mode`, `filter`, `first` | `kinds` and `purposes` | `{records, scores, pending}` |
| **watch** | `watch=1` | `watch`, `filter`, `from`, `generation` | `kinds` alone | the ndjson tail |
| **aggregate** | `aggregate` (or `downsample`) | `aggregate`, `downsample`, `filter` | a metric read: `kinds`, `implements` and the stream properties; a property read: all of them | `{metric?, property?, unit?, units?, bucket?, from?, exact, rows, sources?, head}` ([the aggregate read](#the-aggregate-read)) |

A kind named in `filter.kinds` that this repository never declared is
`404 not_found`, exactly as the record path answers for an unknown kind.

**The list** is the general read: distinct records across any set of kinds,
filtered, ordered, keyset-paged ([pagination](#pagination)), with two optional
sidecars and an optional [count](#counting-the-filtered-set). `withAnnotations=1` adds each row's `annotations`, off by default
so a list of a heavily annotated record stays small. `expand` follows
references one hop:

```http
GET /api/v1/records?filter={"kinds":["ada.example.com/tasks/task"],
                             "properties":{"status":{"eq":"open"}}}
                    &orderBy=dueAt&first=20&expand=project,assignee

→ {"records": [...], "cursor": "eyJv…", "head": 4207, "generation": "7f3a0c2e9b1d4e6f",
   "included": {"ada.example.com/tasks/project/kq3v9x2m41pf": {"id": "kq3v9x2m41pf",
                  "kind": "ada.example.com/tasks/project", "properties": {…}, …},
                "ada.example.com/people/person/9f2k": {…}}}
```

`expand=a,b` names **reference properties declared by the kinds the filter
admits**, and the page carries their referents under `included`, keyed by the
record path exactly as the pointing row wrote it, each referent once however
many rows point at it. A client that wants the join inline does one map
lookup per `ref`. The rules: one hop only; a single or `repeated` reference
expands and a `keyed: true` map of pointers does not; a pointer written under a former id resolves to the canonical
record; a dangling pointer has no entry, and the row's own `{ref}` value still
says where it pointed; a name no admitted kind declares as a reference is
`422 validation` listing what could have been expanded; and a page whose
expansion would load more than 500 referents is `422` telling you to lower
`first` or expand fewer properties. A single-record `GET` does not expand.

**The ranked read** is [search](#search): `q` scores and orders instead of
filtering, `mode` picks the arm, `first` is the hit count, and `filter.kinds`
and `filter.purposes` narrow the candidates. It carries `records` in rank order, a `scores` sidecar
keyed by record path, and `pending`; no `cursor`, `head` or `generation`,
because a ranking has no keyset and opens no single snapshot, so it claims
none. Every other list parameter (`orderBy`, `after`, `offset`, `expand`,
`withAnnotations`) and every filter arm but `kinds` and `purposes` is refused
with `q` by name: both ranking arms cap candidates BEFORE hydration, so a predicate applied
to the top-k afterwards would not be the filtered top-k, and the substrate does
not pretend otherwise.

**The tail** is the [watch](changelog.md#watching) narrowed to a set of kinds:
`GET /api/v1/records?watch=1&filter={"kinds":[…]}&from=&generation=` streams
the same ndjson frames `GET /api/v1/changes?watch=1` does, opened with a
bookmark and resumable from one. The change filter has no property arms, so
under `watch=1` the `filter` admits `kinds` alone and every other arm is
refused by name; `/changes` keeps its own richer change filter (`ops`,
`actors`, their exclusions, `recordId`+`recordKind`, `q`) and alone takes
`values=1`, each property's [before and after](changelog.md#values-on-request).

### Who points at a record: `referencing`

The reverse read is a filter arm, not a sub-resource. `referencing` narrows
the list to the records holding a reference AT one record, optionally through
one named property:

```http
GET /api/v1/records?filter={"referencing":{"ref":"ada.example.com/tasks/project/kq3v9x2m41pf",
                                            "property":"project"}}

→ {"records": [...], "head": 4207, "generation": "…",
   "matches": {"ada.example.com/tasks/task/t9": [{"property": "project"}],
               "ada.example.com/notes/note/n4": [{"property": "project"},
                                                  {"property": "links", "path": "related.project"}]}}
```

The target is matched by its canonical id **and every former id**, so a pointer
written before a [merge](projection.md#merges) still counts, and every declared
reference answers, pinned or not, a kind's own property or one nested inside an
object. The answer is a page of distinct RECORDS, the same envelope as every
other list, and it composes with the rest of the grammar (`kinds` for the
source kind, `properties`, `orderBy`, `first`/`after`, `expand`). One source
can point at the target from two sites, so the page carries `matches` beside
it: for each record on the page, keyed by its record path, every site at which
it points at the target, `property` naming the reference and `path` locating
a nested site (absent at the top level). A `ref` that is not a `<kind>/<id>`
path, or whose kind is unknown, is `422 validation`.

A page carries no total. [`count=1`](#counting-the-filtered-set) asks for the
number of records pointing at the target, and that count scans every pointer
at it, so a reader paging a record with thousands of pointers asks on its
first page alone and carries the number forward.

`refs` in place of `ref` names several targets, and the page is the records
pointing at ANY of them: one read for "what points at me" when the owner holds
a work address, a personal one and an alias. Each target is matched by its
canonical and former ids as `ref` is, the targets may be of different kinds,
and `property` narrows the same way:

```http
GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/google/calendarevent"],
                            "referencing":{"refs":["providers.substrate.reamde.dev/google/emailaddress/work",
                                                   "providers.substrate.reamde.dev/google/emailaddress/home"],
                                           "property":"attendees"}}
```

A record pointing at two of the targets is on the page once, counted once by
`count`, and its `matches` entry names each site it points from. `refs` takes
at most 256 paths; a longer list, a filter setting both `ref` and `refs`, and
a `referencing` naming neither are `422 validation`, and the message says
which.

It is the GENERAL question, and it stays paged for that reason: any record can
be pointed at from anywhere, by any property. The narrow one — which MIRRORS
converged on this subject — is answered inline on the single-record read, as
[`linkedFrom`](#the-flat-record).

### Creating a record: `POST /api/v1/records`

`POST /api/v1/records` is the one body-addressed write: the body is the put
input WITH `kind`, and the server assigns the id.

```http
POST /api/v1/records
{"kind": "ada.example.com/tasks/task",
 "properties": {"name": "Buy milk", "dueAt": "2026-08-13T09:00:00Z"}}

→ 201 {"id": "kq3v9x2m41pf", "kind": "ada.example.com/tasks/task", …, "version": 1}
```

A body with no `kind` is `422`, an unknown kind `404`, and a body carrying an
`id` is `422` naming the door for a chosen id: `PUT` at the record path, which
fixes `(kind, id)` in the URL. Letting `POST` upsert under a supplied id would
make two doors to one write with two answers about what a repeat does. The
status is `201` for a create and `200` for an update, the same rule `PUT`
follows, and `Idempotency-Key` binds to it
([idempotency](#idempotency-and-retries)).

## The flat record

Requests and responses carry the **flat record**: one JSON object with
`properties` and the server-set fields at the top level. The
four-key [envelope](data-model.md#the-envelope) is the YAML document form;
REST never wraps. `title`, `body`, and the temporal properties appear inside
`properties` and nowhere else, so `PutInput` and `PatchInput` accept them only
there. `PutInput` carries an optional top-level `kind`, `id` and `ifVersion`;
the CLI is what maps `metadata.id` and `metadata.ifVersion` onto them. On a
`PUT` the path names both the kind and the id, so the path is what the write
addresses; on `POST /api/v1/records` the body's `kind` names the kind and an
`id` is refused ([above](#creating-a-record-post-apiv1records)).

A worked sequence over the to-do list. Add a task (the body names the kind,
the server assigns the id):

```http
POST /api/v1/records
{"kind": "samples.substrate.reamde.dev/tasks/task",
 "properties": {"name": "Buy milk", "dueAt": "2026-08-13T09:00:00Z"}}

→ 201 {"id": "kq3v9x2m41pf", "kind": "samples.substrate.reamde.dev/tasks/task",
       "properties": {"name": "Buy milk", "title": "Buy milk", "status": "open",
                      "dueAt": "2026-08-13T09:00:00Z"},
       "version": 1, "createdAt": "2026-08-04T10:00:00Z",
       "updatedAt": "2026-08-04T10:00:00Z"}
```

List what is open, soonest first (the filter is URL-encoded JSON, the grammar
is below):

```http
GET /api/v1/records
      ?filter={"kinds":["samples.substrate.reamde.dev/tasks/task"],
               "properties":{"status":{"eq":"open"}}}&orderBy=dueAt

→ {"records": [...], "cursor": "eyJv…", "head": 4207, "generation": "7f3a0c2e9b1d4e6f"}
```

Complete one. A state change is just a patch, and the
[declaration](data-model.md#validation-and-state-machines) stamps
`completedAt`:

```http
PATCH /api/v1/samples.substrate.reamde.dev/tasks/task/kq3v9x2m41pf
{"properties": {"status": "done"}}
```

Read the person GitHub linked up. Single-record reads carry two sidecars a
list never does. `propertyMeta` is per property: who wrote it, at which tier,
and the alternatives other sources assert
([managed properties](projection.md#managed-properties) explains the
mechanism). `linkedFrom` is the records that map ONTO this one
([reading the links back](projection.md#reading-the-links-back-linkedfrom)).
And, if you asked by an id that was merged away, `canonicalId` tells you where
it went ([merges](projection.md#merges)):

```http
GET /api/v1/samples.substrate.reamde.dev/people/person/9f2k

→ {"id": "9f2k", "kind": "samples.substrate.reamde.dev/people/person",
   "properties": {"name": "Ada Lovelace", "emails": ["ada@example.com"]},
   "propertyMeta": {"name": {"manager": "console", "tier": "owner",
     "updatedAt": "2026-08-04T09:12:00Z",
     "alternatives": [
       {"actor": "function:providers.substrate.reamde.dev:github:githubsync",
        "value": "ada", "updatedAt": "2026-08-04T08:00:00Z",
        "source": "providers.substrate.reamde.dev/github/user/ada"}]}},
   "linkedFrom": [
     {"ref": "providers.substrate.reamde.dev/github/user/ada",
      "kind": "providers.substrate.reamde.dev/github/user", "title": "ada",
      "property": "person",
      "mapping": "samples.substrate.reamde.dev/people/githubuserperson"},
     {"ref": "providers.substrate.reamde.dev/slack/user/U42",
      "kind": "providers.substrate.reamde.dev/slack/user", "title": "Ada L.",
      "property": "person",
      "mapping": "samples.substrate.reamde.dev/people/slackuserperson"}]}
```

An alternative's `updatedAt` is its source record's, not the target's, and
its `source` is that record's path; a machine-held property's manager names
its `source` too, where its own offer backs the stored value.
[Reading provenance](projection.md#reading-provenance-propertymeta) has the
rule.

`linkedFrom` is one entry per source record whose mapping-owned subject slot
points here: `ref` its record path, `kind` the same kind said on its own,
`title` its display title, `property` the slot it points from and `mapping`
the [recordmapping](projection.md#record-mappings) that owns that slot, sorted
by kind then id. The target is matched by its canonical id AND every former
id, so a mirror linked before a merge still counts. The key is ABSENT — never
an empty array — on a kind no mapping targets, so "nothing maps onto this
kind" and "nothing has linked yet" are the same absence rather than two
answers. A list read never carries it and there is no opt-in: it is one query
per record, and [`referencing`](#who-points-at-a-record-referencing) is the
bulk answer
([decision record 0088](decisions/0088-a-single-record-read-carries-its-inbound-mapping-owned-links.md)).

## The five mutations

These five mutations are the complete write surface, for every actor, forever.
Each one addresses its
target by **full identity**: the kind beside the id (the record path's
`{authority}/{package}/{kind}` names the kind for `put`, `patch` and `delete`;
`POST /api/v1/records` and `merge` carry it in the body; an agent's `write`
tool takes it as an argument), because an id is unique per kind, never per
repository:

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

`DELETE ?purge=true` runs that purge in the delete itself, so a writer can
start a record over: the tombstone lands if the record was live, what it owns
through `onDelete: cascade` is tombstoned for the sweep, the row is
hard-deleted, and the changelog records a `gc` entry with reason `purged`.
The next `put` at the id creates a fresh record, and a mapping source
resolves its subject again through the probes when that `put` leaves the
subject slot out, which is how one identity is re-resolved after a mapping's
probes improve. A `put` that names the slot keeps the subject it names, so
put back the old `properties` without it. The old subject is not deleted or
re-pointed. On a kind a mapping points at, that `put` is refused like any
client-supplied create id ([idempotency](#idempotency-and-retries)), so a
purged subject comes back through `POST` with a new id. `purge` takes `true`
or `1`, and `false` or `0` is a plain delete; it combines with `ifVersion`.
A retried purge answers `404 not found`, because the record no longer exists.
A record a finalizer holds answers `409 conflict` and nothing changes, the
tombstone included: delete it without `purge`, wait for the finalizers to
release, then purge. A purge through a merge loser's former id answers `409
conflict` naming the canonical id, because the former id resolves to the
winner. A declaration record answers `422 validation`: it leaves through
admission
([decision record 0122](decisions/0122-a-delete-may-purge-so-a-put-starts-the-record-over.md)).
Purging an account runs its whole `onDelete: cascade` inside the request,
under the repository's write lock, and the mirrors it owns are only
tombstoned: a sync within the sweep window restores them with their old
properties and subjects, so purge those mirrors first when they must start
over too. `purge` is a REST and `substratectl` option only: an agent's
`write` tool and a function's `delete` effect always delete plainly, because
a purge cannot be undone. It differs from the bundle lifecycle's `purge`
([bundles](bundles.md)), which tombstones a bundle's records for the sweep.

A `patch` onto a tombstone is refused `404 not found` and changes nothing:
a patch edits a record that exists, and a tombstone is gone to every list.
The one patch a tombstone takes releases finalizers (`removeFinalizers` and
nothing else), which is how a teardown lets the collector take it.

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

One rule places every verb, written into the contract: **a resource's
operational verbs live at the resource**, its own
`{authority}/{package}/{kind}/{id}` path, one level below the id where no id
can sit. That is why the trigger verbs live under
`substrate.reamde.dev/core/trigger/…` — trigger records are core's, so their
verbs sit beside them — and why `merge` and `split`, which act on two records,
sit at the version root instead. A record's reverse read is not a verb: it is
the [`referencing` filter arm](#who-points-at-a-record-referencing) of the
records route. The one cross-kind status read, `GET /api/v1/sync/status`,
sits at the root for the same reason: it lists every record of any kind
binding the core `sync` trait, with the trait's properties joined to the
record triggers on its kind ([connections](bundles.md#connections)).

### Idempotency and retries

A retried write is safe when the request names its own target. `put` with an id
is a primary-key upsert, so retrying it lands the same row. Any of the five
mutations under its version precondition (`ifVersion` on `put`, `patch`,
`delete` and `split`, `winnerVersion` and `loserVersion` on `merge`) is
compare-and-set: the second attempt sees the version it already moved and fails
`conflict`. A blob `PUT` is content addressed by its digest. The trigger
delivery path carries its own idempotency key, so a redelivered change applies
once. The exception is a function body that runs an agent: the agent commits
its writes as it runs, so the delivery claims itself when the agent opens its
thread. A delivery that fails after that parks instead of retrying, one a crash
interrupted is parked as interrupted and rerun once by the first trigger pass
after the restart, and a retry of either by hand runs the agent again
([running an agent](functions.md#running-an-agent)).

A retried write is NOT safe on its own when the server assigns the identity or
the effect. `POST /api/v1/records` mints a random id, so a client that
retries after a timeout creates a second record.
`POST …/core/function/{name}/call` and `…/core/agent/{name}/call` run the body
again, with its effects. A retried `merge` or `split` under a version
precondition fails `conflict`, because the first attempt moved the versions,
and the client cannot tell that from a merge somebody else made.

Those five operations take the `Idempotency-Key` request header: the client's
name for one attempt, any string up to 255 bytes (a UUID is the usual choice).
A function or agent body hands its own external effects an idempotency key
derived from the client's, so a retry presents the same key downstream too.

```http
POST /api/v1/records
Idempotency-Key: 6f1c2e3a-9b0d-4c7e-8a21-5d3f0b9e7c44
Content-Type: application/json

{"kind": "samples.substrate.reamde.dev/tasks/task",
 "properties": {"name": "file the report"}}
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
- A failed attempt stores no outcome. A `422`, a `500 function_failed` or a
  connection lost before the commit leaves no key behind, and the retry runs
  the operation again. The exceptions are an agent call and a function call
  whose body ran an agent, in the next item.
- An agent call binds its key to the thread the moment the thread opens,
  because the loop's tool effects commit one by one before the run settles.
  A repeat after the first attempt failed mid-run, or after the server died
  before settling, is `409 conflict` naming the thread: the client reads the
  thread (its messages record every effect) and runs again under a new key.
  One key never opens two threads. A function call whose body runs an agent
  binds its key to the first thread the same way, so a repeat after the body
  failed is `409 conflict` naming that thread.
- A stored outcome is capped at 1 MiB. A larger one is not kept: the effect
  still ran once, and the repeat is `409 conflict` saying the outcome was not
  retained. For a create, merge or split the message names the record the
  first attempt wrote, so the client reads it instead.
- Agent chat streams and is excluded; there is no stored outcome to replay.
  `vocabulary/apply`, catalog import and `POST /tokens` do not take the header,
  and a `POST /api/v1/records` whose `kind` is a vocabulary kind (`kind`,
  `trait`, ...) refuses it (`422`): a declaration is addressed by its own id.

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

A filter is one JSON document, URL-encoded in the records route's `?filter=`,
and the same document an agent's [`query` tool](agents.md#tools) and the CLI's
`--filter` take:

```json
{"kinds": ["samples.substrate.reamde.dev/tasks/task"],
 "properties": {"status": {"eq": "open"},
                "dueAt": {"lt": "2026-08-11T00:00:00Z"}},
 "labels": {"owner/starred": {"eq": true}}}
```

- `kinds` names kinds by reference, in full (`<authority>/<package>/<name>`);
  a bare word is `422 validation` naming every kind the repository declares
  under it ([0101](decisions/0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)).
  Absent, the list spans every kind in the repository; a kind the repository
  never declared is `404`.
- `properties` carries one condition per property. The operator set is one
  rule, not a per-type table: `secret` and `digest` properties refuse
  filtering entirely, `reference` takes `eq`, `in`, `contains` and `exists`
  and is filtered by the PATH string it points at (not by the object the read
  serves) — a single reference as one indexed equality on that path, an index
  the engine builds for every kind's single references without a declaration,
  a `repeated` one by containment — and every other declared property takes
  the full grammar (`eq`, `gt`,
  `gte`, `lt`, `lte`, `in`, `prefix`, `contains`, `exists`), compared as its
  declared [property type](data-model.md#property-types). State properties
  filter here like any other. A `money` property takes `eq`, `in`, the four
  comparisons and `exists`, and each operand is itself a money value:
  `{"price": {"gte": {"amount": 1000, "currency": "EUR"}}}` is every price
  of at least 10.00 EUR. The comparison holds within the operand's currency;
  a bare number carries no currency and is refused. A `quantity` property
  takes the same operators, and each operand is itself a quantity value
  without a comparator:
  `{"weight": {"lt": {"value": "160", "unit": "[lb_av]"}}}` is every weight
  below 160 pounds, a stored 72.4 kg among them. The engine converts the
  operand exactly into each unit the rows hold, inside the operand's
  [measure](units.md); a stored value of another measure never matches, and an
  operand outside a declared `measures:` is refused. On a kind binding the
  [observed](observations.md) trait, a `value` whose row says `delta: true`
  holds a temperature difference, as the observed index reads it: the
  filter compares it with the operand read as a difference, so
  `{"value": {"gt": {"value": "0.3", "unit": "Cel"}}}` is a deviation above
  0.3 K on such a row and a temperature above 0.3 °C on any other. Where the
  property declares `units:`, those codes are every unit a row can hold and
  no row is read to find them. Otherwise the grammar admits codes no list
  names (`Mg`, `kg/h`), so each request first reads the distinct units of the
  filtered kinds' rows (the live ones, or the tombstones under
  `"deleted": true`): one pass over the rows the rest of the filter selects
  (its time range and every other condition; the quantity comparisons are
  built last, so each narrows by everything else), whatever `first` says,
  and a refusal past 256 distinct spellings among them. An order by the
  quantity reads the same way, under the whole filter. Declaring `units:`
  removes that read. A value stored with a
  comparator (`<0.5`) matches only where the comparison holds for its whole
  half-line, and `in` is refused on a property that declares `comparator:
  true` ([the table](data-model.md#property-types)). `match` is the one operator that reads words
  rather than values: a query in the [search grammar](#the-search-grammar)
  against ONE property's own text, so `{"notes": {"match": "rack lay*"}}` is
  every record whose `notes` holds a word `rack` and a word starting `lay`,
  in any form and any case. It applies to string-family and prose properties
  (scalar or repeated, whose items are matched together), `title`, `body` and
  labels, and is refused by name on a number, an instant, a boolean, a state
  or a reference, which have no words. It vectorizes the value at read time,
  so it is exact to the property and not index-backed; the index-backed
  whole-record form is `search`.
- `search` narrows to the records whose **search index** matches a query in
  the [search grammar](#the-search-grammar): every text the kind indexes at
  once (the title, every string-family property, prose that has not opted
  out), index-backed. It is a PREDICATE like every other arm, so it composes
  with `properties`, `labels` and the rest, keeps the caller's `orderBy` and
  pages by the same keyset cursor. It says nothing about rank: that is `q`,
  the ranked read, which takes the same grammar and answers scores instead of
  a page. A query with no word to match (`"*"`, `"-"`) is `422 validation`
  naming the arm, never an empty page.
- `labels` matches the short, indexed metadata, and takes the **same condition
  objects** a property does: `{"owner/starred": {"eq": true}}`, never a bare
  value.
- `ids` narrows to a list of ids within the kinds already selected.
- `deleted` picks the tombstones: absent or `false` lists only live records,
  `true` lists only soft-deleted ones.
- `orphaned` picks the **orphaned mapping targets**: `true` lists only the
  records the engine has marked — a kind something maps onto, no live covered
  source left, and nothing above the machine tier holding a property — and `false`
  only the unmarked. Absent is every record. It is derived state, not a
  property, so a re-linked record leaves the set on its next write
  ([the orphan mark](projection.md#when-the-last-source-goes-the-orphan-mark)).
- `ambiguous` picks the **ambiguous mapping sources**: `true` lists only the
  records the engine has marked (a source a mapping left unlinked because its
  probe found several candidates), and `false` only the unmarked. Absent is
  every record. Derived state like `orphaned`, it clears on the write that
  links the source
  ([`onAmbiguous`](projection.md#when-a-probe-finds-several-candidates-onambiguous)).
- `implements` selects every kind carrying one [trait](data-model.md#traits),
  across every package. Every arm narrows, so `implements` intersects with the
  kinds already in play rather than widening them; alone it is the cross-kind
  query. A pair that can match nothing is a `validation` error naming the
  mismatch, not an empty page.
- `purposes` selects the kinds declaring one of the named
  [purposes](vocabulary.md#the-reserved-keys), `primary`, `supporting` or
  `internal`, an undeclared purpose reading as `primary`:
  `{"purposes": ["primary", "supporting"]}` is everything but machinery. It
  intersects with `kinds` like `implements` does, and a set that admits no
  kind is an empty page, because a repository with no supporting kinds has no
  supporting records. Any other word is `422 validation` naming the three.
  The ranked read takes it beside `kinds`; the tail refuses it.
- `referencing` is the reverse read: the records pointing at one record,
  `{"ref": "<kind>/<id>", "property": …}` with `property` optional, or at any
  of several, `{"refs": ["<kind>/<id>", …]}`, at most 256
  ([above](#who-points-at-a-record-referencing)).

Ordering is `orderBy` with camelCase columns (`dueAt`, `at:desc,createdAt`, or
a JSON list of `{property, desc}`). A number sorts as a number and an instant
as an instant; a `money` property sorts by the exact number it denotes, across
currencies, so a list that wants one currency filters on it. A `quantity`
property sorts by its value converted to its measure's base unit (155 `[lb_av]`
between 50 kg and 71000 g), kept to 40 decimal places of the base unit (two
values that agree that far tie and sort by kind and id), in every unit a write
admitted: the codes the property lists, else its measure's codes, else every
code the table lists, and then any other code the read's rows hold (1 `Mg`
sorts as 1000 kg, though the mass table lists no `Mg`), found by the same one
read of the rows' units a filter makes. Each kind's rows sort in the measure that kind declares, so a
10 `Cel` temperature (283.15 K) sorts after a 20 `K` difference, and a row
marked `delta: true` sorts as a difference. `rad` sorts through pi at 40
places; a level or an arbitrary unit sorts by its own number. A declared
index on a quantity holds the same expression over the declared codes and
needs `measures:` or `units:`; it serves the order while every stored unit is
one of those codes, which `units:` guarantees. The engine resolves an order over the kinds the filter
selects (`kinds`, narrowed by `implements` and `purposes`), or over every kind
when the filter names none, so another kind's declaration of the same name
changes nothing. Where two selected kinds declare the name differently (a
quantity on one, a string on another), the order sorts by the property's
stored JSON text, and a filter naming the one kind restores the typed order. A
name any selected kind declares `secret` or `digest` is refused. Only declared
properties filter and order:
**filterable, indexed, and declared are the same set**, so a query that would
be slow is one the grammar cannot express.

A parameter or a filter arm a given [mode](#the-records-route) does not honor
is a `bad_request` that names it, never a silent success: `orderBy` with `q`,
`filter.properties` with `watch=1`, a `first` misspelled `First`. A misspelled
ordering column is refused naming the camelCase replacement, and a malformed
filter document is refused naming the field that would not decode. The
single-record `GET` honors no parameter at all, so once its kind resolves any
one it is sent (`expand`, `withEdges`, a typo) is the same `bad_request`, and
`DELETE` refuses every parameter but `ifVersion` and `purge`. A query string
that does not parse (a `;` separator, a bad `%` escape) is a `bad_request` on
each of these routes and `/changes`, naming the parse error, because the pair
it drops would otherwise read as absent.

### The window read

A list whose filter bounds `at` on **both** ends (`gte` or `gt`, and `lt` or
`lte`) over kinds that bind `temporal` is a **window read**, and it answers
with more than rows: every series among the kinds in play (a record of a kind
binding [`recurring`](data-model.md#traits) whose `recurrence` or `rdates` is
set) is expanded inside the window, and its occurrences are merged into the
page beside the plain rows and the overrides, ordered by slot
([decision 0081](decisions/0081-the-window-read-computes-occurrences-and-recurring-is-core.md)).
A slot the series' `exdates` names, or that an `override` row claims through
`recurrenceOf` and `originalAt` (whatever that row's own `at` and kind), is
left out; the series row itself is never on the page, it is on the timeline
only through its occurrences.

```http
GET /api/v1/records
      ?filter={"implements":"substrate.reamde.dev/core/temporal",
               "properties":{"at":{"gte":"2026-07-01T00:00:00Z","lt":"2026-07-08T00:00:00Z"}}}
      &orderBy=at

→ {"records": [
     {"id": "levo_20260701T060000Z", "kind": ".../health/medicationschedule",
      "properties": {"name": "Levothyroxine", "at": "2026-07-01T06:00:00Z",
                     "recurrenceOf": {"ref": ".../health/medicationschedule/levo"},
                     "originalAt": "2026-07-01T06:00:00Z", ...},
      "version": 0, "computed": true, ...},
     {"id": "dentist", "kind": ".../calendar/calendarevent", "properties": {...}, "version": 3, ...},
     ...],
   "cursor": "w.eyJh…", "head": 4211, "generation": "7f3a0c2e9b1d4e6f"}
```

A **computed occurrence** is served in the record envelope so nothing has to
render it differently: the series' kind, the id `<seriesId>_<slot>` (the slot
in UTC, `YYYYMMDDTHHMMSSZ`), the series' properties with `recurrence`,
`rdates` and `exdates` removed and the slot written under the series' own
temporal name (`endsAt` keeps the anchor's duration in the rule's zone's wall
clock), `version` 0, and `computed: true`. When the series' kind binds
`override` too (`task` and `medicationschedule` do), the envelope also
carries `recurrenceOf` and `originalAt`, so a `PUT` at the id, the envelope
as its body, **materializes the occurrence as an override**: that is how one
instance is moved, edited or given more detail. `GET` at that id answers the
same envelope while the series produces the slot and nothing overrides it. A
kind that does not bind `override` (a provider's series mirror, whose
exceptions are rows of another kind) gets a read-only envelope without the
pair. Cancelling one occurrence is adding its slot to the series' `exdates`.
A stored record at a computed id always wins.

The rules that hold the page correct: `orderBy` is `at`, ascending or
descending, and nothing else (a `bad_request` names it); the candidate series
are enumerated whole, and more than 10,000 in one window is a `validation`
error rather than a short page; a series whose rule the expander cannot walk
(too dense for its budget, an unknown zone) is named on the page's
`problems` and the page stands without it; a cursor is pinned to the filter
and direction it was minted under. One bound alone (`at` with only `gte`) is
an ordinary list: the rows are filtered as ever and nothing is computed.

A function body's `host.records.list` and an agent's `query` tool answer the
same window read for the same filter
([decision 0111](decisions/0111-a-function-and-an-agent-list-read-is-the-window-read.md)).

## Pagination

Lists page forward with a keyset cursor carried behind one opaque token. You
pass `first` for the page size (default 50, at most 500) and, on the next
request, the `cursor` a page returned as `after`:

```http
GET /api/v1/records?filter={"kinds":["samples.substrate.reamde.dev/tasks/task"]}&first=50
→ {"records": [...], "cursor": "eyJv…", "head": 4211, "generation": "7f3a0c2e9b1d4e6f"}

GET /api/v1/records?filter={"kinds":["samples.substrate.reamde.dev/tasks/task"]}&first=50&after=eyJv…
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
`orderBy` AND the `filter` it was minted for, so replaying it against a
different order or a different predicate is refused (`422 validation`) rather
than silently seeking past rows the new predicate admits and answering a short
page that looked complete. An exhausted list carries no `cursor` at all: there
is no next page.

### Addressing a page by its number

A reader that draws a numbered pagination bar cannot walk to page seven, so
the list takes `offset` as well: the count of ordered rows to discard before
the page begins ([decision 0084](decisions/0084-a-records-list-pages-by-offset-beside-the-keyset-cursor.md)).

```http
GET /api/v1/records?filter={"kinds":["samples.substrate.reamde.dev/tasks/task"]}&first=50&offset=300
→ {"records": [...], "cursor": "eyJv…", "head": 4211, "generation": "7f3a0c2e9b1d4e6f"}
```

`offset` and `after` are **alternatives**, and sending both is refused
(`422 validation`): a cursor seeks to a position in the order and an offset
skips a count of rows, so a page cannot do both. Two properties of the keyset
walk are given up by asking for a page this way, and neither is recoverable:

- **It is not stable.** An offset page is a snapshot of one instant. A row
  inserted or deleted by a concurrent writer shifts every later page, so a row
  can be skipped or seen twice across two offset reads. A walk that must see
  each row exactly once — an export, a sync, an agent reading a feed — pages
  with `after`.
- **It costs what it skips.** The rows before the page are ordered and then
  discarded, so a deep offset page costs more than a shallow one, where a
  keyset seek costs the same at any depth.

An offset page still returns a `cursor` minted from its last row, so a reader
that jumped to a page can hand off to a stable walk from there. `offset` is
refused on the [window read](#the-window-read) (`400 bad_request`), which
merges computed occurrences into the page and so has no row count to skip, and
on the ranked read, which has no keyset at all.

### Counting the filtered set

`count=1` asks the list for the size of the whole set its filter admits, and
the page answers it as `count` beside the rows
([decision 0134](decisions/0134-the-records-list-counts-its-filtered-set-on-request.md)):

```http
GET /api/v1/records?filter={"kinds":["samples.substrate.reamde.dev/tasks/task"],
                             "properties":{"status":{"eq":"open"}}}&first=1&count=1
→ {"records": [...], "cursor": "eyJv…", "head": 4211, "generation": "7f3a0c2e9b1d4e6f", "count": 312}
```

The count is the filter's, not the page's: `first`, `after` and `offset`
do not move it, so every page of one walk answers the same number unless a
write lands between them. It honors every arm the list does (`kinds`,
`implements`, `purposes`, `properties`, `labels`, `search`, `ids`, `referencing`,
`deleted`, `orphaned`, `ambiguous`) and excludes exactly what the list
excludes, a tombstone and a merged-away loser included. It is read in the
page's own snapshot, so the rows and the number agree. Zero is a count and is
answered; a list that did not ask carries no `count` at all. `1` is the only
value: any other is `400 bad_request` naming it, because a `count=true` read
as "no" would answer a page without the number its caller is about to render.
A reader that wants the number alone asks `first=1`.

**It costs a scan of every matching row.** The count runs the list's own
predicate as one `SELECT count(*)` with no ordering and no row read out, so
it is served by the same indexes the list is, and the engine caps nothing.
Postgres has no cheaper exact count: a count over a million matching rows
visits a million index entries or rows. That is why it is opt-in, paid only
by the reader who asks.

`count` is refused on the [window read](#the-window-read) (`400
bad_request`): its page merges computed occurrences, which are not rows, so a
count of the stored rows would disagree with the page beside it. The ranked
read and the tail refuse it with every other parameter they do not honor.

Two continuation styles exist, and the parameter name says which you are
holding. The changelog uses real sequence numbers, `from` forward and `before`
backward, because a seq is a meaningful ordinal (its history response returns
a `cursor` seq to pass as the next `before`). Record lists use an opaque
cursor, passed back as `after`, or `offset` for a numbered page.

Every list response also carries the changelog **head** seq captured at the snapshot
it was served from, pinned once at the walk's start and carried through the
cursor, so every page of one walk reports the same head, and the history
**generation** that head belongs to. List, then resume a
[watch](changelog.md#watching) with `from={head}&generation={generation}`:
every listed row's change is at or before `head`, and the watch replays exactly
the changes after it, so the handoff has no gap and no double-see. A cursor
minted under one generation is refused after a restore replaces the history
with the same `410 compacted` the changefeed gives a stale `from=`, its
problem object naming the current `head` and `generation`: list again, never
a `422` a client would read as its own mistake.

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
registration asks for; the request surface,
with its endpoint and its compatibility; and a feature list. No dialect
is on the wire: the
[vocabulary](vocabulary.md#vocabulary-evolution-and-the-dialect-contract) and
[changelog](changelog.md#the-dialect-a-changelog-is-written-in) dialects are
stored per repository, and a binary too old for a store refuses to open it,
which surfaces as `unavailable`. That
feature list is what a client reads instead of trying a route to see whether
it exists: each entry names a feature, its stability and the `surfaces` that
serve it:

```json
{"versions": [{"name": "v1", "status": "served"}],
 "server": {"version": "…", "build": "…"},
 "changelog": {"horizon": 0},
 "features": [{"name": "triggers", "stability": "stable", "surfaces": ["rest"]},
              {"name": "changefeed", "stability": "stable", "surfaces": ["rest"]},
              {"name": "search", "stability": "beta", "surfaces": ["rest"]},
              {"name": "agents", "stability": "alpha", "surfaces": ["rest"]}],
 "surfaces": {"rest": {"endpoint": "/api/v1", "compatibility": "supported"}},
 "grammar": {"kind": "<authority>/<package>/<name>",
             "record": "<authority>/<package>/<kind>/<id>",
             "collection": "/api/v1/records",
             "recordPath": "/api/v1/{authority}/{package}/{kind}/{id}",
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
records: a trigger and a blob manifest are ordinary records and read through
the records route whatever the entry says. Every entry lists `rest`, the one
surface, and the key stays a list so a second surface, should one come, lands
as a new name beside it rather than a reshaped document. The example above is
abridged; the full list, and it is the same for every deployment of a given
build, is `triggers`, `functions`, `bundles`, `blobs`, `export`, `changefeed`,
`search`, `aggregate`, `units`, `embeddings` and `agents`. `grammar.collection`
is the records route and `grammar.recordPath` the record path, so a client
checks the two shapes it must agree with the substrate about before it
addresses anything.

`surfaces` is the verdict per request surface, and it is a different axis from
a feature's stability. `compatibility` is `supported` on `rest`: the REST API
is the interface a client builds on, and a break there is announced, never
silent. A second, preview surface used to sit beside it and is gone
([decision 0079](decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md));
the object is keyed so its removal was one key leaving, not a reshaped
document. A feature's `stability` says how far that feature's own shape has
settled, and it binds on the one door.

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

**The settled features report `stable`**: `triggers`, `functions`, `bundles`,
`blobs` and `changefeed`. `search` reports `beta`: it is served, at
`GET /api/v1/records?q=`, and the hit shape (the per-arm `scores` beside the
page) is young. `export` reports `beta` too: `GET /api/v1/export` streams the
repository's recovery export, a tar of its directory in the snapshot format
([backups](operations.md#backups)), and it freezes by a decision of its own,
not by age. `agents` and `embeddings` report `alpha` because their shapes are
still moving; a surface in their `surfaces` says where they are served, not
that they are frozen. `embeddings` reach a caller as the semantic arm of
[`search`](#search), its one door.

The list is a literal in the server (`internal/api/discovery.go`), and it is
every feature the build serves: one implementation serves them all, so there
is nothing per-deployment to compute. A client reads the list for the
stability stamps and the `surfaces`, not to find out whether a route is
there.

`embeddings` is listed like the rest. Discovery opens no repository, so it
does not answer the narrower question of whether the CALLER's repository
declares an [`llm/provider` row](agents.md): the first semantic query answers
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
announced in discovery before a removal: a version's `status` in `versions`
is `served` today, and `deprecated` is the other value the field declares.
There is no Kubernetes-style multi-version conversion machinery.

## Search

Search is the records route's **ranked read**, `GET /api/v1/records?q=`, and
there is no other door: `?filter=` selects rows by predicate, `q` scores and
orders them, and the two answer different questions.

```http
GET /api/v1/records?q=quarterly+review&mode=hybrid
                    &filter={"kinds":["ada.example.com/notes/note"]}&first=20

→ {"records": [{"id": "n4", "kind": "ada.example.com/notes/note", …}, …],
   "scores": {"ada.example.com/notes/note/n4": {"lexical": 0.42, "semantic": 0.81}, …},
   "pending": 0}
```

It has two arms:

- **Lexical**, on by default for every kind. The title and every
  string-family property index into full-text search, weighted in three bands
  (title first, then declared string properties, then the rest), and `q` takes
  the [search grammar](#the-search-grammar) below. A property opts
  out with `fts: false`; secret-typed properties never index. An email
  address, a URL or a path also indexes the words inside it
  (`ada@inbox.example` is found by `ada`, `inbox` and `inbox.example`), and a
  word with a diacritic also indexes its folded spelling (`José` is found by
  `jose`). Changing what a kind indexes re-indexes its existing records in the
  same apply, and a binary whose indexing rules changed re-indexes every
  record at the repository's next open, in both cases without moving a
  `version` or `updatedAt`.
- **Semantic**, strictly opt-in per property with `embed: true` (the shipped
  vocabulary opts in long prose: message and mail bodies, task and event
  descriptions, and transcripts). Opted-in text is chunked into overlapping windows
  and embedded **asynchronously** after commit, off a queue, so writes never
  wait on an embedding call. Vectors live in Postgres (pgvector) beside
  everything else, 1536 wide
  ([0026](decisions/0026-embedding-vectors-are-1536-wide-or-refused.md)).

**How the lexical arm ranks.** Its candidates are the records holding ANY of
the query's words, every `-exclusion` still applied, so a query with one word
no record holds still finds the records holding the others. The records
holding every word rank first. Within that, the arm ranks by
[BM25F](decisions/0136-search-ranks-by-bm25f-and-a-kinds-purpose.md): each band is a
field with its own weight (title 3, short strings 1.5, prose 1) and its own
length normalization, a word held by few records weighs more than one held by
many, and repetition saturates, so a title match outranks a long text that
repeats the word. A prefix (`lay*`) ranks the word itself above its
completions. `filter.search` and `match` stay predicates: they require every
word, exactly as the grammar reads.

`mode` picks `lexical`, `semantic`, or `hybrid` (the default): hybrid runs both
arms and fuses them by rank (reciprocal rank fusion), so a record near the
top of both ranks highest and neither arm's scale decides. Every hit's score
is then weighed by its kind's [purpose](vocabulary.md#the-reserved-keys):
`primary` 1, `supporting` 0.8, `internal` 0.4, so machinery (accounts, sync
state, the vocabulary itself) ranks below the data it describes without being
hidden; `filter.purposes` hides it. `first` is the hit count, 20 by default.
The answer is the records in rank order, each one's raw per-arm scores under
`scores` keyed by record path (`lexical` is the BM25F sum, unbounded,
`semantic` cosine similarity, 0 where an arm did not rank), so a caller can
threshold rather than trust a rank, and `pending`: the number of properties
the drain has yet to buy vectors for, counted whenever the semantic arm was
asked for. Non-zero means the ranking covers a partial index (a repository
[restored from its directory](operations.md#backups), a `reembed` in
progress), and it falls to 0 as the drain buys. In a repository that has named
no embeddings provider, hybrid degrades to lexical and `semantic` reports an
error rather than pretending. While properties are queued and no vector from
the resolved provider and model has landed yet, `semantic` refuses with the
`unavailable` code and the count, so "no vectors yet" never reads as "no
matches"; hybrid returns its lexical arm alone. With nothing queued, a
repository with nothing embeddable returns no hits, and a row re-pointed at a
model nobody ran `substratectl repository reembed` for is refused naming the
command.

**Which model bought the vectors is data, per repository.** The one
[`llm/provider`](agents.md#providers) row declaring `embedModel` is where a
repository buys them, each stored vector names that row and that model, and the
semantic arm scores only the currently resolved pair. Re-point the row and the
older vectors stop being scored rather than being ranked against the new ones:
cosine distance between two models' vectors is not a distance. `substratectl
--dsn … repository reembed <repository>` queues their replacement, which the
server's drain loop buys a batch at a time. There is no REST verb for it: it is
the operator's hat, on the box.

### The search grammar

One grammar is read wherever words are matched against a record: the ranked
read's `q`, the list's [`search`](#the-filter-grammar) arm and a property's
`match`. It is web-search syntax plus a word prefix, and every word is
matched in any form (`racks` finds `rack`, the dictionary's stemming) and any
case:

| You type | It means |
| --- | --- |
| `rack layout` | every word, in any order |
| `lay*` | a word starting with `lay` (a star anywhere in a word marks it a prefix of its letters: `*lay*` and `lay*` are one query, because the index holds whole words and can answer "starts with" but never "contains"). A prefix is matched as typed, not stemmed: `ans*` finds `Ansel` and not `anthropic`, and it also finds the word's own stemmed form |
| `"rack layout"` | the words adjacent, in that order |
| `-lunch` | without this word; `-"weekly sync"` without this phrase |
| `rack OR lunch` | either word (`OR` in capitals; `or` is a word) |

A word with a diacritic also matches its folded spelling, so `José` and
`jose` find the same records. Nothing typed reaches the query parser as an
operator: `geo:*` is the word `geo:` as a prefix, `a&b` is one word.
A query that is stars, quotes and dashes alone has no word to match and is
refused as `422 validation` naming the door (`q`, `filter.search`, or the
property), never answered as "no matches". The semantic arm embeds the query
as typed: a prefix means nothing to an embedding.

The substrate does retrieval only: it returns typed records with scores, and
anything generative built on top (a RAG loop, an assistant) is a client
reading this API like every other. [Functions](functions.md) run on the shared
runner and reach the same search through a host call, under their declared
read allowlist, and an agent reaches it through the `q` arm of its
[`query` tool](agents.md#tools).

One ranking rule is built in: the shipped `person` carries a two-state
`prominence` machine (`utility` at birth, `known` once something promotes it,
an address-book sync or the owner), and search ranks `utility` people below
every `known` person, however well they score, so the recruiter who emailed
once never outranks a friend. Against the hits of other kinds a `utility`
person's score is halved and competes like any other, so a contact synced a
moment ago still outranks the machinery their name happens to match, and a
strong match on a task still outranks them.

## The aggregate read

`GET /api/v1/records?aggregate=` reduces measured values into time buckets
([decision 0161](decisions/0161-the-aggregate-read-is-a-mode-of-the-records-route-checked-before-it-runs.md)):
average heart rate per week, steps per day, hours a door was open. It reads
the observed index the fold derives from every kind that binds core's
`observed` trait ([observations](observations.md#the-observed-index)), or a
declared number of the filtered kinds. `aggregate` and `downsample` are strict
JSON, so a misspelled key is a `400` naming it, and the mode refuses `q`,
`watch`, `count`, `orderBy`, `first`, `after`, `offset`, `expand` and
`withAnnotations` by name. Everything else a request can be refused for is a
`422 validation` naming the limit, the form, the measure or the parameter, and
it is refused before any point or rollup row is reduced: the one statement
that finds which forms and codes the range holds runs first, over the rollup
rows' keys and the points at the range's cut ends.

```http
GET /api/v1/records?filter={"kinds":["ada.example.com/observations/trace"]}
  &aggregate={"metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
              "range": {"gte": "2026-04-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
              "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
              "reducers": [{"durationIn": [95, 114]}, {"durationIn": [114, 133]}, "avg"],
              "maxGap": "PT5M",
              "sources": {"policy": "priority", "order": ["garmin-forerunner-265", "apple-watch-s10"]},
              "unit": "min"}

→ {"metric": "ada.example.com/observations/metric/health/heart_rate",
   "unit": "/min",
   "bucket": {"calendar": "week", "timezone": "Europe/Athens", "weekStart": "monday"},
   "from": "raw", "exact": false,
   "rows": [{"start": "2026-04-06T00:00:00+03:00", "end": "2026-04-13T00:00:00+03:00",
             "form": "gauge", "from": "raw",
             "sources": ["ada.example.com/observations/source/garmin-forerunner-265"],
             "coverage": "P6DT22H10M",
             "values": [{"reducer": {"durationIn": [95, 114]}, "value": "612.4", "unit": "min"},
                        {"reducer": {"durationIn": [114, 133]}, "value": "171", "unit": "min"},
                        {"reducer": "avg", "value": "131.6", "unit": "/min"}],
             "count": 24810, "bounded": 0, "synthetic": false, "unavailable": false}],
   "sources": ["ada.example.com/observations/source/garmin-forerunner-265"],
   "head": {"changelog": 2171234, "generation": "7f3a0c2e9b1d4e6f"}}
```

A function reads the same body with
[`host.records.aggregate`](functions.md#the-sdk), an agent with the `query`
tool's `filter`, `aggregate` and `downsample` arguments
([agents](agents.md#tools)), and a person with
[`substratectl aggregate`](substratectl.md#reading), which has a flag for
each field. A function's and an agent's read is refused before it runs where
its series times buckets pass the rows left of their read budget.

### The request

| Key | Means | Default |
| --- | --- | --- |
| `metric` | `{"ref": "<metric record path>"}`: read the observed index for this metric; a path that is not a metric's, or that no metric record has and no observed value names, is a `422` | |
| `metrics` | `[{"ref", "unit"?, "reducers"?, "from"?, "statistic"?}]`: up to 16 metrics in one read ([several metrics](#several-metrics-in-one-read)) | `reducers` and `from` are the read's |
| `property` | `{"name", "anchor"?, "utcOffset"?, "groupBy"?, "sources"?}`: reduce a declared number or duration of the filtered kinds instead, placed in time by `anchor` ([the property read](#the-property-read)) | `anchor` is `at` |
| `range` | `{"gte", "lt"}`, two RFC 3339 instants, the half-open range | required, except on the latest read |
| `bucket` | one of the four time modes below; absent is one bucket over the whole range | |
| `reducers` | the reducers, each a name or a one-key object with its argument | required, except on a downsample and on a `metrics` read whose every entry names its own |
| `groupBy` | any of `source`, `about`, `code`, `metric`, `kind`, `channel`, and `statistic` on a read from summaries or the latest read | |
| `from` | `raw` (gauges, amounts, counters), `summaries` (summary points) or `prefer-raw` | `raw` |
| `minCoverage` | `prefer-raw`'s floor, 0 to 1 | `0.9` |
| `sources` | `{"policy", "order"?}`: `separate`, `priority` or `summaries` | `separate` |
| `unit` | a unit of the metric's measure for the values, or a duration unit for `durationIn` and `timeInState`; refused beside `metrics`, whose entries name their own | the metric record's `unit`, else the first stream's |
| `maxGap` | an ISO 8601 duration capping how long a gauge's value holds after its point | none |
| `fill` | `null`, `zero` or `locf` | none |
| `rolling` | `{"buckets", "reducers"?, "through"?}`: `avg`, `sum`, `min`, `max` or `stddev` over a window of buckets ending at this bucket (`through: "current"`) or the one before it (`"previous"`) | `reducers` is `["avg"]`, `through` is `current` |
| `budget` | `{"maxBuckets"?, "maxSamples"?, "maxGroups"?}` | see [budgets](#budgets) |

Exactly one of `metric`, `metrics` and `property`; two is a `422`. A metric read's
`filter` takes `kinds` and `implements` (a kind that does not bind `observed`
is refused by name) and `eq` or `in` on the stream properties `source`,
`about`, `metric` and `channel`; `eq` beside `in` on one property admits what
both admit, as the list's filter does. Any other arm is a `400`. A property
read takes the whole [filter grammar](#the-filter-grammar).

A `source`, `about` or `metric` value is a record path, `{ref}`, or a bare id
the read completes with the kind the selected kinds' reference pins, as the
list's filter does: `manual` is `<authority>/observations/source/manual` and
never `.../source/googlefit/x/manual`. A bare id is a `422` where those kinds
pin no kind, any kind (`about`) or two. A path no stream holds answers no
rows.

A value may leave out its `source`, `about` or `channel`. The filter selects
those streams with `{"exists": false}`, and the others with
`{"exists": true}`. A `null` inside `in` is the absent key, so
`{"channel": {"in": [null, "workout"]}}` is "no channel, or workout". `eq`
on `""` is refused.

`code` filters the intervals by code after they are built from every
transition, so a door's `closed` still ends the `open` before it.
`statistic` (`eq` or `in`) narrows the summaries to those statistics; a
gauge's, an amount's or a counter's values carry none and are kept.

### The four time modes

| `bucket` | Buckets | A row's `start` and `end` |
| --- | --- | --- |
| `{"every": "PT5M", "origin"?, "place"?}` | fixed lengths counted from `origin` (the Unix epoch by default), on UTC instants | in the range's offset |
| `{"calendar": "day", "timezone": "Europe/Athens", "weekStart"?, "offset"?, "place"?}` | `minute`, `hour`, `day`, `week`, `month` or `year` in the zone; a day across a clock change is 23 or 25 hours | in the zone, each with its own offset |
| `{"calendar": "day", "fallbackTimezone"?, "place"?}` (no `timezone`) | the recorded-local calendar: each point lands on the date its own UTC offset names, so a travel day can be 20 or 31 hours | `start` at the offset of the row's own first point, `end` at its last point's (points at one instant ordered by record id, then index); a row with no point, at the offsets of the first and last value held in it; a row `fill` makes, at its bucket's other rows', else the range's |
| `{"records": {"filter", "around"?, "maxRecords"?, "sources"?, "utcOffsets"?}}` | each record the filter matches with `at` inside the range is one bucket, `[at, endsAt)` widened by `around` (`{"before", "after"}`, two durations), less the records `sources` drops | the record's window, in the range's offset, or with `utcOffsets` (`{"start": "utcOffsetStart", "end"?: "utcOffsetEnd"}`, the records' declared `int` properties holding minutes east of UTC; `end` is `start`'s where absent) `start` at the record's start offset and `end` at its end offset, the range's where the record holds none; the row carries `record: {ref, version}` and `nonAdditive: true`, because windows may overlap |

`weekStart` is a day name (`monday` by default) and `offset` shifts a day's,
week's, month's or year's start by less than a day (`PT4H` for a vendor day
from 04:00). A bucket that the range cuts is clipped to the range, and its
`start` or `end` says so.

`place` says where a value with a span lands: `start` (the default) puts it in
the bucket its start lies in, `end` in the bucket its last instant lies in, so
a night from 23:00 to 07:00, or from 00:30 to 08:00, lands on the day it ends
and each day holds one night. A span ending at midnight lands on the day
before. Under `end` the read takes the values whose end lies in the range,
wherever they start (a night started the evening before the range counts),
reads amounts from raw points, and is a `422` where it would take a gauge's or
a counter's points, which are instants; the records mode and the property read
refuse it.

**Where a value lands.** In the `every` and `calendar` modes a point lands
whole in the bucket its start lies in (its end under `place` `end`), so an
amount that runs past the bucket's end counts whole there, and a gauge's
held time is cut at each bucket's start and end. In the records mode a
gauge's held time is cut at each window's start and end, and an amount
counts in every window its span meets by the part inside: a sleep stage from
23:59:59.5 to 00:30 counts 29 minutes 59.5 seconds in a window opening at
midnight, and one running ten minutes past the window's end counts up to
it. The part is clipped to the window, so the row's `coverage` is the time
inside, and its number is taken in proportion to the time inside, linearly:
exact for a stage whose value is its duration, an approximation for an
amount spread unevenly over its span. Each row counts such amounts in
`count` and says how many it cut in `prorated`. A summary, a gauge's point
and a counter's reading land in a window by their instant, so a night's
recovery stamped at the instant the night ends falls outside it (the window
is `[at, endsAt)`): widen the window by `around` to take it ([a night and
its recovery](observations.md#reading)). The records mode reads amounts from
raw points.

**The records' source policy.** Several sources write one night or one run
as a record each, and the records mode makes a window of every record the
filter matches, so a night three devices recorded is three windows.
`bucket.records.sources` chooses among them:

```json
{"policy": "priority", "order": ["whoop-4-0", "garmin-forerunner-265"], "property": "source", "match": ["sport"]}
```

`property` names the records' declared property that holds their source (a
reference, a string or an enum, declared by every kind the filter selects),
and `order` ranks up to 100 of its values, first highest; a reference's
bare id is completed as the filter completes it. A record whose own
`[at, endsAt)` overlaps a kept record of a higher-ranked source is dropped
and makes no window. A record with no `endsAt` is an instant: a kept
higher-ranked record whose span holds its `at`, or an instant at the same
`at`, drops it, and it drops no record with a span, so a night a band
wrote before it had an end does not drop the watch's whole night. A source
the order does not name ranks last, and two records of one rank never drop
each other. `match` (optional) names
declared string, enum or reference properties that must also be equal: with
`["sport"]` a band's three-hour activity drops neither the run nor the walk
a watch recorded inside it unless they name its sport. A record outside the
range that overlaps one inside still drops it where the policy keeps it,
which the read decides from the records ranked above it, beyond the range
too. A record is kept or dropped as a range holding them all would decide,
so two adjacent ranges never count one night twice and never drop one from
both. `separate`, the default, keeps every record. This
policy chooses the windows, the request's `sources` chooses among the points
inside each window, and each takes its own `order`.

In the recorded-local calendar the range selects points by instant, so a day
at another offset than the range's is cut where the range starts and ends: a
range of 2026-09-21 at `+03:00` over points at `+01:00` answers 2026-09-20
from `22:00+01:00` to midnight and 2026-09-21 from midnight to `22:00+01:00`.
A row's `start` and `end` are its own group's and code's, whatever else the
read selects, except on a [several-metric read](#several-metrics-in-one-read).

A value written without a UTC offset has no recorded-local date. A
recorded-local read of a stream holding one in the range is a `422` naming
the stream and `bucket.fallbackTimezone`, decided before any point is
reduced. With `fallbackTimezone` (an IANA zone) the read places each such
value by that zone's clock at its instant, reads the stream from raw points,
and counts the values it placed so in the row's `offsetless`. A calendar in a
named `timezone` places every value by that zone and takes no
`fallbackTimezone`.

**The latest read.** No `bucket`, no `range` and the one reducer `last` is the
newest point of each group, straight from the index's latest table: the
dashboard's "current value of everything". `metric` is optional (the filter's
`properties.metric` may select several), `groupBy` takes the stream keys and
`statistic` (a night's latest average and maximum, a row each), and each
row's `start` and `end` are the newest point's `ts` and `ts_end` at its own
offset, its `count` the group's point count. It takes `groupBy`, `unit`
(beside a `metric`) and the filter alone: `fill`, `rolling`, `maxGap`,
`minCoverage`, a `from` other than `raw`, a `sources` policy other than
`separate`, `groupBy` `code`, `metrics` and `filter.properties.code` are
each a `422`.

```json
{"filter": {"implements": "substrate.reamde.dev/core/observed"},
 "aggregate": {"groupBy": ["metric", "source", "about"], "reducers": ["last"]}}
```

### Several metrics in one read

`metrics` names up to 16 metrics read over one `range`, `bucket`,
`groupBy`, `sources` policy, `fill`, `rolling` and `budget`, so a day's
screen is one request. Sleep, resting heart rate, HRV and steps for
2026-09-24 in Athens, each night on the day it ended:

```http
GET /api/v1/records?aggregate=
  {"metrics": [{"ref": "ada.example.com/observations/metric/health/sleep_stage", "reducers": ["sum"], "unit": "h"},
               {"ref": "ada.example.com/observations/metric/health/resting_heart_rate", "from": "summaries", "statistic": "vendor"},
               {"ref": "ada.example.com/observations/metric/health/hrv_rmssd", "from": "summaries", "statistic": "avg"},
               {"ref": "ada.example.com/observations/metric/health/steps", "reducers": ["sum"]}],
   "range": {"gte": "2026-09-24T00:00:00+03:00", "lt": "2026-09-25T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens", "place": "end"},
   "groupBy": ["code"],
   "sources": {"policy": "priority"},
   "reducers": ["last"]}

→ {"units": {"ada.example.com/observations/metric/health/hrv_rmssd": "ms",
             "ada.example.com/observations/metric/health/resting_heart_rate": "/min",
             "ada.example.com/observations/metric/health/sleep_stage": "h",
             "ada.example.com/observations/metric/health/steps": "{steps}"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens", "place": "end"},
   "exact": false,
   "rows": [{"start": "2026-09-24T00:00:00+03:00", "end": "2026-09-25T00:00:00+03:00", "form": "summary",
             "group": {"metric": "ada.example.com/observations/metric/health/hrv_rmssd"}, "from": "summaries",
             "values": [{"reducer": "last", "value": "61", "unit": "ms"}], "count": 1, "bounded": 0, "synthetic": false, "unavailable": false},
            {"start": "2026-09-24T00:00:00+03:00", "end": "2026-09-25T00:00:00+03:00", "form": "summary",
             "group": {"metric": "ada.example.com/observations/metric/health/resting_heart_rate"}, "from": "summaries",
             "values": [{"reducer": "last", "value": "52", "unit": "/min"}], "count": 1, "bounded": 0, "synthetic": false, "unavailable": false},
            {"start": "2026-09-24T00:00:00+03:00", "end": "2026-09-25T00:00:00+03:00", "form": "amount",
             "group": {"metric": "ada.example.com/observations/metric/health/sleep_stage", "code": "deep"}, "from": "raw",
             "values": [{"reducer": "sum", "value": "1.25", "unit": "h"}], "count": 3, "bounded": 0, "synthetic": false, "unavailable": false},
            {"start": "2026-09-24T00:00:00+03:00", "end": "2026-09-25T00:00:00+03:00", "form": "amount",
             "group": {"metric": "ada.example.com/observations/metric/health/steps"}, "from": "raw",
             "values": [{"reducer": "sum", "value": "8412", "unit": "{steps}"}], "count": 96, "bounded": 0, "synthetic": false, "unavailable": false}],
   "sources": ["ada.example.com/observations/source/garmin-forerunner-265", "ada.example.com/observations/source/whoop-4-0"],
   "head": {"changelog": 2171240, "generation": "7f3a0c2e9b1d4e6f"}}
```

The answer above leaves out the sleep stages other than `deep`; each code is
a row of its own.

- An entry names the metric by its record path in `ref`, the unit its values
  answer in (the metric record's `unit` by default), its `reducers` and
  `from` (the read's by default), and `statistic`, the one summary statistic
  its summaries are read at. `statistic` is refused on an entry read from
  `raw`, which reads no summaries. The read's own `unit` is refused beside
  `metrics`, because a unit belongs to one measure.
- The filter and every other option are the read's and apply to every
  metric. `filter.properties.statistic` narrows every metric's summaries
  beside an entry's `statistic`; `filter.properties.code` filters every
  metric's values, so a metric whose values carry no code answers no row
  under it; `filter.properties.metric` narrows too, so a metric it leaves
  out answers none. `minCoverage` applies to the metrics read from
  `prefer-raw` and is refused where none is.
- Every row names its metric in `group.metric`, whatever `groupBy` says,
  and the rows are ordered by bucket, then metric path, then the other
  groups and the code. Each value carries its unit. The response has no
  `metric` and no `unit`: `units` names each metric's, its values' unit or a
  coded metric's durations unit. `from` is the metrics' `from` where they
  all read from the same points, and absent otherwise.
- Each metric's reducers, unit, forms and statistics are checked on their
  own before any rollup row or point is read for a reduction, and a refusal
  names the entry. Where the option is the entry's, the path names it
  (`aggregate.metrics[1].reducers[0]: twa does not apply to an amount`).
  Where the option or budget is the read's, the message opens with the
  entry and its metric (`aggregate.reducers[0]: in aggregate.metrics[1]
  (<metric path>), sum does not apply to a gauge`). Summaries holding two
  statistics are refused at the entry, which names one in its `statistic`.
  A metric named twice is a `422`.
- The budgets hold the metrics together: `maxGroups` counts every metric's
  groups, `maxBuckets` every metric's series times the buckets, and
  `maxSamples` adds up every metric's statements.
- In the recorded-local calendar the rows of one bucket share its `start`
  and `end`: the offsets of the first and last value the read takes in that
  bucket over every metric and group. A client joins the metrics of one day
  on `start` however far the day's points were apart.
- The latest read selects several metrics with `filter.properties.metric`,
  and a downsample reads one metric: `metrics` is refused on both.

### Reducers, by form

Every reducer is checked against the forms the read takes: each form a
stream's points hold inside the range (or the records-mode windows),
including a gauge whose value from before the range still holds into it.
The form of a stream's newest point decides nothing. A reducer the form does
not take is a `422` naming the form: `sum does not apply to a gauge`.

| Form | Reducers |
| --- | --- |
| gauge | `count`, `avg`, `twa`, `min`, `max`, `first`, `last`, `stddev`, `{"percentile": q}`, `{"durationIn": [lo, hi]}`, `circularMean` (an angle stream only) |
| coded gauge | `count`, `first`, `last`, `timeInState` |
| amount | `count`, `sum`, `avg`, `min`, `max`, `first`, `last`, `stddev`, `{"percentile": q}` |
| coded amount | `count`, `sum`, `min`, `max`, grouped by `code` |
| counter | `count`, `first`, `last`, `min`, `max`, `increase`, `rate` |
| summary | `count`, `first`, `last`, `avg`, `min` and `max` over the row's summaries, all of one `statistic`; `sum` where that statistic is `sum` or `count`, `null` otherwise |

- `twa` weighs each value by the time it holds: until the next point of its
  stream, its own `hold`, or `maxGap`, whichever comes first.
- `durationIn` is the time a value in `[lo, hi)` holds; `lo` and `hi` are in
  the values' unit. `timeInState` is the time each code holds: `{"timeInState":
  ["open"]}` names the codes, and the bare word or `[]` answers one value per
  code the bucket holds, each with its `code`.
- `increase` is a counter's change under its contract (decision 0157), the
  step in from the stream's previous reading included, so consecutive buckets
  add up; `rate` is that over the time its steps span, in the unit over `s`.
  Both are `null` in a bucket whose readings have no reading before them.
  A counter's `min` and `max` are in the read's unit; its steps are taken on
  the stored digits.
- `circularMean` averages angles on the circle and answers in `deg`; it has no
  answer when the angles cancel.
- A `{"timeInState": [...]}` with `groupBy: ["code"]` answers each row's own
  code.
- A row reads the summaries of one `statistic`. A group whose summaries hold
  two (a night's `avg` and `max` from one source) is a `422` naming them:
  pick one with `filter.properties.statistic`, or split them with
  `groupBy: ["statistic"]`, which a bucketed read takes only from summaries.
  `avg`, `min` and `max` then reduce that statistic's values whatever it is,
  so the weekly mean of a daily `vendor` resting heart rate is the mean of
  those values (a summary of summaries), and `first`, `last` and `count` read
  that statistic alone. `sum` adds only `sum` and `count` summaries. Over any
  other statistic a read from summaries is a `422`, and under `prefer-raw` a
  bucket that falls to such summaries answers `sum` `null`, so a source's
  `vendor` daily total does not refuse the days its raw points cover.

A row never mixes forms: a group whose streams hold two forms is a `422`
asking to group them apart.

### Units

Each value carries its own `unit`. The values' unit is `unit` where it is a
unit of the metric's measure (the metric record's `measures`, else the
measure of the first stream's stored unit), else the metric record's `unit`,
else the first stream's. A `unit` outside the measure is a `422` naming the
measure, except a duration unit beside `durationIn` or `timeInState`, which
sets the durations' unit (`s` otherwise). `count` has no unit, `stddev` is a
difference (a temperature's has no offset), and `circularMean` is in `deg`.
Every stored value is converted from its own unit, exactly where the factor
has a finite decimal expansion. A value the read takes whose unit does not
convert (another measure, or another annotation: `{beats}/min` and `/min`
count different things) is a `422` naming its unit and, where the read knows
it, its source: a point in the range that the code filter and the source
policy keep, a rollup row, or a gauge's value held into the range. A value
the read drops refuses nothing: one outside the range, one whose `hold` or
`maxGap` ends before the range, a code the filter leaves out, or a source the
policy sets aside. One entry in the wrong measure therefore refuses only the
reads that take it.

### Sources and `from`

- `separate` keeps every source apart: one row per source per bucket, each
  naming its source in `sources`. It splits by source alone: one source's
  channels and `about` values share its row, so WHOOP's day cycle and a
  workout's heart rate reduce together. `groupBy: ["channel"]` (or
  `["about"]`) splits them, the group with no key being the streams without
  one, and the filter's `exists` picks one side.
- `priority` resolves an ordered list (`order` names sources as the filter
  does: a record path, or a bare id completed exactly, so an entry no stream
  holds ranks nothing) per stored interval before any bucket is cut: the first
  source holds where it has a value, the next fills what it does not cover,
  and a lower source's point inside a higher source's hold is dropped. An amount
  or a summary of a lower source is dropped whole where it overlaps a higher
  source's. A total therefore does not depend on the bucket width. Two
  counters are refused under `priority`.
- `summaries` reads summary points alone, from the first source in `order`
  that has one in the bucket.

Where `priority` or `summaries` names no `order`, the read ranks the
selected sources by each source record's `priorities` for the metric's id
([observations](observations.md#the-base-kinds)), 1 first; a source the record does
not rank, or with no record, goes after the ranked ones, and the stream order
(by kind, then source path) breaks ties. An `order` in the request wins
whole, the sources it does not name after those it does. Either way a stream
that names no source (a value typed in by hand) ranks after every named
source, so a scale's weight wins over a typed one in a bucket both hold.

`from: "prefer-raw"` reads raw points where the bucket holds at least one and
they cover at least `minCoverage` of the row (held time for a gauge, the union
of spans for an amount, over the row's own `start` to `end`), and the
summaries otherwise. A bucket with no summary is served from its raw points
whatever they cover, and one with no raw point from its summaries. A gauge's value held in from an earlier bucket, under its
`hold` and `maxGap`, adds held time but is no reading of the bucket: a day
with no reading of its own is served by its summary. A bucket never mixes the
two, and each row's `from` says which served it; the row's `form` is `summary`
where summaries did.

### The response

| Key | Means |
| --- | --- |
| `metric`, `property` | what was read; neither on a several-metric read |
| `unit` | the values' unit (a coded read's durations unit); absent on a several-metric read |
| `units` | on a several-metric read, each metric's unit by its record path |
| `bucket` | the bucket as the read applied it, defaults filled in; a downsample's computed columns |
| `from` | the request's `from`; on a several-metric read, the metrics' `from` where they all read from the same points |
| `exact` | `false` when any value came from a float64 reduction (`sum`, `avg`, `twa`, `stddev`, `rate`, `percentile`, `circularMean`, a rolling value) or a conversion rounded to 20 significant digits. `min` and `max` compare the stored digits exactly and answer the winner's, so they are exact unless their conversion rounds. Also `false` on a metric, downsample or latest read while the background pass re-derives the observed index of a kind the read selects ([observations](observations.md#the-observed-index)): until the pass reaches a record, the rows hold the points its previous declaration derived |
| `rows` | one per bucket and group, ordered by bucket, then group, then code |
| `sources` | every source that fed a row, by record path |
| `head` | `{changelog, generation}`: the changelog position and history generation the read was served at, as a list page's `head` and `generation` |

| Row key | Means |
| --- | --- |
| `start`, `end` | the bucket, each with its own offset |
| `form` | the form of the points that fed the row; absent on a property read |
| `group` | present with `groupBy`: a record path for `source`, `about` and `metric`, the kind for `kind`, the code for `code`, the summaries' statistic for `statistic`; a money property's `currency` |
| `record` | `{ref, version}`, the records mode's matched record |
| `from` | `raw` or `summaries` |
| `sources` | the sources that fed the row |
| `coverage` | an ISO 8601 duration: held time for a gauge, the union of spans for an amount or a summary, the time a counter's steps span |
| `values` | one per reducer in request order (`timeInState` without states, one per code), then the rolling values |
| `count` | the points that fed the row; on the latest read the group's point count |
| `bounded` | the values written with a comparator (a lab's `<0.01`); each is also in every average and sum at its stated number, because a rollup row keeps their count and not their sum |
| `offsetless` | in the recorded-local calendar, the points among `count` written without a UTC offset, placed by `bucket.fallbackTimezone`; absent when none |
| `prorated` | in the records mode, the amounts among `count` the window cut, each counted by the part of its span inside; absent when none |
| `synthetic` | a row `fill` made |
| `unavailable` | a row whose range has expired index points a reducer needs; nothing expires yet, so it is always `false` |
| `nonAdditive` | the records mode: windows may overlap |

A value is `{reducer, value, unit?, code?, at?, rolling?, of?, n?}`. `value`
is a decimal string, or `null` where the reducer has no answer (an average of
no points). The read never rounds for display: an average or a conversion
carries up to 20 significant digits (`72.272608695652173913`), and a client
shows it at the metric record's `displayPrecision` decimal places. A coded
`first` or `last` answers its `code` with `value` `null`.
On a downsample, `at` is the instant a column's `first`, `last`, `min` or
`max` was taken at, in the row's offset; a `min` or `max` served from rollup
rows may leave it out ([downsample](#fill-rolling-and-downsample)). A
rolling value names the reducer it rolls over in `of`, the window in
`rolling`, and in `n` the buckets of the window that held a value.

### Where a bucket is served from

The index keeps 15-minute rollup rows per stream
([observations](observations.md#rollup-rows)), and a group's buckets are
served from them where every reducer merges exactly from rows: `count`, `sum`,
`avg`, `min`, `max`, `first`, `last`, `stddev`, `increase`, `rate`,
`circularMean`, and `twa` and `timeInState` while every row's largest gap is
within `maxGap` (or there is none). Each row is converted from its own unit
before it merges. The rows read are the range's, or in the records mode each
window's, which may reach past the range. A gauge's held time is walked
across the rows, the value current at each window's start read with one
lookup per stream. An amount's or a counter's bucket boundary that cuts a row
(a range starting at 10:07, a records window) is read from raw points and
merged in, and in the records mode an amount is read from raw points alone,
because a window takes the part of an amount that started in a row before
it. Each of those points counts against `maxSamples`, where a rollup row
would count once per quarter hour, so a records-mode read of a dense
amount over many long windows costs more than an `every` or `calendar`
read of the same span. The read finds those amounts by the stream's longest
span among its amounts that meet the windows, one statement before the
forms are found. The index orders a span by its end alone, so that
statement visits every value with a span the selected streams hold from
the first window's start to their newest, and counts each against
`maxSamples`: a read of windows long before a dense stream's newest amounts
may need a larger budget. An amount's coverage is the union of its spans on both paths: the
rows' covered times add where their spans cannot overlap, and where they may
(two overlapping traces, two offsets in one quarter hour) the union is taken
from the raw spans in one statement per group.

Everything else reads raw points: `percentile` and `durationIn`, an amount in
the records mode, a `maxGap` tighter than a row's gaps, `priority` over two
or more streams, a gauge's bucket boundary that cuts a row, a recorded-local
`minute` bucket or an `offset` that is not whole quarter hours, a
recorded-local read of a stream with a point in the range, or a gauge value
held into it, whose UTC offset is not whole quarter hours (a row at `+00:01`
holds points on both sides of local midnight) or was never written (placed
by `fallbackTimezone`), and every summary. Raw points and rollups answer the
same, to float rounding: `internal/observed` holds both.

### `fill`, `rolling` and `downsample`

`fill` makes each group's buckets dense: `null` adds rows whose values are
`null`, `zero` rows of `"0"`, `locf` rows carrying the previous row's values
whole, each with its own `code` (a coded `last` carries the previous code,
a `timeInState` the previous row's states); a coded `first` or `last` that
`null` or `zero` makes answers nothing. Each made row is `synthetic`.

`fill` makes rows only where a series has none. A gauge's bucket holding a
value carried in from before but no point of its own is a real row with
`count` 0: `twa`, `durationIn` and `timeInState` answer the held value
there, the reducers over points (`first`, `last`, `min`, `max`, `avg`,
`stddev`, `percentile`) answer `null`, and `locf` leaves the row as it is.
A series with no row in the range has no shape to fill from and gets none:
a range where a source has no point and holds no value answers no row for
it under any `fill`.

`rolling` then adds, to every row, each rolling reducer over each of its
values across a window of `buckets` buckets: that bucket and the
`buckets - 1` before it, or with `"through": "previous"` the `buckets` before
it, so a baseline leaves the day itself out. Filled rows are in the window,
and a bucket with no row or a `null` value is skipped. Each rolling value
carries `n`, the buckets in its window that held a value, so a window over a
gap says so. A window reaches before the range: the read reduces the buckets
it needs there (counted in `maxBuckets`, with `fill` applied) and answers
none of them, so a bucket's rolling value does not depend on where the range
starts, and the rows a rolling read answers are the rows the same read
answers without it. In the recorded-local calendar a value before the range
counts only in the days before the range's first. A day before the range's
first that the range also reaches (a value at an offset west of the range's)
answers its values in the range alone, and is one place in every window,
holding the whole day: its values from both sides of the range's start. That
day's own window reaches back no further than the read's lookback, and its
`n` says so. `stddev` is the population deviation of the window's values.
`rolling` is refused in the records mode and without a `bucket`.

`downsample={"points": 800, "method": "m4"}` beside an `aggregate` naming a
`metric` and a `range` cuts the range into at most `points` columns, each
answering `first`, `last`, `min` and `max`: the four values a chart needs per
pixel column. Each of the four carries `at`, the instant its point was taken
at, so a chart draws them in time order where they lie in the column; of
equal extremes, the earliest. A column is widened to whole quarter hours
where the range allows, so its rows are whole rollup rows; the response's
`bucket` names the width. A rollup row keeps its extremes and not their
instants, so a column served from rows finds each extreme's point in the
quarter hour holding it, one statement for the read, its points counted
against `maxSamples`. Where those points would pass what the read has left
of `maxSamples`, the read skips that statement: it answers the same values,
and the `min` and `max` served from rows carry no `at`. A dense stream hits
this first: at one point a second a row holds 900 points, so 800 columns
need about 1.4 million. It is refused beside `reducers`, `rolling`, `fill`
and `bucket`.

### The property read

`{"property": {"name": "price", "anchor": "paidAt"}}` reduces a declared
`int`, `float`, `decimal`, `quantity`, `money` or `duration` property of the
filtered kinds with `count`, `sum`, `avg`, `min`, `max` and `stddev`, in
exact arithmetic: money grouped by currency (each row's `group.currency`,
the values' `unit` the currency), a quantity converted into `unit` or the
first value's unit, a duration into `unit`, a duration unit, or `s`.
`stddev` is the population deviation: the variance is exact and its square
root has 20 significant digits, so the read is `exact` only where the root
is. The anchor is `at`, `endsAt`, `dueAt`, `createdAt`, `updatedAt`, a
declared `datetime`, or a declared `date`, which places its record at the
start of its day in the calendar's `timezone` (UTC in the `every` mode and
with no bucket); any other column or property is a `422` naming it. The
records mode is refused.

```json
{"filter": {"kinds": ["ada.example.com/finance/expense"]},
 "aggregate": {"property": {"name": "amount", "anchor": "paidAt"},
               "range": {"gte": "2026-01-01T00:00:00Z", "lt": "2027-01-01T00:00:00Z"},
               "bucket": {"calendar": "month", "timezone": "Europe/Athens"},
               "reducers": ["sum", "count"]}}
```

**The recorded-local calendar.** A record's UTC offset is a property its
kind declares (`health/sleep` and `health/workout` declare `utcOffsetStart`
and `utcOffsetEnd`, minutes east of UTC), so `property.utcOffset` names the
declared `int` property that holds the anchor's offset, and each record
lands on the date its own offset names, as a metric read's point does: a
night that ends at 01:30Z in Los Angeles is on its wake day there. A record
that holds no offset is a `422` naming it, decided before any value is
reduced, unless `bucket.fallbackTimezone` places it by that zone's clock at
its anchor and the row counts it in `offsetless`. Under `property.sources`
only a record the policy keeps is placed, so a dropped record's missing
offset refuses nothing. A row's `start` and `end` are
written at the offsets of its first and last records and clipped to the
range. `utcOffset` is refused outside the recorded-local calendar, and a
`date` anchor, which names its day already, inside it.

**Groups.** `groupBy` takes `kind`. `property.groupBy` names the records'
own declared string, enum or reference properties (`sport`, `source`), and
the read makes a row per bucket and combination of their values, each row
naming them in `group.properties`, a reference as its record path. A record
without one of them is grouped with the others that lack it, and that key
is absent from its row's `properties`. `maxGroups` counts the combinations,
with the kinds and currencies, before a value is read.

**Sources.** `property.sources` is the [records' source
policy](#the-four-time-modes), applied to the records the read reduces
before any value is: a weekly running distance over workouts four devices
each recorded counts each run once. It weighs the records that hold the
property, so a higher-ranked source's workout without a `distance` drops no
other source's. The request's `sources` is the points' policy and is refused
on a property read.

Average sleep per wake day, by week, each night on the local day it ended
and one device's night where several recorded it:

```json
{"filter": {"kinds": ["ada.example.com/health/sleep"], "properties": {"isNap": {"eq": false}}},
 "aggregate": {"property": {"name": "timeAsleep", "anchor": "endsAt", "utcOffset": "utcOffsetEnd",
                            "sources": {"policy": "priority", "property": "source",
                                        "order": ["whoop-4-0", "garmin-forerunner-265"]}},
               "range": {"gte": "2026-09-06T00:00:00+03:00", "lt": "2026-10-04T00:00:00+03:00"},
               "bucket": {"calendar": "week", "weekStart": "sunday"},
               "reducers": ["avg", "stddev", "count"],
               "unit": "h"}}
```

### Budgets

Every budget is checked before the work it bounds, and a request over one is
a `422` naming the limit; a result is never cut short. The range's buckets
and the records mode's records are counted as they are enumerated. The
groups, the code series and series times buckets are checked after the
statement that finds the range's forms and codes, before any point is read
for a reduction. Each statement that reads raw points or rollup rows counts
them first and adds them to the read's `maxSamples` total: in the records
mode, the values with a span visited to find how far amounts reach into the
windows (counted as they are read, and the statement stops one past the
budget); finding the forms
(the points at the range's cut ends, and its summaries), the rollup rows (one
statement for every group they serve; a group sent to raw points after all
gives its rows back), the reduction's raw points, and the raw spans an
amount's coverage is joined from where two rollup rows may overlap, which is
counted once the rows are merged. A rollup row counts as one: there is at most
one per stream, group and quarter hour, so a year of a dense stream is about
35,000. The one point per stream and window a gauge's value is carried in from
counts against no budget. A recorded-local read can meet days its range's
offsets do not name (a point at another offset); those buckets are counted
after the points are evaluated, before a row is made.

| Budget | Bounds | Default | Cap |
| --- | --- | --- | --- |
| `maxBuckets` | series times buckets, a series being a group, or with `groupBy` on `code` a group's code (columns on a downsample, records in the records mode), over every metric of a several-metric read; a `fill` is checked again before it makes a row, and no read answers more rows | 1,440 | 10,000 |
| `maxSamples` | the raw points and rollup rows a read's statements take, each statement's counted before it runs and added up, and in the records mode the values with a span it visits to find how far amounts reach; the records a property read takes, and those outside the range its source policy weighs | 1,000,000 | 20,000,000 |
| `maxGroups` | the groups a `groupBy` or the `separate` policy makes, every metric's together on a several-metric read; a `groupBy` on `code` counts the codes the selected streams' points hold in the range, before a point is read for a reduction; a property read counts its kinds, currencies and `property.groupBy` values before it reads a value | 200 | 2,000 |
| `bucket.records.maxRecords` | the records the records mode matches, a record whose window is empty (no `endsAt`, no `around`) included though it makes no row; with a source policy, also the records outside the range it weighs | 1,000 | 10,000 |

Every statement runs under a timeout inside the caller's remaining deadline,
30 seconds where the caller set none or a later one. The work after the
statements has no timeout of its own: it checks the request's context and
answers the cancellation at the first check after the caller stops. It
checks every thousand points, spans, extents, rows, quarter hours, cover
intervals or filled buckets it copies, walks, selects or reduces, inside each
reducer, an amount's coverage, the `summaries` policy's choice of source and
the `priority` resolution too; between a sort's runs of a thousand elements
and every thousand indexes it writes, merges or moves; and before each row a
rolling window is computed for. Two steps can still take more than constant
work before the next check: a point is placed in every records-mode window
that holds it, and the points of a bucket end that cuts a rollup row are
merged as one row.

### The refusals

- `400`: a misspelled key in `aggregate` or `downsample`; a parameter of
  another mode; `downsample` without `aggregate`; a filter arm a metric read
  cannot test.
- `404`: a kind in `filter.kinds` the repository never declared.
- `422`: two selectors, or a bucketed metric read with neither; a `metric.ref`
  that names no metric; no range or no reducers; `metrics` empty, over 16, naming
  a metric twice, beside the read's `unit`, on the latest read or beside
  `downsample`; an entry's `statistic` where the entry reads `raw`; an
  unknown reducer or a bad
  argument; a reducer the form does not take (naming the form); `circularMean`
  off an angle; a `unit` outside the measure (naming the measure); a value the
  read takes whose unit does not convert; a group of two forms; a
  recorded-local read of a stream holding a value written without a UTC offset
  and no `bucket.fallbackTimezone`, or a `fallbackTimezone` outside the
  recorded-local calendar; `place` `end` over a gauge or a counter, in the
  records mode or on a property read; a group whose summaries hold two
  statistics, or `sum` over a statistic other than `sum` and `count` on a
  read from summaries; `groupBy` `statistic` on a bucketed read that is not
  from summaries; `priority` over two counters; a budget below zero or over its
  cap, or a read over a budget (naming the limit); `rolling` without a
  `bucket`; `downsample` beside `reducers`, `rolling`, `fill` or `bucket`;
  `minCoverage` without `prefer-raw`; on the latest read, `fill`,
  `rolling`, `maxGap`, `minCoverage`, a `from` other than `raw`, a `sources`
  policy other than `separate`, `groupBy` `code`, `unit` without `metric`,
  or `filter.properties.code`; a records source policy that is not `separate` or
  `priority`, a `priority` without `property` or `order`, or a `property`
  or `match` entry the selected kinds do not all declare as a single
  string, enum or reference; a `utcOffsets` property the selected kinds do
  not all declare as an `int`; `sources` on a property read; on a property
  read, a `utcOffset` outside the recorded-local calendar, the
  recorded-local calendar without one, a record holding no offset there
  without `bucket.fallbackTimezone`, or a `property.groupBy` property that is
  not a declared single string, enum or reference.

## The unit table

`GET /api/v1/units` answers the engine's [unit table](units.md), the one a
write holds a quantity to: every measure (`name`, `base`, `scale`, `display`,
`dimension`, `note`) with the codes it lists, each code's `print` symbol and
its exact rational `factor` to the base, an absolute temperature's `offset`,
and the `aliases` a write reads. A code no rational number converts (a level,
an arbitrary unit, `rad`) has no `factor`. The route takes no parameter.

`GET /api/v1/units/convert` converts one value exactly, as `host.units.convert`
does in a function. `value` is exact digits, at most 1000 of them; `from` and
`to` are codes or aliases, percent-encoded in the query (`%5Blb_av%5D` for
`[lb_av]`); and `measure`, optional, names the measure to convert in where the
units' own reading is not meant (`temperature-difference` for a deviation):

```http
GET /api/v1/units/convert?value=72.4&from=kg&to=%5Blb_av%5D
```

```json
{"value": "159.61467782185136844", "unit": "[lb_av]", "measure": "mass", "exact": false}
```

`value` is every digit where the result has a finite decimal expansion, else
20 significant digits with `exact: false`. A missing parameter or an unknown
one is `400 bad_request`; a value, a unit or a pair of units the table refuses
(`kg` to `J`, `mmol/mol` to `%`) is `422 validation` carrying the table's
reason. Neither route reads the repository; the token gates them as it gates
every route under `/api/v1`.

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
REST carries is a lossless view of it: `properties`
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
An agent's `write` tool decodes its `input` through the same strict path.

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
`200`, consistently across `POST /api/v1/records` and `PUT` at the record
path.

## Blobs

A blob is bytes addressed by their SHA-256. `PUT /api/v1/blobs` stores the
request body and answers `201` with the manifest, whose `digest`
(`blob-sha256-<hex>`) is the blob's address; `PUT /api/v1/blobs/{digest}`
does the same and answers `400` when the body does not hash to the digest.
`GET /api/v1/blobs/{digest}` answers `200` with the bytes, the manifest's
media type as `Content-Type` and the digest as `ETag`
([the blob store](operations.md#the-blob-store)).

**A read hashes what it serves.** The server reads the whole blob and checks
its SHA-256 and its size against the digest and the manifest before it sends
the status line, so stored bytes that fail the check are never served. The
read answers `500` with code `internal` and a message naming the digest, and
the server logs one ERROR line, `request failed`, carrying the same message:

```json
{"error": {"code": "internal", "message": "substrate: stored data is corrupt: blobbytes: the stored bytes do not match their digest: blob-sha256-4f2a… hashes to blob-sha256-9c01…"}}
```

It is a `500` and not a `409`: the damage is in the server's store and no
request the client can make repairs it, while `conflict` tells a client to
re-read and retry. [`repository verify`](operations.md#operator-recovery)
lists every damaged blob.

The export (`GET /api/v1/export`) streams blob bytes into its tar after its
`200` is sent. It hashes each blob as it copies and holds back the blob's last
chunk until the digest checks, and does the same for each finished changelog
segment against its `.sha256` sidecar. On a mismatch the server cuts the connection:
the handler aborts with `http.ErrAbortHandler`, which closes an HTTP/1.1
connection before the chunked body's terminator and resets an HTTP/2 stream.
It logs `export aborted mid-stream` naming the digest. The client sees a
failed transfer and an archive with no `snapshot.json`, never a complete
`200`.

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
reach the server. When the trigger declares `source.webhook.signature`, the
server verifies the provider's signature before it stores anything
([functions](functions.md#webhook-signatures)).

| Answer | When |
| --- | --- |
| `202 {"fire": "hook-…"}` | the request is recorded in the repository's changelog and the fire is on its way; a restart resumes it under the same id, which names its `triggerrun` row. A repeat of a signed request the door admitted, inside its timestamp's `maxSkew`, gets the first request's fire id and stores nothing |
| `401 auth` | the trigger declares a key and a signature, the request carries the key, and its signature is missing or wrong, its timestamp is missing or stale, its body is multipart, or the signing secret does not resolve; the body says `webhook signature refused` and nothing is stored |
| `404 not_found` | no such repository, trigger or key, a disabled trigger, a trigger of another source, or a request the signature of a trigger with no key refuses: one answer for all of them |
| `408` | the body did not arrive within two minutes of the headers, or eight minutes for a multipart request: about 560 kbit/s for either bound |
| `413` | a non-multipart body over 8 MiB, a multipart request over 32 MiB, or more than 1 MiB of body or inline part values for a trigger whose callable is an agent |
| `431` | headers over 16 KiB |
| `503` + `Retry-After` | more than eight deliveries in flight, or a repeat of a signed request the door is still recording |

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
| `bad_request`  | 400  | A malformed request, an unknown field, or an unsupported query parameter.                        |
| `validation`   | 422  | An undeclared property, a malformed value, a type mismatch, or a `mustExist` reference naming a record that does not exist — a value in the body, addressed by `problemDetails[].path`. |
| `conflict`     | 409  | A version check failed (`ifVersion`); re-read and retry. Also the accept of a change request whose change no longer applies, whatever the cause ([the patch request sibling](projection.md#the-patch-request-sibling)) — the message says which, and only an `ifVersion` says "version conflict". |
| `parked`       | 409  | A retry of a parked delivery (`POST …/parked/{failureId}/retry`) ran and failed again: the row stays parked one attempt older, and the message names the new error's first line ([functions](functions.md#triggers)). |
| `guard`        | 403  | A refused state transition, or a protected operation (a subject reference, a kind with live records). |
| `lossy`        | 403  | A declaration change would remove values from the fold, or a re-import would replace a sample copy edited since it was imported, and no confirmation for that plan came with it; preview the plan and confirm it ([bundles](bundles.md#install-and-lifecycle)). |
| `forbidden`    | 403  | The caller may not do this at all.                                                               |
| `auth`         | 401  | Missing, invalid, or expired token, a refused login, or a webhook signature that a keyed trigger declares and that a request carrying the key fails; a trigger with no key answers that refusal `404` ([webhooks](#webhooks)). |
| `not_found`    | 404  | No such record ADDRESSED by the request; a former id is not this, it resolves ([merges](projection.md#merges)). A referent a write's body names is `validation`, not this. |
| `rate_limited` | 429  | Slow down; the response carries `Retry-After`.                                                   |

`bad_request` is also the code on a `405` (a wrong method at an `/api` path)
and on the `408`, `413` and `431` a webhook request receives when its body
is too slow, or past its body or header cap ([webhooks](#webhooks)).

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
index is never mistaken for an empty match. A repository that refuses writes
until the server restarts (its directory fell behind its tables and the
server could not repair it while running) also answers `unavailable`, with a
30-second `Retry-After` and a message that names the restart.

The same problem object appears in the [watch stream](changelog.md)'s terminal
error frame and as an agent tool's error, so an error means the same thing
wherever it surfaces. An unmatched path under an API prefix is that same
object with `404 not_found` — never the console's HTML with a 200.

One more code answers a cursor from a history the server no longer holds:
`compacted` (410) answers a `from=` or `before=` the changelog cannot resume,
below the retention [horizon](changelog.md#frames-and-the-horizon), above the
head, or under a history generation the server does not hold, and a list
`after=` cursor minted under another generation. Its problem object names the
current `head` and `generation`, telling a consumer that has fallen too far
behind, or resumes after a restore, to re-list rather than silently miss rows.

Every code a request can receive is above; those thirteen strings are the whole
closed set, and nothing else appears in `error.code`. The
[policy door](agents.md#the-policy-door)'s `gate` verdict is not one of them: it
holds an agent's write for the owner's review and surfaces as a tool result
inside the agent loop, never as an HTTP response
([0037](decisions/0037-gated-is-an-agent-loop-verdict-not-a-wire-code.md)).

Next: [users and tokens](auth.md), the way in and what a token is.
