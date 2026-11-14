# Projection: mappings, ownership, and merges

The same human appears in a dozen systems. The substrate never fuses records
by value. Instead each source keeps its own record, points at the one subject
it describes, and the subject's shared properties are **projected** from all
those sources at once, under a per-property ownership rule that protects your
hand edits. This page is that projection: record mappings, managed properties
and their marks, and the merges that join two subjects that turn out to be one.

## Record mappings

**What a source holds stays its own record**, pointing at the one subject it
describes through an ordinary reference. A `recordmapping` NAMES that reference
into being — the source kind declares nothing — and declares how the record's
properties reach the subject.

**The package that owns the TARGET kind declares the mapping**, and no other
([decision record 0049](decisions/0049-the-owner-of-a-mappings-target-declares-it.md)).
A provider ships mirror kinds and nothing that names a kind it does not own;
the package that owns the kind
those mirrors describe says how they reach it, and edits, versions and deletes
that declaration like everything else it owns. A mapping onto somebody else's
kind is refused, whichever end the declaring package owns.

Three rules follow. **`from` may name a kind in any package**, and it resolves
when the mapping is installed, so a mapping naming a provider that is absent
fails on that document. That is why a sample's shipped mappings are
conditional: the catalog's add route drops the ones whose
provider this repository does not hold rather than refuse the whole add
([suggested mappings](bundles.md#suggested-mappings)). The apply door does the
same when the request asks it to, with `holdWaitingMappings`
([how the vocabulary reaches a repository](vocabulary.md#how-the-vocabulary-reaches-a-repository)).

**One mapping per (source kind, subject property), and one per (source kind,
target kind).** The first is the record's rule: one mirror kind reaches two
subject kinds through two `subject: true` references, and two mappings through
one reference are refused. The second goes past it, because two slots onto ONE
target give the [subject hop](data-model.md#kinds-and-references) two answers for one
value: a reference that admits that kind could resolve through either mapping,
and a recompute would take one source's contributions twice. So a source kind
reaches a given target through one property only.

**Uninstalling a package another package's mapping names as its source is
refused**, naming the mapping: removing Linear while a mapping reads
`linear/issue` fails until the mapping goes. The same guard refuses an UPGRADE
that drops the kind, which is the one case with a remedy other than deleting
the mapping: apply the provider's new closure and the edited mapping in ONE
batch, and the admission sees the end state rather than the gap. A change to a
source kind that a mapping's paths no longer type-check against is refused the
same way, on the mapping that stopped fitting.

This is the mapping that says GitHub's `user` mirror describes a `person`. You
do not have to write it: the people sample SHIPS it, and three more like it, as
[suggested mappings](bundles.md#suggested-mappings) adding the sample admits once the
provider they read is installed.

```yaml
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: samples.substrate.reamde.dev/people/githubuserperson
data:
  authority: samples.substrate.reamde.dev
  package: people                  # the package that owns `person`
  from: providers.substrate.reamde.dev/github/user
  to: samples.substrate.reamde.dev/people/person
  property: person                 # the slot this mapping puts on `user`
  match:                           # first-link probes: how a new record
    - from: email                  #   finds an existing person
      to: emails
  map:                             # assignment rules, nothing else
    name:
      path: name
    displayName:
      path: login
    emails:
      path: email
      merge: union
```

**THE MAPPING OWNS THE LINK.** `github/user` declares no `person` property at
all: GitHub cannot know which kind a repository keeps its people in, nor what
it calls the slot, so it declares neither
([decision record 0096](decisions/0096-a-mapping-synthesises-its-subject-slot.md)).
Admitting the mapping SYNTHESISES the reference on the source kind's live
registry entry — named by `property`, pinned at `to`, single, `mustExist`,
never cascading, and `managed: true`, which is a client's cue to render it
read-only. A value of another kind is refused, a bare id completes against the
pin, and until the mapping exists there is no slot to fill.

Three things follow. A second consumer may map the same source onto its own
kind under its own word, and neither consumer needs the provider to have
anticipated either. A mapping whose `property` collides with a property the
source kind declares FOR ITSELF is refused, naming both, because the mapping
would otherwise take a declared slot over silently. And removing the mapping
removes the slot, which is refused while live records still link through it:
a subject link is not an ordinary optional value, and the only road back from
clearing one is a full resync.

A source kind MAY still declare the slot itself, and one written before this
rule does. The declaration then stands — its description, its `required:` —
and the mapping stamps the pin and the marker onto it, so a bundle that has
not been rewritten, and a declaration read back out with `get -o yaml` and
applied again, both land unchanged.

**A reference may pin a mirror — and mostly should not.** Any reference,
anywhere, may name a kind some mapping reads as its `from`
([decision record 0095](decisions/0095-a-reference-may-pin-a-mapping-source-kind.md));
a pin at the source is satisfied by the value as written, so it never takes the
hop and the one-hop rule is untouched. That is what lets a provider model its
API's own relations as references (`issue.assignees` at `github/user[]`,
`calendarevent.attendees[].address` at `google/emailaddress`, `message.user`
at `slack/user`), which it could not do while adding a sample's mappings
retroactively narrowed what the provider was allowed to declare.

**Outside the provider, point at the subject.** A task's `assignee` belongs at
`person`, not at one provider's view of a person: the subject hop below lets
a provider's function write the `github/user` path it actually holds into that
person-pinned slot, so nothing is lost by pinning the subject and a merge
moves every pointer at once. A consumer kind pinned at a mirror instead ties
itself to one provider, is left dangling when that provider is uninstalled,
and is not carried by a merge. Pin the mirror when the relation IS the
provider's — inside its own package — and the subject everywhere else.

**A mapping carries a relation the same way.** A map rule copies a mirror
reference as it is, and the target's pin decides what is stored
([decision record 0120](decisions/0120-a-map-rule-reaches-a-mirrors-subject-through-the-targets-pin.md)).
This rule, in the package that owns `task`, gives a GitHub issue's task its
assignee:

```yaml
  from: providers.substrate.reamde.dev/github/issue
  to: ~/tasks/task
  property: task
  map:
    name:
      path: issueTitle
    assignee:                      # pinned at person on task
      path: assignee               # a github/user reference on the issue
```

The recompute writes the `github/user` reference into the person-pinned
`assignee`, and the subject hop stores that user's person. A user with no
person yet gets one, as any hop does. The offer behind the value keeps the
`github/user` as the issue wrote it, and the read compares it through the
user's person, so the issue shows as the source and no alternative appears.
A repeated source onto a repeated target (`assignees` onto a `person[]` slot)
resolves each item, and two users of one person are one entry. A path never
crosses a reference: `assignees[].person` is refused, naming this spelling.
The value follows the user's person when the ISSUE is next written. Moving
the user to another person (a split) leaves the task on the old one until
then, and the read lists the issue's offer as an alternative meanwhile.

**A `match` probe reads the source record's own values**, so it needs a value
and not a pointer: a source that declares its email addresses as references
cannot probe them onto `person.emails`, because what the row holds there is a
record path. A provider that wants its rows matchable keeps the scalar beside
the reference (`email`, `emailAddresses`) and probes that.

**A probe compares exactly unless it folds.** Every value is trimmed, and a
value read from an `email` property is lowercased, but the target's value is
compared as stored, so `Ada Example` and `ada example` are two people. A probe
that declares `fold: case` lowercases and trims both ends, the target's
stored value item by item on a repeated property. The stored side is trimmed
of ASCII spaces only, so a stored value ending in a tab never matches:

```yaml
  match:
    - from: realName
      to: name
      fold: case
```

`case` is the only fold. It is opt-in because a probe links records without
asking, and short strings also carry identifiers whose case is significant.
A folded probe computes `lower(btrim())` on each stored value it reads, and
Postgres folds that side under the database's locale
([decision record 0116](decisions/0116-a-probe-folds-case-only-when-it-declares-fold-case.md)).

`from:` and `to:` are kind references, so a mapping says exactly which two
kinds it joins and an installed manifest can name a shipped kind without
guessing. `map` is assignment-only, keyed by the subject property written: each
rule is an object naming the source `path` it reads (`name`, `name.displayName`
or `emails[].value`) beside an optional `merge`, and there are no expressions and
no conditionals. A bare path (`name: name`) is refused naming the object
that replaced it: one property has one shape, which is what lets the
`recordmapping` kind declare a rule's own fields. Computation is the
bundle's job before the write, and the loader type-checks every path
against both declared kinds, so a disagreement fails on the manifest that
caused it, never on the first sync that hits it. Both `match` and `map` may
be empty: a link-only mapping carries structure and copies nothing.

**`merge` is where the two ends' repetitions are settled**, and there are
three. `atomic`, the default, takes one source's value whole and needs both
ends to agree on repetition. `union` takes the deduped union of every live
source's items and needs a REPEATED target; a single-valued path contributes
its one value, which is legal. `first` is union's question from the other
side: the HEAD of a repeated source onto a SINGLE-valued target, which is the
only rule that admits a Google contact's `names[].displayName` onto
`person.name` — a provider that mirrors an API array verbatim has one value
the subject wants and a repetition it does not
([decision record 0086](decisions/0086-the-head-of-a-repeated-source-is-spelled-with-brackets.md)).

Two things about `first` are worth stating. It is POSITIONAL, and position is
not primacy: it takes the array's first entry, not the one the provider flagged
primary, so a provider that does not sort its array meaningfully wants a
derived scalar instead. And an EMPTY source writes NOTHING rather than an empty
value: a contact whose `names[]` is empty leaves the person's name to whatever
else offers one instead of clearing it. The rule applies per source, before the
selection across sources, so the choice between two contacts that both carry a
name is the ordinary latest-write-wins.

Three behaviors fall out of this one document:

- **Match, shell birth, or park.** A `user` arriving without its `person`
  reference is resolved in the same transaction: exactly one live person
  carrying that email links, and none mints a fresh person, a **shell** with
  no properties. Two syncs racing the same new person mint **one** shell.
  SEVERAL candidates follow the mapping's `onAmbiguous`
  ([below](#when-a-probe-finds-several-candidates-onambiguous)); by default the
  source parks with its slot unset rather than add a third person the same
  address then points at, and it resolves on its next write once the owner has
  settled the ambiguity, or when the mapping is applied again
  ([below](#sources-that-exist-before-their-mapping)). A source that offers
  nothing at all — no probe value and no mapped value — mints nothing either,
  because a shell born from it is a row no probe can ever match
  ([decision record 0087](decisions/0087-an-unresolved-source-parks-instead-of-minting.md)).
  Two callers still mint whatever the source carries, because both need a
  record to point at: the [subject hop](data-model.md#kinds-and-references),
  and a source kind that declares its own subject slot `required:`. Nothing
  ever auto-merges; joining two existing people is the owner's manual `merge`,
  and it is reversible.
- **Recompute, with yield.** The person's mapped properties are recomputed
  from all live source records whenever one changes: `name` from the latest
  writer, `emails` as the union of what every source asserts. But a value
  **you** wrote is never touched (the next section is the whole rule).
- **Ids that never lie.** After a merge, the losing id resolves to the winner
  forever, and any read by it says so.

### Core's mappings onto contacts

Core ships nine mappings onto its own `contact` kind: Apple's `contact`,
Google's `contact`, user-made `contactgroup` labels and `emailaddress`, Slack's
`user` (bots excluded) and `bot`, GitHub's `user`, Linear's `user` and
Beeper's `user`, each filling a slot named `contact` on its mirror
(`kinds/substrate.reamde.dev/core/contactmappings.yaml`). Every repository
holds core and nobody can edit it, so these mappings never refuse a
repository's vocabulary. Each one **waits** instead, out of every mapping read,
while its provider is not installed, while the installed provider does not fit
it (an older version lacking a path it reads), or while another package's
mapping fills its slot or its target
([decision record 0232](decisions/0232-core-maps-the-providers-people-onto-contacts-and-a-seeded-mapping-waits-instead-of-refusing.md)).
The registry decides it again at every build, so installing the provider is
what switches its mapping on, and uninstalling it is not refused on core's
account. A mapping that waits although its provider is installed is logged at
every open. Your own mapping of the same mirror on the slot `contact` wins it;
on another slot (the people sample's `person`), both run.

### Sources that exist before their mapping

**The apply that admits a mapping links the sources that already exist**
([decision record 0107](decisions/0107-an-apply-links-the-sources-its-mappings-left-unlinked.md)).
A mapping resolves a subject on the source's own write, and the usual order is
the provider first, its sync, then the mapping, so without this every mirror
synced before the mapping kept an empty slot until the provider wrote that row
again. In the same transaction as the declaration, every live source whose
slot names no live record is decided exactly as its own write would decide it:
one candidate links, none mints, and a source that offers nothing, parks on
an ambiguous probe, or falls outside the mapping's
[`where`](#which-sources-a-mapping-covers-where) stays unlinked. Each link is an ordinary write of the
source's slot, credited to the mapping, so it is in the changelog and a
rebuild replays it.

The same pass runs for every mapping a batch **changes**, and for every
mapping a batch **names by document**, changed or not. Re-applying an
unchanged mapping is therefore how a repository reprojects it: after the owner
merges the candidates a parked source was waiting on, or after an upgrade from
a binary that did not backfill, apply the documents of the package that
declares the mapping again, the mapping among them.

```bash
substratectl apply -f people.yaml    # the package closure, recordmapping included
```

A source that is already linked is not touched, so a second apply links
nothing new and mints nothing. The cost is one probe per unlinked source, in
the apply's transaction, while every other write to the repository waits for
the apply. Two side effects: a slot naming a **deleted** subject counts as
unlinked, so each such apply mints a fresh subject for that source, as the
source's own write would; and a link bumps the source's `version` and
`updated_at`, so it becomes the newest writer for its target's `atomic`
properties.

### When a probe finds several candidates: `onAmbiguous`

A probe that finds two people sharing an address cannot tell them apart. The
mapping says what happens then, beside its `match`:

```yaml
  match:
    - from: emailAddresses[].value
      to: emails
  onAmbiguous: park                # park (the default) | oldest | mint
```

- `park` leaves the source's slot unset and **marks** the source. The mark is
  derived storage like the [orphan mark](#when-the-last-source-goes-the-orphan-mark):
  the source's own write sets and clears it, a rebuild derives it again, and
  the list filter reads it, `filter.ambiguous` (`substratectl get <kind>
  --ambiguous`). It is read when the source is written, so merging the two
  people leaves it in place until the source's next sync, or the next apply of
  the mapping, links it and clears it. A source that is unlinked because it
  offers nothing is not marked.
- `oldest` links the candidate created first. Ids are random, so creation is
  the only order among candidates that means anything.
- `mint` mints a fresh subject, as though the probe had found none.

The two callers that must mint (the subject hop, and a slot declared
`required:`) still mint under `park`, and link the oldest under `oldest`.

**Whatever the policy, a probed value never spreads.** Recompute does not
write a value of a property some probe matches on (`emails` above) onto a
target when another live target already holds it and this one does not. That
is what stops a shell minted out of an ambiguous probe from taking the shared
address through `merge: union`, and a linked contact from writing an address
it shares with another person onto the person it linked. A value the target
already holds stays, so a duplicate that exists is the owner's to settle with
`merge`. The withheld value is still the source's, and a read shows it as an
[alternative](#where-a-value-came-from-the-marks) the owner may adopt by writing
it
([decision record 0103](decisions/0103-an-ambiguous-probe-follows-its-mappings-policy-and-a-probed-value-never-spreads.md)).

### Which sources a mapping covers: `where`

A mapping covers every record of its source kind unless it says otherwise.
`where` narrows it to the records that meet one condition per property, in the
[filter grammar](api.md#the-filter-grammar)'s condition objects, all of which
must hold:

```yaml
  from: providers.substrate.reamde.dev/github/pullrequest
  to: samples.substrate.reamde.dev/tasks/task
  property: task
  where:
    state:
      eq: open
  map:
    name:
      path: title
```

A condition means exactly what the same entry under `filter.properties`
means on a list of the source kind, because the engine compiles it with the
same code. It names a property the source kind declares, never a sensitive
one and never the mapping's own slot, and a bare value (`state: open`) is
refused, as the filter refuses one. An operator that does not fit the type
(`match` on a number, `gt` on a reference) fails the apply that declares it.

Two things a list accepts are refused in a `where`. A declared property named
`createdAt`, `updatedAt`, `deletedAt`, `id` or `version` is refused, because
the filter grammar reads that name as the record's own column, which a
`where` cannot read. A condition that tests nothing (`eq: null`, `in: []`, an
empty `prefix`) is refused rather than dropped. Every apply compiles every
`where` again, so a change to the source kind that breaks one (a retyped
property it names) fails that apply.

A record outside the `where` is treated as a deleted source is:

- **Its own write resolves nothing.** It links no subject, mints none, and is
  never marked ambiguous. The write that brings it inside resolves it then,
  and so does the apply that [links existing sources](#sources-that-exist-before-their-mapping).
- **It contributes nothing.** Recompute reads no value from it, so a record
  that leaves the `where` releases what it projected, and a subject left with
  no covered source takes the [orphan mark](#when-the-last-source-goes-the-orphan-mark).
- **Its link stays.** Only merge and split move a subject slot, so the pointer
  it already holds is kept, `linkedFrom` still lists it, and a record that
  comes back inside projects onto the same subject instead of minting another.
  The kept pointer also spares the subject from `SUBSTRATE_ORPHAN_GRACE`
  collection, which skips any record a live record points at.
- **The subject hop refuses it** when it holds no link, rather than mint a
  subject for a record the mapping does not cover.

Changing a mapping's `where` recomputes every record of its target kind in
the apply, so a narrowed mapping releases what it no longer covers at once. A
widened `where` is a changed mapping too, so the same apply links every
unlinked record it newly covers
([decision record 0107](decisions/0107-an-apply-links-the-sources-its-mappings-left-unlinked.md),
[decision record 0118](decisions/0118-a-mapping-where-narrows-its-sources-in-the-filter-grammar.md)).

### Reading the links back: `linkedFrom`

The link lives on the SOURCE, so nothing among a subject's own properties says
which mirrors point at it. The subject's single-record read says it instead:
`GET /api/v1/<kind path>/<id>` carries `linkedFrom`, one entry per source
record whose mapping-owned subject slot names this record
([decision record 0088](decisions/0088-a-single-record-read-carries-its-inbound-mapping-owned-links.md)):

```http
GET /api/v1/samples.substrate.reamde.dev/people/person/9f2k

→ {"id": "9f2k", …,
   "linkedFrom": [
     {"ref": "providers.substrate.reamde.dev/github/user/ada",
      "kind": "providers.substrate.reamde.dev/github/user", "title": "ada",
      "property": "person",
      "mapping": "samples.substrate.reamde.dev/people/githubuserperson"}]}
```

Four things to know about it. It is derived at read time from the mapping set
and the reference index, so nothing is stored and a mapping installed or
removed changes the answer on the next read. It counts a pointer written under
a **former id**, exactly as [`referencing`](api.md#who-points-at-a-record-referencing)
does, so a merged subject answers for the mirrors that linked to the loser.
The key is **absent, never empty**, on a kind no mapping targets, which is how
"nothing maps onto this kind" is told apart from "nothing has linked yet". And
a **list read never carries it**: it is one query per record, and the reverse
read is the bulk, paged answer. `linkedFrom` is the mirror question;
`referencing` is the general one — every record pointing at this one, through
any property, mapping or not.

The console shows it as the **Where it comes from** section of a record's
page, grouped by the mapping that brought each record
([web console](console.md#a-record)).

## Managed properties

The mechanism behind "hand edits survive syncs" is per-property **marks**
([decision 0284](decisions/0284-a-propertys-mark-stores-whether-sync-may-replace-it-apart-from-where-its-value-came-from.md)).
Every property a write sets carries one beside its value, and it holds two
facts apart: whether sync may replace the value, and where the value came
from.

- **`pinned`**: sync leaves the value alone. It is stored when the value is
  written, from the type of the [actor](api.md#actors) that wrote it: a
  person's, an app's, a function's and an agent's direct write pins; a
  package's and the engine's do not. The moment you type a name into the
  console, `name` is pinned. Nothing works it out again later: not a
  declaration, not the actor as it stands now.
- **`setBy`** links the actor record that wrote the value directly, and
  **`onBehalfOf`** the one it acted for, where it acted for someone. A
  value sync wrote has neither: the engine wrote it.
- **`derivedFrom`** links the source records whose offers back the value:
  the one offering it, every one that agrees with it, or, on a union, each
  contributing one. Both `setBy` and `derivedFrom` may be present: a
  package's own write that a source also offers.
- **`setAt`** is when the value was set: not a later pin, and not a change in
  the alternatives.

A mark names no token: which token wrote a value is on the
[changelog](changelog.md) entry that wrote it. A kind move, a merge and a
rename carry the mark whole.

Mapping recompute runs whenever a source record changes, and per mapped
property it follows three rules:

- **Yield.** If the value is pinned, the recompute leaves it alone, and what
  each source record would have written is an **alternative** beside it.
  Your edit survives the sync, and so does a function's: its write is a
  visible pin, never a silent freeze.
- **Select.** Otherwise the latest-updated live source wins (`atomic`, and
  `first` with the head of its repeated path in hand) or the union of every
  live source's items lands (`union`), and the mark names the source records
  behind it, so a name says it came from GitHub, not from "the system".
  Nothing in a manifest ranks sources; ties break deterministically by kind
  reference, then id. A value recompute leaves as it is, pinned or not,
  keeps its mark, its `derivedFrom` held to the sources whose offers still
  back it: a source that now offers something else is an alternative. When a
  vocabulary apply removes the last mapping that supplied a property, a value
  that stays (pinned, or required) keeps its mark and loses its sources.
- **Delete.** A property no live source carries, and nothing pinned, is
  deleted. When the provider stops asserting a phone number, it goes; what
  another source still asserts, stays. A required property keeps its last
  value, and its `derivedFrom` is dropped, so no link dangles.

<!-- The anchor keeps decision record 0094 resolving: it links
#reading-provenance-propertymeta, and an accepted record's body may not change. -->
<a id="reading-provenance-propertymeta"></a>

### Where a value came from: the marks

Single-record reads carry each property's mark as `marks`, and beside it the
**alternatives**: what each live source record offers, where it differs from
the stored value. Grace is one contact built from two Google address books,
work and home; Ada fixed the name by hand, the Chat app added a nickname for
her, and sync supplied the rest:

```json
"marks": {
  "name": {
    "pinned": true,
    "setBy": {"ref": "substrate.reamde.dev/core/actor/ada"},
    "setAt": "2026-11-10T09:14:03Z",
    "alternatives": [
      {"derivedFrom": {"ref": "providers.substrate.reamde.dev/google/contact/work-c123"},
       "value": "G. Hopper", "updatedAt": "2026-11-12T03:00:11Z"},
      {"derivedFrom": {"ref": "providers.substrate.reamde.dev/google/contact/home-c900"},
       "value": "Grace M. Hopper", "updatedAt": "2026-11-09T21:40:52Z"}
    ]
  },
  "nickname": {
    "pinned": true,
    "setBy": {"ref": "substrate.reamde.dev/core/actor/chat"},
    "onBehalfOf": {"ref": "substrate.reamde.dev/core/actor/ada"},
    "setAt": "2026-11-11T18:02:40Z"
  },
  "jobTitle": {
    "derivedFrom": [{"ref": "providers.substrate.reamde.dev/google/contact/work-c123"}],
    "setAt": "2026-11-12T03:00:11Z"
  },
  "emails": {
    "derivedFrom": [
      {"ref": "providers.substrate.reamde.dev/google/contact/home-c900"},
      {"ref": "providers.substrate.reamde.dev/google/contact/work-c123"}
    ],
    "setAt": "2026-11-12T03:00:11Z"
  }
}
```

**Alternatives are keyed by source record**, so two accounts of one
provider, synced by one function, are two. An alternative's `updatedAt` is
its source record's, not the moment the substrate wrote anything: like the
value, it is a function of the live records, so a `repository rebuild` or a
restore derives the same alternatives. On a reference property both sides
are compared through a mirror's stored subject and the former-id trail, so
an offer naming a `github/user` backs the person the pin stored for it
([decision record 0120](decisions/0120-a-map-rule-reaches-a-mirrors-subject-through-the-targets-pin.md)).
A link a mapping fills (the subject slot) carries no mark: its declaration
says which mapping fills it. Lists and changes never carry `marks`; only a
single-record read assembles them.

**`pin` and `release` are explicit actions**, each an entry of the record's
history (`POST /api/v1/<kind>/<id>/pin` and `…/release`, with the
properties named, [api](api.md#the-five-mutations); `substratectl pin` and
`release`). Pin keeps a value sync wrote. Release hands a pinned property
back to sync, which picks its value again among the alternatives at once.
Both work on a required property, which keeps its value where no source
offers one. Adopting an alternative is releasing the property, or writing
the value. One deliberate cost comes with this: a pinned value is immune to
fresher truth until released, which is exactly why every read shows the
alternatives beside it.

**A mark goes with its value.** A write that takes a property out of a
record takes the property's mark with it, whether or not its history line
says `unmark`, so a pin never stands on a value nobody holds and never stops
a sync filling the property later. A mark the same line moves to another
name stands, and a mark on a property the record never held (a subject slot
with no value yet) is left as it is. A replay of the line does the same, so
a `repository rebuild` gives back the marks the live writes left.

### Contributing a value

There is exactly one way for a provider to contribute a value without
pinning it: ship a **source kind** and write your own records. Your records
become live sources once a mapping points them at a subject, your values
compete in the same selection as every provider's, and they release by
omission when your records go. The provider's half is its own vocabulary and
NOTHING ELSE — no slot, no pointer at a kind it does not own:

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: enrich.example.com/crm/enrichment
data:
  authority: enrich.example.com
  package: crm
  names:
    singular: enrichment
  properties:
    name:
      type: string
    email:
      type: email
```

The repository's half is the mapping, declared by the package that owns the
kind being described. **The mapping brings the slot**: admitting it
synthesises `person` on `enrichment` — a reference named by `property`, pinned
at `to`, single, `mustExist`, never cascading and `managed: true`
([decision record 0096](decisions/0096-a-mapping-synthesises-its-subject-slot.md)).

```yaml
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: samples.substrate.reamde.dev/people/enrichmentperson
data:
  authority: samples.substrate.reamde.dev
  package: people
  from: enrich.example.com/crm/enrichment
  to: samples.substrate.reamde.dev/people/person
  property: person
  match:
    - from: email
      to: emails
  map:
    name:
      path: name
```

A function that puts an `enrichment` record now contributes `name` to the
linked person: freshest source wins, the value shows as an alternative when
someone holds the property, and deleting the record withdraws it. Writing the
person's property directly remains possible, and is a pin.

State properties are never recomputed: a state moves through its
[declared transitions](data-model.md#validation-and-state-machines) or not
at all, so no amount of syncing can quietly complete a task.

### When the last source goes: the orphan mark

A target minted from a source outlives it. Delete the last GitHub issue that
projected onto a task — or replace the provider, which purges its rows and
mints them again under new ids — and recompute empties the mapped properties,
but the row stays: a husk nothing describes and no probe can ever match. One
re-seed left 2,727 of them on one repository
([#578](https://github.com/geoah/substrate/issues/578)).

The engine marks those rows. A record is **orphaned** when all three hold:
something maps onto its kind, no live record that mapping's `where` covers
links to it through the mapping's subject slot (counted over every id it has
ever had, so a merge does not hide a source), and nothing on it is pinned. A
pinned property is a hand's, and a record a hand has written on is not a
husk, whether that hand was yours or a function's. Releasing the pin, or
clearing the property, makes it one again.

The mark is derived, like an alternative: nothing writes it into the
changelog, a re-link clears it on the next recompute, and a
`repository rebuild` derives it again from the records it replayed. It is read
through the list filter, never through a property:

```http
GET /api/v1/records?filter={"kinds":["~/tasks/task"],"orphaned":true}
```

`substratectl get '~/tasks/task' --orphaned` is the same read. What to do with the list
is yours: delete from it, re-run the sync that should have re-linked them, or
leave it. A deployment may also ask the GC sweep to collect them, which is off
by default and windowed when it is on
([`SUBSTRATE_ORPHAN_GRACE`](operations.md#collecting-orphaned-mapping-targets),
[decision record 0092](decisions/0092-an-orphaned-mapping-target-is-marked-and-collected-on-request.md)).

## Merges

Nothing in the substrate fuses by value: two people holding the same email
address are two records until somebody merges them. Merging is always a
deliberate act, one of the five mutations, and the engine never performs one
on its own. What it does instead is suggest.

`merge(kind, winner, loser)` takes two live records of the **same kind**,
addressed by the (kind, id) pair the kind reference beside the two ids spells
out (an edition is not a bad copy of a work, so a merge across that line is a
category error the engine refuses), and joins them so the winner absorbs the
loser's place in the graph:

- **Nothing is re-pointed.** Every record that pointed at the loser still
  holds the loser's path, and every read of that path resolves to the winner
  through the former-id trail, a source record's subject reference and a
  `referencing` read at the winner included. The winner absorbs the loser's place in
  the graph by resolution, not by rewriting rows.
- **Labels fill gaps**: the winner's stand, the loser's land where the winner
  has none. Annotations move too, colliding keys resolving newest-wins.
- **Properties do not migrate, and neither do their marks.** The winner now
  has more sources pointing at it, so its mapped properties are recomputed,
  through the same yield rules as any sync, which is how a hand edit on the
  winner survives its own merge. Copying values across would freeze a stale
  answer into the winner. A mark stands only beside the value it marks, so a
  pin stays with its value on the record it was pinned on: the loser keeps
  both on its tombstone, and a split returns them. A correction made on the
  loser is set on the winner by hand.
- **States never move.** A merge is not a transition, and nothing in it may
  demote a person or complete a task.
- **The loser is tombstoned, not erased**: a finalizer holds garbage
  collection off it, so the merge stays reversible.

Every merge writes a `recordmerge` record: an ordinary record carrying
`winner` and `loser` references and a `moved` property recording the labels
and annotations the merge moved. That record is what makes the undo
possible.

A merge decided from a stale read can carry the versions it read:
`winnerVersion` and `loserVersion`, each optional, each refusing the whole
merge with a `conflict` when that participant has moved since. A split takes
`ifVersion` on the `recordmerge` record alone, never on the pair, because it
reverts the merge and keeps every edit after it
([the five mutations](api.md#the-five-mutations)).

### Former ids resolve to the winner

Merging means ids move, and a client that cached one must not silently read
stale data. Three guarantees:

- **Any read by a former id returns the canonical record and says so.** The
  response carries `canonicalId`, present only when the id you used was not the
  canonical one, so a stale id self-corrects on its next read instead of
  404ing. The trail is the kind's own: a former id resolves within its kind,
  and another kind wearing the same id is untouched.
- **Trails stay flat.** After A merges into B and B into C, both A and B are
  former ids of C directly: resolution is one lookup, bounded forever.
- **A former id resolves for as long as its winner exists.** A tombstoned
  loser's id stays a former id of its winner until the winner is itself
  deleted and purged; the purge drops the trail with the record, and the id is
  free for a new record after that. A (kind, id) pair held in a note or an
  agent's memory is valid while the record it names is live or tombstoned.

### Split, the undo

`split` reverses one merge, addressed by the `recordmerge` record's own id:

```http
POST /api/v1/split
{"merge": "01jbx9m2qz7k4c5v8n3w6t1hrd"}
```

The loser comes back to life at its own id, the moved labels, annotations and
marks go back where the record says they came from, the former-id trail
drops the alias so the loser's id resolves to the loser again, and both sides
recompute from the source sets they now have. A split
reverts _the merge_, not everything that happened after it: a label or
annotation rewritten since the merge keeps its newer value. The split writes
its own `recordsplit` record, pointing at the merge it undid.

## Merge requests

A `recordmergerequest` is the envelope a suggested merge travels in before
anyone has agreed to it. A function or an app writes one, the owner decides,
and **accepting it is what performs the merge**. Here is one as a
duplicate detector would write it, proposing that a second record of Ada is
the same person:

```yaml
kind: substrate.reamde.dev/core/recordmergerequest
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
    # the record that survives, and the one merged away into it
    winner: samples.substrate.reamde.dev/people/person/9f2k
    loser: samples.substrate.reamde.dev/people/person/x41c
```

The `decision` state is the whole lifecycle: `proposed` until somebody
decides, then `accepted` or `rejected`, each transition stamping `decidedAt`.
Accepting is an ordinary
[state transition](data-model.md#validation-and-state-machines), a patch:

```http
PATCH /api/v1/substrate.reamde.dev/core/recordmergerequest/dupe-9f2k-x41c
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

Nothing ships a duplicate detector today: suggestions come from whatever
[function](functions.md) or app you install to write them (probing for a
shared email address, scoring overlapping names, whatever the matcher
decides), one request per strong candidate. The owner reviews the queue in
the [console](console.md#merge-and-change-requests) and accepts or rejects, with the request's
rationale beside a field-by-field comparison of the two records. Merging without a request is the same mutation driven directly:
a REST post naming the kind and the two ids:

```http
POST /api/v1/merge
{"kind": "samples.substrate.reamde.dev/people/person", "winner": "9f2k", "loser": "x41c"}
```

### The patch request sibling

A `recordpatchrequest` is `recordmergerequest`'s sibling: an app or agent
proposes a change, the owner decides, and accepting applies it atomically,
re-validated against the version it was computed on. It carries an `op`,
`create`, `patch` (the default), or `delete`, so a reviewed write can mint a
new record, edit an existing one, or tombstone one. A create names its target
by `targetKind` and `targetId`, because the record does not exist yet; a patch
and a delete carry the `target` reference. These are the request's own words: the
policy selector that gated the write spells the same act `put` or `patch`, and
a trigger watching it spells it `create` or `update`
([change verbs](changelog.md#change-verbs)). The agent
[`propose` tool](agents.md) emits exactly this request rather than writing the
target directly, which is how a semi-trusted agent contributes a dangerous
verb through review instead of on its own authority.

Three rules keep the review honest. **The reviewed envelope is immutable**:
once a request is proposed, `op`, `targetKind`, `targetId`, `diff` and the
`target` reference are frozen, and a write that would change them is refused, so
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
sees, never a no-op mistaken for done. **A failed accept is one refusal**:
whatever the cause — the diff applies nothing, the target moved or vanished, a
guard or the emit ceiling refused it — the decision answers `409 conflict`
naming that reason, because what conflicts is the decision with the target's
state. It never says "version conflict", which is the caller's own `ifVersion`
losing, and it is never the `422 validation` of a malformed decision, so a
client can branch on the answer.

**The owner may adjust the values before accepting.** The accepting write may
carry `adjustedDiff` beside `decision: accepted`, and the accept applies it
instead of `diff` ([0115](decisions/0115-an-owner-adjusts-a-change-request-on-the-accepting-write.md)).
It takes the same two shapes as `diff`, is admitted the same way (every
property declared and writable, stored in the wrapper form), and is then held
to the same re-validation: the target's version and the no-op check. It
replaces `diff` whole, so name every value to apply: a proposed property it
omits is not applied. The version it checks is the `ifVersion` inside
`adjustedDiff` where it carries one, which lets the owner accept against the
target version they reviewed; otherwise it is the request's `targetVersion`,
and an `ifVersion` inside the proposed `diff` is not used. `diff` keeps what was proposed, so the request records both what the
proposer asked for and what the owner applied, and the proposing thread's
`proposalDecision` message carries `adjustedDiff` too. It is written on that one write
only: a proposal, a rejection or a later write that carries it is refused, it
is frozen once written, a delete request has no values to adjust, and only the
owner's hand adjusts (installed code and the policy judge answer `403`).

```http
PATCH /api/v1/substrate.reamde.dev/core/recordpatchrequest/r41c
{"ifVersion": 1, "properties": {"decision": "accepted",
  "adjustedDiff": {"properties": {"priority": "urgent", "dueAt": "2026-10-01T17:00:00Z"}}}}
```

Next: [the API](api.md), the surface every one of these operations rides.
