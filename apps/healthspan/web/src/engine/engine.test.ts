import { describe, expect, it } from "vitest"
import * as C from "./curves"
import {
  BODY_CARRY_FORWARD_DAYS,
  buildSeries,
  CALIBRATION_DAYS,
  computeReport,
  countsWorkout,
  DEFS,
  isStrength,
  isStrengthWorkout,
  LOCK_RECOVERIES,
  LOCK_WINDOW_NIGHTS,
  MIN_COVERED_DAYS,
  offsetMinutes,
  PACE_MAX,
  PACE_MIN,
  paceOf,
  PROJECTION_YEARS,
  restingNight,
  SIX_MONTHS,
  THIRTY,
} from "./engine"
import {
  EMPTY_INPUT,
  type EngineInput,
  type RestingSummary,
  type SleepSession,
  type WorkoutSession,
} from "./input"

const EMPTY: EngineInput = EMPTY_INPUT
const ME = { birthDate: "1990-01-01", sex: "male" as const }
const H = 3_600_000

/** `n` nights ending the night before `last` (a Sunday), each a counted
 * sleep and a resting heart rate summary over it, as a band writes them. */
function nights(
  last: string,
  n: number,
  extra: Partial<SleepSession> = {}
): Pick<EngineInput, "sleeps" | "rhr"> {
  const sleeps: SleepSession[] = []
  const rhr: RestingSummary[] = []
  const end = Date.parse(`${last}T07:00:00Z`)
  for (let i = 0; i < n; i++) {
    const wake = end - i * 86_400_000
    const at = new Date(wake - 8 * H).toISOString()
    const endsAt = new Date(wake).toISOString()
    sleeps.push({
      at,
      endsAt,
      offsetEnd: 0,
      source: "band",
      nap: false,
      asleepMs: 7 * H,
      consistency: 70,
      ...extra,
    })
    rhr.push({ at, endsAt, offset: 0, source: "band", value: 60 })
  }
  return { sleeps, rhr }
}

const dayOf = (d: number) => new Date(d * 86_400_000).toISOString().slice(0, 10)

describe("buildSeries", () => {
  it("puts Saturday night's sleep, which ends on Sunday, on Saturday", () => {
    const s = buildSeries({ ...EMPTY, ...nights("2026-10-25", 1) })
    expect(dayOf([...s.sleepHours.keys()][0])).toBe("2026-10-24")
  })

  it("places a sleep by the offset it ended at", () => {
    // 23:30Z on the 24th is 01:30 on the 25th at +02:00: the night of the
    // 24th, not the 23rd.
    const s = buildSeries({
      ...EMPTY,
      sleeps: [
        {
          at: "2026-10-24T16:00:00Z",
          endsAt: "2026-10-24T23:30:00Z",
          offsetEnd: 120,
          nap: false,
          asleepMs: 7 * H,
        },
      ],
    })
    expect(dayOf([...s.sleepHours.keys()][0])).toBe("2026-10-24")
  })

  it("counts naps toward hours but not consistency", () => {
    const n = nights("2026-10-25", 1)
    const nap: SleepSession = {
      ...n.sleeps[0],
      nap: true,
      consistency: 25,
      asleepMs: H,
    }
    const s = buildSeries({ ...EMPTY, ...n, sleeps: [...n.sleeps, nap] })
    expect([...s.sleepHours.values()][0]).toBeCloseTo(8)
    expect([...s.sleepConsistency.values()][0]).toBe(70)
  })

  it("leaves out a sleep with no stage totals, and says when no source scores consistency", () => {
    const n = nights("2026-10-25", 1, { consistency: undefined })
    const unstaged: SleepSession = { ...n.sleeps[0], asleepMs: undefined }
    const s = buildSeries({ ...EMPTY, sleeps: [unstaged] })
    expect(s.sleepHours.size).toBe(0)
    expect(s.worn.size).toBe(0)
    const t = buildSeries({ ...EMPTY, sleeps: n.sleeps })
    expect(t.consistencyScored).toBe(false)
    expect(DEFS[0].source(t, ME)).toMatch(/^None of your sleep sources/)
  })

  it("skips a workout with no measure of effort", () => {
    const w: WorkoutSession = {
      at: "2026-10-20T10:00:00Z",
      endsAt: "2026-10-20T10:40:00Z",
      sport: "strength",
    }
    expect(countsWorkout(w)).toBe(false)
    expect(buildSeries({ ...EMPTY, workouts: [w] }).strength.size).toBe(0)
    expect(
      buildSeries({ ...EMPTY, workouts: [{ ...w, averageHeartRate: 110 }] })
        .strength.size
    ).toBe(1)
  })

  it("takes daily step totals as they are", () => {
    const s = buildSeries({
      ...EMPTY,
      steps: new Map([["2026-10-20", 7000]]),
    })
    expect([...s.steps.values()]).toEqual([7000])
    expect(dayOf([...s.steps.keys()][0])).toBe("2026-10-20")
  })

  it("reads offsets including Z", () => {
    expect(offsetMinutes("Z")).toBe(0)
    expect(offsetMinutes("-07:00")).toBe(-420)
    expect(offsetMinutes("+02:00")).toBe(120)
  })
})

