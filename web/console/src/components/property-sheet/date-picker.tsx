/** A date, or a date and a time, picked from a calendar that drops from the
 * value: the month as a grid the arrows walk (Page Up and Down change month,
 * Shift for a year), today marked, and for a datetime a time box that reads
 * "9:30", "0930" or "9pm". A `date` saves on the day's click; a datetime saves
 * on Save, Enter in the time box, or a click outside once something changed.
 * Esc leaves without a write. The stored spelling is what the native control
 * wrote: `YYYY-MM-DD` for a date, an RFC 3339 instant for a datetime. */

import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from "react"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import {
  dayKey,
  dayName,
  dayOf,
  addMonths,
  monthGrid,
  monthTitle,
  moveDay,
  parseDay,
  parseTime,
  type Day,
} from "./calendar"
import { friendlyCalendarDay, friendlyDateTime, fromLocalInput } from "./dates"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"

const WEEKDAYS = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"]

const pad = (n: number) => String(n).padStart(2, "0")

/** The stored value as the picker's day and time. */
function seed(value: unknown, withTime: boolean): { day?: Day; time: string } {
  if (typeof value !== "string" || !value) return { time: "" }
  if (!withTime) return { day: parseDay(value.slice(0, 10)), time: "" }
  const t = Date.parse(value)
  if (Number.isNaN(t)) return { time: "" }
  const d = new Date(t)
  const midnight = d.getHours() === 0 && d.getMinutes() === 0
  return {
    day: dayOf(d),
    time: midnight ? "" : `${pad(d.getHours())}:${pad(d.getMinutes())}`,
  }
}

export interface DatePickerProps {
  /** The property's label, for the grid's accessible name. */
  label: string
  /** The stored value. */
  value: unknown
  /** A datetime: the picker carries a time and saves on Save. */
  withTime: boolean
  /** A required property offers no Clear. */
  required?: boolean
  pending?: boolean
  /** The value to write: the stored spelling, or "" to clear. */
  onSave: (next: string) => void
  onCancel: () => void
}

