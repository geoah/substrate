import { readFileSync, writeFileSync } from "node:fs"
import { join } from "node:path"
import { Effect } from "effect"
import { describe, expect, it } from "vitest"
import { csv } from "../csv"
import { IMPORTERS } from "../index"
import { KINDS, sourceKey } from "../shared"
import {
  fakeContext,
  inputFromRecord,
  pdfFixture,
  planDiff,
  plannedRecord,
  runResult,
  viewPlan,
  type PlanView,
} from "../testing"
import type { Input, Plan } from "../types"
import { nameDate, newestTests, readExports } from "./exports"
import {
  AMENDED_AFTER,
  AMENDED_BEFORE,
  DROPPED_TEST,
  HURDLE_TEST,
  LIST_TEST,
  NEWER,
  OLDER,
  ORDER_TEST,
  REVIEW_AFTER,
  REVIEW_BEFORE,
  REVIEWED_TEST,
  TWO_EXPORTS,
  hurdleRow,
  numanExport,
  randoxRow,
  type TestSpec,
} from "./fixtures"
import { numan } from "./index"
import { clinicalBand, hurdleBands, londonTime, methodOf } from "./report"

const GOLDEN = join(import.meta.dirname, "fixtures/two-exports.plan.json")

async function read(input: Input, ctx = fakeContext()): Promise<Plan> {
  return Effect.runPromise(numan.read(input, ctx))
}

async function failure(input: Input) {
  const r = await runResult(numan.read(input, fakeContext()))
  return r._tag === "Failure" ? r.failure : undefined
}

const result = (plan: Plan, key: string) =>
  plannedRecord(plan, KINDS.labresult, key)?.properties
const report = (plan: Plan, key: string) =>
  plannedRecord(plan, KINDS.labreport, key)

function documentJson(plan: Plan, key: string): unknown {
  const file = report(plan, key)?.properties.file as { $doc: string }
  const doc = plan.documents.find((d) => d.digest === file.$doc)!
  return JSON.parse(new TextDecoder().decode(doc.bytes))
}

const one = (spec: TestSpec, name = "alex-numan-20260101.json") =>
  inputFromRecord(name, { [name]: numanExport([spec]) })

describe("reading two exports", () => {
  it("makes the plan the fixture holds", async () => {
    const plan = await read(inputFromRecord("Numan exports", TWO_EXPORTS))
    if (process.env.UPDATE_GOLDEN)
      writeFileSync(GOLDEN, `${JSON.stringify(viewPlan(plan), null, 2)}\n`)
    const golden = JSON.parse(readFileSync(GOLDEN, "utf8")) as PlanView
    expect(planDiff(plan, golden)).toEqual([])
  })

  it("writes the Numan source first, then each report before its results, newest test first", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(plan.source).toEqual(sourceKey("numan"))
    expect(plan.records[0]).toEqual({
      ...sourceKey("numan"),
      mode: "create",
      properties: { name: "Numan", class: "service" },
    })
    const reports = plan.records
      .filter((r) => r.kind === KINDS.labreport)
      .map((r) => r.key)
    expect(reports).toEqual([
      LIST_TEST.shortId,
      AMENDED_AFTER.shortId,
      ORDER_TEST.shortId,
      REVIEWED_TEST.shortId,
      HURDLE_TEST.shortId,
      DROPPED_TEST.shortId,
    ])
    let current = ""
    for (const r of plan.records.slice(1)) {
      expect(r.namespace).toBe("numan")
      if (r.kind === KINDS.labreport) current = r.key
      else expect(r.key.startsWith(`${current}/`)).toBe(true)
    }
  })

  it("keys a report by the test's short id and a result by the short id and the lab's code", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(report(plan, ORDER_TEST.shortId)).toBeDefined()
    expect(
      result(plan, `${ORDER_TEST.shortId}/LDLCholesterol`)?.report
    ).toEqual({
      $ref: {
        kind: KINDS.labreport,
        namespace: "numan",
        key: ORDER_TEST.shortId,
      },
    })
    expect(
      result(plan, `${LIST_TEST.shortId}/Cholesterol / HDL Ratio`)
    ).toBeDefined()
    // Hurdle's rows share one id, which is not their key
    const hurdle = plan.records.filter((r) =>
      r.key.startsWith(`${HURDLE_TEST.shortId}/`)
    )
    expect(new Set(hurdle.map((r) => r.properties.upstreamId))).toEqual(
      new Set(["hd-test-0001"])
    )
    expect(hurdle.map((r) => r.key)).toEqual([
      `${HURDLE_TEST.shortId}/TSH`,
      `${HURDLE_TEST.shortId}/LDL`,
      `${HURDLE_TEST.shortId}/B12`,
      `${HURDLE_TEST.shortId}/FOL`,
      `${HURDLE_TEST.shortId}/EGFR`,
    ])
  })
})

