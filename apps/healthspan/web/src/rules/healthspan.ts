// The Healthspan rules. Each finding attaches to the
// row it is about: `subject` is the row id, with a `:` suffix where a row
// holds a second finding (`rhr:elevated`, `vo2max:stale`); `rowOf` reads
// the row back.
//
// The nine contributors are judged by the age score itself, never by
// windows of their own: a contributor whose 30-day average would add
// `ADDS_YEARS` or more on the latest snapshot gets its word ("Short",
// "Few"), and one without data gets "No data". So the badge on a row always
// agrees with the bar under it.
//
// The signal rules keep the old app's thresholds: recovery by WHOOP's
// bands, resting heart rate and HRV a standard deviation off the 30-night
// baseline, breathing +1.5 per minute, skin temperature 0.5 °C, SpO2 95
// percent ("app rules", meant to be tuned), and a two-day sync gap. Every
// window ends today, never at the newest reading, so old data cannot pass
// for this week's. A 7-night window needs 5 nights, a baseline 14 of its 30
// days, and a "last night" reading counts only when it is dated today or
// yesterday. A feed that stopped is the "Sync stopped" finding's to report,
// not a current warning's.

import type { Sex } from "../engine/curves"
import { targetFor } from "../engine/curves"
import { BODY_CARRY_FORWARD_DAYS, THIRTY } from "../engine/engine"
import { ADDS_YEARS, isSlipping } from "../engine/focus"
import { formatImpact } from "../engine/format"
import type { MetricId, Report } from "../engine/report"
import {
  CONTRIBUTOR_LABELS,
  CONTRIBUTOR_WORDS,
  noDataText,
  valueText,
} from "../healthspan/healthspanMetrics"
import { formatDate } from "../lib/format"
import { type Daily, type HealthspanSeries, newest } from "../healthspan/read"
import {
  RECOVERY_GREEN,
  RECOVERY_YELLOW,
  recoveryWord,
  SPO2_LOW,
} from "../healthspan/segmentTones"
import type { Finding } from "./types"

const DAY = 86_400_000

