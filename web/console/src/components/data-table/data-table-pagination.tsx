/** The footer pinned under a collection grid: which rows are on screen out of
 * how many ("1–50 of 331"), any note the page adds, and the page controls —
 * previous, the numbered pages, next. A page is a number on the wire
 * (`offset=`, decision 0084), so every page is one click away.
 *
 * The total comes from a bounded count walk, so it can be a FLOOR ("10,000+"),
 * and the bar says so: it draws no number past the floor it cannot vouch for,
 * and Next follows the server's own cursor rather than the count. */

import type { ReactNode } from "react"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

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
  /** The server says another page exists. Authoritative over `total`. */
  hasNext: boolean
  onPage: (page: number) => void
  /** A page is being fetched: a spinner beside the range. The controls stay
   * live, since a second click means the second page. */
  loading?: boolean
  /** Replaces the range on the left ("72 tasks · 60 at the top level"). */
  summary?: ReactNode
  /** Quiet notes after the range. */
  children?: ReactNode
  className?: string
}

const PAGE_BUTTON =
  "inline-grid h-[26px] min-w-[26px] cursor-pointer place-items-center rounded-[5px] border border-border-strong bg-background px-[7px] text-xs text-muted-foreground tabular-nums outline-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-default disabled:opacity-40 disabled:hover:bg-background aria-[current=page]:border-foreground aria-[current=page]:bg-foreground aria-[current=page]:text-background"

export function DataTablePagination({
  page,
  pageSize,
  rows,
  total,
  totalCapped,
  hasNext,
  onPage,
  loading,
  summary,
  children,
  className,
}: DataTablePaginationProps) {
  const first = rows === 0 ? 0 : (page - 1) * pageSize + 1
  const last = rows === 0 ? 0 : first + rows - 1
  const pageCount =
    total === undefined ? undefined : Math.max(1, Math.ceil(total / pageSize))
  const paged = page > 1 || hasNext

  return (
    <div
      data-slot="grid-footer"
      className={cn(
        "flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 bg-background text-[12.5px] text-faint",
        className
      )}
    >
      <span className="flex min-w-0 items-center gap-1.5">
        {summary ??
          (rows === 0 ? null : (
            <span className="tabular-nums">
              {first.toLocaleString()}–{last.toLocaleString()}
              {total !== undefined && (
                <>
                  {" of "}
                  {total.toLocaleString()}
                  {totalCapped ? "+" : ""}
                </>
              )}
            </span>
          ))}
        {loading && <Spinner className="size-3" />}
      </span>
      {children}
      {paged && (
        <nav
          aria-label="Pages"
          className="ml-auto flex shrink-0 items-center gap-1"
        >
          <button
            type="button"
            className={PAGE_BUTTON}
            aria-label="Previous page"
            disabled={page <= 1}
            onClick={() => onPage(page - 1)}
          >
            <ChevronLeftIcon className="size-3.5" />
          </button>
          {pageCount === undefined ? (
            // The count has not answered yet: say where we are rather than
            // drawing a bar whose width would jump when it does.
            <span className="px-1.5 tabular-nums">Page {page}</span>
          ) : (
            <>
              {pageItems(page, Math.max(pageCount, page)).map((n, i) =>
                n === null ? (
                  <span key={`gap-${i}`} aria-hidden className="px-1">
                    …
                  </span>
                ) : (
                  <button
                    key={n}
                    type="button"
                    className={PAGE_BUTTON}
                    aria-label={`Page ${n}`}
                    aria-current={n === page ? "page" : undefined}
                    onClick={() => onPage(n)}
                  >
                    {n}
                  </button>
                )
              )}
              {totalCapped && (
                <span aria-hidden className="px-1">
                  …
                </span>
              )}
            </>
          )}
          <button
            type="button"
            className={PAGE_BUTTON}
            aria-label="Next page"
            disabled={!hasNext}
            onClick={() => onPage(page + 1)}
          >
            <ChevronRightIcon className="size-3.5" />
          </button>
        </nav>
      )}
    </div>
  )
}
