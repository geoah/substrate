/** A record view's stats block: one tile per item, each one reducer of one
 * metric over the record's window or one property of the record, with the
 * band it falls in and, where the block names a baseline, its change
 * against the daily values of the days before the window. The metric
 * items are read in as few reads as `statsReads` makes; a tile never
 * fires a read of its own. */

import { useEffect, useMemo, type ReactNode } from "react"

import { StatTile } from "./stat-tile"
import {
  Failed,
  Loading,
  Part,
  Quiet,
  TileGrid,
  UNBOUND,
  useMetricReads,
  useMetricsReads,
  type BlockContext,
} from "./parts"
import { spanWords } from "./span-words"
import { DeclaredValue } from "@/components/property-sheet/property-value"
import { sheetRows } from "@/components/property-sheet/sheet-rows"
import type { AggregateResult, AggregateRow } from "@/lib/api/types"
import {
  DEFAULT_PRECISION,
  metricWords,
  readingText,
  reducerKey,
  refIdOf,
  sameRef,
  type MetricInfo,
} from "@/lib/observations"
import { numberOf, rowValue } from "@/lib/observations-answer"
import {
  amountOf,
  bandOf,
  baselineOf,
  baselineReducer,
  scaleBaseline,
  deltaWords,
  itemMetric,
  partialItem,
  readsByCode,
  rowsInDays,
  statsReads,
  valueAt,
  type Band,
  type Bound,
  type StatItem,
  type StatsBlock,
} from "@/lib/record-view"
import type { TimeRange } from "@/lib/observations-time"
import { humanizeName } from "@/lib/record-schema"
import { isTimeUnit } from "@/lib/units"

export function StatsBlockView({
  block,
  ctx,
  onEmpty,
}: {
  block: StatsBlock
  ctx: BlockContext
  /** Said under `empty: hide`: whether the block drew nothing. */
  onEmpty?: (hidden: boolean) => void
}) {
  const paths = block.items.map((item) =>
    item.property ? undefined : itemMetric(item, ctx.record)
  )
  const distinctPaths = [
    ...new Set(paths.filter((p): p is string => Boolean(p))),
  ]
  const metrics = useMetricReads(distinctPaths)
  const metricsIn = distinctPaths.every((p) => !metrics.get(p)?.pending)
  const infos = useMemo(() => {
    const out = new Map<string, MetricInfo>()
    for (const [path, read] of metrics) if (read.info) out.set(path, read.info)
    return out
  }, [metrics])
  const bound = ctx.bound
  const windowed = Boolean(bound?.window)
  // Nothing is read before the metric records are in: a tile's precision
  // and unit come from them, and the reads would be issued twice.
  const reads = useMemo(
    () =>
      bound && windowed && metricsIn
        ? statsReads(bound, block, ctx.timezone, infos, ctx.record)
        : {
            value: [],
            baseline: [],
            itemRead: block.items.map(() => -1),
            itemBaseline: block.items.map(() => -1),
            itemDays: block.items.map(() => undefined),
          },
    [bound, windowed, metricsIn, block, ctx.timezone, infos, ctx.record]
  )
  const observed = bound?.observed ?? []
  const values = useMetricsReads(reads.value, observed)
  const baselines = useMetricsReads(reads.baseline, observed)
  const metricItems =
    paths.some(Boolean) || block.items.some((i) => i.metricProperty)
  const pending =
    windowed &&
    metricItems &&
    (!metricsIn || values.pending || baselines.pending)
  // A failed baseline costs the tiles their comparison, never their values.
  const error = values.errors.find(Boolean)
  const baselineError = baselines.errors.find(Boolean)
  const dropped = new Set([
    ...values.dropped.flat(),
    ...baselines.dropped.flat(),
  ])
  // Under `empty: hide` a block none of whose items holds a value (a
  // vendor's scores on a record another source wrote) is left out once
  // its reads are in; a failed read is still said.
  const hide = block.empty === "hide"
  const settled = !pending
  const hidden =
    hide &&
    settled &&
    !error &&
    !block.items.some((item, i) =>
      item.property
        ? filled(valueAt(ctx.record, item.property))
        : Boolean(paths[i]) &&
          !dropped.has(paths[i]!) &&
          held(values.answers[reads.itemRead[i]], paths[i]!, item)
    )
  useEffect(() => {
    if (!hide || !settled) return
    onEmpty?.(hidden)
  }, [hide, settled, hidden, onEmpty])
  if (hidden) return null
  return (
    <Part title={block.label}>
      {metricItems && !windowed && <Quiet>{UNBOUND}</Quiet>}
      {pending ? (
        <Loading />
      ) : error ? (
        <Failed
          error={error}
          onRetry={() => {
            values.refetch()
            baselines.refetch()
          }}
        />
      ) : (
        <TileGrid label={block.label ?? "Figures"}>
          {block.items.map((item, i) => (
            <div key={i} role="listitem" className="contents">
              {item.property ? (
                <PropertyTile item={item} block={block} ctx={ctx} />
              ) : (
                <MetricTile
                  item={item}
                  block={block}
                  path={paths[i]}
                  info={infos.get(paths[i] ?? "")}
                  answer={values.answers[reads.itemRead[i]]}
                  baseline={baselines.answers[reads.itemBaseline[i]]}
                  baselineDays={reads.itemDays[i]}
                  dropped={dropped.has(paths[i] ?? "")}
                  windowed={windowed}
                  bound={bound}
                  ctx={ctx}
                />
              )}
            </div>
          ))}
        </TileGrid>
      )}
      {!pending && !error && baselineError && (
        <Failed error={baselineError} onRetry={baselines.refetch} />
      )}
    </Part>
  )
}

