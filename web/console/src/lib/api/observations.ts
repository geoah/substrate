/** The aggregate read: `GET /api/v1/records?filter=…&aggregate=…`, the
 * records route's mode that reduces observed values into buckets
 * (docs/plans/observations/implementation/README.md pins the wire). One
 * answer shape, `AggregateResult`, for every request.
 *
 * The aggregate travels as strict JSON in its own query parameter, beside
 * the ordinary `filter`, and is refused beside `q`, `watch`, `count`,
 * `orderBy`, `first`, `after`, `offset` and `expand`, so none of them is
 * ever sent here. */

import { queryOptions, type QueryClient } from "@tanstack/react-query"

import { request } from "./http"
import { RECORDS_PATH } from "./records"
import type { Aggregate, AggregateResult, RecordFilter } from "./types"

/** One aggregate read: the records filter and the `aggregate` parameter,
 * which this module encodes onto the records route. */
export interface AggregateRead {
  filter?: RecordFilter
  aggregate: Aggregate
}

/** The query string of one aggregate read, `filter` first. */
export function aggregateSearch(read: AggregateRead): URLSearchParams {
  const q = new URLSearchParams()
  if (read.filter && Object.keys(read.filter).length) {
    q.set("filter", JSON.stringify(read.filter))
  }
  q.set("aggregate", JSON.stringify(read.aggregate))
  return q
}

/** The path one aggregate read requests. */
export function aggregatePath(read: AggregateRead): string {
  return `${RECORDS_PATH}?${aggregateSearch(read)}`
}

export function fetchAggregate(
  read: AggregateRead,
  signal?: AbortSignal
): Promise<AggregateResult> {
  return request<AggregateResult>("GET", aggregatePath(read), undefined, {
    signal,
  })
}

/** One aggregate read as a query. The key names the observed kinds, so a
 * write to a record of one of them refreshes it (records.ts
 * recordWriteReaches), and the read itself, so two charts asking the same
 * share an answer. Under `useQuery` the previous answer stays on screen
 * while the next loads; `useQueries` starts each new read with none, so a
 * chart of several reads passes `heldAnswer` itself. */
export function aggregateQueryOptions(
  read: AggregateRead,
  kinds: readonly string[] = []
) {
  return queryOptions({
    queryKey: ["records", [...kinds], "aggregate", read],
    queryFn: ({ signal }) => fetchAggregate(read, signal),
    placeholderData: (prev) => prev,
  })
}

/** The newest answer the cache holds that a chart can draw while `read`
 * loads: a read of the same metric over the same range, in the same unit
 * and grouping, that asked for every reducer `read` asks for, whatever its
 * bucket, filter or source. Any other answer would draw under the new
 * read's axis caption, series and column what it does not hold. A new
 * range is a new chart, so nothing holds across one. */
export function heldAnswer(
  client: QueryClient,
  read: AggregateRead,
  kinds: readonly string[] = []
): AggregateResult | undefined {
  const { metric, range } = read.aggregate
  if (!metric || !range) return undefined
  let newest: { at: number; data: AggregateResult } | undefined
  // A key's kinds match as a prefix; the held answer must name them all.
  const named = JSON.stringify(kinds)
  for (const query of client
    .getQueryCache()
    .findAll({ queryKey: ["records", [...kinds], "aggregate"] })) {
    if (JSON.stringify(query.queryKey[1]) !== named) continue
    const held = query.queryKey[3] as AggregateRead | undefined
    const data = query.state.data as AggregateResult | undefined
    if (
      !data ||
      held?.aggregate.metric?.ref !== metric.ref ||
      held.aggregate.range?.gte !== range.gte ||
      held.aggregate.range?.lt !== range.lt ||
      !drawsAs(held.aggregate, read.aggregate)
    ) {
      continue
    }
    if (!newest || query.state.dataUpdatedAt > newest.at) {
      newest = { at: query.state.dataUpdatedAt, data }
    }
  }
  return newest?.data
}

/** Whether an answer to `held` carries what a chart of `next` draws: the
 * same unit and grouping, and every reducer `next` asks for. */
function drawsAs(held: Aggregate, next: Aggregate): boolean {
  const asked = new Set((held.reducers ?? []).map((r) => JSON.stringify(r)))
  return (
    held.unit === next.unit &&
    JSON.stringify(held.groupBy ?? []) === JSON.stringify(next.groupBy ?? []) &&
    (next.reducers ?? []).every((r) => asked.has(JSON.stringify(r)))
  )
}

/** The request as a person would type it at a shell, against the server
 * and token `substratectl` reads from its environment. */
export function aggregateCurl(read: AggregateRead): string {
  const quote = (s: string) => `'${s.replaceAll("'", `'"'"'`)}'`
  const lines = [
    `curl -G "$SUBSTRATE_SERVER${RECORDS_PATH}"`,
    `  -H "Authorization: Bearer $SUBSTRATE_TOKEN"`,
  ]
  if (read.filter && Object.keys(read.filter).length) {
    lines.push(
      `  --data-urlencode ${quote(`filter=${JSON.stringify(read.filter)}`)}`
    )
  }
  lines.push(
    `  --data-urlencode ${quote(`aggregate=${JSON.stringify(read.aggregate)}`)}`
  )
  return lines.join(" \\\n")
}
