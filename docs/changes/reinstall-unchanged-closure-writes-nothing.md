---
type: fix
---

# Re-installing an unchanged closure writes nothing and keeps every version

`POST /api/v1/catalog/{id}/install` (and a hand `POST
/api/v1/vocabulary/apply` of the same files) over a closure the repository
already holds unchanged appends no changelog entry and keeps the package's
and every declaration's version. Before, each re-install moved the package
and every kind up one (whoop 13, then 14, then 15), because a function's
`permissions.writes`, a bundle input's `kind` and a `timeout` compared unequal
to the spelling the stored row holds. A tool may install on every deploy:

```bash
substratectl install providers.substrate.reamde.dev/whoop
```

A closure that changes one of those values, a single reference or a single
duration, still lands at stored+1.
