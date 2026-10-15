# Codebase map: the fold, migrations, the records read, the contract

Snapshot of 2026-10-11, taken by a read-only agent for the observations
build. Paths are relative to the repository root.

Two traps first: the derived-table hook has to cover tombstone, purge and
restore separately, and a rebuild never passes through `inTx`, so any
per-transaction flush needs its own call.

## 1. The fold

One door for live writes, rebuilds and boot imports:
`internal/engine/fold.go:404` `foldOne(op)` is the fold.

- Live write: reaches it through `txn.fold` (`fold.go:337`).
- Replay: reaches it through `foldEntry` (`fold.go:1249`, which sets
  `t.now = ch.TS`).
- Rebuild: `RebuildRepository` (`rebuild.go:123`) to `t.rebuild`
  (`rebuild.go:224`) to `foldEntry` (`:268`).
- Boot import: `refoldFromFiles` (`repodir.go:775`) runs `t.rebuild` twice:
  once under an empty registry, then again after
  `loadDeclarationsForReplay` (`:809`).

The hook point is `foldRecordOp`, `fold.go:464-524`, which loads the row,
applies the delta, upserts, then derives refs:

```go
op.Delta.applyTo(row)
changed, created, version, err := t.upsertRecord(row, t.foldFTS(row), op.Delta.Force, op.Delta.Restored)
...
if changed {
    row.UpdatedAt = t.now
    if op.Delta.Restored { row.DeletedAt = nil }
    ty, _ := t.derivationView().ByIdentity(row.Kind)
    if ty == nil || declaresReference(ty) {
        if err := t.syncRefs(ref, ty, row.Props); err != nil { ... }
    }
}
```

A sibling `syncObserved(ref, ty, row)` belongs next to `syncRefs`, skipped
for kinds that do not bind the trait. Use `derivationView()` (`fold.go:768`)
so that parked kinds and the candidate registry during an apply resolve the
same way refs do.

Refs as the model: `deriveRefs` (`refs.go:76`) is pure: no clock, no
database. `syncRefs` (`refs.go:280`) deletes and re-inserts per source
record. `syncRefsOfRows` (`refs.go:307`) is the batched version using
`unnest`.

Tombstones and deletes need their own hook calls in `foldOne`:

- `foldTombstone` to `applyTombstone` (`rows.go:367`) updates `deleted_at`
  only. Refs are kept on purpose (decision 0027), so observed rows must be
  removed in this case explicitly.
- `foldPurge` to `applyPurge` (`rows.go:704`) runs the per-table `DELETE`
  list. Add the observed tables there.
- Restore arrives as a `Restored` delta through `foldRecordOp`, so the hook
  there covers it. Check `row.DeletedAt`: a tombstoned row that is updated
  without being restored must derive nothing.
- Merge, GC, move and rename all go through `fold`, `foldRow`, `tombstone`
  and `hardDelete` (`merge.go:216,224`, `move.go:642,717`, `gc.go:213,264`).
- The only writers of `records` are `upsertRecord`, `applyTombstone`,
  `applyBump`, `applyPurge` and `rederiveFTS`, plus the orphan and
  ambiguity marks.

Rebuild bookkeeping:

- `foldTables` (`rebuild.go:107`) is the list of tables a rebuild clears.
  Add the new tables here.
- `foldSnapshot` (`rebuild.go:557-599`) is an explicit query map. Add
  sections here, or `TestRebuildReproducesTheFold` will not see the new
  tables.
- `FoldTables()` (`export_test.go:175`) feeds the refold test automatically.

Per-transaction flush (for "recompute touched buckets once"):

- Live writes: `inTx` calls `settleFold` at `dataset.go:671`, before
  `settleChecksums`.
- Rebuild and import build their own `txn` (`rebuild.go:163`,
  `repodir.go:783`) and never reach `inTx`. Flush at the end of `t.rebuild`,
  before `rederiveOffers` (`rebuild.go:276-279`).
- `inRawTx` refuses any fold (`dataset.go:1165`).

How `reprojectFTS` re-derives (`fold.go:793`): it pages `records WHERE
kind=$1 AND id>$2` in batches of `rebuildBatch` (`rebuild.go:113`) and calls
`rederiveFTS` (`fold.go:829`), a single `UPDATE ... FROM unnest(...)` that
touches neither `version` nor `updated_at`. Callers: the boot upgrade
(`reprojectShipped` at `seed.go:341`) and repository migration 0001 (line
189). A vocabulary apply does not call it: refs are re-derived inside the
transaction (`vocabularywrite.go:573` to `rederiveMovedRefs`,
`reprojection.go:335`); fts is queued (`requestMovedFTS`,
`reprojection.go:378`, writes `index_reprojections`; the background pass
`reprojectRows`, `reprojection.go:702`, runs under `inRawTx`).

Adding a third index to the reprojection machinery means extending
`indexReprojection{refs, fts}` (`reprojection.go:112`), `addMoved` and
`movedProperties` with a shape function (`:153-284`), `withEveryRow`
(`:183`), the `requestReprojections` SQL (`:307`), `reprojectRows`, and the
`index_reprojections` columns via a new `ALTER` migration (0011 is frozen).

