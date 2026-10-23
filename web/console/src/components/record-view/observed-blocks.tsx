/** A record view's chart and spans blocks: what was measured during the
 * record, read over the window the view binds on it. A chart draws each
 * metric it names as a line with its band, which a click or a drag zooms
 * into, or with a `whole` bucket as one bar per reducer (the time in each
 * heart rate zone a view lists, for one). A spans block draws a coded
 * metric's spans as rows in the view's order of codes where the source
 * recorded them in sequence, and its time in each code. */

import { useEffect, useMemo, useState } from "react"
import { useQueries, useQuery } from "@tanstack/react-query"

import {
  Bars,
  Failed,
  Loading,
  Notice,
  Part,
  Quiet,
  UNBOUND,
  useMetricReads,
  useMetricsReads,
  type BlockContext,
} from "./parts"
import { spanWords } from "./span-words"
import { TimelineChart } from "@/components/observations/timeline-chart"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import { useWidth } from "@/hooks/use-width"
import { aggregateQueryOptions } from "@/lib/api/observations"
import { fetchRecordsPage, recordsQueryOptions } from "@/lib/api/records"
import {
  ApiError,
  type AggregateResult,
  type AggregateRow,
} from "@/lib/api/types"
import {
  codeWords,
  distinct,
  metricWords,
  readingText,
  reducerChoiceWords,
  refIdOf,
  sameRef,
  shapeOf,
  statisticLabel,
  type MetricInfo,
} from "@/lib/observations"
import {
  bucketsOf,
  namedSources,
  numberOf,
  rowValue,
  seriesOn,
} from "@/lib/observations-answer"
import {
  clockWords,
  durationWords,
  windowWords,
  type TimeRange,
} from "@/lib/observations-time"
import { codeColor, markOf } from "@/lib/overlay"
import {
  chartBucketSize,
  chartLineReducer,
  chartMeasured,
  chartMetricRows,
  chartReads,
  CHART_SUMMARIES_PAGE,
  CHART_SUMMARIES_PAGES,
  chartSummaries,
  chartSummariesParams,
  fetchChartSummaries,
  sequenceOf,
  spanRows,
  spansListParams,
  spansNarrowed,
  spansSource,
  spansSourceCount,
  spansOf,
  spansTotalsRead,
  summaryBuckets,
  summarySeries,
  summaryValueIn,
  toneSoft,
  type Bound,
  type ChartBlock,
  type ChartSummary,
  type SummarySeries,
  type ChartReducer,
  type Span,
  type SpansBlock,
} from "@/lib/record-view"
import { AXIS_LEFT, timeAxis } from "@/lib/time-axis"
import { listWords } from "@/lib/tools"
import { useUnits } from "@/hooks/use-units"

/** Whether a one-metric read found nothing to say: a repository with no
 * such metric record, which the server refuses by its `metric.ref`. Any
 * other refusal is the view's to fix, and shows. */
function noMetric(error: Error | null): boolean {
  return (
    error instanceof ApiError &&
    error.status === 422 &&
    error.problems.some((p) => p.startsWith("aggregate.metric.ref:"))
  )
}

/** Why a spans block cannot draw a metric: it draws a coded amount, whose
 * values each hold a code over a span. Undefined where it can. */
function notSpans(info: MetricInfo, path: string): string | undefined {
  const name = info.name ?? metricWords(refIdOf(path))
  if (!info.codeSet?.length) {
    return `This spans block draws a coded amount metric, and ${name} lists no codes.`
  }
  if (info.form && info.form !== "amount") {
    return `This spans block draws a coded amount metric, and ${name} is a ${info.form}.`
  }
  return undefined
}

export function ChartBlockView({
  block,
  ctx,
  onEmpty,
}: {
  block: ChartBlock
  ctx: BlockContext
  /** Said under `empty: hide`: whether the chart drew nothing. */
  onEmpty?: (hidden: boolean) => void
}) {
  const { bound, window } = ctx
  if (!bound?.window || !window) {
    return (
      <Part title={block.label}>
        <Quiet>{UNBOUND}</Quiet>
      </Part>
    )
  }
  return (
    <BoundChart
      block={block}
      bound={bound}
      window={window}
      ctx={ctx}
      onEmpty={onEmpty}
    />
  )
}