describe("the newest version of a test", () => {
  it("is read from the export with the later updated_at, its revision that time", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(result(plan, `${AMENDED_AFTER.shortId}/Ferritin`)?.value).toEqual({
      value: "85",
      unit: "ug/L",
    })
    expect(report(plan, AMENDED_AFTER.shortId)?.revision).toBe(
      Date.parse(AMENDED_AFTER.updatedAt!) / 1000
    )
  })

  it("is kept from an older export where a later one leaves it out", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(result(plan, `${DROPPED_TEST.shortId}/ALT`)?.value).toEqual({
      value: "22",
      unit: "U/L",
    })
  })

  it("comes, at the same updated_at, from the export made later, with that export's review", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(documentJson(plan, REVIEWED_TEST.shortId)).toMatchObject({
      review: { attributes: { review: REVIEW_AFTER.review } },
    })
    // whichever order the files come in, and whatever their names say
    const swapped = await read(
      inputFromRecord("x", {
        "b-numan-20260101.json": NEWER,
        "a-numan-20261231.json": OLDER,
      })
    )
    expect(documentJson(swapped, REVIEWED_TEST.shortId)).toMatchObject({
      review: { attributes: { review: REVIEW_AFTER.review } },
    })
    expect(
      result(swapped, `${AMENDED_AFTER.shortId}/Ferritin`)?.value
    ).toMatchObject({ value: "85" })
  })

  it("takes the date in the file's name only where the exports say nothing else, then the later file", async () => {
    const before = numanExport([REVIEWED_TEST], [REVIEW_BEFORE])
    const other = numanExport(
      [
        {
          ...REVIEWED_TEST,
          raw: [randoxRow("Albumin", "Albumin", "46", "G_L")],
        },
      ],
      [REVIEW_BEFORE]
    )
    const albumin = async (files: Record<string, string>) =>
      (
        result(
          await read(inputFromRecord("x", files)),
          `${REVIEWED_TEST.shortId}/Albumin`
        )?.value as { value: string }
      ).value
    expect(
      await albumin({
        "a-numan-20260201.json": other,
        "b-numan-20260101.json": before,
      })
    ).toBe("46")
    expect(
      await albumin({ "a-export.json": other, "b-export.json": before })
    ).toBe("45")
  })

  it("reads the date in a file's name as its last run of exactly eight digits", () => {
    expect(nameDate("alex-numan-20261023.json")).toBe("20261023")
    expect(nameDate("1999-20260101-numan-20261023.json")).toBe("20261023")
    expect(nameDate("numan-202610231.json")).toBe("")
    expect(nameDate("numan.json")).toBe("")
  })

  it("lists every test once, the newest created first", async () => {
    const input = inputFromRecord("x", TWO_EXPORTS)
    const { exports } = await Effect.runPromise(readExports(input.files))
    const tests = newestTests(exports)
    expect(tests.map((t) => t.test.attributes.short_id)).toEqual([
      "NMN001-00000007",
      LIST_TEST.shortId,
      AMENDED_BEFORE.shortId,
      ORDER_TEST.shortId,
      REVIEWED_TEST.shortId,
      HURDLE_TEST.shortId,
      DROPPED_TEST.shortId,
    ])
    expect(tests[2].from.file.path).toBe("alex-numan-20261001.json")
  })
})

