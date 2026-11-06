---
status: accepted
date: 2026-11-05
decision-makers: George Antoniadis
---

# 0228. A person, an app or an agent creates a record under a minted id, a UUIDv7 in 26 Crockford characters

## Context and Problem Statement

A writer could create any record under an id it chose, so apps, agents and
people named records after their titles. The Workouts app slugs a routine's
name into its id (`RoutineID.slug(for:)`), and the routine
`substrate.reamde.dev/training/routine/floor-first-full-body` kept that id
after it was renamed: the id now describes a routine that no longer exists.
A slug also collides: two routines named alike get `-2` suffixes, and a
second writer picking the same slug lands on the first one's record. Minted
ids were 12 base32 characters (60 random bits), with no time order and no
standard form.

Some ids are names, and must stay so. A provider's sync function composes
ids from upstream keys so a replay converges on the same rows
([0162](0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)),
a seed or sample ships records with ids, and some kinds are found by their
ids: a metric by `health/body_mass`, a unit by its spelling, a provider row
by `openai`, an input's record by `default`.

## Considered Options

- Refuse every caller-chosen id on create; only the server mints.
- Refuse a chosen id from a person, an app or an agent on every kind but an
  `internal` one, unless it has the minted shape.
- The same, limited to kinds the repository installed, with seeded and
  provider-published kinds exempt as mirrors.
- The second, with each kind declaring whether its ids are minted or named.
- Keep accepting any id, and fix the apps that slug.

## Decision Outcome

Chosen: the fourth.

- **A minted id is a UUIDv7 written as 26 lowercase Crockford base32
  characters**, the suffix form of a TypeID (`01h2xcejqtf2nbrexx3vqjhp41`).
  It is the size of a ULID, sorts by creation time, has no `i`, `l`, `o` or
  `u`, needs no case, and decodes back to a standard UUID. Base58 and NanoID
  are shorter but case-sensitive, and NanoID is not a UUID.
- **A kind declares who names its records:** `ids: {type: minted}` or
  `ids: {type: named}`. Absent, an `internal` kind reads as `named` and
  every other kind as `minted`. The key is a mapping so a later key (a
  function that generates the ids) joins `type` without a new shape. The
  seeded `observations/metric` declares `named`, and so does every mirror
  kind of the Apple provider, whose ids are the Sync app's hashes of Apple's
  identifiers (`apple-<32 hex>`): never a title, so they cannot go stale, and
  every row a device already pushed keeps its id.
- **A person, an app or an agent creates a record of a minted kind under a
  minted id.** Omitting the id lets the server mint one. An id the writer
  computes in the same shape is accepted: a fresh UUIDv7, so an offline
  outbox knows a record's id before the server answers, or one derived from
  an upstream key (`vocabulary.DerivedMintedID`, a UUIDv8 of the key's
  sha256), so a mirror that re-syncs names the same row. Two spellings
  pass: the server's 26 Crockford characters, and a UUID's canonical
  lowercase text, which the apps already mint (a Workouts session id is
  also its HealthKit workout's sync identifier, and builds that have not
  updated keep sending them). Any other id is refused with
  `422 validation`. The check is on the shape, not the UUID version: only
  an id the server mints is guaranteed to be a UUIDv7.
- **Code that derives ids from data names records of any kind:** the engine
  and the host's other hands, a bundle's install, and a function's effects.
  A bundle closure's data documents keep the ids the package ships whoever
  installs it. A function run as an agent's tool writes as the agent, and
  derives a minted-shape id with `host.ids.minted`.
- **A create request is judged when it is written, against the hand that
  proposes it,** because its accept runs under the owner's actor. A gated
  create or a `propose create` with no id gets a minted target id at once
  (a gate derives it from the gate key, so a retry names the same record);
  a person's or an agent's slug is refused at the request; a function's
  composed id is kept, and the owner's accept lands it as proposed.
- **Updating a record by its id is not naming it.** `PUT` at an existing id,
  a restore onto a tombstone, and `substratectl apply` of a fetched record
  are unchanged, whatever the id looks like. A declaration is named by its
  identity and is outside the rule.
- A kind some `recordmapping` points at stays server-assigned for every
  writer, as before
  ([0049](0049-the-owner-of-a-mappings-target-declares-it.md)).

Refusing every chosen id would break offline creation in the apps, every
provider's idempotent sync and every name-keyed kind. Exempting seeded and
provider-published kinds also exempts every package an app publishes
through the catalog, which lands as `published` like a provider's, so the
Workouts routines would stay open to slugs. Inferring named ids from
`purpose` alone fails on metrics, which an owner browses (`primary`) and
which are found by name. The kind is the one place that knows whether its
ids are names, as it is for its purpose
([0133](0133-a-kind-declares-its-purpose.md)).

### Consequences

- Good, because a record's id never describes its content, so a rename
  never makes it wrong.
- Good, because ids sort by creation time and convert to a UUID for any
  system that wants one.
- Good, because a kind found by name says so in its declaration, and a later
  key can name a generator without a new shape.
- Bad, because ids grow from 12 characters to 26.
- Bad, because the key set is closed: a binary older than this one that
  opens a repository whose stored declarations carry `ids` quarantines the
  package that ships it, the observations package among them. It is the
  cost [0020](0020-dialect-keys-are-reserved-not-tolerated.md) names for
  every new key, and the rollback path is the newer binary.
- Bad, because an app that slugs titles into ids must change before it meets
  this binary: the Workouts app mints its new routines and exercises,
  derives its library and sample ids, and derives its Hevy imports' ids.
  Rows it already wrote keep their ids, and a PUT at one of them is an
  update.
- Bad, because a client id has two accepted spellings, so a reader that
  recognises a minted id checks both.
- Bad, because an id minted before this record (12 characters of `a-z2-7`)
  does not have the minted shape: a person cannot create a record under one
  elsewhere, such as a copy into another repository by `get -o yaml` and
  `apply`. The copy is created without an id, or written by a function.
  Accepting the old shape would admit 12-letter words (`weeklyreview`).
- Bad, because the rule holds a direct write alone: a function the owner
  declares writes under any id, as 0162 needs, and so does an app whose
  approval installs a package with one.
- Bad, because a script that imports data with its own stable keys under
  the owner's token can no longer use them as ids on a minted kind: it
  derives a minted id from the key, keeps the key in a property, or runs as
  a function.

### Confirmation

`TestANewRecordTakesAMintedID` (`internal/engine`) covers each actor and
kind class, a named metric included; `TestAnAgentWriteCreatesUnderAMintedID`,
`TestAGatedCreateMintsItsTargetAndTheAcceptLandsIt` and
`TestAProposedCreateTakesAMintedTarget` cover the agent and request paths;
`TestIDsReserved` holds the key's grammar and default;
`TestNewIDIsAUUIDv7InCrockfordBase32`, `TestEncodeCrockfordKnownValues`,
`TestMintedIDShape`, `TestDerivedMintedID` and `TestIDVectorsMatchPython`
pin the encoding against the TypeID specification and `host.ids.minted`. No
engine test opens a way around the rule: fixtures use minted or derived ids.
The console's create editor applies the same rule before the write
(`serverAssignsID` in `web/console/src/lib/record-yaml.ts`).

## More Information

The rule is `checkCreateID` and `createIDRefusal` in
`internal/engine/write.go`, the key `Kind.IDType` in
`internal/vocabulary/ids.go`, the shape `vocabulary.MintedID`, and the actor
test `substrate.NamesItsRecords`.
