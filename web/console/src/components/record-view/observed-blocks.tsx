/** A record view's chart and spans blocks: what was measured during the
 * record, read over the window the view binds on it. A chart draws each
 * metric it names as a line with its band, which a click or a drag zooms
 * into, or with a `whole` bucket as one bar per reducer (the time in each
 * heart rate zone a view lists, for one). A spans block draws a coded
 * metric's spans as rows in the view's order of codes where the source
 * recorded them in sequence, and its time in each code. */

import { useMemo, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"

import {
  Failed,
  Loading,
  Notice,
  Part,
  Quiet,
  type BlockContext,
} from "./parts"
import { TimelineChart } from "@/components/observations/timeline-chart"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import { useWidth } from "@/hooks/use-width"
import { aggregateQueryOptions } from "@/lib/api/observations"
import { recordQueryOptions, recordsQueryOptions } from "@/lib/api/records"
import { ApiError, type AggregateResult } from "@/lib/api/types"
import {
  bandedReducer,
  codeWords,
  distinct,
  metricInfoOf,
  metricWords,
  readingText,
  reducerChoiceWords,
  refIdOf,
  sameRef,
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
import { codeColor } from "@/lib/overlay"
import { splitRecordPath } from "@/lib/record-path"
import {
  chartBucketSize,
  chartRead,
  metricPathOf,
  sequenceOf,
  spanRows,
  spansListParams,
  spansOf,
  spansTotalsRead,
  type Bound,
  type ChartBlock,
  type ChartReducer,
  type Span,
  type SpansBlock,
} from "@/lib/record-view"
import { AXIS_LEFT, timeAxis } from "@/lib/time-axis"
import { listWords } from "@/lib/tools"
import { unitSymbol } from "@/lib/units"

/** Why a chart or spans block has nothing to read: its view binds no
 * window. A view that binds one the record does not hold leaves these
 * blocks out (`RecordViewSection`). */
const UNBOUND = "Nothing to read here: the view binds no window."

/** Whether a read found nothing to say: a repository with no such metric
 * record, which the server refuses by its `metric.ref`. */
function noMetric(error: Error | null): boolean {
  return (
    error instanceof ApiError &&
    error.status === 422 &&
    error.problems.some((p) => p.startsWith("aggregate.metric"))
  )
}

/** The metric record a metric id names, read for its name, unit and codes. */
function useMetric(bound: Bound, id: string): MetricInfo | undefined {
  return useMetricRead(bound, id).info
}

/** The metric record a metric id names, and whether its read is still
 * out. */
function useMetricRead(
  bound: Bound,
  id: string
): { info?: MetricInfo; pending: boolean } {
  const parts = splitRecordPath(metricPathOf(bound, id))
  const [authority = "", pkg = "", name = ""] = parts?.kind.split("/") ?? []
  const read = useQuery({
    ...recordQueryOptions(authority, pkg, name, parts?.id ?? ""),
    retry: false,
  })
  return {
    info: read.data?.kind ? metricInfoOf(read.data) : undefined,
    pending: read.isPending,
  }
}

/** Why a spans block cannot draw a metric: it draws a coded amount, whose
 * values each hold a code over a span. Undefined where it can. */
function notSpans(info: MetricInfo, id: string): string | undefined {
  const name = info.name ?? metricWords(id)
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
}: {
  block: ChartBlock
  ctx: BlockContext
}) {
  const { bound, window } = ctx
  if (!bound || !window) {
    return (
      <Part title={block.label}>
        <Quiet>{UNBOUND}</Quiet>
      </Part>
    )
  }
  const many = block.metrics.length > 1
  return (
    <Part title={block.label}>
      {block.metrics.map((metric) => (
        <MetricChart
          key={metric}
          block={block}
          metric={metric}
          bound={bound}
          window={window}
          heading={many || !block.label}
          ctx={ctx}
        />
      ))}
    </Part>
  )
}

/** The reducer a line draws: the first in the metric's own unit, else the
 * first named. */
function lineReducer(reducers: readonly ChartReducer[]): ChartReducer {
  return reducers.find((r) => bandedReducer(r.key)) ?? reducers[0]
}

function reducerWords(r: ChartReducer): string {
  return r.label ?? reducerChoiceWords(r.key)
}

function MetricChart({
  block,
  metric,
  bound,
  window,
  heading,
  ctx,
}: {
  block: ChartBlock
  metric: string
  bound: Bound
  window: TimeRange
  /** Whether the metric's name heads it: a chart of several metrics, or
   * one the view gives no label. */
  heading: boolean
  ctx: BlockContext
}) {
  const info = useMetric(bound, metric)
  const name = info?.name ?? metricWords(metric)
  const whole = block.bucket === "whole"
  // A whole read is over the record's window, whatever the zoom.
  const read = chartRead(
    bound,
    block,
    metric,
    whole ? bound.window : window,
    ctx.timezone
  )
  const answer = useQuery(aggregateQueryOptions(read, bound.observed))
  const error = noMetric(answer.error) ? null : answer.error
  return (
    <div
      className="flex flex-col gap-1.5"
      role="group"
      aria-label={heading ? name : undefined}
    >
      {heading && (
        <h4 className="text-[12.5px] text-muted-foreground">{name}</h4>
      )}
      {answer.isPending ? (
        whole ? (
          <Loading />
        ) : (
          <Skeleton className="h-[250px] w-full" />
        )
      ) : error ? (
        <Failed error={error} />
      ) : whole ? (
        <ReducerBars
          answer={answer.data}
          reducers={block.reducers}
          label={block.label ?? name}
          name={name}
          noun={ctx.noun}
        />
      ) : (
        <Line
          answer={answer.data}
          block={block}
          bound={bound}
          window={window}
          name={name}
          unit={info?.unit}
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
  unit: metricUnit,
  ctx,
}: {
  answer?: AggregateResult
  block: ChartBlock
  bound: Bound
  window: TimeRange
  name: string
  unit?: string
  ctx: BlockContext
}) {
  const rows = useMemo(() => answer?.rows ?? [], [answer])
  const buckets = bucketsOf([rows])
  const line = lineReducer(block.reducers)
  const [series] = seriesOn(buckets, rows, line.key)
  const keys = block.reducers.map((r) => r.key)
  const band = keys.includes("min") && keys.includes("max")
  const measured = series?.points.some((p) => p.value !== null) ?? false
  const unit = answer?.unit ?? metricUnit ?? ""
  // The sources that drew the line, by title: the record's own source may
  // keep none of this metric, and the line is then another's.
  const sources = useMemo(
    () => distinct(rows.flatMap(namedSources)).sort(),
    [rows]
  )
  const titles = useReferenceTitles(sources, [...ctx.kinds])
  if (!measured) {
    return (
      <Quiet>
        No {lowerName(name)} was measured during this {ctx.noun}.
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
            Show the whole {ctx.noun}
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
            mark: "line",
            band,
          },
        ]}
        domain={window}
        size={chartBucketSize(block.bucket, window)}
        label={`${name} during this ${ctx.noun}`}
        units={{ left: unitSymbol(unit) || unit }}
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
                          {readingText(v?.value, v?.unit, 0)}
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
  noun,
}: {
  answer?: AggregateResult
  reducers: readonly ChartReducer[]
  label: string
  name: string
  noun: string
}) {
  const row = answer?.rows[0]
  if (!row) {
    return (
      <Quiet>
        No {lowerName(name)} was measured during this {noun}.
      </Quiet>
    )
  }
  return (
    <Bars
      label={label}
      items={reducers.map((r) => {
        const v = rowValue(row, r.key)
        const n = numberOf(v?.value)
        const unit = v?.unit ?? answer?.unit
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
                : readingText(v?.value, unit, 0),
        }
      })}
    />
  )
}

