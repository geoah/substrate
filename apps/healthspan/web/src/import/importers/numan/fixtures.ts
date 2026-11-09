// Invented Numan exports for the importer's tests, in the exact shape
// Numan's web app downloads (a JSON:API document of the owner's tests, each
// with the lab's own rows as a JSON string). Every name, id and value here
// is made up.

interface MeasurementSpec {
  code: string
  value: string
  normal: string
}

export interface TestSpec {
  id: string
  shortId: string
  lab?: string
  createdAt: string
  updatedAt?: string | null
  sampleTakenDate?: string | null
  sampleAccessionedDate?: string | null
  resultsReceivedAt?: string | null
  status?: string
  /** `results_raw`, before it is written as a string. */
  raw: unknown
  /** Each result's `collection_detail` and `collection_method`. */
  collection: readonly (readonly [string | null, string | null])[]
  measurements?: readonly MeasurementSpec[]
  reviewId?: string
}

export interface ReviewSpec {
  id: string
  testId: string
  review: string
  createdAt: string
  updatedAt: string
}

/** One test as Numan's export holds it. */
export function numanTest(t: TestSpec): Record<string, unknown> {
  return {
    id: t.id,
    type: "medical_test",
    attributes: {
      assessment: "normal",
      created_at: t.createdAt,
      customer_id: "cus-0000",
      lab: t.lab ?? "Randox",
      lab_note: null,
      legacy_id: "000",
      legacy_short_id: "OLD-000",
      observations: (t.measurements ?? []).map((m, i) => ({
        display_order: i,
        error_code: null,
        flag: null,
        identifier: m.code.toLowerCase(),
        identifier_short: m.code,
        normal_value: null,
        value: m.value,
      })),
      observations_measurements: (t.measurements ?? []).map((m, i) => ({
        display_order: i,
        error_code: null,
        flag: "normal",
        identifier: m.code.toLowerCase(),
        identifier_short: m.code,
        normal_value: m.normal,
        value: m.value,
      })),
      partially_updated: false,
      patient_date_of_birth: null,
      patient_id: 4242,
      patient_name: "Alex Example",
      product_name: "Invented Blood Test",
      receipt: {},
      receipt_raw: null,
      results: t.collection.map(([detail, method], i) => ({
        biomarker_code: `B${i}`,
        collection_detail: detail,
        collection_method: method,
        error_code: null,
        name: `Biomarker ${i}`,
        result: "1",
        unit: "x",
      })),
      results_raw: JSON.stringify(t.raw),
      results_received_at: t.resultsReceivedAt ?? null,
      sample_accessioned_date: t.sampleAccessionedDate ?? null,
      sample_taken_date: t.sampleTakenDate ?? null,
      short_id: t.shortId,
      source_id: "src-0000",
      source_type: "order",
      status: t.status ?? "completed",
      test_type: "blood",
      updated_at: t.updatedAt ?? null,
      variant: "standard",
    },
    relationships: {
      ...(t.reviewId
        ? {
            medical_test_review: {
              data: { id: t.reviewId, type: "medical_test_review" },
            },
          }
        : {}),
      patient: {
        data: {
          attributes: { patient_full_name: "Alex Example" },
          id: "pat-0000",
          type: "patient",
        },
      },
      primary_handler: {
        data: {
          attributes: {
            doctor_gmc_number: "0000000",
            first_name: "Sam",
            identity_number: "000",
            last_name: "Doctor",
          },
          id: 7,
          type: "user",
        },
      },
    },
  }
}

/** One clinical review as an export includes it. */
export function numanReview(r: ReviewSpec): Record<string, unknown> {
  return {
    attributes: {
      created_at: r.createdAt,
      medical_test_id: r.testId,
      review: r.review,
      reviewed_at: r.updatedAt,
      reviewer_id: 7,
      significant_unreadable_biomarkers: false,
      updated_at: r.updatedAt,
    },
    id: r.id,
    type: "medical_test_review",
  }
}

