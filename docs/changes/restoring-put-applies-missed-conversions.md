---
type: fix
---

# A `put` restoring a tombstone rewrites it into the kind's current shape

A vocabulary apply that renames a property, respells an enum value with
`renamedFrom:`, adds `required:` with a `default:` or drops a property
rewrites live records only. A record deleted before that apply used to come
back from a restoring `put` in its old shape: holding `active` where the kind
now admits `working`, holding a property the kind no longer declares, or
refused outright with `props.mood: widget requires a value`. The restoring
`put` now rewrites the stored row first: a renamed name or spelling moves, a
value the kind no longer admits is removed, and a missing required value
receives its default. A property the `put` names keeps the `put`'s value.

```bash
# the kind declares `{value: working, renamedFrom: active}`; w1 was deleted holding `active`
substratectl apply -f w1.yaml   # restores w1, the document leaves `status` out
substratectl get convert.example.com/cv/widget w1 -o yaml
# data:
#   properties:
#     status: working
```

The restoring entry names each step under `renamed`, `remapped`,
`backfilled` or `nulled`, as a conversion's entry does, and
`GET /api/v1/changes?values=1` shows the removed values leaving there.