export function DatePicker({
  label,
  value,
  withTime,
  required,
  pending,
  onSave,
  onCancel,
}: DatePickerProps) {
  const initial = useMemo(() => seed(value, withTime), [value, withTime])
  const today = dayOf(new Date())
  const [selected, setSelected] = useState<Day | undefined>(initial.day)
  const [focused, setFocused] = useState<Day>(initial.day ?? today)
  const [time, setTime] = useState(initial.time)
  const [timeError, setTimeError] = useState(false)
  const errorId = useId()
  const grid = useRef<HTMLDivElement>(null)
  const start = useRef<HTMLButtonElement>(null)
  const weeks = monthGrid(focused.year, focused.month)
  const focusedKey = dayKey(focused)

  // The arrows move focus with the highlighted day, but only while the grid
  // holds focus: a month step from the header's buttons keeps it there.
  useEffect(() => {
    const root = grid.current
    if (!root?.contains(document.activeElement)) return
    root.querySelector<HTMLElement>(`[data-day="${focusedKey}"]`)?.focus()
  }, [focusedKey])

  /** The instant to write for a datetime, or why there is none. */
  function draft(): { next?: string; invalid?: boolean } {
    if (!selected) return {}
    const t = parseTime(time)
    if (t === undefined) return { invalid: true }
    return { next: fromLocalInput(`${dayKey(selected)}T${t || "00:00"}`) }
  }

  function changed(): boolean {
    const a = initial.day ? dayKey(initial.day) : ""
    const b = selected ? dayKey(selected) : ""
    return a !== b || (parseTime(time) ?? time) !== initial.time
  }

  function commit() {
    if (!changed()) return onCancel()
    const { next, invalid } = draft()
    if (invalid) {
      setTimeError(true)
      return
    }
    onSave(next ?? "")
  }

  function pick(day: Day) {
    setFocused(day)
    if (!withTime) {
      if (initial.day && dayKey(initial.day) === dayKey(day)) onCancel()
      else onSave(dayKey(day))
      return
    }
    setSelected(day)
  }

  function onGridKey(e: KeyboardEvent) {
    const next = moveDay(focused, e.key, e.shiftKey)
    if (!next) return
    e.preventDefault()
    setFocused(next)
  }

  const typed = withTime ? draft().next : undefined
  const shown = selected
    ? typed
      ? friendlyDateTime(typed)
      : friendlyCalendarDay(dayKey(selected))
    : "Pick a date"

  return (
    <Popover
      open
      onOpenChange={(open, details) => {
        if (open) return
        if (withTime && details.reason === "outside-press") commit()
        else onCancel()
      }}
    >
      <PopoverTrigger
        nativeButton={false}
        render={
          <span
            data-slot="date-value"
            className="min-w-0 rounded-md border border-primary bg-background px-2 py-1 text-sm ring-3 ring-primary-soft"
          />
        }
      >
        {shown}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        initialFocus={start}
        className="w-[264px] max-w-[calc(100vw-2rem)] gap-2 p-2.5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Previous month"
            onClick={() => setFocused(addMonths(focused, -1))}
          >
            <ChevronLeftIcon />
          </Button>
          <span aria-live="polite" className="text-[13px] font-medium">
            {monthTitle(focused.year, focused.month)}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Next month"
            onClick={() => setFocused(addMonths(focused, 1))}
          >
            <ChevronRightIcon />
          </Button>
        </div>
        <div
          ref={grid}
          role="grid"
          aria-label={label}
          onKeyDown={onGridKey}
          className="flex flex-col gap-0.5"
        >
          <div role="row" className="grid grid-cols-7">
            {WEEKDAYS.map((w) => (
              <span
                key={w}
                role="columnheader"
                className="grid h-6 place-items-center text-[11.5px] text-faint"
              >
                {w}
              </span>
            ))}
          </div>
          {weeks.map((week) => (
            <div key={dayKey(week[0])} role="row" className="grid grid-cols-7">
              {week.map((day) => {
                const key = dayKey(day)
                const isFocus = key === focusedKey
                const isSelected = selected && key === dayKey(selected)
                const outside = day.month !== focused.month
                return (
                  <span key={key} role="gridcell" aria-selected={isSelected}>
                    <button
                      ref={isFocus ? start : undefined}
                      type="button"
                      data-day={key}
                      tabIndex={isFocus ? 0 : -1}
                      aria-label={dayName(day)}
                      aria-current={key === dayKey(today) ? "date" : undefined}
                      disabled={pending}
                      onClick={() => pick(day)}
                      className={cn(
                        "grid size-8 w-full place-items-center rounded-md text-[13px] tabular-nums outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring",
                        outside && "text-faint",
                        key === dayKey(today) &&
                          !isSelected &&
                          "font-semibold text-primary-text",
                        isSelected &&
                          "bg-primary text-primary-foreground hover:bg-primary"
                      )}
                    >
                      {day.day}
                    </button>
                  </span>
                )
              })}
            </div>
          ))}
        </div>
        {withTime && (
          <label className="flex items-center gap-2 border-t pt-2 text-[13px] text-muted-foreground">
            Time
            <input
              value={time}
              placeholder="09:00"
              inputMode="numeric"
              aria-invalid={timeError || undefined}
              aria-describedby={timeError ? errorId : undefined}
              disabled={pending}
              onChange={(e) => {
                setTime(e.target.value)
                setTimeError(false)
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault()
                  commit()
                }
              }}
              className={cn(
                "h-7 w-20 rounded-md border border-input bg-background px-2 text-sm text-foreground tabular-nums outline-none focus-visible:border-primary focus-visible:ring-3 focus-visible:ring-primary-soft",
                timeError && "border-destructive"
              )}
            />
            {timeError && (
              <span id={errorId} className="text-xs text-destructive">
                Type a time like 09:30
              </span>
            )}
          </label>
        )}
        <div className="flex items-center gap-1.5 border-t pt-2">
          {!required && (initial.day || selected) && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={pending}
              onClick={() => onSave("")}
            >
              Clear
            </Button>
          )}
          {!withTime && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={pending}
              onClick={() => pick(today)}
            >
              Today
            </Button>
          )}
          <span className="ml-auto flex items-center gap-1.5">
            {pending && <Spinner className="size-3.5" />}
            {withTime && (
              <Button
                type="button"
                size="sm"
                disabled={pending || !selected}
                onClick={commit}
              >
                Save
              </Button>
            )}
          </span>
        </div>
      </PopoverContent>
    </Popover>
  )
}
