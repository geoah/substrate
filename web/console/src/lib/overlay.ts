/** Several metrics in one chart, the way Grafana's time series panel draws
 * them: the series of one unit share a y axis, the first unit reads on the
 * left and a second on the right, and a third unit never joins the chart.
 * The timeline page splits its metrics into as few charts as hold them
 * this way; the streams page's panel draws one chart and names what it
 * leaves out.
 *
 * Each metric plans its own read (`laneRead`), and a chart sends the reads
 * that differ only in their metric's own choices as one several-metric read
 * (`sharedReads`). Everything here is a pure function of the registry's
 * metric records, the streams, the reader's choices and the answers. */

import type { AggregateRead } from "@/lib/api/observations"
import type {
  AggregateFrom,
  AggregateGroupKey,
  AggregateRow,
} from "@/lib/api/types"
import {
  bandedReducer,
  channelKey,
  DEFAULT_PRECISION,
  distinct,
  statisticChoices,
  formOf,
  rawOnly,
  reducerChoiceWords,
  reducerKey,
  reducerOptions,
  refPathOf,
  sameRef,
  shapeOf,
  summaryReducers,
  type Form,
  type MetricInfo,
  type ObservedPins,
  type ReadingShape,
  type Stream,
} from "@/lib/observations"
import {
  bucketEstimate,
  bucketOf,
  dayBucket,
  MAX_BUCKETS,
  MAX_BUCKETS_CAP,
  readRange,
  type BucketSize,
  type TimeRange,
} from "@/lib/observations-time"
import { rowsInRange } from "@/lib/observations-answer"
import {
  laneGroupKey,
  laneRead,
  laneReducers,
  type GroupChoice,
  type LaneSpec,
} from "@/lib/observations-reads"
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
  if (reducer.startsWith("durationIn")) return "bars"
  return "line"
}

/** The duration units a time in a range may be shown in, minutes first. */
const DURATION_UNITS = ["min", "s", "h"]

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
  /** The reader's zone: calendar buckets are cut in it, and a recorded-local
   * read places a value written without a UTC offset by it. */
  timezone?: string
  /** Day, week and month buckets read where each value was recorded (the
   * recorded-local calendar), not in the reader's zone. */
  recordedLocal?: boolean
  group: GroupChoice
  /** The reader's pick; absent, each metric reads what its streams hold
   (`fromOf`). */
  from?: AggregateFrom
  /** The sources and things chosen, by id or path; empty for every one. */
  sources: readonly string[]
  /** The chosen sources are an order the reader set (the timeline's Source
   * filter), which a priority read prefers them in. A selection's sources
   * are no order: the server ranks them by their records' `priorities`. */
  sourcesRanked?: boolean
  abouts: readonly string[]
  /** The channels chosen; absent or empty for every one. */
  channels?: readonly string[]
  /** This metric's picked reducer, output unit and summary statistic,
   * where the reader picked. */
  reducer?: string
  unit?: string
  statistic?: string
  /** The streams the plot is drawn from, newest first: a selection's, else
   * every stream of the metric. They decide the form it is read in. */
  streams?: readonly Stream[]
}

/** One entry of a plot's Show menu. */
export interface ReducerOption {
  /** The reducer key (`reducerKey`). */
  value: string
  label: string
  /** Picking it reads raw points alone: no vendor summary takes it. */
  raw?: boolean
  /** Why it reads raw points alone, or why it cannot be picked. */
  hint?: string
  disabled?: boolean
}

