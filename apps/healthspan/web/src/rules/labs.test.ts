import { describe, expect, it } from "vitest"
import { entryByCode, forSex, targetBand } from "../catalogue"
import { demoLabIndex } from "../demo/labs"
import type { LabIndex, LabResult, Series } from "../labs/types"
import {
  evaluateLabs,
  offTarget,
  outOfRange,
  overdue,
  rangeStatus,
  resultFlag,
  seriesFinding,
  seriesStatuses,
  trendFit,
  trendTone,
} from "./labs"

const base: LabResult = {
  id: "x",
  metric: "labs/ldl_cholesterol",
  at: "2026-01-01T00:00:00Z",
  ranges: [],
  interpretation: [],
}
const r = (o: Partial<LabResult>): LabResult => ({ ...base, ...o })
const entry = (code: string) => entryByCode(code)

const series = (code: string, results: LabResult[]): Series => ({
  key: code,
  code,
  results,
  specimens: [],
  apart: [],
})

const emptyIndex: Pick<LabIndex, "subjects" | "sources"> = {
  subjects: new Map(),
  sources: new Map(),
}

describe("rule 1: out of lab range", () => {
  it("follows the lab's flag", () => {
    expect(
      outOfRange(r({ value: 3.4, interpretation: [{ code: "H" }] }))?.word
    ).toBe("High")
    expect(
      outOfRange(r({ value: 1, interpretation: [{ code: "LL" }] }))?.word
    ).toBe("Low")
    expect(
      outOfRange(r({ value: 1, interpretation: [{ code: "A" }] }))?.word
    ).toBe("Abnormal")
  })
  it("trusts a normal flag over the bands", () => {
    expect(
      outOfRange(
        r({
          value: 9,
          interpretation: [{ code: "N" }],
          ranges: [{ low: 1, high: 3 }],
        })
      )
    ).toBeUndefined()
  })
  it("checks an unflagged value against the untoned band", () => {
    const res = outOfRange(r({ value: 4, ranges: [{ low: 1, high: 3 }] }))
    expect(res?.word).toBe("High")
    expect(res?.distance).toBeCloseTo(0.5)
    expect(
      outOfRange(r({ value: 0.5, ranges: [{ low: 1, high: 3 }] }))?.word
    ).toBe("Low")
    expect(
      outOfRange(r({ value: 2, ranges: [{ low: 1, high: 3 }] }))
    ).toBeUndefined()
  })
  it("is in range when inside any one untoned band", () => {
    expect(
      outOfRange(
        r({
          value: 3.5,
          ranges: [
            { low: 1, high: 3 },
            { low: 1, high: 4 },
          ],
        })
      )
    ).toBeUndefined()
  })
  it("reads a poor cut-off on each side", () => {
    expect(
      outOfRange(r({ value: 5, ranges: [{ low: 4, tone: "poor" }] }))?.word
    ).toBe("High")
    expect(
      outOfRange(r({ value: 3.9, ranges: [{ low: 4, tone: "poor" }] }))
    ).toBeUndefined()
    expect(
      outOfRange(r({ value: 1, ranges: [{ high: 2, tone: "poor" }] }))?.word
    ).toBe("Low")
    expect(
      outOfRange(r({ value: 2.1, ranges: [{ high: 2, tone: "poor" }] }))
    ).toBeUndefined()
  })
  it("counts both bounds of the lab's range as inside", () => {
    const range = [{ low: 1, high: 3 }]
    expect(outOfRange(r({ value: 1, ranges: range }))).toBeUndefined()
    expect(outOfRange(r({ value: 3, ranges: range }))).toBeUndefined()
    expect(outOfRange(r({ value: 0.99, ranges: range }))?.word).toBe("Low")
    expect(outOfRange(r({ value: 3.01, ranges: range }))?.word).toBe("High")
  })
  it("reads an explicit good band as the normal range, and a fair one as not", () => {
    expect(
      outOfRange(r({ value: 4, ranges: [{ low: 1, high: 3, tone: "good" }] }))
        ?.word
    ).toBe("High")
    expect(
      outOfRange(r({ value: 4, ranges: [{ low: 1, high: 3, tone: "fair" }] }))
    ).toBeUndefined()
  })
})

