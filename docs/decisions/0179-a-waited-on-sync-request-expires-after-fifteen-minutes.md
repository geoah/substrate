---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0179. A waited-on sync request expires after fifteen minutes

## Context and Problem Statement

[0176](0176-a-reconnect-requests-a-sync.md) made the console read a failing
account with an unanswered sync request as being checked again, and accepted
as a cost that a request whose run never starts hides the failure until a run
starts or the request is answered. A request's run does not start when the
provider's on-request trigger is disabled, missing, capped or behind a long
backlog. In each case the account row reads "Checking again…" with no end,
and the card says the provider is syncing while nothing runs.

## Considered Options

- Keep the wait unbounded
- Bound the wait in the console by a fixed time
- Have the engine stamp a request as started or expired, a new `sync` trait
  property every provider would carry
- Have the console wake the on-request trigger itself and read the wake's
  reply

## Decision Outcome

Chosen: the console bounds the wait at fifteen minutes, measured by the
server's clock as the console last read it.

The dispatcher passes within seconds, and every shipped on-request trigger
fires on the request's own change, so a run that has not started fifteen
minutes after its request is a trigger that will not run, or a backlog the
owner should see. `requestPending` in `web/console/src/lib/sync.ts` answers
false once the request is `REQUEST_WAIT_MS` old with no newer
`lastSyncStartedAt` and no `syncRequestedAck`, and `requestUnstarted` answers
true. The account then reads as failing again: its row says "The sync you
asked for 15m ago hasn’t started", the provider's sync problem carries the
same sentence under its reason, and the fixes stay **Sync now** and, on an
OAuth provider, **Reconnect**. The age is measured with `serverNow`, a lower
bound of the server's clock, so a browser clock running ahead cannot end the
wait early, the reasoning 0176 used for the request's own stamp.

A new trait property would change every provider's declaration for a reading
only the console makes. A wake sent by the console races the dispatcher's own
delivery of the same change, and its reply says only that a scan ran, not
that the request's run started.

### Consequences

- Good, because a failure is hidden for at most fifteen minutes, and the text
  then names why it shows again.
- Good, because no engine, trait or provider changes.
- Bad, because on a repository whose trigger backlog is longer than fifteen
  minutes, the row says the sync has not started for a request that will
  still start; it corrects itself when the run stamps its start, but the
  owner may press Sync now again and queue a second run.
- Bad, because the bound is the console's alone: `substratectl sync status`
  still reads `pending` in REQUESTED and the API carries no bound.
- Bad, because the row changes on the console's minute tick or the next
  refetch, not at the exact second the wait ends.

### Confirmation

`requestPending` and `requestUnstarted` in
`web/console/src/lib/sync.test.ts`; the `syncWords` cases and "a failing
account asked to sync again" in `web/console/src/lib/providers.test.ts`.

## More Information

Amends the second Bad consequence of
[0176](0176-a-reconnect-requests-a-sync.md). Revisit if the engine gains a
stamp that says a request's run started or was dropped.
