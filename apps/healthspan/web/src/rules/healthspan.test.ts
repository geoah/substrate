import { describe, expect, it } from "vitest"
import { targetFor } from "../engine/curves"
import { DEFS } from "../engine/engine"
import { EMPTY_INPUT } from "../engine/input"
import type { Metric, MetricId, Report } from "../engine/report"
import {
  baselineSeries,
  CONTRIBUTOR_LABELS,
  CONTRIBUTOR_WORDS,
  rollingMean,
  tickFormat,
} from "../healthspan/healthspanMetrics"
import type { Daily, HealthspanSeries } from "../healthspan/read"
import {
  addDays,
  baseline,
  cardStatuses,
  evaluateHealthspan,
  findingStatus,
  recentVsBaseline,
  rowHref,
  rowOf,
  stats,
  syncFindings,
  VO2_STALE_DAYS,
  windowOf,
} from "./healthspan"

const today = new Date("2026-10-25T12:00:00Z")

/** `n` days ending today, valued by `f(i)` with i = 0 for the oldest. */
function days(n: number, f: (i: number) => number, end = "2026-10-25"): Daily {
  const m: Daily = new Map()
  for (let i = 0; i < n; i++) m.set(addDays(end, i - n + 1), f(i))
  return m
}

function empty(): HealthspanSeries {
  const e = (): Daily => new Map()
  return {
    input: EMPTY_INPUT,
    steps: e(),
    recovery: e(),
    strain: e(),
    sleepPerformance: e(),
    rhr: e(),
    hrv: e(),
    respiratoryRate: e(),
    spo2: e(),
    skinTemperature: e(),
    hrvSdnn: e(),
    vo2: [],
    lean: [],
    body: { bodyMass: [], bodyFat: [], visceralFat: [] },
    sources: new Map(),
    stepSources: [],
    nightOrder: [],
    workoutOrder: [],
  }
}

/** A latest snapshot whose contributors all sit on target, with `over`
 * changing some. */
function report(
  over: Partial<Record<MetricId, Partial<Metric>>> = {},
  age = 36
): Report {
  const metrics: Metric[] = DEFS.map((d) => ({
    id: d.id,
    section: d.section,
    label: d.label,
    format: d.format,
    sixMonth: targetFor(d.id, age, "male"),
    thirtyDay: targetFor(d.id, age, "male"),
    impact: 0,
    impact30: 0,
    opportunity: 0,
    scale: { min: d.min, max: d.max, minLabel: "", maxLabel: "", segments: [] },
    verdict: { title: "", body: "" },
    curve: [],
    source: "",
    history: [],
    ...over[d.id],
  }))
  return {
    nextUpdateDays: 7,
    snapshots: [
      {
        date: "2026-10-25",
        weekStart: "2026-10-25",
        weekEnd: "2026-10-31",
        age,
        ageScore: age,
        delta: 0,
        pace: 1,
        calibrating: false,
        locked: false,
        contributors: metrics.length,
        coach: { title: "", body: "" },
        metrics,
      },
    ],
  }
}

const subjects = (d: HealthspanSeries, r?: Report) =>
  evaluateHealthspan(d, r, today, "male").map((f) => f.subject)

describe("baseline arithmetic", () => {
  it("is the mean and population deviation of the 30 days before", () => {
    const m = days(40, (i) => (i % 2 ? 60 : 50))
    const b = baseline(m, "2026-10-25")
    expect(b?.mean).toBeCloseTo(55)
    expect(b?.sd).toBeCloseTo(5)
    expect(b?.n).toBe(30)
    expect(stats([1, 2, 3])?.sd).toBeCloseTo(Math.sqrt(2 / 3))
  })
  it("needs 14 days", () => {
    expect(
      baseline(
        days(10, () => 1),
        "2026-10-25"
      )
    ).toBeUndefined()
  })
  it("needs 5 of the 7 recent nights, ending today", () => {
    const base = (i: number) => (i % 2 ? 56 : 54)
    const sparse = days(40, (i) => (i >= 33 ? 70 : base(i)))
    for (const d of ["2026-10-25", "2026-10-23", "2026-10-21"]) sparse.delete(d)
    expect(recentVsBaseline(sparse, "2026-10-25")).toBeUndefined()
    sparse.set("2026-10-25", 70)
    const r = recentVsBaseline(sparse, "2026-10-25")
    expect(r?.window.count).toBe(5)
    expect(r?.window.from).toBe("2026-10-19")
  })
  it("leaves the recent week out of its baseline", () => {
    const m = days(40, (i) => (i >= 33 ? 70 : 55))
    const r = recentVsBaseline(m, "2026-10-25")
    expect(r?.recent).toBe(70)
    expect(r?.base.mean).toBe(55)
  })
})

