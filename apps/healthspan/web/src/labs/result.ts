// How a lab result prints, whatever its shape: a number with its comparator
// and unit, a coded finding, free text, a ratio, a value the importer could
// not normalize, or no result with the lab's reason. Every screen that shows
// a result reads it from here.

import { capitalize, formatValue } from "../lib/format"
import { unitLabel, withUnit } from "../lib/units"
import type { LabResult } from "./types"

export type ResultShape =
  "numeric" | "coded" | "text" | "ratio" | "raw" | "absent"

export interface ShownResult {
  shape: ResultShape
  /** The value alone: `<0.5`, `Not detected`, `1:80`. */
  value: string
  /** The unit as it prints, empty when the shape has none. */
  unit: string
}

export type ResultLike = Pick<
  LabResult,
  | "value"
  | "unit"
  | "comparator"
  | "coded"
  | "text"
  | "ratio"
  | "absentReason"
  | "sourceValue"
  | "sourceUnit"
>

/** The parts of a result, for a tile that sets the unit apart. `unit` is
 * the metric's unit symbol, used when the result names none. */
export function resultParts(
  r: ResultLike,
  precision?: number,
  unit?: string
): ShownResult {
  if (r.value !== undefined)
    return {
      shape: "numeric",
      value: formatValue(r.value, precision, r.comparator),
      unit: unitLabel(r.unit ?? unit),
    }
  if (r.ratio)
    return {
      shape: "ratio",
      value: `${formatValue(r.ratio.numerator)}:${formatValue(r.ratio.denominator)}`,
      unit: "",
    }
  const coded = r.coded?.display ?? r.coded?.code
  if (coded) return { shape: "coded", value: coded, unit: "" }
  if (r.text) return { shape: "text", value: r.text, unit: "" }
  if (r.sourceValue)
    return {
      shape: "raw",
      value: `${r.comparator ?? ""}${r.sourceValue}`,
      unit: r.sourceUnit ?? "",
    }
  return {
    shape: "absent",
    value: r.absentReason
      ? capitalize(r.absentReason.replace(/[-_]/g, " "))
      : "No result",
    unit: "",
  }
}

/** A result as one string: `3.4 mmol/L`, `54%`, `Not detected`. */
export function resultText(
  r: ResultLike,
  precision?: number,
  unit?: string
): string {
  const s = resultParts(r, precision, unit)
  if (s.shape === "numeric") return withUnit(s.value, r.unit ?? unit)
  return s.unit ? `${s.value} ${s.unit}` : s.value
}
