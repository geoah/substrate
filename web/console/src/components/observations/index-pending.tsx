/** What a read says where the server turned it away while it re-derives
 * the observed index of a kind the read selects (`indexPending`): nothing
 * is wrong with the read or the data, and reading again once the server
 * finishes answers it. */

import { RefreshCwIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { ApiError } from "@/lib/api/types"
import { cn } from "@/lib/utils"

export const INDEX_PENDING_WORDS =
  "The server is re-indexing these measurements after a change to their kind, and answers once it finishes."

export function IndexPending({
  error,
  onRetry,
  className,
}: {
  error?: Error | null
  onRetry?: () => void
  className?: string
}) {
  const after = error instanceof ApiError ? error.retryAfter : undefined
  return (
    <div
      role="status"
      data-slot="index-pending"
      className={cn(
        "flex flex-wrap items-center gap-2 text-[12.5px] text-muted-foreground",
        className
      )}
    >
      <span>
        {INDEX_PENDING_WORDS}
        {after ? ` Try again in about ${after} s.` : ""}
      </span>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RefreshCwIcon />
          Read again
        </Button>
      )}
    </div>
  )
}
