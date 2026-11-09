// Invented Randox Health exports in randoxctl's exact shape, for the
// importer's tests only: two visits (one in winter with its health report
// and its body composition sheet, one in summer with neither), every rule
// of the portal's bands, and a Tanita sheet laid out as the scanner prints
// it. No value here is anyone's.

import { pdfFixture } from "../testing"
import { VISITS_ENDPOINT } from "./files"

type Range = {
  sortOrder?: number | null
  description?: string | null
  operator?: number | null
  value1?: string | null
  value2?: string | null
  statusColourSymbol?: string | null
  matchedRange?: boolean | null
}

/** One band, as the portal writes it. */
export function band(
  sortOrder: number,
  description: string,
  operator: number,
  value1: string,
  value2: string | null,
  colour: string,
  matched = false
): Range {
  return {
    sortOrder,
    description,
    operator,
    value1,
    value2,
    statusColour: colour === "G" ? "Green" : colour === "A" ? "Amber" : "Red",
    statusColourSymbol: colour,
    matchedRange: matched,
  } as Range
}

/** One test, as the portal writes it. */
export function test(
  name: string,
  displayValue: string | null,
  unit: string,
  ranges: Range[] | null,
  extra: Record<string, unknown> = {}
) {
  return {
    chart: null,
    name,
    displayOrder: 1,
    description: "",
    displayTest: true,
    result: { value: displayValue, displayValue, secondaryValue: null },
    referenceRange: { name: null, unit, type: 1, ranges },
    secondaryReferenceRange: null,
    ...extra,
  }
}

export function panel(name: string, tests: unknown[]) {
  return { id: 1, name, displayOrder: 1, description: "", tests }
}

/** An order, reported with its report data, or not. */
export function order(
  id: number,
  sample: string,
  report: unknown,
  more: Record<string, unknown> = {}
) {
  return {
    id,
    visitId: 1,
    nexusExternalNumber: sample,
    nexusOrderProgrammeName: "Vitality",
    nexusOrderProgrammeAbbreviation: "VT",
    isReported: report !== null,
    hasPreliminaryResults: false,
    phpReportData: report,
    ...more,
  }
}

export function report(
  summary: Record<string, string | null>,
  panels: unknown[]
) {
  return {
    includeHealthStatusSection: true,
    summaryData: {
      identifier: "000000",
      firstName: "Alex",
      lastName: "Example",
      gender: "M",
      programmes: ["Vitality"],
      ...summary,
    },
    measurements: [],
    panels,
  }
}

export function visit(
  id: number,
  orders: unknown[],
  lastUpdated?: string | null
) {
  return {
    id,
    userID: 1,
    visitDate: "2026-02-10T00:00:00",
    lastUpdated: lastUpdated ?? null,
    phpReportData: null,
    nexusOrders: orders,
  }
}

const json = (v: unknown) => `${JSON.stringify(v, null, 2)}\n`

// ---- the body composition sheet ----

/** A run of the sheet: where it sits, its font and its text. */
export type SheetRun = [y: number, x: number, font: string, text: string]

/** Text as the sheet's generator writes it: a hex string of glyph ids 29
 * below the ASCII code; U+FFFD (the degree sign) as a glyph past ASCII. */
function glyphs(text: string): string {
  return `<${[...text]
    .map((c) =>
      (c === "�" ? 0x63 : c.charCodeAt(0) - 29).toString(16).padStart(4, "0")
    )
    .join("")}>`
}

/** A sheet's PDF: one content stream, deflated with the trailing byte the
 * generator leaves, each run a text block placed by `Tm`, its font set
 * before it. */
export function sheetPdf(runs: readonly SheetRun[]): Uint8Array {
  const content = runs
    .map(
      ([y, x, font, text]) =>
        `/${font} 8 Tf\nBT\n1 0 0 1 ${x} ${y} Tm\n[${glyphs(text)}] TJ\nET`
    )
    .join("\n")
  return pdfFixture([content], { compress: true, trailingEol: true })
}

