/** An on/off switch: a pill with a sliding knob, `role="switch"`. */

import { cn } from "@/lib/utils"

/** The switch's look alone, for a control that carries the state itself (a
 * menu's checkbox item). */
export function SwitchMark({
  checked,
  className,
}: {
  checked: boolean
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cn(
        "relative block h-[18px] w-[30px] shrink-0 rounded-full transition-colors",
        checked ? "bg-primary" : "bg-border-strong",
        className
      )}
    >
      <span
        className={cn(
          "absolute top-[2px] size-[14px] rounded-full bg-background shadow-[0_1px_2px_rgba(0,0,0,0.2)] transition-[left] duration-150",
          checked ? "left-[14px]" : "left-[2px]"
        )}
      />
    </span>
  )
}

export function ToggleSwitch({
  checked,
  onChange,
  label,
  disabled,
  className,
}: {
  checked: boolean
  onChange: (next: boolean) => void
  /** The accessible name; the visible label sits beside the switch. */
  label: string
  disabled?: boolean
  className?: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        "shrink-0 cursor-pointer rounded-full border-0 p-0 outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-default disabled:opacity-50",
        className
      )}
    >
      <SwitchMark checked={checked} />
    </button>
  )
}
