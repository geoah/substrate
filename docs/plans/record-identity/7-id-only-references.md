# Proposal 7: id-only references

Drop the kind from a reference: a record is addressed by its id alone, so
the composition problem disappears instead of being solved. This requires
ids unique across the whole repository (today identity is the
`(repository, kind, id)` triple, and the records table keys on it) or a
registry search at every read, and a typed reference property loses its
pin: any id could name any kind, so admission checks the referent's kind
after the lookup instead of refusing the value by shape. Repository-wide
uniqueness is not the current state: the shipped tree already reuses one id
across kinds (`calendar.substrate.reamde.dev/calendar` names both a kind
declaration and a bundle record), the input convention wants a record named
`default` per configured bundle by design, and the merge trail
(`former_ids`) is keyed per kind too. A forward reference (legal today)
cannot be checked at all when the kind comes from a lookup with nothing yet
to find. This proposal composes with proposals 4, 5 and 6, which still need
to decide what a kind reference looks like.

```
today                   tasks.substrate.reamde.dev/task/t1     (kind, then id)
id-only                 t1                                     (unique in the whole repository)
a declaration           github.com:geoah:vocab:note            (the publisher makes it unique)
GraphQL                 { record(id: "t1") { title } }         (kind found by lookup)
```
