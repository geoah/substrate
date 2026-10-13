/** A quantity value, read: its digits to `places` with the unit's print
 * symbol (`97.18 %`, `<0.5 ng/mL`, `19 score`), the stored digits on hover;
 * with Technical details on, the digits as stored, and the exact text a
 * write takes (`37 Cel`, `19 {score}`) on hover. A value that is not a
 * quantity reads as the JSON it is, so a malformed row is still visible. */

import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  displayQuantity,
  formatQuantity,
  formatQuantityText,
} from "@/lib/units"
import { cn } from "@/lib/utils"

export function QuantityValue({
  value,
  places,
  className,
}: {
  value: unknown
  /** The places shown outside Technical details; `DISPLAY_PLACES` where
   * nothing declares them. */
  places?: number
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const shown = technical
    ? formatQuantity(value)
    : displayQuantity(value, places)
  if (!shown) {
    return (
      <span className={cn("font-mono text-xs break-all", className)}>
        {JSON.stringify(value)}
      </span>
    )
  }
  return (
    <span
      className={cn("tabular-nums", className)}
      title={technical ? formatQuantityText(value) : formatQuantity(value)}
    >
      {shown}
    </span>
  )
}
