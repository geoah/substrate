---
status: accepted
date: 2026-09-19
decision-makers: George Antoniadis
---

# 0084. A records list pages by offset beside the keyset cursor

## Context and Problem Statement

The records list pages with an opaque keyset cursor alone
([pagination](../api.md#pagination)): the token carries the last row's
sort-key values, so the next page seeks past it. That walk is stable and flat
in cost, and it can only go forward one page at a time. The console's browse
footer therefore reads "page 2, 50 rows" with a Previous and a Next and
nothing else — a reader who knows a collection holds 1,200 rows cannot ask
for the last page, or for page 7, or see how many pages there are. Every
numbered pagination bar needs a page to be addressable by its number, and a
keyset position is not a number.

## Considered Options

- Number only the pages already walked, from the cursor stack the console
  keeps — no wire change, but the numbers grow as the reader explores and
  page 7 is still unreachable from page 1.
- Make the cursor transparent, so a client can synthesize the token for an
  arbitrary position — breaks the opacity the wire promises, and a sort key
  is not an ordinal anyway: there is no arithmetic from "page 7" to a key.
- Add an `offset` parameter beside `after`, the two being alternatives.
- Serve the numbered bar by fetching every intervening page — correct, and
  N round trips to reach page N.

## Decision Outcome

Chosen: **`offset` beside `after`, as an alternative and never a companion**,
because it is the only option that makes an arbitrary page directly
addressable, and the cost it carries falls entirely on the reader who asks
for it. A cursor seeks to a position in the order and an offset skips a count
of rows; sending both is refused (`422 validation`) rather than resolved by
precedence, because either precedence silently answers a page the caller did
not ask for.

Nothing that already walks changes. The seeded surfaces that must see every
row exactly once — the export, the console's bounded size probe, the agent
runtime's list tool — keep `after`, and the agent tool does not gain `offset`
at all: an agent reading a feed wants the stability, not the addressing.

An offset page still mints a keyset cursor from its last row, so a reader
that jumped to page 7 can hand off to a stable walk from there.

### Consequences

- Good, because a numbered pagination bar is now an ordinary client, with no
  stack of tokens to keep and no walk to reach a page.
- Good, because the guarantee each style gives is visible in the parameter
  name, so a caller that needs stability cannot get instability by accident.
- Bad, because the wire now has two continuation styles for one list, and
  every future paging feature has to answer for both.
- Bad, because an offset page is not stable: a concurrent insert or delete
  shifts every later page, so a row can be skipped or seen twice across two
  offset reads. This is inherent to offsets, documented, and the reason the
  keyset walk stays the default.
- Bad, because a deep offset costs what it skips — the rows before the page
  are ordered and then discarded — where a keyset seek is flat. The reader
  asking for page 400 pays for page 400.
- Bad, because the window read and the ranked read must refuse `offset` by
  name, so the list's grammar is no longer uniform across the three modes.

### Confirmation

`TestListOffset` in `internal/engine/query_db_test.go` holds the engine's
paging, the refusal of `offset` with `after`, and the cursor an offset page
still mints. `internal/api/records_test.go` holds the parameter reaching the
query and the window read's refusal. The console's numbered bar is held by
`web/console/src/components/data-table/data-table-pagination.test.tsx`.

## More Information

Amends the pagination contract in [api.md](../api.md#pagination), which
[decision 0079](0079-graphql-is-removed-and-the-records-read-is-one-route.md)
settled onto one route. Worth reopening if the list ever needs a stable
numbered page — that would take a materialized ordinal per row, not a
parameter.
