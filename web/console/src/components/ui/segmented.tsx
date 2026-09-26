/** One choice among a few, shown all at once: a radio group. Only the chosen
 * option is in the tab order, and the arrow keys, Home and End move the
 * choice along the row, as a radio group does. */

import type { KeyboardEvent, ReactNode } from "react"

import { cn } from "@/lib/utils"

const STEP: Record<string, number> = {
  ArrowRight: 1,
  ArrowDown: 1,
  ArrowLeft: -1,
  ArrowUp: -1,
}

/** The radio group's keys for any row of `role="radio"` items, in the order
 * `values` lists them: chooses the next value and moves focus onto it. */
export function radioKeys<T>(
  values: readonly T[],
  value: T,
  onChange: (value: T) => void
) {
  return (event: KeyboardEvent<HTMLElement>) => {
    const count = values.length
    if (!count) return
    const at = values.indexOf(value)
    let next: number
    if (event.key in STEP) {
      next = at < 0 ? 0 : (at + STEP[event.key] + count) % count
    } else if (event.key === "Home") next = 0
    else if (event.key === "End") next = count - 1
    else return
    event.preventDefault()
    if (next !== at) onChange(values[next])
    event.currentTarget
      .querySelectorAll<HTMLElement>('[role="radio"]')
      [next]?.focus()
  }
}

/** Where the tab stop sits: the chosen option, else the first. */
export function radioTabIndex<T>(
  values: readonly T[],
  value: T,
  option: T
): 0 | -1 {
  const chosen = values.includes(value) ? value : values[0]
  return option === chosen ? 0 : -1
}

export interface SegmentedOption<T extends string> {
  value: T
  label: ReactNode
}

const LOOK = {
  /** A boxed row; the choice is filled in. */
  boxed: {
    group:
      "inline-flex gap-0.5 rounded-[7px] border border-border-strong p-0.5",
    item: "rounded-[5px] px-2.5 py-1 text-[12.5px]",
    chosen: "bg-foreground text-background",
  },
  /** Words in a row, for a filter above a list; the choice is tinted. */
  plain: {
    group: "flex flex-wrap gap-0.5",
    item: "rounded-md px-2 py-1 text-[13px] hover:text-foreground",
    chosen: "bg-hover font-medium text-foreground",
  },
} as const

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
  disabled,
  look = "boxed",
  className,
}: {
  value: T
  options: readonly SegmentedOption<T>[]
  onChange: (value: T) => void
  /** The group's accessible name: "Rank by". */
  label: string
  disabled?: boolean
  look?: keyof typeof LOOK
  className?: string
}) {
  const values = options.map((o) => o.value)
  const style = LOOK[look]
  return (
    <div
      role="radiogroup"
      aria-label={label}
      aria-disabled={disabled || undefined}
      data-slot="segmented"
      className={cn(style.group, className)}
      onKeyDown={disabled ? undefined : radioKeys(values, value, onChange)}
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          tabIndex={radioTabIndex(values, value, o.value)}
          disabled={disabled}
          onClick={() => onChange(o.value)}
          className={cn(
            "cursor-pointer text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-default",
            style.item,
            value === o.value && style.chosen
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
