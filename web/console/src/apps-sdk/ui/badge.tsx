import type { ReactNode } from "react"

export type BadgeTone = "default" | "muted" | "warning" | "destructive"

export function Badge({
  children,
  tone = "default",
}: {
  children: ReactNode
  tone?: BadgeTone
}) {
  const cls = tone === "default" ? "kit-badge" : `kit-badge kit-badge--${tone}`
  return <span className={cls}>{children}</span>
}

/** A state as the console draws it: an outline badge, a dot, the declared
 * name in the data casing. The dot is muted while the machine sits at its
 * initial state and primary once it has moved; `initial` is the app's to
 * pass (from `useKind`), and without it every state reads as moved. */
export function StateBadge({
  value,
  initial,
}: {
  value: string
  initial?: string
}) {
  const atInitial = initial !== undefined && value === initial
  return (
    <span className={atInitial ? "kit-badge kit-badge--initial" : "kit-badge"}>
      <span className="kit-badge-dot" aria-hidden />
      <span className="kit-badge-data">{value}</span>
    </span>
  )
}
