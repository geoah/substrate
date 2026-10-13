/** One chart of several metrics, each one aggregate read (`lib/overlay.ts`
 * plans them): the series of one unit share the left axis, a second unit
 * reads on the right, and a metric of a third unit is left out with a
 * sentence saying which and what to deselect. A level is a line with its
 * band, an amount or a count is bars, a category is bars stacked per code;
 * one crosshair and one tooltip list every series at the hovered bucket
 * with its unit.
 *
 * Above the chart, one row per metric: its name and axis, its reducer and
 * unit, where its read stands (reading, refreshing, refused), and its
 * legend, each entry a toggle that hides or shows that series. Under the
 * chart, each metric's numbers, and with Technical details on, each read
 * itself. */

import { useState, type ReactNode } from "react"
import { useQueries, useQueryClient } from "@tanstack/react-query"

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
  type AggregateRead,
} from "@/lib/api/observations"
import {
  ApiError,
  type AggregateResult,
  type AggregateRow,
} from "@/lib/api/types"
import {
  codeWords,
  readingText,
  reducerKey,
  reducerLabel,
  reducerName,
} from "@/lib/observations"
import {
  coverageWords,
  windowWords,
  type BucketSize,
  type TimeRange,
} from "@/lib/observations-time"
import { latestRead } from "@/lib/observations-reads"
import {
  answerReducers,
  bucketsOf,
  namedSources,
  rowValue,
  seriesOn,
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

function lowerFirst(text: string): string {
  return text.charAt(0).toLowerCase() + text.slice(1)
}

export function OverlayChart({
  obs,
  plots,
  range,
  size,
  title = false,
  onReducer,
  onUnit,
  onZoom,
  className,
}: {
  obs: Observations
  /** The metrics, in the order they take the axes. */
  plots: Plot[]
  range: TimeRange
  size: BucketSize
  /** Head the chart with its metrics' names (the timeline page); a panel
   * that names itself leaves it off. */
  title?: boolean
  onReducer: (metric: string, value: string) => void
  onUnit: (metric: string, value: string) => void
  onZoom: (range: TimeRange) => void
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set())
  // The window a keyboard zoom asked for: its chart takes the focus once
  // drawn, since the zoomed-out one it had goes while the new one is read.
  const [focusFor, setFocusFor] = useState<string>()
  const windowKey = `${range.start}-${range.end}`
  const plan = assignAxes(plots)
  const drawn = plots.filter((p) => !plan.leftOut.includes(p))
  const sideOf = (plot: Plot): AxisSide =>
    plan.axes.find((a) => a.plots.includes(plot))?.side ?? "left"
  const twoAxes = plan.axes.length > 1
  const refusal = leftOutWords(plan)

  const client = useQueryClient()
  const queries = useQueries({
    queries: drawn.map((plot) => {
      const read = plot.read ?? latestRead()
      return {
        ...aggregateQueryOptions(read, obs.observedIds),
        // A changed bucket, filter or reducer the cached answer carries
        // holds the frame while it loads (`heldAnswer`).
        placeholderData: (prev?: AggregateResult) =>
          prev ?? heldAnswer(client, read, obs.observedIds),
        enabled: obs.knows(plot.metric) && Boolean(plot.read),
      }
    }),
  })
  const states = drawn.map((plot, i) => {
    const query = queries[i]
    const reads = Boolean(plot.read)
    return {
      plot,
      query,
      rows: reads ? plotRows(plot, query.data?.rows ?? []) : NO_ROWS,
      pending: reads && query.isPending,
      failed: reads && query.isError,
      stale: reads && query.isFetching && !query.isPending,
      // An earlier read's answer drawn while this one loads: what it lacks
      // or drops says nothing of this read.
      held: reads && query.isPlaceholderData,
    }
  })

  const buckets = bucketsOf(states.map((s) => s.rows))
  const sourceText = (value: string) => obs.nameOf(value, obs.pins.source)
  function labelOf(plot: Plot, s: Series): string {
    if (plot.groupKey === "source" && s.value) {
      const name = sourceText(s.value)
      return s.channel ? `${name} (${s.channel})` : name
    }
    if (plot.groupKey === "about") {
      return s.value ? obs.nameOf(s.value, obs.pins.about) : "You"
    }
    if (plot.groupKey === "code" && s.value) return codeWords(s.value)
    return plot.name
  }

  // Every value each metric could show, in order, so a toggle or a filter
  // never repaints the series that stay.
  const universe: string[] = []
  const all: Drawn[] = []
  for (const state of states) {
    const { plot } = state
    const answered = seriesOn(
      buckets,
      state.rows,
      plot.pickedKey,
      plot.groupKey
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
  const shown = all.filter((d) => !hidden.has(d.key))
  // A level's band is drawn where its metric shows one line: under several,
  // the washes pile up over the lines they belong to.
  const lonely = (plot: Plot) =>
    shown.filter((d) => d.plot === plot).length === 1
  const chartSeries: ChartSeries[] = shown.map((d) => ({
    key: d.key,
    value: d.series.value,
    label: d.label,
    color: d.color,
    points: d.series.points,
    mark: d.plot.mark,
    band: d.plot.band && lonely(d.plot),
    axis: d.side,
    stack: d.plot.mark === "stack" ? d.plot.metric : undefined,
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
  const label = drawn
    .map((p) => `${p.name}, ${reducerLabel(p.reducer)}`)
    .join("; ")

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
    return (
      <>
        {head}
        {byPlot.length === 0 && (
          <p className="mt-1 text-muted-foreground">Nothing measured.</p>
        )}
        {byPlot.map(({ plot, rows }) => (
          <div key={plot.metric} className="mt-2 border-t border-border pt-2">
            <p className="mb-1 text-faint">
              {plot.name}, {lowerFirst(reducerLabel(plot.reducer))}
            </p>
            <dl className="grid grid-cols-[auto_1fr_auto] items-center gap-x-2 gap-y-0.5">
              {rows.map((d) => {
                const row = d.series.points[b].row!
                return (
                  <div key={d.key} className="contents">
                    <span
                      aria-hidden
                      className="h-0.5 w-3 shrink-0 rounded-full"
                      style={{ background: d.color }}
                    />
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

  const notes: string[] = []
  for (const { plot, query, held } of states) {
    const own: string[] = []
    const ranked = [
      ...plot.sourceOrder.map(sourceText),
      ...(plot.sourceless ? ["values naming no source"] : []),
    ]
    if (plot.groupKey !== "source" && plot.spec && ranked.length > 1) {
      const [first, ...rest] = ranked
      own.push(
        `Where sources overlap, ${first} is used first, then ${rest.join(", then ")}.`
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
          s.source ? sourceText(s.source) : "no source",
          s.channel ? ` (${s.channel})` : "",
          s.about ? `, about ${obs.nameOf(s.about, obs.pins.about)}` : "",
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
    if (technical && plot.fromReason === "some-summaries") {
      own.push(
        `Read with prefer-raw: raw points where they cover a bucket, a vendor summary where they do not, because ${listWords(plot.summarySources.map(sourceText))} ${plot.summarySources.length === 1 ? "keeps" : "keep"} summaries, which a raw read leaves out.`
      )
    }
    // `exact` is false whenever a float reduction was involved, so an
    // average is never exact: a developer's fact, not an everyday warning.
    if (technical && !held && query.data?.exact === false) {
      own.push("Not exact: a float reduction or a rounded conversion.")
    }
    for (const note of own) {
      notes.push(drawn.length > 1 ? `${plot.name}: ${lowerFirst(note)}` : note)
    }
  }

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
        {states.map(({ plot, query, rows, pending, failed, stale, held }) => (
          <div
            key={plot.metric}
            role="group"
            aria-label={plot.name}
            data-slot="chart-plot"
            className="flex flex-col gap-1"
          >
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
              {!title || drawn.length > 1 ? (
                <span className="flex items-baseline gap-1.5 text-[13px] font-medium">
                  {plot.name}
                  <ShapeWord
                    shape={plot.shape}
                    className="text-[12.5px] font-normal text-faint"
                  />
                </span>
              ) : null}
              {twoAxes && (
                <span className="text-[12.5px] text-faint">
                  {`${sideOf(plot) === "left" ? "Left" : "Right"} axis, ${unitWords(plot.unit)}`}
                </span>
              )}
              <Picker
                label="Show"
                options={plot.reducers.map((value) => ({
                  value,
                  label: reducerLabel(value),
                }))}
                selected={[plot.reducer]}
                onChange={([next]) =>
                  onReducer(
                    plot.metric,
                    next && next !== plot.reducers[0] ? next : ""
                  )
                }
              />
              {plot.units.length > 1 && (
                <Picker
                  label="Unit"
                  options={plot.units.map((value) => ({
                    value,
                    label: unitLabel(value),
                  }))}
                  selected={[plot.outputUnit ?? plot.stored ?? ""]}
                  onChange={([next]) =>
                    onUnit(
                      plot.metric,
                      next && next !== plot.stored ? next : ""
                    )
                  }
                />
              )}
              {pending && (
                <span className="text-[12.5px] text-faint">Reading…</span>
              )}
              {stale && (
                <span className="text-[12.5px] text-faint">Updating…</span>
              )}
            </div>
            {technical && plot.spec && (
              <IdText value={plot.spec.metricPath} copy />
            )}
            {legend && (
              <ul
                aria-label={`Legend, ${plot.name}`}
                className="flex flex-wrap gap-x-1 gap-y-0.5 text-[12.5px]"
              >
                {all
                  .filter((d) => d.plot === plot)
                  .map((d) => {
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
                          onClick={() => toggle(d.key)}
                        >
                          <span
                            aria-hidden
                            className={cn(
                              "shrink-0",
                              plot.mark === "line"
                                ? "h-0.5 w-3 rounded-full"
                                : "size-2.5 rounded-[3px]"
                            )}
                            style={{
                              background: off
                                ? "var(--border-strong)"
                                : d.color,
                            }}
                          />
                          {d.label}
                        </button>
                      </li>
                    )
                  })}
              </ul>
            )}
            {plot.blocked && (
              <p className="text-[12.5px] text-muted-foreground">
                {plot.blocked}
              </p>
            )}
            {!pending &&
              !failed &&
              !held &&
              rows.length > 0 &&
              all
                .filter((d) => d.plot === plot)
                .every((d) =>
                  d.series.points.every((p) => p.value === null)
                ) && (
                <p className="text-[12.5px] text-muted-foreground">
                  No {lowerFirst(reducerLabel(plot.reducer))} in this range.
                  {plot.fromReason === "some-summaries"
                    ? " Its streams mix raw points and vendor summaries: group by source to see each, or pick another under Show."
                    : plot.fromReason === "summaries-only"
                      ? " A vendor summary answers only the statistic it keeps: pick another under Show."
                      : ""}
                </p>
              )}
            {failed && query.error && (
              <div className="text-[12.5px] text-destructive">
                <p>
                  {query.error instanceof ApiError && query.error.status === 422
                    ? "The server refused this read: "
                    : "This read failed: "}
                  {query.error.message}
                </p>
                {query.error instanceof ApiError &&
                  query.error.problems.map((problem) => (
                    <p key={problem}>{problem}</p>
                  ))}
              </div>
            )}
          </div>
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
        <div className="mt-2">
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
      {states.map(({ plot, rows }) =>
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
          />
        ) : null
      )}
      {technical &&
        states.map(({ plot }) =>
          plot.read ? (
            <RequestDetails
              key={plot.metric}
              read={plot.read}
              caption={drawn.length > 1 ? plot.name : undefined}
            />
          ) : null
        )}
    </section>
  )
}

/** One series at one bucket in full: every value its row carries, the
 * points, the covered time, the sources and where the row came from. */
function SeriesDetail({
  plot,
  row,
  valueText,
  sourceText,
}: {
  plot: Plot
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

/** The request behind one metric of a chart, to run again with `curl`: the
 * `filter` and `aggregate` parameters as JSON, and the whole call. */
function RequestDetails({
  read,
  caption,
}: {
  read: AggregateRead
  caption?: string
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
      <CodeBlock source={json} lang="json" className="mt-1.5" />
    </div>
  )
}