describe("rule 1: a comparator result is the interval it denotes", () => {
  it("fires when every value it can be is outside the range", () => {
    expect(
      outOfRange(
        r({ value: 5, comparator: ">", ranges: [{ low: 0, high: 5 }] })
      )?.word
    ).toBe("High")
    expect(
      outOfRange(
        r({ value: 1, comparator: "<", ranges: [{ low: 1, high: 4 }] })
      )?.word
    ).toBe("Low")
    expect(
      outOfRange(r({ value: 6, comparator: ">", ranges: [{ high: 5 }] }))?.word
    ).toBe("High")
  })
  it("is in range when every value it can be is inside", () => {
    // A concentration cannot be negative: `<0.5 mg/L` is 0 up to 0.5.
    expect(
      rangeStatus(
        r({
          value: 0.5,
          comparator: "<",
          unit: "mg/L",
          ranges: [{ low: 0, high: 4.9 }],
        })
      )?.status
    ).toBe("in")
    expect(
      rangeStatus(r({ value: 20, comparator: "<", ranges: [{ high: 20 }] }))
        ?.status
    ).toBe("in")
    expect(
      rangeStatus(r({ value: 90, comparator: ">", ranges: [{ low: 90 }] }))
        ?.status
    ).toBe("in")
  })
  it("is uncertain when it straddles a bound", () => {
    expect(
      rangeStatus(
        r({ value: 5, comparator: "<", ranges: [{ low: 2, high: 10 }] })
      )?.status
    ).toBe("uncertain")
    expect(
      rangeStatus(
        r({ value: 5, comparator: ">=", ranges: [{ low: 0, high: 5 }] })
      )?.status
    ).toBe("uncertain")
    expect(
      rangeStatus(
        r({ value: 1, comparator: "<=", ranges: [{ low: 1, high: 4 }] })
      )?.status
    ).toBe("uncertain")
    expect(
      outOfRange(
        r({ value: 5, comparator: "<", ranges: [{ low: 2, high: 10 }] })
      )
    ).toBeUndefined()
    expect(
      resultFlag(
        r({ value: 5, comparator: "<", ranges: [{ low: 2, high: 10 }] }),
        undefined
      )
    ).toEqual({ word: "Uncertain", tone: "neutral" })
  })
  it("lets a signed score's bound run to minus infinity", () => {
    const tscore = (value: number, comparator: string) =>
      rangeStatus(
        r({ value, comparator, unit: "score", ranges: [{ low: -2, high: 2 }] })
      )
    // `<= -1` could be -1 (inside) or -3 (below): uncertain, never normal.
    expect(tscore(-1, "<=")?.status).toBe("uncertain")
    expect(tscore(-1, "<")?.status).toBe("uncertain")
    // Every value below -2 is below the range; -2 itself is inside.
    expect(
      outOfRange(
        r({
          value: -2,
          comparator: "<",
          unit: "score",
          ranges: [{ low: -2, high: 2 }],
        })
      )?.word
    ).toBe("Low")
    expect(tscore(-2, "<=")?.status).toBe("uncertain")
    expect(tscore(-2.01, "<=")?.status).toBe("out")
    // Above 2 is above the range; at or above 2 straddles its top.
    expect(tscore(2, ">")?.status).toBe("out")
    expect(tscore(2, ">=")?.status).toBe("uncertain")
    expect(tscore(1.99, ">")?.status).toBe("uncertain")
  })
  it("treats a result with no unit, or a unitless ratio, as signed", () => {
    expect(
      rangeStatus(
        r({ value: 0.5, comparator: "<", ranges: [{ low: 0, high: 4.9 }] })
      )?.status
    ).toBe("uncertain")
    expect(
      rangeStatus(
        r({
          value: 0.5,
          comparator: "<",
          unit: "1",
          ranges: [{ low: 0, high: 4.9 }],
        })
      )?.status
    ).toBe("uncertain")
    // A negative bound in a non-negative unit still reads down to minus infinity.
    expect(
      rangeStatus(
        r({
          value: -1,
          comparator: "<=",
          unit: "mg/L",
          ranges: [{ low: -2, high: 2 }],
        })
      )?.status
    ).toBe("uncertain")
  })
  it("reads a poor cut-off the same way", () => {
    expect(
      outOfRange(
        r({ value: 4, comparator: ">", ranges: [{ low: 4, tone: "poor" }] })
      )?.word
    ).toBe("High")
    expect(
      rangeStatus(
        r({ value: 5, comparator: "<", ranges: [{ low: 4, tone: "poor" }] })
      )?.status
    ).toBe("uncertain")
    expect(
      rangeStatus(
        r({ value: 4, comparator: "<", ranges: [{ low: 4, tone: "poor" }] })
      )
    ).toBeUndefined()
  })
  it("still follows the lab's flag first", () => {
    expect(
      outOfRange(
        r({
          value: 10,
          comparator: "<",
          ranges: [{ low: 10, high: 71 }],
          interpretation: [{ code: "L" }],
        })
      )?.word
    ).toBe("Low")
  })
  it("calls a coded result out of range only by a non-N code", () => {
    expect(outOfRange(r({ coded: { display: "Negative" } }))).toBeUndefined()
    expect(
      outOfRange(
        r({ coded: { display: "Positive" }, interpretation: [{ code: "POS" }] })
      )?.word
    ).toBe("Abnormal")
    expect(
      outOfRange(
        r({ coded: { display: "Negative" }, interpretation: [{ code: "N" }] })
      )
    ).toBeUndefined()
  })
})

