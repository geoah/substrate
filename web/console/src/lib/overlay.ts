/** Several metrics in one chart, the way Grafana's time series panel draws
 * them: the series of one unit share a y axis, the first unit reads on the
 * left and a second on the right, and a third unit never joins the chart.
 * The timeline page splits its metrics into as few charts as hold them
 * this way; the streams page's panel draws one chart and names what it
 * leaves out.
 *
 * Each metric is still one aggregate read (`laneRead`): a chart composes
 * its metrics' reads. Everything here is a pure function of the registry's
 * metric records, the streams, the reader's choices and the answers. */

import {
  DEFAULT_PRECISION,
  channelKey,
  MAX_BUCKETS,
  SUMMARY_REDUCERS,
  bucketEstimate,
  bucketOf,
  formOf,
  laneGroupKey,
  laneRead,
  laneReducers,
  reducerKey,
  reducerOptions,
  refPathOf,
  sameRef,
  shapeOf,
  type AggregateFrom,
  type AggregateGroupKey,
  type AggregateRead,
  type BucketSize,
  type Form,
  type GroupChoice,
  type LaneSpec,
  type MetricInfo,
  type ObservedPins,
  type ReadingShape,
  type Stream,
  type TimeRange,
} from "@/lib/observations"
import type { AggregateRow } from "@/lib/api/types"
import { listWords } from "@/lib/tools"
import { unitChoices } from "@/lib/unit-choices"
import { unitSymbol } from "@/lib/units"

/** How a series is drawn: a line, bars, or bars stacked per category. */
export type ChartMark = "line" | "bars" | "stack"

export type AxisSide = "left" | "right"

/** The axis a count reads on: a count carries no unit, and two counts
 * share an axis whatever they count. */
export const COUNT_UNIT = "{count}"

/** How a chart draws a reducer's answer: categories stack, totals and
 * counts are bars, levels and rates are a line. */
export function markOf(shape: ReadingShape, reducer: string): ChartMark {
  if (shape === "coded") return "stack"
  if (shape === "amount") return "bars"
  if (["sum", "count", "increase"].includes(reducer)) return "bars"
  return "line"
}

/** The unit a reducer's values come back in, which is the axis they read
 * on: none for a count, a duration for `timeInState` and `durationIn` (the
 * picked duration unit, seconds otherwise), the unit per second for a
 * `rate`, degrees for a circular mean, and otherwise the output unit (the
 * picked one, else the stored one). docs/api.md, "Units". */
export function answerUnitOf(
  reducer: string,
  unit: string | undefined
): string {
  if (reducer === "count") return COUNT_UNIT
  if (reducer === "timeInState" || reducer.startsWith("durationIn")) {
    return unit && /^(s|min|h|d|wk|ms)$/.test(unit) ? unit : "s"
  }
  if (reducer === "circularMean") return "deg"
  if (reducer === "rate") return unit ? `${unit}/s` : "/s"
  return unit ?? ""
}

/** An axis's unit in words, as its caption and the legend say it. */
export function unitWords(unit: string): string {
  if (unit === COUNT_UNIT) return "count"
  return (unit && unitSymbol(unit)) || "no unit"
}

/** One y axis of a chart: its unit, its side, and the metrics on it. */
export interface Axis<T> {
  unit: string
  side: AxisSide
  plots: T[]
}

/** Which axis each metric takes, in the order the metrics come: the first
 * unit on the left, the second on the right, and every metric of a third
 * unit left out. */
export interface AxisPlan<T> {
  axes: Axis<T>[]
  leftOut: T[]
}

export function assignAxes<T extends { unit: string }>(
  plots: readonly T[]
): AxisPlan<T> {
  const axes: Axis<T>[] = []
  const leftOut: T[] = []
  for (const plot of plots) {
    const axis = axes.find((a) => a.unit === plot.unit)
    if (axis) axis.plots.push(plot)
    else if (axes.length < 2) {
      axes.push({
        unit: plot.unit,
        side: axes.length ? "right" : "left",
        plots: [plot],
      })
    } else leftOut.push(plot)
  }
  return { axes, leftOut }
}

/** The metrics as as few charts as hold them at two units each: the units
 * in the order they first come, two to a chart, every metric in the chart
 * of its unit and in its own order there. */
export function splitCharts<T extends { unit: string }>(
  plots: readonly T[]
): T[][] {
  const units: string[] = []
  for (const p of plots) if (!units.includes(p.unit)) units.push(p.unit)
  const charts: T[][] = []
  for (let i = 0; i < units.length; i += 2) {
    const pair = units.slice(i, i + 2)
    charts.push(plots.filter((p) => pair.includes(p.unit)))
  }
  return charts
}

