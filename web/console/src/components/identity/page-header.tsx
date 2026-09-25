/** Every page's head: an optional glyph, the title, one meta line, and the
 * page's actions on the right. Left-aligned; the breadcrumb is the shell's,
 * never the page's. */

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export function PageHeader({
  title,
  meta,
  description,
  actions,
  glyph,
  size = "page",
  className,
}: {
  title: ReactNode
  /** One quiet line under the title: counts, who and when, a reference in
   * technical mode. */
  meta?: ReactNode
  /** A sentence or two about what the page holds. */
  description?: ReactNode
  actions?: ReactNode
  /** A `KindGlyph` (size lg) or another mark. */
  glyph?: ReactNode
  /** `record` is the larger title a record page carries. */
  size?: "page" | "record"
  className?: string
}) {
  return (
    <header
      data-slot="page-header"
      className={cn("flex items-start gap-3.5", className)}
    >
      {glyph && <div className="shrink-0 pt-0.5">{glyph}</div>}
      <div className="min-w-0 flex-1">
        <h1
          className={cn(
            "text-balance break-words",
            size === "page"
              ? "text-[26px] leading-tight font-[650] tracking-[-0.02em]"
              : "text-[32px] leading-[1.15] font-bold tracking-[-0.025em]"
          )}
        >
          {title}
        </h1>
        {meta && (
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-[12.5px] text-faint">
            {meta}
          </div>
        )}
        {description && (
          <p className="mt-1.5 max-w-[68ch] text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {actions && (
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {actions}
        </div>
      )}
    </header>
  )
}