describe("restingNight", () => {
  const sleep = (at: string, endsAt: string, source = "band") => ({
    at,
    endsAt,
    offsetEnd: 0,
    source,
    nap: false,
  })

  it("takes the night of the same source's sleep it overlaps most", () => {
    const sleeps = [
      sleep("2026-10-24T23:00:00Z", "2026-10-25T07:00:00Z"),
      sleep("2026-10-25T13:00:00Z", "2026-10-25T14:00:00Z"),
    ]
    const day = restingNight(
      {
        at: "2026-10-25T00:00:00Z",
        endsAt: "2026-10-26T00:00:00Z",
        value: 50,
        source: "band",
      },
      sleeps,
      0
    )
    expect(dayOf(day)).toBe("2026-10-24")
  })

  it("ignores another source's sleep", () => {
    const day = restingNight(
      {
        at: "2026-10-25T01:00:00Z",
        endsAt: "2026-10-25T23:00:00Z",
        value: 50,
        source: "watch",
      },
      [sleep("2026-10-25T13:00:00Z", "2026-10-26T07:00:00Z")],
      0
    )
    // No sleep of its own: it starts before noon, so the day before.
    expect(dayOf(day)).toBe("2026-10-24")
  })

  it("puts a span starting at noon or later on its own day", () => {
    const day = restingNight(
      {
        at: "2026-10-24T22:00:00Z",
        endsAt: "2026-10-25T22:00:00Z",
        offset: 120,
        value: 50,
      },
      [],
      0
    )
    // 22:00Z is midnight on the 25th at +02:00: before noon, the day before.
    expect(dayOf(day)).toBe("2026-10-24")
    expect(
      dayOf(
        restingNight(
          {
            at: "2026-10-24T21:00:00Z",
            endsAt: "2026-10-25T21:00:00Z",
            offset: 0,
            value: 50,
          },
          [],
          0
        )
      )
    ).toBe("2026-10-24")
  })
})

describe("isStrength", () => {
  it("matches the strength list in any spelling and leaves generic activity out", () => {
    expect(isStrength("weightlifting")).toBe(true)
    expect(isStrength("Functional Fitness")).toBe(true)
    expect(isStrength("F45 Training")).toBe(true)
    expect(isStrength("functional-fitness")).toBe(true)
    expect(isStrength("activity")).toBe(false)
    expect(isStrength("running")).toBe(false)
  })

  it("takes the shared strength codes from any source", () => {
    expect(isStrengthWorkout({ sport: "strength" })).toBe(true)
    expect(isStrengthWorkout({ sport: "yoga" })).toBe(true)
    expect(isStrengthWorkout({ sport: "pilates" })).toBe(true)
    expect(isStrengthWorkout({ sport: "hiit", sportSource: "hiit" })).toBe(true)
    expect(
      isStrengthWorkout({
        sport: "hiit",
        sportSource: "High Intensity Interval Training",
      })
    ).toBe(false)
    expect(isStrengthWorkout({ sport: "other", sportSource: "barre3" })).toBe(
      true
    )
  })
})

