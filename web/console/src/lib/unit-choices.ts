/** The units a reading may be shown in: the codes of its measure the server
 * converts the stored unit to. The table (`lib/units.ts`) lists every code
 * of a measure, and beside them the classes the engine converts a value
 * between (`converts`), which `TestConsoleUnitsMatchTheEngine` writes from
 * `vocabulary.ConvertIn` itself, so this module decides nothing about UCUM:
 * it looks the pair up. */

import { lookupMeasure, measureOf, type Measure } from "@/lib/units"

/** Whether the server converts a value from one listed code of a measure
 * to another: the two share one of the measure's classes. */
export function converts(measure: Measure, from: string, to: string): boolean {
  if (from === to) return true
  return measure.converts.some((cls) => cls.includes(from) && cls.includes(to))
}

/** The units a reading may be shown in: the codes of the metric's measure
 * (the declared `measures`, else the stored unit's) the server converts the
 * stored unit to, in the table's order, or the stored unit alone where the
 * table does not place it (`{steps}`). Empty where nothing is stored. */
export function unitChoices(
  stored: string | undefined,
  measures?: string
): string[] {
  if (!stored) return []
  const measure = lookupMeasure(measures) ?? lookupMeasure(measureOf(stored))
  if (!measure || !measure.units.includes(stored)) return [stored]
  return measure.units.filter((code) => converts(measure, stored, code))
}

/** The units a stored unit is read in day to day, where its measure lists
 * many no one picks for it: a body weight in kilograms is weighed in pounds
 * and stones, never micrograms or tonnes. A unit this does not name keeps
 * its measure's full list. */
const EVERYDAY: Record<string, readonly string[]> = {
  kg: ["kg", "[lb_av]", "[stone_av]"],
  g: ["g", "kg", "mg", "[oz_av]", "[lb_av]"],
  m: ["m", "km", "[mi_i]", "[ft_i]"],
  km: ["km", "m", "[mi_i]"],
  L: ["L", "mL", "[gal_us]", "[foz_us]"],
  mL: ["mL", "L", "[foz_us]", "[cup_us]"],
}

/** The units a chart's Unit menu offers of `units` (`unitChoices`): the
 * everyday ones for the stored unit, with any `picked` among them, in the
 * table's order; all of them where the stored unit has no everyday list. */
export function everydayUnits(
  units: readonly string[],
  stored: string | undefined,
  picked?: string
): string[] {
  const everyday = stored ? EVERYDAY[stored] : undefined
  if (!everyday) return [...units]
  return units.filter((u) => everyday.includes(u) || u === picked)
}

/** `everydayUnits` for units spelled another way than the table's codes (a
 * repository graph's ids): each spelling, the list's and the offered ones,
 * is compared as `canonical` reads it. */
export function everydayUnitsBy(
  units: readonly string[],
  stored: string | undefined,
  picked: string | undefined,
  canonical: (unit: string) => string
): string[] {
  const key = stored ? canonical(stored) : undefined
  const everyday = key
    ? Object.entries(EVERYDAY).find(([code]) => canonical(code) === key)?.[1]
    : undefined
  if (!everyday) return [...units]
  const held = new Set(everyday.map(canonical))
  return units.filter((u) => held.has(canonical(u)) || u === picked)
}
