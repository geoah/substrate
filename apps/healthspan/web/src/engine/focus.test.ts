import { describe, expect, it } from "vitest"
import {
  ADDS_YEARS,
  atStake,
  focusList,
  focusSentence,
  neutralBand,
  SLIPPING,
} from "./focus"
import type { Metric } from "./report"

function metric(over: Partial<Metric>): Metric {
  return {
    id: "strength",
    section: "strain",
    label: "Strength Activity Time (Weekly)",
    format: "hours",
    sixMonth: 1,
    thirtyDay: 1,
    impact: 0,
    impact30: 0,
    opportunity: 0,
    scale: { min: 0, max: 2, minLabel: "0h", maxLabel: "2h", segments: [] },
    verdict: { title: "", body: "" },
    curve: [],
    source: "",
    history: [],
    ...over,
  }
}

describe("thresholds", () => {
  it("add years from 0.05 and slip from 0.1", () => {
    expect(ADDS_YEARS).toBe(0.05)
    expect(SLIPPING).toBe(0.1)
  })
})

describe("atStake", () => {
  it("adds what the 6 months add and what the 30 days would add on top", () => {
    expect(atStake(metric({ impact: 0.3, impact30: 0.5 }))).toBeCloseTo(0.5)
    expect(atStake(metric({ impact: -0.4, impact30: -0.1 }))).toBeCloseTo(0.3)
    expect(atStake(metric({ impact: -0.4, impact30: -0.6 }))).toBe(0)
    expect(atStake(metric({ impact: 0.2, impact30: null }))).toBeCloseTo(0.2)
  })
})

describe("focusList", () => {
  const ms = [
    metric({ id: "steps", impact: 0.2, impact30: 0.2 }),
    metric({ id: "strength", impact: -0.2, impact30: 0.4 }),
    metric({ id: "rhr", impact: -0.5, impact30: -0.5 }),
    metric({ id: "sleepHours", impact: 0.04, impact30: 0.1 }),
    metric({ id: "vo2max", impact: null, impact30: null, sixMonth: null }),
    metric({ id: "zones13", impact: 0.1, impact30: 0.1 }),
    metric({ id: "zones45", impact: 0.06, impact30: 0.06 }),
  ]
  it("keeps what adds years or is slipping, most at stake first", () => {
    expect(focusList(ms).map((m) => m.id)).toEqual([
      "strength",
      "steps",
      "zones13",
      "zones45",
    ])
  })
  it("caps the list when asked", () => {
    expect(focusList(ms, { limit: 3 }).map((m) => m.id)).toEqual([
      "strength",
      "steps",
      "zones13",
    ])
  })
})

describe("focusSentence", () => {
  it("names the slip in years", () => {
    expect(
      focusSentence(
        metric({ thirtyDay: 1.1, sixMonth: 1.5, impact: 0, impact30: 0.5 })
      )
    ).toBe(
      "1:06 h this month, 1:30 h over 6 months. Slipping: if it holds, it adds 0.5 more years."
    )
  })
  it("says when a contributor adds years now", () => {
    expect(focusSentence(metric({ impact: 0.3, impact30: 0.3 }))).toBe(
      "1:00 h this month, 1:00 h over 6 months. Below target, adding years now."
    )
  })
  it("is the bare comparison otherwise", () => {
    expect(focusSentence(metric({}))).toBe(
      "1:00 h this month, 1:00 h over 6 months."
    )
  })
})

describe("neutralBand", () => {
  it("spans the segments that cost nothing", () => {
    const m = metric({
      scale: {
        min: 0,
        max: 10,
        minLabel: "",
        maxLabel: "",
        segments: [1, 0.5, 0.1, 0.04, 0, -0.2, -0.5, -0.5, -0.5, -0.5],
      },
    })
    expect(neutralBand(m)).toEqual([3, 10])
  })
  it("is undefined on a bar that always costs", () => {
    const m = metric({
      scale: { min: 0, max: 2, minLabel: "", maxLabel: "", segments: [1, 0.5] },
    })
    expect(neutralBand(m)).toBeUndefined()
  })
})
