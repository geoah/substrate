// The synthetic member's fitness as the shared kinds hold it: a band's
// nights, recoveries and workouts, the same nights re-exported through the
// phone, a watch's strength sessions, daily resting heart rate, HRV and
// VO2 max, the phone's steps, body composition scans from an imaging
// centre and a scale, and the day reads the server would answer over them.
// The screens run this through the live transform (healthspan/read.ts).
// Nothing here is anyone's data.

import type { AggregateAnswer, AggregateRow } from "@substrate/client"
import type { SubstrateRecord } from "@substrate/react"
import { KIND } from "../kinds"
import {
  toHealthspan,
  type HealthspanRaw,
  type HealthspanSeries,
} from "../healthspan/read"
import { DEMO_SOURCES, demoSourceRecords, q, rec, ref, rng } from "./shared"

const H = 3_600_000
const MIN = 60_000
const DAY = 86_400_000
const MET = "substrate.reamde.dev/observations/metric/"

/** Demo-only states, from `?healthspan=`: `calibrating` (60 days of
 * data), `paused` (nothing worn in the last 20 nights), `locked` (15
 * nights in all), `onevo2` (one VO2 max reading). */
export type DemoState = "calibrating" | "paused" | "locked" | "onevo2"

export function demoState(v: string | null | undefined): DemoState | undefined {
  return v === "calibrating" ||
    v === "paused" ||
    v === "locked" ||
    v === "onevo2"
    ? v
    : undefined
}

/** `PT7H28M8S`. */
function duration(ms: number): string {
  const s = Math.round(ms / 1000)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return `PT${h ? `${h}H` : ""}${m ? `${m}M` : ""}${s % 60 || (!h && !m) ? `${s % 60}S` : ""}`
}

/** `+01:00` from minutes east of UTC. */
function offsetText(min: number): string {
  const a = Math.abs(min)
  return `${min < 0 ? "-" : "+"}${String(Math.floor(a / 60)).padStart(2, "0")}:${String(a % 60).padStart(2, "0")}`
}

const iso = (t: number) => new Date(t).toISOString()

/** A local date at `offset` of an instant. */
function localDate(t: number, offset: number): string {
  return iso(t + offset * MIN).slice(0, 10)
}

/** One row of a day read: the date at the member's offset, the reducer's
 * value, the source that fed it. */
function dayRow(
  date: string,
  offset: number,
  value: number,
  source: string,
  reducer: "last" | "sum" | "avg",
  metric?: string
): AggregateRow {
  const next = iso(Date.parse(`${date}T00:00:00Z`) + DAY).slice(0, 10)
  return {
    start: `${date}T00:00:00${offsetText(offset)}`,
    end: `${next}T00:00:00${offsetText(offset)}`,
    group: metric ? { metric: MET + metric } : undefined,
    values: [{ reducer, value: String(value) }],
    sources: [`substrate.reamde.dev/observations/source/${source}`],
    count: 1,
  }
}

const answer = (rows: AggregateRow[]): AggregateAnswer => ({
  rows,
  exact: true,
  head: { changelog: 0, generation: "" },
})

/** The member's records and day reads, `today` at local midnight. The last
 * four weeks drift from the six months before them (later bedtimes, fewer
 * runs, strength on Mondays only), the last week runs high on resting
 * heart rate and low on recovery, so the screen has findings and a
 * slipping contributor. */
