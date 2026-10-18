/** The pieces every block of a record view is drawn with: what a block
 * reads from, its heading, and the quiet lines it says instead of a value. */

import type { ReactNode } from "react"
import { TriangleAlertIcon } from "lucide-react"

import { IndexPending } from "@/components/observations/index-pending"
import { Skeleton } from "@/components/ui/skeleton"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { TimeRange } from "@/lib/observations-time"
import { indexPending } from "@/lib/observations-reads"
import type { Bound } from "@/lib/record-view"

/** What every block of one view on one record reads from. */
export interface BlockContext {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: readonly KindInfo[]
  /** The record in words, lower case: its kind's display name. */
  noun: string
  /** The window the view binds on the record, where it binds one. */
  bound?: Bound
  /** The part of the window the reader zoomed into, else the whole. */
  window?: TimeRange
  zoomed: boolean
  onZoom: (range: TimeRange) => void
  onReset: () => void
  timezone: string
}

/** A section the console cannot draw, said where it would be. */
export function Notice({ children }: { children: ReactNode }) {
  return (
    <p
      role="note"
      className="mt-3 flex items-start gap-1.5 text-[12.5px] text-muted-foreground"
    >
      <TriangleAlertIcon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </p>
  )
}

/** A block's own heading and what it holds. */
export function Part({
  title,
  children,
}: {
  title?: string
  children: ReactNode
}) {
  return (
    <div className="mt-3 flex flex-col gap-1.5" role="group" aria-label={title}>
      {title && <h3 className="text-[13px] font-medium">{title}</h3>}
      {children}
    </div>
  )
}

export function Loading() {
  return <Skeleton className="h-24 w-full max-w-[520px]" />
}

export function Failed({
  error,
  onRetry,
}: {
  error: Error
  onRetry?: () => void
}) {
  if (indexPending(error))
    return <IndexPending error={error} onRetry={onRetry} />
  return (
    <p className="text-[12.5px] text-destructive">
      This read failed: {error.message}
    </p>
  )
}

export function Quiet({ children }: { children: ReactNode }) {
  return <p className="text-[12.5px] text-muted-foreground">{children}</p>
}