/** A Tanita MC-780 sheet's runs, its labels where the scanner prints them
 * and invented values, with the decoys its layout holds: labels repeated
 * in other boxes and fonts, and numbers near the values. */
export const SHEET_RUNS: SheetRun[] = [
  [700, 70, "F1", "MC-780"],
  [772, 440, "F3", "10/02/2026 09:20"],
  [758, 60, "F1", "ID"],
  [758, 100, "F3", "4242"],
  [744, 60, "F1", "Name"],
  [744, 88, "F3", "Alex Example"],
  [698, 130, "F1", "Result"],
  [698, 186, "F1", "Desirable"],
  // the result box
  [684, 62, "F2", "Weight"],
  [684, 135, "F3", "81.6"],
  [684, 190, "F3", "68.0-83.0"],
  [670, 62, "F2", "Fat"],
  [670, 135, "F3", "22.4"],
  [670, 190, "F3", "11.0-21.9"],
  [656, 62, "F2", "Fat Mass"],
  [656, 135, "F3", "18.3"],
  [642, 62, "F2", "FFM"],
  [642, 135, "F3", "63.3"],
  [628, 62, "F2", "Muscle Mass"],
  [628, 135, "F3", "60.1"],
  [628, 190, "F3", "52.0-66.0"],
  [614, 62, "F2", "BMI"],
  [614, 135, "F3", "25.2"],
  [614, 190, "F3", "18.5-24.9"],
  [600, 62, "F2", "Metabolic Age"],
  [600, 135, "F3", "41"],
  [566, 125, "F3", "7510"],
  [566, 159, "F3", "kJ"],
  [560, 59, "F2", "BMR"],
  [552, 125, "F3", "1795"],
  [552, 154, "F3", "kcal"],
  [527.5, 59, "F2", "Visceral Fat"],
  [520, 59, "F2", "Rating"],
  [520, 135, "F3", "8"],
  [486.5, 59, "F2", "TBW"],
  [492.8, 130, "F3", "45.0"],
  [492.8, 158.7, "F3", "kg"],
  [478.7, 130, "F3", "55.1"],
  [478.7, 159.4, "F3", "%"],
  [486.6, 180.6, "F2", "ECW"],
  [486.6, 200, "F3", "19.2"],
  [486.6, 249, "F2", "ICW"],
  [486.6, 270, "F3", "25.8"],
  [450, 59, "F2", "ECW/TBW"],
  [450, 135, "F3", "42.7"],
  [448, 159.4, "F3", "%"],
  // the top box: the label, its value about ten units under it
  [699.6, 381.6, "F4", "Fat Mass"],
  [689.3, 385, "F3", "18.3"],
  [699.6, 427.4, "F4", "Bone Mass"],
  [689.3, 430, "F3", "3.3"],
  [699.6, 481.4, "F4", "Protein"],
  [689.3, 485, "F3", "12.4"],
  [699.6, 532.5, "F4", "ECW"],
  [689.3, 535, "F3", "19.2"],
  [612.3, 336.4, "F4", "Weight"],
  [612.3, 424.7, "F4", "Muscle Mass"],
  // the muscle mass balance and the leg muscle score
  [282, 85, "F2", "Muscle Mass Balance"],
  [282, 260, "F2", "Leg Muscle Score"],
  [190, 270, "F3", "95"],
  [253.8, 85, "F3", "Left Arm"],
  [253.7, 146.5, "F3", "Right Arm"],
  // the segmental analysis: muscle on the left half, fat on the right
  [410.6, 156.4, "F2", "Muscle Mass"],
  [397, 97, "F4", "Trunk"],
  [387, 97, "F3", "31.2kg"],
  [361, 67, "F4", "Left Arm"],
  [351, 67, "F3", "3.4kg"],
  [361, 266, "F4", "Right Arm"],
  [351, 266, "F3", "3.5kg"],
  [325.5, 79, "F4", "Left Leg"],
  [315.5, 79, "F3", "10.6kg"],
  [325.4, 254, "F4", "Right Leg"],
  [315.4, 254, "F3", "10.8kg"],
  [410.6, 433, "F2", "Fat"],
  [397, 355, "F4", "Trunk"],
  [387, 355, "F3", "24.1% / 10.2kg"],
  [361, 325, "F4", "Left Arm"],
  [351, 325, "F3", "19.8% / 0.9kg"],
  [361, 524, "F4", "Right Arm"],
  [351, 524, "F3", "19.5% / 0.9kg"],
  [325.5, 337, "F4", "Left Leg"],
  [315.5, 337, "F3", "18.7% / 2.6kg"],
  [325.4, 512, "F4", "Right Leg"],
  [315.4, 512, "F3", "18.9% / 2.7kg"],
  // the phase angles at 50 kHz
  [132.2, 545, "F1", "Phase"],
  [125.5, 546, "F1", "Angle"],
  [137.7, 395, "F1", "Reactance Resistance"],
  [116.4, 394.3, "F1", "H-L"],
  [116.4, 450, "F3", "512.4"],
  [116.4, 560, "F3", "6.2�"],
  [105, 394.3, "F1", "RL"],
  [105, 560, "F3", "6.5�"],
  [93.4, 394.3, "F1", "LL"],
  [93.4, 560, "F3", "6.4�"],
  [82, 394.3, "F1", "RH"],
  [82, 560, "F3", "5.9�"],
  [70, 394.3, "F1", "LH"],
  [70, 560, "F3", "5.8�"],
  [58.6, 394.3, "F1", "L-L"],
  [58.6, 560, "F3", "7.1�"],
]

