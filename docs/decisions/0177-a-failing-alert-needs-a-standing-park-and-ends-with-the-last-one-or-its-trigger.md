---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0177. A failing alert needs a standing park, and ends with the last one or its trigger

## Context and Problem Statement

On 2026-10-20 five production triggers (`github-on-request`,
`google-gmail-on-connect`, `linear-on-connect`, `linear-on-request`,
`mneme-rollup-prose-monthly`) had read `failing` for 15 to 30 days with
nothing parked. `readFailing` built the streak from parked run rows alone and
never read `trigger_failures` (`internal/engine/health.go`), so a park the
owner forgot still counted. `ForgetTriggerFailure` resolved only the parked
alert, a retry that settled as a skip resolved nothing, and a disabled or
deleted trigger kept its alert
([0148](0148-an-ongoing-problem-is-one-alert-record-written-under-the-callable-it-is-about.md)
lists the deletion as a known bad). The parks behind the production alerts
were forgotten before #912 reached production on 2026-10-15.

## Considered Options

- Resolve the failing alert at forget time only.
- Read health from the ledger instead of the run rows.
- Gate the alert on a standing ledger row, resolve it when the last one ends,
  and have the dispatcher pass resolve the alerts nothing holds open.

## Decision Outcome

Chosen: the third. Resolving at forget time alone would not clear any alert
already open, and reading health from the ledger was rejected in
[0152](0152-a-hand-retry-writes-a-run-row-and-the-newest-ok-run-outlives-retention.md):
a forget and a retry both retire a row, so the ledger cannot say whether the
callable works. Success still comes only from an `ok` run; the ledger gates
and ends the alert:

- The alert opens only while at least one delivery of the trigger stands in
  `trigger_failures` (a park, an abandoned claim or a run in flight; an
  admitted webhook request does not count), and the streak counts the parked
  runs from the oldest standing row on. A park forgotten or retried away is
  dismissed like a hand resolve: only a park after it starts a streak.
- `settleParkedAlert` resolves the failing alert in the transaction where the
  trigger's parked count reaches zero: a forget, a retry that delivered or
  skipped, or a retirement
  ([0142](0142-a-schedule-fire-that-settles-retires-its-triggers-older-parked-fires.md),
  [0178](0178-a-sync-delivery-that-settles-ok-retires-the-accounts-older-parks-of-the-same-callable.md)).
- The pass that reads health resolves, each in its own transaction that
  confirms under the changelog lock: an open failing alert with nothing
  standing, the failing alert of a disabled or unparseable trigger, and the
  failing and parked alerts of a trigger that is gone. A trigger whose
  callable does not resolve keeps its alert; its parks stand and its status
  carries the error.

### Consequences

- Good, because the stale alerts resolve at the first pass after the deploy,
  and a forget, a skipped retry and a disable each end the alert they make
  stale.
- Good, because a deleted trigger's alerts no longer stay open, which amends
  0148's consequence.
- Bad, because a trigger whose parks were all forgotten while its callable is
  still broken reads `ok` until a new park outlives the window.
- Bad, because the pass counts `trigger_failures` per trigger once a minute,
  and opens one transaction per stale alert, once.
- Bad, because a disabled trigger's parks stay listed while its alert is
  resolved, and enabling it again reopens nothing until a new park outlives
  the window.
- Bad, because the count `settleParkedAlert` reads leaves out a claim this
  process holds, so a forget of the last park while an agent run of the same
  trigger is out resolves the alert; it reopens a window after that run parks.

### Confirmation

`internal/engine/health_db_test.go`:
`TestAParkForgottenInsideTheWindowOpensNoFailingAlert`,
`TestForgettingTheLastParkResolvesTheFailingAlert`,
`TestAnOpenFailingAlertWithNoStandingParkResolvesAtTheNextPass`,
`TestDeletingATriggerResolvesItsAlerts`. `internal/engine/health_fn_db_test.go`:
`TestARetryThatSkipsTheLastParkResolvesTheFailingAlert`,
`TestDisablingATriggerResolvesItsFailingAlert`. `internal/engine/health_test.go`:
`TestAFailingReadIsDueOnlyPastTheWindowAndWithSomethingNew`.
