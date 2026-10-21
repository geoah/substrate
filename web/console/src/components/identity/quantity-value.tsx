/** A quantity value, read: its digits to `places` with the unit's print
 * symbol (`97.18 %`, `<0.5 ng/mL`, `19 score`), the stored digits on hover;
 * with Technical details on, the digits as stored, and the exact text a
 * write takes (`37 Cel`, `19 {score}`) on hover. A value that is not a
 * quantity reads as the JSON it is, so a malformed row is still visible. */

import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useUnits } from "@/hooks/use-units"
import { unitsUnavailable } from "@/lib/unit-reader"
import { formatQuantityText } from "@/lib/units"
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
  const units = useUnits()
  const shown = technical ? units.format(value) : units.display(value, places)
  if (!shown) {
    return (
      <span className={cn("font-mono text-xs break-all", className)}>
        {JSON.stringify(value)}
      </span>
    )
  }
  // Until a graph-enabled server's units load, a value reads as stored, its
  // unit as the id the write took, and says why.
  const unavailable = unitsUnavailable(units)
  return (
    <span
      className={cn("tabular-nums", className)}
      aria-busy={units.status === "loading" || undefined}
      title={
        unavailable ??
        (technical ? formatQuantityText(value) : units.format(value))
      }
    >
      {shown}
    </span>
  )
}