export function demoHealthspanRaw(
  today: Date,
  state?: DemoState,
  seed = 7
): HealthspanRaw {
  const r = rng(seed)
  const gauss = () => (r() + r() + r() + r() - 2) / 0.8165
  // The member lives in the viewer's zone: each row at its own offset.
  const off = (t: number) => -new Date(t).getTimezoneOffset()
  const band = DEMO_SOURCES.band.id
  const watch = DEMO_SOURCES.watch.id
  const phone = DEMO_SOURCES.phone.id
  const relay = DEMO_SOURCES.bandViaPhone.id
  const sessions: SubstrateRecord[] = []
  const rhr: SubstrateRecord[] = []
  const signals: AggregateRow[] = []
  const sdnn: AggregateRow[] = []
  const steps: AggregateRow[] = []
  const days = state === "calibrating" ? 60 : state === "locked" ? 15 : 200
  // Local midnight of today, as an instant.
  const midnight = new Date(
    today.getFullYear(),
    today.getMonth(),
    today.getDate()
  ).getTime()
  const worn = (d: number) => !(state === "paused" && d <= 20)

  const sig = (date: string, metric: string, v: number) =>
    signals.push(
      dayRow(
        date,
        off(Date.parse(`${date}T12:00:00Z`)),
        v,
        band,
        "last",
        metric
      )
    )

  for (let d = days; d >= 1; d--) {
    const recent = d <= 28
    const dayStart = midnight - d * DAY
    const weekday = new Date(dayStart).getDay()
    const offset = off(dayStart + 12 * H)
    const date = localDate(dayStart, offset)
    if (worn(d)) {
      // A night from about 23:00, later and looser lately, to the morning
      // after this date.
      const bed =
        dayStart +
        23 * H +
        gauss() * (recent ? 0.6 : 0.45) * H +
        (recent ? 0.25 * H : 0)
      const asleepH = Math.max(5, 7.4 + gauss() * 0.5 - (recent ? 0.35 : 0))
      const awake = (0.35 + r() * 0.3) * H
      const rem = asleepH * (0.22 + r() * 0.04) * H
      const deep = asleepH * (0.19 + r() * 0.04) * H
      const light = asleepH * H - rem - deep
      const ends = bed + asleepH * H + awake
      const consistency = Math.round(
        Math.min(98, Math.max(30, (recent ? 71 : 75) + gauss() * 7))
      )
      const performance = Math.round(Math.min(100, 80 + gauss() * 8))
      const wakeDate = localDate(ends, offset)
      const night = {
        at: iso(bed),
        endsAt: iso(ends),
        isNap: false,
        utcOffsetStart: offset,
        utcOffsetEnd: offset,
        light: duration(light),
        deep: duration(deep),
        rem: duration(rem),
        awake: duration(awake),
        timeAsleep: duration(asleepH * H),
        timeInBed: duration(ends - bed),
      }
      sessions.push(
        rec(KIND.sleep, `${band}/${d}/sleep`, {
          ...night,
          name: `Night of ${date}`,
          source: ref(KIND.source, band),
          scores: {
            sleepPerformance: String(performance),
            sleepConsistency: String(consistency),
          },
          respiratoryRate: q(14.6 + gauss() * 0.3, "1/min", 1),
        })
      )
      // The band's nights again, re-exported through the phone a minute
      // early: one device twice, which the night order keeps once.
      if (d <= 120)
        sessions.push(
          rec(KIND.sleep, `${relay}/sleep/${d}`, {
            ...night,
            at: iso(bed - MIN),
            endsAt: iso(ends - MIN),
            name: `Night of ${date}`,
            source: ref(KIND.source, relay),
          })
        )
      // A nap most Sundays.
      if (weekday === 0 && r() < 0.6) {
        const at = dayStart + 15 * H
        sessions.push(
          rec(KIND.sleep, `${band}/${d}/nap`, {
            at: iso(at),
            endsAt: iso(at + 40 * MIN),
            isNap: true,
            utcOffsetStart: offset,
            utcOffsetEnd: offset,
            light: "PT28M",
            deep: "PT5M",
            rem: "PT3M",
            name: `Nap on ${date}`,
            source: ref(KIND.source, band),
          })
        )
      }
      // The recovery's resting heart rate over the night it scores; the
      // last six nights run four beats high.
      const beats = Math.round(
        58 + gauss() * 2.2 + (recent ? 1 : 0) + (d <= 6 ? 4 : 0)
      )
      rhr.push(
        rec(KIND.observation, `${band}/${d}/resting_heart_rate`, {
          metric: ref(KIND.metric, "health/resting_heart_rate"),
          source: ref(KIND.source, band),
          form: "summary",
          statistic: "vendor",
          period: "sleep",
          at: iso(bed),
          endsAt: iso(ends),
          utcOffset: offset,
          value: q(beats, "1/min", 0),
        })
      )
      sig(wakeDate, "health/resting_heart_rate", beats)
      sig(
        wakeDate,
        "whoop/recovery",
        Math.round(Math.min(99, Math.max(8, (d <= 7 ? 46 : 62) + gauss() * 14)))
      )
      sig(wakeDate, "whoop/sleep_performance", performance)
      sig(
        wakeDate,
        "health/hrv_rmssd",
        Math.round(68 + gauss() * 8 - (d <= 7 ? 9 : 0))
      )
      sig(
        wakeDate,
        "health/respiratory_rate",
        Math.round((d === 10 ? 20.6 : 14.6 + gauss() * 0.3) * 10) / 10
      )
      sig(wakeDate, "health/spo2", Math.round((96.5 + gauss() * 0.6) * 10) / 10)
      sig(
        wakeDate,
        "health/skin_temperature",
        Math.round((33.6 + gauss() * 0.2) * 100) / 100
      )
      // The day's strain lands on the day the cycle ended.
      const strainBands = [6.2, 11.8, 15.4, 18.6]
      sig(
        date,
        "whoop/strain",
        d <= 14 && d % 3 === 0
          ? strainBands[(d / 3) % 4]
          : Math.round((10 + gauss() * 3) * 10) / 10
      )

      // A walk most days, zone 1 and a little zone 2.
      if (r() < 0.8) {
        const len = (25 + r() * 35) * MIN
        const start = dayStart + 12 * H + r() * 5 * H
        sessions.push(
          bandWorkout(
            band,
            `${d}/walk`,
            start,
            len,
            "walking",
            "walking",
            offset,
            [0.55, 0.4, 0.05, 0, 0]
          )
        )
      }
      // Runs: three a week before, one lately.
      const runs = recent
        ? weekday === 6
        : weekday === 2 || weekday === 4 || weekday === 6
      if (runs) {
        const len = (35 + r() * 25) * MIN
        const hard = r() < (recent ? 0.6 : 0.35)
        sessions.push(
          bandWorkout(
            band,
            `${d}/run`,
            dayStart + 7 * H,
            len,
            "running",
            "running",
            offset,
            hard
              ? [0.04, 0.12, 0.24, 0.3, 0.24]
              : [0.04, 0.12, 0.44, 0.34, 0.06]
          )
        )
      }
      // Strength: twice a week before, Mondays only lately.
      if (weekday === 1 || (weekday === 4 && !recent)) {
        const len = (40 + r() * 20) * MIN
        sessions.push(
          bandWorkout(
            band,
            `${d}/strength`,
            dayStart + 18 * H,
            len,
            "weightlifting",
            "strength",
            offset,
            [0.3, 0.45, 0.2, 0.05, 0]
          )
        )
      }
    }
    // The watch's own strength training on some Fridays before the drift,
    // with an average heart rate and no zones.
    if (worn(d) && weekday === 5 && !recent && r() < 0.5) {
      const at = dayStart + 8 * H
      sessions.push(
        rec(KIND.workout, `${watch}/workout/${d}`, {
          source: ref(KIND.source, watch),
          sport: "strength",
          sportSource: "Functional Strength Training",
          at: iso(at),
          endsAt: iso(at + 30 * MIN),
          utcOffsetStart: offset,
          utcOffsetEnd: offset,
          duration: "PT30M",
          averageHeartRate: q(112 + gauss() * 6, "1/min", 0),
          name: `Strength on ${date}`,
        })
      )
    }
    // The watch's daily resting heart rate and HRV over the last ten
    // days it was worn: a day summary from local midnight.
    if (worn(d) && d <= 10) {
      const v = Math.round(60 + gauss() * 2)
      rhr.push(
        rec(KIND.observation, `${watch}/resting_heart_rate/${d}`, {
          metric: ref(KIND.metric, "health/resting_heart_rate"),
          source: ref(KIND.source, watch),
          form: "summary",
          statistic: "vendor",
          period: "day",
          at: iso(dayStart),
          endsAt: iso(dayStart + DAY),
          utcOffset: offset,
          value: q(v, "1/min", 0),
        })
      )
    }
    if (worn(d) && d <= 60)
      sdnn.push(
        dayRow(date, offset, Math.round(45 + gauss() * 6), watch, "avg")
      )
    steps.push(
      dayRow(
        date,
        offset,
        Math.round(Math.max(1500, 9200 + gauss() * 2400 - (d <= 7 ? 1500 : 0))),
        phone,
        "sum"
      )
    )
  }

  // A VO2 max estimate every 20 days over the last 200, drifting up; one,
  // five days back, in the `onevo2` state.
  const vo2: AggregateRow[] = []
  const vo2Row = (t: number, v: number) =>
    dayRow(localDate(t, off(t)), off(t), v, watch, "last")
  if (state === "onevo2") vo2.push(vo2Row(midnight - 5 * DAY + 12 * H, 41.2))
  else
    for (let k = 9; k >= 0; k--)
      vo2.push(
        vo2Row(
          midnight - (k * 20 + 3) * DAY + 12 * H,
          Math.round((40.2 + (9 - k) * 0.15 + gauss() * 0.3) * 10) / 10
        )
      )

  return {
    sources: demoSourceRecords(),
    sessions,
    rhr,
    signals: answer(signals),
    hrvSdnn: answer(sdnn),
    steps: answer(steps),
    vo2: answer(vo2),
    metrics: [],
    body: demoBodyResults(today),
    height: answer([
      {
        start: iso(midnight - 300 * DAY),
        end: iso(midnight - 300 * DAY),
        values: [{ reducer: "last", value: "1.80" }],
        sources: [`substrate.reamde.dev/observations/source/${phone}`],
        count: 1,
      },
    ]),
  }
}