/** Whether a record's property holds a value a tile draws. */
function filled(value: unknown): boolean {
  return value !== undefined && value !== null && value !== ""
}

/** Whether an answer holds a value for a metric item. */
function held(
  answer: AggregateResult | undefined,
  path: string,
  item: StatItem
): boolean {
  const code = readsByCode(item) ? item.code : undefined
  const v = rowValue(itemRows(answer, path, code)[0], item.key)
  return Boolean(v) && v!.value !== null
}

/** The rows of an answer that hold one item: its metric's, and for an
 * item read grouped by code (`readsByCode`) its code's. */
function itemRows(
  answer: AggregateResult | undefined,
  path: string,
  code?: string
): AggregateRow[] {
  return (answer?.rows ?? []).filter(
    (row) =>
      sameRef(row.group?.metric, path) &&
      (code === undefined || row.group?.code === code)
  )
}

/** The bands that hold for an item, `metric` naming its metric. */
function bandsFor(bands: readonly Band[], metric?: string): Band[] {
  return bands.filter((b) => !b.metric || sameRef(b.metric, metric))
}

function MetricTile({
  item,
  block,
  path,
  info,
  answer,
  baseline,
  baselineDays,
  dropped,
  windowed,
  bound,
  ctx,
}: {
  item: StatItem
  block: StatsBlock
  path?: string
  info?: MetricInfo
  answer?: AggregateResult
  baseline?: AggregateResult
  /** The days of `baseline` the item compares with. */
  baselineDays?: TimeRange
  dropped: boolean
  windowed: boolean
  bound?: Bound
  ctx: BlockContext
}) {
  const label =
    item.label ?? info?.name ?? (path ? metricWords(refIdOf(path)) : "Metric")
  const common = {
    label,
    show: item.show,
    min: item.min,
    max: item.max,
    bands: bandsFor(block.bands, path),
  }
  if (!path) {
    return (
      <StatTile
        {...common}
        words={`This ${ctx.noun} names no metric record in ${item.metricProperty ?? "its metric property"}.`}
      />
    )
  }
  if (dropped) {
    return <StatTile {...common} words={`No record names the metric ${path}`} />
  }
  if (!windowed) return <StatTile {...common} words="Nothing to read" />
  const code = readsByCode(item) ? item.code : undefined
  const row = itemRows(answer, path, code)[0]
  const v = rowValue(row, item.key)
  if (!v || v.value === null) {
    const span = spanWords(bound, ctx.noun)
    const soFar = span.soFar ? ` ${span.soFar}` : ""
    return (
      <StatTile {...common} words={`Not measured ${span.during}${soFar}`} />
    )
  }
  const precision =
    item.precision ?? info?.displayPrecision ?? DEFAULT_PRECISION
  const unit = v.unit ?? answer?.units?.[path] ?? info?.unit
  const numeric = numberOf(v.value)
  // A time reads in words with its unit spelled in (`4h 42m`); any other
  // value is digits beside its unit.
  const timed = Boolean(unit && isTimeUnit(unit))
  const text = readingText(
    v.value,
    timed ? unit : undefined,
    precision,
    v.comparator
  )
  const band = numeric === null ? undefined : bandOf(numeric, block.bands, path)
  // A record's own window still running (today's cycle) holds part of what
  // it will: a value that grows over it is said so far, and compared with
  // nothing. Last night's summary in it is whole and compared.
  const partial = bound ? partialItem(bound, info) : false
  const daily =
    block.baseline > 0 && !partial
      ? baselineOf(
          rowsInDays(itemRows(baseline, path, code), baselineDays),
          reducerKey(baselineReducer(item.reducer, info))
        )
      : undefined
  const base = daily
    ? scaleBaseline(daily, item.reducer, bound?.window, bound?.lastWindow)
    : undefined
  const delta =
    base && numeric !== null
      ? deltaWords(
          numeric,
          base.avg,
          item.better,
          unit,
          precision,
          base.span > 1 ? undefined : base.days
        )
      : undefined
  return (
    <StatTile
      {...common}
      value={text}
      numeric={numeric}
      unit={timed ? undefined : unit}
      band={band ? { label: band.label ?? "", tone: band.tone } : undefined}
      delta={delta}
      baseline={base}
      qualifier={partial ? "so far" : undefined}
      note={
        base
          ? base.span > 1
            ? `against ${base.span} days at the daily average of the previous ${base.days}`
            : `against the average day of the previous ${base.days}`
          : undefined
      }
    />
  )
}

