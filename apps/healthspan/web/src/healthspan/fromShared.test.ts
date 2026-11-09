// The engine on records in the shape the band and phone adapters write
// them (kinds/providers.substrate.reamde.dev/whoop/bundle.yaml's
// adaptwhoop, applehealth/bundle.yaml's workout and sleep outputs), with
// the contributor values docs/plans/healthspan.md chooses for each case.

import type { SubstrateRecord } from "@substrate/react"
import { describe, expect, it } from "vitest"
import { buildSeries, computeReport, dayIso } from "../engine/engine"
import {
  inputFromShared,
  keepBySource,
  leanReadings,
  placeEndDay,
  restingKept,
  type LabValue,
  type SharedRecords,
} from "./fromShared"

const SRC = "substrate.reamde.dev/observations/source/"
const MET = "substrate.reamde.dev/observations/metric/"
const BAND = "band/a1"
const PHONE = "phone/p1/watch"

let n = 0
const rec = (
  kind: string,
  properties: Record<string, unknown>
): SubstrateRecord => ({
  id: `r${++n}`,
  kind,
  properties,
  version: 1,
  createdAt: "2026-10-25T00:00:00Z",
  updatedAt: "2026-10-25T00:00:00Z",
})
const ref = (path: string) => ({ ref: path })

/** A band sleep: one offset at both ends; stage totals and scores only when
 * scored (the adapter's `if scored`). */
function bandSleep(
  at: string,
  endsAt: string,
  o: {
    scored?: boolean
    nap?: boolean
    offset?: number
    consistency?: number
  } = {}
) {
  const scored = o.scored ?? true
  return rec("substrate.reamde.dev/health/sleep", {
    source: ref(SRC + BAND),
    at,
    endsAt,
    isNap: o.nap ?? false,
    utcOffsetStart: o.offset ?? 0,
    utcOffsetEnd: o.offset ?? 0,
    ...(scored
      ? {
          light: "PT4H",
          deep: "PT1H30M",
          rem: "PT1H30M",
          awake: "PT20M",
          timeAsleep: "PT7H",
          scores: {
            sleepPerformance: "88",
            sleepConsistency: String(o.consistency ?? 70),
          },
        }
      : {}),
  })
}

/** A band recovery's resting heart rate: a vendor summary over the sleep it
 * scores (`period: sleep`), or over its cycle when the sleep is absent. */
function bandResting(at: string, endsAt: string, value: number, offset = 0) {
  return rec("substrate.reamde.dev/observations/observation", {
    metric: ref(MET + "health/resting_heart_rate"),
    source: ref(SRC + BAND),
    form: "summary",
    statistic: "vendor",
    period: "sleep",
    at,
    endsAt,
    utcOffset: offset,
    value: { value: String(value), unit: "1/min" },
  })
}

/** A band workout: strain, average heart rate and zones only when scored. */
function bandWorkout(
  at: string,
  endsAt: string,
  sportSource: string,
  sport: string,
  o: { scored?: boolean; offset?: number } = {}
) {
  return rec("substrate.reamde.dev/health/workout", {
    source: ref(SRC + BAND),
    sport,
    sportSource,
    at,
    endsAt,
    utcOffsetStart: o.offset ?? 0,
    utcOffsetEnd: o.offset ?? 0,
    ...((o.scored ?? true)
      ? {
          strain: "9.4",
          averageHeartRate: { value: "118", unit: "1/min" },
          zoneDurations: {
            zone0: "PT2M",
            zone1: "PT10M",
            zone2: "PT15M",
            zone3: "PT5M",
            zone4: "PT4M",
          },
        }
      : {}),
  })
}

/** A phone workout: the sport mapped, the average heart rate where the app
 * sent one, no zones and no strain. */
function phoneWorkout(
  at: string,
  endsAt: string,
  name: string,
  sport: string,
  hr?: number
) {
  return rec("substrate.reamde.dev/health/workout", {
    source: ref(SRC + PHONE),
    sport,
    sportSource: name,
    at,
    endsAt,
    utcOffsetStart: 60,
    utcOffsetEnd: 60,
    duration: "PT45M",
    ...(hr === undefined
      ? {}
      : { averageHeartRate: { value: String(hr), unit: "1/min" } }),
  })
}