describe("a test's times", () => {
  it("reads the sample's time as London wall-clock, whatever its Z says", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(report(plan, ORDER_TEST.shortId)?.properties).toMatchObject({
      at: "2026-06-15T08:30:00+01:00",
      orderedAt: "2026-06-14T19:45:10+01:00",
      issuedAt: "2026-06-17T14:05:00+01:00",
    })
    expect(report(plan, DROPPED_TEST.shortId)?.properties.at).toBe(
      "2026-01-10T07:45:00+00:00"
    )
    expect(result(plan, `${ORDER_TEST.shortId}/HGB`)).toMatchObject({
      at: "2026-06-15T08:30:00+01:00",
      utcOffset: 60,
    })
    expect(londonTime("2026-03-29T01:30:00Z")).toBe("2026-03-29T01:30:00+00:00")
    expect(londonTime("2026-10-25T01:30:00.000Z")).toBe(
      "2026-10-25T01:30:00+01:00"
    )
    expect(londonTime("2026-10-25")).toBeUndefined()
  })

  it("takes, with no sample time, the time the lab received it in UTC, and the time results came as written", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(report(plan, HURDLE_TEST.shortId)?.properties).toMatchObject({
      at: "2026-02-03T09:15:00.000+00:00",
      orderedAt: null,
      issuedAt: "2026-02-04T16:20:00.000Z",
    })
    expect(report(plan, LIST_TEST.shortId)?.properties.issuedAt).toBe(
      LIST_TEST.resultsReceivedAt
    )
  })

  it("takes the revision from updated_at, else created_at", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(report(plan, LIST_TEST.shortId)?.revision).toBe(
      Date.parse(LIST_TEST.createdAt) / 1000
    )
    for (const r of plan.records.filter((x) =>
      x.key.startsWith(ORDER_TEST.shortId)
    ))
      expect(r.revision).toBe(Date.parse(ORDER_TEST.updatedAt!) / 1000)
  })

  it("leaves out a test that names no time its sample was taken or received", async () => {
    const plan = await read(inputFromRecord("x", TWO_EXPORTS))
    expect(report(plan, "NMN001-00000007")).toBeUndefined()
    expect(plan.left).toContainEqual({
      what: "test NMN001-00000007",
      why: "it names no time its sample was taken or received",
    })
  })
})