/** An export's JSON text: its tests, its reviews (as Numan writes them,
 * each in a list of its own), and its page. */
export function numanExport(
  tests: readonly TestSpec[],
  reviews: readonly ReviewSpec[] = [],
  page: { current: number; last: number; next: number | null } = {
    current: 1,
    last: 1,
    next: null,
  }
): string {
  return JSON.stringify({
    data: tests.map(numanTest),
    errors: [],
    meta: {
      filters: {},
      sort: { column: "created_at", order: "desc" },
      pagination: {
        items_per_page: 25,
        items_total: 99999,
        current_page: page.current,
        next_page: page.next,
        prev_page: null,
        last_page: page.last,
      },
      columns: [{ name: "updated_at", sortable: true, sorted: false }],
    },
    included: reviews.map((r) => [numanReview(r)]),
  })
}

/** A Randox row as Numan relays it. */
export function randoxRow(
  analyte: string,
  displayName: string,
  result: string,
  units: string,
  extra: Record<string, unknown> = {}
): Record<string, unknown> {
  return {
    analyte,
    caveat: null,
    dateOfReceipt: "2026-06-16T09:00:00",
    dateOfReport: null,
    displayName,
    group: "Panel",
    lowHigh: null,
    orderNumber: "ORD-0001",
    refHigh: "",
    refLow: "",
    result,
    sampleType: "Serum",
    units,
    ...extra,
  }
}

/** A Hurdle row. */
export function hurdleRow(
  code: string,
  name: string,
  unit: string,
  result: string,
  interpretation: string | null,
  rule: unknown,
  testId = "hd-test-0001"
): Record<string, unknown> {
  return {
    biomarker: { code, name, unit },
    interpretation,
    interpretationData: { value: rule },
    metadata: { source: "invented" },
    result,
    sample_taken_on: null,
    specimenId: "sp-0001",
    testId,
  }
}

const IN_CLINIC = ["in-clinic", "venous"] as const
const TASSO = ["tasso", "capillary"] as const
const FINGER = ["fingerprick", "capillary"] as const
const HOME = [null, null] as const

/** A Randox order taken in clinic in summer: lab ranges and flags, Numan's
 * clinical ranges (one in a unit no fixed factor moves, two moved by a
 * factor, one open-ended), a repeated analyte (once the same, once not),
 * a comparator, words, an analyte found by its name, one with no metric,
 * and the clinic's blood pressure. */
