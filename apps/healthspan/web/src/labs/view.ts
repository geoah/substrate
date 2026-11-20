// What the lab screens show for a subject, computed once from the index:
// its series (one per channel), the finding per series, the statuses the
// filters read, and what a card shows.

import {
  DEFAULT_CADENCE_MONTHS,
  PANELS,
  assessmentFor,
  catalogueOrder,
  directionPhrase,
  forSex,
  greenBands,
  panelFor,
  targetBand,
  targetSpan,
  type CatalogueEntry,
} from "../catalogue"
import type { Sex } from "../kinds"
import { clipBands } from "../lib/chart"
import {
  channelLabel,
  formatDate,
  formatMonth,
  formatRate,
  rangeText,
  rangeWithUnit,
} from "../lib/format"
import {
  latestLine,
  markOf,
  seriesLegend,
  sourceMarkers,
  sourcesByCount,
  type LatestLine,
  type LegendItem,
  type SourceMark,
} from "../lib/sourceMarkers"
import {
  emptySpan,
  inDomain,
  type EmptySpan,
  type TimeDomain,
} from "../lib/timeDomain"
import type { TrendMarker } from "../lib/trendMath"
import {
  normalBands,
  offTarget,
  overdue,
  rangeStatus,
  resultFlag,
  seriesFinding,
  seriesStatuses,
  trendFit,
  trendTone,
  type LabStatus,
  type TrendFit,
} from "../rules/labs"
import type { Finding } from "../rules/types"
import type { Tone } from "../ui/tone"
import type { UnitForm } from "../catalogue"
import { moved, twinsOf } from "./labIndex"
import { resultParts, resultText } from "./result"
import type { LabIndex, LabResult, Series, Specimen, Subject } from "./types"
import { convert } from "./units"

/** Regions in the order a regional subject lists them; the whole body
 * (no channel) first. */
const CHANNEL_ORDER = [
  "",
  "total",
  "trunk",
  "arms",
  "legs",
  "android",
  "gynoid",
]

export interface LabView {
  /** The subject's code: an entry's, or a metric id. */
  code: string
  subject: Subject
  name: string
  panel: string
  important: boolean
  /** The unit its points are in, and their decimals. */
  unit?: string
  precision?: number
  /** Every series: one, or one per channel. */
  series: Series[]
  /** The series a card shows: the one with the strongest finding, else
   * the first channel in a fixed order. */
  lead: Series
  /** The strongest finding across the series. */
  finding?: Finding
  findings: Finding[]
  /** The entry as it applies to the owner's sex. */
  entry?: CatalogueEntry
  /** The statuses holding on any series, for the Status filter. */
  statuses: Set<LabStatus>
  /** The card's status (word, tone, edge) from the lead series. */
  status: CardStatus
}

/** Every subject with a result, as the screens show it for `sex`. */
export function labViews(index: LabIndex, today: Date, sex?: Sex): LabView[] {
  const bySubject = new Map<string, Series[]>()
  for (const s of index.series.values()) {
    const list = bySubject.get(s.code) ?? []
    list.push(s)
    bySubject.set(s.code, list)
  }
  const out: LabView[] = []
  for (const [code, series] of bySubject) {
    const subject = index.subjects.get(code)
    if (!subject) continue
    series.sort((a, b) => {
      const ia = CHANNEL_ORDER.indexOf(a.channel ?? "")
      const ib = CHANNEL_ORDER.indexOf(b.channel ?? "")
      return (
        (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) ||
        (a.channel ?? "").localeCompare(b.channel ?? "")
      )
    })
    // A subject whose every result is shown apart still has a card.
    const charted = series.filter((s) => s.results.length || s.apart.length)
    if (!charted.length) continue
    const entry = subject.entry ? forSex(subject.entry, sex) : undefined
    const findings = charted
      .map((s) => seriesFinding(index, s, entry, today))
      .filter((f): f is Finding => !!f)
      .sort((a, b) => a.rank - b.rank || (b.distance ?? 0) - (a.distance ?? 0))
    const finding = findings[0]
    const lead =
      (finding && charted.find((s) => s.key === finding.subject)) ??
      charted.find((s) => s.results.length) ??
      charted[0]
    out.push({
      code,
      subject,
      name: subject.name,
      panel: panelFor(entry, subject.category),
      important: !!entry?.important,
      unit: subject.unit,
      precision: subject.precision,
      series: charted,
      lead,
      finding,
      findings,
      entry,
      statuses: new Set(
        charted.flatMap((x) => [...seriesStatuses(x, entry, today)])
      ),
      status: seriesStatus(lead, entry, today),
    })
  }
  return out
}

