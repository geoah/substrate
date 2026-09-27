/** The one colour map enum values read on, wherever they are drawn. */

import type { DeclaredProperty } from "@/lib/definition"
import { hashHue, type KindHue } from "@/lib/kind-glyph"

/** What an enum hue needs of the property: its name, which picks the ladder,
 * and its declared values, which order the ladder and carry the labels. The
 * grid's DeclaredProperty and the sheet's PropSpec both are one. */
export type EnumProperty = Pick<DeclaredProperty, "name" | "values">

/** Words that say "no value in particular"; they read plain, never on a hue. */
const QUIET_ENUMS = new Set(["none", "unknown", "other"])

/** A ladder (priority, severity) climbs from quiet to loud with its declared
 * order; any other enum takes a stable hue per value. */
const LADDER = /priority|severity|urgency|importance/i
const LADDER_HUES: KindHue[] = ["gray", "blue", "yellow", "orange", "red"]

/** The hue an enum value reads on; undefined for a quiet word, which reads
 * plain. */
export function enumHue(
  prop: EnumProperty,
  value: string
): KindHue | undefined {
  if (QUIET_ENUMS.has(value)) return undefined
  if (LADDER.test(prop.name) && prop.values?.length) {
    const loud = prop.values.filter((v) => !QUIET_ENUMS.has(v.value))
    const at = loud.findIndex((v) => v.value === value)
    if (at >= 0) {
      const step = LADDER_HUES.length - loud.length + at
      return LADDER_HUES[Math.max(1, Math.min(LADDER_HUES.length - 1, step))]
    }
  }
  return hashHue(value)
}
