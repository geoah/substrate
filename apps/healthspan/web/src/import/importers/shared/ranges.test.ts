import { describe, expect, it } from "vitest"
import { sharedUnits } from "../testing"
import {
  bandText,
  codedResult,
  cutoffText,
  flagOfRange,
  flagOfTonedBands,
  INTERPRETATION,
  interpretation,
  plainRanges,
  referenceRanges,
  tonedBandText,
  tonedRanges,
  type Band,
} from "./ranges"

const units = sharedUnits()

describe("a band's text", () => {
  it("reads a lab's word through the band words", () => {
    expect(bandText({ lab: "lab" })).toBe("normal")
    expect(bandText({ lab: " Deficient " })).toBe("low: deficient")
    expect(bandText({ lab: "Low risk" })).toBe("normal: low risk")
    expect(bandText({ lab: "desirable" })).toBe("optimal")
    expect(bandText({ lab: "insufficient" })).toBe("normal: insufficient")
    expect(bandText({ lab: "normal", expected: "negative" })).toBe(
      "normal: negative"
    )
  })

  it("marks a provider's own range by its detail", () => {
    expect(bandText({ word: "normal", detail: "Numan clinical range" })).toBe(
      "normal: Numan clinical range"
    )
  })

  it("names another panel's band by the panel", () => {
    expect(bandText({ lab: "high", panel: "Metabolic Syndrome" }, true)).toBe(
      "high: metabolic syndrome"
    )
  })
})

describe("a one-sided cut-off", () => {
  // The scripts' RandoxCutoffs: strict and inclusive cut-offs keep one bound
  // and differ in their words alone.
  it("keeps its strictness in words, its bound the same", () => {
    const bands: Band[] = [
      { lab: "Deficiency", high: "25", inclusive: false, tone: "poor" },
      { lab: "Deficiency", high: "25", inclusive: true, tone: "poor" },
      { lab: "Risk", low: "1.7", inclusive: false, tone: "poor" },
      { lab: "Risk", low: "1.7", inclusive: true, tone: "poor" },
    ]
    expect(bands.map((b) => cutoffText(b))).toEqual([
      "under 25",
      "at most 25",
      "over 1.7",
      "at least 1.7",
    ])
    const ranges = tonedRanges(bands, { ucum: "nmol/L", units })
    expect(ranges.map((r) => r.text)).toEqual([
      "deficiency: under 25",
      "deficiency: at most 25",
      "risk: over 1.7",
      "risk: at least 1.7",
    ])
    expect(ranges[0].high).toEqual(ranges[1].high)
    expect(ranges[2].low).toEqual({ value: "1.7", unit: "nmol/L" })
  })

  it("is the band's own word, with its criterion and another panel", () => {
    expect(
      tonedBandText(
        {
          lab: "Risk",
          low: "1.7",
          inclusive: false,
          panel: "Metabolic Syndrome",
          criterion: "Fasting",
        },
        true
      )
    ).toBe("risk: fasting, over 1.7, metabolic syndrome")
    expect(tonedBandText({ lab: "Moderately Raised" })).toBe(
      "moderately raised"
    )
    expect(tonedBandText({})).toBe("range")
  })
})

describe("toned ranges", () => {
  const optimal: Band = {
    lab: "Optimal",
    high: "3.0",
    inclusive: true,
    tone: "good",
    matched: true,
  }
  const high: Band = { lab: "High", low: "3.0", inclusive: false, tone: "poor" }
  const fair: Band = {
    lab: "Borderline",
    low: "2.5",
    high: "3.0",
    tone: "fair",
  }
  const secondary: Band = {
    lab: "Risk",
    low: "1.7",
    inclusive: false,
    tone: "poor",
    panel: "Metabolic Syndrome",
    primary: false,
  }

  it("orders the value's good band first, then fair, poor and other panels'", () => {
    const ranges = tonedRanges([high, secondary, fair, optimal], {
      ucum: "mmol/L",
      units,
    })
    expect(ranges).toEqual([
      { high: { value: "3.0", unit: "mmol/L" }, text: "optimal: at most 3.0" },
      {
        low: { value: "2.5", unit: "mmol/L" },
        high: { value: "3.0", unit: "mmol/L" },
        text: "borderline",
        tone: "fair",
      },
      {
        low: { value: "3.0", unit: "mmol/L" },
        text: "high: over 3.0",
        tone: "poor",
      },
      {
        low: { value: "1.7", unit: "mmol/L" },
        text: "risk: over 1.7, metabolic syndrome",
        tone: "poor",
      },
    ])
  })

  it("keeps once a band two panels print", () => {
    const again: Band = {
      ...optimal,
      panel: "Other",
      primary: false,
      matched: false,
    }
    expect(
      tonedRanges([optimal, again], { ucum: "mmol/L", units })
    ).toHaveLength(1)
  })

  it("keeps words alone for a value whose unit is kept as received", () => {
    const deficiency: Band = {
      lab: "Deficiency",
      high: "25",
      inclusive: false,
      tone: "poor",
      unit: "furlongs/L",
    }
    expect(referenceRanges([deficiency], { units }).ranges).toEqual([
      { text: "deficiency: under 25", tone: "poor" },
    ])
  })

  it("gives words their band's own unit where the repository knows it", () => {
    const strip: Band = {
      lab: "Normal",
      high: "15",
      tone: "good",
      unit: "mg/dl",
    }
    expect(tonedRanges([strip], { units })[0].high).toEqual({
      value: "15",
      unit: "mg/dL",
    })
    const cells: Band = {
      lab: "Normal",
      high: "10",
      tone: "good",
      unit: "RBC/µl",
    }
    expect(tonedRanges([cells], { units })[0]).toEqual({ text: "normal" })
  })
})