/** One metric of a chart, planned before its read answers. */
export interface Plot {
  metric: string
  name: string
  shape: ReadingShape
  reducer: string
  /** The reducers it may ask for, the default first. */
  reducers: string[]
  /** The Show menu: every reducer its forms take, a raw-only one marked as
   * such, and one the reader's pick of where to read from rules out
   * disabled with the reason. */
  reducerMenu: ReducerOption[]
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
  /** The buckets its read is expected to make, counting every group
   * (`bucketEstimate`): a read several metrics share adds theirs up
   * (`sharedReads`). */
  estimate: number
  groupKey?: AggregateGroupKey
  /** The read groups by channel too, so each channel is a series. */
  byChannel: boolean
  /** The column the chart draws, a reducer key. */
  pickedKey: string
  /** Why the plot asks nothing, in words; absent where it reads. */
  blocked?: string
  /** The sources the read takes: in the order a priority read prefers
   * them where `ranked`, else in the streams' order. */
  sourceOrder: string[]
  /** The reader set the order (`sources.order`); otherwise the server ranks
   * the sources by their records' `priorities` for the metric. */
  ranked: boolean
  /** The read takes a stream naming no source too, which the server ranks
   * as `LaneSpec.sourceless` says. */
  sourceless: boolean
  /** The series a selection names, where the read, grouped by source,
   * about or channel, answers others too: the chart draws these alone
   * (`plotRows`). */
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
   * some of its streams keep summaries and others raw points, `raw` because
   * the reducer picked takes no summary. */
  from: AggregateFrom
  fromReason?: "summaries-only" | "some-summaries" | "raw-only"
  /** The sources among its streams that keep vendor summaries. */
  summarySources: string[]
  /** The statistics those summaries keep, the default first: a row reads
   * one, so where they keep several the reader picks (`statistic`). */
  statistics: string[]
  /** The statistic its summaries are read in, where they keep several and
   * the read takes them: sent as `filter.properties.statistic`. */
  statistic?: string
  /** The sources whose summaries keep none of that statistic, which the
   * read leaves out. */
  statisticLeftOut: string[]
  /** Where its buckets are recorded-local days, weeks or months: their size
   * and the range the reader asked for, which the read widens
   * (`readRange`). */
  local?: { size: BucketSize; range: TimeRange }
}

/** Whether a stream keeps vendor summaries: its newest point is one, or
 * the latest read answered a statistic for it. A stream whose newest point
 * is raw may keep summaries too (Garmin's nightly HRV average and maximum
 * beside its five-minute trace), and a read taking them must name one. */
export function keepsSummaries(
  stream: Pick<Stream, "form" | "statistics">
): boolean {
  return stream.form === "summary" || Boolean(stream.statistics?.length)
}

/** Where a metric reads from when the reader picked nothing, from the
 * forms of the streams it draws: summaries where every one keeps vendor
 * summaries alone, raw where a bucket is covered and the summary otherwise
 * (`prefer-raw`) where some keep summaries, raw points where none does. A
 * raw read takes no summary, so a summary-only stream would draw nothing. */
export function fromOf(
  streams: readonly Pick<Stream, "form" | "statistics">[]
): {
  from: AggregateFrom
  reason?: "summaries-only" | "some-summaries"
} {
  if (!streams.some(keepsSummaries)) return { from: "raw" }
  if (streams.every((s) => s.form === "summary")) {
    return { from: "summaries", reason: "summaries-only" }
  }
  return { from: "prefer-raw", reason: "some-summaries" }
}

