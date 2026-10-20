/** The pieces every block of a record view is drawn with: what a block
 * reads from, its heading, the quiet lines it says instead of a value, the
 * metric records and batched aggregate reads its blocks share, and bars. */

import type { ReactNode } from "react"
import {
  useQueries,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query"
import { RefreshCwIcon, TriangleAlertIcon } from "lucide-react"

import { IndexPending } from "@/components/observations/index-pending"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  aggregateQueryOptions,
  fetchAggregate,
  type AggregateRead,
} from "@/lib/api/observations"
import { recordQueryOptions } from "@/lib/api/records"
import {
  ApiError,
  type AggregateResult,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { metricInfoOf, type MetricInfo } from "@/lib/observations"
import type { TimeRange } from "@/lib/observations-time"
import { indexPending } from "@/lib/observations-reads"
import { splitRecordPath } from "@/lib/record-path"
import type { Bound } from "@/lib/record-view"

/** Why a block that reads a window has nothing to read: its view binds
 * none. A view that binds one the record does not hold leaves these blocks
 * out (`RecordViewSection`). */
export const UNBOUND = "Nothing to read here: the view binds no window."

/** What every block of one view on one record reads from. */
export interface BlockContext {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: readonly KindInfo[]
  /** The record in words, lower case: its kind's display name. */
  noun: string
  /** The window the view binds on the record, where it binds one. */
  bound?: Bound
  /** The part of the window the reader zoomed into, else the whole. */
  window?: TimeRange
  zoomed: boolean
  onZoom: (range: TimeRange) => void
  onReset: () => void
  timezone: string
}

/** A section the console cannot draw, said where it would be. */
export function Notice({ children }: { children: ReactNode }) {
  return (
    <p
      role="note"
      className="mt-3 flex items-start gap-1.5 text-[12.5px] text-muted-foreground"
    >
      <TriangleAlertIcon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </p>
  )
}

/** A block's own heading and what it holds. */
export function Part({
  title,
  children,
}: {
  title?: string
  children: ReactNode
}) {
  return (
    <div className="mt-3 flex flex-col gap-1.5" role="group" aria-label={title}>
      {title && <h3 className="text-[13px] font-medium">{title}</h3>}
      {children}
    </div>
  )
}

export function Loading() {
  return <Skeleton className="h-24 w-full max-w-[520px]" />
}

export function Failed({
  error,
  onRetry,
}: {
  error: Error
  onRetry?: () => void
}) {
  if (indexPending(error))
    return <IndexPending error={error} onRetry={onRetry} />
  return (
    <div
      data-slot="read-failed"
      className="flex flex-wrap items-center gap-2 text-[12.5px]"
    >
      <p className="text-destructive">This read failed: {error.message}</p>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RefreshCwIcon />
          Read again
        </Button>
      )}
    </div>
  )
}

export function Quiet({ children }: { children: ReactNode }) {
  return <p className="text-[12.5px] text-muted-foreground">{children}</p>
}

/** The metric records at some paths, read for their names, units, forms
 * and codes, each with whether its read is still out. A path no record
 * holds answers no `info` once read. */
// eslint-disable-next-line react-refresh/only-export-components -- the hooks every block shares
export function useMetricReads(
  paths: readonly string[]
): ReadonlyMap<string, { info?: MetricInfo; pending: boolean }> {
  const distinct = [...new Set(paths)]
  const reads = useQueries({
    queries: distinct.map((path) => {
      const parts = splitRecordPath(path)
      const [authority = "", pkg = "", name = ""] = parts?.kind.split("/") ?? []
      return {
        ...recordQueryOptions(authority, pkg, name, parts?.id ?? ""),
        retry: false,
      }
    }),
  })
  const out = new Map<string, { info?: MetricInfo; pending: boolean }>()
  distinct.forEach((path, i) => {
    const read = reads[i]
    out.set(path, {
      info: read.data?.kind ? metricInfoOf(read.data) : undefined,
      pending: read.isPending,
    })
  })
  return out
}

/** The entry of a several-metric read a refusal names as no metric: the
 * server says `aggregate.metrics[i].ref` where the i-th metric names no
 * record (or a record that is no metric). Any other refusal of an entry (a
 * reducer, unit or statistic the metric does not admit) is the view's to
 * fix, so the read fails with the server's words. */
const ENTRY_PROBLEM = /^aggregate\.metrics\[(\d+)\]\.ref:/

function refusedEntry(error: unknown): number | undefined {
  if (!(error instanceof ApiError) || error.status !== 422) return undefined
  for (const p of error.problems) {
    const m = ENTRY_PROBLEM.exec(p)
    if (m) return Number(m[1])
  }
  return undefined
}

/** What a several-metric read answers, and the metrics it dropped. */
type Dropping = { answer?: AggregateResult; dropped: string[] }

/** One several-metric read, asked again without each entry the server
 * refuses, until it answers or no entry is left. */
