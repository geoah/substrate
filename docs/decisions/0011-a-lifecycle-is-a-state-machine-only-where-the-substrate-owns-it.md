---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0011. A lifecycle is a state machine only where the substrate owns it

## Context and Problem Statement

This record is written retrospectively, at the pre-stabilization design
review, from the shipped vocabulary. The same concept appears in two shapes
across `kinds/`: a task's status is a declared `state` machine with
transitions and stamps (`kinds/tasks.substrate.reamde.dev/task.yaml`), while
a mirrored GitHub issue's state and a calendar event's status are plain
strings (`kinds/github.bundles.substrate.reamde.dev/bundle.yaml`,
`kinds/calendar.substrate.reamde.dev/calendarevent.yaml`). The review found
the split perfectly consistent across every authority and written down
nowhere, which means the next vocabulary author picks a side by imitation
and may imitate the wrong neighbour.

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
substrate is the writer of the transition. A mirror asserting a machine over
provider data would refuse syncs whenever the provider moves in a way the
declaration did not anticipate, and since removing or renaming an occupied
state is a narrowing the vocabulary refuses, every wrongly guessed provider
state would be frozen into the kind. Strings everywhere would have thrown
away refusal, stamps and `notifies` where they genuinely hold.

### Consequences

- Good, because syncs never fight the vocabulary: a provider inventing a new
  lifecycle value is a new string, not a refused write.
- Good, because where a machine exists it means something: its transitions
  are the complete set of legal moves, and stamps and notifies can hang off
  them.
- Bad, because provider-owned lifecycles get no write-time validation, no
  stamps and no notifies, and their value sets live in prose descriptions
  rather than declarations.
- Bad, because two spellings of "status" coexist in the tree, and a reader
  must know which side a kind is on before filtering or transitioning it.

### Confirmation

None: this is held by review only. No linter can tell who owns a lifecycle.

## More Information

The doctrine generalizes past lifecycles: the vocabulary declares closed sets
only where the substrate can enforce them, and mirrors report the provider's
world verbatim. Revisit a specific kind's side only when write ownership of
that lifecycle actually moves.
