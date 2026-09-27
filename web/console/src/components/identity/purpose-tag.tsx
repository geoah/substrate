/** A kind's purpose where it is not primary ("supporting", "internal"), as a
 * small outlined tag beside its name. Technical mode only: everyday mode
 * lists primary kinds alone. */

import type { KindPurpose } from "@/lib/definition"
import { cn } from "@/lib/utils"

export function PurposeTag({
  purpose,
  className,
}: {
  purpose: KindPurpose
  className?: string
}) {
  if (purpose === "primary") return null
  return (
    <span
      data-slot="purpose-tag"
      className={cn(
        "shrink-0 rounded-[3px] border border-border-strong px-1 text-[11.5px] leading-4 font-normal text-faint",
        className
      )}
    >
      {purpose}
    </span>
  )
}
