/** The pieces a settings page is made of: a titled section holding a box of
 * rows, each row a title and a sentence on the left and its control on the
 * right, and a segmented picker for a few named choices. */

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export function SettingsSection({
  title,
  hint,
  children,
  id,
}: {
  title: string
  hint?: string
  children: ReactNode
  id?: string
}) {
  return (
    <section id={id} aria-label={title} className="mt-8 first:mt-6">
      <div className="mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <h2 className="text-[15px] font-semibold tracking-[-0.01em]">
          {title}
        </h2>
        {hint && <span className="text-[12.5px] text-faint">{hint}</span>}
      </div>
      <div className="overflow-hidden rounded-[10px] border border-border">
        {children}
      </div>
    </section>
  )
}

export function SettingRow({
  title,
  description,
  control,
  children,
  className,
}: {
  title: ReactNode
  description?: ReactNode
  control?: ReactNode
  /** What opens under the row: a form, a table. */
  children?: ReactNode
  className?: string
}) {
  return (
    <div
      data-slot="setting-row"
      className={cn("border-b border-border last:border-b-0", className)}
    >
      <div className="grid grid-cols-1 items-center gap-3 px-4 py-3.5 sm:grid-cols-[minmax(0,1fr)_auto]">
        <div className="min-w-0">
          <div className="font-medium [overflow-wrap:anywhere]">{title}</div>
          {description && (
            <div className="mt-0.5 max-w-[60ch] text-[12.5px] text-faint">
              {description}
            </div>
          )}
        </div>
        {control && (
          <div className="flex flex-wrap items-center gap-2 sm:justify-end">
            {control}
          </div>
        )}
      </div>
      {children && <div className="px-4 pb-4">{children}</div>}
    </div>
  )
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
  disabled,
}: {
  value: T
  options: readonly { value: T; label: string }[]
  onChange: (value: T) => void
  label: string
  disabled?: boolean
}) {
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className="inline-flex gap-0.5 rounded-[7px] border border-border-strong p-0.5"
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          disabled={disabled}
          onClick={() => onChange(o.value)}
          className={cn(
            "cursor-pointer rounded-[5px] px-2.5 py-1 text-[12.5px] text-muted-foreground disabled:cursor-default",
            value === o.value && "bg-foreground text-background"
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/** A width choice drawn as a page: the bar is as wide as the page would be. */
export function WidthPicker<T extends string>({
  value,
  options,
  onChange,
  label,
  disabled,
}: {
  value: T
  options: readonly { value: T; label: string; fill: string }[]
  onChange: (value: T) => void
  label: string
  disabled?: boolean
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex gap-2">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          aria-label={o.label}
          disabled={disabled}
          onClick={() => onChange(o.value)}
          className={cn(
            "flex w-[86px] cursor-pointer flex-col items-center gap-1.5 rounded-lg border border-border-strong bg-background p-2 text-xs text-muted-foreground disabled:cursor-default",
            value === o.value &&
              "border-primary text-foreground shadow-[0_0_0_3px_var(--primary-soft)]"
          )}
        >
          <span
            aria-hidden
            className="flex h-10 w-16 gap-[3px] rounded border border-border-strong bg-panel p-1"
          >
            <i
              className="block h-full rounded-[2px] bg-border-strong"
              style={{ width: o.fill }}
            />
          </span>
          {o.label}
        </button>
      ))}
    </div>
  )
}
