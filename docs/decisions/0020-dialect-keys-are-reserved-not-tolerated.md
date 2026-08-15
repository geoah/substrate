---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis (via the issue-110 agent session)
---

# 0020. Dialect keys are reserved by name; there is no tolerated `x-` namespace

## Context and Problem Statement

A declaration's key set is closed: an unknown key quarantines the authority
that ships it, and for core it fails the repository's open
(internal/engine/vocabularywrite.go, `buildAuthoritiesSeparately`). Adding a
dialect key is therefore an ecosystem event: every binary that might read the
closure has to know the key before any closure uses it, and bundles are
shared across servers, so "upgrade first" is not one operator's decision.
[Issue #110](https://github.com/geoah/substrate/issues/110) asked for three
keys to be reserved ahead of their implementations and, in the same change,
for a ruling on whether the dialect gains a tolerated `x-` prefix, because
retrofitting tolerance is itself a key older binaries cannot read.

## Considered Options

- Reserve keys by name, one coordinated event per batch, and keep every key
  owned by the dialect
- Add a tolerated `x-` namespace: an unknown `x-`-prefixed key is admitted,
  stored and ignored
- Admit and store every unknown key, quarantining nothing
- Reserve nothing and take a coordinated event per key, as each is
  implemented

## Decision Outcome

Chosen: reserve keys by name. `unique`, `deprecated` and an edge's
`properties` block land in this change, validated at load, stored on the
declaration and acted on by nothing; `renamedFrom` (ticket 003) was the
precedent. There is no `x-` namespace and no other tolerated prefix, now or
later.

The case against tolerance is that a tolerated key is silently unenforced.
`x-unique: true` in a closure would be admitted by every binary and honored by
none, and the author cannot tell those two states apart, which is exactly
the failure a closed key set exists to prevent. A key nobody validates also
accretes meaning: two authorities spell the same idea differently, a client
starts reading one of the spellings, and the prefix that was supposed to be
private is a contract with no owner. The quarantine is not friction to route
around; it is the signal that a binary is being asked to read something it
does not understand, and issue #104 is already about the cases where that
signal arrives too late.

Admitting every unknown key is the same trade without even a prefix to mark
it. Reserving nothing keeps the door strict but pays a full upgrade cycle per
key, one at a time, which is the cost this record is spending once instead.

Reserving by name has an honest price: a reserved key that is never
implemented is dead surface in the dialect, and a key reserved with the wrong
shape is worse, because changing it later is another coordinated event. Two
consequences follow. A reservation ships with its validation, so a declaration
that could never be honored is refused at the door rather than stored and
found unenforceable later. And a reservation whose shape is not settled is not
made: the retirement markers of
[issue #146](https://github.com/geoah/substrate/issues/146) (`reserved` lists
of retired property names, enum values and kind names) are left out of this
batch for that reason, and will cost their own event.

### Consequences

- Good, because a declaration key means the same thing on every server that
  admits it: there is no class of key that is stored, shared and honored by
  nobody.
- Good, because the three implementations behind these keys (a unique index,
  a deprecation-aware picker, edge-property validation) each ship without an
  ecosystem-wide upgrade in front of them.
- Bad, because an authority that needs a private annotation today has nowhere
  to put one. If that need becomes real the answer is a declared block with a
  name and a type, not a prefix rule.
- Bad, because the dialect now carries three keys nothing enforces, and a
  reader who assumes `unique:` is policed will be wrong. The declaration
  reference says so in both places, and the loader's own refusals are narrower
  than the eventual enforcement, never wider.
- Bad, because #146's markers still cost a coordinated event this change could
  have absorbed had their shape been settled first.

### Confirmation

`TestUniqueReserved`, `TestDeprecatedReserved` and
`TestEdgePropertiesReserved` (internal/vocabulary/reserved_test.go) hold the
door's validation; `TestSchemaEvolutionReservedKeysRoundTrip`
(internal/engine/evolution_db_test.go) holds the keys through admission, the
stored row and a rebuild from rows. The absence of a tolerated namespace is
held by the closed key sets themselves (`checkKeys` has no prefix branch)
and by review.

## More Information

Reopen trigger: a concrete need for per-authority private annotation that a
declared, named block cannot serve. Such a proposal has to say what reads the
annotation, since a key nothing reads is the case this record refuses.
