---
status: accepted
date: 2026-08-18
decision-makers: George Antoniadis
---

# 0036. Operational lists answer with an `{items, cursor?}` envelope

## Context and Problem Statement

Before v1 freezes the REST contract, the operational lists each returned a
bare, unbounded array under a per-endpoint key: `/tokens` under `tokens`, the
catalog under `catalog`, trigger and bundle status under `triggers` and
`bundles`, parked deliveries under `parked`, trait implementors under `kinds`
([#127](https://github.com/geoah/substrate/issues/127)). None carries a
continuation marker. Adding pagination to any of them after v1 would reshape
its response body, a breaking change to every client. The record, history and
incoming lists already page with a keyset `{records, cursor, head}` envelope
and need no change.

## Considered Options

- Leave the operational lists as bare arrays and reshape each one if and when
  pagination is needed.
- Wrap every operational list in one `{items, cursor?}` envelope now, with
  `cursor` always absent until keyset pagination is added.
- Reuse the record `Page` shape (`records`/`cursor`/`head`) for the
  operational lists too.

## Decision Outcome

Chosen: one `{items, cursor?}` envelope for every operational list now. The
server fills `items` with the whole set and omits `cursor`, so the body shape
is fixed before v1. Adding keyset pagination later fills `cursor` and bounds
`items`, an additive change no client's parser breaks on. The record `Page`
stays separate: its `records` key names its element, and its `head` marker
carries the list-to-watch handoff that the operational lists have no analogue
for, so folding the two would burden the operational lists with a field that
means nothing to them.

### Consequences

- Good, because pagination lands on any operational list as a filled field,
  never a reshaped body.
- Good, because `Items` and `Cursor` are one Go type (`OperationalList[T]`)
  and one console type, so the six endpoints cannot drift apart.
- Bad, because every client that read `res.tokens` or `res.catalog` today must
  move to `res.items` at once: the rename is the breaking change, paid now to
  avoid paying it per endpoint later.
- Bad, because two list envelopes now exist (`OperationalList` and the record
  `Page`), and a reader must know which endpoints use which.

### Confirmation

`internal/api` tests assert each endpoint answers `{"items": [...]}`. The wire
golden (`internal/substrate/wire_test.go`, `wire.golden.json`) pins
`OperationalList` to `items`/`cursor`, and the console vitest fails until
`types.ts` mirrors it.

## More Information

Records, history and incoming keep their keyset `Page`
([internal/substrate/query.go](../../internal/substrate/query.go)). The
operational lists are `/tokens`, `/catalog`,
`{core}/trigger/status`, `{core}/trigger/{id}/parked`, `{core}/bundle/status`
and `{core}/trait/{id}/implementors`.
