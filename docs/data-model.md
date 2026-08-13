# The data model

Everything a user has lives in one **repository**, and everything in a
repository is a **record**. A task, a person, a token, a kind declaration:
all records, all reading and writing as one shape. This page is that shape and
the rules around it: the changelog underneath, identity, the envelope, kinds and
references, what a property can hold, traits, and the validation every write
passes.

## The repository and its changelog

Registering creates a user and, in the same transaction, that user's one
repository. A repository is:

- an **append-only changelog**: every change, in one strictly sequential order, with
  values in the payload;
- the **records**: the fold of that changelog, which is what reads and queries
  answer from;
- two side stores: **blobs** (content-addressed bytes) and **sealed** (secret
  material, wrapped under the substrate's key).

**The changelog is the truth and the records are its fold.** One piece of code turns
the first into the second, so a live write and a full rebuild take the same
path and cannot drift. `substratectl repository rebuild` clears the fold and replays
the whole changelog into it, reproducing the records bit for bit and appending
nothing — a required, tested path, not a recovery hope.

The changelog does not carry the side stores' bytes, so **a backup is changelog plus blobs
plus sealed, as one unit**. That sentence is also the definition of what a
repository contains.

Sequence numbers are per repository, gapless, and assigned at commit, so
commit-visibility order **is** sequence order and a consumer resuming from a
remembered seq misses nothing. Nothing prunes or compacts today.
[The changelog and watch](changelog.md) is the consumer's side of this.

The changelog is unsigned: no hashes, no chain, no signatures. It is trusted
storage, not evidence.

## Records and identity

A **record** is one typed thing. Its identity is the pair `(kind, id)`. An id
is unique per kind, never per repository, so a bare id names nothing: a person
and a task may both be `9f2k` and stay unrelated. **Always address a record by
its full identity, on every surface.**

Ids the server mints are 12 characters of lowercase base32. A writer may
supply its own id on create, which is how an integration composes a stable id
out of a provider's own key; supplied ids allow a wider character set (RFC 3986
unreserved plus `:`, `@` and `/`, up to 128 characters). Ids are never derived
from content and never reused.

Three more words, used precisely on every page:

- A **document** is one serialized YAML item, `---`-separated when there are
  several.
- The **envelope** is the four-key document shape every record serializes to:
  `kind`, `metadata`, `data`, `status`.
- A **manifest** is an envelope-shaped document written to be applied
  declaratively: a kind declaration in git, an
  [bundle](bundles.md)'s install closure, anything `apply -f` takes.

The envelope is the YAML form. It is what [`substratectl get -o yaml`](substratectl.md)
emits and what `apply` and the batch vocabulary apply consume.
[REST](api.md) and [GraphQL](graphql-and-search.md) reads return the record
flat, as one JSON object, not wrapped in an envelope.

## The envelope

Here is a task from the to-do list:

```yaml
kind: tasks.substrate.geoah.me/task
metadata:
  id: t9                          # omit on create; the server assigns one
  labels:                         # short, queryable metadata
    owner/starred: true
data:
  properties:
    title: Buy milk
    description: the oat kind
    status: open                  # a state property, see Validation below
    dueAt: 2026-08-13T09:00:00Z
  edges:
    - rel: project
      to:
        kind: tasks.substrate.geoah.me/project
        id: infra7
status:                           # server-set, ignored on input
  version: 4
  createdAt: "2026-08-04T09:00:00Z"
  updatedAt: "2026-08-04T09:12:00Z"
```

Three rules make the envelope predictable:

- **`kind` is the record's kind reference**, and it is the only place the kind
  appears. Nothing splits it into parts.
- **`data` carries two keys and no others**, `properties` and `edges`.
  Everything authored, `title` and the temporal properties included, lives in
  `data.properties`, so nothing needs a reserved list to name a property.
- **`status` is server-set and ignored on input**, so a document you read,
  edit, and re-apply means exactly what it looks like: the block you carried
  along is dropped rather than fought over.

A property is one key under `data.properties`. An edge is one entry under
`data.edges`, naming its `rel` and its target record. Edge targets are
written `{kind, id}`, the record reference split into its two parts. A bare
`{id}` is accepted as shorthand only where the declaration already fixes the
target kind: the reference resolves within that one kind and nowhere else,
because ids are unique per kind. A polymorphic edge (`to: any`) always requires
the full form. Declarations may name their target by bare kind name too
(`to: project`): a bare name resolves in the declaring authority first, then
uniquely across all authorities, and a name that stays ambiguous refuses to
load.

