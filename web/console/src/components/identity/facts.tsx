/** Labelled facts, one per row: a label column and a value column under a
 * rule. The agent page and the Settings rows that sit beside it share it, so
 * their columns line up. */

import type { ReactNode } from "react"

export function Facts({ rows }: { rows: Array<[string, ReactNode]> }) {
  return (
    <dl className="grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-3 border-t sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]">
      {rows.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="py-2.5 text-[13.5px] text-muted-foreground">
            {label}
          </dt>
          <dd className="min-w-0 py-2.5 text-[13.5px]">{value}</dd>
        </div>
      ))}
    </dl>
  )
}
