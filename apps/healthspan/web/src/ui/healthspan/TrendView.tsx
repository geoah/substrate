import { useCallback, useState } from "react"
import { PACE_MAX, PACE_MIN } from "../../engine/engine"
import type { Snapshot } from "../../engine/report"
import { paceText } from "../../healthspan/healthspanMetrics"
import { CHART_GUTTER } from "../../lib/chart"
import { formatMonth, formatShortDate } from "../../lib/format"
import { ReadoutLine } from "../ReadoutLine"
import {
  TrendChart,
  type TrendChartProps,
  type TrendPoint,
} from "../TrendChart"
import { ageLines } from "./HealthspanHero"

const week = (s: Snapshot) =>
  `week of ${formatShortDate(`${s.weekStart}T12:00:00Z`)}`

/** One weekly chart with its readout line above it: the newest snapshot
 * at rest, the hovered one while a point is active. */
function WeeklyChart({
  title,
  snaps,
  value,
  text,
  axis,
  extra,
}: {
  title: string
  snaps: Snapshot[]
  value: (s: Snapshot) => number
  text: (s: Snapshot) => string
  axis: { start: string; end: string }
  extra: Partial<TrendChartProps>
}) {
  const [active, setActive] = useState<TrendPoint | null>(null)
  const onActive = useCallback((p: TrendPoint | null) => setActive(p), [])
  const newest = snaps[snaps.length - 1]
  const at = active
    ? snaps.find((s) => s.date === String(active.at).slice(0, 10))
    : undefined
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-[10px] bg-card px-4 pt-3 pb-4">
      <h3 className="text-[15px] font-semibold">{title}</h3>
      <ReadoutLine
        latest={{
          value: newest ? text(newest) : "None",
          context: newest ? week(newest) : "",
        }}
        selected={at ? { date: week(at), value: text(at) } : undefined}
      />
      <TrendChart
        label={`${title} by week`}
        data={snaps.map((s) => ({
          at: s.date,
          value: value(s),
          series: "line",
        }))}
        formatTime={formatMonth}
        timeRange={axis}
        height={200}
        gutter={CHART_GUTTER}
        readout="none"
        onActiveChange={onActive}
        syncKey="healthspan"
        {...extra}
      />
    </div>
  )
}

/** The pace of aging and the age score by week over the screen's axis. */
export function TrendView({
  snaps,
  axis,
}: {
  snaps: Snapshot[]
  axis: { start: string; end: string }
}) {
  return (
    <section className="mb-7" aria-labelledby="trend-title">
      <h2
        id="trend-title"
        className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase"
      >
        Trend view
      </h2>
      <div className="grid gap-4 md:grid-cols-2">
        <WeeklyChart
          title="Pace of aging"
          snaps={snaps}
          axis={axis}
          value={(s) => s.pace}
          text={(s) => paceText(s.pace)}
          extra={{
            valueRange: { min: PACE_MIN, max: PACE_MAX },
            bands: [
              {
                from: 1,
                to: 1,
                tone: "neutral",
                label: "Steady, 1.0x",
                variant: "outline",
              },
            ],
            formatValue: paceText,
            formatTick: (v) => paceText(v),
            legend: [
              {
                key: "steady",
                label: "Steady, 1.0x",
                glyph: { kind: "band", tone: "neutral", variant: "outline" },
              },
            ],
          }}
        />
        <WeeklyChart
          title="Age score"
          snaps={snaps}
          axis={axis}
          value={(s) => s.ageScore}
          text={(s) => {
            const a = ageLines(s.ageScore, s.age)
            return `${a.score} years, age ${a.age}`
          }}
          extra={{
            baseline: snaps.map((s) => ({ at: s.date, value: s.age })),
            unit: "years",
            formatValue: (v) => v.toFixed(1),
            legend: [
              {
                key: "line",
                label: "Age score",
                glyph: { kind: "line", tone: "accent" },
              },
              {
                key: "age",
                label: "Your age",
                glyph: { kind: "line", tone: "neutral" },
              },
            ],
          }}
        />
      </div>
    </section>
  )
}
