---
status: accepted
date: 2026-09-19
decision-makers: George Antoniadis
---

# 0085. A sync is a core trait, and the dispatcher stamps it around a delivery

## Context and Problem Statement

Every shipped provider's sync function reports its work onto the account
record as ad hoc properties: `syncStatus` as free text (`ok (12 skipped)`,
`erroring: github returned HTTP 403`), `lastSyncedAt`, and per-stream
cursors and anchors under names each bundle chose. Nothing in the substrate
recognises the convention, so the console shows token status only, the CLI
has no sync read, and a second author's bundle would repeat the convention
by imitation or invent another. [0019](0019-a-lifecycle-is-a-state-machine-only-where-the-substrate-owns-it.md)
named `syncStatus` as a status its rule did not explain and left it. The
Connections work needs one shape the console can render without knowing the
provider, and an engine-side floor so a body that crashes cannot leave a
record claiming a run that is not happening.

## Considered Options

- A core `sync` trait the account kind binds, its properties written by the
  body through its effects, and the dispatcher stamping a record-sourced
  delivery's start, finish and park onto the same record under the callable's
  own actor.
- The same trait, with `syncState` a `state` machine with transitions and
  stamps.
- A fourth `writer:` role for the engine's hand, beside `oauth`, `connector`
  and `owner`.
- A separate `syncrun` kind the dispatcher writes, joined at read.
- Leave the convention in the bundles and have the console read the fields
  by name.

## Decision Outcome

Chosen: the core `sync` trait with a string state, stamped by the dispatcher
as the callable. The trait contracts twelve properties by datatype
(`syncState`, `syncMessage`, `lastSyncedAt`, `lastSyncStartedAt`,
`lastSyncDurationMs`, `syncRequestedAt`, `syncRequestedAck`, `syncPaused`,
`syncProgress`, `syncError`, `syncErrorAt`, `syncStreams`); the binding kind
declares each with its `writer:`, the owner's two hands (`syncRequestedAt`,
`syncPaused`) apart from the connector's ten. Around a record-sourced
delivery of a binding record whose guard passed, the dispatcher writes
`running` and the start instant before the body runs, `ok` and the duration
in the transaction that commits the last effects (only where the body left
the state at `running`), and `erroring` with the cause in the transaction
that parks. A paused record's deliveries settle as skips. At open, a record
still `running` becomes `erroring`, because [0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)
says no delivery of it can be in flight. `GET /api/v1/sync/status` joins the
trait's properties with the record triggers on the kind, and `substratectl
sync status` prints it.

The state is a string, not a machine, because 0019 rules: the substrate and
the body both write it and there is no move between its five words worth
refusing, so a machine would only refuse a sync. A trait contracts a
datatype alone, so the set (`never`, `running`, `ok`, `erroring`,
`throttled`) is held in prose and in the engine's constants, exactly as
`tokenStatus` is. No fourth writer role, because the dispatcher stamps under
the callable's actor at the bundle tier, the write context it already
builds for the body's effects: the binding kind's `writer: connector` holds,
and a record trigger never delivers its own callable's writes, so a stamp
cannot fire the trigger that made it. A separate run kind was rejected
because the `triggerrun` ledger already is one, and what the page needs is the
record's current state, not another log. Reading the fields by name was
rejected because it works for one author's bundles and is a guess for anyone
else's.

### Consequences

- Good, because any provider that binds the trait renders on the Connections
  page, the record page and the CLI the day it binds, and a crashed body
  cannot leave a stale `running`.
- Good, because the stamps ride the delivery's own transactions: no reader
  meets a parked delivery whose record still says `running`, or effects
  committed with no settlement recorded.
- Bad, because a schedule-sourced delivery names no record, so the
  dispatcher stamps nothing around the shipped bundles' hourly runs; there
  the body's own `syncState` and `lastSyncedAt` writes are the whole truth,
  and `running` is never visible.
- Bad, because a request against a multi-stream provider needs a per-stream
  acknowledgement inside `syncStreams` for its guards, which the trait can
  only describe in prose; a single-function provider uses `syncRequestedAck`
  alone.
- Bad, because the shipped bundles now carry both shapes until their next
  major, and the legacy `syncStatus` strings stay writable beside the trait.

### Confirmation

`internal/engine/sync_db_test.go` pins the running-then-ok stamps, the park's
`erroring`, the paused skip and the status read's join;
`internal/api/verbs_test.go` pins the route at the version root; the wire
golden pins `SyncStatus`, `SyncProgress` and `SyncStream`;
`web/console/src/lib/sync.test.ts` and `pages/connections.test.tsx` pin the
console's reading of the trait and the verbs it sends; `kinds/kinds_test.go`
loads the Google bundle bound to the trait.

## More Information

Revisits the `syncStatus` half of 0019's open bullet; `tokenStatus` stays
as it was. Revisit if the dispatcher learns which records a schedule fire
touched (a widened effect capture would let it stamp those too), or if the
trait dialect grows a way to contract a value set.
