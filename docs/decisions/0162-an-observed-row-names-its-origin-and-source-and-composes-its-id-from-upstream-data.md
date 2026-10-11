---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0162. An observed row names its origin and source, and composes its id from upstream data

## Context and problem statement

One watch's heart rate can reach a repository through Apple Health and
through Health Connect, and a WHOOP recovery is re-synced every hour. The
review of 2026-10-10 found that choosing between sources is not the same as
knowing two rows are one measurement (finding 8), and that traces fed by
several providers and corrections to the latest value were underspecified
(finding 13) ([the plan, section 13](../plans/observations.md)). A re-sync,
a rescore and a stale page must converge on the same rows, never duplicate
them or move them backwards
([data flows](../plans/observations/proposal/01-data-flows.md)).

## Considered options

- `origin` and `source` on every row, ids composed from the upstream
  identity, deduplication only on an exact identity, one origin provider per
  trace, and a `revision` that never falls
- Minted ids, with a lookup by upstream id before each write
- Hashed ids (`host.ids.external`) for every row
- Deduplication across providers by a near-equal timestamp and value

## Decision outcome

Chosen: the first.

- An observation carries `origin`, a reference to the mirror row that
  delivered it, and `source`, the device that produced it. A trace names its
  mirror rows in `origins`. The source is the device; the origin is the
  delivery.
- Ids are composed from source data, never minted:
  `<provider>/<account record id>/<upstream id>[/<role>]` for a row derived
  from one upstream object (`whoop/george/1047392/recovery`), and
  `<provider>/<account record id>/<metric>/<period start>` for a trace. The
  id alphabet admits `/`, `:` and `@` up to 128 characters
  (`internal/vocabulary/naming.go`); only a segment outside the alphabet, or
  an id past the limit, is hashed with `host.ids.external`, and the hash
  replaces that one segment.
- Two rows are one measurement only when their upstream identity is exactly
  equal. Nothing deduplicates on a similar timestamp or value: two
  deliveries of one device without a shared identity are two sources, and
  the read's source policy chooses between them
  ([0161](0161-the-aggregate-read-is-a-mode-of-the-records-route-checked-before-it-runs.md)).
- One trace per metric, source, `about`, `channel` and period has exactly one
  origin provider, so no trace mixes deliveries from two providers.
- `revision` is the upstream's `updated_at` or version, and it never falls: a
  rewrite with a lower revision than the stored one is refused, so a stale
  page cannot overwrite a newer result. An upstream change raises it. The
  adapter's own version is stamped apart from `revision`, so an adapter fix
  can rewrite output the upstream did not change.
- A delivery writes the complete set its mirror row implies and deletes the
  earlier outputs of that origin it no longer implies, found by
  `referencing` the origin.

Minted ids need a lookup before every write and duplicate a row when two
deliveries race. Hashing every id makes ids unreadable when a readable one
fits. Near-equal matching merges two measurements that happened to agree,
and nobody can undo it from the stored rows.

### Consequences

- Good, because an identical re-sync converges on the same ids, and a stale
  delivery cannot regress a newer result.
- Good, because an id says where its row came from.
- Bad, because one device reached through two providers without a shared
  identity is two sources, and `sources: separate` shows it twice until a
  policy picks one.
- Bad, because an upstream without a stable id needs a recipe the adapter
  owns, and changing that recipe changes every id it made.
- Bad, because an equal revision is admitted, so a faulty adapter can
  overwrite a value at the same revision, which is also how an adapter fix
  works.
- Bad, because reconciliation runs a `referencing` read per delivery.

### Confirmation

Tests the work package 2 and 5 implementers will write:
`TestObservedWriteRefusesAFallingRevision` (internal/engine), and per adapter
against its mock upstream, an identical re-sync that changes nothing, a
rescored record that converges, a stale delivery that is refused and an
upstream deletion that removes its outputs, the phase 1 checks in
[review and decisions](../plans/observations/proposal/05-review-and-decisions.md).

## More information

Version 1's [section 7](../plans/health-data.md) has the id recipe and
per-origin reconciliation; [the unified view](../plans/observations/proposal/02e-unified-view.md)
works one device through two providers. The data flows page describes a
correction as a write with a higher `revision`; this record also admits an
equal one, because the review's thirteenth finding needs an adapter fix to
rewrite output under an unchanged upstream revision, and phase 1 needs an
identical re-sync to change nothing.
