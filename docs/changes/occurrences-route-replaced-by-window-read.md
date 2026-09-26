---
type: breaking
release: v0.79.0
---

# Remove `GET /api/v1/occurrences`; a two-sided `at` filter computes occurrences

From v0.79.0 `GET /api/v1/occurrences` answers `404`. Recurring series are
expanded by the window read instead: a `GET /api/v1/records` list whose
filter bounds `at` on both ends returns the stored rows in the window and,
merged in slot order, one computed occurrence per slot of every series among
the kinds in play (decision record 0081). The recurrence traits moved to
core as `substrate.reamde.dev/core/recurring` and
`substrate.reamde.dev/core/override`; the scheduling sample no longer
declares `recurring`.

```http
before: GET /api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-08T00:00:00Z&limit=1000
        -> {"occurrences": [{"kind", "id", "title", "at", "log"}], "truncated": false}

after:  GET /api/v1/records?filter={"implements":"substrate.reamde.dev/core/temporal",
            "properties":{"at":{"gte":"2026-07-01T00:00:00Z","lt":"2026-07-08T00:00:00Z"}}}&orderBy=at
        -> {"records": [...], "cursor": "...", "head": 4211, "generation": "..."}
```

A computed occurrence is a record envelope with `"computed": true`,
`"version": 0` and the id `<seriesId>_<slot>` (slot in UTC,
`YYYYMMDDTHHMMSSZ`). `orderBy` on a window read is `at` alone.

## What to do

1. Replace calls to `/api/v1/occurrences` with the window read above, and
   page it with `first` and `after` instead of `limit`.
2. Treat a row with `computed: true` as read-only unless its kind binds
   `override`; then a `PUT` at its id materializes that one occurrence.
3. In your own kind declarations, bind `substrate.reamde.dev/core/recurring`
   instead of the scheduling sample's `recurring`. A bare trait name is
   refused in declarations from v0.91.0 (decision record 0098).
