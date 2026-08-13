/** A bundle's lifecycle stance as the guide's status voice: outline Badge,
 * tiny colored dot, the state name in the data casing. Semantic tokens only:
 * enabled is primary, disabled/uninstalled recede to muted. Setup is NOT a
 * lifecycle state; it rides beside the badge as the SetupBadge chip, and the
 * two are never collapsed into one signal. */

import { TriangleAlertIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import type { BundleState } from "@/lib/api/bundles"
import { cn } from "@/lib/utils"

const DOT: Record<BundleState, string> = {
  enabled: "bg-primary",
  disabled: "bg-muted-foreground/50",
  uninstalled: "bg-muted-foreground/40",
  // Quarantined wants a person: the stored closure failed admission and a
  // re-install clears it, so it wears the warning tone.
  quarantined: "bg-warning",
}

export function BundleStateBadge({ state }: { state: BundleState }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1.5 font-normal",
        state === "quarantined" && "text-warning"
      )}
    >
      <span className={cn("size-1.5 rounded-full", DOT[state])} />
      <span className="data">{state}</span>
    </Badge>
  )
}

/** The setup chip: warning-toned, "N setup steps", shown ONLY while steps
 * stand. It rides beside the lifecycle badge as a separate signal (a bundle is
 * enabled AND has setup steps; neither claim hides the other). */
export function SetupBadge({ count }: { count: number }) {
  if (count <= 0) return null
  return (
    <Badge
      variant="outline"
      className="gap-1 border-warning/40 font-normal text-warning"
    >
      <TriangleAlertIcon className="size-3 shrink-0" />
      <span className="data">
        {count === 1 ? "1 setup step" : `${count} setup steps`}
      </span>
    </Badge>
  )
}
