/** The two page shapes. A document (a record, a tool, a provider, settings,
 * home, history) is left-aligned at the reader's record width; a table page
 * takes the reader's table width. Neither is ever centred. */

import type { ReactNode } from "react"

import { useLayoutWidths } from "@/hooks/use-console-preferences"
import type { RecordWidth, TableWidth } from "@/lib/console-preferences"
import { cn } from "@/lib/utils"

const RECORD_WIDTHS: Record<RecordWidth, string> = {
  narrow: "max-w-[720px]",
  wide: "max-w-[960px]",
  full: "max-w-none",
}

const TABLE_WIDTHS: Record<TableWidth, string> = {
  wide: "max-w-[1200px]",
  full: "max-w-none",
}

export function DocPage({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  const { recordWidth } = useLayoutWidths()
  return (
    <div
      data-slot="doc-page"
      data-width={recordWidth}
      className={cn(
        "w-full px-4 py-6 md:px-12 md:py-9",
        RECORD_WIDTHS[recordWidth],
        className
      )}
    >
      {children}
    </div>
  )
}

export function TablePage({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  const { tableWidth } = useLayoutWidths()
  return (
    <div
      data-slot="table-page"
      data-width={tableWidth}
      className={cn(
        "w-full px-4 py-6 md:px-8",
        TABLE_WIDTHS[tableWidth],
        className
      )}
    >
      {children}
    </div>
  )
}
