# API consolidation: one records read, GraphQL removed

Status: IMPLEMENTED 2026-09-12. Joint: drafted by Claude, reviewed and revised
with Codex. The binding choices are decision record
[0079](../decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md);
this page stays as the record of the reasoning and the review that shaped it.

## Goal

Remove GraphQL and give the REST surface the three things GraphQL was kept
for: a cross-kind query, following references in one round trip, and ranked
search. Fold every "read some records" door into ONE route,
`GET /api/v1/records`, so a client, the console, the CLI and an agent tool
all learn one grammar.

This is not dogmatic REST. The model is Kubernetes': resource-oriented paths,
verbs as HTTP methods where they fit, one list grammar with `watch` as a mode
of the same list, opaque continue cursors, and a version precondition on a
write. What we take and what we leave is written below.

## What Kubernetes does, and what carries over

| Kubernetes | Substrate today | Verdict |
| --- | --- | --- |
| `/apis/{group}/{version}/{resource}/{name}` | `/api/v1/{authority}/{package}/{kind}/{id}` | keep; the record URL is its reference value (0033) |
| `GET …/{resource}` per resource; never across resources | `GET /api/v1/{a}/{p}/{k}` per kind | REPLACE with one cross-kind `GET /api/v1/records` |
| `labelSelector`, `fieldSelector` (indexed fields only) | `filter` JSON: kinds, implements, ids, properties, labels, deleted | keep ours; it is richer |
| `limit` + opaque `continue` | `first` + opaque `after` | keep; ADD the normalized filter to what the cursor binds, so a cursor replayed under a different filter is refused like a different `orderBy` is today |
| consistent snapshot across pages | fresh snapshot per page | do NOT claim it; document that a walk may see a row twice |
| `resourceVersion` on the list, `?watch=1` on the same route, BOOKMARK, `410 Gone` when too old | `head`/`generation` on the page, `?watch=1`, one opening bookmark, 410 on the watch | keep; make the list's generation mismatch answer 410 too; add periodic progress bookmarks |
| `resourceVersionMatch` | none | no |
| `metadata.resourceVersion` in the body → `409` | `ifVersion` in the body → conflict; optional | keep, optional |
| `fieldValidation=Strict` | strict decoding always | keep; do not add Warn/Ignore |
| `PATCH` with a content type per flavour | one merge patch | keep one |
| `dryRun=All` | `vocabulary/plan` only | not this pass; it is admission without persistence, a feature of its own |
| server-side apply, field ownership | none | no |
| `DELETE …/{resource}` (deletecollection) | none | no; a bulk delete is a script over a list |
| Table / metadata-only output | none | not this pass |
| `/status` sub-resource | `status` in the YAML envelope, server-owned | keep; NOTE it is a CLI/YAML shape only, the JSON wire record is flat |
| no search, no joins, no expand | GraphQL `search`, `target` | ADD as list parameters, below |

