---
status: proposed
date: 2026-10-10
decision-makers: George Antoniadis
---

# 0148. An ongoing problem is one alert record, written under the callable it is about

## Context and problem statement

Nothing tells the owner when a callable stops working. On one repository
every agent run failed for 40 hours (864 runs) and a sync stored no mail for
two days while `sync status` read `ok`; both were found by hand
([#879](https://github.com/geoah/substrate/issues/879)). The parked
deliveries were there, but only in each trigger's `…/parked` list and in
`triggerrun` rows, one per delivery. The owner needs one durable signal per
problem that the console and `substratectl` can show, and that installed
code can react to. Whatever writes that signal runs inside the delivery
transactions, so it must not be able to wake the trigger whose failure it
reports.

## Considered options

- A core `alert` kind: one record per problem, its id derived from the
  problem's key, `state` a plain enum, written by the engine under the actor
  of the callable it is about, with a repeat throttle.
- The same kind with a random id and `key` declared `unique`.
- The same kind with `state` a `state` machine (`open` to `resolved`).
- The same kind written under the engine's own actor, `substrate`.
- No kind: derive health at read time from `triggerrun` rows and the parked
  failures.
- An inbox page in the console gathering alerts.

## Decision outcome

Chosen: the core `alert` kind, one record per problem under a key-derived id,
a plain enum state, written under the callable's actor with a five-minute
repeat throttle. The first source is parked deliveries: the transaction that
parks a delivery raises `trigger.parked/<trigger id>`, and so does the
open-time sweep that rewrites an agent run a stop interrupted as a park. The
transactions that retire parked rows (a retry that delivered, a forget, the
retirement of
[0142](0142-a-schedule-fire-that-settles-retires-its-triggers-older-parked-fires.md))
resolve it once none stand. The count is the trigger status's: an agent claim
a run in this process holds is a delivery under way, and one nothing holds is
parked work. Every write is an ordinary record write through the changelog,
so a rebuild replays it.

**One row per problem.** `unique` is reserved and inert
([0020](0020-dialect-keys-are-reserved-not-tolerated.md)), so nothing in the
store would stop two rows for one key. The record id is the key itself where
the key fits the record id alphabet, and an alphabet slug with a hash suffix
where it does not, so the upsert is a load by id and a put inside the
caller's transaction. One row per failure was rejected: `triggerrun` already
is that row, and 864 of them is the noise the issue complains about.

**A plain enum state.** A put cannot perform a state transition, and a problem
that comes back must reopen a resolved row from the same write path that
raised it. `syncState` is a string for the same reason
([0085](0085-a-sync-is-a-core-trait-the-dispatcher-stamps.md)). The owner may
put `resolved` by hand; the next occurrence reopens it.

**Written under the callable's actor.** A record trigger never receives writes
carrying its own callable's actor (`matchChanges`). So an alert about function
`F` written as `F` reaches every notifier over `core/alert` except `F`'s own
triggers, and a notifier whose code always fails parks once: the alert about
the notifier is its own write. Under the `substrate` actor that alert would
deliver to the notifier, park it again, and repeat until the throttle caught
it. 0085 stamps a sync as the callable for the same reason.

**A repeat throttle.** A repeat on an open row at the same level within five
minutes of the row's last write writes nothing; a level change, a resolve and a
reopen always write. Without it a trigger parking every few seconds writes an
alert per park and wakes every notifier per park.

**No inbox page, and an amendment to Home.** An alert is shown where its
records live: the provider's, the agent's and the tool's page list the open
alerts whose `about` names their records, with the error, the times and the
count. [0130](0130-the-console-serves-four-things-and-its-navigation-follows-them.md)
rejected an inbox, one "what needs you" surface gathering suggestions and
failing syncs, and the console's Home has asked nothing of the reader since.
This record amends 0130 on that one point: while an alert is open, Home shows
how many are open and up to five of them, each linking to the agent's or
tool's page it is about, with the full list one link away in the kind's
collection. That is not the inbox 0130 rejected. Nothing is gathered away from the page that
explains it, because Home only points at that page and nothing is decided on
Home. The engine's queues stay off Home: one alert stands for one problem
however many parked deliveries it covers, and the deliveries themselves are
listed on the trigger, as before. With no alert open, Home is what 0130
describes.

Deriving health at read time was rejected: retention prunes an ok run after
twenty newer ones, a hand retry that delivers writes no run row
([0091](0091-a-parked-delivery-is-retried-or-forgotten.md)), and a read-time
answer gives a notifier nothing to trigger on.

### Consequences

- Good, because a record trigger over `core/alert` is the notification hook:
  a webhook or an email is a function the owner installs, with no new
  configuration in the server.
- Good, because the console and the CLI read alerts through the one records
  route; no new route or wire struct exists.
- Bad, because `count` and `detail` lag inside the throttle window: a burst of
  parks shows the first park's count until a write five minutes later or an
  unpark recounts it.
- Bad, because `about` names the record of the last park that wrote the row,
  not every parked record.
- Bad, because deleting a trigger leaves its alert as it stood. The delete
  path holds the trigger's record lock before the registry-dependency lock a
  put takes, so a write there would break the lock order; the owner resolves
  it by hand.
- Bad, because a notifier never receives alerts about itself or about the
  agents it may run, so its own failures show only as its own alert row.
- Bad, because every park and every settlement that retires a parked row reads
  one more row, and counts the trigger's parked rows when an alert is open.
- Bad, because Home now asks for attention while an alert is open, which 0130
  kept off it.

### Confirmation

`internal/engine/alerts_db_test.go` and `alerts_internal_db_test.go`: a park
raises one alert and a second park inside the interval writes nothing; a
retry that delivers, a forget and a settled schedule fire resolve it; a park
after the resolve reopens it; a rebuild reproduces it; a trigger that parks
its own trigger or function record raises its alert; a restart that parks an
interrupted agent run raises it; a claim nothing holds keeps it open; and a
notifier over `core/alert` whose code always fails parks once. The notifier
test fails when the alert is written under the `substrate` actor.

## More information

The health signal of #879 (every run of a trigger failing for a window) is a
second source of the same kind, raised by the dispatcher and resolved by any
ok delivery. Revisit the throttle interval if a notifier needs every
occurrence, and the lock order if trigger deletion should resolve its alert.
