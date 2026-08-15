---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0003. A transition's `notifies:` marker is the one resolution primitive

## Context and Problem Statement

PR #72 taught threads one interaction: deciding a proposed mutation wrote a
`system` message into the proposing thread and resumed it, hardcoded to the
`recordpatchrequest` kind in the engine. The interactions plan
([docs/plans/thread-interactions.md](../plans/thread-interactions.md), PR
#73) needed the same report-back for asks, and would have needed it again
for every future interaction shape. Without one primitive, each shape grows
its own write-back arm and its own resume bugs.

## Considered Options

- A declared transition marker (`notifies: <property>`) the engine keys on
- Keep hardcoding kinds in the engine, one arm per interaction shape
- A `waitsFor` barrier edge plus a `waiting` thread state (the independent
  design pass's shape)

## Decision Outcome

Chosen: the declared marker. A state transition carrying `notifies: <prop>`
(a reference property pinned to core's `llmthread`) writes the kind's
envelope into that thread and schedules the resume, inside the resolving
transaction. `recordpatchrequest` and `llminteraction` both ride it;
behavior for decisions stayed byte-identical through the migration. The
marker beat more hardcoding because the third consumer was already visible,
and beat the barrier/state design because threads legitimately hold several
pending interactions at once, so a single-barrier rule would have regressed
the shipped gate flow ("is anything waiting on me" stays a derived query).

Because a resume is a paid agent turn, the marker ships bounded: core kinds
only (the loader refuses it elsewhere), agents declare `resume: always |
never`, a thread's own agent never resumes its own thread, and a per-thread
hourly budget caps transition cycles. Delivery is at-least-once, not
fire-and-forget: the loop re-checks at settle and a background sweep
recovers resumes a restart dropped.

### Consequences

- Good, because any core kind gains report-back and resume by declaring two
  things, with one engine path under all of them.
- Good, because the resume bounds are one set of rules, not per-shape.
- Bad, because bundle kinds cannot participate yet: the core-only
  restriction is a trust decision deferred, not solved.
- Bad, because a loader widening means closures using the marker do not
  load on older binaries — the ordinary skew rule, but real.

### Confirmation

`TestNotifiesRefusedOutsideCore` and `TestNotifiesDemandsAThreadReference`
(internal/vocabulary/notifies_test.go) hold the loader contract;
internal/engine/agentresolution_db_test.go holds the bounds and the
delivery hardening; the PR #72 decision tests pass unchanged on the marker
path (internal/engine/agentdecision_db_test.go).

## More Information

Landed in PR #78. Revisit the core-only restriction when the resume bounds
have earned wider trust; the plan's rollout section carries that intent.
