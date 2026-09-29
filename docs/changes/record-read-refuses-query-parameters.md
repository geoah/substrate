---
type: fix
---

# A single-record `GET` refuses any query parameter

`GET /api/v1/{authority}/{package}/{kind}/{id}` honors no query parameter.
Before this release it ignored every one it was sent and answered `200`, so a
client sending `expand`, a stale `withEdges` or a typo got the bare record
back and could not tell. It now answers `400 bad_request` naming the
parameter, the same body `GET /api/v1/records` gives:

```http
GET /api/v1/samples.substrate.reamde.dev/people/person/9f2k?withEdges=1

→ 400 {"error": {"code": "bad_request",
                 "message": "unknown query parameter \"withEdges\""}}
```

A query string that does not parse, such as `?filter=%ZZ` or `?a=1;b=2`, is
now `400 bad_request` naming the parse error on the record read, the records
route, `DELETE` and `/changes`. Before, the unreadable pair was dropped, so
`GET /api/v1/records?filter=%ZZ` listed every record.

The console and `substratectl` send neither and are unaffected.

## What to do

1. Drop every query parameter from a single-record `GET`. The read already
   carries `annotations`, so `withAnnotations=1` loses nothing.
2. For a record's referents, list it with
   `GET /api/v1/records?filter={"ids":[…],"kinds":[…]}&expand=…`.
3. Percent-encode `;` and `%` inside a query value (`%3B`, `%25`).
