---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0159. The base observation kinds live in a sample every domain sample requires

## Context and problem statement

Four kinds, `metric`, `observation`, `trace` and `source`, are shared by every
domain that observes things (health first, then home, weather, vehicles).
Where they live is fixed by the first record written: `movedFrom` carries
rows only on the shipped boot upgrade of a seeded package
([0078](0078-a-kind-move-is-ordinary-record-writes.md)), so moving a sample's
kind later takes a repository migration
([0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md))
in every repository that imported it
([the plan, section 7](../plans/observations.md)).

## Considered options

- A seeded package, `substrate.reamde.dev/observations`
- The four kinds in core
- One `observations` sample with every domain inside it
- A copy of the four kinds in each domain sample
- A small base sample, `observations`, that each domain sample requires

## Decision outcome

Chosen: the small base sample every domain sample requires.

`samples.substrate.reamde.dev/observations` holds the four kinds and the
physical metrics several domains share (`air_temperature`,
`relative_humidity`, `energy`, `power`, `illuminance`,
`atmospheric_pressure`). Each domain sample, `health` first, requires it with
a `requiresAtLeast` floor
([0070](0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md),
[0100](0100-a-requires-floor-binds-a-copy-not-a-hand-declared-package.md)),
and ships its own metrics under package-prefixed ids (`health/heart_rate`), so
two domains never write one row.

The four kind names are frozen before the first import. A domain never
edits a base kind: health-only context is a `qualifiers` code, and
health-only definition data is a health kind pointing at the metric
(`health/analyte`). Retiring a base kind or property name follows
[0055](0055-a-retired-name-is-declared-and-never-inferred-from-a-prune.md).

A seeded package refuses a repository's own writes, so nobody could add a
property to `observation`, a bad declaration there stops every repository
from opening, and the engine would address kinds
([0077](0077-the-llm-kinds-live-in-their-own-seeded-package.md)). Core holds
what exists because the substrate exists, and unproven kinds stay out of the
stable set ([0015](0015-unproven-kinds-stay-out-of-the-stable-set.md)). One
sample for every domain has one version line, so a weather fix offered as an
upgrade discards the owner's edits to health. A copy per domain means two
`metric` kinds, two `air_temperature` records and no rule that they are one.

### Consequences

- Good, because the base stays small, its floors rarely bind, and
  re-importing a domain discards only that domain's edits.
- Good, because the user owns the imported copy and may add a property to a
  base kind.
- Bad, because a property the user adds to a base kind is lost at the next
  base upgrade.
- Bad, because a domain's shipped `metric` rows survive the domain's
  removal.
- Bad, because renaming a base kind after the first import is a repository
  migration in every repository, which the frozen names exist to avoid.

### Confirmation

Tests the work package 2 implementers will write:
`TestObservationsSampleKindNamesAreFrozen` pins the four names under
`samples/observations`, and `TestHealthRequiresObservations` holds the
`requiresAtLeast` floor. `mise run kinds:check` refuses a changed declaration
without a version bump. That a domain never edits a base kind is held by
review only.

## More information

[The plan, sections 7.1 to 7.3](../plans/observations.md) has the option
table and the domain list, and the design assessment's section 3
([04-design-assessment.md](../plans/observations/04-design-assessment.md))
argues it in full. The review's twelfth finding softened "a sample's kind can
never be moved" to "a move is expensive"; the names stay frozen for that
cost. Revisit if the catalog's upgrade gains a way to carry a kind move.
