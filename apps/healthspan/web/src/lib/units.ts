// Unit symbols as substrate writes them (`/core/unit/<symbol>`), how to show
// them, and the linear conversions between the ones a lab result can arrive
// in and the unit its metric declares. A result in a unit with no conversion
// keeps its own value and unit.

/** `substrate.reamde.dev/core/unit/umol/L` -> `umol/L`. */
export function unitSymbol(ref: unknown): string | undefined {
  const r =
    typeof ref === "string" ? ref : (ref as { ref?: string } | undefined)?.ref
  if (!r) return undefined
  const i = r.indexOf("/core/unit/")
  return i < 0 ? r : r.slice(i + "/core/unit/".length)
}

const DISPLAY: Record<string, string> = {
  percent: "%",
  "1": "",
  umol: "µmol",
  "umol/L": "µmol/L",
  "ug/L": "µg/L",
  "ug/mL": "µg/mL",
  "1e9/L": "×10⁹/L",
  "1e12/L": "×10¹²/L",
  "1/min": "bpm",
  "mL/min/1.73m2": "mL/min/1.73m²",
  "kg/m2": "kg/m²",
  "g/cm2": "g/cm²",
  Cel: "°C",
  a: "years",
  "mL/kg/min": "mL/kg/min",
  "mmol/L_glucose": "mmol/L",
  score: "",
}

/** How a unit symbol prints beside a value. */
export function unitLabel(symbol: string | undefined): string {
  if (!symbol) return ""
  return DISPLAY[symbol] ?? symbol
}

/** factor such that value_in_to = value_in_from × factor. */
const FACTORS: Record<string, Record<string, number>> = {
  g: { kg: 0.001 },
  kg: { g: 1000 },
  cm: { m: 0.01 },
  m: { cm: 100 },
  "ng/mL": { "ug/L": 1 },
  "ug/L": { "ng/mL": 1 },
  "pg/mL": { "ng/L": 1 },
  "ng/L": { "pg/mL": 1 },
  "g/L": { "g/dL": 0.1, "mg/dL": 100 },
  "g/dL": { "g/L": 10 },
  "uIU/mL": { "mIU/L": 1 },
  "mIU/L": { "uIU/mL": 1 },
  "1": { percent: 100 },
  percent: { "1": 0.01 },
  "IU/L": { "U/L": 1 },
  "U/L": { "IU/L": 1 },
}

/** The factor from one unit to another, or undefined when no linear
 * conversion is known. Equal units convert by 1. */
export function factor(
  from: string | undefined,
  to: string | undefined
): number | undefined {
  if (!from || !to || from === to) return 1
  return FACTORS[from]?.[to]
}

/** A formatted value with its unit: `3.4 mmol/L`, `54%`. */
export function withUnit(value: string, symbol: string | undefined): string {
  const u = unitLabel(symbol)
  if (!u) return value
  return u === "%" || u.startsWith("°") ? `${value}${u}` : `${value} ${u}`
}
