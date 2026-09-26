---
type: breaking
release: v0.73.0
---

# The `s3` blob backend and every `SUBSTRATE_BLOB_*` variable are removed

Blob bytes live in one place now: `$SUBSTRATE_DATA_ROOT/repositories/<authority>/blobs/<digest>`.
`SUBSTRATE_BLOB_STORE` and every `SUBSTRATE_BLOB_S3_*` variable are no
longer read, so a host that still sets them boots and ignores them. A
deployment that ran `SUBSTRATE_BLOB_STORE=s3` boots without its blob bytes,
because nothing reads the bucket (decision record 0075).

`snapshot.json` loses its `blobLocation` key, and its key set is closed. A
snapshot or export written by v0.72.0 or earlier carries the key, so
`repository verify` reports a `snapshot:` finding on it, and a v0.73.0
`substratectl export` refuses an archive from an older server:

```
the archive's snapshot.json does not read: json: unknown field "blobLocation"
```

The boot ignores `snapshot.json`, so restoring such a copy still imports it.

## What to do

1. With `SUBSTRATE_BLOB_STORE` unset or `fs`, remove the variable. Nothing
   else changes.
2. With `s3`, stop the server first. Copy each object
   `<prefix><authority>/<digest>` from the bucket to
   `$SUBSTRATE_DATA_ROOT/repositories/<authority>/blobs/<digest>`. Remove
   every `SUBSTRATE_BLOB_*` variable. Start v0.73.0 and run
   `substratectl repository verify <repository>` for each repository.
3. After the upgrade, take a new `substratectl export` or
   `substratectl repository snapshot` to replace any copy written before it.