describe("contributor findings", () => {
  it("fire nothing when every contributor is on target", () => {
    expect(subjects(empty(), report())).toEqual([])
  })
  it("word a contributor whose 30-day average adds 0.05 years or more", () => {
    const r = report({
      sleepConsistency: { impact: 0, impact30: 0.05, thirtyDay: 69.6 },
      steps: { impact: 0.3, impact30: 0.2 },
      rhr: { impact: 0, impact30: 0.04 },
    })
    const fs = evaluateHealthspan(empty(), r, today, "male")
    expect(fs.map((f) => [f.subject, f.word, f.tone])).toEqual([
      ["sleepConsistency", "Inconsistent", "warning"],
      ["steps", "Few", "warning"],
    ])
    expect(fs[0].href).toBe("#/healthspan?focus=contributor-sleepConsistency")
  })
  it("has a word and a label for every contributor", () => {
    for (const d of DEFS) {
      expect(CONTRIBUTOR_WORDS[d.id]).toBeTruthy()
      expect(CONTRIBUTOR_LABELS[d.id]).toBeTruthy()
    }
    expect(Object.keys(CONTRIBUTOR_LABELS).sort()).toEqual(
      DEFS.map((d) => d.id).sort()
    )
  })
  it("names the 30-day value, the target and what holding it adds", () => {
    const r = report({
      sleepHours: { thirtyDay: 6.5, impact: 0.1, impact30: 0.6 },
    })
    const f = evaluateHealthspan(empty(), r, today, "male").find(
      (x) => x.subject === "sleepHours"
    )
    expect(f?.word).toBe("Short")
    expect(f?.detail).toBe(
      "6:30 h over the last 30 days against a target of 7:00 h; if it holds it adds 0.5 more years"
    )
    const noSex = evaluateHealthspan(empty(), r, today).find(
      (x) => x.subject === "sleepHours"
    )
    expect(noSex?.detail).toBe(
      "6:30 h over the last 30 days; if it holds it adds 0.5 more years"
    )
  })
  it("says what a contributor adds at its level when it is not slipping", () => {
    const r = report({ steps: { thirtyDay: 6000, impact: 0.5, impact30: 0.4 } })
    const f = evaluateHealthspan(empty(), r, today, "male").find(
      (x) => x.subject === "steps"
    )
    expect(f?.detail).toContain("at this level it adds 0.4 years")
  })
  it("marks a contributor without data", () => {
    const r = report({
      vo2max: { sixMonth: null, thirtyDay: null, impact: null, impact30: null },
    })
    const f = evaluateHealthspan(empty(), r, today, "male")
    expect(f.map((x) => [x.subject, x.word, x.tone])).toEqual([
      ["vo2max", "No data", "info"],
    ])
  })
  it("fires none without a score", () => {
    expect(subjects(empty(), undefined)).toEqual([])
  })
})