describe("rule 2: off target", () => {
  it("fires outside the green band only", () => {
    const ldl = entry("ldl_cholesterol")
    expect(offTarget(r({ value: 3.2 }), ldl)?.label).toBe("High")
    expect(offTarget(r({ value: 2.4 }), ldl)).toBeUndefined()
  })
  it("never fires where the catalogue sets no target", () => {
    expect(offTarget(r({ value: 99 }), entry("labs/igg"))).toBeUndefined()
    expect(
      offTarget(r({ value: 99 }), entry("free_androgen_index"))
    ).toBeUndefined()
  })
  it("counts both bounds of the target as on target (MPV 7.0 to 12.0)", () => {
    const mpv = entry("mean_platelet_volume")
    const at = (value: number) => offTarget(r({ value }), mpv)
    expect(at(7)).toBeUndefined()
    expect(at(12)).toBeUndefined()
    expect(at(6.99)?.label).toBe("Low")
    expect(at(12.01)?.label).toBe("High")
  })
  it("gives a shared edge to the green band (LDL 3.0, triglycerides 1.7)", () => {
    const ldl = entry("ldl_cholesterol")
    expect(offTarget(r({ value: 3 }), ldl)).toBeUndefined()
    expect(offTarget(r({ value: 3.01 }), ldl)?.label).toBe("High")
    expect(targetBand(ldl, 3)?.tone).toBe("success")
    const tri = entry("triglycerides")
    expect(targetBand(tri, 1.7)?.label).toBe("Normal")
    expect(offTarget(r({ value: 1.71 }), tri)?.label).toBe("Borderline High")
    expect(targetBand(tri, 2.2)?.label).toBe("Borderline High")
  })
  it("reads a comparator result against the target as an interval", () => {
    const ldl = entry("ldl_cholesterol")
    expect(offTarget(r({ value: 2, comparator: "<" }), ldl)).toBeUndefined()
    expect(offTarget(r({ value: 3, comparator: ">" }), ldl)?.label).toBe("High")
    expect(offTarget(r({ value: 4, comparator: "<" }), ldl)).toBeUndefined()
  })
  it("words the target with inclusive bounds", () => {
    const today = new Date("2026-10-29T12:00:00Z")
    expect(
      seriesFinding(
        emptyIndex,
        series("ldl_cholesterol", [r({ value: 3.2 })]),
        entry("ldl_cholesterol"),
        today
      )?.detail
    ).toContain("outside the target of up to 3")
    expect(
      seriesFinding(
        emptyIndex,
        series("mean_platelet_volume", [r({ value: 12 })]),
        entry("mean_platelet_volume"),
        today
      )
    ).toBeUndefined()
  })
  it("counts HDL above the normal band as green", () => {
    expect(
      offTarget(r({ value: 1.9 }), entry("hdl_cholesterol"))
    ).toBeUndefined()
  })
  it("reads the targets for the owner's sex", () => {
    const today = new Date("2026-10-29T12:00:00Z")
    const t = series("testosterone", [r({ value: 1.2 })])
    expect(
      seriesFinding(emptyIndex, t, entry("testosterone"), today)?.word
    ).toBe("Off target")
    expect(
      seriesFinding(
        emptyIndex,
        t,
        forSex(entry("testosterone")!, "female"),
        today
      )
    ).toBeUndefined()
  })
})

