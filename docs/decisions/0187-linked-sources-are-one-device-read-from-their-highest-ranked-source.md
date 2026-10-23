---
status: proposed
date: 2026-10-23
decision-makers: George Antoniadis
---

# 0187. Linked sources are one device, read from their highest-ranked source

## Context and Problem Statement

A scale that syncs to its maker's app and to Apple Health reaches a
repository as two `source` records, one per adapter
([0162](0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)).
The aggregate read's `priority` policy drops a lower source's value only
where it overlaps a higher one's, so a weigh-in one route stamps a few
seconds early counts twice ([#986](https://github.com/geoah/substrate/issues/986)).
0162 rules out matching values by close times, so the repository needs an
explicit statement that two sources are one device, and a read rule for it.

## Considered Options

- A core `sourcelink` trait (a list of `sources`) and an `observations`
  sample kind binding it; under `priority`, a linked device answers each
  metric and `about` from its highest-ranked source alone
- A new property on core's `source` trait naming the device a source belongs
  to
- A `device` reference on the observed value
- Deduplication of near-equal values across sources

## Decision Outcome

Chosen: the `sourcelink` trait and sample kind, because it adds a trait and a
kind without changing one a repository already imported.

- `substrate.reamde.dev/core/sourcelink` contracts `sources`, a list of
  references. The loader holds the list shape. The read takes links from
  every live record of a kind binding it, and an entry whose kind does not
  bind `source` links nothing.
- `samples.substrate.reamde.dev/observations/sourcelink` binds it, with a
  `name` and `sources` pinned to the `source` trait.
- Links join transitively: two links sharing a source make one device.
- Under `priority`, before any interval is resolved, the read groups the
  selected streams of one device by metric and `about`, keeps the
  highest-ranked source's streams (the request's `order`, else the sources'
  `priorities`, the stream order breaking a tie) and drops the other linked
  sources' streams whole. `separate` ignores links.
- A caller holding a reads allowlist needs every kind binding the trait
  granted for a priority read, `order` or none, as it needs the source kind
  for a ranking by `priorities`.

A property on `source` would refuse every imported kind binding the trait at
the next boot, the reason `observed` never grows
([0154](0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md)).
A reference on the value cannot change: `observed` never gains a property.
Near-equal matching is what 0162 rejected.

### Consequences

- Good, because one weigh-in delivered by two routes counts once under
  `priority`, and the owner decides which sources are one device.
- Good, because a link is an ordinary record: deleting it restores the old
  answer, and no stored value changes.
- Bad, because the losing route's values leave the read whole, so a reading
  only that route delivered is not counted under `priority` while the
  winning route has any value of the metric and `about` in the read.
- Bad, because a function or an agent with a reads allowlist that already
  read under `priority` with an `order` is refused until it grants the link
  kind.
- Bad, because the records-mode and property-read source policies, which
  match records by a property, do not read links.

### Confirmation

`TestAggregatePriorityTakesALinkedDeviceFromOneSource` holds the read rule,
transitivity and deletion;
`TestAggregateRankingBySourcePrioritiesHoldsTheSourceKind` holds the
allowlist; `TestObservationsSampleKindNamesAreFrozen` holds the kind name.

## More Information

Revisit if an upstream starts carrying a shared device identity that
adapters can write onto `source` themselves.
