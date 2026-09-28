/** A money value, read: the amount in the reader's locale with the stored
 * scale kept (`€19.99`), the exact text a write takes (`19.99 EUR`) on hover.
 * A value that is not money reads as the JSON it is, so a malformed row is
 * still visible. */

import { formatMoney, moneyText } from "@/lib/money"
import { cn } from "@/lib/utils"

export function MoneyValue({
  value,
  className,
}: {
  value: unknown
  className?: string
}) {
  const shown = formatMoney(value)
  if (!shown) {
    return (
      <span className={cn("font-mono text-xs break-all", className)}>
        {JSON.stringify(value)}
      </span>
    )
  }
  return (
    <span className={cn("tabular-nums", className)} title={moneyText(value)}>
      {shown}
    </span>
  )
}
