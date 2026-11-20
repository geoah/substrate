import { readFileSync } from "node:fs"
import { join } from "node:path"
import { Effect } from "effect"
import { describe, expect, it } from "vitest"
import { IMPORTERS } from ".."
import { csv } from "../csv"
import {
  chooseMetric,
  epoch,
  KINDS,
  type Band,
  type MetricChoice,
} from "../shared"
import {
  fakeContext,
  inputFromRecord,
  pdfFixture,
  planDiff,
  plannedRecord,
  runResult,
  type PlanView,
} from "../testing"
import type { ImportError, Input, Plan } from "../types"
import {
  band,
  exportFiles,
  FILE_NAMES,
  HEALTH_REPORT_PDF,
  LISTING,
  MANIFEST,
  order,
  panel,
  report,
  SHEET_RUNS,
  sheetPdf,
  SUMMER_SAMPLE,
  test,
  visit,
  WINTER_SAMPLE,
  WINTER_VISIT,
} from "./fixtures"
import { randox } from "."
import { RANDOX_TESTS } from "./tests"
import { fastedFor, portalTime, randoxBands } from "./visit"

const ctx = fakeContext()
const GOLDEN = JSON.parse(
  readFileSync(join(import.meta.dirname, "golden.json"), "utf8")
) as PlanView

const json = (v: unknown) => `${JSON.stringify(v, null, 2)}\n`

const read = async (input: Input) => {
  const r = await runResult(randox.read(input, ctx))
  if (r._tag === "Failure") throw new Error(JSON.stringify(r.failure))
  return r.success
}

const failure = async (input: Input): Promise<ImportError | undefined> => {
  const r = await runResult(randox.read(input, ctx))
  return r._tag === "Failure" ? r.failure : undefined
}

const props = (plan: Plan, key: string) =>
  plannedRecord(plan, KINDS.labresult, key)?.properties

/** The export with some files replaced, or left out where undefined. */
function exportWith(
  changes: Record<string, string | Uint8Array | undefined>,
  name = "randox"
): Input {
  const files = { ...exportFiles(), ...changes }
  for (const k of Object.keys(files))
    if (files[k] === undefined) delete files[k]
  return inputFromRecord(name, files as Record<string, string | Uint8Array>)
}

/** An export of one visit file and nothing else. */
const oneVisit = (v: unknown, more: Record<string, string> = {}) =>
  inputFromRecord("randox", { "items/visit/1.json": json(v), ...more })