/** A chart's every metric in as few reads as the server admits
 * (`chartReads`), each drawn from its own rows. */
function BoundChart({
  block,
  bound,
  window,
  ctx,
  onEmpty,
}: {
  block: ChartBlock
  bound: Bound
  window: TimeRange
  ctx: BlockContext
  onEmpty?: (hidden: boolean) => void
}) {
  const whole = block.bucket === "whole"
  // A whole read is over the record's window, whatever the zoom.
  const { reads, readOf } = chartReads(
    bound,
    block,
    whole ? (bound.window ?? window) : window,
    ctx.timezone
  )
  // A zoom keeps the line on screen until the zoomed read answers.
  const answer = useMetricsReads(reads, bound.observed, { hold: true })
  const infos = useMetricReads(block.metrics)
  const many = block.metrics.length > 1
  // Under `empty: hide` a chart none of whose metrics was measured over the
  // record's window (heart rate inside a night a source scored without
  // samples) is left out once its reads are in; a failed read is still
  // said. A zoom changes nothing: a chart drawn over the whole window
  // stays drawn (the reset lives on the one holding the zoom), and one
  // left out stays out, whatever the zoomed part holds.
  const hide = block.empty === "hide"
  const rawSettled = !answer.pending && !ctx.zoomed
  // A metric whose line has no value over the record's window (a source
  // that keeps heart rate variability only as one nightly summary) draws
  // its summaries inside the window instead. Which metrics do is decided
  // over the whole window and kept through a zoom.
  const linelessNow = whole
    ? []
    : block.metrics.filter(
        (metric, i) =>
          !answer.errors[readOf[i]] &&
          !answer.dropped[readOf[i]]?.includes(metric) &&
          !chartMeasured(block, metric, answer.answers[readOf[i]])
      )
  const [lineless, setLineless] = useState<readonly string[]>([])
  if (rawSettled && lineless.join("\n") !== linelessNow.join("\n")) {
    setLineless(linelessNow)
  }
  const summaryReads = useQueries({
    queries: block.metrics.map((metric) => {
      const params = chartSummariesParams(bound, block, metric)
      return {
        // Under the records prefix with the observed kinds, so a write to
        // one of them reads the summaries again.
        queryKey: [
          "records",
          [...bound.observed],
          "chart-summaries",
          params,
        ] as const,
        queryFn: ({ signal }: { signal: AbortSignal }) =>
          fetchChartSummaries(params, (p) => fetchRecordsPage(p, signal)),
        enabled: lineless.includes(metric),
      }
    }),
  })
  const summariesOf = (i: number): ChartSummary[] | undefined => {
    const metric = block.metrics[i]
    if (!lineless.includes(metric)) return undefined
    const read = summaryReads[i]?.data
    return read && chartSummaries(read, bound, block.sources, metric)
  }
  const summaryError = (i: number): Error | null =>
    lineless.includes(block.metrics[i])
      ? (summaryReads[i]?.error ?? null)
      : null
  const summariesPending = block.metrics.some(
    (metric, i) => lineless.includes(metric) && summaryReads[i]?.isPending
  )
  const settled =
    rawSettled &&
    !summariesPending &&
    lineless.join("\n") === linelessNow.join("\n")
  const emptyNow =
    hide &&
    settled &&
    !answer.errors.some(Boolean) &&
    !block.metrics.some((_, i) => summaryError(i)) &&
    !block.metrics.some(
      (metric, i) =>
        !answer.dropped[readOf[i]]?.includes(metric) &&
        (chartMeasured(block, metric, answer.answers[readOf[i]]) ||
          Boolean(summariesOf(i)?.length))
    )
  // What the chart decided over the whole window, kept through a zoom.
  const [unzoomed, setUnzoomed] = useState(false)
  if (settled && unzoomed !== emptyNow) setUnzoomed(emptyNow)
  const hidden = ctx.zoomed ? unzoomed : emptyNow
  useEffect(() => {
    if (!hide || !settled) return
    onEmpty?.(hidden)
  }, [hide, settled, hidden, onEmpty])
  if (hidden) return null
  return (
    <Part title={block.label}>
      {block.metrics.map((metric, i) => (
        <MetricChart
          key={metric}
          block={block}
          metric={metric}
          info={infos.get(metric)?.info}
          answer={answer.answers[readOf[i]]}
          pending={answer.pending}
          error={answer.errors[readOf[i]]}
          dropped={answer.dropped[readOf[i]].includes(metric)}
          summaries={summariesOf(i)}
          summariesPending={
            lineless.includes(metric) && summaryReads[i]?.isPending
          }
          summariesError={summaryError(i)}
          summariesTruncated={Boolean(summaryReads[i]?.data?.truncated)}
          onRetrySummaries={() => void summaryReads[i]?.refetch()}
          onRetry={answer.refetch}
          bound={bound}
          window={window}
          heading={many || !block.label}
          ctx={ctx}
        />
      ))}
    </Part>
  )
}

