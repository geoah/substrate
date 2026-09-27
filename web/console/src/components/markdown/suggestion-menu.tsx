/** The popup a suggestion opens at the cursor: a list of rows, grouped, that
 * the arrow keys walk and Enter or Tab picks, while typing stays in the
 * editor. The suggestion plugin owns the keys and forwards them through
 * `keysRef`, so the list never takes focus. */

import { useEffect, useRef, useState, type ReactNode } from "react"

import { cn } from "@/lib/utils"

export interface MenuRow {
  key: string
  group: string
  icon: ReactNode
  label: ReactNode
  /** Beside the label, quieter: a shortcut, a collection name. */
  hint?: ReactNode
  pick: () => void
}

/** Where the plugin hands a key to the open list: true when the list used it. */
export interface MenuKeys {
  current: ((event: KeyboardEvent) => boolean) | null
}

export function SuggestionMenu({
  label,
  rows,
  keysRef,
  empty,
  busy = false,
}: {
  label: string
  rows: MenuRow[]
  keysRef: MenuKeys
  empty: ReactNode
  busy?: boolean
}) {
  const [active, setActive] = useState(0)
  // A new set of rows starts from its first one.
  const signature = rows.map((r) => r.key).join("\n")
  const [shownSignature, setShownSignature] = useState(signature)
  if (shownSignature !== signature) {
    setShownSignature(signature)
    setActive(0)
  }
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    keysRef.current = (event) => {
      if (!rows.length) return false
      if (event.key === "ArrowDown") {
        setActive((i) => (i + 1) % rows.length)
        return true
      }
      if (event.key === "ArrowUp") {
        setActive((i) => (i - 1 + rows.length) % rows.length)
        return true
      }
      if (event.key === "Enter" || event.key === "Tab") {
        rows[Math.min(active, rows.length - 1)]?.pick()
        return true
      }
      return false
    }
    return () => {
      keysRef.current = null
    }
  }, [keysRef, rows, active])

  useEffect(() => {
    list.current
      ?.querySelector(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: "nearest" })
  }, [active])

  return (
    <div
      ref={list}
      role="listbox"
      aria-label={label}
      aria-busy={busy}
      className="z-50 no-scrollbar max-h-80 w-72 overflow-y-auto rounded-lg border border-border bg-popover p-1 text-sm text-popover-foreground shadow-lg"
    >
      {rows.length === 0 && (
        <div className="px-2 py-1.5 text-[13px] text-faint">{empty}</div>
      )}
      {rows.map((row, i) => {
        const heading =
          i === 0 || rows[i - 1].group !== row.group ? row.group : undefined
        return (
          <div key={row.key}>
            {heading && (
              <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground first:pt-1">
                {heading}
              </div>
            )}
            <div
              role="option"
              aria-selected={i === active}
              data-index={i}
              // The editor keeps focus: a press here must not blur it.
              onMouseDown={(e) => e.preventDefault()}
              onMouseEnter={() => setActive(i)}
              onClick={row.pick}
              className={cn(
                "flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 select-none [&_svg]:size-4 [&_svg]:shrink-0",
                i === active && "bg-muted text-foreground"
              )}
            >
              <span className="flex shrink-0 items-center text-muted-foreground">
                {row.icon}
              </span>
              <span className="min-w-0 flex-1 truncate">{row.label}</span>
              {row.hint && (
                <span className="shrink-0 pl-2 text-[12px] text-faint">
                  {row.hint}
                </span>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}