describe("rule 3: overdue", () => {
  const today = new Date("2026-10-29T12:00:00Z")
  const two = [
    r({ at: "2024-01-01T00:00:00Z" }),
    r({ at: "2025-08-01T00:00:00Z" }),
  ]
  it("fires on an important entry older than its cadence", () => {
    expect(overdue(two, entry("ldl_cholesterol"), today)).toBe(14)
  })
  it("needs two results and an important entry", () => {
    expect(
      overdue(two.slice(1), entry("ldl_cholesterol"), today)
    ).toBeUndefined()
    expect(overdue(two, entry("urea"), today)).toBeUndefined()
  })
  it("gives PSA two years, and none to a woman", () => {
    expect(overdue(two, entry("psa"), today)).toBeUndefined()
    const old = [
      r({ at: "2023-01-01T00:00:00Z" }),
      r({ at: "2023-08-01T00:00:00Z" }),
    ]
    expect(overdue(old, entry("psa"), today)).toBe(38)
    expect(overdue(old, forSex(entry("psa")!, "female"), today)).toBeUndefined()
  })
})

describe("trendFit", () => {
  const month = 30.4375 * 86_400_000
  const pts = (values: number[]) =>
    values.map((value, m) => ({
      at: new Date(Date.UTC(2026, 0, 1) + m * month).toISOString(),
      value,
    }))
  it("fits a rising series: rate, direction and endpoints", () => {
    const fit = trendFit(pts([10, 11, 12]))
    expect(fit?.direction).toBe("up")
    expect(fit?.ratePerMonth).toBeCloseTo(100 / 11, 2)
    expect(fit?.n).toBe(3)
    expect(fit?.from.value).toBeCloseTo(10, 6)
    expect(fit?.to.value).toBeCloseTo(12, 6)
    expect(fit?.from.at).toBe(pts([0])[0].at)
  })
  it("puts the endpoints on the line, not on the noisy points", () => {
    const fit = trendFit(pts([10, 14, 12]))
    expect(fit?.from.value).toBeCloseTo(11, 6)
    expect(fit?.to.value).toBeCloseTo(13, 6)
  })
  it("calls a falling series down with a negative rate", () => {
    const fit = trendFit(pts([12, 11, 10]))
    expect(fit?.direction).toBe("down")
    expect(fit?.ratePerMonth).toBeLessThan(0)
  })
  it("is flat while the printed rate is half a percent a month or less", () => {
    // 0.47 and -0.51 both print 0.5: both flat, so the arrow agrees.
    expect(trendFit(pts([100, 100.47, 100.94]))?.direction).toBe("flat")
    expect(trendFit(pts([100, 99.49, 98.98]))?.direction).toBe("flat")
    expect(trendFit(pts([100, 100.56, 101.12]))?.direction).toBe("up")
  })
  it("is flat within half a percent of the mean per month", () => {
    // 0.04 per month on a mean of 10.04 is about 0.4 percent.
    const fit = trendFit(pts([10, 10.04, 10.08]))
    expect(fit?.direction).toBe("flat")
    expect(fit?.ratePerMonth).toBeCloseTo(4 / 10.04, 6)
    expect(trendFit(pts([10, 10.06, 10.12]))?.direction).toBe("up")
  })
  it("needs three points on more than one instant", () => {
    expect(trendFit(pts([10, 11]))).toBeUndefined()
    expect(trendFit([])).toBeUndefined()
    const at = "2026-01-01T00:00:00Z"
    expect(
      trendFit([
        { at, value: 1 },
        { at, value: 2 },
        { at, value: 3 },
      ])
    ).toBeUndefined()
  })
})

describe("trendTone", () => {
  const ldl = entry("ldl_cholesterol")
  const hdl = entry("hdl_cholesterol")
  it("calls falling good when lower is better", () => {
    expect(trendTone({ direction: "down" }, ldl)).toBe("success")
    expect(trendTone({ direction: "up" }, ldl)).toBe("danger")
  })
  it("calls rising good when higher is better", () => {
    expect(trendTone({ direction: "up" }, hdl)).toBe("success")
    expect(trendTone({ direction: "down" }, hdl)).toBe("danger")
  })
  it("is neutral when flat, without a goal or without a fit", () => {
    expect(trendTone({ direction: "flat" }, ldl)).toBe("neutral")
    expect(trendTone({ direction: "up" }, entry("tsh"))).toBe("neutral")
    expect(trendTone(undefined, ldl)).toBe("neutral")
  })
})

