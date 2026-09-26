---
type: breaking
release: v0.70.0
---

# The server refuses every database migrated before v0.70.0

v0.70.0 replaces the 25 files in `internal/engine/migrations/` with one
`0001_init.up.sql` that builds the final schema. A database an earlier binary
migrated records migrations 2 to 25 and an older hash for 1, so the boot
refuses it before applying or serving anything:

```
substrate/engine: the database applied migrations this binary does not carry: 24 migration(s) recorded that this binary does not carry (it carries up to 1), ...
substrate/engine: 1 migration(s) this database applied are not the ones this binary carries. ...
```

There is no in-place upgrade. The data root carries over instead: a boot
against an empty database imports every repository directory under
`SUBSTRATE_DATA_ROOT`. That works only for a data root v0.69.0 wrote.
v0.69.0 removed the reads of older stores, so a `repository.json` with
`"format": 3` (written by v0.68.0 and earlier) is refused by v0.69.0 and
v0.70.0 alike.

## What to do

1. Stop the v0.69.0 server. Back up `$SUBSTRATE_DATA_ROOT` and
   `SUBSTRATE_CREDENTIAL_KEY`.
2. Create an empty database and point `DATABASE_URL` at it.
3. Start v0.70.0 with the same `SUBSTRATE_DATA_ROOT` and
   `SUBSTRATE_CREDENTIAL_KEY`. The boot creates each repository's row from
   its directory and replays its changelog.
4. For each repository, run
   `SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify <repository>`.
5. Expect clients to re-list once: the import mints a new history
   generation, so saved change cursors no longer resume.
6. On v0.68.0 or earlier, no path exists: register again on v0.70.0 and
   write the data back.
