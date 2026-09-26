---
type: breaking
release: v0.76.0
---

# Write trigger delivery rows as `substrate.reamde.dev/core/triggerrun`

From v0.76.0 the engine records each trigger delivery attempt as a
`substrate.reamde.dev/core/triggerrun` record. Before, it wrote
`substrate.reamde.dev/core/run`. The properties are the same kind of row
(`trigger`, `status`, `callable` and the rest), but nothing moves the old
rows: a repository created before v0.76.0 keeps a dormant `core/run` kind
holding the history it had, and a fresh repository never has it.

This hits any client or console bookmark that lists delivery attempts by
kind:

```http
before: GET /api/v1/substrate.reamde.dev/core/run?filter={"properties":{"status":{"eq":"parked"}}}
after:  GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/triggerrun"],"properties":{"status":{"eq":"parked"}}}
```

The route change in that example is the GraphQL removal of the same release.

## What to do

1. Replace `substrate.reamde.dev/core/run` with
   `substrate.reamde.dev/core/triggerrun` wherever a client lists or watches
   delivery attempts.
2. To read attempts from before the upgrade, list the old kind as well; the
   engine no longer writes or prunes it.