const base: SharedRecords = {
  sleeps: [],
  workouts: [],
  rhr: [],
  steps: new Map(),
  vo2: [],
  lean: [],
  nightOrder: [BAND, PHONE],
  workoutOrder: [BAND, PHONE],
}
const series = (r: Partial<SharedRecords>) =>
  buildSeries(inputFromShared({ ...base, ...r }))
const days = (m: Map<number, number> | Set<number>) =>
  [...(m instanceof Map ? m.keys() : m)].map(dayIso).sort()

describe("a resting heart rate's night under a night order", () => {
  it("is its own source's sleep's night when the order prefers another source's", () => {
    const from = (r: SubstrateRecord, source: string) => ({
      ...r,
      properties: { ...r.properties, source: ref(SRC + source) },
    })
    const at = "2026-11-02T13:00:00Z"
    const end = "2026-11-02T14:00:00Z"
    const s = series({
      sleeps: [bandSleep(at, end), from(bandSleep(at, end), PHONE)],
      rhr: [from(bandResting(at, end, 52), PHONE)],
    })
    // The band's sleep is kept for the night's hours; the phone's, dropped
    // by the order, still dates the phone's resting heart rate.
    expect(days(s.sleepHours)).toEqual(["2026-11-01"])
    expect(days(s.rhr)).toEqual(["2026-11-01"])
    expect(days(s.recoveries)).toEqual(["2026-11-01"])
  })
})

describe("which sleeps count", () => {
  it("counts a scored sleep's light, deep and REM, naps added, consistency without naps", () => {
    const s = series({
      sleeps: [
        bandSleep("2026-10-24T23:00:00Z", "2026-10-25T07:00:00Z", {
          consistency: 81,
        }),
        bandSleep("2026-10-25T14:00:00Z", "2026-10-25T15:00:00Z", {
          nap: true,
          consistency: 20,
        }),
      ],
    })
    // The nap ended on the 25th too: the night of the 24th.
    expect(days(s.sleepHours)).toEqual(["2026-10-24"])
    expect([...s.sleepHours.values()][0]).toBeCloseTo(14)
    expect([...s.sleepConsistency.values()]).toEqual([81])
  })

  it("leaves an unscored sleep out of hours and days worn", () => {
    const s = series({
      sleeps: [
        bandSleep("2026-10-24T23:00:00Z", "2026-10-25T07:00:00Z", {
          scored: false,
        }),
      ],
    })
    expect(s.sleepHours.size).toBe(0)
    expect(s.worn.size).toBe(0)
  })
})

describe("which night", () => {
  it("moves a sleep ending after the next cycle starts, and the recovery spanning it, to the night it ended in", () => {
    // The cycle began on the evening of the 23rd; this sleep began the
    // evening after and ended on the 25th: the night of the 24th.
    const at = "2026-10-24T22:30:00Z"
    const endsAt = "2026-10-25T06:30:00Z"
    const s = series({
      sleeps: [bandSleep(at, endsAt)],
      rhr: [bandResting(at, endsAt, 52)],
    })
    expect(days(s.sleepHours)).toEqual(["2026-10-24"])
    expect([...s.rhr]).toEqual([[s.rhr.keys().next().value, 52]])
    expect(days(s.rhr)).toEqual(["2026-10-24"])
    expect(days(s.recoveries)).toEqual(["2026-10-24"])
    expect(days(s.worn)).toEqual(["2026-10-24"])
  })

  it("puts a recovery over an unscored sleep on that sleep's night, in the gate and worn", () => {
    const at = "2026-10-24T23:00:00Z"
    const endsAt = "2026-10-25T07:00:00Z"
    const s = series({
      sleeps: [bandSleep(at, endsAt, { scored: false })],
      rhr: [bandResting(at, endsAt, 58)],
    })
    expect(s.sleepHours.size).toBe(0)
    expect(days(s.rhr)).toEqual(["2026-10-24"])
    expect(days(s.recoveries)).toEqual(["2026-10-24"])
    expect(days(s.worn)).toEqual(["2026-10-24"])
  })

  it("places a recovery spanning a cycle by when the cycle starts", () => {
    const evening = bandResting(
      "2026-10-24T22:10:00Z",
      "2026-10-25T21:40:00Z",
      55
    )
    const afterMidnight = bandResting(
      "2026-10-26T00:40:00Z",
      "2026-10-26T23:00:00Z",
      56
    )
    const s = series({ rhr: [evening, afterMidnight] })
    // From the evening: its own day. From 00:40: the day before.
    expect(days(s.rhr)).toEqual(["2026-10-24", "2026-10-25"])
  })

  it("places each row at its own offset across a time zone change", () => {
    const s = series({
      sleeps: [
        bandSleep("2026-10-20T21:00:00Z", "2026-10-21T05:00:00Z", {
          offset: 120,
        }),
        // Ten hours east: 07:00 on the 25th there, still the 24th in UTC.
        bandSleep("2026-10-24T13:00:00Z", "2026-10-24T21:00:00Z", {
          offset: 600,
        }),
      ],
      workouts: [
        bandWorkout(
          "2026-10-24T23:30:00Z",
          "2026-10-25T00:10:00Z",
          "running",
          "running",
          {
            offset: 600,
          }
        ),
      ],
    })
    expect(days(s.sleepHours)).toEqual(["2026-10-20", "2026-10-24"])
    expect(days(s.zones13)).toEqual(["2026-10-25"])
  })
})

