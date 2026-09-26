---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the #584 agent session)
---

# 0106. An apply links the sources its mappings left unlinked

## Context and Problem Statement

A mapping resolves a source's subject on the SOURCE's own write
(`internal/engine/write.go`, `ensureSubject`), and admitting a mapping is not a
write to its sources. [0049](0049-the-owner-of-a-mappings-target-declares-it.md)
recorded the cost as a consequence: rows synced before the mapping stay
unpointed until they are written again. The usual order is exactly that one
(install the provider, let it sync, then declare the mapping), and Mneme v6
measured it at 161 GitHub issues and 270 pull requests with an empty `task`
slot and no task minted
([#584](https://github.com/geoah/substrate/issues/584), mneme-v6 T-013). A
no-op patch commits nothing, so nothing a client can send reliably re-touches a
row either.

## Considered Options

- Link the existing sources in the apply that admits or changes a mapping
- The same, and also for every mapping the batch names by document, changed or
  not
- A separate verb that reprojects one mapping over its live sources, the way
  `trigger replay` rewinds a cursor
- A paged background job started by the apply

## Decision Outcome

Chosen: the second. The vocabulary apply, in its own transaction and against
the candidate registry, visits every live source of each mapping it admits,
redefines or names by document, and decides each one whose slot names no live
record exactly as that source's own write would: one candidate links, none
mints, and a source that offers nothing or parks on an ambiguous probe stays
unlinked, the parked one marked as its own write would mark it ([0087](0087-an-unresolved-source-parks-instead-of-minting.md),
[0103](0103-an-ambiguous-probe-follows-its-mappings-policy-and-a-probed-value-never-spreads.md)).
Each link is `writeSubject`, the engine's own write of the slot, credited to
the mapping ([0096](0096-a-mapping-synthesises-its-subject-slot.md)), so it
reaches the changelog and a rebuild replays it. The pass runs before
`recomputeMappingTargets`, which then reads the targets it found or minted.

The first option alone leaves no door for a repository whose rows were left
unlinked by an earlier binary, or by an ambiguity the owner has since settled:
its mapping is unchanged, so no apply would visit it. Counting a document the
batch names makes re-applying the package that declares the mapping that door,
with no new route, verb or wire type. A separate verb was rejected for the
same reason in reverse: it is surface the apply already covers, and it would
leave the default order broken until somebody learns to call it. A background
job was rejected because the apply already recomputes every live target of a
changed mapping in its transaction, and a second, asynchronous half would be
the one part of an apply a reader cannot see land.

### Consequences

- Good, because "apply the provider, then the mapping" now covers the rows the
  provider already synced, in the transaction that admits the mapping.
- Good, because re-applying a mapping reprojects it, and a second re-apply
  with nothing unlinked writes nothing.
- Bad, because an apply naming a mapping pays one refs query for its sources
  and one probe per unlinked source, inside the apply's transaction, every
  time. A source that offers nothing or stays ambiguous is probed again on each
  such apply.
- Bad, because the apply's previews (`PlanVocabularyApply`,
  `PlanBundleUpgrade`) do not say how many sources the apply will link or how
  many subjects it will mint, and the links are not counted against
  `SUBSTRATE_CONVERSION_CEILING`
  ([0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md)).
- Bad, because the pass runs while the apply holds the exclusive
  registry-dependency lock, so every write to the repository waits for it.
- Bad, because a subject the owner deleted comes back sooner. A slot naming a
  deleted record counts as unlinked, so every apply that names the mapping
  mints a fresh subject for each live source (with something to offer) whose
  person or task was deleted. The source's own write already did this; the
  difference is that a client re-applying its packages on every deploy sees
  deleted subjects return each release, not at each row's next sync.
- Bad, because each link bumps the source's `version` and `updated_at`, as a
  subject hop does, so the linked source becomes the newest writer for its
  target's `atomic` properties under latest-write-wins.
- Bad, because the links are written by the apply's actor (the owner, or the
  sample's bundle actor for a catalog import), not the provider, so the
  changelog entries for a provider's rows name that actor. The slot's manager
  is still the mapping.

### Confirmation

`internal/engine/mappingbackfill_db_test.go`:
`TestInstallingAMappingLinksTheSourcesThatAlreadyExist` (a probe hit links, a
miss mints and projects, a source offering nothing stays unlinked, a re-apply
mints nothing) and `TestReapplyingAMappingLinksASourceItParked` (an unchanged
mapping applied again links a source whose ambiguity was settled).
`internal/engine/mappingambiguous_db_test.go`:
`TestTheAmbiguityMarkIsDerivedAgain` (a mapping edit that still parks keeps
the mark; switching to `oldest` links the oldest candidate and clears it).

## More Information

Amends the consequence of
[0049](0049-the-owner-of-a-mappings-target-declares-it.md) that rows synced
before their mapping stay unpointed.

Also amends [0103](0103-an-ambiguous-probe-follows-its-mappings-policy-and-a-probed-value-never-spreads.md):
a parked source's mark and unset slot now stand until its next sync or the
next apply that names its mapping. That answers 0103's reopen trigger
("settling an ambiguity should re-resolve the sources waiting on it without
their next sync") through a re-apply, not automatically on the merge or
delete. It rewrites `TestTheAmbiguityMarkIsDerivedAgain` from 0103's
Confirmation to match. [0087](0087-an-unresolved-source-parks-instead-of-minting.md)'s
"resolved again on the record's next write" gains the same second path.

Closes
[#584](https://github.com/geoah/substrate/issues/584). The rules are in
[projection.md](../projection.md#sources-that-exist-before-their-mapping).

Reopen when one apply's backfill is too large for one transaction; the paged
job is the option that answers that.
