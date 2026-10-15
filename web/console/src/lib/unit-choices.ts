/** The units a reading may be shown in: the codes of its measure the server
 * converts the stored unit to. The table (`lib/units.ts`) lists every code
 * of a measure, but the server refuses some pairs inside one
 * (internal/vocabulary/units.go `linear`): two annotations (`{count}/min`
 * and `/min` count different things), two arbitrary units or one per
 * volume beside one alone (`[IU]`, `[IU]/L`, `[arb'U]`), a level (`dBm`),
 * and a factor only pi states (`rad` and `deg`). The console cannot parse
 * UCUM, so these checks read a listed code's parts: enough for the codes
 * the table lists, which are the only ones offered. */

import { MEASURES, lookupMeasure, measureOf, type Measure } from "@/lib/units"

/** The atoms the table lists only under an arbitrary measure, read off its
 * codes: `[IU]`, `[arb'U]`. */
const ARBITRARY_ATOMS = new Set(
  MEASURES.filter((m) => m.scale === "arbitrary").flatMap((m) =>
    m.units.flatMap((code) => code.match(/\[[^\]]*\]/g) ?? [])
  )
)

/** The atoms UCUM defines through pi. */
const PI_ATOMS = new Set(["[pi]", "deg", "'", "''"])

/** One factor of a code, with the power it is raised to: negative on the
 * divisor's side of a `/`. */
interface Component {
  body: string
  power: number
}

/** A code's factors. UCUM's `/` divides by the one factor after it, and a
 * parenthesized group is one factor read the same way inside; brackets and
 * braces are opaque. */
function components(code: string, sign = 1): Component[] {
  const out: Component[] = []
  let text = ""
  let next = sign
  let depth = 0
  let closer = ""
  const flush = () => {
    if (!text) return
    const group = /^\((.*)\)(-?\d+)?$/.exec(text)
    if (group) {
      out.push(...components(group[1], next * Number(group[2] ?? 1)))
    } else {
      const power = /^(.*[^\d-])(-?\d+)$/.exec(text)
      out.push(
        power
          ? { body: power[1], power: next * Number(power[2]) }
          : { body: text, power: next }
      )
    }
    text = ""
  }
  for (const c of code) {
    if (closer) {
      text += c
      if (c === closer) closer = ""
      continue
    }
    if (c === "[") closer = "]"
    else if (c === "{") closer = "}"
    else if (c === "(") depth++
    else if (c === ")") depth--
    else if (depth === 0 && (c === "." || c === "/")) {
      flush()
      next = c === "/" ? -sign : sign
      continue
    }
    text += c
  }
  flush()
  return out
}

/** A count per key as one comparable string. */
function tally(counts: Map<string, number>): string {
  return [...counts]
    .filter(([, n]) => n !== 0)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, n]) => `${k}^${n}`)
    .join(" ")
}

/** What a code holds besides its dimension and factor, which two codes of
 * one measure must agree on to convert: its annotations, its arbitrary
 * atoms and its power of pi. */
function identity(code: string): string {
  const notes = new Map<string, number>()
  const arbitrary = new Map<string, number>()
  let pi = 0
  for (const { body, power } of components(code)) {
    for (const note of body.match(/\{[^}]*\}/g) ?? []) {
      notes.set(note, (notes.get(note) ?? 0) + power)
    }
    const bare = body.replace(/\{[^}]*\}/g, "")
    for (const atom of bare.match(/\[[^\]]*\]/g) ?? []) {
      if (ARBITRARY_ATOMS.has(atom)) {
        arbitrary.set(atom, (arbitrary.get(atom) ?? 0) + power)
      }
    }
    if (PI_ATOMS.has(bare)) pi += power
  }
  return `${tally(notes)}|${tally(arbitrary)}|${pi}`
}

/** A listed code with each factor's metric prefix dropped, where the
 * factor is a bracketed atom (`m[IU]`) or the table lists it both ways in
 * one measure (`mL` and `L`): two codes of one shape share a dimension. */
function shape(code: string): string {
  return components(code)
    .map(({ body, power }) => {
      let bare = body.replace(/^[^[{]+(?=\[)/, "")
      if (
        bare === body &&
        MEASURES.some(
          (m) => m.units.includes(body) && m.units.includes(body.slice(1))
        )
      ) {
        bare = body.slice(1)
      }
      return `${bare}^${power}`
    })
    .join(" ")
}

/** Whether the server converts a value from one listed code of a measure
 * to another. */
export function converts(measure: Measure, from: string, to: string): boolean {
  if (from === to) return true
  switch (measure.scale) {
    case "logarithmic":
      return false
    case "arbitrary":
      // Every code of the measure has its own dimension, so the shapes say
      // whether two share one.
      return shape(from) === shape(to)
    default:
      return identity(from) === identity(to)
  }
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