describe("stale body readings", () => {
  it("calls a VO2 max older than 90 days stale, beside its other finding", () => {
    expect(VO2_STALE_DAYS).toBe(90)
    const d = empty()
    d.vo2 = [{ at: addDays("2026-10-25", -120), value: 38 }]
    const r = report({ vo2max: { impact: 0.5, impact30: 0.5, thirtyDay: 38 } })
    const fs = evaluateHealthspan(d, r, today, "male")
    expect(fs.map((f) => [f.subject, f.word])).toEqual([
      ["vo2max:stale", "Stale"],
      ["vo2max", "Below target"],
    ])
    expect(fs.map(rowOf)).toEqual(["vo2max", "vo2max"])
  })
  it("says stale, not no data, for a scan too old to count", () => {
    const d = empty()
    d.lean = [
      {
        at: "2025-09-01T10:00:00Z",
        value: 82,
        basis: "fatFreeMass",
        source: "lab/scan",
      },
    ]
    const r = report({
      leanMass: {
        sixMonth: null,
        thirtyDay: null,
        impact: null,
        impact30: null,
      },
    })
    const fs = evaluateHealthspan(d, r, today, "male")
    expect(fs.map((f) => [f.subject, f.word])).toEqual([["leanMass", "Stale"]])
    expect(fs[0].detail).toContain("it no longer counts")
  })
  it("reports staleness without a score too", () => {
    const d = empty()
    d.vo2 = [{ at: "2026-01-01T09:00:00Z", value: 38 }]
    expect(subjects(d)).toEqual(["vo2max"])
  })
})

describe("signal rules", () => {
  it("flags recovery red and yellow by WHOOP's bands, on the recovery row", () => {
    const d = empty()
    d.recovery = days(1, () => 30)
    expect(evaluateHealthspan(d, undefined, today)[0]).toMatchObject({
      subject: "recovery",
      tone: "danger",
      word: "Red",
      href: rowHref("recovery", false),
    })
    d.recovery = days(1, () => 50)
    expect(evaluateHealthspan(d, undefined, today)[0]).toMatchObject({
      word: "Yellow",
    })
    d.recovery = days(1, () => 80)
    expect(subjects(d)).not.toContain("recovery")
  })
  it("puts an elevated resting heart rate on the contributor row", () => {
    const d = empty()
    d.rhr = days(40, (i) => (i >= 33 ? 60 : i % 2 ? 56 : 54))
    const f = evaluateHealthspan(d, undefined, today).find(
      (x) => x.word === "Elevated"
    )
    expect(f?.subject).toBe("rhr:elevated")
    expect(f && rowOf(f)).toBe("rhr")
    expect(f?.href).toBe("#/healthspan?focus=contributor-rhr")
    d.rhr = days(40, (i) => (i >= 33 ? 56.5 : i % 2 ? 56 : 54))
    expect(subjects(d)).not.toContain("rhr:elevated")
  })
  it("flags HRV a deviation and 10 percent below baseline", () => {
    const d = empty()
    d.hrv = days(40, (i) => (i >= 33 ? 50 : i % 2 ? 72 : 68))
    expect(subjects(d)).toContain("hrv")
    d.hrv = days(40, (i) => (i >= 33 ? 66 : i % 2 ? 72 : 68))
    expect(subjects(d)).not.toContain("hrv")
  })
  it("flags breathing, blood oxygen and skin temperature", () => {
    const d = empty()
    d.respiratoryRate = days(31, (i) => (i === 30 ? 16.6 : 15))
    d.spo2 = days(1, () => 94)
    d.skinTemperature = days(31, (i) => (i === 30 ? 34.2 : 33.6))
    expect(subjects(d)).toEqual(
      expect.arrayContaining(["respiratoryRate", "spo2", "skinTemperature"])
    )
  })
  it("judges last night's readings only when they are from last night", () => {
    const d = empty()
    d.respiratoryRate = days(31, (i) => (i === 30 ? 16.6 : 15), "2026-10-20")
    d.spo2 = days(1, () => 94, "2026-10-20")
    d.skinTemperature = days(31, (i) => (i === 30 ? 34.2 : 33.6), "2026-10-20")
    expect(subjects(d)).toEqual([])
    d.spo2 = days(1, () => 94, "2026-10-24")
    expect(subjects(d)).toContain("spo2")
  })
  it("lets a stopped feed raise only the sync findings", () => {
    const stopped = new Date("2026-10-30T12:00:00Z")
    const d = empty()
    const span = (f: (i: number) => number) => days(21, f, "2026-10-21")
    d.recovery = span(() => 30)
    d.rhr = span((i) => (i >= 14 ? 62 : i % 2 ? 56 : 54))
    d.hrv = span((i) => (i >= 14 ? 50 : i % 2 ? 72 : 68))
    d.steps = span(() => 3000)
    d.respiratoryRate = span((i) => (i === 20 ? 17 : 15))
    d.spo2 = span(() => 93)
    d.skinTemperature = span((i) => (i === 20 ? 34.5 : 33.6))
    expect(
      evaluateHealthspan(d, undefined, stopped).map((f) => f.subject)
    ).toEqual([])
    expect(syncFindings(d, stopped).map((f) => f.subject)).toEqual([
      "syncNights",
      "syncSteps",
    ])
  })
  it("counts a window recent when its newest value is at most two days old", () => {
    const m = days(25, () => 1, "2026-10-23")
    expect(windowOf(m, "2026-10-25", 30, 21, "2026-10-25").covered).toBe(true)
    expect(windowOf(m, "2026-10-26", 30, 21, "2026-10-26").covered).toBe(false)
    expect(syncFindings({ ...empty(), recovery: m, steps: m }, today)).toEqual(
      []
    )
  })
  it("reports a sync stopped after two days, and none for a series never fed", () => {
    const d = empty()
    expect(syncFindings(d, today)).toEqual([])
    d.rhr = days(1, () => 58, "2026-10-21")
    d.steps = days(1, () => 9000, "2026-10-24")
    expect(syncFindings(d, today).map((f) => f.subject)).toEqual(["syncNights"])
  })
})

