/** A quantity value, read: the digits as stored with the unit's print symbol
 * (`37 °C`, `<0.5 ng/mL`), the exact text a write takes (`37 Cel`) on hover.
 * A value that is not a quantity reads as the JSON it is, so a malformed row
 * is still visible. */

import { formatQuantity, formatQuantityText } from "@/lib/units"
import { cn } from "@/lib/utils"

export function QuantityValue({
  value,
  className,
}: {
  value: unknown
  className?: string
}) {
  const shown = formatQuantity(value)
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
      title={formatQuantityText(value)}
    >
      {shown}
    </span>
  )
}
