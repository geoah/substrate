# Vocabulary as records

Vocabulary is data. Every vocabulary element is a manifest, a document in the same
[envelope](data-model.md#the-envelope) as everything else, and every
declaration is an ordinary record in the repository's own changelog, readable
through the same collections as data. "What does the vocabulary say" is a query,
not a file read.

## The declarable kinds

Ten kinds declare everything:

| Kind                           | Declares                                              | Taught on                                         |
| ------------------------------ | ----------------------------------------------------- | ------------------------------------------------- |
| `substrate.reamde.dev/core/authority`     | one authority: the DNS name packages publish under    | [Data model](data-model.md#kinds-and-references) |
| `substrate.reamde.dev/core/package`       | one package: the group its kinds live in, and its version | [Data model](data-model.md#kinds-and-references) |
| `substrate.reamde.dev/core/kind`          | one kind: the properties its records carry            | [Data model](data-model.md#kinds-and-references) |
| `substrate.reamde.dev/core/propertytype`  | one custom property type: a refinement of a base type | [Data model](data-model.md#property-types)       |
| `substrate.reamde.dev/core/trait`         | one trait, bound by kinds                             | [Data model](data-model.md#traits)               |
| `substrate.reamde.dev/core/recordmapping` | how a source record's properties reach its subject    | [Projection](projection.md)                      |
| `substrate.reamde.dev/core/function`      | one pure callable in Python or Go                     | [Functions](functions.md)                        |
| `substrate.reamde.dev/core/agent`         | one callable whose body is an LLM loop                | [Agents](agents.md)                              |
| `substrate.reamde.dev/core/bundle`        | one bundle: the closure it installs as a unit      | [Bundles](bundles.md)                      |
| `substrate.reamde.dev/core/actor`         | one name writes are attributed to                     | [The API](api.md#actors)                         |

A manifest wears the same four-key [envelope](data-model.md#the-envelope) as
any record. Its `kind:` is one of the ten above, always a core kind whatever
package the document declares into, and any other envelope key is a load
error.

A declaration's id is its declared name. For a `kind` that name is the
kind reference, and it must equal
`<data.authority>/<data.package>/<data.names.singular>`: a manifest that spells
them differently is a load error, never a silent rename. A `propertytype`,
`trait`, `recordmapping`, `function` and `agent` take the same
`<authority>/<package>/<name>` form. A `package`'s id is
`<authority>/<package>`, and a `bundle`'s id is the package it owns
(`providers.substrate.reamde.dev/google`). An `authority` document's id is the
DNS name itself, and an `actor`'s id is a bare word. So the closure header, a
package, is seven lines:

```yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: samples.substrate.reamde.dev/tasks
data:
  authority: samples.substrate.reamde.dev
  package: tasks
  version: 1
```

Each declarable kind has a collection under `substrate.reamde.dev/core`, named
for the kind, and declarations list and read there like any other record. A
declaration's id is the one id form that carries a `/`, which is legal in a
URI path segment only percent-encoded, so a REST path spells it `%2F` and the
API decodes it once:

```http
GET /api/v1/substrate.reamde.dev/core/kind/samples.substrate.reamde.dev%2Ftasks%2Ftask
```

## How the vocabulary reaches a repository

The binary's embedded tree is a seed, not a package. A declaration is a
record in the repository's own changelog, and there are exactly three ways one gets
there, each an ordinary changelog entry attributed to the hand that wrote it and
auditable in the [changelog](changelog.md):

- **The seed, once, at creation.** Creating a repository writes the binary's
  embedded tree into the new repository's changelog as ordinary record entries,
  under the actor `bundle:core`, in the same transaction as the repository
  itself. After that the tree has no standing over that repository: nothing
  re-projects it at open, and nothing is ever pruned. A shipped package the
  tree stops declaring stays in every repository that already holds it.
- **The boot-time upgrade, at the first open under a new binary.** Every
  declaration carries a `version`, its package's unless the declaration
  overrides it. The first open of a repository in a process diffs
  the binary's shipped declarations against the stored ones and appends the
  difference as explicit entries under the actor `substrate`: one transaction
  per repository, convergent and idempotent, so an unchanged tree writes
  nothing at all. Only same-or-newer wins, never a downgrade and never a
  prune, and a repository nobody opens is never touched. A version is an
  incremental integer, ordered as plain integers; 0 is the absent version
  and orders below everything. A package whose stored rows belong to
  somebody else here is skipped whole: the upgrade never seizes a name it
  does not already own.
- **An install, which is a copy.** Installing a bundle writes that
  bundle's manifests into the repository's changelog under
  `bundle:<authority>:<package>`
  ([Bundles](bundles.md)). The shipped catalog is a source, never a
  package, and nothing on the serving path reads it.

Install and apply are one path. `POST …/vocabulary/apply` with
`{"documents": […]}` is the batch verb, the same closure an install applies,
and where `substratectl apply` routes any vocabulary documents it is given. A generic
PUT, PATCH, or DELETE of a vocabulary record is a batch of one. On every one
of these doors the engine maintains the `version` itself: an incoming value is
honored only when it moves past the stored one, a changed definition lands at
stored+1, an unchanged one keeps its stored version, and a changed or deleted
declaration that cannot carry a version of its own (a trait, a function)
moves its package's forward instead, so nobody bumps by hand.

**The package chokepoint** decides who may write a declaration, which is what
a package is for. Shipped vocabulary, a package whose stored rows say
`source: builtin`, is writable only through a substrate path: the seed, an
upgrade, an install, which is what the actors `substrate` and
`bundle:<authority>:<package>` name. A generic API write into one is
`forbidden`, and neither actor form can be claimed by a request.

A **provider** package is refused on the same terms. Installing one from the
catalog's provider tier (`providers.substrate.reamde.dev/google` and the five
beside it) writes `source: published` on its package row and its declarations,
and afterwards a `POST …/vocabulary/apply` naming that package is `403`,
whoever holds the token. The publisher ships each change with a version bump
and the [upgrade preview](bundles.md#install-and-lifecycle) offers it; the
RECORDS of a provider's kinds are the repository's as usual, and so is every
lifecycle verb on its bundle. The same closure applied by hand from the files
the binary ships carries no tier and lands `installed`, which is the one way to
hold a provider's declarations open to editing
([decision 0048](decisions/0048-providers-are-published-samples-are-copied.md)).

Everything else, the kinds the repository declares itself and the sample
packages it imported, is the user's to write.

Opening a repository rebuilds its registry from the stored declaration
records, shipped, published and installed alike, each carrying the source its
rows record. Nothing consults the embedded tree, and a repository whose rows hold
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
  singulars and plurals and a package name stay `[a-z][a-z0-9]*`; an authority
  stays a dotted lowercase DNS name; an actor is a bare word or a prefixed
  machine hand, never a DNS name; enum and state values stay lowercase words.
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
  `property` names a reference the from-kind declares `subject: true`, which
  must be single-valued, `required: true`, `mustExist: true`, never
  `onDelete: cascade`, and pinned at the mapping's `to`; a mapping's `to` kind
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
- narrowing a `reference` property's `kind:` or `trait:` pin while records
  point outside the new one (unconstrained to a pin, or one pin to another;
  widening back to `any` narrows nothing)
- tightening a keyed map's `keyPattern:` while records hold a key the new
  contract refuses, since a key is not rewritable in place
- every one of those inside an object property's declared `fields:`, at each
  level the dialect nests: a dropped field, a field whose datatype or container
  changed, a field's removed enum value, a field's tightened keys, a field
  reference's narrowed target, each counted where the value actually sits
- adding `required:` to a property a record lacks, and declaring a **new**
  property `required:`, which strands every live record at once: none of them
  can carry a property no declaration had
- adding `mustExist:` to a reference whose stored values name records that are
  not there
- the same four shapes on a reference's own
  [link properties](#reference-properties) (dropped, retyped, an enum value
  removed, `required:` added), counted over the stored values that carry them

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

`required:` is enforced on writes, against the record the write produces: a
create that omits the property is refused with `422`, and so is a patch that
clears it, while a patch that never mentions it is not. An empty value is no
value: `""`, `[]` and `{}` are refused exactly as an absent property is. On a
declared object's `fields:` the same rule holds against the object the write
stores, since an object value is written whole. A required reference is no
different: every write leaves the record carrying one, or it is refused.

A `default:` beside it is what a create that does not name the property stores,
materialized into the row and the changelog entry at the write. It is a
property's own: a `default:` inside `fields:` is refused, because nothing builds
an object to put one in. Defaults do not backfill, so adding `required:` to a
stored declaration is still a narrowing: the guard counts the records that hold
no value for it, by the same rule the write path refuses them. Nothing converts
or discards your records behind your back; they are yours to migrate, and the
refusal tells you how many stand in the way.

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

## The reserved keys

A declaration's key set is closed, so a key one binary does not know
[quarantines](#quarantine) the package that ships it. That makes adding a
key an upgrade of every binary that might read the closure, which is why these
three are in the dialect before anything acts on them. Each is admitted,
validated at load and stored on the declaration; none of them changes a write.
`renamedFrom:`, above, is the third.

**`unique:` marks one value per record.** At most one live record of the kind
carries any given value, which is the constraint behind "one person per email"
and "one book per ISBN":

```yaml
properties:
  email:
    type: email
    unique: true
```

**Nothing enforces it yet**: no index exists and no duplicate write is
refused. It is refused where it could not be stated: on a `repeated:` or
`keyed:` property, on an object's field, and on `object`, `json`, `state`,
`secret` and `blobref`, whose stored values have no equality an index could
police.

**`deprecated:` marks what a client should stop offering.** A property, an
object property, a state property, a reference and a single enum value each
take it:

```yaml
properties:
  size:
    type: string
    deprecated: true
  flavor:
    type: enum
    values:
      - sweet
      - value: salty
        deprecated: true
  predecessor:
    type: reference
    deprecated: true
```

The declaration still validates and still stores, so every existing record
keeps working; what changes is what a picker offers, a form shows and a tool
card suggests writing. This repository prefers add-and-deprecate to narrowing,
and the marker is what makes the deprecated half tellable from the live one. A
`deprecated:` declaration may not also be `required:`, because a form cannot
both stop offering a value and refuse to submit without it.

## Reference properties

A link can carry values of its own: where it sits in a list, what role it
names, when it started. They are declared under the reference's own
`properties:`, and what is not declared is refused:

```yaml
properties:
  author:
    type: reference
    kind: person
    repeated: true
    properties:
      order:
        type: int
        description: where this one sits in the list
      role:
        type: enum
        values:
          - value: writer
          - value: editor
```

A write sends them beside the value's `ref` key, and the engine coerces each
one exactly as it coerces a record's own property, against the declared
datatype, enum set, pattern and bounds. An undeclared name answers `422`, on
every reference: `ref` alone is always legal, because it is what a read serves,
and a reference declaring no block admits nothing beside it. The value is
written whole, like every property value, so a name the write leaves out is a
name the link stops carrying, and `required:` here means every stored value of
the property has one.

A link property is a flat single value: one scalar, enum or refinement, and
never a list, a map, an object, a machine or another reference. The block is
legal on a single or a `repeated:` reference and refused on a `keyed:` one,
where the map key already carries what distinguishes the entries. That bound is
what lets core's own `kind` declaration state the block field by field, and it
is the line the dialect draws: anything a link cannot hold under it is a record
with a reference at each end.

Two spellings a record property takes are refused here, both because the
stored value has to be the document that was written. A value is a mapping
(`{value: writer}`), never the bare word `writer`, and a property is a mapping
(`{type: int}`), never a bare datatype.

The block evolves under the
[narrowing](#vocabulary-evolution-and-the-dialect-contract) guards, counted
over the stored values that carry them: dropping a declared link property,
retyping one, removing an enum value it holds, and adding `required:` are each
refused while live values would be stranded. The reference itself is classified
the same way. Dropping it, taking `repeated:` off one that holds several
targets, repointing its pin at a narrower kind and adding `mustExist:` all
count the values they would strand.

## Quarantine

A binary that tightens a vocabulary or trait contract can make an
already-installed bundle's stored closure fail admission at the next
repository open. When that happens the substrate does not brick the
repository: it installs the maximal admissible subset of the installed
packages and **quarantines** the rest. Each quarantined package is
logged with its admission reason, left out of the live registry (its kinds
refuse writes and its callables do not run), and marked `quarantined: true`
with a `quarantineReason` on its `package` record. The package is the unit, so
one broken closure parks it and leaves every other package its authority
publishes serving. The console surfaces
such a bundle as "needs re-install". Re-applying a valid closure clears
the marker, and so does a later open under a binary that relaxed the contract
again.

Next: [projection](projection.md), how many source records describe one
subject.
