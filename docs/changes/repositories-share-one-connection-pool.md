---
type: fix
---

# Repositories share one Postgres pool, capped by `SUBSTRATE_REPOSITORY_CONNECTIONS`

A server used to open a connection pool per repository and close none of
them, so a host with a few hundred repositories ran Postgres out of
connections and every read failed with `sorry, too many clients already`.
Every repository now draws from one pool of `SUBSTRATE_REPOSITORY_CONNECTIONS`
connections (default `16`, at least `4`), one repository takes at most half
of the pool and never more than eight, and a process holds at most the cap
plus 12: 4 for its admin pool, 5 for its maintenance pool, 2 for repository
migrations and 1 for a commit-time catch-up. The default holds a process to
28.

A deployment running up to three servers on a Postgres with the stock
`max_connections=100` fits them in 3 x (18 + 12) = 90, with 10 left for
operator sessions:

```
SUBSTRATE_REPOSITORY_CONNECTIONS=18
```

The shared pool publishes `substrate_db_pool_*` series under
`pool="repositories"` at `GET /metrics` (with `SUBSTRATE_METRICS=true`).
