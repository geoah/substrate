# Data flows

Every source takes the same path: a provider mirrors what arrived, an adapter function writes observations, the changelog records them, the fold builds the rows, and apps read through one route. The only part that differs per source is the adapter.

## The flow, drawn

&#91;embedded content: four source lanes converging on the changelog, the fold and the two reads\]

Read each lane top down: what arrives, what the adapter writes, what the fold derives. The bottom two rows are shared by every lane and already exist, except the aggregate read.

## Each step

| Step | Who does it | Exists today? | Per day, one heavy user (estimate) |
| --- | --- | --- | --- |
| the provider sync writes mirror rows | the provider's own sync function, on its account record | yes for WHOOP, which ships today; Oura, Withings, the Apple Health export and Health Connect are proposed and not yet written | WHOOP: 3 to 5 rows; an Apple export: one file |
| the adapter writes observations and traces | one function per provider, in a sample such as `healthwhoop`, fired by a record trigger on the mirror kinds | new, one per provider | 100 to 150 records holding 45,500 values |
| the changelog append | the engine, as for every write | yes | about 1 KB per small record, 8 KB per hour of 5-second samples |
| the fold into `records` | `fold.go`, the one path from changelog to rows | yes | one row per record |
| deriving `refs` and `fts` | `fold.go`, same transaction | yes |  |
| deriving `observed_points`, `observed_rollups`, `observed_latest` | `fold.go`, same transaction, only for kinds that bind `observed` | new | 45,500 point rows, a few thousand rollup rows, one latest row per stream |
| list and filter reads | `GET /api/v1/records` | yes |  |
| aggregate reads | `GET /api/v1/records?aggregate=`, `host.records.aggregate` in functions, the `query` tool's `aggregate` argument | new |  |
| rebuild and verify | `substratectl repository rebuild` replays the segment files; the three tables are cleared and re-derived; `verify` never reads them | yes, extended to the three tables |  |

## Corrections, late data and deletions

Every correction is an ordinary write with a higher `revision`, and projecting one record never reads another, so a correction touches only its own rows and buckets.

1. **A rescore.** WHOOP changes a cycle's score. The mirror row's `updatedAt` rises, the trigger fires, the adapter rewrites the same ids with the higher revision, and the fold deletes the old point rows, inserts the new ones, recomputes the touched 15-minute buckets and the stream's latest row, all in one transaction.
2. **A late delivery.** An Apple Health export arrives a day late with an hour's samples. The adapter rewrites that hour's trace; only that hour's buckets change.
3. **A stale delivery.** A rewrite with a lower revision than stored is refused. Nothing changes, and a re-sync of old pages cannot overwrite a newer result.
4. **An upstream deletion.** WHOOP deletes a sleep. The mirror row goes, the adapter finds its outputs by `referencing` the origin and deletes them, and the tombstone removes their points, recomputes the buckets and recomputes the latest row from the points that remain.
5. **A user edit.** A hand-entered weighing corrected with a PUT takes the same path as any record write.
6. **A rebuild.** Clears the three tables and replays the segment files. Export and verify read the changelog alone, because the three tables are never the truth.
