---
type: feature
---

# `filter.referencing.refs` reads what points at any of several records

A client that lists "records about me" across an owner's several addresses
made one `GET /api/v1/records` per address and merged the pages itself.
`referencing` now takes `refs`, a list of record paths, in place of `ref`: the
page is every record pointing at any of them, each record once, with the same
`property` narrowing, former-id matching and `matches` as the single form.

```http
GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/google/calendarevent"],
                            "referencing":{"refs":["providers.substrate.reamde.dev/google/emailaddress/work",
                                                   "providers.substrate.reamde.dev/google/emailaddress/home"],
                                           "property":"attendees"}}
```

`substratectl get <kind> --referencing <kind>/<id> --referencing <kind>/<id>`
sends the same filter. `refs` takes at most 256 paths. A longer list, or a
filter that sets both `ref` and `refs`, is `422 validation`. A filter with
`ref` alone reads as it did before.
