/** The one mark for "nothing here", wherever a value is read: the grid's
 * cells and the property sheet alike. A screen reader hears the word. */

import type { ReactNode } from "react"

import { EMPTY_VALUE } from "@/lib/grid-values"
import { cn } from "@/lib/utils"

export function EmptyValue({
  children,
  className,
}: {
  /** Words in place of the mark, where the absence says more ("Cleared"). */
  children?: ReactNode
  className?: string
}) {
  return (
    <span data-slot="empty-value" className={cn("text-faint", className)}>
      {children ?? (
        <>
          <span aria-hidden>{EMPTY_VALUE}</span>
          <span className="sr-only">Empty</span>
        </>
      )}
    </span>
  )
}