/** A moment's local date, `YYYY-MM-DD`: the day the owner is living. */
export function isoDay(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`
}

export function addDays(day: string, n: number): string {
  return new Date(Date.parse(`${day}T00:00:00Z`) + n * DAY)
    .toISOString()
    .slice(0, 10)
}

/** The values of the days in [from, to). */
export function between(m: Daily, from: string, to: string): number[] {
  const out: number[] = []
  for (const [d, v] of m) if (d >= from && d < to) out.push(v)
  return out
}

export function mean(vs: number[]): number | undefined {
  return vs.length ? vs.reduce((a, b) => a + b, 0) / vs.length : undefined
}

/** Mean and population standard deviation. */
export function stats(
  vs: number[]
): { mean: number; sd: number; n: number } | undefined {
  const m = mean(vs)
  if (m === undefined) return undefined
  const sd = Math.sqrt(vs.reduce((a, v) => a + (v - m) ** 2, 0) / vs.length)
  return { mean: m, sd, n: vs.length }
}

/** The newest day with a value, not after `today`. */
export function newestDay(m: Daily, today: string): string | undefined {
  let best: string | undefined
  for (const d of m.keys()) if (d <= today && (!best || d > best)) best = d
  return best
}

/** The 30-day baseline before `day` (the day itself left out); undefined
 * with fewer than 14 days in it. */
export function baseline(
  m: Daily,
  day: string
): { mean: number; sd: number; n: number } | undefined {
  const s = stats(between(m, addDays(day, -30), day))
  return s && s.n >= COVERAGE.baseline ? s : undefined
}

/** Days with a value a window needs: 5 of 7, 21 of 30. */
export const COVERAGE = { week: 5, month: 21, baseline: 14 } as const

/** Days without a value after which a feed has stopped. A window's newest
 * value must be newer than this for the window to count. */
export const STALE_AFTER_DAYS = 2

export interface Window {
  /** Mean of the days with a value; undefined with none. */
  mean?: number
  /** Days with a value. */
  count: number
  /** Days in the window. */
  days: number
  /** First and last day of the window, inclusive. */
  from: string
  to: string
  /** The newest day with a value, if any. */
  newest?: string
  /** Whether `count` meets the window's coverage and the newest value is
   * recent (see `STALE_AFTER_DAYS`). */
  covered: boolean
}

/** The `days` days ending `to` (inclusive), and whether at least `need` of
 * them have a value with the newest no more than `STALE_AFTER_DAYS` before
 * `today`. */
export function windowOf(
  m: Daily,
  to: string,
  days: number,
  need: number,
  today: string
): Window {
  const from = addDays(to, -(days - 1))
  const vs = between(m, from, addDays(to, 1))
  const newest = newestDay(m, to)
  const recent =
    !!newest && newest >= from && newest >= addDays(today, -STALE_AFTER_DAYS)
  return {
    mean: mean(vs),
    count: vs.length,
    days,
    from,
    to,
    newest,
    covered: vs.length >= need && recent,
  }
}

/** `on 6 of the 7 nights to 29 Oct 2026`. */
export function coverageText(w: Window, unit: "nights" | "days"): string {
  return `on ${w.count} of the ${w.days} ${unit} to ${dayText(w.to)}`
}

/** Mean of the 7 days ending `end` (inclusive) and the baseline of the 30
 * days before them; undefined unless both are covered. */
export function recentVsBaseline(
  m: Daily,
  end: string,
  n = 7
):
  | {
      recent: number
      window: Window
      base: { mean: number; sd: number; n: number }
    }
  | undefined {
  const window = windowOf(m, end, n, COVERAGE.week, end)
  const base = baseline(m, window.from)
  if (!window.covered || window.mean === undefined || !base) return undefined
  return { recent: window.mean, window, base }
}

/** The newest value when it is from today or yesterday. */
export function lastNight(
  m: Daily,
  today: string
): { day: string; value: number } | undefined {
  const day = newestDay(m, today)
  if (!day || day < addDays(today, -1)) return undefined
  return { day, value: m.get(day) as number }
}

/** A date, `YYYY-MM-DD`, as the screens print it. */
const dayText = (day: string) => formatDate(`${day}T12:00:00Z`)

const r1 = (v: number) => (Math.round(v * 10) / 10).toString()
const r0 = (v: number) => Math.round(v).toLocaleString("en-GB")

/** A VO2 max older than this is stale: a watch estimates it every few
 * weeks of outdoor walks or runs. */
export const VO2_STALE_DAYS = 90

/** The row a finding attaches to. */
export function rowOf(f: Pick<Finding, "subject">): string {
  return f.subject.split(":")[0]
}

/** The route that opens a row's card on the Healthspan screen. */
export function rowHref(row: string, contributor: boolean): string {
  return `#/healthspan?focus=${contributor ? "contributor" : "signal"}-${row}`
}

/** The contributor findings. A VO2 max or body reading older than its
 * limit is "Stale" whether or not there is a score. On the latest
 * snapshot: the words of those whose 30-day average would add years, and
 * "No data" for those without data (a stale reading that no longer counts
 * says "Stale" instead). The target is named when `sex` is known. */
function contributorFindings(
  report: Report | undefined,
  sex: Sex | undefined,
  newestOf: Partial<Record<MetricId, { at: string; days: number }>>,
  today: Date
): Finding[] {
  const snap = report?.snapshots.at(-1)
  const out: Finding[] = []
  const stale = new Set<MetricId>()
  for (const [id, old] of Object.entries(newestOf) as [
    MetricId,
    { at: string; days: number },
  ][]) {
    if (!old || today.getTime() - Date.parse(old.at) <= old.days * DAY) continue
    const m = snap?.metrics.find((x) => x.id === id)
    const counts = !!m && m.sixMonth !== null
    stale.add(id)
    out.push({
      section: "healthspan",
      subject: counts ? `${id}:stale` : id,
      title: CONTRIBUTOR_LABELS[id],
      detail: `The newest reading is from ${formatDate(old.at)}, more than ${old.days} days ago${counts ? "" : "; it no longer counts"}`,
      tone: "info",
      word: "Stale",
      since: old.at,
      href: rowHref(id, true),
      rank: 3,
    })
  }
  if (!snap) return out
  for (const m of snap.metrics) {
    const id = m.id
    const title = CONTRIBUTOR_LABELS[id]
    const href = rowHref(id, true)
    if (m.sixMonth === null || m.impact === null) {
      if (!stale.has(id))
        out.push({
          section: "healthspan",
          subject: id,
          title,
          detail: `${noDataText(id)}; it counts as 0 years`,
          tone: "info",
          word: "No data",
          href,
          rank: 3,
        })
      continue
    }
    if (isSlipping(m))
      out.push({
        section: "healthspan",
        subject: `${id}:slipping`,
        title,
        detail: `30-day average ${valueText(m.thirtyDay, m.format)} against ${valueText(m.sixMonth, m.format)} over 6 months; if it holds it adds ${formatImpact((m.impact30 ?? 0) - m.impact).replace("+", "")} more years`,
        tone: "warning",
        word: "Slipping",
        since: snap.date,
        href,
        rank: 2,
      })
    if (m.impact30 === null || m.impact30 < ADDS_YEARS) continue
    const now = valueText(m.thirtyDay, m.format)
    const target =
      sex === undefined
        ? ""
        : ` against a target of ${valueText(targetFor(id, snap.age, sex), m.format)}`
    const more = m.impact30 - m.impact
    const holds =
      more >= ADDS_YEARS
        ? `if it holds it adds ${formatImpact(more).replace("+", "")} more years`
        : `at this level it adds ${formatImpact(m.impact30).replace("+", "")} years`
    out.push({
      section: "healthspan",
      subject: id,
      title,
      detail: `${now} over the last ${THIRTY} days${target}; ${holds}`,
      tone: "warning",
      word: CONTRIBUTOR_WORDS[id],
      since: snap.date,
      href,
      rank: 2,
    })
  }
  return out
}

