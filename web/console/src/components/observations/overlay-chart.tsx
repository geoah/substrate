/** One chart of several metrics, each planned by `lib/overlay.ts`, read in
 * one request where their reads differ only in each metric's own choices
 * (`sharedReads`) and apart otherwise: the series of one unit share the
 * left axis, a second unit reads on the right, and a metric of a third
 * unit is left out with a sentence saying which and what to deselect. A
 * level is a line, with its band beside a reducer in its own unit
 * (`bandedReducer`); an amount, a count, an increase and a time in a zone
 * are bars (`markOf`); a category is bars stacked per code. One crosshair
 * and one tooltip list every series at the hovered bucket with its unit.
 *
 * Above the chart, one row per metric: its name and axis, its reducer and
 * unit, where its read stands (reading, refreshing, refused), and its
 * legend, each entry a toggle that hides or shows that series. Under the
 * chart, each metric's numbers, and with Technical details on, each
 * request itself. */

import { useState, type ReactNode } from "react"
import {
  useQueries,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query"

import { CodeBlock } from "@/components/code-block"
import { CopyButton } from "@/components/identity/copy-button"
import { IdText } from "@/components/identity/id-text"
import { SectionHead } from "@/components/identity/section-head"
import {
  AboutMark,
  CodeMark,
  GroupRecord,
  ShapeWord,
} from "@/components/observations/marks"
import { Picker } from "@/components/observations/picker"
import { RollupTable } from "@/components/observations/rollup-table"
import {
  TimelineChart,
  type ChartSeries,
} from "@/components/observations/timeline-chart"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { Observations } from "@/hooks/use-observations"
import {
  aggregateCurl,
  aggregateQueryOptions,
  heldAnswer,
  metricAnswer,
  type AggregateRead,
} from "@/lib/api/observations"
import {
  ApiError,
  type AggregateResult,
  type AggregateRow,
} from "@/lib/api/types"
import {
  codeWords,
  distinct,
  readingText,
  reducerKey,
  reducerLabel,
  reducerName,
  statisticWords,
} from "@/lib/observations"
import {
  coverageWords,
  offsetWords,
  windowWords,
  type BucketSize,
  type TimeRange,
} from "@/lib/observations-time"
import { sharedReads } from "@/lib/observations-reads"
import {
  answerReducers,
  bucketsOf,
  namedSources,
  rowValue,
  seriesOn,
  type Bucket,
  type Series,
} from "@/lib/observations-answer"
import {
  assignAxes,
  leftOutWords,
  plotRows,
  seriesColors,
  seriesKey,
  unitWords,
  universeOf,
  type AxisSide,
  type ChartMark,
  type Plot,
} from "@/lib/overlay"
import { listWords } from "@/lib/tools"
import { unitLabel } from "@/lib/units"
import { cn } from "@/lib/utils"

/** One drawn series, with the metric it belongs to. */
interface Drawn {
  key: string
  plot: Plot
  series: Series
  label: string
  color: string
  side: AxisSide
  stale: boolean
}

const NO_ROWS: readonly AggregateRow[] = []

/** Where one metric's read stands, and the rows the chart draws of it. */
interface PlotState {
  plot: Plot
  rows: readonly AggregateRow[]
  pending: boolean
  failed: boolean
  /** Why the read failed. */
  error?: Error | null
  /** Whether the answer is exact, where its request reads this metric
   * alone: a request of several metrics answers it once for all of them,
   * so its details say it (`RequestDetails`). */
  exact?: boolean
  /** A newer answer is on its way while this one is drawn. */
  stale: boolean
  /** An earlier read's answer drawn while this one loads: what it lacks or
   * drops says nothing of this read. */
  held: boolean
  /** The other metrics its request reads: a recorded-local row's offsets
   * are then the day's across all of them, not this metric's own. */
  together: string[]
}

/** One request a chart makes, the metrics it is drawn for, and where it
 * stands. */
interface ChartRequest {
  read: AggregateRead
  plots: Plot[]
  query: UseQueryResult<AggregateResult>
}

/** A read of several metrics the server refused: each of its metrics is
 * read on its own instead, so a refusal of one stays on that metric's row
 * and a budget the metrics pass only apart is never in the way. A server
 * older than `aggregate.metrics` answers it with a 400 naming the key
 * (`aggregate: unknown field "metrics"`), and reads each metric alone. */
function refusedTogether(error: Error | null | undefined): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 422 ||
      (error.status === 400 && error.message.includes('"metrics"')))
  )
}

