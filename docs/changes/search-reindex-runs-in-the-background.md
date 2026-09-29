---
type: fix
---

# The search reindex after an upgrade no longer blocks the repository

When a new binary indexes text differently, the first open of each
repository re-derives every row's search index. That ran in one
transaction inside the open, so every request on the repository waited for
it: 24 minutes on a repository of 336k records, while `/healthz` answered.
It now runs in the background after the open, in transactions of 2000
rows. Reads and writes are served throughout, and a search finds each row
by its old index until the reindex reaches it. The log shows it advancing
kind by kind (a small repository):

```
INFO substrate: re-deriving the search index repository=ada.example.com from=1 to=2 kinds=9 rows=53
INFO substrate: re-derived the search index of one kind repository=ada.example.com kind=samples.substrate.reamde.dev/people/person rows=1 done=1 total=53
INFO substrate: re-derived the search index of one kind repository=ada.example.com kind=substrate.reamde.dev/core/kind rows=32 done=43 total=53
INFO substrate: re-derived the search index repository=ada.example.com from=1 to=2 rows=53 took=46ms
```

A shutdown before the last line leaves the old version recorded, and the
next open starts the reindex again.
