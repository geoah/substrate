/** A section's heading row on every page: the title, a quiet hint beside it,
 * and the section's own actions on the right. One look, so sections on
 * different pages share their margins, alignment and tracking. */

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export function SectionHead({
  title,
  hint,
  actions,
  id,
  className,
}: {
  title: ReactNode
  /** A few quiet words beside the title: a count, what the section holds. */
  hint?: ReactNode
  /** Right-aligned; a button or a link. */
  actions?: ReactNode
  /** The heading's id, for `aria-labelledby` and for scrolling to it. */
  id?: string
  className?: string
}) {
  return (
    <div
      data-slot="section-head"
      className={cn(
        "mt-8 mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-1",
        className
      )}
    >
      <h2
        id={id}
        className="flex min-w-0 scroll-mt-6 items-center gap-2 text-[15px] font-semibold tracking-[-0.01em]"
      >
        {title}
      </h2>
      {hint && <span className="text-[12.5px] text-faint">{hint}</span>}
      {actions && (
        <div className="ml-auto flex items-center gap-2 self-center">
          {actions}
        </div>
      )}
    </div>
  )
}