// The reference forms are quadratic over 2,500 nights: seconds under a
// whole suite's load, which the default 5 s timeout does not allow.
describe(
  "daily chart baselines at production scale",
  { timeout: 30_000 },
  () => {
    // The old quadratic forms, kept here as the reference.
    function naiveMean(m: Daily): { at: string; value: number }[] {
      const out: { at: string; value: number }[] = []
      for (const day of [...m.keys()].sort()) {
        const t = Date.parse(`${day}T00:00:00Z`)
        const vs = [...m]
          .filter(([d]) => {
            const dt = Date.parse(`${d}T00:00:00Z`)
            return dt <= t && dt > t - 30 * 86_400_000
          })
          .map(([, v]) => v)
        if (vs.length >= 14)
          out.push({
            at: day,
            value: vs.reduce((a, b) => a + b, 0) / vs.length,
          })
      }
      return out
    }
    function naiveBaseline(m: Daily) {
      const out: { at: string; value: number; low: number; high: number }[] = []
      for (const day of [...m.keys()].sort()) {
        const t = Date.parse(`${day}T00:00:00Z`)
        const vs = [...m]
          .filter(([d]) => {
            const dt = Date.parse(`${d}T00:00:00Z`)
            return dt < t && dt >= t - 30 * 86_400_000
          })
          .map(([, v]) => v)
        if (vs.length < 14) continue
        const mean = vs.reduce((a, b) => a + b, 0) / vs.length
        const sd = Math.sqrt(
          vs.reduce((a, v) => a + (v - mean) ** 2, 0) / vs.length
        )
        out.push({ at: day, value: mean, low: mean - sd, high: mean + sd })
      }
      return out
    }
    // 2,500 nights with a few missing, ending today.
    const long = days(2500, (i) => 55 + ((i * 37) % 11))
    for (const k of ["2026-10-01", "2025-03-03", "2022-01-10"]) long.delete(k)

    it("gives the same 30-day means as the quadratic form, in one pass", () => {
      const fast = rollingMean(long, 30, "2026-04-27")
      const ref = naiveMean(long).filter((p) => p.at >= "2026-04-27")
      expect(fast.length).toBe(ref.length)
      fast.forEach((p, i) => {
        expect(p.at).toBe(ref[i].at)
        expect(p.value).toBeCloseTo(ref[i].value, 9)
      })
      const b = baselineSeries(long)
      const rb = naiveBaseline(long)
      expect(b.length).toBe(rb.length)
      expect(b.at(-1)?.low).toBeCloseTo(rb.at(-1)?.low ?? 0, 9)
    })
    it("takes milliseconds where the quadratic form took far longer", () => {
      const t0 = performance.now()
      naiveMean(long)
      const slow = performance.now() - t0
      const t1 = performance.now()
      rollingMean(long, 30, "2026-04-27")
      baselineSeries(long)
      const fast = performance.now() - t1
      // Recorded for the report; the bound is loose so slow CI passes.
      console.info(
        `2,500 nights: quadratic ${slow.toFixed(0)} ms, sliding ${fast.toFixed(1)} ms`
      )
      expect(fast).toBeLessThan(250)
      expect(fast).toBeLessThan(slow)
    })
  }
)