describe("the Randox importer's plan", () => {
  it("is the golden plan for the invented export", async () => {
    expect(planDiff(await read(exportWith({})), GOLDEN)).toEqual([])
  })

  it("asks nothing", async () => {
    expect(
      await Effect.runPromise(randox.questions(exportWith({}), ctx))
    ).toEqual([])
  })

  it("keys every record by the provider's data", async () => {
    const plan = await read(exportWith({}))
    expect(plan.source).toEqual({
      kind: KINDS.source,
      namespace: "source",
      key: "randox",
    })
    const keys = plan.records.map((r) => `${r.namespace} ${r.key}`)
    expect(keys[0]).toBe("source randox")
    expect(keys).toContain(`randox ${WINTER_SAMPLE}`)
    expect(keys).toContain(`randox ${WINTER_SAMPLE}/LDL Cholesterol`)
    expect(keys).toContain(
      `randox ${WINTER_SAMPLE}/Total Cholesterol / HDL Cholesterol Ratio`
    )
    expect(keys).toContain(`randox ${WINTER_SAMPLE}/bia`)
    expect(keys).toContain(`randox ${WINTER_SAMPLE}/bia/Weight`)
    expect(keys).toContain(`randox ${WINTER_SAMPLE}/bia/Muscle Mass`)
    expect(keys).toContain(`randox ${WINTER_SAMPLE}/bia/Muscle Mass/trunk`)
    expect(keys).toContain(`randox ${WINTER_SAMPLE}/bia/Phase Angle`)
    expect(keys).toContain(`randox ${WINTER_SAMPLE}/bia/Phase Angle/leg_to_leg`)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it("writes each report before its results, each visit in its id's numeric order", async () => {
    const plan = await read(exportWith({}))
    const reports = plan.records
      .filter((r) => r.kind === KINDS.labreport)
      .map((r) => r.key)
    expect(reports).toEqual([
      SUMMER_SAMPLE,
      WINTER_SAMPLE,
      `${WINTER_SAMPLE}/bia`,
    ])
    let current = ""
    for (const r of plan.records) {
      if (r.kind === KINDS.labreport) current = r.key
      if (r.kind === KINDS.labresult)
        expect(
          (r.properties.report as { $ref: { key: string } }).$ref.key
        ).toBe(current)
    }
  })

  it("stores the health report and the sheet under neutral names, never the portal's file names", async () => {
    const plan = await read(exportWith({}))
    expect(plan.documents.map((d) => d.name)).toEqual([
      "Randox health report 2026-02-10.pdf",
      "Randox body composition 2026-02-10.pdf",
    ])
    expect(new TextDecoder().decode(plan.documents[0].bytes)).toBe(
      HEALTH_REPORT_PDF
    )
    const text = JSON.stringify(plan, (_, v: unknown) =>
      v instanceof Uint8Array ? undefined : v
    )
    for (const name of Object.values(FILE_NAMES))
      expect(text).not.toContain(name)
  })
})

describe("revisions", () => {
  it("are the visit's last update in London, else the manifest's newest run", async () => {
    const plan = await read(exportWith({}))
    const winter = plannedRecord(plan, KINDS.labreport, WINTER_SAMPLE)!
    expect(winter.revision).toBe(epoch("2026-02-11T10:00:00Z"))
    expect(
      plannedRecord(plan, KINDS.labreport, `${WINTER_SAMPLE}/bia`)!.revision
    ).toBe(winter.revision)
    expect(props(plan, `${WINTER_SAMPLE}/bia/Weight`)).toBeDefined()
    for (const r of plan.records)
      if (r.key.startsWith(WINTER_SAMPLE))
        expect(r.revision).toBe(winter.revision)
    const summer = plannedRecord(plan, KINDS.labreport, SUMMER_SAMPLE)!
    expect(summer.revision).toBe(epoch("2026-02-13T07:30:15Z"))
  })

  it("read a summer last update at its summer offset", async () => {
    const plan = await read(
      oneVisit(
        visit(
          1,
          [
            order(
              9,
              "RH9-1",
              report({ sampleCollectionDate: "2025-07-01T09:00:00" }, [
                panel("Diabetes Health", [
                  test("Glucose", "5.0", "mmol/l", null),
                ]),
              ])
            ),
          ],
          "2025-07-01T12:00:00.1234567"
        )
      )
    )
    expect(plannedRecord(plan, KINDS.labreport, "RH9-1")!.revision).toBe(
      epoch("2025-07-01T11:00:00Z")
    )
  })

  it("stop the read where neither the visit nor a manifest gives one", async () => {
    const lone = visit(1, [
      order(
        9,
        "RH9-1",
        report({ sampleCollectionDate: "2026-01-01T09:00:00" }, [])
      ),
    ])
    expect(await failure(oneVisit(lone))).toMatchObject({
      _tag: "Unreadable",
      path: "items/visit/1.json",
      field: "lastUpdated",
    })
  })
})

describe("orders", () => {
  it("leave out one not reported yet, saying so", async () => {
    const plan = await read(exportWith({}))
    expect(plan.left).toContainEqual({
      what: "items/visit/101.json: order RH9-00000102",
      why: "it is not reported yet",
    })
    expect(plannedRecord(plan, KINDS.labreport, "RH9-00000102")).toBeUndefined()
  })

  it("take the fasting from eight hours between the fast and the sample, written only with a specimen", async () => {
    const plan = await read(exportWith({}))
    expect(props(plan, `${WINTER_SAMPLE}/LDL Cholesterol`)!.specimen).toEqual({
      material: "blood",
      collectedAt: "2026-02-10T09:00:00+00:00",
      fasting: true,
    })
    expect(props(plan, `${SUMMER_SAMPLE}/Glucose`)!.specimen).toEqual({
      material: "blood",
      collectedAt: "2025-07-15T08:30:00+01:00",
      fasting: false,
    })
    expect(props(plan, `${SUMMER_SAMPLE}/Waist Circumference`)!.specimen).toBe(
      null
    )
    expect(
      props(plan, `${WINTER_SAMPLE}/Microalbumin / Creatinine Ratio`)!.specimen
    ).toMatchObject({ material: "urine" })
    expect(
      props(plan, `${WINTER_SAMPLE}/Protein (Urine)`)!.specimen
    ).toMatchObject({
      material: "urine",
    })
  })

  it("count the fast in wall-clock hours, eight exactly fasting", () => {
    const at = portalTime("2026-03-29T08:00:00")!
    expect(fastedFor(at, portalTime("2026-03-29T00:00:00.1234567")!)).toBe(true)
    expect(fastedFor(at, portalTime("2026-03-29T00:00:01")!)).toBe(false)
    expect(portalTime("2026-03-29T08:00:00Z")).toBeUndefined()
    expect(portalTime("yesterday")).toBeUndefined()
  })

  it("take the method from the order's programme", async () => {
    const plan = await read(exportWith({}))
    expect(props(plan, `${WINTER_SAMPLE}/Glucose`)!.method).toBe(
      "Vitality programme, in clinic"
    )
    expect(props(plan, `${WINTER_SAMPLE}/Height`)!.method).toBe(
      "measured at the clinic"
    )
    const unnamed = visit(
      1,
      [
        order(
          9,
          "RH9-1",
          report({ sampleCollectionDate: "2026-01-01T09:00:00" }, [
            panel("Diabetes Health", [test("Glucose", "5.0", "mmol/l", null)]),
          ]),
          { nexusOrderProgrammeName: null }
        ),
      ],
      "2026-01-02T09:00:00"
    )
    expect(props(await read(oneVisit(unnamed)), "RH9-1/Glucose")!.method).toBe(
      "in clinic"
    )
  })

  it("write no file where the listing's PDF is not in the folder, and say so", async () => {
    const plan = await read(
      exportWith({ [`files/report/${FILE_NAMES.PHP}.pdf`]: undefined })
    )
    const r = plannedRecord(plan, KINDS.labreport, WINTER_SAMPLE)!
    expect(r.properties.file).toBe(null)
    expect(plan.documents.map((d) => d.name)).toEqual([
      "Randox body composition 2026-02-10.pdf",
    ])
    expect(plan.left).toContainEqual({
      what: `the health report of ${WINTER_SAMPLE}`,
      why: "the visit's listing names it, and it is not in the folder",
    })
  })

  it("read a visit with no listing as one with no PDFs", async () => {
    const plan = await read(
      exportWith({ "items/visit-reports/101.json": undefined })
    )
    expect(
      plannedRecord(plan, KINDS.labreport, WINTER_SAMPLE)!.properties.file
    ).toBe(null)
    expect(
      plannedRecord(plan, KINDS.labreport, `${WINTER_SAMPLE}/bia`)
    ).toBeUndefined()
    expect(props(plan, `${WINTER_SAMPLE}/Weight`)).toMatchObject({
      value: { value: "81.4", unit: "kg" },
    })
    expect(plan.documents).toEqual([])
  })

  it("take only a published, final PDF of the order", async () => {
    const listing = LISTING.map((r) =>
      r.fileName === FILE_NAMES.PHP ? { ...r, published: false } : r
    )
    const plan = await read(
      exportWith({ "items/visit-reports/101.json": json(listing) })
    )
    expect(
      plannedRecord(plan, KINDS.labreport, WINTER_SAMPLE)!.properties.file
    ).toBe(null)
  })

  it("leave out a second order of one sample id", async () => {
    const twice = visit(
      1,
      [
        order(
          9,
          "RH9-1",
          report({ sampleCollectionDate: "2026-01-01T09:00:00" }, [
            panel("Diabetes Health", [test("Glucose", "5.0", "mmol/l", null)]),
          ])
        ),
        order(
          10,
          "RH9-1",
          report({ sampleCollectionDate: "2026-01-01T09:00:00" }, [
            panel("Diabetes Health", [test("Glucose", "6.0", "mmol/l", null)]),
          ])
        ),
      ],
      "2026-01-02T09:00:00"
    )
    const plan = await read(oneVisit(twice))
    expect(props(plan, "RH9-1/Glucose")!.value).toEqual({
      value: "5.0",
      unit: "mmol/L",
    })
    expect(plan.left).toContainEqual({
      what: "items/visit/1.json: order RH9-1",
      why: "an earlier visit has an order with the same sample id; the first is kept",
    })
  })
})

describe("tests and their bands", () => {
  it("are read by operator, tone and sort order", () => {
    const bands = randoxBands({
      unit: "mmol/l",
      ranges: [
        band(5, "Between", 0, "1.0", "2.0", "G", true),
        band(1, "Under", 1, "1", "", "R"),
        band(2, "At most", 2, "1", null, "A"),
        band(3, "Over", 3, "1", "", "x"),
        band(4, "At least", 4, "1", "", " g "),
        band(6, "Coded", 0, "Not Detected", "", "G"),
        band(7, "Trace", 0, "Trace", "", "A"),
        band(8, "Words alone", 0, "", "", "G"),
        band(9, "", 0, "", "", "G"),
        band(10, "One bound", 0, "3.0", "", "R"),
      ],
    })
    const base = { unit: "mmol/l", matched: false }
    expect(bands).toEqual<Band[]>([
      {
        ...base,
        lab: "Under",
        tone: "poor",
        low: null,
        high: "1",
        inclusive: false,
      },
      {
        ...base,
        lab: "At most",
        tone: "fair",
        low: null,
        high: "1",
        inclusive: true,
      },
      {
        ...base,
        lab: "Over",
        tone: "poor",
        low: "1",
        high: null,
        inclusive: false,
      },
      {
        ...base,
        lab: "At least",
        tone: "good",
        low: "1",
        high: null,
        inclusive: true,
      },
      {
        ...base,
        lab: "Between",
        tone: "good",
        matched: true,
        low: "1.0",
        high: "2.0",
      },
      {
        ...base,
        lab: "Coded",
        tone: "good",
        criterion: "Not Detected",
        expected: "not detected",
      },
      {
        ...base,
        lab: "Trace",
        tone: "fair",
        criterion: "Trace",
        expected: null,
      },
      { ...base, lab: "Words alone", tone: "good" },
      {
        ...base,
        lab: "One bound",
        tone: "poor",
        criterion: "3.0",
        expected: null,
      },
    ])
    expect(randoxBands(null)).toEqual([])
    expect(randoxBands({ unit: "x", ranges: null })).toEqual([])
  })

  it("keep a cut-off's strictness in its text, the bounds the same", async () => {
    const ops = [1, 2, 3, 4].map((op) =>
      test(`Vitamin D`, "40", "nmol/l", [
        band(1, "Cut", op, "25", "", "R", true),
      ])
    )
    const texts: unknown[] = []
    for (const t of ops) {
      const v = visit(
        1,
        [
          order(
            9,
            "RH9-1",
            report({ sampleCollectionDate: "2026-01-01T09:00:00" }, [
              panel("Nutritional Health", [t]),
            ])
          ),
        ],
        "2026-01-02T09:00:00"
      )
      texts.push(
        props(await read(oneVisit(v)), "RH9-1/Vitamin D")!.referenceRanges
      )
    }
    const q = { value: "25", unit: "nmol/L" }
    expect(texts).toEqual([
      [{ high: q, text: "cut: under 25", tone: "poor" }],
      [{ high: q, text: "cut: at most 25", tone: "poor" }],
      [{ low: q, text: "cut: over 25", tone: "poor" }],
      [{ low: q, text: "cut: at least 25", tone: "poor" }],
    ])
  })

  it("keep a value in a unit the repository does not know as received, its ranges in words", async () => {
    const plan = await read(exportWith({}))
    expect(props(plan, `${WINTER_SAMPLE}/Ferritin`)).toMatchObject({
      value: null,
      sourceValue: "<20",
      sourceUnit: "furlongs/L",
      normalizationFailure: "unknown-unit",
      referenceRanges: [{ text: "deficiency: under 25", tone: "poor" }],
    })
    expect(props(plan, `${WINTER_SAMPLE}/Vitamin D`)).toMatchObject({
      value: { value: "20", unit: "nmol/L", comparator: "<" },
      sourceValue: null,
      normalizationFailure: null,
    })
  })

  it("flag the band the portal marks, from the test's own panel only", async () => {
    const plan = await read(exportWith({}))
    const flag = (k: string) =>
      (
        props(plan, `${WINTER_SAMPLE}/${k}`)!.interpretation as
          { code: string }[] | null
      )?.[0]?.code
    expect(flag("LDL Cholesterol")).toBe("N")
    expect(flag("Body Mass Index (BMI)")).toBe("H")
    expect(flag("Triglycerides")).toBe("H")
    expect(flag("Vitamin D")).toBe("L")
    expect(flag("Ferritin")).toBe("A")
    expect(flag("Height")).toBeUndefined()
  })

  it("leave out the blood pressure row, an unknown test and an empty result, saying why", async () => {
    const plan = await read(exportWith({}))
    expect(plan.left).toEqual(
      expect.arrayContaining([
        {
          what: `Blood pressure, ${WINTER_SAMPLE}`,
          why: "it repeats the systolic value and its bands beside the systolic and diastolic rows",
        },
        {
          what: `Glorbin Index, ${WINTER_SAMPLE}`,
          why: "the repository has no metric for Glorbin Index",
        },
        { what: `Vitamin B12, ${WINTER_SAMPLE}`, why: "it has no value" },
      ])
    )
  })

  it("match a test the table does not name through the catalogue", async () => {
    const v = visit(
      1,
      [
        order(
          9,
          "RH9-1",
          report({ sampleCollectionDate: "2026-01-01T09:00:00" }, [
            panel("Heart Health", [
              test("Non-HDL Cholesterol", "3.1", "mmol/l", null),
            ]),
          ])
        ),
      ],
      "2026-01-02T09:00:00"
    )
    expect(
      props(await read(oneVisit(v)), "RH9-1/Non-HDL Cholesterol")!.metric
    ).toBe(`${KINDS.metric}/labs/non_hdl_cholesterol`)
  })

  // The units the portal prints each test in, and the shared metric each
  // is written under (the owner's former metrics mapped to the shared
  // catalogues).
  const UNITS: Record<string, [string | null, string]> = {
    Height: ["m", "health/height"],
    Weight: ["kg", "health/body_mass"],
    "Body Mass Index (BMI)": ["kg/m²", "health/bmi"],
    "Oxygen Saturation": ["%", "health/spo2"],
    "Waist Circumference": ["cm", "health/waist_circumference"],
    "Hip Circumference": ["cm", "health/hip_circumference"],
    "Waist / Hip Ratio": ["Ratio", "health/waist_hip_ratio"],
    Pulse: ["BPM", "health/heart_rate"],
    "Systolic Blood pressure": ["mmHg", "health/systolic_blood_pressure"],
    "Diastolic Blood pressure": ["mmHg", "health/diastolic_blood_pressure"],
    Haemoglobin: ["g/l", "labs/haemoglobin"],
    Haematocrit: ["%", "labs/haematocrit"],
    "Mean Cell Haemoglobin (MCH)": ["pg", "labs/mch"],
    "Mean Cell Haemoglobin Concentration (MCHC)": ["g/l", "labs/mchc"],
    "Red Blood Cell Mean Cell Volume (MCV)": ["fl", "labs/mcv"],
    "Red Blood Cell Count": ["10¹²/L", "labs/rbc"],
    "Basophil Count": ["10⁹/L", "labs/basophils"],
    "Eosinophil Count": ["10⁹/L", "labs/eosinophils"],
    "Lymphocyte Count": ["10⁹/L", "labs/lymphocytes"],
    "Monocyte Count": ["10⁹/L", "labs/monocytes"],
    "Neutrophil Count": ["10⁹/L", "labs/neutrophils"],
    "White Blood Cell Count": ["10⁹/L", "labs/wbc"],
    "Platelet Count": ["10⁹/L", "labs/platelets"],
    Iron: ["µmol/l", "labs/iron"],
    Ferritin: ["µg/l", "labs/ferritin"],
    "Total Iron Binding Capacity (TIBC)": ["µmol/l", "labs/tibc"],
    Transferrin: ["g/l", "labs/transferrin"],
    "Transferrin Saturation": ["%", "labs/transferrin_saturation"],
    "Total Cholesterol": ["mmol/l", "labs/total_cholesterol"],
    "LDL Cholesterol": ["mmol/l", "labs/ldl_cholesterol"],
    "HDL Cholesterol": ["mmol/l", "labs/hdl_cholesterol"],
    "Total Cholesterol / HDL Cholesterol Ratio": [
      "-",
      "labs/cholesterol_hdl_ratio",
    ],
    Triglycerides: ["mmol/l", "labs/triglycerides"],
    "Apolipoprotein A-I": ["mg/dl", "labs/apolipoprotein_a1"],
    "Apolipoprotein B": ["mg/dl", "labs/apolipoprotein_b"],
    "Apolipoprotein B / A-I Ratio": ["-", "labs/apob_apoa1_ratio"],
    "Apolipoprotein CII": ["mg/dl", "labs/apolipoprotein_c2"],
    "Apolipoprotein CIII": ["mg/dl", "labs/apolipoprotein_c3"],
    "Apolipoprotein E": ["mg/dl", "labs/apolipoprotein_e"],
    "Small LDL Cholesterol": ["mg/dl", "labs/small_dense_ldl_cholesterol"],
    "Lipoprotein (a)": ["nmol/l", "labs/lipoprotein_a"],
    "High Sensitivity C-Reactive Protein (hsCRP)": ["mg/l", "labs/hs_crp"],
    "Creatine Kinase": ["U/l", "labs/creatine_kinase"],
    "Fatty Acid Binding Protein-3 (FABP-3)": ["ng/ml", "labs/fabp3"],
    "Cardiovascular Risk Score": ["%", "labs/cardiovascular_risk"],
    Glucose: ["mmol/l", "labs/fasting_glucose"],
    HbA1c: ["mmol/mol", "labs/hba1c_ifcc"],
    Insulin: ["pmol/l", "labs/insulin"],
    "C-peptide": ["ng/ml", "labs/c_peptide"],
    "HOMA-IR": ["-", "labs/homa_ir"],
    "Microalbumin / Creatinine Ratio": [
      "mg/mmol creatinine",
      "labs/urine_albumin_creatinine_ratio",
    ],
    Leptin: ["µg/l", "labs/leptin"],
    Resistin: ["ng/ml", "labs/resistin"],
    Creatinine: ["µmol/l", "labs/creatinine"],
    "Estimated Glomerular Filtration Rate (eGFR)": [
      "ml/min/1.73m²",
      "labs/egfr",
    ],
    "Cystatin C": ["mg/l", "labs/cystatin_c"],
    "Calcium (adjusted)": ["mmol/l", "labs/calcium_adjusted"],
    Chloride: ["mmol/l", "labs/chloride"],
    Magnesium: ["mmol/l", "labs/magnesium"],
    Phosphate: ["mmol/l", "labs/phosphate"],
    Potassium: ["mmol/l", "labs/potassium"],
    Sodium: ["mmol/l", "labs/sodium"],
    Urea: ["mmol/l", "labs/urea"],
    "Uric Acid": ["µmol/l", "labs/uric_acid"],
    "Bilirubin (Urine)": [null, "labs/urine_bilirubin"],
    "Glucose (Urine)": [null, "labs/urine_glucose"],
    "Ketones (Urine)": [null, "labs/urine_ketones"],
    "Nitrite (Urine)": [null, "labs/urine_nitrite"],
    "pH (Urine)": ["pH", "labs/urine_ph"],
    "Protein (Urine)": [null, "labs/urine_protein"],
    "Red Blood Cells (Urine)": [null, "labs/urine_blood"],
    "Urobilinogen (Urine)": [null, "labs/urine_urobilinogen"],
    "White Blood Cells (Urine)": [null, "labs/urine_leukocytes"],
    "Alanine Aminotransferase (ALT)": ["U/l", "labs/alt"],
    "Alkaline Phosphatase (ALP)": ["U/l", "labs/alp"],
    "Aspartate Aminotransferase (AST)": ["U/l", "labs/ast"],
    "Gamma-Glutamyltransferase (GGT)": ["U/l", "labs/ggt"],
    "Total Bilirubin": ["µmol/l", "labs/bilirubin"],
    "Fibrosis-4 Score (FIB-4)": ["-", "labs/fib4"],
    Albumin: ["g/l", "labs/albumin"],
    "Pancreatic Amylase": ["U/l", "labs/pancreatic_amylase"],
    Lipase: ["U/l", "labs/lipase"],
    "H. pylori": ["U/ml", "labs/h_pylori_antibody"],
    "Total Antioxidant Status (TAS)": [
      "mmol/l",
      "labs/total_antioxidant_status",
    ],
    "Folic acid": ["µg/l", "labs/folate_mass"],
    "Vitamin B12": ["ng/l", "labs/vitamin_b12_mass"],
    "Vitamin D": ["nmol/l", "labs/vitamin_d"],
    "Rheumatoid Factor (RF)": ["kU/l", "labs/rheumatoid_factor"],
    "Parathyroid Hormone (PTH)": ["pmol/l", "labs/pth"],
    "Immunoglobulin E (IgE)": ["kU/l", "labs/ige"],
    "C-Reactive Protein (CRP)": ["mg/l", "labs/crp"],
    "Immunoglobulin A (IgA)": ["g/l", "labs/iga"],
    "Immunoglobulin G (IgG)": ["g/l", "labs/igg"],
    "Immunoglobulin M (IgM)": ["g/l", "labs/igm"],
    "Antistreptolysin O (ASO)": ["IU/ml", "labs/aso"],
    "Complement Component 3 (C3)": ["g/l", "labs/complement_c3"],
    "Complement Component 4 (C4)": ["g/l", "labs/complement_c4"],
    "Thyroid Stimulating Hormone (TSH)": ["mIU/l", "labs/tsh"],
    "Follicle Stimulating Hormone": ["U/l", "labs/fsh"],
    "Luteinising Hormone": ["U/l", "labs/lh"],
    Prolactin: ["mIU/l", "labs/prolactin"],
    "Free Thyroxine (FT4)": ["pmol/l", "labs/free_t4"],
    "Free Tri-iodothyronine (FT3)": ["pmol/l", "labs/free_t3"],
    "Anti-Thyroglobulin Antibody (Anti-Tg)": ["IU/ml", "labs/anti_tg"],
    "Anti-Thyroid Peroxidase Antibody (Anti-TPO)": ["kU/l", "labs/anti_tpo"],
    "Thyroxine-binding globulin (TBG)": ["µg/ml", "labs/tbg"],
    "Total Prostate Specific Antigen (TPSA)": ["µg/l", "labs/psa"],
    Oestradiol: ["pmol/l", "labs/estradiol"],
    Testosterone: ["nmol/l", "labs/testosterone"],
    "Sex Hormone Binding Globulin": ["nmol/l", "labs/shbg"],
    "Free Testosterone": ["nmol/l", "labs/free_testosterone"],
  }

  it("are each written under a shared metric, in a unit it takes", () => {
    expect(Object.keys(UNITS).sort()).toEqual(Object.keys(RANDOX_TESTS).sort())
    const got: Record<string, string> = {}
    const want: Record<string, string> = {}
    for (const [name, [unit, metric]] of Object.entries(UNITS)) {
      const c: MetricChoice = chooseMetric(
        RANDOX_TESTS[name],
        unit === null ? { numeric: false } : { numeric: true, unit },
        ctx
      )
      got[name] = `${c.kind} ${c.kind === "none" ? c.why : c.metric.id}`
      want[name] = `${unit === null ? "words" : "value"} ${metric}`
    }
    expect(got).toEqual(want)
  })
})

describe("the body composition sheet", () => {
  it("keeps the order's row of a measurement both give where it has bands, else the sheet's", async () => {
    const plan = await read(exportWith({}))
    expect(props(plan, `${WINTER_SAMPLE}/Weight`)).toBeUndefined()
    expect(props(plan, `${WINTER_SAMPLE}/bia/Weight`)).toMatchObject({
      value: { value: "81.6", unit: "kg" },
    })
    expect(props(plan, `${WINTER_SAMPLE}/Body Mass Index (BMI)`)).toBeDefined()
    expect(props(plan, `${WINTER_SAMPLE}/bia/BMI`)).toBeUndefined()
    expect(plan.left).toEqual(
      expect.arrayContaining([
        {
          what: `Weight, ${WINTER_SAMPLE}`,
          why: "the body composition sheet gives the same measurement with its desirable range; the sheet's is kept",
        },
        {
          what: "BMI, 4242 2026-02-10 09:20",
          why: "the visit's results give the same measurement; theirs is kept",
        },
      ])
    )
  })

  it("keeps the sheet's row where only the sheet's has a range, the order's where neither has", async () => {
    // the order's BMI without its bands
    const winter = structuredClone(WINTER_VISIT)
    const tests = (
      winter.nexusOrders[0] as {
        phpReportData: {
          panels: {
            tests: { name: string; referenceRange: { ranges: unknown } }[]
          }[]
        }
      }
    ).phpReportData.panels[0].tests
    tests.find(
      (t) => t.name === "Body Mass Index (BMI)"
    )!.referenceRange.ranges = null
    const changed = { "items/visit/101.json": json(winter) }
    const plan = await read(exportWith(changed))
    expect(
      props(plan, `${WINTER_SAMPLE}/Body Mass Index (BMI)`)
    ).toBeUndefined()
    expect(props(plan, `${WINTER_SAMPLE}/bia/BMI`)).toBeDefined()
    // and the sheet's BMI without its desirable range
    const bare = await read(
      exportWith({
        ...changed,
        [`files/report/${FILE_NAMES.BCA}.pdf`]: sheetPdf(
          SHEET_RUNS.filter((r) => r[3] !== "18.5-24.9")
        ),
      })
    )
    expect(props(bare, `${WINTER_SAMPLE}/Body Mass Index (BMI)`)).toBeDefined()
    expect(props(bare, `${WINTER_SAMPLE}/bia/BMI`)).toBeUndefined()
    // a measurement only the sheet gives is kept, range or none
    expect(props(bare, `${WINTER_SAMPLE}/bia/FFM`)).toBeDefined()
  })

  it("stops the read where the sheet's layout is not this one", async () => {
    const pdf = pdfFixture([
      "/F1 8 Tf\nBT 1 0 0 1 60 758 Tm (Not a sheet) Tj ET",
    ])
    const e = await failure(
      exportWith({ [`files/report/${FILE_NAMES.BCA}.pdf`]: pdf })
    )
    expect(e).toMatchObject({
      _tag: "Unreadable",
      path: `files/report/${FILE_NAMES.BCA}.pdf`,
    })
    expect((e as { message: string }).message).toMatch(/not in the layout/)
  })

  it("says so where the listed sheet is not in the folder, and keeps the order's weight", async () => {
    const plan = await read(
      exportWith({ [`files/report/${FILE_NAMES.BCA}.pdf`]: undefined })
    )
    expect(
      plannedRecord(plan, KINDS.labreport, `${WINTER_SAMPLE}/bia`)
    ).toBeUndefined()
    expect(props(plan, `${WINTER_SAMPLE}/Weight`)).toBeDefined()
    expect(plan.left).toContainEqual({
      what: `the body composition sheet of ${WINTER_SAMPLE}`,
      why: "the visit's listing names it, and it is not in the folder; its values are not read",
    })
  })
})

describe("reading a file that is not what the portal writes", () => {
  it("names the file and the field", async () => {
    const broken = visit(
      1,
      [order(9, "RH9-1", { summaryData: { fastingDate: null }, panels: [] })],
      "2026-01-02T09:00:00"
    )
    expect(await failure(oneVisit(broken))).toEqual({
      _tag: "Unreadable",
      path: "items/visit/1.json",
      field: "nexusOrders[0].phpReportData.summaryData.sampleCollectionDate",
      message: "Missing key",
    })
    const badTime = visit(
      1,
      [order(9, "RH9-1", report({ sampleCollectionDate: "10/02/2026" }, []))],
      "2026-01-02T09:00:00"
    )
    expect(await failure(oneVisit(badTime))).toMatchObject({
      _tag: "Unreadable",
      field: "nexusOrders[0].phpReportData.summaryData.sampleCollectionDate",
    })
    expect(
      await failure(inputFromRecord("x", { "items/visit/1.json": "{nope" }))
    ).toEqual({
      _tag: "Unreadable",
      path: "items/visit/1.json",
      message: "the file is not JSON",
    })
    expect(
      await failure(oneVisit({ lastUpdated: 5, nexusOrders: [] }))
    ).toMatchObject({ _tag: "Unreadable", field: "lastUpdated" })
  })

  it("is not its own where there is no visit folder", async () => {
    expect(
      await failure(inputFromRecord("x", { "a.json": "{}" }))
    ).toMatchObject({
      _tag: "NotMine",
    })
  })
})

// ---- detection ----

const others: Record<string, Input> = {
  "an NHS App run": inputFromRecord("20261028T140405Z", {
    "manifest.json": json({
      tool: "nhsctl v1",
      startedAt: "2026-10-28T14:04:05Z",
      endpoints: ["GET api.nhsapp.example/v1/patient/demographics"],
      responses: [
        { seq: 1, file: "responses/0001-api-v1-patient-demographics.json" },
      ],
    }),
    "responses/0001-api-v1-patient-demographics.json": json({ name: "x" }),
  }),
  "a MyChart export": inputFromRecord("mychart", {
    "manifest.json": json({
      tool: "mychartctl dev",
      provider: "example",
      items: ["test-results/1", "visit-details/2"],
      files: [],
      lists: [],
      runs: [{ startedAt: "2026-10-28T14:04:05Z" }],
    }),
    "items/test-results/1.json": json({ key: "1" }),
    "items/visit-details/2.json": json({ csn: "2" }),
  }),
  "a Numan export": inputFromRecord("a-numan-20260101.json", {
    "a-numan-20260101.json": json({
      data: [{ id: "1", attributes: { short_id: "VIR-1" } }],
    }),
  }),
  "a DXA printout": inputFromRecord("scan-result-1.pdf", {
    "scan-result-1.pdf": pdfFixture(["BT 1 0 0 1 10 10 Tm (Total) Tj ET"]),
  }),
  "a CSV of results": inputFromRecord("labs.csv", {
    "labs.csv": "Test,Value,Unit\nLDL,2.9,mmol/L\n",
  }),
}

describe("detection", () => {
  it("is sure of a folder whose manifest names the portal's visits", async () => {
    expect(await Effect.runPromise(randox.detect(exportWith({})))).toEqual({
      importer: "randox",
      confidence: 1,
      why: "A Randox Health export of 2 visits: randoxctl's manifest names the portal's visits",
    })
  })

  it("finds the export inside the folder or zip it was handed in", async () => {
    const files = Object.fromEntries(
      Object.entries(exportFiles()).map(([k, v]) => [`data/randox/${k}`, v])
    )
    const input = inputFromRecord("data.zip", files)
    expect(await Effect.runPromise(randox.detect(input))).toMatchObject({
      confidence: 1,
    })
    const plan = await read(input)
    // the same records; what is left out names its files by their paths
    expect(
      planDiff({ ...plan, name: "randox", left: GOLDEN.left }, GOLDEN)
    ).toEqual([])
    expect(plan.left[0].what).toBe(
      "data/randox/items/visit-reports/101.json: the biomarker tracking report"
    )
  })

  it("is less sure of visits in the portal's shape without the manifest", async () => {
    expect(
      await Effect.runPromise(
        randox.detect(exportWith({ "manifest.json": undefined }))
      )
    ).toEqual({
      importer: "randox",
      confidence: 0.6,
      why: "Randox Health visits in items/visit (2 visits), without randoxctl's manifest",
    })
    expect(
      await Effect.runPromise(
        randox.detect(
          exportWith({
            "manifest.json": json({ files: {}, runs: MANIFEST.runs }),
          })
        )
      )
    ).toMatchObject({ confidence: 0.6 })
  })

  it("does not claim visits of another shape, nor nothing", async () => {
    expect(
      await Effect.runPromise(randox.detect(oneVisit({ id: 1, visits: [] })))
    ).toBeUndefined()
    expect(
      await Effect.runPromise(
        randox.detect(inputFromRecord("x", { "items/visit/1.json": "{nope" }))
      )
    ).toBeUndefined()
    expect(
      await Effect.runPromise(randox.detect({ name: "x", files: [] }))
    ).toBeUndefined()
  })

  for (const [what, input] of Object.entries(others))
    it(`does not claim ${what}`, async () => {
      expect(await Effect.runPromise(randox.detect(input))).toBeUndefined()
    })

  it("is not claimed by another importer", async () => {
    const input = exportWith({})
    const importers = await Promise.all(
      Object.entries(IMPORTERS)
        .filter(([id]) => id !== "randox")
        .map(([, load]) => load())
    )
    for (const importer of [...importers, csv]) {
      const match = await Effect.runPromise(importer.detect(input))
      expect({ importer: importer.id, match }).toEqual({
        importer: importer.id,
        match: undefined,
      })
    }
  })
})
