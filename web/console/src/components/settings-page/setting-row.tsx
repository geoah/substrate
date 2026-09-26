/** The pieces a settings page is made of: a titled section holding a box of
 * rows, each row a title and a sentence on the left and its control on the
 * right, and a width picker drawn as pages. */

import type { ReactNode } from "react"

import { SectionHead } from "@/components/identity/section-head"
import { radioKeys, radioTabIndex } from "@/components/ui/segmented"
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
      <SectionHead title={title} hint={hint} className="mt-0" />
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
  const values = options.map((o) => o.value)
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className="flex gap-2"
      onKeyDown={disabled ? undefined : radioKeys(values, value, onChange)}
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          tabIndex={radioTabIndex(values, value, o.value)}
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