function reducerWords(r: ChartReducer): string {
  return r.label ?? reducerChoiceWords(r.key)
}

/** One metric's rows of a chart's batched answer (`chartMetricRows`),
 * with the unit the answer names for it. */
function metricAnswer(
  answer: AggregateResult | undefined,
  block: ChartBlock,
  metric: string
): AggregateResult | undefined {
  if (!answer) return undefined
  return {
    ...answer,
    unit: answer.units?.[metric] ?? answer.unit,
    rows: chartMetricRows(block, metric, answer),
  }
}

function MetricChart({
  block,
  metric,
  info,
  answer: batched,
  pending,
  error,
  dropped,
  summaries,
  summariesPending,
  summariesError,
  summariesTruncated,
  onRetrySummaries,
  onRetry,
  bound,
  window,
  heading,
  ctx,
}: {
  block: ChartBlock
  metric: string
  info?: MetricInfo
  answer?: AggregateResult
  pending: boolean
  error: Error | null
  /** The server refused this metric's entry: no record holds it. */
  dropped: boolean
  /** The summaries inside the record's window, read where the line has no
   * value over it; undefined where it has one. */
  summaries?: readonly ChartSummary[]
  summariesPending: boolean
  summariesError: Error | null
  summariesTruncated: boolean
  onRetrySummaries: () => void
  onRetry: () => void
  bound: Bound
  window: TimeRange
  /** Whether the metric's name heads it: a chart of several metrics, or
   * one the view gives no label. */
  heading: boolean
  ctx: BlockContext
}) {
  const name = info?.name ?? metricWords(refIdOf(metric))
  const whole = block.bucket === "whole"
  const answer = useMemo(
    () => metricAnswer(batched, block, metric),
    [batched, block, metric]
  )
  return (
    <div
      className="flex flex-col gap-1.5"
      role="group"
      aria-label={heading ? name : undefined}
    >
      {heading && (
        <h4 className="text-[12.5px] text-muted-foreground">{name}</h4>
      )}
      {dropped ? (
        <Quiet>No record names the metric {metric}.</Quiet>
      ) : pending && !answer ? (
        whole ? (
          <Loading />
        ) : (
          <Skeleton className="h-[250px] w-full" />
        )
      ) : error ? (
        <Failed error={error} onRetry={onRetry} />
      ) : whole ? (
        <ReducerBars
          answer={answer}
          reducers={block.reducers}
          label={block.label ?? name}
          name={name}
          during={spanWords(ctx.bound, ctx.noun).during}
        />
      ) : (
        <Line
          answer={answer}
          block={block}
          bound={bound}
          window={window}
          name={name}
          metric={metric}
          info={info}
          summaries={summaries}
          summariesPending={summariesPending}
          summariesError={summariesError}
          summariesTruncated={summariesTruncated}
          onRetrySummaries={onRetrySummaries}
          ctx={ctx}
        />
      )}
    </div>
  )
}

/** One metric over the window: a line with its lowest-to-highest band
 * where the chart reads both, which a click or a drag zooms into, and the
 * sources it was drawn from. */