export const ORDER_TEST: TestSpec = {
  id: "tst-aaaa",
  shortId: "NMN001-00000001",
  createdAt: "2026-06-14T18:00:00.000Z",
  updatedAt: "2026-06-18T10:00:00.000Z",
  sampleTakenDate: "2026-06-15T08:30:00Z",
  sampleAccessionedDate: "2026-06-16T09:00:00Z",
  resultsReceivedAt: "2026-06-17T13:00:00.000Z",
  reviewId: "rev-aaaa",
  collection: [IN_CLINIC, IN_CLINIC],
  measurements: [
    { code: "LDL", value: "2.9 mmol/L", normal: "0.0 - 3.0" },
    { code: "HDL", value: "1.40 mmol/L", normal: "> 1.0" },
    { code: "CHO", value: "5.6 mmol/L", normal: "< 5.0" },
    { code: "FOLA", value: "14.0 nmol/L", normal: "8.8 - 60.8" },
    { code: "B12", value: "420 pg/mL", normal: "200 - 900" },
    { code: "GFR", value: "90 mL/min/1.73m²", normal: "90 - 99999" },
    { code: "TEST", value: "8.1 nmol/L", normal: "" },
    { code: "MCHC", value: "33.5 g/dL", normal: "32.0 - 36.0" },
    { code: "FSH", value: "4.2 U/L", normal: "1.5 - 12.4" },
  ],
  raw: {
    orderCreatedDate: "2026-06-14T19:45:10.12",
    orderId: 1001,
    orderNumber: "ORD-0001",
    patientDiastolicBloodPressure: 76,
    patientEthnicity: "Not stated",
    patientHeight: 180,
    patientSystolicBloodPressure: 118,
    patientWeight: 80,
    reportResults: [
      randoxRow("LDLCholesterol", "LDL Cholesterol", "2.9", "MMOL_L", {
        refLow: "0.0",
        refHigh: "3.0",
        dateOfReport: "2026-06-17T14:05:00.123",
        group: "Lipids",
      }),
      randoxRow("HDLCholesterol", "HDL Cholesterol", "1.40", "MMOL_L", {
        refLow: "1.0",
        group: "Lipids",
      }),
      randoxRow("HDLCholesterol", "HDL Cholesterol", "1.40", "MMOL_L", {
        refLow: "1.0",
        group: "Heart",
      }),
      randoxRow("TotalCholesterol", "Total Cholesterol", "5.6", "MMOL_L", {
        refHigh: "5.0",
        lowHigh: "H",
      }),
      randoxRow("TotalCholesterol", "Total Cholesterol", "5.7", "MMOL_L", {
        refHigh: "5.0",
        lowHigh: "H",
        group: "Heart",
      }),
      randoxRow("Testosterone", "Testosterone", "8.1", "NMOL-L", {
        refLow: "8.6",
        refHigh: "29.0",
        lowHigh: "L",
      }),
      randoxRow("25-OHVitaminD", "Vitamin D", "<10", "NMOL-L", {
        refLow: "50",
        refHigh: "175",
        lowHigh: "L",
      }),
      randoxRow("FolicAcid", "Folate", "6.2", "UG_L", {
        refLow: "3.9",
        refHigh: "26.8",
      }),
      randoxRow("VitaminB12", "Vitamin B12", "420", "NG_L", {
        refLow: "197",
        refHigh: "771",
      }),
      randoxRow("FollicleStimulatingHormone", "FSH", "4.2", "IU_L", {
        refLow: "1.5",
        refHigh: "12.4",
      }),
      randoxRow("FreeAndrogenIndex", "Free Androgen Index", "45.0", "RATIO", {
        refLow: "35.0",
        refHigh: "92.6",
      }),
      randoxRow("eGFR", "eGFR", ">90", "ml/min/1.73m2", { refLow: "90" }),
      randoxRow("MeanCellHaemConc", "MCHC", "335", "G_L", {
        refLow: "320",
        refHigh: "360",
        sampleType: "Whole Blood",
      }),
      randoxRow("HGB", "Haemoglobin", "Haemolysed", "G_L", {
        refLow: "130",
        refHigh: "170",
        sampleType: "Whole Blood",
      }),
      randoxRow("Zinc", "Zinc", "12.0", "UMOL-L"),
      randoxRow("SerumIron", "Iron", "18", "UMOL-L", {
        refLow: "10",
        refHigh: "30",
      }),
    ],
    resultsUploadDate: "2026-06-17T14:00:00",
    sampleCollectionDate: "2026-06-15T08:30:00",
  },
}

/** A Hurdle home kit with no sample time: the lab's rules as bands, a unit
 * the repository does not know, Numan's ranges moved or left out, an open
 * end, a biomarker with no metric. */
