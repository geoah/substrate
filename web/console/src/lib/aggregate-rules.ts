/** The aggregate read's rules as the server enforces them: the reducers
 * each form admits, the summary statistics `sum` adds, each budget's
 * default and cap, and the rollup tiers a metric may keep.
 * `aggregate.rules.json` is written from the server's own tables by a Go
 * test (`TestConsoleAggregateRulesMatchTheServer` in internal/vocabulary),
 * so the console never holds a copy that can drift; regenerate it with
 * that test's `-update`, never by hand. */

import rules from "./aggregate.rules.json"

/** The keys `FORM_REDUCERS` holds: the four forms, and a coded gauge and a
 * coded amount (a metric with a code set). */
export type ReducerTable =
  "gauge" | "amount" | "counter" | "summary" | "coded gauge" | "coded amount"

/** The reducers each form admits (`substrate.FormReducers`): the read
 * refuses any other. */
export const FORM_REDUCERS: Readonly<Record<ReducerTable, readonly string[]>> =
  rules.reducers

/** Every reducer the read knows, any form. */
export const ALL_REDUCERS: ReadonlySet<string> = new Set(
  Object.values(rules.reducers).flat()
)

/** The summary statistics `sum` adds (`substrate.AggregateAdditiveStatistics`). */
export const ADDITIVE_STATISTICS: readonly string[] = rules.additiveStatistics

/** The default `budget.maxBuckets` a read is held to; past it the server
 * refuses with a `422` before reading anything. */
export const MAX_BUCKETS = rules.budget.maxBuckets.default
/** The most `budget.maxBuckets` may raise it to. */
export const MAX_BUCKETS_CAP = rules.budget.maxBuckets.cap
/** The most groups one read may ask for (`budget.maxGroups`'s cap). */
export const MAX_GROUPS_CAP = rules.budget.maxGroups.cap

/** The tier widths a metric's `rollups` may list, finest first. */
export const ROLLUP_TIERS: readonly string[] = rules.rollupTiers

/** The tiers a metric keeps where its `rollups` lists none. */
export const DEFAULT_ROLLUPS: readonly string[] = rules.defaultRollups