/** What a chart says when a third unit is left out: which metrics, in
 * which unit, and what to deselect to draw them. Undefined where nothing
 * is left out. */
export function leftOutWords<T extends { unit: string; name: string }>(
  plan: AxisPlan<T>
): string | undefined {
  if (!plan.leftOut.length) return undefined
  const named = (plots: readonly T[]) => {
    const units: string[] = []
    for (const p of plots) if (!units.includes(p.unit)) units.push(p.unit)
    return listWords(
      units.map(
        (u) =>
          `${listWords(plots.filter((p) => p.unit === u).map((p) => p.name))} (${unitWords(u)})`
      )
    )
  }
  const one = plan.leftOut.length === 1
  const drawn = plan.axes.map((a) => named(a.plots))
  return (
    `Up to two units in one chart: ${named(plan.leftOut)} ${one ? "is" : "are"} left out. ` +
    `Deselect ${one ? "it" : "them"}, or deselect ${drawn.join(" or ")} to draw ${one ? "it" : "them"} here.`
  )
}

// ── one metric's plot ──────────────────────────────────────────────────────

/** What planning a plot reads of the page's discovery (`useObservations`). */
export interface PlotSource {
  pins: ObservedPins
  /** Every stream, newest first. */
  streams: readonly Stream[]
  metricOf: (value: string) => MetricInfo | undefined
  metricName: (value: string) => string
  metricPath: (value: string) => string | undefined
}

/** The choices one chart's metrics are read under. */
export interface PlotScope {
  range: TimeRange
  size: BucketSize
  timezone?: string
  group: GroupChoice
  /** The reader's pick; absent, each metric reads what its streams hold
   (`fromOf`). */
  from?: AggregateFrom
  /** The sources and things chosen, by id or path; empty for every one. */
  sources: readonly string[]
  abouts: readonly string[]
  /** The channels chosen; absent or empty for every one. */
  channels?: readonly string[]
  /** This metric's picked reducer and output unit, where the reader picked. */
  reducer?: string
  unit?: string
  /** The streams the plot is drawn from, newest first: a selection's, else
   * every stream of the metric. They decide the form it is read in. */
  streams?: readonly Stream[]
}

/** One metric of a chart, planned before its read answers. */
export interface Plot {
  metric: string
  name: string
  shape: ReadingShape
  form?: Form
  reducer: string
  /** The reducers its form allows, the default first. */
  reducers: string[]
  /** The unit its answer reads in, which is its axis (`assignAxes`,
   * `splitCharts`). */
  unit: string
  /** The picked output unit, where it is not the stored one. */
  outputUnit?: string
  stored?: string
  /** The units it may be shown in; one or none offers no choice. */
  units: string[]
  precision: number
  codes?: string[]
  mark: ChartMark
  /** A level draws its lowest-to-highest band. */
  band: boolean
  spec?: LaneSpec
  read?: AggregateRead
  groupKey?: AggregateGroupKey
  /** The column the chart draws, a reducer key. */
  pickedKey: string
  /** Why the plot asks nothing, in words; absent where it reads. */
  blocked?: string
  /** The sources in the order a priority read prefers them. */
  sourceOrder: string[]
  /** The read takes a stream naming no source too, after every named one. */
  sourceless: boolean
  /** The series a selection names, where the read, grouped by source,
   * answers others too: the chart draws these alone (`plotRows`). */
  only?: string[]
  /** Streams a selection leaves out that the read takes anyway and folds
   * into a selected series: a read narrows sources, things and channels
   * each on its own, and no `in` names a stream with none. */
  mixedIn: Stream[]
  /** The series the plot could show grouped by source (each source's
   * channels apart) or by about, for colours that hold while a filter or a
   * toggle hides some. */
  sources: string[]
  abouts: string[]
  /** Where it reads from, and why, where the reader picked nothing:
   * `summaries` because the metric has nothing else, `prefer-raw` because
   * some of its streams keep summaries and others raw points. */
  from: AggregateFrom
  fromReason?: "summaries-only" | "some-summaries"
  /** The sources among its streams that keep vendor summaries. */
  summarySources: string[]
}

function distinct(values: (string | undefined)[]): string[] {
  return [...new Set(values.filter((v): v is string => Boolean(v)))]
}

/** Where a metric reads from when the reader picked nothing, from the
 * forms of the streams it draws: summaries where every one keeps vendor
 * summaries, raw where a bucket is covered and the summary otherwise
 * (`prefer-raw`) where some do, raw points where none does. A raw read
 * takes no summary, so a summary-only stream would draw nothing. */
