// A unit as a provider prints it, made the UCUM code the repository's unit
// graph knows it by (apps/healthspan/docs/importers/00-shared.md, Units).
// The tables are data: a spelling a provider prints that is not here is
// kept as received until a row is added for it.

import { unitId, type UnitGraph } from "../../../labs/units"

/** A provider's spelling of a unit, as printed, to its UCUM code. */
export const UNIT_SPELLINGS: Readonly<Record<string, string>> = {
  G_L: "g/L",
  "g/l": "g/L",
  "g/L": "g/L",
  "g/dL": "g/dL",
  "10E9-L": "10*9/L",
  "10⁹/L": "10*9/L",
  "x10^9/L": "10*9/L",
  "x 10^9/L": "10*9/L",
  "10E12-L": "10*12/L",
  "10¹²/L": "10*12/L",
  "X10^12/L": "10*12/L",
  "x10^12/L": "10*12/L",
  RATIO: "1",
  ratio: "1",
  Ratio: "1",
  "-": "1",
  "1": "1",
  "L/L": "1",
  ".": "1",
  PMOLL: "pmol/L",
  "pmol/l": "pmol/L",
  "pmol/L": "pmol/L",
  IU_L: "[IU]/L",
  "NMOL-L": "nmol/L",
  "nmol/l": "nmol/L",
  "nmol/L": "nmol/L",
  MMOL_MOL: "mmol/mol",
  "mmol/mol": "mmol/mol",
  PERCENT: "%",
  "%": "%",
  MMOL_L: "mmol/L",
  "mmol/l": "mmol/L",
  "mmol/L": "mmol/L",
  FL: "fL",
  fl: "fL",
  fL: "fL",
  PG: "pg",
  pg: "pg",
  MIU_L: "m[IU]/L",
  "mIU/l": "m[IU]/L",
  "mIU/L": "m[IU]/L",
  "mU/L": "m[IU]/L",
  UG_L: "ug/L",
  "µg/l": "ug/L",
  "µg/L": "ug/L",
  "ug/L": "ug/L",
  U_L: "U/L",
  "U/l": "U/L",
  "U/L": "U/L",
  "IU/L": "U/L",
  "UMOL-L": "umol/L",
  "µmol/l": "umol/L",
  "µmol/L": "umol/L",
  "umol/L": "umol/L",
  "ml/min/1.73m2": "mL/min/{1.73_m2}",
  "ml/min/1.73m²": "mL/min/{1.73_m2}",
  "mL/min/1.73m^2": "mL/min/{1.73_m2}",
  "mL/min/1.73m²": "mL/min/{1.73_m2}",
  NG_L: "ng/L",
  "ng/l": "ng/L",
  "ng/L": "ng/L",
  "µIU/mL": "u[IU]/mL",
  "ng/mL": "ng/mL",
  "ng/ml": "ng/mL",
  "pg/mL": "pg/mL",
  "mg/dl": "mg/dL",
  "mg/l": "mg/L",
  "mg/L": "mg/L",
  "µg/ml": "ug/mL",
  "kU/l": "k[IU]/L",
  "IU/ml": "[IU]/mL",
  "U/ml": "[arb'U]/mL",
  BPM: "/min",
  "beats/min": "/min",
  mmHg: "mm[Hg]",
  cm: "cm",
  m: "m",
  kg: "kg",
  "kg/m²": "kg/m2",
  "kg/m2": "kg/m2",
  pH: "[pH]",
  "mg/mmol creatinine": "mg/mmol",
  "mg/mmol": "mg/mmol",
  g: "g",
  "g/cm2": "g/cm2",
  "kcal/d": "kcal/d",
  a: "a",
  "{score}": "{score}",
  deg: "deg",
  "/day": "/d",
  "U/week": "/wk",
  day: "d",
  s: "s",
  "mm/hr": "mm/h",
}

/** A spelling one analyte means otherwise than the table does: FSH's and
 * LH's `U/L` are international units, a free androgen index's `RATIO` is a
 * percent, as is the GP record's `.` for nucleated red cells per 100 white
 * cells. The analyte is the catalogue entry's code, or for a measure
 * outside the catalogue its shared metric's id after the package
 * (`blood_glucose`, `nucleated_red_cells_per_100_wbc`), as `analyteKey`
 * names it; a `null` spelling is a unit the provider leaves out. */
