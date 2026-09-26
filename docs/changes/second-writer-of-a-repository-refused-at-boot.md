---
type: breaking
release: v0.82.0
---

# A second server on one database does not boot: each repository has one writer lease

From v0.82.0 a server takes a Postgres session-level advisory lock per
repository when it opens it, on one connection pinned for the life of the
process (decision record 0083). A second server pointed at the same
database cannot take the lease and exits at boot, whether or not it shares
the first one's `SUBSTRATE_DATA_ROOT`. Before, two servers under two data
roots both ran and repaired each other's changelog rows at their next write.

This hits operators who run two servers against one database, including a
rolling update that starts the new server before the old one stops. The
second server fails with:

```text
substrate/engine: boot check: repository ada.example.com: substrate/engine: another
process is this repository's writer: repository ada.example.com. One server per
database: ...
```

If the pinned connection drops, the server refuses every write with
`503 unavailable` until its heartbeat takes the lease back.
`repository verify` and `repository reembed` take no lease and still run
beside a live server; every other operator command meets the lease.

## What to do

1. Run one server per database, and roll out so the old server stops
   before the new one starts (on Kubernetes, the `Recreate` strategy).
2. If a server refuses to boot with no other server running, find the
   holder in `pg_locks` with the query in `docs/operations.md` and end it
   with `pg_terminate_backend(pid)`.
3. Stop the server before an operator command that writes, such as
   `substratectl --dsn … user reset`. It is refused while the server holds
   the lease, even when run against a data root of its own.
4. Contributors: `mise run dev:stop` no longer stops Postgres, each tree gets
   its own database `substrate_<tree directory name>`, and `dev:wipe:all`
   removes the shared container.
