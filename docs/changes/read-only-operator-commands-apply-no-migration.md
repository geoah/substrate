---
type: fix
---

# `repository verify` and `repository reembed` apply no migration

`substratectl repository verify` and `substratectl repository reembed` open
the engine read-only, beside a live server. Before, that open still applied
every schema migration the CLI carried, so a `substratectl` newer than the
server migrated the server's database and closed its rollback. Now the
read-only open applies nothing. It reads `schema_migrations` and refuses a
database missing a migration the CLI carries, naming each one:

```text
$ substratectl repository verify ada.example.com
error: open the substrate database: substrate/engine: the database has not
applied migrations this binary carries: 1 migration(s) pending, 10
(0010_records_matching), and this process opened the database read-only; open
it once with a process that writes (the server's boot applies them), or run
the substratectl of the release the server runs
```

A database holding a migration the CLI does not carry is refused as before.
Run both commands with the `substratectl` of the release the server runs.
`repository rebuild`, `rotate-generation`, `snapshot` and `user reset` need
the server stopped and still apply pending migrations, as the server's boot
would.
