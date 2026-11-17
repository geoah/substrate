---
status: accepted
date: 2026-11-17
decision-makers: George Antoniadis
---

# 0333. A mirror of an outside service keeps all of its history

## Context and Problem Statement

[0282](0282-a-kind-declares-how-long-its-history-is-kept-and-a-repository-may-override-it.md)
set the shipped retention classes, and one of them put `retention: P30D` on
the ten provider package headers: every kind a provider's sync mirrors from
an outside service would keep 30 days of history past each record's latest
state, and the compaction pass
([0300](0300-a-paced-pass-compacts-what-is-past-each-kinds-retention-and-forgets-old-deletions.md))
removes what is past it. No release carries those versions yet, and an
upgrade to one is held until the owner consents to the shorter history.
Reading that provider upgrades would keep only 30 days, the owner ruled the
class a mistake.

## Considered Options

- A mirror keeps all of its history: the provider headers state no
  retention.
- A mirror keeps 30 days, and an owner who wants more writes an override
  for each kind (0282).

## Decision Outcome

Chosen: a mirror keeps all of its history, as a kind a person writes does.
The ten provider headers state no `retention`, so their kinds read `all`,
and the `all` that each `config` and `account` and Apple's `share` stated
against the header goes too, since absence reads the same. This supersedes
0282's mirror class. The rest of 0282 stands as written: the grammar, a
header's default and a kind's own key, the `core/retention` override, the
consent an upgrade that shortens a kind's history asks for, and the one
lookup, `retentionOf`.

An outside service is not its mirror's backup. Once the service changes an
item, its earlier state is gone from the service, and once it deletes one,
the item is: no sync can fetch either again, so the substrate's history is
the one place they remain. A message edited, an issue rewritten or an event
moved keeps its earlier states here or nowhere. The second option makes
losing them the default and keeping them the owner's chore, one override per
kind across up to fifteen kinds a provider declares, and an owner finds out
they needed one only after the pass has run.

An owner who wants less still gets it through 0282's override: a
`substrate.reamde.dev/core/retention` record whose `appliesTo` names the
kind's declaration and whose `retention` is the shorter value, which wins
over what the kind declares.

Bookkeeping keeps its seven days: a sync's progress through one item
(`beeper/chatsync`, `google/calendarsync`, `notion/pagesync`,
`slack/conversationsync`), Apple Health's delivery receipt
(`applehealth/export`), and core's `trigger`, `triggerrun` and `alert`
([0283](0283-trigger-run-records-keep-a-week-of-history-and-positions-stay-in-the-log.md),
[0320](0320-a-triggers-own-history-is-bookkeeping-and-keeps-seven-days.md)).
They record a sync's own runs, not what the service holds.

An upgrade from a provider version that states `P30D` lengthens every
mirror's history and asks nothing. One from a version before 0282 shortens
only the bookkeeping kind a package has, from all to seven days, and asks
about that kind alone.

### Consequences

- Good, because a mirror's history outlives its service's copy: an edit or
  a move there leaves the earlier states here.
- Good, because the shipped classes are two: bookkeeping keeps seven days,
  and every other kind keeps all.
- Bad, because a mirror's history grows for as long as its sync runs, and
  the compaction takes none of it unless the owner overrides a kind.
- Bad, because a record deleted, here or at the service, still loses its
  history when the compaction pass forgets the deletion
  (`SUBSTRATE_DELETIONS_KEPT`, 30 days by default), whatever its kind keeps
  (0300). Keeping a mirror's deleted items for good takes
  `SUBSTRATE_DELETIONS_KEPT=0`, which keeps every deletion of every kind.
- Bad, because an owner who wants 30 days for a whole provider writes one
  override per kind: an override of a package is still an addition for
  later (0282).

### Confirmation

`TestEveryShippedKindsRetentionIsHeld` (kinds) holds every shipped kind but
the bookkeeping at `all`, and the bookkeeping at `P7D`, against
`kinds/retention.golden.json`.

## More Information

Supersedes 0282's mirror class, and nothing else of it. Each provider
package's version notes name the version that dropped its header's
retention.