function Line({
  answer,
  block,
  bound,
  window,
  name,
  metric,
  info,
  summaries,
  summariesPending,
  summariesError,
  summariesTruncated,
  onRetrySummaries,
  ctx,
}: {
  answer?: AggregateResult
  block: ChartBlock
  bound: Bound
  window: TimeRange
  name: string
  metric: string
  info?: MetricInfo
  summaries?: readonly ChartSummary[]
  summariesPending: boolean
  summariesError: Error | null
  summariesTruncated: boolean
  onRetrySummaries: () => void
  ctx: BlockContext
}) {
  const units = useUnits()
  const metricUnit = info?.unit
  const rows = useMemo(() => answer?.rows ?? [], [answer])
  const buckets = bucketsOf([rows])
  const line = chartLineReducer(block.reducers)
  const [series] = seriesOn(buckets, rows, line.key)
  const keys = block.reducers.map((r) => r.key)
  const band = keys.includes("min") && keys.includes("max")
  const measured = series?.points.some((p) => p.value !== null) ?? false
  // The line's own reducer says its unit: a time in a range answers in a
  // unit of duration, a rate per second and a count in none, whatever the
  // metric's unit the answer names.
  const unit = valueUnit(rows, line.key) ?? answer?.unit ?? metricUnit ?? ""
  // The sources that drew the line, by title: the record's own source may
  // keep none of this metric, and the line is then another's.
  const sources = useMemo(
    () => distinct(rows.flatMap(namedSources)).sort(),
    [rows]
  )
  const titles = useReferenceTitles(sources, [...ctx.kinds])
  if (!measured && summariesError) {
    return <Failed error={summariesError} onRetry={onRetrySummaries} />
  }
  if (!measured && summaries?.length) {
    return (
      <SummaryLine
        summaries={summaries}
        truncated={summariesTruncated}
        block={block}
        window={window}
        name={name}
        metric={metric}
        info={info}
        ctx={ctx}
      />
    )
  }
  if (!measured && summariesPending) {
    return <Skeleton className="h-[250px] w-full" />
  }
  if (!measured) {
    return (
      <Quiet>
        No {lowerName(name)} was measured{" "}
        {spanWords(ctx.bound, ctx.noun).during}.
      </Quiet>
    )
  }
  return (
    <>
      {ctx.zoomed && (
        <div className="flex items-center gap-2 text-[12.5px] text-muted-foreground">
          <span>{windowWords(window.start, window.end)}</span>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-[12.5px] font-normal"
            onClick={ctx.onReset}
          >
            Show {spanWords(ctx.bound, ctx.noun).whole}
          </Button>
        </div>
      )}
      <TimelineChart
        // A new window is a new chart: no hover or drag carries over.
        key={`${window.start}-${window.end}`}
        buckets={buckets}
        series={[
          {
            key: line.key,
            color: "var(--series-1)",
            points: series.points,
            mark: markOf(shapeOf(info), line.key),
            band,
          },
        ]}
        bands={block.bands
          .filter((b) => !b.metric || sameRef(b.metric, metric))
          .map((b) => ({
            low: b.low,
            high: b.high,
            color: toneSoft(b.tone),
            label: b.label,
          }))}
        domain={window}
        size={chartBucketSize(block.bucket, window)}
        label={`${name} ${spanWords(ctx.bound, ctx.noun).during}`}
        units={{ left: units.symbol(unit) || unit }}
        onZoom={(next) => ctx.onZoom(next)}
        tooltip={(b) => {
          const row = series.points[b]?.row
          return (
            <>
              <p className="font-medium">
                {windowWords(buckets[b].start, buckets[b].end)}
              </p>
              {row ? (
                // A value and its unit never break apart.
                <dl className="mt-1 grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
                  {block.reducers.map((r) => {
                    const v = rowValue(row, r.key)
                    return (
                      <div key={r.key} className="contents">
                        <dt className="text-faint">{reducerWords(r)}</dt>
                        <dd className="text-right whitespace-nowrap tabular-nums">
                          {readingText(
                            units,
                            v?.value,
                            v?.unit,
                            0,
                            v?.comparator
                          )}
                        </dd>
                      </div>
                    )
                  })}
                </dl>
              ) : (
                <p className="mt-1 text-muted-foreground">Nothing measured.</p>
              )}
            </>
          )
        }}
      />
      {sources.length > 0 && block.sources !== "only" && (
        <Quiet>
          {sourcesWords(
            name,
            sources.map((p) => titles.get(p) || refIdOf(p)),
            Boolean(
              bound.source && !sources.some((p) => sameRef(p, bound.source))
            ),
            ctx.noun
          )}
        </Quiet>
      )}
    </>
  )
}

