---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis (via the issue-120 agent session)
---

# 0013. State machines say `done` and `abandoned`; a domain word survives only by decision

## Context and Problem Statement

The shipped state machines disagreed on the two commonest outcomes
([issue #120](https://github.com/geoah/substrate/issues/120)). Success in the
occurrence logs was three words: `completed` (tasklog, workoutlog), `done`
(routinelog), `taken` (medicationschedulelog), with tasklog saying `completed`
while its own parent task's terminal state is `done`, inside one authority.
Abandonment was three words across three kinds: project `abandoned`, task
`dropped`, order `canceled`. State values are the hardest thing to rename once
records accumulate: a rename is a narrowing, and narrowings are refused while
live records hold the old value, so the words had to be settled before the
freeze.

## Considered Options

- `done` for the logs, matching the task machine's own terminal state; or
  `completed`, the majority word among the logs
- `abandoned` for giving up, matching project; or `dropped`, the task's word
- Unify every machine onto the settled pair, or let a kind keep its domain
  word where the domain genuinely speaks differently

## Decision Outcome

Chosen: `done` is the one success word for the occurrence logs, `abandoned` is
the one word for giving up, and a domain word survives only where a comment on
the declaration says it was decided.

`done` beat `completed` because the task machine already terminates in `done`
and a log should not disagree with the very kind whose occurrences it marks;
it is also the shorter word and routinelog already used it. So tasklog and
workoutlog moved `completed` to `done` (tasks authority version 3, fitness
version 2). `abandoned` beat `dropped` because project already used it, the
word is unambiguous where `dropped` collides with ordinary prose about
discarded values, and mneme's project lifecycle (the source these kinds were
ported from) says abandoned. So task moved `dropped` to `abandoned`.

Two domain words stay, each now marked as a decision on its declaration:
medicationschedulelog keeps `taken`, because a dose is taken and "a done dose"
is nobody's sentence; order keeps `canceled`, because either party cancels an
order while abandoning is only ever the buyer's act, and `canceled` is what
every vendor UI and email says. Both were kept by this decision, not inherited
from drift.

### Consequences

- Good, because one word means one thing: a query for `status = "done"` reads
  the same across tasklog, routinelog and workoutlog, and an agent prompt can
  say "mark it done" without a per-kind translation table.
- Good, because future kinds have a rule to follow instead of a corpus to
  imitate: `done`, `abandoned`, or a deliberate, commented domain word.
- Bad, because the rename is a narrowing: a repository holding a `completed`
  tasklog or a `dropped` task refuses the version 3 upgrade until those
  records are rewritten. Taken deliberately pre-freeze, while such rows exist
  only in dev databases.
- Bad, because "decided domain word" is a judgment call a future author can
  stretch; nothing mechanical distinguishes a decision from drift except the
  comment this decision requires.

### Confirmation

None: this is held by review only. `mise run kinds:check` holds the version
bumps that carried the renames, but no linter knows a state value from a
string, so a future machine reintroducing `completed` is a reviewer's catch.

## More Information

Issue #120 frames this as the companion to the record on where state machines
belong: that one places the machines, this one settles the words inside them.
The renames and the two keep-comments landed with the version
bumps named above, in the change that closes issue #120.