describe("plain ranges", () => {
  // The scripts' RandoxCutoffs: another source's `lab` band reads normal.
  it("reads a lab's range as normal", () => {
    const { ranges } = referenceRanges(
      [{ low: "0.0", high: "3.0", lab: "lab" }],
      { ucum: "mmol/L", units }
    )
    expect(ranges).toEqual([
      {
        low: { value: "0.0", unit: "mmol/L" },
        high: { value: "3.0", unit: "mmol/L" },
        text: "normal",
      },
    ])
  })

  it("puts the normal band first and a provider's own range last", () => {
    const bands: Band[] = [
      {
        word: "normal",
        detail: "Numan clinical range",
        low: "1",
        high: "3",
        unit: "mmol/L",
      },
      { high: "3.0", lab: "high" },
      { high: "3.0", lab: "normal" },
    ]
    const { ranges } = plainRanges(bands, { ucum: "mmol/L", units })
    expect(ranges.map((r) => [r.text, r.tone])).toEqual([
      ["normal", undefined],
      ["high", "poor"],
      ["normal: Numan clinical range", undefined],
    ])
  })

  it("moves a provider's range into the value's unit by a fixed factor", () => {
    const { ranges, notes } = plainRanges(
      [
        {
          word: "normal",
          detail: "Numan clinical range",
          low: "13.0",
          high: "17",
          unit: "g/dL",
        },
      ],
      { ucum: "g/L", units }
    )
    expect(notes).toEqual([])
    expect(ranges[0]).toMatchObject({
      low: { value: "130", unit: "g/L" },
      high: { value: "170", unit: "g/L" },
    })
  })

  it("leaves out a range no fixed factor moves, with a note", () => {
    const { ranges, notes } = plainRanges(
      [
        {
          word: "normal",
          detail: "Numan clinical range",
          low: "7",
          high: "40",
          unit: "nmol/L",
        },
      ],
      { ucum: "ug/L", units }
    )
    expect(ranges).toEqual([])
    expect(notes).toEqual(["the range in nmol/L has no fixed factor to ug/L"])
  })

  it("keeps a band once and drops one with neither bounds nor a word", () => {
    const { ranges } = plainRanges(
      [
        { low: "1", high: "2", lab: "lab" },
        { low: "1", high: "2", lab: "lab" },
        { low: "x" },
      ],
      { ucum: "mmol/L", units }
    )
    expect(ranges).toHaveLength(1)
    expect(referenceRanges([], { units }).ranges).toBeNull()
  })
})

describe("a flag", () => {
  it("is written as the HL7 interpretation, with the lab's words", () => {
    expect(interpretation("H", "H")).toEqual([
      { system: INTERPRETATION, code: "H", display: "High" },
    ])
    expect(interpretation("N")).toEqual([
      { system: INTERPRETATION, code: "N", display: "Normal" },
    ])
    expect(interpretation("L", "Below desirable range")).toEqual([
      { system: INTERPRETATION, code: "L", display: "Below desirable range" },
    ])
    expect(interpretation("Q")).toEqual([{ code: "Q" }])
    expect(interpretation(undefined)).toBeNull()
  })

  it("comes from the toned band the provider marks as the value's", () => {
    const good: Band = { lab: "Normal", low: "1", high: "3", tone: "good" }
    const good2: Band = {
      lab: "Optimal",
      low: "1.5",
      high: "2.5",
      tone: "good",
    }
    const above: Band = { lab: "High", low: "3", tone: "poor" }
    const below: Band = { lab: "Low", high: "1", tone: "poor" }
    const odd: Band = { lab: "Odd", criterion: "Trace", tone: "fair" }
    expect(flagOfTonedBands([{ ...good, matched: true }, above])).toBe("N")
    expect(flagOfTonedBands([good, good2, { ...above, matched: true }])).toBe(
      "H"
    )
    expect(flagOfTonedBands([good, { ...below, matched: true }])).toBe("L")
    expect(flagOfTonedBands([good, { ...odd, matched: true }])).toBe("A")
    expect(flagOfTonedBands([good, above])).toBeUndefined()
  })

  it("comes from a range, bounds inclusive, as decimals", () => {
    expect(flagOfRange("5.5", "4.0", "5.0")).toBe("H")
    expect(flagOfRange("3.9", "4.0", "5.0")).toBe("L")
    expect(flagOfRange("4.0", "4.00", "5.0")).toBe("N")
    expect(flagOfRange("10", undefined, "9.5")).toBe("H")
    expect(flagOfRange("x", "1", "2")).toBeUndefined()
  })
})

describe("a coded result", () => {
  it("is a code with SNOMED CT's qualifier, its words as received", () => {
    expect(codedResult("Negative")).toEqual({
      code: "negative",
      codedValue: {
        system: "http://snomed.info/sct",
        code: "260385009",
        display: "Negative",
      },
    })
    expect(codedResult("NOT DETECTED")?.code).toBe("not-detected")
    expect(codedResult("No flag")).toEqual({
      code: "none",
      codedValue: { display: "No flag" },
    })
    expect(codedResult("Trace")).toBeUndefined()
  })
})
