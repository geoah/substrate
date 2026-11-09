import { describe, expect, it } from "vitest"
import { fakeContext } from "../testing"
import { KINDS, sourceKey } from "./keys"
import {
  labRecords,
  labReport,
  labResult,
  sourceRecord,
  type LabReportInput,
  type LabRow,
} from "./lab"
import { INTERPRETATION } from "./ranges"

const ctx = fakeContext()

const report: LabReportInput = {
  namespace: "randox",
  key: "RH1-00000001",
  source: sourceKey("randox"),
  lab: "Example Health",
  accession: "RH1-00000001",
  at: "2026-01-19T09:00:00+00:00",
  issuedAt: "2026-01-21T21:34:00+00:00",
  status: "final",
  file: "blob-sha256-ab",
  revision: 1768900000,
  review: true,
}

const row = (r: Partial<LabRow>): LabRow => ({
  key: "RH1-00000001/Vitamin D",
  analyte: { entry: "vitamin_d" },
  coding: { system: "randox-health", code: "Vitamin D", display: "Vitamin D" },
  value: "20",
  unit: "nmol/l",
  ...r,
})

const RESULT_KEYS = [
  "metric",
  "source",
  "report",
  "form",
  "hold",
  "at",
  "utcOffset",
  "channel",
  "value",
  "code",
  "codedValue",
  "text",
  "sourceValue",
  "sourceUnit",
  "normalizationFailure",
  "referenceRanges",
  "interpretation",
  "coding",
  "upstreamId",
  "specimen",
  "method",
  "status",
  "provenance",
]

describe("a provider's source", () => {
  it("is created where absent and never written again", () => {
    expect(
      sourceRecord({
        namespace: "randox",
        name: "Randox",
        class: "lab",
        manufacturer: "Randox Laboratories",
      })
    ).toEqual({
      kind: KINDS.source,
      namespace: "source",
      key: "randox",
      mode: "create",
      properties: {
        name: "Randox",
        class: "lab",
        manufacturer: "Randox Laboratories",
      },
    })
  })
})

describe("a lab report", () => {
  it("holds the report's properties, its document and source by reference", () => {
    const r = labReport(report)
    expect(r).toEqual({
      kind: KINDS.labreport,
      namespace: "randox",
      key: "RH1-00000001",
      mode: "maintain",
      revision: 1768900000,
      createOnly: ["review"],
      properties: {
        lab: "Example Health",
        accession: "RH1-00000001",
        at: "2026-01-19T09:00:00+00:00",
        orderedAt: null,
        issuedAt: "2026-01-21T21:34:00+00:00",
        status: "final",
        file: { $doc: "blob-sha256-ab" },
        source: {
          $ref: { kind: KINDS.source, namespace: "source", key: "randox" },
        },
        review: "pending",
      },
    })
  })

  it("writes no review where the owner is not asked to check it", () => {
    const r = labReport({ ...report, review: false, file: undefined })
    expect(r.createOnly).toBeUndefined()
    expect(r.properties).not.toHaveProperty("review")
    expect(r.properties.file).toBeNull()
  })
})

