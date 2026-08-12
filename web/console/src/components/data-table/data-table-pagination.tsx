/** Numbered pagination (rule 5, owner amendment): `1–N of total` on the left,
 * Previous / numbered window / Next on the right. The total comes from the
 * separate count query and can lag a beat behind the page — while it counts,
 * the range shows and Next follows the page's own cursor. */

import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { pageItems } from "@/lib/pagination"
import { cn } from "@/lib/utils"

interface DataTablePaginationProps {
  /** 1-based current page. */
  page: number
  pageSize: number
  /** Rows on the current page. */
  rows: number
  /** Exact total when known; undefined while the count query runs. */
  total?: number
  /** The page's own continuation cursor — Next works even before the total. */
  hasMore: boolean
  counting?: boolean
  onPageChange: (page: number) => void
}

export function DataTablePagination({
  page,
  pageSize,
  rows,
  total,
  hasMore,
  counting,
  onPageChange,
}: DataTablePaginationProps) {
  const start = rows === 0 ? 0 : (page - 1) * pageSize + 1
  const end = (page - 1) * pageSize + rows
  const pageCount =
    total !== undefined ? Math.max(1, Math.ceil(total / pageSize)) : undefined

  return (
    <div className="flex shrink-0 items-center justify-between gap-4 border-t px-6 py-2 text-xs text-muted-foreground">
      <span className="flex items-center gap-1.5 data">
        {rows === 0 && total === 0
          ? "0 of 0"
          : `${start.toLocaleString()}–${end.toLocaleString()} of ${
              total !== undefined ? total.toLocaleString() : "…"
            }`}
        {counting && <Spinner className="size-3" />}
      </span>
      <div className="flex items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2"
          disabled={page <= 1}
          onClick={() => onPageChange(page - 1)}
        >
          Previous
        </Button>
        {pageCount !== undefined ? (
          pageItems(page, pageCount).map((item, i) =>
            item === "…" ? (
              <span key={`gap-${i}`} className="px-1">
                …
              </span>
            ) : (
              <Button
                key={item}
                variant={item === page ? "outline" : "ghost"}
                size="sm"
                className={cn("h-7 min-w-7 px-1")}
                onClick={() => onPageChange(item)}
              >
                {item.toLocaleString()}
              </Button>
            )
          )
        ) : (
          <Button variant="outline" size="sm" className="h-7 min-w-7 px-1">
            {page.toLocaleString()}
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2"
          disabled={pageCount !== undefined ? page >= pageCount : !hasMore}
          onClick={() => onPageChange(page + 1)}
        >
          Next
        </Button>
      </div>
    </div>
  )
}
