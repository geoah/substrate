// The synthetic member's labs: nine panels over three years from the two
// invented labs, three DXA scans, the metric and analyte records a
// repository holds once `labs` 2 and `health` 3 are installed, and the unit
// graph they convert through. Nothing here is anyone's data. The rows are
// shaped as substrate serves them and go through the live transforms.
//
// What it shows on Labs: LDL and vitamin D out of the lab's range,
// triglycerides off target only (inside the lab's range, in the
// "Borderline High" band), PSA overdue, hs-CRP starting in the last year
// (the empty span in a five-year view), HDL and total cholesterol moving
// the better way and ALT the worse way, DXA regions, and every unit path:
// the lipids of the full panels with their mg/dL twins, HbA1c with its
// percent twins, vitamin D written in ng/mL with nmol/L twins (the newest
// clinic one before its twin is written), vitamin B12 only in pg/mL,
// apolipoprotein B under two metrics of one form, and PSA on a metric
// record that carries no codes.

import type { SubstrateRecord } from "@substrate/client"
import { CATALOGUE, LOINC_SYSTEM } from "../catalogue"
import { KIND } from "../kinds"
import { toLabIndex, type LabRows } from "../labs/labIndex"
import type { LabIndex } from "../labs/types"
import type { GraphUnit, UnitGraph } from "../labs/units"
import { DEMO_SOURCES, demoSourceRecords, q, rec, ref, rng } from "./shared"

/** The units the demo writes in, each with its map to its component's
 * root, as `GET /api/v1/units` answers them. */
const UNITS: [id: string, root: string, factor: number][] = [
  ["mol/L", "mol/L", 1],
  ["mmol/L", "mol/L", 1e-3],
  ["umol/L", "mol/L", 1e-6],
  ["nmol/L", "mol/L", 1e-9],
  ["pmol/L", "mol/L", 1e-12],
  ["g/L", "g/L", 1],
  ["g/dL", "g/L", 10],
  ["mg/dL", "g/L", 1e-2],
  ["mg/L", "g/L", 1e-3],
  ["ug/dL", "g/L", 1e-5],
  ["ug/L", "g/L", 1e-6],
  ["ng/mL", "g/L", 1e-6],
  ["ng/dL", "g/L", 1e-8],
  ["ng/L", "g/L", 1e-9],
  ["pg/mL", "g/L", 1e-9],
  ["1", "1", 1],
  ["percent", "1", 1e-2],
  ["mmol/mol", "1", 1e-3],
  ["1e9/L", "1/L", 1e9],
  ["1e12/L", "1/L", 1e12],
  ["g", "g", 1],
  ["kg", "g", 1e3],
  ["m", "m", 1],
  ["cm", "m", 1e-2],
  ["U/L", "U/L", 1],
  ["IU/L", "IU/L", 1],
  ["mIU/L", "IU/L", 1e-3],
  ["IU/mL", "IU/L", 1e3],
  ["fL", "fL", 1],
  ["pg", "pg", 1],
  ["mL/min/1.73m2", "mL/min/1.73m2", 1],
  ["kg/m2", "kg/m2", 1],
  ["mmHg", "mmHg", 1],
  ["mm/h", "mm/h", 1],
]

export function demoUnitGraph(): UnitGraph {
  return {
    units: new Map<string, GraphUnit>(
      UNITS.map(([id, root, factor]) => [id, { id, root, factor, offset: 0 }])
    ),
  }
}

const M = KIND.metric
const loinc = (code: string) => [{ system: LOINC_SYSTEM, code }]

/** The shared metrics the demo's entries name, as the catalogue installs
 * them; the demo's PSA record carries no codes, so it resolves by its id. */
function sharedMetrics() {
  return CATALOGUE.flatMap((e) =>
    e.metric
      ? [
          rec(M, e.metric, {
            name: e.name,
            unit: ref(KIND.unit, e.displayUnit),
            codes: e.code === "psa" || !e.loinc[0] ? [] : loinc(e.loinc[0]),
            displayPrecision: e.precision,
            category: e.metric.startsWith("health/") ? "body" : "labs",
          }),
        ]
      : []
  )
}