/** A metric the window holds no point of, drawn from its summaries inside
 * the window: each one value held level across the span it covers, one
 * line per source and statistic, in the chart's unit where it names one,
 * else the metric's. */
function SummaryLine({
  summaries,
  truncated,
  block,
  window,
  name,
  metric,
  info,
  ctx,
}: {
  summaries: readonly ChartSummary[]
  /** The window holds more summaries than were read. */
  truncated: boolean
  block: ChartBlock
  window: TimeRange
  name: string
  metric: string
  info?: MetricInfo
  ctx: BlockContext
}) {
  const units = useUnits()
  const unit = block.unit ?? info?.unit ?? summaries[0].unit
  const size = chartBucketSize(block.bucket, window)
  const buckets = useMemo(
    () => summaryBuckets(window, summaries),
    [window, summaries]
  )
  const { lines, omitted } = useMemo(
    () =>
      summarySeries(summaries, buckets, (s) => summaryValueIn(s, unit, units)),
    [summaries, buckets, unit, units]
  )
  const sources = useMemo(
    () => distinct(summaries.flatMap((s) => (s.source ? [s.source] : []))),
    [summaries]
  )
  const titles = useReferenceTitles(sources, [...ctx.kinds])
  const sourceName = (source: string | undefined) =>
    source ? titles.get(source) || refIdOf(source) : "no source"
  const lineName = (line: SummarySeries) => {
    const stat = line.statistic ? statisticLabel(line.statistic) : undefined
    const who = sourceName(line.source)
    return stat ? `${who}, ${lowerName(stat)}` : who
  }
  const during = spanWords(ctx.bound, ctx.noun).during
  // A bound (`<0.5`) is no value a stroke may draw: it is said instead,
  // once per summary however many pieces it was cut into.
  const bounds = summaries.filter(
    (s, i) =>
      s.comparator &&
      summaries.findIndex(
        (o) =>
          o.span.start === s.span.start &&
          o.span.end === s.span.end &&
          o.source === s.source &&
          o.value === s.value
      ) === i
  )
  const boundWords = bounds.map(
    (s) =>
      `${sourceName(s.source)}: ${readingText(units, s.value, s.unit, info?.displayPrecision, s.comparator)} over ${windowWords(s.span.start, s.span.end)}`
  )
  // Said whatever the chart draws: a zoomed part with no summary in it
  // still offers the way back, and a capped read says it is capped.
  const zoomBar = ctx.zoomed && (
    <div className="flex items-center gap-2 text-[12.5px] text-muted-foreground">
      <span>{windowWords(window.start, window.end)}</span>
      <Button
        variant="ghost"
        size="sm"
        className="h-7 text-[12.5px] font-normal"
        onClick={ctx.onReset}
      >
        Show {spanWords(ctx.bound, ctx.noun).whole}
      </Button>
    </div>
  )
  const truncatedNote = truncated && (
    <Quiet>
      Shows the first{" "}
      {(CHART_SUMMARIES_PAGE * CHART_SUMMARIES_PAGES).toLocaleString("en")}{" "}
      summaries read inside this window; more follow.
    </Quiet>
  )
  if (!lines.some((l) => l.points.some((p) => p.value !== null))) {
    return (
      <>
        {zoomBar}
        <Quiet>
          {boundWords.length
            ? `No samples of ${lowerName(name)} were kept ${during}, and its summaries are bounds, not values: ${listWords(boundWords)}.`
            : `No summary of ${lowerName(name)} covers ${windowWords(window.start, window.end)}.`}
        </Quiet>
        {truncatedNote}
      </>
    )
  }
  return (
    <>
      {zoomBar}
      <TimelineChart
        key={`${window.start}-${window.end}`}
        buckets={buckets}
        series={lines.map((line, i) => ({
          key: line.key,
          color: `var(--series-${(i % 8) + 1})`,
          points: line.points,
          mark: "level",
        }))}
        bands={block.bands
          .filter((b) => !b.metric || sameRef(b.metric, metric))
          .map((b) => ({
            low: b.low,
            high: b.high,
            color: toneSoft(b.tone),
            label: b.label,
          }))}
        domain={window}
        size={size}
        label={`${name} ${during}, from summaries`}
        units={{ left: units.symbol(unit) || unit }}
        onZoom={(next) => ctx.onZoom(next)}
        tooltip={(b) => {
          const held = lines.flatMap((line) => {
            const s = line.held[b]
            return s ? [{ line, s }] : []
          })
          return (
            <>
              <p className="font-medium">
                {windowWords(buckets[b].start, buckets[b].end)}
              </p>
              {held.length ? (
                <dl className="mt-1 grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
                  {held.map(({ line, s }) => (
                    <div key={line.key} className="contents">
                      <dt className="text-faint">
                        {lineName(line)},{" "}
                        {windowWords(s.span.start, s.span.end)}
                      </dt>
                      <dd className="text-right whitespace-nowrap tabular-nums">
                        {readingText(
                          units,
                          s.value,
                          s.unit,
                          info?.displayPrecision
                        )}
                      </dd>
                    </div>
                  ))}
                </dl>
              ) : (
                <p className="mt-1 text-muted-foreground">Nothing measured.</p>
              )}
            </>
          )
        }}
      />
      <Quiet>{summaryWords(name, lines.map(lineName), during)}</Quiet>
      {omitted > 0 && (
        <Quiet>
          Draws the {lines.length} lines covering the most time; {omitted} more
          are not drawn.
        </Quiet>
      )}
      {boundWords.length > 0 && (
        <Quiet>
          Bounds, not values, so not drawn: {listWords(boundWords)}.
        </Quiet>
      )}
      {truncatedNote}
    </>
  )
}

