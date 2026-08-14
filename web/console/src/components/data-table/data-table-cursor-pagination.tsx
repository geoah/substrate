/** Prev/next pagination for cursor-addressed feeds (the changelog is
 * seq-addressed — no offset cursor, so no page numbers; ticket 009's recorded
 * deviation, kept inside the one table system per the 2026-08-06 ruling).
 * Same seam anatomy as the numbered bar: range/summary left, controls right. */

import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"

interface DataTableCursorPaginationProps {
  /** 1-based position, purely informational ("page 3"). */
  page: number
  /** Rows on the current page. */
  rows: number
  hasPrev: boolean
  hasNext: boolean
  onPrev: () => void
  onNext: () => void
  /** The next page is being fetched. */
  loading?: boolean
  /** Extra voice for the left seam ("beginning of the feed", counts…). */
  summary?: React.ReactNode
  /** Rails drop the outer gutter a step. */
  density?: "default" | "compact"
}

export function DataTableCursorPagination({
  page,
  rows,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  loading,
  summary,
  density = "default",
}: DataTableCursorPaginationProps) {
  return (
    <div
      className={cn(
        "flex shrink-0 items-center justify-between gap-4 border-t py-2 text-xs text-muted-foreground",
        density === "compact" ? "px-4" : "px-6"
      )}
    >
      <span className="flex min-w-0 items-center gap-1.5 truncate data">
        page {page.toLocaleString()}
        {rows > 0 &&
          `, ${rows.toLocaleString()} ${rows === 1 ? "row" : "rows"}`}
        {summary && <span className="truncate">, {summary}</span>}
      </span>
      <div className="flex shrink-0 items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2"
          disabled={!hasPrev || loading}
          onClick={onPrev}
        >
          Previous
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2"
          disabled={!hasNext || loading}
          onClick={onNext}
        >
          {loading && <Spinner className="size-3" />}
          Next
        </Button>
      </div>
    </div>
  )
}
