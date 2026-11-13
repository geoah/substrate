---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0266. A temporary failure is retried over minutes, and the body says which it is

## Context and Problem Statement

A failed delivery was tried three times, 25 ms and 100 ms apart, and then
set aside until a person retried it (`internal/engine/functions.go`). A
provider that was down for a minute therefore needed a person, and a body
that ran out of time held its trigger's lane through two more timeouts. A
body had no way to say "try later": its error reached the engine as text
(`internal/runner/runner.go`). The finding is F10, point 5, in
[pull request 1313](https://github.com/geoah/substrate/pull/1313).

## Considered Options

- Retry every failure over minutes.
- Wait between attempts inside the delivery, as the 25 ms and 100 ms waits
  do.
- Keep the next attempt's time in a new column of the parked row, written
  by a new field of the park effect.
- Park a temporary failure at once, mark the row as waiting in its error,
  and let a walk each dispatcher pass retry the rows that are due, the time
  read from the row's park time and attempt count.

## Decision Outcome

Chosen: park at once, mark the row, and retry from a walk, because it keeps
the schedule in the delivery ledger without a new effect field
([0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)),
and because nothing holds a lane, a delivery slot or a transaction while a
retry waits. Retrying every failure would retry a broken body for an hour.
Waiting inside the delivery holds the trigger's lane for minutes. A new
field on the park effect is a payload an older binary reads differently,
which is a new changelog dialect.

What this sets:

- **The body says which it is.** A body raises `host.Temporary`, or lets a
  network error or an HTTP "try later" (408, 429, 502, 503, 504) escape.
  The runner reports it as `temporary` on the response frame (protocol
  version 7) and as `runner.ErrTemporary`. A body that runs past its
  `timeout` is temporary too. Any other failure is the body's own.
- **A temporary failure parks at once**, its cursor or fire state moved past
  it, under an error that starts `temporary, retried automatically at
  <time>:`. No alert is raised.
- **The walk retries it** when it is due: 15 s, 30 s, then 1, 2, 4, 8, 16
  and 32 minutes after each park, through the path a retry by hand takes.
  Past the last wait the row is set aside for a person and the alert is
  raised.
- **A permanent failure is set aside as before**: at once where a retry
  reproduces it, after the two quick retries otherwise.
- **Nothing that may run an agent is retried by the walk**, because a
  retry would run the agent and its writes again: an agent trigger, a
  delivery whose body already ran one, and a function granted
  `permissions.agents` or `permissions.call`. Their temporary failures are
  set aside for a person, and the walk reads the callable again when a row
  falls due, setting aside one whose function has gained such a grant.
- **A waiting row is parked at the walk's clock**, the clock its due time is
  read on, and the walk reads the row again under its hold before it runs
  it, so a hand's retry meanwhile moves the next one later.

### Consequences

- Good, because a provider's short outage settles with no person, and a
  stuck body costs its lane one timeout, not three.
- Good, because a restart or a restore folds the same waiting rows back,
  and their times with them.
- Bad, because the waiting state lives in an error's text. A reader that
  wants it matches the prefix, and an older binary shows the row as an
  ordinary park.
- Bad, because the status counts a waiting row as parked, and the sync
  status shows its account `erroring` until the retry delivers.
- Bad, because a provider's `Retry-After` is not honoured: the gaps are the
  server's.
- Bad, because a function that may call another function or run an agent
  gets no automatic retry, even when the call would not reach an agent.

### Confirmation

`TestATemporaryFailureForAMinuteNeedsNoPerson`,
`TestAPermanentFailureIsStillSetAside` and
`TestATemporaryFailurePastItsRetriesIsSetAside` and
`TestATemporaryFailureOfAFunctionThatMayRunAnAgentIsSetAside` and
`TestAWaitingRowWhoseFunctionMayNowRunAnAgentIsSetAside`
(`internal/engine/temporaryretry_db_test.go`) drive the walk on a test
clock, `TestTheWalkDoesNotRetryARowAHandRetriedMeanwhile` the hold, and
`TestOnlyAFunctionThatRunsNoAgentWaitsForTheWalk` which failures wait. `TestABodySaysWhetherItsFailureIsTemporary` (`internal/runner`)
holds what the host reports as temporary.

## More Information

[When a delivery fails](../functions.md#when-a-delivery-fails) is the
author's description. Revisit if a failure needs a time of its own, such as
a provider's `Retry-After`: that is a field on the parked row, and a new
changelog dialect.
