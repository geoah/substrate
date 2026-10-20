---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0183. An unreachable Beeper Desktop throttles the sync, and throttled reads as waiting

## Context and Problem Statement

The Beeper sync (`messagessync`) dials Beeper Desktop, which serves its API
on the owner's computer, often over a tailnet. Each time that computer
sleeps, the 15-minute walk fails its first connect, stamps the account
`erroring`, and the console says "Syncing <account> is failing", several
times a day. The next run after the computer wakes catches up, so the alarm
asks the owner to fix nothing, and alarms that mean nothing teach the owner
to ignore the real ones. Nothing else on the account tells the two cases
apart.

## Considered Options

- Run Beeper Desktop in the cluster beside the server, so its API is always
  up
- Stamp `throttled` on a failure to reach Beeper Desktop at all, with a "not
  reachable since" sentence, and keep the walk's place
- Stamp `throttled` first, and `erroring` after 24 hours without a
  completed run
- Hide the failure in the console only

## Decision Outcome

Chosen: stamp `throttled`. A failure to reach Beeper Desktop at all (a
connect timeout, a refused connection, no route to the host or network, a
name that does not resolve) is the one class of failure the next run fixes
by itself. That is what `throttled` already means for a 429: the provider
holds the sync back, it resumes on its own, and the owner has nothing to
fix.

The body classifies the failure from requests' exception and the socket
error under it. On one it writes `syncState: throttled`, the same sentence
on `syncMessage` and `syncStatus` ("Beeper Desktop is not reachable (since
<time>); syncing resumes when it is back"), both streams `throttled`, and
`lastSyncedAt`. It writes no `syncError` and no `retryNotBefore`, so the
next tick and Sync now both try again. `unreachableSince` on the account
keeps the first unreachable instant across runs. A run that ends having
reached Beeper, whatever Beeper answered, deletes it, and a run that reached
Beeper before a later connect failed starts a new stretch at its own
attempt. The cursors are kept, so a walk
the sleep interrupted resumes where it stopped, and the per-chat handlers
re-raise one instead of filing it as that chat's failure.

A 4xx, a 5xx, a bad body, a read timeout (the app took the connection and
did not answer), a TLS or proxy failure, and the sandbox connect gate's
EACCES refusal stay `erroring`: those want a person. A failure the
classifier does not recognise stays `erroring` too.

The console words `throttled` as "Waiting" and "Waiting for <provider>", and
a provider whose every account is throttled as "Connected · waiting", for
every provider, with the body's sentence as the reason after it ("Waiting
for Beeper: Beeper Desktop is not reachable (since ...)"). It raised
no "is failing" problem for `throttled` before and raises none now.

Running Beeper Desktop in the cluster needs a third-party image vetted and
run, and the owner's own Desktop would still be the one with the logins.
Turning a long throttled stretch into `erroring` is the better end state,
and `unreachableSince` makes it a few lines in the body. It waits for a
sync-health window that applies one threshold to every provider, rather
than a Beeper-only 24-hour rule now. A console-only change leaves
the record saying `erroring` to every other reader, the CLI's `sync status`
and Mneme among them.

### Consequences

- Good, because a sleeping computer raises no alarm, and the walk resumes in
  place when it wakes.
- Good, because the console wording is generic: a rate limit reads as
  waiting too, which it is.
- Bad, because a Beeper Desktop that is gone for good (uninstalled, moved to
  another address) reads as waiting until someone looks. `unreachableSince`
  and the amber dot are the tell; nothing turns a long stretch into a
  failure until the sync-health window exists. A stale `apiBase`, the API
  switched off in Beeper (a refused connection) and a mistyped `.ts.net`
  name all read this way.
- Bad, because every tick while the computer sleeps still spends one
  connect attempt, up to the 20-second connect timeout.
- Bad, because a Desktop that is up but hangs (a read timeout) still reads
  `erroring`.

### Confirmation

- `internal/providertest/py/test_beeper_offline.py`, run by
  `TestBodyHelpersBeeper`: the classifier's cases, the throttled stamp, and
  the clearing.
- `TestBeeperUnreachableDesktopPauses`,
  `TestBeeperUnreachableMidWalkKeepsItsPlace`,
  `TestBeeperUnreachableInEachPhaseKeepsItsQueue`,
  `TestBeeperReachedThenUnreachableStartsANewStretch`,
  `TestBeeperDesktopBackClearsTheStretch` and
  `TestBeeperDesktopErrorStillErrors` in
  `internal/providertest/beeper_test.go`.
- Case 13 of `internal/providere2e/providers/beeper/scenario.py`.
- The `syncWords` and `providerStanding` cases in
  `web/console/src/lib/providers.test.ts`.

## More Information

Builds on [0085](0085-a-sync-is-a-core-trait-the-dispatcher-stamps.md), the
sync trait, and [0176](0176-a-reconnect-requests-a-sync.md). Revisit if
Beeper Desktop moves into the cluster, or when a health window exists that
can turn a long throttled stretch into a failure.
