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
import type {
  Aggregate,
  AggregateMetricRead,
  AggregateResult,
  AggregateRow,
  RecordFilter,
} from "./types"

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

/** Each metric's own read within `read`: `read` itself where it names one
 * metric (or none), and where it names several (`aggregate.metrics`), per
 * entry the read that metric alone makes. That read takes the entry's
 * unit, its reducers and `from` (else the read's), its statistic as
 * `filter.properties.statistic`, and the read's filter, range, bucket,
 * grouping and sources. It carries no budget, since a several-metric
 * read's budget counts every metric. */
export function metricReads(read: AggregateRead): AggregateRead[] {
  const { metrics } = read.aggregate
  if (!metrics) return [read]
  const shared: Aggregate = { ...read.aggregate }
  delete shared.metrics
  delete shared.budget
  return metrics.map((entry) => ({
    filter: entry.statistic
      ? {
          ...read.filter,
          properties: {
            ...read.filter?.properties,
            statistic: { eq: entry.statistic },
          },
        }
      : read.filter,
    aggregate: entryAggregate(shared, entry),
  }))
}

function entryAggregate(shared: Aggregate, entry: AggregateMetricRead) {
  const out: Aggregate = { ...shared, metric: { ref: entry.ref } }
  if (entry.unit) out.unit = entry.unit
  if (entry.reducers) out.reducers = entry.reducers
  if (entry.from) out.from = entry.from
  return out
}

/** The part of `result`, the answer to `read`, that one metric's own read
 * answers: `result` itself where it answers one metric, and where it
 * answers several (`units`), the rows naming `ref` in `group.metric` under
 * its unit. Those rows leave `group.metric` out unless `read` groups by
 * metric, as the metric's own read would answer them. Undefined where
 * `units` does not name `ref`: an answer held for other metrics. */
export function metricAnswer(
  result: AggregateResult,
  read: AggregateRead,
  ref: string
): AggregateResult | undefined {
  if (!result.units) return result
  if (!(ref in result.units)) return undefined
  const keep = read.aggregate.groupBy?.includes("metric") ?? false
  const out: AggregateResult = {
    ...result,
    metric: ref,
    unit: result.units[ref] || undefined,
    rows: result.rows.flatMap((row) => {
      if (row.group?.metric !== ref) return []
      if (keep) return [row]
      const group = { ...row.group }
      delete group.metric
      const copy: AggregateRow = { ...row, group }
      if (!Object.keys(group).length) delete copy.group
      return [copy]
    }),
  }
  delete out.units
  if (!out.unit) delete out.unit
  return out
}

/** The newest answer the cache holds that a chart can draw while `read`
 * loads: a read of the same metric over the same range, in the same unit,
 * grouping and summary statistic, that asked for every reducer `read` asks
 * for, whatever its bucket, other filters or source, alone or as one
 * metric of a several-metric read. Any other answer would draw under the
 * new read's axis caption, series and column what it does not hold: a
 * cycle's average is no stand-in for its highest. A new range is a new
 * chart, so nothing holds across one. For a several-metric read, each
 * metric's held rows together, under `units` naming the metrics they
 * hold. */
export function heldAnswer(
  client: QueryClient,
  read: AggregateRead,
  kinds: readonly string[] = []
): AggregateResult | undefined {
  if (read.aggregate.metrics) return heldTogether(client, read, kinds)
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
    if (!held || !data) continue
    const ownRead = metricReads(held).find(
      (r) => r.aggregate.metric?.ref === metric.ref
    )
    const own = ownRead?.aggregate
    if (
      !own ||
      own.range?.gte !== range.gte ||
      own.range?.lt !== range.lt ||
      !drawsAs(own, read.aggregate) ||
      statisticOf(ownRead.filter) !== statisticOf(read.filter)
    ) {
      continue
    }
    const part = metricAnswer(data, held, metric.ref)
    if (!part) continue
    if (!newest || query.state.dataUpdatedAt > newest.at) {
      newest = { at: query.state.dataUpdatedAt, data: part }
    }
  }
  return newest?.data
}

/** A several-metric read's held answer: each metric's own (`heldAnswer`),
 * its rows naming it in `group.metric`, `units` naming the metrics held.
 * Undefined where none is held. */
function heldTogether(
  client: QueryClient,
  read: AggregateRead,
  kinds: readonly string[]
): AggregateResult | undefined {
  const parts = metricReads(read).flatMap((one) => {
    const ref = one.aggregate.metric?.ref
    const held = ref ? heldAnswer(client, one, kinds) : undefined
    return ref && held ? [{ ref, held }] : []
  })
  if (!parts.length) return undefined
  return {
    units: Object.fromEntries(parts.map((p) => [p.ref, p.held.unit ?? ""])),
    exact: parts.every((p) => p.held.exact),
    rows: parts.flatMap(({ ref, held }) =>
      held.rows.map((row) => ({ ...row, group: { ...row.group, metric: ref } }))
    ),
    head: parts[0].held.head,
  }
}

/** The summary statistic a read's filter narrows to, as one string. */
function statisticOf(filter: RecordFilter | undefined): string {
  return JSON.stringify(filter?.properties?.statistic ?? null)
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
