# Substrate fit for high-volume health data: what the codebase can and cannot do today

Researcher 6 of 6, 2026-10-08. Scope: the substrate code itself at `/home/geoah/.t3/worktrees/substrate/t3-d9d5090b` (branch `t3/d9d5090b`, head `33e03af1`), read only. Nothing was edited, no database suite and no server was run. Every claim cites a file and line in that checkout unless marked "inferred" (read from code, not executed) or "unverified". Changelog line sizes were measured from real dev segment files on this machine (read only). GitHub issues were read with `gh` on 2026-10-08. The mneme v6 health package cited in section 7 is outside this repository, at `~/src/github.com/geoah/rxtx.dev/svc/mneme-v6/kinds/health/`.

## Summary

- **Write cost**: one record write is one transaction holding the repository-wide changelog lock, a `records` upsert touching about 11 indexes (4 of them GIN), one `property_managers` row per property, `refs` rows per reference, one `changelog` row with 5 indexes, and one segment-file line written and fsynced twice. A two-field sample costs about 1,000 bytes of changelog (about 25x its JSON), dominated by one `manager` effect per property. A function invocation commits all its effects in one transaction, but still one changelog line per record. There is no multi-record REST write. Caps: 1 MiB request, 8 MiB function response frame, 60 s function wall clock, 200,000 effects per 2-minute paged drain chain, 64 MiB line, 256 MiB segment.
- **Read path**: only scalar references and the built-in time columns are index-backed. Numeric and date range filters on properties cast per row and use no index; without `kinds` in the filter they even compare as text. There is no aggregation of any kind exposed; `count=1` is the only aggregate and is uncapped. An aggregation read fits as a fourth mode of `GET /api/v1/records`, beside list, window, ranked and tail.
- **Derived indexes**: `refs` and `fts` are pure functions of the folded row and the declaration, rebuilt by replay; `embeddings` is an asynchronous queue-fed index kept across rebuilds. A narrow samples table fits the `refs` contract exactly; a rollup table fits the `embed_queue` one.
- **Blobs**: 64 MiB, content-addressed, fs only, in the backup. A function cannot upload one, and nothing server-side reads blob bytes.
- **Types**: `money` is a complete template for a structured value type (engine-owned table, console copy, typed filter and order). There is no unit concept anywhere in the dialect; a `propertytype` refinement cannot add one. Objects and keyed maps are not filterable.
- **Traits**: `temporal` is the only hot trait and its three columns are hard-coded in about 20 places.
- **Providers**: WHOOP mirrors daily objects (cycle, recovery, sleep, workout) in WHOOP's units with nested `score` objects, 25 per page, 48-hour overlap, ids from upstream keys, no deletes. Record mappings copy values only (exact datatype match, two-level paths, no arithmetic), so canonicalisation and unit conversion must be a function; mneme v6 already does exactly this for WHOOP.
- **Agents**: the `query` tool returns full records, at most 10,000 rows per run, no projection or aggregate. 30 sleep rows are easy; 40,000 samples are not answerable.
- **History**: no issue or decision proposes units, time series or aggregation. The health and fitness samples were deleted on 2026-09-10 for lack of consumers. `docs/plans/compaction.md` is about agent context, not data.

## 1. Write path: what one `put` costs

All paths are under `/home/geoah/.t3/worktrees/substrate/t3-d9d5090b`. Verified by reading the code on 2026-10-08 unless marked otherwise.

### What one record write does, statement by statement

A `put` of one record, inside one `inTx` transaction, does the following (in order):

1. **Takes the per-repository changelog advisory lock** before anything else (`internal/engine/rows.go:257-272`, `changelogLockKey`). Every writer in a repository is serialized from its first append to its commit, so seq order equals commit order. Consequence: a repository has exactly one write lane; throughput is bounded by one transaction at a time.
2. **Loads the prior row** (`foldRecordOp` calls `t.loadRow`, `internal/engine/fold.go:461-475`), applies the delta, and **upserts `records`** with one `INSERT ... ON CONFLICT DO UPDATE ... WHERE <any column IS DISTINCT FROM>` that also computes `fts` with three `to_tsvector('english', ...)` calls (`internal/engine/rows.go:178-246`). An identical rewrite matches the `WHERE` nothing and returns no row, so "a re-sync leaves no wake" (`docs/changelog.md:8`).
3. **Re-derives `refs`** for the row when the kind declares any reference: `DELETE FROM refs WHERE src_kind=$1 AND src=$2` then one `INSERT` per reference value (`internal/engine/refs.go:280-298`). Kinds with no reference site skip this (`fold.go:500-515`).
4. **Writes one `property_managers` upsert per accepted property** (`internal/engine/write.go:1245-1287` calling `setManager`, `rows.go:609-626`, `applyManager` `rows.go:637-665`), and enqueues an embedding row for each property declared `embed: true` (`write.go:1283-1287`).
5. **Appends one `changelog` row**: `INSERT INTO changelog (seq, ...) VALUES ((SELECT coalesce(max(seq),0)+1 FROM changelog), ...) RETURNING seq, payload::text` (`rows.go:296-357`). The payload is jsonb and carries the whole fold (every effect the write applied) under `fold` (`fold.go:15-29`, `appendChange` `rows.go:297-307`).
6. **At commit**: `settleFold` merges late effects into the last entry (`fold.go:355-397`), `settleChecksums` canonicalizes each entry, computes SHA-256, refuses a line over `MaxLineBytes` and runs `UPDATE changelog SET hash=$2, txn=$3` per entry (`internal/engine/checksum.go:80-121`).
7. **Disk commit protocol** (decision 0062): under `writerMu` the lines are written and fsynced without the final newline, Postgres commits, then the newline is written and fsynced (`docs/decisions/0062-...md`; `changelogfile.Writer.Prepare/Commit/Abort`, `internal/changelogfile/writer.go`). So **one transaction = two fsyncs on the segment file plus the Postgres commit fsync**.

Index maintenance per new row (from `internal/engine/migrations/0001_init.up.sql:86-117` and 0007/0008):

| table | indexes touched per insert |
| --- | --- |
| `records` | PK `(repository, kind, id)`; btrees on `(repository, at)`, `ends_at`, `due_at`, `created_at`, `updated_at`, `deleted_at`; GIN `props jsonb_path_ops`, GIN `labels jsonb_path_ops`, GIN `states jsonb_path_ops`, GIN `fts`; plus every declared `indices:` partial index of the kind and one btree per scalar reference property (`internal/engine/indices.go:371-436`) |
| `changelog` | PK `(repository, seq)`, `changelog_record_idx`, `changelog_pair_idx`, `changelog_kind_seq_idx` (0007), `changelog_record_seq_idx` (0008) |
| `property_managers` | PK `(repository, record_kind, record_id, property)` per property |
| `refs` | PK plus `refs_dst_idx` per reference value |

So one small record costs roughly: 1 records row with ~11 index entries, N manager rows (N = properties written), R refs rows, 1 changelog row with ~5 index entries, the payload stored **twice** (jsonb in Postgres, canonical line in the segment file), and two file fsyncs.

### Bytes per changelog line, measured

Measured from real segment files of dev repositories on this machine (`.dev/data/repositories/*/changelog/000000000000001.ndjson` in sibling worktrees; read only):

| entry | bytes per line |
| --- | --- |
| `put` of a `core/triggerrun` (9 properties, machine-written) | 2,255 to 2,457 (median 2,302) |
| `put` of a hand-written `mneme.rxtx.dev/health/observation` definition (9 properties) | 1,922 to 2,489 |
| `delivery` (trigger bookkeeping) | 391 to 560 |
| `delete` / `gc` of a triggerrun | ~380 to 399 |

The shape (one real line, `seq 384`): the envelope (`actor`, `kind`, `op`, `principal`, `recordId`, `seq`, `sum`, `ts`, `txn`), then `payload.fold[0]` = the record delta with every set value, then **one `manager` effect per property** (`{"actor","id","kind":"manager","principal","property","ref","tier"}`, about 160 to 230 bytes each because it repeats the full kind reference and the record id), then `payload.properties` (the names again). Numbers are rendered canonically as `1E0`, `7.2E1` (`changelogfile` canonical form, `internal/changelogfile/entry.go`).

Synthesized with the same encoding (Python, sorted keys, compact), for a function-written record. A temporal `at` is stored only in the `records.at` column (`internal/engine/write.go:1008-1036`, the "trait-backed hot columns" loop), travels as `delta.at`, and still counts as an accepted property, so it gets its own `manager` effect and `property_managers` row:

| record | properties payload alone | changelog line |
| --- | --- | --- |
| minimal sample `{bpm}` plus the `at` column, written by a provider function | 38 bytes as JSON | about 1,000 bytes |
| `observationlog` `{observation(ref), value, source(ref)}` plus `at` | about 200 bytes | about 1,570 bytes |

**Amplification is roughly 25x on the file alone for a two-field sample**, dominated by the per-property `manager` effects. One sample per second for a year (31.5 M records) would be about 32 GB of segment files plus the same payloads in the `changelog` jsonb column, before the `records` row and its indexes. Real scale reference: the owner's `geoah.me` repository had 2,168,142 entries in 15.8 GB of segments, 336k records (issue #761, #719), of which ~10.5 GB was Slack paged-drain cursors (issue #760, fixed by decision 0141).

