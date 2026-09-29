---
type: fix
---

# A purge erases the record's sealed secrets, and `repository verify` names orphans

A record purged by the GC sweep or by `DELETE ?purge=true` used to leave its
rows in the `sealed` table and its files under `sealed/` behind, so every
backup kept ciphertext no record pointed at. The purge now erases them in
the same transaction. Each GC sweep also erases the sealed rows that no
record, live or tombstoned, holds the ref of, which clears what earlier
purges left.

`repository verify` counts those rows and files and names each one as a
finding, so on a repository that purged a record holding a secret-typed
property (an `llm/provider` with an `apiKey`, a connected account), verify
and `repository snapshot` fail until the first GC sweep has run, five minutes
after the server boots:

```
$ substratectl --dsn "$DATABASE_URL" repository verify ada.example.com
repository ada.example.com
  sealed:   4 rows, 4 files, 1 held by no record
  FINDING:  sealed secret:3f9a0c1d2e4b5a6978c0d1e2f3a4b5c6 (substrate.reamde.dev/llm/provider old): an orphan: no live or tombstoned record holds the ref, so nothing reads the material
```

Start the server on this release and let it run one sweep, then verify
again. The erasure reaches the live table and the directory only: backups
and snapshots taken before it still hold the ciphertext, so a leaked key
must also be revoked at its provider.
