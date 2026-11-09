// The Healthspan screen's cards as data: the nine age score contributors,
// the signals a source reports and the three body readings, each with its
// badges, edge, headline, a scale on every card (a zone bar whose segments
// name their range and meaning, or a stated reason), a suggestion, the
// Details chart with the bar's zones, the info rows and its history link.
// Built once per data and day; the card component only lays this out.

import { targetFor, type Sex } from "../engine/curves"
import { buildSeries, dayIso } from "../engine/engine"
import { atStake } from "../engine/focus"
import { impactTone, tenths } from "../engine/format"
import type { Metric, MetricId, Report, SectionId } from "../engine/report"
import { clipBands } from "../lib/chart"
import { formatDate, formatShortDate } from "../lib/format"
import { markOf, sourceMarkers, sourcesByCount } from "../lib/sourceMarkers"
import {
  addDays,
  baseline as baselineBefore,
  cardStatuses,
  isoDay,
  rowOf,
  type HealthspanStatus,
} from "../rules/healthspan"
import type { Finding } from "../rules/types"
import type { ChartKeyItem } from "../ui/ChartKey"
import type { Tone } from "../ui/tone"
import type { TrendBand, TrendPoint } from "../ui/TrendChart"
import type { ZoneBarTick } from "../ui/ZoneBar"
import {
  HEALTHSPAN_CONTENT,
  notInScore,
  VO2_ONE_READING,
  type BodyKey,
  type HealthspanCardKey,
  type Source,
} from "./content"
import {
  baselineSeries,
  chartFormat,
  CONTRIBUTOR_LABELS,
  HEALTHSPAN_METRICS,
  impactText,
  latest,
  meanOver,
  noDataText,
  rollingMean,
  tickFormat,
  valueParts,
  valueText,
  weekSoFar,
  type SignalId,
} from "./healthspanMetrics"
import {
  CONTRIBUTOR_INPUTS,
  newest,
  sourceName,
  type BodyCardRole,
  type Daily,
  type HealthspanSeries,
} from "./read"
import {
  bmiBand,
  bmiScale,
  bodyFatBand,
  bodyFatScale,
  breathingScale,
  contributorScale,
  hrvScale,
  recoveryScale,
  signed,
  skinScale,
  sleepPerformanceScale,
  spo2Scale,
  strainScale,
  visceralScale,
  type Scale,
  type ScaleSegment,
} from "./scales"
import { recoveryWord, SPO2_LOW } from "./segmentTones"
import {
  bodySuggestion,
  contributorSuggestion,
  signalSuggestion,
  trendWords,
  type Suggestion,
} from "./suggest"

export type HealthspanGroup =
  "sleep" | "strain" | "fitness" | "signals" | "body"
export const HEALTHSPAN_GROUPS: HealthspanGroup[] = [
  "sleep",
  "strain",
  "fitness",
  "signals",
  "body",
]
export const GROUP_LABELS: Record<HealthspanGroup, string> = {
  sleep: "Sleep",
  strain: "Strain",
  fitness: "Fitness",
  signals: "Signals",
  body: "Body",
}

export type HealthspanCardKind = "contributor" | "signal" | "body"

export interface CardBadge {
  label: string
  tone?: Tone
  isMono?: boolean
}

/** A judgement chip: an arrow and a word or rate, toned by whether the
 * direction is the better one. */
export interface CardChip {
  direction: "up" | "down" | "flat"
  label: string
  tone: "success" | "danger" | "neutral"
  tooltip: string
}

export interface CardHeadline {
  value: string
  unit?: string
  context: string
  /** Contributors only: the age score effect ("-0.1 years"), or null
   * for "No data". Undefined draws no stack. */
  impact?: { text: string; tone: "warning" | "success" | "neutral" } | null
}

export interface CardMarker {
  at: number
  side: "above" | "below"
  label: string
  prefix: string
  valueText: string
}

export interface CardBar {
  label: string
  min: number
  max: number
  segments: ScaleSegment[]
  markers: CardMarker[]
  ticks?: ZoneBarTick[]
  minLabel?: string
  maxLabel?: string
  /** Grey only because there is no target (no birth date and sex): the
   * key does not call it "No effect". */
  untoned?: boolean
}

export interface CardFacts {
  items: { label: string; value: string }[]
  chip?: CardChip
}

/** What a details chart draws, for the page key: the weekly 30-day
 * average over the 6-month one (most contributors), each night or day over
 * its 30-day average (hours of sleep, steps, resting heart rate, and the
 * signals), or lab readings with a marker shape per source (body). */
export type ChartKind = "weekly" | "daily" | "readings"

export interface CardChart {
  kind: ChartKind
  label: string
  data: TrendPoint[]
  baseline?: { at: string; value: number; low?: number; high?: number }[]
  /** What the baseline line is, for the "Selected:" line. */
  baselineName?: string
  bands: TrendBand[]
  unit: string
  formatValue: (v: number) => string
  formatTick?: (v: number, step: number) => string
  /** A short line under the chart ("1 reading"). */
  caption?: string
  /** The y range, for a chart whose data alone gives a flat axis (one
   * reading). */
  valueRange?: { min: number; max: number }
  /** Under the chart: a chip the card does not already show. */
  chip?: CardChip
  /** Facts shown with the chart that the card does not already show
   * (resting heart rate's 7-night mean). */
  facts?: { label: string; value: string }[]
}

export interface HealthspanCardData {
  /** The element id: `contributor-sleepHours`, `signal-hrv`,
   * `body-bodyMass`. */
  id: string
  /** The contributor or signal id, or the body reading's role. */
  key: string
  kind: HealthspanCardKind
  group: HealthspanGroup
  label: string
  /** The contributor's weekly metric, for ranking. */
  metric?: Metric
  findings: Finding[]
  statuses: Set<HealthspanStatus>
  badges: CardBadge[]
  edge?: "danger" | "warning" | "info"
  headline: CardHeadline
  /** The trend chip at the headline's end on a card with no age score
   * effect to show there (strain, HRV, breathing rate, skin temperature). */
  headlineChip?: CardChip
  bar?: CardBar
  facts?: CardFacts
  /** The stated reason when a card has no bar ("Needs your height for
   * the BMI scale"), shown in its place. */
  line?: string
  /** The suggestion: the line under the visual and the info's row. */
  note: Suggestion
  chart: CardChart
  info: { label: string; value: string }[]
  /** The sources behind the card's scale and content, linked in its Info
   * popover. */
  sources: Source[]
  historyHref: string
}