describe("which workouts count toward strength", () => {
  it("counts the band's uncommon strength sports and leaves the rest out", () => {
    const s = series({
      workouts: [
        bandWorkout(
          "2026-10-20T08:00:00Z",
          "2026-10-20T08:30:00Z",
          "barre3",
          "other"
        ),
        bandWorkout(
          "2026-10-21T08:00:00Z",
          "2026-10-21T08:20:00Z",
          "box-fitness",
          "boxing"
        ),
        bandWorkout(
          "2026-10-22T08:00:00Z",
          "2026-10-22T08:10:00Z",
          "babywearing",
          "other"
        ),
        bandWorkout(
          "2026-10-23T08:00:00Z",
          "2026-10-23T08:40:00Z",
          "meditation",
          "other"
        ),
        bandWorkout(
          "2026-10-24T08:00:00Z",
          "2026-10-24T09:00:00Z",
          "running",
          "running"
        ),
      ],
    })
    expect(days(s.strength)).toEqual(["2026-10-20", "2026-10-21", "2026-10-22"])
    expect([...s.strength.values()].reduce((a, b) => a + b, 0)).toBeCloseTo(60)
    // Every scored one has zones.
    expect(s.zones13.size).toBe(5)
  })

  it("leaves an unscored band workout out entirely", () => {
    const s = series({
      workouts: [
        bandWorkout(
          "2026-10-20T08:00:00Z",
          "2026-10-20T09:00:00Z",
          "weightlifting",
          "strength",
          {
            scored: false,
          }
        ),
      ],
    })
    expect(s.strength.size).toBe(0)
    expect(s.zones13.size).toBe(0)
  })

  it("counts the phone's traditional and functional strength training with a heart rate", () => {
    const s = series({
      workouts: [
        phoneWorkout(
          "2026-10-20T17:00:00Z",
          "2026-10-20T17:45:00Z",
          "Traditional Strength Training",
          "strength",
          112
        ),
        phoneWorkout(
          "2026-10-21T17:00:00Z",
          "2026-10-21T17:30:00Z",
          "Functional Strength Training",
          "strength",
          121
        ),
        // Entered by hand with nothing measured: not counted.
        phoneWorkout(
          "2026-10-22T17:00:00Z",
          "2026-10-22T18:00:00Z",
          "Traditional Strength Training",
          "strength"
        ),
      ],
    })
    expect(days(s.strength)).toEqual(["2026-10-20", "2026-10-21"])
    expect([...s.strength.values()]).toEqual([45, 30])
    expect(s.zones13.size).toBe(0)
  })
})

