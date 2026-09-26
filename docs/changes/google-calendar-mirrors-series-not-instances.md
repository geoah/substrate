---
type: breaking
release: v0.79.0
---

# Mirror Google Calendar recurring events as one series row plus exceptions

Before v0.79.0 the Google provider's calendar sync wrote one
`providers.substrate.reamde.dev/google/event` row per occurrence of a
recurring event, up to 365 days ahead. From v0.79.0 it writes a recurring
master as one `providers.substrate.reamde.dev/google/series` row carrying
the rule (`recurrence`, `rdates`, `exdates`), a modified occurrence as an
`event` row with `recurrenceOf` and `originalAt`, and never a row for an
unmodified occurrence. The first sync after the upgrade re-reads each
calendar in full and deletes every legacy per-occurrence row.

This hits a client that lists Google events for a date range as a plain
list: recurring meetings disappear from it. Ask for a window instead, which
computes the occurrences from the series rows:

```http
before: GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/google/event"],
            "properties":{"at":{"gte":"2026-07-01T00:00:00Z"}}}
after:  GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/google/event",
            "providers.substrate.reamde.dev/google/series"],
            "properties":{"at":{"gte":"2026-07-01T00:00:00Z","lt":"2026-07-08T00:00:00Z"}}}
```

The two kinds were renamed `calendarevent` and `calendarseries` in v0.88.0.

## What to do

1. Read calendar ranges with both bounds on `at` and both kinds in
   `filter.kinds`, as above.
2. Follow an exception to its series through its `recurrenceOf` reference.
3. Nothing for stored data: the first sync after the upgrade converts it.