describe("seriesStatuses", () => {
  const today = new Date("2026-10-29T12:00:00Z")
  const ldl = (results: LabResult[]) =>
    seriesStatuses(
      series("ldl_cholesterol", results),
      entry("ldl_cholesterol"),
      today
    )
  it("holds range and target together when both rules fire", () => {
    const s = ldl([
      r({ at: "2026-10-01T00:00:00Z", value: 3.4, ranges: [{ high: 3 }] }),
    ])
    expect([...s].sort()).toEqual(["range", "target"])
  })
  it("holds target alone when the lab range is wider than the target", () => {
    const s = ldl([
      r({ at: "2026-10-01T00:00:00Z", value: 3.4, ranges: [{ high: 4 }] }),
    ])
    expect([...s]).toEqual(["target"])
  })
  it("holds ok beside overdue, never beside range or target", () => {
    const s = ldl([
      r({ at: "2024-01-01T00:00:00Z", value: 2.5 }),
      r({ at: "2025-01-01T00:00:00Z", value: 2.5 }),
    ])
    expect([...s].sort()).toEqual(["ok", "overdue"])
  })
  it("reads only the newest result", () => {
    const s = ldl([
      r({ at: "2026-01-01T00:00:00Z", value: 3.4, ranges: [{ high: 3 }] }),
      r({ at: "2026-10-01T00:00:00Z", value: 2.5, ranges: [{ high: 3 }] }),
    ])
    expect([...s]).toEqual(["ok"])
  })
  it("never holds ok without a lab range or a target", () => {
    const lean = series("health/lean_mass", [
      r({ at: "2026-10-01T00:00:00Z", value: 60 }),
    ])
    expect(seriesStatuses(lean, undefined, today).size).toBe(0)
    const tsh = series("tsh", [r({ at: "2026-10-01T00:00:00Z", value: 2 })])
    expect([...seriesStatuses(tsh, entry("tsh"), today)]).toEqual(["ok"])
  })
  it("is empty for a series with no result", () => {
    expect(ldl([]).size).toBe(0)
  })
})

describe("evaluateLabs on the demo member", () => {
  const today = new Date("2026-10-25T00:00:00")
  const index = demoLabIndex(today)
  const fs = evaluateLabs(index, today)
  it("folds the derived twins under their inputs", () => {
    expect(index.series.has("labs/hba1c")).toBe(false)
    expect(index.series.has("labs/ldl_cholesterol_mass")).toBe(false)
    expect(index.series.get("hba1c")?.results.length).toBe(9)
  })
  it("finds the two flags first, then targets, then overdue", () => {
    const words = fs.map((f) => `${f.subject}:${f.word}`)
    expect(words.slice(0, 2).sort()).toEqual([
      "ldl_cholesterol:High",
      "vitamin_d:Low",
    ])
    expect(fs.find((f) => f.subject === "psa")?.word).toBe("Overdue")
    const ranks = fs.map((f) => f.rank)
    expect([...ranks].sort()).toEqual(ranks)
  })
  it("lists a series once, and opens its history", () => {
    const subjects = fs.map((f) => f.subject)
    expect(new Set(subjects).size).toBe(subjects.length)
    expect(fs.find((f) => f.subject === "ldl_cholesterol")?.href).toBe(
      "#/labs/ldl_cholesterol"
    )
  })
  it("orders important before not important within a rule", () => {
    const off = fs.filter((f) => f.rank === 2)
    const firstPlain = off.findIndex((f) => !f.important)
    if (firstPlain >= 0)
      expect(off.slice(firstPlain).every((f) => !f.important)).toBe(true)
  })
  it("drops PSA's overdue for a woman", () => {
    const female = evaluateLabs(index, today, "female")
    expect(female.find((f) => f.subject === "psa")).toBeUndefined()
  })
})

describe("resultFlag by target band", () => {
  it("names and tones an off-target result by the band it lands in", () => {
    // Non-HDL 3.57 is in the red "High" band (from 3.5).
    expect(
      resultFlag(r({ value: 3.57 }), entry("non_hdl_cholesterol"))
    ).toEqual({
      word: "High",
      tone: "danger",
    })
    // Triglycerides 1.9 is in the yellow "Borderline High" band.
    expect(resultFlag(r({ value: 1.9 }), entry("triglycerides"))).toEqual({
      word: "Borderline High",
      tone: "warning",
    })
  })
})
