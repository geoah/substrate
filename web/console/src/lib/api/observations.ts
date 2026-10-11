/** The aggregate read: `GET /api/v1/records?filter=…&aggregate=…`, the
 * records route's mode that reduces observed values into buckets
 * (docs/plans/observations/implementation/README.md pins the wire). One
 * answer shape, `AggregateResult`, for every request.
 *
 * The aggregate travels as strict JSON in its own query parameter, beside
 * the ordinary `filter`, and is refused beside `q`, `watch`, `count`,
 * `orderBy`, `first`, `after`, `offset` and `expand`, so none of them is
 * ever sent here. */

import { queryOptions } from "@tanstack/react-query"

import type { AggregateRead } from "@/lib/observations"

import { request } from "./http"
import { RECORDS_PATH } from "./records"
import type { AggregateResult, Downsample } from "./types"

/** The query string of one aggregate read, `filter` first. */
export function aggregateSearch(
  read: AggregateRead,
  downsample?: Downsample
): URLSearchParams {
  const q = new URLSearchParams()
  if (read.filter && Object.keys(read.filter).length) {
    q.set("filter", JSON.stringify(read.filter))
  }
  q.set("aggregate", JSON.stringify(read.aggregate))
  if (downsample) q.set("downsample", JSON.stringify(downsample))
  return q
}

/** The path one aggregate read requests. */
export function aggregatePath(
  read: AggregateRead,
  downsample?: Downsample
): string {
  return `${RECORDS_PATH}?${aggregateSearch(read, downsample)}`
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
 * share an answer. The previous answer stays on screen while the next loads:
 * a chart holds its frame rather than flashing empty. */
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
