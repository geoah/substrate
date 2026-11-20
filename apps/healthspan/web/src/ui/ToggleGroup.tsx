import { cx } from "@substrate/react"
import { useRef, type HTMLAttributes, type KeyboardEvent } from "react"

export interface ToggleGroupOption<T extends string> {
  value: T
  label: string
  /** A count after the label ("Overdue 3"). */
  count?: number
  isDisabled?: boolean
}

export interface ToggleGroupProps<T extends string> extends Omit<
  HTMLAttributes<HTMLDivElement>,
  "onChange" | "defaultValue" | "role"
> {
  /** The group's accessible name ("Range", "Status"). */
  label: string
  options: ToggleGroupOption<T>[]
  /** The selected values; exactly one when `isExclusive`. */
  value: T[]
  /** The new selection, in option order. */
  onChange: (value: T[]) => void
  /** A segmented control: one strip, exactly one option selected, a radio
   * group (arrow keys move and select, Home and End jump). Off: separate
   * buttons pressed or not, any number at once ("Out of range" and "Off
   * target" together). */
  isExclusive?: boolean
  /** `md` 32 px, `sm` 28 px. Default `md`. */
  size?: "sm" | "md"
}

/** A row of toggles for a short choice whose options should all show: a
 * screen's range (1Y, 2Y, 5Y, All) and scope (Important, All) as a
 * segmented control, its status filters as buttons that combine. The row
 * wraps on a narrow screen. */
export function ToggleGroup<T extends string>({
  label,
  options,
  value,
  onChange,
  isExclusive,
  size = "md",
  className,
  onKeyDown,
  ...rest
}: ToggleGroupProps<T>) {
  const ref = useRef<HTMLDivElement>(null)
  const selected = new Set(value)
  const inOrder = (set: Set<T>) =>
    options.map((o) => o.value).filter((v) => set.has(v))
  const press = (v: T) => {
    if (isExclusive) {
      if (selected.has(v) && value.length === 1) return
      onChange([v])
      return
    }
    const next = new Set(selected)
    if (next.has(v)) next.delete(v)
    else next.add(v)
    onChange(inOrder(next))
  }
  // A segmented control has one tab stop, on the selected option (or the
  // first enabled one while nothing is selected).
  const enabled = options.filter((o) => !o.isDisabled)
  const tabStop = isExclusive
    ? (
        options.find((o) => selected.has(o.value) && !o.isDisabled) ??
        enabled[0]
      )?.value
    : undefined
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    onKeyDown?.(e)
    if (e.defaultPrevented || !isExclusive || !enabled.length) return
    const keys = [
      "ArrowRight",
      "ArrowDown",
      "ArrowLeft",
      "ArrowUp",
      "Home",
      "End",
    ]
    if (!keys.includes(e.key)) return
    e.preventDefault()
    const at = Math.max(
      0,
      enabled.findIndex((o) => o.value === tabStop)
    )
    const n = enabled.length
    const i =
      e.key === "Home"
        ? 0
        : e.key === "End"
          ? n - 1
          : e.key === "ArrowRight" || e.key === "ArrowDown"
            ? (at + 1) % n
            : (at - 1 + n) % n
    const target = enabled[i]
    if (!target) return
    onChange([target.value])
    ref.current
      ?.querySelector<HTMLElement>(`[data-value="${CSS.escape(target.value)}"]`)
      ?.focus()
  }
  const h = size === "sm" ? "h-7" : "h-8"
  return (
    <div
      {...rest}
      ref={ref}
      role={isExclusive ? "radiogroup" : "group"}
      aria-label={label}
      onKeyDown={onKey}
      className={cx(
        isExclusive
          ? "inline-flex max-w-full flex-wrap rounded-[9px] bg-fill p-0.5"
          : "inline-flex max-w-full flex-wrap gap-1.5",
        className
      )}
    >
      {options.map((o) => {
        const on = selected.has(o.value)
        return (
          <button
            key={o.value}
            type="button"
            data-value={o.value}
            disabled={o.isDisabled}
            {...(isExclusive
              ? {
                  role: "radio",
                  "aria-checked": on,
                  tabIndex: o.value === tabStop ? 0 : -1,
                }
              : { "aria-pressed": on })}
            onClick={() => press(o.value)}
            className={cx(
              "inline-flex shrink-0 items-center gap-1.5 px-3 text-[13px] font-medium whitespace-nowrap disabled:opacity-40",
              h,
              isExclusive
                ? cx(
                    size === "sm" ? "rounded-[6px]" : "rounded-[7px]",
                    on
                      ? "bg-segment text-label shadow-[0_1px_3px_rgb(0_0_0/0.12)]"
                      : "text-label hover:bg-selected"
                  )
                : cx(
                    "rounded-full",
                    on
                      ? "bg-accent/15 text-accent-text ring-1 ring-accent/40"
                      : "bg-fill text-label hover:bg-selected"
                  )
            )}
          >
            {o.label}
            {o.count !== undefined && (
              <span
                className={cx(
                  "tabular-nums",
                  on && !isExclusive ? "text-accent-text" : "text-secondary"
                )}
              >
                {o.count.toLocaleString("en-GB")}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}

/** A segmented control over one value: `ToggleGroup` with `isExclusive`,
 * for a screen's range or scope. */
export function SegmentedControl<T extends string>(
  props: Omit<ToggleGroupProps<T>, "value" | "onChange" | "isExclusive"> & {
    value: T
    onChange: (value: T) => void
  }
) {
  const { value, onChange, ...rest } = props
  return (
    <ToggleGroup
      {...rest}
      isExclusive
      value={[value]}
      onChange={([v]) => {
        if (v !== undefined) onChange(v)
      }}
    />
  )
}