/** Order inside a panel: the catalogue's, then what sits outside it by
 * name. */
export function compareInPanel(
  a: Pick<LabView, "code" | "name">,
  b: Pick<LabView, "code" | "name">
): number {
  const ia = catalogueOrder(a.code) ?? Number.POSITIVE_INFINITY
  const ib = catalogueOrder(b.code) ?? Number.POSITIVE_INFINITY
  if (ia !== ib) return ia < ib ? -1 : 1
  return a.name.localeCompare(b.name) || a.code.localeCompare(b.code)
}

/** The views grouped by panel in panel order, each panel in catalogue
 * order. */
export function byPanel<V extends Pick<LabView, "code" | "name" | "panel">>(
  views: readonly V[]
): { panel: string; views: V[] }[] {
  return panelOrder(views.map((v) => v.panel)).map((panel) => ({
    panel,
    views: views.filter((v) => v.panel === panel).sort(compareInPanel),
  }))
}

const STATUS_RANK: Record<CardStatusKey, number> = {
  range: 0,
  target: 1,
  overdue: 2,
  ok: 3,
  none: 4,
}

/** The order of the one grid shown with "Group by panel" off: card status
 * (range, target, overdue, ok, none), then panel order, then catalogue
 * order. */
export function flatOrder<
  V extends Pick<LabView, "code" | "name" | "panel" | "status">,
>(views: readonly V[]): V[] {
  const panels = panelOrder(views.map((v) => v.panel))
  return [...views].sort(
    (a, b) =>
      STATUS_RANK[a.status.key] - STATUS_RANK[b.status.key] ||
      panels.indexOf(a.panel) - panels.indexOf(b.panel) ||
      compareInPanel(a, b)
  )
}

/** Panels in the catalogue's order, then the category panels by name. */
export function panelOrder(names: Iterable<string>): string[] {
  const set = new Set(names)
  const rest = [...set].filter((n) => !PANELS.includes(n)).sort()
  return [...PANELS.filter((p) => set.has(p)), ...rest]
}

/** The card's `data-status`: the strongest status of the lead series. */
export type CardStatusKey = LabStatus | "none"

export interface StatusBadge {
  word: string
  tone: Tone
}

export interface CardStatus {
  key: CardStatusKey
  /** The status word beside the name; absent when the subject has neither
   * a range nor a target. */
  badge?: StatusBadge
  /** Show a separate "Overdue" badge: rule 3 fires and the status badge is
   * not already "Overdue". */
  overdueBadge: boolean
  /** The 3 px leading edge, or none. */
  edge?: "danger" | "warning" | "info"
}

/** The status word, tone and edge of a series' newest result: out of the
 * lab's range, else off target (named by the band it lands in), else
 * overdue, else the green band's label, "In range" or "Uncertain". */