describe("Randox's rows", () => {
  it("reads an analyte once, from its first row, noting a second only where it differs", async () => {
    const plan = await read(one(ORDER_TEST))
    expect(
      plan.records.filter((r) =>
        r.key.startsWith(`${ORDER_TEST.shortId}/HDLCholesterol`)
      )
    ).toHaveLength(1)
    expect(
      result(plan, `${ORDER_TEST.shortId}/TotalCholesterol`)?.value
    ).toEqual({ value: "5.6", unit: "mmol/L" })
    expect(plan.left.filter((l) => l.why.startsWith("a second value"))).toEqual(
      [
        {
          what: `Total Cholesterol, ${ORDER_TEST.shortId}`,
          why: "a second value under the same key; the first is kept",
        },
      ]
    )
  })

  it("writes the lab's range and flag, Normal where a number has a range and no flag", async () => {
    const plan = await read(one(ORDER_TEST))
    const t = result(plan, `${ORDER_TEST.shortId}/Testosterone`)
    expect(t?.interpretation).toEqual([
      {
        system:
          "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation",
        code: "L",
        display: "Low",
      },
    ])
    expect(t?.referenceRanges).toEqual([
      {
        low: { value: "8.6", unit: "nmol/L" },
        high: { value: "29.0", unit: "nmol/L" },
        text: "normal",
      },
    ])
    expect(
      result(plan, `${ORDER_TEST.shortId}/LDLCholesterol`)?.interpretation
    ).toMatchObject([{ code: "N", display: "Normal" }])
    expect(
      result(plan, `${LIST_TEST.shortId}/Cholesterol / HDL Ratio`)
    ).toBeUndefined()
    const list = await read(one(LIST_TEST))
    expect(
      result(list, `${LIST_TEST.shortId}/Cholesterol / HDL Ratio`)
    ).toMatchObject({
      value: { value: "3.2", unit: "1" },
      referenceRanges: null,
      interpretation: null,
    })
  })

  it("splits a comparator, keeps words as text with no flag, and names the specimen", async () => {
    const plan = await read(one(ORDER_TEST))
    expect(result(plan, `${ORDER_TEST.shortId}/25-OHVitaminD`)?.value).toEqual({
      value: "10",
      unit: "nmol/L",
      comparator: "<",
    })
    expect(result(plan, `${ORDER_TEST.shortId}/HGB`)).toMatchObject({
      value: null,
      text: "Haemolysed",
      interpretation: null,
      referenceRanges: [{ text: "normal" }],
      specimen: {
        material: "whole blood",
        collectedAt: "2026-06-15T08:30:00+01:00",
      },
      coding: { system: "randox", code: "HGB", display: "Haemoglobin" },
      upstreamId: "tst-aaaa/HGB",
      method: "venous, in clinic",
      status: "final",
    })
  })

  it("takes an analyte its table lacks by its name, and leaves out one the repository has no metric for", async () => {
    const plan = await read(one(ORDER_TEST))
    expect(result(plan, `${ORDER_TEST.shortId}/SerumIron`)?.metric).toBe(
      `${KINDS.metric}/labs/iron`
    )
    expect(result(plan, `${ORDER_TEST.shortId}/Zinc`)).toBeUndefined()
    expect(plan.left).toContainEqual({
      what: `Zinc, ${ORDER_TEST.shortId}`,
      why: "the repository has no metric for Zinc",
    })
  })

  it("writes each analyte in its unit under the shared metric that takes it", async () => {
    const plan = await read(one(ORDER_TEST))
    const metricOf = (code: string) =>
      result(plan, `${ORDER_TEST.shortId}/${code}`)?.metric
    expect(metricOf("FolicAcid")).toBe(`${KINDS.metric}/labs/folate_mass`)
    expect(metricOf("VitaminB12")).toBe(`${KINDS.metric}/labs/vitamin_b12_mass`)
    expect(
      result(plan, `${ORDER_TEST.shortId}/FollicleStimulatingHormone`)?.value
    ).toEqual({ value: "4.2", unit: "[IU]/L" })
    expect(
      result(plan, `${ORDER_TEST.shortId}/FreeAndrogenIndex`)?.value
    ).toEqual({ value: "45.0", unit: "%" })
    expect(result(plan, `${ORDER_TEST.shortId}/eGFR`)?.value).toEqual({
      value: "90",
      unit: "mL/min/{1.73_m2}",
      comparator: ">",
    })
  })

  it("writes the order's blood pressure, measured at the clinic", async () => {
    const plan = await read(one(ORDER_TEST))
    expect(
      result(plan, `${ORDER_TEST.shortId}/patientSystolicBloodPressure`)
    ).toMatchObject({
      metric: `${KINDS.metric}/health/systolic_blood_pressure`,
      value: { value: "118", unit: "mm[Hg]" },
      referenceRanges: null,
      interpretation: null,
      specimen: null,
      method: "measured at the clinic, sent with the order",
      upstreamId: "tst-aaaa/patientSystolicBloodPressure",
      coding: {
        system: "randox",
        code: "patientSystolicBloodPressure",
        display: "patientSystolicBloodPressure",
      },
    })
    expect(
      result(plan, `${ORDER_TEST.shortId}/patientDiastolicBloodPressure`)?.value
    ).toEqual({ value: "76", unit: "mm[Hg]" })
  })

  it("issues the report at the first date of report, else when the results came", async () => {
    const plan = await read(one(AMENDED_AFTER))
    expect(report(plan, AMENDED_AFTER.shortId)?.properties).toMatchObject({
      lab: "Randox (ordered through Numan)",
      accession: AMENDED_AFTER.shortId,
      issuedAt: "2026-06-29T11:00:00+01:00",
      orderedAt: null,
      status: "completed",
    })
  })
})

