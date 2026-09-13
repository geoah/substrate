import type { ReactNode } from "react"

export interface EmptyProps {
  title: ReactNode
  description?: ReactNode
  action?: { label: string; onPress(): void }
}

export function Empty({ title, description, action }: EmptyProps) {
  return (
    <div className="kit-empty" role="status">
      <p className="kit-empty-title">{title}</p>
      {description !== undefined && description !== null && (
        <p className="kit-empty-desc">{description}</p>
      )}
      {action && (
        <button
          type="button"
          className="kit-btn kit-empty-action"
          onClick={action.onPress}
        >
          {action.label}
        </button>
      )}
    </div>
  )
}
