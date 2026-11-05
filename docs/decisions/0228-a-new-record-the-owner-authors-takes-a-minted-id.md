---
status: proposed
date: 2026-11-05
decision-makers: George Antoniadis
---

# 0228. A new record the owner authors takes a minted id, a UUIDv7 in 26 Crockford characters

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

Some writers must keep naming records. A provider's sync composes ids from
upstream keys so a replay converges on the same rows
([0162](0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)),
a seed or sample ships records with ids, and an `internal` kind's id is the
name the substrate resolves by (a unit's spelling, the `openai` provider
row, an input's `default`).

## Considered Options

- Refuse every caller-chosen id on create; only the server mints.
- Refuse a caller-chosen id on create of an owner-authored record unless it
  has the minted shape; keep composed ids where the id is derived from data.
- Keep accepting any id, and fix the apps that slug.

## Decision Outcome

Chosen: the second.

- **A minted id is a UUIDv7 written as 26 lowercase Crockford base32
  characters**, the suffix form of a TypeID (`01h2xcejqtf2nbrexx3vqjhp41`).
  It is the size of a ULID, sorts by creation time, has no `i`, `l`, `o` or
  `u`, needs no case, and decodes back to a standard UUID. Base58 and NanoID
  are shorter but case-sensitive, and NanoID is not a UUID.
- **A create of a kind the repository installed** (an imported sample, a
  kind the owner declared), **written by a person, an app
  or an agent, takes a minted id.** Omitting the id lets the server mint one;
  an id the writer minted in the same shape is accepted, so an offline
  outbox knows a record's id before the server answers. Any other id is
  refused with `422 validation`. The check is on the shape (26 Crockford
  characters, the first `0` to `7`), not the UUID version: only an id the
  server mints is guaranteed to be a UUIDv7.
- **A create request is judged when it is written, against the hand that
  proposes it,** because its accept runs under the owner's actor. A gated
  create or a `propose create` with no id gets a minted target id at once
  (a gate derives it from the gate key, so a retry names the same record);
  a person's or an agent's slug is refused at the request; a function's
  composed id is kept, and the owner's accept lands it as proposed.
- **Updating a record by its id is not naming it.** `PUT` at an existing id,
  a restore onto a tombstone, and `substratectl apply` of a fetched record
  are unchanged, whatever the id looks like.
- **Writers that derive ids keep naming records:** the engine and the host's
  other hands, a bundle's install, and a function's effects. So do kinds the
  substrate seeds or a provider publishes, whose rows mirror an upstream
  (the Apple sync app writes `providers.substrate.reamde.dev/apple/*` rows
  under Apple's identifiers), and `internal` kinds.
- A kind some `recordmapping` points at stays server-assigned for every
  writer, as before
  ([0049](0049-the-owner-of-a-mappings-target-declares-it.md)).

Refusing every chosen id would break offline creation in the apps and every
provider's idempotent sync. Fixing only the apps leaves every agent and
script free to slug again.

### Consequences

- Good, because a record's id never describes its content, so a rename
  never makes it wrong.
- Good, because ids sort by creation time and convert to a UUID for any
  system that wants one.
- Bad, because ids grow from 12 characters to 26.
- Bad, because a script that imports data with its own stable keys under
  the owner's token can no longer use them as ids: it keeps the key in a
  property, or runs as a function.
- Bad, because a package an app publishes through the catalog lands as
  `published`, like a provider's, so its kinds are exempt: the Workouts
  app's `substrate.reamde.dev/training` routines can still be created under
  slugs. Telling an app's authored kinds from a provider's mirror kinds
  needs a signal the declaration does not carry yet; that is an open
  question on this record.
- Bad, because a `substratectl apply` of a new record that carries
  `metadata.id` is refused until its writer drops or mints the id. Records
  already stored keep their ids.

### Confirmation

`TestACreateOfAnInstalledKindTakesAMintedID` (`internal/engine`) covers each
actor and kind class; `TestAnAgentWriteCreatesUnderAMintedID`,
`TestAGatedCreateMintsItsTargetAndTheAcceptLandsIt` and
`TestAProposedCreateTakesAMintedTarget` cover the agent and request paths
under the production rule; `TestNewIDIsAUUIDv7InCrockfordBase32` and
`TestEncodeCrockfordKnownValues` pin the encoding against the TypeID
specification. The console's create editor refuses the same ids before the
write (`serverAssignsID` in `web/console/src/lib/record-yaml.ts`).

## More Information

The rule is `checkCreateID` in `internal/engine/write.go`, the shape
`vocabulary.MintedID`, and the actor test `substrate.NamesItsRecords`.
Revisit if a writer outside functions and bundles needs composed ids on an
installed kind.