export function seriesStatus(
  s: Pick<Series, "results">,
  entry: CatalogueEntry | undefined,
  today: Date
): CardStatus {
  const newest = s.results[s.results.length - 1]
  if (!newest) return { key: "none", overdueBadge: false }
  const rs = rangeStatus(newest)
  const late = overdue(s.results, entry, today) !== undefined
  if (rs?.status === "out")
    return {
      key: "range",
      badge: { word: rs.word, tone: "danger" },
      overdueBadge: late,
      edge: "danger",
    }
  const off = offTarget(newest, entry)
  if (off) {
    const band = entry?.target?.find(
      (b) => b.tone !== "success" && b.label === off.label
    )
    return {
      key: "target",
      badge: { word: off.label, tone: band?.tone ?? "warning" },
      overdueBadge: late,
      edge: "warning",
    }
  }
  if (late)
    return {
      key: "overdue",
      badge: { word: "Overdue", tone: "info" },
      overdueBadge: false,
      edge: "info",
    }
  if (rs?.status === "uncertain")
    return {
      key: "ok",
      badge: { word: "Uncertain", tone: "neutral" },
      overdueBadge: false,
    }
  const green =
    newest.value !== undefined ? targetBand(entry, newest.value) : undefined
  if (green?.tone === "success")
    return {
      key: "ok",
      badge: { word: green.label, tone: "success" },
      overdueBadge: false,
    }
  if (rs?.status === "in")
    return {
      key: "ok",
      badge: { word: "In range", tone: "success" },
      overdueBadge: false,
    }
  return { key: "none", overdueBadge: false }
}

/** A chart point: one numeric result, its source's marker and its flag. */
export interface ChartPoint {
  at: string
  value: number
  marker: TrendMarker
  /** The source id; matches a legend item's `key`. */
  series: string
  /** `danger` out of the lab's range, `warning` off target, else none. */
  tone?: "danger" | "warning"
  result: LabResult
}

/** Every numeric result as chart points, oldest first. With `withTarget`
 * false (values in another form's unit) no point is off target. */
export function chartPoints(
  results: readonly LabResult[],
  markers: Map<string, SourceMark>,
  entry: CatalogueEntry | undefined,
  withTarget = true
): ChartPoint[] {
  return results.flatMap((r) => {
    if (r.value === undefined) return []
    const mark = markOf(markers, r)
    const flag = resultFlag(r, entry, withTarget)
    const tone = flag?.tone === "neutral" ? undefined : flag?.tone
    return [
      {
        at: r.at,
        value: r.value,
        marker: mark.marker,
        series: mark.id,
        tone,
        result: r,
      },
    ]
  })
}

/** The specimen a shown result stands for. */
export function specimenOf(
  series: Pick<Series, "results" | "specimens">,
  r: LabResult
): Specimen | undefined {
  const i = series.results.indexOf(r)
  return i < 0 ? undefined : series.specimens[i]
}

/** A specimen's twins as they print after its value: "6.0%". */
export function twinText(
  index: LabIndex,
  s: Specimen | undefined
): string | undefined {
  if (!s) return undefined
  const twin = twinsOf(index, s)[0]
  if (!twin) return undefined
  const info = index.metrics.get(twin.metric)
  return resultText(twin, info?.precision, info?.unit)
}

export interface TrendText {
  /** The chip's text: "0.5%/month", or "Too few results for a trend". */
  label: string
  /** The chip's tooltip. */
  tooltip: string
  /** The chip's accessible name, in words. */
  aria: string
}

/** What the trend chip says. The rate prints without a sign: the arrow
 * carries the direction and the colour whether it is the better one. */
