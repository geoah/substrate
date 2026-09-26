---
type: breaking
release: v0.75.0
---

# The catalog stops shipping seven sample packages

The catalog no longer lists these packages, and importing one answers `404`:

- `samples.substrate.reamde.dev/health`
- `samples.substrate.reamde.dev/fitness`
- `samples.substrate.reamde.dev/commerce`
- `samples.substrate.reamde.dev/routines`
- `samples.substrate.reamde.dev/journal`
- `samples.substrate.reamde.dev/food`
- `samples.substrate.reamde.dev/places`

```
POST /api/v1/catalog/samples.substrate.reamde.dev%2Fhealth/import
→ 404 {"error": {"code": "not_found", "message": "substrate: not found: bundle \"samples.substrate.reamde.dev/health\""}}
```

`substratectl import samples.substrate.reamde.dev/health` fails the same
way. `samples.substrate.reamde.dev/scheduling` stays.

A repository that imported one of them keeps its copy under its own
authority (for example `<authority>/health/medication`) and every record of
it. The catalog no longer offers an upgrade for that copy.

## What to do

1. For a repository that already holds a copy, nothing.
2. To declare one of these packages on a new repository, take its files from
   `samples/<name>/` at tag `v0.74.0` and declare them by hand. No catalog
   door serves them.
