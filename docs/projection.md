# Projection: mappings, ownership, and merges

The same human appears in a dozen systems. The substrate never fuses records
by value. Instead each source keeps its own record, points at the one subject
it describes, and the subject's shared properties are **projected** from all
those sources at once, under a per-property ownership rule that protects your
hand edits. This page is that projection: record mappings, managed properties
and tiers, and the merges that join two subjects that turn out to be one.

## Record mappings

**What a source holds stays its own record**, pointing at the one subject it
describes through an ordinary edge. A `recordmapping` names that edge and
declares how the record's properties reach the subject.

The GitHub [integration](bundles-catalog.md#github) ships a `user` kind
(GitHub's record of an account, in GitHub's own shape) and this mapping onto
the shipped `person`:

```yaml
kind: core.substrate.reamde.dev/recordmapping
metadata:
  id: github.bundles.substrate.reamde.dev/userperson
data:
  authority: github.bundles.substrate.reamde.dev
  from: github.bundles.substrate.reamde.dev/user
  to: people.substrate.geoah.me/person
  edge: person                     # the ordinary edge the mapping rides
  match:                           # first-link probes: how a new record
    - from: email                  #   finds an existing person
      to: emails
  map:                             # assignment paths, nothing else
    name: name
    displayName: login
    emails:
      path: email
      merge: union
```

`from:` and `to:` are kind references, so a mapping says exactly which two
kinds it joins and an installed manifest can name a shipped kind without
guessing. `map` is assignment-only: paths like `name`, `name.displayName`, or
`emails[].value`, no expressions and no conditionals. Computation is the
bundle's job before the write, and the loader type-checks every path
against both declared kinds, so a disagreement fails on the manifest that
caused it, never on the first sync that hits it. Both `match` and `map` may
be empty: a link-only mapping carries structure and copies nothing.

Three behaviors fall out of this one document:

- **Match, or shell birth.** A `user` arriving without its `person` edge is
  resolved in the same transaction: exactly one live person carrying that
  email links; zero, or several, mint a fresh person instead of guessing.
  Two syncs racing the same new person mint **one** shell. Nothing ever
  auto-merges; joining two existing people is the owner's manual `merge`, and
  it is reversible.
- **Recompute, with yield.** The person's mapped properties are recomputed
  from all live source records whenever one changes: `name` from the latest
  writer, `emails` as the union of what every source asserts. But a value
  **you** wrote is never touched (the next section is the whole rule).
- **Ids that never lie.** After a merge, the losing id resolves to the winner
  forever, and any read by it says so.

## Managed properties

The mechanism behind "hand edits survive syncs" is per-property ownership.
Every accepted write records its actor as the property's **manager**: the
substrate always knows, per record per property, who holds the current value.
Beside the actor it records a **tier**, the manager's standing against
recompute. There are three:

- **machine**: the sync machinery. Authority-declared connector actors and
  the engine's own hand write here, and everything recompute writes is
  machine-held whatever actor it credits. Machine-held values are recompute's
  to replace.
- **bundle**: installed code. Every write a [function](functions.md) or
  [agent](agents.md) makes through its dispatch holds here.
- **owner**: you. The three human doors (`api`, `console`, `substratectl`) are
  declared at this tier, and so is any actor no declaration knows. The moment
  you type a name into the console, you are the manager of `name`.

**The tier is an explicit attribute of the write context, never an inference
from the actor's spelling.** A declared actor may carry
`tier: owner|bundle|machine` on its [actor document](api.md#actors)
(machine is the default for an authority-declared actor), function and agent
dispatch stamps the bundle tier on its own writes, and an actor no data
places anywhere defaults to the owner tier: a stranger's client holds like
you, never like the machinery. The governing tier is resolved from the live
declaration on every write, never frozen at mint, so re-declaring an actor at
a different tier changes what already-minted tokens may do from their next
write onward. Renaming an actor never changes write semantics.

Mapping recompute runs whenever a source record changes, and per mapped
property it follows three rules:

- **Yield.** If the manager's tier is above machine, the recompute leaves the
  value alone and records what it would have written as an **alternative**
  beside it. Your edit survives the sync, and so does a function's: an
  bundle write is a visible pin, never a silent freeze.
- **Select.** Otherwise the latest-updated live source wins (`atomic`) or the
  union of every live source's items lands (`union`), and the manager becomes
  the winning source's actor at the machine tier, so the changelog says a name
  came from GitHub, not from "the system". Nothing in a manifest ranks
  sources; ties break deterministically by kind reference, then id.
- **Delete.** A property no live source carries, and no outside manager holds,
  is deleted. When the provider stops asserting a phone number, it goes; what
  another source still asserts, stays.

### The ledger on the wire

Single-record reads surface the whole ledger as `propertyMeta`: per property,
its manager, its tier, when it changed, and the **alternatives**, every live
source value that differs from the stored one:

```json
"propertyMeta": {
  "name": {
    "manager": "owner",
    "tier": "owner",
    "alternatives": [
      {"actor": "function:githubsync", "value": "ada"}
    ]
  }
}
```

The owner typed "Ada Lovelace" by hand, GitHub still says "ada", and both
facts are on the wire. Lists and changes never carry `propertyMeta`; only a
single-record read assembles it. Adopting an alternative is just writing it.
**Releasing** a hand edit is patching the property to null: the delete clears
the value and its manager, and the same transaction recomputes from live
sources, so the property refills on the spot, back to following the sources.
One deliberate cost comes with this: a held value is immune to fresher truth
until released, which is exactly why every read shows the alternatives beside
it. One release works for every tier: a bundle pin lets go exactly like an
owner hold.

### Contributing a value

There is exactly one way for an integration to contribute a value without
pinning it: ship a **source kind** and a **recordmapping**, and write your
own records. Your records become live sources, your values compete in the same
selection as every provider's, and they release by omission when your records
go. A minimal contribution:

```yaml
kind: core.substrate.reamde.dev/kind
metadata:
  id: enrich.example.com/enrichment
data:
  authority: enrich.example.com
  names:
    singular: enrichment
    plural: enrichments
  properties:
    name:
      type: string
    email:
      type: email
  edges:
    person:
      to: people.substrate.geoah.me/person
      required: true
---
kind: core.substrate.reamde.dev/recordmapping
metadata:
  id: enrich.example.com/enrichmentperson
data:
  authority: enrich.example.com
  from: enrich.example.com/enrichment
  to: people.substrate.geoah.me/person
  edge: person
  match:
    - from: email
      to: emails
  map:
    name: name
```

A function that puts an `enrichment` record now contributes `name` to the
linked person: freshest source wins, the value shows as an alternative when
someone holds the property, and deleting the record withdraws it. Writing the
person's property directly remains possible, and is a pin.

State properties are never recomputed: a state moves through its
[declared transitions](data-model.md#validation-and-state-machines) or not
at all, so no amount of syncing can quietly complete a task.

## Merges

Nothing in the substrate fuses by value: two people holding the same email
address are two records until somebody merges them. Merging is always a
deliberate act, one of the seven mutations, and the engine never performs one
on its own. What it does instead is suggest.

`merge(kind, winner, loser)` takes two live records of the **same kind**,
addressed by the (kind, id) pair the kind reference beside the two ids spells
out (an edition is not a bad copy of a work, so a merge across that line is a
category error the engine refuses), and joins them so the winner absorbs the
loser's place in the graph:

- **Every edge re-points at the winner**, incoming and outgoing, and every
  source record's subject edge moves with them. Collisions with edges the
  winner already has dedupe.
- **Labels fill gaps**: the winner's stand, the loser's land where the winner
  has none. Annotations move too, colliding keys resolving newest-wins.
- **Properties do not migrate.** The winner now has more sources pointing at
  it, so its mapped properties are recomputed, through the same yield rules as
  any sync, which is how a hand edit on the winner survives its own merge.
  Copying values across would freeze a stale answer into the winner. The
  loser's manager rows migrate where the winner holds nothing, tier included,
  so a migrated bundle pin yields on the winner exactly as it did on the
  loser.
- **States never move.** A merge is not a transition, and nothing in it may
  demote a person or complete a task.
- **The loser is tombstoned, not erased**: a finalizer holds garbage
  collection off it, so the merge stays reversible.

Every merge writes a `recordmerge` record: an ordinary record carrying
`winner` and `loser` edges and a `moved` property recording everything the
merge moved. That record is what makes the undo possible.

### Ids that never lie

Merging means ids move, and a client that cached one must not silently read
stale data. Three guarantees:

- **Any read by a former id returns the canonical record and says so.** The
  response carries `canonicalId`, present only when the id you used was not the
  canonical one, so a stale id self-corrects on its next read instead of
  404ing. The trail is the kind's own: a former id resolves within its kind,
  and another kind wearing the same id is untouched.
- **Trails stay flat.** After A merges into B and B into C, both A and B are
  former ids of C directly: resolution is one lookup, bounded forever.
- **Ids are never reused within a kind, and never re-derived.** A tombstoned
  loser's id stays a former id of its winner forever, so notes, annotations,
  and an agent's memory can hold a full (kind, id) pair without a validity
  window.

### Split, the undo

`split` reverses one merge, addressed by the `recordmerge` record's own id:

```http
POST /api/v1/core.substrate.reamde.dev/recordsplits
{"merge": "kq3v9x2m41pf"}
```

The loser comes back to life at its own id, the moved edges, subject edges,
labels, annotations and manager rows go back where the record says they came
from, and both sides recompute from the source sets they now have. A split
reverts _the merge_, not everything that happened after it: a label or
annotation rewritten since the merge keeps its newer value. The split writes
its own `recordsplit` record, pointing at the merge it undid.

## Merge requests

A `recordmergerequest` is the envelope a suggested merge travels in before
anyone has agreed to it. A function or an app writes one, the owner decides,
and **accepting it is what performs the merge**. Here is one as the shipped
duplicate detector writes it, proposing that a second record of Ada is the
same person:

```yaml
kind: core.substrate.reamde.dev/recordmergerequest
metadata:
  id: dupe-9f2k-x41c              # deterministic: the pair, sorted
data:
  properties:
    rationale: '"Ada Lovelace" and "ada" look like the same person'
    evidence:                     # the signals that matched
      signals:
        - signal: email
          value: ada@example.com
    decision: proposed            # a state: proposed, accepted, rejected
  edges:
    - rel: winner                 # the record that survives the merge
      to:
        kind: people.substrate.geoah.me/person
        id: 9f2k
    - rel: loser                  # the record merged away into the winner
      to:
        kind: people.substrate.geoah.me/person
        id: x41c
```

The `decision` state is the whole lifecycle: `proposed` until somebody
decides, then `accepted` or `rejected`, each transition stamping `decidedAt`.
Accepting is an ordinary
[state transition](data-model.md#validation-and-state-machines), a patch:

```http
PATCH /api/v1/core.substrate.reamde.dev/recordmergerequests/dupe-9f2k-x41c
{"properties": {"decision": "accepted"}}
```

The transition's `onEnter: applyMerge` performs the merge in the same
transaction, re-running the merge's own guards, so a stale request (a record
already merged away, deleted, or of the wrong kind) fails the transition
whole: the request stays `proposed` and gains a conflict annotation saying
why. A reviewer's note rides the same atomic write, as an `owner/note`
annotation on the request: one patch carries properties, labels, and
annotations together.

The deterministic id does double duty. It dedupes suggestions, and it is the
**rejection memory**: a request for the pair in _any_ state suppresses
re-suggesting it, so a rejected pair stays rejected instead of coming back
after every sync.

The suggestions come from a shipped [function](functions.md): on every person
created, it probes for likely duplicates (an exactly shared email address is
near-certain; strongly overlapping names score by similarity) and emits one
request per strong candidate, preferring the established record as the winner.
The owner reviews the queue in the [console](console.md) and accepts or
rejects, with the matcher's evidence beside a field-by-field comparison of the
two records. Merging without a request is the same mutation driven directly:
`merge` over [GraphQL](graphql-and-search.md), or a REST post naming the kind
and the two ids:

```http
POST /api/v1/core.substrate.reamde.dev/recordmerges
{"kind": "people.substrate.geoah.me/person", "winner": "9f2k", "loser": "x41c"}
```

### The patch request sibling

A `recordpatchrequest` is `recordmergerequest`'s sibling: an app or agent
proposes a change, the owner decides, and accepting applies it atomically,
re-validated against the version it was computed on. It carries an `op`,
`create`, `patch` (the default), or `delete`, so a reviewed write can mint a
new record, edit an existing one, or tombstone one. A create names its target
by `targetKind` and `targetId`, because the record does not exist yet; a patch
and a delete carry the `target` edge. The agent
[`propose` tool](agents.md) emits exactly this request rather than writing the
target directly, which is how a semi-trusted agent contributes a dangerous
verb through review instead of on its own authority.

Three rules keep the review honest. **The reviewed envelope is immutable**:
once a request is proposed, `op`, `targetKind`, `targetId`, `diff` and the
`target` edge are frozen, and a write that would change them is refused, so
the values the reviewer read cannot be swapped underneath them. `decision` and
`rationale` stay mutable, because deciding is the point. **The decision is
optimistic**: an owner's accept or reject must carry `ifVersion`, the request
version the reviewer read, so a concurrent change cannot slip under the
decision. And **accept is authorized as the write it performs**: a function or
agent driving the accepted transition must have the written kind in its
effective emit set — `targetKind` for a create, the target's kind for a patch
or delete — so a callable that can only emit `recordpatchrequest` cannot
self-accept its way to an arbitrary write. Owner acceptance stays unbounded.
Every deterministic refusal leaves the request `proposed` and annotates it
with why, and an accepted diff that changes nothing is a conflict the owner
sees, never a no-op mistaken for done.

Next: [the API](api.md), the surface every one of these operations rides.
