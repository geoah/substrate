/** A bundle's lifecycle stance as the guide's status voice: outline Badge,
 * tiny colored dot, the state name in the data casing. Semantic tokens only
 * — needs-configuration wears the warning token (it wants a person), enabled
 * is primary, disabled/uninstalled recede to muted. */

import { Badge } from "@/components/ui/badge"
import type { BundleState } from "@/lib/api/bundles"
import { cn } from "@/lib/utils"

const DOT: Record<BundleState, string> = {
  enabled: "bg-primary",
  "needs-configuration": "bg-warning",
  disabled: "bg-muted-foreground/50",
  uninstalled: "bg-muted-foreground/40",
}

const LABEL: Record<BundleState, string> = {
  enabled: "enabled",
  "needs-configuration": "needs configuration",
  disabled: "disabled",
  uninstalled: "uninstalled",
}

export function BundleStateBadge({ state }: { state: BundleState }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1.5 font-normal",
        state === "needs-configuration" && "text-warning"
      )}
    >
      <span className={cn("size-1.5 rounded-full", DOT[state])} />
      <span className="data">{LABEL[state]}</span>
    </Badge>
  )
}