async function readDropping(
  read: AggregateRead,
  signal: AbortSignal
): Promise<Dropping> {
  const dropped: string[] = []
  let metrics = read.aggregate.metrics ?? []
  for (;;) {
    if (read.aggregate.metrics && !metrics.length) return { dropped }
    try {
      const answer = await fetchAggregate(
        { ...read, aggregate: { ...read.aggregate, metrics } },
        signal
      )
      return { answer, dropped }
    } catch (error) {
      const at = refusedEntry(error)
      const ref = at === undefined ? undefined : metrics[at]?.ref
      if (ref === undefined) throw error
      dropped.push(ref)
      metrics = metrics.filter((_, i) => i !== at)
    }
  }
}

/** A read with its range, bucket and budget left out: two reads alike
 * here read the same metrics the same way, over another stretch of time. */
function readShape(read: AggregateRead): string {
  const { range, bucket, budget, ...rest } = read.aggregate
  void range
  void bucket
  void budget
  return JSON.stringify([read.filter ?? null, rest])
}

/** The newest answer the cache holds of a read like `read` over another
 * stretch of time (`readShape`): what a chart draws while a zoom loads. */
function heldDropping(
  client: QueryClient,
  read: AggregateRead,
  kinds: readonly string[]
): Dropping | undefined {
  const shape = readShape(read)
  // A key's kinds match as a prefix; the held answer must name them all.
  const named = JSON.stringify(kinds)
  let newest: { at: number; data: Dropping } | undefined
  for (const query of client
    .getQueryCache()
    .findAll({ queryKey: ["records", [...kinds], "aggregate"] })) {
    const key = query.queryKey
    if (key[4] !== "dropping" || JSON.stringify(key[1]) !== named) continue
    const data = query.state.data as Dropping | undefined
    const held = key[3] as AggregateRead | undefined
    if (!data?.answer || !held || readShape(held) !== shape) continue
    if (!newest || query.state.dataUpdatedAt > newest.at) {
      newest = { at: query.state.dataUpdatedAt, data }
    }
  }
  return newest?.data
}

/** Several-metric aggregate reads, run together. Where the server refuses a
 * read for one of its entries (a metric no record holds), that entry is
 * dropped and the read asked again without it, so one missing metric
 * never blanks a block; `dropped[i]` names the metrics read i lost. A read
 * whose every entry is dropped answers undefined. With `hold`, a read
 * that has not answered yet draws the newest answer of the same read over
 * another stretch of time, as a chart keeps its line while a zoom loads;
 * `useQueries` starts each new read with none of its own. */
// eslint-disable-next-line react-refresh/only-export-components -- the hooks every block shares
export function useMetricsReads(
  reads: readonly AggregateRead[],
  observed: readonly string[],
  { hold = false }: { hold?: boolean } = {}
): {
  answers: (AggregateResult | undefined)[]
  pending: boolean
  errors: (Error | null)[]
  dropped: string[][]
  refetch: () => void
} {
  const client = useQueryClient()
  const results = useQueries({
    queries: reads.map((read) => {
      const base = aggregateQueryOptions(read, observed)
      return {
        // The key keeps the aggregate read's prefix, so a write to an
        // observed kind refreshes it, and a suffix of its own, since what
        // it holds is no bare answer.
        queryKey: [...base.queryKey, "dropping"],
        queryFn: ({ signal }: { signal: AbortSignal }) =>
          readDropping(read, signal),
        placeholderData: (prev: Dropping | undefined) =>
          prev ?? (hold ? heldDropping(client, read, observed) : undefined),
      }
    }),
  })
  return {
    answers: results.map((r) => r.data?.answer),
    pending: results.some((r) => r.isPending),
    errors: results.map((r) => r.error ?? null),
    dropped: results.map((r) => r.data?.dropped ?? []),
    refetch: () => {
      for (const r of results) void r.refetch()
    },
  }
}

/** A bar per item, its length its share of the largest. */
export function Bars({
  label,
  items,
}: {
  label: string
  items: {
    key: string
    name: ReactNode
    amount: number | null
    text: string
    /** The bar's colour; the first series colour where absent. */
    color?: string
  }[]
}) {
  const most = Math.max(0, ...items.map((i) => i.amount ?? 0))
  return (
    <dl
      aria-label={label}
      className="grid max-w-[520px] grid-cols-[auto_1fr_auto] items-center gap-x-3 gap-y-1 text-[13px]"
    >
      {items.map((item) => (
        <div key={item.key} className="contents">
          <dt className="text-muted-foreground">{item.name}</dt>
          <div aria-hidden className="h-2 rounded-full bg-hover">
            <div
              className="h-2 rounded-full"
              style={{
                width: most ? `${((item.amount ?? 0) / most) * 100}%` : 0,
                background: item.color ?? "var(--series-1)",
              }}
            />
          </div>
          <dd className="text-right tabular-nums">{item.text}</dd>
        </div>
      ))}
    </dl>
  )
}