export interface HealthspanCardInputs {
  data: HealthspanSeries
  /** The age score; undefined without a birth date and sex. */
  report?: Report
  /** The weekly averages with or without a profile. */
  averages?: Report
  profile: { sex?: Sex; birthDate?: string }
  findings: Finding[]
  today: Date
}

/** The page's shared axis: the 182 days ending today. */
export function healthspanAxis(today: Date): { start: string; end: string } {
  const end = isoDay(today)
  return { start: addDays(end, -181), end }
}

const SECTION_GROUP: Record<SectionId, HealthspanGroup> = {
  sleep: "sleep",
  strain: "strain",
  fitness: "fitness",
}

/** "night to 24 Oct", or "last night" when the night ended today. */
export function nightContext(day: string, today: string): string {
  return day === today
    ? "last night"
    : `night to ${formatShortDate(`${day}T12:00:00Z`)}`
}

/** The engine files a night under the evening it began; the page names it
 * by the morning it ended, as the WHOOP reads do. */
function mornings(m: Map<number, number>): Daily {
  return new Map([...m].map(([d, v]) => [dayIso(d + 1), v]))
}

function byDay(m: Map<number, number>): Daily {
  return new Map([...m].map(([d, v]) => [dayIso(d), v]))
}

function points(m: Daily, axis: { start: string; end: string }): TrendPoint[] {
  return [...m]
    .filter(([d]) => d >= axis.start && d <= axis.end)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([at, value]) => ({ at, value, series: "line" }))
}

/** A baseline rule's badge says over what span it holds, so "Elevated"
 * beside a resting heart rate on target is not a contradiction. */
export function badgeWord(f: Pick<Finding, "subject" | "word">): string {
  return f.subject === "rhr:elevated" || f.subject === "hrv"
    ? `${f.word} this week`
    : f.word
}

function edgeOf(findings: Finding[]): HealthspanCardData["edge"] {
  if (findings.some((f) => f.tone === "danger")) return "danger"
  if (findings.some((f) => f.tone === "warning")) return "warning"
  if (findings.some((f) => f.tone === "info")) return "info"
  return undefined
}

/** "→ steady", "↗ rising", "↘ falling": the 30-day mean against the
 * 6-month one, steady within 2 percent. `better` names the better
 * direction, if there is one. */
export function trendChip(
  m30: number | undefined,
  m182: number | undefined,
  better?: "up" | "down"
): CardChip {
  const d =
    m30 === undefined || m182 === undefined || m182 === 0
      ? 0
      : (m30 - m182) / Math.abs(m182)
  const direction = d > 0.02 ? "up" : d < -0.02 ? "down" : "flat"
  const label =
    direction === "up" ? "rising" : direction === "down" ? "falling" : "steady"
  const tone =
    direction === "flat" || !better
      ? "neutral"
      : direction === better
        ? "success"
        : "danger"
  return {
    direction,
    label,
    tone,
    tooltip:
      "30-day average against the 6-month average, steady within 2 percent",
  }
}

/** The contributor's verdict number as a chip: the 30-day average against
 * the 6-month one in percent, toned by whether it moves the years down. */
export function verdictChip(m: Metric): CardChip | undefined {
  if (m.sixMonth === null || m.thirtyDay === null || m.sixMonth === 0)
    return undefined
  const pct = ((m.thirtyDay - m.sixMonth) / Math.abs(m.sixMonth)) * 100
  const direction = Math.abs(pct) < 0.05 ? "flat" : pct > 0 ? "up" : "down"
  const y6 = m.impact ?? 0
  const y30 = m.impact30 ?? y6
  const tone =
    direction === "flat" || Math.abs(y30 - y6) < 0.01
      ? "neutral"
      : y30 < y6
        ? "success"
        : "danger"
  return {
    direction,
    label: `${Math.abs(pct).toFixed(1)}% vs 6-month`,
    tone,
    tooltip: "The 30-day average against the 6-month average",
  }
}

/** The latest body reading against the previous one, untoned (nothing
 * judges weight or body fat here). */
function changeChip(
  latestValue: number,
  previous: number
): CardChip | undefined {
  if (previous === 0) return undefined
  const pct = ((latestValue - previous) / Math.abs(previous)) * 100
  const direction = Math.abs(pct) < 0.05 ? "flat" : pct > 0 ? "up" : "down"
  return {
    direction,
    label: `${Math.abs(pct).toFixed(1)}% vs previous`,
    tone: "neutral",
    tooltip: "The latest reading against the one before it",
  }
}

/** A bar's zones as chart bands: runs of adjacent segments with the same
 * tone and level merged into one band from the run's start value to its
 * end, labelled by what the run's segments mean ("green: primed for a
 * high-strain day", or "adds 1.1 years to adds 0.4 years"). The chart and
 * the bar read one segments array, so colours and levels agree. */
export function zoneBands(
  bar: Pick<CardBar, "min" | "max" | "segments">
): TrendBand[] {
  const w = (bar.max - bar.min) / bar.segments.length
  const out: TrendBand[] = []
  let start = 0
  for (let i = 1; i <= bar.segments.length; i++) {
    const a = bar.segments[start]
    const b = bar.segments[i]
    if (b && b.tone === a.tone && b.level === a.level) continue
    const last = bar.segments[i - 1]
    const meaning = (l?: string) =>
      l?.includes(", ") ? l.slice(l.lastIndexOf(", ") + 2) : l
    const first = meaning(a.label)
    const end = meaning(last.label)
    // The end zones run on past the bar's ends: a value beyond the scale
    // still sits in its first or last zone.
    out.push({
      from: start === 0 ? undefined : bar.min + start * w,
      to: i === bar.segments.length ? undefined : bar.min + i * w,
      tone: a.tone,
      level: a.level,
      label:
        first && end ? (first === end ? first : `${first} to ${end}`) : a.tone,
    })
    start = i
  }
  return out
}