describe("computeReport", () => {
  const today = new Date("2026-10-25T12:00:00Z")

  it("needs 21 resting heart rate nights in the 31 before a Sunday", () => {
    expect(
      computeReport({ ...EMPTY, ...nights("2026-10-25", 20) }, ME, today)
        .snapshots
    ).toHaveLength(0)
    const r = computeReport(
      { ...EMPTY, ...nights("2026-10-25", 21) },
      ME,
      today
    )
    expect(r.snapshots).toHaveLength(1)
    expect(r.snapshots[0].weekStart).toBe("2026-10-25")
    expect(r.snapshots[0].weekEnd).toBe("2026-10-31")
    expect(r.snapshots[0].calibrating).toBe(true)
  })

  it("divides weekly minutes by the days worn, not by 26 weeks", () => {
    const n = nights("2026-10-25", 28)
    // One 20-minute walk at midday on each worn day.
    const workouts: WorkoutSession[] = n.sleeps.map((s) => ({
      at: new Date(Date.parse(s.endsAt) - 19 * H).toISOString(),
      endsAt: new Date(
        Date.parse(s.endsAt) - 19 * H + 20 * 60_000
      ).toISOString(),
      offsetStart: 0,
      sport: "walking",
      zones: { zone1: 20 * 60_000 },
    }))
    const snap = computeReport(
      { ...EMPTY, ...n, workouts },
      ME,
      today
    ).snapshots.at(-1)
    const z = snap?.metrics.find((m) => m.id === "zones13")
    expect(z?.sixMonth).toBeCloseTo((20 * 7) / 60, 1)
  })

  it("counts a missing contributor as zero and says how many are known", () => {
    const snap = computeReport(
      { ...EMPTY, ...nights("2026-10-25", 28) },
      ME,
      today
    ).snapshots.at(-1)
    expect(snap?.metrics.find((m) => m.id === "steps")?.impact).toBeNull()
    expect(snap?.contributors).toBeLessThan(9)
  })

  it("uses a manual VO2 max only from its date on", () => {
    const data = { ...EMPTY, ...nights("2026-10-25", 60) }
    const r = computeReport(
      data,
      { ...ME, vo2Max: { value: 50, asOf: "2026-10-20" } },
      today
    )
    const vo2 = r.snapshots.map(
      (s) => s.metrics.find((m) => m.id === "vo2max")?.sixMonth ?? null
    )
    expect(vo2.at(-1)).toBe(50)
    expect(vo2[0]).toBeNull()
  })

  it("prefers a hand-entered VO2 max over a reading, and reads the reading without one", () => {
    const data: EngineInput = {
      ...EMPTY,
      ...nights("2026-10-25", 60),
      vo2: [{ at: "2026-10-20T08:00:00Z", value: 37.4, source: "watch" }],
    }
    const vo2 = (
      st: typeof ME & { vo2Max?: { value: number; asOf: string } }
    ) =>
      computeReport(data, st, today)
        .snapshots.at(-1)
        ?.metrics.find((m) => m.id === "vo2max")?.sixMonth
    expect(vo2({ ...ME, vo2Max: { value: 44, asOf: "2026-05-01" } })).toBe(44)
    expect(vo2(ME)).toBeCloseTo(37.4)
  })

  it("names the age score, never a product, in its sentences", () => {
    const r = computeReport(
      { ...EMPTY, ...nights("2026-10-25", 60) },
      ME,
      today
    )
    const snap = r.snapshots.at(-1)
    expect(snap?.ageScore).toBeCloseTo((snap?.age ?? 0) + (snap?.delta ?? 0))
    const words = [
      snap?.coach.body,
      ...(snap?.metrics ?? []).flatMap((m) => [m.verdict.body, m.source]),
    ].join(" ")
    expect(words).not.toMatch(/WHOOP/)
  })
})

describe("paceOf", () => {
  it("is 1.0x when the 30 days match the 6 months, 3.0x at most", () => {
    expect(paceOf(-1, -1)).toBe(1)
    // 0.25 years better over 30 days: 1 - 0.3 × 0.25 / 0.5.
    expect(paceOf(-1, -1.25)).toBeCloseTo(0.85)
    expect(paceOf(0, 5)).toBe(3)
    expect(paceOf(0, -5)).toBe(-1)
  })
})

describe("named constants", () => {
  it("hold the values the model states", () => {
    expect([SIX_MONTHS, THIRTY, MIN_COVERED_DAYS]).toEqual([182, 30, 7])
    expect([CALIBRATION_DAYS, LOCK_RECOVERIES, LOCK_WINDOW_NIGHTS]).toEqual([
      90, 21, 31,
    ])
    expect([PROJECTION_YEARS, PACE_MIN, PACE_MAX]).toEqual([0.5, -1, 3])
    expect(BODY_CARRY_FORWARD_DAYS).toBe(365)
  })
  it("set carry forward on VO2 max and lean body mass only", () => {
    const carried = DEFS.filter((d) => d.carryForwardDays).map((d) => d.id)
    expect(carried).toEqual(["vo2max", "leanMass"])
    for (const d of DEFS.filter((x) => x.carryForwardDays))
      expect(d.carryForwardDays).toBe(BODY_CARRY_FORWARD_DAYS)
  })
})

