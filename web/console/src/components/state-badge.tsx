/** Record state as the guide's status voice: outline Badge, tiny dot, the
 * declared state name in the data casing. Semantic tokens only — the dot is
 * muted while the machine sits at its initial state and primary once it has
 * moved, which distinguishes without inventing a palette. */

import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export function StateBadge({
  value,
  initial,
}: {
  value: string
  initial?: string
}) {
  const atInitial = initial !== undefined && value === initial
  return (
    <Badge variant="outline" className="gap-1.5 font-normal">
      <span
        className={cn(
          "size-1.5 rounded-full",
          atInitial ? "bg-muted-foreground/50" : "bg-primary"
        )}
      />
      <span className="data">{value}</span>
    </Badge>
  )
}
