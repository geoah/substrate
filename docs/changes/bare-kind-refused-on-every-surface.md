---
type: breaking
release: v0.92.0
---

# A bare kind, trait, function or agent name is refused on every surface

API clients, function bodies, trigger and policy authors, and
`substratectl` users are hit. Every place that took a bare word now takes
the full `<authority>/<package>/<name>` and refuses a bare one with `422`
`validation`, listing the spellings the repository declares. An unknown full
name stays `404`.

- `filter.kinds` on `GET /api/v1/records` (all three modes) and `kind` on
  `POST /api/v1/records`: a bare word was `404 unknown kind task`, now `422`.
- `filter.implements` takes a full trait: `substrate.reamde.dev/core/temporal`.
- `POST /api/v1/substrate.reamde.dev/core/function/{name}/call`,
  `…/core/agent/{name}/call` and `…/core/agent/{name}/chat`: `{name}` is the
  full identity, percent-encoded.
- A trigger's `source.record.kinds` and a policy's `selector.kinds` refuse a
  bare entry at write time.
- `host.records.list(["task"])` in a function body is refused.
- `substratectl get`, `patch`, `delete` and `search --kinds` refuse a bare
  kind, and `get`, `patch` and `delete` lose `--package`.

Before and after:

```
substratectl get task
substratectl get example.com/tasks/task
```

## What to do

1. Upgrade to v0.92.1 or later, not v0.92.0. On v0.92.0 a stored trigger
   with a bare `source.record.kinds` entry is skipped and a stored policy
   with a bare `selector.kinds` entry gates nothing; repository migration
   `0002_qualify_bare_selector_kinds` in v0.92.1 rewrites both at first open.
2. Replace every bare kind, trait, function and agent name in scripts,
   function bodies and agent tool calls with the full spelling the refusal
   lists. `substratectl kinds` prints every kind.
3. Drop `--package` from `substratectl` invocations and pass the full kind.
4. Treat `422` `validation` on the records route as a spelling error, not a
   missing kind.