describe("Numan's clinical range", () => {
  it("is read from normal_value in the unit after the value's first space", () => {
    const band = (normal: string, value = "1 mmol/L") =>
      clinicalBand({ identifier_short: "X", value, normal_value: normal })
    expect(band("0.0 - 3.0")).toEqual({
      low: "0.0",
      high: "3.0",
      lab: "normal",
      detail: "Numan clinical range",
      unit: "mmol/L",
    })
    expect(band("90 - 99999")).toMatchObject({ low: "90", high: null })
    expect(band("< 5.0")).toMatchObject({ low: null, high: "5.0" })
    expect(band(">1.0")).toMatchObject({ low: "1.0", high: null })
    expect(band(" 2 - 3 ", "2.5")).toMatchObject({ unit: null })
    expect(band("")).toBeUndefined()
    expect(band("Negative")).toBeUndefined()
  })

  it("is moved into the value's unit by a fixed factor, or left out with a note", async () => {
    const plan = await read(one(ORDER_TEST))
    expect(
      result(plan, `${ORDER_TEST.shortId}/MeanCellHaemConc`)?.referenceRanges
    ).toEqual([
      {
        low: { value: "320", unit: "g/L" },
        high: { value: "360", unit: "g/L" },
        text: "normal",
      },
      {
        low: { value: "320", unit: "g/L" },
        high: { value: "360", unit: "g/L" },
        text: "normal: Numan clinical range",
      },
    ])
    expect(
      result(plan, `${ORDER_TEST.shortId}/VitaminB12`)?.referenceRanges
    ).toContainEqual({
      low: { value: "200", unit: "ng/L" },
      high: { value: "900", unit: "ng/L" },
      text: "normal: Numan clinical range",
    })
    expect(
      result(plan, `${ORDER_TEST.shortId}/FolicAcid`)?.referenceRanges
    ).toEqual([
      {
        low: { value: "3.9", unit: "ug/L" },
        high: { value: "26.8", unit: "ug/L" },
        text: "normal",
      },
    ])
    expect(plan.left).toContainEqual({
      what: `Folate, ${ORDER_TEST.shortId}`,
      why: "the range in nmol/L has no fixed factor to ug/L, so it is left out",
    })
  })

  it("keeps its words beside a value kept as received", async () => {
    const plan = await read(one(HURDLE_TEST))
    expect(result(plan, `${HURDLE_TEST.shortId}/TSH`)).toMatchObject({
      value: null,
      sourceValue: "2.10",
      sourceUnit: "µIU/mL",
      normalizationFailure: "unknown-unit",
      referenceRanges: [
        { text: "normal" },
        { text: "low", tone: "poor" },
        { text: "high", tone: "poor" },
        { text: "normal: Numan clinical range" },
      ],
    })
  })
})

