---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0257. A kind move is checked only while its old kind holds rows, and may add optional properties

## Context and Problem Statement

[0078](0078-a-kind-move-is-ordinary-record-writes.md) refuses a shipped
`movedFrom` whose two property sets differ, in either direction. The boot
upgrade never asked that check: the list it was added to was replaced a few
lines later, which the 1.0 review found by reading
([#1313](https://github.com/geoah/substrate/pull/1313)). Restoring it as
written refuses the one move the tree ships. The four old core kinds stay
declared in every repository that predates the move to `llm` (0077), so the
move is classified on every later upgrade of `llm/thread`, `llm/message` or
`llm/provider`, and each of those has gained properties since. Every such
upgrade would be refused, in most real deployments, forever.

## Considered Options

- Restore the check as written.
- Ask the check only while the old kind holds live rows.
- Let the new kind declare properties the old one lacks, unless it requires
  one.
- Both of the last two.
- Restore only the checks the shipped move cannot trip (a duplicate source, a
  swap, a mapped kind), and leave the property check out of the boot.

## Decision Outcome

Chosen: both. The checks on a move are asked only while its old kind holds a
live row, and the new kind may declare a property the old one lacks, unless it
requires it.

- **Only with live rows.** 0078 already says a move is classified from the
  live rows and that a second admission finds nothing to move. An old kind
  declared and empty carries nothing, so the declaration checks (the property
  sets, a mapped kind, a duplicate or swapped move) refuse nothing for it,
  whatever its new kind has become. The checks the store answers stay as they
  are: a live reference still pinned at the old kind refuses the upgrade, rows
  or none. The boot and the upgrade preview read the same list: the checks run
  where the move's rows are counted, under the door's reader.
- **An optional property may be added.** A carried row holds no value for
  it, which an optional property admits. A required one would refuse every
  carried row at its put, so it is refused, naming the property. A property
  the old kind declares and the new one does not stays refused, and so does a
  rename, as 0078 says.

Restoring the check as written stops every repository that predates the `llm`
move from upgrading. Live rows alone still refuses a repository that holds
rows in an old core kind, because the `llm` kinds have gained properties since.
The optional allowance alone keeps refusing an empty old kind for any required
property the new kind ever gains. Leaving the property check out of the boot
lets a move that cannot be honored fail the repository's open, which is what
the tree did while the check was lost.

This amends 0078: what a move refuses, and when it is asked. 0078 stands
otherwise.

### Consequences

- Good, because an old kind left declared and empty no longer trips the
  declaration checks on any later upgrade of the kind it moved to.
- Good, because a move the engine cannot honor is refused with a line that
  names the property, in the boot's log and in the preview, instead of failing
  the repository's open.
- Bad, because a move may now change a kind's shape a little: a property
  added with the move is optional on the carried rows, and nothing says the
  move added it.
- Bad, because an old kind that holds rows again (nothing stops a write to it)
  turns the check back on at the next upgrade of its new kind, and that
  upgrade is refused until the rows are moved by hand or the new kind fits.

### Confirmation

`TestABootUpgradeRefusesAMoveItCannotHonor`,
`TestABootUpgradeMovesRowsIntoAKindThatAddsAnOptionalProperty` and
`TestABootUpgradeOfAKindMovedOutOfAnEmptyKindIsNotRefused`
(`internal/engine/shippedmove_db_test.go`) hold the three cases at the boot
and in the preview; `TestMovedFromProblemsRefuseADroppedOrRequiredProperty`
(`internal/vocabulary`) the rule; and `TestBootUpgradeMovesTheLLMRowsOutOfCore`
(`internal/testenv`) the shipped move.

## More Information

Revisit if a move ever needs to add a required property: that wants a default
the move writes, which is a conversion step 0078 does not have.
