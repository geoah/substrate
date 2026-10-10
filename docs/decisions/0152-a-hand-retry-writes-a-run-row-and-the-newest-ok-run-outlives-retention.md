---
status: proposed
date: 2026-10-10
decision-makers: George Antoniadis
---

# 0152. A hand retry writes a run row, and the newest ok run outlives retention

## Context and problem statement

Trigger health ([#879](https://github.com/geoah/substrate/issues/879)) opens
an alert once a trigger's runs since its newest `ok` run have all parked for
longer than a window, and the next `ok` delivery resolves it. The run rows
could not answer "since its newest `ok` run":

- A retry by hand wrote no run row, whether it delivered or failed again
  ([0091](0091-a-parked-delivery-is-retried-or-forgotten.md) states this as a
  consequence). A retry that delivered left the old parks reading as a
  current streak, so the alert opened after a success. A retry that failed
  again left no sighting, so an alert the owner resolved never reopened.
- Retention kept the newest twenty runs that did not park. Twenty skips
  after an `ok` run tombstoned it, and the parks before it read as current.

## Considered options

- Read health from the delivery ledger (`trigger_failures`) beside the run
  rows. A forget and a retry both retire a row, so the ledger cannot tell a
  success from a dismissal, and a failed retry only moves `parked_at`.
- Record a retry's outcome on the alert alone. A retry before any alert
  opened has no alert to record it on.
- A retry writes a run row like a dispatch, and retention keeps the newest
  `ok` run.

## Decision outcome

Chosen: the third.

A hand retry writes one `substrate.reamde.dev/core/triggerrun` row, mode
`manual`, in the transaction that settles it: status `ok` when it delivers,
`skipped` when the guard no longer matches, and `parked` with the new error
and attempt count when it fails again. The once-only rerun of an agent
delivery a restart interrupted runs through the same retry
([0151](0151-an-agent-delivery-a-restart-interrupted-is-rerun-once-at-the-first-pass.md))
and writes the same row. 0091's two verbs stand unchanged: retry runs the
delivery, forget runs nothing and writes no run.

Retention keeps a trigger's newest `ok` run however old, beside the newest
twenty runs that did not park.

Trigger health then reads the run rows alone: the parks after the later of
the newest `ok` run and the alert's last resolve.

### Consequences

- Good, because one success clears the signal by construction, and a
  failed retry is a failure the signal counts.
- Good, because the run ledger now records every attempt a person started,
  which is what a reader of a trigger's runs expected.
- Bad, because each retry adds a changelog entry, and a trigger keeps one
  `ok` run more than twenty when its newest `ok` run is old.

### Confirmation

`internal/engine`'s `TestAHandRetryThatDeliversInsideTheWindowKeepsTheAlertShut`,
`TestTheNewestOkRunOutlivesTheRetention` and
`TestAFailedHandRetryAfterAResolveReopensTheAlert` each fail when the
retry's `ok` run, the retention exemption or the failed retry's parked run is
removed.

## More information

This amends [0091](0091-a-parked-delivery-is-retried-or-forgotten.md)'s
consequence that a retry writes no run record. The alert itself is
[0148](0148-an-ongoing-problem-is-one-alert-record-written-under-the-callable-it-is-about.md).
