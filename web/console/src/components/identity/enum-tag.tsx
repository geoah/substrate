/** An enum value as every surface draws it (the grid, the property sheet,
 * the filters, a suggested change, a history move): the value's label on a
 * hue. One colour map, so "High" is the same red wherever it is read. */

import { enumHue, type EnumProperty } from "@/lib/enum-hue"
import { enumLabel } from "@/lib/grid-values"
import { HUE_CLASSES } from "@/lib/kind-glyph"
import { cn } from "@/lib/utils"

export function EnumTag({
  prop,
  value,
  className,
}: {
  prop: EnumProperty
  value: string
  className?: string
}) {
  const label = enumLabel(prop, value)
  const hue = enumHue(prop, value)
  if (!hue) {
    return (
      <span
        data-slot="enum-tag"
        className={cn("text-muted-foreground", className)}
      >
        {label}
      </span>
    )
  }
  return (
    <span
      data-slot="enum-tag"
      data-hue={hue}
      className={cn(
        "inline-block max-w-full truncate rounded-[4px] px-[7px] align-middle text-[0.93em] leading-5",
        HUE_CLASSES[hue].tile,
        className
      )}
    >
      {label}
    </span>
  )
}