// ---- the export ----

export const WINTER_SAMPLE = "RH9-00000101"
export const SUMMER_SAMPLE = "RH9-00000007"
const PHP = "100001_20260210_5101_SCFIRSTPHP_EM_S"
const PRELIMINARY = "100001_20260210_5101_SCFIRSTPHP_EM_P"
const BCA = "100001_20260210_5101_SCFIRSTBCA_EM_S"
const TRACK = "100001_20260210_SCTRACKRES_EM_S"

export const HEALTH_REPORT_PDF = "%PDF-1.4\n% an invented health report\n"

/** The winter visit: a reported order with every kind of band, and an
 * order not reported yet. */
export const WINTER_VISIT = visit(
  101,
  [
    order(
      5101,
      WINTER_SAMPLE,
      report(
        {
          sampleCollectionDate: "2026-02-10T09:00:00",
          fastingDate: "2026-02-09T22:00:00",
          resultDate: "2026-02-12T21:34:00.1234567",
        },
        [
          panel("Personal Health Measurements", [
            test("Height", "1.80", "m", []),
            test("Weight", "81.4", "kg", null),
            test("Body Mass Index (BMI)", "25.1", "kg/m²", [
              band(2, "Overweight", 0, "25.0", "29.9", "A", true),
              band(1, "Healthy", 0, "18.5", "24.9", "G"),
              band(3, "Obese", 4, "30", null, "R"),
            ]),
            test("Blood pressure", "128", "mmHg", [
              band(1, "Normal", 1, "130", "", "G", true),
            ]),
            test("Systolic Blood pressure", "128", "mmHg", [
              band(1, "Normal", 1, "130", "", "G", true),
              band(2, "High", 4, "130", "", "R"),
            ]),
            test("Pulse", "62", "BPM", null),
          ]),
          panel("Heart Health", [
            test("LDL Cholesterol", "2.9", "mmol/l", [
              band(1, "Optimal", 2, "3.0", "", "G", true),
              band(2, "High", 3, "3.0", "", "R"),
            ]),
            test("HDL Cholesterol", "1.4", "mmol/l", [
              band(1, "Risk", 1, "1.0", "", "R"),
              band(2, "Optimal", 4, "1.0", "", "G", true),
            ]),
            test("Triglycerides", "1.9", "mmol/l", [
              band(1, "Optimal", 1, "1.7", "", "G"),
              band(2, "Raised", 4, "1.7", "", "A", true),
            ]),
            test("Total Cholesterol / HDL Cholesterol Ratio", "3.2", "-", [
              band(1, "Optimal", 1, "4.0", "", "G", true),
            ]),
          ]),
          panel("Metabolic Syndrome", [
            test("HDL Cholesterol", "1.4", "mmol/l", [
              band(1, "Risk", 1, "1.03", "", "R"),
              band(2, "Normal", 4, "1.03", "", "G", true),
            ]),
            test("Triglycerides", "1.9", "mmol/l", [
              band(1, "Optimal", 1, "1.7", "", "G"),
              band(2, "Risk", 3, "1.7", "", "R", true),
            ]),
          ]),
          panel("Diabetes Health", [
            test("Glucose", "5.1", "mmol/l", [
              band(1, "Normal", 0, "3.9", "5.5", "G", true),
              band(2, "Raised", 0, "5.6", "6.9", "A"),
              band(3, "High", 4, "7.0", "", "R"),
            ]),
            test("HbA1c", "35", "mmol/mol", null),
            test(
              "Microalbumin / Creatinine Ratio",
              "not detected",
              "mg/mmol creatinine",
              [
                band(1, "Normal", 0, "Not Detected", "", "G", true),
                band(2, "Raised", 4, "3.5", "", "R"),
              ]
            ),
          ]),
          panel("Urinalysis", [
            test("Protein (Urine)", "Negative", "mg/dl", [
              band(1, "Normal", 0, "Negative", "", "G", true),
              band(2, "Abnormal", 0, "Trace", "", "R"),
              band(3, "Abnormal", 4, "30", "", "R"),
            ]),
            test("pH (Urine)", "6.0", "pH", [
              band(1, "Normal", 0, "5.0", "8.0", "G", true),
            ]),
            test("Red Blood Cells (Urine)", "Negative", "RBC/µl", [
              band(1, "Normal", 0, "Negative", "", "G", true),
              band(2, "Abnormal", 4, "10", "", "R"),
            ]),
            test("Urobilinogen (Urine)", "Normal", "mg/dl", [
              band(1, "Normal", 0, "Normal", "", "G", true),
            ]),
          ]),
          panel("Liver Health", [
            test("Fibrosis-4 Score (FIB-4)", "0.62", "-", [
              band(1, "Low Risk", 1, "1.3", "", "G", true),
              band(2, "Indeterminate", 0, "1.3", "2.67", "A"),
              band(3, "High Risk", 4, "2.67", "", "R"),
            ]),
          ]),
          panel("Nutritional Health", [
            test("Vitamin D", "<20", "nmol/l", [
              band(1, "Deficiency", 1, "25", "", "R", true),
              band(2, "Insufficiency", 0, "25", "50", "A"),
              band(3, "Optimal", 0, "50", "200", "G"),
            ]),
            test("Folic acid", "9.1", "µg/l", null),
            test("Ferritin", "<20", "furlongs/L", [
              band(1, "Deficiency", 1, "25", "", "R", true),
            ]),
            test("Glorbin Index", "5", "U/l", [
              band(1, "Normal", 0, "1", "9", "G", true),
            ]),
            test("Vitamin B12", null, "ng/l", null),
          ]),
          panel("Pituitary & Adrenal Health", [
            test("Follicle Stimulating Hormone", "4.2", "U/l", [
              band(1, "Normal", 0, "1.5", "12.4", "G", true),
            ]),
            test(
              "Estimated Glomerular Filtration Rate (eGFR)",
              ">90",
              "ml/min/1.73m²",
              [band(1, "Normal", 4, "90", "", "G", true)],
              {
                secondaryReferenceRange: {
                  name: null,
                  unit: "ml/min/1.73m²",
                  type: 1,
                  ranges: [band(1, "Normal", 4, "60", "", "G", true)],
                },
              }
            ),
          ]),
        ]
      )
    ),
    order(5102, "RH9-00000102", null),
  ],
  "2026-02-11T10:00:00.1234567"
)

