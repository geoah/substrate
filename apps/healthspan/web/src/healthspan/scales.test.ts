import { targetFor } from "../engine/curves"
import { computeReport, yearsAt } from "../engine/engine"
import type { Metric, MetricId } from "../engine/report"
import { describe, expect, it } from "vitest"
import { demoSeries } from "../demo/healthspan"
import {
  bmiBand,
  bmiScale,
  bodyFatBand,
  bodyFatScale,
  breathingScale,
  contributorScale,
  hrvScale,
  recoveryScale,
  type Scale,
  signed,
  skinScale,
  sleepPerformanceScale,
  spo2Scale,
  strainBand,
  strainScale,
  visceralScale,
} from "./scales"

const tones = (s: Scale) => s.segments.map((x) => x.tone)
/** The segment holding `v`, as the ZoneBar picks it. */
const at = (s: Scale, v: number) => {
  const n = s.segments.length
  return s.segments[
    Math.max(
      0,
      Math.min(n - 1, Math.floor(((v - s.min) / (s.max - s.min)) * n))
    )
  ]
}

const today = new Date("2026-10-25T12:00:00Z")
const report = computeReport(
  demoSeries(today).input,
  { birthDate: "1990-03-14", sex: "male" },
  today
)
const metric = (id: MetricId) =>
  report.snapshots.at(-1)?.metrics.find((m) => m.id === id) as Metric

describe("contributor scales", () => {
  const ids: MetricId[] = [
    "sleepConsistency",
    "sleepHours",
    "zones13",
    "zones45",
    "strength",
    "steps",
    "vo2max",
    "rhr",
  ]
  it("puts the target on a segment edge, so no segment straddles it", () => {
    for (const id of ids) {
      const m = metric(id)
      const target = targetFor(id, 36, "male")
      const s = contributorScale(m, target)
      const w = (s.max - s.min) / s.segments.length
      const k = (target - s.min) / w
      expect(Math.abs(k - Math.round(k)), id).toBeLessThan(1e-6)
      expect(s.max, id).toBeGreaterThanOrEqual(m.scale.max)
      // Widened by under a segment for the target, plus whatever holds the
      // averages.
      const reach = Math.max(m.scale.max, m.sixMonth ?? 0, m.thirtyDay ?? 0)
      expect(s.max - reach, id).toBeLessThan(w)
      expect(s.max, id).toBeGreaterThanOrEqual(reach)
      expect(s.ticks[0].at).toBe(target)
    }
  })
  it("labels each segment with the years at both its ends, grey only where nothing moves", () => {
    for (const id of ids) {
      const m = metric(id)
      const target = targetFor(id, 36, "male")
      const s = contributorScale(m, target)
      const w = (s.max - s.min) / s.segments.length
      s.segments.forEach((seg, i) => {
        const lo = s.min + i * w
        const hi = lo + w
        const ys = [lo, (lo + hi) / 2, hi].map((v) => yearsAt(m.curve, v))
        if (seg.tone === "neutral") {
          expect(seg.label, id).toMatch(/, no effect$/)
          for (const y of ys) expect(Math.abs(y)).toBeLessThan(0.025)
        } else if (seg.tone === "warning") {
          expect(seg.label, id).toMatch(/, adds /)
          expect(hi <= target + 1e-9 || lo >= target - 1e-9, id).toBe(true)
        } else {
          expect(seg.label, id).toMatch(/, removes /)
        }
      })
    }
    // VO2 max at 41.4: the segment under the target only adds.
    const v = contributorScale(metric("vo2max"), 41.4)
    const below = v.segments.find((x) => /to 41\.4 ml/.test(x.label ?? ""))
    expect(below?.label).toMatch(/adds (up to )?\d\.\d( to \d\.\d)? years$/)
  })
  it("names ranges only, no years and no tones, without a target", () => {
    const s = contributorScale(metric("vo2max"), undefined)
    for (const seg of s.segments) {
      expect(seg.tone).toBe("neutral")
      expect(seg.label).not.toMatch(/years|adds|removes/)
      expect(seg.label).toMatch(/no target without your birth date and sex$/)
    }
    expect(s.ticks).toEqual([])
  })
})