/** What a Healthspan card is flagged for. One predicate
 * per status, read by the card's badges, the focus list and the filter:
 * `adds` a contributor whose 30-day average adds `ADDS_YEARS` or more,
 * `slipping` one whose last 30 days would add `SLIPPING` more than its 6
 * months, `flagged` a signal rule (recovery, HRV, breathing, blood oxygen,
 * skin temperature, an elevated resting heart rate), `nodata` "No data" or
 * "Stale", and `ok` none of these. */
export type HealthspanStatus = "adds" | "slipping" | "flagged" | "nodata" | "ok"

export const HEALTHSPAN_STATUSES: HealthspanStatus[] = [
  "adds",
  "slipping",
  "flagged",
  "nodata",
  "ok",
]

const CONTRIBUTOR_IDS = new Set<string>(Object.keys(CONTRIBUTOR_WORDS))

/** The status a finding gives its card. */
export function findingStatus(
  f: Pick<Finding, "subject" | "word" | "tone">
): HealthspanStatus {
  if (f.subject.endsWith(":slipping")) return "slipping"
  if (f.word === "No data" || f.word === "Stale") return "nodata"
  if (CONTRIBUTOR_IDS.has(f.subject) && f.tone === "warning") return "adds"
  return "flagged"
}

/** The statuses of a card from the findings on it; `ok` when none. Sync
 * findings are the page banner's, never a card's. */
export function cardStatuses(card: {
  findings: Pick<Finding, "subject" | "word" | "tone">[]
}): Set<HealthspanStatus> {
  const out = new Set<HealthspanStatus>()
  for (const f of card.findings)
    if (!f.subject.startsWith("sync")) out.add(findingStatus(f))
  if (!out.size) out.add("ok")
  return out
}

/** Every Healthspan finding but the sync ones. `report` is the age score's,
 * undefined without a birth date and sex; `sex` names the targets. */
