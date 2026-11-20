import { cx } from "@substrate/react"
import { TextButton } from "./IconButton"

export interface EmptyStateProps {
  /** A short title that guides ("Nothing needs attention"), not one that
   * only says nothing is there. */
  title: string
  /** One line saying what to do next. */
  description?: string
  /** One action under it. */
  action?: { label: string; onClick: () => void }
  /** `default`: a block standing in for a section or a screen. `compact`:
   * one line, for a table's or a chart's empty slot. */
  variant?: "default" | "compact"
  className?: string
}

/** What a screen, a section or a list shows when it has nothing. */
export function EmptyState({
  title,
  description,
  action,
  variant = "default",
  className,
}: EmptyStateProps) {
  if (variant === "compact")
    return (
      <div className={cx("py-2 text-[15px] text-secondary", className)}>
        <p>{title}</p>
        {action && (
          <TextButton onClick={action.onClick}>{action.label}</TextButton>
        )}
      </div>
    )
  return (
    <div className={cx("px-4 py-12 text-center", className)}>
      <p className="text-[20px] font-semibold">{title}</p>
      {description && (
        <p className="mt-1 text-[15px] text-secondary">{description}</p>
      )}
      {action && (
        <TextButton className="mt-2" onClick={action.onClick}>
          {action.label}
        </TextButton>
      )}
    </div>
  )
}
