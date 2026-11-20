import { computeReport } from "../engine/engine"
import type { Metric, MetricId } from "../engine/report"
import { describe, expect, it } from "vitest"
import { demoSeries } from "../demo/healthspan"
import { isSlipping } from "../engine/focus"
import {
  type BodyInput,
  bodySuggestion,
  type ContributorInput,
  contributorSuggestion,
  contributorTrend,
  type SignalInput,
  SUGGESTION_MAX,
  signalSuggestion,
  type Suggestion,
} from "./suggest"

const today = new Date("2026-10-25T12:00:00Z")
const report = computeReport(
  demoSeries(today).input,
  { birthDate: "1990-01-01", sex: "male" },
  today
)
const metric = (id: MetricId, over: Partial<Metric> = {}): Metric => ({
  ...(report.snapshots.at(-1)?.metrics.find((m) => m.id === id) as Metric),
  ...over,
})

/** Words a wellness suggestion never uses. */
const BANNED = /diagnos|disease|\bmg\b|dose|treat/i

const every: Suggestion[] = []
const check = (s: Suggestion) => {
  every.push(s)
  return s
}

describe("contributor suggestions", () => {
  const ids: MetricId[] = [
    "sleepConsistency",
    "sleepHours",
    "zones13",
    "zones45",
    "strength",
    "steps",
    "vo2max",
    "rhr",
    "leanMass",
  ]
  const c = (
    i: Omit<ContributorInput, "label" | "hasTargets"> & {
      hasTargets?: boolean
      label?: string
    }
  ) => check(contributorSuggestion({ label: "Label", hasTargets: true, ...i }))
  it("picks a sentence for each band and trend, with a basis of numbers", () => {
    for (const id of ids) {
      // The whoop-only report has no lean mass scan; give it one.
      const raw = metric(id)
      const mid = (raw.scale.min + raw.scale.max) / 2
      const m = {
        ...raw,
        sixMonth: raw.sixMonth ?? mid,
        thirtyDay: raw.thirtyDay ?? mid,
      }
      const adds = c({ metric: { ...m, impact: 1, impact30: 1 } })
      const ok = c({ metric: { ...m, impact: 0, impact30: 0 } })
      expect(adds.text).not.toBe(ok.text)
      // Worse is the Slipping rule: 0.1 years or more added this month.
      const worse = c({ metric: { ...m, impact: 0, impact30: 0.04 } })
      expect(worse.basis).toMatch(/; steady\.$/)
      const slipping = c({ metric: { ...m, impact: -0.2, impact30: 0 } })
      expect(slipping.basis).toMatch(/; slipping\.$/)
      const better = c({ metric: { ...m, impact: 0, impact30: -0.2 } })
      expect(better.basis).toMatch(/; improving\.$/)
      expect(adds.basis).toMatch(
        /^30-day .*; 6-month .*; (improving|slipping|steady)\.$/
      )
    }
  })

  it("agrees with the Slipping badge: slipping exactly when isSlipping", () => {
    const m = metric("sleepConsistency")
    for (const [impact, impact30] of [
      [0, 0.09],
      [0, 0.1],
      [0.2, 0.25],
      [-0.3, -0.1],
    ]) {
      const x = { ...m, impact, impact30 }
      expect(contributorTrend(x) === "worse").toBe(isSlipping(x))
    }
    const s = c({ metric: { ...m, impact: -0.3, impact30: -0.1 } })
    expect(s.text).toBe(
      "Consistency slipped this month; hold your usual wake time."
    )
  })

  it("names a slipping month in words, with the target after it", () => {
    const m = metric("sleepHours")
    const s = c({
      metric: {
        ...m,
        impact: -0.2,
        impact30: 0,
        thirtyDay: 7.1,
        sixMonth: 7.5,
      },
      target: 7,
    })
    expect(s.text).toBe(
      "Nights are getting shorter this month; guard your bedtime."
    )
    expect(s.basis).toMatch(/, against a target of 7:00 h;/)
    expect(c({ metric: metric("steps"), target: 8000 }).basis).toMatch(
      /against a target of 8,000 steps/
    )
  })

  it("claims no target and no years without a birth date and sex", () => {
    for (const id of ids) {
      const m = metric(id)
      const s = c({
        metric: { ...m, impact30: 2 },
        hasTargets: false,
        label: "Steps a day",
      })
      if (id === "leanMass") continue
      expect(s.text).toMatch(
        /^Steps a day (rose|fell|held) this month; add your birth date and sex/
      )
      expect(`${s.text} ${s.basis}`).not.toMatch(
        /target of|years|for your age|slipp|improv/
      )
    }
  })

  it("says one VO2 max reading honestly, and the elevated and stale cases", () => {
    expect(c({ metric: metric("vo2max"), oneReading: true }).text).toBe(
      "One reading only; more readings from your source would show a trend."
    )
    const up = c({ metric: { ...metric("rhr"), impact30: 0 }, elevated: true })
    expect(up.text).toMatch(/up this week/)
    expect(up.basis).toMatch(/more than one deviation above/)
    expect(c({ metric: metric("leanMass"), stale: true }).text).toBe(
      "A new body composition scan would refresh this."
    )
  })
})