describe("Hurdle's rows", () => {
  it("reads each interpretation's rule as a band and the interpretation as the flag", async () => {
    const plan = await read(one(HURDLE_TEST))
    expect(result(plan, `${HURDLE_TEST.shortId}/B12`)).toMatchObject({
      value: { value: "150", unit: "pg/mL" },
      referenceRanges: [
        { low: { value: "180", unit: "pg/mL" }, text: "normal" },
        {
          high: { value: "180", unit: "pg/mL" },
          text: "low: deficient",
          tone: "poor",
        },
        {
          low: { value: "180", unit: "pg/mL" },
          high: { value: "900", unit: "pg/mL" },
          text: "normal: Numan clinical range",
        },
      ],
      interpretation: [{ code: "L", display: "Deficient" }],
      specimen: { material: "capillary blood" },
      method: "capillary microtainer, home kit",
      coding: { system: "hurdle", code: "B12", display: "Vitamin B12" },
    })
    expect(report(plan, HURDLE_TEST.shortId)?.properties.lab).toBe(
      "Hurdle (ordered through Numan)"
    )
  })

  it("reads a rule's bounds by their comparators, and no other shape", () => {
    expect(
      hurdleBands({ NORMAL: [">=0.27", "<=4.2"], HIGH: [">4.2"], Odd: ["1-2"] })
    ).toEqual([
      { low: "0.27", high: "4.2", lab: "normal" },
      { low: "4.2", high: null, lab: "high" },
      { low: null, high: null, lab: "odd" },
    ])
    expect(hurdleBands(null)).toEqual([])
    expect(hurdleBands([{ age: "40-49", NORMAL: ["<3"] }])).toBeUndefined()
    expect(hurdleBands({ NORMAL: "<3" })).toBeUndefined()
  })

  it("leaves out a rule of another shape, with a note, and keeps the value", async () => {
    const spec: TestSpec = {
      ...DROPPED_TEST,
      raw: [hurdleRow("ALT", "ALT", "IU/L", "22", "NORMAL", [{ any: 1 }])],
    }
    const plan = await read(one(spec))
    expect(result(plan, `${spec.shortId}/ALT`)).toMatchObject({
      value: { value: "22", unit: "U/L" },
      referenceRanges: null,
    })
    expect(plan.left).toEqual([
      {
        what: `ALT, ${spec.shortId}`,
        why: "Hurdle's ranges for it (data[0].attributes.results_raw[0].interpretationData.value) are not in a shape this importer reads, so they are left out",
      },
    ])
  })
})

describe("how a sample was taken", () => {
  it("is the first way the results name, read through the table", () => {
    const way = (detail: string | null, method: string | null) => ({
      collection_detail: detail,
      collection_method: method,
    })
    expect(methodOf([way("in-clinic", "venous")])).toBe("venous, in clinic")
    expect(methodOf([way(null, null), way("tasso", "capillary")])).toBe(
      "capillary, Tasso+ device"
    )
    expect(
      methodOf([way("tasso", "capillary"), way("fingerprick", "capillary")])
    ).toBe("capillary, finger prick")
    expect(methodOf([way(null, null)])).toBe("capillary microtainer, home kit")
    expect(methodOf([way("post", "venous")])).toBe(
      "capillary microtainer, home kit"
    )
    expect(methodOf([])).toBe("capillary microtainer, home kit")
  })
})

describe("the report's file", () => {
  it("is the test and its review as canonical JSON, under a neutral name", async () => {
    const plan = await read(one(ORDER_TEST))
    const file = report(plan, ORDER_TEST.shortId)?.properties.file as {
      $doc: string
    }
    const doc = plan.documents.find((d) => d.digest === file.$doc)!
    expect(doc.name).toBe("Numan test 2026-06-15.json")
    expect(doc.mediaType).toBe("application/json")
    const text = new TextDecoder().decode(doc.bytes)
    expect(text.startsWith('{"review":null,"test":{"attributes":{')).toBe(true)
    expect(text).not.toMatch(/\s"|":\s/)
    const exported = JSON.parse(numanExport([ORDER_TEST])) as {
      data: unknown[]
    }
    expect(JSON.parse(text)).toEqual({ review: null, test: exported.data[0] })
    expect(plan.documents).toHaveLength(1)
  })
})

