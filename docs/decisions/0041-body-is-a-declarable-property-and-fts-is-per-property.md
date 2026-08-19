---
status: accepted
date: 2026-08-19
decision-makers: George Antoniadis
---

# 0041. Body is a declarable, column-backed property, and FTS is per-property

## Context and Problem Statement

Every record carried a built-in `body`: an untyped text column
(`internal/engine/fold.go`), accepted on any write whether the kind wanted it
or not, and appended to the FTS band C unconditionally
(`ftsBands`, `internal/engine/validate.go`). `body` was reserved, so a kind
could not declare its own `body` property, could not type or describe it, and
could not keep it out of the search index. This is the body half of
[#68](https://github.com/geoah/substrate/issues/68); the title half stayed
reserved and derived under [0016](0016-a-kind-titles-itself-from-a-declared-property.md).

## Considered Options

- Keep `body` reserved and always-indexed, the state before this record.
- Drop the `body` column and store body as an ordinary jsonb property.
- Keep the `body` column as a hot optimization column, but make it opt-in: a
  kind declares `body`, the column backs it, and its `fts` flag decides
  indexing.

## Decision Outcome

Chosen: the third option. `body` stops being reserved. A kind that carries body
DECLARES a `body` property; its datatype must be text-family (`text` or
`markdown`), because the hot column is text, and a non-text `body` is refused at
load. A declared `body` maps to the existing hot column through the same
`columnProp` and write path that back `title` and the temporal columns, so no
migration and no jsonb move: the bytes stay where they were.

A kind that does not declare `body` no longer carries one: a write naming
`properties.body` on it is refused as an undeclared property, exactly like any
other undeclared name.

FTS unwelds from the column. `ftsBands` no longer appends the stored body
unconditionally. It appends `row.Body` to band C only when the kind declares
`body`, that property's `fts` is true, and it is not sensitive. So
`body: {type: text, fts: false}` is stored and served but never indexed, and
`body: {type: text}` (fts defaults true for text) indexes exactly as the built-in
column did. The `foldFTS` unknown-kind fallback keeps indexing the stored body:
an unknown kind has no declaration to consult, so indexing its body is the safe
default.

Dropping the column was rejected: body is up to thousands of characters of prose
on the kinds that carry it (GitHub issues and pull requests), and a hot column
reads and orders without unpacking jsonb.

### Consequences

- Good, because a kind's body is now typed, described, and searchable-by-choice
  like any other property, and a kind with no body carries none.
- Good, because indexing follows the same per-property `fts` flag the rest of
  the vocabulary already uses, so there is one rule for what gets searched.
- Bad, because a declared `body` bypasses `coerceProps` (it is routed to the
  column before validation, as `title` is), so `required`, `pattern` and `min`
  on a `body` property are not enforced. Body is a text blob in practice; this
  matches `title` and is left as is.
- Bad, because every kind that wrote the old built-in body must now declare
  `body` or its writes are refused. The GitHub `issue` and `pullrequest` mirrors
  gained a `body: {type: text}` declaration and their authority bumped.

### Confirmation

`internal/engine` FTS tests cover the three cases: a declared
`body: {type: text, fts: false}` is served but absent from search, a declared
`body: {type: text}` is found by its body text, and `properties.body` on a kind
that declares no body is refused. `internal/vocabulary` holds that a text `body`
loads and a non-text `body` is refused. `mise run kinds:check` holds the version
bump.

## More Information

[#68](https://github.com/geoah/substrate/issues/68) carries both halves;
[0016](0016-a-kind-titles-itself-from-a-declared-property.md) settled the title
half and is unchanged here. The function `source` property (the inline body of a
function) is a separate rename and is not touched by this record.
