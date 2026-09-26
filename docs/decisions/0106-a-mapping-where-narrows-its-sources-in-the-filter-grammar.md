---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the #581 agent session)
---

# 0106. A mapping's `where` narrows its sources in the filter grammar

## Context and Problem Statement

A `recordmapping` covered every record of its source kind, so a repository
that wanted only some of them projected (the open pull requests, the ones
awaiting the owner) had to narrow the provider's sync query inside a
bundle's settings, fork the mirror kind, or replace the mapping with a
trigger ([#581](https://github.com/geoah/substrate/issues/581), mneme-v6
T-013). The population of a mapped kind was decided outside the manifest
the repository owns.

## Considered Options

- Leave it to the sync query and filter at read time
- A `where` in a new predicate language of its own
- A `where` taking the filter grammar's condition objects, evaluated by the
  filter compiler against the source row
- A `where` in CEL, as trigger guards are written

## Decision Outcome

Chosen: a `where` map from a declared property of the source kind to one
filter condition object, all of which must hold, compiled by the records
route's own filter code (`query.go` `condProp`) and asked of the source row as
a one-row relation. It is the grammar every client already writes for
`filter.properties`, so a condition means on a mapping exactly what it means
on a list, including reference paths, the former-id trail and the operator
refusals; a second language would drift from it, and CEL would be a third
spelling of the same predicates.

A record outside the `where` is treated as a tombstoned source: its write
resolves and mints no subject and is not marked ambiguous, recompute reads
nothing from it, the orphan mark counts it as no source, and the subject hop
refuses it when it holds no link. Its stored subject pointer is KEPT, because
only merge and split move a subject slot, which also means a
record that comes back inside projects onto the same subject. A source slot
declared `required:` cannot carry a `where`, since a record outside it could
not be written at all.

### Consequences

- Good, because which records project is a diffable line in the mapping,
  owned by the package that owns the target (record 0049).
- Good, because narrowing a live mapping releases what it no longer covers in
  the apply, through the existing recompute of a changed mapping.
- Bad, because a record a widened `where` newly covers resolves only on its
  next write; nothing re-resolves the unlinked rows in the apply.
- Bad, because every source write under a mapping with a `where`, and every
  recompute over such sources, pays one extra query per source row.
- Bad, because a kept pointer means `linkedFrom` still lists a record the
  mapping no longer covers.

### Confirmation

`internal/engine/mappingwhere_db_test.go` holds the behavior (resolution,
release and orphan mark, return to the same subject, the apply-time
recompute, the hop refusal, the apply-time compile check);
`TestMappingWhere` in `internal/vocabulary` holds the loader's rules.

## More Information

Reopen this if a `where` needs to read something other than the source row's
own properties (another record, labels), or if the per-row query shows up in
a sync's cost.
