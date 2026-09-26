---
type: breaking
---

# A function's `host.records.list` and an agent's `query` bounded on `at` compute occurrences

A function body's `host.records.list` (and `host.list`) and an agent's
`query` tool whose filter bounds `at` on both ends now answer the records
route's window read (decision record 0107). The page carries the plain
events and overrides in the window and, merged by slot, the occurrences
computed from every series among the kinds read, each with
`computed: true`, `version: 0` and the id `<seriesId>_<slot>`. Series rows
leave the page: a series whose own `at` falls in the window used to be
listed as a row, and now only its occurrences appear, without `recurrence`,
`rdates` or `exdates`.

```python
page = host.records.list(["providers.substrate.reamde.dev/google/calendarseries",
                          "providers.substrate.reamde.dev/google/calendarevent"],
                         where={"at": {"gte": "2026-09-24T00:00:00Z",
                                       "lt": "2026-09-25T00:00:00Z"}})
# before: stored events and any series anchored that day, newest created first
# after:  stored events plus {"id": "abc_20260924T130000Z", "computed": True, ...},
#         `at` ascending, no series rows
```

The window read's rules now apply to such a list:

- `order` is `at` alone (ascending or descending); with no `order` the page
  is `at` ascending, not newest created first.
- `offset` is refused.
- A window whose `gte` is not before its `lt` is refused.
- A list over kinds none of which binds `temporal` is refused; it used to
  return an empty page.
- More than 10,000 matching series is refused; narrow the filter.

Bounds take the same forms as on a plain list: an RFC 3339 instant, a
zone-less date-time or a bare date, the last two read as UTC.

## What to do

1. Delete any RRULE expansion a body runs over a two-sided `at` list, or it
   will see each occurrence twice.
2. Treat a `computed: true` row as read-only. Skip it when writing back
   what you listed, or write the series under its own id. A patch at a
   computed id fails with `not found`, and a put at one creates a new
   record (an override where the kind binds `override`), so put at a
   computed id only to materialize that one occurrence on purpose.
3. Read series rows themselves with a one-sided `at` bound or by `ids`.
4. Drop an `order` other than `at` from such a list, and page it with
   `after` instead of `offset`.
5. A paged body whose stored continuation holds a list cursor from before
   the upgrade gets `bad cursor: not a window cursor`; restart that walk
   from the first page.
6. Lists bounded on one end of `at`, or not on `at` at all, are unchanged.
