---
status: accepted
date: 2026-08-18
decision-makers: George Antoniadis
---

# 0040. The four occurrence logs all say `done`; `medicationschedulelog` loses its `taken` exception

## Context and Problem Statement

[0013](0013-state-machines-say-done-and-abandoned.md) settled the occurrence
logs on `done` for success and let one domain word survive by decision:
`medicationschedulelog` kept `taken`, because "a done dose is nobody's
sentence". Extracting the shared `occurrencelog` trait
([#122](https://github.com/geoah/substrate/issues/122)) makes that exception
cost more than it saves. The trait contracts the flip machine's shape
(`status` is a `state`, `scheduledAt` a `datetime`, `details` a `markdown`)
across `tasklog`, `workoutlog`, `medicationschedulelog` and `routinelog`, and
a trait carries no values, so each kind still declares its own states. Three
of the four declare `done` / `skipped`; the fourth declared `taken` /
`skipped`. One flip machine spelled two ways is the drift the extraction
exists to remove.

## Considered Options

- Keep `taken` on `medicationschedulelog`, as 0013 decided, and let the four
  logs the trait unifies still disagree on the success word.
- Move `medicationschedulelog` to `done` / `skipped`, so the four occurrence
  logs the trait binds spell their one flip machine one way.

## Decision Outcome

Chosen: `medicationschedulelog` moves from `taken` / `skipped` to `done` /
`skipped`, matching `tasklog`, `routinelog` and `workoutlog`. The four kinds
that bind `occurrencelog` now spell success `done` everywhere. This supersedes
0013's `taken` exception and nothing else it decided: `done` is still the one
success word and `abandoned` the one word for giving up across the state
machines, and `order` still keeps `canceled` by 0013's reasoning (both parties
cancel an order, abandoning is only the buyer's act). Only the
`medicationschedulelog` line of 0013 is reversed.

### Consequences

- Good, because a query for `status = "done"` reads the same across all four
  occurrence logs, and an agent prompt says "mark it done" with no per-kind
  translation.
- Good, because the `occurrencelog` trait now has no binding kind whose
  success word argues with the others, so the one flip machine is one word.
- Bad, because "a done dose" reads slightly less naturally than "a taken
  dose"; the consistency of one word across the four logs is worth the
  awkward reading, which is the judgment 0013 made the other way.
- Bad, because the rename is a narrowing: a repository holding a `taken`
  `medicationschedulelog` refuses the health version 3 upgrade until those
  rows are rewritten. Taken deliberately pre-freeze, while such rows exist
  only in dev databases.

### Confirmation

None mechanical: no linter knows a state value from a string, so this is a
reviewer's catch, exactly as 0013 noted. `mise run kinds:check` holds the
health authority version bump (2 to 3) that carries the rename.

## More Information

This lands with the trait extraction in
[#122](https://github.com/geoah/substrate/issues/122), alongside decision
[0039](0039-the-substrate-stores-a-recurrence-rule-and-never-expands-it.md) on
the occurrence stance. It supersedes
[0013](0013-state-machines-say-done-and-abandoned.md), whose done/abandoned
rule and `canceled` exception it carries forward unchanged.
