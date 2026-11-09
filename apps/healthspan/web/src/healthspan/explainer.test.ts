import { describe, expect, it } from "vitest"
import { demoSeries } from "../demo/healthspan"
import * as C from "../engine/curves"
import * as E from "../engine/engine"
import { EMPTY_INPUT } from "../engine/input"
import {
  daysOfData,
  EXPLAINER_CONSTANTS,
  explainer,
  explainerFunction,
  explainerRows,
  explainerRules,
  explainerSources,
} from "./explainer"
import { CONTRIBUTOR_LABELS } from "./healthspanMetrics"
import { CONTRIBUTOR_INPUTS } from "./read"

const AGE = 40
const profile = { birthDate: "1986-01-01", sex: "female" as const }
const snap = { age: AGE, metrics: [] }

describe("explainer constants", () => {
  it("are the engine's own exports", () => {
    for (const [name, v] of Object.entries(EXPLAINER_CONSTANTS))
      expect(v, name).toBe(E[name as keyof typeof E])
  })
  it("print in the rules", () => {
    const text = explainerRules()
      .map((r) => `${r.key}: ${r.value}`)
      .join("\n")
    for (const n of [
      E.SIX_MONTHS,
      E.THIRTY,
      E.CALIBRATION_DAYS,
      E.LOCK_RECOVERIES,
      E.LOCK_WINDOW_NIGHTS,
      E.BODY_CARRY_FORWARD_DAYS,
      E.PACE_DAMPING,
      E.PROJECTION_YEARS,
      E.MIN_COVERED_DAYS,
    ])
      expect(text).toContain(String(n))
    expect(text).toContain("\u22121.0x to 3.0x")
  })
  it("say how far calibration has come while calibrating", () => {
    const cal = explainerRules({ calibrating: true }, 60).find(
      (r) => r.key === "Calibrating"
    )
    expect(cal?.value).toBe(
      `Fewer than ${E.CALIBRATION_DAYS} days of data; 60 of ${E.CALIBRATION_DAYS} days so far`
    )
  })
})

describe("explainer rows", () => {
  const rows = explainerRows(snap, profile)
  it("has one row per engine contributor, in its order", () => {
    expect(rows.map((r) => r.id)).toEqual(E.DEFS.map((d) => d.id))
    expect(rows.map((r) => r.label)).toEqual(
      E.DEFS.map((d) => CONTRIBUTOR_LABELS[d.id])
    )
  })
  it("states each target as the curve functions give it for the age and sex", () => {
    const by = Object.fromEntries(rows.map((r) => [r.id, r.target]))
    expect(by.sleepConsistency).toBe(`${C.SLEEP_CONSISTENCY_NEUTRAL}%`)
    expect(by.sleepHours).toBe("7:00 h")
    expect(by.zones13).toBe(
      `1:${String(Math.round(C.zones13Target(AGE) - 60)).padStart(2, "0")} h`
    )
    expect(by.strength).toBe(`0:${C.STRENGTH_NEUTRAL_MIN} h`)
    expect(by.steps).toBe(`${C.stepsTarget(AGE).toLocaleString("en-US")} steps`)
    expect(by.vo2max).toBe(`${C.vo2Target(AGE, "female").toFixed(1)} ml/kg/min`)
    expect(by.rhr).toBe(`${C.rhrTarget("female")} bpm`)
    expect(by.leanMass).toBe(`${C.leanTarget(AGE, "female")}%`)
    expect(rows.find((r) => r.id === "vo2max")?.targetNote).toBe(
      "for your age and sex"
    )
    expect(rows.find((r) => r.id === "sleepHours")?.targetNote).toBeUndefined()
  })
  it("gives the most each can add and remove; lean body mass removes nothing", () => {
    const lean = rows.find((r) => r.id === "leanMass")
    expect(lean?.removesAtMost).toBe("nothing")
    expect(lean?.addsAtMost).toMatch(/^\+\d+\.\d years$/)
    const strength = rows.find((r) => r.id === "strength")
    expect(strength?.addsAtMost).toBe("+1.2 years")
    expect(strength?.removesAtMost).toBe("\u22121.6 years")
  })
  it("leaves out targets that need a missing age or sex", () => {
    const noSex = explainerRows(undefined, { birthDate: "1986-01-01" })
    expect(noSex.find((r) => r.id === "vo2max")?.target).toBeUndefined()
    expect(noSex.find((r) => r.id === "zones13")?.target).toBeDefined()
    expect(noSex.find((r) => r.id === "sleepHours")?.target).toBe("7:00 h")
  })
  it("names where each contributor's data comes from", () => {
    for (const r of rows)
      expect(r.feedsFrom).toBe(CONTRIBUTOR_INPUTS[r.id].summary)
  })
})

describe("explainer text", () => {
  it("counts the contributors in words and names the window", () => {
    expect(explainerFunction()).toContain("the sum of nine impacts")
    expect(explainerFunction()).toContain(`${E.SIX_MONTHS}-day average`)
  })
  it("lists each group of sources with how it is ranked and what fed it", () => {
    const today = new Date("2026-10-25T12:00:00Z")
    const s = explainerSources(demoSeries(today))
    expect(s.map((x) => x.data)).toEqual([
      "Sleeps",
      "Workouts",
      "Steps, VO2 max and resting heart rate",
      "Body composition results",
    ])
    // The relayed nights are dropped whole, so only the band fed them.
    expect(s[0].order).toEqual(["Demo Band"])
    expect(s[1].order).toEqual(["Demo Watch", "Demo Band"])
    expect(s[2].feeds).toEqual(["Steps a day", "VO2 max", "Resting heart rate"])
    expect(s[3].order).toEqual(["Demo DXA", "Demo Scale"])
    expect(explainerSources().every((x) => x.order.length === 0)).toBe(true)
  })
  it("names only the shared catalogue's metrics", () => {
    for (const input of Object.values(CONTRIBUTOR_INPUTS))
      for (const m of input.metrics) expect(m, m).toMatch(/^health\//)
  })
  it("assembles the four parts", () => {
    const e = explainer(undefined, profile)
    expect(e.rows).toHaveLength(E.DEFS.length)
    expect(e.rules.length).toBeGreaterThan(5)
  })
  it("counts days of data before a snapshot", () => {
    const input = {
      ...EMPTY_INPUT,
      rhr: [
        {
          at: "2026-10-01T23:00:00Z",
          endsAt: "2026-10-02T07:00:00Z",
          offset: 0,
          value: 55,
        },
      ],
    }
    expect(daysOfData(input, "2026-10-25")).toBe(24)
  })
})