describe("what is left out", () => {
  it("lists every file that is not a Numan export, and an export that is one page of several", async () => {
    const plan = await read(
      inputFromRecord("x", {
        "alex-numan-20260101.json": numanExport([DROPPED_TEST], [], {
          current: 1,
          last: 3,
          next: 2,
        }),
        "notes.json": '{"hello": "world"}',
        "broken.json": "{not json",
        "readme.txt": "hello",
        "older/alex-numan-20250101.json": numanExport([ORDER_TEST]),
      })
    )
    expect(plan.left).toEqual([
      {
        what: "alex-numan-20260101.json",
        why: "it is page 1 of 3; the tests on later pages come only from other exports",
      },
      { what: "broken.json", why: "it is not a Numan export" },
      { what: "notes.json", why: "it is not a Numan export" },
      {
        what: "older/alex-numan-20250101.json",
        why: "it is not a Numan export",
      },
      { what: "readme.txt", why: "it is not a Numan export" },
    ])
    expect(report(plan, ORDER_TEST.shortId)).toBeUndefined()
  })

  it("writes no report, and no file, for a test none of whose values can be written", async () => {
    const plan = await read(
      one({
        ...DROPPED_TEST,
        raw: [hurdleRow("SE", "Selenium", "µmol/L", "1.1", null, null)],
      })
    )
    expect(plan.records).toHaveLength(1)
    expect(plan.documents).toEqual([])
    expect(plan.left).toEqual([
      {
        what: `Selenium, ${DROPPED_TEST.shortId}`,
        why: "the repository has no metric for Selenium",
      },
      {
        what: `test ${DROPPED_TEST.shortId}`,
        why: "none of its values can be written, so its report is not",
      },
    ])
  })
})

describe("a file that is not what Numan promised", () => {
  const bad = (patch: (test: Record<string, unknown>) => void) => {
    const doc = JSON.parse(numanExport([ORDER_TEST])) as {
      data: Record<string, unknown>[]
    }
    patch(doc.data[0])
    return inputFromRecord("x", { "e.json": JSON.stringify(doc) })
  }
  const attributes = (t: Record<string, unknown>) =>
    t.attributes as Record<string, unknown>

  it("names the file and the field, never the value", async () => {
    expect(
      await failure(bad((t) => (attributes(t).created_at = 1234567)))
    ).toEqual({
      _tag: "Unreadable",
      path: "e.json",
      field: "data[0].attributes.created_at",
      message: "Expected string",
    })
    expect(
      await failure(bad((t) => (attributes(t).results_raw = "{secret")))
    ).toEqual({
      _tag: "Unreadable",
      path: "e.json",
      field: "data[0].attributes.results_raw",
      message: "the field is not JSON",
    })
    expect(
      await failure(
        bad(
          (t) =>
            (attributes(t).results_raw = JSON.stringify({
              reportResults: [{ analyte: "LDLCholesterol", result: 2.9 }],
            }))
        )
      )
    ).toMatchObject({
      field: "data[0].attributes.results_raw.reportResults[0].result",
    })
    expect(
      await failure(
        bad(
          (t) =>
            (attributes(t).results_raw = JSON.stringify([
              { biomarker: { code: "LDL", name: "LDL" }, result: "1" },
            ]))
        )
      )
    ).toMatchObject({
      field: "data[0].attributes.results_raw[0].biomarker.unit",
      message: "Missing key",
    })
    expect(
      await failure(bad((t) => (attributes(t).results_raw = '"words"')))
    ).toMatchObject({
      field: "data[0].attributes.results_raw",
      message: "Expected a Randox order or a list of results",
    })
    const time = await failure(
      bad((t) => (attributes(t).sample_taken_date = "15/06/2026 08:30"))
    )
    expect(time).toEqual({
      _tag: "Unreadable",
      path: "e.json",
      field: "data[0].attributes.sample_taken_date",
      message: "Expected a time as YYYY-MM-DDTHH:MM:SS",
    })
    expect(JSON.stringify(time)).not.toContain("08:30")
  })

  it("is not the importer's at all where no file carries Numan's marks", async () => {
    expect(
      await failure(inputFromRecord("x", { "a.json": '{"data": []}' }))
    ).toEqual({
      _tag: "NotMine",
      message: "None of these files is a Numan export.",
    })
  })
})

