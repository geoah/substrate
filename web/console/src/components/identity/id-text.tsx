/** An identifier someone might copy — a record id, a reference, an actor —
 * in the mono voice, wrapping rather than truncating. Pages show one only in
 * technical mode or where the reader must copy it. */

import type { ReactNode } from "react"

import { CopyButton } from "@/components/identity/copy-button"
import { cn } from "@/lib/utils"

export function IdText({
  value,
  children,
  copy = false,
  className,
}: {
  value: string
  /** What shows, when not the value itself (a highlighted reference). */
  children?: ReactNode
  /** Adds a CopyButton for the value. */
  copy?: boolean
  className?: string
}) {
  return (
    <span
      className={cn(
        "inline-flex max-w-full min-w-0 items-center gap-1 align-middle",
        className
      )}
    >
      <span className="font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
        {children ?? value}
      </span>
      {copy && <CopyButton value={value} />}
    </span>
  )
}
