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

The console and `substratectl get` send no parameter on this read and are
unaffected. A client that does drops the parameter; for referents, list with
`GET /api/v1/records?filter=…&expand=…` instead.
