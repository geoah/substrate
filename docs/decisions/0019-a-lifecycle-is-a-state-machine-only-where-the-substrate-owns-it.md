---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0019. A lifecycle is a state machine only where the substrate owns it

## Context and Problem Statement

This record is written retrospectively, at the pre-stabilization design
review, from the shipped vocabulary. The same concept appears in three
shapes across `kinds/`: a task's status is a declared `state` machine with
transitions and stamps (`kinds/tasks.substrate.reamde.dev/task.yaml`), while
a mirrored GitHub issue's state and a calendar event's status are plain
strings (`kinds/github.bundles.substrate.reamde.dev/bundle.yaml`,
`kinds/calendar.substrate.reamde.dev/calendarevent.yaml`). A third shape sits
beside them: `core/llmthread.status` and `core/run.status` are engine-written
`enum`s, closed value sets with no declared transitions. The split holds
across every authority wherever the value is a record's own lifecycle, and it
is written down nowhere, which means the next vocabulary author picks a side
by imitation and may imitate the wrong neighbour.

## Considered Options

- State machines everywhere: mirror provider lifecycles into declared
  machines too.
- Plain strings everywhere: drop the machine and validate nothing.
- A machine where the substrate owns the transitions; a plain string where a
  provider does.

## Decision Outcome

Chosen: ownership decides. A `state` property's value is the authority to
refuse an illegal move, to stamp a transition (`completedAt`,
`decidedAt`), and to notify on one. That authority is real only where the
transition passes through the substrate, whoever asked for it: an owner or an
agent moves a task, and the engine accepts or refuses the move. A mirror
records a move the provider already made, so there is nothing to refuse.
A mirror asserting a machine over provider data would refuse syncs whenever
the provider moves in a way the declaration did not anticipate, and since
removing or renaming an occupied
state is a narrowing the vocabulary refuses, every wrongly guessed provider
state would be frozen into the kind. Strings everywhere would have thrown
away refusal, stamps and `notifies` where they genuinely hold.

Ownership is necessary, not sufficient. A machine is worth declaring only
where there are illegal moves to refuse: `core/run.status` is stamped once
with a delivery's outcome and `core/llmthread.status` settles from `running`
to one terminal value, so both ship as closed `enum`s. Substrate-written and
no legal moves to police means an enum; a provider writes it means a plain
string.

### Consequences

- Good, because a sync never fails on the vocabulary: a provider inventing a
  new lifecycle value lands as a new string, not a refused write.
- Good, because where a machine exists it means something: its transitions
  are the complete set of legal moves, and stamps and notifies can hang off
  them.
- Bad, because provider-owned lifecycles get no write-time validation, no
  stamps and no notifies, and their value sets live in prose descriptions
  rather than declarations.
- Bad, because three spellings of "status" coexist in the tree (`state`,
  `enum`, plain string), and a reader must know which one a kind is on before
  filtering or transitioning it.
- Bad, because the tree already holds statuses this rule does not explain.
  `tokenStatus`, from the `core/accountconfig` trait, is a connection
  lifecycle the OAuth facility writes (`writer: oauth`) and every account
  kind declares as a plain string; a connector's `syncStatus` carries its
  reason inside the value (`ignored: duplicate account`). Both are
  substrate-written, and neither declares its value set. This decision does
  not revisit them.

### Confirmation

None: this is held by review only. No linter can tell who owns a lifecycle.

## More Information

The rule generalizes past lifecycles: the vocabulary declares closed sets
only where the substrate can enforce them, and mirrors report the provider's
world verbatim. Revisit a specific kind's side only when write ownership of
that lifecycle actually moves. Where a machine does exist, the words inside
it are settled by [0013](0013-state-machines-say-done-and-abandoned.md).