## The proposed surface

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/records?filter&orderBy&first&after&expand&withAnnotations` | the one list: distinct records, cross-kind, optional sidecars |
| GET | `/api/v1/records?q&mode&filter&first` | ranked records; `filter.kinds` only at first |
| GET | `/api/v1/records?watch=1&filter&from&generation` | the tail; `filter.kinds` only at first |
| POST | `/api/v1/records` | create under a server-assigned id; the body names `kind`; a supplied `id` is refused (PUT is the door for a chosen id) |
| GET/PUT/PATCH/DELETE | `/api/v1/{authority}/{package}/{kind}/{id}` | unchanged |
| — | every other route | unchanged |

Removed, after consumers move: `GET/POST /api/v1/{a}/{p}/{k}` and the three
405 stubs beside them, `GET …/{id}/incoming`, `GET …/trait/{id}/records`,
`POST /api/v1/graphql`, the reserved record id `incoming`.

`/changes`, `/occurrences`, `/merge`, `/split`, `/vocabulary/*`,
`/catalog/*`, `/blobs`, `/export`, `/tokens`, the trigger and bundle verbs,
`function/{name}/call`, `agent/{name}/call|chat` stay. They are operational
verbs, not record reads.

**The three modes are one route but not one grammar.** `filter` is shared;
each mode names which arms and which other parameters it honors and refuses
the rest by name, which is what `unsupportedParam` does today. The page
shape is shared too: `{records, cursor?, head, generation}` plus the
sidecars a mode adds. Selling the modes as uniform would be false; the
truth is one place to look and one filter to learn.

### The filter grammar gains one arm: `referencing`

```jsonc
{
  "kinds": ["ada.example.com/tasks/task"],
  "implements": "substrate.reamde.dev/core/schedulable",
  "ids": ["…"],
  "properties": {"status": {"eq": "open"}},
  "labels": {"…": {"exists": true}},
  "deleted": false,
  // NEW: records holding a reference AT this record.
  "referencing": {"ref": "ada.example.com/tasks/project/kq3v9x2m41pf",
                  "property": "project"}          // property optional
}
```

It replaces `GET …/{id}/incoming`. In the engine it is a correlated `EXISTS`
against the refs index inside the list SQL (the index carries both endpoints
and the site coordinates; `List` reads only `records` today, so this is the
one real engine addition). Two things the incoming read does must carry
over: the target is canonicalized and matched by its canonical id AND every
former id (`idsOf`), or every pointer older than a merge is missed; and
`fromKind` becomes `filter.kinds`.

What changes for the reader: the answer is a page of distinct RECORDS, the
same envelope as every other list. The `{property, path, from}` row shape
and the `total` go. One source can hold several matching sites (a top-level
property and a nested one), so the page carries a `matches` sidecar,
`{"<kind>/<id>": [{"property": "project", "path": ""}, …]}`, keyed by the
source's record path. The console's graph drawer reads `total` for a closed
group's count today and groups rows by property; it moves to one request per
property (its drill-down already does that) and counts what it gets, or a
later `count` mode is added. That is an explicit console change, not free.

### `expand` follows references one hop

`?expand=project,assignees` names reference properties whose targets the
server loads and returns beside the page under `included`, keyed by record
path, each referent once however many rows point at it:

```json
{"records": [...], "cursor": "…", "head": 4120, "generation": "…",
 "included": {"ada.example.com/tasks/project/kq3v9x2m41pf": {"id": "…", "kind": "…", "properties": {…}, …}}}
```

Sidecar, not inline: the wire record is flat (`id`, `kind`, `properties`,
`labels`, `version`, …) with no `status` key to hide a join under, so inline
would mean a new key on every record and a referent repeated per row. A
client that wants inline joins it in one map lookup.

Rules: ONE hop only in this pass (two hops and expanding a single-record
GET are cut as over-reach; both are additive later). A dangling pointer
expands to nothing and the `{ref}` value still says where it pointed. A
reference written under a former id resolves to the canonical record, which
means the loader is a per-kind `List` by `ids` (kinds and ids are independent
predicates, so one mixed query would overmatch the cross product) followed by
an alias pass through `canonicalOf`. Chunk at the 500-row cap, cap the
sidecar's record count and bytes, and say what a tombstoned referent does
(proposal: included, `deletedAt` set, like a direct GET). This is still better
than the GraphQL resolver, which did one `Get` per pointer.

### `q` ranks instead of filtering

`GET /api/v1/records?q=quarterly+review&mode=hybrid&filter={"kinds":[…]}&first=20`
is the `search` query. The engine already hydrates records into hits, so the
page is the records in rank order plus a `scores` sidecar,
`{"<kind>/<id>": {"lexical": 0.42, "semantic": 0.81}}`, and `pending`.

Constraints that are real, not stylistic: both ranking arms cap candidates
BEFORE hydration, so a filter must be pushed into the ranking SQL, not
applied to the top-k afterwards. Only `filter.kinds` is pushed today, so only
`kinds` is admitted with `q` at first and every other arm is refused by name.
`orderBy`, `after`, `expand` and `watch` are refused with `q`; `first` is
`k`. A ranked page has no `cursor`, and no `head` or `generation` either:
`Search` opens separate reads and returns no head, so the ranked page
carries `records`, `scores` and `pending` and claims no snapshot. Discovery's `search` feature gains `rest` and keeps its
`beta` stability, which is the maturity axis; 0053's compatibility axis is
untouched. 0022's fear of freezing the hit shape is answered by `beta`.

### Watch on the one route

`GET /api/v1/records?watch=1&filter={"kinds":[…]}&from&generation` is the
collection watch with the kind moved into the filter. `ChangeFilter` carries
kinds, ops, actors, exclusions, a record id and a substring `q`; it has no
labels, properties, implements, ids or deleted. So under `watch=1` the
`filter` admits `kinds` alone and refuses every other arm by name, exactly as
the collection watch refuses `filter` outright today. Post-filtering the
tail on properties is a later step. The cross-collection `GET /changes`
keeps its own richer change filter and is not touched.

### Writes

Per-record verbs stay at the record path; the path fixes `(kind, id)` and is
the reference value, so 0033 holds for it. `POST /api/v1/records` is the one
addition, and it is a semantic change, not a relocation: the collection POST
today accepts an upsert input with an id, the new one refuses a supplied id
and creates under a server-assigned one. `Idempotency-Key` binds to it as it
binds to the collection POST now. Nothing else about writes moves: no
mandatory preconditions, no dry run.

## Agent tools

`substrate.reamde.dev/core/graphql` and `…/mutate` go. `…/query` is the
read tool and needs more than a rename: today it requires one `kind` to list,
overwrites `filter.kinds` with it, and returns records with no continuation.
It becomes the route's grammar (`filter` with `kinds` a list, `orderBy`,
`first`, `after`, `expand`, `q`/`mode`), still bounded by the agent's `reads`
allowlist and row budget, which is what the loop enforces per kind. A new
`…/write` host function takes `{op: put|patch|delete, kind, id, input,
ifVersion}`, is granted like `mutate` (bounded by the agent's effective emit)
and is refused on the call API like `mutate`. `propose` and `ask` are
untouched.

`graphql` and `mutate` are RECORDS of kind `function`, not kinds, so
`retired` does not apply; they are pruned from the `core` closure with a
package version bump, and the core `agent` kind plus the `llm` and `pebble`
samples that name them move to `query`/`write` in the same bump.

## Decisions touched

- 0053 and 0058: superseded by one record, "GraphQL is removed; the records
  read is one route". 0022 is already superseded.
- 0033: stands for the record URL. 0042 and 0047: the segment-count
  disambiguation and the three-segment collection lose their collection
  half; each is amended by the new record naming the sentences it retires,
  not silently reinterpreted.
- 0036 stands; the page shape is unchanged.
- New: "`referencing` is a filter arm; incoming is not a sub-resource".
- New: "`expand` returns referents in an `included` sidecar keyed by record
  path, one hop".

## Everything that breaks, by consumer

- **Console**: list cache keys and the create body in `lib/api/records.ts`;
  the incoming drawer and its `total` in `components/record/graph.tsx`; the
  trait "account configs" read moves to `filter.implements`;
  `graphqlTypeName` in `lib/definition.ts` goes; `types.ts` and
  `wire.golden.json` gain the new page fields.
- **CLI**: `list`, `get`, `put`, `patch`, `delete` paths in
  `commands/client.go`; its page decoder drops `head`/`generation` today
  and should stop doing so; `delete` sends no `ifVersion`.
- **e2e**: GraphQL joins and the agent stories that post to `/graphql`,
  incoming row-shape assertions, the collection-watch refusal test.
- **Discovery**: the `graphql` key leaves `surfaces`. That document is
  additive-only for REST features; removing a preview surface is the one
  break here and the new decision record owns it.
- **Docs**: `api.md`'s REST/GraphQL section and table, `agents.md`'s tool
  list, `functions.md`'s host functions, `terms.md` (`schema` loses its
  GraphQL sense), and `.mise/docscheck.sh`'s "retired GraphQL promise" rule
  becomes a "GraphQL is gone" rule scoped to the live pages, with the
  decision records exempt as history.

## Order of work

Additive first, breaks last, and nothing removed until its consumers are off
it. Steps 1–5 each ship alone.

1. `GET /api/v1/records` with today's grammar, cross-kind; cursor binds the
   filter; list generation mismatch answers 410. Console and CLI list
   through it.
2. `filter.referencing` with the `matches` sidecar. Console drawer moves.
3. `expand`, one hop, `included` sidecar, per-kind batched with alias
   resolution.
4. `q`/`mode` on the route with `scores`; discovery `search` gains `rest`.
5. `query` tool takes the route's grammar; `write` tool added; `agent` kind
   and samples move; core package version bumped; e2e agent stories
   rewritten.
6. BREAK: `POST /api/v1/records`; collection routes, `/incoming`,
   `trait/{id}/records`, the reserved id removed.
7. BREAK: `POST /api/v1/graphql`, `internal/gql`, `internal/api/graphql.go`,
   `internal/engine/agentgql.go`, the `graphql-go` dependency, the console
   name folding and the `graphql` discovery surface removed. `graphql` and
   `mutate` pruned. Decision records written. Docs and docscheck updated.

## Cut from the first draft, on review

- Two-hop `expand` and `expand` on a single-record GET.
- Inline expansion or scores under a `status` key: the wire record has none.
- The claim that steps 1–4 were "purely additive" while step 2 removed
  `/incoming`; removal is now step 6.
- The claim of one uniform grammar across list, search and watch.
- `dryRun`, mandatory version guards, deletecollection, table output.
