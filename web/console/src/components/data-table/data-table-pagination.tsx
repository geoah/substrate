/** Numbered pagination for offset-addressed lists. A page is a number on the
 * wire (`offset=`, decision 0084), so every page is reachable in one click
 * and the bar can say how many there are — the cursor bar beside this one
 * (data-table-cursor-pagination.tsx) is for the feeds where it cannot.
 *
 * Same seam anatomy as that one: the range on the left says WHICH rows are on
 * screen and out of how many, labelled; the controls sit right. The total
 * comes from a bounded count walk, so it can be a FLOOR ("10,000+"), and the
 * bar says so rather than drawing a last page that is not the last one. */

import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { pageItems } from "@/lib/pagination"
import { cn } from "@/lib/utils"

interface DataTablePaginationProps {
  /** 1-based page number. */
  page: number
  /** Rows a full page holds — the `first` the list asked for. */
  pageSize: number
  /** Rows actually on this page; the last page is short. */
  rows: number
  /** Total rows matching the view, when it is known. */
  total?: number
  /** The count outran its ceiling: `total` is a floor, not the total. */
  totalCapped?: boolean
  /** The server says another page exists. Authoritative over `total`, which
   * may be a floor, so Next keeps working past a capped count. */
  hasNext: boolean
  onPage: (page: number) => void
  /** A page is being fetched. It shows as a spinner beside the range and
   * never disables the controls: a reader who clicks twice while a page is
   * in flight means the second page, and waiting for the first is a stall. */
  loading?: boolean
  /** Rails drop the outer gutter a step. */
  density?: "default" | "compact"
}

export function DataTablePagination({
  page,
  pageSize,
  rows,
  total,
  totalCapped,
  hasNext,
  onPage,
  loading,
  density = "default",
}: DataTablePaginationProps) {
  const first = rows === 0 ? 0 : (page - 1) * pageSize + 1
  const last = rows === 0 ? 0 : first + rows - 1
  // A capped total is a floor, so the page it implies is a floor too: the bar
  // draws up to it and Next carries on past it.
  const pageCount =
    total === undefined ? undefined : Math.max(1, Math.ceil(total / pageSize))
  const exactCount = pageCount !== undefined && !totalCapped

  return (
    <div
      className={cn(
        "flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t py-2 text-xs text-muted-foreground",
        density === "compact" ? "px-4" : "px-6"
      )}
    >
      <span className="flex min-w-0 items-center gap-1.5">
        {rows === 0 ? (
          "No rows"
        ) : (
          <>
            <span>Rows</span>
            <span className="text-foreground tabular-nums">
              {first.toLocaleString()}–{last.toLocaleString()}
            </span>
            {total !== undefined && (
              <span>
                of{" "}
                <span className="text-foreground tabular-nums">
                  {total.toLocaleString()}
                  {totalCapped ? "+" : ""}
                </span>
              </span>
            )}
          </>
        )}
        {loading && <Spinner className="size-3" />}
      </span>
      <nav
        aria-label="Pagination"
        className="flex shrink-0 items-center gap-0.5"
      >
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="First page"
          disabled={page === 1}
          onClick={() => onPage(1)}
        >
          <ChevronsLeftIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Previous page"
          disabled={page === 1}
          onClick={() => onPage(page - 1)}
        >
          <ChevronLeftIcon />
        </Button>
        {pageCount === undefined ? (
          // The count has not answered yet. Say where we are rather than
          // drawing a bar whose width would jump when it does.
          <span className="px-2 tabular-nums">Page {page}</span>
        ) : (
          pageItems(page, pageCount).map((n, i) =>
            n === null ? (
              <span
                key={`gap-${i}`}
                aria-hidden
                className="px-1 text-muted-foreground/60"
              >
                …
              </span>
            ) : (
              <Button
                key={n}
                variant={n === page ? "outline" : "ghost"}
                size="icon-sm"
                className={cn(
                  "tabular-nums",
                  n === page && "font-medium text-foreground"
                )}
                aria-label={`Page ${n}`}
                aria-current={n === page ? "page" : undefined}
                onClick={() => onPage(n)}
              >
                {n}
              </Button>
            )
          )
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Next page"
          disabled={!hasNext}
          onClick={() => onPage(page + 1)}
        >
          <ChevronRightIcon />
        </Button>
        {/* A capped total has no known last page, so there is no button that
         * could honestly go to one. */}
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Last page"
          disabled={!exactCount || page === pageCount}
          onClick={() => pageCount !== undefined && onPage(pageCount)}
        >
          <ChevronsRightIcon />
        </Button>
      </nav>
    </div>
  )
}
