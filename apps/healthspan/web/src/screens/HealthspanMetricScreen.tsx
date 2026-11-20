import { describe } from "@substrate/client"
import { useCallback, useMemo, useState, type ReactNode } from "react"
import { useBackend } from "../data"
import type { HealthspanView } from "../data/healthspan"
import { targetFor } from "../engine/curves"
import type { Metric } from "../engine/report"
import {
  badgeWord,
  BODY_CARDS,
  contributorChartBands,
  signalChartBands,
} from "../healthspan/healthspanCards"
import {
  baselineSeries,
  chartFormat,
  CONTRIBUTOR_LABELS,
  contributorVerdict,
  HEALTHSPAN_METRICS,
  impactText,
  tickFormat,
  valueText,
  type HealthspanMetric,
} from "../healthspan/healthspanMetrics"
import { sourceName, type BodyCardRole } from "../healthspan/read"
import { CHART_GUTTER, tickText } from "../lib/chart"
import { formatDate, formatMonth } from "../lib/format"
import {
  markOf,
  seriesLegend,
  sourceMarkers,
  sourcesByCount,
} from "../lib/sourceMarkers"
import { go } from "../route"
import { addDays, isoDay, rowOf } from "../rules/healthspan"
import type { Finding } from "../rules/types"
import { Badge } from "../ui/Badge"
import { EmptyState } from "../ui/EmptyState"
import { ReadoutLine } from "../ui/ReadoutLine"
import { Screen } from "../ui/Screen"
import { SkeletonText } from "../ui/Skeleton"
import { TrendChart, type TrendPoint } from "../ui/TrendChart"

// One Healthspan card over time: a contributor week by week, a signal day
// by day over the last 182 days, or a body reading scan by scan. The
// readout line, the chart on its own axis, the values.

const HEIGHT = 280
const back = {
  label: "Healthspan",
  onClick: () => go({ screen: "healthspan", query: "" }),
}

/** The findings on this card, each with its sentence. */
function Notes({ findings }: { findings: Finding[] }) {
  if (!findings.length) return null
  return (
    <ul className="mb-4 flex flex-col gap-2 rounded-[10px] bg-card px-4 py-3">
      {findings.map((f) => (
        <li key={f.subject} className="flex flex-col items-start gap-1">
          <Badge label={badgeWord(f)} tone={f.tone} />
          <span className="text-[15px] leading-snug">{`${f.detail}.`}</span>
        </li>
      ))}
    </ul>
  )
}

function Panel(props: { title: string; meta?: string; children: ReactNode }) {
  return (
    <section className="mb-7">
      <h2 className="mb-1.5 flex items-baseline gap-2 px-4 text-[13px] tracking-wide text-secondary uppercase">
        {props.title}
        {props.meta && <span className="normal-case">{props.meta}</span>}
      </h2>
      <div className="flex flex-col gap-2 rounded-[10px] bg-card p-4">
        {props.children}
      </div>
    </section>
  )
}

