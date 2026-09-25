/** The one hover card every identity mark opens — a record, a kind, an
 * actor: a top block (mark, title, a line under it), an optional grid of
 * facts, and a mono footer carrying the full reference, so everything
 * technical stays one hover away in everyday mode. Slow to open, closed by
 * any scroll, and never put on a whole table row. */

import { useEffect, useState, type ReactElement, type ReactNode } from "react"

import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

export const HOVER_OPEN_DELAY = 500

export function IdentityHoverCard({
  trigger,
  children,
  className,
  card,
  delay = HOVER_OPEN_DELAY,
  label,
}: {
  /** The element the mark renders as: a router `Link`, a `button`, a
   * `span`. */
  trigger: ReactElement
  /** The mark itself. */
  children: ReactNode
  className?: string
  /** The card body; a function mounts only while open, so a card that reads
   * something reads it on hover, never before. */
  card: ReactNode | ((open: boolean) => ReactNode)
  delay?: number
  /** The trigger's accessible name, where its text alone does not say it. */
  label?: string
}) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!open) return undefined
    const close = () => setOpen(false)
    window.addEventListener("scroll", close, true)
    return () => window.removeEventListener("scroll", close, true)
  }, [open])
  return (
    <HoverCard open={open} onOpenChange={setOpen}>
      <HoverCardTrigger
        delay={delay}
        render={trigger}
        className={className}
        aria-label={label}
      >
        {children}
      </HoverCardTrigger>
      <HoverCardContent
        align="start"
        className="w-72 max-w-[calc(100vw-2rem)] overflow-hidden rounded-[10px] border border-border-strong bg-background p-0 text-[12.5px] leading-[1.45] shadow-card ring-0"
      >
        {typeof card === "function" ? card(open) : card}
      </HoverCardContent>
    </HoverCard>
  )
}

export interface IdentityFact {
  label: string
  value: ReactNode
}

/** The card's look. Every part but the title is optional. */
export function IdentityCard({
  mark,
  title,
  sub,
  description,
  facts,
  reference,
  loading,
}: {
  mark?: ReactNode
  title: ReactNode
  /** One quiet line under the title: the collection, "via this console". */
  sub?: ReactNode
  description?: ReactNode
  facts?: IdentityFact[]
  /** The full reference, in the mono footer. */
  reference?: string
  /** Facts are on their way. */
  loading?: boolean
}) {
  return (
    <div className="flex min-w-0 flex-col">
      <div className="px-3 pt-3 pb-2.5">
        <div className="flex min-w-0 items-center gap-2 text-[13.5px] font-semibold">
          {mark}
          <span className="min-w-0 break-words">{title}</span>
        </div>
        {sub && <div className="mt-0.5 text-xs text-faint">{sub}</div>}
        {description && (
          <div className="mt-1.5 text-muted-foreground">{description}</div>
        )}
      </div>
      {loading ? (
        <div className="flex flex-col gap-1.5 border-t px-3 pt-2 pb-2.5">
          <Skeleton className="h-3 w-4/5" />
          <Skeleton className="h-3 w-3/5" />
        </div>
      ) : facts && facts.length > 0 ? (
        // minmax(0,1fr): a long unbroken value must wrap inside the card.
        <dl className="grid grid-cols-[5rem_minmax(0,1fr)] gap-x-2 gap-y-1 border-t px-3 pt-2 pb-2.5">
          {facts.map((fact) => (
            <div key={fact.label} className="contents">
              <dt className="truncate text-faint">{fact.label}</dt>
              <dd className="break-words text-foreground">{fact.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      {reference && (
        <div
          className={cn(
            "border-t bg-panel px-3 py-[7px] font-mono text-[11px] break-all text-faint"
          )}
        >
          {reference}
        </div>
      )}
    </div>
  )
}
