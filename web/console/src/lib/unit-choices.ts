/** The units a chart's Unit menu offers day to day, of the units a reading
 * may be shown in (a reader's `choices`, every unit of the stored unit's
 * linear component). */

/** The units a stored unit is read in day to day, where its component holds
 * many no one picks for it: a body weight in kilograms is weighed in pounds
 * and stones, never micrograms or tonnes. A unit this does not name keeps
 * its component's full list. Keys and members are shipped unit ids, compared
 * as the repository's graph resolves them. */
const EVERYDAY: Record<string, readonly string[]> = {
  kg: ["kg", "lb", "st"],
  g: ["g", "kg", "mg", "oz", "lb"],
  m: ["m", "km", "mi", "ft"],
  km: ["km", "m", "mi"],
  L: ["L", "mL", "gal", "floz"],
  mL: ["mL", "L", "floz", "cup"],
}

/** The everyday ones of `units` for a reading stored in `stored`, with any
 * `picked` among them, in the order of `units`; all of them where the
 * stored unit has no everyday list. Each spelling, the list's and the
 * offered ones, is compared as `canonical` reads it. */
export function everydayUnitsBy(
  units: readonly string[],
  stored: string | undefined,
  picked: string | undefined,
  canonical: (unit: string) => string
): string[] {
  const key = stored ? canonical(stored) : undefined
  const everyday = key
    ? Object.entries(EVERYDAY).find(([id]) => canonical(id) === key)?.[1]
    : undefined
  if (!everyday) return [...units]
  const held = new Set(everyday.map(canonical))
  return units.filter((u) => held.has(canonical(u)) || u === picked)
}