export function evaluateHealthspan(
  data: HealthspanSeries,
  report: Report | undefined,
  todayDate: Date,
  sex?: Sex
): Finding[] {
  const today = isoDay(todayDate)
  const out: Finding[] = []
  const add = (f: Omit<Finding, "section" | "rank"> & { rank?: number }) =>
    out.push({
      section: "healthspan",
      rank: f.tone === "danger" ? 1 : f.tone === "warning" ? 2 : 3,
      ...f,
    })

  // Recovery: today's, or last night's when today has none yet.
  const recDay = newestDay(data.recovery, today)
  if (recDay && recDay >= addDays(today, -1)) {
    const v = data.recovery.get(recDay) as number
    if (v < RECOVERY_GREEN) {
      const w = recoveryWord(v)
      add({
        subject: "recovery",
        title: "Recovery",
        detail: `${r0(v)}% on ${dayText(recDay)}, ${w.word.toLowerCase()} in WHOOP's bands`,
        tone: v < RECOVERY_YELLOW ? "danger" : "warning",
        word: w.word,
        since: recDay,
        href: rowHref("recovery", false),
      })
    }
  }

  const rhr = recentVsBaseline(data.rhr, today)
  if (
    rhr &&
    rhr.recent > rhr.base.mean + rhr.base.sd &&
    rhr.recent - rhr.base.mean >= 2
  )
    add({
      subject: "rhr:elevated",
      title: "Resting heart rate",
      detail: `${r1(rhr.recent)} bpm a night ${coverageText(rhr.window, "nights")}, against a baseline of ${r1(rhr.base.mean)} bpm from ${rhr.base.n} nights before`,
      tone: "warning",
      word: "Elevated",
      since: rhr.window.to,
      href: rowHref("rhr", true),
    })
  const hrv = recentVsBaseline(data.hrv, today)
  if (
    hrv &&
    hrv.recent < hrv.base.mean - hrv.base.sd &&
    (hrv.base.mean - hrv.recent) / hrv.base.mean >= 0.1
  )
    add({
      subject: "hrv",
      title: "HRV",
      detail: `${r0(hrv.recent)} ms a night ${coverageText(hrv.window, "nights")}, against a baseline of ${r0(hrv.base.mean)} ms from ${hrv.base.n} nights before`,
      tone: "warning",
      word: "Depressed",
      since: hrv.window.to,
      href: rowHref("hrv", false),
    })
  const resp = lastNight(data.respiratoryRate, today)
  const respBase = resp && baseline(data.respiratoryRate, resp.day)
  if (resp && respBase && resp.value - respBase.mean >= 1.5)
    add({
      subject: "respiratoryRate",
      title: "Breathing rate",
      detail: `${r1(resp.value)} per minute on ${dayText(resp.day)}, ${r1(resp.value - respBase.mean)} above the baseline from ${respBase.n} nights before`,
      tone: "warning",
      word: "Up",
      since: resp.day,
      href: rowHref("respiratoryRate", false),
    })
  const spo2 = lastNight(data.spo2, today)
  if (spo2 && spo2.value < SPO2_LOW)
    add({
      subject: "spo2",
      title: "Blood oxygen",
      detail: `${r1(spo2.value)}% on ${dayText(spo2.day)}, below ${SPO2_LOW}%`,
      tone: "warning",
      word: "Low",
      since: spo2.day,
      href: rowHref("spo2", false),
    })
  const skin = lastNight(data.skinTemperature, today)
  const skinBase = skin && baseline(data.skinTemperature, skin.day)
  if (skin && skinBase && Math.abs(skin.value - skinBase.mean) > 0.5)
    add({
      subject: "skinTemperature",
      title: "Skin temperature",
      detail: `${r1(skin.value)} °C on ${dayText(skin.day)}, ${r1(Math.abs(skin.value - skinBase.mean))} °C ${skin.value > skinBase.mean ? "above" : "below"} the baseline from ${skinBase.n} nights before`,
      tone: "warning",
      word: "Off",
      since: skin.day,
      href: rowHref("skinTemperature", false),
    })

  const vo2 = newest(data.vo2)
  const lean = newest(data.lean)
  out.push(
    ...contributorFindings(
      report,
      sex,
      {
        vo2max: vo2 && { at: vo2.at, days: VO2_STALE_DAYS },
        leanMass: lean && { at: lean.at, days: BODY_CARRY_FORWARD_DAYS },
      },
      todayDate
    )
  )
  return out
}

/** The dates nights ended on: a counted sleep's or a resting heart rate
 * summary's, at its own offset. */
export function nightEnds(data: HealthspanSeries): Daily {
  const out: Daily = new Map(data.rhr)
  for (const sl of data.input.sleeps) {
    if (sl.asleepMs === undefined) continue
    const t = Date.parse(sl.endsAt) + (sl.offsetEnd ?? 0) * 60_000
    out.set(new Date(t).toISOString().slice(0, 10), 1)
  }
  return out
}

/** Sync gaps: nights or steps that arrived in the last six months and then
 * stopped, more than two days ago. A series no source ever fed is not a
 * gap. Shown as the screen's banner and as findings. */
export function syncFindings(
  data: HealthspanSeries,
  todayDate: Date
): Finding[] {
  const today = isoDay(todayDate)
  const out: Finding[] = []
  const check = (subject: string, title: string, m: Daily) => {
    const d = newestDay(m, today)
    if (d && d < addDays(today, -STALE_AFTER_DAYS))
      out.push({
        section: "healthspan",
        subject,
        title,
        detail: `Nothing since ${formatDate(`${d}T12:00:00Z`)}`,
        tone: "info",
        word: "Sync stopped",
        since: d,
        href: "#/healthspan",
        rank: 3,
      })
  }
  check("syncNights", "Nights", nightEnds(data))
  check("syncSteps", "Steps", data.steps)
  return out
}
