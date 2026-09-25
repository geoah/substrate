/** An on/off switch: a pill with a sliding knob, `role="switch"`. */

import { cn } from "@/lib/utils"

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
        "relative h-[18px] w-[30px] shrink-0 cursor-pointer rounded-full border-0 p-0 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-default disabled:opacity-50",
        checked ? "bg-primary" : "bg-border-strong",
        className
      )}
    >
      <span
        aria-hidden
        className={cn(
          "absolute top-[2px] size-[14px] rounded-full bg-background shadow-[0_1px_2px_rgba(0,0,0,0.2)] transition-[left] duration-150",
          checked ? "left-[14px]" : "left-[2px]"
        )}
      />
    </button>
  )
}
