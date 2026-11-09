import { cx } from "@substrate/react"
import type { ButtonHTMLAttributes, ReactNode, Ref } from "react"

const ICON_BUTTON =
  "inline-flex size-9 shrink-0 items-center justify-center rounded-full text-secondary hover:bg-fill hover:text-label active:bg-selected disabled:opacity-40"

/** A small round button holding one icon, for a card's header or a
 * toolbar: its `label` is its accessible name and its hover text. With
 * `href` it is a link (a history screen). `active` marks it while its panel
 * is open or its settings differ from the defaults. */
export function IconButton({
  label,
  active,
  href,
  className,
  children,
  ref,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  label: string
  active?: boolean
  href?: string
  children: ReactNode
  ref?: Ref<HTMLButtonElement>
}) {
  const cls = cx(ICON_BUTTON, active && "bg-selected text-label", className)
  if (href)
    return (
      <a href={href} aria-label={label} title={label} className={cls}>
        {children}
      </a>
    )
  return (
    <button
      type="button"
      {...rest}
      ref={ref}
      aria-label={label}
      title={label}
      className={cls}
    >
      {children}
    </button>
  )
}

/** A quiet button of words in the accent, for an action inside a panel or
 * an empty state ("Clear filters", "Reset to defaults"). */
export function TextButton({
  className,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      {...rest}
      className={cx(
        "inline-flex min-h-11 items-center text-[15px] text-accent-text disabled:opacity-40",
        className
      )}
    />
  )
}