/** A bar per item, its length its share of the largest. */
function Bars({
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

export function SpansBlockView({
  block,
  ctx,
}: {
  block: SpansBlock
  ctx: BlockContext
}) {
  const { bound, window } = ctx
  if (!bound || !window) {
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
  const metric = useMetricRead(bound, block.metric)
  const info = metric.info
  const name = info?.name ?? metricWords(block.metric)
  // Nothing is read before the metric record says it is a coded amount: a
  // coded gauge's points hold no span and the server refuses its sum.
  const refused = info ? notSpans(info, block.metric) : undefined
  const readable = Boolean(info) && !refused
  const totals = useQuery({
    ...aggregateQueryOptions(spansTotalsRead(bound, block), bound.observed),
    enabled: readable && block.totals,
  })
  const spans = useQuery({
    ...recordsQueryOptions(spansListParams(bound, block)),
    enabled: readable,
  })
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
  if (spans.isPending || (block.totals && totals.isPending)) {
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
      {spans.error && <Failed error={spans.error} />}
      {block.totals &&
        (totals.error && !noMetric(totals.error) ? (
          <Failed error={totals.error} />
        ) : (
          <CodeTotals
            answer={totals.data}
            palette={palette}
            words={words}
            label={label}
            name={name}
            noun={ctx.noun}
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
  noun,
}: {
  answer?: AggregateResult
  palette: readonly string[]
  words: (code: string) => string
  label: string
  name: string
  noun: string
}) {
  const rows = (answer?.rows ?? []).filter((r) => r.group?.code)
  if (!rows.length) {
    return (
      <Quiet>
        No {lowerName(name)} was recorded for this {noun}.
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
