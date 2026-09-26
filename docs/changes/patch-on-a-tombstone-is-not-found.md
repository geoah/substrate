---
type: fix
---

# `patch` on a deleted record answers `404` and writes nothing

A `PATCH` addressed to a tombstoned record (deleted, not yet collected) used
to succeed: the version climbed and the properties changed while
`deletedAt` stayed set, so every list still hid the row, and a mapping
source written that way minted subjects from it. It now answers
`404 not found` and changes nothing. A bundle function's `patch` effect onto
a tombstone fails its delivery the same way, as a patch onto a collected
record already did.

With the tasks sample imported into a repository whose authority is
`alice.example.com`:

```http
DELETE /api/v1/alice.example.com/tasks/task/t1
PATCH  /api/v1/alice.example.com/tasks/task/t1
       {"properties": {"name": "Renamed"}}
-> 404, error.code "not_found": record t1 is deleted; a put restores it
```

A `put` to the same path still restores the record. A patch that only
releases finalizers (`{"removeFinalizers": [...]}`) is still accepted on a
tombstone, because that is how a teardown lets the collector take it.

An effect `put` with `ifAbsent: true` now treats a tombstone as absent and
restores it, where it used to skip it. A function that mints a mirror with
`if_absent` and then patches it (the Slack, Notion, Linear, Beeper, Whoop and
GitHub providers do) therefore writes a deleted mirror back on its next sync.

## What to do

1. A client that patches a record it may have deleted: send `PUT` with the
   whole record instead, or expect `404` and follow it with a `PUT`.
2. A provider function that patches a mirror it may have lost: put the
   record first, then patch it. `if_absent=True` is enough:

   ```python
   host.effects.put(kind, eid, properties=props, if_absent=True)
   host.effects.patch(kind, eid, properties=props)
   ```