/** A contributor's chart bands: its bar's zones (cut at the target, as
 * the card's bar is) over the drawn values, and VO2 max's dashed target.
 * No zones without a target. The card and the history page both use it. */
export function contributorChartBands(
  m: Metric,
  target: number | undefined,
  values: number[]
): TrendBand[] {
  if (target === undefined) return []
  const bands: TrendBand[] = clipBands(
    zoneBands(contributorScale(m, target)),
    values
  )
  if (m.id === "vo2max")
    bands.push({
      from: target,
      to: target,
      tone: "warning",
      label: `Target ${valueText(target, m.format)}`,
      variant: "outline",
    })
  return bands
}

/** A signal's chart bands: the bar's zones where they are fixed values on
 * the chart's axis (recovery, sleep performance, blood oxygen, strain),
 * breathing rate's absolute adult range, and none for HRV and skin
 * temperature, which chart against a rolling baseline and keep the ribbon.
 * The card and the history page both use it. */
export function signalChartBands(id: string, values: number[]): TrendBand[] {
  const sc =
    id === "recovery"
      ? recoveryScale()
      : id === "sleepPerformance"
        ? sleepPerformanceScale()
        : id === "spo2"
          ? spo2Scale()
          : id === "strain"
            ? strainScale()
            : undefined
  if (sc) return clipBands(zoneBands(sc), values)
  if (id === "respiratoryRate")
    return clipBands(
      [
        {
          from: 12,
          to: 20,
          tone: "success" as const,
          level: 3 as const,
          label: "12 to 20/min, the adult resting range",
        },
      ],
      values
    )
  return []
}

/** The card's info rows: what it is, why it
 * matters, what moves it, how it is measured, the reader's standing, the
 * suggestion with its basis, and for a card outside the age model why it
 * changes no years. */
function infoRows(
  key: HealthspanCardKey,
  standing: string,
  note: Suggestion,
  opts: { measuredBy?: string; notInScore?: string } = {}
): { label: string; value: string }[] {
  const c = HEALTHSPAN_CONTENT[key]
  const rows = [
    { label: "What it is", value: c.what },
    { label: "Why it matters", value: c.why },
    { label: "What moves it", value: c.moves },
    { label: "Measured by", value: opts.measuredBy ?? c.measuredBy },
    { label: "Your standing", value: standing },
    { label: "Suggestion", value: `${note.text} Basis: ${note.basis}` },
  ]
  if (opts.notInScore) rows.push({ label: "Age score", value: opts.notInScore })
  return rows
}

/** Every card, contributors first (the engine's order), then the
 * signals a source reports, then body. */
export function healthspanCards(
  inp: HealthspanCardInputs
): HealthspanCardData[] {
  const today = isoDay(inp.today)
  const axis = healthspanAxis(inp.today)
  const forCard = (row: string) =>
    inp.findings.filter(
      (f) => rowOf(f) === row && !f.subject.startsWith("sync")
    )
  return [
    ...contributorCards(inp, today, axis, forCard),
    ...signalCards(inp, today, axis, forCard),
    ...bodyCards(inp, axis),
  ]
}