/** The other forms and the body metrics no entry names. */
const EXTRA_METRICS: [
  id: string,
  name: string,
  unit: string,
  code: string | undefined,
  precision: number,
  category: string,
][] = [
  [
    "labs/ldl_cholesterol_mass",
    "LDL cholesterol (mass)",
    "mg/dL",
    "2089-1",
    0,
    "labs",
  ],
  [
    "labs/hdl_cholesterol_mass",
    "HDL cholesterol (mass)",
    "mg/dL",
    "2085-9",
    0,
    "labs",
  ],
  [
    "labs/total_cholesterol_mass",
    "Total cholesterol (mass)",
    "mg/dL",
    "2093-3",
    0,
    "labs",
  ],
  [
    "labs/triglycerides_mass",
    "Triglycerides (mass)",
    "mg/dL",
    "2571-8",
    0,
    "labs",
  ],
  ["labs/vitamin_d_mass", "Vitamin D (mass)", "ng/mL", "62292-8", 0, "labs"],
  ["labs/vitamin_b12_mass", "Vitamin B12 (mass)", "pg/mL", "2132-9", 0, "labs"],
  ["labs/hba1c", "HbA1c", "percent", "4548-4", 1, "labs"],
  // An earlier import's own metric: the same analyte in the display form.
  [
    "demo/apolipoprotein_b",
    "Apolipoprotein B (mg/dL)",
    "mg/dL",
    "1884-6",
    0,
    "labs",
  ],
  ["health/body_fat_fraction", "Body fat", "percent", "41982-0", 1, "body"],
  ["health/lean_mass", "Lean mass", "kg", undefined, 1, "body"],
  ["health/fat_free_mass", "Fat-free mass", "kg", undefined, 1, "body"],
]

/** The analyte pairs the demo converts through. */
const PAIRS: [molar: string, mass: string, molarMass: number][] = [
  ["labs/ldl_cholesterol", "labs/ldl_cholesterol_mass", 386.65],
  ["labs/hdl_cholesterol", "labs/hdl_cholesterol_mass", 386.65],
  ["labs/total_cholesterol", "labs/total_cholesterol_mass", 386.65],
  ["labs/triglycerides", "labs/triglycerides_mass", 885.7],
  ["labs/vitamin_d", "labs/vitamin_d_mass", 400.64],
  ["labs/vitamin_b12", "labs/vitamin_b12_mass", 1355.37],
]

function analyteRecords() {
  const conv = (target: string, formula: string, parameters?: object) => ({
    target: ref(M, target),
    formula,
    ...(parameters ? { parameters } : {}),
    version: "1",
  })
  const out = PAIRS.flatMap(([molar, mass, mm]) => [
    rec(KIND.analyte, molar, {
      metric: ref(M, molar),
      molarMass: String(mm),
      conversions: [conv(mass, "molar-mass")],
    }),
    rec(KIND.analyte, mass, {
      metric: ref(M, mass),
      molarMass: String(mm),
      conversions: [conv(molar, "molar-mass")],
    }),
  ])
  const hba1c = { slope: "10.929", intercept: "2.15" }
  out.push(
    rec(KIND.analyte, "labs/hba1c", {
      metric: ref(M, "labs/hba1c"),
      conversions: [conv("labs/hba1c_ifcc", "ngsp-to-ifcc", hba1c)],
    }),
    rec(KIND.analyte, "labs/hba1c_ifcc", {
      metric: ref(M, "labs/hba1c_ifcc"),
      conversions: [conv("labs/hba1c", "ifcc-to-ngsp", hba1c)],
    })
  )
  return out
}

interface Def {
  /** The metric written, its unit and the decimals written. */
  metric: string
  unit: string
  precision: number
  /** Typical value, spread between panels, the lab's normal range. */
  mean: number
  sd: number
  low?: number
  high?: number
  /** Tested only at the yearly full panel. */
  yearly?: boolean
  /** The first panel (0 to 8) that tests it. */
  from?: number
  /** A steady change per panel, ending at `mean` on the newest panel. */
  drift?: number
  /** The clinic's own normal range, where it differs from the lab's. */
  clinic?: { low?: number; high?: number }
}