describe("the source orders", () => {
  const band = bandSleep("2026-10-24T23:00:00Z", "2026-10-25T07:00:00Z", {
    consistency: 90,
  })
  const phone = rec("substrate.reamde.dev/health/sleep", {
    source: ref(SRC + PHONE),
    at: "2026-10-24T22:50:00Z",
    endsAt: "2026-10-25T06:40:00Z",
    utcOffsetStart: 60,
    utcOffsetEnd: 60,
    isNap: false,
    light: "PT3H",
    deep: "PT1H",
    rem: "PT1H",
  })
  it("keeps the first source's night and drops the overlapping one whole", () => {
    const first = series({ sleeps: [band, phone] })
    expect([...first.sleepHours.values()]).toEqual([7])
    const second = series({ sleeps: [band, phone], nightOrder: [PHONE, BAND] })
    expect([...second.sleepHours.values()]).toEqual([5])
    expect(second.sleepConsistency.size).toBe(0)
  })

  it("keeps two records of one rank, and an instant held by a higher span drops", () => {
    const kept = keepBySource(
      [
        {
          at: "2026-10-01T00:00:00Z",
          endsAt: "2026-10-01T02:00:00Z",
          source: "a",
        },
        {
          at: "2026-10-01T01:00:00Z",
          endsAt: "2026-10-01T03:00:00Z",
          source: "b",
        },
        {
          at: "2026-10-01T02:00:00Z",
          endsAt: "2026-10-01T04:00:00Z",
          source: "c",
        },
        { at: "2026-10-01T01:00:00Z", source: "d" },
        { at: "2026-10-01T05:00:00Z", source: "d" },
      ],
      ["a", "b"]
    )
    // b overlaps a: dropped, so it drops nothing. c starts where a ends,
    // and c and d share a rank; the instant at 01:00 sits inside a's span.
    expect(kept.map((k) => `${k.source}${k.at.slice(11, 13)}`)).toEqual([
      "a00",
      "c02",
      "d05",
    ])
  })
})

describe("resting heart rate under the repository's ranking", () => {
  const night = bandResting("2026-10-24T23:00:00Z", "2026-10-25T07:00:00Z", 54)
  const day = rec("substrate.reamde.dev/observations/observation", {
    metric: ref(MET + "health/resting_heart_rate"),
    source: ref(SRC + PHONE),
    form: "summary",
    statistic: "vendor",
    period: "day",
    at: "2026-10-24T23:00:00Z",
    endsAt: "2026-10-25T23:00:00Z",
    utcOffset: 60,
    value: { value: "51", unit: "1/min" },
  })
  const row = (date: string, source: string) => ({
    start: `${date}T00:00:00+01:00`,
    end: `${date}T23:59:59+01:00`,
    group: { metric: MET + "health/resting_heart_rate" },
    sources: [SRC + source],
    values: [{ reducer: "last" as const, value: "1" }],
    count: 1,
  })

  it("dates a summary by its last instant at its own offset", () => {
    expect(
      placeEndDay({
        at: "2026-10-24T23:00:00Z",
        endsAt: "2026-10-25T23:00:00Z",
        offset: 60,
        value: 1,
      })
    ).toBe("2026-10-25")
  })

  it("keeps the source the day read names", () => {
    const parsed = inputFromShared({ ...base, rhr: [night, day] }).rhr
    expect(parsed).toHaveLength(2)
    const band = restingKept(parsed, { rows: [row("2026-10-25", BAND)] })
    expect(band.map((r) => r.value)).toEqual([54])
    const phone = inputFromShared({
      ...base,
      rhr: [night, day],
      rhrDays: { rows: [row("2026-10-25", PHONE)] },
    }).rhr
    expect(phone.map((r) => r.value)).toEqual([51])
  })

  it("passes the gate on the days a daily source reports", () => {
    const daily = Array.from({ length: 25 }, (_, i) => {
      const start = Date.parse("2026-10-01T23:00:00Z") + i * 86_400_000
      return rec("substrate.reamde.dev/observations/observation", {
        metric: ref(MET + "health/resting_heart_rate"),
        source: ref(SRC + PHONE),
        at: new Date(start).toISOString(),
        endsAt: new Date(start + 86_400_000).toISOString(),
        utcOffset: 60,
        value: { value: "50", unit: "1/min" },
      })
    })
    const report = computeReport(
      inputFromShared({ ...base, rhr: daily }),
      { birthDate: "1990-01-01", sex: "female" },
      new Date("2026-10-26T12:00:00Z")
    )
    expect(report.snapshots.length).toBeGreaterThan(0)
    expect(
      report.snapshots.at(-1)?.metrics.find((m) => m.id === "rhr")?.sixMonth
    ).toBe(50)
  })
})