export const HURDLE_TEST: TestSpec = {
  id: "tst-bbbb",
  shortId: "NMN001-00000002",
  lab: "Hurdle",
  createdAt: "2026-02-01T10:00:00.000Z",
  updatedAt: "2026-02-05T12:00:00.000Z",
  sampleTakenDate: null,
  sampleAccessionedDate: "2026-02-03T09:15:00.000Z",
  resultsReceivedAt: "2026-02-04T16:20:00.000Z",
  reviewId: "rev-bbbb",
  collection: [HOME, HOME],
  measurements: [
    { code: "TSH", value: "2.10 mU/L", normal: "0.27 - 4.20" },
    { code: "LDL", value: "3.4 mmol/L", normal: "0.0 - 3.0" },
    { code: "B12", value: "150 ng/L", normal: "180 - 900" },
    { code: "FOLA", value: "11.6 nmol/L", normal: "8.8 - 60.8" },
    { code: "GFR", value: "90 mL/min/1.73m²", normal: "90 - 99999" },
  ],
  raw: [
    hurdleRow(
      "TSH",
      "Thyroid Stimulating Hormone",
      "µIU/mL",
      "2.10",
      "NORMAL",
      {
        LOW: ["<0.27"],
        NORMAL: [">=0.27", "<=4.2"],
        HIGH: [">4.2"],
      }
    ),
    hurdleRow("LDL", "LDL Cholesterol", "mmol/L", "3.4", "HIGH", {
      NORMAL: ["<3.0"],
      HIGH: [">=3.0"],
    }),
    hurdleRow("B12", "Vitamin B12", "pg/mL", "150", "DEFICIENT", {
      DEFICIENT: ["<180"],
      NORMAL: [">=180"],
    }),
    hurdleRow("FOL", "Folate", "ng/mL", "5.1", "NORMAL", { NORMAL: [">=3.9"] }),
    hurdleRow("EGFR", "eGFR", "mL/min/1.73m^2", ">90", "NORMAL", {
      NORMAL: [">=90"],
    }),
    hurdleRow("SE", "Selenium", "µmol/L", "1.1", null, null),
  ],
}

const ferritin = (result: string, dateOfReport: string | null) => [
  randoxRow("Ferritin", "Ferritin", result, "UG_L", {
    refLow: "30",
    refHigh: "400",
    dateOfReport,
  }),
]

/** A Randox list (a Tasso+ device) that a later export amends. */
export const AMENDED_BEFORE: TestSpec = {
  id: "tst-cccc",
  shortId: "NMN001-00000003",
  createdAt: "2026-06-25T08:00:00.000Z",
  updatedAt: "2026-07-01T09:00:00.000Z",
  sampleTakenDate: "2026-06-27T07:10:00Z",
  sampleAccessionedDate: "2026-06-28T10:00:00Z",
  resultsReceivedAt: "2026-06-29T10:30:00.000Z",
  collection: [TASSO],
  measurements: [{ code: "FERR", value: "80 µg/L", normal: "30 - 400" }],
  raw: ferritin("80", "2026-06-29T11:00:00.5"),
}
export const AMENDED_AFTER: TestSpec = {
  ...AMENDED_BEFORE,
  updatedAt: "2026-08-02T09:00:00.000Z",
  raw: ferritin("85", "2026-06-29T11:00:00.5"),
}

/** A Hurdle kit taken by finger prick in winter, which a later export no
 * longer holds. */
export const DROPPED_TEST: TestSpec = {
  id: "tst-dddd",
  shortId: "NMN001-00000004",
  lab: "Hurdle",
  createdAt: "2026-01-05T08:00:00.000Z",
  updatedAt: "2026-01-12T08:00:00.000Z",
  sampleTakenDate: "2026-01-10T07:45:00Z",
  sampleAccessionedDate: "2026-01-11T09:00:00.000Z",
  resultsReceivedAt: "2026-01-11T15:00:00.000Z",
  collection: [FINGER],
  raw: [
    hurdleRow(
      "ALT",
      "ALT",
      "IU/L",
      "22",
      "NORMAL",
      { NORMAL: ["<=40"], HIGH: [">40"] },
      "hd-test-0004"
    ),
  ],
}

/** A Randox list whose version is the same in both exports, the later
 * holding a later review. */