/** What a summary line is: no samples over the window, and whose values
 * over which spans it draws instead. */
function summaryWords(
  name: string,
  lines: readonly string[],
  during: string
): string {
  return `No samples of ${lowerName(name)} were kept ${during}. The line holds each summary's one value across the span it covers: ${listWords([...lines])}.`
}

/** The unit the values of one reducer answer in: the first value's, `""`
 * for one that has none (a count). Undefined where no row holds a value. */
function valueUnit(
  rows: readonly AggregateRow[],
  key: string
): string | undefined {
  for (const row of rows) {
    const v = rowValue(row, key)
    if (v && v.value !== null) return v.unit ?? ""
  }
  return undefined
}

function lowerName(name: string): string {
  // An acronym keeps its capitals: "HRV", not "hRV".
  return /^[A-Z][A-Z]/.test(name)
    ? name
    : name.charAt(0).toLowerCase() + name.slice(1)
}

/** Which sources a line is drawn from, and where the record's own source
 * is not among them, that it kept none. */
function sourcesWords(
  name: string,
  names: readonly string[],
  ownMissing: boolean,
  noun: string
): string {
  const from = `${name} from ${listWords([...names])}`
  return ownMissing
    ? `${from}: this ${noun}’s own source kept none for it.`
    : `${from}.`
}

/** The milliseconds in one of a duration unit, for the units a reducer
 * that counts time answers in. */
const DURATION_MS: Record<string, number> = {
  ms: 1,
  s: 1_000,
  min: 60_000,
  h: 3_600_000,
  d: 86_400_000,
}

