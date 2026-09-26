---
type: breaking
release: v0.76.0
---

# Remove `POST /api/v1/graphql` and read every list at `GET /api/v1/records`

Any client that posted to `POST /api/v1/graphql`, listed a kind at its
three-segment path, or read `…/{id}/incoming` or `…/trait/{id}/records`
gets `404` from v0.76.0. Agents lose the `substrate.reamde.dev/core/graphql`
and `substrate.reamde.dev/core/mutate` tools. The record path
(`GET`/`PUT`/`PATCH`/`DELETE /api/v1/{authority}/{package}/{kind}/{id}`) is
unchanged. Every filter below is URL-encoded JSON in `?filter=`.

| Read | Before | After |
| --- | --- | --- |
| List a kind | `GET /api/v1/ada.example.com/tasks/task?filter={"properties":{"status":{"eq":"open"}}}&orderBy=dueAt`, or GraphQL `records(filter: {kinds: [...]}, first: 20)` | `GET /api/v1/records?filter={"kinds":["ada.example.com/tasks/task"],"properties":{"status":{"eq":"open"}}}&orderBy=dueAt&first=20` |
| Get one | GraphQL `record(kind, id)` | `GET /api/v1/ada.example.com/tasks/task/kq3v9x2m41pf` |
| Search | GraphQL `search(q: "quarterly review", mode: "hybrid", kinds: [...], k: 20) { hits { record lexical semantic } pending }` | `GET /api/v1/records?q=quarterly+review&mode=hybrid&filter={"kinds":["ada.example.com/notes/note"]}&first=20`, answering `{records, scores, pending}` with `scores` keyed by `<kind>/<id>` |
| Who points here | `GET /api/v1/ada.example.com/tasks/project/p1/incoming?property=project&fromKind=ada.example.com/tasks/task` | `GET /api/v1/records?filter={"referencing":{"ref":"ada.example.com/tasks/project/p1","property":"project"},"kinds":["ada.example.com/tasks/task"]}`, pointing sites in `matches` |
| Trait implementors' records | `GET /api/v1/substrate.reamde.dev/core/trait/{id}/records` | `GET /api/v1/records?filter={"implements":"substrate.reamde.dev/core/accountconfig"}` |
| Tail one kind | `GET /api/v1/ada.example.com/tasks/task?watch=1` | `GET /api/v1/records?watch=1&filter={"kinds":["ada.example.com/tasks/task"]}` |
| Create, server id | `POST /api/v1/ada.example.com/tasks/task` | `POST /api/v1/records` with `"kind"` in the body; a body `id` is `422` |

## What to do

1. Rewrite each call with the table. Put every kind in `filter.kinds`; a
   GraphQL inline fragment is not needed, because each record carries all
   its `properties`.
2. Replace a GraphQL join with `expand=<reference property>` on the list and
   read referents from `included`, keyed by record path.
3. Discard list cursors saved before the upgrade. A cursor binds its filter,
   and one from a replaced history answers `410 compacted`.
4. In each agent's `tools:`, replace `substrate.reamde.dev/core/graphql` with
   `substrate.reamde.dev/core/query`, and `substrate.reamde.dev/core/mutate`
   with `substrate.reamde.dev/core/write` (`{op, kind, id, input,
   ifVersion}`). An agent naming a removed tool is quarantined until it does.
5. Upgrade `substratectl` to 0.76.0 or later. Ranked search is
   `substratectl search <query>`; `get` takes `--expand` and `--referencing`.