export function trendText(
  fit: TrendFit | undefined,
  entry: CatalogueEntry | undefined
): TrendText {
  if (!fit)
    return {
      label: "Too few results for a trend",
      tooltip: "A trend needs three results in this range.",
      aria: "Too few results for a trend: it needs three results in this range",
    }
  const rate = formatRate(fit.ratePerMonth)
  const pct = rate.replace("%/month", "")
  const goal =
    entry?.trendGoal === "lower"
      ? "Lower is better for this metric."
      : entry?.trendGoal === "higher"
        ? "Higher is better for this metric."
        : "The catalogue sets no direction for this metric."
  const moves =
    fit.direction === "up"
      ? `rises by ${pct}%`
      : fit.direction === "down"
        ? `falls by ${pct}%`
        : `changes by ${pct}%`
  const tone = trendTone(fit, entry)
  const said =
    fit.direction === "flat"
      ? `flat, ${pct} percent of the average per month`
      : `${fit.direction === "up" ? "rising" : "falling"} ${pct} percent of the average per month, ${
          tone === "success"
            ? "the better direction"
            : tone === "danger"
              ? "the worse direction"
              : "no direction set"
        }`
  return {
    label: rate,
    tooltip: `Best-fit line through the ${fit.n} results in this range. The value ${moves} of its average each month. ${goal}`,
    aria: `Trend: ${said}`,
  }
}

export interface MetricCardData {
  view: LabView
  /** The series the card shows (the lead channel). */
  series: Series
  /** Its newest result, or its newest one shown apart when it has none. */
  latest: LabResult
  latestSpecimen?: Specimen
  /** The big number and its unit. */
  value: string
  unit: string
  /** The twin beside it: "6.0%". */
  twin?: string
  latestLine?: LatestLine
  /** "Trunk, 3 regions" for a regional subject. */
  regionNote?: string
  status: CardStatus
  /** Every numeric result of the series; the chart clips to its domain. */
  points: ChartPoint[]
  /** The sources with counts in the range; data for the Details block and
   * the history screen, never drawn on the card. */
  legend: LegendItem[]
  /** The Details block's lines: sources with counts, the direction, the
   * cadence (empty ones left blank). */
  detailsLines: [string, string, string]
  /** The fit over the numeric results inside the domain. */
  trend?: TrendFit
  trendTone: "success" | "danger" | "neutral"
  trendText: TrendText
  /** How many points the chart draws (inside the domain). */
  drawn: number
  /** The chart's text when it draws nothing: "No results in this range;
   * latest May 2024". */
  emptyText: string
  /** The sources as one line, for a subject with nothing to chart. */
  sourcesText: string
  /** The hatched span before the first point, on the page's domain. */
  emptySpan?: EmptySpan
  /** The results no conversion brings into the display unit, newest
   * first, each as it prints: "120 mg/dL on 3 Mar 2025". */
  apart: string[]
  description?: string
}

/** Everything one card shows, on the page's shared domain. */
export function metricCard(
  index: LabIndex,
  view: LabView,
  domain: TimeDomain,
  markers: Map<string, SourceMark>
): MetricCardData {
  const s = view.lead
  const latestSpecimen = s.specimens[s.specimens.length - 1]
  const latest =
    s.results[s.results.length - 1] ?? s.apart[s.apart.length - 1].shown
  const entry = view.entry
  const shown = resultParts(latest, view.precision, view.unit)
  const points = chartPoints(s.results, markers, entry)
  // Fit only what the chart draws: the domain ends at noon today.
  const inside = points.filter((p) => inDomain(p.at, domain))
  const trend = trendFit(inside.map((p) => ({ at: p.at, value: p.value })))
  const legend = seriesLegend(s.results, markers, (r) => inDomain(r.at, domain))
  return {
    view,
    series: s,
    latest,
    latestSpecimen,
    value: shown.value,
    unit: shown.unit,
    twin: s.results.length ? twinText(index, latestSpecimen) : undefined,
    latestLine: latestLine(index.sources, s.results),
    regionNote: s.channel
      ? `${channelLabel(s.channel)}, ${view.series.length} regions`
      : undefined,
    status: view.status,
    points,
    legend,
    detailsLines: detailsLines(
      view,
      legend.map((l) => l.label)
    ),
    drawn: inside.length,
    emptyText: points.length
      ? `No results in this range; latest ${formatMonth(points[points.length - 1].at)}`
      : "No results to chart",
    sourcesText: legend.map((l) => l.label).join(", "),
    trend,
    trendTone: trendTone(trend, entry),
    trendText: trendText(trend, entry),
    emptySpan: points.length
      ? emptySpan(domain.start, points[0].at)
      : undefined,
    apart: [...s.apart]
      .reverse()
      .map(
        (a) =>
          `${resultText(a.shown, index.metrics.get(a.shown.metric)?.precision)} on ${formatDate(a.shown.at)}`
      ),
    description: view.subject.description,
  }
}

