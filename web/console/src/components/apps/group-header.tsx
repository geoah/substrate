/** A section heading in a grouped list: sticky under the chrome, the group's
 * label and count, and a tap that folds the group. Touch height, no callout. */

import { ChevronDownIcon } from "lucide-react"

import { cn } from "@/lib/utils"

export function GroupHeader({
  label,
  count,
  collapsed,
  onToggle,
  className,
}: {
  label: string
  count: number
  collapsed: boolean
  onToggle: () => void
  className?: string
}) {
  return (
    <button
      type="button"
      aria-expanded={!collapsed}
      onClick={onToggle}
      className={cn(
        "sticky top-0 z-[1] flex min-h-10 w-full items-center gap-2 border-b bg-muted/60 px-4 text-left text-xs font-medium tracking-wide text-muted-foreground uppercase backdrop-blur select-none [-webkit-touch-callout:none]",
        className
      )}
    >
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="data text-[0.7rem] normal-case">{count}</span>
      <ChevronDownIcon
        className={cn(
          "size-3.5 transition-transform",
          collapsed && "-rotate-90"
        )}
      />
    </button>
  )
}
