---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
amended-by: 0103, 0106
---

# 0087. A source that offers nothing, or that a probe cannot tell apart, parks instead of minting

## Context and Problem Statement

Every unpointed source record minted a subject. Measured on Mneme v6's seeded
repository (2026-09-16): 4,675 persons, 4,112 of them mapping-minted shells,
1,838 of those unreferenced and empty. Two mints made them. **Nothing to say:**
614 Slack users — deleted or external members with no profile at all — each
minted a person carrying no name, no address and nothing any probe could ever
match, so the next source describing the same human minted another.
**Several candidates:** a probe finding two people who share an address was
treated as a probe finding none, so it minted a third, and `merge: union` then
wrote the shared address onto that shell, which made every later probe see
three candidates and mint a fourth. 138 addresses were held by more than one
person; the owner's own was on three. Convergence DEGRADED as more sources
synced, which is the opposite of what a hub kind is for
([#577](https://github.com/geoah/substrate/issues/577), and T-026 item 3).

## Considered Options

- Keep minting, and collect the empty shells with a sweep afterwards
- Mint, and mark the row (`ambiguous: true`, `orphaned: true`) for a client to
  filter
- Park the source: leave the subject slot unset and resolve it again on the
  record's next write
- Park it and record the parking as a row of its own (a `recordmapping` status,
  or a `recordpatchrequest` a judge settles)

## Decision Outcome

Chosen: the third. The slot a mapping synthesises is single, `mustExist` and
NOT `required` (record 0096), so an unset slot is a legal state the write path
already allows — `mustExist` is a rule about a value that exists, not a demand
that one does. Nothing had to be forced, and no new record kind had to be
invented to hold a state the absence of a value states exactly.

`matchOrMint` now distinguishes three answers, and takes a `mustMint` from its
caller:

1. **Exactly one candidate links.** Unchanged.
2. **No candidate** mints a shell, which is the hub's growth — unless the
   source offers NOTHING AT ALL: no probe value and no mapped value, empty
   lists and blank strings counting as nothing. A mapping with neither probes
   nor map rules is link-only, carries structure and copies nothing, so every
   record of its source kind still mints.
3. **Several candidates** never mints. The record parks unlinked.

**Two callers still demand a subject and mint whatever the source carries.**
The subject hop (`references.go`), because somebody's write names this mirror
in a slot pinned at the subject kind and there has to be a record to point at.
And a source kind that declares its own subject reference `required:` — every
bundle written before record 0096 does — because a write that left it unset
would be refused by `checkRequiredProps` and the record would be lost instead
of the link. The rule is therefore: the slot is filled when something demands
it, and otherwise a source that cannot say who it describes waits.

Parking is not a state to clear. An unset slot is resolved again on the
record's next write, exactly as an absent one is, so the moment the owner
merges the two people the probe could not tell apart — or the connector syncs
a profile onto the empty user — the next sync links it.

### Consequences

- Good, because convergence improves with more sources instead of degrading:
  the poisoning loop is cut at its first step, and no shared address ever
  lands on a shell minted out of that ambiguity.
- Good, because a repository stops accumulating empty rows that nothing can
  match, which the owner sees as people who are not people.
- Bad, because "every source record has its subject from the first moment" is
  no longer true, and a consumer that assumed it must handle an empty slot. The
  slot is optional in the declaration, so this was always the contract; it is
  newly the practice.
- Bad, because a parked record is silent: nothing lists what is waiting and
  why. `propertyMeta` says nothing, and the owner learns of it by noticing a
  contact with no person. A parked-source read is the obvious follow-up and it
  is not in this change.
- Bad, because the two demanding callers keep the old behavior, so a
  pre-0096 bundle whose mirror declares `required: true` still mints out of an
  ambiguous probe. That is the declaration's own contract, and the way out is
  to drop the declaration and let the mapping synthesise the slot.
- Bad, because nothing collects the shells already minted. This stops the
  growth; it does not clean up
  ([#578](https://github.com/geoah/substrate/issues/578) is that question).

### Confirmation

`TestASourceWithNothingToOfferMintsNoPerson` and
`TestAnAmbiguousProbeParksInsteadOfMinting` (`internal/engine`) hold both
halves, including the resolve-on-next-write.
`TestAmbiguousMatchCreates` holds the other side of the rule: a mirror that
declares its slot `required:` still mints, because the write cannot land
otherwise. `TestNoMatchCreatesShell` and `TestUnlinkedSourceGetsAShell` hold
that a source with something to say still mints one.

## More Information

geoah/mneme-v6 `docs/upstream.md` asks N and O, ticket T-026 items 3 and 4.
Amends the resolution rule of
[0049](0049-the-owner-of-a-mappings-target-declares-it.md) as restated by
[0096](0096-a-mapping-synthesises-its-subject-slot.md); the mapping set's keys
and ownership are untouched. Reopen when a parked source needs to be VISIBLE —
a list of unresolved sources, or the `onAmbiguous: park | first | mint` knob
#577 proposes, which is a per-mapping policy rather than the engine's one rule.