/** The three lines of a card's Details block, always three so every card's
 * block has one height and the charts in a row stay level: the sources
 * with counts ("Lab B, 4 of 6 results. Lab A, 2 of 3 results."), the
 * direction ("Lower is better") and the cadence ("Checked every 12
 * months", important entries); a line with nothing to say is empty. */
export function detailsLines(
  view: Pick<LabView, "entry" | "important">,
  sources: string[]
): [string, string, string] {
  return [
    sources.length ? `${sources.join(". ")}.` : "",
    directionPhrase(view.entry) ?? "",
    view.important
      ? `Checked every ${view.entry?.cadenceMonths ?? DEFAULT_CADENCE_MONTHS} months`
      : "",
  ]
}

/** Where a result comes from, for the headline's first line: its date, its
 * source's name, and whether the sample was fasting. */
export interface ResultWhere {
  date: string
  source?: string
  fasting: boolean
}

export function resultWhere(
  index: Pick<LabIndex, "sources">,
  r: LabResult
): ResultWhere {
  return {
    date: formatDate(r.at),
    source: r.source
      ? (index.sources.get(r.source)?.name ?? r.source)
      : undefined,
    fasting: r.fasting === true,
  }
}

/** The headline's second line: the ranges that applied to a result, "Lab
 * up to 2.30 mmol/L, target up to 1.70" (the target's unit is the same, so
 * it is not repeated), "Target up to 1.70 mmol/L" without a lab range, and
 * empty with neither. `withTarget` is false in another form's unit. */
export function rangesLine(
  view: Pick<LabView, "entry" | "unit" | "precision">,
  r: LabResult | undefined,
  withTarget = true
): string {
  const p = view.precision
  const unit = r?.unit ?? view.unit
  const parts: string[] = []
  const normal = r ? normalBands(r)[0] : undefined
  if (normal)
    parts.push(`lab ${rangeWithUnit(normal.low, normal.high, p, unit)}`)
  const span = withTarget ? targetSpan(view.entry) : undefined
  if (span && (span.from !== undefined || span.to !== undefined))
    parts.push(
      `target ${parts.length ? rangeText(span.from, span.to, p) : rangeWithUnit(span.from, span.to, p, view.unit)}`
    )
  const line = parts.join(", ")
  return line.charAt(0).toUpperCase() + line.slice(1)
}

/** The selected result's line, as the history screen's readout says it:
 * "Selected: 26 Jan 2026, Lab B, fasting, lab up to 2.30 mmol/L". */
export function selectedContext(
  index: Pick<LabIndex, "sources">,
  view: Pick<LabView, "unit" | "precision">,
  r: LabResult
): string {
  const parts = [formatDate(r.at)]
  const source = r.source
    ? (index.sources.get(r.source)?.name ?? r.source)
    : undefined
  if (source) parts.push(source)
  if (r.fasting) parts.push("fasting")
  const normal = normalBands(r)[0]
  if (normal)
    parts.push(
      `lab ${rangeWithUnit(normal.low, normal.high, view.precision, r.unit ?? view.unit)}`
    )
  return `Selected: ${parts.join(", ")}`
}

/** The Labs subtitle: "1,344 results from 5 sources, Nov 2023 to Oct
 * 2026". */