export const REVIEWED_TEST: TestSpec = {
  id: "tst-eeee",
  shortId: "NMN001-00000005",
  createdAt: "2026-05-01T08:00:00.000Z",
  updatedAt: "2026-05-06T08:00:00.000Z",
  sampleTakenDate: "2026-05-02T09:00:00Z",
  sampleAccessionedDate: "2026-05-03T09:00:00Z",
  resultsReceivedAt: "2026-05-04T09:00:00.000Z",
  reviewId: "rev-eeee",
  collection: [TASSO],
  raw: [
    randoxRow("Albumin", "Albumin", "45", "G_L", {
      refLow: "35",
      refHigh: "50",
      dateOfReport: "2026-05-04T10:00:00.250",
    }),
  ],
}
export const REVIEW_BEFORE: ReviewSpec = {
  id: "rev-eeee",
  testId: "tst-eeee",
  review: "<p>Looks fine.</p>",
  createdAt: "2026-05-06T08:00:00.000Z",
  updatedAt: "2026-05-06T08:00:00.000Z",
}
export const REVIEW_AFTER: ReviewSpec = {
  ...REVIEW_BEFORE,
  review: "<p>Looks fine; the reviewer's number corrected.</p>",
  updatedAt: "2026-09-20T08:00:00.000Z",
}

/** A Randox list that names no date of report, taken two ways. */
export const LIST_TEST: TestSpec = {
  id: "tst-ffff",
  shortId: "NMN001-00000006",
  createdAt: "2026-09-01T08:00:00.000Z",
  updatedAt: null,
  sampleTakenDate: "2026-09-02T08:00:00Z",
  sampleAccessionedDate: "2026-09-03T08:00:00Z",
  resultsReceivedAt: "2026-09-04T08:00:00.000Z",
  collection: [HOME, TASSO],
  measurements: [{ code: "B_HBA1C", value: "36 mmol/mol", normal: "< 42" }],
  raw: [
    randoxRow("Hb A1C", "HbA1c", "36", "MMOL_MOL", {
      refHigh: "42",
      sampleType: "Blood",
    }),
    randoxRow(
      "Cholesterol / HDL Ratio",
      "Cholesterol / HDL Ratio",
      "3.2",
      "RATIO"
    ),
  ],
}

/** A test that names no time its sample was taken or received. */
export const TIMELESS_TEST: TestSpec = {
  id: "tst-gggg",
  shortId: "NMN001-00000007",
  createdAt: "2026-09-10T08:00:00.000Z",
  updatedAt: "2026-09-12T08:00:00.000Z",
  collection: [TASSO],
  raw: [randoxRow("Albumin", "Albumin", "44", "G_L")],
}

const REVIEWS = [
  {
    id: "rev-aaaa",
    testId: "tst-aaaa",
    review: "<p>Speak to your GP about cholesterol.</p>",
    createdAt: "2026-06-18T10:00:00.000Z",
    updatedAt: "2026-06-18T10:00:00.000Z",
  },
  {
    id: "rev-bbbb",
    testId: "tst-bbbb",
    review: "<p>Your B12 is low.</p>",
    createdAt: "2026-02-05T12:00:00.000Z",
    updatedAt: "2026-02-05T12:00:00.000Z",
  },
]

/** The older of two exports. */
export const OLDER = numanExport(
  [ORDER_TEST, HURDLE_TEST, AMENDED_BEFORE, DROPPED_TEST, REVIEWED_TEST],
  [...REVIEWS, REVIEW_BEFORE]
)

/** The newer: it amends one test, re-reviews another, drops one and adds
 * two. */
export const NEWER = numanExport(
  [
    ORDER_TEST,
    HURDLE_TEST,
    AMENDED_AFTER,
    REVIEWED_TEST,
    LIST_TEST,
    TIMELESS_TEST,
  ],
  [...REVIEWS, REVIEW_AFTER]
)

/** The two exports as the owner hands them in. */
export const TWO_EXPORTS = {
  "alex-numan-20260701.json": OLDER,
  "alex-numan-20261001.json": NEWER,
}
