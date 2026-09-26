---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-586 agent session)
---

# 0107. A probe folds case only when it declares `fold: case`

## Context and Problem Statement

A `recordmapping` probe compared exactly: `probeValues`
(`internal/engine/mapping.go`) trimmed every source value and lowercased only
an `email` one, and the lookup compared that against the target's value as
stored. A `name → name` probe therefore read `Ada Example` and `ada example`
as two people and minted a second one (#586). Case is the cheapest near-miss
a probe can close, and one of eight near-misses in a 123-identity GitHub seed
was pure case.

## Considered Options

- Fold every short-string probe, both ends, always.
- An opt-in key on the probe, `fold: case`, that folds both ends.
- Leave probes exact and let a scoring matcher write `recordmergerequest`
  rows.

## Decision Outcome

Chosen: the opt-in `fold: case`, because a probe links records automatically
and only a split undoes a link. Short-string probes also carry identifiers
where case is significant (a `url` probe's path, a provider's base62 key), so
folding them all would join records a mapping author never meant to join.
The scoring matcher stays the answer for everything past case (nicknames,
initials, suffixes); it is still unshipped and not needed for case.

A folded probe lowercases and trims the source value and compares it with
the target's stored value lowercased and trimmed, item by item on a repeated
property. The withholding rule of record 0103 reads keys the same way, so
under a fold a value another target holds in a different casing is withheld
too. An unfolded probe is unchanged, `email` included: its source value is
lowercased and the target's is compared as stored.

### Consequences

- Good, because a mapping over human-typed names (`slack/user.realName`, an
  address book's display name) converges one spelling per person, and no
  existing mapping changes behaviour.
- Good, because `fold` is one word with one value, so a later fold (accents,
  whitespace runs) is a new value, not a new key.
- Bad, because a folded probe computes `lower(btrim())` on every live row of
  the target kind it reads. An exact probe reads the same rows today, but an
  index added later for exact probes (a top-level `props @>` containment, or
  an expression index) would not serve a folded one.
- Bad, because Postgres folds the stored side under the database's
  `LC_CTYPE` while Go folds the source side: under a `C` locale only ASCII
  folds, so `Émile` and `émile` stay two people.
- Bad, because an exact `email` probe still misses a target that stores the
  address in mixed case. A mapping that wants both ends folded says
  `fold: case`.
- Bad, because the key is new: a binary older than this record refuses a
  mapping carrying it, as every new dialect key does (record 0020).

### Confirmation

`internal/engine/mappingfold_db_test.go` (scalar and repeated targets, the
exact default, and the withholding rule under a fold) and the `fold` cases in
`TestMappingRules` (`internal/vocabulary/vocabulary_test.go`).

## More Information

Issue #586. `docs/projection.md` documents the key beside `match`. Revisit if
an exact `email` probe missing a mixed-case target becomes a reported problem:
the fix is then to fold every `email` probe.