The envelope is the one canonical representation. The flat JSON that the
[API](api.md#the-canonical-envelope) returns is a lossless view of it: the
same `properties` and `edges` under `data`, the same server-set fields under
`status`. Edges are a **list** in the canonical envelope, one entry per target;
the flat read form groups them by `rel` into a map for convenience, and
the two convert without loss. Because the mapping round-trips, a document you
read applies back unchanged, which is what makes `substratectl get -o yaml | substratectl
apply` a no-op on a record that has not changed.

## Labels and annotations

Opinions about a record never get welded into it. They layer on under
`metadata`, in two forms:

- **Labels**: short scalar values under a namespaced key
  (`owner/starred: true`). Indexed, filterable like a property.
- **Annotations**: arbitrary JSON under the same key convention. Fetched
  with the record, never filtered on.

The rule of thumb is mechanical: filter on it, label; blob, annotation.
Writers may only touch their own key namespace.

## Kinds and references

A **kind** is what a record is, and it is named one of two ways:

- **bare** — `task` — when the kind belongs to this repository alone;
- **authority-qualified** — `tasks.substrate.geoah.me/task` — when an **authority**
  publishes it.

An authority is a DNS name. It says who publishes a kind and, more
importantly, who may change that kind's declaration: shipped vocabulary is the
substrate's to write, your own bare kinds are yours, and an installed
bundle's kinds belong to that bundle. The two forms cannot collide,
because they are different shapes, and within one repository a bare name is
unique.

A **record reference** writes kind and id together:
`tasks.substrate.geoah.me/task/t9` for a qualified kind, `task/t9` for a bare one. That
is the string form; on REST the same reference is split into path segments
(`/api/v1/tasks.substrate.geoah.me/tasks/t9`), and on GraphQL it travels as two
arguments.

The shipped vocabulary is split by subsystem, Kubernetes-style, each subsystem
its own authority: `people.substrate.geoah.me`, `messaging.substrate.geoah.me`,
`calendar.substrate.geoah.me`, `tasks.substrate.geoah.me`, `media.substrate.geoah.me` — each a bundle you
IMPORT — and `core.substrate.reamde.dev` for the substrate's own machinery, which is the
only one a new repository is seeded with. Authorities namespace names; they
never partition the data: an edge crosses authorities as easily as it stays
inside one.

A **kind declaration is itself a record**, living in the repository's own changelog
like everything else, whatever authority it declares into. Your repository was
seeded with `core.substrate.reamde.dev` when it was created, and everything else — the
vocabulary above included — arrived as an import you asked for; either way the
declarations are rows in your repository, not a file the server reads at query
time. So "what
does the vocabulary say" is a query, not a file read.
[Vocabulary as records](vocabulary.md) is that whole story.

The to-do list needs two kinds. **`people.substrate.geoah.me/person` ships built in**,
one record per human, the target of every "a person" edge in the system:

```yaml
kind: people.substrate.geoah.me/person
metadata:
  id: 9f2k                        # server-assigned: nothing external names a human
data:
  properties:
    name: Ada Lovelace
    emails:
      - ada@example.com
```

The task kind we declare ourselves. Kinds are manifests: YAML documents,
versioned and reviewed in git (or installed by an
[bundle](bundles.md)). A declaration wears the same envelope as data,
and its `kind` is always a core kind — the meta-model lives in
`core.substrate.reamde.dev` whatever authority the document declares into:

```yaml
kind: core.substrate.reamde.dev/kind   # kind declarations are core records
metadata:
  id: tasks.substrate.geoah.me/task        # = <data.authority>/<data.names.singular>
data:
  authority: tasks.substrate.geoah.me      # the authority being declared into
  names:
    singular: task
    plural: tasks
  properties:
    description:
      type: markdown
    url:
      type: url
    dueAt:
      type: datetime
    status:
      type: state                 # a state machine, declared in place
      states:
        - proposed
        - open
        - done
        - dropped
      initial: open
      transitions:
        - from: proposed
          to: open
        - from: proposed
          to: dropped
        - from: open
          to: done
          stamps:
            completedAt: now
        - from: done
          to: open
  edges:
    project:
      to: project
    source:
      to: any                     # the message, mail, or issue the task came from
```

A declaration record's id **is** a kind reference, which is the one place a
`/` and a dot are legal in an id. Drop the `authority:` line and the two
qualified names, and the same document declares a bare `task` kind that is
yours alone.

Every property here names a declared property type (`markdown`, `url`,
`datetime`, `state`), covered next.

(The shipped task kind declares `dueAt` in one line through a shared
[trait](#traits) instead of the plain `datetime` shown here.)

## Property types

Every property declares a type. The type decides two things: what a write
must look like to be accepted, and which filter operators the query grammar
offers for it. Nothing else about a property is special; `title` and
`description` are declared and written identically.

| Property type      | Validation / meaning                                        | Filter operators |
| ------------------ | ----------------------------------------------------------- | ---------------- |
| `string`           | short, single-line                                          | eq, prefix, in   |
| `text`             | long-form prose                                             | (full-text only) |
| `markdown`         | `text` renderers treat as Markdown                          | (full-text only) |
| `int`, `float`     | numbers, optional `min`/`max`                               | eq, range        |
| `bool`             | true/false                                                  | eq               |
| `datetime`, `date` | RFC 3339 instants / civil dates                             | range            |
| `duration`         | e.g. `47m12s`                                               | range            |
| `email`            | refined `string`, RFC 5322 mailbox                          | eq               |
| `url`              | refined `string`, absolute URL                              | eq, prefix       |
| `phone`            | refined `string`, E.164 normalized                          | eq               |
| `timezone`         | IANA zone name                                              | eq               |
| `recurrence`       | RFC 5545 RRULE string                                       | eq               |
| `enum`             | one of declared `values`                                    | eq, in           |
| `state`            | a state machine: `states`, `initial`, `transitions`, stamps | eq, in           |
| `secret`           | a credential: written like a string, read back redacted, stored in the sealed store | (none) |
| `digest`           | a server-minted SHA-256 comparator, redacted like a secret  | (none)           |
| `blobref`          | names stored bytes by digest                                | (none)           |
| `object`           | inline `fields:` of scalar types, one level                 | (none)           |
| `reference`        | a typed pointer at another record: `{kind, id}`             | (none)           |
| `json`             | escape hatch: schemaless blob, never filtered               | (none)           |

Our to-do list already uses four: the task's `description` is `markdown`,
its `url` is a `url`, `dueAt` is a `datetime`, and `status` is a `state`.
Every property may also be declared `repeated: true`, which holds a list of
its type and filters with `contains`. The built-in `person` uses it for
addresses:

```yaml
emails:
  type: email
  repeated: true
```

**Enums.** An `enum` accepts one of a declared list of `values`. Each value
is either a bare string or a `{value, label}` pair, where the optional label
is what a client renders in a picker while the value is what is stored and
filtered:

```yaml
kind:
  type: enum
  values:
    - value: direct
      label: Direct message
    - value: group
      label: Group chat
    - value: channel
      label: Channel
```

Validation is on the value alone, so a bare-string list stays valid, and an
empty label leaves the client to humanize the value. Declaration order is
render order.

**Secrets.** A `secret` property stores a credential. Writes take a string
like any other property, but the material never lands in the record: the
engine moves it into the sealed store (encrypted, AES-256-GCM under the
deployment's credential key) and the record and the changelog carry only an
opaque ref, so rotation deletes the old material instead of retiring it into
an immutable log. Every read, whatever the surface, returns the sentinel
`<redacted>`. Applying a document carrying the sentinel back leaves the
stored value alone, so a read-edit-apply round trip never wipes a
credential. A secret offers no filter operators and cannot be ordered by
(comparing against a redacted value would reconstruct it one probe at a
time), never indexes into [search](graphql-and-search.md#search), never
renders into a title, and a [record mapping](projection.md) may never read
one: a secret never leaves its record.

A `digest` shares the redaction but not the indirection, because the engine
itself must compare it in SQL: a one-way SHA-256 the server minted, stored
as the value. The `core.substrate.reamde.dev/token` kind stores its hash
this way ([users and tokens](auth.md)).

**Objects.** An `object` property declares its fields inline, each a scalar
type, one level deep, no object inside an object. This is how an
integration's kinds mirror what their provider actually sends. In the GitHub
[integration](bundles-catalog.md#github), issues carry milestones in
GitHub's own shape:

```yaml
milestone:
  type: object
  fields:
    name: string
    number: int
    state: string
    dueOn: datetime
```

`repeated: true` over an object holds a list of objects. Object properties
validate recursively on write and stay out of the filter grammar until a
consumer needs them.

**References.** A `reference` is a typed pointer stored as a property value:
the same `{kind, id}` pair an edge target wears, but data, not a graph edge.
Reach for it where a manifest field needs to NAME another record, like a
trigger's `callable`:

```yaml
callable:
  type: reference
  to: any
```

An optional `to:` pins the referent kind, exactly like an edge's `to:`;
`to: any` (and an absent `to:`) leaves it unconstrained, and then the value
must carry an explicit kind. A value is `{kind, id}`, and a bare id string is
accepted only when `to:` pins a concrete kind. Validation checks the shape and
that the referent KIND exists; the referent RECORD need not exist at write
time, because a reference is a pointer, not an edge. `repeated: true` holds a
list of references. The [console](console.md) renders a reference as a link
to the referent's detail page.

A reference is not an edge. An edge is a traversable relationship with its own
[incoming views](api.md#rest-resources) and its part in
[record mapping](projection.md) subject resolution; a reference is an inert
value you read and rewrite like any other property. Point at a record as a
relationship with an edge; name one as data with a reference.

**Blob references.** A `blobref` names stored bytes by their digest. The bytes
live in the repository's content-addressed blob store
(`PUT /api/v1/blobs`, `GET /api/v1/blobs/{digest}`), and their metadata is an
ordinary `core.substrate.reamde.dev/blob` record whose id **is** the digest, so the same
bytes always mint the same blob. A read resolves the ref to
`{digest, name, mimeType, size, status}`, never to the bytes inline.

A blob's `name` and `mimeType` are both **optional and descriptive**. The
upload says them — the name as `?name=` or a `Content-Disposition` filename,
the type as the `Content-Type` header — and neither takes part in dedup,
because the digest is the identity: the same bytes uploaded again under
another name are the same blob, still carrying the first name. A name is a
filename, never a path; one with a separator in it is refused. The read hands
both back, as `Content-Type` and a `Content-Disposition` filename.

**Your own property types.** A custom property type is a **refinement** of a
base type plus validations, declared as a `propertytype` manifest and local to
its authority. The media authority defines one for ISBNs:

```yaml
kind: core.substrate.reamde.dev/propertytype
metadata:
  id: media.substrate.geoah.me/isbn
data:
  authority: media.substrate.geoah.me
  description: "ISBN-10 or ISBN-13, normalized and hyphen-free"
  base: string
  pattern: "^(97[89])?[0-9]{9}[0-9X]$"
```

Because the declaration is a record, "what does `isbn` validate" is a
query, not a file read.

**Search coverage.** Full-text search covers the title and every string-family
property by default; a property may additionally declare `embed: true` to opt
into the semantic pipeline. Both are covered in
[GraphQL and search](graphql-and-search.md#search).

## Traits

Some properties mean the same thing on every kind that carries them:
"when does this sit on the timeline" is one question whether the record is a
calendar event, an email, or a task. A **trait** declares such a set of
properties once, as a `trait` manifest, and any kind binds it with one line.
Binding gives the kind the trait's properties, their indexes, and a shared
GraphQL interface.

The one worked example is `temporal`, shipped in core:

```yaml
kind: core.substrate.reamde.dev/trait
metadata:
  id: core.substrate.reamde.dev/temporal
data:
  authority: core.substrate.reamde.dev
  oneOf:
    point:
      at: datetime
    range:
      at: datetime
      endsAt: datetime
```

`oneOf` declares two variants: a `point` in time, or a `range` with an end.
A calendar event spans time, so its kind binds the range variant under
`traits:`:

```yaml
traits:
  - temporal(range)
```

and with that one line the kind carries `at` and `endsAt`, indexed, and joins
the `Temporal` GraphQL interface, so "everything on the timeline this week,
whatever its kind" is one query.

A binding may also rename where the trait's property lands. A task's moment
on the timeline is its due date, so the shipped task kind binds:

```yaml
traits:
  - "temporal(point: dueAt)"
```

which is the point variant with its `at` property carried under the name
`dueAt`. This is how the `dueAt` shown earlier as a plain `datetime` is
really declared: one line instead of a property block, and the task still
answers every `Temporal` query. (Temporal properties are the substrate's one
"hot" trait: they map onto dedicated storage columns, which is why the trait
lives in core.)

**Bundle traits.** A few traits are more than shared properties: the host
recognizes them by identity and builds behavior on top. These are how an
[bundle](bundles.md) declares the pieces the substrate's OAuth
facility and lifecycle machinery need to see. Two ship in core:

- **`accountconfig`** is a **Connection** kind. Binding it gives the kind
  `tokenRef` (a secret), `tokenStatus`, and a repeated `grantedScopes`, the
  properties the OAuth facility owns and writes as a provider account connects
  and syncs.
- **`oauth2`** carries the OAuth client credentials, `clientId` and a
  secret-typed `clientSecret`, for a bundle that speaks OAuth.

Binding one is the same single line as `temporal`, without a variant. A
provider account kind declares:

```yaml
traits:
  - accountconfig
```

Because implementing a trait is queryable, a client can page every record of
a trait (`GET …/core.substrate.reamde.dev/traits/{id}/records`), which is what the
console's connections view over `accountconfig` accounts is.
[Bundles](bundles.md) puts these three interfaces to work.

## Validation and state machines

The substrate has no verb endpoints: no "complete", no "accept", no "close".
Every behavior is a declaration checked on write.

Properties are validated on every write against the kind's declaration, and
unknown ones are rejected: a write naming a property the kind does not declare
fails whole, with the error naming the property. Whatever a property's type,
it is filterable exactly as declared (**filterable, indexed, and declared are
the same set**), so you cannot write a slow query, only extend the vocabulary.
(Vocabulary documents pass through admission of their own;
[vocabulary as records](vocabulary.md) covers that side.)

A `patch` removes a property by naming it with a `null` value, which is why a
stored property never holds `null`: a null always means delete. A property's
value replaces whole, and the [API page](api.md#the-canonical-envelope) pins
the rest of the patch rules (merge depth, status codes, strict decoding).

A `state` property declares a state machine in place: its `states`, its
`initial` state, and its `transitions`, as the task's `status` above shows.
The state property is the entire behavioral seam. Completing a task is a
`patch` setting `status: done`; the declaration checks the transition is
legal and stamps `completedAt` for you. Illegal moves are rejected, so a
state can never be corrupted by an eager writer. The rules, each one
mechanical:

- **Creations are born in the declared `initial` state.** A creating write
  may name any declared state instead (an integration mirroring a provider's
  already-done item starts it there); an undeclared state name is refused.
- **Transitions travel only as `patch`.** A `put` that would move a state is
  refused ("patch does transitions"), so re-applying a document you read can
  never accidentally complete a task.
- **A patch naming the current state is a no-op**, never an illegal
  transition. This is what lets a [function](functions.md) re-assert `done`
  on every delivery without churning: the re-assertion appends no changelog entry.
- **Stamps record the moment of a transition.** `completedAt: now` writes the
  transition time into a datetime property that the stamp itself declares; a
  stamp name may not collide with a declared property.
- **Transitions carry no guards**: any actor may perform any declared
  transition. What a transition may additionally do is declared too: an
  `onEnter` effect runs in the same transaction, which is how
  [accepting a merge request](projection.md#merge-requests) performs the
  merge.
- **States are never recomputed.** A state moves through its declared
  transitions or not at all, so no amount of [syncing](projection.md) can
  quietly complete a task.
- **A state cannot be removed out from under its records.** Re-declaring the
  machine without a state some record still occupies is refused with the
  count, like every narrowing vocabulary change
  ([vocabulary evolution](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)).

## The server-owned status

The envelope's `status` block is server-set and ignored on input:
`version`, `createdAt`, `updatedAt`, `deletedAt` and `finalizers` on a
tombstone, `formerIds` after a merge, and per-property provenance
([managed properties](projection.md#managed-properties)). Two consequences:

- A document you `get`, edit, and `apply` back means exactly what it looks
  like; the `status` you carried along is ignored.
- **Optimistic concurrency** is one field: a write may assert
  `metadata.ifVersion` with the version it read, and a mismatch is a
  conflict, so the caller re-reads and retries instead of overwriting a write
  it never saw.

Refused writes come back as one error shape with one code each: `validation`
for a rejected property, `conflict` for a version mismatch, `guard` for an
illegal transition. [The API](api.md#errors) has the full table.

Next: [vocabulary as records](vocabulary.md), how these declarations reach the
repository and evolve without breaking the data underneath them.