describe("steps axis ticks", () => {
  const tick = tickFormat("steps") as (v: number, step: number) => string
  it("print thousands only when the step is a thousand or more, to the step's decimals", () => {
    expect([2500, 5000, 7500, 10000].map((v) => tick(v, 2500))).toEqual([
      "2.5K",
      "5.0K",
      "7.5K",
      "10.0K",
    ])
    expect([5000, 10000].map((v) => tick(v, 5000))).toEqual(["5K", "10K"])
    expect([8000, 8250, 8500].map((v) => tick(v, 250))).toEqual([
      "8,000",
      "8,250",
      "8,500",
    ])
    expect([9900, 9950].map((v) => tick(v, 50))).toEqual(["9,900", "9,950"])
  })
})

describe("slipping and card statuses (085, F5)", () => {
  it("adds a Slipping finding when the 30 days would add 0.1 more years", () => {
    const r = report({
      sleepConsistency: {
        sixMonth: 74,
        thirtyDay: 72.5,
        impact: -0.53,
        impact30: -0.33,
      },
    })
    const fs = evaluateHealthspan(empty(), r, today, "male")
    expect(fs.map((f) => [f.subject, f.word, f.tone, f.rank])).toEqual([
      ["sleepConsistency:slipping", "Slipping", "warning", 2],
    ])
    expect(fs[0].detail).toBe(
      "30-day average 73% against 74% over 6 months; if it holds it adds 0.2 more years"
    )
    expect(rowOf(fs[0])).toBe("sleepConsistency")
    // Slipping without adding years now: slipping, not adds.
    expect([...cardStatuses({ findings: fs })]).toEqual(["slipping"])
  })
  it("gives a card adds and slipping together, and ok without findings", () => {
    const r = report({
      strength: { thirtyDay: 0.5, impact: -0.2, impact30: 0.4 },
    })
    const fs = evaluateHealthspan(empty(), r, today, "male")
    expect(new Set(fs.map(findingStatus))).toEqual(
      new Set(["adds", "slipping"])
    )
    expect([...cardStatuses({ findings: [] })]).toEqual(["ok"])
  })
  it("names each finding's status", () => {
    const f = (
      subject: string,
      word: string,
      tone: "warning" | "info" | "danger" = "warning"
    ) => ({
      subject,
      word,
      tone,
    })
    expect(findingStatus(f("steps", "Few"))).toBe("adds")
    expect(findingStatus(f("vo2max", "No data", "info"))).toBe("nodata")
    expect(findingStatus(f("vo2max:stale", "Stale", "info"))).toBe("nodata")
    expect(findingStatus(f("rhr:elevated", "Elevated"))).toBe("flagged")
    expect(findingStatus(f("recovery", "Red", "danger"))).toBe("flagged")
    expect(findingStatus(f("hrv", "Depressed"))).toBe("flagged")
    expect([
      ...cardStatuses({ findings: [f("syncNights", "Sync stopped", "info")] }),
    ]).toEqual(["ok"])
  })
})