function lowerFirst(text: string): string {
  return text.charAt(0).toLowerCase() + text.slice(1)
}

/** A plot's reducer as its Show menu names it. */
function choiceWords(plot: Plot): string {
  return (
    plot.reducerMenu.find((o) => o.value === plot.reducer)?.label ??
    reducerLabel(plot.reducer)
  )
}

export function OverlayChart({
  obs,
  plots,
  peers,
  range,
  size,
  title = false,
  onReducer,
  onUnit,
  onStatistic,
  onZoom,
  className,
}: {
  obs: Observations
  /** The metrics, in the order they take the axes. */
  plots: Plot[]
  /** Every metric read under the same choices beside this chart's, in the
   * page's order, this chart's among them: a request may hold the metrics
   * of several charts, and each chart asks for it under one query key.
   * This chart's metrics where absent. */
  peers?: Plot[]
  range: TimeRange
  size: BucketSize
  /** Head the chart with its metrics' names (the timeline page); a panel
   * that names itself leaves it off. */
  title?: boolean
  onReducer: (metric: string, value: string) => void
  onUnit: (metric: string, value: string) => void
  onStatistic: (metric: string, value: string) => void
  onZoom: (range: TimeRange) => void
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set())
  // The window a keyboard zoom asked for: its chart takes the focus once
  // drawn, since the zoomed-out one it had goes while the new one is read.
  // Cleared once the focus is in the chart, so a chart drawn anew in the
  // same window (a unit or a grouping read again) leaves the focus alone.
  const [focusFor, setFocusFor] = useState<string>()
  const windowKey = `${range.start}-${range.end}`
  const plan = assignAxes(plots)
  const drawn = plots.filter((p) => !plan.leftOut.includes(p))
  const sideOf = (plot: Plot): AxisSide =>
    plan.axes.find((a) => a.plots.includes(plot))?.side ?? "left"
  const twoAxes = plan.axes.length > 1
  const refusal = leftOutWords(plan)

  const client = useQueryClient()
  // The metrics that read, this chart's and its peers', once their streams
  // are known, as few requests as hold them; the chart asks for those
  // holding its own. A changed bucket, filter or reducer the cached answers
  // carry holds the frame while a request loads (`heldAnswer`), per metric.
  const reading = (peers ?? drawn).filter((p) => p.read && obs.knows(p.metric))
  const shared = sharedReads(
    reading.map((p) => ({ read: p.read!, buckets: p.estimate }))
  ).filter(({ lanes }) => lanes.some((at) => drawn.includes(reading[at])))
  const together = useQueries({
    queries: shared.map(({ read, lanes }) => ({
      ...aggregateQueryOptions(read, obs.observedIds),
      placeholderData: (prev?: AggregateResult) =>
        prev ?? heldAnswer(client, read, obs.observedIds),
      // A refused read of several metrics is not asked again under the same
      // choices: a refetch would clear the refusal its metrics read apart
      // on.
      enabled: (query: { state: { error: Error | null } }) =>
        lanes.length === 1 || !refusedTogether(query.state.error),
    })),
  })
  const refused = shared.map(
    ({ lanes }, i) => lanes.length > 1 && refusedTogether(together[i].error)
  )
  const apart = shared.flatMap(({ lanes }, i) =>
    refused[i]
      ? lanes.map((at) => reading[at]).filter((p) => drawn.includes(p))
      : []
  )
  const alone = useQueries({
    queries: apart.map((plot) => ({
      ...aggregateQueryOptions(plot.read!, obs.observedIds),
      placeholderData: (prev?: AggregateResult) =>
        prev ?? heldAnswer(client, plot.read!, obs.observedIds),
    })),
  })
  const firstDrawn = (r: ChartRequest) =>
    Math.min(...r.plots.map((p) => drawn.indexOf(p)).filter((i) => i >= 0))
  const requests: ChartRequest[] = [
    ...shared.flatMap(({ read, lanes }, i) =>
      refused[i]
        ? []
        : [{ read, plots: lanes.map((at) => reading[at]), query: together[i] }]
    ),
    ...apart.map((plot, i) => ({
      read: plot.read!,
      plots: [plot],
      query: alone[i],
    })),
  ].sort((a, b) => firstDrawn(a) - firstDrawn(b))

  /** The request a plot's rows come from, and where it stands. */
  function sourceOf(
    plot: Plot
  ):
    | { read: AggregateRead; query: UseQueryResult<AggregateResult> }
    | undefined {
    const own = apart.indexOf(plot)
    if (own >= 0) return { read: plot.read!, query: alone[own] }
    const at = reading.indexOf(plot)
    const i = shared.findIndex((s) => s.lanes.includes(at))
    return i >= 0 ? { read: shared[i].read, query: together[i] } : undefined
  }

  const states = drawn.map((plot): PlotState => {
    const blank = {
      plot,
      rows: NO_ROWS,
      failed: false,
      stale: false,
      held: false,
      together: [],
    }
    if (!plot.read) return { ...blank, pending: false }
    const source = sourceOf(plot)
    // A metric whose streams are still being read waits for them.
    if (!source) return { ...blank, pending: true }
    const { read, query } = source
    const answer = query.data
      ? metricAnswer(query.data, read, plot.read.aggregate.metric?.ref ?? "")
      : undefined
    // An answer held for the request's other metrics holds none of this.
    const pending = query.isPending || (query.isPlaceholderData && !answer)
    const own = plot.read.aggregate.metric?.ref
    return {
      plot,
      together: (read.aggregate.metrics ?? [])
        .filter((m) => m.ref !== own)
        .map((m) => obs.metricName(m.ref)),
      rows: plotRows(plot, answer?.rows ?? []),
      pending,
      failed: query.isError,
      error: query.error,
      exact: read.aggregate.metrics ? undefined : answer?.exact,
      stale: query.isFetching && !pending,
      held: query.isPlaceholderData && Boolean(answer),
    }
  })

  // Every plot of a chart shares its scope, so one says whether its days
  // are recorded-local.
  const local = drawn.find((p) => p.local)?.local
  const buckets = bucketsOf(
    states.map((s) => s.rows),
    local?.size
  )
  const sourceText = (value: string) => obs.nameOf(value, obs.pins.source)
  function labelOf(plot: Plot, s: Series): string {
    const name =
      plot.groupKey === "source" && s.value
        ? sourceText(s.value)
        : plot.groupKey === "about"
          ? s.value
            ? obs.nameOf(s.value, obs.pins.about)
            : "You"
          : plot.groupKey === "code" && s.value
            ? codeWords(s.value)
            : plot.name
    return s.channel ? `${name} (${s.channel})` : name
  }

  // A read that landed with no rows beside a metric that has some: the
  // chart draws nothing of it and no axis for it, so its row says so.
  const empty = (state: PlotState) =>
    buckets.length > 0 &&
    Boolean(state.plot.read) &&
    !state.pending &&
    !state.failed &&
    !state.held &&
    state.rows.length === 0

  const stateOf = (plot: Plot) => states.find((st) => st.plot === plot)
  const all = drawnSeries(states, buckets, sideOf, labelOf, local?.size)
  const shown = all.filter((d) => !hidden.has(d.key))
  // A level's band is drawn where its metric shows one line: under several,
  // the washes pile up over the lines they belong to.
  const lonely = (plot: Plot) =>
    shown.filter((d) => d.plot === plot).length === 1
  const chartSeries: ChartSeries[] = shown.map((d) => ({
    key: d.key,
    color: d.color,
    points: d.series.points,
    mark: d.plot.mark,
    band: d.plot.band && lonely(d.plot),
    axis: d.side,
    // A channel's categories stack apart from another channel's.
    stack:
      d.plot.mark === "stack"
        ? seriesKey(d.plot.metric, d.series.channel ?? "")
        : undefined,
    stale: d.stale,
  }))
  const legend = all.length > 1 || drawn.some((p) => p.groupKey)

  function toggle(key: string) {
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const left = plan.axes.find((a) => a.side === "left")
  const right = plan.axes.find((a) => a.side === "right")
  const reads = states.filter((s) => s.plot.read)
  const landed = reads.filter((s) => !s.pending && !s.failed)
  const names = listWords(drawn.map((p) => p.name))
  const label = drawn.map((p) => `${p.name}, ${choiceWords(p)}`).join("; ")

  function groupCell(plot: Plot) {
    return (value: string | undefined): ReactNode => {
      if (plot.groupKey === "source" && value) {
        return <GroupRecord value={value} pin={obs.pins.source} />
      }
      if (plot.groupKey === "about") {
        return <AboutMark value={value} pin={obs.pins.about} />
      }
      if (plot.groupKey === "code") return <CodeMark code={value} />
      return value ?? ""
    }
  }

  function valueText(plot: Plot, row: AggregateRow, key: string): string {
    const v = rowValue(row, key)
    if (v?.value == null && v?.code) return codeWords(v.code)
    return readingText(
      v?.value,
      v?.unit,
      reducerName(key) === "count" ? undefined : plot.precision
    )
  }

  function tooltip(b: number): ReactNode {
    const bucket = buckets[b]
    const head = (
      <p className="font-medium">{windowWords(bucket.start, bucket.end)}</p>
    )
    if (shown.length === 1) {
      const [only] = shown
      const row = only.series.points[b]?.row
      return (
        <>
          {head}
          {row ? (
            <SeriesDetail
              plot={only.plot}
              dayOffsets={Boolean(stateOf(only.plot)?.together.length)}
              row={row}
              valueText={valueText}
              sourceText={sourceText}
            />
          ) : (
            <p className="mt-1 text-muted-foreground">Nothing measured.</p>
          )}
        </>
      )
    }
    const byPlot = drawn
      .map((plot) => ({
        plot,
        rows: shown.filter((d) => d.plot === plot && d.series.points[b]?.row),
      }))
      .filter((p) => p.rows.length)
    // A recorded-local date's rows say at which offsets it was lived.
    const offsets = local
      ? distinct(
          byPlot.flatMap(({ rows }) =>
            rows.map((d) => {
              const row = d.series.points[b].row!
              return offsetWords(row.start, row.end)
            })
          )
        )
      : []
    return (
      <>
        {head}
        {offsets.length > 0 && (
          <p className="text-faint">
            {byPlot.some(({ plot }) => stateOf(plot)?.together.length)
              ? "Day’s UTC offset"
              : "UTC offset"}{" "}
            {offsets.join("; ")}
          </p>
        )}
        {byPlot.length === 0 && (
          <p className="mt-1 text-muted-foreground">Nothing measured.</p>
        )}
        {byPlot.map(({ plot, rows }) => (
          <div key={plot.metric} className="mt-2 border-t border-border pt-2">
            <p className="mb-1 text-faint">
              {plot.name}, {lowerFirst(choiceWords(plot))}
            </p>
            <dl className="grid grid-cols-[auto_1fr_auto] items-center gap-x-2 gap-y-0.5">
              {rows.map((d) => {
                const row = d.series.points[b].row!
                return (
                  <div key={d.key} className="contents">
                    <Swatch mark={plot.mark} color={d.color} />
                    <dt className="truncate text-muted-foreground">
                      {d.label}
                    </dt>
                    <dd className="text-right font-medium text-foreground tabular-nums">
                      {valueText(plot, row, plot.pickedKey)}
                    </dd>
                  </div>
                )
              })}
            </dl>
          </div>
        ))}
      </>
    )
  }

  const notes = chartNotes(states, all, {
    technical,
    sourceText,
    aboutText: (value) => obs.nameOf(value, obs.pins.about),
  })

  return (
    <section data-slot="timeline-lane" aria-label={names} className={className}>
      {title && (
        <SectionHead
          title={names}
          hint={
            drawn.length === 1 ? (
              <ShapeWord shape={drawn[0].shape} />
            ) : undefined
          }
        />
      )}
      <div className="flex flex-col gap-1.5">
        {states.map((state) => (
          <PlotHead
            key={state.plot.metric}
            state={state}
            empty={empty(state)}
            series={all.filter((d) => d.plot === state.plot)}
            named={!title || drawn.length > 1}
            axis={
              twoAxes && !empty(state)
                ? `${sideOf(state.plot) === "left" ? "Left" : "Right"} axis, ${unitWords(state.plot.unit)}`
                : undefined
            }
            legend={legend}
            hidden={hidden}
            onToggle={toggle}
            technical={technical}
            onReducer={onReducer}
            onUnit={onUnit}
            onStatistic={onStatistic}
          />
        ))}
      </div>
      {refusal && (
        <p
          role="note"
          data-slot="chart-refusal"
          className="mt-2 rounded-md bg-panel px-2.5 py-2 text-[12.5px] text-muted-foreground"
        >
          {refusal}
        </p>
      )}
      {notes.map((note) => (
        <p key={note} className="mt-1.5 text-[12.5px] text-faint">
          {note}
        </p>
      ))}
      {reads.length === 0 ? null : landed.length === 0 &&
        reads.some((s) => s.pending) ? (
        <Skeleton className="mt-2 h-[250px] w-full" />
      ) : landed.length === 0 ? null : (
        <div className="mt-2" onFocus={() => setFocusFor(undefined)}>
          <TimelineChart
            // A new window is a new chart: no hover or drag carries over.
            key={windowKey}
            autoFocus={focusFor === windowKey}
            buckets={buckets}
            series={chartSeries}
            domain={range}
            size={size}
            label={label}
            tooltip={tooltip}
            onZoom={(next, keyboard) => {
              setFocusFor(keyboard ? `${next.start}-${next.end}` : undefined)
              onZoom(next)
            }}
            units={{
              left: left ? unitWords(left.unit) : undefined,
              right: right ? unitWords(right.unit) : undefined,
            }}
          />
        </div>
      )}
      {states.map(({ plot, rows, together }) =>
        rows.length > 0 ? (
          <RollupTable
            key={plot.metric}
            rows={rows}
            reducers={answerReducers(rows)}
            groupKey={plot.groupKey}
            groupCell={groupCell(plot)}
            sourceText={sourceText}
            label={`${plot.name} in numbers`}
            caption={drawn.length > 1 ? plot.name : undefined}
            precision={plot.precision}
            local={Boolean(plot.local)}
            offsetsAcross={
              together.length ? listWords([plot.name, ...together]) : undefined
            }
            fallbackZone={plot.read?.aggregate.bucket?.fallbackTimezone}
          />
        ) : null
      )}
      {technical &&
        requests.map(({ read, plots: asked, query }) => (
          <RequestDetails
            key={asked.map((p) => p.metric).join("\n")}
            read={read}
            caption={
              drawn.length > 1 || asked.length > 1
                ? listWords(asked.map((p) => p.name))
                : undefined
            }
            inexact={
              Boolean(read.aggregate.metrics) &&
              !query.isPlaceholderData &&
              query.data?.exact === false
            }
          />
        ))}
    </section>
  )
}

/** Every series the chart's metrics answer, each with its label, its axis
 * and its colour: a colour is a series' place among every value its
 * metric could show, so a toggle or a filter never repaints the ones that
 * stay. A category metric's series come in its own order. */
function drawnSeries(
  states: readonly PlotState[],
  buckets: readonly Bucket[],
  sideOf: (plot: Plot) => AxisSide,
  labelOf: (plot: Plot, s: Series) => string,
  local?: BucketSize
): Drawn[] {
  const universe: string[] = []
  const all: Drawn[] = []
  for (const state of states) {
    const { plot } = state
    const answered = seriesOn(
      buckets,
      state.rows,
      plot.pickedKey,
      plot.groupKey,
      local
    )
    const own = universeOf(
      plot,
      answered.map((s) => s.key)
    )
    for (const v of own) universe.push(seriesKey(plot.metric, v))
    // Categories stack and list in the metric's own order.
    const ordered =
      plot.groupKey === "code"
        ? [...answered].sort((a, b) => own.indexOf(a.key) - own.indexOf(b.key))
        : answered
    for (const s of ordered) {
      all.push({
        key: seriesKey(plot.metric, s.key),
        plot,
        series: s,
        label: labelOf(plot, s),
        color: "",
        side: sideOf(plot),
        stale: state.stale,
      })
    }
  }
  const colors = seriesColors(universe)
  for (const d of all) {
    d.color =
      colors.get(d.key) ?? (d.series.key ? "var(--faint)" : "var(--series-1)")
  }
  return all
}

/** One metric's row above the chart: its name and form, its axis, its
 * reducer and unit, where its read stands, its legend (each entry a toggle),
 * why it reads nothing, and a failed read's reason. */
function PlotHead({
  state,
  empty,
  series,
  named,
  axis,
  legend,
  hidden,
  onToggle,
  technical,
  onReducer,
  onUnit,
  onStatistic,
}: {
  state: PlotState
  /** Its read landed with no rows while the chart draws other metrics. */
  empty: boolean
  /** The metric's own series, in legend order. */
  series: Drawn[]
  /** Name the metric: the chart's title does not already. */
  named: boolean
  /** Which axis it reads on, where the chart has two. */
  axis?: string
  legend: boolean
  hidden: ReadonlySet<string>
  onToggle: (key: string) => void
  technical: boolean
  onReducer: (metric: string, value: string) => void
  onUnit: (metric: string, value: string) => void
  onStatistic: (metric: string, value: string) => void
}) {
  const { plot, rows, pending, failed, error, stale, held } = state
  return (
    <div
      key={plot.metric}
      role="group"
      aria-label={plot.name}
      data-slot="chart-plot"
      className="flex flex-col gap-1"
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        {named ? (
          <span className="flex items-baseline gap-1.5 text-[13px] font-medium">
            {plot.name}
            <ShapeWord
              shape={plot.shape}
              className="text-[12.5px] font-normal text-faint"
            />
          </span>
        ) : null}
        {axis && <span className="text-[12.5px] text-faint">{axis}</span>}
        <Picker
          label="Show"
          options={plot.reducerMenu.map((o) => ({
            value: o.value,
            label: o.label,
            ...(o.hint ? { hint: o.hint } : {}),
            ...(o.disabled ? { disabled: true } : {}),
          }))}
          selected={[plot.reducer]}
          onChange={([next]) =>
            onReducer(
              plot.metric,
              next && next !== plot.reducers[0] ? next : ""
            )
          }
        />
        {plot.statistic && (
          <Picker
            label="Statistic"
            options={plot.statistics.map((value) => ({
              value,
              label: statisticWords(value),
            }))}
            selected={[plot.statistic]}
            onChange={([next]) =>
              onStatistic(
                plot.metric,
                next && next !== plot.statistics[0] ? next : ""
              )
            }
          />
        )}
        {plot.units.length > 1 && (
          <Picker
            label="Unit"
            options={plot.units.map((value) => ({
              value,
              label: unitLabel(value),
            }))}
            selected={[plot.outputUnit ?? plot.stored ?? ""]}
            onChange={([next]) =>
              onUnit(plot.metric, next && next !== plot.stored ? next : "")
            }
          />
        )}
        {pending && <span className="text-[12.5px] text-faint">Reading…</span>}
        {stale && <span className="text-[12.5px] text-faint">Updating…</span>}
      </div>
      {technical && plot.spec && <IdText value={plot.spec.metricPath} copy />}
      {legend && (
        <ul
          aria-label={`Legend, ${plot.name}`}
          className="flex flex-wrap gap-x-1 gap-y-0.5 text-[12.5px]"
        >
          {series.map((d) => {
            const off = hidden.has(d.key)
            return (
              <li key={d.key}>
                <button
                  type="button"
                  aria-pressed={!off}
                  title={off ? `Show ${d.label}` : `Hide ${d.label}`}
                  className={cn(
                    "inline-flex h-6 cursor-pointer items-center gap-1.5 rounded-[5px] px-1.5 text-muted-foreground outline-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
                    off && "text-faint line-through"
                  )}
                  onClick={() => onToggle(d.key)}
                >
                  <Swatch
                    mark={plot.mark}
                    color={off ? "var(--border-strong)" : d.color}
                  />
                  {d.label}
                </button>
              </li>
            )
          })}
        </ul>
      )}
      {plot.blocked && (
        <p className="text-[12.5px] text-muted-foreground">{plot.blocked}</p>
      )}
      {empty && (
        <p className="text-[12.5px] text-muted-foreground">
          Nothing measured in this range.
        </p>
      )}
      {!pending &&
        !failed &&
        !held &&
        rows.length > 0 &&
        series.every((d) => d.series.points.every((p) => p.value === null)) && (
          <p className="text-[12.5px] text-muted-foreground">
            No {lowerFirst(choiceWords(plot))} in this range.
            {plot.fromReason === "some-summaries"
              ? " Its streams mix raw points and vendor summaries: group by source to see each, or pick another under Show."
              : ""}
          </p>
        )}
      {failed && error && (
        <div className="text-[12.5px] text-destructive">
          <p>
            {error instanceof ApiError && error.status === 422
              ? "The server refused this read: "
              : "This read failed: "}
            {error.message}
          </p>
          {error instanceof ApiError &&
            error.problems.map((problem) => <p key={problem}>{problem}</p>)}
        </div>
      )}
    </div>
  )
}

/** What a chart says under itself about each metric's read: how sources
 * are ranked, rows it leaves out, streams it reads beyond the selection,
 * how it reads a vendor's summaries, and (with Technical details on) an
 * inexact answer to a request of the metric alone. Each is prefixed with
 * its metric where the chart draws several. */
function chartNotes(
  states: readonly PlotState[],
  all: readonly Drawn[],
  words: {
    technical: boolean
    sourceText: (value: string) => string
    aboutText: (value: string) => string
  }
): string[] {
  const notes: string[] = []
  for (const { plot, exact, held } of states) {
    const own: string[] = []
    // A ranked read uses the reader's order. Otherwise the server decides
    // it, so the sources are listed by name, implying no order.
    const names = plot.sourceOrder.map(words.sourceText)
    const overlapping = [
      ...(plot.ranked ? names : [...names].sort((a, b) => a.localeCompare(b))),
      ...(plot.sourceless ? ["values naming no source"] : []),
    ]
    if (plot.groupKey !== "source" && plot.spec && overlapping.length > 1) {
      const [first, ...rest] = overlapping
      own.push(
        plot.ranked
          ? `Where sources overlap, ${first} is used first, then ${rest.join(", then ")}.`
          : `Where ${listWords(overlapping)} overlap, the server ranks ${plot.sourceless ? "the sources" : "them"} by each source record’s priorities for this metric, then by record path${plot.sourceless ? ", and values naming no source last" : ""}.`
      )
    }
    const dropped = all
      .filter((d) => d.plot === plot)
      .reduce((n, d) => n + d.series.dropped, 0)
    if (dropped > 0 && !held) {
      own.push(
        `${dropped === 1 ? "1 row shares" : `${dropped} rows share`} a bucket with another row of the same series and ${dropped === 1 ? "is" : "are"} left out of the chart; the table below lists every row.`
      )
    }
    if (plot.mixedIn.length) {
      const named = plot.mixedIn.map((s) =>
        [
          s.source ? words.sourceText(s.source) : "no source",
          s.channel ? ` (${s.channel})` : "",
          s.about ? `, about ${words.aboutText(s.about)}` : "",
        ].join("")
      )
      own.push(
        `Also reads ${listWords(named)}, which the selection leaves out: a read narrows sources, things and channels each on its own.`
      )
    }
    if (plot.shape === "coded") own.push("Stacked by category.")
    if (plot.fromReason === "summaries-only") {
      own.push("Read from the vendor's summaries, the only values it has.")
    }
    if (plot.statistic && plot.statisticLeftOut.length) {
      own.push(
        `Reads the summaries’ ${lowerFirst(statisticWords(plot.statistic))} alone; ${listWords(plot.statisticLeftOut.map(words.sourceText))} ${plot.statisticLeftOut.length === 1 ? "keeps" : "keep"} another statistic and ${plot.statisticLeftOut.length === 1 ? "is" : "are"} left out.`
      )
    }
    if (plot.fromReason === "raw-only" && plot.summarySources.length) {
      own.push(
        `Reads raw points alone: no vendor summary takes ${lowerFirst(choiceWords(plot))}, so ${listWords(plot.summarySources.map(words.sourceText))}’s summaries are left out.`
      )
    }
    if (words.technical && plot.fromReason === "some-summaries") {
      own.push(
        `Read with prefer-raw: raw points where they cover a bucket, a vendor summary where they do not, because ${listWords(plot.summarySources.map(words.sourceText))} ${plot.summarySources.length === 1 ? "keeps" : "keep"} summaries, which a raw read leaves out.`
      )
    }
    // `exact` is false whenever a float reduction was involved, so an
    // average is never exact: a developer's fact, not an everyday warning.
    if (words.technical && !held && exact === false) {
      own.push("Not exact: a float reduction or a rounded conversion.")
    }
    for (const note of own) {
      notes.push(states.length > 1 ? `${plot.name}: ${lowerFirst(note)}` : note)
    }
  }
  return notes
}

/** A series' colour in the shape it is drawn: a stroke for a line, a square
 * for bars or a stack. */
function Swatch({ mark, color }: { mark: ChartMark; color: string }) {
  return (
    <span
      aria-hidden
      data-mark={mark}
      className={cn(
        "shrink-0",
        mark === "line" ? "h-0.5 w-3 rounded-full" : "size-2.5 rounded-[3px]"
      )}
      style={{ background: color }}
    />
  )
}

/** One series at one bucket in full: every value its row carries, the
 * points, the covered time, the sources and where the row came from. */
function SeriesDetail({
  plot,
  dayOffsets,
  row,
  valueText,
  sourceText,
}: {
  plot: Plot
  /** The row's offsets are the day's across the metrics its request
   * reads, not this metric's own. */
  dayOffsets: boolean
  row: AggregateRow
  valueText: (plot: Plot, row: AggregateRow, key: string) => string
  sourceText: (id: string) => string
}) {
  return (
    <div className="mt-2 border-t border-border pt-2">
      <dl className="grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
        {(row.values ?? []).map((v, i) => (
          <TipLine
            key={`${reducerKey(v.reducer)}-${v.code ?? ""}-${i}`}
            label={
              v.rolling !== undefined
                ? `${reducerLabel(v.reducer)} of ${v.rolling}`
                : reducerLabel(v.reducer)
            }
            value={valueText(plot, row, reducerKey(v.reducer))}
            strong
          />
        ))}
        <TipLine label="Points" value={row.count.toLocaleString("en")} />
        {plot.local && (
          <TipLine
            label={dayOffsets ? "Day’s UTC offset" : "UTC offset"}
            value={offsetWords(row.start, row.end)}
          />
        )}
        {row.offsetless ? (
          <TipLine
            label="No offset"
            value={`${row.offsetless.toLocaleString("en")}, placed in ${plot.read?.aggregate.bucket?.fallbackTimezone ?? "your zone"}`}
          />
        ) : null}
        {row.coverage && (
          <TipLine label="Covered" value={coverageWords(row.coverage)} />
        )}
        {namedSources(row).length ? (
          <TipLine
            label={namedSources(row).length === 1 ? "Source" : "Sources"}
            value={namedSources(row).map(sourceText).join(", ")}
          />
        ) : null}
        {row.from && row.from !== "raw" && (
          <TipLine
            label="From"
            value={row.from === "summaries" ? "Vendor summary" : row.from}
          />
        )}
      </dl>
      {row.count === 0 && (
        <p className="mt-1 text-muted-foreground">
          No reading here; an earlier one still holds.
        </p>
      )}
    </div>
  )
}

function TipLine({
  label,
  value,
  strong = false,
}: {
  label: string
  value: string
  strong?: boolean
}) {
  return (
    <>
      <dt className="text-faint">{label}</dt>
      <dd
        className={cn(
          "text-right tabular-nums",
          strong && "font-medium text-foreground"
        )}
      >
        {value}
      </dd>
    </>
  )
}

/** One request of a chart, for one metric or several, to run again with
 * `curl`: the `filter` and `aggregate` parameters as JSON, and the whole
 * call. A request of several metrics answers `exact` once for all of them,
 * so it says here, not on a metric's row, that its answer is not exact. */
function RequestDetails({
  read,
  caption,
  inexact = false,
}: {
  read: AggregateRead
  caption?: string
  inexact?: boolean
}) {
  const json = JSON.stringify(read, null, 2)
  const curl = aggregateCurl(read)
  return (
    <div data-slot="lane-request" className="mt-3 rounded-md bg-panel p-2.5">
      <div className="flex items-center gap-2 text-[12.5px] text-faint">
        <span>{caption ? `Request for ${caption}` : "Request"}</span>
        <CopyButton value={json} label="Copy the request" />
        <span className="ml-3">As curl</span>
        <CopyButton value={curl} label="Copy the curl command" />
      </div>
      {inexact && (
        <p className="mt-1.5 text-[12.5px] text-faint">
          Not exact: a float reduction or a rounded conversion in at least one
          of its metrics.
        </p>
      )}
      <CodeBlock source={json} lang="json" className="mt-1.5" />
    </div>
  )
}