describe("a lab result", () => {
  // The scripts' Results tests, over the shared builder.
  it("keeps a value in a unit the repository does not know as received", () => {
    const out = labResult(
      report,
      row({
        value: "20",
        comparator: "<",
        unit: "furlongs/L",
        bands: [
          {
            lab: "Deficiency",
            high: "25",
            inclusive: false,
            tone: "poor",
            unit: "furlongs/L",
          },
        ],
      }),
      ctx
    )
    const p = out.record!.properties
    expect(p.value).toBeNull()
    expect(p.sourceValue).toBe("<20")
    expect(p.sourceUnit).toBe("furlongs/L")
    expect(p.normalizationFailure).toBe("unknown-unit")
    expect(p.referenceRanges).toEqual([
      { text: "deficiency: under 25", tone: "poor" },
    ])
    expect(p.metric).toBe(`${KINDS.metric}/labs/vitamin_d`)
    expect(out.left).toEqual([
      {
        what: "Vitamin D, RH1-00000001",
        why: "its value is kept as received: its unit, furlongs/L, is not one the repository knows",
      },
    ])
  })

  it("writes a value in a known unit as a quantity, with no failure", () => {
    const p = labResult(report, row({}), ctx).record!.properties
    expect(p.value).toEqual({ value: "20", unit: "nmol/L" })
    expect(p.normalizationFailure).toBeNull()
    expect(p.sourceValue).toBeNull()
  })

  it("writes fasting false where the provider says, and leaves it out where not", () => {
    const fasted = labResult(
      report,
      row({ specimen: { material: "blood", fasting: false } }),
      ctx
    ).record!.properties
    expect(fasted.specimen).toEqual({
      material: "blood",
      collectedAt: report.at,
      fasting: false,
    })
    const unknown = labResult(
      report,
      row({ specimen: { material: "blood" } }),
      ctx
    ).record!.properties
    expect(unknown.specimen).toEqual({
      material: "blood",
      collectedAt: report.at,
    })
  })

  it("takes its report's revision, time, offset and source", () => {
    const r = labResult(
      { ...report, at: "2026-07-01T09:30:00+01:00" },
      row({}),
      ctx
    ).record!
    expect(r.revision).toBe(report.revision)
    expect(r.properties.at).toBe("2026-07-01T09:30:00+01:00")
    expect(r.properties.utcOffset).toBe(60)
    expect(r.properties.report).toEqual({
      $ref: { kind: KINDS.labreport, namespace: "randox", key: "RH1-00000001" },
    })
    expect(r).toMatchObject({
      kind: KINDS.labresult,
      namespace: "randox",
      key: "RH1-00000001/Vitamin D",
      mode: "maintain",
    })
  })

  it("names every property it maintains, null where the provider gives none", () => {
    const p = labResult(report, row({}), ctx).record!.properties
    expect(Object.keys(p)).toEqual(RESULT_KEYS)
    expect(p).toMatchObject({
      form: "gauge",
      hold: "PT0S",
      status: "final",
      channel: null,
      provenance: null,
      interpretation: null,
      coding: {
        system: "randox-health",
        code: "Vitamin D",
        display: "Vitamin D",
      },
    })
  })

  it("writes a coded word as a code, other words as text", () => {
    const negative = labResult(
      report,
      row({ value: null, text: "Negative", unit: "mg/dl" }),
      ctx
    ).record!.properties
    expect(negative.code).toBe("negative")
    expect(negative.codedValue).toMatchObject({ code: "260385009" })
    expect(negative.text).toBeNull()
    const words = labResult(report, row({ value: null, text: "Trace" }), ctx)
      .record!.properties
    expect(words).toMatchObject({ text: "Trace", code: null, value: null })
  })

  it("writes a flag, a region, a page and a method", () => {
    const p = labResult(
      report,
      row({
        analyte: { metric: "health/fat_mass" },
        value: "1234",
        unit: "g",
        flag: "H",
        flagText: "Above desirable range",
        channel: "left_arm",
        page: 2,
        method: "DXA whole body",
        upstreamId: "2026-01-19/reg_fat_mass/left_arm",
      }),
      ctx
    ).record!.properties
    expect(p).toMatchObject({
      metric: `${KINDS.metric}/health/fat_mass`,
      value: { value: "1234", unit: "g" },
      channel: "left_arm",
      provenance: { page: 2 },
      method: "DXA whole body",
      upstreamId: "2026-01-19/reg_fat_mass/left_arm",
      interpretation: [
        { system: INTERPRETATION, code: "H", display: "Above desirable range" },
      ],
    })
  })

  it("is left out with no value, or with no metric in the repository", () => {
    expect(labResult(report, row({ value: null }), ctx)).toEqual({
      left: [{ what: "Vitamin D, RH1-00000001", why: "it has no value" }],
    })
    expect(
      labResult(report, row({ analyte: { metric: "labs/not_a_metric" } }), ctx)
        .record
    ).toBeUndefined()
  })

  it("notes a range it leaves out", () => {
    const out = labResult(
      report,
      row({
        analyte: { entry: "folate" },
        value: "5",
        unit: "ug/L",
        bands: [
          {
            word: "normal",
            detail: "Numan clinical range",
            low: "7",
            high: "40",
            unit: "nmol/L",
          },
        ],
      }),
      ctx
    )
    expect(out.record!.properties.referenceRanges).toBeNull()
    expect(out.left).toEqual([
      {
        what: "Vitamin D, RH1-00000001",
        why: "the range in nmol/L has no fixed factor to ug/L, so it is left out",
      },
    ])
  })
})

describe("a report with its results", () => {
  it("is the report then its results, a repeated key kept once", () => {
    const { records, left } = labRecords(
      report,
      [
        row({}),
        row({ value: "21" }),
        row({
          key: "RH1-00000001/Ferritin",
          analyte: { entry: "ferritin" },
          unit: "µg/l",
        }),
      ],
      ctx
    )
    expect(records.map((r) => [r.kind, r.key])).toEqual([
      [KINDS.labreport, "RH1-00000001"],
      [KINDS.labresult, "RH1-00000001/Vitamin D"],
      [KINDS.labresult, "RH1-00000001/Ferritin"],
    ])
    expect(records[1].properties.value).toEqual({ value: "20", unit: "nmol/L" })
    expect(left).toEqual([
      {
        what: "Vitamin D, RH1-00000001",
        why: "a second value under the same key; the first is kept",
      },
    ])
  })

  it("is not written where no result is left", () => {
    expect(labRecords(report, [row({ value: null })], ctx).records).toEqual([])
  })
})