/** The summer visit: no last update of its own, no listing, a sample not
 * taken fasting. */
export const SUMMER_VISIT = visit(7, [
  order(
    4007,
    SUMMER_SAMPLE,
    report(
      {
        sampleCollectionDate: "2025-07-15T08:30:00",
        fastingDate: "2025-07-15T02:00:00",
        resultDate: null,
      },
      [
        panel("Personal Health Measurements", [
          test("Waist Circumference", "92", "cm", [
            band(1, "Healthy", 1, "94", "", "G", true),
          ]),
        ]),
        panel("Diabetes Health", [
          test("Glucose", "5.4", "mmol/l", [
            band(1, "Normal", 0, "3.9", "5.5", "G", true),
          ]),
        ]),
      ]
    )
  ),
])

export const LISTING = [
  { fileName: PRELIMINARY, published: true, isPreliminaryReport: true },
  { fileName: PHP, published: true, isPreliminaryReport: false },
  { fileName: BCA, published: true, isPreliminaryReport: false },
  { fileName: TRACK, published: true, isPreliminaryReport: false },
].map((r, i) => ({
  id: i + 1,
  visitId: 101,
  reportedDate: "2026-02-12T21:40:00",
  nexusOrderId: 5101,
  ...r,
}))

const entry = (
  path: string,
  resource: string,
  endpoint: string
): [string, Record<string, unknown>] => [
  path,
  {
    resource,
    id: path,
    endpoint,
    sha256: "0".repeat(64),
    bytes: 1,
    firstSeen: "2025-07-20T08:00:01Z",
    lastChanged: "2026-02-13T07:30:16Z",
  },
]

