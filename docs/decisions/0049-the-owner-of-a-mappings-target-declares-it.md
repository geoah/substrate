---
status: accepted
date: 2026-09-02
decision-makers: George Antoniadis
---

# 0049. The owner of a mapping's target declares it and pins the subject

## Context and Problem Statement

A `recordmapping` says how a source record's properties reach the subject one
of its references names ([projection](../projection.md)). Three rules make it
the source's to declare: `parseMapping` (`internal/vocabulary/mapping.go`)
refuses a mapping whose `from` kind is not declared in the mapping's own
package, `resolveMapping` requires the source's `subject: true` reference to be
pinned exactly at `data.to` and marked `required`, and
`mappingInvariantProblems` allows one mapping per source kind. So only Linear
may say where a Linear issue projects, and only once. Under the two catalog
tiers ([0048](0048-providers-are-published-samples-are-copied.md)) a provider
names no user kind and a sample's kinds land under the repository's own
authority, which leaves the mapping from
`providers.substrate.reamde.dev/linear/issue` to `ada.example.com/tasks/task`
with no author at all.

## Considered Options

- Keep declaration with the source's package, and have each provider ship one
  mapping per user vocabulary it knows about
- Let any package declare any mapping
- Move declaration to the owner of `to` and change nothing else
- Move declaration to the owner of `to`, key one-per-source on (source kind,
  subject property), and let a mirror leave its subject reference unpinned and
  optional

## Decision Outcome

Chosen: the fourth. Moving declaration alone (the third option) fails because
a mirror that still pins its subject decides the target the user was supposed
to choose, and a single mapping per source kind lets one mirror reach only one
of the user's kinds. Item 4 below is the uninstall rule that follows from a
mapping and its source living in different packages.

1. **A mapping onto a kind is declared by the package that owns that kind.**
   Ownership is exclusive: the owner of `to` prevails, and the owner of the
   source may declare a mapping only when it owns both ends, which the loader
   already permits. Providers therefore
   ship no mappings, and a third package may declare none. `parseMapping`
   admits a foreign `from` on that condition and resolves the source kind at
   Finalize/Install, the way a `to` pin already resolves.

2. **One mapping per (source kind, subject property).** One mirror kind may
   project onto two user kinds through two `subject: true` references, and two
   mappings through one reference stay refused.

3. **A mirror kind declares its subject reference unpinned and not required.**
   `resolveMapping`'s checks that the reference is pinned exactly at `data.to`
   and carries `required: true` become: pinned at `data.to` or unpinned, and
   required only where the declaring package pins it. The user's mapping is
   what pins the target. `mustExist`, single-valued and never-cascading stay.

4. **Uninstalling a package whose kinds another package's mapping names is
   refused**, naming the mapping, symmetric with `requires` refusing an install
   when the package it names is absent. Removing Linear while the user's
   mapping reads `linear/issue` fails until the mapping goes.

The bipartite rule stays: a mapping's `to` is never any mapping's `from`, and
no reference anywhere may name a mapped source kind, so resolution stays one
hop deep. The projection ALGORITHM is unchanged (match probes, shell mint on
zero or several candidates, recompute where a write above the machine tier
survives), but the projection engine is not: `Registry.MappingFor(from)`
returns the single mapping of a source kind and six call sites read it as the
one answer. `subjectOf` (`internal/engine/mapping.go`), `srcMapping`, the
delete-time recompute and `preRecordLocks`' `subject|<to>` lock key
(`write.go`), the subject hop (`references.go`) and the effect batch's lock
planning (`effects.go`) all take a set keyed by (source kind, subject property)
instead.

Letting any package declare any mapping was rejected because it lets a third
party repoint records between two vocabularies it does not own, with nobody to
ask.

### Consequences

- Good, because a provider ships mirrors, syncs and OAuth metadata and nothing
  that names a user kind, which is what 0048 requires of it.
- Good, because the mapping is a declaration in the user's own package: they
  edit it, delete it and version it like everything else they own.
- Good, because one mirror kind can reach two user kinds through two slots,
  which the per-source key made impossible.
- Bad, because `MappingFor` and its six callers move from one answer to a set,
  and the locking moves with them: `subject|<to>` becomes one key per mapping,
  and a delete recomputes once per mapping rather than once.
- Bad, because a mirror row lands with an empty subject slot until a mapping
  exists AND the row is written again, or another write's reference reaches
  them. `ensureSubject` runs inside the source record's own write and
  `subjectOf` resolves an unpointed row out of band through the subject hop
  (`references.go`), so rows synced before the user declared the mapping stay
  unpointed until one of the two happens.
- Bad, because the ids of a mapping's target become server-assigned.
  `checkCreateID` (`write.go`) refuses a create with a chosen id on any kind a
  mapping points at, so `apply -f` of a new `task` carrying
  `metadata.id: groceries` is refused once a mapping targets `task`: declaring
  a mapping changes how the user's own vocabulary may be written.
- Bad, because uninstall gains a refusal that reads as an obstruction to
  whoever meets it, and the mapping that blocks it may be one the console
  suggested rather than one the user wrote.
- Bad, because the loader resolves `from` against a package that may not be
  installed. The check leaves parse for resolve, so a mapping naming an absent
  provider fails further from the document that caused it.
- Bad, because the suggested mappings a sample ships install only when their
  provider is present, so what an import delivers depends on install order.
- Bad, because a provider's declaration no longer says what its `user` kind
  describes. The answer is in the repository's mapping set.

### Confirmation

`TestMappings`, `TestMappingRules` and `TestInstalledMapping`
(`internal/vocabulary`) hold today's rules, including the ones this record
changes, so the change cannot land without rewriting them. None of them covers
a foreign `from`: `TestMappingRules`' "from not in authority" case names
`x.example.com/nosuch`, an undeclared kind in the declaring authority, so it
proves the declared-kind check and not the ownership rule, and
`TestInstalledMapping` exercises a foreign `to`. Cases for a foreign `from`,
for the (source kind, subject property) key, for an unpinned subject reference
and for the uninstall refusal land with phase 5 of the plan. Until then this is
held by review only.

## More Information

The plan is [docs/plans/providers-and-samples.md](../plans/providers-and-samples.md),
whose phases 4 and 5 carry this; the tiers are
[0048](0048-providers-are-published-samples-are-copied.md) and the package
segment `from` and `to` are spelled in is record 0047. Related:
[0044](0044-a-reference-is-the-only-link-between-records.md), which made
`subject: true` a reference property, and
[0034](0034-a-reference-may-pin-a-trait-not-only-a-kind.md), the other way a
subject reference can be narrowed without naming one kind.

Reopen if projection needs more than one hop: a mapping that fills a reference
on the subject by following the source's own reference through another mapping
is the case phase 4 names, and it changes the bipartite rule rather than
extending this record.
