/** A quantity value, read: the digits as stored with the unit's print symbol
 * (`37 °C`, `<0.5 ng/mL`, `19 score`), and with Technical details on the
 * exact text a write takes (`37 Cel`, `19 {score}`) on hover. A value that
 * is not a quantity reads as the JSON it is, so a malformed row is still
 * visible. */

import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { formatQuantity, formatQuantityText } from "@/lib/units"
import { cn } from "@/lib/utils"

export function QuantityValue({
  value,
  className,
}: {
  value: unknown
  className?: string
}) {
  const [technical] = useTechnicalDetails()
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
      title={technical ? formatQuantityText(value) : shown}
    >
      {shown}
    </span>
  )
}