function contributorCards(
  inp: HealthspanCardInputs,
  today: string,
  axis: { start: string; end: string },
  forCard: (row: string) => Finding[]
): HealthspanCardData[] {
  const snap = (inp.report ?? inp.averages)?.snapshots.at(-1)
  if (!snap) return []
  const hasTargets = !!inp.report
  const sex: Sex | undefined = inp.profile.sex
  const { data } = inp
  const series = buildSeries(data.input)
  const sleepHours = mornings(series.sleepHours)
  const consistency = mornings(series.sleepConsistency)
  const yesterday = addDays(today, -1)
  const worn = weekSoFar(
    new Map([...series.worn].map((d) => [dayIso(d + 1), 1])),
    today
  )

  const headlineOf = (m: Metric): CardHeadline => {
    const night = (d: Daily): CardHeadline | undefined => {
      const l = latest(d, today)
      if (!l) return undefined
      const p = valueParts(l.value, m.format)
      return {
        value: p.num,
        unit: p.unit || undefined,
        context: nightContext(l.day, today),
      }
    }
    const week = (d: Map<number, number>): CardHeadline => {
      if (worn <= 0) return { value: "No data", context: "no data this week" }
      const p = valueParts(weekSoFar(byDay(d), today) / 60, "hours")
      return { value: p.num, unit: p.unit, context: "this week so far" }
    }
    let h: CardHeadline | undefined
    switch (m.id) {
      case "sleepConsistency":
        h = night(consistency)
        break
      case "sleepHours":
        h = night(sleepHours)
        break
      case "rhr":
        h = night(data.rhr)
        break
      case "zones13":
        h = week(series.zones13)
        break
      case "zones45":
        h = week(series.zones45)
        break
      case "strength":
        h = week(series.strength)
        break
      case "steps": {
        const l = latest(data.steps, yesterday)
        if (l)
          h = {
            value: valueParts(l.value, "steps").num,
            unit: "steps",
            context:
              l.day === yesterday
                ? "yesterday"
                : formatDate(`${l.day}T12:00:00Z`),
          }
        break
      }
      case "vo2max": {
        const v = newest(data.vo2)
        if (v)
          h = {
            value: valueParts(v.value, "vo2").num,
            unit: "ml/kg/min",
            context: `${data.vo2.length === 1 ? "1 reading, " : ""}${formatDate(v.at)}, ${sourceName(data.sources, v.source)}`,
          }
        break
      }
      case "leanMass": {
        const l = newest(data.lean)
        if (l)
          h = {
            value: valueParts(l.value, "percent1").num,
            context: `${formatDate(l.at)}, ${sourceName(data.sources, l.source)}`,
          }
        break
      }
    }
    return h ?? { value: "No data", context: "nothing in the last 182 days" }
  }

  const dailyOf: Partial<Record<MetricId, { d: Daily; each: string }>> = {
    sleepHours: { d: sleepHours, each: "each night" },
    steps: { d: data.steps, each: "each day" },
    rhr: { d: data.rhr, each: "each night" },
  }

  return snap.metrics.map((m) => {
    const findings = forCard(m.id)
    const label = CONTRIBUTOR_LABELS[m.id]
    const target =
      hasTargets && sex ? targetFor(m.id, snap.age, sex) : undefined
    const fmt = chartFormat(m.format)
    const statuses = cardStatuses({ findings })
    const badges: CardBadge[] = [
      ...(hasTargets ? [] : [{ label: "No target", tone: "info" as const }]),
      ...findings.map((f) => ({ label: badgeWord(f), tone: f.tone })),
      ...(hasTargets && !findings.length
        ? [{ label: "On track", tone: "success" as const }]
        : []),
      { label: GROUP_LABELS[SECTION_GROUP[m.section]], isMono: true },
    ]
    const headline = headlineOf(m)
    if (hasTargets)
      headline.impact =
        m.impact === null
          ? null
          : {
              text: `${impactText(m.impact)} years`,
              tone: (
                { bad: "warning", good: "success", neutral: "neutral" } as const
              )[impactTone(m.impact)],
            }

    let bar: CardBar | undefined
    let line: string | undefined
    // Without a birth date and sex the curve is a stand-in person's, so the
    // bar names ranges only: no tones, no years.
    const scale = contributorScale(m, hasTargets ? target : undefined)
    if (m.sixMonth === null) line = `${noDataText(m.id)}.`
    else {
      const markers: CardMarker[] = [
        {
          at: m.sixMonth,
          side: "above",
          label: "6-month average",
          prefix: "6mo",
          valueText: valueText(m.sixMonth, m.format),
        },
      ]
      if (m.thirtyDay !== null)
        markers.push({
          at: m.thirtyDay,
          side: "below",
          label: "30-day average",
          prefix: "30d",
          valueText: valueText(m.thirtyDay, m.format),
        })
      bar = {
        ...barOf(label, scale, markers),
        untoned: !hasTargets,
        ticks: scale.ticks.length ? scale.ticks : undefined,
      }
    }

    // The details chart: each night or day for three contributors, the
    // readings themselves for VO2 max (one point when there is one), the
    // weekly 30-day average for the rest; the bar's zones behind it.
    const daily = dailyOf[m.id]
    const vo2Readings =
      m.id === "vo2max"
        ? data.vo2
            .filter((r) => r.at.slice(0, 10) >= axis.start)
            .map((r) => ({ at: r.at, value: r.value, series: "line" }))
        : undefined
    const data_: TrendPoint[] =
      vo2Readings ??
      (daily
        ? points(daily.d, axis)
        : m.history
            .filter((p) => p.thirtyDay !== null)
            .map((p) => ({
              at: p.date,
              value: p.thirtyDay as number,
              series: "line",
            })))
    const base = vo2Readings
      ? []
      : daily
        ? rollingMean(daily.d, 30, axis.start)
        : m.history
            .filter((p) => p.sixMonth !== null)
            .map((p) => ({ at: p.date, value: p.sixMonth as number }))
    const shown = [
      ...data_.filter((p) => String(p.at) >= axis.start).map((p) => p.value),
      ...base.filter((p) => p.at >= axis.start).map((p) => p.value),
    ]
    // A single reading gets an axis 10 percent either side, so it sits in
    // the middle with its zones around it.
    const valueRange =
      data_.length === 1
        ? { min: data_[0].value * 0.9, max: data_[0].value * 1.1 }
        : undefined
    const bands = contributorChartBands(
      m,
      hasTargets ? target : undefined,
      valueRange ? [valueRange.min, valueRange.max] : shown
    )
    const rhr7 = m.id === "rhr" ? meanOver(data.rhr, today, 7) : undefined
    const oneReading = m.id === "vo2max" && data.vo2.length === 1
    const note = contributorSuggestion({
      metric: m,
      label,
      hasTargets,
      target,
      oneReading,
      elevated: findings.some((f) => f.subject === "rhr:elevated"),
      stale: findings.some((f) => f.word === "Stale"),
    })
    const standing = [
      `${headline.value}${headline.unit ? ` ${headline.unit}` : ""}, ${headline.context}.`,
      m.thirtyDay === null
        ? "No 30-day average."
        : `30-day ${valueText(m.thirtyDay, m.format)}, 6-month ${valueText(m.sixMonth, m.format)}${target === undefined ? "" : `, against a target of ${valueText(target, m.format)}${targetNote(m.id)}`}; ${trendWords(m, hasTargets)}.`,
      hasTargets
        ? m.impact === null
          ? "No age score effect: no data."
          : `Age score effect ${impactText(m.impact)} years.`
        : "No age score effect without your birth date and sex.",
    ].join(" ")
    const measuredBy =
      m.id === "vo2max"
        ? `${HEALTHSPAN_CONTENT.vo2max.measuredBy} ${sourceLine(m.id, data)}${oneReading ? ` ${VO2_ONE_READING}` : ""}`
        : m.id === "leanMass"
          ? `${HEALTHSPAN_CONTENT.leanMass.measuredBy} ${sourceLine(m.id, data)}`
          : undefined
    const info = infoRows(m.id, standing, note, { measuredBy })

    return {
      id: `contributor-${m.id}`,
      key: m.id,
      kind: "contributor",
      group: SECTION_GROUP[m.section],
      label,
      metric: m,
      findings,
      statuses,
      badges,
      edge: edgeOf(findings),
      headline,
      bar,
      line,
      note,
      chart: {
        kind: vo2Readings ? "readings" : daily ? "daily" : "weekly",
        label: `${label}, ${vo2Readings ? "readings" : daily ? daily.each : "30-day average by week"}`,
        caption: oneReading ? "1 reading" : undefined,
        data: data_,
        baseline: base,
        baselineName: daily ? "30-day average" : "6-month average",
        bands,
        unit: fmt.unit,
        formatValue: fmt.value,
        formatTick: tickFormat(m.format),
        valueRange,
        // The 30-day change against the 6 months: on no other part of the
        // card (the Info popover's verdict says it in a sentence).
        // None from a single reading.
        chip: hasTargets && !oneReading ? verdictChip(m) : undefined,
        // The 30-day and 6-month means are the bar's markers already.
        facts:
          rhr7 !== undefined
            ? [{ label: "7-night", value: valueText(rhr7, "bpm") }]
            : undefined,
      },
      info,
      sources: HEALTHSPAN_CONTENT[m.id].sources,
      historyHref: `#/healthspan/${m.id}`,
    }
  })
}