describe("scales", () => {
  it("labels every segment of every scale with its range and meaning", () => {
    const all = [
      recoveryScale(),
      sleepPerformanceScale(),
      strainScale(),
      hrvScale(68, 6) as Scale,
      breathingScale(14.6),
      spo2Scale(),
      skinScale(33.6),
      bmiScale(1.78) as Scale,
      bodyFatScale("male", 42) as Scale,
      visceralScale(),
    ]
    for (const s of all) {
      for (const seg of s.segments) {
        expect(seg.label).toMatch(/\d.*, \S/)
        expect(seg.label).not.toMatch(/[–—·]/)
      }
    }
  })

  it("puts recovery's boundaries on 34 and 67", () => {
    const s = recoveryScale()
    expect(s.segments).toHaveLength(33)
    const t = tones(s)
    expect(t.slice(0, 11).every((x) => x === "danger")).toBe(true)
    expect(t.slice(11, 22).every((x) => x === "warning")).toBe(true)
    expect(t.slice(22).every((x) => x === "success")).toBe(true)
    expect(at(s, 46).label).toBe(
      "Recovery 46 to 48%, yellow: ready for moderate strain"
    )
  })

  it("cuts sleep performance at 70 and 85, the last segment ending at 100", () => {
    expect(sleepPerformanceScale().segments.at(-1)?.label).toMatch(/95 to 100%/)
    const t = tones(sleepPerformanceScale())
    expect(t).toHaveLength(20)
    expect(t.indexOf("warning")).toBe(14)
    expect(t.indexOf("success")).toBe(17)
  })

  it("gives strain four accent levels at WHOOP's 10, 14 and 18", () => {
    const s = strainScale()
    expect(new Set(tones(s))).toEqual(new Set(["accent"]))
    expect(s.segments.map((x) => x.level)).toEqual([
      ...Array(10).fill(1),
      ...Array(4).fill(2),
      ...Array(4).fill(3),
      ...Array(3).fill(4),
    ])
    expect(strainBand(9.9).word).toMatch(/^Light/)
    expect(strainBand(13.9).word).toMatch(/^Moderate/)
    expect(strainBand(18).word).toMatch(/^All out/)
    expect(strainBand(21).word).toMatch(/^All out/)
  })

  it("centres HRV on the baseline in half deviations, none without one", () => {
    expect(hrvScale(undefined, 5)).toBeUndefined()
    expect(hrvScale(60, 0)).toBeUndefined()
    const s = hrvScale(60, 5) as Scale
    expect([s.min, s.max, s.segments.length]).toEqual([45, 75, 12])
    expect(tones(s)).toEqual([
      ...Array(4).fill("warning"),
      ...Array(4).fill("neutral"),
      ...Array(4).fill("success"),
    ])
    expect(s.segments[0]).toMatchObject({
      level: 4,
      label: "45 to 48 ms, 2.5 to 3.0 SD below your baseline",
    })
    expect(s.segments[3].level).toBe(1)
    expect(s.ticks).toEqual([
      { at: 60, label: "baseline 60 ms", variant: "baseline" },
    ])
    // No negative zero in the labels next to the baseline.
    expect(s.segments[5].label).toBe(
      "58 to 60 ms, 0.0 to 0.5 SD below your baseline"
    )
    for (const seg of s.segments)
      expect(seg.label).not.toMatch(/-0\.0|\u22120\.0/)
  })

  it("narrows HRV's span, not its centre, when three deviations would pass 0 ms", () => {
    const s = hrvScale(30, 11) as Scale
    expect(s.min).toBeGreaterThanOrEqual(0)
    // The baseline tick sits in the middle of the bar.
    expect((30 - s.min) / (s.max - s.min)).toBeCloseTo(0.5)
    // 31 ms is above the baseline, so never in a "below" segment.
    expect(at(s, 31).label).toMatch(/above your baseline$/)
  })

  it("marks 12 to 20 breaths a minute as the adult range, with the baseline tick", () => {
    const s = breathingScale(14.6)
    expect(tones(s)).toEqual([
      ...Array(4).fill("warning"),
      ...Array(8).fill("success"),
      ...Array(4).fill("warning"),
    ])
    expect(at(s, 20.6).label).toBe(
      "over 20 to 21/min, above the adult resting range"
    )
    expect(s.ticks[0].label).toBe("baseline 14.6/min")
    expect(breathingScale().ticks).toEqual([])
  })

  it("cuts blood oxygen at 95", () => {
    expect(tones(spo2Scale())).toEqual([
      ...Array(5).fill("warning"),
      ...Array(5).fill("success"),
    ])
  })

  it("reads skin temperature as a deviation, usual within a quarter degree", () => {
    const s = skinScale(33.6)
    expect(tones(s)).toEqual([
      ...Array(5).fill("warning"),
      "neutral",
      "neutral",
      ...Array(5).fill("warning"),
    ])
    expect(s.segments.map((x) => x.level ?? 0)).toEqual([
      4, 4, 3, 2, 1, 0, 0, 1, 2, 3, 4, 4,
    ])
    expect(s.minLabel).toBe("−1.5 °C")
    expect(signed(0.2, 2, " °C")).toBe("+0.20 °C")
    expect(signed(-0.2, 2)).toBe("−0.20")
  })

  it("maps BMI to kilograms with the height, none without one", () => {
    expect(bmiScale(undefined)).toBeUndefined()
    const s = bmiScale(1.8) as Scale
    expect(s.min).toBeCloseTo(48.6)
    expect(s.max).toBeCloseTo(129.6)
    // Short tick labels fit the card; the kilograms are in each segment's tip.
    expect(s.ticks.map((t) => t.label)).toEqual([
      "BMI 18.5",
      "BMI 25",
      "BMI 30",
    ])
    expect(bmiBand(18.4).word).toBe("underweight")
    expect(bmiBand(24.9).word).toBe("healthy weight")
    expect(bmiBand(29.9).word).toBe("overweight")
    expect(bmiBand(36)).toMatchObject({ word: "obese", level: 4 })
    expect(at(s, 78.4).label).toMatch(/healthy weight$/)
    // Half-unit segments: 18.5, 25 and 30 are edges, so BMI 18.3 is under.
    expect(s.segments).toHaveLength(50)
    expect(at(s, 18.3 * 1.8 * 1.8).label).toMatch(
      /^BMI 18\.0 to 18\.5, .*underweight$/
    )
    expect(at(s, 18.6 * 1.8 * 1.8).label).toMatch(
      /^BMI 18\.5 to 19\.0, .*healthy weight$/
    )
  })

  it("bands body fat by Gallagher's table for the sex and age", () => {
    expect(bodyFatScale(undefined, 40)).toBeUndefined()
    expect(bodyFatScale("male", undefined)).toBeUndefined()
    expect(bodyFatBand(19.9, "male", 30).word).toBe("healthy")
    expect(bodyFatBand(20, "male", 30).word).toBe("overweight")
    expect(bodyFatBand(25, "male", 30).word).toBe("obese")
    expect(bodyFatBand(7.9, "male", 30).word).toBe("below the healthy range")
    expect(bodyFatBand(21.9, "male", 45).word).toBe("healthy")
    expect(bodyFatBand(28, "male", 45).word).toBe("obese")
    expect(bodyFatBand(24.9, "male", 65).word).toBe("healthy")
    expect(bodyFatBand(32.9, "female", 30).word).toBe("healthy")
    expect(bodyFatBand(39, "female", 30).word).toBe("obese")
    expect(bodyFatBand(33.9, "female", 50).word).toBe("healthy")
    expect(bodyFatBand(42, "female", 70).word).toBe("obese")
    // The table's ends cover younger and older ages.
    expect(bodyFatBand(19.9, "male", 18).word).toBe("healthy")
    expect(bodyFatBand(29, "male", 85).word).toBe("overweight")
    const s = bodyFatScale("female", 50) as Scale
    expect(s.ticks.map((t) => t.at)).toEqual([23, 34, 40])
  })

  it("keeps Tanita's healthy visceral range to 12", () => {
    expect(tones(visceralScale())).toEqual([
      ...Array(12).fill("success"),
      ...Array(7).fill("warning"),
    ])
  })
})
