import { cx } from "@substrate/react"
import type { HTMLAttributes } from "react"
import { CHIP, DOT, type Tone } from "./tone"

export interface BadgeProps extends Omit<
  HTMLAttributes<HTMLSpanElement>,
  "title"
> {
  /** The word: "High", "Overdue", "Slipping". */
  label: string
  /** Its colour. Default `neutral`. Status is never colour alone: the word
   * says it. */
  tone?: Tone
  /** A sentence on hover saying what the word means. */
  title?: string
  /** A quiet label with no tone ("Important", a panel's name), in the
   * secondary colour on no fill. */
  isMono?: boolean
}

/** A short status word on a tinted chip: a card's status, a finding's
 * word, a flag in a table. */
export function Badge({
  label,
  tone = "neutral",
  title,
  isMono,
  className,
  ...rest
}: BadgeProps) {
  return (
    <span
      {...rest}
      title={title}
      className={cx(
        "inline-block h-5 max-w-full shrink-0 truncate rounded-full align-middle text-[12px] leading-5 font-medium",
        isMono ? "px-0 font-normal text-secondary" : cx("px-2", CHIP[tone]),
        className
      )}
    >
      {label}
    </span>
  )
}

export type StatusLevel = "neutral" | "success" | "warning" | "danger"

/** The compact status mark for a dense row: a toned dot whose `label` is
 * its accessible name and its hover text, so the colour is never alone. */
export function StatusDot({
  level,
  label,
  className,
  ...rest
}: Omit<HTMLAttributes<HTMLSpanElement>, "title"> & {
  level: StatusLevel
  label: string
}) {
  return (
    <span
      {...rest}
      role="img"
      title={label}
      aria-label={label}
      className={cx(
        "inline-block size-2 shrink-0 rounded-full",
        DOT[level],
        className
      )}
    />
  )
}