Open-time backfill model: `searchindex.go:27` `checkSearchIndex` compares
the `search_index` version (migration 0009) with the binary's version and
re-derives behind the open.

## 2. Migrations

- Naming and numbering: `NNNN_name.up.sql` in `internal/engine/migrations/`.
  The highest is `0011_index_reprojections`, so the next is `0012`.
- Runner: loader `migrate.go:290`; runner `migrateThrough` (`:78`) holds an
  advisory lock and runs one transaction per migration (`applyMigration`
  `:273`). The sha256 is recorded, and an edited migration is refused
  (`checkRecorded` `:191`).
- Checks: `mise run lint:migrations` (`.mise.toml:340`) and `frozen:check` (`:420`).

Row-level security pattern. Copy `0011_index_reprojections.up.sql`
verbatim: `repository text NOT NULL DEFAULT
current_setting('substrate.repository')`; the primary key leads with
`repository`; `ENABLE` plus `FORCE ROW LEVEL SECURITY`; `CREATE POLICY
<t>_repository ... FOR ALL USING/WITH CHECK (repository =
current_setting('substrate.repository', true))`; an explicit `GRANT` `DO $$`
block (the `ON ALL TABLES` grant in 0001 only covered tables that existed
then). The rationale is in the `0001_init.up.sql:1-26` header.
`0010_records_matching` explains why non-leakproof operators under RLS
cannot probe indexes.

Lists to extend: `repositoryScopedTables` (`repositories.go:478`), used for
erase and rollback at `:397,421,456`; the test copy at
`isolation_db_test.go:28` (the subtest at about `:570-620` asserts
relrowsecurity and forced, the policy expression, and the NOT NULL default).

Schema assertion. There is no schema golden. The guards are the isolation
test above and `migrationrefold_db_test.go:79`
`TestEveryMigrationLeavesAFoldTheRebuildReproduces`, which stages each
migration, compares the live fold with the rebuilt fold over `FoldTables()`,
and holds the rule that a SQL migration may not change the fold.
`refoldSeeded` is at `:54`; `refoldLags` is frozen (`:67`). Data backfill
belongs in a repository migration (`repomigrate.go:77`,
`repomigration_NNNN_<name>.go`), not in SQL.

## 3. The records read

Route: `internal/api/records.go:49` `getRecords` picks one of three modes in
a `switch` (`:58`): `watch=1`, then `q`, then the list. Parameter
allow-lists are at `:40-46`. The refusal patterns `rankedFilterArm` (`:204`)
and `filterArmBeyondKinds` (`:193`) are the model for an aggregate mode's
allowed filter arms. The mount is `api.go:268`. Parsers in `rest.go`:
`unsupportedParam` `:247`, `parseFilter` `:298` (strict JSON), `parseQuery` `:326`.

Request struct (`internal/substrate/query.go:173`):

```go
type Query struct {
    Filter  Filter  `json:"filter"`
    OrderBy []Order `json:"orderBy,omitempty"`
    First   int     `json:"first,omitempty"` // default 50, max 500
    After   string  `json:"after,omitempty"` // opaque cursor
    Offset  int     `json:"offset,omitempty"`
    WithAnnotations bool `json:"withAnnotations,omitempty"`
    Expand  []string `json:"expand,omitempty"`
    Count   bool     `json:"count,omitempty"`
}
```

- `Filter` is at `:36`: kinds, implements, purposes, ids, properties,
  labels, deleted, orphaned, ambiguous, referencing, search. `Cond` is at
  `:13`, `Page` at `:214`, `RankedPage` at `:241`.
- Time windows: `filter.properties.at` with both ends set becomes a window
  read. `window.Bounds` (`internal/window/window.go:65`) to `window.Read`;
  otherwise `ds.List` (`records.go:136-145`).
- Ranking is `q` and `mode` to `ds.Search` (`engine/search.go:27`). The tail
  is `streamChanges`.

Engine SQL builder: `internal/engine/query.go:436` `List` opens a repeatable
read, read-only transaction, builds `b := &builder{overRecords: true}`, calls
`ds.buildFilter(ctx, tx, b, q.Filter)`. `builder` is at `:386` (`arg`,
`add`, `textArray`). `buildFilter` (`:677`) returns the admitted kinds.
`listSQL` is at `:648`. `countSQL` (`:663`) is the closest template for an
aggregate over a filtered `records` set.

How property filters compile: `condProp` (`:858`) routes to `condColumn` for
hot columns (`:1452`; map at `:834`), to `states` for state properties,
refuses sensitive properties, and sends references to `condReference`.
`condMoney` (`:944`) handles money. `condJSON` (`:1520`) handles everything
else: int and float as `(props->>k)::numeric`, decimal as exact digits
`::numeric`, datetime as `::timestamptz`.