export function labsSummary(
  index: Pick<LabIndex, "results">,
  markers: Map<string, SourceMark>
): string {
  const n = index.results.length
  if (!n) return "No results"
  const named = [...markers.keys()].filter((k) => k !== "").length
  const first = index.results[0].at
  const last = index.results[n - 1].at
  return `${n.toLocaleString("en-GB")} ${n === 1 ? "result" : "results"} from ${named} ${named === 1 ? "source" : "sources"}, ${formatMonth(first)} to ${formatMonth(last)}`
}

export interface BandLine {
  label: string
  tone: "success" | "warning" | "danger"
  /** "Optimal: up to 3.00 mmol/L". */
  text: string
}

/** The catalogue's target bands as the explanation lists them. */
export function bandLines(
  view: Pick<LabView, "entry" | "unit" | "precision">
): BandLine[] {
  return (view.entry?.target ?? []).map((b) => ({
    label: b.label,
    tone: b.tone,
    text: `${b.label}: ${rangeWithUnit(b.from, b.to, view.precision, view.unit)}`,
  }))
}

/** A band behind the chart, shaped as the TrendChart's `TrendBand`. */
export interface ChartBand {
  from?: number
  to?: number
  tone: "success" | "danger" | "warning" | "neutral"
  label: string
  variant?: "fill" | "outline"
  stroke?: "dashed" | "dotted"
}

export interface ChartBandOptions {
  /** False where the values are in another form's unit: no target zones. */
  withTarget?: boolean
  /** The Target zones display setting. Default on. */
  targetZones?: boolean
  /** The Lab range display setting. Default on. */
  labRange?: boolean
}

/** The chart's bands: the catalogue's target bands as washes in their own
 * tones (Target zones), so green, yellow and red only ever mean the
 * catalogue; and the newest result's lab range (Lab range) as a pale
 * neutral fill with dotted edges. The lab's `poor` cut-offs are not drawn:
 * the badge and the readout carry High or Low. Clip them to the data with
 * `clipBands`. */
export function chartBands(
  index: Pick<LabIndex, "sources">,
  entry: CatalogueEntry | undefined,
  newest: LabResult | undefined,
  opts: ChartBandOptions = {}
): ChartBand[] {
  const { withTarget = true, targetZones = true, labRange = true } = opts
  const out: ChartBand[] = []
  const target = withTarget ? (entry?.target ?? []) : []
  if (targetZones)
    for (const b of target)
      out.push({ from: b.from, to: b.to, tone: b.tone, label: b.label })
  if (labRange && newest) {
    const lab = newest.source
      ? index.sources.get(newest.source)?.name
      : undefined
    const label = lab ? `Lab range (${lab})` : "Lab range"
    const normal = normalBands(newest)[0]
    if (normal)
      out.push({
        from: normal.low,
        to: normal.high,
        tone: "neutral",
        label,
        stroke: "dotted",
      })
  }
  return out
}

/** The bands one card draws: `chartBands` by the display settings, clipped
 * to the points inside the domain and the trendline's ends. */
export function cardBands(
  index: Pick<LabIndex, "sources">,
  card: Pick<MetricCardData, "points" | "trend" | "latest" | "view">,
  domain: TimeDomain,
  display: { targetZones: boolean; labRange: boolean; trendline: boolean }
): ChartBand[] {
  const drawn = card.points
    .filter((p) => inDomain(p.at, domain))
    .map((p) => p.value)
  const ends =
    display.trendline && card.trend
      ? [card.trend.from.value, card.trend.to.value]
      : []
  return clipBands(
    chartBands(index, card.view.entry, card.latest, {
      targetZones: display.targetZones,
      labRange: display.labRange,
    }),
    [...drawn, ...ends]
  )
}

