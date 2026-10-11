---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0155. An observed value names what it is about with an `about` reference

## Context and problem statement

In health the thing observed is always the user, so version 1 of the plan had
no slot for it ([health-data.md](../plans/health-data.md)). A house has
dozens: a living-room temperature is about the room, a fuel level about the
car, a child's weight about the child. Every observation standard has the
slot (SOSA's feature of interest, FHIR's subject). The `observed` trait cannot
gain a property after it ships
([0154](0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md)),
so the slot's name and place are settled before the first version
([the plan, section 4](../plans/observations.md)).

## Considered options

- `about`, an optional reference on the value pinned to `kind: any`, in the
  index key
- The same reference named `subject`, the word FHIR and SOSA use
- The thing on the metric or the source record instead of on the value
- A generic subject kind, or a marker trait, that rooms, cars and people all
  are
- A `location` property on every value

## Decision outcome

Chosen: `about` on the value, pinned to any kind, absent meaning the
repository's owner.

`about` is optional and never cascades: deleting a room leaves its readings.
It is on every observation and trace and in the index key
([0160](0160-the-observed-index-is-three-derived-tables-and-a-rollup-serves-only-exact-answers.md)),
so a filter to one room, grouping by room and "hours the door was open" read
rollups instead of raw points. It is on the value, not the metric or the
source, because one metric is observed about many things, a sensor moves
between rooms, and the engine never reads another record to know what an
indexed number means. It is not `subject`, because `subject: true` already
marks a mapping's reference
([0049](0049-the-owner-of-a-mappings-target-declares-it.md),
[0096](0096-a-mapping-synthesises-its-subject-slot.md)).

The thing observed is a typed kind in its domain sample (a `home/area`, a
`home/fixture`, a vehicle, a plant), never a generic subject kind. A kind
that every thing is, or a trait with no properties that rooms, cars and
people all bind, is "any record" under another name, and `kind: any` already
stores the full record path the index needs.

There is no `location` property on a value. A fixed sensor's place is a fact
about the thing it observes, held on that record. A moving source's position
is itself an observation, kept out of the index for now
([0163](0163-positions-forecasts-and-money-stay-out-of-the-observed-index.md)).
A weather reading is `about` a place record. A `location` on every point
would copy the room onto millions of rows and go stale when the sensor moves.

### Consequences

- Good, because one metric and one source serve every thing observed, and a
  moved sensor changes only the readings written after the move.
- Good, because health writes no `about` and the owner is implied.
- Bad, because `about` widens the index key, so a house with many things has
  many streams (index keys, defined in 0160) and many rollup rows.
- Bad, because `kind: any` admits a pointer at a record that makes no sense
  (a task); only the writer's code prevents it.
- Bad, because grouping by a property of the thing (every room on the first
  floor) is a join the aggregate read does not do: a function resolves the
  rooms first and groups by `about`.

### Confirmation

Tests the work package 2 and 3 implementers will write:
`TestObservedAboutAdmitsAnyKind` writes readings about records of two
unrelated kinds and about nobody; `TestObservedAboutIsInTheStreamKey` writes
one source's readings about two rooms and reads them back from
`observed_rollups` as two streams. That no base kind declares `location` is
held by review only.

## More information

[The plan, section 4](../plans/observations.md) argues `about` and the
missing `location`; [the implementation brief](../plans/observations/implementation/README.md)
settled `about` over `subject`. Revisit if a second person in health needs
the owner named explicitly rather than implied by an absent `about`.