describe("steps and VO2 max from day reads", () => {
  it("takes the day rows as they are", () => {
    const input = inputFromShared({
      ...base,
      steps: {
        rows: [
          {
            start: "2026-10-24T00:00:00+01:00",
            end: "2026-10-25T00:00:00+01:00",
            values: [{ reducer: "sum", value: "8123" }],
            count: 1,
          },
        ],
      },
      vo2: {
        rows: [
          {
            start: "2026-10-20T00:00:00+01:00",
            end: "2026-10-21T00:00:00+01:00",
            values: [{ reducer: "last", value: "41.2" }],
            sources: [SRC + PHONE],
            count: 1,
          },
        ],
      },
    })
    expect([...input.steps]).toEqual([["2026-10-24", 8123]])
    expect(input.vo2).toEqual([
      { at: "2026-10-20T12:00:00Z", value: 41.2, source: PHONE },
    ])
  })
})

describe("lean body mass from scans", () => {
  const result = (
    metric: string,
    source: string,
    at: string,
    value: number,
    unit: string,
    extra: Partial<LabValue> = {}
  ): LabValue => ({ metric, source, at, value, unit, ...extra })
  const scan = (
    source: string,
    at: string,
    mass: number,
    ffmKg: number,
    fat: number
  ): LabValue[] => [
    result("health/body_mass", source, at, mass, "kg"),
    result("health/fat_free_mass", source, at, ffmKg, "kg"),
    result("health/body_fat_fraction", source, at, fat, "percent"),
    // Lean soft tissue leaves bone out; never the model's lean mass.
    result("health/lean_mass", source, at, ffmKg - 2.6, "kg"),
  ]
  it("divides a scan's fat-free mass by the same scan's weight", () => {
    const [l] = leanReadings(
      scan("lab/x", "2026-10-27T09:00:00Z", 69.6, 58.977, 15.3)
    )
    expect(l.value).toBeCloseTo(84.74, 1)
    expect(l.basis).toBe("fatFreeMass")
  })
  it("converts grams and never pairs a scan with another day's or source's weight", () => {
    const rows = [
      result(
        "health/fat_free_mass",
        "lab/x",
        "2026-10-27T09:00:00Z",
        58977,
        "g"
      ),
      result("health/body_mass", "scale/y", "2026-10-27T08:00:00Z", 70, "kg"),
      result("health/body_mass", "lab/x", "2026-02-06T09:00:00Z", 71.6, "kg"),
    ]
    expect(leanReadings(rows)).toEqual([])
    const paired = leanReadings([
      ...rows,
      result("health/body_mass", "lab/x", "2026-10-27T09:00:00Z", 69.6, "kg"),
    ])
    expect(paired[0].value).toBeCloseTo(84.74, 1)
  })
  it("falls back to 100 less the scan's body fat, in percent or fraction", () => {
    const [l] = leanReadings([
      result(
        "health/body_fat_fraction",
        "scale/y",
        "2026-10-20T09:00:00Z",
        15.3,
        "percent"
      ),
    ])
    expect(l.value).toBeCloseTo(84.7)
    expect(l.basis).toBe("bodyFat")
    const [f] = leanReadings([
      result(
        "health/body_fat_fraction",
        "lab/x",
        "2026-10-20T09:00:00Z",
        0.2,
        "1"
      ),
    ])
    expect(f.value).toBeCloseTo(80)
  })
  it("reads an owner's metric of the same name, and leaves regional results out", () => {
    const roles = new Map([["mine/ffm", "fatFreeMass" as const]])
    const rows = [
      result("mine/ffm", "lab/x", "2026-10-27T09:00:00Z", 60, "kg"),
      result("health/body_mass", "lab/x", "2026-10-27T09:00:00Z", 75, "kg"),
      result(
        "health/body_fat_fraction",
        "lab/x",
        "2026-10-28T09:00:00Z",
        18,
        "percent",
        { channel: "arms" }
      ),
    ]
    expect(leanReadings(rows, roles).map((l) => l.value)).toEqual([80])
  })
  it("keeps the ranked source's scan on a day with two, one a day, oldest first", () => {
    const rows = [
      ...scan("scale/y", "2026-10-27T08:00:00Z", 70, 56, 20),
      ...scan("lab/x", "2026-10-27T11:00:00Z", 69.6, 58.977, 15.3),
      ...scan("lab/x", "2026-02-06T09:00:00Z", 71.6, 61.794, 13.7),
    ]
    const ls = leanReadings(rows, undefined, ["lab/x"])
    expect(ls.map((l) => [l.at.slice(0, 10), l.source])).toEqual([
      ["2026-02-06", "lab/x"],
      ["2026-10-27", "lab/x"],
    ])
  })
})
