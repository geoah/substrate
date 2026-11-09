# @substrate/client

The Substrate REST API from a browser, with no framework: what every other
web kit package builds on.

## Records

```ts
import { SubstrateClient } from "@substrate/client"

const client = new SubstrateClient("https://substrate.example.com", token)
const page = await client.listAll({
  kinds: ["substrate.reamde.dev/template/item"],
})
const one = await client.record("substrate.reamde.dev/template/item", id) // null when gone
await client.put(kind, id, { name: "Milk" }, 0) // ifVersion 0: create only
await client.patch(kind, id, { name: "Oat milk" }, 3) // held to version 3
await client.delete(kind, id) // restorable; one already gone is done
```

`aggregate` is [the aggregate read](../../../../docs/api.md#the-aggregate-read),
typed whole: the request (`metric` or `metrics`, `property`, `latest`,
`range`, `bucket`, `reducers`, `from`, `sources`, `groupBy`, `budget`,
`fill`, `rolling`, `unit`) and the answer (`units`, `bucket`, `exact`, rows
with their group, values, sources and count, and `head`, which is zero
under an app's token). The filter and the downsample go beside it:

```ts
const week = await client.aggregate(
  {
    metric: { ref: "substrate.reamde.dev/observations/metric/health/steps" },
    range: {
      gte: "2026-08-10T00:00:00+03:00",
      lt: "2026-08-17T00:00:00+03:00",
    },
    bucket: { calendar: "day", timezone: "Europe/Athens" },
    reducers: ["sum"],
    sources: { policy: "priority" },
  },
  {
    filter: { kinds: ["substrate.reamde.dev/observations/observation"] },
    signal,
  }
)
week.rows.map((r) => [r.start, r.values[0].value]) // decimal strings, never rounded
```

A refusal throws a `SubstrateError` with the envelope's `code`, `status`,
`problems` and `problemDetails`, `words` for a screen, and `conflict`,
`transient` (429 or 5xx), `retryAfter` and `insufficientScope`. `describe`
turns anything thrown into words, a network failure included.

## Kinds, declared once

```ts
import { defineKind, stringProp } from "@substrate/client"

export const Item = defineKind("substrate.reamde.dev/template/item", {
  read: (r) => ({
    id: r.id,
    version: r.version,
    name: stringProp(r, "name") ?? "",
  }),
  write: (draft: { name: string }) => ({ name: draft.name.trim() }),
})
Item.read(record) // the app's own value
Item.write({ name: " Milk " }) // { name: "Milk" }
Item.ref(id) // a reference property's value
```

A reference not named in full (`<authority>/<package>/<name>`) throws at
definition, as every surface refuses one.

## The change feed

```ts
import { follow } from "@substrate/client"

const stop = new AbortController()
follow(
  client,
  kinds,
  cursor,
  (e) => {
    // opened (with the cursor), row, relist, interrupted, signedOut,
    // needsApproval, ended
  },
  stop.signal
)
```

It resumes from the last row after a drop, takes a stream silent for 75 s
for dead, and asks for a relist on a 410 or a `compacted` frame.
`parseWatchFrame` reads one ndjson line.

## Finding a server

`locate(typed, signal, requirement)` reads an address, or a repository's
`_substrate` TXT record over DNS over HTTPS (`parseSubstrateTXT`), its
`repository.json`, or its parent domain. With a `requirement`
(`{ minimumVersion, scopeVerbs, detailTypes }`), a server that cannot meet it
throws `ServerTooOld` with what it cannot do yet, in words that finish "It
can't yet …" (`judgeServer`). `parseServerVersion`, `compareServerVersions`
and `displayServerVersion` order and say a build's version as the Swift kit's
`ServerVersion` does.

## Ids and positions

`uuidv7()` is the lowercase, time-ordered id a create is written under, so
every retry lands the same row. `positionBetween(a, b)` and
`positionAfter(list)` give exact decimal positions, written as strings,
which never run out of room.