Cost of reading that history back is also measured: `changelogfile.Verify` ran at about 1 ms per entry on amd64 (33 min for 2.17 M entries), snapshot took 6 h 40 min on a Pi CM4 (issue #761, closed 2026-09-29 with fixes). A first open after a search-index version bump re-derived `fts` for 336k rows in 24 minutes (#719, now batched by `searchReindexBatch = 2000`, `internal/engine/searchindex.go:11-13`). Every row added for health samples lengthens every `verify`, `snapshot`, `rebuild` and reindex linearly.

### Batching and transactions

- **REST has no multi-record write.** Routes (`internal/api/api.go:268-350`): `POST /api/v1/records` creates one record, `PUT/PATCH/DELETE /api/v1/{a1}/{a2}/{a3}/{a4}` address one record, `POST /merge` and `/split` are the only multi-record verbs. `substratectl apply` sends each data document as its own request (`cmd/substratectl/commands/apply.go:163`); only declarations go in one `/vocabulary/apply` batch.
- **One transaction may append several changelog lines.** Decision 0057 frames them: every line carries `txn` (the seq of the transaction's last entry); the writer appends whole transactions only and never splits one across segments (`internal/changelogfile/writer.go:18-23`, `ErrTxnIncomplete`). Registration seeds the whole `core` package in one transaction; merge writes two lines in one.
- **A function invocation's effects**: see section 8 (whether they land in one transaction).
- **Caps**: request body 1 MiB (`maxRequestBody = 1 << 20`, `internal/api/core.go:76`); vocabulary apply 32 MiB (`internal/api/vocabulary.go:15`); changelog line 64 MiB (`MaxLineBytes = 64 << 20`, `internal/changelogfile/reader.go:15`), refused before commit (`checksum.go:104-107`); segment rotation at 256 MiB (`DefaultSegmentBytes`, `writer.go:11-13`; `SUBSTRATE_CHANGELOG_SEGMENT_BYTES`, `internal/config/data.go:29`, minimum 1 MiB); a vocabulary conversion over more than 10,000 live records is refused (`DefaultConversionCeiling`, `internal/engine/convert.go:107`; issue #157 open), which matters for any narrowing of a high-volume kind.

## 2. Read path: filters, ordering, cursors, counts

Read from the code (no `EXPLAIN` was run, no tests were run). Paths relative to the checkout.

### Main findings

- **Almost no property filter can use the GIN index on `props`.** The index is `records_props_idx ON records USING gin (props jsonb_path_ops)` (`internal/engine/migrations/0001_init.up.sql:114`). `jsonb_path_ops` serves only `@>`/`@?`/`@@` applied to `props` itself. The only list predicate of that shape is the probe for a **repeated or keyed reference** (`props @> $n::jsonb`, `internal/engine/query.go:1176-1182`, comment at `1199-1201`). Everything else compiles to `props->>key`, `props->key @> ...` (a sub-value, not indexable by this index) or `jsonb_exists(props, k)`.
- **Range filters on properties have no index.** Typed comparisons cast, e.g. `(props->>($k::text))::numeric > $n`; no index expression matches a cast, including a declared `indices:` btree on `(props->>'x')`. Only the hot columns are index-backed for ranges: `records_at_idx`, `records_ends_at_idx`, `records_due_at_idx`, `records_created_at_idx`, `records_updated_at_idx`, `records_deleted_at_idx`, all `(repository, col)` (`0001_init.up.sql:108-113`). Note none of them leads with `kind`, so "kind X with `at` in a month" walks every kind's rows in that range.
- **Without `kinds` in the filter, typed comparison is off.** `condProp` looks the datatype up only in the kinds the filter resolved (`query.go:651, 659-723, 863-875`); with no `kinds`/`implements`/`purposes` every comparison is textual (a numeric `gt` compares strings). `orderBy`, by contrast, checks every kind in the registry.
- **`duration` compares as text** (ISO 8601 strings, `validate.go:513-518`), so `PT10M` sorts before `PT9M`.
- **Nested object paths are not supported**: `address.city` becomes a literal top-level key `props->>('address.city')` and matches nothing (`exists:false` would match everything). `internal/vocabulary/types.go:340-344` says objects and keyed maps stay out of FTS, embeddings and the filter grammar.
- **There is no aggregation read.** `count=1` is the only aggregate a client can ask for, and it has no cap and no timeout.

### How `filter.properties` compiles (`condProp`, `query.go:830-880`)

Order of dispatch: built-in columns via `columnFor` (`at`, `endsAt`, `dueAt`, `createdAt`, `updatedAt`, `deletedAt`, `title`, `body`, `id`, `version`; `query.go:806-828`) go to `condColumn` (`query.go:1347-1393`); states go to `condJSON(..."states"...)`; `secret`/`digest` are refused; references go to `condReference`; `match` is gated by `matchRefusal`; money goes to `condMoney`; everything else to `condJSON` (`query.go:1415-1528`). Keys are bound as parameters (`props->>($k::text)`).

| Operator | SQL shape (`condJSON`) |
| --- | --- |
| `eq`/`gt`/`gte`/`lt`/`lte` | `<typedExpr> op $n` (`query.go:1473-1485`) |
| `in` | `props->>($k::text) = ANY($n::text[])`, always text (`1486-1492`) |
| `prefix` | `props->>($k::text) LIKE $n` (`1493-1495`) |
| `contains` | `props->($k::text) @> '[v]'::jsonb` (`1496-1502`), not GIN-backed |
| `match` | `to_tsvector('english', <text or joined array>) @@ tsquery` (`1503-1519`), computed per row |
| `exists` | `jsonb_exists(props, $k)` (`1520-1526`) |

Typed expressions (`query.go:1432-1472`): `int`, `float` and `decimal` cast `::numeric` (decimal's bound travels as exact digits with `::numeric`, `1436-1454`); `datetime`, `date` cast `::timestamptz` (bound parsed by `substrate.ParseInstant`); `bool` casts `::boolean`; `duration`, strings, enum, `json`, `object` compare as text.

`condMoney` (`query.go:917-968`): `prefix`, `match`, `contains` refused; a comparison is `(props->k->>'currency') = $c AND (props->k->>'amount') ~ '^-?[0-9]+$' AND (props->k->>'amount')::numeric <op> $amt::numeric`; the operand must be a money object (no bare number); `in` is an OR of equalities. This is the existing template for "a structured value compared within its unit": **the comparison holds within one currency; across currencies a filter never matches**.

`condReference` (`query.go:1128-1235`): range and text operators refused; a scalar reference compiles to `coalesce(props->'p'->>'ref', props->>'p') = ANY($n::text[])` (with merged former ids added, `1048-1066`), which matches the automatic per-kind btree `referenceIndexStatements` creates (`internal/engine/indices.go:422-436`). That index is the one property index the list path reliably hits; there is an `EXPLAIN` test for it (`internal/engine/refindex_db_test.go:79-104`).

Declared `indices:` (`indices.go:371-476`) build `CREATE INDEX CONCURRENTLY idx_<hash> ON records (repository, (props->>'x'), ...) WHERE kind = '<kind>'`. The filter's text equality binds the key as a parameter while the index inlines it; whether Postgres matches the two likely depends on custom vs generic plans (unverified, no test covers it). Typed (cast) predicates never match it. The mneme v6 health package declares exactly such an index, `observationlog: indices: [[observation, at]]`, which works because `observation` is a scalar reference (path expression) and `at` is a column.

### `orderBy` (`orderExpr`, `query.go:1567-1621`)

Built-in columns sort natively. A property sorts by an expression: `(props->>'x')::numeric` when every kind declaring it says int/float/decimal; money by `moneyValueSQL` (exact numeric from amount and the currency's minor unit, so money ORDERS across currencies, `query.go:888-908`); datetime/date `::timestamptz`; scalar reference by its path; anything else (`duration`, repeated) as text. Default order is `created_at DESC`; a `(kind, id)` tiebreak is always appended (`query.go:1628-1645`); nullable expressions get `NULLS LAST`, so `at DESC NULLS LAST` cannot use a backward scan of `records_at_idx` (`query.go:1548-1560, 1650-1664`). No property sort is index-backed unless a declared index expression equals it exactly.

### Keyset cursor (`query.go:490-511, 516-526, 734-757, 1679-1724`)

Each sort key is also selected as `(expr)::text AS __k<i>`; the query runs `LIMIT first+1`. The token is base64url JSON `{o, k, h, g}`: `o` = rendered order + `|` + `filterDigest` (first 8 bytes of SHA-256 of the filter JSON, `expand.go:303-307`), `k` = the last row's key values (nil = NULL), `h` = the changelog head of the first page, `g` = the history generation. Reuse checks the signature and the generation (a different generation is `410`, `internal/api/records.go:151-161`). The seek is a lexicographic OR-of-ANDs. Each list runs in a `REPEATABLE READ` read-only transaction (`query.go:424`). `offset` is an alternative to `after` (decision 0084).

### Page caps

`defaultPageSize = 50`, `maxPageSize = 500` (`query.go:25-27`), larger values are silently lowered (`448-454`). The window read repeats 50/500 (`internal/window/window.go:156-162`). The ranked read defaults to `k = 20` with a candidate pool of `max(400, 4k)` (`internal/engine/bm25.go:49, 208`). Every listed row is then hydrated by extra queries (former ids, optional annotations, blobref resolution, `query.go:332-356, 596-602`).

### `count=1` (decision 0134)

Only the literal `1` is accepted (`internal/api/rest.go:358-363`). SQL: `SELECT count(*) FROM records WHERE <same predicate>` in the page's snapshot, before the seek (`countSQL`, `query.go:479-489, 635-645`). No cap and no `statement_timeout` anywhere in `internal/` (grep found none). Refused on the window read (`window.go:150-155`), the ranked read and the tail. Decision 0134 lists "the cost is unbounded and uncapped" as an accepted bad consequence.

### Does anything aggregate today?

No `date_trunc`, `percentile` or facet SQL exists in `internal/`. Every `GROUP BY`/`sum`/`avg`/`count` is internal: BM25 statistics and term frequencies (`bm25.go:108-127, 245-263, 317, 340`), the semantic arm's best chunk per record (`search.go:424-428`), the reindex plan's `SELECT kind, count(*) ... GROUP BY kind` (`searchindex.go:222`), changelog bookkeeping (`changevalues.go:604-611`), trigger status counts (`functions.go:2419-2421`, `sync.go:455`), agent turn counters (`agentdecision.go:252, 328`, `agentloop.go:799`), upgrade guards (`schemadiff.go`), conversion digests (`convert.go:617-628`). None is exposed. The console's "Group by" request (issue #679) was closed and deferred by the owner.

### Route modes and where an aggregation read would plug in

`GET /api/v1/records` (`internal/api/api.go:268`, handler `internal/api/records.go:49-169`) branches on parameters, each mode with its own allow-list (`records.go:38-47`, unknown parameters refused by `unsupportedParam`, `rest.go:247-270`):

| mode | trigger | engine |
| --- | --- | --- |
| tail | `watch=1`; filter limited to `kinds` | `h.streamChanges` (`internal/api/watch.go:129`) |
| ranked | `q=`; filter limited to `kinds`, `purposes` | `ds.Search` (`internal/engine/search.go:27-133`) |
| window | `at` bounded on both ends | `window.Read` (`internal/window/window.go:135-205`) over `ds.Window` (`internal/engine/window.go:39-206`) |
| list | otherwise | `ds.List` (`internal/engine/query.go:411-614`) |

Functions and agents reach the same split through `readList` (`internal/engine/runner.go:288-305`, decision 0111). The least violent place for an aggregation is a fourth mode on the same route, selected by its own parameter with its own allow-list, implemented as a new `substrate.Dataset` method (`internal/substrate/dataset.go:64-70`) that reuses the `count=1` pattern: same snapshot, `buildFilter` (`query.go:649-801`) for the WHERE clause, and `orderExpr` for typed group and measure expressions. It must also be added to `internal/api/fake_test.go` (the API is tested against a hand fake) and to `readList` so functions and agents get it. Decision 0079 ("the records read is one route") and 0134 (count rides the list rather than a second route) are the precedent for a mode rather than a new endpoint.

### `implements` and `referencing`

`implements` resolves in Go to a kind list (`reg.ImplementingStrict`, intersected with `kinds`) and compiles to `kind = ANY($1::text[])` (`query.go:666-723`). `referencing` is a correlated `EXISTS (SELECT 1 FROM refs r WHERE r.src_kind = records.kind AND r.src = records.id AND r.dst_kind = $k AND r.dst = $id ...)` (`expand.go:54-80`), walking `refs_dst_idx`.

### Indexes on `records` and `refs`

From `0001_init.up.sql`: PK `(repository, kind, id)`; btrees on `(repository, at|ends_at|due_at|created_at|updated_at|deleted_at)`; GIN `jsonb_path_ops` on `props`, `labels`, `states`; GIN on `fts`; `refs` PK `(repository, src_kind, src, property, path, ord)` and `refs_dst_idx`. Later: `records_recurring_idx` (0002), `records_orphaned_at_idx` (0003), `records_ambiguous_at_idx` (0006), the RLS-bypassing `records_matching(tsquery, int)` function (0010). At runtime: declared `indices:` and one btree per scalar reference property, built `CONCURRENTLY` (`indices.go:250-268`). `embeddings` has no vector index.

## 3. Derived indexes as a pattern

The engine already runs five kinds of state that are not the changelog itself, and they fall into three contracts. A new narrow samples or rollup table would have to pick one.

| derived state | derived from | when written | rebuilt by | file |
| --- | --- | --- | --- | --- |
| `records` (the fold) | changelog effects (`payload.fold`) | in the write's transaction, through `txn.fold` only | `RebuildRepository` replays every line through `foldOne` | `internal/engine/fold.go:15-50, 401-445` |
| `refs` | the folded row's properties + the kind declaration (pure `deriveRefs`) | synchronously in `foldRecordOp` after the upsert; skipped for kinds with no reference site | the replay re-derives it per row; `reprojectRefs`/`syncRefsOfKind` re-derive a kind when its reference shape changes | `internal/engine/refs.go:13-40, 76, 280-372`; `fold.go:494-515`; `vocabularywrite.go:2988-2993` |
| `fts` (a column on `records`) | the folded row + the kind's declaration | synchronously, inside the same `INSERT ... ON CONFLICT` | `reprojectFTS` on a vocabulary apply that changes indexing (in the apply's transaction, moves no version and writes no changelog entry); open-time `reindexSearch` in batches of 2000 when `searchIndexVersion` rises | `fold.go:37-50, 519-620`; `internal/engine/searchindex.go:11-60` |
| `property_offers` | mapping recompute over the fold | synchronously during recompute | cleared and re-derived after a rebuild (`rederiveOffers`) | `internal/engine/rebuild.go:47-53, 317-366` |
| `embeddings` + `embed_queue` | record text properties declared `embed: true` | **asynchronously**: the write only upserts a queue row and bumps its `generation` (`enqueueEmbed`, `internal/engine/rows.go:669-681`); a worker computes vectors later and loses a race by generation | **kept** across a rebuild because vectors are expensive and still belong to the reproduced row | `rebuild.go:58-60`; `0001_init.up.sql:318-346` |

The written rules (from `fold.go`'s header and `rebuild.go:16-85`):

1. Every column of `records` except `fts` is a function of the changelog alone, and `fold.go` is the only path that writes it. `settleFold` rolls back any transaction that changed the fold without appending an entry (`fold.go:355-372`). CLAUDE.md: "Anything that writes `records` directly, or a changelog entry that reaches one store and not the other outside `inTx`, is wrong."
2. A derived index has **no fold effect of its own** when it is a pure function of the folded row plus the declaration, so the replay reproduces it "byte for byte" and no second description can drift (`refs.go:22-26`, `fold.go:494-500`).
3. Anything derived that is not reproducible from the changelog (sealed payloads, blob bytes) lives in the repository directory beside the segments so the directory remains the backup unit (decision 0051).
4. A change in the derivation rule re-derives the affected rows without moving `version` or appending changelog entries (`reprojectFTS`, the open-time reindex).

### Would a third derived table fit?

A narrow table such as `samples(repository, kind, series, at, value numeric, unit text, record_id)` fed from records of a declared kind (or a kind binding a declared trait) fits contract 2 exactly, the `refs` shape:

- **Derivation**: a pure `deriveSamples(kind declaration, folded props, at)` called in `foldRecordOp` right where `syncRefs` is called (`fold.go:500-515`), as `DELETE ... WHERE src = id` then `INSERT`, gated on the kind declaring the trait so other kinds pay nothing (the same skip `declaresReference` gives refs).
- **Rebuild**: add it to `foldTables` (`rebuild.go:100-102`) so it is cleared and re-derived by the replay; add a `reproject` hook in the vocabulary apply for kinds whose sample shape changed.
- **Schema**: a new SQL migration `0011_...up.sql` (append-only; `mise run frozen:check` refuses edits to landed ones; `lint:migrations` holds naming), with RLS like every other per-repository table. No repository migration is needed for an empty new table; a repository migration (`repomigration_NNNN_*.go`, decision 0099) is only for rewriting stored rows through the changelog.
- **What it would buy**: a btree `(repository, kind, series, at)` and typed `numeric` values make range scans and `GROUP BY date_trunc(...)` cheap, which `props` jsonb cannot do (section 2).
- **What it would not fix**: the per-record write cost of section 1. Each sample is still one record, one ~1 KB changelog line and N manager rows. A derived table speeds reads; it does not shrink the truth.

An **aggregate rollup** table (daily min/max/sum/count per series) is a different contract. It is derived from many rows, not one, so it cannot be recomputed per write from that row alone. It would follow either the `property_offers` model (derived from the fold, re-derived wholesale after a rebuild) or the `embed_queue` model (a dirty-bucket queue written in the write's transaction, a worker recomputing buckets asynchronously, kept or recomputed after a rebuild). Both precedents exist; neither touches the changelog.

The bigger structural alternative, where many samples share one record (a chunk of a day's heart rate as one record or one blob), changes the unit of truth rather than adding an index, and is evaluated in the ranking at the end.

## 4. Blobs

- **API.** `PUT /api/v1/blobs`, `PUT /api/v1/blobs/{digest}`, `GET /api/v1/blobs/{digest}` (`internal/api/api.go:302-304`). Cap `maxBlobBody = 64 << 20`, enforced by `http.MaxBytesReader` (`internal/api/blobs.go:18-21, 123`); over it the server answers `413`. The source comment: "Blobs are archives and attachments, not streams; a larger payload belongs in a chunked design this build does not have." At most 8 uploads at once (`maxConcurrentBlobPuts`, `blobs.go:28`). A read loads the whole blob and re-hashes it before answering; there are **no range reads** (`docs/api.md:1101-1124`, `docs/operations.md:247-260`).
- **Addressing.** `blob-sha256-<64 hex>` (`internal/engine/blobs.go:55-66`, `internal/blobbytes/blobbytes.go:47-53`). A manifest record of kind `substrate.reamde.dev/core/blob`, id = digest, carries `digest`, `size`, `name`, `mediaType`, `createdBy`, `status` (`pending`/`stored`) (`blobs.go:36-49`); write order is pending manifest, bytes, stored (decision 0030). The manifest is a system kind the generic record API cannot write.
- **Backend.** fs only (decision 0075): `<data root>/repositories/<repo>/blobs/<digest>` (`internal/blobbytes/fs.go:26-34`), stored plaintext (decision 0031).
- **`blobref`.** The stored value is the digest; reads resolve it to `{digest, name, mediaType, size, status}`, never the bytes (`internal/vocabulary/types.go:69-73`, `blobs.go:465-505`). A write checks the manifest exists and takes a shared per-digest lock (`validateBlobRefs`, `blobs.go:411-452`).
- **GC.** `blobGCPass` tombstones manifests no live `blobref` names, after `BlobUploadGrace = time.Minute`, and deletes the bytes (`blobs.go:22-28, 513-647`; called from `gc.go:53`).
- **Can a function upload a blob? No.** There is no `host.blobs` call (section 8); the only `PutBlob` callers are the HTTP handler (`internal/api/blobs.go:72`) and webhook ingestion (`internal/engine/webhooks.go:357, 525, 551`). A function body has no API token and cannot reach loopback or private addresses. It **can** write an existing digest into a `blobref` property in an effect (normal validation applies); digests reach a body from webhook multipart parts (`parts[].blob`, `docs/functions.md:303-320`) or from a record a client wrote.
- **Backup.** Blobs live in the repository directory, which is the backup unit (decision 0051); the owner export streams every `stored` digest with hash verification (`internal/engine/export.go:149-166, 234-243, 295-320`), and restore re-validates `blobref`s (`restore.go:238`).
- **Server-side reads of bytes.** Only `GET /blobs`, export/snapshot, `verify` (`verify.go:464`) and reloading a parked webhook's body (`webhooks.go:575, 596`). Nothing extracts text, embeds, indexes or derives anything from blob content; function bodies only ever see the manifest.

Consequence for "series chunks as blobs plus a derived index": every piece is missing today. A provider function cannot write a chunk, nothing server-side can read one to derive an index, reads cannot fetch a byte range, and the derived-index contract (section 3) folds from records, not from blob bytes. A design on this path needs at minimum a host upload call and a server-side reader, both new surfaces.

## 5. Property types: `money` and `decimal` as the template for a unit-bearing value

### How the two are built end to end

| layer | `decimal` | `money` |
| --- | --- | --- |
| constant | `DatatypeDecimal`, `internal/vocabulary/types.go:31-37` | `DatatypeMoney`, `types.go:38-44`; both in `builtinKinds` (`types.go:128-135`) |
| value support | none beyond the grammar | `internal/vocabulary/money.go`: member names `amount`/`currency` (`20-23`), the ISO 4217 minor-unit table `currencyDecimals` (`39-73`, may only grow, pinned by `TestCurrencyTableOnlyGrows`), `MoneyDecimal`, `FormatMoney`, and `moneyInt`, which reads int64, float64 and `json.Number` (`132-148`) |
| declaration rules | generic scalar path (`pattern`, `min`, `max`, `values` allowed); default must be a string (`load.go:2323-2332`) | `pattern` and `values` refused, only `min`/`max` (`load.go:2137-2142`); default must be a map (`2316-2322`) |
| write coercion | `coerceDecimal` (`internal/engine/validate.go:650-666`): must be a JSON string, grammar `^[+-]?[0-9]+(\.[0-9]+)?$`, exponent refused, scale kept (`"19.90"` stays), `min`/`max` checked exactly with `big.Rat` (`670-701`) | `coerceMoney` (`validate.go:742-779`): exactly `{amount, currency}`, amount a safe integer (`asInt`, `596-618`), currency in the table, `min`/`max` bound the denoted value |
| stored in `props` | `"19.90"` | `{"amount": 1999, "currency": "EUR"}`; no hot column for either (`splitProps`, `write.go:394-471`) |
| filter | `(props->>k)::numeric` with the bound as exact digits cast `::numeric` (`query.go:1436-1454`); `in` compares text, so `in: ["19.9"]` misses `"19.90"` (inferred) | `condMoney` (`query.go:917-959`): currency equality AND amount regex guard AND `amount::numeric op $amt::numeric`; bare number refused; `prefix`/`match`/`contains` refused |
| order | `(props->>'x')::numeric` if every declaring kind agrees (`numericProp`, `1242-1257`) | `moneyValueSQL` = `(amount || 'e-' || minor_unit)::numeric`, so it orders across currencies (`888-908`, `1261-1274`) |
| title / search | renders as its digits | `FormatMoney` in `displayTemplate` (`validate.go:919-922, 1125-1130`), renders `19.99 EUR`; a money map indexes as empty text, so money never reaches search |
| narrowing guards | `boundPredicate` regex guard + `::numeric` (`schemadiff.go:1387-1407`) | same via `moneyValueSQL` |
| API test fake | `case "decimal"`, `internal/api/fake_test.go:560` | `case "money"`, `fake_test.go:564` |
| console | `lib/record-schema.ts` grammar and examples | `lib/money.ts` (a copy of the currency table, held equal by `internal/vocabulary/currency_console_test.go:28`), `components/identity/money-value.tsx`, `components/record/money-input.tsx`, inline and list editors, `lib/filters.ts:77-80` (operand typed as `19.99 EUR`), `lib/format.ts:161-166`, column renderers, `property-icon.ts:35-36` |
| meta-kind | `kinds/substrate.reamde.dev/core/propertytype.yaml:63` (`base:` enum) | `propertytype.yaml:64` |
| docs | `docs/data-model.md:319`, `docs/api.md:499-516, 569-572` | `docs/data-model.md:320, 372-391`; decision 0140 |

The wire golden (`web/console/src/lib/api/wire.golden.json`, written by `internal/substrate/wire_test.go:33, 246`) pins struct field names only; property values are `map[string]any` (`internal/substrate/record.go:36`), so **a new datatype does not touch the golden**. No `types.ts` change is needed either: datatypes reach the console as strings inside `KindInfo.definition`, and an unknown one "passes through and is treated as a string" (`web/console/src/lib/record-schema.ts:75-77`).

### Checklist for a new `quantity` (or `measurement`) datatype

Grepping `money` and `decimal` across `internal/`, `kinds/`, `web/console/src`, `docs/` gives this file list:

1. `internal/vocabulary/types.go` (constant, `builtinKinds`); a new `internal/vocabulary/quantity.go` (member names, a unit table if the engine owns one, a formatter, a tolerant number reader); `internal/vocabulary/load.go` (key restrictions, `parseDefault`, and the `unique`/`field`/`keyed` forbidden lists, `1823-1829, 1905-1922`).
2. `kinds/substrate.reamde.dev/core/propertytype.yaml` (`base:` enum, bump `version`).
3. `internal/engine/validate.go` (`coerceScalar` case and a `coerceQuantity`; title rendering).
4. `internal/engine/query.go` (`condProp` dispatch, a `condQuantity`, an order predicate and a SQL value expression).
5. `internal/engine/schemadiff.go` (`patternApplies`, `boundsApply`, `boundPredicate`).
6. `internal/api/fake_test.go:531-570`.
7. Unit and DB tests modeled on `validate_internal_test.go:114`, `money_test.go`, `money_db_test.go`, and, if the console holds a unit table, an equality test like `currency_console_test.go` and an only-grows pin like `currency_pin_test.go:47`.
8. The console files listed in the table.
9. `docs/data-model.md`, `docs/api.md`, `docs/vocabulary.md`, a decision record, and a `BREAKING CHANGE` footer only if an existing shape changes.

Two design forks the money precedent settles or exposes:

- **Comparison semantics.** Money compares only within one currency in a filter but orders across currencies by exact value, because the engine owns an exact conversion (the minor-unit table). A quantity's units have exact conversions within a dimension (kg and lb, ms and min) but not across dimensions; a filter that converts within a dimension needs a factor table and dimension check in SQL, which money never needed (money never converts between currencies).
- **Exactness.** Decision 0012 forces a choice up front: a float64 `value` (lossy, cheap, matches every wearable API) or exact digits in a string (the `decimal` stance). Money chose an integer of minor units, which only works because the minor unit is a fact about the currency; a sensor value has no natural minor unit.
- **Cost**: an `old binary` refuses an unknown `type:` as "unknown property type" (`load.go:2100`), and dialect keys are reserved, not tolerated (decision 0020), so shipping a new datatype is an upgrade every binary reading the closure must have.

A gap noticed in passing (unverified, no failing test): `moneyInt` reads a `json.Number` with `Int64()`, which fails on the changelog's canonical exponent spelling such as `1.999E3` (`money.go:143-145`); no current caller formats money from a replayed delta.

### `object`, `repeated`, `keyed`, `json`, `propertytype`

- **`object`**: closed key set `objectPropKeys` (`load.go:1845-1860`); nesting cap `MaxFieldDepth = 4`, "a kind's own property is level 1, so a level-MaxFieldDepth field holds a scalar and never an object", declared rather than discovered "because every narrowing guard walks it" (`types.go:392-397`, enforced at `load.go:2348-2352`). Fields may not be `json`, `secret`, `digest`, `state`, `blobref` (`load.go:1905-1911`); money is allowed. `coerceObject` (`validate.go:248-292`) refuses undeclared fields, applies field defaults and `required`. Templates read one level in (`{name.field}`, `{list[].field}`). Note `docs/data-model.md:334` still says "one level", contradicting the code and line 439 of the same page.
- **Filtering objects**: documented as out of the filter grammar (`docs/data-model.md:446-448`, `types.go:342-344`) but **not refused**: a filter on an object name falls through to `condJSON`, so `{"addresses": {"contains": {"city": "Paris"}}}` compiles to `props->'addresses' @> '[{"city":"Paris"}]'` and works by accident, unindexed. No dotted paths. A health design cannot rely on filtering `score.recoveryScore` inside WHOOP's `score` object.
- **`repeated`**: a list of the type; filters with `contains` as text containment (`query.go:867-869, 1496-1502`); excluded from numeric and money ordering.
- **`keyed`**: a JSON object whose keys are data (`types.go:165-176`), `keyPattern: camel|kindRef` (`types.go:384-455`); cannot be repeated, cannot be `json`/`secret`/`digest`/`state`/`blobref`, no fts, no embed, no default (`load.go:1916-2286`). Out of the filter grammar per the docs; in code only keyed money is special-cased, so a `gt` on a keyed number would likely fail at runtime with a numeric cast error (inferred). WHOOP uses keyed datetime maps for `syncCursors`.
- **`json`**: accepts any JSON (`validate.go:456-457`), text-only filtering, not allowed as a field, keyed or unique.
- **`propertytype` refinement**: closed data keys `authority, package, base, pattern, min, max, values, description` (`load.go:272-275`). It **can** add a regex, numeric bounds and an enum value list; it **cannot** add a unit, a default, `required`, cardinality, fields, or a new member to a structured value; there is no `unit` key anywhere in the dialect. Refinements are package-local and cannot refine a refinement (`load.go:2097-2108`). A property using one appends to its `values` rather than narrowing them (inferred). A trait contract naming a refinement appears unsatisfiable because the check compares base datatypes (`load.go:2793-2796`, inferred bug).

Closed property key set (`propKeys`, `load.go:1785-1816`): `type, repeated, embed, fts, values, pattern, min, max, description, base, fields, writer, displayName, keyed, keyPattern, managed, required, default, renamedFrom, unique, deprecated`. A `unit:` key on a `float` property would be a new reserved dialect key under decision 0020. Kind data keys (`typeDataKeys`, `load.go:794-813`): `authority, package, names, displayTemplate, properties, traits, indices, description, version, retired, movedFrom, purpose, label`.

## 6. Traits

A trait is a record of core's `trait` kind: `properties` (name to datatype, keyed) and `oneOf` variants (`kinds/substrate.reamde.dev/core/trait.yaml`; loader keys `authority, package, oneOf, properties, description`, `load.go:277-279`). A trait contracts presence and datatype only (`docs/traits.md:32-35`). Trait property datatypes must be built-in or `reference`; variant properties built-in only (`load.go:485-497, 631-646`).

**`oneOf` variants**: a list of `{name, properties}` (`load.go:583-616`), bound with the grammar `name(variant: col1, col2)` (`load.go:39`); a variant trait requires a variant and a variant-free trait refuses one (`bindCapability`, `load.go:1564-1628`).

**`temporal` and the hot columns.** `core/temporal` has variants `point {at}` and `range {at, endsAt}` (`kinds/substrate.reamde.dev/core/core.yaml:113-137`). Binding maps each variant property onto one of three hard-coded columns, `at`, `ends_at`, `due_at` (`isHot`, `HotBinding`, `load.go:1655-1691`); `temporal(point: dueAt)` is how `due_at` is reached, there is no separate due trait (`samples/tasks/task.yaml:21`). On write, `splitProps` moves the value out of the property map (`write.go:427-444`), the apply loop sets `row.At` (`write.go:1008-1037`), the fold carries `delta.at` (`fold.go:120-124, 877-950`), and the upsert writes the column (`rows.go:195-235`). **`at` is stored only in the column, never in `props`** (`schemadiff.go:126-135`: "A hot property is NEVER a key in `props`"); reads project it back into `properties` (`validate.go:1290-1301`). `at`, `endsAt`, `dueAt`, `title` are reserved property names on every kind (`load.go:1320-1325`). Shipped bindings: `temporal(point)` x36, `temporal(range)` x8, `temporal(point: dueAt)` x1 across `kinds/` and `samples/`.

**What a second hot trait would take.** Nothing generalizes hot columns; the three names are hard-coded in about 20 places, and all three are `*time.Time`. A numeric hot column (say `value numeric` for a measurement trait) would need: a SQL migration adding the column and index (precedent `0003_orphaned_at`, `0006_ambiguous_at`); vocabulary (`isHot`, `HotBinding`, `reservedProps`, the binding error, `mapping.go:160-171`); wire constants (`internal/substrate/tier.go:37-39`, `record.go:28-32`); the write path (`splitProps`, `isHotTime`, `hotTime`, the apply loop, `hotColumnOf`, `propertyWritable`) with a non-time parser; the fold (`rowDelta`, `diffRow`, `applyTo`, which changes the changelog payload format: an older binary replaying it would drop the new key unless it decodes strictly, unverified); `rows.go` (`erow`, `recordCols`, the scan, the upsert); `rebuild.go:547-550`; read projection (`recordOf`, `changevalues.go:180-182`, `mapping.go:508-533`, `mappingwhere.go`); query (`recordColumns`, the `HasSuffix(col, "at")` heuristic in `columnValue`, `query.go:1395-1397`); admission (`schemadiff.go:131-158`, `convert.go:271, 289`, `vocabularywrite.go:2207-2210`); `internal/catalog/suggested.go:372-380`; console hot-name handling; docs. There is also no guard today for unbinding `temporal` from a kind holding rows (`schemadiff.go` checks `UsesHot` once), a gap a second hot trait would inherit.

**The cheaper precedent**: `core/recurring` got index support without a column, through a partial expression index over `props` (`internal/engine/migrations/0002_recurring_index.up.sql`, "A partial index rather than a column, so the fold stays untouched"). For a measurement value that stays in `props`, a partial expression index `ON records (repository, kind, ((props->>'value')::numeric)) WHERE <trait kinds>` would not touch the fold, but the filter compiler would have to emit the identical expression for Postgres to use it (it currently binds the key as a parameter and casts per row, section 2).

**`implements`** resolves in Go to a kind list (`ImplementingStrict`, `types.go:1172-1194`, full identities only; a bare name is refused) and compiles to `kind = ANY(...)` (`query.go:666-723`). Nothing trait-specific reaches SQL. `docs/traits.md:145, 162` still show bare `implements` examples that the code would refuse. There is also `GET /api/v1/substrate.reamde.dev/core/trait/{id}/implementors` (`internal/api/api.go:334`). So "all measurements across kinds" is expressible today as `implements: <authority>/<package>/<trait>`, but the per-property typed comparison then uses the first matching kind's datatype (section 2).

**Core traits shipped** (`kinds/substrate.reamde.dev/core/core.yaml`): `temporal` (line 115), `recurring` (160), `override` (189), `accountconfig` (207), `oauth2` (231), `sync` (259; 12 properties including `syncProgress: json`). One sample trait: `samples.substrate.reamde.dev/scheduling/occurrencelog`.

## 7. Providers, the WHOOP sync and record mappings

### The WHOOP bundle (`kinds/providers.substrate.reamde.dev/whoop/`)

Files: `bundle.yaml` (2,442 lines: package, bundle, 7 kinds, the `whoopsync` function with inline Python), `triggers.yaml` (3 triggers), `README.md`. Package `version: 18` (`bundle.yaml:121`). One input, `client` (the `config` kind, `inject: functions`, `bundle.yaml:142-146`). OAuth endpoints at `https://api.prod.whoop.com/oauth/oauth2/{auth,token}`, `featureScopes` maps each toggle to scopes, no `revocationEndpoint` (`bundle.yaml:158-200`). **No mapping, on purpose** (`bundle.yaml:124-126`; `docs/bundles-catalog.md:30`: "No provider ships a mapping").

Kinds and typing (mirrors keep WHOOP's own names and units, no derived values, no `raw`, `bundle.yaml:36-46`; `docs/bundles-catalog.md:801-812`):

| kind | trait | notable properties |
| --- | --- | --- |
| `config` | `core/oauth2` | `clientId`, `clientSecret` (secret), `apiBase` (url) |
| `account` | `core/accountconfig`, `core/sync` | five `enabled*` toggles, `syncFrequency` (`off/hourly/daily`), `backfillDepth` (`none/last2d/last30d/last90d/last1y/all`), `syncCursors` and `syncFloors` (keyed datetime maps), `retryNotBefore`, sync trait fields (`bundle.yaml:277-581`) |
| `user` | none | `userId` int, `heightMeter` float, `weightKilogram` float, `maxHeartRate` int |
| `cycle` | `temporal(range)` | `cycleId` int, `timezoneOffset` string, `scoreState` enum (`scored/pendingscore/unscorable`), `score` object `{strain float, kilojoule float, averageHeartRate int, maxHeartRate int}` |
| `recovery` | `temporal(point)` at `created_at` | `cycle` reference (its identity; a v2 recovery has no id), `score` object `{userCalibrating bool, recoveryScore, restingHeartRate, hrvRmssdMilli, spo2Percentage, skinTempCelsius}` all float, `sleep` reference |
| `sleep` | `temporal(range)` | `sleepId` UUID string, `v1Id` int, `nap` bool, `score.stageSummary` (nested object, int milliseconds and counts), `score.sleepNeeded` (nested object, int ms), four float percentages/rates |
| `workout` | `temporal(range)` | `sportName` string (not an enum), `sportId` int, `score` object with `zoneDurations` nested object (`zoneZeroMilli`..`zoneFiveMilli` int) |

Units are implicit in property names (`kilojoule`, `*Milli`, `distanceMeter`, `skinTempCelsius`), not in values. `restingHeartRate` is float on `recovery` and the heart rates are int on `cycle`/`workout`, which already shows the kind of drift a `quantity` type would remove. No intraday series (heart rate samples) exists: WHOOP's public v2 API does not publish them (recalled, unverified here).

Triggers (`triggers.yaml`): `whoop-on-connect` (record trigger on `account` create/update, guarded by CEL: connected, never synced, a toggle on, `47-73`), `whoop-on-request` (connected and `syncRequestedAck != syncRequestedAt`, `84-100`), `whoop-scheduled` (`FREQ=HOURLY`, `105-119`). All call `whoopsync`.

### How `whoopsync` pages, writes, dedups and updates (`bundle.yaml:1367-2442`)

- `runtime: python`, `timeout: PT60S`, `permissions.network: ["api.prod.whoop.com", "127.0.0.1", "localhost"]` (`1387-1390`), and a body-side origin check because the list is not enforced (`_check_origin`, `1535-1553`).
- Constants: `PAGE_LIMIT = 25` (WHOOP's maximum, `1465`), `BUDGET_SECONDS = 40.0` per invocation (`1469`), `OVERLAP_HOURS = 48` (`1498`), `ALL_EPOCH = "2015-01-01T00:00:00Z"`. Streams in order cycle, recovery, sleep, workout so referenced rows exist first (`1475-1480`).
- **Paging**: request `?limit=25&start=<floor>[&nextToken=...]`; a response `next_token` ends the invocation with `{"more": host.page.more(cur)}` (`2249-2327`). Each WHOOP page is one engine page, i.e. one transaction. `end` is never sent (WHOOP defaults it to now).
- **Writes**: every row is `put(..., if_absent=True)` then a full `patch` (`_Sink.write`, `1881-1885`); a declared field the payload omits is patched to `None`, which deletes it (`1747-1761`). Nested objects are written whole. A thin `user` row is put first for every distinct `user_id` (`1888-1898`).
- **Dedup**: ids are `host.ids.external("whoop", <account id>, "<prefix>/<upstream key>")` (`_rid`, `1737-1738`): `cycle/<id>`, `recovery/<cycle_id>`, `sleep/<uuid>`, `workout/<uuid>`. A re-read converges; an identical rewrite appends nothing.
- **Updates**: WHOOP has no incremental cursor, so each stream re-reads from its `syncCursors` entry minus 48 hours, because WHOOP re-scores for hours after a record appears (`_floor`, `1694-1723`). A drained stream's new cursor is the run-start instant (`2320-2325`). Cursors, floors, `retryNotBefore`, `syncRequestedAck` and the sync-trait fields land in one `patch` of the account (`_stamp`, `2096-2181`), with no `if_version` guard.
- **Deletes**: none. The body never calls `host.effects.delete`; rows go only when the account is deleted (`onDelete: cascade`). Contrast Google Calendar, which sweeps cancelled events.
- **Errors**: 429 parses `Retry-After` into `retryNotBefore` and stamps `throttled`; 401/403/other 4xx abandon the stream; 5xx stamps `erroring`; a malformed record is skipped and counted (`1569-1618, 2266-2275, 2302-2314, 2428-2442`).
- **Risk observed**: WHOOP checks only its 40 s per-invocation budget, not the engine's 2-minute drain deadline (`drainDeadline`, `internal/engine/functions.go:115`). GitHub had to add `DRAIN_SECONDS = 75` and a `chainStart` to stop in time (`kinds/providers.substrate.reamde.dev/github/bundle.yaml:2776-2785, 2834-2841`). A `backfillDepth: all` WHOOP walk would likely park, and its floors are banked only in `_stamp`. Not reproduced; inferred from the code.

### Test seams

- There is **no `tools/mockserver`** in this repo; comments citing it come from the older mneme tree. The mock is `internal/providere2e/runner/mockserver.py` (recordings named `<METHOD>_<path-slug>[__<query-slug>].json`, fallbacks without pagination params, `__response`/`__responses` envelopes, fault injection at `POST /__mock/faults`).
- `internal/providere2e` (`TestProviderE2E`, `e2e_test.go:49` lists `whoop, linear, notion, beeper, github, google, slack`) builds the binaries, registers a repository per provider, rewires the bundle at the mock, completes OAuth against a stub and runs `providers/<p>/scenario.py`. WHOOP's fixtures (`internal/providere2e/fixtures/whoop/`) were written from WHOOP's published OpenAPI schema, not a real pull (`fixtures/whoop/README.md:6-39`). The WHOOP scenario asserts one row per upstream record at WHOOP's id, forbidden derived properties absent (`raw`, `calories`, `asleepMs`, `hrvMs`...), references resolve, temporal rules, enum `scoreState`, the second `nextToken` page, idempotent re-sync, sync-trait progress, and 429/500 behaviour (`providers/whoop/scenario.py:17-42, 865-943`).
- `internal/providertest` runs one callable through `engine.NewStepper` (`internal/engine/steptest.go`) against a Go `httptest` fake, page by page (`harness_test.go`, `github_test.go:59-260`). It covers firecrawl, github, google_drive, google_gmail, slack; **no WHOOP test**.

### Adding a provider

1. `kinds/providers.substrate.reamde.dev/<name>/` with `bundle.yaml` (`core/package`, `core/bundle` whose `installs:` lists the members, the kinds, the functions), `triggers.yaml`, `README.md`.
2. No Go registration: `kinds/kinds.go:43` embeds `all:providers.substrate.reamde.dev`, and the catalog loads every authority but the seed (`kinds/kinds.go:56-58`). (`internal/engine/seedproviders.go` is unrelated: it seeds the three LLM provider rows.)
3. A section in `docs/bundles-catalog.md` plus its counts and table, and `docs/bundles.md`'s provider list; `mise run lint:docs` holds them.
4. Fixtures, `e2e.json` (unique `mockPort`) and `scenario.py` under `internal/providere2e`, and the name in `e2e_test.go:49`; optionally a `providertest`.
5. Console casing and colour (`web/console/src/lib/kind-names.ts`, `actor-identity.ts`).
6. `mise run kinds:check` needs no bump for a new tree.
7. Mappings onto shared kinds go in the package that owns the target kind (decision 0049), never in the provider.

### Record mappings: copy only

Grammar (`internal/vocabulary/mapping.go:206-214`): `from`, `to`, `property`, `match` (probes, optional `fold: case`), `map` (`{path, merge}`, merge `atomic|union|first`), `where` (filter grammar, decision 0118), `onAmbiguous`. The type says it plainly: "Map is assignment paths per target property, nothing else: no expression language, computation stays in connector normalize" (`mapping.go:70-72`); `docs/projection.md:179-190` says "Computation is the bundle's job before the write". Constraints (`mapping.go:111-156, 494-558`):

- paths are `a`, `a.b` or `a[].b` only, so `score.strain` maps but `score.stageSummary.totalInBedTimeMilli` does not;
- **datatypes must be identical** (`sp.Datatype != tp.Datatype` refuses, `mapping.go:540-543`), so even int to float is refused, let alone unit conversion;
- one mapping per (source kind, target kind), and one source record links to one target record, so one WHOOP `recovery` cannot become three readings;
- a mapping cannot write a constant (which observation a reading belongs to) or a reference back to its source.

Several source kinds may target one kind through separate mappings; recompute picks the latest-updated source per property and records per-property provenance (managers with tiers `machine`, `bundle`, `owner`, served as `propertyMeta`, decisions 0094 and 0113; `internal/engine/mapping.go:735-802`).

### Prior art: mneme v6's health package (outside this repo)

`~/src/github.com/geoah/rxtx.dev/svc/mneme-v6/kinds/health/` is a real substrate package (16 kinds) built on this engine. Its `observation` (definition: `unit` as a free string, `valueKind`, `aggregation` enum `sum|avg|min|max|count|latest`, `target`) plus `observationlog` (one reading: `temporal(point)`, `observation` reference, `value` float, `source` reference, `indices: [[observation, at]]`) is the "root + log" pattern; `bloodmarker` carries `value`, `unit` string, `referenceLow`/`referenceHigh`. `mappings.whoop.yaml` explains in four quoted "walls" why the WHOOP projection is a **function, not a `recordmapping`**: one source row makes one target row, a mapping cannot name a constant, cannot fill `source`, cannot do arithmetic. The function `whoophealth` (`timeout: PT20S`, read budget 12 calls / 24 rows) runs on record triggers for the four WHOOP kinds, writes 3 readings per recovery, 2 per sleep (computing minutes asleep), 1 per cycle, converts kJ to kcal (`KJ_PER_KCAL = 4.184`) for `health/workout`, keys every row with `host.ids.external`, and deletes a reading when WHOOP retracts a score. It stores **one record per reading**, with the unit on the definition and the aggregation rule as data the reader applies. This is the closest existing design to the question, and it confirms three gaps: no unit type, no aggregation read, and canonicalisation only possible in a function.

Related history in this repo: issue #498 and commit `f47c21f5` (2026-09-10) deleted the mneme-ported `samples/health` (484 lines) and `samples/fitness` (306 lines) because nothing imported them; issue #125 flagged `workoutset.weight` "carries no unit ... kg vs lb is ambiguous from the first record written"; issue #275 split `fitness/workout` plan from session; issue #143 aligned provider timestamp naming.

## 8. The function runtime

### How a body runs

`main(input, host)` returns `{effects?, output?, more?}` (`internal/runner/host.py:670-733`). One Python process per installation keyed by repository, function and content hash (`internal/runner/pyhost.go:13-20`), speaking a JSON-lines protocol (version 5, `internal/runner/protocol.go:72`). Without a PEP 723 block the system `python3` runs `host.py`; with one, `uv sync --script` provisions a venv at registration and the venv interpreter runs the body (`pyhost.go:220-317`). The sandbox is not bubblewrap: it is Landlock + seccomp + rlimits applied by a re-exec stub (`internal/sandbox/sandbox.go:19-40`).

### Every `host.*` call

| call | where |
| --- | --- |
| `host.records.get(kind, id)` | `host.py:537-540`; Go `internal/runner/runner.go:944-982` |
| `host.records.list(kinds, where=None, first=None, after=None, order=None)` | `host.py:542-558`; Go `runner.go:983-1008`, engine `readList` `internal/engine/runner.go:296-305` (the window read when `at` is bounded both ends, decision 0111) |
| `host.records.search(q, kinds, k=None, mode=None)` | `host.py:560-566`; Go `runner.go:1009-1037` |
| `host.functions.call(function, input=None)` | `host.py:569-577`; Go `runner.go:1038-1061` |
| `host.agents.call(agent, input)` | `host.py:580-590`; engine `runner.go:445` |
| `host.effects.put(kind, id, properties=None, if_absent=False, if_version=..., on_conflict=None)` | `host.py:411-430`, decoded by `internal/engine/effects.go:92-202` |
| `host.effects.patch(...)`, `host.effects.delete(kind, id)` | `host.py:432-448` |
| `host.effects.merge(kind, id, loser)`, `host.effects.split(kind, merge)` | `host.py:450-460` (need `permissions.mutations`) |
| `host.effects.propose(...)` | `host.py:462-508` (sugar for a `core/recordpatchrequest`) |
| `host.ids.external(provider, account, external_id)`, `host.ids.url(url)` | `host.py:229-251`; Go mirror `internal/runner/ids.go:32-69` (slug + first 32 hex of SHA-256, e.g. `whoop-<32 hex>`) |
| `host.page.resume()`, `host.page.more(cursor)` | `host.py:254-273` |
| `host.version(record)`, `host.config()`, `host.log(msg)`, `host.logs` | `host.py:609-629` |

There is **no `host.http`, no `host.blobs`, no file or aggregate call**; the Go dispatcher refuses any other op (`runner.go:1062-1063`). Documented surface: `docs/functions.md:613-633`.

### Outbound HTTP

The body opens its own sockets (WHOOP pins `requests==2.32.4` through PEP 723, `kinds/providers.substrate.reamde.dev/whoop/bundle.yaml:1392-1397`). `permissions.network` is a **binary** gate: declaring any entry allows IPv4/IPv6 sockets, declaring none makes seccomp refuse them; the host names themselves are "still only documentation" (`docs/functions.md:111-115`; `internal/runner/policy.go:105-112`). What is enforced is destination class: every `connect(2)` is checked against `egress.Blocked` (loopback, link-local, RFC 1918, ULA, CGNAT, multicast; `internal/egress/egress.go:17-52`), re-permittable by `SUBSTRATE_SANDBOX_EGRESS_ALLOW`. WHOOP therefore enforces its own origin in `_check_origin` (`whoop/bundle.yaml:1535-1553`). No cap on request count or response size; a body may stream a large response into its own work dir or `TMPDIR` (up to `RLIMIT_FSIZE` 512 MiB, `policy.go:93-96`), but nothing can leave the body except effects and output inside the **8 MiB response frame** (`MAX_FRAME_BYTES = 8 << 20`, `host.py:35`; parent ceiling 16 MiB, `runner.go:453`).

### Limits

| limit | value | where |
| --- | --- | --- |
| source size | 256 KiB (`SourceMaxBytes = 256 << 10`) | `internal/vocabulary/function.go:212` |
| wall clock | default 5 s, max 60 s, host calls included | `function.go:216-217`; `runner.go:315-321` |
| process start | 5 s; 120 s for a PEP 723 provision | `pyhost.go:45, 58` |
| reads per run | calls default 16 / max 1000; rows default 500 / max 10000 | `function.go:218-221` |
| page | 500 rows | `internal/engine/query.go:27` |
| effects per run | **no count cap**; the 8 MiB frame is the ceiling | `host.py:35` |
| paged drain chain | 512 pages, 2 minutes from the first page, 200,000 effects, 256 MiB of effects | `internal/engine/functions.go:110-127` |
| logs | 200 lines, 4096 chars | `host.py:33-34` |
| memory, process count | none (needs a cgroup) | `docs/functions.md:463-466`; `sandbox.go:131-135` |

### Transactions

All effects of one non-paged invocation commit in **one Postgres transaction** together with the delivery's settlement (`internal/engine/functions.go:1007-1024`): `applyEffects` locks every target first and then applies each effect through the ordinary write path (`effects.go:361-372, 397-475`). Effects of functions it calls accumulate into the same transaction (`docs/functions.md:807-818`). But **each record write is its own changelog entry** (section 1); the entries share a `txn` value (decision 0057), plus one `delivery` ledger entry. A paged drain commits one transaction per page with its cursor (`pagedDrain`, `functions.go:1759-1900`; decision 0141 stores only the cursor's hash in the changelog). After a crash or an import into an empty database, a drain that stopped between pages restarts from page one, so effects must be idempotent (`loadPagedProgress`, `functions.go:1932-1972`). Only trigger deliveries page; direct calls and agent tool calls ignore `more` (`internal/engine/runner.go:141-149`).

Throughput implication: a backfill of N samples is N record writes, N changelog lines and N x (properties) manager rows, arriving in chains of at most 200,000 effects per 2 minutes, each chain then parking and resuming on retry.

### PEP 723

Supported (`internal/runner/pep723.go:15-30`), resolved by `uv` against uv's default index (PyPI) at registration, with network allowed and a shared cache under `<UserCacheDir>/substrate-runner/py/uv-cache` (`pyhost.go:167, 339-349`); not offline on first registration. The image is `alpine:3.24` with system `python3` (`Dockerfile:149-150`), so a dependency needs a pure-Python or musllinux wheel. `fitparse` and `pint` are pure Python as far as recalled (unverified here), so `import fitparse` or `import pint` should work. A FIT file still cannot reach the body except over HTTP from the upstream, since blob bytes are not readable from a body.

### What the sandbox forbids

Landlock: read/execute only system dirs and the interpreter tree, read-only specific `/etc` files, read/write only its own work dir and `/dev/{null,zero,full,random,urandom}`, nothing under `/proc` (`policy.go:23-103`). Seccomp deny-list: ptrace, mount, io_uring, bpf, perf, module loading, setns/unshare, clone3, keyrings, userfaultfd and more (`internal/sandbox/seccomp_linux.go:292-371`). Subprocesses are allowed and inherit the jail (`sandbox.go:39-40`). Environment is default-deny (`runner.go:655-685`). macOS has no confinement (`docs/functions.md:442-451`).

## 9. Agents

Four built-in tools, all `runtime: host` functions in `kinds/substrate.reamde.dev/core/hostfunctions.yaml`, dispatched at `internal/engine/agentloop.go:1090-1103`: `core/query`, `core/write`, `core/propose`, `core/ask`; plus declared function tools and sub-agents.

`query` (`runQueryTool`, `agentloop.go:1226-1378`) takes `id`, `kind`, `filter`, `orderBy`, `first`, `after`, `expand`, `q`, `mode` (`hostfunctions.yaml:56-96`). With `kind` + `id` it returns one record; with `q` a ranked search (filter limited to `kinds`/`purposes`); otherwise a list through `readList` (the window read when `at` is bounded both ends). Default `first` is 20 (`listDefaultFirst`, `agentloop.go:61-63`), max 500, clamped to the remaining row budget. Budgets come from `permissions.reads`: 16 calls / 500 rows by default, at most 1000 / 10000 (`internal/vocabulary/agent.go:437-443`). The result is the plain `json.Marshal` of each full `substrate.Record` (every property, labels, annotations, version, timestamps; `agentloop.go:1068-1074, 1377`, `internal/substrate/record.go:13-51`): **no projection, no count (the `Count` field exists on `substrate.Query` but the tool never sets it), no aggregation, no summarization**. Run budgets: `maxTurns` 8 (max 64), `maxToolCalls` 32 (max 256), `deadlineSeconds` 120 (max 600) (`docs/agents.md:103-105`). Thread compaction (decision 0138) only trims older conversation turns.

"How did I sleep this month":

- **Over 30 WHOOP `sleep` mirror rows**: one `query` call, e.g. `kind: providers.substrate.reamde.dev/whoop/sleep`, `filter: {properties: {at: {gte: "2026-09-01", lt: "2026-10-01"}}}`, `first: 31`. About 30 full records (each with the nested `score.stageSummary` in milliseconds) land in context, and the model does the arithmetic (total asleep = in bed minus awake minus no-data). Cheap and feasible today.
- **Over 40,000 heart-rate samples**: not feasible. The per-run ceiling is 10,000 rows, a page is 500 full records (80+ calls), every record is serialized whole into the context, and there is no count, projection or aggregate. Even a function tool cannot do it on demand: it is also capped at 10,000 rows and cannot page (only trigger deliveries page). The answer has to be precomputed: a triggered or scheduled function writing daily summary records, or a new aggregate read the `query` tool can call.

## 10. What the repository already says about health, units, series and aggregation

Searched on 2026-10-08 with `gh issue list -R geoah/substrate --search "health OR fitness OR whoop OR units OR aggregation OR timeseries OR measurement" --state all --limit 100` plus follow-up searches (`sum OR avg OR group OR histogram`, `batch OR bulk`, `compaction`, `quantity OR unit`, `series`, `large OR volume OR scale OR performance`), and by grepping `docs/`.

### Issues

| # | state | what it says that matters here |
| --- | --- | --- |
| #108 | closed 2026-08-15 | "Numbers: no decimal or money type, int is silently 53-bit, duration is Go-syntax only". Produced decision 0012 (exact or refused) and later 0140 (money). Notes "the calendar and health vocabularies will want" ISO durations. |
| #125 | closed 2026-08-19 | Rename before freeze, including "`workoutset.weight` carries no unit ('in the gym's unit'): kg vs lb is ambiguous from the first record written". The only issue that names the unit problem directly. |
| #275 | closed 2026-09-29 | `fitness/workout` was both the recurring plan and one session (`strain`, `averageHeartRate`, `energyKcal`); "tracker totals on a plan row mean nothing". |
| #143 | closed 2026-09-29 | Provider mirror kinds disagree on timestamp names and types (WHOOP `start`/`end` vs Google `startAt`/`endAt`); align and document which form new mirrors copy. |
| #498 | closed 2026-09-10 | Deleted seven mneme-ported sample packages including `samples/health` (484 lines) and `samples/fitness` (306), because nothing imported them (commit `f47c21f5`). The health vocabulary now lives only in mneme v6 (section 7). |
| #679 | closed 2026-09-29 | Console "Group by" with counts on collections: deferred by the owner. The only aggregation request on record, and it is a UI ask. |
| #761 | closed 2026-09-29 | Real scale numbers: `geoah.me` repository, 2,168,142 entries, 15.8 GB of segments; snapshot 6 h 39 min on a Pi CM4; verify about 1 ms per entry, 70% of CPU in `changelogfile.Decode`. |
| #760 | closed | Paged drains wrote the whole cursor per page: Slack was about 10.5 GB of the 15.8 GB. Fixed by decision 0141. Shows the changelog size is a live operational concern. |
| #745 | closed | Snapshot/verify read about 50x the repository size by re-scanning segments per 500-row page. |
| #719 | closed | First open after a search-index version bump re-derived `fts` for 336k records in 24 minutes with every API request blocked; a records list normally answers in 0.6 s on that repository. |
| #157 | open | Vocabulary conversions run in one transaction and are refused above 10,000 live records (`DefaultConversionCeiling`); chunked execution unbuilt. A high-volume health kind would hit this on its first narrowing. |
| #142 | open | Five deferred core-kind items; not health-specific. |
| #482 | open | Strategy issue; proposes "content out of the log: the changelog stores hashes, references, and provenance; payload content lives in content-addressed storage", which is the direction a chunk-as-blob design would lean on. |
| #76 | closed | Agent thread compaction (see below). |

No issue proposes a measurement/quantity type, a unit registry, a time-series store, an aggregation API or data compaction. Searches for `observation`, `timeseries`, `quantity` returned nothing relevant.

### Decision records that constrain a design

- **0012** Numbers are exact or refused: `decimal` rides in strings, `int` is a safe integer, filters and ordering cast `::numeric`. A `quantity` value would inherit this: float64 or exact digits, chosen up front.
- **0140** Money is `{amount, currency}` with the amount an integer of the currency's minor unit; the engine owns the code table (`internal/vocabulary/money.go`), the table only grows, the console carries a copy held equal by a test; a filter compares within one currency; ordering sorts by exact value across currencies. This is the direct template for a unit-bearing value type (section 5).
- **0020** Dialect keys are reserved by name, never tolerated by prefix; a new declaration key lands inert and validated, in batches. A new `unit:` key on a property, or a new datatype, is an upgrade every binary must understand.
- **0039 / 0043 / 0081 / 0111** The substrate stores a rule and never expands it in storage; occurrences are computed at read in `internal/window`, used by the route, functions and agents alike. This is the existing precedent for "computed at read, one implementation for every surface", which an aggregation read would follow. 0039 lists `health/observation` and `fitness/workout` among the kinds that carried an RRULE.
- **0044** A reference is the only link; **0049** the owner of a mapping's target declares it.
- **0050 / 0057 / 0062** The changelog is checksummed ndjson segments; a transaction's lines carry `txn` and never cross a segment; the final newline is the commit marker. Parquet was considered and rejected in 0050 ("its footer is written at close, so a file under active append is unreadable ... a columnar layout pays nothing for heterogeneous JSON payloads read sequentially").
- **0079** The records read is one route; **0134** `count=1` rides the list rather than a second route, with "the cost is unbounded and uncapped" accepted.
- **0099** Repository migrations are code run once per repository at first open, writing through the changelog.
- **0136** Search ranks by BM25F and a kind's purpose; **0133** a kind declares `purpose` (`primary`, `supporting`, `internal`). A samples kind would be `supporting` or `internal` so it stays out of default lists and search.
- **0138** "A compacted thread keeps its rows and replays a summary message over the range it covers": about agent context, but its shape (keep the source rows, add a summary row naming the range it covers) is the same shape a rollup record would have.

### `docs/plans/compaction.md`

It is about **agent conversation context** (issue #76, decision 0138), not data compaction: a `summary` message role with `covers: {from, through}` replayed in place of older turns. Nothing in it addresses compacting records or the changelog. There is no plan for changelog compaction; the changelog is append-only by design and only `gc` purges tombstoned records (decision 0072 reserves a purged id).

### Plans that name health

`docs/plans/providers-and-samples.md:22, 88` and `docs/plans/packages.md:123-124` list `fitness`, `food`, `health` among planned sample packages; those pages "record what was designed then, not the tree now" (commit `f47c21f5`). `docs/plans/google-calendar-mirror.md:248-249` notes the health and fitness kinds no longer exist in this tree.

## Extension points ranked by how much they bend the current contract

Ranked from least to most bending. "Contract" means the rules in CLAUDE.md and the decision records: the changelog is the truth and `records` its fold, a reference is the only link, declared equals indexed equals filterable, behavior lives in declarations, one records read route, exact numbers or refusal, additive vocabulary upgrades, reserved dialect keys.

| rank | extension | engine change | what it bends | what it buys | limits it keeps |
| --- | --- | --- | --- | --- | --- |
| 1 | **One record per reading in user-space kinds, canonicalised by a triggered function** (the mneme v6 `observation` + `observationlog` pattern, section 7) | none | nothing | works today; mirrors keep upstream units, the function converts (kJ to kcal, ms to min) and computes (minutes asleep); dedup by `host.ids.external`; retraction by delete | about 1 KB of changelog per reading plus a `records` row, ~11 index entries and one `property_managers` row per property (section 1); no aggregation; agent reads cap at 10,000 rows per run; fine for daily summaries (WHOOP is ~1,500 records a year), wrong for 1 Hz samples (~31.5 M rows and ~32 GB of segments a year) |
| 2 | **Make declared `indices:` usable for typed ranges**: emit the same expression the index declares (literal key, same cast), add typed expression indexes (`((props->>'value')::numeric)`), lead with `kind` for `at` ranges, use the declared datatype when the filter names an `implements` trait | small, query compiler and `indices.go` | nothing; it makes the existing "filterable = indexed = declared" promise true for numbers and dates | index-backed `value > x` and per-kind `at` windows | still one row per reading |
| 3 | **An aggregation read mode on `GET /api/v1/records`** (bucket by `date_trunc` over `at`, measure `min/max/avg/sum/count/latest` over a numeric property, group by a reference such as `observation`), a new `Dataset` method beside `count=1`, wired into `readList` so functions and the agent `query` tool get it | medium, additive | adds a fourth mode to the one route; precedent is 0079 (one route), 0134 (`count=1` rides the list) and 0111 (functions and agents get the same reads); must be added to `internal/api/fake_test.go` | "how did I sleep this month" in one call returning 30 numbers, not 30 full records; charts without client folds | cost scales with rows scanned (no cap exists for `count=1` today); needs rank 2 to be fast |
| 4 | **A `quantity` datatype `{value, unit}`** modeled on `money` (engine-owned unit table that only grows, console copy held equal by a test, filter within a unit or within a dimension, order across units by converted value) | medium, about 20 files plus console (section 5 checklist), a decision record | additive dialect; every binary reading such a closure must know the type (decision 0020); conversion semantics are new (money never converts) | units become data instead of property-name suffixes (`kilojoule`, `*Milli`) or free strings (`observation.unit`); canonicalisation can be checked by the engine | does not change storage cost or aggregation |
| 5 | **Cheaper changelog lines for bulk machine writes**: one manager effect per record (actor and tier once, property list) instead of one per property, or a dedicated bulk-put effect | medium | the fold payload format and replay (`changelogdialect.go` exists for exactly this kind of change); `property_managers` semantics if rows are collapsed | roughly halves the per-reading line size for small records (the manager effects are about half of a 1 KB line for a two-property reading, more for wider records) | still one line and one row per reading |
| 6 | **A derived `samples` table** fed from records of kinds binding a declared measurement trait, derived like `refs` (pure function of the folded row and declaration, re-derived in `foldRecordOp`, cleared and replayed by `RebuildRepository`, reprojected on a vocabulary change) | medium-high: SQL migration, fold hook, rebuild table list, reprojection, query mode | none of the written rules: it is the third instance of an existing contract (`refs`, `fts`) | narrow typed rows, `(kind, series, at)` btree, cheap GROUP BY; records stay the truth | write cost per reading unchanged (it adds a row) |
| 7 | **A rollup table** (daily/weekly buckets per series) maintained asynchronously through a dirty-bucket queue, like `embed_queue`, kept or recomputed after a rebuild | medium-high: a worker, a queue, invalidation | adds a derived store computed from many rows; precedent exists (`embeddings`, `property_offers`) | constant-time month and year summaries for agents and charts | eventual consistency; a second place an answer comes from |
| 8 | **A second hot trait with a numeric column on `records`** (e.g. `measurement` mapping `value` to a `value numeric` column) | high: the three hot names are hard-coded in ~20 sites, all typed `*time.Time`; the fold delta gains a key | the fold format and the "one hot trait" statement in `docs/traits.md:181-190` | a plain btree on a real column | wastes a column on every non-measurement row; same per-row write cost |
| 9 | **Chunked series records**: one record per (series, day) holding the samples as a repeated float or `json` array, with `at`/`endsAt` spanning the chunk | none | "declared = filterable" for individual samples (an array element cannot be filtered or ordered); the 1 MiB request body bounds a chunk (1 Hz for a day is 86,400 numbers, roughly 0.4 to 1 MB of JSON depending on precision); patches append by rewriting the whole array, so the changelog grows by the chunk size per update | 1 changelog line per chunk instead of per sample (orders of magnitude fewer rows and lines) | values opaque to the query language; aggregation must happen in a function or a new read mode that unnests arrays |
| 10 | **Series bytes in blobs** with a manifest record and a derived index computed from blob contents | high: a host upload call for functions, a server-side blob reader, a derived index fed from bytes rather than the fold, range reads | "the changelog is the truth" (the samples would not be in the changelog at all, only their digest); the derived-index contract (derived from the fold); blobs are plaintext (0031) and unencrypted health data is a new exposure | compact storage, no per-sample changelog cost, 64 MiB per object | nothing in the server reads blob bytes today (section 4); rebuild would need to re-read blobs |
| 11 | **A separate time-series store** (columnar files or an extension such as TimescaleDB) outside `records` | very high | everything: a second truth, a second query language, backup outside the repository directory (0051); Parquet was considered and rejected for the changelog in 0050 | best raw performance | breaks the one-store, one-fold model the codebase is built around |

Reading the table: ranks 1 to 3 are compatible with the current contract and together answer the daily-summary use case (WHOOP cycles, sleeps, recoveries, workouts, lab markers). The intraday volume (heart rate, steps, glucose at minute or second resolution) is where every option either keeps a ~1 KB-per-sample cost (ranks 1, 6, 8) or gives up per-sample addressability (ranks 9, 10, 11). Rank 9 is the cheapest way to cross that line without engine work; rank 6 plus rank 3 is the most contract-faithful way to make per-sample reads fast, but it does not reduce write cost, so it only works together with rank 5 or a decision that intraday samples are stored as chunks.
