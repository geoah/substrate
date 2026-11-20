import { BarButton, Sheet, cx } from "@substrate/react"
import { ListFilter } from "lucide-react"
import { useState, type ReactNode } from "react"
import type { DisplayControl, DisplayPage } from "../lib/display"
import { DisplaySettings } from "./DisplaySettings"

export interface MobileFiltersProps {
  page: DisplayPage
  control: DisplayControl
  /** How many secondary filters differ from their defaults ("Filters
   * 2"). */
  activeCount: number
  /** The filter controls, in order, each in a `MobileGroup`. */
  children: ReactNode
}

/** A phone's filters: a "Filters" button (with how many are set) opening a
 * sheet of the secondary filters, then the display settings. Filters apply
 * as they change; Done only closes the sheet. */
export function MobileFilters({
  page,
  control,
  activeCount,
  children,
}: MobileFiltersProps) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={activeCount ? `Filters, ${activeCount} set` : "Filters"}
        className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-full bg-fill px-3 text-[13px] font-medium hover:bg-selected"
      >
        <ListFilter className="size-4" aria-hidden />
        Filters
        {activeCount > 0 && (
          <span className="min-w-5 rounded-full bg-accent px-1.5 text-center text-[12px] leading-5 font-semibold text-white tabular-nums">
            {activeCount}
          </span>
        )}
      </button>
      <Sheet
        open={open}
        onClose={() => setOpen(false)}
        title="Filters"
        actions={
          <BarButton prominent onClick={() => setOpen(false)}>
            Done
          </BarButton>
        }
      >
        <div className="mb-7 flex flex-col gap-4">{children}</div>
        <h3 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
          Display
        </h3>
        <DisplaySettings page={page} control={control} variant="inline" />
      </Sheet>
    </>
  )
}

/** One filter control in the sheet, its label above it. */
export function MobileGroup({
  label,
  children,
  className,
}: {
  label: string
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cx("flex flex-col gap-1.5", className)}>
      <span className="px-1 text-[13px] font-medium text-secondary">
        {label}
      </span>
      {children}
    </div>
  )
}
