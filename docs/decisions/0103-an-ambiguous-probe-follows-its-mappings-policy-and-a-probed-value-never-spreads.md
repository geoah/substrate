---
status: accepted
date: 2026-09-25
decision-makers: George Antoniadis (via the #577 agent session)
amended-by: 0106
---

# 0103. An ambiguous probe follows its mapping's `onAmbiguous`, and a probed value never spreads to a second target

## Context and Problem Statement

[0087](0087-an-unresolved-source-parks-instead-of-minting.md) stopped the
default path from minting a subject out of a probe with several candidates,
and left three things open. The two callers that must mint (the subject hop,
and a source kind that declares its slot `required:`) still minted, and
`merge: union` still copied the shared address onto the shell they minted,
so the next probe on that address saw three candidates. A parked source was
silent: nothing listed it. And there was one rule for every mapping, where
Mneme asked for a per-mapping choice
([#577](https://github.com/geoah/substrate/issues/577), mneme-v6 T-005).
0087 named both of the last two as the trigger to reopen it.

## Considered Options

- Keep 0087's single rule, and stop only the shared value from spreading
- A per-mapping `onAmbiguous: park | first | mint`, as the issue proposed
- A per-mapping `onAmbiguous: park | oldest | mint`, a derived mark on a
  parked source, and a recompute rule that no probed value spreads
- A `recordpatchrequest` per ambiguous source for a judge to settle

## Decision Outcome

Chosen: the third.

**`onAmbiguous` is a key of the mapping**, beside `match`, because it answers
for the probe list as a whole. `park`, the default, is 0087's rule: the slot
stays unset. `oldest` links the candidate created first. `mint` mints a
shell whatever the caller demands. The issue's `first` is spelled `oldest`:
record ids are random, so "the first candidate" has no order behind it, and
`first` already means "the head of a repeated source" in `merge`. A demanding
caller under `park` still mints, because it cannot wait.

**A probed value never spreads.** Recompute withholds from a target every
value of a property some probe matches on that another live target holds and
this target does not. That is what keeps `mint`, and the demanding callers,
from poisoning the next probe; it also stops a linked source from writing an
address it shares with another person onto the one it linked. A value the
target already holds stays, so the existing duplicates do not move: the owner
settles them with merge. The source keeps the value, its offer row keeps it,
and a read shows it as an alternative.

**A parked source is marked.** `records.ambiguous_at` is derived storage on
the source row, the class of `orphaned_at`
([0092](0092-an-orphaned-mapping-target-is-marked-and-collected-on-request.md)).
The source's own write sets and clears it, a rebuild and a mapping change
derive it again, and `filter.ambiguous` lists it. A `recordpatchrequest` per
source was rejected: it is a record with a lifecycle for a fact the unset slot
and the probe already state, and a client that wants a judge can build one on
the list.

### Consequences

- Good, because no policy makes the next probe more ambiguous than this one,
  so convergence cannot degrade as sources are added.
- Good, because the owner can list what is waiting (`substratectl get <kind>
  --ambiguous`) instead of noticing a contact with no person.
- Bad, because the mark is read when the SOURCE is written. Merging or
  deleting a candidate writes the candidates, so the mark and the unset slot
  both stand until the source's next sync.
- Bad, because a legitimate shared address (a family inbox) now lands on the
  first person that takes it and on nobody after, unless the owner writes it.
  Releasing an owner hold on such an address hands it back to the rule, so the
  released target may lose it.
- Bad, because a recompute that gains a new probed value pays one probe query
  per value, and two concurrent recomputes may both add the same new value;
  the next probe on it then parks.
- Bad, because `onAmbiguous` is a new key in a closed key set: a binary older
  than this one refuses a closure that sets it (0020).

### Confirmation

`internal/engine/mappingambiguous_db_test.go`:
`TestAnAmbiguousMintDoesNotCopyTheSharedAddress` (the cascade),
`TestALinkedTargetDoesNotTakeAnAddressAnotherHolds`,
`TestAnAddressATargetAlreadyHoldsStays`, `TestOnAmbiguousParkMarksTheSource`,
`TestOnAmbiguousOldestLinksTheFirstCreated`,
`TestOnAmbiguousMintMintsWithoutTheSharedAddress` and
`TestTheAmbiguityMarkIsDerivedAgain`. `TestMappingRules`
(`internal/vocabulary`) holds the key's parse and its refusals.

## More Information

Amends [0087](0087-an-unresolved-source-parks-instead-of-minting.md); closes
[#577](https://github.com/geoah/substrate/issues/577). Reopen when settling an
ambiguity should re-resolve the sources waiting on it without their next sync.