/** " for your age", " for your sex", " for your age and sex", or nothing. */
function targetNote(id: MetricId): string {
  const age = ["zones13", "zones45", "steps"]
  const both = ["vo2max", "leanMass"]
  return both.includes(id)
    ? " for your age and sex"
    : age.includes(id)
      ? " for your age"
      : id === "rhr"
        ? " for your sex"
        : ""
}

function sourceLine(id: MetricId, data: HealthspanSeries): string {
  const base = CONTRIBUTOR_INPUTS[id].summary
  if (id === "vo2max") {
    const v = newest(data.vo2)
    return v
      ? `${base}. Newest: ${sourceName(data.sources, v.source)}, ${formatDate(v.at)}.`
      : `${base}. None in the last two years.`
  }
  if (id === "leanMass") {
    const l = newest(data.lean)
    return l
      ? `${base}. Newest: ${sourceName(data.sources, l.source)}, ${formatDate(l.at)}${l.basis === "fatFreeMass" ? "" : ", worked out as 100 less the scan's body fat"}.`
      : `${base}. No scan yet.`
  }
  return `${base}.`
}

/** The signals on the screen, in order; one no source reports is left
 * out. */
export const SIGNAL_IDS: readonly SignalId[] = HEALTHSPAN_METRICS.map(
  (m) => m.id
)

const JUDGED = new Set(["recovery", "sleepPerformance", "spo2"])

/** A judged signal's band as its badge, the word its note and bar use
 * ("Sufficient", not "On track" beside a "sufficient" note). */
function bandBadge(id: string, v: number): CardBadge {
  if (id === "recovery") {
    const w = recoveryWord(v)
    return { label: w.word, tone: w.tone }
  }
  if (id === "sleepPerformance")
    return v >= 85
      ? { label: "Optimal", tone: "success" }
      : v >= 70
        ? { label: "Sufficient", tone: "warning" }
        : { label: "Poor", tone: "danger" }
  return v >= SPO2_LOW
    ? { label: "Normal", tone: "success" }
    : { label: "Low", tone: "warning" }
}

/** A nightly or daily signal whose newest reading is older than this many
 * days is stale. */
export const SIGNAL_STALE_DAYS = 3

/** A scale and its markers as the card's bar. */
function barOf(label: string, scale: Scale, markers: CardMarker[]): CardBar {
  return {
    label: `${label} scale`,
    min: scale.min,
    max: scale.max,
    segments: scale.segments,
    markers,
    ticks: scale.ticks,
    minLabel: scale.minLabel,
    maxLabel: scale.maxLabel,
  }
}