export interface MetricInfoData {
  name: string
  /** The panel it sits in, a quiet badge beside the name. */
  panel?: string
  description?: string
  /** Why it matters and what moves it (the important entries). */
  why?: string
  moves?: string
  sources: { name: string; url: string }[]
  /** "Lower is better." */
  direction?: string
  bands: BandLine[]
  /** "Lab range (Lab A): up to 3.00 mmol/L", from the newest result. */
  labRange?: string
  /** The sentence for the current status word, where the catalogue has
   * one. */
  assessment?: string
  /** "Checked every 12 months, last 25 Sept 2026", important entries. */
  cadence?: string
}

/** The info panel's content: catalogue fields and the newest result only.
 * `statusWord` is the card's badge word. */
export function metricInfo(
  index: Pick<LabIndex, "sources">,
  view: Pick<
    LabView,
    "name" | "entry" | "important" | "unit" | "precision" | "subject"
  > & { panel?: string },
  newest: LabResult | undefined,
  statusWord: string | undefined
): MetricInfoData {
  const phrase = directionPhrase(view.entry)
  const normal = newest ? normalBands(newest)[0] : undefined
  const lab = newest?.source
    ? index.sources.get(newest.source)?.name
    : undefined
  const months = view.entry?.cadenceMonths ?? DEFAULT_CADENCE_MONTHS
  return {
    name: view.name,
    panel: view.panel,
    description: view.subject.description,
    why: view.entry?.why,
    moves: view.entry?.moves,
    sources: (view.entry?.sources ?? []).map((s) => ({
      name: s.title,
      url: s.url,
    })),
    direction: phrase ? `${phrase}.` : undefined,
    bands: bandLines(view),
    labRange: normal
      ? `${lab ? `Lab range (${lab})` : "Lab range"}: ${rangeWithUnit(normal.low, normal.high, view.precision, newest?.unit ?? view.unit)}`
      : undefined,
    assessment: assessmentFor(view.entry, statusWord),
    cadence:
      view.important && newest
        ? `Checked every ${months} months, last ${formatDate(newest.at)}`
        : undefined,
  }
}

/** Whether a subject's newest result is judged at all: a lab range or a
 * target. */
export function isJudged(
  r: LabResult,
  entry: CatalogueEntry | undefined,
  withTarget = true
): boolean {
  return (
    normalBands(r).length > 0 || (withTarget && greenBands(entry).length > 0)
  )
}

/** A series' results in one unit form other than the display form: each
 * specimen's member in that form (a twin, or a result shown apart), in the
 * form's unit where it converts, oldest first. */
export function formResults(
  index: Pick<LabIndex, "metrics" | "graph">,
  series: Pick<Series, "specimens" | "apart">,
  form: UnitForm
): LabResult[] {
  const ids = new Set(form.metrics.map((m) => m.id))
  const out: LabResult[] = []
  for (const s of [...series.specimens, ...series.apart]) {
    const m = s.members.find((x) => ids.has(x.metric))
    if (!m) continue
    const from = m.unit ?? index.metrics.get(m.metric)?.unit
    const r =
      form.unit && from !== form.unit
        ? moved(m, form.unit, (v) => convert(index.graph, v, from, form.unit))
        : undefined
    out.push(r ?? m)
  }
  return out.sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
}

/** The unit forms a series can be read in: the display form, then each
 * other form some specimen has a member in. */
export function seriesForms(
  subject: Pick<Subject, "forms">,
  series: Pick<Series, "specimens" | "apart">
): UnitForm[] {
  const held = new Set(
    [...series.specimens, ...series.apart].flatMap((s) =>
      s.members.map((m) => m.metric)
    )
  )
  return subject.forms.filter(
    (f) => f.display || f.metrics.some((m) => held.has(m.id))
  )
}

/** Each source's marker on the lab screens: by result count over every
 * result, most first, so a source keeps its shape on every card and on a
 * history screen. */
export function labMarkers(
  index: Pick<LabIndex, "results" | "sources"> | undefined
): Map<string, SourceMark> {
  if (!index) return new Map()
  return sourceMarkers(
    sourcesByCount(index.results, index.sources),
    index.sources
  )
}