/** A band workout: scored, with a strain, an average heart rate and its
 * zone times as shares of its length (zones 1 to 5). */
function bandWorkout(
  source: string,
  id: string,
  start: number,
  len: number,
  sportSource: string,
  sport: string,
  offset: number,
  zones: number[]
): SubstrateRecord {
  const z = zones.map((x) => x * len)
  return rec(KIND.workout, `${source}/${id}`, {
    source: ref(KIND.source, source),
    sport,
    sportSource,
    at: iso(start),
    endsAt: iso(start + len),
    utcOffsetStart: offset,
    utcOffsetEnd: offset,
    duration: duration(len),
    strain: (6 + (z[2] + z[3] + z[4]) / MIN / 8).toFixed(1),
    averageHeartRate: q(100 + (60 * (z[3] + z[4])) / len, "1/min", 0),
    zoneDurations: Object.fromEntries(
      z.map((ms, i) => [`zone${i + 1}`, duration(ms)])
    ),
    name: `${sportSource} on ${iso(start).slice(0, 10)}`,
  })
}

/** Body composition: an imaging centre's scans a year apart (the newer two
 * weeks back), and a scale's readings every five weeks with a visceral fat
 * rating. */
export function demoBodyResults(today: Date): SubstrateRecord[] {
  const out: SubstrateRecord[] = []
  const scan = (
    days: number,
    source: string,
    readings: [string, number, string][]
  ) => {
    const midnight = new Date(
      today.getFullYear(),
      today.getMonth(),
      today.getDate()
    ).getTime()
    const at = new Date(midnight - days * DAY + 10 * H).toISOString()
    const report = `${source}/${at.slice(0, 10)}`
    for (const [metric, value, unit] of readings)
      out.push(
        rec(KIND.labresult, `${report}/${metric.split("/")[1]}`, {
          at,
          metric: ref(KIND.metric, metric),
          source: ref(KIND.source, source),
          report: ref(KIND.labreport, report),
          value: q(value, unit, 2),
          status: "final",
        })
      )
  }
  const dxa = DEMO_SOURCES.dxa.id
  const scale = DEMO_SOURCES.scale.id
  scan(14 + 364, dxa, [
    ["health/body_mass", 79.6, "kg"],
    ["health/body_fat_fraction", 21.4, "percent"],
    ["health/lean_mass", 59900, "g"],
    ["health/fat_free_mass", 62600, "g"],
  ])
  scan(14, dxa, [
    ["health/body_mass", 78.4, "kg"],
    ["health/body_fat_fraction", 19.6, "percent"],
    ["health/lean_mass", 60300, "g"],
    ["health/fat_free_mass", 63000, "g"],
  ])
  for (let k = 1; k <= 5; k++)
    scan(k * 35 + 3, scale, [
      ["health/body_mass", 79.3 - k * 0.05 + (k % 2) * 0.3, "kg"],
      ["health/body_fat_fraction", 20.9 - (k % 3) * 0.2, "percent"],
      ["health/visceral_fat_rating", k === 1 ? 7 : 8, "score"],
    ])
  return out
}

/** The member's series as the screens read them, in the default source
 * orders. */
export function demoSeries(today: Date, state?: DemoState): HealthspanSeries {
  return toHealthspan(demoHealthspanRaw(today, state), {})
}