describe("signal suggestions", () => {
  const sig = (i: SignalInput) => check(signalSuggestion(i))
  it("follows recovery's colour and adds a falling trend when it fits", () => {
    expect(sig({ id: "recovery", latest: 20 }).text).toMatch(/^Red/)
    expect(sig({ id: "recovery", latest: 50 }).text).toMatch(/^Yellow/)
    expect(sig({ id: "recovery", latest: 80, m30: 50, m182: 60 }).text).toBe(
      "Green recovery; room for a high-strain day if you want it. Trending down this month."
    )
  })
  it("covers sleep performance's three bands", () => {
    expect(sig({ id: "sleepPerformance", latest: 60 }).text).toMatch(/^Poor/)
    expect(sig({ id: "sleepPerformance", latest: 75 }).text).toMatch(
      /^Sufficient/
    )
    expect(sig({ id: "sleepPerformance", latest: 90 }).text).toMatch(/^Optimal/)
    // WHOOP's score is not hours against need any more.
    for (const v of [60, 75, 90])
      expect(sig({ id: "sleepPerformance", latest: v }).text).not.toMatch(
        /need/
      )
  })
  it("matches strain to recovery and notes an All Out day", () => {
    expect(sig({ id: "strain", latest: 8 }).text).toMatch(/^Match strain/)
    expect(sig({ id: "strain", latest: 8, recovery: 20 }).text).toMatch(/Light/)
    expect(sig({ id: "strain", latest: 8, recovery: 50 }).text).toMatch(
      /Moderate/
    )
    expect(sig({ id: "strain", latest: 8, recovery: 80 }).text).toMatch(/High/)
    expect(
      sig({ id: "strain", latest: 8, recovery: 80, strainYesterday: 19 }).text
    ).toMatch(/^Yesterday was All Out; recovery is green/)
  })
  it("reads HRV against the baseline, one night and a week", () => {
    const b = { mean: 60, sd: 5 }
    expect(sig({ id: "hrv", latest: 60 }).text).toMatch(/needs 14 nights/)
    expect(sig({ id: "hrv", latest: 50, m7: 52, baseline: b }).text).toMatch(
      /this week/
    )
    expect(sig({ id: "hrv", latest: 50, m7: 60, baseline: b }).text).toMatch(
      /one night/
    )
    expect(sig({ id: "hrv", latest: 70, m7: 60, baseline: b }).text).toMatch(
      /good week/
    )
    expect(sig({ id: "hrv", latest: 60, m7: 60, baseline: b }).text).toMatch(
      /usual range/
    )
  })
  it("mentions a GP only when the card's finding has fired", () => {
    expect(
      sig({ id: "respiratoryRate", latest: 20.6, finding: true }).text
    ).toMatch(/GP/)
    expect(sig({ id: "spo2", latest: 94, finding: true }).text).toMatch(/GP/)
    // Past the range without a finding: one night, no GP.
    expect(sig({ id: "respiratoryRate", latest: 20.6 }).text).toBe(
      "Breathing rate was over 20 last night; one night is not a pattern."
    )
    expect(sig({ id: "respiratoryRate", latest: 11 }).text).not.toMatch(/GP/)
    expect(sig({ id: "respiratoryRate", latest: 14.6 }).text).not.toMatch(/GP/)
    expect(sig({ id: "spo2", latest: 94 }).text).not.toMatch(/GP/)
    expect(sig({ id: "spo2", latest: 97 }).text).not.toMatch(/GP/)
    for (const id of [
      "recovery",
      "sleepPerformance",
      "strain",
      "hrv",
      "skinTemperature",
    ] as const)
      expect(sig({ id, latest: 1, finding: true }).text).not.toMatch(/GP/)
  })
  it("says a stale reading is old, never a current verdict", () => {
    const s = sig({ id: "recovery", latest: 50, staleSince: "5 Oct 2026" })
    expect(s.text).toBe(
      "The newest reading is from 5 Oct 2026; a new night from your source brings it up to date."
    )
    expect(s.text).not.toMatch(/Yellow/)
  })
  it("reads skin temperature as a deviation", () => {
    const b = { mean: 33.6, sd: 0.2 }
    expect(
      sig({ id: "skinTemperature", latest: 34.3, baseline: b }).text
    ).toMatch(/off your baseline last night/)
    expect(
      sig({ id: "skinTemperature", latest: 33.8, baseline: b }).basis
    ).toBe("Last night +0.20 °C from the 30-night baseline of 33.60 °C.")
  })
  it("says so when there is nothing to judge", () => {
    expect(sig({ id: "spo2" }).text).toBe("No recent readings to judge.")
  })
})