function signalCards(
  inp: HealthspanCardInputs,
  today: string,
  axis: { start: string; end: string },
  forCard: (row: string) => Finding[]
): HealthspanCardData[] {
  const recovery = latest(inp.data.recovery, today)
  return HEALTHSPAN_METRICS.flatMap((m) => {
    const id = m.id
    const daily = m.daily(inp.data)
    if (!daily.size) return []
    const findings = forCard(id)
    const fmt = (v: number) =>
      m.precision
        ? v.toFixed(m.precision)
        : Math.round(v).toLocaleString("en-GB")
    const attached = m.unit === "%" || m.unit === "/min"
    const show = (v: number) =>
      attached ? `${fmt(v)}${m.unit}` : `${fmt(v)} ${m.unit}`.trim()
    const l = latest(daily, today)
    const m30 = meanOver(daily, today, 30)
    const m182 = meanOver(daily, today, 182)
    const m7 = meanOver(daily, today, 7)
    // The 30-night baseline before the newest night (14 nights needed).
    const base = l ? baselineBefore(daily, l.day) : undefined
    const headline: CardHeadline = l
      ? {
          value: attached ? `${fmt(l.value)}${m.unit}` : fmt(l.value),
          unit: attached || !m.unit ? undefined : m.unit,
          // A day's figure (a morning's recovery, a day's SDNN) is named
          // by its day; a night's by the morning it ended.
          context:
            id === "recovery" || id === "hrvSdnn"
              ? l.day === today
                ? "today"
                : l.day === addDays(today, -1)
                  ? "yesterday"
                  : formatDate(`${l.day}T12:00:00Z`)
              : id === "strain"
                ? formatDate(`${l.day}T12:00:00Z`)
                : nightContext(l.day, today),
        }
      : { value: "No data", context: "nothing in the last 182 days" }
    const avgMarkers = (): CardMarker[] => [
      ...(m182 === undefined
        ? []
        : [
            {
              at: m182,
              side: "above" as const,
              label: "6-month average",
              prefix: "6mo",
              valueText: show(m182),
            },
          ]),
      ...(m30 === undefined
        ? []
        : [
            {
              at: m30,
              side: "below" as const,
              label: "30-day average",
              prefix: "30d",
              valueText: show(m30),
            },
          ]),
    ]
    const nightMarkers = (
      value: (v: number) => number,
      text: (v: number) => string
    ): CardMarker[] => [
      ...(l
        ? [
            {
              at: value(l.value),
              side: "above" as const,
              label: "Last night",
              prefix: "last",
              valueText: text(l.value),
            },
          ]
        : []),
      ...(m7 === undefined
        ? []
        : [
            {
              at: value(m7),
              side: "below" as const,
              label: "7-night mean",
              prefix: "7n",
              valueText: text(m7),
            },
          ]),
    ]
    let bar: CardBar | undefined
    let line: string | undefined
    switch (id) {
      case "recovery":
        bar = barOf(m.label, recoveryScale(), avgMarkers())
        break
      case "sleepPerformance":
        bar = barOf(m.label, sleepPerformanceScale(), avgMarkers())
        break
      case "spo2":
        bar = barOf(m.label, spo2Scale(), avgMarkers())
        break
      case "strain":
        bar = barOf(m.label, strainScale(), [
          ...(l
            ? [
                {
                  at: l.value,
                  side: "above" as const,
                  label: l.day === today ? "Today" : "Latest day",
                  prefix: l.day === today ? "today" : "latest",
                  valueText: show(l.value),
                },
              ]
            : []),
          ...avgMarkers().filter((x) => x.prefix === "30d"),
        ])
        break
      case "hrv":
      case "hrvSdnn": {
        const sc = hrvScale(base?.mean, base?.sd)
        if (sc)
          bar = barOf(
            m.label,
            sc,
            nightMarkers((v) => v, show)
          )
        else line = "Needs 14 nights for a baseline."
        break
      }
      case "respiratoryRate":
        bar = barOf(
          m.label,
          breathingScale(base?.mean),
          nightMarkers((v) => v, show)
        )
        break
      case "skinTemperature":
        if (base)
          bar = barOf(
            m.label,
            skinScale(base.mean),
            nightMarkers(
              (v) => v - base.mean,
              (v) => signed(v - base.mean, 2, " °C")
            )
          )
        else line = "Needs 14 nights for a baseline."
        break
    }
    const bands = signalChartBands(id, [...daily.values()])
    const chip =
      id === "strain" ||
      id === "hrv" ||
      id === "hrvSdnn" ||
      id === "respiratoryRate" ||
      id === "skinTemperature"
        ? trendChip(
            m30,
            m182,
            id === "hrv" || id === "hrvSdnn" ? "up" : undefined
          )
        : undefined
    // A newest reading older than the limit is stale: a "Stale" badge and
    // a note saying so, never a current verdict.
    const stale = !!l && l.day < addDays(today, -SIGNAL_STALE_DAYS)
    const badges: CardBadge[] = stale
      ? [{ label: "Stale", tone: "info" }]
      : findings.length
        ? findings.map((f) => ({ label: badgeWord(f), tone: f.tone }))
        : JUDGED.has(id) && l
          ? [bandBadge(id, l.value)]
          : []
    const prev = l ? latest(daily, addDays(l.day, -1)) : undefined
    const note = signalSuggestion({
      id: id,
      latest: l?.value,
      m30,
      m182,
      m7,
      baseline: base ? { mean: base.mean, sd: base.sd } : undefined,
      finding: findings.length > 0,
      staleSince: stale && l ? formatDate(`${l.day}T12:00:00Z`) : undefined,
      recovery: recovery?.value,
      strainYesterday: id === "strain" ? prev?.value : undefined,
    })
    const standing = l
      ? [
          `${headline.value}${headline.unit ? ` ${headline.unit}` : ""}, ${headline.context}.`,
          base &&
          (id === "hrv" || id === "respiratoryRate" || id === "skinTemperature")
            ? `7-night ${m7 === undefined ? "none" : show(m7)}; baseline ${show(base.mean)} (SD ${fmt(base.sd)}).`
            : `30-day ${m30 === undefined ? "none" : show(m30)}, 6-month ${m182 === undefined ? "none" : show(m182)}.`,
          bar
            ? `Zone: ${zoneWord(bar, id === "skinTemperature" && base ? l.value - base.mean : l.value)}.`
            : "",
        ]
          .filter(Boolean)
          .join(" ")
      : "No readings in the last 182 days."
    return {
      id: `signal-${id}`,
      key: id,
      kind: "signal",
      group: "signals",
      label: m.label,
      findings,
      statuses: stale
        ? new Set<HealthspanStatus>([
            ...[...cardStatuses({ findings })].filter((x) => x !== "ok"),
            "nodata",
          ])
        : cardStatuses({ findings }),
      badges,
      edge: edgeOf(findings),
      headline,
      headlineChip: chip,
      bar,
      line,
      note,
      chart: {
        kind: "daily",
        label: `${m.label}, daily`,
        data: points(daily, axis),
        baseline: m.hasBaseline
          ? baselineSeries(daily).filter((b) => b.at >= axis.start)
          : undefined,
        baselineName: m.hasBaseline ? "30-day mean" : undefined,
        bands,
        unit: m.unit,
        formatValue: fmt,
        // A card with a headline chip already shows its trend.
        chip: chip
          ? undefined
          : trendChip(
              m30,
              m182,
              id === "recovery" || id === "sleepPerformance" ? "up" : undefined
            ),
      },
      sources: HEALTHSPAN_CONTENT[id as HealthspanCardKey].sources,
      info: infoRows(id, standing, note, {
        notInScore: notInScore(m.label, "signal"),
      }),
      historyHref: `#/healthspan/${id}`,
    }
  })
}

/** The words of the segment holding `v` ("yellow: ready for moderate
 * strain"), from its label after the range. */
function zoneWord(
  bar: Pick<CardBar, "min" | "max" | "segments">,
  v: number
): string {
  const n = bar.segments.length
  const i = Math.max(
    0,
    Math.min(n - 1, Math.floor(((v - bar.min) / (bar.max - bar.min)) * n))
  )
  const label = bar.segments[i].label ?? ""
  return label.includes(", ") ? label.slice(label.indexOf(", ") + 2) : label
}

/** The body readings: their role, the catalogue metric their content is
 * keyed by, and their label. */
export const BODY_CARDS: { role: BodyCardRole; key: BodyKey; label: string }[] =
  [
    { role: "bodyMass", key: "health/body_mass", label: "Weight" },
    { role: "bodyFat", key: "health/body_fat_fraction", label: "Body fat" },
    {
      role: "visceralFat",
      key: "health/visceral_fat_rating",
      label: "Visceral fat rating",
    },
  ]

/** A body card's unit as printed. */
const BODY_UNIT: Record<BodyCardRole, string> = {
  bodyMass: "kg",
  bodyFat: "%",
  visceralFat: "",
}

const BODY_PRECISION: Record<BodyCardRole, number> = {
  bodyMass: 1,
  bodyFat: 1,
  visceralFat: 0,
}

