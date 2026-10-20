---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0176. A reconnect requests a sync

## Context and Problem Statement

An OAuth account whose token refresh failed keeps reading as failing after
the owner reconnects it. The reconnect cleared `syncError` and `syncErrorAt`
and left `syncState` at `erroring`, for the next run to settle. Nothing asked
for that run: a provider's on-connect trigger fires only on an account that
never synced, its on-request trigger only on an unanswered
`syncRequestedAt`, and the schedule runs when it is due (40 minutes later in
the case that raised this). Until then the console's provider page said
"Syncing … is failing" with the refresh error, about an account the owner had
just fixed.

## Considered Options

- Leave the engine alone and have the console hide the failure for a while
  after `tokenStatus` reads connected again
- Have each provider's on-connect trigger also fire on a reconnect
- Have the reconnect stamp `syncRequestedAt`, and have the console read a
  failing account with a request its sync has not started as being checked
  again

## Decision Outcome

Chosen: the reconnect stamps a sync request, and the console reads a pending
request as a check in progress.

The OAuth callback's one patch on an account whose kind binds the core `sync`
trait, that has synced before (`lastSyncedAt` is set) and is not paused,
writes `syncRequestedAt` with the callback's instant beside `tokenStatus` and
the cleared error pair. A reconnect is the owner's own act, begun with their
token, so the request stands at the owner tier, the tier `writer: owner`
needs, while the facility's own properties stay at the machine tier and the
error pair at the bundle tier: the write holds each property to its own
writer at its own tier (`txn.propTiers`). It is one changelog entry, so the
provider's on-request trigger sees the new status and the request together
and fires once. A first connect stamps nothing: the on-connect trigger runs
the first sync, and a request beside it would run the backfill twice.
`syncState` is still left alone.

The console treats a failing account (`syncState: erroring`, or, on a record
that carries no `syncState`, a legacy `syncStatus` that begins with
`erroring`) whose request is newer than both `lastSyncStartedAt` and
`syncRequestedAck` as being checked again: the provider page raises no sync
problem for it, the account row says "Checking again…", and the card's line
says it is syncing. When the requested run starts, the dispatcher stamps
`running` and a newer `lastSyncStartedAt`, and the account reads as syncing.
Where `syncState` is present it wins over the legacy string, which a bundle
rewrites only when a run ends, so the old failure in it does not return while
the run is in flight. A run that fails stamps `erroring` with its own reason,
so the failure comes back.

The same rule covers Sync now. Its request is dated by the server's clock as
the console last read it, from a response's `Date` header, which is never
later than the server's own instant: compared with the server-stamped
`lastSyncStartedAt`, a request dated by a browser clock running ahead would
read as waiting after its run had started and parked, and hide that run's
failure.

A console-only delay would guess at how long a check takes and show either a
stale failure or a hidden one, and nothing would run the check. Firing the
on-connect trigger on reconnects means changing every shipped provider's
guard and still leaves no record of the request a reader can compare against.

### Consequences

- Good, because the failure clears the moment the owner reconnects and comes
  back with the new reason when the requested run fails.
- Good, because it is generic: no provider's triggers change, and the console
  names no provider
  ([0166](0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md)).
- Bad, because a write now holds properties at different tiers, which is
  machinery for one caller that a later caller could reach for too.
- Bad, because a request whose run never starts (an on-request trigger that
  is disabled or missing) keeps the account reading as being checked until a
  run starts or the request is answered.

### Confirmation

`TestOAuthReconnectRequestsASync`, `TestOAuthFirstConnectRequestsNoSync`,
`TestOAuthReconnectOfAPausedAccountRequestsNoSync` and
`TestOAuthReconnectRequestDeliversOnce` in
`internal/engine/oauth_db_test.go`; the "a failing account asked to sync
again" cases in `web/console/src/lib/providers.test.ts`, `requestPending`
and `healthOf` in `web/console/src/lib/sync.test.ts`, and `requestSync` in
`web/console/src/lib/api/sync.test.ts`.

## More Information

Builds on [0085](0085-a-sync-is-a-core-trait-the-dispatcher-stamps.md), the
`sync` trait and the dispatcher's stamps this reads. The reconnect still
leaves `syncState` to the next run, and now asks for that run. Revisit if a
provider's sync cannot be asked for on demand.
