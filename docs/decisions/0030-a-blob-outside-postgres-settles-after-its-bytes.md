---
status: accepted
date: 2026-08-17
decision-makers: geoah
---

# 0030. A blob outside Postgres settles after its bytes, behind a pending manifest

## Context and Problem Statement

Blob bytes were a `bytea` column, so the bytes and the `stored` manifest
committed in one transaction under the per-digest advisory lock: no reader
could ever see a `stored` manifest whose bytes were missing, and no crash could
leave one half without the other. The `fs` and `s3` backends
([#97](https://github.com/geoah/substrate/issues/97)) cannot join that
transaction, so the two writes come apart and something has to say what a crash
between them leaves.

## Considered Options

- Write the bytes first, then settle the manifest, and find the leftover
  objects by listing the store.
- Settle the manifest first, then write the bytes.
- Record the intent as a `pending` manifest, write the bytes, then settle to
  `stored`.

## Decision Outcome

Chosen: the `pending` manifest first, then the bytes, then `stored`. Each step
that touches Postgres takes the same exclusive per-digest advisory lock the
one-transaction path took.

`pending` and `BlobUploadGrace` already exist for exactly this window, and the
sweep already collects an unreferenced pending manifest, so the crash leftover
is a record Postgres holds rather than an object only a listing can find.
Settling first was rejected outright: it publishes a `stored` manifest whose
bytes are not there, which is the one state readers must never see.

The order gives three crash outcomes and no others:

- Crash before the manifest: nothing happened.
- Crash after the manifest, before the bytes: a `pending` manifest, no bytes.
  It never reads as a blob, and the sweep collects it once it is unreferenced
  and past the grace.
- Crash after the bytes, before `stored`: the same `pending` manifest with an
  object behind it. Collecting the manifest deletes the object with it.

`stored` is written in one place, and the guard that admits it probes the
backend for the bytes, so the invariant is enforced rather than assumed.

Collection keeps the mirror-image order: the manifest is tombstoned first and
the object deleted after the commit. A failure there leaves bytes no manifest
names, which the orphan sweep reaps on a later pass by listing the store under
the same lock; the opposite order would leave a live manifest pointing at bytes
that are gone.

### Consequences

- Good, because a reader never sees a `stored` manifest without bytes, on any
  backend, and the guard proves it per write rather than by construction.
- Good, because the crash leftover is discoverable in Postgres, so the common
  case never pays for a bucket listing.
- Good, because every step is idempotent: re-running an upload, a sweep or a
  migration is safe.
- Bad, because an upload on `fs` or `s3` appends two changelog entries (the
  pending mint, then the settle) where postgres appends one.
- Bad, because an upload is now two transactions and a byte write, so a client
  can observe a `pending` manifest that never becomes `stored`.
- Bad, because the orphan sweep enumerates the store, which costs a listing
  per pass on s3 (bounded, and resumed by a cursor).

### Confirmation

`internal/engine/blobs_external_db_test.go`: a byte write that fails leaves a
pending manifest and no readable blob, an object with no manifest is unreadable
and swept, and collection removes the object. The postgres backend keeps the
one-transaction settle, held by
`TestPostgresSettlesInTheCallersTransaction` in `internal/blobbytes`.

## More Information

The one-transaction settle is still what the default backend does: the store
interface declares it separately (`blobbytes.InTransaction`), the engine takes
it wherever it is offered, and a refactor that dropped it would fail the test
above rather than quietly move every deployment onto the two-step path.