/** A property of the record as a tile: a top-level one as the sheet draws
 * it, a value inside a keyed map as text. */
function PropertyTile({
  item,
  block,
  ctx,
}: {
  item: StatItem
  block: StatsBlock
  ctx: BlockContext
}) {
  const path = item.property!
  const segments = path.split(".")
  const head = segments[0]
  const nested = segments.length > 1
  const rows = useMemo(
    () => sheetRows(ctx.record, ctx.kind, true).all,
    [ctx.record, ctx.kind]
  )
  const row = rows.find((r) => r.name === head)
  const label =
    item.label ??
    (nested ? undefined : row?.spec.label) ??
    humanizeName(segments[segments.length - 1])
  const value = valueAt(ctx.record, path)
  const precision = item.precision ?? DEFAULT_PRECISION
  const amount = amountOf(value, precision)
  const numeric = amount?.amount ?? null
  const band = numeric === null ? undefined : bandOf(numeric, block.bands)
  const common = {
    label,
    show: item.show,
    min: item.min,
    max: item.max,
    bands: bandsFor(block.bands),
    numeric,
    band: band ? { label: band.label ?? "", tone: band.tone } : undefined,
  }
  if (value === undefined || value === null || value === "") {
    return <StatTile {...common} words={`Not set on this ${ctx.noun}`} />
  }
  let figure: ReactNode
  let text: string | undefined
  if (nested || !row || item.show !== "number") {
    text = amount?.text ?? (typeof value === "string" ? value : undefined)
    if (text === undefined) text = JSON.stringify(value)
  } else {
    figure = (
      <span className="text-[22px] font-medium tabular-nums">
        <DeclaredValue spec={row.spec} value={row.value} />
      </span>
    )
  }
  return <StatTile {...common} value={text} figure={figure} />
}
