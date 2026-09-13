import type { ReactNode } from "react"

export interface GroupProps {
  title: ReactNode
  action?: { label: string; onPress(): void }
  children?: ReactNode
}

/** A titled section with an optional trailing button. */
export function Group({ title, action, children }: GroupProps) {
  return (
    <section className="kit-group">
      <div className="kit-group-head">
        <h2 className="kit-group-title">{title}</h2>
        {action && (
          <button
            type="button"
            className="kit-btn kit-group-action"
            onClick={action.onPress}
          >
            {action.label}
          </button>
        )}
      </div>
      {children}
    </section>
  )
}
