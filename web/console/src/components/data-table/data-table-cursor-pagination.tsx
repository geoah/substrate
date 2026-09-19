/** Prev/next pagination for cursor-addressed feeds (the changelog is
 * seq-addressed — no offset cursor, so no page numbers; ticket 009's recorded
 * deviation, kept inside the one table system per the 2026-08-06 ruling).
 * Same seam anatomy and the same controls as the numbered bar
 * (data-table-pagination.tsx): the position on the left, labelled; the
 * chevrons right. It says a page rather than a row range because these feeds
 * filter their pages client-side (a time range, a stitched former id), so a
 * page here is not a fixed count of rows and the arithmetic would lie. */

import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

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
        "flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t py-2 text-xs text-muted-foreground",
        density === "compact" ? "px-4" : "px-6"
      )}
    >
      <span className="flex min-w-0 items-center gap-1.5 truncate">
        <span>Page</span>
        <span className="text-foreground tabular-nums">
          {page.toLocaleString()}
        </span>
        {rows > 0 && (
          <span className="tabular-nums">
            · {rows.toLocaleString()} {rows === 1 ? "row" : "rows"}
          </span>
        )}
        {summary && <span className="truncate">· {summary}</span>}
        {loading && <Spinner className="size-3" />}
      </span>
      <div className="flex shrink-0 items-center gap-0.5">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Previous page"
          disabled={!hasPrev}
          onClick={onPrev}
        >
          <ChevronLeftIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Next page"
          disabled={!hasNext}
          onClick={onNext}
        >
          <ChevronRightIcon />
        </Button>
      </div>
    </div>
  )
}