/** One metric's plot: its form, reducer, unit, mark, axis, and the read it
 * makes, or why it makes none. The budget counts groups times buckets, the
 * groups worked out from the streams before the answer says. */
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
  const askedFrom: AggregateFrom = scope.from ?? auto.from
  // A row reads one statistic of the summaries (the server refuses two), so
  // where they keep several the read names one: the reader's, else the
  // average, the one kept, or the vendor's own.
  const summaryStreams = selected.filter(keepsSummaries)
  const statistics = statisticChoices(
    summaryStreams.flatMap((s) => s.statistics ?? [])
  )
  const statistic =
    scope.statistic && statistics.includes(scope.statistic)
      ? scope.statistic
      : statistics[0]
  const reducerMenu = menuOf(shape, form, info, {
    statistic,
    from: askedFrom,
    // Past a raw read, a bucket may be served by a summary, and the server
    // refuses a reducer a summary does not take.
    summaries: summaryStreams.length > 0,
    // Where the reader picked nothing and some streams hold raw points, a
    // reducer no summary takes reads those alone.
    switchable: !scope.from && selected.some((s) => s.form !== "summary"),
  })
  const reducers = reducerMenu.filter((o) => !o.disabled).map((o) => o.value)
  const reducer =
    scope.reducer && reducers.includes(scope.reducer)
      ? scope.reducer
      : reducers[0]
  const rawPick = Boolean(reducerMenu.find((o) => o.value === reducer)?.raw)
  const from: AggregateFrom = rawPick ? "raw" : askedFrom
  const narrowsStatistic = from !== "raw" && statistics.length > 1
  const statisticLeftOut = narrowsStatistic
    ? distinct(
        summaryStreams
          .filter((s) => s.statistics && !s.statistics.includes(statistic!))
          .map((s) => s.source)
      )
    : []
  // The server answers in the picked unit, else the metric's, else its
  // first stream's, which need not be the newest: where the metric names
  // no unit, the read names the one the axis is captioned with. A time in
  // a range is a duration, in minutes unless the reader picks another.
  const stored =
    info?.unit ??
    (
      selected.find((s) => s.unit && s.form !== "summary") ??
      selected.find((s) => s.unit)
    )?.unit
  const timed = reducer.startsWith("durationIn")
  const units = timed ? DURATION_UNITS : unitChoices(stored, info?.measures)
  const picked =
    scope.unit && units.includes(scope.unit)
      ? scope.unit
      : timed
        ? DURATION_UNITS[0]
        : undefined
  const unit = timed || picked !== stored ? picked : undefined
  const asked = picked ?? (info?.unit ? undefined : stored)
  const codes = info?.codeSet
  // The key a chart's series are told apart by, as `laneGroupKey` has it,
  // and whether the read groups by channel too: grouped by source always,
  // otherwise where the streams it takes are on more than one channel (no
  // channel counting as one), which no `in` can tell apart.
  const groupKey: AggregateGroupKey | undefined =
    shape === "coded"
      ? "code"
      : scope.group === "metric"
        ? undefined
        : scope.group
  const channelsTaken = new Set(selected.map((s) => s.channel ?? "")).size
  const byChannel = groupKey === "source" || channelsTaken > 1
  // The part of a series a stream decides: its source or its thing where
  // the chart is grouped by one, and its channel where the read groups by
  // channel. Spelled as the answer's groups are, record paths.
  const partOf = (s: Pick<Stream, "source" | "about" | "channel">) => {
    const base =
      groupKey === "source"
        ? (refPathOf(s.source, obs.pins.source) ?? s.source ?? "")
        : groupKey === "about"
          ? (refPathOf(s.about, obs.pins.about) ?? s.about ?? "")
          : ""
    return byChannel ? channelKey(base, s.channel) : base
  }
  // The groups the read makes, as it groups: a category per code, each
  // source's or thing's channels apart where it groups by channel.
  const groups =
    (shape === "coded" ? codes?.length || 4 : 1) *
    (new Set(selected.map(partOf)).size || 1)
  const local = Boolean(scope.recordedLocal) && dayBucket(scope.size)
  const range = readRange(scope.range, scope.size, local)
  // A night lands on the day it ends, so each day holds one night and a
  // week or a month is the sum of its days. The server places only spans
  // so: a gauge's or a counter's points are instants, and it refuses them.
  const wake =
    info?.category === "sleep" &&
    dayBucket(scope.size) &&
    selected.length > 0 &&
    selected.every((s) => s.form === "amount" || s.form === "summary")
  const estimate = bucketEstimate(range, scope.size, groups)
  const metricPath = obs.metricPath(metric)
  const sourceOrder = chosenSources.length
    ? chosenSources
    : distinct(selected.map((s) => s.source))
  const ranked = Boolean(scope.sourcesRanked) && chosenSources.length > 0

  // Where the read groups by what a stream decides (its source, its thing
  // or its channel), a stream the selection leaves out answers a series of
  // its own unless it shares a selected one, so its rows can be dropped; a
  // stream sharing a selected series folds into that line.
  const apart = groupKey === "source" || groupKey === "about" || byChannel
  const extra = scope.streams
    ? selected.filter((s) => !own.some((o) => o.key === s.key))
    : []
  // The owner's series and a sourceless stream's are "", which is kept.
  const named = [...new Set(own.map(partOf))]
  const only =
    apart && extra.some((s) => !named.includes(partOf(s))) ? named : undefined
  const mixedIn = extra.filter((s) => !only || named.includes(partOf(s)))

  const spec: LaneSpec | undefined = metricPath
    ? {
        metricPath,
        shape,
        reducer,
        ...(asked ? { unit: asked } : {}),
        from,
        range,
        bucket: {
          ...bucketOf(scope.size, scope.timezone, local),
          ...(wake ? { place: "end" } : {}),
        },
        group: scope.group,
        sources: chosenSources.map((s) => refPathOf(s, obs.pins.source) ?? s),
        sourceOrder,
        ...(ranked ? { ranked } : {}),
        ...(sourceless ? { sourceless } : {}),
        about: chosenAbouts.map((a) => refPathOf(a, obs.pins.about) ?? a),
        ...(channels.length ? { channels: [...channels] } : {}),
        ...(narrowsStatistic ? { statistic } : {}),
        ...(byChannel && groupKey !== "source" ? { byChannel } : {}),
        // Past the default, the read asks for the cap: the estimate is a
        // guess (a month as 30 days, four codes where none are listed),
        // and the cap is what holds a read back.
        ...(estimate > MAX_BUCKETS ? { maxBuckets: MAX_BUCKETS_CAP } : {}),
      }
    : undefined

  const blocked = !metricPath
    ? "No record names this metric, so it can’t be read here."
    : estimate > MAX_BUCKETS_CAP
      ? `This read would make about ${estimate.toLocaleString("en")} buckets counting every group, more than the ${MAX_BUCKETS_CAP.toLocaleString("en")} one read may hold. Pick a larger bucket or a shorter range.`
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
    reducer,
    reducers,
    reducerMenu,
    unit: answerUnitOf(reducer, unit ?? stored),
    outputUnit: unit,
    stored,
    units,
    precision: info?.displayPrecision ?? DEFAULT_PRECISION,
    codes,
    mark,
    band: mark === "line" && shape === "gauge" && bandedReducer(reducer),
    spec,
    read: spec && !blocked ? laneRead(spec) : undefined,
    estimate,
    groupKey: spec ? laneGroupKey(spec) : undefined,
    byChannel,
    pickedKey: spec ? reducerKey(laneReducers(spec)[0]) : reducer,
    blocked,
    sourceOrder,
    ranked,
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
    ...(scope.from
      ? {}
      : rawPick
        ? { fromReason: "raw-only" as const }
        : auto.reason
          ? { fromReason: auto.reason }
          : {}),
    summarySources: distinct(summaryStreams.map((s) => s.source)),
    statistics,
    ...(narrowsStatistic ? { statistic } : {}),
    statisticLeftOut,
    ...(local ? { local: { size: scope.size, range: scope.range } } : {}),
  }
}

