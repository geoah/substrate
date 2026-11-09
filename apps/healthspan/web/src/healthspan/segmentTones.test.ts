import type { Metric } from "../engine/report"
import { describe, expect, it } from "vitest"
import { segmentColours } from "../engine/segments"
import {
  NEUTRAL_YEARS,
  PACE_SCALE,
  paceWord,
  recoveryWord,
  contributorSegments,
  markerTone,
  segmentTones,
} from "./segmentTones"

// The engine's ramps, faint to full, and its grey.
const BAD = ["#69512a", "#926928", "#be8126", "#ffa722"]
const GOOD = ["#0d5e47", "#118e67", "#0bb77d", "#00f29e"]

function asColour(s: { tone: string; level?: number }): string {
  if (s.tone === "neutral") return "#969696"
  return (s.tone === "warning" ? BAD : GOOD)[(s.level ?? 4) - 1]
}

describe("segmentTones", () => {
  it("matches the engine's segmentColours on the same years", () => {
    const cases = [
      [3.1, 2.4, 1.6, 0.9, 0.3, 0.02, -0.01, -0.3, -0.9, -1.6],
      [0.15, 0.12, 0.09, 0.06, 0.03, 0, -0.03, -0.06, -0.09, -0.1],
      [2, 1.5, 1, 0.5, 0, 0, 0, 0, 0, 0],
      [-2.5, -2, -1.5, -1, -0.5, 0, 0.5, 1, 2, 4],
      [0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5],
    ]
    for (const years of cases)
      expect(segmentTones(years).map(asColour)).toEqual(segmentColours(years))
  })
  it("is neutral under 0.025 years and never green on a bar that only adds", () => {
    expect(NEUTRAL_YEARS).toBe(0.025)
    expect(segmentTones([0.024, -0.024])).toEqual([
      { tone: "neutral" },
      { tone: "neutral" },
    ])
    const lean = segmentTones([3, 2, 1, 0, 0])
    expect(lean.some((s) => s.tone === "success")).toBe(false)
    expect(lean[0]).toEqual({ tone: "warning", level: 4 })
  })
})

describe("recovery words and the pace bar", () => {
  it("names recovery's bands at 34 and 67", () => {
    expect(recoveryWord(66).word).toBe("Yellow")
    expect(recoveryWord(67).word).toBe("Green")
    expect(recoveryWord(33).word).toBe("Red")
  })
  it("tones pace by its distance from 1.0x", () => {
    const s = PACE_SCALE.segments
    expect([PACE_SCALE.min, PACE_SCALE.max, s.length]).toEqual([-1, 3, 40])
    // Segment midpoints are -0.95, -0.85 ... 2.95; 0.95 and 1.05 are steady.
    expect(s[19]).toEqual({ tone: "neutral" })
    expect(s[20]).toEqual({ tone: "neutral" })
    expect(s[18]).toEqual({ tone: "success", level: 1 })
    expect(s[0]).toEqual({ tone: "success", level: 4 })
    expect(s[21]).toEqual({ tone: "warning", level: 1 })
    expect(s[39]).toEqual({ tone: "warning", level: 4 })
    expect([
      paceWord(-0.2),
      paceWord(0.85),
      paceWord(0.9),
      paceWord(1.09),
      paceWord(1.1),
    ]).toEqual(["Reversing", "Slowing", "Steady", "Steady", "Accelerated"])
    // The word's steady range is the bar's grey range.
    const grey = s
      .map((x, i) => (x.tone === "neutral" ? i : -1))
      .filter((i) => i >= 0)
    expect(grey.map((i) => -1 + i * 0.1)).toEqual([
      expect.closeTo(0.9),
      expect.closeTo(1.0),
    ])
  })
})

describe("segments under markers", () => {
  const metric = (over: Partial<Metric>): Metric => ({
    id: "steps",
    section: "strain",
    label: "Steps",
    format: "steps",
    sixMonth: null,
    thirtyDay: null,
    impact: null,
    impact30: null,
    opportunity: 0,
    // 0 to 16,000 steps in ten segments; years fall linearly, 0 at 8,000.
    scale: {
      min: 0,
      max: 16000,
      minLabel: "",
      maxLabel: "",
      segments: [2, 1.6, 1.2, 0.8, 0.4, 0.06, -0.2, -0.4, -0.6, -0.8],
    },
    curve: [
      [0, 2.5],
      [8000, 0],
      [16000, -1],
    ],
    verdict: { title: "", body: "" },
    source: "",
    history: [],
    ...over,
  })
  it("colour a marker's segment by the badge rule on its own value", () => {
    // 7,900 steps adds 0.03 years: no badge, so not orange, though the
    // segment's midpoint (7,200) is.
    const segs = contributorSegments(
      metric({ thirtyDay: 7900, sixMonth: 12000 }),
      8000
    )
    expect(segs[4]).toEqual({ tone: "neutral" })
    expect(segs[7].tone).toBe("success")
    // 6,000 steps adds 0.6 years: orange, at its ramp's level.
    expect(contributorSegments(metric({ thirtyDay: 6000 }), 8000)[3].tone).toBe(
      "warning"
    )
  })
  it("keeps the target's segment neutral when no marker sits in it", () => {
    expect(contributorSegments(metric({ thirtyDay: 2000 }), 8000)[5]).toEqual({
      tone: "neutral",
    })
  })
  it("lets the 30-day marker win a shared segment", () => {
    const segs = contributorSegments(
      metric({ thirtyDay: 7900, sixMonth: 7000 }),
      8000
    )
    expect(segs[4]).toEqual({ tone: "neutral" })
  })
  it("tones by the badge rule", () => {
    expect(markerTone(0.05, { tone: "warning", level: 3 })).toEqual({
      tone: "warning",
      level: 3,
    })
    expect(markerTone(0.049, { tone: "warning", level: 3 })).toEqual({
      tone: "neutral",
    })
    expect(markerTone(-0.03, { tone: "neutral" })).toEqual({
      tone: "success",
      level: 1,
    })
  })
})
