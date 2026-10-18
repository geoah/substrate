/** The timeline's range: presets as rows (today, the last 7 and 30 days, 6
 * months, a year) and a custom range of whole days, each end picked on the
 * property sheet's own calendar. A range a chart zoomed into reads as its
 * window (`rangeWords`). */

import { useState } from "react"
import { CalendarRangeIcon, ChevronDownIcon } from "lucide-react"

import { DatePicker } from "@/components/property-sheet/date-picker"
import { Button } from "@/components/ui/button"
import { ChoiceList } from "@/components/ui/choice-list"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  dayKeyOf,
  dayRange,
  dayWords,
  isRangePreset,
  RANGE_PRESETS,
  rangeWords,
  type RangePreset,
  type TimeRange,
} from "@/lib/observations-time"

export function RangePicker({
  preset,
  range,
  onPreset,
  onCustom,
}: {
  /** The preset in force, or `custom`. */
  preset: RangePreset | "custom"
  range: TimeRange
  onPreset: (preset: RangePreset) => void
  onCustom: (range: TimeRange) => void
}) {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<"start" | "end" | null>(null)
  const label =
    preset === "custom"
      ? rangeWords(range)
      : (RANGE_PRESETS.find((p) => p.value === preset)?.label ?? preset)
  const firstDay = dayKeyOf(range.start)
  const lastDay = dayKeyOf(range.end - 1)

  function pickDay(which: "start" | "end", day: string) {
    setEditing(null)
    if (!day) return
    const next =
      which === "start" ? dayRange(day, lastDay) : dayRange(firstDay, day)
    if (next) onCustom(next)
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-[12.5px] text-faint">Range</span>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              className="h-7 gap-1.5 text-[12.5px] font-normal"
            />
          }
        >
          <CalendarRangeIcon className="size-3.5 text-muted-foreground" />
          {label}
          <ChevronDownIcon className="size-3.5 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-60 p-0">
          <ChoiceList
            label="Range"
            options={[
              ...RANGE_PRESETS,
              { value: "custom", label: "Custom range" },
            ]}
            selected={[preset]}
            onChange={([next]) => {
              setOpen(false)
              if (isRangePreset(next)) onPreset(next)
              else if (next === "custom") onCustom(range)
            }}
          />
        </PopoverContent>
      </Popover>
      {preset === "custom" && (
        <>
          <span className="text-[12.5px] text-faint">from</span>
          {editing === "start" ? (
            <DatePicker
              label="First day"
              value={firstDay}
              withTime={false}
              required
              onSave={(day) => pickDay("start", day)}
              onCancel={() => setEditing(null)}
            />
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-[12.5px] font-normal"
              aria-label={`First day, ${dayWords(range.start, true)}`}
              onClick={() => setEditing("start")}
            >
              {dayWords(range.start, true)}
            </Button>
          )}
          <span className="text-[12.5px] text-faint">to</span>
          {editing === "end" ? (
            <DatePicker
              label="Last day"
              value={lastDay}
              withTime={false}
              required
              onSave={(day) => pickDay("end", day)}
              onCancel={() => setEditing(null)}
            />
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-[12.5px] font-normal"
              aria-label={`Last day, ${dayWords(range.end - 1, true)}`}
              onClick={() => setEditing("end")}
            >
              {dayWords(range.end - 1, true)}
            </Button>
          )}
        </>
      )}
    </div>
  )
}