/** One value per reducer over the whole window, as bars. */
function ReducerBars({
  answer,
  reducers,
  label,
  name,
  during,
}: {
  answer?: AggregateResult
  reducers: readonly ChartReducer[]
  label: string
  name: string
  /** The window in words: "during this cycle", "in the last 7 days". */
  during: string
}) {
  const units = useUnits()
  const row = answer?.rows[0]
  if (!row) {
    return (
      <Quiet>
        No {lowerName(name)} was measured {during}.
      </Quiet>
    )
  }
  return (
    <Bars
      label={label}
      items={reducers.map((r) => {
        const v = rowValue(row, r.key)
        const n = numberOf(v?.value)
        // A value answers in its reducer's own unit, a count in none.
        const unit = v ? v.unit : answer?.unit
        const ms = unit ? DURATION_MS[unit] : undefined
        return {
          key: r.key,
          name: reducerWords(r),
          amount: n,
          text:
            n === null
              ? "none"
              : ms
                ? durationWords(n * ms)
                : readingText(units, v?.value, unit, 0, v?.comparator),
        }
      })}
    />
  )
}

export function SpansBlockView({
  block,
  ctx,
}: {
  block: SpansBlock
  ctx: BlockContext
}) {
  const { bound, window } = ctx
  if (!bound?.window || !window) {
    return (
      <Part title={block.label}>
        <Quiet>{UNBOUND}</Quiet>
      </Part>
    )
  }
  return <BoundSpans block={block} bound={bound} window={window} ctx={ctx} />
}

function BoundSpans({
  block,
  bound,
  window,
  ctx,
}: {
  block: SpansBlock
  bound: Bound
  window: TimeRange
  ctx: BlockContext
}) {
  const metric = useMetricReads([block.metric]).get(block.metric) ?? {
    pending: true,
  }
  const info = metric.info
  const name = info?.name ?? metricWords(refIdOf(block.metric))
  // Nothing is read before the metric record says it is a coded amount: a
  // coded gauge's points hold no span and the server refuses its sum.
  const refused = info ? notSpans(info, block.metric) : undefined
  const readable = Boolean(info) && !refused
  // The list draws one source's spans: the record's under `only`, else the
  // one the totals' policy answered from, so the two halves agree.
  const narrowed = spansNarrowed(bound, block)
  const totals = useQuery({
    ...aggregateQueryOptions(spansTotalsRead(bound, block), bound.observed),
    enabled: readable && (block.totals || !narrowed),
  })
  const source = spansSource(bound, block, totals.data)
  const spans = useQuery({
    ...recordsQueryOptions(spansListParams(bound, block, source)),
    enabled: readable && (narrowed || !totals.isPending),
  })
  const sources = narrowed ? 0 : spansSourceCount(totals.data)
  const order = block.codes.map((c) => c.code)
  // The metric's own codes give each code the colour the timeline gives it,
  // and nothing draws before they are read, so a colour never swaps.
  const palette = info?.codeSet ?? []
  const words = (code: string) =>
    block.codes.find((c) => c.code === code)?.label ?? codeWords(code)
  const label = block.label ?? name
  if (metric.pending) {
    return (
      <Part title={label}>
        <Loading />
      </Part>
    )
  }
  if (!info) {
    return (
      <Part title={label}>
        <Quiet>
          No record names the metric {block.metric}, so there is nothing to
          read.
        </Quiet>
      </Part>
    )
  }
  if (refused) return <Notice>{refused}</Notice>
  if (spans.isPending || ((block.totals || !narrowed) && totals.isPending)) {
    return (
      <Part title={label}>
        <Loading />
      </Part>
    )
  }
  return (
    <Part title={label}>
      {spans.data && (
        <SpanRows
          window={window}
          spans={spansOf(spans.data.records)}
          order={order}
          palette={palette}
          words={words}
          label={label}
        />
      )}
      {spans.data?.cursor && (
        <Quiet>
          Shows the first {spans.data.records.length.toLocaleString("en")} spans
          that start inside this window; more follow.
        </Quiet>
      )}
      {sources > 1 && source !== undefined && (
        <Quiet>
          The totals take each stretch from the first of {sources} sources that
          holds it; the spans drawn are{" "}
          {source ? `${refIdOf(source)}’s` : "the values naming no source"}.
        </Quiet>
      )}
      {spans.error && (
        <Failed error={spans.error} onRetry={() => void spans.refetch()} />
      )}
      {block.totals &&
        (totals.error && !noMetric(totals.error) ? (
          <Failed error={totals.error} onRetry={() => void totals.refetch()} />
        ) : (
          <CodeTotals
            answer={totals.data}
            palette={palette}
            words={words}
            label={label}
            name={name}
            during={spanWords(ctx.bound, ctx.noun).during}
          />
        ))}
    </Part>
  )
}

