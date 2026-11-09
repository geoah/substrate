import type { ReactNode } from "react"
import {
  LOCK_RECOVERIES,
  LOCK_WINDOW_NIGHTS,
  PACE_MAX,
  PACE_MIN,
} from "../../engine/engine"
import { atStake } from "../../engine/focus"
import { formatImpact } from "../../engine/format"
import type { Metric, Report } from "../../engine/report"
import {
  focusCards,
  totalAtStake,
  type HealthspanCardData,
} from "../../healthspan/healthspanCards"
import {
  CONTRIBUTOR_LABELS,
  paceText,
} from "../../healthspan/healthspanMetrics"
import { PACE_SCALE, paceWord } from "../../healthspan/segmentTones"
import { formatDate, formatShortDate } from "../../lib/format"
import { go } from "../../route"
import { Badge } from "../Badge"
import { EmptyState } from "../EmptyState"
import { ZoneBar } from "../ZoneBar"

// The screen's hero: the age score, the pace of aging with its scale, and
// the years at stake over the focus list with a link to what needs
// attention. Without a birth date and sex, without nights, or before the
// score unlocks, an empty state says why. The 21 and 31 come from the
// engine.

export type HeroState = "noProfile" | "noNights" | "locked" | "ready"

export function heroState(
  report: Report | undefined,
  hasProfile: boolean,
  hasNights: boolean
): HeroState {
  if (!hasProfile) return "noProfile"
  if (!hasNights) return "noNights"
  if (!report?.snapshots.length) return "locked"
  return "ready"
}

/** The score and the age to one decimal, and the difference worked out
 * from those two printed numbers, so the line never contradicts itself
 * ("42.7 years, 0.3 years younger than your age of 43.0"). */
export function ageLines(
  ageScore: number,
  age: number
): { score: string; age: string; sub: string } {
  const score = ageScore.toFixed(1)
  const a = age.toFixed(1)
  const d = Math.round((Number(score) - Number(a)) * 10) / 10
  const sub =
    d === 0
      ? `Right on track for your age of ${a}`
      : `${Math.abs(d).toFixed(1)} ${Math.abs(d) === 1 ? "year" : "years"} ${d < 0 ? "younger" : "older"} than your age of ${a}`
  return { score, age: a, sub }
}

/** The "At stake" figure and its sentence. */
export function atStakeLines(cards: HealthspanCardData[]): {
  value: string
  sub: string
} {
  const focus = focusCards(cards)
  if (!focus.length)
    return { value: "0.0", sub: "Nothing adds years or is slipping" }
  const m = focus[0].metric as Metric
  return {
    value: formatImpact(totalAtStake(cards)),
    sub: `${CONTRIBUTOR_LABELS[m.id]} adds the most: ${formatImpact(atStake(m))} if this month holds`,
  }
}

function Stat(props: {
  label: string
  value: string
  unit?: string
  sub?: string
  children?: ReactNode
}) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 rounded-[10px] bg-card px-4 pt-2.5 pb-3 sm:gap-1 sm:pt-3 sm:pb-4">
      <p className="text-[13px] text-secondary">{props.label}</p>
      <p className="text-[28px] leading-[34px] font-semibold tabular-nums sm:text-[34px] sm:leading-[40px]">
        {props.value}
        {props.unit && (
          <span className="text-[15px] font-normal text-secondary">
            {" "}
            {props.unit}
          </span>
        )}
      </p>
      {props.sub && <p className="text-[13px] text-secondary">{props.sub}</p>}
      {props.children}
    </div>
  )
}

export function HealthspanHero({
  report,
  cards,
  hasProfile,
  hasNights,
}: {
  report: Report | undefined
  cards: HealthspanCardData[]
  hasProfile: boolean
  hasNights: boolean
}) {
  const state = heroState(report, hasProfile, hasNights)
  if (state === "noProfile")
    return (
      <EmptyState
        className="mb-6 rounded-[10px] bg-card"
        title="Your birth date and sex give the age score"
        description="Without them the cards show your averages, with no targets and no years."
        action={{
          label: "Finish getting started",
          onClick: () => go({ screen: "start" }),
        }}
      />
    )
  if (state === "noNights")
    return (
      <EmptyState
        className="mb-6 rounded-[10px] bg-card"
        title="No nights yet"
        description="The age score reads sleeps and resting heart rate from the sources your repository holds."
        action={{
          label: "Open Sources",
          onClick: () => go({ screen: "sources" }),
        }}
      />
    )
  if (state === "locked")
    return (
      <EmptyState
        className="mb-6 rounded-[10px] bg-card"
        title={`The age score unlocks after ${LOCK_RECOVERIES} nights with a resting heart rate in ${LOCK_WINDOW_NIGHTS}`}
        description={`${report?.paused?.recoveries ?? 0} in the last ${LOCK_WINDOW_NIGHTS} nights.`}
      />
    )
  const r = report as Report
  const snap = r.snapshots[r.snapshots.length - 1]
  const { score, sub: ageLine } = ageLines(snap.ageScore, snap.age)
  const sub = r.paused
    ? `Needs ${LOCK_RECOVERIES} nights with a resting heart rate in ${LOCK_WINDOW_NIGHTS}; ${r.paused.recoveries} in the last ${LOCK_WINDOW_NIGHTS}. Showing the week of ${formatDate(`${snap.weekStart}T12:00:00Z`)}.`
    : ageLine
  const week = formatShortDate(`${snap.weekStart}T12:00:00Z`)
  const when = r.paused
    ? `Week of ${week}; updates once nights return`
    : `Week of ${week}, next update in ${r.nextUpdateDays} ${r.nextUpdateDays === 1 ? "day" : "days"}`
  const stake = atStakeLines(cards)
  return (
    <div className="mb-6 grid gap-3 sm:grid-cols-3 sm:gap-4">
      <Stat label="Age score" value={score} unit="years" sub={sub}>
        {(snap.calibrating ||
          r.paused ||
          snap.contributors < snap.metrics.length) && (
          <span className="flex flex-wrap gap-1.5">
            {snap.calibrating && <Badge label="Calibrating" tone="info" />}
            {r.paused && <Badge label="Paused" tone="warning" />}
            {snap.contributors < snap.metrics.length && (
              <Badge
                label={`${snap.contributors} of ${snap.metrics.length} contributors`}
                isMono
              />
            )}
          </span>
        )}
      </Stat>
      <Stat
        label="Pace of aging"
        value={paceText(snap.pace)}
        sub={paceWord(snap.pace)}
      >
        <div className="mt-1 flex flex-col gap-1.5">
          <ZoneBar
            label="Pace of aging scale"
            size="sm"
            min={PACE_SCALE.min}
            max={PACE_SCALE.max}
            segments={PACE_SCALE.segments}
            markers={[
              {
                at: snap.pace,
                side: "above",
                label: "Pace of aging",
                valueText: paceText(snap.pace),
              },
            ]}
            ticks={[{ at: 1, label: paceText(1) }]}
            minLabel={`Slow, ${paceText(PACE_MIN)}`}
            maxLabel={`Fast, ${paceText(PACE_MAX)}`}
          />
          <p className="text-[13px] text-secondary">{when}</p>
        </div>
      </Stat>
      <Stat label="At stake" value={stake.value} unit="years" sub={stake.sub}>
        {stake.value !== "0.0" && (
          <a
            href="#/healthspan?status=adds,slipping"
            className="text-[15px] text-accent-text underline-offset-2 hover:underline"
          >
            Show what needs attention
          </a>
        )}
      </Stat>
    </div>
  )
}
