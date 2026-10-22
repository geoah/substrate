/** A record view's trend block: one strip per item, a metric bucketed by
 * the calendar day, week or month over the buckets ending with the one that
 * holds the record, that bucket tinted, the trailing average as a dashed
 * line where the block asks for one, and a caption saying the record's
 * bucket against that average. Every item is in the block's one read. A
 * trend does not follow the view's zoom: its buckets are the record's
 * neighbours, not parts of its window. */

import { useEffect, useMemo } from "react"

import {
  Failed,
  Loading,
  Part,
  Quiet,
  UNBOUND,
  useMetricReads,
  useMetricsReads,
  type BlockContext,
} from "./parts"
import {
  TimelineChart,
  type ChartSeries,
} from "@/components/observations/timeline-chart"
import type {
  AggregateResult,
  AggregateRow,
  AggregateValue,
} from "@/lib/api/types"
import {
  DEFAULT_PRECISION,
  metricWords,
  readingText,
  reducerKey,
  reducerName,
  refIdOf,
  sameRef,
  shapeOf,
  type MetricInfo,
} from "@/lib/observations"
import {
  numberOf,
  rowValue,
  seriesOn,
  type SeriesPoint,
} from "@/lib/observations-answer"
import { dayWords, windowWords, type TimeRange } from "@/lib/observations-time"
import { markOf, type ChartMark } from "@/lib/overlay"
import {
  calendarGrid,
  deltaWords,
  itemMetric,
  toneText,
  toneSoft,
  trendAnchor,
  trendRange,
  trendRead,
  type Bound,
  type TrendBlock,
  type TrendItem,
} from "@/lib/record-view"
import { useUnits } from "@/hooks/use-units"

const STRIP_HEIGHT = 120

const BUCKET_NOUN: Record<TrendBlock["bucket"], string> = {
  day: "day",
  week: "week",
  month: "month",
}

export function TrendBlockView({
  block,
  ctx,
  onEmpty,
}: {
  block: TrendBlock
  ctx: BlockContext
  /** Said under `empty: hide`: whether the block drew nothing. */
  onEmpty?: (hidden: boolean) => void
}) {
  const paths = block.items.map((item) => itemMetric(item, ctx.record))
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
  // The anchors move with a metric's `bucketPlace`, so nothing is read
  // before the metric records say where each one places a value.
  const range = useMemo(
    () =>
      bound && metricsIn
        ? trendRange(bound, block, infos, ctx.record)
        : undefined,
    [bound, metricsIn, block, infos, ctx.record]
  )
  const read = useMemo(
    () =>
      bound && metricsIn
        ? trendRead(bound, block, ctx.timezone, infos, ctx.record)
        : undefined,
    [bound, metricsIn, block, ctx.timezone, infos, ctx.record]
  )
  const reads = useMemo(() => (read ? [read] : []), [read])
  const answer = useMetricsReads(reads, bound?.observed ?? [])
  const error = answer.errors.find(Boolean)
  // Under `empty: hide` a block none of whose strips holds a value is left
  // out once its read is in; a failed read is still said.
  const hide = block.empty === "hide"
  const settled = !bound?.window || (metricsIn && !answer.pending && !!range)
  const hidden =
    hide &&
    settled &&
    !error &&
    !block.items.some((item, i) =>
      stripRows(answer.answers[0], paths[i], item).some((row) => {
        const v = rowValue(row, item.key)
        return Boolean(v) && v!.value !== null
      })
    )
  useEffect(() => {
    if (!hide || !settled) return
    onEmpty?.(hidden)
  }, [hide, settled, hidden, onEmpty])
  if (hidden) return null
  if (!bound?.window) {
    return (
      <Part title={block.label}>
        <Quiet>{UNBOUND}</Quiet>
      </Part>
    )
  }
  return (
    <Part title={block.label}>
      {!metricsIn || answer.pending || !range ? (
        <Loading />
      ) : error ? (
        <Failed error={error} onRetry={answer.refetch} />
      ) : (
        block.items.map((item, i) => (
          <Strip
            key={i}
            item={item}
            block={block}
            path={paths[i]}
            info={infos.get(paths[i] ?? "")}
            answer={answer.answers[0]}
            dropped={answer.dropped.flat().includes(paths[i] ?? "")}
            range={range}
            bound={bound}
            ctx={ctx}
          />
        ))
      )}
    </Part>
  )
}

/** The rows of an answer that hold one strip: its metric's, and for a
 * read grouped by code (a coded item that is no time in a state) its
 * code's. */
function stripRows(
  answer: AggregateResult | undefined,
  path: string | undefined,
  item: TrendItem
): AggregateRow[] {
  const byCode = Boolean(
    item.code && !reducerKey(item.reducer).startsWith("timeInState")
  )
  return (answer?.rows ?? []).filter(
    (row) =>
      path &&
      sameRef(row.group?.metric, path) &&
      (!byCode || row.group?.code === item.code)
  )
}

/** A row's trailing average of one reducer: the rolling value whose `of`
 * is that reducer. */
function rollingValue(
  row: AggregateRow | undefined,
  key: string
): AggregateValue | undefined {
  return (row?.values ?? []).find(
    (v) => v.rolling !== undefined && reducerKey(v.of ?? "") === key
  )
}

/** How an item's strip is drawn: as the view says, else by the metric's
 * form and the reducer. A category is drawn as bars of its one code. */
