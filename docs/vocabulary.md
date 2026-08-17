# Vocabulary as records

Vocabulary is data. Every vocabulary element is a manifest, a document in the same
[envelope](data-model.md#the-envelope) as everything else, and every
declaration is an ordinary record in the repository's own changelog, readable
through the same collections as data. "What does the vocabulary say" is a query,
not a file read.

## The declarable kinds

Nine kinds declare everything:

| Kind                           | Declares                                              | Taught on                                         |
| ------------------------------ | ----------------------------------------------------- | ------------------------------------------------- |
| `core.substrate.reamde.dev/authority`     | one authority: the DNS name that publishes kinds      | [Data model](data-model.md#kinds-and-references) |
| `core.substrate.reamde.dev/kind`          | one kind: its properties and edges                    | [Data model](data-model.md#kinds-and-references) |
| `core.substrate.reamde.dev/propertytype`  | one custom property type: a refinement of a base type | [Data model](data-model.md#property-types)       |
| `core.substrate.reamde.dev/trait`         | one trait, bound by kinds                             | [Data model](data-model.md#traits)               |
| `core.substrate.reamde.dev/recordmapping` | how a source record's properties reach its subject    | [Projection](projection.md)                      |
| `core.substrate.reamde.dev/function`      | one pure callable in Python or Go                     | [Functions](functions.md)                        |
| `core.substrate.reamde.dev/agent`         | one callable whose body is an LLM loop                | [Agents](agents.md)                              |
| `core.substrate.reamde.dev/bundle`        | one bundle: the closure it installs as a unit      | [Bundles](bundles.md)                      |
| `core.substrate.reamde.dev/actor`         | one name writes are attributed to                     | [The API](api.md#actors)                         |

A manifest wears the same four-key [envelope](data-model.md#the-envelope) as
any record. Its `kind:` is one of the nine above, always a core kind whatever
authority the document declares into, and any other envelope key is a load
error.

A declaration's id is its declared name. For a `kind` that name is the
kind reference, and it must equal `<data.authority>/<data.names.singular>`: a
manifest that spells the two differently is a load error, never a silent
rename. A `propertytype`, `trait`, `recordmapping`, `function` and `agent`
take the same `<authority>/<name>` form, and a `bundle`'s name is its
authority's leading label (`google.bundles.substrate.reamde.dev/google`). An `authority`
document's id is the DNS name itself, and an `actor`'s id is a bare word. So
the smallest manifest, an authority, is five lines:

```yaml
kind: core.substrate.reamde.dev/authority
metadata:
  id: tasks.substrate.reamde.dev
data:
  version: 1
```

Each declarable kind has a collection under `core.substrate.reamde.dev`, the name of
its name, and declarations list and read there like any other record. A
declaration's id is the one id form that carries a `/`, which is legal in a
URI path segment only percent-encoded, so a REST path spells it `%2F` and the
API decodes it once:

```http
GET /api/v1/core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev%2Ftask
```

## How the vocabulary reaches a repository

The binary's embedded tree is a seed, not an authority. A declaration is a
record in the repository's own changelog, and there are exactly three ways one gets
there, each an ordinary changelog entry attributed to the hand that wrote it and
auditable in the [changelog](changelog.md):

- **The seed, once, at creation.** Creating a repository writes the binary's
  embedded tree into the new repository's changelog as ordinary record entries,
  under the actor `bundle:core`, in the same transaction as the repository
  itself. After that the tree has no standing over that repository: nothing
  re-projects it at open, and nothing is ever pruned. A shipped authority the
  tree stops declaring stays in every repository that already holds it.
- **The boot-time upgrade, at the first open under a new binary.** Every
  declaration carries a `version`, the declaring authority's unless the
  declaration overrides it. The first open of a repository in a process diffs
  the binary's shipped declarations against the stored ones and appends the
  difference as explicit entries under the actor `substrate`: one transaction
  per repository, convergent and idempotent, so an unchanged tree writes
  nothing at all. Only same-or-newer wins, never a downgrade and never a
  prune, and a repository nobody opens is never touched. A version is an
  incremental integer, ordered as plain integers; 0 is the absent version
  and orders below everything. An authority whose stored rows belong to
  somebody else here is skipped whole: the upgrade never seizes a name it
  does not already own.
- **An install, which is a copy.** Installing a bundle writes that
  bundle's manifests into the repository's changelog under `bundle:<name>`
  ([Bundles](bundles.md)). The shipped catalog is a source, never an
  authority, and nothing on the serving path reads it.

Install and apply are one path. `POST /api/v1/-/vocabulary/apply` with
`{"documents": […]}` is the batch verb, the same closure an install applies,
and where `substratectl apply` routes any vocabulary documents it is given. A generic
PUT, PATCH, or DELETE of a vocabulary record is a batch of one. On every one
of these doors the engine maintains the `version` itself: an incoming value is
honored only when it moves past the stored one, a changed definition lands at
stored+1, an unchanged one keeps its stored version, and a changed or deleted
declaration that cannot carry a version of its own (a trait, a function)
moves its authority's forward instead, so nobody bumps by hand.

**The authority chokepoint** decides who may write a declaration, which is
what an authority is for. Shipped vocabulary, an authority whose stored rows
say `source: builtin`, is writable only through a substrate path: the seed,
an upgrade, an install, which is what the actors `substrate` and
`bundle:<name>` name. A generic API write into one is `forbidden`, and
neither actor form can be claimed by a request. Everything else, the
repository's own bare kinds and the bundles it installed, is the user's to
write.

Opening a repository rebuilds its registry from the stored declaration
records, shipped and installed alike, each carrying the source its rows
record. Nothing consults the embedded tree, and a repository whose rows hold
no vocabulary at all refuses to open rather than serving a substrate in which
nothing resolves.

## Admission

An apply, a generic vocabulary write, and an install all pass the same
admission: **the loader is the validator.** Not a separate validation package, not a
webhook: the rules live where the manifest is parsed. A batch is **one
transaction, every document admitted or none** (a refusal carries the full
problem list), and a committed batch is active immediately, no restart
anywhere. A candidate registry is built and compiled whole, closure
resolution and CEL guards and templates and the GraphQL-name uniqueness check
included, before the write transaction opens, so a broken closure fails the
batch rather than half-loading. One per-repository mutex serializes vocabulary
writes against each other; data writes never take it and never wait.

The loader's rules are hard errors, never warnings. The load-bearing ones:

- **Casing is one rule.** Every declared name and system key is camelCase
  with initialisms uppercase (`displayTemplate`, `oneOf`, `ifVersion`,
  `onEnter`, `endsAt`). Snake spellings are errors, not aliases. Kind
  a kind name stays `[a-z][a-z0-9]*`; an authority stays a dotted
  lowercase DNS name; an actor is a bare word or a prefixed machine hand,
  never a DNS name; enum and state values stay lowercase words.
- **Reserved property names.** `title`, `body`, `at`, `endsAt`, `dueAt` are
  the five properties every record already carries, each with its own storage
  column; redeclaring one is a load error naming the built-in. The temporal
  three arrive through the `temporal` trait. The built-in `title` is not a
  kind's display storage either: a kind that has a heading declares its own
  property for it (`name`, `summary`, `subject`) and renders the title with a
  `displayTemplate`, which is what the engine writes into the column. On a
  kind that declares one, a written `title` is IGNORED rather than refused, so
  a writer that means the heading writes the declared property
  ([decision record 0016](decisions/0016-a-kind-titles-itself-from-a-declared-property.md)).
- **Unknown keys anywhere in `data` are refused**, so a typo cannot be
  silently ignored.
- **Mapping constraints**: at most one `recordmapping` per `from` kind; its
  `edge` must be declared on the from-kind, `required`, not `many`, not
  `ownerRef`, and its `to` matches the mapping's `to`; a mapping's `to` kind
  may not itself be any mapping's `from` (bipartite, one level). Every `map`
  path type-checks against both declared kinds at load, so a disagreement
  fails on the manifest that caused it, never on the first sync that hits it.
- **States**: `type: state` requires `states` and `transitions`; `initial` is
  a single declared state; every `from`/`to` is a declared state. A stamp
  target declared in `properties` must be a single-valued `datetime`; one
  left undeclared is auto-declared as that, so declarations written before
  targets were declarable keep loading. Transitions carry no guard.
- **Enum values are an ordered list**, each entry either a bare value
  (`values: [off, hourly, daily]`) or a `{value, label}` mapping. Declaration
  order is render order, and validation reads the value alone.

Three guardrails worth knowing:

- **Deleting a kind with live records is refused**, with the count, inside
  the same transaction. Cascade is never a default, and kind references are
  never reused: history orphans by design and stays readable.
- **Narrowing a kind that has live records is refused**, with the count. The
  next section is the contract.
- **Shipped vocabulary refuses a generic API write**; the repository's own
  kinds and its installed bundles are what change through the API.

The seed, the boot-time upgrade and the open-time rebuild are not admission:
a shipped change lands with the binary, and quarantine is the backstop for a
stored closure the binary no longer accepts.

## Vocabulary evolution and the dialect contract

The vocabulary changes over the life of a repository, and v1 fixes how, so a binary
upgrade or a bundle upgrade can never silently corrupt data already written
against the old shape. The contract has two halves.

**A per-repository vocabulary dialect.** Each repository carries a monotonic
vocabulary-dialect integer, stamped by the binary when the repository is opened.
Dialect promotions are keyed, recorded, ordered steps from N to N+1, run at
open and recorded per repository, so the history of how a repository's vocabulary
advanced is itself readable. A step that rewrites stored rows stamps the new
number in the same transaction as the rewrite, which is what makes a promotion
**one-way**: [upgrading the binary](operations.md#upgrading-the-binary) is where
that lands on an operator. A binary whose maximum supported dialect is
below a repository's stored dialect refuses to open that repository with a
named error ("the store speaks a newer schema dialect than this binary"),
rather than opening it and misreading rows written by a newer shape; the API
surfaces the refusal as `503 repository temporarily unavailable`, never as an
invalid token. A repository's stored dialect is internal to its own store and
never appears on the wire; what
[API discovery](api.md#discovery) reports is the binary's maximum, which is
the number a client actually needs.

**Admission refuses narrowing.** A declaration change that would strand
existing data is refused at admission, as a `guard` error naming every
narrowed property with the count of live records affected, all problems at
once. The narrowing diffs that refuse:

- dropping a property a record still carries
- renaming a property (`renamedFrom:`, below)
- changing a property's datatype, or its container: a `repeated:` flip and a
  `keyed:` flip both count, because a map is not a list and neither is a scalar,
  and no stored value converts between them
- removing an enum value a record still holds
- removing a state a record still occupies
- narrowing a `reference` property's `kind:` pin while records point outside
  the new one (unconstrained to a kind, or one kind to another; widening back
  to `any` narrows nothing)
- tightening a keyed map's `keyPattern:` while records hold a key the new
  contract refuses, since a key is not rewritable in place
- every one of those inside an object property's declared `fields:`, at each
  level the dialect nests: a dropped field, a field whose datatype or container
  changed, a field's removed enum value, a field's tightened keys, a field
  reference's narrowed target, each counted where the value actually sits
- adding `required:` to a property a record lacks, and declaring a **new**
  property `required:`, which strands every live record at once: none of them
  can carry a property no declaration had

Widening diffs (a new kind, a new optional property, a new enum value, a new
state or transition, removing `required:`) always admit. The guard counts, it
never blanket-refuses the class: removing an enum value no record holds
admits, and dropping a property admits once every record has nulled it. As a
minimal example, re-applying the task kind with `abandoned` removed refuses
while one task still sits in it:

```yaml
properties:
  status:
    type: state
    states:
      - proposed
      - open
      - done
```

`required:` is a form hint, unenforced on writes, but adding it to a stored
declaration is still a narrowing: the guard counts the records that lack the
property. Nothing converts or discards your records behind your back; they
are yours to migrate, and the refusal tells you how many stand in the way.

**Renaming: `renamedFrom:` is reserved.** A property may declare the name it
replaces:

```yaml
properties:
  dimensions:
    type: string
    renamedFrom: size
```

The key is admitted, validated (it may not name the property itself, a name
the kind still declares, or a built-in) and stored, but **not yet acted on**:
nothing rewrites records today, so a rename whose old name live records still
carry refuses like any other narrowing change. It is reserved so that when
the rewrite arrives, the declaration is already in the manifest dialect and
nothing changes shape on the wire.

## Quarantine

A binary that tightens a vocabulary or trait contract can make an
already-installed bundle's stored closure fail admission at the next
repository open. When that happens the substrate does not brick the
repository: it installs the maximal admissible subset of the installed
authorities and **quarantines** the rest. Each quarantined authority is
logged with its admission reason, left out of the live registry (its kinds
refuse writes and its callables do not run), and marked `quarantined: true`
with a `quarantineReason` on its `authority` record. The console surfaces
such a bundle as "needs re-install". Re-applying a valid closure clears
the marker, and so does a later open under a binary that relaxed the contract
again.

Next: [projection](projection.md), how many source records describe one
subject.