/** Plausible adult values, by entry code. */
const DEFS: Record<string, Def> = {
  creatinine: {
    metric: "labs/creatinine",
    unit: "umol/L",
    precision: 0,
    mean: 82,
    sd: 5,
    low: 59,
    high: 104,
  },
  egfr: {
    metric: "labs/egfr",
    unit: "mL/min/1.73m2",
    precision: 0,
    mean: 96,
    sd: 4,
    low: 60,
  },
  cystatin_c: {
    metric: "labs/cystatin_c",
    unit: "mg/L",
    precision: 2,
    mean: 0.82,
    sd: 0.05,
    low: 0.57,
    high: 1.05,
    yearly: true,
  },
  total_cholesterol: {
    metric: "labs/total_cholesterol",
    unit: "mmol/L",
    precision: 2,
    mean: 4.5,
    sd: 0.12,
    high: 5,
    drift: -0.15,
  },
  triglycerides: {
    metric: "labs/triglycerides",
    unit: "mmol/L",
    precision: 2,
    mean: 1.1,
    sd: 0.2,
    high: 2.3,
  },
  ldl_cholesterol: {
    metric: "labs/ldl_cholesterol",
    unit: "mmol/L",
    precision: 2,
    mean: 2.9,
    sd: 0.25,
    high: 3,
    clinic: { high: 3.4 },
  },
  hdl_cholesterol: {
    metric: "labs/hdl_cholesterol",
    unit: "mmol/L",
    precision: 2,
    mean: 1.45,
    sd: 0.05,
    low: 1,
    drift: 0.04,
  },
  non_hdl_cholesterol: {
    metric: "labs/non_hdl_cholesterol",
    unit: "mmol/L",
    precision: 2,
    mean: 3.4,
    sd: 0.25,
    high: 4,
  },
  apolipoprotein_b: {
    metric: "demo/apolipoprotein_b",
    unit: "mg/dL",
    precision: 0,
    mean: 92,
    sd: 6,
    high: 130,
    yearly: true,
  },
  lipoprotein_a: {
    metric: "labs/lipoprotein_a",
    unit: "nmol/L",
    precision: 0,
    mean: 40,
    sd: 3,
    high: 75,
    yearly: true,
  },
  tsh: {
    metric: "labs/tsh",
    unit: "mIU/L",
    precision: 2,
    mean: 1.9,
    sd: 0.4,
    low: 0.27,
    high: 4.2,
    clinic: { low: 0.4, high: 4 },
  },
  testosterone: {
    metric: "labs/testosterone",
    unit: "nmol/L",
    precision: 1,
    mean: 18,
    sd: 2.5,
    low: 8.6,
    high: 29,
    clinic: { low: 7.6, high: 31.4 },
  },
  alt: {
    metric: "labs/alt",
    unit: "U/L",
    precision: 0,
    mean: 30,
    sd: 2,
    high: 50,
    drift: 1.5,
  },
  ast: {
    metric: "labs/ast",
    unit: "U/L",
    precision: 0,
    mean: 24,
    sd: 4,
    high: 40,
    yearly: true,
  },
  vitamin_d: {
    metric: "labs/vitamin_d",
    unit: "nmol/L",
    precision: 0,
    mean: 70,
    sd: 15,
    low: 50,
    high: 200,
  },
  vitamin_b12: {
    metric: "labs/vitamin_b12_mass",
    unit: "pg/mL",
    precision: 0,
    mean: 420,
    sd: 50,
    low: 197,
    high: 771,
  },
  ferritin: {
    metric: "labs/ferritin",
    unit: "ug/L",
    precision: 0,
    mean: 130,
    sd: 25,
    low: 30,
    high: 400,
  },
  haemoglobin: {
    metric: "labs/haemoglobin",
    unit: "g/dL",
    precision: 1,
    mean: 15.1,
    sd: 0.4,
    low: 13,
    high: 17,
  },
  hba1c: {
    metric: "labs/hba1c_ifcc",
    unit: "mmol/mol",
    precision: 0,
    mean: 35,
    sd: 1.5,
    high: 42,
  },
  fasting_glucose: {
    metric: "labs/fasting_glucose",
    unit: "mmol/L",
    precision: 1,
    mean: 4.9,
    sd: 0.25,
    low: 3.9,
    high: 5.6,
    yearly: true,
  },
  insulin: {
    metric: "labs/insulin",
    unit: "pmol/L",
    precision: 1,
    mean: 48,
    sd: 10,
    high: 140,
    yearly: true,
  },
  hs_crp: {
    metric: "labs/hs_crp",
    unit: "mg/L",
    precision: 2,
    mean: 0.7,
    sd: 0.25,
    high: 5,
    from: 7,
  },
  psa: {
    metric: "labs/psa",
    unit: "ug/L",
    precision: 2,
    mean: 0.8,
    sd: 0.1,
    high: 2.5,
  },
  uric_acid: {
    metric: "labs/uric_acid",
    unit: "umol/L",
    precision: 0,
    mean: 330,
    sd: 25,
    low: 200,
    high: 430,
    yearly: true,
  },
  wbc: {
    metric: "labs/wbc",
    unit: "1e9/L",
    precision: 2,
    mean: 5.8,
    sd: 0.7,
    low: 4,
    high: 11,
  },
  platelets: {
    metric: "labs/platelets",
    unit: "1e9/L",
    precision: 0,
    mean: 240,
    sd: 25,
    low: 150,
    high: 400,
  },
  rbc: {
    metric: "labs/rbc",
    unit: "1e12/L",
    precision: 2,
    mean: 4.95,
    sd: 0.15,
    low: 4.4,
    high: 5.8,
  },
  haematocrit: {
    metric: "labs/haematocrit",
    unit: "percent",
    precision: 1,
    mean: 44,
    sd: 1.2,
    low: 40,
    high: 52,
  },
  mcv: {
    metric: "labs/mcv",
    unit: "fL",
    precision: 1,
    mean: 89,
    sd: 1.5,
    low: 80,
    high: 100,
  },
}