/** A plot's Show menu: every reducer its form takes (`reducerOptions`).
 * Where the read may take a vendor summary, those a summary takes come
 * first and the rest read raw points alone: picking one switches the read
 * to `raw` where the reader left where to read from automatic and some
 * stream holds raw points, and is disabled with the reason otherwise. A
 * metric whose streams all keep summaries reads in a summary's terms. */
function menuOf(
  shape: ReadingShape,
  form: Form | undefined,
  info: MetricInfo | undefined,
  read: {
    from: AggregateFrom
    summaries: boolean
    switchable: boolean
    statistic?: string
  }
): ReducerOption[] {
  const option = (value: string): ReducerOption => ({
    value,
    label: reducerChoiceWords(value),
  })
  const own = reducerOptions(shape, form, info, read.statistic)
  if (read.from === "raw" || !read.summaries || shape === "summary") {
    return own.map(option)
  }
  const takes = summaryReducers(read.statistic, read.from)
  const shared = own.filter((k) => !rawOnly(k) && takes.includes(k))
  return [
    ...shared.map(option),
    ...own
      .filter((k) => !shared.includes(k))
      .map((k) =>
        read.switchable && rawOnly(k)
          ? { ...option(k), raw: true, hint: "Raw points only" }
          : {
              ...option(k),
              disabled: true,
              hint: rawOnly(k)
                ? "Needs raw points: set Read from to Raw"
                : "Adds only summaries of a sum or a count",
            }
      ),
  ]
}

/** The rows of one plot's answer the chart draws and tabulates: every
 * row, but those of a series the selection leaves out (`Plot.only`) and,
 * in the recorded-local calendar, those of a date outside the range the
 * reader asked for. */
export function plotRows(
  plot: Pick<Plot, "only" | "groupKey" | "byChannel" | "local">,
  rows: readonly AggregateRow[]
): readonly AggregateRow[] {
  const dated = plot.local
    ? rowsInRange(rows, plot.local.size, plot.local.range)
    : rows
  const only = plot.only
  if (!only) return dated
  return dated.filter((r) => {
    const base =
      plot.groupKey === "source"
        ? (r.group?.source ?? "")
        : plot.groupKey === "about"
          ? (r.group?.about ?? "")
          : ""
    return only.includes(
      plot.byChannel ? channelKey(base, r.group?.channel) : base
    )
  })
}

/** Every value one plot's series could take, in a stable order: its
 * sources, its things (the owner first), its categories (the metric's own
 * order), or its one series, then any other the answer names (a channel's
 * series beside them). */
export function universeOf(
  plot: Pick<Plot, "groupKey" | "sources" | "abouts" | "codes">,
  answered: readonly string[]
): string[] {
  const base =
    plot.groupKey === "source"
      ? [...plot.sources].sort()
      : plot.groupKey === "about"
        ? ["", ...[...plot.abouts].sort()]
        : plot.groupKey === "code"
          ? [...(plot.codes ?? [])]
          : [""]
  return [...base, ...[...answered].filter((k) => !base.includes(k)).sort()]
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