export const MANIFEST = {
  files: Object.fromEntries([
    entry(
      `files/report/${BCA}.pdf`,
      "report",
      `v2/Reports/GetReportFromAzureByFileName/${BCA}`
    ),
    entry(
      "items/visit-reports/101.json",
      "visit-reports",
      "v2/Reports/GetPublishedReportsByVisitId/101/false"
    ),
    entry("items/visit/101.json", "visit", VISITS_ENDPOINT),
    entry("items/visit/7.json", "visit", VISITS_ENDPOINT),
    entry("lists/visits.json", "list", "v2/Visit/GetAllVisitsByUserId"),
  ]),
  runs: [
    {
      started: "2025-07-20T08:00:00Z",
      finished: "2025-07-20T08:01:00Z",
      new: 5,
      changed: 0,
      unchanged: 0,
    },
    {
      started: "2026-02-13T07:30:15.123456789Z",
      finished: "2026-02-13T07:31:00Z",
      new: 4,
      changed: 1,
      unchanged: 3,
    },
  ],
}

/** The export's files by path, as randoxctl writes them. */
export function exportFiles(): Record<string, string | Uint8Array> {
  return {
    "manifest.json": json(MANIFEST),
    "lists/visits.json": json([{ id: 101 }, { id: 7 }]),
    "items/visit/101.json": json(WINTER_VISIT),
    "items/visit/7.json": json(SUMMER_VISIT),
    "items/visit-reports/101.json": json(LISTING),
    [`files/report/${PHP}.pdf`]: HEALTH_REPORT_PDF,
    [`files/report/${PRELIMINARY}.pdf`]:
      "%PDF-1.4\n% an invented preliminary report\n",
    [`files/report/${BCA}.pdf`]: sheetPdf(SHEET_RUNS),
    [`files/report/${TRACK}.pdf`]: "%PDF-1.4\n% an invented tracking report\n",
  }
}

export const FILE_NAMES = { PHP, PRELIMINARY, BCA, TRACK }