/** The lipids the full panel's lab writes twins of, in mg/dL. */
const TWINNED: Record<string, [mass: string, molarMass: number]> = {
  total_cholesterol: ["labs/total_cholesterol_mass", 386.65],
  hdl_cholesterol: ["labs/hdl_cholesterol_mass", 386.65],
  ldl_cholesterol: ["labs/ldl_cholesterol_mass", 386.65],
  triglycerides: ["labs/triglycerides_mass", 885.7],
}

const DAY = 86_400_000
const LAB = DEMO_SOURCES.lab
const CLINIC = DEMO_SOURCES.clinic
const DXA = DEMO_SOURCES.dxa

/** The demo member's lab rows: nine panels over three years ending a month
 * before `today`, then three yearly DXA scans, the last two weeks before
 * it. */
export function demoLabRows(today = new Date(), seed = 11): LabRows {
  const r = rng(seed)
  const gauss = () => (r() + r() + r() + r() - 2) / 0.8165
  const metrics = [
    ...sharedMetrics(),
    ...EXTRA_METRICS.map(([id, name, unit, code, precision, category]) =>
      rec(M, id, {
        name,
        unit: ref(KIND.unit, unit),
        codes: code ? loinc(code) : [],
        displayPrecision: precision,
        category,
      })
    ),
  ]
  const results: SubstrateRecord[] = []
  const reports: SubstrateRecord[] = []
  const end = today.getTime() - 30 * DAY
  const result = (id: string, props: Record<string, unknown>) =>
    results.push(rec(KIND.labresult, id, props))

  for (let i = 0; i < 9; i++) {
    const at = new Date(end - (8 - i) * 121 * DAY)
    at.setUTCHours(8, 30, 0, 0)
    const full = i % 3 === 2
    const source = full ? LAB : CLINIC
    const reportId = `demo/${at.toISOString().slice(0, 10)}`
    for (const [code, d] of Object.entries(DEFS)) {
      if (d.yearly && !full) continue
      if (d.from !== undefined && i < d.from) continue
      // A test done twice, long ago, to show the overdue rule.
      if (code === "psa" && i > 1) continue
      let v = d.mean + gauss() * d.sd + (d.drift ?? 0) * (i - 8)
      // Two flagged results on the newest panel: LDL just above its range,
      // vitamin D low after a winter; triglycerides off target only.
      if (i === 8 && code === "ldl_cholesterol") v = 3.42
      if (i === 8 && code === "vitamin_d") v = 41
      if (i === 8 && code === "triglycerides") v = 1.9
      v = Math.max(0, v)
      const { low, high } = !full && d.clinic ? d.clinic : d
      const outHigh = high !== undefined && v > high
      const outLow = low !== undefined && v < low
      let metric = d.metric
      let unit = d.unit
      let precision = d.precision
      let range = { low, high }
      let value = v
      // The clinic reports vitamin D in ng/mL; the newest apolipoprotein B
      // comes in the shared metric's g/L.
      const toMass = (x: number | undefined) =>
        x === undefined ? undefined : (x * 400.64) / 1000
      if (code === "vitamin_d" && !full) {
        metric = "labs/vitamin_d_mass"
        unit = "ng/mL"
        precision = 1
        value = toMass(v) as number
        range = { low: toMass(low), high: toMass(high) }
      }
      if (code === "apolipoprotein_b" && i === 8) {
        metric = "labs/apolipoprotein_b"
        unit = "g/L"
        precision = 2
        value = v / 100
        range = { low: undefined, high: (high as number) / 100 }
      }
      const props: Record<string, unknown> = {
        at: at.toISOString(),
        metric: ref(M, metric),
        source: ref(KIND.source, source.id),
        report: ref(KIND.labreport, reportId),
        value: q(value, unit, precision + 1),
        status: "final",
        referenceRanges:
          range.low !== undefined || range.high !== undefined
            ? [
                {
                  ...(range.low !== undefined
                    ? { low: q(range.low, unit) }
                    : {}),
                  ...(range.high !== undefined
                    ? { high: q(range.high, unit) }
                    : {}),
                  text: "normal",
                },
              ]
            : [],
        interpretation: [
          {
            code: outHigh ? "H" : outLow ? "L" : "N",
            display: outHigh ? "High" : outLow ? "Low" : "Normal",
          },
        ],
        specimen: { material: "blood", fasting: full },
      }
      const id = `${reportId}/${code}`
      result(id, props)
      const twin = (
        twinMetric: string,
        twinValue: number,
        twinUnit: string,
        formula = "molar-mass"
      ) =>
        result(`${id}/twin`, {
          ...props,
          metric: ref(M, twinMetric),
          value: q(twinValue, twinUnit, 2),
          referenceRanges: [],
          interpretation: [],
          derivedFrom: [ref(KIND.labresult, id)],
          conversion: { formula, version: "1" },
        })
      const pair = TWINNED[code]
      if (full && pair) twin(pair[0], (v * pair[1]) / 10, "mg/dL")
      if (code === "hba1c")
        twin("labs/hba1c", v / 10.929 + 2.15, "percent", "ifcc-to-ngsp")
      // The newest clinic vitamin D's twin is not written yet.
      if (code === "vitamin_d" && !full && i !== 7)
        twin("labs/vitamin_d", v, "nmol/L")
    }
    reports.push(
      rec(KIND.labreport, reportId, {
        at: at.toISOString(),
        issuedAt: new Date(at.getTime() + 2 * DAY).toISOString(),
        lab: source.name,
        accession: `${full ? "DL" : "DC"}${at.toISOString().slice(2, 10).replace(/-/g, "")}${i}`,
        status: "final",
        source: ref(KIND.source, source.id),
      })
    )
  }

  // Three DXA scans, a year apart: whole-body composition and three
  // regions of body fat.
  for (let k = 0; k < 3; k++) {
    const at = new Date(today.getTime() - (14 + (2 - k) * 365) * DAY)
    at.setUTCHours(10, 0, 0, 0)
    const reportId = `demo-dxa/${at.toISOString().slice(0, 10)}`
    const scan = (
      metric: string,
      value: number,
      unit: string,
      channel?: string
    ) =>
      result(
        `${reportId}/${metric.split("/")[1]}${channel ? `-${channel}` : ""}`,
        {
          at: at.toISOString(),
          metric: ref(M, metric),
          source: ref(KIND.source, DXA.id),
          report: ref(KIND.labreport, reportId),
          value: q(value, unit, 2),
          status: "final",
          referenceRanges: [],
          ...(channel ? { channel } : {}),
        }
      )
    const fat = 22.4 - k * 1.4
    scan("health/body_mass", 80.1 - k * 0.85, "kg")
    scan("health/body_fat_fraction", fat, "percent")
    scan("health/body_fat_fraction", fat - 2.4, "percent", "arms")
    scan("health/body_fat_fraction", fat - 3.7, "percent", "legs")
    scan("health/body_fat_fraction", fat + 3.2, "percent", "trunk")
    scan("health/lean_mass", 58600 + k * 600, "g")
    scan("health/fat_free_mass", 61800 + k * 600, "g")
    reports.push(
      rec(KIND.labreport, reportId, {
        at: at.toISOString(),
        lab: DXA.name,
        accession: `DX${at.toISOString().slice(2, 10).replace(/-/g, "")}`,
        status: "final",
        source: ref(KIND.source, DXA.id),
      })
    )
  }

  return {
    results,
    reports,
    metrics,
    analytes: analyteRecords(),
    sources: demoSourceRecords(),
  }
}

const cache = new Map<string, LabIndex>()

/** The demo member's lab index for `today`, built once a day. */
export function demoLabIndex(today: Date): LabIndex {
  const key = today.toDateString()
  let index = cache.get(key)
  if (!index) {
    index = toLabIndex(demoLabRows(today), demoUnitGraph())
    cache.set(key, index)
  }
  return index
}
