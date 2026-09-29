---
status: accepted
date: 2026-09-29
decision-makers: George Antoniadis
---

# 0141. A paged drain names its cursor by hash, and a park carries it whole

## Context and Problem Statement

[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)
made every motion of `paged_cursors` a `page` effect on a `delivery` entry,
and the effect carried the resume row whole, cursor included. A drain writes
one per page, so the changelog grew by the cursor's size on every page. Slack's
cursor holds its pending lists, about 380 KB at 9,000 items, and on one
repository its two triggers were 10.5 GB of the 15.8 GB of segment files
([#760](https://github.com/geoah/substrate/issues/760)); every `verify`,
`snapshot`, `rebuild` and boot import reads them. 0064 also promised that a
restore brings a parked drain back at its last committed page.

## Considered Options

- The cursor lives only in `paged_cursors`, a page entry records its hash, and
  a park carries the cursor whole.
- The same without the park's copy, so every restored drain starts over.
- The entry stores a delta against the previous page's cursor.
- The cursor goes to the blob store and the entry holds its digest.

## Decision Outcome

Chosen: the first. A middle page's `page` effect carries every column of the
resume row except the cursor, which it names by `cursorSha256` and
`cursorBytes`, the digest and length of the JSON the drain encoded; the bytes
go to `paged_cursors` alone (`pageTx`). Every park re-states the resume row of
the chain its failure names, cursor whole, in the park's own entry (`parkTx`,
`checkpointPagedCursor`). A replay stores the cursor an entry carries and JSON
null for one it names by hash. A drain that reads a null cursor starts the
chain over: the body runs from its first page under a fresh budget and
deadline, and the row's version stays as the fence (`loadPagedProgress`).

A page entry is a few hundred bytes whatever the cursor holds, so a drain of N
pages appends N small entries. The park's copy keeps 0064's promise where it
matters: a parked failure is the handle a retry resumes from, and parks are
rare where pages are not. Without the copy, every restore restarts every
parked drain. A delta is as large as the cursor on a chain's first page and on
any page that queues a long list, and its replay needs every earlier page of
the chain. The blob store writes a `core/blob` manifest record per page, which
the public feed serves, and a collection for every superseded cursor, and it
still writes the bytes once a page.

An older binary reads a hash-only page as a page with no cursor and stores
JSON null, the same row this binary's replay stores, so the changelog dialect
stays at 1.

### Consequences

- Good, because a page entry no longer grows with the cursor.
- Good, because a parked drain still resumes from its last committed page
  after a rebuild or an import.
- Bad, because a drain that stopped between pages without parking (a crash, a
  shutdown mid-drain) starts over from its first page after a rebuild or an
  import, and repeats the pages it had committed. A paged body keys its
  effects so a repeated page writes nothing new.
- Bad, because a rebuild no longer reproduces that row exactly: its cursor
  comes back null.
- Bad, because a park copies the cursor into the changelog, and every retry
  that parks again copies it again.
- Bad, because a body that pages with no cursor gets a fresh budget on each
  retry by hand, where the budget used to span retries.
- Bad, because entries written before this record keep their cursors whole.
  They replay as they did, and nothing reclaims their space.

### Confirmation

`TestAPageEntryStaysSmallAsItsCursorGrows` (internal/engine) drains a cursor
past 200 KB and holds each page's entry under 2 KB in the table and the
segment file, with the hash naming the stored cursor and the park carrying it.
`TestARebuildResumesAParkedDrainAndStartsAnInterruptedOneOver` rebuilds over a
parked chain, an interrupted one and an entry in the old shape, and asserts
each row and what the next delivery does.
`TestAMiddlePageNamesItsCursorWithoutCarryingIt` holds the encoding.
`TestARestoredRepositoryResumesItsDeliveries` and `TestReleaseAcceptanceDrill`
still resume a parked drain after an import.

## More Information

This amends the `page` effect of 0064; the rest of 0064 stands. Reopen if an
interrupted drain's restart costs too much: a rebuild over the same database
could keep the row's cursor where it matches the hash the ledger recorded,
which needs no change to the entry.
