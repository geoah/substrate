---
type: breaking
release: v0.71.0
---

# `POST /recovery/enroll` is removed and `upgrade.renames` leaves the wire

Two removals, both for clients older than the server.

`POST /recovery/enroll` and `substratectl recovery enroll` are gone. The
server no longer routes the path. Registration is the only writer of the
`recoverykey` record, and nothing adds one later.

The `upgrade` object no longer carries `renames`. It appears on a catalog
entry (`GET /api/v1/catalog`, `GET /api/v1/catalog/{id}`) and in
`GET /api/v1/vocabulary/upgrade`. Each rename was always listed in `steps`
too, as `"step": "rename"`:

```json
{"available": true, "from": 3, "to": 4,
 "renames": [{"kind": "ada.example.com/tasks/task", "from": "due", "to": "dueAt", "records": 12}],
 "steps": [{"step": "rename", "kind": "ada.example.com/tasks/task", "property": "dueAt",
            "from": "due", "to": "dueAt", "records": 12}],
 "work": 12, "lossy": false}
```

From v0.71.0 the same object has no `renames` key.

## What to do

1. Read renames from `upgrade.steps`, keeping the entries whose `step` is
   `rename`. `kind`, `from`, `to` and `records` hold the values `renames`
   held.
2. Remove any call to `POST /recovery/enroll` or
   `substratectl recovery enroll`. There is no replacement.