export const UNIT_OVERRIDES: readonly {
  analyte: string
  spelling: string | null
  ucum: string
}[] = [
  { analyte: "fsh", spelling: "U/l", ucum: "[IU]/L" },
  { analyte: "lh", spelling: "U/l", ucum: "[IU]/L" },
  { analyte: "fsh", spelling: "U/L", ucum: "[IU]/L" },
  { analyte: "lh", spelling: "U/L", ucum: "[IU]/L" },
  { analyte: "free_androgen_index", spelling: "RATIO", ucum: "%" },
  { analyte: "free_androgen_index", spelling: "ratio", ucum: "%" },
  { analyte: "fib4", spelling: "-", ucum: "{score}" },
  { analyte: "egfr", spelling: "mL/min", ucum: "mL/min/{1.73_m2}" },
  { analyte: "cholesterol_hdl_ratio", spelling: null, ucum: "1" },
  { analyte: "cholesterol_hdl_ratio", spelling: "", ucum: "1" },
  { analyte: "nucleated_red_cells_per_100_wbc", spelling: ".", ucum: "%" },
  { analyte: "blood_glucose", spelling: "mmol/L", ucum: "mmol/L{glucose}" },
  { analyte: "d_dimer", spelling: "ng/mL FEU", ucum: "ng/mL" },
]

/** Every UCUM code the tables write: a value is written as a quantity only
 * in one of these, and only where the repository's unit graph holds it. */
export const UCUM_CODES: ReadonlySet<string> = new Set([
  ...Object.values(UNIT_SPELLINGS),
  ...UNIT_OVERRIDES.map((o) => o.ucum),
])

// The two micro signs look alike and are two code points: the table is
// written with U+00B5, and a provider printing U+03BC means the same.
const micro = (s: string) => s.replace(/\u03bc/g, "\u00b5")

/** A printed unit's UCUM code: the analyte's override first, then the
 * table, else the spelling as printed. Undefined only for a missing unit
 * no override names. */
export function ucum(
  spelling: string | null | undefined,
  analyte?: string
): string | undefined {
  const s = spelling ?? null
  if (analyte) {
    const o = UNIT_OVERRIDES.find(
      (x) =>
        x.analyte === analyte &&
        (x.spelling === s || (s !== null && x.spelling === micro(s)))
    )
    if (o) return o.ucum
  }
  if (s === null) return undefined
  return UNIT_SPELLINGS[s] ?? UNIT_SPELLINGS[micro(s)] ?? s
}

/** A unit the repository knows: its UCUM code, which a value is written
 * in, and the id of the unit record the graph resolves it to. */
export interface KnownUnit {
  ucum: string
  id: string
}

/** A printed unit as the repository knows it, or undefined where the
 * tables give no code (it is kept as received) or the graph holds none
 * for the code (the server would refuse it). */
export function knownUnit(
  spelling: string | null | undefined,
  analyte: string | undefined,
  graph: UnitGraph
): KnownUnit | undefined {
  const code = ucum(spelling, analyte)
  if (code === undefined || !UCUM_CODES.has(code)) return undefined
  const id = unitId(graph, code)
  return id === undefined ? undefined : { ucum: code, id }
}

/** The fixed factors a provider's own reference range is moved into its
 * value's unit by, from one UCUM code to another; a pair listed once
 * converts both ways where its factor is 1. */
export const FIXED_FACTORS: readonly {
  from: string
  to: string
  factor: string
}[] = [
  { from: "g/dL", to: "g/L", factor: "10" },
  { from: "pg/mL", to: "ng/L", factor: "1" },
  { from: "ng/L", to: "pg/mL", factor: "1" },
  { from: "ng/mL", to: "ug/L", factor: "1" },
  { from: "ug/L", to: "ng/mL", factor: "1" },
  { from: "m[IU]/L", to: "u[IU]/mL", factor: "1" },
  { from: "u[IU]/mL", to: "m[IU]/L", factor: "1" },
]

/** The fixed factor from one UCUM code to another, "1" for the same code,
 * or undefined where no fixed factor joins them (a mass and an amount
 * concentration): the range is then left out. */
export function fixedFactor(from: string, to: string): string | undefined {
  if (from === to) return "1"
  return FIXED_FACTORS.find((f) => f.from === from && f.to === to)?.factor
}