Fake: `internal/api/fake_test.go:823` `List` records `d.lastQuery` (field
`:302`) and filters in memory. `fake.fail("List")` provides error injection.
The interface assertion is at `:1187`. The closest precedent for a new read
method is `fake_window_test.go:20` (`Window`). Test style:
`records_test.go:72` `TestRecordsListCarriesTheGrammar` asserts on
`ds.lastQuery`.

Adding a field or parameter to the contract: add the Go struct in
`internal/substrate`; add it to `wireTypes` (`wire_test.go:44`); regenerate
with `go test ./internal/substrate/ -run TestWireGolden -update` (writes
`web/console/src/lib/api/wire.golden.json`); mirror it in
`web/console/src/lib/api/types.ts` (`Page` `:236`, `Cond` `:401`,
`RecordFilter` `:428`); optionally add a client in `records.ts` (`listPath`
`:111`, `searchQueryOptions` `:552`); add a feature entry in
`api/discovery.go` (about `:183`; `search` is beta); update `docs/api.md:56`.

## 4. The substrate contract

`substrate.Dataset` (`internal/substrate/dataset.go:25`) is one frozen v1
interface with no optional extensions. Every method must be implemented by
both the engine (`engine.go:1452` asserts `_ substrate.Dataset =
(*dataset)(nil)`) and the API fake (`fake_test.go:1187`). Precedent for
adding a read: `Window`, with `WindowQuery` in `substrate/occurrence.go:24`,
engine `engine/window.go:39`, fake `fake_window_test.go:20`. `window.Reader`
(`window.go:119`) shows the narrow consumer-interface pattern. The function
host's list goes through `readList` (`engine/runner.go:335`).

Tests. All engine DB tests are `*_db_test.go` in package `engine_test`, run
with `t.Parallel()`. `newService` (`helpers_db_test.go:45`) uses
`engine.MigratedDSN` (`export_test.go:134`), a clone of `migratedTemplate`
(`:111`) which is `testdb.NewTemplate` (`testdb.go:379`), via `Clone`
(`:389`). `testdb.NewSchema` (`:261`) is for from-empty migration tests.
Fixtures: `newDataset` (`:73`), `importVocabulary` (`:115`), `mustPut`
(`:231`). Rebuild pattern: `rebuild_db_test.go` `foldOf` `:38`,
`TestRebuildReproducesTheFold` `:229`.

## 5. Existing aggregation, bucketing and numerics

- No time bucketing or statistics anywhere: no `date_trunc`,
  `width_bucket`, `percentile`, `time_bucket` or `generate_series`. The only
  SQL aggregates are `avg` and `sum` in `bm25.go:119,260`.
- Datatypes: `vocabulary/types.go:26-45`.
- Decimal: `canonicalDecimal` (`validate.go:713`) stores exact digit strings,
  compared with `::numeric`.
- Money: the shape is `{amount int minor-units, currency}`
  (`vocabulary/money.go:21`, `CurrencyDecimals` `:78`); coercion is
  `coerceMoney` (`validate.go:742`). `moneyValueSQL` (`query.go:915`) gives
  the exact `numeric` value using the per-currency exponent
  (`currencyDecimalsSQL` `:925`). `condMoney` refuses comparisons across
  currencies, so an aggregate should group by currency.
- Classifiers: `numericProp`, `moneyProp`, `datetimeProp`
  (`query.go:1347-1399`). Each holds only if every kind that declares the
  name agrees.
- Timezones: `time.LoadLocation` in `occurrence/occurrence.go:80` and
  `window.go:448` (`wallClockEnd`).
- Tests: `money_db_test.go`.

## 6. The CLI

- `cmd/substratectl/commands/get.go:18` `getCommand` builds the parameters
  with `listQuery` (`:284`) and `editFilter` and `setFilterKinds`
  (`client.go:281-305`), then calls `cl.list` (`client.go:261`) on
  `pathRecords` (`client.go:43`). Output goes through `printRecords` (`get.go:222`).
- Best template for an `aggregate` subcommand: `search.go:18`. It is a
  separate records-route mode with its own `cl.search` (`client.go:273`),
  table printer (`:118`) and `-o yaml|json`.
- Plumbing: register it in `root.go:218-242`; `printAs` is at `render.go:129`.
- CLI tests: fake server `fake_test.go:27` (`handler` `:198`), assertions as
  in `records_route_test.go`. Docs: `docs/substratectl.md`, the Reading section.

## Gotchas

- Tombstones: refs survive a tombstone; observed rows need explicit removal
  in the `foldTombstone` branch and re-derivation on restore.
- Clock-free derivation: the derived columns must be a function of (row,
  declaration) alone, with no clock, or rebuilds diverge (`refs.go:274`).
- Rebuild flush: rebuild and import never run `settleFold` or `inTx`, so a
  per-transaction flush needs its own call in `t.rebuild`.
- Refold test: if no seeded record binds `observed`, the new tables are
  empty on both sides and the refold test passes without testing anything.
  Add observed data to the seed (`seedRefoldSource`,
  `migrationrefold_db_test.go:159`) to make it meaningful.
