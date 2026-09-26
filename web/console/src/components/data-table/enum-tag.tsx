/** An enum value as the grid and its filters draw it: the value's label on
 * a hue. A ladder (priority, severity) climbs from quiet to loud with its
 * declared order; any other enum takes a stable hue per value; the quiet
 * words ("none", "unknown", "other") stay plain. */

import type { DeclaredProperty } from "@/lib/definition"
import { enumLabel } from "@/lib/grid-values"
import { HUE_CLASSES, hashHue, type KindHue } from "@/lib/kind-glyph"
import { cn } from "@/lib/utils"

const QUIET_ENUMS = new Set(["none", "unknown", "other"])

/** A ladder (priority, severity) climbs from quiet to loud with its declared
 * order; any other enum takes a stable hue per value. */
const LADDER = /priority|severity|urgency|importance/i
const LADDER_HUES: KindHue[] = ["gray", "blue", "yellow", "orange", "red"]

function enumHue(prop: DeclaredProperty, value: string): KindHue {
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

export function EnumTag({
  prop,
  value,
}: {
  prop: DeclaredProperty
  value: string
}) {
  const label = enumLabel(prop, value)
  if (QUIET_ENUMS.has(value)) {
    return <span className="text-muted-foreground">{label}</span>
  }
  return (
    <span
      className={cn(
        "inline-block max-w-full truncate rounded-[4px] px-[7px] align-middle text-[0.93em] leading-5",
        HUE_CLASSES[enumHue(prop, value)].tile
      )}
    >
      {label}
    </span>
  )
}
