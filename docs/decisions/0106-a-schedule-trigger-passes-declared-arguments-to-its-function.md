---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-636 agent session)
---

# 0106. A schedule trigger passes declared arguments to its function

## Context and Problem Statement

A schedule fire hands its callable an envelope with the fire id, the instant
and the repository, and nothing else, so one function that behaves
differently per schedule needs one thin entrypoint function per schedule
(Mneme's rollup ships `rundaily`, `runweekly` and `runmonthly` over one
driver). A record delivery and a webhook fire do not have this problem: the
changed record or the request is their input
([#636](https://github.com/geoah/substrate/issues/636)).

## Considered Options

- Keep one entrypoint function per schedule.
- A field `source.schedule.arguments`.
- A top-level `arguments` property on the trigger, admitted on a schedule
  source to a function only, held to the function's declared `arguments:`.

## Decision Outcome

Chosen: the top-level `arguments` property. Every schedule fire, and every
retry of a parked one, hands the map to the body as `input["args"]`, the
same key a direct call uses.

A field under `source.schedule` was the first shape tried and the kind
dialect refuses it: an object field is declared shape, and `json` is for
shapes the kind does not own. These values take the callable's shape, so they
are a `json` property beside `callable`, which is also where the arguments
belong: they bind the callable, not the clock.

The engine checks the map with the check `CallFunction` applies to a call's
args (`vocabulary.CheckValue` against the function's compiled `arguments:`),
twice: when the trigger is written, and at each fire against the body
resolved for that fire, because an apply since the write may have narrowed
the function. A fire whose arguments the live function refuses parks after
one attempt, since a retry reproduces it. Arguments bound to a function that
declares no `arguments:` are refused, since nothing says the body reads them.

A record or webhook source carrying `arguments` is refused at write time, and
so is an agent callable: an agent reads its fire as the envelope in the
turn's user message and declares no arguments to check against.

### Consequences

- Good, because one function serves many schedules, and the trigger record
  says what each schedule passes.
- Good, because the value is checked where it is written, so a typo is a
  `422` on the trigger rather than a parked fire an hour later. This covers
  argument names and types; `enum` values are not checked yet, on this path
  or the call API.
- Bad, because a function re-applied with a narrower signature parks every
  schedule that passes the old arguments, until each trigger's `arguments`
  are fixed or dropped (a patch that only disables the trigger is refused
  too, because every write re-checks the whole record).
- Bad, because `arguments` is one more property that means something on one
  source arm only; widening it to the other arms is an additive change, but
  each needs its own reason.

### Confirmation

`TestAScheduleTriggerPassesItsArguments` in
`internal/engine/scheduleargs_db_test.go` fires a schedule with arguments,
reads them in the body, and parks the fire after the function narrows.
`TestTriggerArgumentsAreAdmittedOnAScheduleToAFunctionOnly` in
`internal/engine/triggers_internal_test.go` holds the refusals.

## More Information

Item 1 of [#55](https://github.com/geoah/substrate/issues/55) (a trigger that
targets a host function) needs declared trigger arguments too, and would
reuse this property; it also needs trigger-owned grants, which this record
does not decide. Worth reopening if a record or webhook trigger needs
arguments beside its envelope.
