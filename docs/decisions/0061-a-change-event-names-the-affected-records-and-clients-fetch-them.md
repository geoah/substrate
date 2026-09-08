---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0061. A change event names the affected records with their versions, and clients fetch them

## Context and Problem Statement

Every change row on `GET /api/v1/changes`, on a collection's `watch=1` stream
and on GraphQL's `changelog` and `history` carried the engine's replay effects
under `payload.fold`: the `foldOp` union of `internal/engine/fold.go`, values
included. The console decoded that shape
(`web/console/src/lib/changelog.ts`), so a storage change was an API break:
changelog dialect 2 retired the `edge` effects and broke every decoder of
them. The documented payload keys (`properties`, `states`, `created`) name
what moved without a version, so no client could keep a current copy from
them alone, and `changefeed` cannot move from `beta` to `stable` with an
internal spelling on the wire
([#377](https://github.com/geoah/substrate/issues/377),
[0053](0053-rest-is-supported-all-of-graphql-is-preview.md)).

## Considered Options

- Identities and versions: each row carries `affected`, one `{kind, id,
  version, deleted}` per record the entry moved, projected at read from the
  stored effects; `fold` leaves every response; clients fetch the record.
- Values on the wire: a public form of `rowDelta` (`set`, `del`, `states`,
  `labels`) per affected record, redacted, so a client replays history
  without a fetch.
- Keep `fold` behind an opt-in (`?include=effects`) for the console and
  diagnostics, documented as preview.
- Compute the version at read by counting a record's effects up to the row's
  seq, so no entry format changes.

## Decision Outcome

Chosen: identities and versions. `substrate.Change` gains
`affected: []AffectedRecord`, `queryChanges` derives it from the stored `fold`
effects and deletes the key before the row leaves the engine, so REST, the
watch streams and both GraphQL doors serve one projection and none of them
carries an effect. The live fold stamps `version` on each `record`,
`tombstone` and `bump` effect it applies (`foldOp.Version`, ignored on
replay, since the fold reproduces the same count); a purge names the record
`deleted` with no version; an entry with no record effect names its addressed
`(kind, recordId)` with none. The console renders `affected` and no longer
knows the key `fold`.

The promise is bounded on purpose: a client that fetches each affected record
as the stream names it, and drops the deleted ones, holds a current copy, and
a copy at or past the named version need not fetch. No value and no history
travels on a public row. That is the same promise the trigger envelope
already makes by shipping the record's current state
(`internal/runner/protocol.go`).

Values on the wire were rejected because a public `rowDelta` re-couples the
contract to the delta grammar: the next retired effect kind would be the
dialect-2 break again, and redaction would have to follow every value shape.
An opt-in for `fold` was rejected because its one consumer, the console, is
the client this decision moves off it; an operator reads the effects in the
segment files and through `repository verify`, and a preview door can be
added if a need appears. Counting effects at read was rejected because it is
a scan of the changelog per page for a number the write already knows.

### Consequences

- Good, because the changelog's storage spelling can change without an API
  change: dialects are about replay, not about clients.
- Good, because a consumer can keep a copy with two fields and a fetch, and
  skip the fetch when its copy is current.
- Good, because no property value rides the feed, so the fold-shaped
  redaction (`redactChangePayload`) is gone rather than maintained.
- Bad, because `fold` leaving `payload` is a wire break for any client that
  decoded it; the console was the only known one, and `changefeed` is still
  `beta`.
- Bad, because entries written before this release (v0.47.0 and earlier)
  carry no `version` in their effects, so their events name records without
  one and a client fetches every time; nothing backfills them.
- Bad, because a record's history cannot be reconstructed from public rows;
  a consumer that needs values at each step must read the record on every
  event or wait for the optional event detail that is filed separately.
- Bad, because `foldOp.Version` is a stored field the replay ignores: a fold
  that ever stopped producing the same count would not be caught by it.

### Confirmation

`TestChangesNameEachAffectedRecordWithItsVersion`,
`TestAnEntryWithoutEffectsStillNamesItsRecord`,
`TestAClientKeepsACurrentCopyFromTheStreamAlone` and
`TestChangeRowsCarryNoPropertyValues` (internal/engine) hold the projection,
the fallback, the copy-keeping promise and the absence of values.
`TestEveryChangeSurfaceServesTheEventAndNoEffects` (internal/testenv) reads
the history page, the forward read, both watch streams and both GraphQL
doors over a socket and refuses a `fold` key on any of them.
`wire.golden.json` pins `Change.affected` and `AffectedRecord`, and the
console's `wire.golden.test.ts` holds `types.ts` to it.

## More Information

Depends on [0056](0056-a-change-cursor-is-a-seq-under-a-history-generation.md)
for the cursor the copy resumes from. Optional event detail (per-record
values, ops, redaction policy) is a separate ticket and would be a new record.
Reopen if a client outside this tree needs history from the stream, or if
transaction framing ([#363](https://github.com/geoah/substrate/issues/363))
wants the event to name its transaction: `affected` is per entry and composes
with a `txn` beside it.
