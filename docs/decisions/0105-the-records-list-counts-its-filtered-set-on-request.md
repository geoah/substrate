---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0105. The records list counts its filtered set on request

## Context and Problem Statement

The records list answers pages and never a total
([pagination](../api.md#pagination)). The console needs sizes everywhere: the
sidebar's collections, the home cards, a numbered pagination bar's last page
([0084](0084-a-records-list-pages-by-offset-beside-the-keyset-cursor.md)). It
has been deriving them by probing one-row pages at doubling offsets and
bisecting, about thirty requests for a 10,000-row collection, falling back to
a bounded keyset walk that answers `N+` past its ceiling. The owner asked for
a real count.

## Considered Options

- A `count=1` parameter on the list, answered as `count` beside the page.
- A separate count route (`GET /api/v1/records/count?filter=`).
- An always-present total on every page.
- Keep deriving the size in the client.

## Decision Outcome

Chosen: **`count=1` on the list, answered as an optional `count` on the
page**, because the number belongs to exactly the question the list already
answers — one filter grammar, one set of refusals, one snapshot — and a
second route would have been a second place to keep that grammar honest,
against [0079](0079-graphql-is-removed-and-the-records-read-is-one-route.md)'s
one records route. It is opt-in rather than always present because an exact
count scans every matching row, and a feed paging through a large kind should
not pay for a number it never shows.

The contract: the count is the size of the set the FILTER admits, whatever
`first`, `after` or `offset` the page carries; it honors every list arm and
excludes exactly what the list excludes; it is read in the page's snapshot;
zero is answered; `1` is the only accepted value. The window read refuses it,
because its page merges computed occurrences that are not rows. The ranked
read and the tail refuse it as a parameter they do not honor.

### Consequences

- Good, because a size is one request and exact, where it was dozens and a
  floor past a ceiling.
- Good, because a page and its count come from one snapshot, so a numbered
  bar's last page agrees with the rows it pages.
- Good, because an older server refuses the unknown parameter by name, so a
  client detects the feature from the answer and keeps its fallback.
- Bad, because the cost is unbounded and uncapped: a count visits every
  matching row, and a reader asking for it over a million rows pays that on
  every request. Postgres has no cheaper exact answer.
- Bad, because every future list feature must now say what it does to the
  count, as it already must for `after` and `offset`.
- Bad, because the window read has no count, so a calendar cannot size its
  window by this route.

### Confirmation

`TestListCount` in `internal/engine/query_db_test.go` holds every arm, the
tombstone and merge exclusions, and paging leaving the number alone.
`TestRecordsListCount` and `TestWindowRefusals` in `internal/api/` hold the
parameter's spelling, the absent key, and the refusals. The console's
`countRecords` tests in `web/console/src/lib/api/records.test.ts` hold the
server count and the fallback against a server without it.

## More Information

Amends the list contract in [api.md](../api.md#counting-the-filtered-set).
Worth reopening if a count is ever wanted cheaper than exact, which would be
an estimate from the planner's statistics and a different field, never this
one answering approximately.