// ---- detection ----

/** Minimal inputs carrying another provider's marks, from their pages. */
const OTHERS: Record<string, Input> = {
  randox: inputFromRecord("randox", {
    "manifest.json": JSON.stringify({
      files: {},
      runs: [
        { started: "2026-01-20T10:00:00Z", finished: "2026-01-20T10:01:00Z" },
      ],
    }),
    "items/visit/123.json": JSON.stringify({
      id: 123,
      lastUpdated: "2026-01-20T10:00:00.1234567",
      nexusOrders: [],
    }),
  }),
  nhs: inputFromRecord("20260101T120000Z", {
    "manifest.json": JSON.stringify({
      tool: "nhsctl v0.1",
      startedAt: "2026-01-01T12:00:00Z",
      responses: [],
    }),
    "responses/0001-api-v1-patient-my-record.json": JSON.stringify({
      seq: 1,
      body: { response: { testResults: { data: [] } } },
    }),
  }),
  mychart: inputFromRecord("example", {
    "manifest.json": JSON.stringify({
      tool: "mychartctl",
      provider: "example",
      items: {},
      runs: [{ startedAt: "2026-01-01T12:00:00Z" }],
    }),
    "items/test-results/WP-1.json": JSON.stringify({
      orderName: "FULL BLOOD COUNT",
      results: [],
    }),
  }),
  dxa: inputFromRecord("dexa", {
    "scan-result-1.pdf": pdfFixture(["BT /F1 12 Tf 72 720 Td (Lunar) Tj ET"]),
    "scan-result-3.json": JSON.stringify({
      photo: "scan-result-3.jpg",
      scan: "01/01/2026",
      composition: [],
      distribution: [],
    }),
  }),
  csv: inputFromRecord("labs.csv", {
    "labs.csv": "date,test,value,unit\n2026-01-01,LDL,2.9,mmol/L\n",
  }),
  jsonapi: inputFromRecord("other.json", {
    "other.json": JSON.stringify({
      data: [{ id: "1", type: "order", attributes: { short_id: "X" } }],
    }),
  }),
}

describe("detecting a Numan export", () => {
  it("is sure of one or several exports by their marks, and says how many tests", async () => {
    expect(
      await Effect.runPromise(numan.detect(inputFromRecord("x", TWO_EXPORTS)))
    ).toEqual({
      importer: "numan",
      confidence: 1,
      why: "2 Numan exports of 7 tests.",
    })
    expect(await Effect.runPromise(numan.detect(one(ORDER_TEST)))).toEqual({
      importer: "numan",
      confidence: 1,
      why: "A Numan export of 1 test.",
    })
    expect(
      await Effect.runPromise(
        numan.detect(
          inputFromRecord("x", { ...TWO_EXPORTS, "notes.txt": "hello" })
        )
      )
    ).toMatchObject({ confidence: 0.9 })
  })

  it("does not claim another provider's input", async () => {
    for (const [name, input] of Object.entries(OTHERS))
      expect(await Effect.runPromise(numan.detect(input)), name).toBeUndefined()
  })

  it("is not claimed by another importer", async () => {
    const input = inputFromRecord("x", TWO_EXPORTS)
    for (const [name, load] of Object.entries(IMPORTERS)) {
      if (name === "numan") continue
      const other = await load()
      expect(await Effect.runPromise(other.detect(input)), name).toBeUndefined()
    }
    expect(await Effect.runPromise(csv.detect(one(ORDER_TEST)))).toBeUndefined()
  })

  it("asks nothing", async () => {
    expect(
      await Effect.runPromise(
        numan.questions(inputFromRecord("x", TWO_EXPORTS), fakeContext())
      )
    ).toEqual([])
    expect(numan.accepts).toEqual({ kind: "files", extensions: [".json"] })
  })
})