function markFor(item: TrendItem, info?: MetricInfo): ChartMark {
  if (item.show === "bars") return "bars"
  if (item.show === "line") return "line"
  const mark = markOf(shapeOf(info), reducerName(item.reducer))
  return mark === "stack" ? "bars" : mark
}

function Strip({
  item,
  block,
  path,
  info,
  answer,
  dropped,
  range,
  bound,
  ctx,
}: {
  item: TrendItem
  block: TrendBlock
  path?: string
  info?: MetricInfo
  answer?: AggregateResult
  dropped: boolean
  range: TimeRange
  bound: Bound
  ctx: BlockContext
}) {
  const units = useUnits()
  const name =
    item.label ?? info?.name ?? (path ? metricWords(refIdOf(path)) : "Metric")
  const buckets = useMemo(
    () => calendarGrid(range, block.bucket),
    [range, block.bucket]
  )
  // A read grouped by code answers a row per code: the item's is the one
  // holding its code.
  const rows = useMemo(
    () => stripRows(answer, path, item),
    [answer, path, item]
  )
  if (!path) {
    return (
      <Quiet>
        This {ctx.noun} names no metric record in{" "}
        {item.metricProperty ?? "its metric property"}.
      </Quiet>
    )
  }
  if (dropped) {
    return (
      <Quiet>
        No record names the metric {path}, so there is nothing to read.
      </Quiet>
    )
  }
  const [series] = seriesOn(buckets, rows, item.key)
  const points: SeriesPoint[] =
    series?.points ??
    buckets.map((_, i) => ({ bucket: i, value: null, low: null, high: null }))
  const rolling: SeriesPoint[] = points.map((p) => ({
    bucket: p.bucket,
    row: p.row,
    value: numberOf(rollingValue(p.row, item.key)?.value),
    low: null,
    high: null,
  }))
  const unit =
    rows.flatMap((r) => {
      const v = rowValue(r, item.key)
      return v && v.value !== null ? [v.unit ?? ""] : []
    })[0] ??
    answer?.units?.[path] ??
    info?.unit ??
    ""
  const precision = info?.displayPrecision ?? DEFAULT_PRECISION
  const anchor = trendAnchor(bound, info?.bucketPlace)
  const highlight =
    anchor === undefined
      ? -1
      : buckets.findIndex((b) => anchor >= b.start && anchor < b.end)
  const chartSeries: ChartSeries[] = [
    {
      key: item.key,
      color: "var(--series-1)",
      points,
      mark: markFor(item, info),
    },
  ]
  if (block.rolling > 0 && rolling.some((p) => p.value !== null)) {
    chartSeries.push({
      key: `${item.key}:rolling`,
      color: "var(--faint)",
      points: rolling,
      mark: "line",
      dash: true,
    })
  }
  const bands = block.bands
    .filter((b) => !b.metric || sameRef(b.metric, path))
    .map((b) => ({
      low: b.low,
      high: b.high,
      color: toneSoft(b.tone),
      label: b.label,
    }))
  const at = highlight >= 0 ? points[highlight] : undefined
  const held = at?.row ? rowValue(at.row, item.key) : undefined
  const average = at ? rolling[highlight].value : null
  const delta =
    at && at.value !== null && average !== null
      ? deltaWords(units, at.value, average, item.better, unit, precision)
      : undefined
  return (
    <div className="flex flex-col gap-1" role="group" aria-label={name}>
      <h4 className="text-[12.5px] text-muted-foreground">{name}</h4>
      <TimelineChart
        buckets={buckets}
        series={chartSeries}
        domain={range}
        size={block.bucket}
        label={`${name}, by the ${BUCKET_NOUN[block.bucket]}`}
        units={{ left: units.symbol(unit) || unit }}
        bands={bands}
        highlight={highlight >= 0 ? highlight : undefined}
        height={STRIP_HEIGHT}
        onZoom={() => {}}
        tooltip={(b) => {
          const row = points[b]?.row
          const v = rowValue(row, item.key)
          const r = rollingValue(row, item.key)
          return (
            <>
              <p className="font-medium">
                {windowWords(buckets[b].start, buckets[b].end)}
              </p>
              {row && v ? (
                <dl className="mt-1 grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
                  <dt className="text-faint">{name}</dt>
                  <dd className="text-right whitespace-nowrap tabular-nums">
                    {readingText(
                      units,
                      v.value,
                      v.unit,
                      precision,
                      v.comparator
                    )}
                  </dd>
                  {r && r.value !== null && (
                    <>
                      <dt className="text-faint">
                        Average of the {block.rolling} before
                      </dt>
                      <dd className="text-right whitespace-nowrap tabular-nums">
                        {readingText(units, r.value, r.unit, precision)}
                      </dd>
                    </>
                  )}
                </dl>
              ) : (
                <p className="mt-1 text-muted-foreground">Nothing measured.</p>
              )}
            </>
          )
        }}
      />
      <p
        className="text-[12.5px] text-muted-foreground"
        data-slot="trend-caption"
      >
        {at && held && held.value !== null ? (
          <>
            {readingText(
              units,
              held.value,
              held.unit,
              precision,
              held.comparator
            )}{" "}
            on {dayWords(buckets[highlight].start)}
            {delta && (
              <>
                ,{" "}
                <span style={{ color: toneText(delta.tone) }}>
                  {delta.text}
                </span>
              </>
            )}
          </>
        ) : (
          `Nothing measured in this ${BUCKET_NOUN[block.bucket]}`
        )}
      </p>
    </div>
  )
}
