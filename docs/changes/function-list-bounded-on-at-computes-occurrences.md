---
type: breaking
---

# A function's `host.records.list` bounded on `at` computes occurrences

A function body's `host.records.list` (and `host.list`) and an agent's
`query` tool whose filter bounds `at` on both ends now answer the records
route's window read: the stored rows in the window and, merged by slot, the
occurrences computed from every series among the kinds read, with
`computed: true` and the id `<seriesId>_<slot>` (decision record 0107).
Before, they answered the stored rows alone.

```python
page = host.records.list(["providers.substrate.reamde.dev/google/calendarseries",
                          "providers.substrate.reamde.dev/google/calendarevent"],
                         where={"at": {"gte": "2026-09-24T00:00:00Z",
                                       "lt": "2026-09-25T00:00:00Z"}})
# before: stored events only
# after:  stored events plus {"id": "abc_20260924T130000Z", "computed": True, ...}
```

The window read's rules now apply to such a list: `order` is `at` alone
(ascending or descending), `offset` is refused, and a window whose ends
meet is refused.

## What to do

1. Delete any RRULE expansion a body runs over a two-sided `at` list, or it
   will see each occurrence twice.
2. Drop an `order` other than `at` from such a list, and page it with
   `after` instead of `offset`.
3. Lists bounded on one end of `at`, or not on `at` at all, are unchanged.
