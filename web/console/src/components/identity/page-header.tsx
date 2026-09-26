/** Every page's head, in one of two layouts. `page`: the glyph beside the
 * title, the actions on the right. `record`: the glyph above, on one row with
 * the actions, and the larger title under it — the head of a record, a merge,
 * a change request and a new record alike. Either way one place holds the
 * title, one meta line, a description and the actions. Left-aligned; the
 * breadcrumb is the shell's, never the page's.
 *
 * On a phone (under 560px) the page layout's actions drop below the title,
 * so a long title keeps the full width and wraps between words; it breaks
 * inside a word only when one word alone is wider than the page. */

import type { ReactNode } from "react"

import { pageTitleClass } from "./page-title"
import { cn } from "@/lib/utils"

export function PageHeader({
  title,
  heading,
  meta,
  description,
  actions,
  glyph,
  size = "page",
  children,
  className,
}: {
  /** The words of the `h1`. */
  title?: ReactNode
  /** A heading the page draws itself, in place of the `h1` from `title`:
   * a title edited in place. It should use `pageTitleClass(size)`. */
  heading?: ReactNode
  /** One quiet line under the title: counts, who and when, a reference in
   * technical mode. */
  meta?: ReactNode
  /** A sentence or two about what the page holds. */
  description?: ReactNode
  actions?: ReactNode
  /** A `KindGlyph` (size lg) or another mark. */
  glyph?: ReactNode
  /** `record` sets the glyph above the larger title. */
  size?: "page" | "record"
  /** What else belongs to the head, under the description: a callout. */
  children?: ReactNode
  className?: string
}) {
  const h1 = heading ?? (
    <h1 className={cn(pageTitleClass(size), size === "record" && "mt-2.5")}>
      {title}
    </h1>
  )
  const text = (
    <>
      {h1}
      {meta && (
        <div
          data-slot="page-meta"
          className="mt-1.5 flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-[12.5px] text-faint"
        >
          {meta}
        </div>
      )}
      {description && (
        <p className="mt-1.5 max-w-[68ch] text-muted-foreground">
          {description}
        </p>
      )}
    </>
  )
  const actionRow = actions && (
    <div data-slot="page-actions" className="flex flex-wrap items-center gap-2">
      {actions}
    </div>
  )

  if (size === "record") {
    return (
      <header
        data-slot="page-header"
        data-layout="record"
        className={className}
      >
        {(glyph || actions) && (
          <div className="flex items-start justify-between gap-3">
            {glyph ?? <span />}
            {actionRow}
          </div>
        )}
        {text}
        {children}
      </header>
    )
  }
  return (
    <header data-slot="page-header" data-layout="page" className={className}>
      <div className="flex flex-wrap items-start gap-x-3.5 gap-y-3 min-[560px]:flex-nowrap">
        {glyph && <div className="shrink-0 pt-0.5">{glyph}</div>}
        <div className="min-w-0 flex-1">{text}</div>
        {actions && (
          <div className="basis-full min-[560px]:shrink-0 min-[560px]:basis-auto">
            {actionRow}
          </div>
        )}
      </div>
      {children}
    </header>
  )
}