describe("targetFor", () => {
  it("is the value each contributor's curve puts at zero years", () => {
    for (const [age, sex] of [
      [36, "male"],
      [65, "female"],
    ] as const) {
      for (const d of DEFS) {
        const t = C.targetFor(d.id, age, sex)
        expect(
          Math.abs(d.years(t, age, { birthDate: "1990-01-01", sex })),
          d.id
        ).toBeLessThan(1e-9)
      }
    }
  })
  it("names the curve functions' own targets in display units", () => {
    expect(C.targetFor("zones13", 40, "male")).toBeCloseTo(
      C.zones13Target(40) / 60
    )
    expect(C.targetFor("zones45", 40, "male")).toBeCloseTo(
      C.zones45Target(40) / 60
    )
    expect(C.targetFor("strength", 40, "male")).toBeCloseTo(
      C.STRENGTH_NEUTRAL_MIN / 60
    )
    expect(C.targetFor("steps", 61, "male")).toBe(C.stepsTarget(61))
    expect(C.targetFor("vo2max", 40, "female")).toBe(C.vo2Target(40, "female"))
    expect(C.targetFor("rhr", 40, "female")).toBe(C.rhrTarget("female"))
    expect(C.targetFor("leanMass", 40, "male")).toBe(C.leanTarget(40, "male"))
    expect(C.targetFor("sleepConsistency", 40, "male")).toBe(
      C.SLEEP_CONSISTENCY_NEUTRAL
    )
    expect(C.targetFor("sleepHours", 40, "male")).toBe(C.SLEEP_HOURS_NEUTRAL)
  })
})

describe("body readings carried forward", () => {
  const today = new Date("2026-10-25T12:00:00Z")
  const metric = (data: EngineInput, id: string) =>
    computeReport(data, ME, today)
      .snapshots.at(-1)
      ?.metrics.find((m) => m.id === id)

  it("carries a VO2 max reading 200 days old into both windows", () => {
    const m = metric(
      {
        ...EMPTY,
        ...nights("2026-10-25", 60),
        vo2: [{ at: "2026-04-08T08:00:00Z", value: 44 }],
      },
      "vo2max"
    )
    expect(m?.sixMonth).toBe(44)
    expect(m?.thirtyDay).toBe(44)
  })

  it("prefers readings inside the window over an older one", () => {
    const m = metric(
      {
        ...EMPTY,
        ...nights("2026-10-25", 60),
        vo2: [
          { at: "2026-04-08T08:00:00Z", value: 44 },
          { at: "2026-10-10T08:00:00Z", value: 40 },
        ],
      },
      "vo2max"
    )
    expect(m?.thirtyDay).toBe(40)
  })

  it("carries forward only through the def's own carryForwardDays", () => {
    const def = DEFS.find((d) => d.id === "vo2max")
    const s = buildSeries({
      ...EMPTY,
      vo2: [{ at: "2026-04-08T08:00:00Z", value: 44 }],
    })
    const to = Math.floor(Date.parse("2026-10-25T00:00:00Z") / 86_400_000)
    expect(def?.value(s, to - THIRTY, to, ME)).toBeNull()
    expect(def?.value(s, to - THIRTY, to, ME, def.carryForwardDays)).toBe(44)
  })

  it("drops a reading older than the carry-forward limit", () => {
    const m = metric(
      {
        ...EMPTY,
        ...nights("2026-10-25", 60),
        lean: [{ at: "2025-10-01T08:00:00Z", value: 82 }],
      },
      "leanMass"
    )
    expect(m?.sixMonth).toBeNull()
    expect(m?.impact).toBeNull()
  })

  it("reads lean body mass as given, in percent", () => {
    const data: EngineInput = {
      ...EMPTY,
      ...nights("2026-10-25", 60),
      lean: [{ at: "2026-10-01T08:00:00Z", value: 84.7 }],
    }
    expect([...buildSeries(data).lean.values()]).toEqual([84.7])
    expect(metric(data, "leanMass")?.thirtyDay).toBeCloseTo(84.7)
  })
})
