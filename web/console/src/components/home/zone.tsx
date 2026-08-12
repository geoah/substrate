/** The dashboard's shared zone grammar: every zone opens with the same
 * header row — a title that IS the door to the full surface (rule 9: the
 * underline arrives on hover), an optional status word, and the right-edge
 * link spelling the destination — and fails with the same compact retry
 * band. Skeletons stay per-zone (they mirror each zone's own layout). */

import type { ReactNode } from "react"
import { Link } from "@tanstack/react-router"
import { ArrowRightIcon } from "lucide-react"

import { Button } from "@/components/ui/button"

export function ZoneHeader({
  title,
  to,
  linkLabel,
  status,
}: {
  title: string
  /** The zone's surface — both the title and the right-edge link open it. */
  to: string
  linkLabel: string
  /** A short "is it okay" claim beside the title (all quiet, N pending). */
  status?: ReactNode
}) {
  return (
    <div className="flex items-baseline gap-3">
      <h2 className="text-sm font-semibold">
        <Link to={to} className="underline-offset-4 hover:underline">
          {title}
        </Link>
      </h2>
      {status}
      <Link
        to={to}
        className="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
      >
        {linkLabel}
        <ArrowRightIcon className="size-3" />
      </Link>
    </div>
  )
}

/** A zone that didn't load: the message and a retry, no taller than a row —
 * one broken zone must not shove the other three off the glance. */
export function ZoneError({
  message,
  onRetry,
}: {
  message: string
  onRetry: () => void
}) {
  return (
    <div className="flex items-center gap-3 rounded-md border border-dashed px-3 py-2.5 text-xs text-muted-foreground">
      <span className="min-w-0 truncate">{message}</span>
      <Button
        variant="outline"
        size="sm"
        className="ml-auto h-6 shrink-0 px-2 text-xs"
        onClick={onRetry}
      >
        Retry
      </Button>
    </div>
  )
}
