import { ActorChip } from "@/components/actor-chip"
import type { ChangeRow } from "@/lib/api/types"
import { changeSources } from "@/lib/changelog"

export function ChangeActor({ row }: { row: ChangeRow }) {
  const sources = changeSources(row)
  const initiated = row.payload?.triggeredBy
  const mapped = row.payload?.mechanism === "mapping" || sources.length > 0
  if (row.actor !== "substrate") return <ActorChip actor={row.actor} />
  if (!mapped)
    return (
      <div className="space-y-1">
        <ActorChip actor={row.actor} />
        <p className="text-xs text-muted-foreground">
          System write; cause not recorded.
        </p>
      </div>
    )
  const sameSource = sources.length === 1 && sources[0] === initiated
  return (
    <div className="flex min-w-0 flex-col items-start gap-2 py-1">
      {sources.map((actor) => (
        <ActorChip key={actor} actor={actor} />
      ))}
      <span className="text-xs text-muted-foreground">
        {sameSource
          ? "Triggered mapping recomputation by the engine"
          : "Mapping recomputation by the engine"}
      </span>
      {typeof initiated === "string" && !sameSource && (
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-xs text-muted-foreground">Triggered by</span>
          <ActorChip actor={initiated} />
        </div>
      )}
    </div>
  )
}
