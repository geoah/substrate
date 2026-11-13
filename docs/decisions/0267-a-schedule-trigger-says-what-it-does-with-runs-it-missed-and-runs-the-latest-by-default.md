---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0267. A schedule trigger says what it does with runs it missed, and runs the latest by default

## Context and Problem Statement

A schedule trigger that missed occurrences, because the server was off, the
trigger was disabled or a repository was restored to an older fire state,
fired every one of them, oldest first, ten per pass
(`internal/engine/schedulecursor.go`,
[0147](0147-a-pass-runs-its-schedule-triggers-in-a-lane-beside-its-record-triggers.md)).
Every schedule the binary ships is an hourly or quarter-hourly sync whose
body decides for itself whether an account is due, so a week offline meant
168 runs that each found little to do. The finding is F10, point 6, in
[pull request 1313](https://github.com/geoah/substrate/pull/1313).

## Considered Options

- Keep firing every missed occurrence.
- Fire only the latest, for every trigger.
- A property on the trigger that says which, with the latest as the default.

## Decision Outcome

Chosen: a property, `source.schedule.missed`, `latest` or `all`, absent
reading as `latest`, because most schedules poll, and a poll that missed
runs needs one run now, while a schedule whose every occurrence is work of
its own (a reminder per occurrence, a digest per day) must be able to keep
each.

What this sets:

- **`latest`** fires the newest due occurrence alone. The fire state moves
  from where it stood to that occurrence in the fire's own transaction, and
  the occurrences in between are never fired. One log line counts them.
- **`all`** is the previous behaviour: each occurrence once, oldest first,
  at most ten per pass.
- **The status counts what a pass would fire**: one pending occurrence for
  `latest`, however many were missed.
- **The shipped schedules state nothing.** Each is a sync that checks per
  account whether it is due, or the reading list's weekly digest, which
  patches the one `latest` digest record; the latest run is all any of them
  needs.

### Consequences

- Good, because a server that was off for a week runs one sync per trigger
  when it comes back, not hundreds.
- Good, because a trigger that needs every occurrence says so in one key.
- Bad, because a trigger written before this that relied on every occurrence
  firing changes behaviour until its owner writes `missed: all`. That is a
  break, and its upgrade steps say so.
- Bad, because an older server does not know the key and refuses to
  dispatch a trigger that states it.

### Confirmation

`TestAWeekOfflineRunsOneSync`,
`TestAScheduleThatStatesAllFiresEachMissedOccurrence` and
`TestAScheduleRefusesAnUnknownMissed`
(`internal/engine/schedulemissed_db_test.go`). The schedule lane's tests and
`TestScheduleFiresMissedOccurrencesInOrder` state `missed: all` and hold its
catch-up.

## More Information

[Triggers](../functions.md#triggers) describes the key.
