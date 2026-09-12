/** The range control under a timeline's header: the active range as one
 * line (`12 Sep – 12 Oct`, with Reset while it overrides the window), a row
 * of quick-range chips that scrolls sideways rather than wrapping, and a
 * Custom chip that reveals two native date inputs prefilled from the active
 * range. Every target is 44 px tall on touch; the pill inside a chip is the
 * visible part. The choice is reported through `onChange` and nothing here
 * reads or writes the URL: the layout owns that. */

import { useState } from "react"
import { CalendarRangeIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import {
  activeQuickRange,
  parseDayKey,
  rangeLabel,
  QUICK_RANGES,
  type TimelineRange,
} from "./timeline-range"

function Chip({
  pressed,
  onClick,
  children,
}: {
  pressed: boolean
  onClick: () => void
  children: string
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className="flex h-11 shrink-0 items-center outline-none select-none [-webkit-touch-callout:none] md:h-9 focus-visible:[&>span]:ring-3 focus-visible:[&>span]:ring-ring/50"
    >
      <span
        className={cn(
          "flex h-8 items-center rounded-full border px-3 text-sm whitespace-nowrap transition-colors md:h-7 md:text-[0.8rem]",
          pressed
            ? "border-primary/30 bg-primary/10 font-medium text-primary"
            : "border-border bg-background text-foreground active:bg-muted"
        )}
      >
        {children}
      </span>
    </button>
  )
}

export function TimelineRangeBar({
  range,
  chosen,
  now,
  onChange,
}: {
  /** The range in force: the chosen one, else the view's window as days. */
  range: TimelineRange
  /** Whether `range` overrides the window; Reset shows only then. */
  chosen: boolean
  now: number
  /** A new range, or null to fall back to the window. */
  onChange: (range: TimelineRange | null) => void
}) {
  const quick = activeQuickRange(range, now)
  const customRange = chosen && quick === undefined
  // Open from the start when the address already carries a range no chip
  // names, so the days it names are on screen and editable.
  const [customOpen, setCustomOpen] = useState(customRange)

  const setDay = (side: "from" | "to", value: string) => {
    const key = parseDayKey(value)
    if (key && key !== range[side]) onChange({ ...range, [side]: key })
  }

  return (
    <div className="border-b bg-background">
      <div className="flex min-h-11 items-center gap-2 pr-2 pl-4">
        <CalendarRangeIcon
          aria-hidden
          className="size-4 shrink-0 text-muted-foreground"
        />
        <span
          aria-live="polite"
          className="min-w-0 flex-1 truncate text-sm font-medium tabular-nums"
        >
          {rangeLabel(range, now)}
        </span>
        {chosen && (
          <Button
            variant="ghost"
            size="sm"
            className="h-11 px-3 text-sm text-muted-foreground md:h-8"
            onClick={() => {
              setCustomOpen(false)
              onChange(null)
            }}
          >
            Reset
          </Button>
        )}
      </div>
      <div
        role="group"
        aria-label="Range"
        className="flex [scrollbar-width:none] gap-2 overflow-x-auto px-4 pb-1 [&::-webkit-scrollbar]:hidden"
      >
        {QUICK_RANGES.map((q) => (
          <Chip
            key={q.key}
            pressed={!customOpen && quick === q.key}
            onClick={() => {
              setCustomOpen(false)
              onChange(q.range(now))
            }}
          >
            {q.label}
          </Chip>
        ))}
        <Chip
          pressed={customOpen || customRange}
          onClick={() => setCustomOpen((o) => !o)}
        >
          Custom
        </Chip>
      </div>
      {customOpen && (
        <div className="flex items-end gap-3 px-4 pt-1 pb-3">
          <label className="flex min-w-0 flex-1 flex-col gap-1 text-xs text-muted-foreground">
            From
            <Input
              type="date"
              value={range.from}
              max={range.to}
              onChange={(e) => setDay("from", e.target.value)}
              className="h-11 w-full min-w-0 md:h-9"
            />
          </label>
          <label className="flex min-w-0 flex-1 flex-col gap-1 text-xs text-muted-foreground">
            To
            <Input
              type="date"
              value={range.to}
              min={range.from}
              onChange={(e) => setDay("to", e.target.value)}
              className="h-11 w-full min-w-0 md:h-9"
            />
          </label>
        </div>
      )}
    </div>
  )
}
