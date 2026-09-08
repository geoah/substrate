---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0056. A change cursor is a seq under a history generation

## Context and Problem Statement

A change cursor was one bare `seq`: `from=` on `GET /api/v1/changes`, on its
`watch=1` stream and on a collection's `watch=1`, held only to the retention
horizon, which is 0. [0017](0017-the-changelog-is-one-writers-total-order.md)
made `(repository, seq)` an entry's only identity and
[0052](0052-the-authority-is-the-repository-id.md) made the authority the
repository id, so a repository directory imported into a database that holds
no row for it comes back under the same id with a changelog whose seqs restart
at the copy's head. A client holding a cursor from the longer history the
database held before got an empty page and waited until the new history passed
its cursor; every replacement write below it was never delivered
([#374](https://github.com/geoah/substrate/issues/374)).
[0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)
rejected chains and signatures, so nothing in an entry proves which history a
seq belongs to; the fix has to be a marker.

## Considered Options

- A server-minted history generation on the `repositories` row, minted at
  registration and at import, carried beside `head` and in the bookmark, and
  required beside `from`.
- Bind the cursor to the entry checksum the client already holds
  (`from=500&hash=…`), read back at resume and compared to the stored `hash`.
- Rotate the marker only when the imported directory differs from the one the
  database last held.
- Leave cursors bare and document that a restore requires every client to
  reset by hand.

## Decision Outcome

Chosen: the history generation. `repositories.history_generation` is an opaque
string minted where the row is written (`insertRepositoryRow`,
`importRepositoryDir`), read once at open, and never changed by a restart or
`repository rebuild`, neither of which touches the row. A cursor is the pair
`(seq, generation)`: the bookmark frame is
`{"bookmark": N, "generation": "…"}`, a list envelope and a history page carry
`generation` beside `head`, and `from` or `before` above 0 needs `generation=`. The
server answers a cursor under another generation, above the head, or with no
generation with `410 compacted` naming the current head and generation;
`from=0` and `before=0` name no entry and need none. A list cursor carries the generation
of the head it was minted with and is refused across a change, so a `head`
from one history never reaches a watch on another.

The checksum binding was not chosen because it verifies one entry rather than
the numbering: it needs an entry read on every resume, a list page would have
to fetch its head entry's hash to hand off, and a bare `from` with no hash
would have to keep working unverified or break the same clients anyway. One
value per repository rides every envelope for free. Rotating only on a
differing directory was rejected because two copies of one directory carry
nothing that tells them apart; the manifest has no field that changes between
them. Bare cursors were rejected because the console and `substratectl watch`
both resume with nobody watching.

The generation qualifies 0017 rather than replacing it: an entry's identity is
still `(repository, seq)`; the generation says which numbering that identity
belongs to. It is a marker and not a proof, which is what 0050 allows.

### Consequences

- Good, because a saved cursor either resumes gaplessly or is refused with the
  position to re-list from, on every REST door alike; there is no silent
  continuation.
- Good, because a restart and a rebuild keep every cursor, since neither
  writes the row.
- Bad, because importing an unchanged directory into an empty database also
  rotates: every client resets once when nothing was lost.
- Bad, because `from=N` without `generation=` is a wire break: a client written
  to the bare cursor is refused until it sends the pair, and cursors saved
  before the migration are refused once, because the migration mints the first
  generation.
- Bad, because a second value rides every handoff, and the console, the CLI
  and every integration must carry the pair together.

### Confirmation

`TestResumeCursorIsHeldToTheHead`,
`TestRestoredHistoryResetsTheCursorAndLosesNothing` and
`TestListAndHistoryCarryTheHandoff` (internal/api) hold the wire rule on the
three doors.
`TestHistoryGenerationHoldsAcrossRestartAndRebuildAndRotatesOnImport`
(internal/engine) holds the row's lifecycle against a real import of an older
directory. `TestListRefusesACursorFromAnotherHistory` (internal/engine) holds
the list cursor.

## More Information

GraphQL `changelog(from)` is preview and stays unverified. Reopen if a path
appears that replaces a repository's history while the server is running: the
generation would then have to change under open datasets, which read it once.
