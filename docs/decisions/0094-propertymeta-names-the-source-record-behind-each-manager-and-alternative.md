---
status: accepted
date: 2026-09-19
decision-makers: George Antoniadis
---

# 0094. `propertyMeta` names the source record behind each manager and alternative

## Context and Problem Statement

A single-record read says who holds each property (`manager`, an actor) and
what every other live source says instead (`alternatives`, one per actor).
Both are actors, and an actor is a writing hand, not a record: eight Beeper
users converging on one person are one actor
(`function:providers.substrate.reamde.dev:beeper:beepersync`), so a reader
looking at "Beeper says *Ada L.*" cannot tell WHICH of the eight said it, and
the console had nothing to link. Owner's review of a person eight provider
rows converged on, 2026-09-19: the provenance tab "does not link the manager
or the alternatives to the source record each came from". The console could
guess the record from the mapping and the alternative's `updatedAt`, which is
the source record's stamp, but two sources written in one sync share a stamp,
and a guess that is right most of the time is a wrong link some of the time.

## Considered Options

- Resolve it client-side: match the alternative's `updatedAt` against the
  `linkedFrom` entries' records, read one at a time.
- Store the source on the offer row, and serve it on every alternative and,
  where the read can stand behind it, on the manager.
- Record the source in the changelog entry a recompute writes, so the manager
  row itself carries it.

## Decision Outcome

Chosen: the second. `property_offers` gains a `source` column, the record
path of the live source the offer's value and stamp are read from (for an
atomic property the record whose value it is; for a union property the latest
of that actor's sources carrying the property, which is also where the stamp
comes from). The single-record read serves it as `source` on every
alternative, and on the manager when the manager holds at the machine tier
and its own actor's offer BACKS the stored value: equal to it, or, on a union
property, every item of it among the stored items.

It beat the client-side guess because the engine already knows the answer at
the moment it derives the offer (`syncOffers` selects one candidate and
takes its stamp), and a stamp collision would make the guess lie. It beat the
changelog route because `property_offers` is DERIVED storage (rebuild.go
`rederiveOffers` recomputes the table from the fold) and a source path on a
derived row costs no migration of the truth, no fold rule and no dialect
bump, whereas a new field on the changelog entry is all three; and because the
manager's source is a READING of the derived row, not a fact the changelog
owes anyone: a hand edit and a bundle pin have no source, and a machine-held
value whose offer no longer matches it has none either, which is the honest
answer.

Four rules make it precise.

1. **The column is derived, like the row.** Recompute writes it, nothing in
   the changelog carries it, a rebuild fills it for every live target. A row
   minted before the column existed is `''` until its target next
   recomputes, and the wire omits an empty source rather than inventing one.

2. **The manager's source is conditional.** It is set only where the
   manager's tier is `machine` and the manager actor's own offer backs the
   stored value. On a union the holding actor's offer is a subset of the
   stored union; it backs the value and is not listed as an alternative to
   itself. No other alternative's listing changes.

3. **A kind grant covers the source.** A source path names a record of
   ANOTHER kind, so the runner's and the agent loop's `reads` allowlist blank
   the paths of kinds the body is not granted (`substrate.MetaWithinKinds`),
   on the manager and on each alternative, exactly as they filter
   `linkedFrom` ([0088](0088-a-single-record-read-carries-its-inbound-mapping-owned-links.md),
   rule 5). The offer's value and actor stay, as they were before.

4. **Lists never carry it**, because lists never carry `propertyMeta`.

### Consequences

- Good, because the console links every alternative and every machine-held
  value to the record it came from with no second read: the ledger's source
  pills are titled off the `linkedFrom` entries the same read carries.
- Good, because nothing about the truth changed: no fold rule, no changelog
  field, no dialect stamp. A rebuild derives the same table, `source`
  included, and the rebuild fingerprint now holds that.
- Bad, because a manager's source is absent for a while after an upgrade,
  until each target recomputes or the operator rebuilds; the console shows
  the manager without a source pill in that window rather than a wrong one.
- Bad, because a machine-held union property with several contributing
  actors names ONE source, the manager's latest, though the stored value is
  the union of all of them. The other contributors are visible as the
  Sources section's members; the ledger names where the manager's own items
  came from.

### Confirmation

`TestPropertyMetaNamesTheSourceRecordBehindEachOfferAndTheManager`
(`internal/engine`) holds rules 1 and 2: two mirrors under an owner hold
answer two alternatives each naming its record and no manager source; a
release names the winning mirror on the manager and leaves the other as the
one alternative; a union names the latest contributor and lists no
contributor as an alternative. `TestGetFiltersInboundLinksByTheAllowlist`
(`internal/runner`) holds rule 3. The wire golden
(`internal/substrate/wire_test.go`, `web/console/src/lib/api/wire.golden.test.ts`)
pins the two new optional fields, and the console's `ProvenanceRail` vitest
holds the ledger that reads them.

## More Information

Follows [0088](0088-a-single-record-read-carries-its-inbound-mapping-owned-links.md),
whose `linkedFrom` is the list this field points into.
[projection.md](../projection.md#reading-provenance-propertymeta) is the page.
Reopen if a manager's source needs to be a fact the changelog owes — a
`repository verify` that checks it, say — which is the third option this
record declined.