/** A table of bounded values, newest first; the first column the date. */
function Values(props: {
  label: string
  head: string[]
  rows: { key: string; cells: ReactNode[] }[]
  empty: string
}) {
  if (!props.rows.length)
    return <EmptyState title={props.empty} variant="compact" />
  return (
    <div className="mt-2 max-h-[420px] overflow-y-auto">
      <table className="w-full text-[15px]" aria-label={props.label}>
        <thead>
          <tr className="text-[13px] text-secondary">
            {props.head.map((h, i) => (
              <th
                key={h}
                scope="col"
                className={`sticky top-0 bg-card py-1 font-normal ${i ? "pl-3 text-right" : "text-left"}`}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {props.rows.map((r) => (
            <tr key={r.key} className="border-t border-separator/60">
              {r.cells.map((c, i) => (
                <td
                  key={i}
                  className={`py-1.5 align-top ${i ? "pl-3 text-right tabular-nums" : ""}`}
                >
                  {c}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function HealthspanMetricScreen(props: { id: string }) {
  const read = useBackend().healthspan.useHealthspan()
  const view = read.data
  if (!view)
    return (
      <Screen title="Healthspan" back={back}>
        {read.error !== undefined ? (
          <EmptyState
            title="Healthspan could not be read"
            description={describe(read.error)}
            action={{ label: "Try again", onClick: read.refresh }}
          />
        ) : (
          <SkeletonText lines={4} />
        )}
      </Screen>
    )
  const notes = view.findings.filter(
    (f) => rowOf(f) === props.id && !f.subject.startsWith("sync") && f.detail
  )
  const contributor = (view.report ?? view.averages).snapshots
    .at(-1)
    ?.metrics.find((m) => m.id === props.id)
  if (contributor)
    return <ContributorHistory view={view} m={contributor} notes={notes} />
  const signal = HEALTHSPAN_METRICS.find((m) => m.id === props.id)
  if (signal) return <SignalHistory view={view} m={signal} notes={notes} />
  const body = BODY_CARDS.find((b) => b.role === props.id)
  if (body)
    return <BodyHistory view={view} role={body.role} label={body.label} />
  return (
    <Screen title="Healthspan" back={back}>
      <EmptyState
        title="Nothing to show here"
        description="This link names no Healthspan card."
        action={{ label: "Open Healthspan", onClick: back.onClick }}
      />
    </Screen>
  )
}

function ContributorHistory({
  view,
  m,
  notes,
}: {
  view: HealthspanView
  m: Metric
  notes: Finding[]
}) {
  const [active, setActive] = useState<TrendPoint | null>(null)
  const onActive = useCallback((p: TrendPoint | null) => setActive(p), [])
  const scored = !!view.report
  const label = CONTRIBUTOR_LABELS[m.id]
  const fmt = chartFormat(m.format)
  const history = m.history
  const age = view.report?.snapshots.at(-1)?.age
  const target =
    scored && view.profile.sex && age !== undefined
      ? targetFor(m.id, age, view.profile.sex)
      : undefined
  // The same zones as the card's detail chart.
  const bands = contributorChartBands(
    m,
    target,
    history
      .flatMap((p) => [p.thirtyDay, p.sixMonth])
      .filter((v): v is number => v !== null)
  )
  const newest = [...history].reverse().find((p) => p.thirtyDay !== null)
  const sel = active
    ? history.find((p) => p.date === String(active.at).slice(0, 10))
    : undefined
  const week = (d: string) => `week of ${formatDate(`${d}T12:00:00Z`)}`
  return (
    <Screen
      title={label}
      subtitle={scored ? contributorVerdict(m).body : undefined}
      back={back}
    >
      <Notes findings={notes} />
      <Panel title="Week by week">
        <ReadoutLine
          latest={{
            value: newest ? valueText(newest.thirtyDay, m.format) : "None",
            context: newest ? `30-day average, ${week(newest.date)}` : "",
          }}
          selected={
            sel
              ? {
                  date: week(sel.date),
                  value: valueText(sel.thirtyDay, m.format),
                  extra: `6-month average ${valueText(sel.sixMonth, m.format)}`,
                }
              : undefined
          }
        />
        <TrendChart
          label={`${label}, 30-day average by week`}
          data={history
            .filter((p) => p.thirtyDay !== null)
            .map((p) => ({
              at: p.date,
              value: p.thirtyDay as number,
              series: "line",
            }))}
          baseline={history
            .filter((p) => p.sixMonth !== null)
            .map((p) => ({ at: p.date, value: p.sixMonth as number }))}
          bands={bands}
          unit={fmt.unit}
          formatValue={fmt.value}
          formatTick={tickFormat(m.format) ?? tickText}
          formatTime={formatMonth}
          gutter={CHART_GUTTER}
          height={HEIGHT}
          readout="none"
          onActiveChange={onActive}
          empty="No weeks yet"
          legend={[
            {
              key: "line",
              label: "30-day average",
              glyph: { kind: "line", tone: "accent" },
            },
            {
              key: "base",
              label: "6-month average",
              glyph: { kind: "line", tone: "neutral" },
            },
            ...(bands.some((b) => b.variant !== "outline")
              ? [
                  {
                    key: "zones",
                    label: "Zones from the bar",
                    glyph: { kind: "band" as const, tone: "success" as const },
                  },
                ]
              : []),
            ...(bands.some((b) => b.variant === "outline")
              ? [
                  {
                    key: "target",
                    label: "VO2 max target",
                    glyph: {
                      kind: "band" as const,
                      tone: "warning" as const,
                      variant: "outline" as const,
                    },
                  },
                ]
              : []),
          ]}
        />
        <Values
          label={`${label} by week`}
          head={["Week", "6-month", "30-day", ...(scored ? ["Years"] : [])]}
          rows={[...history].reverse().map((p) => ({
            key: p.date,
            cells: [
              formatDate(`${p.date}T12:00:00Z`),
              valueText(p.sixMonth, m.format),
              valueText(p.thirtyDay, m.format),
              ...(scored
                ? [p.impact === null ? "No data" : impactText(p.impact)]
                : []),
            ],
          }))}
          empty="No weeks yet"
        />
      </Panel>
    </Screen>
  )
}

function SignalHistory({
  view,
  m,
  notes,
}: {
  view: HealthspanView
  m: HealthspanMetric
  notes: Finding[]
}) {
  const [active, setActive] = useState<TrendPoint | null>(null)
  const onActive = useCallback((p: TrendPoint | null) => setActive(p), [])
  const end = isoDay(view.today)
  const start = addDays(end, -181)
  const series = m.daily(view.series)
  const days = useMemo(
    () =>
      [...series]
        .filter(([d]) => d >= start && d <= end)
        .sort(([a], [b]) => a.localeCompare(b)),
    [series, start, end]
  )
  const base = useMemo(
    () => (m.hasBaseline ? baselineSeries(series) : []),
    [m, series]
  )
  const baseByDay = new Map(base.map((b) => [b.at, b]))
  const fmt = (v: number) =>
    m.precision ? v.toFixed(m.precision) : Math.round(v).toLocaleString("en-GB")
  const unit = !m.unit
    ? ""
    : m.unit === "%" || m.unit.startsWith("°") || m.unit === "/min"
      ? m.unit
      : ` ${m.unit}`
  const newest = days[days.length - 1]
  const dayBands = signalChartBands(
    m.id,
    days.map(([, v]) => v)
  )
  const selDay = active ? String(active.at).slice(0, 10) : undefined
  const selBase = selDay ? baseByDay.get(selDay) : undefined
  const dayText = (d: string) => formatDate(`${d}T12:00:00Z`)
  return (
    <Screen title={m.label} subtitle={m.source} back={back}>
      <Notes findings={notes} />
      <Panel title="Day by day" meta="Last 182 days">
        <ReadoutLine
          latest={{
            value: newest ? `${fmt(newest[1])}${unit}` : "None",
            context: newest ? dayText(newest[0]) : "",
          }}
          selected={
            active && selDay
              ? {
                  date: dayText(selDay),
                  value: `${fmt(active.value)}${unit}`,
                  extra: selBase
                    ? `30-day mean ${fmt(selBase.value)}${unit}`
                    : undefined,
                }
              : undefined
          }
        />
        <TrendChart
          label={`${m.label}, last 182 days`}
          data={days.map(([at, value]) => ({ at, value, series: "day" }))}
          baseline={
            m.hasBaseline ? base.filter((b) => b.at >= start) : undefined
          }
          bands={dayBands}
          unit={m.unit}
          formatValue={fmt}
          formatTick={tickText}
          formatTime={formatMonth}
          timeRange={{ start, end }}
          gutter={CHART_GUTTER}
          height={HEIGHT}
          readout="none"
          onActiveChange={onActive}
          empty="No values in the last 182 days"
          legend={[
            {
              key: "day",
              label: "Each day",
              glyph: { kind: "line", tone: "accent" },
            },
            ...(m.hasBaseline
              ? [
                  {
                    key: "base",
                    label: "30-day mean and one deviation",
                    glyph: { kind: "line" as const, tone: "neutral" as const },
                  },
                ]
              : []),
            ...(dayBands.length
              ? [
                  {
                    key: "zones",
                    label: "Zones from the card's bar",
                    glyph: { kind: "band" as const, tone: "success" as const },
                  },
                ]
              : []),
          ]}
        />
        <Values
          label={`${m.label} by day`}
          head={["Day", m.label, ...(m.hasBaseline ? ["Baseline"] : [])]}
          rows={[...days].reverse().map(([d, v]) => {
            const b = baseByDay.get(d)
            return {
              key: d,
              cells: [
                dayText(d),
                `${fmt(v)}${unit}`,
                ...(m.hasBaseline ? [b ? `${fmt(b.value)}${unit}` : ""] : []),
              ],
            }
          })}
          empty="No values in the last 182 days"
        />
      </Panel>
    </Screen>
  )
}

const BODY_UNIT: Record<BodyCardRole, { unit: string; digits: number }> = {
  bodyMass: { unit: " kg", digits: 1 },
  bodyFat: { unit: "%", digits: 1 },
  visceralFat: { unit: "", digits: 0 },
}

function BodyHistory({
  view,
  role,
  label,
}: {
  view: HealthspanView
  role: BodyCardRole
  label: string
}) {
  const [active, setActive] = useState<TrendPoint | null>(null)
  const onActive = useCallback((p: TrendPoint | null) => setActive(p), [])
  const results = view.series.body[role]
  const names = view.series.sources
  const markers = sourceMarkers(sourcesByCount(results, names), names)
  const { unit, digits } = BODY_UNIT[role]
  const fmt = (v: number) =>
    v.toLocaleString("en-GB", {
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    })
  const newest = results[results.length - 1]
  const legend = seriesLegend(results, markers)
  return (
    <Screen
      title={label}
      subtitle="Body composition results from every source, scan by scan."
      back={back}
    >
      <Panel title="Readings">
        <ReadoutLine
          latest={{
            value: newest ? `${fmt(newest.value)}${unit}` : "None",
            context: newest
              ? `${formatDate(newest.at)}, ${sourceName(names, newest.source)}`
              : "",
          }}
          selected={
            active
              ? {
                  date: formatDate(new Date(active.at)),
                  value: `${fmt(active.value)}${unit}`,
                  extra: active.detail?.[0],
                }
              : undefined
          }
        />
        <TrendChart
          label={`${label}, readings`}
          data={results.map((r) => {
            const mk = markOf(markers, r)
            return {
              at: r.at,
              value: r.value,
              marker: mk.marker,
              series: r.source ?? "",
              detail: [mk.name],
            }
          })}
          unit={unit.trim()}
          formatValue={fmt}
          formatTick={tickText}
          formatTime={formatMonth}
          gutter={CHART_GUTTER}
          height={HEIGHT}
          readout="none"
          onActiveChange={onActive}
          emptyBefore={false}
          empty="No readings yet"
          legend={legend.map((l) => ({
            key: l.key,
            label: l.label,
            glyph: { kind: "marker" as const, shape: l.marker },
          }))}
        />
        <Values
          label={`${label} readings`}
          head={["Reading", label]}
          rows={[...results].reverse().map((r, i) => ({
            key: `${r.at}-${r.source ?? ""}-${i}`,
            cells: [
              <span key="d" className="flex flex-col">
                <span>{formatDate(r.at)}</span>
                <span className="text-[13px] text-secondary">
                  {sourceName(names, r.source)}
                </span>
              </span>,
              `${fmt(r.value)}${unit}`,
            ],
          }))}
          empty="No readings yet"
        />
      </Panel>
    </Screen>
  )
}
