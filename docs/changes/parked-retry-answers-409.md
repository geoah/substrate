---
type: breaking
release: v0.94.0
---

# A retry of a parked delivery that fails again answers `409` `parked`, not `500`

Clients and scripts retrying parked trigger deliveries are hit, including
`substratectl trigger retry`. When the retried delivery runs and fails
again, `POST /api/v1/substrate.reamde.dev/core/trigger/{id}/parked/{fid}/retry`
answered `500` `internal`. From v0.94.0 it answers `409` with code `parked`,
and the message names the new error's first line:

```
trigger <id>: parked delivery <fid> ran again and failed, it stays parked at attempt <n>: <first line of the error>
```

The row stays parked, one attempt older. A retry that succeeds still answers
`200` with `{"ran": …}` and deletes the row.

## What to do

1. Treat `409` `parked` from the retry route as "the delivery still fails":
   fix the callable, then retry or forget the row
   (`DELETE …/trigger/{id}/parked/{fid}`).
2. Stop treating a `500` from this route as the failed-again outcome; a
   `500` now means a server fault.