function bodyCards(
  inp: HealthspanCardInputs,
  axis: { start: string; end: string }
): HealthspanCardData[] {
  const { data } = inp
  const all = BODY_CARDS.flatMap((c) => data.body[c.role])
  const markers = sourceMarkers(sourcesByCount(all, data.sources), data.sources)
  const heightM = data.heightM
  const sex = inp.profile.sex
  const age = inp.profile.birthDate
    ? (inp.today.getTime() - Date.parse(`${inp.profile.birthDate}T00:00:00Z`)) /
      (365.2425 * 86_400_000)
    : undefined
  return BODY_CARDS.map(({ role, key, label }) => {
    const results = data.body[role]
    const name = (r: { source?: string }) => sourceName(data.sources, r.source)
    const last = results[results.length - 1]
    const prev = results[results.length - 2]
    const fmt = (v: number) =>
      v.toLocaleString("en-GB", { maximumFractionDigits: BODY_PRECISION[role] })
    const unitText = BODY_UNIT[role]
    const show = (v: number) =>
      unitText === "%"
        ? `${fmt(v)}%`
        : unitText
          ? `${fmt(v)} ${unitText}`
          : fmt(v)
    const headline: CardHeadline = last
      ? {
          value: unitText === "%" ? `${fmt(last.value)}%` : fmt(last.value),
          unit: unitText && unitText !== "%" ? unitText : undefined,
          context: `${formatDate(last.at)}, ${name(last)}`,
        }
      : { value: "No data", context: "no readings yet" }
    const inAxis = results.filter((r) => r.at.slice(0, 10) >= axis.start)
    const points: TrendPoint[] = inAxis.map((r) => {
      const mk = markOf(markers, r)
      return {
        at: r.at,
        value: r.value,
        marker: mk.marker,
        series: r.source ?? "",
        detail: [mk.name],
      }
    })
    // The scale: BMI from the newest height for weight, Gallagher's bands
    // for the sex and age for body fat, the rating's own bands for visceral
    // fat; without the input, a stated reason in place of the bar.
    const scale =
      role === "bodyMass"
        ? bmiScale(heightM)
        : role === "bodyFat"
          ? bodyFatScale(sex, age)
          : visceralScale()
    const markersOf: CardMarker[] = [
      ...(last
        ? [
            {
              at: last.value,
              side: "above" as const,
              label: "Latest reading",
              prefix: "latest",
              valueText: show(last.value),
            },
          ]
        : []),
      ...(prev && role !== "visceralFat"
        ? [
            {
              at: prev.value,
              side: "below" as const,
              label: "Previous reading",
              prefix: "previous",
              valueText: show(prev.value),
            },
          ]
        : []),
    ]
    const bar = scale && last ? barOf(label, scale, markersOf) : undefined
    const line = bar
      ? undefined
      : !last
        ? "No readings yet."
        : role === "bodyMass"
          ? "Needs your height for the BMI scale."
          : "Needs your birth date and sex for a range."
    // The band in words beside the colour.
    let badges: CardBadge[] = []
    if (last && bar) {
      const word =
        role === "bodyMass" && heightM
          ? bmiBand(last.value / (heightM * heightM))
          : role === "bodyFat" && sex && age !== undefined
            ? bodyFatBand(last.value, sex, age)
            : {
                tone:
                  last.value <= 12
                    ? ("success" as const)
                    : ("warning" as const),
                word: last.value <= 12 ? "healthy" : "excess",
              }
      badges = [
        {
          label: `${word.word.charAt(0).toUpperCase()}${word.word.slice(1)}${role === "visceralFat" ? " range" : ""}`,
          tone: word.tone,
        },
      ]
    }
    const note = bodySuggestion({
      metric: key,
      latest: last?.value,
      previous: prev
        ? { value: prev.value, date: formatDate(prev.at) }
        : undefined,
      heightM,
      sex,
      age,
    })
    const standing = last
      ? [
          `${show(last.value)}, ${formatDate(last.at)}, ${name(last)}.`,
          prev
            ? `Previous ${show(prev.value)}, ${formatDate(prev.at)}.`
            : "No earlier reading.",
          bar ? `Zone: ${zoneWord(bar, last.value)}.` : "",
        ]
          .filter(Boolean)
          .join(" ")
      : "No readings yet."
    return {
      id: `body-${role}`,
      key: role,
      kind: "body",
      group: "body",
      label,
      findings: [],
      statuses: cardStatuses({ findings: [] }),
      badges,
      headline,
      bar,
      line,
      note,
      chart: {
        kind: "readings",
        label: `${label}, readings`,
        data: points,
        bands: bar
          ? clipBands(
              zoneBands(bar),
              points.map((d) => d.value)
            )
          : [],
        unit: unitText,
        formatValue: fmt,
        // No trend from fewer than two readings.
        chip: last && prev ? changeChip(last.value, prev.value) : undefined,
        valueRange:
          points.length === 1
            ? { min: points[0].value * 0.9, max: points[0].value * 1.1 }
            : undefined,
      },
      sources: HEALTHSPAN_CONTENT[key].sources,
      info: infoRows(key, standing, note, {
        measuredBy:
          role === "bodyMass" && !heightM
            ? `${HEALTHSPAN_CONTENT[key].measuredBy} Needs your height for the BMI scale.`
            : undefined,
        notInScore: notInScore(label, "body"),
      }),
      historyHref: `#/healthspan/${role}`,
    }
  })
}

/** The focus list: the contributor cards with `adds` or `slipping`, most
 * years at stake first. The hero's "At stake" sums the same list. */
export function focusCards(cards: HealthspanCardData[]): HealthspanCardData[] {
  return cards
    .filter(
      (c) =>
        c.kind === "contributor" &&
        (c.statuses.has("adds") || c.statuses.has("slipping"))
    )
    .sort((a, b) => atStake(b.metric as Metric) - atStake(a.metric as Metric))
}

/** The years at stake over the focus list: the sum of the rows' badges as
 * printed (to a tenth each), so the hero agrees with the list under it. */
export function totalAtStake(cards: HealthspanCardData[]): number {
  return focusCards(cards).reduce(
    (a, c) => a + tenths(atStake(c.metric as Metric)),
    0
  )
}

/** The headline while a details chart point is selected: the point's
 * value and "Selected: 12 Jul 2026, 30-day average 7:05 h". */
