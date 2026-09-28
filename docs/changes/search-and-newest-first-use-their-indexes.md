---
type: fix
---

# Word search and newest-first lists read their indexes

A lexical search or a list `search` filter for a word few records hold, and
a list ordered by `createdAt` or `updatedAt` (the default order), read an
index instead of every record in the repository. A word most records hold
is still matched record by record, which is the cheaper read for it. On a
repository of 389k records, this search spent 1.9 s counting the word's
documents and 2.3 s gathering candidates:

```sh
curl -H "Authorization: Bearer $TOKEN" \
  'https://substrate.example.com/api/v1/records?q=maruasa*&mode=lexical&first=8'
```

On a copy of that repository the count now takes 5 ms, and the query behind
`GET /api/v1/records?first=1` went from 190 ms to under 1 ms.

The first boot on this release runs migration 0010. It creates one SQL
function, `records_matching`, owned by `substrate_maint`, and builds no
index, so it finishes at once. A binary from before this release refuses the
database afterwards (`ErrDatabaseNewer`), so a rollback past this release
needs the database from before it.

A list continuation token minted before the upgrade under a `createdAt` or
`updatedAt` order is refused once with `422`; the client starts the walk
again.
