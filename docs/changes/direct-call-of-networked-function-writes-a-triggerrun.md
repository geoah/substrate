---
type: feature
---

# A direct call of a networked function writes a `core/triggerrun` row

A `POST …/core/function/{name}/call` of a function that declares
`permissions.network`, or whose `permissions.call` grant reaches one that
does, now writes one `substrate.reamde.dev/core/triggerrun` row with
`mode: call` and no `trigger` (decision record 0106). The row holds the
`callableRef`, the `caller`, the `principal` (the token id), `startedAt`,
`finishedAt`, `status` (`ok`, or `failed` when the body ran and failed), the
applied `effects`, `outputBytes`, and `output` when it is at most 4096 bytes
of JSON. Call runs are never pruned.

A client that lists `triggerrun` rows and assumed `trigger` is always set
must allow for its absence. To read every direct call of one function:

```http
GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/triggerrun"],"referencing":{"ref":"substrate.reamde.dev/core/function/{name}","property":"callableRef"},"properties":{"mode":{"eq":"call"}}}
```