export function selectedHeadline(
  chart: CardChart,
  point: { at: string | number | Date; value: number; detail?: string[] }
): Pick<CardHeadline, "value" | "unit" | "context"> {
  const day = new Date(point.at).toISOString().slice(0, 10)
  const base = chart.baseline?.find((b) => b.at.slice(0, 10) === day)
  const v = chart.formatValue(point.value)
  const attached = chart.unit === "%" || chart.unit === "/min"
  return {
    value: attached ? `${v}${chart.unit}` : v,
    unit: attached || !chart.unit ? undefined : chart.unit,
    context: `Selected: ${formatDate(new Date(point.at))}${
      point.detail?.length ? `, ${point.detail.join(", ")}` : ""
    }${
      base && chart.baselineName
        ? `, ${chart.baselineName} ${chart.formatValue(base.value)}${attached ? chart.unit : chart.unit ? ` ${chart.unit}` : ""}`
        : ""
    }`,
  }
}

/** The Healthspan screen's key, built from what the visible
 * cards draw, so it never names a layer the page does not show:
 * - the bars' pointers (6-month, 30-day, latest reading) and their tick;
 * - the segment colours by the words true of every bar: Better, Worse,
 *   Poor (only when a red segment is drawn) and No effect (grey on a toned
 *   bar; a bar that is grey for want of a target adds nothing);
 * - with Details on, each chart type's lines, the bands and ribbons drawn,
 *   VO2 max's target line, and the body charts' source shapes;
 * - with the trend view drawn, its weekly snapshots.
 * What a contributor's colour means in years stays on its card. */
export function healthspanKey(
  cards: HealthspanCardData[],
  opts: { details: boolean; trendView: boolean }
): ChartKeyItem[] {
  const items: ChartKeyItem[] = []
  const bars = cards.map((c) => c.bar).filter((b): b is CardBar => !!b)
  const prefixes = new Set(bars.flatMap((b) => b.markers.map((m) => m.prefix)))
  // The pointers in the order a card reads them: above the bar, then below.
  const POINTERS: [string, string, "above" | "below"][] = [
    ["6mo", "6-month average", "above"],
    ["today", "Today", "above"],
    ["last", "Last night", "above"],
    ["latest", "Latest reading", "above"],
    ["30d", "30-day average", "below"],
    ["7n", "7-night average", "below"],
    ["previous", "Previous reading", "below"],
  ]
  for (const [prefix, label, side] of POINTERS)
    if (prefixes.has(prefix))
      items.push({
        key: `ptr-${prefix}`,
        label,
        glyph: { kind: "pointer", side },
      })
  // A target or threshold draws a solid notch, a personal baseline a
  // hollow one; each key item only when a shown bar draws it.
  const ticks = bars.flatMap((b) => b.ticks ?? [])
  if (ticks.some((t) => t.variant !== "baseline"))
    items.push({
      key: "tick",
      label: "Target or threshold",
      glyph: { kind: "tick" },
    })
  if (ticks.some((t) => t.variant === "baseline"))
    items.push({
      key: "baseline",
      label: "Your baseline",
      glyph: { kind: "tick", variant: "baseline" },
    })
  const tones = new Set(
    bars.filter((b) => !b.untoned).flatMap((b) => b.segments.map((s) => s.tone))
  )
  if (tones.has("success"))
    items.push({
      key: "better",
      label: "Better",
      glyph: { kind: "band", tone: "success" },
    })
  if (tones.has("warning"))
    items.push({
      key: "worse",
      label: "Worse",
      glyph: { kind: "band", tone: "warning" },
    })
  if (tones.has("danger"))
    items.push({
      key: "poor",
      label: "Poor",
      glyph: { kind: "band", tone: "danger" },
    })
  if (tones.has("neutral"))
    items.push({
      key: "no-effect",
      label: "No effect or usual",
      glyph: { kind: "band", tone: "neutral" },
    })
  if (tones.has("accent"))
    items.push({
      key: "strain",
      label: "Strain level",
      glyph: { kind: "band", tone: "accent" },
    })

  if (opts.details) {
    const charts = cards.map((c) => c.chart).filter((c) => c.data.length)
    if (charts.some((c) => c.kind === "weekly"))
      items.push(
        {
          key: "weekly-30d",
          label: "30-day average by week",
          glyph: { kind: "line", tone: "accent" },
        },
        {
          key: "weekly-6mo",
          label: "6-month average by week",
          glyph: { kind: "line", tone: "neutral" },
        }
      )
    const daily = charts.filter((c) => c.kind === "daily")
    if (daily.length)
      items.push({
        key: "each",
        label: "Each night or day",
        glyph: { kind: "line", tone: "accent" },
      })
    if (
      daily.some(
        (c) =>
          c.baseline?.length && c.baseline.every((b) => b.low === undefined)
      )
    )
      items.push({
        key: "daily-30d",
        label: "Rolling 30-day average",
        glyph: { kind: "line", tone: "neutral" },
      })
    if (daily.some((c) => c.baseline?.some((b) => b.low !== undefined)))
      items.push({
        key: "ribbon",
        label: "30-day mean and one deviation",
        glyph: { kind: "band", tone: "neutral" },
      })
    if (charts.some((c) => c.bands.some((b) => b.variant !== "outline")))
      items.push({
        key: "zones",
        label: "Zones from the bar",
        glyph: { kind: "band", tone: "success" },
      })
    if (charts.some((c) => c.bands.some((b) => b.variant === "outline")))
      items.push({
        key: "target-line",
        label: "VO2 max target",
        glyph: { kind: "band", tone: "warning", variant: "outline" },
      })
    const shapes = new Map<string, ChartKeyItem>()
    for (const c of charts.filter((x) => x.kind === "readings"))
      for (const p of c.data)
        if (p.marker && p.detail?.[0] && !shapes.has(p.detail[0]))
          shapes.set(p.detail[0], {
            key: `source-${p.detail[0]}`,
            label: p.detail[0],
            glyph: { kind: "marker", shape: p.marker },
          })
    items.push(...shapes.values())
  }
  if (opts.trendView)
    items.push({
      key: "weekly",
      label: "Weekly snapshot",
      glyph: { kind: "marker", shape: "circle", tone: "accent" },
    })
  return items
}