/** The time in each code, by the code's words, each bar in its code's
 * colour. */
function CodeTotals({
  answer,
  palette,
  words,
  label,
  name,
  during,
}: {
  answer?: AggregateResult
  palette: readonly string[]
  words: (code: string) => string
  label: string
  name: string
  /** The window in words: "during this cycle", "in the last 7 days". */
  during: string
}) {
  const rows = (answer?.rows ?? []).filter((r) => r.group?.code)
  if (!rows.length) {
    return (
      <Quiet>
        No {lowerName(name)} was recorded {during}.
      </Quiet>
    )
  }
  return (
    <Bars
      label={`Time in each code of ${label}`}
      items={rows.map((r) => {
        const code = r.group!.code!
        const minutes = numberOf(rowValue(r, "sum")?.value)
        return {
          key: code,
          name: words(code),
          amount: minutes,
          text: minutes === null ? "none" : durationWords(minutes * 60_000),
          color: codeColor(code, palette),
        }
      })}
    />
  )
}

const SPAN_ROW = 18
const SPAN_AXIS = 18

/** The spans in sequence: a row per code, a block per span over the
 * window, so the record reads left to right, on the same time axis as the
 * view's line charts (`timeAxis`): the same width, margins and labelled
 * times. Drawn only where the spans never overlap (`sequenceOf`). */
function SpanRows({
  window,
  spans,
  order,
  palette,
  words,
  label,
}: {
  window: TimeRange
  spans: Span[]
  order: readonly string[]
  palette: readonly string[]
  words: (code: string) => string
  label: string
}) {
  const { ref, width } = useWidth(720)
  const whole = sequenceOf(spans)
  if (!whole) return null
  // The rows are the whole window's, so a zoom keeps each code on its row;
  // the spans are those the window holds, cut to it.
  const rows = spanRows(whole, order)
  const shown = whole.filter(
    (s) => s.end > window.start && s.start < window.end
  )
  const { x, labels } = timeAxis(window, chartBucketSize("auto", window), width)
  const height = rows.length * SPAN_ROW + SPAN_AXIS
  const held = rows
    .filter((code) => shown.some((s) => s.code === code))
    .map(words)
    .join(", ")
  return (
    <div ref={ref} className="w-full">
      <svg
        width={width}
        height={height}
        role="img"
        aria-label={`${label} from ${clockWords(window.start)} to ${clockWords(window.end)}: ${shown.length} spans of ${held}`}
        className="block"
        data-slot="span-rows"
      >
        {rows.map((code, i) => (
          <text
            key={code}
            x={AXIS_LEFT - 8}
            y={i * SPAN_ROW + SPAN_ROW / 2}
            textAnchor="end"
            dominantBaseline="middle"
            className="fill-faint text-[11.5px]"
          >
            {words(code)}
          </text>
        ))}
        {shown.map((s) => {
          const row = rows.indexOf(s.code)
          const x0 = x(Math.max(s.start, window.start))
          const x1 = x(Math.min(s.end, window.end))
          return (
            <rect
              key={`${s.start}-${s.code}`}
              data-code={s.code}
              x={x0}
              y={row * SPAN_ROW + 2}
              width={Math.max(1, x1 - x0)}
              height={SPAN_ROW - 4}
              rx={2}
              fill={codeColor(s.code, palette)}
            >
              <title>
                {`${words(s.code)}, ${clockWords(s.start)} to ${clockWords(s.end)}`}
              </title>
            </rect>
          )
        })}
        {labels.map((l) => (
          <text
            key={l.i}
            x={l.at}
            y={height - 4}
            textAnchor="middle"
            className="fill-faint text-[11.5px] tabular-nums"
          >
            {l.text}
          </text>
        ))}
      </svg>
    </div>
  )
}