export function fromOf(streams: readonly Pick<Stream, "form">[]): {
  from: AggregateFrom
  reason?: "summaries-only" | "some-summaries"
} {
  const summaries = streams.filter((s) => s.form === "summary").length
  if (!summaries) return { from: "raw" }
  if (summaries === streams.length) {
    return { from: "summaries", reason: "summaries-only" }
  }
  return { from: "prefer-raw", reason: "some-summaries" }
}

/** One metric's plot: its form, reducer, unit, mark, axis, and the read it
 * makes, or why it makes none. The same arithmetic the timeline used per
 * chart: the budget counts groups times buckets, guessing the groups
 * before the answer says. */
export function planPlot(
  obs: PlotSource,
  metric: string,
  scope: PlotScope
): Plot {
  const info = obs.metricOf(metric)
  const name = obs.metricName(metric)
  const every = obs.streams.filter((s) => sameRef(s.metric, metric))
  const own = scope.streams
    ? scope.streams.filter((s) => sameRef(s.metric, metric))
    : every
  const palette = scope.streams ? own : every
  // The address may spell a record by id or by path; the plot uses the
  // spelling its streams carry. The chosen sources keep the reader's order,
  // which a priority read prefers them in.
  const sources = distinct(own.map((s) => s.source))
  const chosenSources = distinct(
    scope.sources.map((s) => sources.find((l) => sameRef(l, s)))
  )
  const abouts = distinct(own.map((s) => s.about))
  const chosenAbouts = abouts.filter((l) =>
    scope.abouts.some((a) => sameRef(l, a))
  )
  const channels = scope.channels ?? []
  // The streams the read selects, a selection's and any others its filter
  // takes: the filter narrows each key apart.
  const selected = every.filter(
    (s) =>
      (!chosenSources.length || chosenSources.includes(s.source ?? "")) &&
      (!chosenAbouts.length || chosenAbouts.includes(s.about ?? "")) &&
      (!channels.length || channels.includes(s.channel ?? ""))
  )
  const sourceless = selected.some((s) => !s.source)
  // Newest first. A stream that keeps raw points says how the metric is
  // measured; a vendor summary only where every stream keeps one.
  const newest = own.find((s) => s.form !== "summary") ?? own[0]
  const shape = shapeOf(info, newest)
  const form = formOf(info, newest)
  const auto = fromOf(selected)
  const from: AggregateFrom = scope.from ?? auto.from
  // Past a raw read, a bucket may be served by a summary, and the server
  // refuses a reducer a summary does not take.
  const summaries = from !== "raw" && selected.some((s) => s.form === "summary")
  const reducers = reducerOptions(shape, form).filter(
    (r) => !summaries || SUMMARY_REDUCERS.includes(r)
  )
  const reducer =
    scope.reducer && reducers.includes(scope.reducer)
      ? scope.reducer
      : reducers[0]
  // The server answers in the picked unit, else the metric's, else its
  // first stream's, which need not be the newest: where the metric names
  // no unit, the read names the one the axis is captioned with.
  const stored =
    info?.unit ??
    (
      selected.find((s) => s.unit && s.form !== "summary") ??
      selected.find((s) => s.unit)
    )?.unit
  const units = unitChoices(stored, info?.measures)
  const picked =
    scope.unit && units.includes(scope.unit) ? scope.unit : undefined
  const unit = picked !== stored ? picked : undefined
  const asked = picked ?? (info?.unit ? undefined : stored)
  const codes = info?.codeSet
  // The groups the read makes, as it groups: grouped by source, each
  // source's channels apart; by about, the owner's streams among them.
  const groups =
    shape === "coded"
      ? codes?.length || 4
      : scope.group === "source"
        ? new Set(selected.map((s) => channelKey(s.source ?? "", s.channel)))
            .size || 1
        : scope.group === "about"
          ? new Set(selected.map((s) => s.about ?? "")).size || 1
          : 1
  const estimate = bucketEstimate(scope.range, scope.size, groups)
  const metricPath = obs.metricPath(metric)
  const sourceOrder = chosenSources.length
    ? chosenSources
    : distinct(selected.map((s) => s.source))

  // Grouped by source, the answer keeps each source's channels apart, so
  // the rows of a stream the selection leaves out can be dropped; any other
  // grouping folds it into a selected series.
  const seriesOf = (s: Stream) =>
    channelKey(
      refPathOf(s.source, obs.pins.source) ?? s.source ?? "",
      s.channel
    )
  const extra = scope.streams
    ? selected.filter((s) => !own.some((o) => o.key === s.key))
    : []
  const bySource = shape !== "coded" && scope.group === "source"
  const named = own.map(seriesOf)
  const only =
    bySource && extra.some((s) => !named.includes(seriesOf(s)))
      ? distinct(named)
      : undefined
  const mixedIn = extra.filter((s) => !only || named.includes(seriesOf(s)))

  const spec: LaneSpec | undefined = metricPath
    ? {
        metricPath,
        shape,
        reducer,
        ...(asked ? { unit: asked } : {}),
        from,
        range: scope.range,
        bucket: bucketOf(scope.size, scope.timezone),
        group: scope.group,
        sources: chosenSources.map((s) => refPathOf(s, obs.pins.source) ?? s),
        sourceOrder,
        ...(sourceless ? { sourceless } : {}),
        about: chosenAbouts.map((a) => refPathOf(a, obs.pins.about) ?? a),
        ...(channels.length ? { channels: [...channels] } : {}),
      }
    : undefined

  const blocked = !metricPath
    ? "No record names this metric, so it can’t be read here."
    : estimate > MAX_BUCKETS
      ? `This read would make about ${estimate.toLocaleString("en")} buckets counting every group, more than the ${MAX_BUCKETS.toLocaleString("en")} one read may hold. Pick a larger bucket or a shorter range.`
      : scope.sources.length > 0 && chosenSources.length === 0
        ? "None of the chosen sources wrote this metric."
        : scope.abouts.length > 0 && chosenAbouts.length === 0
          ? "This metric is about none of the chosen things."
          : undefined

  const mark = markOf(shape, reducer)
  return {
    metric,
    name,
    shape,
    form,
    reducer,
    reducers,
    unit: answerUnitOf(reducer, unit ?? stored),
    outputUnit: unit,
    stored,
    units,
    precision: info?.displayPrecision ?? DEFAULT_PRECISION,
    codes,
    mark,
    band: mark === "line" && shape === "gauge",
    spec,
    read: spec && !blocked ? laneRead(spec) : undefined,
    groupKey: spec ? laneGroupKey(spec) : undefined,
    pickedKey: spec ? reducerKey(laneReducers(spec)[0]) : reducer,
    blocked,
    sourceOrder,
    sourceless,
    ...(only ? { only } : {}),
    mixedIn,
    // Every stream it could draw, whichever are drawn, so a source filter
    // or a legend toggle never repaints the series that stay; a selection
    // is its own palette. Spelled as the answer's groups are, record paths.
    sources: distinct(
      palette.map(
        (s) =>
          s.source &&
          channelKey(
            refPathOf(s.source, obs.pins.source) ?? s.source,
            s.channel
          )
      )
    ),
    abouts: distinct(
      palette.map(
        (s) => s.about && (refPathOf(s.about, obs.pins.about) ?? s.about)
      )
    ),
    from,
    ...(scope.from ? {} : auto.reason ? { fromReason: auto.reason } : {}),
    summarySources: distinct(
      selected.filter((s) => s.form === "summary").map((s) => s.source)
    ),
  }
}