describe("body suggestions", () => {
  const body = (i: BodyInput) => check(bodySuggestion(i))
  it("reads weight on the BMI scale and adds a change over 2 percent", () => {
    expect(body({ metric: "health/body_mass", latest: 78 }).text).toMatch(
      /height reading/
    )
    expect(
      body({ metric: "health/body_mass", latest: 55, heightM: 1.8 }).text
    ).toMatch(/under/)
    expect(
      body({
        metric: "health/body_mass",
        latest: 78,
        heightM: 1.8,
        previous: { value: 82, date: "27 Jun 2026" },
      }).text
    ).toBe(
      "Weight is in the healthy BMI range. It fell 4.9% since 27 Jun 2026."
    )
    expect(
      body({ metric: "health/body_mass", latest: 100, heightM: 1.8 }).text
    ).toMatch(/above/)
  })
  it("reads body fat on Gallagher's bands, or asks for the profile", () => {
    expect(body({ metric: "health/body_fat_fraction", latest: 20 }).text).toBe(
      "Add your birth date and sex to see your healthy range."
    )
    for (const [v, re] of [
      [6, /under/],
      [15, /in the healthy/],
      [22, /^Body fat is above/],
      [30, /well above/],
    ] as const)
      expect(
        body({
          metric: "health/body_fat_fraction",
          latest: v,
          sex: "male",
          age: 30,
        }).text
      ).toMatch(re)
  })
  it("reads visceral fat on the scale's rating", () => {
    expect(
      body({ metric: "health/visceral_fat_rating", latest: 7 }).text
    ).toMatch(/healthy/)
    expect(
      body({ metric: "health/visceral_fat_rating", latest: 14 }).text
    ).toMatch(/excess/)
    expect(body({ metric: "health/visceral_fat_rating" }).text).toMatch(
      /^No readings/
    )
  })
})

describe("every suggestion", () => {
  it("stays under the limit and uses wellness words only", () => {
    expect(every.length).toBeGreaterThan(60)
    for (const s of every) {
      expect(s.text.length, s.text).toBeLessThan(SUGGESTION_MAX)
      expect(s.text, s.text).not.toMatch(BANNED)
      expect(s.basis, s.basis).not.toMatch(BANNED)
      expect(`${s.text} ${s.basis}`).not.toMatch(/[–—·]/)
    }
  })
})