/** The rows of one plot's answer the chart draws and tabulates: every
 * row, but those of a series the selection leaves out (`Plot.only`). */
export function plotRows(
  plot: Pick<Plot, "only">,
  rows: readonly AggregateRow[]
): readonly AggregateRow[] {
  const only = plot.only
  if (!only) return rows
  return rows.filter((r) =>
    only.includes(channelKey(r.group?.source ?? "", r.group?.channel))
  )
}

/** Every value one plot's series could take, in a stable order: its
 * sources, its things (the owner first), its categories (the metric's own
 * order, then any the answer names besides), or its one series. */
export function universeOf(
  plot: Pick<Plot, "groupKey" | "sources" | "abouts" | "codes">,
  answered: readonly string[]
): string[] {
  if (plot.groupKey === "source") return [...plot.sources].sort()
  if (plot.groupKey === "about") return ["", ...[...plot.abouts].sort()]
  if (plot.groupKey === "code") {
    const codes = plot.codes ?? []
    return [...codes, ...[...answered].filter((k) => !codes.includes(k)).sort()]
  }
  return [""]
}

/** The key one series is told apart by across a chart's metrics. */
export function seriesKey(metric: string, value: string): string {
  return `${metric}\u0000${value}`
}

/** The colour of each series across a chart: its place among every value
 * the chart's plots could show, so a toggle or a filter never repaints the
 * ones that stay. Past the eighth, the quiet grey. */
export function seriesColors(universe: readonly string[]): Map<string, string> {
  const out = new Map<string, string>()
  universe.forEach((key, at) =>
    out.set(key, at < 8 ? `var(--series-${at + 1})` : "var(--faint)")
  )
  return out
}
